package ui

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Name          string
	Bg            lipgloss.Color
	Fg            lipgloss.Color
	Border        lipgloss.Color
	BorderFocus   lipgloss.Color
	Selected      lipgloss.Color
	Unread        lipgloss.Color
	Dimmed        lipgloss.Color
	StatusBar     lipgloss.Color
	StatusFg      lipgloss.Color
	Error         lipgloss.Color
	Overlay       lipgloss.Color
	OverlayBorder lipgloss.Color
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
	for i, t := range BuiltinThemes {
		if t.Name == name {
			return t, i
		}
	}
	return BuiltinThemes[0], 0
}

// ── Catppuccin ──────────────────────────────────────────────────────────────

var CatppuccinMocha = Theme{
	Name:          "catppuccin-mocha",
	Bg:            "#1e1e2e",
	Fg:            "#cdd6f4",
	Border:        "#6c7086",
	BorderFocus:   "#89b4fa",
	Selected:      "#89b4fa",
	Unread:        "#a6e3a1",
	Dimmed:        "#585b70",
	StatusBar:     "#313244",
	StatusFg:      "#cdd6f4",
	Error:         "#f38ba8",
	Overlay:       "#313244",
	OverlayBorder: "#89b4fa",
}

var CatppuccinLatte = Theme{
	Name:          "catppuccin-latte",
	Bg:            "#eff1f5",
	Fg:            "#4c4f69",
	Border:        "#9ca0b0",
	BorderFocus:   "#1e66f5",
	Selected:      "#1e66f5",
	Unread:        "#40a02b",
	Dimmed:        "#8c8fa1",
	StatusBar:     "#e6e9ef",
	StatusFg:      "#4c4f69",
	Error:         "#d20f39",
	Overlay:       "#e6e9ef",
	OverlayBorder: "#1e66f5",
}

var CatppuccinFrappe = Theme{
	Name:          "catppuccin-frappe",
	Bg:            "#303446",
	Fg:            "#c6d0f5",
	Border:        "#626880",
	BorderFocus:   "#8caaee",
	Selected:      "#8caaee",
	Unread:        "#a6d189",
	Dimmed:        "#51576d",
	StatusBar:     "#292c3c",
	StatusFg:      "#c6d0f5",
	Error:         "#e78284",
	Overlay:       "#292c3c",
	OverlayBorder: "#8caaee",
}

var CatppuccinMacchiato = Theme{
	Name:          "catppuccin-macchiato",
	Bg:            "#24273a",
	Fg:            "#cad3f5",
	Border:        "#5b6078",
	BorderFocus:   "#8aadf4",
	Selected:      "#8aadf4",
	Unread:        "#a6da95",
	Dimmed:        "#494d64",
	StatusBar:     "#1e2030",
	StatusFg:      "#cad3f5",
	Error:         "#ed8796",
	Overlay:       "#1e2030",
	OverlayBorder: "#8aadf4",
}

// ── Nord ─────────────────────────────────────────────────────────────────────

var Nord = Theme{
	Name:          "nord",
	Bg:            "#2e3440",
	Fg:            "#eceff4",
	Border:        "#4c566a",
	BorderFocus:   "#88c0d0",
	Selected:      "#88c0d0",
	Unread:        "#a3be8c",
	Dimmed:        "#4c566a",
	StatusBar:     "#3b4252",
	StatusFg:      "#d8dee9",
	Error:         "#bf616a",
	Overlay:       "#3b4252",
	OverlayBorder: "#88c0d0",
}

// ── Dracula ──────────────────────────────────────────────────────────────────

var Dracula = Theme{
	Name:          "dracula",
	Bg:            "#282a36",
	Fg:            "#f8f8f2",
	Border:        "#6272a4",
	BorderFocus:   "#bd93f9",
	Selected:      "#bd93f9",
	Unread:        "#50fa7b",
	Dimmed:        "#6272a4",
	StatusBar:     "#21222c",
	StatusFg:      "#f8f8f2",
	Error:         "#ff5555",
	Overlay:       "#21222c",
	OverlayBorder: "#bd93f9",
}

