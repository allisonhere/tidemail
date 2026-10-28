package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

var readerOpenKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}
var readerPreviousKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}, Alt: true}

func recordReaderActivations(t *testing.T) (*[]string, func(Model, tea.Msg) Model) {
	t.Helper()
	original := browserOpen
	t.Cleanup(func() { browserOpen = original })
	var requests []string
	browserOpen = func(_, target string) error {
		requests = append(requests, target)
		return nil
	}
	return &requests, func(m Model, msg tea.Msg) Model {
		next, cmd := m.Update(msg)
		if cmd != nil {
			// Reader navigation returns no background commands in these fixtures.
			// Execute activation commands through the mock, never a real opener.
			if result := cmd(); result != nil {
				if _, ok := result.(BrowserOpenedMsg); !ok {
					t.Fatalf("unexpected reader command result %T", result)
				}
			}
		}
		return next.(Model)
	}
}

func readerActivationModel(t *testing.T, focus bool) Model {
	t.Helper()
	// Ordinary visible text AND an extracted link elsewhere: the old no-link
	// fixtures never exercised fallback to an implicitly selected destination.
	m := readerLinksModel(t, `<p>Ordinary visible body text.</p><p>Another ordinary line.</p><p><a href="https://example.com/first">First action</a> and <a href="https://example.com/second">Second action</a></p>`+strings.Repeat("<p>Ordinary trailing paragraph.</p>", 40), "")
	m.cfg.Display.FocusLine = focus
	m.cfg.Display.MouseSupport = true
	return m
}

