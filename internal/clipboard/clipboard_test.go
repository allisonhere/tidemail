package clipboard

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestCopyFallsBackToOSC52WhenNoToolFound(t *testing.T) {
	origLook, origWriter := lookPath, osc52Writer
	t.Cleanup(func() { lookPath, osc52Writer = origLook, origWriter })

	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	var buf bytes.Buffer
	osc52Writer = &buf

	if err := Copy("hi"); err != nil {
		t.Fatalf("Copy should succeed via OSC 52 fallback, got %v", err)
	}

	out := buf.String()
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi"))
	if !strings.HasPrefix(out, want) {
		t.Fatalf("OSC 52 output = %q, want prefix %q", out, want)
	}
}

func TestReadErrorsWhenNoToolFound(t *testing.T) {
	origLook := lookPath
	t.Cleanup(func() { lookPath = origLook })
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	if _, err := Read(); err == nil {
		t.Fatal("Read should error when no clipboard tool is available")
	}
}
