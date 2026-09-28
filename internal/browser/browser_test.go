package browser

import (
	"errors"
	"reflect"
	"testing"
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
