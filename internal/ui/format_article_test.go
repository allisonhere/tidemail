package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// The core guard against the content-loss bug: formatArticleParagraph used to
// switch on lines[0] and, for quotes and headings, render only that one line.
func TestFormatArticleParagraphKeepsEveryLine(t *testing.T) {
	cases := map[string][]string{
		"multi-line quote": {
			"> Can you send the report?",
			"> Also, when is the meeting?",
			"> Thanks",
		},
		"heading followed by body": {
			"# Quarterly results",
			"Revenue grew.",
			"Costs fell.",
		},
		"quote then reply": {
			"> Original question here",
			"Answer to the question.",
		},
		"heading then quote then prose": {
			"# Topic",
			"> quoted line",
			"trailing prose line",
		},
	}

	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			got := ansi.Strip(formatArticleParagraph(strings.Join(lines, "\n"), 60, CatppuccinMocha, true))
			for _, line := range lines {
				_, body, isQuote := splitQuotePrefix(line)
				if !isQuote {
					if text, _, isHeading := splitATXHeading(line); isHeading {
						body = text
					} else {
						body = line
					}
				}
				for _, word := range strings.Fields(body) {
					if !strings.Contains(got, word) {
						t.Fatalf("dropped %q from input line %q\ngot: %q", word, line, got)
					}
				}
			}
		})
	}
}

func TestFormatArticleParagraphPreservesQuoteDepth(t *testing.T) {
	cases := []struct {
		name  string
		input string
		bars  int
	}{
		{"single", "> one level", 1},
		{"double", ">> two levels", 2},
		{"spaced double", "> > two levels", 2},
		{"triple", ">>> three levels", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ansi.Strip(formatArticleParagraph(tc.input, 60, CatppuccinMocha, true))
			if n := strings.Count(got, "|"); n != tc.bars {
				t.Fatalf("want %d quote bars, got %d: %q", tc.bars, n, got)
			}
		})
	}
}

// The bar used to be prefixed before wrapping, so continuation lines of a long
// quote lost it and visually merged with the surrounding body text.
func TestFormatArticleParagraphBarsWrappedQuoteLines(t *testing.T) {
	long := "> " + strings.Repeat("quoted words that will certainly wrap ", 6)
	got := ansi.Strip(formatArticleParagraph(long, 30, CatppuccinMocha, true))
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected the quote to wrap over several lines, got %d: %q", len(lines), got)
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "| ") {
			t.Fatalf("line %d lost its quote bar: %q\nfull: %q", i, ln, got)
		}
	}
}

func TestFormatArticleHeadingDetection(t *testing.T) {
	cases := []struct {
		input     string
		isHeading bool
	}{
		{"# Real heading", true},
		{"### Also a heading", true},
		{"#1 priority", false},
		{"#hashtag", false},
		{"#!/bin/sh", false},
		{"#include <stdio.h>", false},
		{"###", false},
		{"####### too many", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			_, _, ok := splitATXHeading(tc.input)
			if ok != tc.isHeading {
				t.Fatalf("splitATXHeading(%q) ok=%v, want %v", tc.input, ok, tc.isHeading)
			}
			// Whatever the classification, the visible text must survive.
			got := ansi.Strip(formatArticleParagraph(tc.input, 60, CatppuccinMocha, true))
			if !tc.isHeading && !strings.Contains(got, strings.TrimSpace(tc.input)) {
				t.Fatalf("non-heading %q was altered: %q", tc.input, got)
			}
		})
	}
}

func TestFormatArticleBulletMarkerNotGreedy(t *testing.T) {
	cases := []struct {
		input string
		body  string
		ok    bool
	}{
		{"- item", "item", true},
		{"* item", "item", true},
		{"+ item", "item", true},
		{"- -5 degrees", "-5 degrees", true},
		{"--- separator", "", false},
		{"-", "", false},
		{"--", "", false},
		{"-no space", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			body, ok := splitBulletMarker(tc.input)
			if ok != tc.ok || body != tc.body {
				t.Fatalf("splitBulletMarker(%q) = (%q, %v), want (%q, %v)", tc.input, body, ok, tc.body, tc.ok)
			}
		})
	}
}

