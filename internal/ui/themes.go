package ui

import (
	"slices"

	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"
)

// Theme is an alias for tideui.Theme. TideMail's UI is a fork of the shared
// tideui toolkit; aliasing the type (rather than redeclaring an identical
// struct) keeps the two from silently drifting and lets TideMail hand its
// themes straight to any tideui API. The field set is identical: the twelve
// core colors, the Family and Variant labels, and the optional Success,
// Warning, Link, Quote, Match, Tag and Cursor roles.
type Theme = tideui.Theme

// The palettes come from tideui, which owns the theme collection (families,
// variants, upstream sources and licenses; see docs/themes.md). VT52 and VT100
// are the exception: TideMail dims their focus/selection accent below tideui's
// full-bright value to satisfy its stricter focused-pane contrast floor, so it
// derives its own copy of those two.
var CatppuccinMocha = tideui.CatppuccinMocha

// retroAccent returns t with its focus, selection and modal-border accent
// replaced by a dimmer one.
func retroAccent(t Theme, accent string) Theme {
	t.BorderFocus = lipgloss.Color(accent)
	t.Selected = lipgloss.Color(accent)
	t.OverlayBorder = lipgloss.Color(accent)
	return t
}

var (
	// VT100: accent dimmed from tideui's #00ff00 so the focused-pane border
	// clears TideMail's 7:1 contrast floor.
	VT100 = retroAccent(tideui.VT100, "#116611")
	// VT52: accent dimmed from tideui's #ffb020 for the same reason.
	VT52 = retroAccent(tideui.VT52, "#664400")
)

// BuiltinThemes is tideui's collection, grouped by family, with TideMail's
// VT100 and VT52 in place of tideui's.
var BuiltinThemes = func() []Theme {
	themes := slices.Clone(tideui.BuiltinThemes)
	for i, t := range themes {
		switch t.Name {
		case ThemeNameVT100:
			themes[i] = VT100
		case ThemeNameVT52:
			themes[i] = VT52
		}
	}
	return themes
}()

func ThemeByName(name string) (Theme, int) {
	if isMatchOmarchy(name) {
		return omarchyPlaceholderTheme, omarchyIndex
	}
	for i, t := range BuiltinThemes {
		if t.Name == name {
			return t, i
		}
	}
	return BuiltinThemes[0], 0
}

// PickableThemes is BuiltinThemes plus the runtime-resolved "match-omarchy"
// pseudo-theme. The theme pickers (Settings screen and the T overlay) iterate
// this; contrast tests keep iterating BuiltinThemes only.
func PickableThemes() []Theme {
	out := make([]Theme, 0, len(BuiltinThemes)+1)
	out = append(out, BuiltinThemes...)
	out = append(out, omarchyPlaceholderTheme)
	return out
}

// pickableThemeNameAt returns the theme name at idx into PickableThemes(),
// clamping an out-of-range index to the first theme.
func pickableThemeNameAt(idx int) string {
	pt := PickableThemes()
	if idx < 0 || idx >= len(pt) {
		idx = 0
	}
	return pt[idx].Name
}

// pickableThemeLabelAt returns the human-readable label (family and variant) of
// the theme at idx into PickableThemes(), clamping like pickableThemeNameAt.
func pickableThemeLabelAt(idx int) string {
	pt := PickableThemes()
	if idx < 0 || idx >= len(pt) {
		idx = 0
	}
	return pt[idx].Label()
}
