package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckResolvesMatchingAsset(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://example.com/releases/latest" {
			t.Fatalf("unexpected URL: %s", req.URL)
		}
		return responseWithBody(http.StatusOK, `{
			"tag_name":"v1.2.3",
			"body":"Faster updates.\n\nMore detail.",
			"published_at":"2026-03-30T12:00:00Z",
			"assets":[
				{"name":"tidemail-linux-x86_64.tar.gz","browser_download_url":"https://example.com/download"},
				{"name":"tidemail-darwin-aarch64.tar.gz","browser_download_url":"https://example.com/other"},
				{"name":"SHA256SUMS","browser_download_url":"https://example.com/sums"}
			]
		}`), nil
	})}
	updater := &Updater{
		ReleasesURL: "https://example.com/releases/latest",
		HTTPClient:  client,
		GOOS:        "linux",
		GOARCH:      "amd64",
	}

	result, err := updater.Check("v1.2.2")
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !result.Available {
		t.Fatal("expected update to be available")
	}
	if result.Latest.AssetName != "tidemail-linux-x86_64" {
		t.Fatalf("unexpected asset name: %q", result.Latest.AssetName)
	}
	if result.Latest.DownloadURL != "https://example.com/download" {
		t.Fatalf("unexpected download URL: %q", result.Latest.DownloadURL)
	}
	if result.Latest.ChecksumsURL != "https://example.com/sums" {
		t.Fatalf("unexpected checksums URL: %q", result.Latest.ChecksumsURL)
	}
	if result.Latest.Summary != "Faster updates." {
		t.Fatalf("unexpected summary: %q", result.Latest.Summary)
	}
	if result.Latest.PublishedAt.IsZero() {
		t.Fatal("expected published time to be populated")
	}
}

// archiveServer returns an HTTP client that serves the archive at the GitHub
// download URL and a SHA256SUMS body at the checksums URL. sums lets a test
// inject a wrong/absent checksum to exercise the fail-closed paths.
func archiveServer(archive []byte, sums string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://github.com/o/r/releases/download/archive.tar.gz":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/gzip"}},
				Body:       io.NopCloser(bytes.NewReader(archive)),
			}, nil
		case "https://github.com/o/r/releases/download/SHA256SUMS":
			return responseWithBody(http.StatusOK, sums), nil
		default:
			return responseWithBody(http.StatusNotFound, "not found"), nil
		}
	})}
}

const (
	testDownloadURL  = "https://github.com/o/r/releases/download/archive.tar.gz"
	testChecksumsURL = "https://github.com/o/r/releases/download/SHA256SUMS"
)

