package mailapp

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

func outgoing() smtp.OutgoingMessage {
	return smtp.OutgoingMessage{
		From: "Ann <ann@example.test>", To: []string{"Bob <bob@example.test>"}, CC: []string{"cy@example.test"},
		BCC: []string{"dee@example.test"}, Subject: "plans", Body: "meet at the café", HTMLBody: "<p>meet at the café</p>",
		Attachments: []smtp.Attachment{{Name: "a.txt", Data: []byte("secret attachment")}},
	}
}

const armoredMsg = "-----BEGIN PGP MESSAGE-----\n\nQUJD\n-----END PGP MESSAGE-----\n"
const armoredSig = "-----BEGIN PGP SIGNATURE-----\n\nQUJD\n-----END PGP SIGNATURE-----\n"

func TestProtectEncryptLeavesNoPlaintext(t *testing.T) {
	var got plugin.PrepareRequest
	out, err := Protect(context.Background(), func(_ context.Context, r plugin.PrepareRequest) (plugin.PrepareResult, error) {
		got = r
		return plugin.PrepareResult{Encrypted: []byte(armoredMsg)}, nil
	}, outgoing(), true, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.From != "ann@example.test" || !got.Sign || !got.Encrypt {
		t.Errorf("request = %+v", got)
	}
	// Every recipient, Bcc included, must be able to read it, and the plugin
	// gets bare addresses.
	if strings.Join(got.Recipients, ",") != "bob@example.test,cy@example.test,dee@example.test" {
		t.Errorf("recipients = %v", got.Recipients)
	}
	if out.Body != "" || out.HTMLBody != "" || len(out.Attachments) != 0 {
		t.Fatalf("plaintext survived: %+v", out)
	}
	raw := smtp.BuildRaw(configFor("ann@example.test"), out)
	for _, leak := range []string{"café", "secret attachment", "a.txt"} {
		if bytes.Contains(raw, []byte(leak)) {
			t.Errorf("built message contains %q", leak)
		}
	}
	if !bytes.Contains(raw, []byte("multipart/encrypted")) || !bytes.Contains(raw, []byte("BEGIN PGP MESSAGE")) || !bytes.Contains(raw, []byte("Subject: plans")) {
		t.Errorf("not a PGP/MIME message:\n%s", raw)
	}
}

func TestProtectSignedKeepsTheSignedBytesExact(t *testing.T) {
	var signed []byte
	out, err := Protect(context.Background(), func(_ context.Context, r plugin.PrepareRequest) (plugin.PrepareResult, error) {
		signed = r.Entity
		if len(r.Recipients) != 0 || r.Encrypt {
			t.Errorf("sign-only request: %+v", r)
		}
		return plugin.PrepareResult{Signature: []byte(armoredSig), MicAlg: "pgp-sha256"}, nil
	}, outgoing(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	raw := smtp.BuildRaw(configFor("ann@example.test"), out)
	// The first MIME part of the built message is exactly what the plugin
	// signed, and 8-bit text was made transfer-safe first.
	b := out.Protected
	start := bytes.Index(raw, []byte("--"+boundaryOf(t, raw)+"\r\n")) + len(boundaryOf(t, raw)) + 4
	if !bytes.HasPrefix(raw[start:], signed) || !bytes.HasPrefix(raw[start+len(signed):], []byte("\r\n--")) {
		t.Errorf("signed entity differs from what was sent for signing")
	}
	if !bytes.Equal(b.Entity, signed) || bytes.Contains(signed, []byte("café")) || !bytes.Contains(signed, []byte("quoted-printable")) {
		t.Errorf("entity not mail-safe: %q", signed)
	}
	if !bytes.Contains(raw, []byte("micalg=pgp-sha256")) {
		t.Error("micalg missing")
	}
}

func boundaryOf(t *testing.T, raw []byte) string {
	t.Helper()
	i := bytes.Index(raw, []byte("boundary=\""))
	if i < 0 {
		t.Fatal("no boundary")
	}
	rest := raw[i+len("boundary=\""):]
	return string(rest[:bytes.IndexByte(rest, '"')])
}

func TestProtectFailsClosed(t *testing.T) {
	boom := errors.New("no usable key for bob@example.test")
	for _, c := range []struct{ sign, encrypt bool }{{true, true}, {false, true}, {true, false}} {
		out, err := Protect(context.Background(), func(context.Context, plugin.PrepareRequest) (plugin.PrepareResult, error) {
			return plugin.PrepareResult{}, boom
		}, outgoing(), c.sign, c.encrypt)
		if !errors.Is(err, boom) {
			t.Errorf("sign=%v encrypt=%v: err = %v", c.sign, c.encrypt, err)
		}
		if out.Protected != nil {
			t.Error("a failed protect returned a protected message")
		}
	}
}

func TestProtectLeavesPlainMailAloneAndRejectsBadAddresses(t *testing.T) {
	called := false
	prep := func(context.Context, plugin.PrepareRequest) (plugin.PrepareResult, error) {
		called = true
		return plugin.PrepareResult{}, nil
	}
	if out, err := Protect(context.Background(), prep, outgoing(), false, false); err != nil || called || out.Protected != nil {
		t.Fatalf("plain: %v called=%v", err, called)
	}
	bad := outgoing()
	bad.To = []string{"not an address"}
	if _, err := Protect(context.Background(), prep, bad, false, true); err == nil || called {
		t.Fatalf("bad recipient: err=%v called=%v", err, called)
	}
}

func TestProtectedBuildIsStable(t *testing.T) {
	out, _ := Protect(context.Background(), func(context.Context, plugin.PrepareRequest) (plugin.PrepareResult, error) {
		return plugin.PrepareResult{Encrypted: []byte(armoredMsg)}, nil
	}, outgoing(), false, true)
	out.EnsureIdentity("ann@example.test")
	cfg := configFor("ann@example.test")
	if !bytes.Equal(smtp.BuildRaw(cfg, out), smtp.BuildRaw(cfg, out)) {
		t.Error("two builds differ: the Sent copy would not match the delivered message")
	}
}

func configFor(user string) config.AccountConfig { return config.AccountConfig{User: user, From: user} }
