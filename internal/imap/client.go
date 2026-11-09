package imap

import (
	"context"
	"crypto/tls"
	"errors"
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
	conn                *imapclient.Client
	selectedUIDValidity uint32
	netConn             net.Conn
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

func (c *Client) Connect(ctx context.Context) error {
	if (c.cfg.UsesGoogleOAuth2() || c.cfg.UsesMicrosoftOAuth2()) && !c.cfg.IMAPTLS {
		return fmt.Errorf("OAuth requires an encrypted IMAP connection; enable IMAP TLS")
	}
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
	opts := &imapclient.Options{WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader}}
	if c.onUpdate != nil {
		notify := c.onUpdate
		opts.UnilateralDataHandler = &imapclient.UnilateralDataHandler{
			Mailbox: func(d *imapclient.UnilateralDataMailbox) { notify(MailboxUpdate{NumMessages: d.NumMessages}) },
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
		return auth.GoogleAccessToken(ctx, c.cfg.ClientID, c.cfg.ClientSecret, c.cfg.SessionKey(), c.cfg.RefreshToken)
	case c.cfg.UsesMicrosoftOAuth2():
		return auth.MSAccessToken(ctx, c.cfg.ClientID, c.cfg.SessionKey(), c.cfg.RefreshToken)
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

// interrupt aborts whatever operation is in flight on the connection, for
// shutdown. It closes the transport rather than only expiring its deadline:
// go-imap resets the read deadline around every response it reads, so an
// expired deadline set between two responses is erased and the reader then
// blocks with no deadline at all, which hung quitting mid-sync. A closed
// socket can't be reopened, so the blocked read always fails. For TLS the raw
// TCP socket is closed, so nothing waits on a close_notify write.
func (c *Client) interrupt() {
	conn := c.netConn
	if conn == nil {
		return
	}
	conn.SetDeadline(time.Now()) //nolint:errcheck
	if tc, ok := conn.(*tls.Conn); ok {
		conn = tc.NetConn()
	}
	conn.Close() //nolint:errcheck
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
	Flags   []string
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
			sm.Flags = append(sm.Flags, string(f))
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
			Delimiter: listDelimiter(mb.Delim),
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

// MessagesPerFolderFirstSync is the smaller first page used for folders other
// than the inbox. A fetch pulls whole bodies, attachments included, and buffers
// the batch before returning — and Sent is exactly where a user's large
// outgoing attachments accumulate. Asking Gmail for 100 of those makes it drop
// the connection mid-response ("unexpected EOF") after about a minute, where 25
// completes in three seconds. Older mail still pages in on demand.
const MessagesPerFolderFirstSync = 25

func (c *Client) FetchMessages(ctx context.Context, mailboxName string, limit int) ([]db.Message, error) {
	return c.fetchMessages(ctx, mailboxName, limit, time.Time{})
}

func (c *Client) FetchSince(ctx context.Context, mailboxName string, since time.Time) ([]db.Message, error) {
	return c.fetchMessages(ctx, mailboxName, MessagesPerInitialSync, since)
}

// FetchSinceLimit returns at most limit messages. Callers processing a whole
// incremental backlog should use FetchSincePage and follow its more result.
func (c *Client) FetchSinceLimit(ctx context.Context, mailboxName string, since time.Time, limit int) ([]db.Message, error) {
	if limit <= 0 {
		limit = MessagesPerInitialSync
	}
	return c.fetchMessages(ctx, mailboxName, limit, since)
}

// AppendSent stores a copy of an outgoing message in mailboxName, flagged
// \Seen because mail you just sent is not unread. SMTP submission leaves no
// copy behind, so without this the Sent folder stays empty on any server that
// does not file submitted mail itself.
//
// APPEND writes to a mailbox without selecting it, so this does not disturb
// whichever mailbox the pooled connection currently has selected.
func (c *Client) AppendSent(ctx context.Context, mailboxName string, raw []byte, when time.Time) error {
	return c.AppendWithFlags(ctx, mailboxName, raw, when, imap.FlagSeen)
}

// AppendWithFlags stores raw in mailboxName with the given flags (for example \Seen, or
// \Seen and \Draft for a saved draft).
func (c *Client) AppendWithFlags(ctx context.Context, mailboxName string, raw []byte, when time.Time, flags ...imap.Flag) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if len(raw) == 0 {
		return fmt.Errorf("empty message")
	}
	defer c.applyDeadline(ctx)()

	cmd := c.conn.Append(mailboxName, int64(len(raw)), &imap.AppendOptions{
		Flags: flags,
		Time:  when,
	})
	if _, err := cmd.Write(raw); err != nil {
		// The literal must still be closed, or the encoder keeps hold of the
		// connection and every later command on this pooled session hangs.
		cmd.Close() //nolint:errcheck // the write error is the one worth reporting
		return fmt.Errorf("append %s: %w", mailboxName, err)
	}
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("append %s: %w", mailboxName, err)
	}
	if _, err := cmd.Wait(); err != nil {
		return fmt.Errorf("append %s: %w", mailboxName, err)
	}
	return nil
}

// ErrUIDValidityChanged means the server renumbered a mailbox since its UIDs
// were cached, so a write by cached UID would hit a different message. The next
// sync notices the new UIDVALIDITY and rebuilds the cache.
var ErrUIDValidityChanged = errors.New("mailbox changed on the server; sync before trying again")

type uidValidityKey struct{}

type uidValidityExpectation struct {
	mailbox string
	value   uint32
}

// WithUIDValidity records which UIDVALIDITY the caller's cached UIDs for
// mailbox belong to. A UID write to that mailbox under the returned context
// refuses to run (ErrUIDValidityChanged) when the server reports a different
// one. Zero, or another mailbox, leaves the check off.
func WithUIDValidity(ctx context.Context, mailbox string, uidValidity uint32) context.Context {
	return context.WithValue(ctx, uidValidityKey{}, uidValidityExpectation{mailbox, uidValidity})
}

// selectForUIDWrite selects mailbox read-write and enforces the context's
// expected UIDVALIDITY, if any, before UIDs are used for a write.
func (c *Client) selectForUIDWrite(ctx context.Context, mailboxName string) error {
	selected, err := c.conn.Select(mailboxName, nil).Wait()
	if err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}
	if want, ok := ctx.Value(uidValidityKey{}).(uidValidityExpectation); ok &&
		want.mailbox == mailboxName && want.value != 0 && selected.UIDValidity != want.value {
		return ErrUIDValidityChanged
	}
	c.selectedUIDValidity = selected.UIDValidity
	return nil
}

func (c *Client) MarkSeen(ctx context.Context, mailboxName string, uid uint32, seen bool) error {
	return c.MarkSeenUIDs(ctx, mailboxName, []uint32{uid}, seen)
}

// MarkSeenUIDs updates the \Seen flag for a set of UIDs with one IMAP STORE.
func (c *Client) MarkSeenUIDs(ctx context.Context, mailboxName string, uids []uint32, seen bool) error {
	if len(uids) == 0 {
		return nil
	}
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	if err := c.selectForUIDWrite(ctx, mailboxName); err != nil {
		return err
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
	uidSet := imap.UIDSetNum()
	for _, uid := range uids {
		uidSet.AddNum(imap.UID(uid))
	}
	return c.conn.Store(uidSet, flags, nil).Close()
}

// MarkFlagged adds or removes the \Flagged keyword (the "star") on a message.
func (c *Client) MarkFlagged(ctx context.Context, mailboxName string, uid uint32, flagged bool) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	if err := c.selectForUIDWrite(ctx, mailboxName); err != nil {
		return err
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
	// A plain EXPUNGE removes every \Deleted message in the mailbox, including
	// ones another client flagged but has not expunged. Lift \Deleted from
	// those for the duration, then put it back.
	found, err := c.conn.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagDeleted}}, nil).Wait()
	if err != nil {
		return fmt.Errorf("find other deleted messages: %w", err)
	}
	var others imap.UIDSet
	for _, uid := range found.AllUIDs() {
		if !uidSet.Contains(uid) {
			others.AddNum(uid)
		}
	}
	keep := func(op imap.StoreFlagsOp) error {
		return c.conn.Store(others, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close()
	}
	if len(others) > 0 {
		if err := keep(imap.StoreFlagsDel); err != nil {
			return fmt.Errorf("protect other deleted messages: %w", err)
		}
	}
	_, expungeErr := c.conn.Expunge().Collect()
	if len(others) > 0 {
		if err := keep(imap.StoreFlagsAdd); err != nil && expungeErr == nil {
			return fmt.Errorf("restore other deleted messages: %w", err)
		}
	}
	if expungeErr != nil {
		return fmt.Errorf("expunge: %w", expungeErr)
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
	if err := c.selectForUIDWrite(ctx, mailboxName); err != nil {
		return err
	}
	uidSet := uidSetOf(uids)
	caps := c.conn.Caps()
	if !caps.Has(imap.CapMove) && !caps.Has(imap.CapIMAP4rev2) && !caps.Has(imap.CapUIDPlus) {
		// go-imap's MOVE fallback would finish with a plain EXPUNGE, removing
		// other clients' \Deleted messages too. Copy, then expunge only ours.
		validity := c.selectedUIDValidity
		if _, err := c.conn.Copy(uidSet, targetMailbox).Wait(); err != nil {
			if !isCopyUIDParseError(err) {
				return fmt.Errorf("copy to %s: %w", targetMailbox, err)
			}
			if validity == 0 {
				return &PartialMoveError{Target: targetMailbox, Err: ErrUIDValidityChanged}
			}
			recovery := New(c.cfg)
			if err := recovery.Connect(ctx); err != nil {
				return &PartialMoveError{Target: targetMailbox, Err: err}
			}
			defer recovery.Close() //nolint:errcheck
			recoveryCtx := WithUIDValidity(ctx, mailboxName, validity)
			defer recovery.applyDeadline(recoveryCtx)()
			if err := recovery.selectForUIDWrite(recoveryCtx, mailboxName); err != nil {
				return &PartialMoveError{Target: targetMailbox, Err: err}
			}
			if err := recovery.markDeletedAndExpunge(uidSet); err != nil {
				return &PartialMoveError{Target: targetMailbox, Err: err}
			}
			return nil
		}
		return c.markDeletedAndExpunge(uidSet)
	}
	var err error
	for attempt := 0; ; attempt++ {
		_, err = c.conn.Move(uidSet, targetMailbox).Wait()
		if err == nil {
			return nil
		}
		if isCopyUIDParseError(err) {
			// The MOVE succeeded; go-imap only choked parsing the COPYUID
			// resp-code on the OK response (see isCopyUIDParseError).
			return nil
		}
		if attempt == 0 && !isTransientServerError(err) {
			break
		}
		// Gmail sometimes completes the move and still answers NO, so look
		// at the source mailbox before deciding anything: if our messages
		// are gone, the move worked. This also covers a retry that fails
		// because the earlier attempt had in fact moved the mail. If the
		// check itself fails, report the original error rather than
		// re-sending a MOVE that may already have happened.
		remaining, serr := c.remainingUIDs(uidSet)
		if serr != nil {
			break
		}
		if len(remaining) == 0 {
			return nil
		}
		if !isTransientServerError(err) || attempt >= len(moveRetryDelays) {
			break
		}
		select {
		case <-time.After(moveRetryDelays[attempt]):
		case <-ctx.Done():
			return fmt.Errorf("move to %s: %w", targetMailbox, err)
		}
	}
	return fmt.Errorf("move to %s: %w", targetMailbox, err)
}

// remainingUIDs returns which of uidSet are still present in the selected mailbox.
func (c *Client) remainingUIDs(uidSet imap.UIDSet) ([]imap.UID, error) {
	found, err := c.conn.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{uidSet}}, nil).Wait()
	if err != nil {
		return nil, err
	}
	return found.AllUIDs(), nil
}

