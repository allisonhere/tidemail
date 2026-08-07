package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
)

// ThemeNameVT52 is the built-in theme that uses ASCII borders and glyphs.
const ThemeNameVT52 = "vt52"

// ThemeNameVT100 is the DEC green phosphor theme (Unicode borders).
const ThemeNameVT100 = "vt100"

// ThemeUsesASCII reports whether the theme uses ASCII box-drawing and punctuation.
func ThemeUsesASCII(themeName string) bool {
	return themeName == ThemeNameVT52
}

// MergeRetroTweak applies optional #rrggbb overrides from config onto a base retro theme.
func MergeRetroTweak(base Theme, tw config.RetroTerminalTweak) Theme {
	out := base
	if tw.Bg != "" {
		out.Bg = lipgloss.Color(tw.Bg)
	}
	if tw.Fg != "" {
		c := lipgloss.Color(tw.Fg)
		out.Fg = c
		out.StatusFg = c
	}
	if tw.Accent != "" {
		c := lipgloss.Color(tw.Accent)
		out.BorderFocus = c
		out.Selected = c
		out.OverlayBorder = c
	}
	return out
}

// ApplyDisplayOverrides returns the effective theme for cfg (merge vt52/vt100 tweaks).
func ApplyDisplayOverrides(t Theme, cfg config.Config) Theme {
	switch t.Name {
	case ThemeNameVT52:
		return MergeRetroTweak(t, cfg.Display.VT52)
	case ThemeNameVT100:
		return MergeRetroTweak(t, cfg.Display.VT100)
	default:
		return t
	}
}

// MergedThemeFromConfig resolves cfg.Theme to a builtin theme and applies retro overrides.
func MergedThemeFromConfig(cfg config.Config) (Theme, int) {
	base, idx := ThemeByName(cfg.Theme)
	return ApplyDisplayOverrides(base, cfg), idx
}

// MergedBuiltinThemeAtIndex returns the theme at idx with cfg-based retro overrides applied.
func MergedBuiltinThemeAtIndex(cfg config.Config, idx int) Theme {
	if idx < 0 || idx >= len(BuiltinThemes) {
		idx = 0
	}
	t := BuiltinThemes[idx]
	return ApplyDisplayOverrides(t, cfg)
}
