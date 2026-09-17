package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func outgoingFixture() smtp.OutgoingMessage {
	return smtp.OutgoingMessage{To: []string{"you@example.com"}, Subject: "hi", Body: "there"}
}

// Appending our own copy to a server that already files submitted mail shows
// the user two of everything, so the skip has to be right.
func TestServerFilesSentMail(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config.AccountConfig
		want bool
	}{
		{"gmail smtp host", config.AccountConfig{SMTPHost: "smtp.gmail.com"}, true},
		{"gmail relay", config.AccountConfig{SMTPHost: "smtp-relay.gmail.com"}, true},
		{"gmail host cased", config.AccountConfig{SMTPHost: "SMTP.Gmail.Com"}, true},
		// A Workspace account on a custom address still submits through Gmail.
		{"provider says gmail", config.AccountConfig{Provider: "Gmail", SMTPHost: "mail.example.com"}, true},
		{"custom domain", config.AccountConfig{SMTPHost: "mail.example.com", Provider: "Custom"}, false},
		{"empty", config.AccountConfig{}, false},
		// Must not match a lookalike domain.
		{"lookalike domain", config.AccountConfig{SMTPHost: "notsmtp.gmail.com.evil.test"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverFilesSentMail(tc.cfg); got != tc.want {
				t.Fatalf("serverFilesSentMail(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}

// The mail is already delivered and recorded, so a failure to file a copy must
// never look like a failed send.
func TestAppendToSentFolderIsBestEffort(t *testing.T) {
	// No session pool: the copy cannot be made, and that must be reported
	// rather than panicking or being mistaken for a delivery error.
	if err := appendToSentFolder(nil, nil, config.AccountConfig{IMAPHost: "mail.example.com"}, outgoingFixture()); err != nil {
		t.Fatalf("a missing database or pool should be a no-op, got %v", err)
	}
	// Gmail files its own copy, so appending is skipped entirely.
	if err := appendToSentFolder(nil, nil, config.AccountConfig{SMTPHost: "smtp.gmail.com"}, outgoingFixture()); err != nil {
		t.Fatalf("gmail should be skipped, got %v", err)
	}
}
