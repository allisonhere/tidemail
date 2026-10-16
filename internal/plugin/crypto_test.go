package plugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const (
	rawPermission     = "[permissions]\nmessage_raw = true\n"
	composePermission = "[permissions]\nmessage_compose = true\n"
	armoredMessage    = "-----BEGIN PGP MESSAGE-----\nabc\n-----END PGP MESSAGE-----\n"
	armoredSignature  = "-----BEGIN PGP SIGNATURE-----\nabc\n-----END PGP SIGNATURE-----\n"
)

// helperCrypto answers message.decode and message.prepare with the given
// data, after leaving a marker file to prove it ran.
func helperCrypto(data string) func() {
	return func() {
		req := readRequest()
		_ = os.WriteFile(markerFile, nil, 0o644)
		respond(req, json.RawMessage(data))
	}
}

func init() {
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	helpers["tidemail-plugin-decodegood"] = helperCrypto(`{"data":"aGVsbG8=","signature":{"status":"good","signer":"Ann <ann@example.com>","fingerprint":"ABCD"}}`)
	helpers["tidemail-plugin-decodebad"] = helperCrypto(`{"signature":{"status":"trusted-by-me"}}`)
	helpers["tidemail-plugin-decodectl"] = helperCrypto(`{"signature":{"status":"good","signer":"Ann\u001b[31m"}}`)
	helpers["tidemail-plugin-verifydata"] = helperCrypto(`{"data":"aGk=","signature":{"status":"good"}}`)
	helpers["tidemail-plugin-prepenc"] = helperCrypto(`{"encrypted":` + q(btoa(armoredMessage)) + `}`)
	helpers["tidemail-plugin-prepsig"] = helperCrypto(`{"signature":` + q(btoa(armoredSignature)) + `,"micalg":"pgp-sha256"}`)
	helpers["tidemail-plugin-prepsigbadalg"] = helperCrypto(`{"signature":` + q(btoa(armoredSignature)) + `,"micalg":"sha256; evil"}`)
	helpers["tidemail-plugin-prepplain"] = helperCrypto(`{"encrypted":"cGxhaW50ZXh0"}`)
}

func btoa(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func cryptoManager(t *testing.T, command string, perms ...string) *Manager {
	t.Helper()
	root := t.TempDir()
	installPlugin(t, root, "p", "p", command, perms...)
	m, err := Discover(root)
	if err != nil || len(m.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, m.Errors())
	}
	return m
}

func ranMarker(t *testing.T, m *Manager) bool {
	t.Helper()
	p, _ := m.Plugin("p")
	_, err := os.Stat(filepath.Join(p.Dir, markerFile))
	return err == nil
}

func TestDecodeAndPrepareNeedTheirPermissions(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-decodegood", metadataPermission)
	_, err := m.Decode(context.Background(), "p", DecodeRequest{Kind: DecodeDecrypt, Data: []byte("x")})
	if !errors.Is(err, ErrPermissionDenied) || ranMarker(t, m) {
		t.Fatalf("decode without message_raw: err=%v ran=%v", err, ranMarker(t, m))
	}
	m = cryptoManager(t, "tidemail-plugin-prepenc", rawPermission)
	_, err = m.Prepare(context.Background(), "p", PrepareRequest{Entity: []byte("x"), Encrypt: true, Recipients: []string{"a@b.c"}})
	if !errors.Is(err, ErrPermissionDenied) || ranMarker(t, m) {
		t.Fatalf("prepare with only message_raw: err=%v ran=%v", err, ranMarker(t, m))
	}
}

func TestGenericCallRefusesCryptoMethods(t *testing.T) {
	m := cryptoManager(t, "tidemail-plugin-decodegood", rawPermission)
	for _, method := range []string{MethodMessageDecode, MethodMessagePrepare} {
		if _, err := m.Call(context.Background(), "p", method, nil); err == nil || ranMarker(t, m) {
			t.Errorf("Call(%s) should be refused before starting the plugin (err=%v)", method, err)
		}
	}
}

