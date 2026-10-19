package ui

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

const armoredForTest = "-----BEGIN PGP MESSAGE-----\n\nQUJD\n-----END PGP MESSAGE-----\n"

// installPrepPlugin installs a message_compose plugin whose message.prepare
// answer is data (a JSON object), or an error when data is empty.
func installPrepPlugin(t *testing.T, root, data string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test plugins need a Unix shell")
	}
	dir := filepath.Join(root, "pgp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"pgp\"\nname = \"PGP\"\napi = 1\ncommand = \"run\"\n\n[permissions]\nmessage_compose = true\n"
	reply := fmt.Sprintf(`{"api":1,"type":"response","request_id":"%%s","ok":true,"data":%s}`, data)
	if data == "" {
		reply = `{"api":1,"type":"response","request_id":"%s","ok":false,"error":{"code":"no_public_key","message":"no usable key for bob@example.com"}}`
	}
	script := "#!/bin/sh\nread -r line\nid=$(printf '%s' \"$line\" | sed -n 's/.*\"request_id\":\"\\([^\"]*\\)\".*/\\1/p')\nprintf '" + reply + "\\n' \"$id\"\n"
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func pgpSendModel(t *testing.T, prepData string) Model {
	t.Helper()
	root := t.TempDir()
	installPrepPlugin(t, root, prepData)
	m := newSendTestModel(t, 5)
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	return m
}

func secretMessage() smtp.OutgoingMessage {
	return smtp.OutgoingMessage{
		From: "ann@example.com", To: []string{"bob@example.com"}, Subject: "plans",
		Body: "attack at dawn", HTMLBody: "<p>attack at dawn</p>",
		Attachments: []smtp.Attachment{{Name: "map.txt", Data: []byte("hidden valley")}},
	}
}

func TestPGPTogglesNeedAPlugin(t *testing.T) {
	m := newSendTestModel(t, 5)
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.overlay = overlayCompose
	next, _ := m.handleCompose(altKey('e'))
	m = next.(Model)
	if m.compose.pgpEncrypt || !strings.Contains(m.compose.statusMsg, "No PGP plugin") {
		t.Fatalf("encrypt on without a plugin: on=%v status=%q", m.compose.pgpEncrypt, m.compose.statusMsg)
	}

	m = pgpSendModel(t, `{}`)
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.overlay = overlayCompose
	for _, k := range []tea.KeyMsg{altKey('s'), altKey('e')} {
		next, _ := m.handleCompose(k)
		m = next.(Model)
	}
	if !m.compose.pgpSign || !m.compose.pgpEncrypt {
		t.Fatalf("toggles: sign=%v encrypt=%v", m.compose.pgpSign, m.compose.pgpEncrypt)
	}
	if !strings.Contains(m.compose.statusMsg, "PGP encrypt: on") {
		t.Errorf("a toggle should say so on the status line, got %q", m.compose.statusMsg)
	}
	next, _ = m.handleCompose(altKey('e'))
	if next.(Model).compose.pgpEncrypt {
		t.Error("a second Alt+E should turn encrypt off")
	}
	// F8 and F9 still work for terminals that pass them through.
	for _, k := range []tea.KeyType{tea.KeyF8, tea.KeyF9} {
		next, _ := m.handleCompose(tea.KeyMsg{Type: k})
		m = next.(Model)
	}
	if m.compose.pgpSign || m.compose.pgpEncrypt {
		t.Errorf("F8/F9 should toggle too (both were on): sign=%v encrypt=%v", m.compose.pgpSign, m.compose.pgpEncrypt)
	}
}

func TestSendCarriesTheProtectionRequest(t *testing.T) {
	c := NewCompose(config.AccountConfig{User: "ann@example.com", From: "ann@example.com"}, nil, nil)
	c.toInput.SetValue("bob@example.com")
	c.pgpAvailable, c.pgpEncrypt = true, true
	_, cmd, _ := c.send()
	q, ok := cmd().(SendQueuedMsg)
	if !ok || !q.Encrypt || q.Sign {
		t.Fatalf("queued = %+v", q)
	}
	c.pgpAvailable = false // plugin removed while the toggle was on
	if _, cmd, _ := c.send(); cmd != nil {
		t.Error("a send with PGP on and no plugin must not go out in the clear")
	}
}

func TestEncryptedSendQueuesOnlyCiphertext(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte(armoredForTest))
	m := pgpSendModel(t, `{"encrypted":"`+enc+`"}`)
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.overlay = overlayCompose

	next, cmd := m.Update(SendQueuedMsg{Account: config.AccountConfig{}, Msg: secretMessage(), Encrypt: true})
	m = next.(Model)
	if len(m.pendingSends) != 0 || cmd == nil || !m.compose.busy {
		t.Fatalf("queued before protection finished: pending=%d busy=%v", len(m.pendingSends), m.compose.busy)
	}
	out := cmd()
	protected, ok := out.(OutgoingProtectedMsg)
	if !ok || protected.Err != nil {
		t.Fatalf("protect: %#v", out)
	}
	next, _ = m.Update(protected)
	m = next.(Model)
	if len(m.pendingSends) != 1 || m.overlay != overlayNone {
		t.Fatalf("pending=%d overlay=%v", len(m.pendingSends), m.overlay)
	}
	items, err := m.db.ListOutbox()
	if err != nil || len(items) != 1 {
		t.Fatalf("outbox: %v %d", err, len(items))
	}
	item, err := m.db.GetOutbox(items[0].ID)
	if err != nil || len(item.MessageJSON) == 0 {
		t.Fatalf("reading the queued payload: %v", err)
	}
	payload := string(item.MessageJSON)
	for _, leak := range []string{"attack at dawn", "hidden valley", "map.txt"} {
		if strings.Contains(payload, leak) {
			t.Errorf("the queued message contains %q", leak)
		}
	}
	var queued smtp.OutgoingMessage
	if err := json.Unmarshal(item.MessageJSON, &queued); err != nil || queued.Protected == nil ||
		!strings.HasPrefix(string(queued.Protected.Encrypted), "-----BEGIN PGP MESSAGE-----") {
		t.Errorf("the queued message has no ciphertext: %v %+v", err, queued.Protected)
	}
}