func TestDownloadExtractsBinary(t *testing.T) {
	archive := testArchive(t, "tidemail-linux-x86_64", "#!/bin/sh\necho tide\n")
	sums := sha256Hex(archive) + "  tidemail-linux-x86_64.tar.gz\n"
	updater := &Updater{HTTPClient: archiveServer(archive, sums)}
	asset, err := updater.Download(ReleaseInfo{
		Version:      "v1.2.3",
		AssetName:    "tidemail-linux-x86_64",
		DownloadURL:  testDownloadURL,
		ChecksumsURL: testChecksumsURL,
	})
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}

	data, err := os.ReadFile(asset.BinaryPath)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if !strings.Contains(string(data), "echo tide") {
		t.Fatalf("unexpected binary content: %q", string(data))
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	archive := testArchive(t, "tidemail-linux-x86_64", "real")
	// Checksum of different content — must not install.
	sums := sha256Hex([]byte("tampered")) + "  tidemail-linux-x86_64.tar.gz\n"
	updater := &Updater{HTTPClient: archiveServer(archive, sums)}
	_, err := updater.Download(ReleaseInfo{
		Version:      "v1.2.3",
		AssetName:    "tidemail-linux-x86_64",
		DownloadURL:  testDownloadURL,
		ChecksumsURL: testChecksumsURL,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got: %v", err)
	}
}

func TestDownloadRejectsMissingChecksumsAsset(t *testing.T) {
	archive := testArchive(t, "tidemail-linux-x86_64", "real")
	updater := &Updater{HTTPClient: archiveServer(archive, "")}
	_, err := updater.Download(ReleaseInfo{
		Version:     "v1.2.3",
		AssetName:   "tidemail-linux-x86_64",
		DownloadURL: testDownloadURL,
		// ChecksumsURL deliberately empty.
	})
	if err == nil || !strings.Contains(err.Error(), "refusing to install unverified") {
		t.Fatalf("expected unverified-update refusal, got: %v", err)
	}
}

func TestDownloadRejectsUntrustedHost(t *testing.T) {
	archive := testArchive(t, "tidemail-linux-x86_64", "real")
	updater := &Updater{HTTPClient: archiveServer(archive, "")}
	_, err := updater.Download(ReleaseInfo{
		Version:      "v1.2.3",
		AssetName:    "tidemail-linux-x86_64",
		DownloadURL:  "https://evil.example.com/payload.tar.gz",
		ChecksumsURL: testChecksumsURL,
	})
	if err == nil || !strings.Contains(err.Error(), "untrusted host") {
		t.Fatalf("expected untrusted-host refusal, got: %v", err)
	}
}

func TestIsTrustedAssetHost(t *testing.T) {
	trusted := []string{
		"https://github.com/o/r/releases/download/x.tar.gz",
		"https://objects.githubusercontent.com/abc",
		"https://release-assets.githubusercontent.com/abc",
	}
	for _, u := range trusted {
		if !TrustedAssetHost(u) {
			t.Errorf("expected %q to be trusted", u)
		}
	}
	untrusted := []string{
		"http://github.com/o/r/x.tar.gz", // not https
		"https://github.com.evil.com/x",  // suffix trick
		"https://evilgithub.com/x",       // not github
		"https://raw.githubusercontent.com.evil.com/x",
		"not a url",
		"",
	}
	for _, u := range untrusted {
		if TrustedAssetHost(u) {
			t.Errorf("expected %q to be untrusted", u)
		}
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestInstallReplacesExistingBinary(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "tide")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatalf("write current binary: %v", err)
	}
	newBinary := filepath.Join(dir, "downloaded")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	updater := New()
	result, err := updater.Install(DownloadedAsset{
		Release:    ReleaseInfo{Version: "v1.2.3"},
		BinaryPath: newBinary,
	}, current)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if !result.Restartable {
		t.Fatal("expected install to be restartable")
	}
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("expected current binary to be replaced, got %q", string(data))
	}
}

func TestInstallReturnsManualCommandWhenTargetDirNotWritable(t *testing.T) {
	dir := t.TempDir()
	protectedDir := filepath.Join(dir, "protected")
	if err := os.Mkdir(protectedDir, 0o755); err != nil {
		t.Fatalf("create protected dir: %v", err)
	}
	if err := os.Chmod(protectedDir, 0o555); err != nil {
		t.Fatalf("chmod protected dir: %v", err)
	}
	defer os.Chmod(protectedDir, 0o755) //nolint:errcheck

	newBinary := filepath.Join(dir, "downloaded")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	oldHome := userHomeDir
	oldWritable := dirWritable
	defer func() {
		userHomeDir = oldHome
		dirWritable = oldWritable
	}()
	userHomeDir = func() (string, error) { return "", os.ErrNotExist }
	dirWritable = func(string) error { return os.ErrPermission }

	target := filepath.Join(protectedDir, "tide")
	result, err := New().Install(DownloadedAsset{
		Release:    ReleaseInfo{Version: "v1.2.3"},
		BinaryPath: newBinary,
	}, target)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if !result.RequiresManual {
		t.Fatal("expected install to require manual step")
	}
	if !strings.Contains(result.ManualCommand, "sudo install -m 0755") {
		t.Fatalf("unexpected manual command: %q", result.ManualCommand)
	}
}

