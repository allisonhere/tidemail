package browser

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestCommand(t *testing.T) {
	const target = "https://example.com/support"
	tests := []struct {
		name       string
		goos       string
		configured string
		wantName   string
		wantArgs   []string
	}{
		{name: "linux", goos: "linux", wantName: "xdg-open", wantArgs: []string{target}},
		{name: "macOS", goos: "darwin", wantName: "open", wantArgs: []string{target}},
		{name: "Windows", goos: "windows", wantName: "rundll32", wantArgs: []string{"url.dll,FileProtocolHandler", target}},
		{name: "configured", goos: "linux", configured: "firefox", wantName: "firefox", wantArgs: []string{target}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, args, err := Command(tt.goos, tt.configured, target)
			if err != nil {
				t.Fatalf("Command() error: %v", err)
			}
			if name != tt.wantName || !reflect.DeepEqual(args, tt.wantArgs) {
				t.Fatalf("Command() = %q, %#v; want %q, %#v", name, args, tt.wantName, tt.wantArgs)
			}
		})
	}
}

func TestCommandRejectsUnsupportedPlatformAndEmptyTarget(t *testing.T) {
	if _, _, err := Command("plan9", "", "https://example.com"); err == nil {
		t.Fatal("expected unsupported platform error")
	}
	if _, _, err := Command("linux", "", "  "); err == nil {
		t.Fatal("expected empty target error")
	}
}

func TestOpenReturnsLauncherFailureWithoutLaunchingBrowser(t *testing.T) {
	wantErr := errors.New("launcher failed")
	original := startCommand
	t.Cleanup(func() { startCommand = original })
	startCommand = func(string, ...string) error { return wantErr }

	if err := Open("test-browser", "https://example.com"); !errors.Is(err, wantErr) {
		t.Fatalf("Open() error = %v; want %v", err, wantErr)
	}
}

func TestOpenLocalFileLauncher(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("mocking the Linux platform opener")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if err := OpenLocalFile("", "/tmp/email.html"); err == nil {
		t.Fatal("missing opener must fail")
	}
	launcher := filepath.Join(dir, "xdg-open")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := OpenLocalFile("", "/tmp/email.html"); err == nil {
		t.Fatal("opener exit failure must be reported")
	}
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec /bin/sleep 3\n"), 0700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := OpenLocalFile("", "/tmp/email.html"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Fatalf("waited for browser exit: %v", elapsed)
	}
}
