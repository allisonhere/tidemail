package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Cryptography methods. They carry raw message parts and so need the
// message_raw and message_compose permissions; TideMail owns all MIME parsing
// and assembly, and a plugin only performs the cryptographic step on the
// bytes it is given. Both go through the permission-checked Manager entry
// points below, never through Call.
const (
	// MethodMessageDecode asks a plugin to decrypt and/or verify one part of
	// a received message (a DecodeRequest). Needs message_raw.
	MethodMessageDecode = "message.decode"
	// MethodMessagePrepare asks a plugin to sign and/or encrypt one outgoing
	// MIME entity (a PrepareRequest). Needs message_compose.
	MethodMessagePrepare = "message.prepare"
)

// MaxCryptoTimeoutSeconds caps the manifest timeout. Decode and prepare can
// wait on a passphrase or smartcard prompt, so they may exceed
// DefaultTimeout.
const MaxCryptoTimeoutSeconds = 120

// Decode kinds.
const (
	// DecodeDecrypt: Data is an ASCII-armored encrypted or clear-signed
	// message. The result carries the plaintext and any signature verdict.
	DecodeDecrypt = "decrypt"
	// DecodeVerify: Data is a signed MIME entity and Signature its detached
	// ASCII-armored signature. The result carries only the verdict.
	DecodeVerify = "verify"
)

// Signature statuses a plugin may report.
const (
	SignatureNone       = "none"
	SignatureGood       = "good"
	SignatureBad        = "bad"
	SignatureUnknownKey = "unknown_key"
	SignatureExpired    = "expired"
)

var signatureStatuses = map[string]bool{
	SignatureNone: true, SignatureGood: true, SignatureBad: true,
	SignatureUnknownKey: true, SignatureExpired: true,
}

// DecodeRequest is the data of a message.decode request. Byte fields travel
// as base64.
type DecodeRequest struct {
	// ID is TideMail's identifier for the message.
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Data      []byte `json:"data"`
	Signature []byte `json:"signature,omitempty"`
}

