package omarchy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePaletteFromResolverMap(t *testing.T) {
	m := map[string]string{
		"accent":     "#faa968",
		"background": "#05182e",
		"foreground": "#f6dcac",
		"selection":  "#134e5a",
		"muted":      "#2a6b78",
		"red":        "#f85525",
		"green":      "#028391",
		"mode":       "dark",
	}
	p, ok := parsePalette(m)
	if !ok {
		t.Fatal("parsePalette returned ok=false for a complete map")
	}
	if p.Background != "#05182e" || p.Foreground != "#f6dcac" || p.Accent != "#faa968" {
		t.Fatalf("unexpected palette: %+v", p)
	}
	if p.Error != "#f85525" || p.Ok != "#028391" || p.Mode != "dark" {
		t.Fatalf("unexpected palette: %+v", p)
	}
}

func TestParseFlatTOMLSemantic(t *testing.T) {
	src := `mode = "dark"

accent = "#faa968"
selection = "#134e5a"
muted = "#2a6b78"
background = "#05182e"
foreground = "#f6dcac"
red = "#f85525"
green = "#028391"
`
	p, ok := parsePalette(parseFlatTOML(src))
	if !ok {
		t.Fatal("expected ok for semantic colors.toml")
	}
	if p.Accent != "#faa968" || p.Selection != "#134e5a" || p.Muted != "#2a6b78" {
		t.Fatalf("unexpected palette: %+v", p)
	}
}

func TestParseFlatTOMLColorNumbersOnly(t *testing.T) {
	src := `foreground = "#e0e6ed"
background = "#181c22"
selection_background = "#82eeff"
color0 = "#181c22"
color1 = "#ff7b92"
color2 = "#4ecdc4"
color4 = "#6a85ff"
color8 = "#3d4455"
`
	p, ok := parsePalette(parseFlatTOML(src))
	if !ok {
		t.Fatal("expected ok for color0..15 colors.toml")
	}
	if p.Background != "#181c22" || p.Foreground != "#e0e6ed" {
		t.Fatalf("bg/fg not resolved: %+v", p)
	}
	if p.Error != "#ff7b92" || p.Ok != "#4ecdc4" {
		t.Fatalf("ansi red/green not aliased: %+v", p)
	}
	if p.Selection != "#82eeff" {
		t.Fatalf("selection not aliased from selection_background: %+v", p)
	}
	if p.Muted != "#3d4455" {
		t.Fatalf("muted not aliased from color8: %+v", p)
	}
}

func TestParseAlacritty(t *testing.T) {
	src := `[colors.primary]
background = "#05182e"
foreground = "#f6dcac"

[colors.selection]
background = "#134e5a"

[colors.normal]
black = "#05182e"
red = "#f85525"
green = "#028391"
blue = "#3f8f8a"

[colors.bright]
black = "#2a6b78"
`
	p, ok := parseAlacritty(src)
	if !ok {
		t.Fatal("expected ok for alacritty.toml")
	}
	if p.Background != "#05182e" || p.Foreground != "#f6dcac" {
		t.Fatalf("bg/fg: %+v", p)
	}
	if p.Accent != "#3f8f8a" || p.Error != "#f85525" || p.Ok != "#028391" {
		t.Fatalf("ansi mapping: %+v", p)
	}
	if p.Selection != "#134e5a" || p.Muted != "#2a6b78" {
		t.Fatalf("selection/muted: %+v", p)
	}
}

func TestInferMode(t *testing.T) {
	cases := map[string]string{
		"#05182e": "dark",
		"#000000": "dark",
		"#ffffff": "light",
		"#eff1f5": "light",
		"":        "dark",
		"garbage": "dark",
	}
	for hex, want := range cases {
		if got := inferMode(hex); got != want {
			t.Errorf("inferMode(%q) = %q, want %q", hex, got, want)
		}
	}
}

func TestParsePaletteRejectsIncomplete(t *testing.T) {
	if _, ok := parsePalette(map[string]string{"accent": "#faa968"}); ok {
		t.Error("expected ok=false when background/foreground are missing")
	}
}

