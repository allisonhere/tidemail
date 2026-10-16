package pgpmail

import (
	"errors"
	"fmt"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// DecodeFunc asks a plugin to decrypt or verify one part.
type DecodeFunc func(plugin.DecodeRequest) (plugin.DecodeResult, error)

// maxDepth bounds nested protection (for example signed inside encrypted).
const maxDepth = 3

// View is a decoded message, held in memory for display. It is never stored.
type View struct {
	// Encrypted says the content was decrypted.
	Encrypted bool
	// Signature is the verdict on the message's signature, or status none.
	Signature plugin.Signature
	// Message carries the displayable body fields (text, HTML, attachments).
	Message db.Message
}

// Decode decrypts and verifies everything protected in raw, one layer at a
// time, using decode for the cryptography. A plugin error is returned as is,
// so the caller can show the plugin's own message. A message with nothing
// protected is an error: callers should check Find first.
func Decode(raw []byte, id int64, decode DecodeFunc) (View, error) {
	view := View{Signature: plugin.Signature{Status: plugin.SignatureNone}}
	cur := raw
	protected := false
	for range maxDepth {
		p := Find(cur)
		if p == nil {
			break
		}
		protected = true
		switch p.Kind {
		case KindEncrypted:
			res, err := decode(plugin.DecodeRequest{ID: id, Kind: plugin.DecodeDecrypt, Data: p.Ciphertext})
			if err != nil {
				return View{}, err
			}
			view.Encrypted = true
			view.noteSignature(res.Signature)
			cur = res.Data
		case KindSigned:
			res, err := decode(plugin.DecodeRequest{ID: id, Kind: plugin.DecodeVerify, Data: p.Entity, Signature: p.Signature})
			if err != nil {
				return View{}, err
			}
			view.noteSignature(res.Signature)
			cur = p.Entity
		case KindInline:
			res, err := decode(plugin.DecodeRequest{ID: id, Kind: plugin.DecodeDecrypt, Data: p.Ciphertext})
			if err != nil {
				return View{}, err
			}
			view.Encrypted = !p.ClearSigned()
			view.noteSignature(res.Signature)
			view.Message = db.Message{BodyText: imap.DecodeBody(textEntity(res.Data)).BodyText}
			return view, nil
		}
	}
	if !protected {
		return View{}, errors.New("this message has no PGP-protected part")
	}
	if Find(cur) != nil {
		return View{}, fmt.Errorf("message is protected more than %d layers deep", maxDepth)
	}
	view.Message = imap.DecodeBody(cur)
	return view, nil
}

// noteSignature keeps the most informative verdict seen so far: any real
// verdict replaces "none", and a bad one is never replaced by a good one.
func (v *View) noteSignature(s plugin.Signature) {
	switch {
	case s.Status == plugin.SignatureNone:
	case v.Signature.Status == plugin.SignatureNone, s.Status == plugin.SignatureBad:
		v.Signature = s
	}
}

// textEntity wraps decrypted inline text so the body parser treats it as
// plain text.
func textEntity(text []byte) []byte {
	return append([]byte("Content-Type: text/plain; charset=utf-8\r\n\r\n"), text...)
}
