package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReaderViewportImmediateScrolling(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := readerFocusFixture(t, false)
	// Neither a visible focus nor a stale offscreen/pending focus can delay keys.
	for _, focus := range []int{4, 72, 119} {
		for _, pending := range []bool{false, true} {
			m.viewport.SetYOffset(70)
			m.contentFocusLine, m.contentFocusPending = focus, pending
			for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyUp, tea.KeyUp} {
				before := m.viewport.YOffset
				delta := 1
				if key == tea.KeyUp {
					delta = -1
				}
				m = apply(m, tea.KeyMsg{Type: key})
				if m.viewport.YOffset != before+delta {
					t.Fatal("invisible focus delayed viewport movement")
				}
				m = apply(m, readerOpenKey)
				m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			}
		}
	}
	if len(*requests) != 0 {
		t.Fatalf("viewport scrolling armed links: %v", *requests)
	}
}

func TestReaderViewportLinksTransition(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(fmt.Sprint(short), func(t *testing.T) {
			requests, apply := recordReaderActivations(t)
			body := "<p>Ordinary text.</p>"
			if !short {
				body = strings.Repeat(body, 100)
			}
			m := readerLinksModel(t, body+`<a href="https://example.com/one">One</a><a href="https://example.com/two">Two</a>`, "")
			m.cfg.Display.FocusLine = false
			m.viewport.GotoTop()
			for !m.viewport.AtBottom() {
				before := m.viewport.YOffset
				m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
				if m.viewport.YOffset != before+1 || m.contentLinkIdx != -1 || m.contentFocusedLink != "" {
					t.Fatal("scrolling entered LINKS before deliberate bottom press")
				}
			}
			if short && m.viewport.YOffset != 0 {
				t.Fatal("short fixture does not fit")
			}
			bottom := m.viewport.YOffset
			m = apply(m, readerOpenKey)
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			if len(*requests) != 0 {
				t.Fatal("reaching bottom armed activation")
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
			assertReaderLinkSelection(t, m, 0)
			// Existing local selected-row renderer supplies both highlight and chevron,
			// with no full-width reading-focus styling in this mode.
			rendered := strings.Split(m.renderContentFocusLine(m.viewport.View(), m.contentBodyWidth(), m.contentBodyHeight(), true), "\n")
			row := m.contentLinkRows[0] - m.viewport.YOffset
			if rendered[row] != m.renderContentLinkRow(0, m.contentBodyWidth(), true) {
				t.Fatal("selected LINKS styling changed")
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
			assertReaderLinkSelection(t, m, 1)
			m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
			assertReaderLinkSelection(t, m, 1)
			m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
			assertReaderLinkSelection(t, m, 0)
			before := m.viewport.YOffset
			m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
			if m.contentLinkIdx != -1 || m.contentFocusedLink != "" || m.viewport.YOffset != before {
				t.Fatal("Up did not exit LINKS without scrolling")
			}
			m = apply(m, readerOpenKey)
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
			if m.viewport.YOffset != max(0, before-1) {
				t.Fatal("Up did not resume viewport scrolling")
			}
			if len(*requests) != 0 {
				t.Fatal("navigation activated link")
			}
			if !short && bottom == 0 {
				t.Fatal("long fixture does not scroll")
			}
		})
	}
}

func TestReaderViewportWithoutLinks(t *testing.T) {
	m := readerFocusFixture(t, false)
	for i := 0; i < 200; i++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}
	if !m.viewport.AtBottom() || m.contentLinkIdx != -1 || m.contentFocusedLink != "" {
		t.Fatal("no-link message acquired selection")
	}
	before := m.viewport.YOffset
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	if m.viewport.YOffset != before-1 {
		t.Fatal("no-link message did not scroll up")
	}
}

func TestReaderViewportDirectSelectionResizeAndMessageChange(t *testing.T) {
	requests, apply := recordReaderActivations(t)
	m := readerActivationModel(t, false)
	// Seeing a footer entry before the bottom does not enter selection mode.
	m.viewport.SetYOffset(max(0, m.contentLinkRows[0]-m.contentBodyHeight()+1))
	if m.viewport.AtBottom() {
		t.Fatal("fixture needs a partially visible footer")
	}
	before := m.viewport.YOffset
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.viewport.YOffset != before+1 || m.contentLinkIdx != -1 {
		t.Fatal("partial footer changed scroll mode")
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
	assertReaderLinkSelection(t, m, 0)
	m = apply(m, tea.WindowSizeMsg{Width: 60, Height: 24})
	assertReaderLinkSelection(t, m, 0)
	m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
	assertReaderLinkSelection(t, m, 1)
	m = apply(m, readerWheel(m, true))
	if m.contentLinkIdx != -1 || m.contentFocusedLink != "" {
		t.Fatal("wheel left stale activation")
	}
	m = apply(m, readerOpenKey)
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = apply(m, readerPreviousKey)
	assertReaderLinkSelection(t, m, 1)
	// Replacing the selected message invalidates the shared selection context.
	m.messages[0].ID = 2
	m.filteredMessages = m.messages
	m.setViewportForCurrentRow()
	if m.contentLinkIdx != -1 || m.contentFocusedLink != "" {
		t.Fatal("message switch retained selection")
	}
	m = apply(m, readerOpenKey)
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(*requests) != 0 {
		t.Fatalf("stale activation: %v", *requests)
	}
}
