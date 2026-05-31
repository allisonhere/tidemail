package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/allisonhere/tide/internal/auth"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
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

	if c.cfg.UsesOAuth2() {
		// Refresh the access token, then authenticate with XOAUTH2.
		accessToken := c.cfg.Password // May have been set with initial access token
		if c.cfg.RefreshToken != "" {
			tok, err := auth.RefreshAccessToken(c.cfg.ClientID, c.cfg.ClientSecret, c.cfg.RefreshToken)
			if err != nil {
				client.Close()
				return fmt.Errorf("oauth2 refresh: %w", err)
			}
			accessToken = tok.AccessToken
		}
		if accessToken == "" {
			client.Close()
			return fmt.Errorf("oauth2: no access token or refresh token available")
		}
		saslClient := sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
			Username: c.cfg.User,
			Token:    accessToken,
		})
		if err := client.Authenticate(saslClient); err != nil {
			client.Close()
			return fmt.Errorf("oauth2 auth: %w", err)
		}
	} else {
		if err := client.Login(c.cfg.User, c.cfg.Password).Wait(); err != nil {
			client.Close()
			return fmt.Errorf("login: %w", err)
		}
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
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}
	uidSet := imap.UIDSetNum(imap.UID(uid))
	if _, err := c.conn.Copy(uidSet, targetMailbox).Wait(); err != nil {
		return fmt.Errorf("copy to %s: %w", targetMailbox, err)
	}
	return c.markDeletedAndExpunge(uidSet)
}

func (c *Client) DeleteMessage(ctx context.Context, mailboxName string, uid uint32) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	if _, err := c.conn.Select(mailboxName, nil).Wait(); err != nil {
		return fmt.Errorf("select %s: %w", mailboxName, err)
	}
	return c.markDeletedAndExpunge(imap.UIDSetNum(imap.UID(uid)))
}
