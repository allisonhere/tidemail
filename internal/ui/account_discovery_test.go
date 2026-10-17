package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/autodiscover"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// customFormOnEmail is an add-account form on Custom with the cursor in the
// Username row holding email.
func customFormOnEmail(email string) AccountManager {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.resetForm()
	am.userInput.SetValue(email)
	am.focusField(amFieldUser)
	return am
}

// stubDiscovery replaces DNS for the test and records each lookup.
func stubDiscovery(t *testing.T, res autodiscover.Result, err error) *[]string {
	t.Helper()
	var calls []string
	old := discoverServers
	discoverServers = func(_ context.Context, email string) (autodiscover.Result, error) {
		calls = append(calls, email)
		return res, err
	}
	t.Cleanup(func() { discoverServers = old })
	return &calls
}

// runDiscovery runs whatever lookup cmd produced and applies its result.
func runDiscovery(t *testing.T, am AccountManager, cmd tea.Cmd) AccountManager {
	t.Helper()
	var msgs []tea.Msg
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch m := c().(type) {
		case tea.BatchMsg:
			for _, child := range m {
				walk(child)
			}
		default:
			msgs = append(msgs, m)
		}
	}
	walk(cmd)
	for _, m := range msgs {
		if d, ok := m.(ServersDiscoveredMsg); ok {
			return am.applyDiscovery(d)
		}
	}
	t.Fatal("no server lookup was started")
	return am
}

var srvResult = autodiscover.Result{Servers: autodiscover.Servers{IMAPHost: "mail.example.org", IMAPPort: 993, IMAPTLS: true, SMTPHost: "smtp.example.org", SMTPPort: 587, SMTPTLS: true}, Source: "DNS SRV"}

func TestLeavingEmailRowFillsServersFromDNS(t *testing.T) {
	calls := stubDiscovery(t, srvResult, nil)
	am := customFormOnEmail("alice@example.org")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	am = runDiscovery(t, am, cmd)

	if len(*calls) != 1 || (*calls)[0] != "alice@example.org" {
		t.Fatalf("lookups: %v", *calls)
	}
	cfg := am.buildCfg()
	if cfg.IMAPHost != "mail.example.org" || cfg.IMAPPort != 993 || !cfg.IMAPTLS || cfg.SMTPHost != "smtp.example.org" || cfg.SMTPPort != 587 || !cfg.SMTPTLS {
		t.Fatalf("form not filled from DNS: %+v", cfg)
	}
	if !strings.HasPrefix(am.statusMsg, "FOUND: mail.example.org:993 / smtp.example.org:587") || am.busy {
		t.Fatalf("status %q busy=%v", am.statusMsg, am.busy)
	}

	// Leaving the row again with the same address doesn't repeat the lookup.
	am.focusField(amFieldUser)
	if _, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys); cmd != nil {
		if _, ok := cmd().(ServersDiscoveredMsg); ok {
			t.Fatal("lookup repeated for the same address")
		}
	}
}

func TestChangingDiscoveredEmailAllowsAnotherLookup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result autodiscover.Result
	}{
		{"SRV servers", srvResult},
		{"MX preset", autodiscover.Result{Servers: autodiscover.Gmail, Preset: "Gmail", Source: "MX records"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubDiscovery(t, tc.result, nil)
			am := customFormOnEmail("alice@example.org")
			am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
			am = runDiscovery(t, am, cmd)

			am.focusField(amFieldUser)
			for range []rune("alice@example.org") {
				am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyBackspace}, DefaultKeys)
			}
			if am.userInput.Value() != "" || am.provider != "Custom" || !am.hostRowsEmpty() {
				t.Fatalf("clearing email kept discovery: email=%q provider=%q hosts=%q/%q", am.userInput.Value(), am.provider, am.imapHostInput.Value(), am.smtpHostInput.Value())
			}
			am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bob@other.example")}, DefaultKeys)
			am, cmd, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
			am = runDiscovery(t, am, cmd)
			if len(*calls) != 2 || (*calls)[1] != "bob@other.example" {
				t.Fatalf("second email was not looked up: %v", *calls)
			}
			if am.userInput.Value() != "bob@other.example" {
				t.Fatalf("second email changed: %q", am.userInput.Value())
			}
		})
	}
}

