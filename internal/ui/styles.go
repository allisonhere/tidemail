package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
)

type Styles struct {
	Theme Theme
	// PlainUI is true for the vt52 theme (ASCII borders and glyphs).
	PlainUI bool
	// Density is normalized ("comfortable" | "compact") and matches config.Display.Density.
	Density string
	// RoundedCorners is true when pane borders should use rounded corners
	// instead of square ones, per config.Display.PaneCorners.
	RoundedCorners bool

	// Pane containers
	PaneHeaderActive   lipgloss.Style
	PaneHeaderInactive lipgloss.Style

	// Feed list items
	FeedItem                  lipgloss.Style
	FeedItemSelected          lipgloss.Style
	FeedItemSelectedFocused   lipgloss.Style
	FeedItemSelectedUnfocused lipgloss.Style
	UnreadBadge               lipgloss.Style

	// Article list items
	ArticleUnread   lipgloss.Style
	ArticleRead     lipgloss.Style
	ArticleSelected lipgloss.Style
	ArticleTime     lipgloss.Style
	UnreadDot       lipgloss.Style

	// Content pane
	ContentTitle     lipgloss.Style
	ContentMeta      lipgloss.Style
	ContentBody      lipgloss.Style
	ContentFocusLine lipgloss.Style
	SearchMatch      lipgloss.Style

	// Status bar
	StatusBar       lipgloss.Style
	StatusError     lipgloss.Style
	StatusSpinner   lipgloss.Style
	StatusHint      lipgloss.Style
	StatusBarJoiner lipgloss.Style
	StatusNotice    lipgloss.Style

	// Overlay chrome
	Overlay      lipgloss.Style
	OverlayTitle lipgloss.Style
	OverlayHint  lipgloss.Style

	// Inputs inside overlays/feed manager
	InputFocused   lipgloss.Style
	InputUnfocused lipgloss.Style
	InputLabel     lipgloss.Style

	// Help screen
	HelpSection     lipgloss.Style
	HelpSectionBody lipgloss.Style
	HelpKey         lipgloss.Style
	HelpDesc        lipgloss.Style

	// Spinner
	Spinner lipgloss.Style
}

// ListItemLineStride is the number of terminal lines one feed/article list row occupies.
func (s Styles) ListItemLineStride() int {
	if s.Density == "compact" {
		return 1
	}
	return 2
}

// lipPaneBorder returns Unicode line-drawing or ASCII borders for panes and overlays.
func lipPaneBorder(plain bool) lipgloss.Border {
	if plain {
		return lipgloss.ASCIIBorder()
	}
	return lipgloss.NormalBorder()
}

// lipOverlayBorder returns rounded corners or ASCII corners for modal chrome.
func lipOverlayBorder(plain bool) lipgloss.Border {
	if plain {
		return lipgloss.ASCIIBorder()
	}
	return lipgloss.RoundedBorder()
}

// paneFrameBorder picks the pane border glyph set: rounded corners when
// requested, falling back to ASCII for the plain vt52 theme either way.
func paneFrameBorder(plainUI, rounded bool) lipgloss.Border {
	if rounded {
		return lipOverlayBorder(plainUI)
	}
	return lipPaneBorder(plainUI)
}

// PaneFrame returns a full 4-sided border box for a pane, colored to signal
// whether that pane currently has focus.
func (s Styles) PaneFrame(focused bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Background(s.Theme.Bg).
		BorderBackground(s.Theme.Bg).
		Border(paneFrameBorder(s.PlainUI, s.RoundedCorners)).
		AlignVertical(lipgloss.Top)
	if focused {
		return style.BorderForeground(accentReadableOn(s.Theme.BorderFocus, s.Theme.Bg, paneFocusMinContrast))
	}
	return style.BorderForeground(s.Theme.Border)
}

