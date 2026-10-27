package ui

import (
	"fmt"
	"html"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestHTMLActionableLinksExcludeSourceResources(t *testing.T) {
	noiseOnly := `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><head><meta property="og:url" content="https://noise.example/og">
<style>p {background:url(https://noise.example/css)}</style><script>"https://noise.example/script"</script></head>
<body><!-- https://noise.example/comment --><noscript>https://noise.example/noscript</noscript>
<template>https://noise.example/template</template><p title="https://noise.example/title">Visible prose.</p></body></html>`
	if got := extractActionableLinksFromHTML(noiseOnly, ""); len(got) != 0 {
		t.Fatalf("source-only URLs became destinations: %#v", got)
	}
	source := `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">
<html xmlns="http://www.w3.org/1999/xhtml"><head>
<meta property="og:url" content="https://noise.example/og"><link href="https://noise.example/style.css" rel="stylesheet">
<style>.banner {background:url(https://noise.example/css)}</style>
<script>const url="https://noise.example/script"; const markup='<a href="https://noise.example/script-anchor">Hidden</a>';</script>
</head><body background="https://noise.example/background">
<!-- https://noise.example/comment <a href="https://noise.example/comment-anchor">Hidden</a> -->
<p title="https://noise.example/title" data-url="https://noise.example/data">Visible prose.</p>
<img src="https://noise.example/image.png" data-src="https://noise.example/lazy.png" alt="https://noise.example/alt">
<a href="https://example.com/action"><img src="https://noise.example/button.png" alt="Newsletter banner"></a>
<a href="https://example.com/photo.png">Photo</a><a href="https://example.com/report.pdf">Report</a>
<a href="http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">DTD reference</a>
<a href="javascript:alert(1)">Unsupported</a><a href="file:///tmp/report">Local file</a><a href="tel:123">Phone</a>
</body></html>`
	links, labels := extractHTMLLinks(source, "")
	want := []string{"https://example.com/action", "https://example.com/photo.png", "https://example.com/report.pdf", "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd"}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("links = %#v, want %#v", links, want)
	}
	if labels[want[0]] != "[image: Newsletter banner]" || labels[want[3]] != "DTD reference" {
		t.Fatalf("lost anchor descriptions: %#v", labels)
	}
	if links := extractActionableLinksFromHTML(`<img src="https://example.com/image.png" data-src="https://example.com/lazy.png">`, ""); len(links) != 0 {
		t.Fatalf("standalone image destinations: %#v", links)
	}
}

func TestHTMLMailtoRequiresExplicitSupportedAnchor(t *testing.T) {
	source := `<p>mailto:bare@example.com</p><a href="MAILTO:case@example.com?subject=Case%20preserved">Case</a>
<div style="mso-hide:all"><a href="mailto:fallback@example.com?subject=Approve&amp;body=Reply%0D%0A">Approve</a></div>
<a href="mailto:control@example.com?body=&#27;bad">Control</a>
<a href="mailto:">Empty</a><a href="javascript:alert(1)">Script</a>`
	links, labels := extractHTMLLinks(source, "")
	want := []string{"MAILTO:case@example.com?subject=Case%20preserved", "mailto:fallback@example.com?subject=Approve&body=Reply%0D%0A"}
	if !reflect.DeepEqual(links, want) || labels[want[1]] != "Approve" {
		t.Fatalf("links = %#v, labels = %#v", links, labels)
	}
	if got := extractActionableLinks("mailto:bare@example.com https://example.com/plain", ""); !reflect.DeepEqual(got, []string{"https://example.com/plain"}) {
		t.Fatalf("plain-text extraction changed: %#v", got)
	}
}

func TestHTMLBareURLsRespectVisibleTextBoundaries(t *testing.T) {
	source := `<p>https://example.com/<strong>inline</strong>?id=7&amp;action=view</p>
<p>https://example.com/paragraph-one</p><p>https://example.com/paragraph-two</p>
<table><tr><td>https://example.com/cell-one</td><td>https://example.com/cell-two</td></tr></table>
<div>https://example.com/break-one<br>https://example.com/break-two</div>
<p>https://example.com/comment<!-- https://noise.example/comment -->-joined</p>
<div hidden>https://noise.example/hidden</div><p aria-hidden="true">https://noise.example/aria</p>
<div style="display:none">https://noise.example/display</div><span style="visibility:hidden">https://noise.example/visibility</span>
<div style="font-size:0">https://noise.example/zero<span style="font-size:16px">https://example.com/restored</span></div>
<div style="mso-hide:all">https://noise.example/outlook-bare<a href="https://example.com/approve">Approve</a></div>`
	links, labels := extractHTMLLinks(source, "")
	want := []string{"https://example.com/inline?id=7&action=view", "https://example.com/paragraph-one", "https://example.com/paragraph-two", "https://example.com/cell-one", "https://example.com/cell-two", "https://example.com/break-one", "https://example.com/break-two", "https://example.com/comment-joined", "https://example.com/restored", "https://example.com/approve"}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("links = %#v, want %#v", links, want)
	}
	if labels[want[len(want)-1]] != "Approve" {
		t.Fatalf("lost fallback anchor label: %#v", labels)
	}
}

func TestHTMLBareURLsRespectAnchorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []string
	}{
		{
			"adjacent URL-valued anchors",
			`<a href="https://a.example/">https://a.example/</a><a href="https://b.example/">https://b.example/</a>`,
			[]string{"https://a.example/", "https://b.example/"},
		},
		{
			"bare URLs on either side of an anchor",
			`<p>https://before.example/<a href="https://anchor.example/">https://anchor.example/</a>https://after.example/</p>`,
			[]string{"https://anchor.example/", "https://before.example/", "https://after.example/"},
		},
		{
			"bare URL beside a descriptive anchor",
			`<p>https://bare.example/<a href="https://anchor.example/">View</a></p>`,
			[]string{"https://anchor.example/", "https://bare.example/"},
		},
		{
			"ordinary inline formatting stays continuous",
			`<p>https://<strong>example</strong>.<em>com</em>/<span>split</span>?id=7&amp;action=view</p>`,
			[]string{"https://example.com/split?id=7&action=view"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractActionableLinksFromHTML(tc.source, ""); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("links = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReaderMailtoActionsPreserveDestinations(t *testing.T) {
	approve := "mailto:workflow@example.com?subject=Approve%20invoice&body=" + strings.Repeat("synthetic%20", 300) + "%0D%0AEnd.!"
	deny := strings.Replace(approve, "Approve%20", "Deny%20", 1)
	literalEntity := "mailto:workflow@example.com?subject=Review&amp;body=literal%26value"
	var source strings.Builder
	for _, action := range []struct{ target, label string }{{approve, "Approve"}, {deny, "Deny"}, {"https://example.com/view", "View"}, {approve, "Approve again"}, {deny, "Deny again"}, {"mailto:help@example.com", ""}, {literalEntity, "Literal entity"}} {
		fmt.Fprintf(&source, `<p><a href="%s">%s</a></p>`, html.EscapeString(action.target), action.label)
	}
	m := readerLinksModel(t, source.String(), "Unused https://example.com/plain-alternative")
	want := []string{approve, deny, "https://example.com/view", "mailto:help@example.com", literalEntity}
	if !reflect.DeepEqual(m.contentLinks, want) {
		t.Fatalf("destinations = %#v, want %#v", m.contentLinks, want)
	}
	if m.contentLinkLabels[approve] != "Approve" || m.contentLinkLabels[deny] != "Deny" {
		t.Fatalf("stored labels were rewritten: %#v", m.contentLinkLabels)
	}
	footer := ansi.Strip(m.renderContentLinks(m.contentBodyWidth()))
	for _, label := range []string{"[mailto: Approve]", "[mailto: Deny]", "[mailto: email action]", "View"} {
		if !strings.Contains(footer, label) {
			t.Fatalf("missing %q in %q", label, footer)
		}
	}
	if strings.Contains(footer, "workflow@example.com") || strings.Contains(footer, "[mailto: View]") {
		t.Fatalf("unexpected footer: %q", footer)
	}
	originalOpen, originalCopy := browserOpen, clipboardWriteCmd
	t.Cleanup(func() { browserOpen, clipboardWriteCmd = originalOpen, originalCopy })
	var opened, copied string
	browserOpen = func(_, target string) error { opened = target; return nil }
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	for i := range want {
		// Wrap to the first item, then navigate forward through every action.
		if i == 0 {
			m.contentLinkIdx = len(want) - 1
		}
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		m = next.(Model)
		assertReaderLinkSelection(t, m, i)
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'o'}}} {
			_, cmd := m.Update(key)
			if cmd == nil {
				t.Fatal("missing open command")
			}
			cmd()
			if opened != want[i] {
				t.Fatalf("opened %q, want %q", opened, want[i])
			}
		}
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if copied != want[i] {
			t.Fatalf("copied %q, want %q", copied, want[i])
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true})
	m = next.(Model)
	assertReaderLinkSelection(t, m, len(want)-2)
}

func TestReaderMailtoLabelsFollowViewportAndNarrowWidths(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&source, `<p><a href="mailto:workflow@example.com?subject=Action%%20%d">Review synthetic approval action %02d</a></p>`, i, i)
	}
	m := readerLinksModel(t, source.String(), "")
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 120, Height: 32}} {
		next, _ := m.Update(size)
		m = next.(Model)
		for i := 0; i < 40; i++ {
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
			m = next.(Model)
			assertReaderLinkSelection(t, m, m.contentLinkIdx)
			line := m.contentLines[m.contentLinkRows[m.contentLinkIdx]]
			if !strings.Contains(line, "[mailto:") {
				t.Fatalf("lost bracketed label: %q", line)
			}
		}
		for _, line := range strings.Split(m.renderContentLinks(m.contentBodyWidth()), "\n") {
			if ansi.StringWidth(line) > m.contentBodyWidth() {
				t.Fatalf("footer exceeds width: %q", line)
			}
		}
	}
	// Exercise a label longer than the footer width without changing its target.
	view := m.renderContentLinks(20)
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > 20 {
			t.Fatalf("narrow footer exceeds width: %q", line)
		}
	}
}
