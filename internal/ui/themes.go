package ui

import "github.com/allisonhere/tideui"

// Theme is an alias for tideui.Theme. TideMail's UI is a fork of the shared
// tideui toolkit; aliasing the type (rather than redeclaring an identical
// struct) keeps the two from silently drifting and lets TideMail hand its
// themes straight to any tideui API. The field set is identical:
// Name, Bg, Fg, Border, BorderFocus, Selected, Unread, Dimmed, StatusBar,
// StatusFg, Error, Overlay, OverlayBorder — all lipgloss.Color.
type Theme = tideui.Theme

// The stock palettes come straight from tideui — verified byte-identical to
// TideMail's former local copies — so a palette fix upstream now reaches
// TideMail on a version bump. VT52/VT100 are the exception: TideMail dims their
// focus/selection accent below tideui's full-bright value to satisfy its
// stricter focused-pane contrast floor, so those two stay defined here.
var (
	CatppuccinMocha       = tideui.CatppuccinMocha
	CatppuccinLatte       = tideui.CatppuccinLatte
	CatppuccinFrappe      = tideui.CatppuccinFrappe
	CatppuccinMacchiato   = tideui.CatppuccinMacchiato
	Nord                  = tideui.Nord
	Dracula               = tideui.Dracula
	GruvboxDark           = tideui.GruvboxDark
	GruvboxLight          = tideui.GruvboxLight
	TokyoNight            = tideui.TokyoNight
	TokyoNightDay         = tideui.TokyoNightDay
	RosePine              = tideui.RosePine
	RosePineMoon          = tideui.RosePineMoon
	RosePineDawn          = tideui.RosePineDawn
	OneDark               = tideui.OneDark
	MagentaGeode          = tideui.MagentaGeode
	CoralSunset           = tideui.CoralSunset
	LavenderFieldsForever = tideui.LavenderFieldsForever
)

// VT100 — DEC-style green phosphor on black CRT. Accent dimmed from tideui's
// #00ff00 so the focused-pane border clears TideMail's 7:1 contrast floor.
var VT100 = Theme{
	Name:          "vt100",
	Bg:            "#000000",
	Fg:            "#33ff33",
	Border:        "#145214",
	BorderFocus:   "#116611",
	Selected:      "#116611",
	Unread:        "#66ff66",
	Dimmed:        "#3dcc3d",
	StatusBar:     "#001a00",
	StatusFg:      "#33ff33",
	Error:         "#ff6b6b",
	Overlay:       "#001200",
	OverlayBorder: "#116611",
}

// VT52 — P4-style amber phosphor on black; ASCII borders when this theme is
// active (see ThemeUsesASCII). Accent dimmed from tideui's #ffb020.
var VT52 = Theme{
	Name:          "vt52",
	Bg:            "#000000",
	Fg:            "#ffcc66",
	Border:        "#6b4e14",
	BorderFocus:   "#664400",
	Selected:      "#664400",
	Unread:        "#ffe6a8",
	Dimmed:        "#a67c2e",
	StatusBar:     "#1a1206",
	StatusFg:      "#ffcc66",
	Error:         "#ff6666",
	Overlay:       "#140e04",
	OverlayBorder: "#664400",
}

var BuiltinThemes = []Theme{
	CatppuccinMocha,
	CatppuccinLatte,
	CatppuccinFrappe,
	CatppuccinMacchiato,
	Nord,
	Dracula,
	GruvboxDark,
	GruvboxLight,
	TokyoNight,
	TokyoNightDay,
	RosePine,
	RosePineMoon,
	RosePineDawn,
	OneDark,
	MagentaGeode,
	CoralSunset,
	LavenderFieldsForever,
	VT100,
	VT52,
}

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
