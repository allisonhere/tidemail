package ui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// contrastCheck describes a single foreground-on-background pair to verify.
type contrastCheck struct {
	name     string
	fg, bg   func(Styles) lipgloss.Color
	minRatio float64
}

// styleColor extracts a lipgloss.Color from a TerminalColor interface value.
// Returns "" if the value is not a lipgloss.Color (e.g. ANSI 256 colors).
func styleColor(c lipgloss.TerminalColor) lipgloss.Color {
	if v, ok := c.(lipgloss.Color); ok {
		return v
	}
	return ""
}

var contrastChecks = []contrastCheck{
	// Pane headers
	{
		name:     "PaneHeaderActive fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.PaneHeaderActive.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.PaneHeaderActive.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "PaneHeaderInactive fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.PaneHeaderInactive.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.PaneHeaderInactive.GetBackground()) },
		minRatio: 4.5,
	},

	// Status bar
	{
		name:     "StatusBar fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusBar.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusBar.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "StatusHint fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusHint.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusHint.GetBackground()) },
		minRatio: 3.0,
	},
	{
		name:     "StatusBarJoiner fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusBarJoiner.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusBarJoiner.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "StatusNotice fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusNotice.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusNotice.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "StatusError fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusError.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.StatusError.GetBackground()) },
		minRatio: 4.5,
	},

	// Article list
	{
		name:     "ArticleUnread fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ArticleUnread.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ArticleUnread.GetBackground()) },
		minRatio: 3.0,
	},
	{
		name:     "ArticleRead fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ArticleRead.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ArticleRead.GetBackground()) },
		minRatio: 2.8,
	},
	{
		name:     "ArticleSelected fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ArticleSelected.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ArticleSelected.GetBackground()) },
		minRatio: 4.5,
	},

	// Feed list
	{
		name:     "FeedItem fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.FeedItem.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.FeedItem.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "FeedItemSelected fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.FeedItemSelected.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.FeedItemSelected.GetBackground()) },
		minRatio: 4.5,
	},

	// Content
	{
		name:     "ContentTitle fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentTitle.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentTitle.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "ContentBody fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentBody.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentBody.GetBackground()) },
		minRatio: 4.5,
	},
	{
		name:     "ContentMeta fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentMeta.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentMeta.GetBackground()) },
		minRatio: 3.0,
	},
	{
		name:     "ContentFocusLine fg/bg",
		fg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentFocusLine.GetForeground()) },
		bg:       func(s Styles) lipgloss.Color { return styleColor(s.ContentFocusLine.GetBackground()) },
		minRatio: 4.5,
	},
}

func TestAllThemesPassContrastChecks(t *testing.T) {
	for _, theme := range BuiltinThemes {
		theme := theme
		t.Run(theme.Name, func(t *testing.T) {
			styles := BuildStyles(theme, "comfortable", "square")
			for _, check := range contrastChecks {
				fg := check.fg(styles)
				bg := check.bg(styles)
				if fg == "" || bg == "" {
					t.Errorf("%s: fg=%q bg=%q — one or both colors are unset", check.name, fg, bg)
					continue
				}
				ratio := contrastRatio(fg, bg)
				if ratio < check.minRatio {
					t.Errorf("%s: contrast %.2f:1 < %.1f:1  (fg=%s bg=%s)",
						check.name, ratio, check.minRatio, fg, bg)
				}
			}
		})
	}
}

func TestAllThemesContentFocusLineBackgroundIsVisible(t *testing.T) {
	for _, theme := range BuiltinThemes {
		theme := theme
		t.Run(theme.Name, func(t *testing.T) {
			styles := BuildStyles(theme, "comfortable", "square")
			focusBg := styleColor(styles.ContentFocusLine.GetBackground())
			if focusBg == "" {
				t.Fatal("content focus line background is unset")
			}
			if ratio := contrastRatio(focusBg, theme.Bg); ratio < 1.5 {
				t.Fatalf("content focus line background contrast %.2f:1 < 1.5:1 (focus bg=%s pane bg=%s)", ratio, focusBg, theme.Bg)
			}
		})
	}
}

