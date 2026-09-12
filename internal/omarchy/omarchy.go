// Package omarchy reads the palette of the currently-active Omarchy desktop
// theme so TideMail's "match-omarchy" theme can follow it.
//
// Omarchy (https://omarchy.org) stages the active theme under
// ~/.local/state/omarchy/current/theme/ and ships a resolver,
// `omarchy-theme-color`, that applies its full alias/shade cascade. We prefer
// shelling out to that resolver and fall back to parsing the theme's
// colors.toml / alacritty.toml directly when it is not on PATH.
package omarchy

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Palette is the raw, un-corrected palette read from the active Omarchy theme.
// Callers are expected to run these colors through their own contrast
// correction before use.
type Palette struct {
	Name       string // active theme slug, best-effort ("" if unknown)
	Mode       string // "dark" or "light"
	Background string
	Foreground string
	Accent     string
	Selection  string
	Muted      string
	StatusBg   string // a slightly-off-background surface for status bars
	Error      string // red
	Ok         string // green
}

// resolverTimeout bounds the `omarchy-theme-color` subprocess.
const resolverTimeout = 2 * time.Second

// roots returns the directories that may hold Omarchy's "current" state, most
// authoritative first. Current releases stage the active theme under
// ~/.local/state/omarchy, but installs predating that move keep it in
// ~/.config/omarchy and are still live, so probing only the state root finds
// nothing on those machines. OMARCHY_DIR overrides both for containers and
// deterministic tests.
func roots() []string {
	if dir := strings.TrimSpace(os.Getenv("OMARCHY_DIR")); dir != "" {
		return []string{dir}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateHome == "" {
		stateHome = filepath.Join(home, ".local", "state")
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		configHome = filepath.Join(home, ".config")
	}
	return []string{filepath.Join(stateHome, "omarchy"), filepath.Join(configHome, "omarchy")}
}

// currentThemeDir returns the first root that actually stages a theme, so an
// empty state root (Omarchy creates ~/.local/state/omarchy for unrelated
// things like toggles/) does not shadow a populated config root.
func currentThemeDir() string {
	for _, root := range roots() {
		dir := filepath.Join(root, "current", "theme")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return ""
}

func themeNamePath() string {
	for _, root := range roots() {
		p := filepath.Join(root, "current", "theme.name")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// currentThemeName reads the active theme slug, or "" if unavailable.
func currentThemeName() string {
	if p := themeNamePath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			if name := strings.TrimSpace(string(b)); name != "" {
				return name
			}
		}
	}
	// No theme.name: the symlink target's basename is the slug.
	if dir := currentThemeDir(); dir != "" {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Base(resolved)
		}
	}
	return ""
}

// CurrentSignature returns a token that changes whenever the active Omarchy
// theme changes. It is cheap enough to poll: it stats theme.name (falling back
// to colors.toml) and combines the mtime with the theme name. Empty string
// means Omarchy state could not be found.
func CurrentSignature() string {
	name := currentThemeName()
	var stamp string
	if p := themeNamePath(); p != "" {
		if fi, err := os.Stat(p); err == nil {
			stamp = strconv.FormatInt(fi.ModTime().UnixNano(), 10)
		}
	}
	if stamp == "" {
		if dir := currentThemeDir(); dir != "" {
			if fi, err := os.Stat(filepath.Join(dir, "colors.toml")); err == nil {
				stamp = strconv.FormatInt(fi.ModTime().UnixNano(), 10)
			}
		}
	}
	// Omarchy switches themes by repointing current/theme, which can leave
	// theme.name's mtime untouched, so the link target has to be part of the
	// token too or a switch goes unnoticed.
	var target string
	if dir := currentThemeDir(); dir != "" {
		target, _ = filepath.EvalSymlinks(dir)
	}
	if name == "" && stamp == "" && target == "" {
		return ""
	}
	return name + "@" + stamp + "@" + target
}

// CurrentPalette locates the active Omarchy theme and parses its palette. ok is
// false (with no error) when Omarchy is absent or nothing parseable is found —
// callers fall back to their default theme.
func CurrentPalette() (p Palette, ok bool) {
	name := currentThemeName()

	if m, mok := runResolver(); mok {
		p, ok = parsePalette(m)
	}
	if !ok {
		if dir := currentThemeDir(); dir != "" {
			if b, err := os.ReadFile(filepath.Join(dir, "colors.toml")); err == nil {
				p, ok = parsePalette(parseFlatTOML(string(b)))
			}
			if !ok {
				if b, err := os.ReadFile(filepath.Join(dir, "alacritty.toml")); err == nil {
					p, ok = parseAlacritty(string(b))
				}
			}
		}
	}
	if !ok {
		return Palette{}, false
	}

	if p.Name == "" {
		p.Name = name
	}
	if p.Mode != "light" && p.Mode != "dark" {
		p.Mode = inferMode(p.Background)
	}
	return p, true
}

// runResolver runs `omarchy-theme-color --all` and returns its key→value map.
func runResolver() (map[string]string, bool) {
	bin, err := exec.LookPath("omarchy-theme-color")
	if err != nil {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), resolverTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--all").Output()
	if err != nil {
		return nil, false
	}
	m := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		key, val, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key != "" && val != "" {
			m[key] = val
		}
	}
	if len(m) == 0 {
		return nil, false
	}
	return m, true
}

