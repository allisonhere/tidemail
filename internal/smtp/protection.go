package smtp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// Protection is a message that was signed and/or encrypted before it was
// queued, as RFC 3156 PGP/MIME. A plugin does the cryptography; TideMail
// assembles the MIME around the result, so the outbox holds exactly what will
// be sent and a retry never needs the plugin (or a passphrase) again.
//
// Exactly one shape is set: Encrypted for multipart/encrypted, or Entity with
// Signature and MicAlg for multipart/signed.
type Protection struct {
	// Encrypted is the ASCII-armored PGP message (signed inside when the user
	// also asked to sign).
	Encrypted []byte `json:"encrypted,omitempty"`
	// Entity is the signed MIME entity, byte for byte, without its final line
	// break (which belongs to the boundary that follows it).
	Entity    []byte `json:"entity,omitempty"`
	Signature []byte `json:"signature,omitempty"`
	MicAlg    string `json:"micalg,omitempty"`
}

// boundary is derived from the content so every build of the same message
// gives the same bytes: the copy appended to Sent must match what was sent.
func (p *Protection) boundary() string {
	sum := sha256.Sum256(append(append([]byte{}, p.Encrypted...), p.Signature...))
	return "tide-pgp-" + hex.EncodeToString(sum[:8])
}

// assemble returns the content headers and body that follow the envelope
// headers: a complete multipart/signed or multipart/encrypted entity.
func (p *Protection) assemble() []byte {
	b := p.boundary()
	var out bytes.Buffer
	if len(p.Encrypted) > 0 {
		out.WriteString("Content-Type: multipart/encrypted; protocol=\"application/pgp-encrypted\"; boundary=\"" + b + "\"\r\n\r\n")
		out.WriteString("--" + b + "\r\n")
		out.WriteString("Content-Type: application/pgp-encrypted\r\nContent-Description: PGP/MIME version identification\r\n\r\nVersion: 1\r\n\r\n")
		out.WriteString("--" + b + "\r\n")
		out.WriteString("Content-Type: application/octet-stream; name=\"encrypted.asc\"\r\nContent-Description: OpenPGP encrypted message\r\nContent-Disposition: inline; filename=\"encrypted.asc\"\r\n\r\n")
		out.Write(crlfBytes(p.Encrypted))
		out.WriteString("\r\n--" + b + "--\r\n")
		return out.Bytes()
	}
	out.WriteString("Content-Type: multipart/signed; protocol=\"application/pgp-signature\"; micalg=" + p.MicAlg + "; boundary=\"" + b + "\"\r\n\r\n")
	out.WriteString("--" + b + "\r\n")
	out.Write(p.Entity)
	out.WriteString("\r\n--" + b + "\r\n")
	out.WriteString("Content-Type: application/pgp-signature; name=\"signature.asc\"\r\nContent-Description: OpenPGP digital signature\r\nContent-Disposition: attachment; filename=\"signature.asc\"\r\n\r\n")
	out.Write(crlfBytes(p.Signature))
	out.WriteString("\r\n--" + b + "--\r\n")
	return out.Bytes()
}

// crlfBytes gives armored text CRLF line endings and no trailing line break
// (the boundary adds one).
func crlfBytes(b []byte) []byte {
	b = bytes.TrimRight(b, "\r\n")
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(b, []byte("\n"), []byte("\r\n"))
}
