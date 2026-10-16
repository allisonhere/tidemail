package plugincatalog

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidemail/internal/plugin"
)

// ErrNoBuild means the plugin has no download for this operating system and
// architecture.
var ErrNoBuild = errors.New("no build of this plugin for this platform")

const (
	maxArchiveBytes = 64 << 20  // compressed download
	maxUnpackedSize = 128 << 20 // all files together
	maxFiles        = 16
	stagePrefix     = ".install-" // hidden: plugin discovery skips dot-entries
)

// Install downloads entry for this platform into pluginsDir. replaceDir is the
// directory of an already-installed plugin with the same ID, which is replaced
// in place (so the ID never exists twice), or "" for a new install.
//
// Everything happens in a hidden staging directory first: the archive's
// SHA-256 must match the catalog, the files must be plain, and the manifest
// inside must be valid and agree with the catalog on ID, version and
// permissions. Only then is the staged directory moved into place, and a
// replaced copy is put back if that move fails. The plugin is never run.
func (c *Client) Install(ctx context.Context, entry Entry, pluginsDir, replaceDir string) (plugin.Plugin, error) {
	asset, ok := entry.Assets[Platform()]
	if !ok {
		return plugin.Plugin{}, fmt.Errorf("%w (%s)", ErrNoBuild, Platform())
	}
	if !c.trusted(asset.URL) {
		return plugin.Plugin{}, fmt.Errorf("refusing to download from %s", asset.URL)
	}
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return plugin.Plugin{}, err
	}
	stage, err := os.MkdirTemp(pluginsDir, stagePrefix)
	if err != nil {
		return plugin.Plugin{}, fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	archive := filepath.Join(stage, "download.tar.gz")
	if err := c.download(ctx, asset, archive); err != nil {
		return plugin.Plugin{}, err
	}
	staged := filepath.Join(stage, "plugin")
	if err := extract(archive, staged); err != nil {
		return plugin.Plugin{}, fmt.Errorf("unpack %s: %w", entry.ID, err)
	}
	if err := markExecutable(staged); err != nil {
		return plugin.Plugin{}, err
	}
	p, err := plugin.LoadPlugin(staged)
	if err != nil {
		return plugin.Plugin{}, fmt.Errorf("the downloaded plugin is not valid: %w", err)
	}
	if err := agrees(entry, p.Manifest); err != nil {
		return plugin.Plugin{}, err
	}

	target := replaceDir
	if target == "" {
		target = filepath.Join(pluginsDir, entry.ID)
		if _, err := os.Lstat(target); err == nil {
			return plugin.Plugin{}, fmt.Errorf("%s already exists and is not a plugin TideMail loaded; move it away first", target)
		}
	}
	if err := swap(staged, target, pluginsDir); err != nil {
		return plugin.Plugin{}, err
	}
	return plugin.LoadPlugin(target)
}

// agrees checks the downloaded manifest against what the user was shown.
func agrees(entry Entry, m plugin.Manifest) error {
	switch {
	case m.ID != entry.ID:
		return fmt.Errorf("the download is plugin %q, not %q", m.ID, entry.ID)
	case m.Version != entry.Version:
		return fmt.Errorf("the download is version %s, but the catalog lists %s", m.Version, entry.Version)
	case m.API != entry.API:
		return fmt.Errorf("the download speaks API %d, but the catalog lists %d", m.API, entry.API)
	case m.Permissions != entry.Permissions:
		return errors.New("the download asks for different permissions than the catalog lists; not installing it")
	}
	return nil
}

func (c *Client) download(ctx context.Context, a Asset, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "tide-plugin-install")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxArchiveBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n > maxArchiveBytes {
		return errors.New("the download is larger than a plugin should be")
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
		return fmt.Errorf("checksum mismatch: the download is %s, the catalog says %s", got, a.SHA256)
	}
	return nil
}

// extract unpacks a .tar.gz of plain files into dest. Anything but a flat set
// of regular files is refused.
func extract(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	files, total := 0, int64(0)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if name == "." {
				continue
			}
			return fmt.Errorf("unexpected directory %q", hdr.Name)
		case tar.TypeReg:
		default:
			return fmt.Errorf("%q is not a plain file", hdr.Name)
		}
		if name == "." || name == ".." || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
			return fmt.Errorf("unsafe file name %q", hdr.Name)
		}
		if files++; files > maxFiles {
			return errors.New("too many files")
		}
		if total += hdr.Size; total > maxUnpackedSize || hdr.Size < 0 {
			return errors.New("unpacked size is too large")
		}
		out, err := os.OpenFile(filepath.Join(dest, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, cerr := io.Copy(out, io.LimitReader(tr, hdr.Size))
		if err := out.Close(); cerr == nil {
			cerr = err
		}
		if cerr != nil {
			return cerr
		}
	}
	if files == 0 {
		return errors.New("the archive is empty")
	}
	return nil
}

// markExecutable makes the manifest's command runnable. Files are unpacked
// non-executable so that nothing but the one declared command can be run.
func markExecutable(dir string) error {
	m, err := plugin.LoadManifest(filepath.Join(dir, plugin.ManifestFile))
	if err != nil {
		return fmt.Errorf("the downloaded plugin is not valid: %w", err)
	}
	exe := filepath.Join(dir, m.Command)
	info, err := os.Lstat(exe)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("the downloaded plugin has no command %q", m.Command)
	}
	return os.Chmod(exe, 0o755)
}

// swap moves staged into target. An existing target is moved aside first and
// restored if the move fails, so a failed update leaves the old version.
func swap(staged, target, pluginsDir string) error {
	var backup string
	if _, err := os.Lstat(target); err == nil {
		b, err := os.MkdirTemp(pluginsDir, stagePrefix+"old-")
		if err != nil {
			return err
		}
		backup = filepath.Join(b, "previous")
		if err := os.Rename(target, backup); err != nil {
			_ = os.RemoveAll(b)
			return fmt.Errorf("move the installed copy aside: %w", err)
		}
		defer func() { _ = os.RemoveAll(b) }()
	}
	if err := os.Rename(staged, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		return fmt.Errorf("install: %w", err)
	}
	return nil
}

// Remove deletes an installed plugin's directory. dir must sit directly inside
// pluginsDir, so a bad value can never delete anything else. A symlinked
// plugin directory is unlinked, never followed.
func Remove(dir, pluginsDir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(pluginsDir)
	if err != nil {
		return err
	}
	if filepath.Dir(absDir) != absRoot || filepath.Base(absDir) == "" || strings.HasPrefix(filepath.Base(absDir), ".") {
		return fmt.Errorf("%s is not a plugin directory inside %s", dir, pluginsDir)
	}
	info, err := os.Lstat(absDir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(absDir)
	}
	return os.RemoveAll(absDir)
}