// parseFlatTOML reads the flat `key = "value"` lines Omarchy's colors.toml uses
// (no section headers) into a map, applying the minimal legacy alias set so a
// color0..15-only theme still yields semantic names.
func parseFlatTOML(s string) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
		val = tomlValue(val)
		if key != "" && val != "" {
			m[key] = val
		}
	}
	alias := func(dst, src string) {
		if m[dst] == "" && m[src] != "" {
			m[dst] = m[src]
		}
	}
	alias("background", "bg")
	alias("background", "color0")
	alias("foreground", "fg")
	alias("foreground", "color7")
	alias("red", "color1")
	alias("green", "color2")
	alias("muted", "color8")
	alias("selection", "selection_background")
	alias("selection", "color8")
	return m
}

// parsePalette pulls the fields we need out of a resolved key→value map.
// It needs at least a background and foreground to succeed.
func parsePalette(m map[string]string) (Palette, bool) {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(m[k]); v != "" {
				return v
			}
		}
		return ""
	}
	p := Palette{
		Name:       get("name", "theme_name"),
		Mode:       strings.ToLower(get("mode", "theme_type")),
		Background: get("background", "bg", "color0"),
		Foreground: get("foreground", "text", "fg", "color7"),
		Accent:     get("accent", "color4", "blue"),
		// Omarchy's generated Kitty, Alacritty and Ghostty configs all resolve
		// selection_background to the accent for semantic palettes; color8 is
		// only right for older ANSI-only ones, so it is tried first.
		Selection: get("selection", "selection_background", "color8", "accent"),
		Muted:     get("muted", "color8", "dark_foreground"),
		StatusBg:  get("lighter_background", "surface", "dark_background", "color8"),
		Error:     get("danger", "red", "color1"),
		Ok:        get("success", "green", "color2"),
	}
	if !isHex(p.Background) || !isHex(p.Foreground) {
		return Palette{}, false
	}
	return p, true
}

// parseAlacritty handles the older `[colors.*]` table layout as a last resort.
func parseAlacritty(s string) (Palette, bool) {
	var section string
	vals := map[string]string{} // "section.key" -> hex
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = tomlValue(val)
		if section != "" && key != "" && val != "" {
			vals[section+"."+key] = val
		}
	}
	p := Palette{
		Background: vals["colors.primary.background"],
		Foreground: vals["colors.primary.foreground"],
		Accent:     firstNonEmpty(vals["colors.normal.blue"], vals["colors.bright.blue"]),
		Selection:  vals["colors.selection.background"],
		Muted:      firstNonEmpty(vals["colors.bright.black"], vals["colors.normal.black"]),
		StatusBg:   firstNonEmpty(vals["colors.bright.black"], vals["colors.normal.black"]),
		Error:      firstNonEmpty(vals["colors.normal.red"], vals["colors.bright.red"]),
		Ok:         firstNonEmpty(vals["colors.normal.green"], vals["colors.bright.green"]),
	}
	if !isHex(p.Background) || !isHex(p.Foreground) {
		return Palette{}, false
	}
	return p, true
}

// inferMode mirrors Omarchy's own auto-detect: sum of the RGB bytes > 382 is
// light, else dark.
func inferMode(bgHex string) string {
	r, g, b, ok := hexBytes(bgHex)
	if !ok {
		return "dark"
	}
	if int(r)+int(g)+int(b) > 382 {
		return "light"
	}
	return "dark"
}

// tomlValue extracts a scalar from the right-hand side of a `key = value`
// line. Every Omarchy color is a quoted hex string, so a comment stripper that
// simply cuts at the first '#' erases the value itself; the quoted span is
// taken first and only an unquoted remainder is scanned for a trailing
// comment.
func tomlValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 0 && (value[0] == '"' || value[0] == '\'') {
		if end := strings.IndexByte(value[1:], value[0]); end >= 0 {
			return value[1 : end+1]
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func isHex(s string) bool {
	_, _, _, ok := hexBytes(s)
	return ok
}

// hexBytes parses #rgb or #rrggbb into 0-255 components.
func hexBytes(s string) (r, g, b uint8, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "#")
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}
