package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

// Regression coverage for issue #151: an Outlook/M365 shared mailbox accepts
// the shared address for IMAP delegate access, but SMTP must authenticate as
// the account the credentials actually belong to. The SMTP User row lets an
// account keep its IMAP login while overriding only the SMTP one.

func TestBuildCfgIncludesSMTPUserOverride(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.userInput.SetValue("shared@example.com")
	am.smtpUserInput.SetValue("  me@example.com  ") // trimmed like every other field

	cfg := am.buildCfg()
	if cfg.User != "shared@example.com" {
		t.Fatalf("User = %q, want unchanged shared@example.com", cfg.User)
	}
	if cfg.SMTPUser != "me@example.com" {
		t.Fatalf("SMTPUser = %q, want trimmed me@example.com", cfg.SMTPUser)
	}
}

func TestBuildCfgLeavesSMTPUserEmptyWhenUnset(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.userInput.SetValue("alice@example.com")

	cfg := am.buildCfg()
	if cfg.SMTPUser != "" {
		t.Fatalf("SMTPUser = %q, want empty so SMTPLoginUser() falls back to User", cfg.SMTPUser)
	}
	if cfg.SMTPLoginUser() != "alice@example.com" {
		t.Fatalf("SMTPLoginUser() = %q, want fallback to User", cfg.SMTPLoginUser())
	}
}

func TestPopulateFormFromRestoresSMTPUser(t *testing.T) {
	am := NewAccountManager(nil)
	am.populateFormFrom(config.AccountConfig{
		Name:     "Shared",
		Provider: "Outlook",
		User:     "shared@example.com",
		SMTPUser: "me@example.com",
	})
	if am.smtpUserInput.Value() != "me@example.com" {
		t.Fatalf("smtpUserInput = %q after populateFormFrom, want me@example.com", am.smtpUserInput.Value())
	}
}

func TestAccountFormRendersSMTPUserRow(t *testing.T) {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.focusField(amFieldSMTPUser)

	view := ansi.Strip(am.View(90, 40, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "SMTP User") {
		t.Fatalf("expected the form to render an SMTP User row:\n%s", view)
	}
}
