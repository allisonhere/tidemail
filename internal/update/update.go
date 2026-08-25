package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// checksumsAssetName is the release asset listing the SHA-256 of every archive.
// The updater verifies a downloaded archive against it and refuses to install
// on mismatch or if it is missing, so a tampered release asset can't be
// auto-installed as arbitrary code.
const checksumsAssetName = "SHA256SUMS"

const defaultReleasesURL = "https://api.github.com/repos/allisonhere/tidemail/releases/latest"
const binaryName = "tidemail"

// SuggestedManualInstallScript is shown when an update is available but the running binary's install directory is not writable (before any download/install attempt).
const SuggestedManualInstallScript = "curl -fsSL https://raw.githubusercontent.com/allisonhere/tidemail/main/install.sh | sh"

// Updater checks GitHub releases and installs the asset matching the current platform. -allie
type Updater struct {
	ReleasesURL string
	HTTPClient  *http.Client
	GOOS        string
	GOARCH      string
}

// ReleaseInfo is the normalized release and asset metadata the UI needs after a check. -allie
type ReleaseInfo struct {
	Version      string
	PublishedAt  time.Time
	Summary      string
	Body         string
	AssetName    string
	DownloadURL  string
	ChecksumsURL string
}

// CheckResult reports whether a newer stable release is available for the running version. -allie
type CheckResult struct {
	CurrentVersion string
	Latest         ReleaseInfo
	Available      bool
}

// DownloadedAsset points at the extracted update payload staged for installation. -allie
type DownloadedAsset struct {
	Release     ReleaseInfo
	ArchivePath string
	BinaryPath  string
}