func TestAllThemesMessageInlineRenderingColorsAreReadable(t *testing.T) {
	for _, theme := range BuiltinThemes {
		theme := theme
		t.Run(theme.Name, func(t *testing.T) {
			checks := []struct {
				name     string
				fg, bg   lipgloss.Color
				minRatio float64
			}{
				{name: "heading", fg: messageHeadingColor(theme), bg: theme.Bg, minRatio: 4.5},
				{name: "link", fg: messageLinkColor(theme), bg: theme.Bg, minRatio: 4.5},
				{name: "image", fg: messageImageColor(theme), bg: theme.Bg, minRatio: 3.0},
				{name: "quote", fg: messageMutedColor(theme), bg: theme.Bg, minRatio: 3.0},
				{name: "code", fg: messageCodeFg(theme), bg: messageCodeBg(theme), minRatio: 4.5},
			}
			for _, check := range checks {
				if ratio := contrastRatio(check.fg, check.bg); ratio < check.minRatio {
					t.Errorf("%s contrast %.2f:1 < %.1f:1 (fg=%s bg=%s)", check.name, ratio, check.minRatio, check.fg, check.bg)
				}
			}
		})
	}
}

func TestAccountPickerColorsAreReadableOnAllThemeBackgrounds(t *testing.T) {
	for _, theme := range BuiltinThemes {
		theme := theme
		t.Run(theme.Name, func(t *testing.T) {
			for _, color := range accountColorList {
				if color.Hex == "" {
					continue
				}
				accent := accentReadableOn(lipgloss.Color(color.Hex), theme.Bg, 3)
				if ratio := contrastRatio(accent, theme.Bg); ratio < 3 {
					t.Errorf("%s: contrast %.2f:1 < 3.0:1 (accent=%s adjusted=%s bg=%s)", color.Name, ratio, color.Hex, accent, theme.Bg)
				}
			}
		})
	}
}

func TestAllThemesFocusedPaneBorderMeetsContrastFloor(t *testing.T) {
	for _, theme := range BuiltinThemes {
		theme := theme
		t.Run(theme.Name, func(t *testing.T) {
			styles := BuildStyles(theme, "comfortable", "square")
			border := styleColor(styles.PaneFrame(true).GetBorderTopForeground())
			if border == "" {
				t.Fatal("focused pane border color is unset")
			}
			if ratio := contrastRatio(border, theme.Bg); ratio < paneFocusMinContrast {
				t.Errorf("focused pane border contrast %.2f:1 < %.1f:1 (border=%s bg=%s)", ratio, paneFocusMinContrast, border, theme.Bg)
			}
		})
	}
}

func TestAllThemesSelectedRowBackgroundStandsOut(t *testing.T) {
	for _, theme := range BuiltinThemes {
		theme := theme
		t.Run(theme.Name, func(t *testing.T) {
			styles := BuildStyles(theme, "comfortable", "square")
			selectedBg := styleColor(styles.ArticleSelected.GetBackground())
			if selectedBg == "" {
				t.Fatal("selected row background is unset")
			}
			if ratio := contrastRatio(selectedBg, theme.Bg); ratio < selectedBgMinContrast {
				t.Errorf("selected row background contrast %.2f:1 < %.1f:1 (selected=%s bg=%s)", ratio, selectedBgMinContrast, selectedBg, theme.Bg)
			}
		})
	}
}

// TestAllThemesContrastReport prints a human-readable table for all themes.
// Run with: go test -v -run TestAllThemesContrastReport
func TestAllThemesContrastReport(t *testing.T) {
	if !testing.Verbose() {
		t.Skip("only runs with -v")
	}
	for _, theme := range BuiltinThemes {
		styles := BuildStyles(theme, "comfortable", "square")
		t.Logf("── %s ──", theme.Name)
		for _, check := range contrastChecks {
			fg := check.fg(styles)
			bg := check.bg(styles)
			ratio := contrastRatio(fg, bg)
			pass := "✓"
			if ratio < check.minRatio {
				pass = "✗"
			}
			t.Logf("  %s %-30s  %.2f:1  (min %.1f:1)", pass, check.name, ratio, check.minRatio)
		}
	}
	fmt.Println() // keep linter happy
}