func TestCurrentPaletteMissingStateReturnsNotOK(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	// Ensure the resolver binary is not discoverable.
	t.Setenv("PATH", filepath.Join(dir, "bin"))

	if _, ok := CurrentPalette(); ok {
		t.Fatal("CurrentPalette should be ok=false when no Omarchy state exists")
	}
	if sig := CurrentSignature(); sig != "" {
		t.Fatalf("CurrentSignature should be empty when no Omarchy state exists, got %q", sig)
	}
}

func TestCurrentPaletteReadsStagedColorsTOML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("PATH", filepath.Join(dir, "bin")) // no resolver on PATH

	themeDir := filepath.Join(dir, "omarchy", "current", "theme")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	colors := `mode = "light"
background = "#eff1f5"
foreground = "#4c4f69"
accent = "#1e66f5"
red = "#d20f39"
green = "#40a02b"
`
	if err := os.WriteFile(filepath.Join(themeDir, "colors.toml"), []byte(colors), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "omarchy", "current", "theme.name"), []byte("catppuccin-latte\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, ok := CurrentPalette()
	if !ok {
		t.Fatal("expected ok=true")
	}
	if p.Name != "catppuccin-latte" {
		t.Errorf("name = %q", p.Name)
	}
	if p.Mode != "light" {
		t.Errorf("mode = %q, want light", p.Mode)
	}
	if p.Background != "#eff1f5" || p.Accent != "#1e66f5" {
		t.Errorf("palette = %+v", p)
	}
	if CurrentSignature() == "" {
		t.Error("CurrentSignature should be non-empty when state exists")
	}
}

// The format Omarchy actually ships: a [palette] section header with semantic
// keys. The older tests here only covered background/foreground/red/green,
// which no installed theme uses, so a resolver that could not read a single
// real theme still passed them.
func TestParseFlatTOMLRealSemanticPalette(t *testing.T) {
	src := `[palette]
bg = "#1a1b26"
surface = "#272833"
surface_alt = "#444b6a"
text = "#a9b1d6"
muted = "#686d86"
accent = "#7aa2f7"
accent_alt = "#449dab"
danger = "#f7768e"
success = "#9ece6a"
warning = "#e0af68"
shadow = "#0d0d13"
`
	p, ok := parsePalette(parseFlatTOML(src))
	if !ok {
		t.Fatal("real [palette] colors.toml did not parse")
	}
	for _, c := range []struct{ name, got, want string }{
		{"background", p.Background, "#1a1b26"},
		{"foreground", p.Foreground, "#a9b1d6"},
		{"accent", p.Accent, "#7aa2f7"},
		{"muted", p.Muted, "#686d86"},
		{"statusbg", p.StatusBg, "#272833"},
		{"error", p.Error, "#f7768e"},
		{"ok", p.Ok, "#9ece6a"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// Every value is a hex color, so a comment stripper that cuts at the first '#'
// deletes the color itself and the whole file parses as empty.
func TestParseFlatTOMLKeepsHexPastInlineComment(t *testing.T) {
	m := parseFlatTOML("bg = \"#1a1b26\" # page background\ntext = \"#a9b1d6\"\n")
	if m["bg"] != "#1a1b26" {
		t.Fatalf("bg = %q, want #1a1b26", m["bg"])
	}
}

// ~/.local/state/omarchy exists on machines whose themes live in
// ~/.config/omarchy (Omarchy puts toggles/ there regardless), so the state
// root must not shadow a populated config root.
func TestCurrentPaletteFallsBackToConfigRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", filepath.Join(home, "bin")) // no resolver on PATH

	// An empty-but-present state root, as Omarchy leaves it.
	if err := os.MkdirAll(filepath.Join(home, ".local", "state", "omarchy", "toggles"), 0o755); err != nil {
		t.Fatal(err)
	}
	themeDir := filepath.Join(home, ".config", "omarchy", "current", "theme")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[palette]\nbg = \"#1a1b26\"\nsurface = \"#272833\"\ntext = \"#a9b1d6\"\naccent = \"#7aa2f7\"\n"
	if err := os.WriteFile(filepath.Join(themeDir, "colors.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	p, ok := CurrentPalette()
	if !ok {
		t.Fatal("config-root theme not found while an empty state root exists")
	}
	if p.Background != "#1a1b26" || p.Foreground != "#a9b1d6" {
		t.Fatalf("palette = %+v", p)
	}
	if CurrentSignature() == "" {
		t.Fatal("signature empty for a resolvable config-root theme")
	}
}
