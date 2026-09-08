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
