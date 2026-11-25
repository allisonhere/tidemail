package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/emersion/go-imap/v2"
	imapclient "github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

func attachmentPermissions(t *testing.T, permissions string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tidemail")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("agent_permissions = ["+permissions+"]\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMessageAttachmentsPermissionsAndCachedExport(t *testing.T) {
	id := mailReadFixture(t)
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	// A name longer than a filesystem allows (255 bytes) must still export.
	long := strings.Repeat("a", 300) + ".txt"
	for _, name := range []string{"../../report.txt", "report.txt", `C:\private\report.txt`, "empty.txt", long} {
		data := []byte("private attachment")
		if name == "empty.txt" {
			data = []byte{}
		}
		if _, err := database.SaveAttachment(id, db.Attachment{Filename: name, ContentType: "text/plain", Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	database.Close()
	target := filepath.Join(t.TempDir(), "exports")
	for _, permissions := range []string{"default", "", `'messages.read'`, `'messages.metadata'`} {
		if permissions != "default" {
			attachmentPermissions(t, permissions)
		}
		var out, diag bytes.Buffer
		code := RunMailRead("messages", []string{"attachments", fmt.Sprint(id), "--out", target, "--agent", "--json"}, &out, &diag)
		if code != 3 || !strings.Contains(out.String(), "attachments.read") || strings.Contains(out.String(), "private attachment") {
			t.Fatalf("%d %s %s", code, &out, &diag)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("denied operation created output: %v", err)
		}
	}
	attachmentPermissions(t, `'attachments.read'`)
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(target, "report.txt")); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	code := RunMailRead("messages", []string{"attachments", "--out", target, fmt.Sprintf("cache:%d", id), "--agent", "--json"}, &out, &diag)
	if code != 0 {
		t.Fatalf("%d %s %s", code, &out, &diag)
	}
	var envelope struct {
		Data struct {
			Items []struct {
				Path string
				Size int
			}
		}
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Items) != 5 || strings.Contains(out.String(), "private attachment") || strings.Contains(out.String(), "body_text") {
		t.Fatal(out.String())
	}
	for _, item := range envelope.Data.Items {
		data, err := os.ReadFile(item.Path)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(item.Path) != target || len(data) != item.Size || len(filepath.Base(item.Path)) > 100 || !strings.HasSuffix(item.Path, ".txt") {
			t.Fatal(item)
		}
		info, err := os.Stat(item.Path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("file permissions: %v %v", info, err)
		}
	}
	if data, _ := os.ReadFile(sentinel); string(data) != "keep" {
		t.Fatal("overwrote symlink target")
	}
	out.Reset()
	if code := RunMailRead("messages", []string{"read", fmt.Sprint(id), "--agent", "--json"}, &out, &diag); code != 3 {
		t.Fatalf("attachment permission allowed body read: %d", code)
	}
	// Forced agent mode cannot be disabled by --agent=false.
	attachmentPermissions(t, `'messages.read'`)
	t.Setenv(agentEnv, "1")
	out.Reset()
	if code := RunMailRead("messages", []string{"attachments", fmt.Sprint(id), "--out", target, "--agent=false", "--json"}, &out, &diag); code != 3 {
		t.Fatalf("forced agent bypass: %d %s", code, &out)
	}
}

func TestMessageAttachmentsArgumentsAndEmpty(t *testing.T) {
	id := mailReadFixture(t)
	for _, args := range [][]string{{"attachments", "--help"}, {"attachments", fmt.Sprint(id), "--out", t.TempDir(), "--json"}} {
		var out, diag bytes.Buffer
		if code := RunMailRead("messages", args, &out, &diag); code != 0 {
			t.Fatalf("%d %s %s", code, &out, &diag)
		}
	}
	for _, args := range [][]string{{"attachments"}, {"attachments", "cache:0", "--out", t.TempDir()}, {"attachments", fmt.Sprint(id)}, {"attachments", fmt.Sprint(id), "--out", t.TempDir(), "--limit", "1"}} {
		var out, diag bytes.Buffer
		if code := RunMailRead("messages", append(args, "--json"), &out, &diag); code != 2 || !strings.Contains(out.String(), "invalid_arguments") {
			t.Fatalf("%d %s %s", code, &out, &diag)
		}
	}
}

func TestMessageAttachmentsIMAPFallback(t *testing.T) {
	id := mailReadFixture(t)
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("testuser", "testpass")
	mem.AddUser(user)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	server := imapserver.New(&imapserver.Options{NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return mem.NewSession(), nil, nil
	}, InsecureAuth: true})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(ln)
	defer server.Close()
	defer ln.Close()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := imapclient.New(conn, nil)
	defer client.Close()
	if err := client.Login("testuser", "testpass").Wait(); err != nil {
		t.Fatal(err)
	}
	raw := "Message-ID: <attachment-test@example.com>\r\nSubject: Private body\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=abc\r\n\r\n--abc\r\nContent-Type: text/plain\r\n\r\nprivate body\r\n--abc\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=remote.bin\r\nContent-Transfer-Encoding: base64\r\n\r\nAAEC/w==\r\n--abc--\r\n"
	cmd := client.Append("INBOX", int64(len(raw)), nil)
	if _, err := cmd.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	appended, err := cmd.Wait()
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	m, err := database.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE messages SET uid=?, message_id=?, has_attachment=1 WHERE id=?", uint32(appended.UID), "<attachment-test@example.com>", id); err != nil {
		t.Fatal(err)
	}
	if err := database.SetMailboxUIDValidity(m.MailboxID, appended.UIDValidity); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Accounts: []config.AccountConfig{{ID: "account-stable", User: "testuser", Password: "testpass", IMAPHost: "127.0.0.1", IMAPPort: ln.Addr().(*net.TCPAddr).Port}}}
	m, err = database.GetMessage(id)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := fetchMessageAttachments(database, cfg, m)
	if err != nil || len(parts) != 1 || !bytes.Equal(parts[0].Data, []byte{0, 1, 2, 255}) {
		t.Fatalf("%v %v", parts, err)
	}

	// Exercise the command with both absent metadata and missing cached bytes.
	attachmentPermissions(t, `'attachments.read'`)
	configPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tidemail", "config.toml")
	configText := fmt.Sprintf("agent_permissions = ['attachments.read']\n[[account]]\nid = 'account-stable'\nname = 'Personal'\nuser = 'testuser'\npassword = 'testpass'\nimap_host = '127.0.0.1'\nimap_port = %d\nimap_tls = false\nauth_method = 'password'\n", ln.Addr().(*net.TCPAddr).Port)
	if err := os.WriteFile(configPath, []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	for _, withMetadata := range []bool{false, true} {
		if withMetadata {
			if _, err := database.Exec("INSERT INTO attachments(message_id,filename,content_type,data,size) VALUES(?,?,?,X'',4)", id, "remote.bin", "application/octet-stream"); err != nil {
				t.Fatal(err)
			}
		}
		var out, diag bytes.Buffer
		target := t.TempDir()
		code := RunMailRead("messages", []string{"attachments", fmt.Sprint(id), "--out", target, "--agent", "--json"}, &out, &diag)
		if code != 0 || strings.Contains(out.String(), "private body") {
			t.Fatalf("%d %s %s", code, &out, &diag)
		}
		data, err := os.ReadFile(filepath.Join(target, "remote.bin"))
		if err != nil || !bytes.Equal(data, []byte{0, 1, 2, 255}) {
			t.Fatalf("%v %v", data, err)
		}
	}
	// The fetch leaves the original message unread.
	if _, err := client.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	msgs, err := client.Fetch(imap.UIDSetNum(appended.UID), &imap.FetchOptions{Flags: true}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("%v %v", msgs, err)
	}
	for _, flag := range msgs[0].Flags {
		if flag == imap.FlagSeen {
			t.Fatal("fetch marked message read")
		}
	}
	if err := database.SetMailboxUIDValidity(m.MailboxID, appended.UIDValidity+1); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchMessageAttachments(database, cfg, m); err == nil {
		t.Fatal("accepted stale UIDVALIDITY")
	}
	target := filepath.Join(t.TempDir(), "failed-export")
	var out, diag bytes.Buffer
	if code := RunMailRead("messages", []string{"attachments", fmt.Sprint(id), "--out", target, "--agent", "--json"}, &out, &diag); code != 1 || !strings.Contains(out.String(), "attachment_fetch_failed") {
		t.Fatalf("%d %s %s", code, &out, &diag)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("failed fetch created output: %v", err)
	}
}
