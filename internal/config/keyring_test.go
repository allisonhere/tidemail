package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fillSecrets / stripSecrets shell out to secret-tool; without it every keychain
// lookup returns "" and every store fails. These tests exercise the parts that
// don't depend on a live keychain: the AuthMethod gating.

func TestFillSecretsSkipsClientCredsForPasswordAccount(t *testing.T) {
	cfg := &Config{
		OAuth:    OAuthConfig{GoogleClientID: "app-id", GoogleClientSecret: "app-secret", MSClientID: "ms-id"},
		Accounts: []AccountConfig{{Name: "Gmail", Provider: "Gmail", AuthMethod: AuthPassword}},
	}
	fillSecrets(cfg)
	a := cfg.Accounts[0]
	if a.ClientID != "" || a.ClientSecret != "" {
		t.Fatalf("app-password account got OAuth client creds filled: id=%q secret=%q", a.ClientID, a.ClientSecret)
	}
}

func TestFillSecretsFillsClientCredsForOAuthAccount(t *testing.T) {
	gmail := &Config{
		OAuth:    OAuthConfig{GoogleClientID: "app-id", GoogleClientSecret: "app-secret"},
		Accounts: []AccountConfig{{Name: "Gmail", Provider: "Gmail", AuthMethod: AuthOAuth2}},
	}
	fillSecrets(gmail)
	if a := gmail.Accounts[0]; a.ClientID != "app-id" || a.ClientSecret != "app-secret" {
		t.Fatalf("oauth2 Gmail account: id=%q secret=%q, want app-id/app-secret", a.ClientID, a.ClientSecret)
	}

	outlook := &Config{
		OAuth:    OAuthConfig{MSClientID: "ms-id"},
		Accounts: []AccountConfig{{Name: "Work", Provider: "Outlook", AuthMethod: AuthOAuth2}},
	}
	fillSecrets(outlook)
	if a := outlook.Accounts[0]; a.ClientID != "ms-id" || a.ClientSecret != "" {
		t.Fatalf("oauth2 Outlook account: id=%q secret=%q, want ms-id and no secret", a.ClientID, a.ClientSecret)
	}
}

func TestStripSecretsLeavesTokenOnPasswordAccountUntouched(t *testing.T) {
	// A token on a "password" account is inert (fillSecrets / Uses*OAuth2 gate on
	// AuthMethod). stripSecrets must not wipe it — a wrong auto-migration would
	// otherwise be unrecoverable.
	cfg := &Config{Accounts: []AccountConfig{
		{Name: "Gmail", Provider: "Gmail", AuthMethod: AuthPassword, RefreshToken: "keep-me"},
	}}
	stripSecrets(cfg)
	if got := cfg.Accounts[0].RefreshToken; got != "keep-me" {
		t.Fatalf("stripSecrets wiped a token off an app-password account: %q", got)
	}
}

// fakeSecretTool installs a secret-tool that keeps items in files and logs every
// invocation's arguments, so a test can see exactly what reached the backend.
func fakeSecretTool(t *testing.T) (argLog string) {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "items")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	argLog = filepath.Join(dir, "args.log")
	script := `#!/bin/sh
cmd=$1
for a; do key=$a; done
printf '%s\n' "$*" >> ` + argLog + `
case "$cmd" in
  store) cat > "` + store + `/$key" ;;
  lookup) [ -f "` + store + `/$key" ] && cat "` + store + `/$key" || exit 1 ;;
  clear) rm -f "` + store + `/$key" ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "secret-tool"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TIDEMAIL_DISABLE_KEYRING", "")
	keyringProbeOnce = sync.Once{}
	t.Cleanup(func() { keyringProbeOnce = sync.Once{} })
	return argLog
}

// A password entered anywhere (typed or via the graphical dialog) is saved
// through StoreAccountPassword. It must reach secret-tool on stdin, never in the
// command line, and read back unchanged.
func TestStoreAccountPasswordKeepsTheSecretOutOfArguments(t *testing.T) {
	argLog := fakeSecretTool(t)
	const secret = "pa55 w0rd/with spaces"
	if !StoreAccountPassword("acct-1", secret) {
		t.Fatal("StoreAccountPassword failed against the fake backend")
	}
	if got := GetAccountPassword("acct-1", ""); got != secret {
		t.Errorf("round trip = %q, want %q", got, secret)
	}
	logged, err := os.ReadFile(argLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logged), "pa55") {
		t.Errorf("the secret appeared in secret-tool arguments:\n%s", logged)
	}
	if !strings.Contains(string(logged), "store --label=tidemail application tidemail key") {
		t.Errorf("expected a libsecret store call, got:\n%s", logged)
	}
	if StoreAccountPassword("acct-2", "") {
		t.Error("an empty password (a canceled dialog) must not be stored")
	}
}
