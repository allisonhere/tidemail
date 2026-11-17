package ui

import (
	"slices"

	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/omarchy"
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

// ThemeNameOmarchy is the built-in theme that looks like Omarchy's own apps:
// its default Tokyo Night colors, sharp corners (Omarchy's window rounding is
// 0 by default), no overlay shadow and flat square tags. Unlike match-omarchy
// it does not follow the desktop palette; the look is fixed.
const ThemeNameOmarchy = "omarchy"

// Omarchy is the Omarchy theme's palette: Omarchy's own black-and-white
// "vantablack" theme (pure black, white text, gray accent), taken from its
// colors.toml, so TideMail looks like the rest of the desktop's UI. It goes
// through the same mapping and contrast correction as match-omarchy. Like the
// VT52 and VT100 themes it has no color at all: every role is a shade of gray,
// white or black, errors included.
var Omarchy = func() Theme {
	t := contrastCorrectTheme(omarchyTheme(omarchy.Palette{
		Name:       "vantablack",
		Mode:       "dark",
		Background: "#000000",
		Foreground: "#ffffff",
		Accent:     "#8d8d8d",
		Selection:  "#1a1a1a",
		Muted:      "#7a7a7a",
		StatusBg:   "#1a1a1a", // lighter_background
		Error:      "#d35f5f",
		Ok:         "#b6b6b6",
	}))
	// Omarchy's popups use the plain background with an accent border, not a
	// raised surface.
	t.Overlay = t.Bg
	t.StatusBar = t.Bg
	t.Name = ThemeNameOmarchy
	t.Family, t.Variant = "Omarchy", "Default"
	t.Error = lipgloss.Color("#ffffff")
	t.Success = lipgloss.Color("#b6b6b6")
	t.Warning = lipgloss.Color("#cecece")
	t.Link = lipgloss.Color("#ececec")
	t.Quote = lipgloss.Color("#8d8d8d")
	t.Match = lipgloss.Color("#cecece")
	t.Tag = lipgloss.Color("#9b9b9b")
	t.Cursor = lipgloss.Color("#ffffff")
	return withAlertColors(t)
}()

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
		default:
			themes[i] = withAlertColors(t)
		}
	}
	return append(themes, Omarchy)
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
