package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHTMLUnorderedListKeepsBullet(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<ul><li>Alpha item</li><li>Beta item</li></ul>`, 72, CatppuccinMocha, false))
	if !strings.Contains(got, "• Alpha item") || !strings.Contains(got, "• Beta item") {
		t.Fatalf("expected bulleted list items, got %q", got)
	}
}

func TestHTMLOrderedListKeepsNumberSeparator(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<ol><li>First step</li><li>Second step</li></ol>`, 72, CatppuccinMocha, false))
	if !strings.Contains(got, "1. First step") || !strings.Contains(got, "2. Second step") {
		t.Fatalf("expected numbered items separated from their text, got %q", got)
	}
}

func TestTaskListCheckboxKeepsTrailingSpace(t *testing.T) {
	got := ansi.Strip(renderMarkdown("- [x] Done thing\n- [ ] Pending thing", 72, CatppuccinMocha, false))
	if !strings.Contains(got, "[x] Done thing") || !strings.Contains(got, "[ ] Pending thing") {
		t.Fatalf("expected a space between checkbox and label, got %q", got)
	}
}

func TestHeadingLevelsAreVisuallyDistinct(t *testing.T) {
	h1 := renderHTMLBody(`<h1>Section</h1>`, 72, CatppuccinMocha, false)
	h3 := renderHTMLBody(`<h3>Section</h3>`, 72, CatppuccinMocha, false)
	if ansi.Strip(h1) != ansi.Strip(h3) {
		t.Fatalf("expected identical text for both headings, got %q and %q", ansi.Strip(h1), ansi.Strip(h3))
	}
	if h1 == h3 {
		t.Fatalf("expected h1 and h3 to be styled differently, both rendered %q", h1)
	}
}

func TestHTMLBodyLinkEmitsHyperlink(t *testing.T) {
	const href = "https://example.com/confirm"
	got := renderHTMLBodyOpts(`<p>Please <a href="`+href+`">confirm your address</a> today.</p>`, 72, CatppuccinMocha, false, false)

	if !strings.Contains(got, "\x1b]8;;"+href) {
		t.Fatalf("expected an OSC 8 hyperlink for the body link, got %q", got)
	}
	if strings.Contains(ansi.Strip(got), href) {
		t.Fatalf("expected the destination to stay out of the body text, got %q", ansi.Strip(got))
	}
}

func TestHTMLBodyLinkOmitsHyperlinkWhenLinksFiltered(t *testing.T) {
	const href = "https://example.com/confirm"
	got := renderHTMLBodyOpts(`<p>Please <a href="`+href+`">confirm your address</a> today.</p>`, 72, CatppuccinMocha, false, true)

	if strings.Contains(got, "\x1b]8;;") {
		t.Fatalf("expected no hyperlink when link targets are filtered, got %q", got)
	}
	if !strings.Contains(ansi.Strip(got), "confirm your address") {
		t.Fatalf("expected filtered link to keep its text, got %q", ansi.Strip(got))
	}
}

// The rule used to pad every link with a space on each side, doubling the space
// whenever the surrounding text already supplied one.
func TestHTMLBodyLinkDoesNotDoubleSurroundingSpaces(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<p>You may <a href="https://example.com/u">unsubscribe</a> or stay.</p>`, 72, CatppuccinMocha, false))
	if !strings.Contains(got, "You may unsubscribe or stay.") {
		t.Fatalf("expected single spaces around the link text, got %q", got)
	}
}

// Marking the link text must not change where lines break: a marker measured
// during wrapping used to split "add partners" across two lines.
func TestHTMLBodyLinkDoesNotChangeWrapping(t *testing.T) {
	got := ansi.Strip(renderHTMLBody(`<p>You may <a href="https://example.com/u">unsubscribe</a> or <a href="https://example.com/a">add partners</a> who should receive messages.</p>`, 42, CatppuccinMocha, false))
	if !strings.Contains(got, "add partners") {
		t.Fatalf("expected link text to survive wrapping intact, got %q", got)
	}
}

func TestHTMLDataTableDoesNotUseCodeColors(t *testing.T) {
	got := renderHTMLBody(`<table><tr><th>Item</th><th>Cost</th></tr><tr><td>Widget</td><td>$4.00</td></tr></table>`, 60, CatppuccinMocha, false)

	if strings.Contains(got, ansiBackground(string(messageCodeBg(CatppuccinMocha)))) {
		t.Fatalf("expected a data table to drop the code background, got %q", got)
	}
	if !strings.Contains(ansi.Strip(got), "Widget") || !strings.Contains(ansi.Strip(got), "$4.00") {
		t.Fatalf("expected table contents to survive restyling, got %q", ansi.Strip(got))
	}
}

// ansiBackground renders a hex color as the SGR truecolor background parameters
// glamour emits, so a test can assert a specific background is absent.
func ansiBackground(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return "\x00"
	}
	var vals [3]int
	for i := 0; i < 3; i++ {
		var v int
		for _, c := range hex[i*2 : i*2+2] {
			v *= 16
			switch {
			case c >= '0' && c <= '9':
				v += int(c - '0')
			case c >= 'a' && c <= 'f':
				v += int(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v += int(c-'A') + 10
			}
		}
		vals[i] = v
	}
	return "48;2;" + itoa(vals[0]) + ";" + itoa(vals[1]) + ";" + itoa(vals[2])
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
