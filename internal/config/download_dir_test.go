package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDownloadDir(t *testing.T) {
	home := t.TempDir()
	cfgHome := filepath.Join(home, ".config")

	writeUserDirs := func(t *testing.T, content string) {
		t.Helper()
		if err := os.MkdirAll(cfgHome, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgHome, "user-dirs.dirs"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := func(t *testing.T, xdgDownload string) {
		t.Helper()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", cfgHome)
		t.Setenv("XDG_DOWNLOAD_DIR", xdgDownload)
		t.Setenv("TIDE_TEST_DIR", "/srv/mail")
	}

	t.Run("default is ~/Downloads", func(t *testing.T) {
		env(t, "")
		if got, want := ResolveDownloadDir(""), filepath.Join(home, "Downloads"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("configured absolute path wins", func(t *testing.T) {
		env(t, "/from/env")
		if got := ResolveDownloadDir("/data/mail"); got != filepath.Clean("/data/mail") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("tilde and variables expand", func(t *testing.T) {
		env(t, "")
		if got, want := ResolveDownloadDir("~/Mail Files"), filepath.Join(home, "Mail Files"); got != want {
			t.Fatalf("tilde: got %q, want %q", got, want)
		}
		if got, want := ResolveDownloadDir("$TIDE_TEST_DIR/in"), filepath.Clean("/srv/mail/in"); got != want {
			t.Fatalf("variable: got %q, want %q", got, want)
		}
	})
	t.Run("relative path is taken from home", func(t *testing.T) {
		env(t, "")
		if got, want := ResolveDownloadDir("Documents/mail"), filepath.Join(home, "Documents", "mail"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("surrounding spaces are ignored", func(t *testing.T) {
		env(t, "")
		if got := ResolveDownloadDir("   "); got != filepath.Join(home, "Downloads") {
			t.Fatalf("blank-ish setting should use the default, got %q", got)
		}
	})
	t.Run("XDG_DOWNLOAD_DIR environment variable", func(t *testing.T) {
		env(t, "/mnt/dl")
		if got := ResolveDownloadDir(""); got != filepath.Clean("/mnt/dl") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("a relative XDG_DOWNLOAD_DIR is ignored", func(t *testing.T) {
		env(t, "dl")
		if got := ResolveDownloadDir(""); got != filepath.Join(home, "Downloads") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("user-dirs.dirs", func(t *testing.T) {
		env(t, "")
		writeUserDirs(t, "# comment\nXDG_DESKTOP_DIR=\"$HOME/Desktop\"\nXDG_DOWNLOAD_DIR=\"$HOME/Incoming\"\n")
		if got, want := ResolveDownloadDir(""), filepath.Join(home, "Incoming"); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("environment variable beats user-dirs.dirs", func(t *testing.T) {
		env(t, "/mnt/dl")
		writeUserDirs(t, "XDG_DOWNLOAD_DIR=\"$HOME/Incoming\"\n")
		if got := ResolveDownloadDir(""); got != filepath.Clean("/mnt/dl") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("setting beats everything", func(t *testing.T) {
		env(t, "/mnt/dl")
		writeUserDirs(t, "XDG_DOWNLOAD_DIR=\"$HOME/Incoming\"\n")
		if got := ResolveDownloadDir("/chosen"); got != filepath.Clean("/chosen") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("a user-dirs entry of $HOME means no downloads folder", func(t *testing.T) {
		env(t, "")
		writeUserDirs(t, "XDG_DOWNLOAD_DIR=\"$HOME/\"\n")
		if got := ResolveDownloadDir(""); got != filepath.Join(home, "Downloads") {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("no home directory and nothing set", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("XDG_DOWNLOAD_DIR", "")
		if got := ResolveDownloadDir(""); got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}
