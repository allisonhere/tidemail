package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
)

type MailboxInfo struct {
	Name      string
	Delimiter string
	Flags     []string
}

type Client struct {
	cfg  config.AccountConfig
	conn *imapclient.Client
}

func New(cfg config.AccountConfig) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Connect(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", c.cfg.IMAPHost, c.cfg.IMAPPort)
	var (
		client *imapclient.Client
		err    error
	)
	if c.cfg.IMAPTLS {
		tlsCfg := &tls.Config{ServerName: c.cfg.IMAPHost}
		client, err = imapclient.DialTLS(addr, &imapclient.Options{TLSConfig: tlsCfg})
	} else {
		var conn net.Conn
		dialer := &net.Dialer{}
		conn, err = dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("dial %s: %w", addr, err)
		}
		client = imapclient.New(conn, nil)
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}

	// TideMail authenticates with an app password over IMAP LOGIN (Gmail
	// requires an app password + 2FA).
	if err := client.Login(c.cfg.User, c.cfg.Password).Wait(); err != nil {
		client.Close()
		return fmt.Errorf("login: %w", err)
	}
	c.conn = client
	return nil
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	c.conn.Logout() //nolint:errcheck
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Noop probes connection liveness; the SessionPool uses it to validate a
// pooled connection before reuse.
func (c *Client) Noop(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.Noop().Wait()
}

// ServerMessage is the lightweight per-message state used by sync reconciliation:
// the UID and whether the server considers the message read (\Seen).
type ServerMessage struct {
	UID  uint32
	Seen bool
}

// ServerState returns the state of every message currently in the mailbox plus
// the mailbox's UIDVALIDITY, via one FETCH 1:* (UID FLAGS). Sync uses it for two
// things the additive SINCE fetch can't: reconciling away messages removed
// server-side, and adopting read/unread changes made in another client. An empty
// (non-nil-error) result for a non-empty mailbox is impossible: NumMessages==0
// short-circuits, otherwise 1:* yields every message.
func (c *Client) ServerState(ctx context.Context, mailboxName string) (msgs []ServerMessage, uidValidity uint32, err error) {
	if c.conn == nil {
		return nil, 0, fmt.Errorf("not connected")
	}
	selectData, err := c.conn.Select(mailboxName, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, 0, fmt.Errorf("select %s: %w", mailboxName, err)
	}
	uidValidity = selectData.UIDValidity
	if selectData.NumMessages == 0 {
		return nil, uidValidity, nil
	}
	seqSet := imap.SeqSetNum()
	seqSet.AddRange(1, 0) // 1:* — 0 means "*"
	fetched, err := c.conn.Fetch(seqSet, &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return nil, uidValidity, fmt.Errorf("fetch state: %w", err)
	}
	msgs = make([]ServerMessage, 0, len(fetched))
	for _, m := range fetched {
		sm := ServerMessage{UID: uint32(m.UID)}
		for _, f := range m.Flags {
			if f == imap.FlagSeen {
				sm.Seen = true
				break
			}
		}
		msgs = append(msgs, sm)
	}
	return msgs, uidValidity, nil
}

func (c *Client) ListMailboxes(ctx context.Context) ([]MailboxInfo, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	cmd := c.conn.List("", "*", nil)
	data, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}

	var infos []MailboxInfo
	for _, mb := range data {
		info := MailboxInfo{
			Name:      mb.Mailbox,
			Delimiter: string(mb.Delim),
		}
		for _, f := range mb.Attrs {
			info.Flags = append(info.Flags, string(f))
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func (c *Client) FetchMessages(ctx context.Context, mailboxName string, limit int) ([]db.Message, error) {
	return c.fetchMessages(ctx, mailboxName, limit, time.Time{})
}

func (c *Client) FetchSince(ctx context.Context, mailboxName string, since time.Time) ([]db.Message, error) {
	return c.fetchMessages(ctx, mailboxName, 100, since)
}

func (c *Client) MarkSeen(ctx context.Context, mailboxName string, uid uint32, seen bool) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}

	op := imap.StoreFlagsDel
	if seen {
		op = imap.StoreFlagsAdd
	}
	flags := &imap.StoreFlags{
		Op:     op,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	}
	return c.conn.Store(imap.UIDSetNum(imap.UID(uid)), flags, nil).Close()
}

func (c *Client) markDeletedAndExpunge(uidSet imap.UIDSet) error {
	flags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}
	if err := c.conn.Store(uidSet, flags, nil).Close(); err != nil {
		return fmt.Errorf("mark deleted: %w", err)
	}
	if c.conn.Caps().Has(imap.CapUIDPlus) || c.conn.Caps().Has(imap.CapIMAP4rev2) {
		if _, err := c.conn.UIDExpunge(uidSet).Collect(); err != nil {
			return fmt.Errorf("expunge: %w", err)
		}
		return nil
	}
	if _, err := c.conn.Expunge().Collect(); err != nil {
		return fmt.Errorf("expunge: %w", err)
	}
	return nil
}

func (c *Client) MoveMessage(ctx context.Context, mailboxName string, uid uint32, targetMailbox string) error {
	return c.MoveMessages(ctx, mailboxName, []uint32{uid}, targetMailbox)
}

// MoveMessages moves a batch of messages in one SELECT/MOVE round trip when the
// server supports MOVE. The IMAP library falls back internally when needed.
// Bulk actions must share one connection: issuing one connection per message
// trips per-user connection caps (Gmail: 15, Dovecot default: 10) and the
// overflow silently fails.
func (c *Client) MoveMessages(ctx context.Context, mailboxName string, uids []uint32, targetMailbox string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if len(uids) == 0 {
		return nil
	}
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}
	uidSet := uidSetOf(uids)
	if _, err := c.conn.Move(uidSet, targetMailbox).Wait(); err != nil {
		return fmt.Errorf("move to %s: %w", targetMailbox, err)
	}
	return nil
}

func (c *Client) CreateMailbox(ctx context.Context, name string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if err := c.conn.Create(name, nil).Wait(); err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	return nil
}

func (c *Client) DeleteMessage(ctx context.Context, mailboxName string, uid uint32) error {
	return c.DeleteMessages(ctx, mailboxName, []uint32{uid})
}

// DeleteMessages expunges a batch of messages in one SELECT/STORE/EXPUNGE round
// trip on this connection (see MoveMessages for why batching matters).
func (c *Client) DeleteMessages(ctx context.Context, mailboxName string, uids []uint32) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if len(uids) == 0 {
		return nil
	}
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}
	return c.markDeletedAndExpunge(uidSetOf(uids))
}

func uidSetOf(uids []uint32) imap.UIDSet {
	var set imap.UIDSet
	for _, uid := range uids {
		set.AddNum(imap.UID(uid))
	}
	return set
}
