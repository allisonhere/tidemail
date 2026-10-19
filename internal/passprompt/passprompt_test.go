package passprompt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fake installs a stand-in dialog program that records its arguments and then
// runs body (a shell snippet), on a PATH holding nothing else.
func fake(t *testing.T, name, body string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir) // only the fake: a real zenity must never open during tests
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	t.Setenv("DISPLAY", "")
	for _, k := range []string{"SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		t.Setenv(k, "")
	}
	return argsFile
}

func TestAskReturnsThePasswordWithoutPuttingItInArguments(t *testing.T) {
	args := fake(t, "zenity", `echo 's3cret pass'`)
	got, err := Ask("TideMail", "Password for me@example.com")
	if err != nil || got != "s3cret pass" {
		t.Fatalf("Ask = %q, %v", got, err)
	}
	recorded, _ := os.ReadFile(args)
	if strings.Contains(string(recorded), "s3cret") {
		t.Errorf("the secret must never be on the command line: %q", recorded)
	}
	if !strings.Contains(string(recorded), "--password") || !strings.Contains(string(recorded), "me@example.com") {
		t.Errorf("unexpected arguments: %q", recorded)
	}
}

func TestAskFallsBackToKdialog(t *testing.T) {
	fake(t, "kdialog", `echo hunter2`)
	if got, err := Ask("T", "M"); err != nil || got != "hunter2" {
		t.Fatalf("Ask = %q, %v", got, err)
	}
}

func TestAskCancelAndEmptyAreCancellations(t *testing.T) {
	fake(t, "zenity", `exit 1`)
	if _, err := Ask("T", "M"); !errors.Is(err, ErrCanceled) {
		t.Errorf("exit 1 should be a cancellation, got %v", err)
	}
	fake(t, "zenity", `echo`)
	if _, err := Ask("T", "M"); !errors.Is(err, ErrCanceled) {
		t.Errorf("an empty password should be a cancellation, got %v", err)
	}
}

func TestAskErrorNeverIncludesTheOutput(t *testing.T) {
	fake(t, "zenity", `echo leaked-secret; exit 3`)
	_, err := Ask("T", "M")
	if err == nil || errors.Is(err, ErrCanceled) || strings.Contains(err.Error(), "leaked-secret") {
		t.Errorf("a failing dialog should give a generic error, got %v", err)
	}
}

func TestUnavailableOverSSHWithoutDisplayOrProvider(t *testing.T) {
	fake(t, "zenity", `echo x`)
	if !Available() {
		t.Fatal("expected the fake zenity to count as available")
	}
	t.Setenv("SSH_CONNECTION", "1.2.3.4 1 5.6.7.8 22")
	if Available() {
		t.Error("an SSH session must keep terminal input")
	}
	if _, err := Ask("T", "M"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Ask over SSH = %v", err)
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	if Available() {
		t.Error("no display means no dialog")
	}
	t.Setenv("DISPLAY", ":0")
	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Error("no dialog program means no dialog")
	}
}
