package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/allisonhere/tidemail/internal/auth"
	"github.com/allisonhere/tidemail/internal/config"
)

// A subprocess gives each case a fresh keychain availability probe and keeps
// the fake backend and process-wide OAuth callback out of other CLI tests.
func TestHeadlessRefreshTokenPersistence(t *testing.T) {
	if os.Getenv("TIDEMAIL_TEST_OAUTH_HELPER") == "1" {
		var out, errs bytes.Buffer
		if code := RunHeadless([]string{"send", "--help"}, bytes.NewReader(nil), &out, &errs); code != 0 {
			t.Fatalf("CLI startup: %d %s", code, errs.String())
		}
		const accountID = "stable-account-id"
		const token = "rotated-test-token"
		err := auth.PersistRefreshToken(accountID, token)
		if os.Getenv("TIDEMAIL_DISABLE_KEYRING") == "1" {
			if err == nil {
				t.Fatal("unavailable credential storage reported success")
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if got := config.GetOAuth2RefreshToken(accountID, "Different display name"); got != token {
			t.Fatalf("rotated token was not stored under stable account ID: %q", got)
		}
		return
	}
	if runtime.GOOS != "linux" {
		t.Skip("fake secret-tool backend is Linux-specific")
	}
	for _, disabled := range []string{"0", "1"} {
		t.Run("disabled="+disabled, func(t *testing.T) {
			dir := t.TempDir()
			script := `#!/bin/sh
for arg; do key=$arg; done
case "$1" in
 store) /usr/bin/cat > "$TIDEMAIL_TEST_SECRET_DIR/$key" ;;
 lookup) /usr/bin/cat "$TIDEMAIL_TEST_SECRET_DIR/$key" ;;
 clear) /usr/bin/rm -f "$TIDEMAIL_TEST_SECRET_DIR/$key" ;;
 *) exit 1 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "secret-tool"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("TIDEMAIL_TEST_SECRET_DIR", dir)
			t.Setenv("TIDEMAIL_DISABLE_KEYRING", disabled)
			t.Setenv("TIDEMAIL_TEST_OAUTH_HELPER", "1")
			cmd := exec.Command(os.Args[0], "-test.run=^TestHeadlessRefreshTokenPersistence$")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
		})
	}
}