func TestLooksPreformatted(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"postal address", "Acme Corporation\n123 Main Street\nBoston, MA 02101", true},
		{"aligned table", "Widget    12    $4.00\nGadget     3   $12.50\nDoohickey  7    $9.25", true},
		{"indented code", "func main() {\n    fmt.Println(\"hi\")\n    return\n}", true},
		{"log lines", "2026-07-30 10:00:01 INFO started\n2026-07-30 10:00:02 WARN slow\n2026-07-30 10:00:03 INFO done", true},
		{"signature block", "-- \nJane Doe\nEngineering", true},
		{"forward marker", "----- Original Message -----\nFrom: someone\nSent: today", true},
		{"ruler", "Summary\n========\nAll good", true},
		{
			"prose wrapped at 72",
			"The quarterly report is attached for your review and comment before\n" +
				"we circulate it more widely to the rest of the leadership team at\n" +
				"the end of this week, so please read it carefully and reply soon\n" +
				"with any concerns.",
			false,
		},
		{
			"prose wrapped at 40",
			"The quarterly report is attached for\nyour review and comment before we\ncirculate it more widely to the team\nat the end of the week.",
			false,
		},
		{"two-line prose", "This is a short sentence.\nAnd here is another one.", false},
		{"single line", "Just one line of prose here.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksPreformatted(paragraphLines(tc.body), 100); got != tc.want {
				t.Fatalf("looksPreformatted(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestFormatArticleBodyPreservesPostalAddress(t *testing.T) {
	body := "Acme Corporation\n123 Main Street\nBoston, MA 02101"
	got := ansi.Strip(formatArticleBody(body, 100, CatppuccinMocha, true))
	if n := len(strings.Split(strings.TrimSpace(got), "\n")); n != 3 {
		t.Fatalf("address collapsed to %d lines, want 3: %q", n, got)
	}
}

func TestFormatArticleBodyReflowsHardWrappedProse(t *testing.T) {
	body := "The quarterly report is attached for your review and comment before\n" +
		"we circulate it more widely to the rest of the leadership team at\n" +
		"the end of this week, so please read it carefully and reply soon\n" +
		"with any concerns."
	got := ansi.Strip(formatArticleBody(body, 100, CatppuccinMocha, true))
	if n := len(strings.Split(strings.TrimSpace(got), "\n")); n >= 4 {
		t.Fatalf("prose was not reflowed: %d lines at width 100: %q", n, got)
	}
}

func TestFormatArticleBodyExpandsTabs(t *testing.T) {
	body := "Name\tQty\nWidget\t12\nGadget\t3"
	got := ansi.Strip(formatArticleBody(body, 100, CatppuccinMocha, true))
	if strings.Contains(got, "\t") {
		t.Fatalf("output still contains a tab: %q", got)
	}
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("tabbed block collapsed to %d lines, want 3: %q", len(lines), got)
	}
}

func TestFormatArticleOutputNeverExceedsWidth(t *testing.T) {
	const width = 40
	bodies := []string{
		"> " + strings.Repeat("quoted ", 30),
		"# " + strings.Repeat("heading ", 20),
		"- " + strings.Repeat("bullet ", 20),
		strings.Repeat("prose ", 40),
		"https://example.com/" + strings.Repeat("verylongpath", 12),
		"Acme Corporation\n123 Main Street\nBoston, MA 02101",
		"col1      col2      col3\naaaa      bbbb      cccc\ndddd      eeee      ffff",
	}
	for _, body := range bodies {
		got := formatArticleBody(body, width, CatppuccinMocha, true)
		for i, ln := range strings.Split(got, "\n") {
			if w := lipgloss.Width(ln); w > width {
				t.Fatalf("line %d is %d wide (max %d): %q", i, w, width, ansi.Strip(ln))
			}
		}
	}
}

// URLs inside quotes, headings and bullets were never highlighted, because
// link styling only ran in the prose branch.
func TestFormatArticleHighlightsLinksOutsideProse(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	cases := map[string]string{
		"quote":   "> see https://example.com for details",
		"bullet":  "- see https://example.com for details",
		"heading": "# see https://example.com",
		"prose":   "see https://example.com for details",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			got := formatArticleParagraph(body, 80, CatppuccinMocha, false)
			if got == ansi.Strip(got) {
				t.Fatalf("%s: URL was not styled: %q", name, got)
			}
		})
	}
}