// Signature is a plugin's verdict on a signature.
type Signature struct {
	Status      string `json:"status"`
	Signer      string `json:"signer,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// DecodeResult is the validated data of a message.decode response. Data is
// the decrypted MIME entity (or text, for an inline message) and is empty for
// DecodeVerify.
type DecodeResult struct {
	Data      []byte    `json:"data,omitempty"`
	Signature Signature `json:"signature"`
}

// PrepareRequest is the data of a message.prepare request. Entity is the
// canonical MIME entity to protect (its Content-* headers and body, with CRLF
// line endings); Recipients are every address that must be able to read it.
type PrepareRequest struct {
	From       string   `json:"from"`
	Recipients []string `json:"recipients,omitempty"`
	Entity     []byte   `json:"entity"`
	Sign       bool     `json:"sign"`
	Encrypt    bool     `json:"encrypt"`
}

// PrepareResult is the validated data of a message.prepare response. For a
// sign-only request, Signature (detached, armored) and MicAlg are set. For an
// encrypt request, Encrypted (armored, signed inside when Sign is set) is set.
type PrepareResult struct {
	Signature []byte `json:"signature,omitempty"`
	MicAlg    string `json:"micalg,omitempty"`
	Encrypted []byte `json:"encrypted,omitempty"`
}

var micalgPattern = regexp.MustCompile(`^pgp-[a-z0-9]{3,12}$`)

var (
	armorSignature = []byte("-----BEGIN PGP SIGNATURE-----")
	armorMessage   = []byte("-----BEGIN PGP MESSAGE-----")
)

// Decode sends one received message part to a plugin that declares
// message_raw. It refuses, without starting the plugin, otherwise. The reply
// is validated: a plugin cannot return an unknown signature status or text
// that is not safe to show.
func (m *Manager) Decode(ctx context.Context, pluginID string, req DecodeRequest) (DecodeResult, error) {
	if req.Kind != DecodeDecrypt && req.Kind != DecodeVerify {
		return DecodeResult{}, fmt.Errorf("plugin %q: unknown decode kind %q", pluginID, req.Kind)
	}
	var res DecodeResult
	err := m.cryptoCall(ctx, pluginID, MethodMessageDecode, "message_raw", req, &res,
		func(p Permissions) bool { return p.MessageRaw })
	if err != nil {
		return DecodeResult{}, err
	}
	if !signatureStatuses[res.Signature.Status] {
		return DecodeResult{}, fmt.Errorf("plugin %q: unknown signature status %q", pluginID, res.Signature.Status)
	}
	if err := presentationText("signer", res.Signature.Signer, 200, false); err != nil {
		return DecodeResult{}, fmt.Errorf("plugin %q: %w", pluginID, err)
	}
	if err := presentationText("fingerprint", res.Signature.Fingerprint, 80, false); err != nil {
		return DecodeResult{}, fmt.Errorf("plugin %q: %w", pluginID, err)
	}
	if req.Kind == DecodeVerify && len(res.Data) > 0 {
		return DecodeResult{}, fmt.Errorf("plugin %q: verify returned data", pluginID)
	}
	return res, nil
}

// Prepare sends one outgoing MIME entity to a plugin that declares
// message_compose. It refuses, without starting the plugin, otherwise. The
// reply must contain exactly what the request asked for, so a plugin that
// fails to protect the message produces an error rather than an unprotected
// one.
func (m *Manager) Prepare(ctx context.Context, pluginID string, req PrepareRequest) (PrepareResult, error) {
	if !req.Sign && !req.Encrypt {
		return PrepareResult{}, fmt.Errorf("plugin %q: nothing to prepare (neither sign nor encrypt)", pluginID)
	}
	if req.Encrypt && len(req.Recipients) == 0 {
		return PrepareResult{}, fmt.Errorf("plugin %q: encrypt needs at least one recipient", pluginID)
	}
	var res PrepareResult
	err := m.cryptoCall(ctx, pluginID, MethodMessagePrepare, "message_compose", req, &res,
		func(p Permissions) bool { return p.MessageCompose })
	if err != nil {
		return PrepareResult{}, err
	}
	switch {
	case req.Encrypt:
		if !bytes.HasPrefix(bytes.TrimSpace(res.Encrypted), armorMessage) || len(res.Signature) > 0 {
			return PrepareResult{}, fmt.Errorf("plugin %q: prepare did not return an encrypted message", pluginID)
		}
	default:
		if !bytes.HasPrefix(bytes.TrimSpace(res.Signature), armorSignature) || len(res.Encrypted) > 0 {
			return PrepareResult{}, fmt.Errorf("plugin %q: prepare did not return a signature", pluginID)
		}
		if !micalgPattern.MatchString(res.MicAlg) {
			return PrepareResult{}, fmt.Errorf("plugin %q: prepare returned an invalid micalg %q", pluginID, res.MicAlg)
		}
	}
	return res, nil
}

// cryptoCall is the shared path for decode and prepare: permission check,
// per-plugin slot, the longer timeout, and decoding the response data.
func (m *Manager) cryptoCall(ctx context.Context, pluginID, method, permName string, in, out any, allowed func(Permissions) bool) error {
	p, ok := m.Plugin(pluginID)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownPlugin, pluginID)
	}
	if !allowed(p.Manifest.Permissions) {
		return fmt.Errorf("plugin %q: %w: %s is not declared", pluginID, ErrPermissionDenied, permName)
	}
	data, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("plugin %q: encode %s: %w", pluginID, method, err)
	}
	release, err := m.acquire(ctx, pluginID)
	if err != nil {
		return fmt.Errorf("plugin %q: %w", pluginID, err)
	}
	resp, err := m.callTimeout(ctx, p, method, data, m.cryptoTimeout(p))
	release()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("plugin %q: malformed %s data: %w", pluginID, method, err)
	}
	return nil
}

// cryptoTimeout is the manifest's timeout for decode and prepare, or the
// default. A Manager-wide Timeout (set by tests) wins so they stay fast.
func (m *Manager) cryptoTimeout(p Plugin) time.Duration {
	if m.Timeout > 0 || p.Manifest.Timeout <= 0 {
		return m.timeout()
	}
	return time.Duration(p.Manifest.Timeout) * time.Second
}