func TestFailedProtectionQueuesNothingAndKeepsComposeOpen(t *testing.T) {
	m := pgpSendModel(t, "")
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.pgpEncrypt = true
	m.overlay = overlayCompose

	next, cmd := m.Update(SendQueuedMsg{Account: config.AccountConfig{}, Msg: secretMessage(), Encrypt: true})
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)

	if len(m.pendingSends) != 0 || m.overlay != overlayCompose {
		t.Fatalf("a failed protect must leave compose open: pending=%d overlay=%v", len(m.pendingSends), m.overlay)
	}
	if m.compose.busy || !m.compose.isErr || !strings.Contains(m.compose.statusMsg, "Not sent") || !strings.Contains(m.compose.statusMsg, "no PGP key for bob@example.com") {
		t.Errorf("status = %q busy=%v", m.compose.statusMsg, m.compose.busy)
	}
	if items, _ := m.db.ListOutbox(); len(items) != 0 {
		t.Errorf("outbox has %d items after a failed protect", len(items))
	}
}

func TestEditingAQueuedEncryptedMessageKeepsEncryptOn(t *testing.T) {
	enc := &smtp.Protection{Encrypted: []byte(armoredForTest)}
	sig := &smtp.Protection{Entity: []byte("x"), Signature: []byte("s"), MicAlg: "pgp-sha256"}
	for _, c := range []struct {
		p                     *smtp.Protection
		wantSign, wantEncrypt bool
	}{{enc, false, true}, {sig, true, false}, {nil, false, false}} {
		payload := mustJSON(t, smtp.OutgoingMessage{To: []string{"bob@example.com"}, Protected: c.p})
		sign, encrypt := protectionFlags(payload)
		if sign != c.wantSign || encrypt != c.wantEncrypt {
			t.Errorf("flags(%+v) = %v/%v", c.p, sign, encrypt)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func altKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true} }

func TestHumanPGPError(t *testing.T) {
	wrap := func(code, msg string) error {
		return fmt.Errorf("plugin %q: %w", "pgp", &plugin.Error{Code: code, Message: msg})
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{wrap("no_public_key", "no usable key for drbayless@outlook.com (no such key)"), "Not sent: no PGP key for drbayless@outlook.com (alt+e turns encryption off)"},
		{wrap("no_public_key", "no usable key for bob@example.com"), "Not sent: no PGP key for bob@example.com (alt+e turns encryption off)"},
		{wrap("no_public_key", "no usable key for bob@example.com (expired)"), "Not sent: can't use the PGP key for bob@example.com: expired (alt+e turns encryption off)"},
		{wrap("no_secret_key", "no secret key to sign as me@example.com"), "Not sent: no PGP signing key for me@example.com (alt+s turns signing off)"},
		{wrap("bad_passphrase", "bad passphrase"), "Not sent: wrong passphrase"},
		{wrap("pinentry", "gpg couldn't get your passphrase (cancelled, or no graphical pinentry available): x"), "Not sent: GnuPG got no passphrase (cancelled, or no prompt could open)"},
		{wrap("weird", "something odd"), "Not sent. PGP: something odd"},
		{errors.New("plugin \"pgp\": timed out"), "Not sent. PGP: plugin \"pgp\": timed out"},
	} {
		got := humanPGPError(c.err)
		if got != c.want {
			t.Errorf("humanPGPError(%v)\n got  %q\n want %q", c.err, got, c.want)
		}
		if strings.HasPrefix(got, "Not sent:") && len(got) > 95 {
			t.Errorf("%q is %d characters; the status row is one line", got, len(got))
		}
	}
}
