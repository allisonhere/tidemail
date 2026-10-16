package pgpmail

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/plugin"
)

const cipher = "-----BEGIN PGP MESSAGE-----\r\n\r\nQUJD\r\n-----END PGP MESSAGE-----"
const sig = "-----BEGIN PGP SIGNATURE-----\r\n\r\nQUJD\r\n-----END PGP SIGNATURE-----"

func crlf(s string) []byte { return []byte(strings.ReplaceAll(s, "\n", "\r\n")) }

var pgpMIMEEncrypted = crlf(`From: a@example.com
To: b@example.com
Subject: secret
Content-Type: multipart/encrypted; protocol="application/pgp-encrypted"; boundary="BB"

--BB
Content-Type: application/pgp-encrypted

Version: 1

--BB
Content-Type: application/octet-stream; name="encrypted.asc"

` + strings.ReplaceAll(cipher, "\r\n", "\n") + `
--BB--
`)

const signedEntity = "Content-Type: text/plain; charset=utf-8\n\nhello signed world\n"

var pgpMIMESigned = crlf(`From: a@example.com
Subject: signed
Content-Type: multipart/signed; protocol="application/pgp-signature"; micalg=pgp-sha256; boundary="SS"

--SS
` + signedEntity + `--SS
Content-Type: application/pgp-signature

` + strings.ReplaceAll(sig, "\r\n", "\n") + `
--SS--
`)

func TestFindEncrypted(t *testing.T) {
	p := Find(pgpMIMEEncrypted)
	if p == nil || p.Kind != KindEncrypted || !bytes.Contains(p.Ciphertext, []byte("BEGIN PGP MESSAGE")) {
		t.Fatalf("got %+v", p)
	}
}

func TestFindSignedKeepsExactBytes(t *testing.T) {
	p := Find(pgpMIMESigned)
	if p == nil || p.Kind != KindSigned {
		t.Fatalf("got %+v", p)
	}
	// The signed entity is exactly the first part, without the line break
	// that belongs to the delimiter.
	if want := string(crlf(strings.TrimSuffix(signedEntity, "\n"))); string(p.Entity) != want {
		t.Errorf("entity = %q, want %q", p.Entity, want)
	}
	if !bytes.HasPrefix(p.Signature, []byte("-----BEGIN PGP SIGNATURE")) {
		t.Errorf("signature = %q", p.Signature)
	}
}

func TestFindSignedNormalizesBareLF(t *testing.T) {
	lf := bytes.ReplaceAll(pgpMIMESigned, []byte("\r\n"), []byte("\n"))
	p := Find(lf)
	if p == nil || bytes.Contains(p.Entity, []byte("\n")) && !bytes.Contains(p.Entity, []byte("\r\n")) {
		t.Fatalf("got %+v", p)
	}
	if bytes.Contains(bytes.ReplaceAll(p.Entity, []byte("\r\n"), nil), []byte("\n")) {
		t.Error("entity still has a bare LF")
	}
}

func TestFindInline(t *testing.T) {
	enc := crlf("From: a@b.c\nContent-Type: text/plain\n\nhi\n\n" + strings.ReplaceAll(cipher, "\r\n", "\n") + "\n\nbye\n")
	if p := Find(enc); p == nil || p.Kind != KindInline || p.ClearSigned() {
		t.Fatalf("inline encrypted: %+v", p)
	}
	clear := crlf("Content-Type: text/plain\n\n-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA256\n\nhello\n" + strings.ReplaceAll(sig, "\r\n", "\n") + "\n")
	if p := Find(clear); p == nil || !p.ClearSigned() {
		t.Fatalf("clearsigned: %+v", p)
	}
}

func TestFindIgnoresOrdinaryMail(t *testing.T) {
	for _, raw := range []string{
		"Subject: hi\r\nContent-Type: text/plain\r\n\r\njust text\r\n",
		"Content-Type: multipart/mixed; boundary=\"x\"\r\n\r\n--x\r\nContent-Type: text/plain\r\n\r\nhi\r\n--x--\r\n",
		// a PGP/MIME envelope with the wrong number of parts is not trusted
		"Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"x\"\r\n\r\n--x\r\n\r\nonly\r\n--x--\r\n",
	} {
		if p := Find([]byte(raw)); p != nil {
			t.Errorf("Find(%q) = %+v, want nil", raw, p)
		}
	}
}

