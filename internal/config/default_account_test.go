package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func threeAccounts() []AccountConfig {
	return []AccountConfig{
		{ID: "aaa", Name: "Personal", User: "mira@example.com"},
		{ID: "bbb", Name: "Work", User: "mira@work.example"},
		{ID: "ccc", Name: "Old", User: "mira@isp.example"},
	}
}

func TestDefaultAccountFallsBackToFirstAccount(t *testing.T) {
	cfg := Config{Accounts: threeAccounts()}
	got, ok := cfg.DefaultAccount()
	if !ok || got.ID != "aaa" {
		t.Fatalf("DefaultAccount() = %q, %v; want the first account", got.ID, ok)
	}
	if id := cfg.ExplicitDefaultAccountID(); id != "" {
		t.Fatalf("ExplicitDefaultAccountID() = %q, want empty for an unset default", id)
	}

	empty := Config{}
	if _, ok := empty.DefaultAccount(); ok {
		t.Fatal("DefaultAccount() reported an account with none configured")
	}
}

func TestDefaultAccountResolvesByIDAcrossReorder(t *testing.T) {
	cfg := Config{Accounts: threeAccounts(), DefaultAccountID: "bbb"}
	got, ok := cfg.DefaultAccount()
	if !ok || got.ID != "bbb" {
		t.Fatalf("DefaultAccount() = %q, want bbb", got.ID)
	}

	cfg.ReorderAccounts([]string{"ccc", "bbb", "aaa"})
	got, ok = cfg.DefaultAccount()
	if !ok || got.ID != "bbb" {
		t.Fatalf("after reorder DefaultAccount() = %q, want bbb — the default must not follow position", got.ID)
	}
}

func TestDefaultAccountIgnoresDanglingID(t *testing.T) {
	cfg := Config{Accounts: threeAccounts(), DefaultAccountID: "gone"}
	got, ok := cfg.DefaultAccount()
	if !ok || got.ID != "aaa" {
		t.Fatalf("DefaultAccount() = %q, want the first account for a dangling ID", got.ID)
	}
	if id := cfg.ExplicitDefaultAccountID(); id != "" {
		t.Fatalf("ExplicitDefaultAccountID() = %q, want empty for a dangling ID", id)
	}
}

func TestSetDefaultAccountRejectsUnknownID(t *testing.T) {
	cfg := Config{Accounts: threeAccounts(), DefaultAccountID: "bbb"}
	cfg.SetDefaultAccount("nope")
	if cfg.DefaultAccountID != "" {
		t.Fatalf("DefaultAccountID = %q, want it cleared rather than set to an unknown ID", cfg.DefaultAccountID)
	}
	cfg.SetDefaultAccount("ccc")
	if cfg.DefaultAccountID != "ccc" {
		t.Fatalf("DefaultAccountID = %q, want ccc", cfg.DefaultAccountID)
	}
}

func TestReorderAccountsKeepsEveryAccount(t *testing.T) {
	cfg := Config{Accounts: threeAccounts()}

	cfg.ReorderAccounts([]string{"ccc", "aaa", "bbb"})
	if got := accountIDs(cfg); got != "ccc,aaa,bbb" {
		t.Fatalf("order = %s, want ccc,aaa,bbb", got)
	}

	// A stale list that names only some accounts, plus an ID that no longer
	// exists, must never drop an account.
	cfg.ReorderAccounts([]string{"bbb", "zzz"})
	if got := accountIDs(cfg); got != "bbb,ccc,aaa" {
		t.Fatalf("order = %s, want bbb then the untouched accounts in their previous order", got)
	}
	if len(cfg.Accounts) != 3 {
		t.Fatalf("account count = %d, want 3", len(cfg.Accounts))
	}
}

func accountIDs(c Config) string {
	ids := make([]string, 0, len(c.Accounts))
	for _, a := range c.Accounts {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, ",")
}

func TestSaveLoadRoundTripsDefaultAndOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := DefaultConfig()
	cfg.Accounts = threeAccounts()
	cfg.DefaultAccountID = "bbb"
	cfg.ReorderAccounts([]string{"ccc", "aaa", "bbb"})
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "tidemail", "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(raw)
	// A scalar declared after the account slice must still serialize above the
	// [[account]] tables, or the file is invalid TOML.
	if at, first := strings.Index(text, "default_account"), strings.Index(text, "[[account]]"); at < 0 || at > first {
		t.Fatalf("default_account at %d, first [[account]] at %d; want the scalar first:\n%s", at, first, text)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.DefaultAccountID != "bbb" {
		t.Fatalf("DefaultAccountID = %q, want bbb", loaded.DefaultAccountID)
	}
	if got := accountIDs(loaded); got != "ccc,aaa,bbb" {
		t.Fatalf("order after round trip = %s, want ccc,aaa,bbb", got)
	}
}

func TestLoadClearsDanglingDefaultAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "tidemail", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	toml := `default_account = "deleted-long-ago"

[[account]]
id = "aaa"
name = "Personal"
user = "mira@example.com"
`
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DefaultAccountID != "" {
		t.Fatalf("DefaultAccountID = %q, want it cleared", cfg.DefaultAccountID)
	}
	got, ok := cfg.DefaultAccount()
	if !ok || got.ID != "aaa" {
		t.Fatalf("DefaultAccount() = %q, want the surviving account", got.ID)
	}
}
