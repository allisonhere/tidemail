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
// installRawPlugin installs a message_raw plugin and returns its root.
func installRawPlugin(t *testing.T) string {
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
	return root
}

// pgpModel is a message-list model with a message_raw plugin installed and
// the first message showing in the reading pane.
func pgpModel(t *testing.T) (Model, db.Message) {
	t.Helper()
	m := newPluginModel(t, installRawPlugin(t))
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

// encryptedDBModel is a model on a real database holding one PGP/MIME
// encrypted message (no text of its own, two attachments) and one ordinary
// message with no body. withPlugin installs a PGP plugin.
func encryptedDBModel(t *testing.T, withPlugin bool) (Model, db.Message, db.Message) {
	t.Helper()
	m := newSendTestModel(t, 5)
	acc, err := m.db.AddAccount("", "A", "")
	if err != nil {
		t.Fatal(err)
	}
	mb, err := m.db.UpsertMailbox(db.Mailbox{AccountID: acc, Name: "INBOX", DisplayName: "Inbox", Delimiter: "."})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.db.UpsertMessage(db.Message{
		MailboxID: mb, UID: 1, Subject: "secret", From: "a@example.test", HasAttachment: true,
		AttachmentData: []db.Attachment{
			{Filename: "ATT00001", ContentType: "application/pgp-encrypted", Data: []byte("Version: 1")},
			{Filename: "encrypted.asc", ContentType: "application/octet-stream", Data: []byte("-----BEGIN PGP MESSAGE-----")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.db.UpsertMessage(db.Message{MailboxID: mb, UID: 2, Subject: "empty", From: "b@example.test"}); err != nil {
		t.Fatal(err)
	}
	msgs, err := m.db.ListMessages(mb)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages: %v %d", err, len(msgs))
	}
	var secret, empty db.Message
	for _, x := range msgs {
		if x.Subject == "secret" {
			secret = x
		} else {
			empty = x
		}
	}
	m.messages = msgs
	m.applyFilter()
	if withPlugin {
		mgr, err := plugin.Discover(installRawPlugin(t))
		if err != nil {
			t.Fatal(err)
		}
		m.SetPlugins(mgr, "", nil)
	}
	return m, secret, empty
}

func TestEncryptedMessageExplainsHowToReadIt(t *testing.T) {
	m, secret, empty := encryptedDBModel(t, true)
	if got := m.renderMessageForDisplay(secret, 80).body; !strings.Contains(got, "PGP-encrypted") || !strings.Contains(got, "Press D to decrypt it") || strings.Contains(got, "No message body") {
		t.Errorf("with a plugin the empty body should say how to decrypt, got:\n%s", got)
	}
	if got := m.renderMessageForDisplay(empty, 80).body; !strings.Contains(got, "No message body") {
		t.Errorf("an ordinary empty message keeps its wording, got:\n%s", got)
	}

	m, secret, _ = encryptedDBModel(t, false)
	if got := m.renderMessageForDisplay(secret, 80).body; !strings.Contains(got, "Install a PGP plugin") || !strings.Contains(got, "Settings") {
		t.Errorf("without a plugin the empty body should say how to get one, got:\n%s", got)
	}
}

func TestEncryptedCalloutGivesWayToTheDecryptedText(t *testing.T) {
	m, secret, _ := encryptedDBModel(t, true)
	view := pgpmail.View{Encrypted: true, Message: db.Message{BodyText: "the real text"}}
	next, _ := m.handlePGPDecoded(PGPDecodedMsg{MessageID: secret.ID, View: &view})
	m = next.(Model)
	if got := m.renderMessageForDisplay(secret, 80).body; !strings.Contains(got, "the real text") || strings.Contains(got, "Press D") {
		t.Errorf("decoded body should replace the callout, got:\n%s", got)
	}
}

// selectMessage moves the list cursor to a message and loads it into the
// reading pane, as moving the cursor does in the app.
func selectMessage(t *testing.T, m Model, id int64) Model {
	t.Helper()
	for i, x := range m.filteredMessages {
		if x.ID == id {
			m.messageCursor = i
			m.setViewportForCurrentRow()
			if m.contentMessageID != id {
				t.Fatalf("the reading pane shows %d, want %d", m.contentMessageID, id)
			}
			return m
		}
	}
	t.Fatalf("message %d is not in the list", id)
	return m
}

func TestStatusBarOffersDecryptForProtectedMessages(t *testing.T) {
	m, secret, empty := encryptedDBModel(t, true)
	m = selectMessage(t, m, secret.ID)

	for _, p := range []pane{paneMessages, paneContent} {
		if hint := m.renderPaneHint(p); !strings.Contains(hint, "D decrypt") {
			t.Errorf("pane %v hint lacks the decrypt key: %q", p, hint)
		}
	}
	if strip := m.statusBarKeyHintStrip(); !strings.Contains(strip, "decrypt") {
		t.Errorf("pane-header key strip lacks the decrypt key: %q", strip)
	}

	// An ordinary message offers nothing extra.
	other := selectMessage(t, m, empty.ID)
	if hint := other.renderPaneHint(paneMessages); strings.Contains(hint, "decrypt") {
		t.Errorf("hint on an ordinary message: %q", hint)
	}

	// Once decoded, the hint goes away; a failed attempt keeps it for a retry.
	next, _ := m.handlePGPDecoded(PGPDecodedMsg{MessageID: secret.ID, View: &pgpmail.View{Message: db.Message{BodyText: "x"}}})
	if hint := next.(Model).renderPaneHint(paneMessages); strings.Contains(hint, "decrypt") {
		t.Errorf("hint after decoding: %q", hint)
	}
	next, _ = m.handlePGPDecoded(PGPDecodedMsg{MessageID: secret.ID, Err: errors.New("cancelled")})
	if hint := next.(Model).renderPaneHint(paneMessages); !strings.Contains(hint, "D decrypt") {
		t.Errorf("a failed decode should keep the hint for a retry: %q", hint)
	}
}

func TestNoDecryptHintWithoutAPlugin(t *testing.T) {
	m, secret, _ := encryptedDBModel(t, false)
	m = selectMessage(t, m, secret.ID)
	if hint := m.renderPaneHint(paneMessages); strings.Contains(hint, "decrypt") {
		t.Errorf("offered a key that does nothing: %q", hint)
	}
}
