package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInstallScriptDefaultsToUserLocalBinWithoutSudo(t *testing.T) {
	tmp := t.TempDir()
	archive := writeInstallTestArchive(t, tmp, "tidemail test-version", 0)
	bin := filepath.Join(tmp, "fakebin")
	writeInstallTestTools(t, bin, archive, "")

	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"PATH="+installTestPath(bin),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}

	installed := filepath.Join(tmp, ".local", "bin", "tidemail")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("expected install at %s: %v\n%s", installed, err, out)
	}
	if strings.Contains(string(out), "sudo") || strings.Contains(string(out), "/usr/local/bin") {
		t.Fatalf("expected user-local install without sudo, got output:\n%s", out)
	}
}

func TestInstallScriptRemovesWritableStaleBinaryEarlierOnPath(t *testing.T) {
	tmp := t.TempDir()
	archive := writeInstallTestArchive(t, tmp, "tidemail test-version", 0)
	fakeBin := filepath.Join(tmp, "fakebin")
	staleBin := filepath.Join(tmp, "oldbin")
	writeInstallTestTools(t, fakeBin, archive, "")
	if err := os.MkdirAll(staleBin, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(staleBin, "tidemail")
	if err := os.WriteFile(stale, []byte("#!/bin/sh\necho stale\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"PATH="+installTestPath(fakeBin, staleBin, filepath.Join(tmp, ".local", "bin")),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale binary removed, stat err=%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Removed stale "+stale) {
		t.Fatalf("expected stale binary removal message, got:\n%s", out)
	}
}

func TestInstallScriptLeavesExistingBinaryWhenDownloadedBinaryFailsVersionCheck(t *testing.T) {
	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(installDir, "tidemail")
	if err := os.WriteFile(installed, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	archive := writeInstallTestArchive(t, tmp, "broken", 42)
	fakeBin := filepath.Join(tmp, "fakebin")
	writeInstallTestTools(t, fakeBin, archive, "")

	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"INSTALL_DIR="+installDir,
		"PATH="+installTestPath(fakeBin),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install.sh to fail version check\n%s", out)
	}
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old binary" {
		t.Fatalf("expected existing binary preserved, got %q\n%s", got, out)
	}
	assertNoInstallTemps(t, installDir)
}

func TestInstallScriptFailsClearlyWithoutHomeOrInstallDir(t *testing.T) {
	tmp := t.TempDir()
	archive := writeInstallTestArchive(t, tmp, "tidemail test-version", 0)
	fakeBin := filepath.Join(tmp, "fakebin")
	writeInstallTestTools(t, fakeBin, archive, "")

	cmd := exec.Command("sh", "install.sh")
	cmd.Env = []string{
		"PATH=" + fakeBin + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install.sh to fail without HOME or INSTALL_DIR\n%s", out)
	}
	if !strings.Contains(string(out), "No install directory selected") {
		t.Fatalf("expected clear install-dir error, got:\n%s", out)
	}
}

func TestInstallScriptCleansStagedBinaryWhenInstallVerificationFails(t *testing.T) {
	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(installDir, "tidemail")
	if err := os.WriteFile(installed, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	archive := writeInstallTestArchive(t, tmp, "tidemail test-version", 0)
	fakeInstall := "#!/bin/sh\ndst=''\nfor arg do dst=\"$arg\"; done\ncat > \"$dst\" <<'SCRIPT'\n#!/bin/sh\nexit 42\nSCRIPT\nchmod 755 \"$dst\"\n"
	fakeBin := filepath.Join(tmp, "fakebin")
	writeInstallTestTools(t, fakeBin, archive, fakeInstall)

	cmd := exec.Command("sh", "install.sh")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"INSTALL_DIR="+installDir,
		"PATH="+installTestPath(fakeBin),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install.sh to fail staged verification\n%s", out)
	}
	got, err := os.ReadFile(installed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old binary" {
		t.Fatalf("expected existing binary preserved, got %q\n%s", got, out)
	}
	assertNoInstallTemps(t, installDir)
}

func writeInstallTestArchive(t *testing.T, dir, version string, exitCode int) string {
	t.Helper()
	path := filepath.Join(dir, "tidemail-linux-x86_64.tar.gz")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then echo " + shellQuote(version) + "; exit " + strconv.Itoa(exitCode) + "; fi\n"
	if err := tw.WriteHeader(&tar.Header{Name: "tidemail-linux-x86_64", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeInstallTestTools(t *testing.T, dir, archive, installScript string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(dir, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo Linux;; -m) echo x86_64;; *) exit 1;; esac\n")
	writeExecutable(t, filepath.Join(dir, "curl"), "#!/bin/sh\nout=''\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = '-o' ]; then shift; out=\"$1\"; fi\n  shift || break\ndone\ncp "+shellQuote(archive)+" \"$out\"\n")
	writeExecutable(t, filepath.Join(dir, "sudo"), "#!/bin/sh\necho 'sudo should not be called' >&2\nexit 99\n")
	if installScript != "" {
		writeExecutable(t, filepath.Join(dir, "install"), installScript)
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertNoInstallTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".tidemail.tmp.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		t.Fatalf("expected no staged install files left behind, found %v", matches)
	}
}

func installTestPath(dirs ...string) string {
	parts := append([]string{}, dirs...)
	parts = append(parts, "/usr/bin", "/bin")
	return strings.Join(parts, string(os.PathListSeparator))
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