// moveRetryDelays is the pause before each retry of a MOVE the server refused
// with a transient error. A var so tests can shorten it.
var moveRetryDelays = []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond}

// isTransientServerError reports whether err is a tagged NO that the server
// itself calls a system failure. Gmail answers moves to [Gmail]/Trash with
// "NO System Error (Failure)" intermittently, sometimes after the move already
// happened and sometimes before it did.
func isTransientServerError(err error) bool {
	var ie *imap.Error
	if !errors.As(err, &ie) || ie.Type != imap.StatusResponseTypeNo {
		return false
	}
	return strings.Contains(strings.ToLower(ie.Text), "system error")
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

// RenameMailbox renames a mailbox (and, on most servers, its children).
func (c *Client) RenameMailbox(ctx context.Context, oldName, newName string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	if err := c.conn.Rename(oldName, newName, nil).Wait(); err != nil {
		return fmt.Errorf("rename %s: %w", oldName, err)
	}
	return nil
}

// DeleteMailbox removes a mailbox and every message in it.
func (c *Client) DeleteMailbox(ctx context.Context, name string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	if err := c.conn.Delete(name).Wait(); err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
	}
	return nil
}

// UnseenCounts returns the number of unread messages in each named mailbox, using STATUS (which does
// not select the mailbox). A mailbox the server will not report on is left out of the result.
func (c *Client) UnseenCounts(ctx context.Context, names []string) (map[string]uint32, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}
	out := make(map[string]uint32, len(names))
	for _, name := range names {
		func() {
			defer c.applyDeadline(ctx)()
			data, err := c.conn.Status(name, &imap.StatusOptions{NumUnseen: true}).Wait()
			if err != nil || data == nil || data.NumUnseen == nil {
				return
			}
			out[name] = *data.NumUnseen
		}()
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
	}
	return out, nil
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
	if err := c.selectForUIDWrite(ctx, mailboxName); err != nil {
		return err
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

// listDelimiter turns a LIST hierarchy delimiter into a string. A flat
// namespace reports NIL, which the library decodes as the zero rune; that means
// "no hierarchy", not a NUL delimiter.
func listDelimiter(r rune) string {
	if r == 0 {
		return ""
	}
	return string(r)
}

// MarkAllSeen searches the server, including unread mail not cached locally.
func (c *Client) MarkAllSeen(ctx context.Context, mailbox string) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	if err := c.selectForUIDWrite(ctx, mailbox); err != nil {
		return err
	}
	found, err := c.conn.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil {
		return fmt.Errorf("search unread: %w", err)
	}
	uids := found.AllUIDs()
	for len(uids) > 0 {
		n := min(1000, len(uids))
		if err := c.conn.Store(imap.UIDSetNum(uids[:n]...), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
			return fmt.Errorf("mark unread messages seen: %w", err)
		}
		uids = uids[n:]
	}
	return nil
}

// PartialMoveError means COPY completed but source removal could not be confirmed.
// Callers must not automatically retry the original COPY operation.
type PartialMoveError struct {
	Target string
	Err    error
}

func (e *PartialMoveError) Error() string {
	return fmt.Sprintf("copied to %s; source cleanup unconfirmed: %v", e.Target, e.Err)
}
func (e *PartialMoveError) Unwrap() error { return e.Err }