func TestReaderActivationOrdinaryInteractions(t *testing.T) {
	for _, focus := range []bool{false, true} {
		t.Run(fmt.Sprint(focus), func(t *testing.T) {
			requests, apply := recordReaderActivations(t)
			m := readerActivationModel(t, focus)
			for i := 0; i < 3; i++ {
				m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
				m = apply(m, readerOpenKey)
			}
			if len(*requests) != 0 {
				t.Fatalf("fresh message activated %v", *requests)
			}
			if m.contentLinkIdx != -1 {
				t.Fatalf("fresh message selected link %d", m.contentLinkIdx)
			}
			// Click every cell in the content pane, including its headers, link
			// text, whitespace and all borders. No line-level click may open.
			for y := m.articlesPaneOuterHeight(); y < m.mainHeight(); y++ {
				for x := m.feedsPaneWidth(); x < m.width; x++ {
					m = apply(m, leftClick(x, y))
				}
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = apply(m, readerOpenKey)
			if len(*requests) != 0 {
				t.Fatalf("ordinary interaction activated %v", *requests)
			}
		})
	}
}

func TestReaderActivationExplicitSelection(t *testing.T) {
	for _, focus := range []bool{false, true} {
		for _, previous := range []bool{false, true} {
			t.Run(fmt.Sprintf("focus=%v/previous=%v", focus, previous), func(t *testing.T) {
				requests, apply := recordReaderActivations(t)
				m := readerActivationModel(t, focus)
				key, index := tea.KeyMsg{Type: tea.KeyCtrlN}, 0
				if previous {
					key, index = readerPreviousKey, len(m.contentLinks)-1
				}
				m = apply(m, key)
				assertReaderLinkSelection(t, m, index)
				target := m.contentLinks[index]
				for _, key := range []tea.KeyMsg{readerOpenKey, {Type: tea.KeyEnter}, readerOpenKey} {
					m = apply(m, key)
				}
				if !reflect.DeepEqual(*requests, []string{target, target, target}) {
					t.Fatalf("intentional activations = %v", *requests)
				}
			})
		}
	}
}

func TestReaderActivationClearsAfterMouseAndBodyNavigation(t *testing.T) {
	for _, focus := range []bool{false, true} {
		for _, action := range []string{"click", "wheel", "up", "down"} {
			t.Run(fmt.Sprintf("focus=%v/%s", focus, action), func(t *testing.T) {
				requests, apply := recordReaderActivations(t)
				m := readerActivationModel(t, focus)
				m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
				msg := tea.Msg(tea.KeyMsg{Type: tea.KeyUp})
				switch action {
				case "click":
					msg = leftClick(m.width-1, m.articlesPaneOuterHeight())
				case "wheel":
					msg = tea.MouseMsg{X: m.width - 2, Y: m.mainHeight() - 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}
				case "down":
					// The final footer row has no following link to deliberately focus.
					m = apply(m, readerPreviousKey)
					msg = tea.KeyMsg{Type: tea.KeyDown}
				}
				m = apply(m, msg)
				// Up may deliberately focus a body URL; test invalidation of the
				// extracted-list selection independently of that valid workflow.
				if m.contentLinkIdx != -1 {
					t.Fatal("ordinary navigation retained extracted selection")
				}
				if action == "click" || action == "wheel" || !focus {
					m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
					m = apply(m, readerOpenKey)
					if len(*requests) != 0 {
						t.Fatalf("stale activation: %v", *requests)
					}
				}
			})
		}
	}
}

func TestReaderActivationDeliberatelyFocusedBodyLink(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := readerLinksModel(t, "", "Ordinary body text.\n\nhttps://example.com/first and https://example.com/second\n\n"+strings.Repeat("Trailing ordinary paragraph.\n\n", 40))
	// Navigate real body lines, including a line containing multiple links.
	for attempts := 0; attempts < m.contentLineCount; attempts++ {
		m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
		if target, ok := m.focusedLineLink(); ok {
			if target != "https://example.com/first" {
				t.Fatalf("multiple-link body line resolved %q", target)
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = apply(m, readerOpenKey)
			if !reflect.DeepEqual(*requests, []string{target, target}) {
				t.Fatalf("focused activations = %v, want %q twice", *requests, target)
			}
			// Moving that focus out of view without explicit link navigation
			// must never leave a hidden body target available to Enter.
			m.viewport.GotoBottom()
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			if len(*requests) != 2 {
				t.Fatalf("offscreen focus activated %v", *requests)
			}
			return
		}
	}
	t.Fatal("fixture has no navigable body link")
}

func TestReaderActivationMessageAndThreadIdentity(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := readerActivationModel(t, true)
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	first := m.messages[0]
	second := first
	second.ID = 2 // identical destinations must not transfer intent
	m.messages, m.filteredMessages = []db.Message{second}, []db.Message{second}
	m.setViewportMessage(second)
	m = apply(m, readerOpenKey)
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.contentLinkIdx != -1 || len(*requests) != 0 {
		t.Fatalf("message inherited selection: index=%d requests=%v", m.contentLinkIdx, *requests)
	}
	thread := messageThread{Key: "one", Representative: second, Messages: []db.Message{second}, Count: 1}
	m.setViewportThread(thread)
	m.stepContentLink(1)
	m.setViewportThread(thread)
	m.revealContentLink()
	thread.Key = "two" // even with the same representative
	m.setViewportThread(thread)
	m = apply(m, readerOpenKey)
	if m.contentLinkIdx != -1 || len(*requests) != 0 {
		t.Fatalf("thread inherited selection: index=%d requests=%v", m.contentLinkIdx, *requests)
	}
}

func TestReaderActivationDestinationsAndResize(t *testing.T) {
	for _, fixture := range []struct{ name, html, plain, target string }{
		{"zero", "", "Ordinary body text.", ""},
		{"anchor", `<p>Ordinary body text.</p><a href="https://example.com/anchor">Action</a>`, "", "https://example.com/anchor"},
		{"image", `<p>Ordinary body text.</p><a href="https://example.com/image"><img src="cid:banner" alt="Banner"></a>`, "", "https://example.com/image"},
		{"plain", "", "Ordinary body text.\n\nhttps://example.com/plain", "https://example.com/plain"},
		{"initial-url", "", "https://example.com/initial", "https://example.com/initial"},
		{"mailto", `<p>Ordinary body text.</p><a href="mailto:action@example.com?subject=Review&amp;body=Please%20review">Email action</a>`, "", "mailto:action@example.com?subject=Review&body=Please%20review"},
	} {
		for _, focus := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/focus=%v", fixture.name, focus), func(t *testing.T) {
				requests, apply := recordReaderActivations(t)
				m := readerLinksModel(t, fixture.html, fixture.plain)
				m.cfg.Display.FocusLine = focus
				m = apply(m, readerOpenKey)
				m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
				if len(*requests) != 0 {
					t.Fatalf("fresh activation: %v", *requests)
				}
				m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
				m = apply(m, tea.WindowSizeMsg{Width: 60, Height: 24})
				m = apply(m, readerOpenKey)
				if fixture.target == "" {
					if len(*requests) != 0 {
						t.Fatalf("zero-link activation: %v", *requests)
					}
				} else if !reflect.DeepEqual(*requests, []string{fixture.target}) {
					t.Fatalf("activation = %v, want %q", *requests, fixture.target)
				}
				if strings.Contains(fixture.name, "mailto") && m.overlay == overlayCompose {
					t.Fatal("mailto unexpectedly opened internal compose")
				}
			})
		}
	}
}

func TestReaderActivationEmptyContentClicks(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := readerActivationModel(t, true)
	m.clearViewportMessage()
	for _, point := range [][2]int{{m.feedsPaneWidth(), m.articlesPaneOuterHeight()}, {m.width - 1, m.mainHeight() - 1}, {m.width - 2, m.mainHeight() - 2}} {
		m = apply(m, leftClick(point[0], point[1]))
		m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = apply(m, readerOpenKey)
	}
	if len(*requests) != 0 {
		t.Fatalf("empty content activated %v", *requests)
	}
}
