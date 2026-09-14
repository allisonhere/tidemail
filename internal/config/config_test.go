package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestNormalizeSendMaxAttempts(t *testing.T) {
	for input, want := range map[int]int{-1: 1, 0: 1, 1: 1, 3: 3, 20: 10} {
		if got := NormalizeSendMaxAttempts(input); got != want {
			t.Errorf("NormalizeSendMaxAttempts(%d) = %d, want %d", input, got, want)
		}
	}
}

func TestDefaultConfigMSClientIDFallsBackToThunderbird(t *testing.T) {
	t.Setenv("TIDEMAIL_MS_CLIENT_ID", "")
	if got := DefaultConfig().OAuth.MSClientID; got != ThunderbirdMSClientID {
		t.Fatalf("MSClientID = %q, want the Thunderbird default", got)
	}
	t.Setenv("TIDEMAIL_MS_CLIENT_ID", "my-azure-app")
	if got := DefaultConfig().OAuth.MSClientID; got != "my-azure-app" {
		t.Fatalf("MSClientID = %q, want the env override", got)
	}
}

func TestUsesOAuth2ProviderGates(t *testing.T) {
	gmail := AccountConfig{Provider: "Gmail", AuthMethod: AuthOAuth2, RefreshToken: "x"}
	outlook := AccountConfig{Provider: "Outlook", AuthMethod: AuthOAuth2, RefreshToken: "x"}
	other := AccountConfig{Provider: "Custom", AuthMethod: AuthOAuth2, RefreshToken: "x"}
	if !gmail.UsesGoogleOAuth2() || gmail.UsesMicrosoftOAuth2() {
		t.Fatal("Gmail account misclassified")
	}
	if !outlook.UsesMicrosoftOAuth2() || outlook.UsesGoogleOAuth2() {
		t.Fatal("Outlook account misclassified")
	}
	if other.UsesGoogleOAuth2() || other.UsesMicrosoftOAuth2() {
		t.Fatal("oauth2 marker on a non-OAuth provider must not trigger XOAUTH2")
	}

	// The regression: a leftover refresh token with NO explicit marker must not
	// route the account to XOAUTH2.
	stale := AccountConfig{Provider: "Gmail", RefreshToken: "leftover"}
	if stale.UsesGoogleOAuth2() || stale.UsesOAuth2() {
		t.Fatal("a stale refresh token without auth_method=oauth2 must stay on app-password auth")
	}
	pw := AccountConfig{Provider: "Gmail", AuthMethod: AuthPassword, RefreshToken: "leftover"}
	if pw.UsesGoogleOAuth2() {
		t.Fatal("auth_method=password must stay on app-password auth even with a token present")
	}
}

func TestMigrateAuthMethod(t *testing.T) {
	none := func(string) string { return "" }
	pw := func(string) string { return "pw" }
	tok := func(string) string { return "1//refresh" }

	cases := []struct {
		name        string
		acct        AccountConfig
		getPassword func(string) string
		getToken    func(string) string
		want        string
	}{
		{"legacy gmail with app password", AccountConfig{Provider: "Gmail", Password: "pw"}, none, none, AuthPassword},
		{"legacy gmail, password only in keychain", AccountConfig{Provider: "Gmail"}, pw, tok, AuthPassword},
		{"legacy gmail, token only in keychain, no password anywhere", AccountConfig{Provider: "Gmail"}, none, tok, AuthOAuth2},
		{"legacy gmail, token in TOML, no keychain", AccountConfig{Provider: "Gmail", RefreshToken: "1//from-toml"}, none, none, AuthOAuth2},
		{"legacy outlook, token only", AccountConfig{Provider: "Outlook"}, none, tok, AuthOAuth2},
		{"legacy custom provider", AccountConfig{Provider: "Custom"}, none, tok, AuthPassword},
		{"password stamp with a real password stays put (v1.0.6 hazard)", AccountConfig{Provider: "Gmail", AuthMethod: AuthPassword, Password: "pw"}, none, tok, AuthPassword},
		{"password stamp with NO password + a token is healed to oauth2", AccountConfig{Provider: "Gmail", AuthMethod: AuthPassword}, none, tok, AuthOAuth2},
		{"oauth2 stamp is left alone even with no token yet", AccountConfig{Provider: "Gmail", AuthMethod: AuthOAuth2}, none, none, AuthOAuth2},
	}
	for _, tc := range cases {
		cfg := &Config{Accounts: []AccountConfig{tc.acct}}
		migrateAuthMethod(cfg, tc.getPassword, tc.getToken)
		if got := cfg.Accounts[0].AuthMethod; got != tc.want {
			t.Errorf("%s: AuthMethod = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDefaultConfigDisplayDensityCompact(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Display.Density != "compact" {
		t.Fatalf("expected default display density compact, got %q", cfg.Display.Density)
	}
	if cfg.Display.MarkReadOnFocus {
		t.Fatal("expected mark-read-on-focus to default off")
	}
	if !cfg.Display.FocusLine {
		t.Fatal("expected focus line to default on")
	}
}

func TestDefaultConfigShowsPaneHeaders(t *testing.T) {
	if !DefaultConfig().Display.ShowPaneHeaders {
		t.Fatal("pane header bars should default to visible")
	}
}

func TestNormalizeDisplayDensity(t *testing.T) {
	if got := NormalizeDisplayDensity(""); got != "compact" {
		t.Fatalf("empty: got %q", got)
	}
	if got := NormalizeDisplayDensity("COMPACT"); got != "compact" {
		t.Fatalf("compact: got %q", got)
	}
	if got := NormalizeDisplayDensity("comfortable"); got != "comfortable" {
		t.Fatalf("comfortable: got %q", got)
	}
	if got := NormalizeDisplayDensity("unknown"); got != "compact" {
		t.Fatalf("unknown: got %q", got)
	}
}

func TestDefaultConfigIncludesUpdateDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Updates.CheckOnStartup {
		t.Fatal("expected update checks to be enabled by default")
	}
	if cfg.Updates.CheckIntervalHours != 24 {
		t.Fatalf("expected 24 hour update interval, got %d", cfg.Updates.CheckIntervalHours)
	}
	if cfg.Display.UnreadFirst {
		t.Fatal("expected unread-first ordering to be disabled by default")
	}
	if len(cfg.Accounts) != 0 {
		t.Fatalf("expected default accounts to be empty, got %#v", cfg.Accounts)
	}
}

func TestSaveWritesPrivateConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{{Name: "Personal", User: "alice@example.com", Password: "secret"}}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	cfgPath := filepath.Join(dir, "tidemail", "config.toml")
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected config file mode 0600, got %04o", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(cfgPath))
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("expected config dir mode 0700, got %04o", got)
	}
}

func TestSecurityWarningsDetectsReadableConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgPath := filepath.Join(dir, "tidemail", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte("theme = \"dracula\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	warnings, err := SecurityWarnings()
	if err != nil {
		t.Fatalf("SecurityWarnings returned error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %d: %#v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "chmod 600") {
		t.Fatalf("expected chmod guidance in warning, got %q", warnings[0])
	}
}

func TestRedactSecretsRemovesConfiguredSecrets(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AI.OpenAIKey = "openai-token"
	cfg.Accounts = []AccountConfig{{Name: "Personal", Password: "mail-secret"}}

	got := RedactSecrets("openai=openai-token password=mail-secret harmless=visible", cfg)
	if strings.Contains(got, "openai-token") || strings.Contains(got, "mail-secret") {
		t.Fatalf("expected secrets to be redacted, got %q", got)
	}
	if !strings.Contains(got, "harmless=visible") {
		t.Fatalf("expected harmless text to remain, got %q", got)
	}
}

func TestRedactSecretsRemovesOAuthTokens(t *testing.T) {
	cfg := DefaultConfig()
	cfg.OAuth.GoogleClientSecret = "app-client-secret"
	cfg.Accounts = []AccountConfig{{
		Name:         "Gmail",
		RefreshToken: "1//0-refresh-token",
		ClientSecret: "app-client-secret",
	}}

	got := RedactSecrets("refresh=1//0-refresh-token secret=app-client-secret ok=visible", cfg)
	if strings.Contains(got, "1//0-refresh-token") || strings.Contains(got, "app-client-secret") {
		t.Fatalf("expected OAuth secrets to be redacted, got %q", got)
	}
	if !strings.Contains(got, "ok=visible") {
		t.Fatalf("expected harmless text to remain, got %q", got)
	}
}

func TestLoadPreservesUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgPath := filepath.Join(dir, "tidemail", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	data := `
theme = "catppuccin-mocha"

[display]
icons = true
date_format = "relative"
mark_read_on_open = true
mark_read_on_focus = true
focus_line = false
unread_first = true
browser = ""
density = "compact"

[updates]
check_on_startup = false
check_interval_hours = 12
last_checked_unix = 1710000000
dismissed_version = "v1.2.3"
available_version = "v1.3.0"
available_summary = "New version available."
available_published_unix = 1710001234

[[account]]
name = "Personal"
imap_host = "imap.example.com"
imap_port = 993
imap_tls = true
smtp_host = "smtp.example.com"
smtp_port = 587
smtp_tls = true
user = "alice"
password = "secret"
from = "alice@example.com"
`
	if err := os.WriteFile(cfgPath, []byte(data), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Updates.CheckOnStartup {
		t.Fatal("expected check_on_startup to be false")
	}
	if cfg.Updates.CheckIntervalHours != 12 {
		t.Fatalf("expected interval 12, got %d", cfg.Updates.CheckIntervalHours)
	}
	if cfg.Updates.LastCheckedUnix != 1710000000 {
		t.Fatalf("unexpected last_checked_unix: %d", cfg.Updates.LastCheckedUnix)
	}
	if cfg.Updates.DismissedVersion != "v1.2.3" {
		t.Fatalf("unexpected dismissed_version: %q", cfg.Updates.DismissedVersion)
	}
	if cfg.Updates.AvailableVersion != "v1.3.0" {
		t.Fatalf("unexpected available_version: %q", cfg.Updates.AvailableVersion)
	}
	if cfg.Updates.AvailableSummary != "New version available." {
		t.Fatalf("unexpected available_summary: %q", cfg.Updates.AvailableSummary)
	}
	if cfg.Updates.AvailablePublished != 1710001234 {
		t.Fatalf("unexpected available_published_unix: %d", cfg.Updates.AvailablePublished)
	}
	if len(cfg.Accounts) != 1 {
		t.Fatalf("expected one account, got %d", len(cfg.Accounts))
	}
	if cfg.Accounts[0].Name != "Personal" {
		t.Fatalf("unexpected account name: %q", cfg.Accounts[0].Name)
	}
	if cfg.Accounts[0].IMAPHost != "imap.example.com" {
		t.Fatalf("unexpected imap host: %q", cfg.Accounts[0].IMAPHost)
	}
	if cfg.Accounts[0].User != "alice" {
		t.Fatalf("unexpected account user: %q", cfg.Accounts[0].User)
	}
	if cfg.Accounts[0].Password != "secret" {
		t.Fatalf("unexpected account password: %q", cfg.Accounts[0].Password)
	}
	if cfg.Display.Density != "compact" {
		t.Fatalf("expected display density compact, got %q", cfg.Display.Density)
	}
	if !cfg.Display.MarkReadOnFocus {
		t.Fatal("expected mark_read_on_focus to load true")
	}
	if cfg.Display.FocusLine {
		t.Fatal("expected focus_line to load false")
	}
	if !cfg.Display.UnreadFirst {
		t.Fatal("expected unread_first to load true")
	}
}

// TestSaveLeavesNoTempFiles verifies the atomic write cleans up after itself, so
// repeated saves do not litter the config directory.
func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{{Name: "Personal", User: "alice@example.com"}}
	for i := 0; i < 3; i++ {
		if err := Save(cfg); err != nil {
			t.Fatalf("Save returned error: %v", err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, "tidemail"))
	if err != nil {
		t.Fatalf("read config dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "config.toml" {
		t.Fatalf("expected only config.toml in the config dir, got %v", names)
	}
}

// TestSaveIsAtomicUnderConcurrentReads is the regression test for the in-place
// O_TRUNC write. A reader racing a save must never observe a truncated or
// partially written config: with the old code the file spent a window empty
// between the truncate and the encode, so a concurrent read came back with no
// accounts. The rename makes the replacement indivisible, so every read sees
// either the old file or the new one.
func TestSaveIsAtomicUnderConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{
		{Name: "Personal", User: "alice@example.com"},
		{Name: "Work", User: "alice@work.example.com"},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	cfgPath := filepath.Join(dir, "tidemail", "config.toml")

	done := make(chan struct{})
	bad := make(chan string, 1)
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			if err := Save(cfg); err != nil {
				select {
				case bad <- fmt.Sprintf("Save: %v", err):
				default:
				}
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			select {
			case msg := <-bad:
				t.Fatal(msg)
			default:
			}
			return
		default:
		}
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("config vanished mid-save: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("observed an empty config file mid-save")
		}
		var got Config
		if _, err := toml.Decode(string(data), &got); err != nil {
			t.Fatalf("observed a partially written config mid-save: %v", err)
		}
		if len(got.Accounts) != 2 {
			t.Fatalf("observed %d accounts mid-save, want 2", len(got.Accounts))
		}
	}
}

// TestRedactSecretsHandlesPrefixSecrets is the regression test for
// order-dependent redaction. Replacing in insertion order meant a secret that is
// a prefix of another leaked the difference: "pass" redacted first rewrote
// "pass123" to "[redacted]123", so the longer secret no longer matched and its
// tail survived in cleartext into status and error output.
func TestRedactSecretsHandlesPrefixSecrets(t *testing.T) {
	cfg := Config{}
	cfg.AI.OpenAIKey = "pass"
	cfg.AI.ClaudeKey = "pass123"

	got := RedactSecrets("openai=pass claude=pass123", cfg)

	if strings.Contains(got, "123") {
		t.Fatalf("leaked the longer secret's tail: %q", got)
	}
	if want := "openai=[redacted] claude=[redacted]"; got != want {
		t.Fatalf("RedactSecrets = %q, want %q", got, want)
	}
}

// TestRedactSecretsIndependentOfConfigOrder verifies the result does not depend
// on which field a secret happens to live in, since that determined replacement
// order before.
func TestRedactSecretsIndependentOfConfigOrder(t *testing.T) {
	const in = "a=short b=shortlonger"

	forward := Config{}
	forward.AI.OpenAIKey = "short"
	forward.AI.ClaudeKey = "shortlonger"

	reverse := Config{}
	reverse.AI.OpenAIKey = "shortlonger"
	reverse.AI.ClaudeKey = "short"

	got, want := RedactSecrets(in, forward), RedactSecrets(in, reverse)
	if got != want {
		t.Fatalf("redaction depends on config field order: %q vs %q", got, want)
	}
	if strings.Contains(got, "longer") {
		t.Fatalf("leaked a secret fragment: %q", got)
	}
}
