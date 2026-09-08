package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tideui"

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

// MergeRetroTweak applies optional #rrggbb overrides from config onto a base
// retro theme. tideui.ThemeOverrides.Apply has the identical field mapping
// (Foreground → Fg+StatusFg, Accent → BorderFocus+Selected+OverlayBorder), so
// this defers to it rather than keeping a parallel copy.
func MergeRetroTweak(base Theme, tw config.RetroTerminalTweak) Theme {
	return tideui.ThemeOverrides{
		Background: lipgloss.Color(tw.Bg),
		Foreground: lipgloss.Color(tw.Fg),
		Accent:     lipgloss.Color(tw.Accent),
	}.Apply(base)
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
	if isMatchOmarchy(cfg.Theme) {
		return ApplyDisplayOverrides(omarchyOrFallbackTheme(), cfg), omarchyIndex
	}
	base, idx := ThemeByName(cfg.Theme)
	return ApplyDisplayOverrides(base, cfg), idx
}

// MergedBuiltinThemeAtIndex returns the theme at idx with cfg-based retro overrides applied.
func MergedBuiltinThemeAtIndex(cfg config.Config, idx int) Theme {
	if idx == omarchyIndex {
		return ApplyDisplayOverrides(omarchyOrFallbackTheme(), cfg)
	}
	if idx < 0 || idx >= len(BuiltinThemes) {
		idx = 0
	}
	t := BuiltinThemes[idx]
	return ApplyDisplayOverrides(t, cfg)
}

// omarchyOrFallbackTheme resolves the live Omarchy palette, or falls back to
// the default built-in theme when Omarchy isn't available.
func omarchyOrFallbackTheme() Theme {
	if t, ok := resolveOmarchyTheme(); ok {
		return t
	}
	fallback, _ := ThemeByName(config.DefaultConfig().Theme)
	return fallback
}