func TestInstallFallsBackToUserLocalBinWhenCurrentDirNotWritable(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	currentDir := filepath.Join(dir, "system-bin")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("create current dir: %v", err)
	}
	current := filepath.Join(currentDir, "tidemail")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatalf("write current binary: %v", err)
	}
	newBinary := filepath.Join(dir, "downloaded")
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatalf("write new binary: %v", err)
	}

	oldHome := userHomeDir
	oldWritable := dirWritable
	defer func() {
		userHomeDir = oldHome
		dirWritable = oldWritable
	}()
	userHomeDir = func() (string, error) { return home, nil }
	dirWritable = func(path string) error {
		if path == currentDir {
			return os.ErrPermission
		}
		return ensureDirWritable(path)
	}
	t.Setenv("PATH", currentDir+string(os.PathListSeparator)+filepath.Join(home, ".local", "bin"))

	result, err := New().Install(DownloadedAsset{
		Release:    ReleaseInfo{Version: "v1.2.3"},
		BinaryPath: newBinary,
	}, current)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	target := filepath.Join(home, ".local", "bin", "tidemail")
	if result.RequiresManual {
		t.Fatal("expected writable user-local fallback instead of manual install")
	}
	if result.ExecutablePath != target {
		t.Fatalf("ExecutablePath = %q, want %q", result.ExecutablePath, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read fallback binary: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("fallback binary = %q, want new", string(data))
	}
	oldData, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read original binary: %v", err)
	}
	if string(oldData) != "old" {
		t.Fatalf("original binary changed: %q", string(oldData))
	}
	if result.ShadowedPath != current {
		t.Fatalf("ShadowedPath = %q, want %q", result.ShadowedPath, current)
	}
	if result.ShadowedCommand == "" || !strings.Contains(result.ShadowedCommand, current) {
		t.Fatalf("ShadowedCommand = %q, want command for %q", result.ShadowedCommand, current)
	}
}

func TestInstallTargetCheckDoesNotCreateUserLocalBin(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	currentDir := filepath.Join(dir, "system-bin")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatalf("create current dir: %v", err)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home dir: %v", err)
	}

	oldHome := userHomeDir
	oldWritable := dirWritable
	defer func() {
		userHomeDir = oldHome
		dirWritable = oldWritable
	}()
	userHomeDir = func() (string, error) { return home, nil }
	dirWritable = func(path string) error {
		if path == currentDir {
			return os.ErrPermission
		}
		return ensureDirWritable(path)
	}

	target, err := installTarget(filepath.Join(currentDir, "tidemail"), false)
	if err != nil {
		t.Fatalf("installTarget returned error: %v", err)
	}
	want := filepath.Join(home, ".local", "bin", "tidemail")
	if target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); !os.IsNotExist(err) {
		t.Fatalf("expected install target check not to create %s, stat err=%v", filepath.Dir(want), err)
	}
}

func TestIsStableVersion(t *testing.T) {
	if !IsStableVersion("v1.2.3") {
		t.Fatal("expected v1.2.3 to be stable")
	}
	if IsStableVersion("dev") {
		t.Fatal("expected dev to be unstable")
	}
}

func testArchive(t *testing.T, name, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name:    name,
		Mode:    0o755,
		Size:    int64(len(content)),
		ModTime: time.Unix(1710000000, 0),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("write tar content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestSuggestedManualInstallScriptNonEmpty(t *testing.T) {
	if SuggestedManualInstallScript == "" {
		t.Fatal("SuggestedManualInstallScript is empty")
	}
}

func TestInstallDestinationWritableNoError(t *testing.T) {
	_, err := InstallDestinationWritable()
	if err != nil {
		t.Fatalf("InstallDestinationWritable: %v", err)
	}
}

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func responseWithBody(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// slowArchiveClient serves the release through a real local HTTP server so the
// client's deadlines apply, sending the archive in chunks with gap between them.
func slowArchiveClient(t *testing.T, timeout time.Duration, archive []byte, sums string, chunks int, gap time.Duration) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "SHA256SUMS") {
			_, _ = io.WriteString(w, sums)
			return
		}
		size := (len(archive) + chunks - 1) / chunks
		for i := 0; i < len(archive); i += size {
			_, _ = w.Write(archive[i:min(i+size, len(archive))])
			w.(http.Flusher).Flush()
			select {
			case <-time.After(gap):
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Timeout: timeout, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = "http", target.Host
		return http.DefaultTransport.RoundTrip(req)
	})}
}

