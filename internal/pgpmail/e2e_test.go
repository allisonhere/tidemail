package pgpmail_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/pgpmail"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

// These tests run the real PGP plugin (plugins/pgp in the tidemail-plugins
// repository) with real gpg and throwaway keyrings, through TideMail's plugin
// runtime, mailapp.Protect, smtp.BuildRaw and pgpmail.Decode. They need that
// repository: set TIDEMAIL_PLUGINS_DIR to its checkout (CI there does), or keep
// it next to this one. Without it, or without gpg, they skip.

func pluginsRepo(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("TIDEMAIL_PLUGINS_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "tidemail-plugins")
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins", "pgp", "plugin.toml")); err != nil {
		t.Skip("tidemail-plugins checkout not found (set TIDEMAIL_PLUGINS_DIR)")
	}
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed")
	}
	return dir
}

// installPGPPlugin builds the plugin and installs it the way a user would.
func installPGPPlugin(t *testing.T) *plugin.Manager {
	t.Helper()
	src, err := filepath.Abs(filepath.Join(pluginsRepo(t), "plugins", "pgp"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := filepath.Join(root, "pgp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-buildvcs=false", "-o", filepath.Join(dir, "tidemail-plugin-pgp"), ".")
	build.Dir = src
	// This plugin is a standalone module, not part of the host development workspace.
	build.Env = append(os.Environ(), "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the plugin: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile(filepath.Join(src, "plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	mgr, err := plugin.Discover(root)
	if err != nil || len(mgr.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, mgr.Errors())
	}
	return mgr
}

// keyring creates a fresh GNUPGHOME with one key per address; the directory is
// short so gpg-agent's socket path fits.
func keyring(t *testing.T, addrs ...string) {
	t.Helper()
	home, err := os.MkdirTemp("", "tg")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = exec.Command("gpgconf", "--homedir", home, "--kill", "all").Run()
		_ = os.RemoveAll(home)
	})
	t.Setenv("GNUPGHOME", home)
	for _, a := range addrs {
		out, err := exec.Command("gpg", "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
			"--quick-generate-key", a, "default", "default", "never").CombinedOutput()
		if err != nil {
			t.Fatalf("generate key for %s: %v\n%s", a, err, out)
		}
	}
}

// trustAll marks every key ultimately trusted, as a user would after verifying
// them, so encryption does not need the plugin's trust=always setting.
func trustAll(t *testing.T) {
	t.Helper()
	out, err := exec.Command("gpg", "--batch", "--with-colons", "--list-keys").Output()
	if err != nil {
		t.Fatal(err)
	}
	var trust strings.Builder
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Split(line, ":"); len(f) > 9 && f[0] == "fpr" {
			trust.WriteString(f[9] + ":6:\n")
		}
	}
	cmd := exec.Command("gpg", "--batch", "--import-ownertrust")
	cmd.Stdin = strings.NewReader(trust.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ownertrust: %v\n%s", err, out)
	}
}

func outgoing() smtp.OutgoingMessage {
	m := smtp.OutgoingMessage{
		From: "Ann <ann@example.test>", To: []string{"Bob <bob@example.test>"}, Subject: "plans",
		Body: "meet at the café at noon", HTMLBody: "<p>meet at the café at noon</p>",
	}
	m.EnsureIdentity("ann@example.test")
	return m
}

func protectAndBuild(t *testing.T, mgr *plugin.Manager, msg smtp.OutgoingMessage, sign, encrypt bool) ([]byte, error) {
	t.Helper()
	protected, err := mailapp.Protect(context.Background(), func(ctx context.Context, req plugin.PrepareRequest) (plugin.PrepareResult, error) {
		return mgr.Prepare(ctx, "pgp", req)
	}, msg, sign, encrypt)
	if err != nil {
		return nil, err
	}
	return smtp.BuildRaw(config.AccountConfig{User: "ann@example.test", From: "ann@example.test"}, protected), nil
}

func decodeWith(mgr *plugin.Manager, raw []byte) (pgpmail.View, error) {
	return pgpmail.Decode(raw, 1, func(req plugin.DecodeRequest) (plugin.DecodeResult, error) {
		return mgr.Decode(context.Background(), "pgp", req)
	})
}

func TestEndToEndEncryptedAndSigned(t *testing.T) {
	mgr := installPGPPlugin(t)
	keyring(t, "Ann <ann@example.test>", "Bob <bob@example.test>")
	trustAll(t)

	raw, err := protectAndBuild(t, mgr, outgoing(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("café")) || bytes.Contains(raw, []byte("meet at")) {
		t.Fatal("the plaintext is visible in the message that would be sent")
	}
	view, err := decodeWith(mgr, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Encrypted || view.Signature.Status != "good" || !strings.Contains(view.Message.BodyText, "meet at the café at noon") {
		t.Fatalf("decoded: %+v", view)
	}
}

func TestEndToEndSignedOnlyAndTampering(t *testing.T) {
	mgr := installPGPPlugin(t)
	keyring(t, "Ann <ann@example.test>")

	raw, err := protectAndBuild(t, mgr, outgoing(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("café")) {
		t.Error("8-bit text in a signed message can be re-encoded in transit and break the signature")
	}
	view, err := decodeWith(mgr, raw)
	if err != nil {
		t.Fatal(err)
	}
	if view.Encrypted || view.Signature.Status != "good" || !strings.Contains(view.Message.BodyText, "café") {
		t.Fatalf("decoded: %+v", view)
	}

	// The same bytes, with one word changed in transit, must read as BAD.
	tampered := bytes.Replace(raw, []byte("noon"), []byte("nine"), 1)
	if bytes.Equal(tampered, raw) {
		t.Fatal("test setup: nothing was tampered with")
	}
	bad, err := decodeWith(mgr, tampered)
	if err != nil || bad.Signature.Status != "bad" {
		t.Fatalf("tampered message: %+v %v", bad, err)
	}
}

func TestEndToEndMissingKeyStopsTheSend(t *testing.T) {
	mgr := installPGPPlugin(t)
	keyring(t, "Ann <ann@example.test>")
	trustAll(t)
	msg := outgoing()
	msg.To = []string{"nobody@example.test"}
	if _, err := protectAndBuild(t, mgr, msg, false, true); err == nil || !strings.Contains(err.Error(), "nobody@example.test") {
		t.Fatalf("err = %v", err)
	}
}
