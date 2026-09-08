package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/omarchy"
)

// hostilePalettes are deliberately bad desktop palettes: colors that, taken
// literally, would produce an unreadable TideMail UI. contrastCorrectTheme must
// pull every one of them up to TideMail's contrast floors.
var hostilePalettes = map[string]omarchy.Palette{
	"near-black accent on black": {
		Mode: "dark", Background: "#000000", Foreground: "#050505",
		Accent: "#0a0a0a", Selection: "#111111", Muted: "#0d0d0d",
		StatusBg: "#000000", Error: "#1a0000", Ok: "#001a00",
	},
	"grey on grey": {
		Mode: "dark", Background: "#4a4a4a", Foreground: "#525252",
		Accent: "#555555", Selection: "#4f4f4f", Muted: "#505050",
		StatusBg: "#4b4b4b", Error: "#5a4a4a", Ok: "#4a5a4a",
	},
	"washed-out light": {
		Mode: "light", Background: "#fdfdfd", Foreground: "#efefef",
		Accent: "#f2f2f2", Selection: "#f5f5f5", Muted: "#f0f0f0",
		StatusBg: "#fcfcfc", Error: "#ffecec", Ok: "#ecffec",
	},
	"retro-82 (real, dark)": {
		Mode: "dark", Background: "#05182e", Foreground: "#f6dcac",
		Accent: "#faa968", Selection: "#134e5a", Muted: "#2a6b78",
		StatusBg: "#0a2540", Error: "#f85525", Ok: "#028391",
	},
	"catppuccin-latte (real, light)": {
		Mode: "light", Background: "#eff1f5", Foreground: "#4c4f69",
		Accent: "#1e66f5", Selection: "#bcc0cc", Muted: "#8c8fa1",
		StatusBg: "#e6e9ef", Error: "#d20f39", Ok: "#40a02b",
	},
}

func TestOmarchyThemePassesContrastChecks(t *testing.T) {
	for name, p := range hostilePalettes {
		p := p
		t.Run(name, func(t *testing.T) {
			theme := contrastCorrectTheme(omarchyTheme(p))
			styles := BuildStyles(theme, "comfortable", "square")
			for _, check := range contrastChecks {
				fg := check.fg(styles)
				bg := check.bg(styles)
				if fg == "" || bg == "" {
					t.Errorf("%s: fg=%q bg=%q — one or both colors are unset", check.name, fg, bg)
					continue
				}
				if ratio := contrastRatio(fg, bg); ratio < check.minRatio {
					t.Errorf("%s: contrast %.2f:1 < %.1f:1 (fg=%s bg=%s)",
						check.name, ratio, check.minRatio, fg, bg)
				}
			}

			// The focused-pane border is held to a higher bar than text.
			border := styleColor(styles.PaneFrame(true).GetBorderTopForeground())
			if border == "" {
				t.Fatal("focused pane border color is unset")
			}
			if ratio := contrastRatio(border, theme.Bg); ratio < paneFocusMinContrast {
				t.Errorf("focused pane border contrast %.2f:1 < %.1f:1 (border=%s bg=%s)",
					ratio, paneFocusMinContrast, border, theme.Bg)
			}
		})
	}
}

func TestOmarchyThemeInlineMessageColorsAreReadable(t *testing.T) {
	for name, p := range hostilePalettes {
		p := p
		t.Run(name, func(t *testing.T) {
			theme := contrastCorrectTheme(omarchyTheme(p))
			checks := []struct {
				name     string
				fg, bg   lipgloss.Color
				minRatio float64
			}{
				{"heading", messageHeadingColor(theme), theme.Bg, 4.5},
				{"link", messageLinkColor(theme), theme.Bg, 4.5},
				{"quote", messageMutedColor(theme), theme.Bg, 3.0},
			}
			for _, c := range checks {
				if ratio := contrastRatio(c.fg, c.bg); ratio < c.minRatio {
					t.Errorf("%s contrast %.2f:1 < %.1f:1 (fg=%s bg=%s)",
						c.name, ratio, c.minRatio, c.fg, c.bg)
				}
			}
		})
	}
}

func TestResolveOmarchyThemeFallsBackWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("PATH", filepath.Join(dir, "no-bin"))

	if _, ok := resolveOmarchyTheme(); ok {
		t.Fatal("resolveOmarchyTheme should return ok=false with no Omarchy present")
	}

	merged, idx := MergedThemeFromConfig(config.Config{Theme: ThemeNameMatchOmarchy})
	if idx != omarchyIndex {
		t.Errorf("index = %d, want omarchyIndex %d", idx, omarchyIndex)
	}
	def, _ := ThemeByName(config.DefaultConfig().Theme)
	if merged.Bg != def.Bg || merged.Fg != def.Fg {
		t.Errorf("fallback theme = %+v, want default %q colors", merged, def.Name)
	}
}

func TestResolveOmarchyThemeReadsStagedPalette(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("PATH", filepath.Join(dir, "no-bin"))

	themeDir := filepath.Join(dir, "omarchy", "current", "theme")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	colors := "mode = \"dark\"\nbackground = \"#05182e\"\nforeground = \"#f6dcac\"\naccent = \"#faa968\"\nred = \"#f85525\"\ngreen = \"#028391\"\nmuted = \"#2a6b78\"\n"
	if err := os.WriteFile(filepath.Join(themeDir, "colors.toml"), []byte(colors), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "omarchy", "current", "theme.name"), []byte("retro-82\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	theme, ok := resolveOmarchyTheme()
	if !ok {
		t.Fatal("expected ok=true when a staged colors.toml exists")
	}
	if theme.Name != ThemeNameMatchOmarchy {
		t.Errorf("name = %q", theme.Name)
	}
	if got := contrastRatio(theme.Fg, theme.Bg); got < 4.5 {
		t.Errorf("fg/bg contrast %.2f:1 < 4.5:1", got)
	}
	if currentOmarchyThemeName() != "retro-82" {
		t.Errorf("currentOmarchyThemeName = %q, want retro-82", currentOmarchyThemeName())
	}
}

// The T overlay theme picker must window its list so it stays inside the box
// border on short terminals — the selected row and the footer hints must remain
// visible instead of spilling past the frame.
func TestThemePickerWindowsToFitTerminal(t *testing.T) {
	last := len(PickableThemes()) - 1
	for _, theme := range []string{"catppuccin-mocha", "vt52"} {
		for _, h := range []int{12, 16, 20, 24, 40} {
			m := NewModel(nil, config.Config{Theme: theme, Display: config.DefaultConfig().Display}, "dev", false)
			m.width = 60
			m.height = h
			m.overlay = overlayThemePicker
			m.themeCursor = last // cursor on the final row (match-omarchy)

			out := m.View()
			if !strings.Contains(out, ThemeNameMatchOmarchy) {
				t.Errorf("theme=%s h=%d: selected row %q not visible in picker", theme, h, ThemeNameMatchOmarchy)
			}
			if !strings.Contains(out, "confirm") {
				t.Errorf("theme=%s h=%d: picker footer hints clipped", theme, h)
			}
			if got := strings.Count(out, "\n") + 1; got != h {
				t.Errorf("theme=%s h=%d: rendered %d lines, want exactly %d", theme, h, got, h)
			}
		}
	}
}

func TestThemePickerKeepsMidListCursorVisible(t *testing.T) {
	m := NewModel(nil, config.Config{Theme: "catppuccin-mocha", Display: config.DefaultConfig().Display}, "dev", false)
	m.width = 60
	m.height = 14
	m.overlay = overlayThemePicker
	m.themeCursor = 9 // a theme partway down the list
	want := PickableThemes()[9].Name

	if out := m.View(); !strings.Contains(out, want) {
		t.Errorf("mid-list cursor theme %q not visible on a short terminal", want)
	}
}

func TestPickableThemesHasMatchOmarchyLast(t *testing.T) {
	pt := PickableThemes()
	if len(pt) != len(BuiltinThemes)+1 {
		t.Fatalf("PickableThemes len = %d, want %d", len(pt), len(BuiltinThemes)+1)
	}
	if pt[omarchyIndex].Name != ThemeNameMatchOmarchy {
		t.Errorf("PickableThemes[%d].Name = %q, want %q", omarchyIndex, pt[omarchyIndex].Name, ThemeNameMatchOmarchy)
	}
	if _, idx := ThemeByName(ThemeNameMatchOmarchy); idx != omarchyIndex {
		t.Errorf("ThemeByName(%q) index = %d, want %d", ThemeNameMatchOmarchy, idx, omarchyIndex)
	}
}
