package mailapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	netmail "net/mail"

	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

// Preparer asks a plugin to sign and/or encrypt one MIME entity.
type Preparer func(context.Context, plugin.PrepareRequest) (plugin.PrepareResult, error)

// Protect signs and/or encrypts msg through prepare and returns the message to
// queue: its content replaced by the protected form, with no plaintext body,
// HTML, or attachments left in it when it is encrypted.
//
// It fails closed. Any error means there is nothing to send, and the caller
// must show it and keep the compose window open; an unprotected message is
// never returned in place of a protected one.
func Protect(ctx context.Context, prepare Preparer, msg smtp.OutgoingMessage, sign, encrypt bool) (smtp.OutgoingMessage, error) {
	if !sign && !encrypt {
		return msg, nil
	}
	if msg.Protected != nil {
		return msg, errors.New("message is already protected")
	}
	from, err := bareAddress(msg.From)
	if err != nil {
		return msg, fmt.Errorf("sender: %w", err)
	}
	var recipients []string
	if encrypt {
		for _, list := range [][]string{msg.To, msg.CC, msg.BCC} {
			for _, r := range list {
				addr, err := bareAddress(r)
				if err != nil {
					return msg, fmt.Errorf("recipient %q: %w", r, err)
				}
				recipients = append(recipients, addr)
			}
		}
	}

	// Signed-only mail travels through servers that may re-encode 8-bit text,
	// which would break the signature, so its text is made transfer-safe.
	entity := smtp.BuildEntity(msg, !encrypt)
	if !encrypt {
		// The line break before the closing boundary is not part of what is
		// signed (RFC 3156), so sign without it.
		entity = bytes.TrimSuffix(entity, []byte("\r\n"))
	}
	res, err := prepare(ctx, plugin.PrepareRequest{
		From: from, Recipients: recipients, Entity: entity, Sign: sign, Encrypt: encrypt,
	})
	if err != nil {
		return msg, err
	}

	out := msg
	out.Body, out.HTMLBody, out.Attachments = "", "", nil
	if encrypt {
		out.Protected = &smtp.Protection{Encrypted: res.Encrypted}
	} else {
		out.Protected = &smtp.Protection{Entity: entity, Signature: res.Signature, MicAlg: res.MicAlg}
	}
	return out, nil
}

// bareAddress extracts the address from "Name <addr>" or "addr".
func bareAddress(s string) (string, error) {
	a, err := netmail.ParseAddress(s)
	if err != nil {
		return "", err
	}
	return a.Address, nil
}
