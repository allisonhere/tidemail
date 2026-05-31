package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tide/internal/config"
)

type Styles struct {
	Theme Theme
	// PlainUI is true for the vt52 theme (ASCII borders and glyphs).
	PlainUI bool
	// Density is normalized ("comfortable" | "compact") and matches config.Display.Density.
	Density string

	// Pane containers
	FeedsPane          lipgloss.Style
	ArticlesPane       lipgloss.Style
	ContentPane        lipgloss.Style
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

func BuildStyles(t Theme, density string) Styles {
	plainUI := t.Name == ThemeNameVT52
	d := config.NormalizeDisplayDensity(density)
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

	paneBase := lipgloss.NewStyle().
		Background(t.Bg).
		BorderForeground(t.Border)

	focusedPane := lipgloss.NewStyle().
		Background(t.Bg).
		BorderForeground(t.BorderFocus)

	selectedBg := func() lipgloss.Color {
		if isDark(t.Bg) {
			return adjustLightness(t.Bg, 0.12)
		}
		return adjustLightness(t.Bg, -0.12)
	}()
	selectedBgSoft := func() lipgloss.Color {
		if isDark(t.Bg) {
			return adjustLightness(t.Bg, 0.06)
		}
		return adjustLightness(t.Bg, -0.06)
	}()
	contentFocusBg := focusLineBg(t)

	unreadBg := func() lipgloss.Color {
		if isDark(t.Bg) {
			return adjustLightness(t.Bg, 0.06)
		}
		return adjustLightness(t.Bg, -0.06)
	}()

	return Styles{
		Theme:   t,
		PlainUI: plainUI,
		Density: d,

		FeedsPane: paneBase.
			Border(lipPaneBorder(plainUI), false, true, false, false).
			BorderBackground(t.Bg).
			AlignVertical(lipgloss.Top),
		ArticlesPane: focusedPane.
			Border(lipPaneBorder(plainUI), false, false, true, false).
			BorderBackground(t.Bg).
			AlignVertical(lipgloss.Top),
		ContentPane: lipgloss.NewStyle().
			Background(t.Bg),
		PaneHeaderActive: lipgloss.NewStyle().
			Background(t.BorderFocus).
			Foreground(readableText(t.Fg, t.BorderFocus, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left),
		PaneHeaderInactive: lipgloss.NewStyle().
			Background(t.Border).
			Foreground(readableText(t.Fg, t.Border, 4.5)).
			AlignHorizontal(lipgloss.Left),

		FeedItem: listPad(lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(t.Fg).
			AlignHorizontal(lipgloss.Left)),
		FeedItemSelected: listPad(lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(readableText(t.BorderFocus, selectedBg, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		FeedItemSelectedFocused: listPad(lipgloss.NewStyle().
			Background(selectedBg).
			Foreground(readableText(t.BorderFocus, selectedBg, 4.5)).
			Bold(true).
			AlignHorizontal(lipgloss.Left)),
		FeedItemSelectedUnfocused: listPad(lipgloss.NewStyle().
			Background(selectedBgSoft).
			Foreground(readableText(t.BorderFocus, selectedBgSoft, 4.5)).
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
			Background(func() lipgloss.Color {
				if isDark(t.Bg) {
					return adjustLightness(t.Bg, 0.12)
				}
				return adjustLightness(t.Bg, -0.12)
			}()).
			Foreground(readableText(t.BorderFocus, func() lipgloss.Color {
				if isDark(t.Bg) {
					return adjustLightness(t.Bg, 0.12)
				}
				return adjustLightness(t.Bg, -0.12)
			}(), 4.5)).
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
			Foreground(readableText(t.Bg, t.BorderFocus, 4.5)).
			Bold(true).
			Padding(0, 1).
			MarginBottom(contentTitleMB),
		ContentMeta: lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(readableText(t.Dimmed, t.Bg, 3.0)).
			Italic(true),
		ContentBody: lipgloss.NewStyle().
			Background(t.Bg).
			Foreground(t.Fg),
		ContentFocusLine: lipgloss.NewStyle().
			Background(contentFocusBg).
			Foreground(readableText(t.Fg, contentFocusBg, 4.5)),
		SearchMatch: lipgloss.NewStyle().
			Background(t.BorderFocus).
			Foreground(readableText(t.Fg, t.BorderFocus, 4.5)),

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
			Foreground(readableText(t.Fg, t.BorderFocus, 4.5)).
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
