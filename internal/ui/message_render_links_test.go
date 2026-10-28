package ui

import (
	"fmt"
	"html"
	"reflect"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReaderLinksFollowSuccessfulRenderer(t *testing.T) {
	for _, tc := range []struct {
		name, html, plain, body string
		links                   []string
		labels                  map[string]string
	}{
		{
			name:   "multipart uses HTML tracking destinations",
			html:   `<p>HTML newsletter</p><a href="https://track.example/html?id=7&amp;action=view">Calendar</a>`,
			plain:  "Plain newsletter https://track.example/plain?id=9&action=view",
			body:   "HTML newsletter",
			links:  []string{"https://track.example/html?id=7&action=view"},
			labels: map[string]string{"https://track.example/html?id=7&action=view": "Calendar"},
		},
		{
			name:   "HTML retains bare URLs",
			html:   `<p>HTML newsletter https://example.com/bare</p><a href="https://example.com/anchor">Calendar</a>`,
			plain:  "Unused alternative https://example.com/unused",
			body:   "HTML newsletter",
			links:  []string{"https://example.com/anchor", "https://example.com/bare"},
			labels: map[string]string{"https://example.com/anchor": "Calendar"},
		},
		{
			name:  "plain text preserves URL order and exact duplicate handling",
			plain: "Plain newsletter https://example.com/first https://example.com/second https://example.com/first",
			body:  "Plain newsletter",
			links: []string{"https://example.com/first", "https://example.com/second"},
		},
		{
			name:  "HTML without links does not import plain URLs",
			html:  `<p>HTML newsletter without links</p>`,
			plain: "Unused alternative https://example.com/unused",
			body:  "HTML newsletter without links",
		},
		{
			name:  "empty HTML rendering falls back to plain links and labels",
			html:  `<a style="display:none" href="https://example.com/plain">Unused HTML label</a><a style="display:none" href="https://example.com/unused">Hidden action</a>`,
			plain: "Plain fallback https://example.com/plain",
			body:  "Plain fallback",
			links: []string{"https://example.com/plain"},
		},
		{
			name:  "plain fallback without links stays empty",
			html:  `<a style="display:none" href="https://example.com/unused">Hidden action</a>`,
			plain: "Plain fallback without links",
			body:  "Plain fallback without links",
		},
		{
			name: "no rendered body does not import hidden HTML URLs",
			html: `<a style="display:none" href="https://example.com/unused">Hidden action</a>`,
			body: "No message body.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TIDEMAIL_IMAGE_PROTOCOL", "none")
			m := cacheTestModel(t)
			// Link filtering affects body presentation, not actionable targets.
			for _, filter := range []bool{false, true} {
				m.cfg.Display.FilterLinks = filter
				msg := db.Message{ID: 1, BodyHTML: tc.html, BodyText: tc.plain}
				for pass := 0; pass < 2; pass++ { // cold rendering and cached result
					result := m.renderMessageForDisplay(msg, 72)
					if !strings.Contains(ansi.Strip(result.body), tc.body) {
						t.Fatalf("wrong displayed representation: %q", result.body)
					}
					if len(result.links) != len(tc.links) || (len(tc.links) > 0 && !reflect.DeepEqual(result.links, tc.links)) {
						t.Fatalf("links = %#v, want %#v", result.links, tc.links)
					}
					if len(result.linkLabels) != len(tc.labels) {
						t.Fatalf("labels = %#v, want %#v", result.linkLabels, tc.labels)
					}
					for target, label := range tc.labels {
						if result.linkLabels[target] != label {
							t.Fatalf("label for %q = %q, want %q", target, result.linkLabels[target], label)
						}
					}
				}
			}
		})
	}
}

