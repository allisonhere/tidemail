package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	resetKeyringProbe(t)
	return argLog
}

// resetKeyringProbe clears the memoized keyringUsable probe (sync.Once plus
// its cached result/reason) so a test gets a fresh probe instead of a
// previous test's cached outcome, and restores it afterward.
func resetKeyringProbe(t *testing.T) {
	t.Helper()
	keyringProbeOnce = sync.Once{}
	keyringProbeResult = false
	keyringProbeReason = ""
	t.Cleanup(func() {
		keyringProbeOnce = sync.Once{}
		keyringProbeResult = false
		keyringProbeReason = ""
	})
}

// A password entered anywhere (typed or via the graphical dialog) is saved
// through StoreAccountPassword. It must reach secret-tool on stdin, never in the
// command line, and read back unchanged.
//
// This exercises the public API, which now dispatches on runtime.GOOS, so it
// only applies on Linux: on a real Mac, StoreAccountPassword would reach the
// actual `security` binary instead of the fake secret-tool below, writing to
// the developer's real login keychain. TestDarwinStoreLookupRoundTrip covers
// the equivalent round trip for darwin via the injectable goos parameter,
// safely on any host.
func TestStoreAccountPasswordKeepsTheSecretOutOfArguments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("exercises the Linux secret-tool backend through the public API")
	}
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

// TestBackendForArgv pins the exact argv each platform's backend builds, so
// an accidental argument-order change is caught immediately.
func TestBackendForArgv(t *testing.T) {
	const key = "account:abc"
	tests := []struct {
		goos       string
		wantOK     bool
		wantTool   string
		wantStore  []string
		wantLookup []string
		wantClear  []string
	}{
		{
			goos: "linux", wantOK: true, wantTool: "secret-tool",
			wantStore:  []string{"store", "--label=tidemail", "application", "tidemail", "key", key},
			wantLookup: []string{"lookup", "application", "tidemail", "key", key},
			wantClear:  []string{"clear", "application", "tidemail", "key", key},
		},
		{
			goos: "darwin", wantOK: true, wantTool: "security",
			wantStore:  []string{"add-generic-password", "-U", "-a", key, "-s", "tidemail", "-w"},
			wantLookup: []string{"find-generic-password", "-a", key, "-s", "tidemail", "-w"},
			wantClear:  []string{"delete-generic-password", "-a", key, "-s", "tidemail"},
		},
		{goos: "windows", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			b, ok := backendFor(tt.goos)
			if ok != tt.wantOK {
				t.Fatalf("backendFor(%q) ok = %v, want %v", tt.goos, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if b.tool != tt.wantTool {
				t.Errorf("tool = %q, want %q", b.tool, tt.wantTool)
			}
			if got := b.storeArgs(key); !reflect.DeepEqual(got, tt.wantStore) {
				t.Errorf("storeArgs = %v, want %v", got, tt.wantStore)
			}
			if got := b.lookupArgs(key); !reflect.DeepEqual(got, tt.wantLookup) {
				t.Errorf("lookupArgs = %v, want %v", got, tt.wantLookup)
			}
			if got := b.clearArgs(key); !reflect.DeepEqual(got, tt.wantClear) {
				t.Errorf("clearArgs = %v, want %v", got, tt.wantClear)
			}
		})
	}
}

// fakeSecurity installs a fake `security` binary on PATH that mimics macOS's
// add-generic-password/find-generic-password/delete-generic-password well
// enough to exercise the darwin backend: items are kept in files, every
// invocation's arguments are logged, and a lookup or delete miss exits 44
// (errSecItemNotFound), matching the real tool's exit code rather than just
// "any nonzero".
func fakeSecurity(t *testing.T) (argLog string) {
	t.Helper()
	dir := t.TempDir()
	store := filepath.Join(dir, "items")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	argLog = filepath.Join(dir, "args.log")
	// Argument positions are fixed because this double only needs to handle
	// the exact argv shapes keyring.go's darwin backend builds:
	//   add-generic-password -U -a <key> -s <service> -w <value>  ($3=key $7=value)
	//   find-generic-password -a <key> -s <service> -w             ($2=key)
	//   delete-generic-password -a <key> -s <service>              ($2=key)
	script := `#!/bin/sh
cmd=$1
shift
printf '%s %s\n' "$cmd" "$*" >> ` + argLog + `
case "$cmd" in
  add-generic-password)
    printf '%s' "$7" > "` + store + `/$3"
    ;;
  find-generic-password)
    [ -f "` + store + `/$2" ] && cat "` + store + `/$2" || exit 44
    ;;
  delete-generic-password)
    [ -f "` + store + `/$2" ] && rm -f "` + store + `/$2" || exit 44
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "security"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TIDEMAIL_DISABLE_KEYRING", "")
	resetKeyringProbe(t)
	return argLog
}

// The darwin backend is reached through an injectable goos parameter, not
// runtime.GOOS, so these round trips run on any CI runner (including the
// Linux one this repo actually uses) while still exercising the macOS-only
// code path end to end against the fake security binary above.

func TestDarwinStoreLookupRoundTrip(t *testing.T) {
	fakeSecurity(t)
	const key, secret = "account:acct-1", "pa55 w0rd/with spaces"
	if err := storeSecretFor("darwin", key, secret); err != nil {
		t.Fatalf("storeSecretFor(darwin) failed: %v", err)
	}
	if got := lookupSecretFor("darwin", key); got != secret {
		t.Errorf("lookupSecretFor(darwin) = %q, want %q", got, secret)
	}
}

func TestDarwinLookupMissReturnsEmptyNotError(t *testing.T) {
	fakeSecurity(t)
	if got := lookupSecretFor("darwin", "account:never-stored"); got != "" {
		t.Errorf("lookup of a never-stored key = %q, want empty (first run must not be an error)", got)
	}
}

func TestDarwinClearOnMissingItemDoesNotPanic(t *testing.T) {
	fakeSecurity(t)
	if err := clearSecretFor("darwin", "account:never-stored"); err == nil {
		t.Error("expected an error clearing a nonexistent item")
	}
}

func TestDarwinKeyringUsableProbe(t *testing.T) {
	fakeSecurity(t)
	if !keyringUsableFor("darwin") {
		t.Fatalf("keyringUsableFor(darwin) = false, reason: %s", keyringProbeReason)
	}
}

func TestDarwinKeyringUsableProbeReportsLockedKeychain(t *testing.T) {
	dir := t.TempDir()
	// A `security` that always fails simulates a locked/unreachable keychain.
	if err := os.WriteFile(filepath.Join(dir, "security"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TIDEMAIL_DISABLE_KEYRING", "")
	resetKeyringProbe(t)

	if keyringUsableFor("darwin") {
		t.Fatal("keyringUsableFor(darwin) = true against a failing security binary")
	}
	if keyringProbeReason == "" {
		t.Error("expected a non-empty probe failure reason")
	}
}