func TestChangingEmailKeepsEditedServerAndClearsInferredFrom(t *testing.T) {
	res := srvResult
	res.Username = "alice"
	stubDiscovery(t, res, nil)
	am := customFormOnEmail("alice@example.org")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	am = runDiscovery(t, am, cmd)
	if am.userInput.Value() != "alice" || am.fromInput.Value() != "alice@example.org" {
		t.Fatalf("autoconfig did not fill login and From: %q / %q", am.userInput.Value(), am.fromInput.Value())
	}
	am.imapHostInput.SetValue("my-mail.example.org")
	am.focusField(amFieldUser)
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyBackspace}, DefaultKeys)
	if am.imapHostInput.Value() != "my-mail.example.org" || am.smtpHostInput.Value() != "" || am.fromInput.Value() != "" {
		t.Fatalf("email change kept inferred values or erased edited host: %q / %q / %q", am.imapHostInput.Value(), am.smtpHostInput.Value(), am.fromInput.Value())
	}
}

func TestAutoLookupKeepsTypedServers(t *testing.T) {
	calls := stubDiscovery(t, srvResult, nil)
	am := customFormOnEmail("alice@example.org")
	am.imapHostInput.SetValue("typed.example.org")
	am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if len(*calls) != 0 {
		t.Fatalf("auto lookup ran over typed hosts: %v", *calls)
	}

	// Typing a host while an auto lookup is in flight also wins.
	am = customFormOnEmail("alice@example.org")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	am.smtpHostInput.SetValue("typed-smtp.example.org")
	am = runDiscovery(t, am, cmd)
	if got := am.buildCfg(); got.SMTPHost != "typed-smtp.example.org" || got.IMAPHost != "" {
		t.Fatalf("late auto result overwrote typed host: %+v", got)
	}
}

func TestCtrlLReplacesServersAndHandlesNonCustom(t *testing.T) {
	stubDiscovery(t, srvResult, nil)
	am := customFormOnEmail("alice@example.org")
	am.imapHostInput.SetValue("old.example.org")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyCtrlL}, DefaultKeys)
	am = runDiscovery(t, am, cmd)
	if got := am.buildCfg().IMAPHost; got != "mail.example.org" {
		t.Fatalf("Ctrl+L did not replace the host: %q", got)
	}

	am.setProvider("Gmail")
	am, cmd, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlL}, DefaultKeys)
	if cmd != nil || !strings.Contains(am.statusMsg, "CUSTOM") {
		t.Fatalf("Ctrl+L on a preset: cmd=%v status=%q", cmd != nil, am.statusMsg)
	}

	am = customFormOnEmail("not-an-address")
	am, cmd, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlL}, DefaultKeys)
	if cmd != nil || !strings.Contains(am.statusMsg, "EMAIL ADDRESS") {
		t.Fatalf("Ctrl+L without an address: cmd=%v status=%q", cmd != nil, am.statusMsg)
	}
}

func TestMXResultSwitchesToPreset(t *testing.T) {
	stubDiscovery(t, autodiscover.Result{Servers: autodiscover.Gmail, Preset: "Gmail", Source: "MX records"}, nil)
	am := customFormOnEmail("bob@workspace.example")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	am = runDiscovery(t, am, cmd)
	if am.provider != "Gmail" || am.buildCfg().IMAPHost != "imap.gmail.com" {
		t.Fatalf("provider=%q cfg=%+v", am.provider, am.buildCfg())
	}
	if !strings.Contains(am.statusMsg, "GOOGLE") {
		t.Fatalf("status %q", am.statusMsg)
	}
}

func TestDiscoveryErrorsLeaveFormAlone(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{autodiscover.ErrNotFound, "NO SERVER SETTINGS PUBLISHED FOR example.org"},
		{errors.New("dns timeout"), "SERVER LOOKUP FAILED: dns timeout"},
	} {
		stubDiscovery(t, autodiscover.Result{}, c.err)
		am := customFormOnEmail("alice@example.org")
		am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
		am = runDiscovery(t, am, cmd)
		if !am.hostRowsEmpty() || am.provider != "Custom" || !strings.Contains(am.statusMsg, c.want) {
			t.Fatalf("%v: hosts empty=%v provider=%q status=%q", c.err, am.hostRowsEmpty(), am.provider, am.statusMsg)
		}
	}
}