func TestDecodeEncrypted(t *testing.T) {
	inner := crlf("Content-Type: text/plain; charset=utf-8\n\nthe secret plan\n")
	var got []plugin.DecodeRequest
	v, err := Decode(pgpMIMEEncrypted, 7, func(r plugin.DecodeRequest) (plugin.DecodeResult, error) {
		got = append(got, r)
		return plugin.DecodeResult{Data: inner, Signature: plugin.Signature{Status: plugin.SignatureGood, Signer: "Ann"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != plugin.DecodeDecrypt || got[0].ID != 7 {
		t.Fatalf("requests: %+v", got)
	}
	if !v.Encrypted || v.Signature.Status != plugin.SignatureGood || v.Message.BodyText != "the secret plan" {
		t.Fatalf("view: %+v", v)
	}
}

func TestDecodeSignedAndNested(t *testing.T) {
	// PGP/MIME encrypted whose plaintext is a signed entity: decrypt, then
	// verify, and show the signed content.
	inner := crlf(`Content-Type: multipart/signed; protocol="application/pgp-signature"; boundary="II"

--II
` + signedEntity + `--II
Content-Type: application/pgp-signature

` + strings.ReplaceAll(sig, "\r\n", "\n") + `
--II--
`)
	var kinds []string
	v, err := Decode(pgpMIMEEncrypted, 1, func(r plugin.DecodeRequest) (plugin.DecodeResult, error) {
		kinds = append(kinds, r.Kind)
		if r.Kind == plugin.DecodeDecrypt {
			return plugin.DecodeResult{Data: inner, Signature: plugin.Signature{Status: plugin.SignatureNone}}, nil
		}
		return plugin.DecodeResult{Signature: plugin.Signature{Status: plugin.SignatureBad}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(kinds, ",") != "decrypt,verify" {
		t.Errorf("calls = %v", kinds)
	}
	if v.Signature.Status != plugin.SignatureBad || v.Message.BodyText != "hello signed world" {
		t.Fatalf("view: %+v", v)
	}
}

func TestDecodeInlineAndErrors(t *testing.T) {
	enc := crlf("Content-Type: text/plain\n\n" + strings.ReplaceAll(cipher, "\r\n", "\n") + "\n")
	v, err := Decode(enc, 1, func(plugin.DecodeRequest) (plugin.DecodeResult, error) {
		return plugin.DecodeResult{Data: []byte("plain words"), Signature: plugin.Signature{Status: plugin.SignatureNone}}, nil
	})
	if err != nil || !v.Encrypted || v.Message.BodyText != "plain words" {
		t.Fatalf("inline: %+v %v", v, err)
	}

	boom := errors.New("no secret key")
	if _, err := Decode(pgpMIMEEncrypted, 1, func(plugin.DecodeRequest) (plugin.DecodeResult, error) {
		return plugin.DecodeResult{}, boom
	}); !errors.Is(err, boom) {
		t.Errorf("plugin error not passed through: %v", err)
	}
	if _, err := Decode([]byte("Subject: x\r\n\r\nhi\r\n"), 1, nil); err == nil {
		t.Error("an unprotected message should be an error")
	}
}

func TestNoteSignatureKeepsTheWorstVerdict(t *testing.T) {
	v := View{Signature: plugin.Signature{Status: plugin.SignatureNone}}
	v.noteSignature(plugin.Signature{Status: plugin.SignatureGood})
	v.noteSignature(plugin.Signature{Status: plugin.SignatureBad})
	v.noteSignature(plugin.Signature{Status: plugin.SignatureGood})
	v.noteSignature(plugin.Signature{Status: plugin.SignatureNone})
	if v.Signature.Status != plugin.SignatureBad {
		t.Errorf("status = %s, want bad", v.Signature.Status)
	}
}