func TestDecodeValidatesReply(t *testing.T) {
	ctx := context.Background()
	m := cryptoManager(t, "tidemail-plugin-decodegood", rawPermission)
	res, err := m.Decode(ctx, "p", DecodeRequest{ID: 1, Kind: DecodeDecrypt, Data: []byte("x")})
	if err != nil || string(res.Data) != "hello" || res.Signature.Status != SignatureGood || res.Signature.Signer != "Ann <ann@example.com>" {
		t.Fatalf("good decode: %+v %v", res, err)
	}
	for _, c := range []struct{ cmd, kind string }{
		{"tidemail-plugin-decodebad", DecodeDecrypt},  // unknown status
		{"tidemail-plugin-decodectl", DecodeDecrypt},  // escape sequence in signer
		{"tidemail-plugin-verifydata", DecodeVerify},  // verify must not return data
		{"tidemail-plugin-decodegood", "decrypt-ish"}, // unknown kind
	} {
		m := cryptoManager(t, c.cmd, rawPermission)
		if _, err := m.Decode(ctx, "p", DecodeRequest{Kind: c.kind, Data: []byte("x")}); err == nil {
			t.Errorf("%s/%s: expected an error", c.cmd, c.kind)
		}
	}
}

func TestPrepareMustReturnWhatWasAskedFor(t *testing.T) {
	ctx := context.Background()
	enc := PrepareRequest{From: "me@x.y", Recipients: []string{"a@b.c"}, Entity: []byte("e"), Encrypt: true}
	sig := PrepareRequest{From: "me@x.y", Entity: []byte("e"), Sign: true}

	if res, err := cryptoManager(t, "tidemail-plugin-prepenc", composePermission).Prepare(ctx, "p", enc); err != nil || len(res.Encrypted) == 0 {
		t.Fatalf("encrypt: %+v %v", res, err)
	}
	if res, err := cryptoManager(t, "tidemail-plugin-prepsig", composePermission).Prepare(ctx, "p", sig); err != nil || res.MicAlg != "pgp-sha256" {
		t.Fatalf("sign: %+v %v", res, err)
	}
	// A plugin that returns plaintext, a signature when encryption was asked
	// for, or a hostile micalg must fail: nothing unprotected goes out.
	for _, c := range []struct {
		cmd string
		req PrepareRequest
	}{
		{"tidemail-plugin-prepplain", enc},
		{"tidemail-plugin-prepsig", enc},
		{"tidemail-plugin-prepenc", sig},
		{"tidemail-plugin-prepsigbadalg", sig},
	} {
		if _, err := cryptoManager(t, c.cmd, composePermission).Prepare(ctx, "p", c.req); err == nil {
			t.Errorf("%s: expected an error", c.cmd)
		}
	}
	m := cryptoManager(t, "tidemail-plugin-prepenc", composePermission)
	if _, err := m.Prepare(ctx, "p", PrepareRequest{Entity: []byte("e")}); err == nil {
		t.Error("prepare with neither sign nor encrypt should fail")
	}
	if _, err := m.Prepare(ctx, "p", PrepareRequest{Entity: []byte("e"), Encrypt: true}); err == nil {
		t.Error("encrypt without recipients should fail")
	}
}

func TestManifestTimeout(t *testing.T) {
	for _, bad := range []string{"timeout = -1", "timeout = 121"} {
		if _, err := ParseManifest([]byte("id = \"p\"\nname = \"P\"\napi = 1\ncommand = \"p\"\n" + bad + "\n")); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
	m, err := ParseManifest([]byte("id = \"p\"\nname = \"P\"\napi = 1\ncommand = \"p\"\ntimeout = 60\n"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{}
	if got := mgr.cryptoTimeout(Plugin{Manifest: m}).Seconds(); got != 60 {
		t.Errorf("crypto timeout = %v, want 60", got)
	}
	if got := mgr.cryptoTimeout(Plugin{}); got != DefaultTimeout {
		t.Errorf("default crypto timeout = %v", got)
	}
}
