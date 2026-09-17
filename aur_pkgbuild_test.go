package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testSHAx86     = "0a602f190553bacfc34206c0d7c5787d22963a8a9610550de5837ca195055412"
	testSHAarm     = "59c5ed07b258a193e5c090878b9f4abbf8953798183db3761886697cbb3fdfbb"
	testSHALicense = "7ded3abde5f4be92306e0ee24c6db97b1825e4eaa1b8fd473669c521f5a409dd"
)

func renderPKGBUILD(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "PKGBUILD")
	cmd := exec.Command("bash", append([]string{"packaging/aur/render-pkgbuild.sh", "--output", out}, args...)...)
	combined, err := cmd.CombinedOutput()
	if err != nil {
		return "", string(combined), err
	}
	rendered, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("rendered PKGBUILD missing: %v\n%s", readErr, combined)
	}
	return string(rendered), string(combined), nil
}

func validRenderArgs() []string {
	return []string{
		"--version", "v1.0.16",
		"--pkgrel", "1",
		"--sha256-x86_64", testSHAx86,
		"--sha256-aarch64", testSHAarm,
		"--sha256-license", testSHALicense,
	}
}

func TestRenderPKGBUILDFillsEveryPlaceholder(t *testing.T) {
	rendered, out, err := renderPKGBUILD(t, validRenderArgs()...)
	if err != nil {
		t.Fatalf("render failed: %v\n%s", err, out)
	}

	// pacman rejects a pkgver carrying the tag's leading "v".
	if !strings.Contains(rendered, "pkgver=1.0.16") {
		t.Errorf("expected pkgver=1.0.16 in:\n%s", rendered)
	}
	for _, want := range []string{"pkgrel=1", testSHAx86, testSHAarm, testSHALicense} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered PKGBUILD missing %q", want)
		}
	}
	if strings.Contains(rendered, "@") && strings.Contains(rendered, "@PKGVER@") {
		t.Errorf("unresolved placeholder remains:\n%s", rendered)
	}
	// The download URL keeps the "v" prefix because that is the git tag.
	if !strings.Contains(rendered, "/releases/download/v$pkgver/tidemail-linux-x86_64.tar.gz") {
		t.Errorf("expected tag-prefixed download URL in:\n%s", rendered)
	}
}

// A bad value must fail loudly rather than publish a PKGBUILD that cannot build.
func TestRenderPKGBUILDRejectsInvalidInput(t *testing.T) {
	cases := map[string][]string{
		"non-numeric version": {"--version", "nightly"},
		"version with suffix": {"--version", "v1.0.16-rc1"},
		"zero pkgrel":         {"--pkgrel", "0"},
		"truncated sha":       {"--sha256-x86_64", "0a602f19"},
		"non-hex sha":         {"--sha256-aarch64", strings.Repeat("z", 64)},
	}

	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			args := append(validRenderArgs(), override...)
			_, out, err := renderPKGBUILD(t, args...)
			if err == nil {
				t.Fatalf("expected render to fail for %s, got success:\n%s", name, out)
			}
		})
	}
}

func TestRenderPKGBUILDFailsOnUnresolvedPlaceholder(t *testing.T) {
	tmp := t.TempDir()
	template := filepath.Join(tmp, "PKGBUILD.in")
	if err := os.WriteFile(template, []byte("pkgver=@PKGVER@\nextra=@NOT_SUBSTITUTED@\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	args := append(validRenderArgs(), "--template", template)
	_, out, err := renderPKGBUILD(t, args...)
	if err == nil {
		t.Fatalf("expected failure on unresolved placeholder, got:\n%s", out)
	}
	if !strings.Contains(out, "@NOT_SUBSTITUTED@") {
		t.Errorf("error should name the unresolved placeholder, got:\n%s", out)
	}
}

// makepkg is the real consumer; if it cannot parse the output, nothing else matters.
func TestRenderedPKGBUILDParsesWithMakepkg(t *testing.T) {
	if _, err := exec.LookPath("makepkg"); err != nil {
		t.Skip("makepkg not available")
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "PKGBUILD")
	args := append([]string{"packaging/aur/render-pkgbuild.sh", "--output", out}, validRenderArgs()...)
	if combined, err := exec.Command("bash", args...).CombinedOutput(); err != nil {
		t.Fatalf("render failed: %v\n%s", err, combined)
	}

	cmd := exec.Command("makepkg", "--printsrcinfo")
	cmd.Dir = dir
	srcinfo, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("makepkg --printsrcinfo rejected the PKGBUILD: %v\n%s", err, srcinfo)
	}
	for _, want := range []string{"pkgbase = tidemail-bin", "pkgver = 1.0.16", "provides = tidemail=1.0.16"} {
		if !strings.Contains(string(srcinfo), want) {
			t.Errorf(".SRCINFO missing %q:\n%s", want, srcinfo)
		}
	}
}
