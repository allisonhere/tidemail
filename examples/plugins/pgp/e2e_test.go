package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/pgpmail"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

// The test binary doubles as the plugin when TideMail's runtime starts it
// under the manifest's command name.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "tidemail-pgp" {
		main()
		return
	}
	os.Exit(m.Run())
}

// runtimeManager installs this plugin the way a user would and returns
// TideMail's real plugin manager for it.
func runtimeManager(t *testing.T) *plugin.Manager {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pgp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(dir, "tidemail-pgp")); err != nil {
		t.Fatal(err)
	}
	mgr, err := plugin.Discover(filepath.Dir(dir))
	if err != nil || len(mgr.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, mgr.Errors())
	}
	return mgr
}

func outgoing() smtp.OutgoingMessage {
	m := smtp.OutgoingMessage{
		From: "Ann <ann@example.test>", To: []string{"Bob <bob@example.test>"}, Subject: "plans",
		Body: "meet at the café at noon", HTMLBody: "<p>meet at the café at noon</p>",
	}
	m.EnsureIdentity("ann@example.test")
	return m
}

func protectAndBuild(t *testing.T, mgr *plugin.Manager, sign, encrypt bool) []byte {
	t.Helper()
	protected, err := mailapp.Protect(context.Background(), func(ctx context.Context, req plugin.PrepareRequest) (plugin.PrepareResult, error) {
		return mgr.Prepare(ctx, "pgp", req)
	}, outgoing(), sign, encrypt)
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	return smtp.BuildRaw(config.AccountConfig{User: "ann@example.test", From: "ann@example.test"}, protected)
}

func decodeWith(mgr *plugin.Manager, raw []byte) (pgpmail.View, error) {
	return pgpmail.Decode(raw, 1, func(req plugin.DecodeRequest) (plugin.DecodeResult, error) {
		return mgr.Decode(context.Background(), "pgp", req)
	})
}

func TestEndToEndEncryptedAndSigned(t *testing.T) {
	keyring(t, "Ann <ann@example.test>", "Bob <bob@example.test>")
	mgr := runtimeManager(t)
	// Settings are not stored in a test; keys here are not trusted, so use the
	// trust-model-independent path by marking them ultimately trusted.
	trustAll(t)

	raw := protectAndBuild(t, mgr, true, true)
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
	keyring(t, "Ann <ann@example.test>")
	mgr := runtimeManager(t)

	raw := protectAndBuild(t, mgr, true, false)
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
	keyring(t, "Ann <ann@example.test>")
	mgr := runtimeManager(t)
	trustAll(t)
	out := outgoing()
	out.To = []string{"nobody@example.test"}
	protected, err := mailapp.Protect(context.Background(), func(ctx context.Context, req plugin.PrepareRequest) (plugin.PrepareResult, error) {
		return mgr.Prepare(ctx, "pgp", req)
	}, out, false, true)
	if err == nil || !strings.Contains(err.Error(), "nobody@example.test") || protected.Protected != nil {
		t.Fatalf("err=%v protected=%v", err, protected.Protected)
	}
}
