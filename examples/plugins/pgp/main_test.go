package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// These tests drive the real gpg against throwaway keyrings. They skip when
// gpg is missing. Keys have no passphrase, so no pinentry is involved.

const entity = "Content-Type: text/plain; charset=utf-8\r\n\r\nattack at dawn\r\n"

func needGPG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed")
	}
}

// keyring creates a fresh GNUPGHOME with one key per address and points gpg
// at it. The directory is short so the agent's socket path fits.
func keyring(t *testing.T, addrs ...string) string {
	t.Helper()
	needGPG(t)
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
	return home
}

func export(t *testing.T, home, addr string) []byte {
	t.Helper()
	out, err := exec.Command("gpg", "--homedir", home, "--armor", "--export", addr).Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("export %s: %v", addr, err)
	}
	return out
}

func importKey(t *testing.T, home string, key []byte) {
	t.Helper()
	cmd := exec.Command("gpg", "--homedir", home, "--batch", "--import")
	cmd.Stdin = bytes.NewReader(key)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
}

var real = execGPG{bin: "gpg"}

func TestEncryptSignRoundTrip(t *testing.T) {
	keyring(t, "Ann <ann@example.test>", "Bob <bob@example.test>")
	enc, err := prepare(real, prepareIn{
		From: "ann@example.test", Recipients: []string{"bob@example.test"},
		Entity: []byte(entity), Sign: true, Encrypt: true,
	}, options{AlwaysTrust: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(enc.Encrypted, []byte("-----BEGIN PGP MESSAGE-----")) || bytes.Contains(enc.Encrypted, []byte("attack at dawn")) {
		t.Fatalf("not an encrypted message: %q", enc.Encrypted)
	}
	got, err := decode(real, decodeIn{Kind: "decrypt", Data: enc.Encrypted})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Data) != entity {
		t.Errorf("plaintext = %q", got.Data)
	}
	if got.Signature.Status != "good" || !strings.Contains(got.Signature.Signer, "ann@example.test") || got.Signature.Fingerprint == "" {
		t.Errorf("signature = %+v", got.Signature)
	}
}

func TestEncryptWithoutSigningIsUnsigned(t *testing.T) {
	keyring(t, "Ann <ann@example.test>")
	enc, err := prepare(real, prepareIn{From: "ann@example.test", Recipients: []string{"ann@example.test"}, Entity: []byte(entity), Encrypt: true}, options{AlwaysTrust: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decode(real, decodeIn{Kind: "decrypt", Data: enc.Encrypted})
	if err != nil || got.Signature.Status != "none" || string(got.Data) != entity {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestDetachedSignAndVerify(t *testing.T) {
	keyring(t, "Ann <ann@example.test>")
	signed, err := prepare(real, prepareIn{From: "ann@example.test", Entity: []byte(entity), Sign: true}, options{})
	if err != nil {
		t.Fatal(err)
	}
	if signed.MicAlg != "pgp-sha256" && signed.MicAlg != "pgp-sha512" && signed.MicAlg != "pgp-sha384" {
		t.Errorf("micalg = %q", signed.MicAlg)
	}
	good, err := decode(real, decodeIn{Kind: "verify", Data: []byte(entity), Signature: signed.Signature})
	if err != nil || good.Signature.Status != "good" {
		t.Fatalf("verify: %+v %v", good, err)
	}
	tampered := strings.Replace(entity, "dawn", "dusk", 1)
	bad, err := decode(real, decodeIn{Kind: "verify", Data: []byte(tampered), Signature: signed.Signature})
	if err != nil || bad.Signature.Status != "bad" {
		t.Fatalf("tampered message: %+v %v", bad, err)
	}
}

func TestVerifyWithUnknownKey(t *testing.T) {
	sender := keyring(t, "Ann <ann@example.test>")
	signed, err := prepare(real, prepareIn{From: "ann@example.test", Entity: []byte(entity), Sign: true}, options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = sender
	keyring(t, "Bob <bob@example.test>") // a keyring that has never seen Ann's key
	got, err := decode(real, decodeIn{Kind: "verify", Data: []byte(entity), Signature: signed.Signature})
	if err != nil || got.Signature.Status != "unknown_key" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestClearSignedInline(t *testing.T) {
	home := keyring(t, "Ann <ann@example.test>")
	cmd := exec.Command("gpg", "--homedir", home, "--batch", "--clearsign", "--local-user", "ann@example.test")
	cmd.Stdin = strings.NewReader("hello clear world\n")
	clear, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := decode(real, decodeIn{Kind: "decrypt", Data: clear})
	if err != nil || got.Signature.Status != "good" || !strings.Contains(string(got.Data), "hello clear world") {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestMissingRecipientKeyFailsClosed(t *testing.T) {
	keyring(t, "Ann <ann@example.test>")
	_, err := prepare(real, prepareIn{
		From: "ann@example.test", Recipients: []string{"nobody@example.test"}, Entity: []byte(entity), Encrypt: true,
	}, options{AlwaysTrust: true})
	ce, ok := err.(*codedError)
	if !ok || ce.code != "no_public_key" || !strings.Contains(ce.msg, "nobody@example.test") {
		t.Fatalf("err = %v", err)
	}
}

func TestUntrustedRecipientNeedsTheTrustSetting(t *testing.T) {
	home := keyring(t, "Ann <ann@example.test>")
	other := keyring(t, "Bob <bob@example.test>")
	bobKey := export(t, other, "bob@example.test")
	t.Setenv("GNUPGHOME", home)
	importKey(t, home, bobKey)

	in := prepareIn{From: "ann@example.test", Recipients: []string{"bob@example.test"}, Entity: []byte(entity), Encrypt: true}
	if _, err := prepare(real, in, options{}); err == nil {
		t.Fatal("gpg's own trust rules should refuse an untrusted key in batch mode")
	}
	if _, err := prepare(real, in, options{AlwaysTrust: true}); err != nil {
		t.Fatalf("with trust=always: %v", err)
	}
}

func TestDecryptWithoutTheSecretKey(t *testing.T) {
	home := keyring(t, "Ann <ann@example.test>")
	enc, err := prepare(real, prepareIn{From: "ann@example.test", Recipients: []string{"ann@example.test"}, Entity: []byte(entity), Encrypt: true}, options{AlwaysTrust: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = home
	keyring(t, "Bob <bob@example.test>")
	_, err = decode(real, decodeIn{Kind: "decrypt", Data: enc.Encrypted})
	ce, ok := err.(*codedError)
	if !ok || ce.code != "no_secret_key" {
		t.Fatalf("err = %v", err)
	}
}

func TestGarbageIsNotOpenPGPData(t *testing.T) {
	keyring(t, "Ann <ann@example.test>")
	_, err := decode(real, decodeIn{Kind: "decrypt", Data: []byte("not pgp at all")})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestProtocol(t *testing.T) {
	call := func(in string) response {
		var out, errOut bytes.Buffer
		if code := run(strings.NewReader(in), &out, &errOut, real); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut.String())
		}
		var resp response
		if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
			t.Fatalf("stdout is not one JSON value: %q", out.String())
		}
		return resp
	}
	if r := call(`{"api":1,"type":"request","request_id":"r1","method":"ping"}`); !r.OK || r.RequestID != "r1" {
		t.Errorf("ping: %+v", r)
	}
	if r := call(`{"api":1,"type":"request","request_id":"r2","method":"nope"}`); r.OK || r.Error.Code != "unsupported_method" {
		t.Errorf("unknown method: %+v", r)
	}
	if r := call(`{"api":2,"type":"request","request_id":"r3","method":"ping"}`); r.OK || r.Error.Code != "unsupported_api" {
		t.Errorf("wrong api: %+v", r)
	}
	if r := call(`{"api":1,"type":"request","request_id":"r4","method":"message.prepare","data":{"sign":false,"encrypt":false}}`); r.OK || r.Error.Code != "bad_request" {
		t.Errorf("empty prepare: %+v", r)
	}
}

func TestStatusParsingAndEscapes(t *testing.T) {
	status, rest := splitStatus("gpg: note\n[GNUPG:] GOODSIG ABCD Ann%20Lee <ann@x.test>\n[GNUPG:] VALIDSIG FPR1 2026\n")
	if rest != "gpg: note" {
		t.Errorf("rest = %q", rest)
	}
	v, ok := verdict(status)
	if !ok || v.Status != "good" || v.Signer != "Ann Lee <ann@x.test>" || v.Fingerprint != "FPR1" {
		t.Errorf("verdict = %+v", v)
	}
}

// trustAll marks every key in the current keyring ultimately trusted, as a
// user would after verifying them, so encryption does not need trust=always.
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