func TestStaleDiscoveryResultIsIgnored(t *testing.T) {
	stubDiscovery(t, srvResult, nil)
	am := customFormOnEmail("alice@example.org")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyCtrlL}, DefaultKeys)
	am.userInput.SetValue("alice@other.example") // kept typing
	am = runDiscovery(t, am, cmd)
	if !am.hostRowsEmpty() {
		t.Fatal("result for an old address filled the form")
	}

	am = customFormOnEmail("alice@example.org")
	am, cmd, _ = am.Update(tea.KeyMsg{Type: tea.KeyCtrlL}, DefaultKeys)
	am.mode = amList // form closed
	am = runDiscovery(t, am, cmd)
	if !am.hostRowsEmpty() {
		t.Fatal("result applied to a closed form")
	}
}

func TestAutoconfigLoginMovesAddressToFrom(t *testing.T) {
	res := srvResult
	res.Source, res.Username = "autoconfig", "alice"
	stubDiscovery(t, res, nil)
	am := customFormOnEmail("alice@example.org")
	am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	am = runDiscovery(t, am, cmd)
	cfg := am.buildCfg()
	if cfg.User != "alice" || cfg.From != "alice@example.org" || cfg.IMAPHost != "mail.example.org" {
		t.Fatalf("login/from not taken from autoconfig: %+v", cfg)
	}
	if !strings.HasPrefix(am.statusMsg, "FOUND: ") || !strings.Contains(am.statusMsg, "LOGIN alice") {
		t.Fatalf("status %q", am.statusMsg)
	}
	am.nameInput.SetValue("Work")
	if f := am.validateForm(am.buildCfg()); !f.ok() {
		t.Fatalf("filled form does not validate: %s", f.msg)
	}

	// A From the user already typed is kept.
	am = customFormOnEmail("alice@example.org")
	am.fromInput.SetValue("Alice <alice@example.org>")
	am, cmd, _ = am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	am = runDiscovery(t, am, cmd)
	if got := am.buildCfg().From; got != "Alice <alice@example.org>" {
		t.Fatalf("typed From replaced: %q", got)
	}
}

func TestDiscoveryStatusIsNeverAFailureBadge(t *testing.T) {
	chrome := newManagerChrome(80, CatppuccinMocha, false)
	mxResult := autodiscover.Result{Servers: autodiscover.Gmail, Preset: "Gmail", Source: "MX records"}
	for _, c := range []struct {
		name string
		res  autodiscover.Result
		err  error
		want func(managerChrome) lipgloss.Color
	}{
		{"srv found", srvResult, nil, func(c managerChrome) lipgloss.Color { return c.successFg }},
		{"mx found", mxResult, nil, func(c managerChrome) lipgloss.Color { return c.successFg }},
		{"nothing published", autodiscover.Result{}, autodiscover.ErrNotFound, func(c managerChrome) lipgloss.Color { return c.muted }},
		{"lookup failed", autodiscover.Result{}, errors.New("dns timeout"), func(c managerChrome) lipgloss.Color { return c.errorFg }},
	} {
		t.Run(c.name, func(t *testing.T) {
			stubDiscovery(t, c.res, c.err)
			am := customFormOnEmail("alice@example.org")
			am, cmd, _ := am.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
			if got := am.statusForeground(chrome); got != chrome.muted || am.statusIsFailure(chrome) {
				t.Fatalf("in-progress %q: colour %v, badge %v", am.statusMsg, got, am.statusIsFailure(chrome))
			}
			am = runDiscovery(t, am, cmd)
			if got := am.statusForeground(chrome); got != c.want(chrome) || am.statusIsFailure(chrome) {
				t.Fatalf("%q: colour %v (want %v), badge %v", am.statusMsg, got, c.want(chrome), am.statusIsFailure(chrome))
			}
		})
	}
}
