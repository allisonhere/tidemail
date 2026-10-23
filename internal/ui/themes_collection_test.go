package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
)

// legacyThemeNames are the names released versions accepted. A saved
// config.toml refers to a theme by name, so each must keep resolving to itself.
var legacyThemeNames = []string{
	"catppuccin-mocha", "catppuccin-latte", "catppuccin-frappe", "catppuccin-macchiato",
	"nord", "dracula", "gruvbox-dark", "gruvbox-light", "tokyo-night", "tokyo-night-day",
	"rose-pine", "rose-pine-moon", "rose-pine-dawn", "one-dark", "magenta-geode",
	"coral-sunset", "lavender-fields-forever", "vt100", "vt52",
}

func TestSavedThemeNamesKeepResolving(t *testing.T) {
	for _, name := range append(legacyThemeNames, ThemeNameMatchOmarchy) {
		th, idx := ThemeByName(name)
		if th.Name != name {
			t.Errorf("saved theme %q now resolves to %q", name, th.Name)
		}
		if got := pickableThemeNameAt(idx); got != name {
			t.Errorf("%q: index %d maps back to %q", name, idx, got)
		}
	}
	for _, th := range BuiltinThemes {
		merged, idx := MergedThemeFromConfig(config.Config{Theme: th.Name, Display: config.DefaultConfig().Display})
		if merged.Name != th.Name || BuiltinThemes[idx].Name != th.Name {
			t.Errorf("config theme %q resolved to %q (index %d)", th.Name, merged.Name, idx)
		}
	}
	if th, idx := ThemeByName("a-theme-from-the-future"); idx != 0 || th.Name != "catppuccin-mocha" {
		t.Errorf("unknown theme = %q at %d, want the default", th.Name, idx)
	}
	if got := config.DefaultConfig().Theme; got != BuiltinThemes[0].Name {
		t.Errorf("default config theme %q is not the first built-in %q", got, BuiltinThemes[0].Name)
	}
}

// A config.toml written by an older version names a theme and nothing else
// about it; loading must keep that choice.
func TestOldConfigThemeLoads(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "tidemail"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rose-pine-moon", "gruvbox-light", "vt100", "tokyo-night-storm"} {
		body := "theme = \"" + name + "\"\n"
		if err := os.WriteFile(filepath.Join(dir, "tidemail", "config.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if th, _ := MergedThemeFromConfig(cfg); th.Name != name {
			t.Errorf("config theme %q loaded as %q", name, th.Name)
		}
	}
}

func TestVTThemesKeepTideMailAccent(t *testing.T) {
	if VT100.BorderFocus != "#116611" || VT100.Selected != "#116611" || VT100.OverlayBorder != "#116611" {
		t.Errorf("vt100 accent = %s/%s/%s, want the dimmed #116611", VT100.BorderFocus, VT100.Selected, VT100.OverlayBorder)
	}
	if VT52.BorderFocus != "#664400" {
		t.Errorf("vt52 accent = %s, want the dimmed #664400", VT52.BorderFocus)
	}
	for _, name := range []string{ThemeNameVT100, ThemeNameVT52} {
		th, _ := ThemeByName(name)
		want := VT100
		if name == ThemeNameVT52 {
			want = VT52
		}
		if th.BorderFocus != want.BorderFocus {
			t.Errorf("%s in BuiltinThemes has accent %s, want %s", name, th.BorderFocus, want.BorderFocus)
		}
	}
}

// Every theme must give each semantic role a color that reads where TideMail
// uses it: links, quoted text, search matches, success text, the cursor and tag
// pills. Upstream palettes are copied exactly, so the accessors and chrome do
// the lightness adjustment this checks.
func TestSemanticRolesReadOnEveryTheme(t *testing.T) {
	for _, th := range BuiltinThemes {
		merged := ApplyDisplayOverrides(th, config.DefaultConfig())
		styles := BuildStyles(merged, "compact", "square")
		chrome := newManagerChrome(60, merged, false)

		check := func(role string, fg, bg lipgloss.Color, min float64) {
			t.Helper()
			if got := contrastRatio(fg, bg); got < min {
				t.Errorf("%s: %s %s on %s = %.2f:1, want >= %.1f", th.Name, role, fg, bg, got, min)
			}
		}
		check("link", messageLinkColor(merged), merged.Bg, 4.5)
		check("quote", messageQuoteColor(merged), merged.Bg, 4.5)
		check("warning (star)", starColor(merged), merged.Bg, 3)
		check("success", chrome.successFg, chrome.baseBg, 4.5)
		check("success on status band", chrome.successFg, chrome.statusBg, 4.5)
		check("cursor", chrome.cursor, chrome.baseBg, 3)
		check("search match text", styleColor(styles.SearchMatch.GetForeground()), styleColor(styles.SearchMatch.GetBackground()), 4.5)
		check("search match against page", styleColor(styles.SearchMatch.GetBackground()), merged.Bg, 3)

		for _, key := range []string{tagKeyReply, tagKeyUrgent, tagKeyImportant, tagKeyCategory} {
			tc := resolveTagColor(key, nil, merged)
			check("tag "+key, tc.fg, tc.bg, 4.5)
		}
	}
}
