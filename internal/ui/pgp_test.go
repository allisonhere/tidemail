package ui

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/pgpmail"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// pgpModel is a message-list model with a message_raw plugin installed and
// the first message showing in the reading pane.
func pgpModel(t *testing.T) (Model, db.Message) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test plugins need a Unix shell")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "pgp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"pgp\"\nname = \"PGP\"\napi = 1\ncommand = \"run\"\n\n[permissions]\nmessage_raw = true\n"
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newPluginModel(t, root)
	msg := m.messages[0]
	msg.BodyText = "-----BEGIN PGP MESSAGE-----\n\nQUJD\n-----END PGP MESSAGE-----"
	msg.BodyHTML = ""
	m.messages[0] = msg
	m.applyFilter()
	m.contentMessageID = msg.ID
	return m, msg
}

func TestPGPCommandOnlyWithAReadPlugin(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	if slices.Contains(commandIDs(m), "pgp-decode") {
		t.Fatal("PGP command shown with no plugin")
	}
	m, _ = pgpModel(t)
	if !slices.Contains(commandIDs(m), "pgp-decode") {
		t.Fatal("PGP command missing although a message_raw plugin is installed")
	}
}

func TestPGPHintAppearsForProtectedMessages(t *testing.T) {
	m, msg := pgpModel(t)
	if line := m.pgpStatusLine(msg, 80); !strings.Contains(line, "Press D") {
		t.Errorf("no hint for a protected message: %q", line)
	}
	msg.BodyText = "an ordinary message"
	if line := m.pgpStatusLine(msg, 80); line != "" {
		t.Errorf("hint on an ordinary message: %q", line)
	}
}

func TestPGPDecodedTextReplacesTheBodyOnlyForDisplay(t *testing.T) {
	m, msg := pgpModel(t)
	view := pgpmail.View{
		Encrypted: true,
		Signature: plugin.Signature{Status: plugin.SignatureGood, Signer: "Ann <ann@example.com>"},
		Message:   db.Message{BodyText: "the meeting is at noon"},
	}
	next, _ := m.handlePGPDecoded(PGPDecodedMsg{MessageID: msg.ID, View: &view})
	m = next.(Model)

	if got := m.renderMessageForDisplay(msg, 80).body; !strings.Contains(got, "the meeting is at noon") || strings.Contains(got, "BEGIN PGP") {
		t.Errorf("display body = %q", got)
	}
	line := m.pgpStatusLine(msg, 100)
	if !strings.Contains(line, "decrypted") || !strings.Contains(line, "good signature from Ann") {
		t.Errorf("status line = %q", line)
	}
	// The stored message is untouched: nothing decrypted can reach storage.
	if !strings.Contains(m.messages[0].BodyText, "BEGIN PGP MESSAGE") {
		t.Error("the stored message body was replaced")
	}
}

func TestPGPErrorsAreShown(t *testing.T) {
	m, msg := pgpModel(t)
	next, _ := m.handlePGPDecoded(PGPDecodedMsg{MessageID: msg.ID, Err: errors.New("no secret key")})
	m = next.(Model)
	if line := m.pgpStatusLine(msg, 100); !strings.Contains(line, "no secret key") {
		t.Errorf("status line = %q", line)
	}
	if got := m.renderMessageForDisplay(msg, 80).body; !strings.Contains(got, "BEGIN PGP") {
		t.Error("a failed decode must leave the original body showing")
	}
}

func TestPGPDecodeNeedsAServerMessage(t *testing.T) {
	m, msg := pgpModel(t)
	next, cmd := m.startPGPDecode(msg)
	m = next.(Model)
	if cmd == nil || !strings.Contains(m.statusMsg, "can't be fetched") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if e := m.pgp.entries[msg.ID]; e != nil && e.pending {
		t.Error("a decode that cannot start must not stay pending")
	}
}

func TestPGPSignatureWordingForVerdicts(t *testing.T) {
	for status, want := range map[string]string{
		plugin.SignatureBad:        "BAD SIGNATURE",
		plugin.SignatureUnknownKey: "key you don't have",
		plugin.SignatureExpired:    "expired",
		plugin.SignatureNone:       "not signed",
	} {
		text, _ := describePGP(pgpmail.View{Signature: plugin.Signature{Status: status}})
		if !strings.Contains(text, want) {
			t.Errorf("%s: %q lacks %q", status, text, want)
		}
	}
}
