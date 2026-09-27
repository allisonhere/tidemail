package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// DefaultTimeout bounds one plugin invocation, from launch to exit.
const DefaultTimeout = 5 * time.Second

const (
	// maxResponseBytes caps what TideMail reads from plugin stdout.
	maxResponseBytes = 1 << 20
	// maxStderrBytes caps the plugin stderr kept for diagnostics.
	maxStderrBytes = 16 << 10
	// maxStderrInError caps how much stderr is quoted in an error message.
	maxStderrInError = 512
	// waitDelay is how long Wait keeps waiting for the output pipes to close
	// after the plugin is killed, in case a child process still holds them.
	waitDelay = time.Second
)

// envAllowlist is the only environment a plugin inherits. Anything else,
// including TIDEMAIL_* client secrets and provider API keys, stays in
// TideMail's process.
var envAllowlist = []string{
	"PATH", "HOME", "USER", "LANG", "LC_ALL", "LC_CTYPE", "LC_MESSAGES", "TMPDIR", "TZ",
}

func pluginEnv() []string {
	var env []string
	for _, key := range envAllowlist {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// invoke runs the plugin once: it writes req to stdin, waits for the process
// to exit, and parses stdout as the response. The executable runs directly,
// never through a shell.
//
// secrets are the plugin's own secret settings, keyed by setting key. They go
// into this process's environment only, and any copy of them in the plugin's
// stdout or stderr is masked before TideMail parses or displays it.
func invoke(ctx context.Context, p Plugin, req Request, timeout time.Duration, secrets map[string]string) (Response, error) {
	if err := req.validate(); err != nil {
		return Response{}, fmt.Errorf("plugin %q: %w", p.Manifest.ID, err)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("plugin %q: encode request: %w", p.Manifest.ID, err)
	}
	payload = append(payload, '\n')

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout := &cappedBuffer{max: maxResponseBytes}
	stderr := &cappedBuffer{max: maxStderrBytes}
	cmd := exec.CommandContext(ctx, p.Executable)
	cmd.Dir = p.Dir
	cmd.Env = pluginEnv()
	for key, value := range secrets {
		cmd.Env = append(cmd.Env, SecretEnvVar(key)+"="+value)
	}
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = waitDelay
	configureProcessGroup(cmd)

	runErr := cmd.Run()
	stdout.buf = redactSecrets(stdout.buf, secrets)
	stderr.buf = redactSecrets(stderr.buf, secrets)
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return Response{}, fmt.Errorf("plugin %q timed out after %s%s", p.Manifest.ID, timeout, stderrSuffix(stderr))
		}
		return Response{}, fmt.Errorf("plugin %q: %w", p.Manifest.ID, ctxErr)
	}
	if runErr != nil {
		return Response{}, fmt.Errorf("plugin %q failed: %w%s", p.Manifest.ID, runErr, stderrSuffix(stderr))
	}
	if stdout.truncated {
		return Response{}, fmt.Errorf("plugin %q: response exceeds %d bytes", p.Manifest.ID, maxResponseBytes)
	}
	resp, err := decodeResponse(stdout.buf, req)
	if err != nil {
		return Response{}, fmt.Errorf("plugin %q: %w", p.Manifest.ID, err)
	}
	return resp, nil
}

// minRedactLen skips masking very short secrets, which would otherwise mask
// ordinary text.
const minRedactLen = 6

// redactSecrets replaces every copy of a secret value with asterisks.
func redactSecrets(b []byte, secrets map[string]string) []byte {
	for _, v := range secrets {
		if len(v) >= minRedactLen {
			b = bytes.ReplaceAll(b, []byte(v), []byte("********"))
		}
	}
	return b
}

// cappedBuffer keeps the first max bytes written and discards the rest. It
// never returns a write error, so a chatty plugin is not killed by a broken
// pipe; the caller checks truncated instead.
type cappedBuffer struct {
	buf       []byte
	max       int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	room := b.max - len(b.buf)
	if len(p) > room {
		b.buf = append(b.buf, p[:max(room, 0)]...)
		b.truncated = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// stderrSuffix formats plugin stderr for an error message. It is untrusted
// text headed for a terminal UI, so control characters (including escape
// sequences) are dropped and the length is capped.
func stderrSuffix(b *cappedBuffer) string {
	text := strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, string(b.buf))
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) > maxStderrInError {
		text = strings.ToValidUTF8(text[:maxStderrInError], "") + "…"
	}
	return ": stderr: " + text
}
