package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/auth"
	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
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
	// onUpdate, when set, is invoked from the client's read goroutine whenever
	// the server sends unilateral mailbox data for the selected mailbox. Used
	// by Watcher for IMAP IDLE push notifications.
	onUpdate func(MailboxUpdate)
}

// MailboxUpdate is unilateral mailbox data relayed to NewWithUpdates' callback.
// NumMessages is set for EXISTS updates (nil for flags-only chatter);
// Expunged marks an EXPUNGE.
type MailboxUpdate struct {
	NumMessages *uint32
	Expunged    bool
}

func New(cfg config.AccountConfig) *Client {
	return &Client{cfg: cfg}
}

// NewWithUpdates returns a Client that reports unilateral mailbox updates
// (EXISTS/FLAGS/EXPUNGE while a mailbox is selected) by calling onUpdate.
// onUpdate runs on the connection's read goroutine and must not block.
func NewWithUpdates(cfg config.AccountConfig, onUpdate func(MailboxUpdate)) *Client {
	return &Client{cfg: cfg, onUpdate: onUpdate}
}

// headerWordDecoder decodes RFC 2047 encoded words in envelope text (subjects
// and address display names) with go-message's full charset table.
//
// go-imap's fallback is a bare mime.WordDecoder, whose nil CharsetReader knows
// only utf-8, us-ascii and iso-8859-1; for anything else it errors, and
// Options.decodeText then returns the encoded word unchanged. Subjects in
// Windows-1252, Shift_JIS, GB2312 or KOI8-R were stored and displayed as
// literal =?windows-1252?Q?...?= text. The blank charset import in parse.go
// registers a reader for message bodies only, not for header words. -allie
func headerWordDecoder() *mime.WordDecoder {
	return &mime.WordDecoder{CharsetReader: charset.Reader}
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
	opts := &imapclient.Options{
		WordDecoder: headerWordDecoder(),
	}
	if c.onUpdate != nil {
		notify := c.onUpdate
		opts.UnilateralDataHandler = &imapclient.UnilateralDataHandler{
			Mailbox: func(d *imapclient.UnilateralDataMailbox) {
				notify(MailboxUpdate{NumMessages: d.NumMessages})
			},
			Expunge: func(uint32) { notify(MailboxUpdate{Expunged: true}) },
		}
	}
	client := imapclient.New(conn, opts)

	// Gmail / Outlook accounts with a refresh token authenticate over SASL
	// XOAUTH2; everything else uses an app password over IMAP LOGIN (Gmail
	// requires an app password + 2FA).
	var loginErr error
	if tok, err := c.oauthAccessToken(ctx); err != nil {
		client.Close()
		c.netConn = nil
		return fmt.Errorf("oauth2: %w", err)
	} else if tok != "" {
		clear := c.applyDeadline(ctx)
		loginErr = client.Authenticate(&xoauth2Client{user: c.cfg.User, token: tok})
		clear()
	} else {
		clear := c.applyDeadline(ctx)
		loginErr = client.Login(c.cfg.User, c.cfg.Password).Wait()
		clear()
	}
	if loginErr != nil {
		client.Close()
		c.netConn = nil
		return fmt.Errorf("login: %w", loginErr)
	}
	c.conn = client
	return nil
}

// oauthAccessToken returns a fresh XOAUTH2 access token for an OAuth account, or
// "" when the account authenticates with a password.
func (c *Client) oauthAccessToken(ctx context.Context) (string, error) {
	switch {
	case c.cfg.UsesGoogleOAuth2():
		return auth.GoogleAccessToken(ctx, c.cfg.ClientID, c.cfg.ClientSecret, c.cfg.Name, c.cfg.RefreshToken)
	case c.cfg.UsesMicrosoftOAuth2():
		return auth.MSAccessToken(ctx, c.cfg.ClientID, c.cfg.Name, c.cfg.RefreshToken)
	default:
		return "", nil
	}
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
	return c.closeConn(true)
}

// interrupt expires the socket so any in-flight read/write returns immediately
// with a deadline error. It is safe to call concurrently with an operation in
// progress (net.Conn methods are goroutine-safe) and is used on shutdown to abort
// a blocked fetch instead of waiting the full network timeout for it to finish.
func (c *Client) interrupt() {
	if c.netConn != nil {
		c.netConn.SetDeadline(time.Now()) //nolint:errcheck
	}
}

