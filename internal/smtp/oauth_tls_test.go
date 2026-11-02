package smtp

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/allisonhere/tidemail/internal/config"
	"net"
	"strings"
	"testing"
)

func TestOAuthRequiresSTARTTLSBeforeConnectingOrRefreshing(t *testing.T) {
	for _, provider := range []string{"Gmail", "Outlook"} {
		cfg := config.AccountConfig{Provider: provider, AuthMethod: config.AuthOAuth2, User: "test@example.com", SMTPHost: "127.0.0.1", SMTPPort: -1, SMTPTLS: false}
		err := Send(context.Background(), cfg, OutgoingMessage{To: []string{"to@example.com"}, Body: "hello"})
		if err == nil || !strings.Contains(err.Error(), "enable SMTP TLS (STARTTLS) or use port 465") {
			t.Fatalf("%s: %v", provider, err)
		}
	}
}

func TestOAuthPort465UsesImplicitTLSWithSTARTTLSFlagOff(t *testing.T) {
	old := tlsDial
	defer func() { tlsDial = old }()
	expected := errors.New("TLS dial reached")
	tlsDial = func(context.Context, string, string, *tls.Config) (net.Conn, error) { return nil, expected }
	cfg := config.AccountConfig{Provider: "Outlook", AuthMethod: config.AuthOAuth2, User: "test@example.com", SMTPHost: "smtp.example.com", SMTPPort: 465, SMTPTLS: false}
	err := Send(context.Background(), cfg, OutgoingMessage{To: []string{"to@example.com"}, Body: "hello"})
	if !errors.Is(err, expected) {
		t.Fatalf("implicit TLS blocked: %v", err)
	}
}
