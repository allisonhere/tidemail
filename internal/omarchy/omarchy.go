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

// stateDir returns ~/.local/state/omarchy, honoring XDG_STATE_HOME.
func stateDir() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "omarchy")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "omarchy")
}

func currentThemeDir() string {
	d := stateDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "current", "theme")
}

func themeNamePath() string {
	d := stateDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "current", "theme.name")
}

// currentThemeName reads the active theme slug, or "" if unavailable.
func currentThemeName() string {
	p := themeNamePath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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
	if name == "" && stamp == "" {
		return ""
	}
	return name + "@" + stamp
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
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		// Drop a trailing inline comment on unquoted values.
		if i := strings.IndexByte(val, '#'); i > 0 && !strings.HasPrefix(val, "#") {
			val = strings.TrimSpace(val[:i])
		}
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
		Foreground: get("foreground", "fg", "color7"),
		Accent:     get("accent", "color4", "blue"),
		Selection:  get("selection", "selection_background", "color8"),
		Muted:      get("muted", "color8", "dark_foreground"),
		StatusBg:   get("lighter_background", "dark_background", "color8"),
		Error:      get("red", "color1"),
		Ok:         get("green", "color2"),
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
		val = strings.Trim(strings.TrimSpace(val), `"'`)
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
