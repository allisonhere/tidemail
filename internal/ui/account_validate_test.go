package ui

import (
	"strings"
	"testing"
)

// The account form is the last place a bad value can be named for what it is.
// Past that point it becomes a dial error, a login rejection, or — worse — a
// silent default, so every field is checked for shape before anything
// connects.

// A port that is not a number used to fall through Atoi's zero and become the
// standard port, so a typo connected somewhere the user never chose.
func TestParsePort(t *testing.T) {
	for _, tc := range []struct {
		raw    string
		want   int
		wantOK bool
	}{
		{"", defaultIMAPPort, true}, // empty takes the default
		{"  ", defaultIMAPPort, true},
		{"993", 993, true},
		{" 143 ", 143, true},
		{"0", 0, true}, // parses; the range check rejects it
		{"70000", 70000, true},
		{"abc", 0, false},
		{"99 3", 0, false},
		{"993/tls", 0, false},
	} {
		got, ok := parsePort(tc.raw, defaultIMAPPort)
		if ok != tc.wantOK {
			t.Fatalf("parsePort(%q) ok = %v, want %v", tc.raw, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Fatalf("parsePort(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestValidateFormChecksPortFields(t *testing.T) {
	for _, tc := range []struct {
		imap, smtp string
		wantErr    string
	}{
		{"", "", ""},       // both default
		{"993", "587", ""}, //
		{"143", "25", ""},  // plain ports are fine
		{"abc", "587", "IMAP PORT MUST BE A NUMBER"},
		{"993", "smtp", "SMTP PORT MUST BE A NUMBER"},
		{"0", "587", "IMAP PORT MUST BE 1-65535"},
		{"993", "70000", "SMTP PORT MUST BE 1-65535"},
	} {
		am := newFromFormAccountManager()
		am.imapPortInput.SetValue(tc.imap)
		am.smtpPortInput.SetValue(tc.smtp)
		if got := am.validateForm(am.buildCfg()); got != tc.wantErr {
			t.Fatalf("ports %q/%q: got %q, want %q", tc.imap, tc.smtp, got, tc.wantErr)
		}
	}
}

// A port the config never set shows as an empty field offering the default,
// not as a literal "0" that validation would then refuse to save.
func TestPortFieldValueHidesUnsetPort(t *testing.T) {
	if got := portFieldValue(0); got != "" {
		t.Fatalf("portFieldValue(0) = %q, want empty", got)
	}
	if got := portFieldValue(993); got != "993" {
		t.Fatalf("portFieldValue(993) = %q, want \"993\"", got)
	}
}

func TestHostFormatError(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"imap.example.com", ""},
		{"localhost", ""},
		{"192.0.2.10", ""},
		{"[2001:db8::1]", ""}, // a bracketed IPv6 literal is dialable
		{"imaps://imap.example.com", "URL"},
		{"https://mail.example.com/webmail", "URL"},
		{"imap.example.com:993", "PORT"},
		{"imap example com", "SPACES"},
		{"imap.example.com/mail", "PATH"},
		{"alice@example.com", "EMAIL ADDRESS"},
	} {
		got := hostFormatError("IMAP HOST", tc.host)
		if tc.want == "" {
			if got != "" {
				t.Fatalf("host %q: unexpected error %q", tc.host, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("host %q: got %q, want it to mention %q", tc.host, got, tc.want)
		}
	}
}

// An account with no outgoing server cannot send; the form says so rather than
// leaving the first send to dial a bare port.
func TestValidateFormRequiresSMTPHost(t *testing.T) {
	am := newFromFormAccountManager()
	am.smtpHostInput.SetValue("")
	if got := am.validateForm(am.buildCfg()); got != "SMTP HOST IS REQUIRED" {
		t.Fatalf("expected a missing SMTP host to fail validation, got %q", got)
	}
}

// The preset providers label the field "Email" and authenticate with the full
// address, so a bare login there is a format error, not a login failure.
func TestValidateFormRequiresFullAddressForPresetProviders(t *testing.T) {
	am := newFromFormAccountManager()
	am.provider = "Gmail"
	am.userInput.SetValue("alice")
	if got := am.validateForm(am.buildCfg()); !strings.Contains(got, "FULL ADDRESS") {
		t.Fatalf("expected a bare Gmail login to fail validation, got %q", got)
	}

	am.userInput.SetValue("alice@gmail.com")
	if got := am.validateForm(am.buildCfg()); strings.Contains(got, "FULL ADDRESS") {
		t.Fatalf("expected a full Gmail address to pass the address check, got %q", got)
	}
}

// Custom hosts issue logins that are not addresses at all, including the
// "user#domain.com" convention, so that field stays free-form.
func TestValidateFormAcceptsNonAddressCustomLogins(t *testing.T) {
	for _, user := range []string{
		"alice",
		"alice#example.com",
		"DOMAIN\\alice",
		"alice@example.com",
	} {
		am := newFromFormAccountManager()
		am.userInput.SetValue(user)
		if got := am.validateForm(am.buildCfg()); got != "" {
			t.Fatalf("expected Custom login %q to validate, got %q", user, got)
		}
	}
}

// "#" is an ordinary local-part character. It cannot stand in for "@" — SMTP
// needs local@domain — but an address that contains one is valid and must not
// be caught by the From check.
func TestValidateFormAcceptsHashInsideAddresses(t *testing.T) {
	for _, from := range []string{
		"alice#work@example.com",
		"#alice@example.com",
		"Alice <alice#work@example.com>",
		"alice_example.com#EXT#@tenant.onmicrosoft.com", // Entra guest UPN
	} {
		am := newFromFormAccountManager()
		am.fromInput.SetValue(from)
		if got := am.validateForm(am.buildCfg()); got != "" {
			t.Fatalf("expected From %q to validate, got %q", from, got)
		}
	}
}