func TestReaderMultipartNavigationUsesOnlyHTMLDestinations(t *testing.T) {
	var source, plain strings.Builder
	var targets []string
	// Model the UNO newsletter: distinct tracking tokens in each alternative,
	// with a footer longer than the viewport and repeated plain-text URLs.
	for i := 0; i < 41; i++ {
		target := fmt.Sprintf("https://track.example/html?id=%d&action=view", i)
		targets = append(targets, target)
		fmt.Fprintf(&source, `<p><a href="%s">Newsletter action %02d</a></p>`, html.EscapeString(target), i)
	}
	for i := 0; i < 39; i++ {
		fmt.Fprintf(&plain, "https://track.example/plain?id=%d&action=view\n", i%36)
	}
	m := readerLinksModel(t, source.String(), plain.String())
	if !reflect.DeepEqual(m.contentLinks, targets) {
		t.Fatalf("multipart targets = %#v, want HTML targets only", m.contentLinks)
	}
	footer := ansi.Strip(m.renderContentLinks(m.contentBodyWidth()))
	if strings.Contains(footer, "https://") || strings.Contains(footer, "id=") || !strings.Contains(footer, "Newsletter action 40") {
		t.Fatalf("unexpected footer labels: %q", footer)
	}
	originalOpen, originalCopy := browserOpen, clipboardWriteCmd
	t.Cleanup(func() { browserOpen, clipboardWriteCmd = originalOpen, originalCopy })
	var opened, copied string
	browserOpen = func(_, target string) error { opened = target; return nil }
	clipboardWriteCmd = func(text string) tea.Cmd { copied = text; return nil }
	for i := 0; i < len(targets); i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		m = next.(Model)
		assertReaderLinkSelection(t, m, i)
	}
	// Backward navigation and both wraparound directions retain visible focus.
	for _, step := range []struct {
		key   tea.KeyMsg
		index int
	}{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, 39},
		{tea.KeyMsg{Type: tea.KeyCtrlN}, 40},
		{tea.KeyMsg{Type: tea.KeyCtrlN}, 0},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}, 40},
	} {
		next, _ := m.Update(step.key)
		m = next.(Model)
		assertReaderLinkSelection(t, m, step.index)
		for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'o'}}} {
			_, cmd := m.Update(key)
			if cmd == nil {
				t.Fatal("activation produced no command")
			}
			cmd()
			if opened != targets[step.index] {
				t.Fatalf("opened %q, want %q", opened, targets[step.index])
			}
		}
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		if copied != targets[step.index] {
			t.Fatalf("copied %q, want %q", copied, targets[step.index])
		}
	}
}

func TestReaderRedditLinksUseSpecializedHTMLPolicy(t *testing.T) {
	t.Setenv("TIDEMAIL_IMAGE_PROTOCOL", "none")
	m := cacheTestModel(t)
	source := redditDigestFixture() + `<a href="https://example.com/html-extra">Unrelated HTML link</a>`
	msg := db.Message{ID: 1, From: "Reddit <noreply@redditmail.com>", BodyHTML: source, BodyText: "Plain alternative https://example.com/plain-extra"}
	result := m.renderMessageForDisplay(msg, 72)
	if !strings.Contains(ansi.Strip(result.body), "Read post") || strings.Contains(ansi.Strip(result.body), "Unrelated HTML link") {
		t.Fatalf("expected specialized Reddit body, got %q", result.body)
	}
	want := []string{"https://www.reddit.com/r/omarchy/comments/1vxc6xv/free_ai_in_omarchy/"}
	if !reflect.DeepEqual(result.links, want) {
		t.Fatalf("Reddit links = %#v, want %#v", result.links, want)
	}
}

func TestReaderThreadLinksMergeSelectedRepresentations(t *testing.T) {
	m := readerLinksModel(t, `<a href="https://example.com/shared">First HTML action</a>`, "Unused https://example.com/plain-alternative")
	first := m.messages[0]
	second := db.Message{ID: 2, Subject: first.Subject, BodyText: "Second message https://example.com/shared https://example.com/second"}
	thread := messageThread{Key: "test", Representative: first, Messages: []db.Message{first, second}, Count: 2}
	m.setViewportThread(thread)
	want := []string{"https://example.com/shared", "https://example.com/second"}
	if !reflect.DeepEqual(m.contentLinks, want) {
		t.Fatalf("thread links = %#v, want %#v", m.contentLinks, want)
	}
	if m.contentLinkLabels[want[0]] != "First HTML action" {
		t.Fatalf("thread lost HTML label: %#v", m.contentLinkLabels)
	}
	m.stepContentLink(1)
	m.stepContentLink(1)
	m.setViewportThread(thread)
	m.revealContentLink()
	assertReaderLinkSelection(t, m, 1)
}
