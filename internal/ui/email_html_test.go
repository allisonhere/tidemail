package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSanitizeEmailHTMLBlocksRemoteContent(t *testing.T) {
	src := `<html><head>
<link rel="stylesheet" href="https://evil.example/a.css">
<meta http-equiv="refresh" content="0;url=https://evil.example">
<style>@import url(https://evil.example/i.css); p{background:url("https://evil.example/bg.png")} b{background:url(data:image/png;base64,AAAA)}</style>
<script src="https://evil.example/x.js"></script>
</head><body onload="steal()" background="https://evil.example/b.gif">
<img src="https://evil.example/pixel.gif" width=1>
<img srcset="https://evil.example/a.png 1x" src="cid:logo">
<img src="data:image/gif;base64,R0lGOD">
<p style="background:url(//evil.example/p.png);color:red">hi</p>
<iframe src="https://evil.example"></iframe>
<a href="https://example.com/?a=1&amp;b=2">Go</a>
</body></html>`
	out, blocked, err := sanitizeEmailHTML(src)
	if err != nil {
		t.Fatal(err)
	}
	if blocked != 2 {
		t.Fatalf("blocked = %d, want 2 (pixel and srcset image)", blocked)
	}
	for _, bad := range []string{"evil.example", "onload", "<script", "<iframe", "<link", "refresh", "@import"} {
		// The CSP header names no hosts, so any hit is leaked remote content.
		if strings.Contains(out, bad) {
			t.Errorf("export still contains %q:\n%s", bad, out)
		}
	}
	for _, want := range []string{
		`<a href="https://example.com/?a=1&amp;b=2">Go</a>`,
		`src="cid:logo"`,
		`data:image/gif;base64,R0lGOD`,
		`data:image/png;base64,AAAA`,
		"color:red",
		"Content-Security-Policy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("export lost %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out[strings.Index(out, "<head>"):], `<head><meta http-equiv="Content-Security-Policy"`) {
		t.Errorf("CSP must be the first element in head:\n%s", out)
	}
}

func TestSanitizeEmailHTMLFragment(t *testing.T) {
	out, blocked, err := sanitizeEmailHTML(`<p>hello <img src="http://t.example/p.gif"></p>`)
	if err != nil {
		t.Fatal(err)
	}
	if blocked != 1 || strings.Contains(out, "t.example") || !strings.Contains(out, "Content-Security-Policy") {
		t.Fatalf("blocked=%d out=%s", blocked, out)
	}
}

func TestPruneEmailExports(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	write := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write("tidemail-email-old.html", 2*time.Hour)
	fresh := write("tidemail-email-new.html", time.Minute)
	other := write("keep-me.html", 5*time.Hour)
	pruneEmailExports(dir, now, time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("stale export was not pruned")
	}
	for _, p := range []string{fresh, other} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s should be kept: %v", p, err)
		}
	}
}

func TestOpenEmailHTMLUsesConfiguredBrowser(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	original := openEmailBrowser
	t.Cleanup(func() { openEmailBrowser = original })
	var got string
	openEmailBrowser = func(configured, _ string) error { got = configured; return nil }
	blocked, err := openEmailHTML([]byte("Content-Type: text/html\r\n\r\n<img src='https://t.example/p.gif'>"), "firefox --new-tab")
	if err != nil || got != "firefox --new-tab" || blocked != 1 {
		t.Fatalf("configured=%q blocked=%d err=%v", got, blocked, err)
	}
}

func TestEmailExportDirIsPrivate(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir, err := emailExportDir()
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, err %v", info, err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := emailExportDir(); err == nil {
		t.Fatal("world-readable export dir must be rejected")
	}
}