func BuildStyles(t Theme, density string, paneCorners string) Styles {
	plainUI := t.Name == ThemeNameVT52
	d := config.NormalizeDisplayDensity(density)
	roundedCorners := config.NormalizePaneCorners(paneCorners) == "round"
	listPad := func(s lipgloss.Style) lipgloss.Style {
		if d == "compact" {
			return s
		}
		// Comfortable: one spacer line below each row (not symmetric top+bottom).
		return s.Padding(0, 0, 1, 0)
	}
	modalPadTop, modalPadRight, modalPadBottom, modalPadLeft := 1, 2, 1, 2
	contentTitleMB := 1
	overlayTitleMB := 1
	if d == "compact" {
		modalPadTop, modalPadRight, modalPadBottom, modalPadLeft = 0, 1, 0, 1
		contentTitleMB = 0
		overlayTitleMB = 0
	}

	modalBg := modalSurface(t)
	modalBorder := t.OverlayBorder
	if modalBorder == "" {
		modalBorder = t.Border
	}
	modalAccent := t.BorderFocus
	if modalAccent == "" {
		modalAccent = modalBorder
	}
	modalFg := readableText(t.Fg, modalBg, 4.5)
	modalMuted := mutedText(modalFg, modalBg)
	accentFg := readableText(t.Fg, modalAccent, 4.5)
	helpSectionBg := func() lipgloss.Color {
		if isDark(modalBg) {
			return adjustLightness(modalBg, 0.03)
		}
		return adjustLightness(modalBg, -0.03)
	}()
	helpSectionText := readableText(t.Fg, helpSectionBg, 4.5)
	helpSectionMuted := mutedText(helpSectionText, helpSectionBg)

	selectedBg := selectionBgForRatio(t.Bg, selectedBgMinContrast)
	selectedBgSoft := selectionBgForRatio(t.Bg, selectedBgSoftMinContrast)
	contentFocusBg := focusLineBg(t)

	unreadBg := func() lipgloss.Color {
		if isDark(t.Bg) {
			return adjustLightness(t.Bg, selectionSoftDelta(t.Bg))
		}
		return adjustLightness(t.Bg, -selectionSoftDelta(t.Bg))
	}()

	return Styles{
		Theme:          t,
		PlainUI:        plainUI,
		Density:        d,
		RoundedCorners: roundedCorners,

		PaneHeaderActive: lipgloss.NewStyle().
			Background(t.BorderFocus).
			Foreground(accentReadableOn(t.Fg, t.BorderFocus, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left),
		PaneHeaderInactive: lipgloss.NewStyle().
			Background(t.Border).
			Foreground(accentReadableOn(t.Fg, t.Border, 4.5)).
			AlignHorizontal(lipgloss.Left),

		FeedItem: listPad(lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(t.Fg).
			AlignHorizontal(lipgloss.Left)),
		FeedItemSelected: listPad(lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(accentReadableOn(t.BorderFocus, selectedBg, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		FeedItemSelectedFocused: listPad(lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(accentReadableOn(t.BorderFocus, selectedBg, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		FeedItemSelectedUnfocused: listPad(lipgloss.NewStyle().
			Background(selectedBgSoft).
			Foreground(accentReadableOn(t.BorderFocus, selectedBgSoft, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		UnreadBadge: lipgloss.NewStyle().
			Foreground(t.Unread).
			Bold(true),

		ArticleUnread: listPad(lipgloss.NewStyle().
			Background(unreadBg).
			Foreground(t.Fg).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		ArticleRead: listPad(lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(mutedText(t.Fg, t.Bg)).
			AlignHorizontal(lipgloss.Left)),
		ArticleSelected: listPad(lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(accentReadableOn(t.BorderFocus, selectedBg, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		ArticleTime: lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(t.Dimmed),
		UnreadDot: lipgloss.NewStyle().
			Background(unreadBg).
			Foreground(t.Unread),

		ContentTitle: lipgloss.NewStyle().
			Background(t.BorderFocus).
			Foreground(accentReadableOn(t.Fg, t.BorderFocus, 4.5)).
			Bold(true).
			Padding(0, 1).
			MarginBottom(contentTitleMB),
		ContentMeta: lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(readableText(t.Dimmed, t.Bg, 3.0)).
			Italic(true),
		ContentBody: lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(readableText(t.Fg, t.Bg, 4.5)),
		ContentFocusLine: lipgloss.NewStyle().
			Background(contentFocusBg).
			Foreground(readableText(t.Fg, contentFocusBg, 4.5)),
		SearchMatch: lipgloss.NewStyle().
			Background(t.BorderFocus).
			Foreground(accentReadableOn(t.Fg, t.BorderFocus, 4.5)),

		StatusBar: lipgloss.NewStyle().
			Background(t.StatusBar).
			Foreground(readableText(t.StatusFg, t.StatusBar, 4.5)).
			Padding(0, 1),
		StatusError: lipgloss.NewStyle().
			Background(t.StatusBar).
			Foreground(readableText(t.Error, t.StatusBar, 4.5)).
			Bold(true).
			Padding(0, 1),
		StatusSpinner: lipgloss.NewStyle().
			Background(t.StatusBar).
			Foreground(t.Unread),
		StatusHint: lipgloss.NewStyle().
			Background(t.StatusBar).
			Foreground(readableText(t.StatusFg, t.StatusBar, 3.0)),
		// No padding: used for "  ·  " between status segments so gaps share the status bar BG.
		StatusBarJoiner: lipgloss.NewStyle().
			Background(t.StatusBar).
			Foreground(readableText(t.StatusFg, t.StatusBar, 4.5)),
		StatusNotice: lipgloss.NewStyle().
			Background(t.BorderFocus).
			Foreground(accentReadableOn(t.Fg, t.BorderFocus, 4.5)).
			Bold(true).
			Padding(0, 1),

		Overlay: lipgloss.NewStyle().
			Background(modalBg).
			Border(lipOverlayBorder(plainUI)).
			BorderForeground(modalBorder).
			Padding(modalPadTop, modalPadRight, modalPadBottom, modalPadLeft),
		OverlayTitle: lipgloss.NewStyle().
			Foreground(accentFg).
			Background(modalAccent).
			Bold(true).
			Padding(0, 1).
			MarginBottom(overlayTitleMB),
		OverlayHint: lipgloss.NewStyle().
			Background(modalBg).
			Foreground(modalMuted).
			MarginTop(func() int {
				if d == "compact" {
					return 0
				}
				return 1
			}()),

		InputFocused: lipgloss.NewStyle().
			Background(modalBg).
			Foreground(modalFg).
			Border(lipPaneBorder(plainUI)).
			BorderForeground(modalAccent).
			Padding(0, 1),
		InputUnfocused: lipgloss.NewStyle().
			Background(modalBg).
			Foreground(modalFg).
			Border(lipPaneBorder(plainUI)).
			BorderForeground(modalBorder).
			Padding(0, 1),
		InputLabel: lipgloss.NewStyle().
			Foreground(modalMuted),

		HelpSection: lipgloss.NewStyle().
			Background(helpSectionBg).
			Foreground(t.BorderFocus).
			Bold(true).
			Padding(0, 1),
		HelpSectionBody: lipgloss.NewStyle().
			Background(helpSectionBg).
			Foreground(helpSectionText).
			Padding(0, 1),
		HelpKey: lipgloss.NewStyle().
			Background(helpSectionBg).
			Foreground(t.Unread).
			Width(20),
		HelpDesc: lipgloss.NewStyle().
			Background(helpSectionBg).
			Foreground(helpSectionMuted),

		Spinner: lipgloss.NewStyle().
			Foreground(t.Unread),
	}
}
