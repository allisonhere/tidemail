package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ResolveDownloadDir returns the folder that saving attachments, logs and
// plugin reports starts in, or "" when no home directory can be found.
//
// In order: the folder the user chose in Settings (a leading ~ and $VARIABLES
// are expanded; a relative path is taken from the home directory), the
// XDG_DOWNLOAD_DIR environment variable, the XDG_DOWNLOAD_DIR line in
// user-dirs.dirs (where desktops keep it, and what `xdg-user-dir DOWNLOAD`
// reads), and finally ~/Downloads. The folder need not exist yet.
func ResolveDownloadDir(configured string) string {
	home, _ := os.UserHomeDir()
	if dir := expandUserPath(configured, home); dir != "" {
		return dir
	}
	if dir := absoluteDir(os.Getenv("XDG_DOWNLOAD_DIR")); dir != "" {
		return dir
	}
	if dir := downloadDirFromUserDirs(home); dir != "" {
		return dir
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

// expandUserPath turns what a person typed into an absolute path, or "" when
// they typed nothing.
func expandUserPath(p, home string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home == "" {
			return ""
		}
		p = filepath.Join(home, p[1:])
	}
	p = os.ExpandEnv(p)
	if !filepath.IsAbs(p) {
		if home == "" {
			return ""
		}
		p = filepath.Join(home, p)
	}
	return filepath.Clean(p)
}

// absoluteDir accepts only an absolute path, as the XDG spec requires.
func absoluteDir(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

// downloadDirFromUserDirs reads XDG_DOWNLOAD_DIR from user-dirs.dirs, a shell
// fragment of lines such as XDG_DOWNLOAD_DIR="$HOME/Downloads".
func downloadDirFromUserDirs(home string) string {
	cfgDir := os.Getenv("XDG_CONFIG_HOME")
	if cfgDir == "" {
		if home == "" {
			return ""
		}
		cfgDir = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(cfgDir, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	defer f.Close()

	const key = "XDG_DOWNLOAD_DIR="
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, key) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, key))
		value = strings.Trim(value, `"'`)
		value = strings.ReplaceAll(value, "${HOME}", home)
		value = strings.ReplaceAll(value, "$HOME", home)
		dir := absoluteDir(value)
		// $HOME itself is how a desktop says "no downloads folder".
		if dir == filepath.Clean(home) {
			return ""
		}
		return dir
	}
	return ""
}
