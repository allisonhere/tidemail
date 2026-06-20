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
	cfg config.AccountConfig
	// conn is the IMAP protocol client; netConn is the underlying transport it
	// rides on, retained so per-operation deadlines can be applied (go-imap v2
	// commands don't take a context, so a deadline on the socket is the only way
	// to keep a hung read from blocking forever — see applyDeadline).
	conn    *imapclient.Client
	netConn net.Conn
}

func New(cfg config.AccountConfig) *Client {
	return &Client{cfg: cfg}
}

func (c *Client) Connect(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", c.cfg.IMAPHost, c.cfg.IMAPPort)
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	if c.cfg.IMAPTLS {
		// Dial the TCP socket ourselves (above) and layer TLS on top, rather than
		// imapclient.DialTLS, so we keep a handle on the transport for deadlines
		// and so the handshake honors ctx.
		tlsConn := tls.Client(conn, &tls.Config{ServerName: c.cfg.IMAPHost})
		if dl, ok := ctx.Deadline(); ok {
			tlsConn.SetDeadline(dl) //nolint:errcheck
		}
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			tlsConn.Close() //nolint:errcheck
			return fmt.Errorf("tls handshake %s: %w", addr, err)
		}
		tlsConn.SetDeadline(time.Time{}) //nolint:errcheck
		conn = tlsConn
	}
	c.netConn = conn
	client := imapclient.New(conn, nil)

	// TideMail authenticates with an app password over IMAP LOGIN (Gmail
	// requires an app password + 2FA).
	clear := c.applyDeadline(ctx)
	loginErr := client.Login(c.cfg.User, c.cfg.Password).Wait()
	clear()
	if loginErr != nil {
		client.Close()
		c.netConn = nil
		return fmt.Errorf("login: %w", loginErr)
	}
	c.conn = client
	return nil
}

// applyDeadline bounds the IMAP commands that follow it by ctx's deadline and
// returns a func that clears the deadline again; callers use it as
// `defer c.applyDeadline(ctx)()`. go-imap v2's command results
// (.Wait()/.Collect()) block on the socket with no context awareness, so a
// stalled server read would otherwise hang indefinitely and — because the
// SessionPool serializes one operation per account — wedge every later
// operation for that account. Setting the deadline on the transport makes the
// blocked read fail; the timed-out connection is torn down and the pool dials a
// fresh one on next use.
func (c *Client) applyDeadline(ctx context.Context) func() {
	if c.netConn == nil {
		return func() {}
	}
	dl, ok := ctx.Deadline()
	if !ok {
		return func() {}
	}
	c.netConn.SetDeadline(dl)                            //nolint:errcheck
	return func() { c.netConn.SetDeadline(time.Time{}) } //nolint:errcheck
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	c.conn.Logout() //nolint:errcheck
	err := c.conn.Close()
	c.conn = nil
	c.netConn = nil
	return err
}

// Noop probes connection liveness; the SessionPool uses it to validate a
// pooled connection before reuse.
func (c *Client) Noop(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
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
	defer c.applyDeadline(ctx)()
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
	defer c.applyDeadline(ctx)()

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
	defer c.applyDeadline(ctx)()
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
	defer c.applyDeadline(ctx)()
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
	defer c.applyDeadline(ctx)()
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
	defer c.applyDeadline(ctx)()
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