// ── Gruvbox ──────────────────────────────────────────────────────────────────

var GruvboxDark = Theme{
	Name:          "gruvbox-dark",
	Bg:            "#282828",
	Fg:            "#ebdbb2",
	Border:        "#504945",
	BorderFocus:   "#83a598",
	Selected:      "#83a598",
	Unread:        "#b8bb26",
	Dimmed:        "#504945",
	StatusBar:     "#1d2021",
	StatusFg:      "#ebdbb2",
	Error:         "#fb4934",
	Overlay:       "#32302f",
	OverlayBorder: "#83a598",
}

var GruvboxLight = Theme{
	Name:          "gruvbox-light",
	Bg:            "#fbf1c7",
	Fg:            "#3c3836",
	Border:        "#bdae93",
	BorderFocus:   "#076678",
	Selected:      "#076678",
	Unread:        "#79740e",
	Dimmed:        "#bdae93",
	StatusBar:     "#f2e5bc",
	StatusFg:      "#3c3836",
	Error:         "#cc241d",
	Overlay:       "#f2e5bc",
	OverlayBorder: "#076678",
}

// ── Tokyo Night ───────────────────────────────────────────────────────────────

var TokyoNight = Theme{
	Name:          "tokyo-night",
	Bg:            "#1a1b26",
	Fg:            "#c0caf5",
	Border:        "#414868",
	BorderFocus:   "#7aa2f7",
	Selected:      "#7aa2f7",
	Unread:        "#9ece6a",
	Dimmed:        "#414868",
	StatusBar:     "#16161e",
	StatusFg:      "#a9b1d6",
	Error:         "#f7768e",
	Overlay:       "#16161e",
	OverlayBorder: "#7aa2f7",
}

var TokyoNightDay = Theme{
	Name:          "tokyo-night-day",
	Bg:            "#e1e2e7",
	Fg:            "#3760bf",
	Border:        "#a8aecb",
	BorderFocus:   "#2e7de9",
	Selected:      "#2e7de9",
	Unread:        "#587539",
	Dimmed:        "#a8aecb",
	StatusBar:     "#d0d5e3",
	StatusFg:      "#3760bf",
	Error:         "#f52a65",
	Overlay:       "#d0d5e3",
	OverlayBorder: "#2e7de9",
}

// ── Rosé Pine ─────────────────────────────────────────────────────────────────

var RosePine = Theme{
	Name:          "rose-pine",
	Bg:            "#191724",
	Fg:            "#e0def4",
	Border:        "#403d52",
	BorderFocus:   "#c4a7e7",
	Selected:      "#c4a7e7",
	Unread:        "#9ccfd8",
	Dimmed:        "#403d52",
	StatusBar:     "#1f1d2e",
	StatusFg:      "#e0def4",
	Error:         "#eb6f92",
	Overlay:       "#1f1d2e",
	OverlayBorder: "#c4a7e7",
}

var RosePineMoon = Theme{
	Name:          "rose-pine-moon",
	Bg:            "#232136",
	Fg:            "#e0def4",
	Border:        "#44415a",
	BorderFocus:   "#c4a7e7",
	Selected:      "#c4a7e7",
	Unread:        "#9ccfd8",
	Dimmed:        "#44415a",
	StatusBar:     "#2a2837",
	StatusFg:      "#e0def4",
	Error:         "#eb6f92",
	Overlay:       "#2a2837",
	OverlayBorder: "#c4a7e7",
}