// InstallResult tells the UI whether Tide was replaced directly or needs a manual command. -allie
type InstallResult struct {
	Version         string
	ExecutablePath  string
	RequiresManual  bool
	ManualCommand   string
	Restartable     bool
	ShadowedPath    string
	ShadowedCommand string
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Body        string        `json:"body"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
}

func New() *Updater {
	return &Updater{
		ReleasesURL: defaultReleasesURL,
		HTTPClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (u *Updater) Check(currentVersion string) (CheckResult, error) {
	client := u.httpClient()
	req, err := http.NewRequest(http.MethodGet, u.releasesURL(), nil)
	if err != nil {
		return CheckResult{}, fmt.Errorf("build update check request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tide-update-check")

	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{}, fmt.Errorf("check latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return CheckResult{}, fmt.Errorf("release check failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return CheckResult{}, fmt.Errorf("decode latest release: %w", err)
	}

	assetName, err := u.assetName()
	if err != nil {
		return CheckResult{}, err
	}

	var downloadURL, checksumsURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName + ".tar.gz":
			downloadURL = asset.DownloadURL
		case checksumsAssetName:
			checksumsURL = asset.DownloadURL
		}
	}
	if downloadURL == "" {
		return CheckResult{}, fmt.Errorf("latest release %s does not have asset %s.tar.gz", release.TagName, assetName)
	}

	info := ReleaseInfo{
		Version:      strings.TrimSpace(release.TagName),
		PublishedAt:  release.PublishedAt,
		Summary:      summarizeReleaseNotes(release.Body),
		Body:         strings.TrimSpace(release.Body),
		AssetName:    assetName,
		DownloadURL:  downloadURL,
		ChecksumsURL: checksumsURL,
	}
	return CheckResult{
		CurrentVersion: currentVersion,
		Latest:         info,
		Available:      IsNewerVersion(info.Version, currentVersion),
	}, nil
}

func (u *Updater) Download(release ReleaseInfo) (DownloadedAsset, error) {
	// Integrity is enforced before anything from the archive touches disk as an
	// executable: the download host must be GitHub, and the archive's SHA-256
	// must match the signed-by-channel SHA256SUMS asset. A release missing
	// checksums is rejected rather than installed unverified.
	if !isTrustedAssetHost(release.DownloadURL) {
		return DownloadedAsset{}, fmt.Errorf("refusing to download update from untrusted host: %s", release.DownloadURL)
	}
	if release.ChecksumsURL == "" {
		return DownloadedAsset{}, fmt.Errorf("release %s has no %s asset; refusing to install unverified update", release.Version, checksumsAssetName)
	}

	client := u.httpClient()
	req, err := http.NewRequest(http.MethodGet, release.DownloadURL, nil)
	if err != nil {
		return DownloadedAsset{}, fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "tide-update-download")

	resp, err := client.Do(req)
	if err != nil {
		return DownloadedAsset{}, fmt.Errorf("download update %s: %w", release.Version, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return DownloadedAsset{}, fmt.Errorf("download failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	tmpDir, err := os.MkdirTemp("", "tide-update-*")
	if err != nil {
		return DownloadedAsset{}, fmt.Errorf("create update temp dir: %w", err)
	}

	archivePath := filepath.Join(tmpDir, release.AssetName+".tar.gz")
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return DownloadedAsset{}, fmt.Errorf("create archive file: %w", err)
	}
	if _, err := io.Copy(archiveFile, resp.Body); err != nil {
		archiveFile.Close()
		return DownloadedAsset{}, fmt.Errorf("write archive file: %w", err)
	}
	if err := archiveFile.Close(); err != nil {
		return DownloadedAsset{}, fmt.Errorf("close archive file: %w", err)
	}

	if err := u.verifyArchiveChecksum(release, archivePath); err != nil {
		_ = os.RemoveAll(tmpDir)
		return DownloadedAsset{}, err
	}

	binaryPath, err := extractTarGz(archivePath, tmpDir, release.AssetName)
	if err != nil {
		return DownloadedAsset{}, err
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return DownloadedAsset{}, fmt.Errorf("mark update binary executable: %w", err)
	}

	return DownloadedAsset{
		Release:     release,
		ArchivePath: archivePath,
		BinaryPath:  binaryPath,
	}, nil
}

// verifyArchiveChecksum downloads the release's SHA256SUMS asset and confirms
// the on-disk archive's SHA-256 matches the entry for its filename. Any
// problem — untrusted host, download failure, missing entry, or mismatch — is a
// hard error so a tampered or corrupt archive is never extracted and installed.
func (u *Updater) verifyArchiveChecksum(release ReleaseInfo, archivePath string) error {
	if !isTrustedAssetHost(release.ChecksumsURL) {
		return fmt.Errorf("refusing to fetch checksums from untrusted host: %s", release.ChecksumsURL)
	}

	client := u.httpClient()
	req, err := http.NewRequest(http.MethodGet, release.ChecksumsURL, nil)
	if err != nil {
		return fmt.Errorf("build checksums request: %w", err)
	}
	req.Header.Set("User-Agent", "tide-update-checksums")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download checksums failed: HTTP %d", resp.StatusCode)
	}

	sums := parseChecksums(io.LimitReader(resp.Body, 1<<20))
	want, ok := sums[release.AssetName+".tar.gz"]
	if !ok {
		return fmt.Errorf("%s does not list a checksum for %s.tar.gz", checksumsAssetName, release.AssetName)
	}

	got, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("hash downloaded archive: %w", err)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("update checksum mismatch for %s.tar.gz: expected %s, got %s", release.AssetName, want, got)
	}
	return nil
}

// parseChecksums reads `sha256sum`-style lines ("<hex>␠␠<name>") into a
// filename→hash map, tolerating the binary-mode "*name" marker.
func parseChecksums(r io.Reader) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		out[name] = strings.ToLower(fields[0])
	}
	return out
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isTrustedAssetHost restricts update downloads to GitHub-hosted HTTPS URLs so a
// tampered API response can't redirect the updater to an attacker's host.
func isTrustedAssetHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || host == "githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func (u *Updater) Install(asset DownloadedAsset, currentExec string) (InstallResult, error) {
	result := InstallResult{
		Version:        asset.Release.Version,
		ExecutablePath: currentExec,
	}
	if asset.BinaryPath == "" {
		return result, fmt.Errorf("downloaded update has no binary path")
	}
	if currentExec == "" {
		return result, fmt.Errorf("current executable path is empty")
	}

	targetExec, err := installTarget(currentExec, true)
	if err != nil {
		result.RequiresManual = true
		result.ManualCommand = manualInstallCommand(asset.BinaryPath, currentExec)
		return result, nil
	}
	result.ExecutablePath = targetExec

	nextPath := targetExec + ".new"
	backupPath := targetExec + ".bak"
	_ = os.Remove(nextPath)
	_ = os.Remove(backupPath)

	if err := copyExecutable(asset.BinaryPath, nextPath); err != nil {
		return result, fmt.Errorf("stage update binary: %w", err)
	}

	_, statErr := os.Stat(targetExec)
	targetExists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return result, fmt.Errorf("stat current executable: %w", statErr)
	}

	if targetExists {
		if err := os.Rename(targetExec, backupPath); err != nil {
			return result, fmt.Errorf("backup current executable: %w", err)
		}
	}

	if err := os.Rename(nextPath, targetExec); err != nil {
		if targetExists {
			_ = os.Rename(backupPath, targetExec)
		}
		return result, fmt.Errorf("replace executable: %w", err)
	}

	if targetExists {
		_ = os.Remove(backupPath)
	}
	result.Restartable = true
	if shadowedBy(currentExec, targetExec) {
		result.ShadowedPath = currentExec
		result.ShadowedCommand = removeCommand(currentExec)
	}
	return result, nil
}

func IsStableVersion(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

func IsNewerVersion(latest, current string) bool {
	if latest == "" || current == "" {
		return false
	}
	lv, lok := parseVersion(latest)
	cv, cok := parseVersion(current)
	if lok && cok {
		return compareParsedVersions(lv, cv) > 0
	}
	// If either version is not a valid semver, we cannot reliably compare,
	// so don't claim an update is available.
	return false
}

type parsedVersion struct {
	major int
	minor int
	patch int
}

func parseVersion(v string) (parsedVersion, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return parsedVersion{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return parsedVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return parsedVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return parsedVersion{}, false
	}
	return parsedVersion{major: major, minor: minor, patch: patch}, true
}

func compareParsedVersions(a, b parsedVersion) int {
	switch {
	case a.major != b.major:
		return cmp(a.major, b.major)
	case a.minor != b.minor:
		return cmp(a.minor, b.minor)
	default:
		return cmp(a.patch, b.patch)
	}
}

func cmp(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func summarizeReleaseNotes(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "New Tide release available."
	}
	for _, block := range strings.Split(body, "\n\n") {
		line := strings.Join(strings.Fields(strings.TrimSpace(block)), " ")
		if line == "" {
			continue
		}
		if len(line) > 140 {
			return line[:137] + "..."
		}
		return line
	}
	return "New Tide release available."
}

func (u *Updater) httpClient() *http.Client {
	if u != nil && u.HTTPClient != nil {
		return u.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (u *Updater) releasesURL() string {
	if u != nil && u.ReleasesURL != "" {
		return u.ReleasesURL
	}
	return defaultReleasesURL
}

func (u *Updater) assetName() (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if u != nil && u.GOOS != "" {
		goos = u.GOOS
	}
	if u != nil && u.GOARCH != "" {
		goarch = u.GOARCH
	}

	var arch string
	switch goarch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}

	switch goos {
	case "linux", "darwin":
		return "tidemail-" + goos + "-" + arch, nil
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
}

func extractTarGz(archivePath, destDir, expectedName string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("open gzip stream: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Base(hdr.Name)
		if name != expectedName {
			continue
		}

		outPath := filepath.Join(destDir, name)
		outFile, err := os.Create(outPath)
		if err != nil {
			return "", fmt.Errorf("create extracted binary: %w", err)
		}
		if _, err := io.Copy(outFile, tr); err != nil {
			outFile.Close()
			return "", fmt.Errorf("extract binary: %w", err)
		}
		if err := outFile.Close(); err != nil {
			return "", fmt.Errorf("close extracted binary: %w", err)
		}
		return outPath, nil
	}

	return "", fmt.Errorf("archive does not contain %s", expectedName)
}

// InstallDestinationWritable reports whether the directory containing the current executable allows creating files (same gate as in-place install).
func InstallDestinationWritable() (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if _, err := installTarget(exe, false); err != nil {
		return false, nil
	}
	return true, nil
}

var (
	userHomeDir = os.UserHomeDir
	dirWritable = ensureDirWritable
)

func installTarget(currentExec string, createFallbackDir bool) (string, error) {
	currentExec = strings.TrimSpace(currentExec)
	if currentExec == "" {
		return "", fmt.Errorf("current executable path is empty")
	}
	currentExec, err := filepath.Abs(currentExec)
	if err != nil {
		return "", err
	}
	if err := dirWritable(filepath.Dir(currentExec)); err == nil {
		return currentExec, nil
	}

	fallback, err := defaultUserInstallPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(fallback)
	if createFallbackDir {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	} else if _, err := os.Stat(dir); err != nil {
		if statErr := dirWritable(filepath.Dir(filepath.Dir(dir))); statErr != nil {
			return "", err
		}
		return fallback, nil
	}
	if err := dirWritable(dir); err != nil {
		return "", err
	}
	return fallback, nil
}

func defaultUserInstallPath() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return "", fmt.Errorf("home directory is empty")
	}
	return filepath.Join(home, ".local", "bin", binaryName), nil
}

func shadowedBy(candidate, target string) bool {
	candidate = strings.TrimSpace(candidate)
	target = strings.TrimSpace(target)
	if candidate == "" || target == "" {
		return false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	if candidateAbs == targetAbs {
		return false
	}
	seenCandidate := false
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		path, err := filepath.Abs(filepath.Join(dir, binaryName))
		if err != nil {
			continue
		}
		switch path {
		case targetAbs:
			return seenCandidate
		case candidateAbs:
			seenCandidate = true
		}
	}
	return false
}

func ensureDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".tide-update-write-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(name)
		return closeErr
	}
	return os.Remove(name)
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o755)
}

func manualInstallCommand(binaryPath, target string) string {
	return fmt.Sprintf("sudo install -m 0755 %q %q", binaryPath, target)
}

func removeCommand(path string) string {
	return fmt.Sprintf("sudo rm -f %q", path)
}
