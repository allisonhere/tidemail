package db

import (
	"os"
	"testing"
)

// See internal/ui/main_test.go: the system keychain is global, so tests never
// touch it.
func TestMain(m *testing.M) {
	os.Setenv("TIDEMAIL_DISABLE_KEYRING", "1")
	os.Exit(m.Run())
}