// A download slower than the client's request timeout must still finish
// while data keeps arriving.
func TestDownloadOutlastsTheRequestTimeout(t *testing.T) {
	archive := testArchive(t, "tidemail-linux-x86_64", strings.Repeat("payload ", 4096))
	sums := sha256Hex(archive) + "  tidemail-linux-x86_64.tar.gz\n"
	updater := &Updater{HTTPClient: slowArchiveClient(t, 100*time.Millisecond, archive, sums, 5, 60*time.Millisecond)}
	if _, err := updater.Download(ReleaseInfo{Version: "v1.2.3", AssetName: "tidemail-linux-x86_64", DownloadURL: testDownloadURL, ChecksumsURL: testChecksumsURL}); err != nil {
		t.Fatalf("a slow but steady download failed: %v", err)
	}
}

func TestDownloadGivesUpWhenDataStops(t *testing.T) {
	previous := downloadStallTimeout
	downloadStallTimeout = 100 * time.Millisecond
	t.Cleanup(func() { downloadStallTimeout = previous })
	archive := testArchive(t, "tidemail-linux-x86_64", strings.Repeat("payload ", 4096))
	sums := sha256Hex(archive) + "  tidemail-linux-x86_64.tar.gz\n"
	updater := &Updater{HTTPClient: slowArchiveClient(t, 0, archive, sums, 2, time.Second)}
	_, err := updater.Download(ReleaseInfo{Version: "v1.2.3", AssetName: "tidemail-linux-x86_64", DownloadURL: testDownloadURL, ChecksumsURL: testChecksumsURL})
	if !errors.Is(err, errDownloadStalled) {
		t.Fatalf("a stalled download must fail as stalled, got %v", err)
	}
}

func stagedDirs(t *testing.T, tmp string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(tmp, "tide-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	return dirs
}

// A failed download leaves no staging directory behind.
func TestDownloadCleansUpWhenItFails(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	missing := testArchive(t, "something-else", "x")
	sums := sha256Hex(missing) + "  tidemail-linux-x86_64.tar.gz\n"
	for name, client := range map[string]*http.Client{
		"checksum mismatch": archiveServer(missing, "deadbeef  tidemail-linux-x86_64.tar.gz\n"),
		"binary missing":    archiveServer(missing, sums),
	} {
		_, err := (&Updater{HTTPClient: client}).Download(ReleaseInfo{Version: "v1.2.3", AssetName: "tidemail-linux-x86_64", DownloadURL: testDownloadURL, ChecksumsURL: testChecksumsURL})
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		if dirs := stagedDirs(t, tmp); len(dirs) != 0 {
			t.Fatalf("%s left %v behind", name, dirs)
		}
	}
}

// Install removes the staged download once the binary is in place, but keeps
// it when the user must run a manual command that copies from it.
func TestInstallRemovesTheStagedDownload(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	archive := testArchive(t, "tidemail-linux-x86_64", "new")
	sums := sha256Hex(archive) + "  tidemail-linux-x86_64.tar.gz\n"
	download := func() DownloadedAsset {
		asset, err := (&Updater{HTTPClient: archiveServer(archive, sums)}).Download(ReleaseInfo{Version: "v1.2.3", AssetName: "tidemail-linux-x86_64", DownloadURL: testDownloadURL, ChecksumsURL: testChecksumsURL})
		if err != nil || asset.TempDir == "" {
			t.Fatalf("download: %v (dir %q)", err, asset.TempDir)
		}
		return asset
	}

	current := filepath.Join(t.TempDir(), "tide")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	asset := download()
	if _, err := New().Install(asset, current); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(asset.TempDir); !os.IsNotExist(err) {
		t.Fatalf("staged download survived a successful install: %v", err)
	}

	oldHome, oldWritable := userHomeDir, dirWritable
	t.Cleanup(func() { userHomeDir, dirWritable = oldHome, oldWritable })
	userHomeDir = func() (string, error) { return "", os.ErrNotExist }
	dirWritable = func(string) error { return os.ErrPermission }
	asset = download()
	result, err := New().Install(asset, current)
	if err != nil || !result.RequiresManual {
		t.Fatalf("expected a manual install: %+v %v", result, err)
	}
	if _, err := os.Stat(asset.BinaryPath); err != nil {
		t.Fatalf("the manual command needs the staged binary: %v", err)
	}
}
