package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gpg runs GnuPG. It is an interface only so the protocol handling can be
// exercised without a keyring; the real implementation is execGPG.
type gpg interface {
	// run executes gpg with args, feeding stdin, and returns stdout, the
	// "[GNUPG:]" status lines, the rest of stderr, and the exit error if any.
	run(args []string, stdin []byte) (stdout []byte, status []statusLine, stderr string, err error)
}

type execGPG struct{ bin string }

// commonArgs keep gpg non-interactive on the terminal (a passphrase comes
// from pinentry, never from TideMail's screen) and stop it reaching out to
// key servers on its own, which would leak whose keys are being looked up.
var commonArgs = []string{
	"--batch", "--no-tty", "--status-fd", "2",
	"--no-auto-key-retrieve", "--no-auto-key-locate",
}

func (e execGPG) run(args []string, stdin []byte) ([]byte, []statusLine, string, error) {
	cmd := exec.Command(e.bin, append(append([]string{}, commonArgs...), args...)...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errBuf
	// English messages, so the few human strings checked below are stable.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANGUAGE=C")
	err := cmd.Run()
	status, rest := splitStatus(errBuf.String())
	return out.Bytes(), status, rest, err
}

// statusLine is one "[GNUPG:] KEYWORD args…" line.
type statusLine struct {
	Keyword string
	Args    []string
}

func splitStatus(stderr string) (status []statusLine, rest string) {
	var other []string
	for _, line := range strings.Split(stderr, "\n") {
		if after, ok := strings.CutPrefix(line, "[GNUPG:] "); ok {
			f := strings.Fields(after)
			if len(f) > 0 {
				status = append(status, statusLine{Keyword: f[0], Args: f[1:]})
			}
			continue
		}
		if strings.TrimSpace(line) != "" {
			other = append(other, strings.TrimSpace(line))
		}
	}
	return status, strings.Join(other, "; ")
}

func find(status []statusLine, keyword string) (statusLine, bool) {
	for _, s := range status {
		if s.Keyword == keyword {
			return s, true
		}
	}
	return statusLine{}, false
}

type codedError struct{ code, msg string }

func (e *codedError) Error() string { return e.msg }

func coded(code, format string, args ...any) error {
	return &codedError{code: code, msg: fmt.Sprintf(format, args...)}
}

type options struct{ AlwaysTrust bool }

// ── decode ───────────────────────────────────────────────────────────────

type decodeIn struct {
	Kind      string `json:"kind"`
	Data      []byte `json:"data"`
	Signature []byte `json:"signature"`
}

type signatureOut struct {
	Status      string `json:"status"`
	Signer      string `json:"signer,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type decodeOut struct {
	Data      []byte       `json:"data,omitempty"`
	Signature signatureOut `json:"signature"`
}

func decode(g gpg, in decodeIn) (decodeOut, error) {
	switch in.Kind {
	case "decrypt":
		out, status, rest, err := g.run([]string{"--decrypt"}, in.Data)
		if _, failed := find(status, "DECRYPTION_FAILED"); failed {
			return decodeOut{}, decryptError(status, rest)
		}
		sig, hadSig := verdict(status)
		if err != nil && len(out) == 0 && !hadSig {
			return decodeOut{}, gpgError(status, rest, err)
		}
		return decodeOut{Data: out, Signature: sig}, nil
	case "verify":
		dir, err := os.MkdirTemp("", "tidemail-pgp-")
		if err != nil {
			return decodeOut{}, err
		}
		defer func() { _ = os.RemoveAll(dir) }()
		sigFile := filepath.Join(dir, "sig.asc")
		if err := os.WriteFile(sigFile, in.Signature, 0o600); err != nil {
			return decodeOut{}, err
		}
		_, status, rest, runErr := g.run([]string{"--verify", sigFile, "-"}, in.Data)
		sig, hadSig := verdict(status)
		if !hadSig {
			return decodeOut{}, gpgError(status, rest, runErr)
		}
		return decodeOut{Signature: sig}, nil
	}
	return decodeOut{}, coded("bad_request", "unknown decode kind %q", in.Kind)
}

// verdict turns gpg's signature status lines into a TideMail verdict. The
// second result is false when gpg saw no signature at all.
func verdict(status []statusLine) (signatureOut, bool) {
	sig := signatureOut{Status: "none"}
	if fpr, ok := find(status, "VALIDSIG"); ok && len(fpr.Args) > 0 {
		sig.Fingerprint = fpr.Args[0]
	}
	for _, kw := range []struct{ keyword, status string }{
		{"BADSIG", "bad"},
		{"EXPKEYSIG", "expired"},
		{"EXPSIG", "expired"},
		{"REVKEYSIG", "bad"},
		{"ERRSIG", "unknown_key"},
		{"GOODSIG", "good"},
	} {
		s, ok := find(status, kw.keyword)
		if !ok {
			continue
		}
		sig.Status = kw.status
		if kw.keyword != "ERRSIG" && len(s.Args) > 1 {
			sig.Signer = unescape(strings.Join(s.Args[1:], " "))
		}
		if kw.keyword == "ERRSIG" && len(s.Args) > 0 && sig.Fingerprint == "" {
			sig.Fingerprint = s.Args[0] // the key ID, all gpg knows
		}
		return sig, true
	}
	return sig, false
}

// unescape undoes gpg's %XX escaping in status output.
func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

func decryptError(status []statusLine, rest string) error {
	if _, ok := find(status, "NO_SECKEY"); ok {
		return coded("no_secret_key", "you don't have the secret key this message was encrypted to")
	}
	return gpgError(status, rest, errors.New("decryption failed"))
}

// gpgError describes a failed gpg run in terms a person can act on.
func gpgError(status []statusLine, rest string, err error) error {
	low := strings.ToLower(rest)
	switch {
	case strings.Contains(low, "inappropriate ioctl") || strings.Contains(low, "pinentry") ||
		strings.Contains(low, "cancelled") || strings.Contains(low, "canceled"):
		return coded("passphrase", "gpg couldn't get your passphrase (cancelled, or no graphical pinentry available): %s", firstLine(rest))
	case strings.Contains(low, "no secret key"):
		return coded("no_secret_key", "you don't have the secret key needed here")
	case strings.Contains(low, "no valid openpgp data") || hasKeyword(status, "NODATA"):
		return coded("no_data", "that doesn't contain valid OpenPGP data")
	}
	if rest == "" && err != nil {
		rest = err.Error()
	}
	return coded("gpg_failed", "gpg failed: %s", firstLine(rest))
}

func hasKeyword(status []statusLine, k string) bool { _, ok := find(status, k); return ok }

func firstLine(s string) string {
	if i := strings.Index(s, "; "); i >= 0 {
		s = s[:i]
	}
	return s
}

// ── prepare ──────────────────────────────────────────────────────────────

type prepareIn struct {
	From       string   `json:"from"`
	Recipients []string `json:"recipients"`
	Entity     []byte   `json:"entity"`
	Sign       bool     `json:"sign"`
	Encrypt    bool     `json:"encrypt"`
}

type prepareOut struct {
	Signature []byte `json:"signature,omitempty"`
	MicAlg    string `json:"micalg,omitempty"`
	Encrypted []byte `json:"encrypted,omitempty"`
}

// hashNames maps OpenPGP hash algorithm numbers to RFC 3156 micalg values.
var hashNames = map[string]string{
	"1": "pgp-md5", "2": "pgp-sha1", "3": "pgp-ripemd160",
	"8": "pgp-sha256", "9": "pgp-sha384", "10": "pgp-sha512", "11": "pgp-sha224",
}

func prepare(g gpg, in prepareIn, opts options) (prepareOut, error) {
	if !in.Sign && !in.Encrypt {
		return prepareOut{}, coded("bad_request", "nothing to do: neither sign nor encrypt")
	}
	from := strings.TrimSpace(in.From)
	if in.Sign && from == "" {
		return prepareOut{}, coded("bad_request", "signing needs the sender's address")
	}
	args := []string{"--armor"}
	if opts.AlwaysTrust {
		args = append(args, "--trust-model", "always")
	}
	if in.Sign {
		args = append(args, "--local-user", from)
	}
	if !in.Encrypt {
		out, status, rest, err := g.run(append(args, "--detach-sign"), in.Entity)
		if err != nil || len(out) == 0 {
			return prepareOut{}, signError(status, rest, err, from)
		}
		created, ok := find(status, "SIG_CREATED")
		if !ok || len(created.Args) < 3 || hashNames[created.Args[2]] == "" {
			return prepareOut{}, coded("gpg_failed", "gpg did not report which hash it signed with")
		}
		return prepareOut{Signature: out, MicAlg: hashNames[created.Args[2]]}, nil
	}

	// Encrypt to every recipient, and to the sender so the Sent copy stays
	// readable. A recipient without a usable key stops everything: nothing is
	// ever sent unprotected.
	if from != "" {
		args = append(args, "--encrypt-to", from)
	}
	if in.Sign {
		args = append(args, "--sign")
	}
	args = append(args, "--encrypt")
	for _, r := range in.Recipients {
		args = append(args, "--recipient", r)
	}
	out, status, rest, err := g.run(args, in.Entity)
	for _, s := range status {
		if s.Keyword == "INV_RECP" && len(s.Args) > 1 {
			return prepareOut{}, coded("no_public_key", "no usable key for %s (%s)", unescape(s.Args[1]), invalidReason(s.Args[0]))
		}
	}
	if err != nil || len(out) == 0 {
		if in.Sign {
			return prepareOut{}, signError(status, rest, err, from)
		}
		return prepareOut{}, gpgError(status, rest, err)
	}
	return prepareOut{Encrypted: out}, nil
}

func signError(status []statusLine, rest string, err error, from string) error {
	if strings.Contains(strings.ToLower(rest), "no secret key") || hasKeyword(status, "INV_SGNR") {
		return coded("no_secret_key", "no secret key to sign as %s", from)
	}
	return gpgError(status, rest, err)
}

// invalidReason names the INV_RECP reason codes gpg documents.
func invalidReason(code string) string {
	switch code {
	case "1":
		return "no such key"
	case "2":
		return "ambiguous name"
	case "3":
		return "wrong key usage"
	case "4":
		return "key revoked"
	case "5":
		return "key expired"
	case "10":
		return "key not trusted: sign it, or set Key trust to always in the plugin settings"
	}
	return "unusable key"
}