// closeConn tears down the connection. When logout is true it sends a graceful
// IMAP LOGOUT first, but bounded by a short socket deadline so a wedged
// connection can't hang the caller (quitting must not wait on the network — a
// dropped TCP connection is fine for IMAP servers). Callers that have already
// expired the socket (e.g. an IDLE watcher shutting down) pass logout=false to
// skip the pointless round-trip entirely.
func (c *Client) closeConn(logout bool) error {
	if c.conn == nil {
		return nil
	}
	if logout {
		if c.netConn != nil {
			c.netConn.SetDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
		}
		c.conn.Logout() //nolint:errcheck
	}
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
// the UID and whether the server considers the message read (\Seen) or starred
// (\Flagged).
type ServerMessage struct {
	UID     uint32
	Seen    bool
	Flagged bool
}

// ServerState returns the state of every message currently in the mailbox plus
// the mailbox's UIDVALIDITY, via one FETCH 1:* (UID FLAGS). Sync uses it for two
// things the additive SINCE fetch can't: reconciling away messages removed
// server-side, and adopting read/unread and starred changes made in another
// client. An empty (non-nil-error) result for a non-empty mailbox is impossible:
// NumMessages==0 short-circuits, otherwise 1:* yields every message.
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
			}
			if f == imap.FlagFlagged {
				sm.Flagged = true
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

// MessagesPerInitialSync bounds the first sync of a mailbox: it fetches this
// many of the most recent messages rather than the entire history, which on a
// large mailbox would mean downloading every body before the UI showed
// anything. Older mail is paged in on demand — see Client.FetchOlderThan.
const MessagesPerInitialSync = 100

func (c *Client) FetchMessages(ctx context.Context, mailboxName string, limit int) ([]db.Message, error) {
	return c.fetchMessages(ctx, mailboxName, limit, time.Time{})
}

func (c *Client) FetchSince(ctx context.Context, mailboxName string, since time.Time) ([]db.Message, error) {
	return c.fetchMessages(ctx, mailboxName, MessagesPerInitialSync, since)
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

// MarkFlagged adds or removes the \Flagged keyword (the "star") on a message.
func (c *Client) MarkFlagged(ctx context.Context, mailboxName string, uid uint32, flagged bool) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}

	op := imap.StoreFlagsDel
	if flagged {
		op = imap.StoreFlagsAdd
	}
	flags := &imap.StoreFlags{
		Op:     op,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagFlagged},
	}
	return c.conn.Store(imap.UIDSetNum(imap.UID(uid)), flags, nil).Close()
}

// DeleteResult reports what the server-side delete actually did.
type DeleteResult struct {
	// ExpungeSkipped is set when the messages were flagged \Deleted but not
	// purged from the server, because purging them would have destroyed other
	// messages too. They are gone locally and the server will remove them
	// whenever something else expunges the mailbox.
	ExpungeSkipped bool
	// OthersFlagged is how many messages the skipped purge would have destroyed.
	OthersFlagged int
}

func (c *Client) markDeletedAndExpunge(uidSet imap.UIDSet) (DeleteResult, error) {
	flags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagDeleted},
	}
	if err := c.conn.Store(uidSet, flags, nil).Close(); err != nil {
		return DeleteResult{}, fmt.Errorf("mark deleted: %w", err)
	}
	if c.conn.Caps().Has(imap.CapUIDPlus) || c.conn.Caps().Has(imap.CapIMAP4rev2) {
		if _, err := c.conn.UIDExpunge(uidSet).Collect(); err != nil {
			return DeleteResult{}, fmt.Errorf("expunge: %w", err)
		}
		return DeleteResult{}, nil
	}

	// Without UIDPLUS there is no way to purge specific UIDs: a bare EXPUNGE
	// permanently removes every message in the mailbox carrying \Deleted, not
	// just the ones asked for. Another client — or the user in webmail — can
	// easily have left that flag on mail they still want.
	//
	// It is not blind, though: the server can be asked who else is flagged. When
	// nobody is, a bare EXPUNGE is exactly equivalent to UID EXPUNGE and runs as
	// before, which is the usual case. When somebody is, the purge is skipped
	// rather than destroying mail the user never selected — the messages stay
	// flagged and the server removes them whenever it next expunges. A failed
	// check is treated the same way: without proof it is safe, do not purge. -allie
	others, err := c.otherDeletedUIDs(uidSet)
	if err != nil || len(others) > 0 {
		return DeleteResult{ExpungeSkipped: true, OthersFlagged: len(others)}, nil
	}
	if _, err := c.conn.Expunge().Collect(); err != nil {
		return DeleteResult{}, fmt.Errorf("expunge: %w", err)
	}
	return DeleteResult{}, nil
}

// otherDeletedUIDs returns the UIDs flagged \Deleted in the selected mailbox
// that are not in ours — the messages a bare EXPUNGE would destroy as collateral.
func (c *Client) otherDeletedUIDs(ours imap.UIDSet) ([]uint32, error) {
	data, err := c.conn.UIDSearch(&imap.SearchCriteria{
		Flag: []imap.Flag{imap.FlagDeleted},
	}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search deleted: %w", err)
	}
	var others []uint32
	for _, uid := range data.AllUIDs() {
		if !ours.Contains(uid) {
			others = append(others, uint32(uid))
		}
	}
	return others, nil
}