var RosePineDawn = Theme{
	Name:          "rose-pine-dawn",
	Bg:            "#faf4ed",
	Fg:            "#575279",
	Border:        "#d7d2be",
	BorderFocus:   "#907aa9",
	Selected:      "#907aa9",
	Unread:        "#286983",
	Dimmed:        "#d7d2be",
	StatusBar:     "#fffaf3",
	StatusFg:      "#575279",
	Error:         "#b4637a",
	Overlay:       "#fffaf3",
	OverlayBorder: "#907aa9",
}

// ── One Dark ──────────────────────────────────────────────────────────────────

var OneDark = Theme{
	Name:          "one-dark",
	Bg:            "#282c34",
	Fg:            "#abb2bf",
	Border:        "#3e4451",
	BorderFocus:   "#61afef",
	Selected:      "#61afef",
	Unread:        "#98c379",
	Dimmed:        "#3e4451",
	StatusBar:     "#21252b",
	StatusFg:      "#abb2bf",
	Error:         "#e06c75",
	Overlay:       "#21252b",
	OverlayBorder: "#61afef",
}

// ── Custom (paperheartdesign.com) ─────────────────────────────────────────────

// MagentaGeode — sparkling purples and pinks
var MagentaGeode = Theme{
	Name:          "magenta-geode",
	Bg:            "#47003c",
	Fg:            "#f3b0dc",
	Border:        "#aa4d84",
	BorderFocus:   "#c83fa9",
	Selected:      "#c83fa9",
	Unread:        "#f3b0dc",
	Dimmed:        "#77176e",
	StatusBar:     "#77176e",
	StatusFg:      "#f3b0dc",
	Error:         "#ff7062",
	Overlay:       "#77176e",
	OverlayBorder: "#c83fa9",
}

// CoralSunset — muted purples with warm coral accents
var CoralSunset = Theme{
	Name:          "coral-sunset",
	Bg:            "#444154",
	Fg:            "#fec9c1",
	Border:        "#fc8b79",
	BorderFocus:   "#ff7062",
	Selected:      "#ff7062",
	Unread:        "#fec9c1",
	Dimmed:        "#7a637f",
	StatusBar:     "#7a637f",
	StatusFg:      "#fec9c1",
	Error:         "#ff7062",
	Overlay:       "#7a637f",
	OverlayBorder: "#ff7062",
}

// LavenderFieldsForever — deep indigo with soft lavender
var LavenderFieldsForever = Theme{
	Name:          "lavender-fields-forever",
	Bg:            "#382d72",
	Fg:            "#e5ccf4",
	Border:        "#b7c2c6",
	BorderFocus:   "#a080e1",
	Selected:      "#a080e1",
	Unread:        "#e5ccf4",
	Dimmed:        "#5c509c",
	StatusBar:     "#5c509c",
	StatusFg:      "#e5ccf4",
	Error:         "#ff7062",
	Overlay:       "#5c509c",
	OverlayBorder: "#a080e1",
}

// VT100 — DEC-style green phosphor on black CRT
var VT100 = Theme{
	Name:          "vt100",
	Bg:            "#000000",
	Fg:            "#33ff33",
	Border:        "#145214",
	BorderFocus:   "#00ff00",
	Selected:      "#00ff00",
	Unread:        "#66ff66",
	Dimmed:        "#3dcc3d",
	StatusBar:     "#001a00",
	StatusFg:      "#33ff33",
	Error:         "#ff6b6b",
	Overlay:       "#001200",
	OverlayBorder: "#00ff00",
}

// VT52 — P4-style amber phosphor on black; ASCII borders when this theme is active (see ThemeUsesASCII).
var VT52 = Theme{
	Name:          "vt52",
	Bg:            "#000000",
	Fg:            "#ffcc66",
	Border:        "#6b4e14",
	BorderFocus:   "#ffb020",
	Selected:      "#ffb020",
	Unread:        "#ffe6a8",
	Dimmed:        "#a67c2e",
	StatusBar:     "#1a1206",
	StatusFg:      "#ffcc66",
	Error:         "#ff6666",
	Overlay:       "#140e04",
	OverlayBorder: "#ffb020",
}
