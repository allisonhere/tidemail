package imap

import (
	"context"
	"github.com/allisonhere/tidemail/internal/config"
	"strings"
	"testing"
)

func TestOAuthRequiresIMAPTLSBeforeConnectingOrRefreshing(t *testing.T) {
	for _, provider := range []string{"Gmail", "Outlook"} {
		// Invalid port plus absent credentials prove that transport validation happens
		// before a dial or token refresh could be attempted.
		c := New(config.AccountConfig{Provider: provider, AuthMethod: config.AuthOAuth2, IMAPHost: "127.0.0.1", IMAPPort: -1, IMAPTLS: false})
		err := c.Connect(context.Background())
		if err == nil || !strings.Contains(err.Error(), "enable IMAP TLS") {
			t.Fatalf("%s: %v", provider, err)
		}
		if c.netConn != nil || c.conn != nil {
			t.Fatal("plaintext connection opened")
		}
	}
}
