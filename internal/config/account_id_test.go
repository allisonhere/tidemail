package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
)

// Stamped IDs must be stable: a second pass over the same config must not mint
// new ones, or every Save would orphan the keychain items written under the
// previous generation.
func TestEnsureAccountIDsIsStable(t *testing.T) {
	cfg := &Config{Accounts: []AccountConfig{
		{Name: "Personal"}, {Name: "Work"},
	}}
	if !ensureAccountIDs(cfg) {
		t.Fatal("expected the first pass to stamp IDs")
	}
	first := []string{cfg.Accounts[0].ID, cfg.Accounts[1].ID}
	if first[0] == "" || first[1] == "" {
		t.Fatalf("an account was left without an ID: %#v", cfg.Accounts)
	}
	if first[0] == first[1] {
		t.Fatalf("two accounts got the same ID: %v", first)
	}
	if ensureAccountIDs(cfg) {
		t.Fatal("a second pass must be a no-op")
	}
	if cfg.Accounts[0].ID != first[0] || cfg.Accounts[1].ID != first[1] {
		t.Fatalf("IDs changed on the second pass: %#v", cfg.Accounts)
	}
}

// Two accounts sharing an ID — a hand-edited config, or a block copy-pasted as
// a starting point — must be separated, since the ID is the join key.
func TestEnsureAccountIDsResolvesCollisions(t *testing.T) {
	cfg := &Config{Accounts: []AccountConfig{
		{ID: "same", Name: "Personal"}, {ID: "same", Name: "Work"},
	}}
	if !ensureAccountIDs(cfg) {
		t.Fatal("expected the collision to be resolved")
	}
	if cfg.Accounts[0].ID == cfg.Accounts[1].ID {
		t.Fatalf("collision survived: %#v", cfg.Accounts)
	}
	if cfg.Accounts[0].ID != "same" {
		t.Fatalf("the first claimant should keep the ID, got %q", cfg.Accounts[0].ID)
	}
}

// Loading a config written before IDs existed must stamp one per account and
// leave every other field alone.
func TestLoadStampsIDsOnLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "tidemail", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `theme = "catppuccin-mocha"

[[account]]
name = "David Blangstrup"
imap_host = "imap.gigahost.dk"
user = "d@blangstrup.info"

[[account]]
name = "Gmail"
imap_host = "imap.gmail.com"
user = "d@gmail.com"
`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(cfg.Accounts))
	}
	if cfg.Accounts[0].ID == "" || cfg.Accounts[1].ID == "" || cfg.Accounts[0].ID == cfg.Accounts[1].ID {
		t.Fatalf("legacy accounts were not given distinct IDs: %#v", cfg.Accounts)
	}
	if cfg.Accounts[0].IMAPHost != "imap.gigahost.dk" || cfg.Accounts[1].IMAPHost != "imap.gmail.com" {
		t.Fatalf("stamping IDs disturbed the accounts: %#v", cfg.Accounts)
	}

	// Save must write the IDs through, so the next load reuses them, and must
	// teach the in-memory config the same IDs rather than minting new ones.
	want := []string{cfg.Accounts[0].ID, cfg.Accounts[1].ID}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var round Config
	if _, err := toml.DecodeFile(path, &round); err != nil {
		t.Fatalf("decode saved config: %v", err)
	}
	if len(round.Accounts) != 2 || round.Accounts[0].ID != want[0] || round.Accounts[1].ID != want[1] {
		t.Fatalf("saved IDs do not match the loaded ones: %#v", round.Accounts)
	}
}

// Save on a config assembled in code must stamp IDs into the caller's slice,
// not only into its own copy — otherwise each Save mints a fresh ID and the
// password stored under the previous one is orphaned.
func TestSaveStampsIDsIntoCallersAccounts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Accounts = []AccountConfig{{Name: "Personal", IMAPHost: "imap.example.com"}}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	first := cfg.Accounts[0].ID
	if first == "" {
		t.Fatal("Save did not stamp an ID into the caller's slice")
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if cfg.Accounts[0].ID != first {
		t.Fatalf("a second Save minted a new ID: %q then %q", first, cfg.Accounts[0].ID)
	}
}

// The session/token cache key must be the stable ID, so two accounts sharing a
// display name no longer share an IMAP connection slot or an OAuth token entry.
func TestSessionKeyIsTheStableID(t *testing.T) {
	a := AccountConfig{ID: "aaa", Name: "David Blangstrup", User: "d@blangstrup.info", IMAPHost: "imap.gigahost.dk"}
	b := AccountConfig{ID: "bbb", Name: "David Blangstrup", User: "d@gmail.com", IMAPHost: "imap.gmail.com"}
	if a.SessionKey() == b.SessionKey() {
		t.Fatalf("same-named accounts still share a session key: %q", a.SessionKey())
	}
	if a.SessionKey() != "aaa" {
		t.Fatalf("SessionKey = %q, want the account ID", a.SessionKey())
	}
	// No ID (a config built in code): fall back to something per-account, never
	// the display name.
	noID := AccountConfig{Name: "David Blangstrup", User: "d@gmail.com", IMAPHost: "imap.gmail.com"}
	if noID.SessionKey() != "d@gmail.com@imap.gmail.com" {
		t.Fatalf("fallback SessionKey = %q", noID.SessionKey())
	}
}

// A password stored by an older build under the display name must still be
// found after the account is stamped with an ID — the migration is a read
// fallback, not a rewrite, so a locked keychain can never lose it.
func TestGetAccountPasswordFallsBackToLegacyNameKey(t *testing.T) {
	t.Setenv("TIDEMAIL_DISABLE_KEYRING", "0")
	if !keyringUsable() {
		t.Skip("system keychain unavailable")
	}
	const name = "tidemail-test-legacy-account"
	const id = "tidemail-test-id"
	if err := storeSecret(accountPasswordKey(name), "legacy-pw"); err != nil {
		t.Skipf("cannot write to the keychain: %v", err)
	}
	t.Cleanup(func() {
		_ = clearSecret(accountPasswordKey(name))
		_ = clearSecret(accountPasswordKey(id))
	})
	if got := GetAccountPassword(id, name); got != "legacy-pw" {
		t.Fatalf("legacy password not found: got %q", got)
	}
	// Once an ID-keyed item exists it wins, and the legacy item is left in place.
	if !StoreAccountPassword(id, "new-pw") {
		t.Skip("keychain write failed")
	}
	if got := GetAccountPassword(id, name); got != "new-pw" {
		t.Fatalf("ID-keyed password should win: got %q", got)
	}
	if got := lookupSecret(accountPasswordKey(name)); got != "legacy-pw" {
		t.Fatalf("the legacy item must not be rewritten or cleared: got %q", got)
	}
}
