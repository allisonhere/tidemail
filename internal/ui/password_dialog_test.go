package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/passprompt"
)

// dialogEnv puts only a fake zenity on PATH (a real one must never open in
// tests) in a pretend Wayland session.
func dialogEnv(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "zenity"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("WAYLAND_DISPLAY", "wayland-test")
	for _, k := range []string{"DISPLAY", "SSH_CONNECTION", "SSH_TTY", "SSH_CLIENT"} {
		t.Setenv(k, "")
	}
}

func passwordForm() AccountManager {
	am := NewAccountManager(nil)
	am.mode = amAdd
	am.userInput.SetValue("alice@example.com")
	am.focusField(amFieldPass)
	return am
}

var altP = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p"), Alt: true}

func TestPasswordDialogFillsTheFieldThroughTheNormalForm(t *testing.T) {
	dialogEnv(t, `echo 'dialog-pass'`)
	am := passwordForm()
	am.passInput.SetValue("old")
	am, cmd, _ := am.Update(altP, DefaultKeys)
	if cmd == nil {
		t.Fatalf("alt+p on the Password row should open the dialog (status %q)", am.statusMsg)
	}
	msg, ok := cmd().(PasswordDialogMsg)
	if !ok || msg.Err != nil || msg.Secret != "dialog-pass" {
		t.Fatalf("msg = %#v", cmd())
	}
	am = am.applyPasswordDialog(msg)
	if got := am.passInput.Value(); got != "dialog-pass" {
		t.Errorf("password field = %q", got)
	}
	// It is saved like a typed password: it is what buildCfg hands to the
	// keychain code.
	if got := am.buildCfg().Password; got != "dialog-pass" {
		t.Errorf("buildCfg password = %q", got)
	}
	if strings.Contains(am.statusMsg, "dialog-pass") {
		t.Errorf("the status line must not show the password: %q", am.statusMsg)
	}
}

func TestPasswordDialogCancelLeavesTheFieldAlone(t *testing.T) {
	dialogEnv(t, `exit 1`)
	am := passwordForm()
	am.passInput.SetValue("keep-me")
	am, cmd, _ := am.Update(altP, DefaultKeys)
	am = am.applyPasswordDialog(cmd().(PasswordDialogMsg))
	if am.passInput.Value() != "keep-me" || !strings.Contains(am.statusMsg, "CANCELED") {
		t.Errorf("cancel changed the field or said nothing: %q / %q", am.passInput.Value(), am.statusMsg)
	}
	// A dialog that breaks does not clobber it either.
	am = am.applyPasswordDialog(PasswordDialogMsg{Err: os.ErrInvalid})
	if am.passInput.Value() != "keep-me" || !strings.Contains(am.statusMsg, "TYPE THE PASSWORD") {
		t.Errorf("failure should point back to the field: %q", am.statusMsg)
	}
}

func TestPasswordDialogFallsBackToTheTerminalFieldWhenUnavailable(t *testing.T) {
	dialogEnv(t, `echo should-not-run`)
	t.Setenv("SSH_CONNECTION", "1.2.3.4 1 5.6.7.8 22")
	am := passwordForm()
	am.passInput.SetValue("typed")
	am, cmd, _ := am.Update(altP, DefaultKeys)
	if cmd != nil {
		t.Fatal("over SSH no dialog may open")
	}
	if !strings.Contains(am.statusMsg, "TYPE THE PASSWORD IN THE FIELD") || am.passInput.Value() != "typed" {
		t.Errorf("status %q, field %q", am.statusMsg, am.passInput.Value())
	}
	// Typing still works.
	am, _, _ = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, DefaultKeys)
	if am.passInput.Value() != "typedx" {
		t.Errorf("terminal input broke: %q", am.passInput.Value())
	}
	if passprompt.Available() {
		t.Error("expected the dialog to be unavailable over SSH")
	}
}

func TestPasswordDialogOnlyOnThePasswordRow(t *testing.T) {
	dialogEnv(t, `echo nope`)
	am := passwordForm()
	am.focusField(amFieldUser)
	if _, cmd, _ := am.Update(altP, DefaultKeys); cmd != nil {
		t.Error("alt+p must do nothing outside the Password row")
	}
}

func TestPasswordDialogResultIgnoredAfterTheFormCloses(t *testing.T) {
	am := passwordForm()
	am.mode = amList
	am = am.applyPasswordDialog(PasswordDialogMsg{Secret: "late"})
	if am.passInput.Value() != "" {
		t.Errorf("a late result must not fill a closed form: %q", am.passInput.Value())
	}
}

func TestPasswordDialogKeyReachesTheFormThroughTheModel(t *testing.T) {
	dialogEnv(t, `echo from-dialog`)
	m, _, _ := newIdentityModel(t, acct("Personal", "imap.example.com", "me@example.com", "pw"))
	t.Setenv("PATH", os.Getenv("PATH")) // keep dialogEnv's PATH
	m.accountManager = passwordForm()
	m.overlay = overlayAccountManager
	next, cmd := m.Update(altP)
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("alt+p did not reach the account form (status %q)", m.accountManager.statusMsg)
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if got := m.accountManager.passInput.Value(); got != "from-dialog" {
		t.Errorf("password field = %q", got)
	}
}
