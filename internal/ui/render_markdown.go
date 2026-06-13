package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

// tidemailDarkStyle is a glamour JSON theme that matches TideMail's terminal
// colors. It uses the current theme's accent (BorderFocus) for headings/links,
// dimmed text for blockquotes/code, and the pane background for document surface.
func tidemailDarkStyle(th Theme) string {
	bg := string(th.Bg)
	fg := string(th.Fg)
	accent := string(accentReadableOn(th.BorderFocus, th.Bg, 4.5))
	muted := string(readableText(th.Dimmed, th.Bg, 3.0))
	codeBg := string(messageCodeBg(th))
	codeFg := string(messageCodeFg(th))
	border := string(th.Border)

	// Glamour's theme format uses lipgloss adaptive colors.
	// We build a JSON document with our computed colors.
	// Using lipgloss.Color as-is works because glamour passes them through
	// to the underlying term renderer.
	return `{
	"document": {
		"style": "",
		"margin": 0,
		"background": "` + bg + `"
	},
	"block_quote": {
		"indent": 1,
		"indent_token": "│",
		"prefix": " ",
		"color": "` + muted + `",
		"background": "` + bg + `"
	},
	"paragraph": {
		"background": "` + bg + `"
	},
	"list": {
		"level_indent": 2,
		"background": "` + bg + `"
	},
	"heading": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"h1": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"h2": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"h3": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"h4": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"h5": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"h6": {
		"color": "` + accent + `",
		"bold": true,
		"background": "` + bg + `"
	},
	"text": {
		"color": "` + fg + `",
		"background": "` + bg + `"
	},
	"bold": {
		"bold": true,
		"background": "` + bg + `"
	},
	"italic": {
		"italic": true,
		"background": "` + bg + `"
	},
	"strikethrough": {
		"crossed_out": true,
		"background": "` + bg + `"
	},
	"link": {
		"color": "` + accent + `",
		"underline": true,
		"background": "` + bg + `"
	},
	"link_text": {
		"color": "` + accent + `",
		"underline": true,
		"background": "` + bg + `"
	},
	"code": {
		"color": "` + codeFg + `",
		"background": "` + codeBg + `"
	},
	"code_block": {
		"color": "` + codeFg + `",
		"background": "` + codeBg + `",
		"margin": 0,
		"chroma": {
			"text": {
				"color": "` + codeFg + `"
			},
			"background": {
				"color": "` + codeBg + `"
			}
		}
	},
	"table": {
		"style": "color:` + muted + `",
		"background": "` + bg + `"
	},
	"table_separator": {
		"style": "color:` + muted + `",
		"background": "` + bg + `"
	},
	"hr": {
		"color": "` + border + `",
		"background": "` + bg + `"
	},
	"item": {
		"block_prefix": "",
		"block_suffix": "",
		"background": "` + bg + `"
	},
	"enumeration": {
		"block_prefix": "",
		"block_suffix": "",
		"background": "` + bg + `"
	},
	"task": {
		"ticked": "[x]",
		"unticked": "[ ]",
		"background": "` + bg + `"
	},
	"image": {
		"color": "` + muted + `",
		"background": "` + bg + `"
	},
	"image_text": {
		"color": "` + muted + `",
		"background": "` + bg + `"
	}
}` + "\n"
}

func renderMarkdown(src string, width int, th Theme, plainUI bool) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	if plainUI {
		// Plain UI: render without ANSI styles.
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(styles.NoTTYStyleConfig),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return src
		}
		out, err := r.Render(src)
		if err != nil {
			return src
		}
		return strings.TrimSpace(out)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes([]byte(tidemailDarkStyle(th))),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return src
	}
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	return wrapWithBackground(strings.TrimSpace(out), string(th.Bg), width)
}

// wrapWithBackground ensures every line has the correct background color.
// glamour's output may have ANSI sequences that reset the background mid-line;
// wrapping each line in the background style guarantees consistent coloring.
func wrapWithBackground(s string, bg string, width int) string {
	if s == "" || bg == "" {
		return s
	}
	bgStyle := "\033[48;2;" + hexToAnsiBg(bg) + "m"
	var result []string
	for _, line := range strings.Split(s, "\n") {
		result = append(result, bgStyle+line+"\033[0m")
	}
	return strings.Join(result, "\n")
}

// hexToAnsiBg converts a hex color like "#1a1b26" to ANSI 24-bit background
// parameters like "26;27;28" for use in \033[48;2;R;G;Bm.
func hexToAnsiBg(hex string) string {
	if len(hex) != 7 || hex[0] != '#' {
		return ""
	}
	r := hexToByte(hex[1:3])
	g := hexToByte(hex[3:5])
	b := hexToByte(hex[5:7])
	return fmt.Sprintf("%d;%d;%d", r, g, b)
}

func hexToByte(s string) int {
	var v int
	for _, c := range s {
		v *= 16
		switch {
		case c >= '0' && c <= '9':
			v += int(c - '0')
		case c >= 'a' && c <= 'f':
			v += int(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v += int(c - 'A' + 10)
		}
	}
	return v
}

// enforceWidth ensures no rendered line exceeds the given width by hard-breaking
// long unbroken strings (URLs, code, etc.) that glamour's word-wrap leaves intact.
func enforceWidth(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}
	return ansi.Hardwrap(s, width, false)
}
