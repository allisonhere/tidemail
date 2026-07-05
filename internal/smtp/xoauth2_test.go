package smtp

import (
	"context"
	"net/smtp"
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
)

func TestXOAuth2AuthStartRawResponse(t *testing.T) {
	a := &xoauth2Auth{user: "me@outlook.com", token: "tok-123"}
	mech, resp, err := a.Start(&smtp.ServerInfo{Name: "smtp.office365.com", TLS: true})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if mech != "XOAUTH2" {
		t.Errorf("mech = %q, want XOAUTH2", mech)
	}
	// Must be the RAW string — net/smtp base64-encodes it; pre-encoding here
	// would double-encode.
	want := "user=me@outlook.com\x01auth=Bearer tok-123\x01\x01"
	if string(resp) != want {
		t.Errorf("initial response = %q, want %q", resp, want)
	}
}

func TestXOAuth2AuthNext(t *testing.T) {
	a := &xoauth2Auth{}
	if _, err := a.Next([]byte(`{"status":"401"}`), true); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("Next(more=true) should surface the server challenge, got %v", err)
	}
	if resp, err := a.Next(nil, false); err != nil || resp != nil {
		t.Errorf("Next(more=false) = %v, %v; want nil, nil", resp, err)
	}
}

func TestSMTPAuthSelectsMechanism(t *testing.T) {
	// Password account → PLAIN.
	plain := config.AccountConfig{Provider: "Outlook", User: "me@x.com", Password: "pw"}
	if a, err := smtpAuth(context.Background(), plain, "smtp.office365.com"); err != nil || a == nil {
		t.Fatalf("smtpAuth(password) = %v, %v", a, err)
	}
	// Outlook + refresh token → XOAUTH2 path; a pre-cancelled ctx makes the
	// token refresh fail immediately (no network), proving the branch ran.
	oauth := config.AccountConfig{
		Provider:     "Outlook",
		Name:         "smtp-branch-test",
		User:         "me@outlook.com",
		RefreshToken: "refresh-x",
		ClientID:     "cid",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := smtpAuth(ctx, oauth, "smtp.office365.com"); err == nil ||
		!strings.Contains(err.Error(), "oauth2") {
		t.Errorf("smtpAuth(oauth) expected oauth2 error, got %v", err)
	}
}
