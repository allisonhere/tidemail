package config

import (
	"os"
	"testing"
)

// Credential storage is off by default for this package's tests: the system
// keychain is global, and a test config whose account names match the real ones
// would otherwise read live passwords and write copies under fresh keys. The
// tests that exercise the keychain itself re-enable it for their own scope with
// t.Setenv and clean up the items they create.
func TestMain(m *testing.M) {
	os.Setenv("TIDEMAIL_DISABLE_KEYRING", "1")
	os.Exit(m.Run())
}