// MoveResult reports where a MOVE landed. DestUIDs maps each source UID to the
// UID the message now has in the destination mailbox, which the server reports
// in the COPYUID resp-code (RFC 4315). It is nil when no usable COPYUID came
// back — the server lacks UIDPLUS/IMAP4rev2, or it sent a malformed code we
// deliberately tolerate (see isCopyUIDParseError). Callers must treat a missing
// entry as "destination UID unknown"; a stored UID from the source mailbox is
// always wrong, because UIDs are per-mailbox. -allie
type MoveResult struct {
	UIDValidity uint32
	DestUIDs    map[uint32]uint32
}

// DestUID returns the destination UID for a source UID, or 0 when the server
// did not report one.
func (r MoveResult) DestUID(sourceUID uint32) uint32 {
	return r.DestUIDs[sourceUID]
}

func (c *Client) MoveMessage(ctx context.Context, mailboxName string, uid uint32, targetMailbox string) (uint32, error) {
	res, err := c.MoveMessages(ctx, mailboxName, []uint32{uid}, targetMailbox)
	if err != nil {
		return 0, err
	}
	return res.DestUID(uid), nil
}

// MoveMessages moves a batch of messages in one SELECT/MOVE round trip when the
// server supports MOVE. The IMAP library falls back internally when needed.
// Bulk actions must share one connection: issuing one connection per message
// trips per-user connection caps (Gmail: 15, Dovecot default: 10) and the
// overflow silently fails.
func (c *Client) MoveMessages(ctx context.Context, mailboxName string, uids []uint32, targetMailbox string) (MoveResult, error) {
	if c.conn == nil {
		return MoveResult{}, fmt.Errorf("not connected")
	}
	if len(uids) == 0 {
		return MoveResult{}, nil
	}
	defer c.applyDeadline(ctx)()
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return MoveResult{}, fmt.Errorf("select %s: %w", mailboxName, err)
	}
	uidSet := uidSetOf(uids)
	data, err := c.conn.Move(uidSet, targetMailbox).Wait()
	if err != nil {
		if isCopyUIDParseError(err) {
			// The MOVE succeeded; go-imap only choked parsing the COPYUID
			// resp-code on the OK response (see isCopyUIDParseError). The
			// destination UIDs are lost with it, so report none.
			return MoveResult{}, nil
		}
		return MoveResult{}, fmt.Errorf("move to %s: %w", targetMailbox, err)
	}
	return moveResultOf(data), nil
}

// moveResultOf pairs the COPYUID source and destination UID sets. RFC 4315 says
// the two lists correspond element by element once expanded, so a length
// mismatch (or a dynamic set containing "*") means we cannot trust the pairing
// and report nothing rather than guess a wrong UID.
func moveResultOf(data *imapclient.MoveData) MoveResult {
	if data == nil || data.SourceUIDs == nil || data.DestUIDs == nil {
		return MoveResult{}
	}
	src, srcOK := uidsOf(data.SourceUIDs)
	dst, dstOK := uidsOf(data.DestUIDs)
	if !srcOK || !dstOK || len(src) == 0 || len(src) != len(dst) {
		return MoveResult{UIDValidity: data.UIDValidity}
	}
	pairs := make(map[uint32]uint32, len(src))
	for i, s := range src {
		pairs[s] = dst[i]
	}
	return MoveResult{UIDValidity: data.UIDValidity, DestUIDs: pairs}
}

func uidsOf(set imap.NumSet) ([]uint32, bool) {
	uidSet, ok := set.(imap.UIDSet)
	if !ok {
		return nil, false
	}
	nums, ok := uidSet.Nums()
	if !ok {
		return nil, false
	}
	out := make([]uint32, 0, len(nums))
	for _, n := range nums {
		out = append(out, uint32(n))
	}
	return out, true
}

// isCopyUIDParseError reports whether err is go-imap failing to parse a COPYUID
// resp-code rather than the MOVE/COPY itself failing. Gmail returns a malformed
// `[COPYUID <validity> <src> 0]` on a successful MOVE to Trash, and go-imap
// rejects the `0` ("imap: bad number set value") because IMAP seq-numbers must
// be >= 1. Per RFC 4315 the COPYUID code only appears in a tagged OK response,
// so a parse failure here means the server completed the move — we treat it as
// success. (The failed parse also tripped go-imap's read loop and closed this
// connection; the SessionPool's NOOP probe reconnects before the next use.)
func isCopyUIDParseError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "resp-code-copy")
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

func (c *Client) DeleteMessage(ctx context.Context, mailboxName string, uid uint32) (DeleteResult, error) {
	return c.DeleteMessages(ctx, mailboxName, []uint32{uid})
}

// DeleteMessages expunges a batch of messages in one SELECT/STORE/EXPUNGE round
// trip on this connection (see MoveMessages for why batching matters).
func (c *Client) DeleteMessages(ctx context.Context, mailboxName string, uids []uint32) (DeleteResult, error) {
	if c.conn == nil {
		return DeleteResult{}, fmt.Errorf("not connected")
	}
	if len(uids) == 0 {
		return DeleteResult{}, nil
	}
	defer c.applyDeadline(ctx)()
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return DeleteResult{}, fmt.Errorf("select %s: %w", mailboxName, err)
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
