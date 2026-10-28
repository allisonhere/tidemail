package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func readerFocusFixture(t *testing.T, highlight bool) Model {
	t.Helper()
	m := mouseTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	m = next.(Model)
	m.cfg.Display.FocusLine = highlight
	m.focused = paneContent
	m.filteredMessages = []db.Message{{ID: 1, Subject: "Focus fixture"}}
	m.contentMessageID = 1
	m.contentLines = []string{"Subject", "From: sender@example.com", "", ""}
	for i := 4; i < 120; i++ {
		line := fmt.Sprintf("Body line %d", i)
		if i >= 30 && i < 60 {
			line = ""
		}
		m.contentLines = append(m.contentLines, line)
	}
	m.contentLines[80] = "https://example.com/focused"
	m.contentLineLinks = make([]string, len(m.contentLines))
	m.contentLineCount = len(m.contentLines)
	m.contentFocusable = focusableFromLines(m.contentLines)
	m.viewport.SetContent(strings.Join(m.contentLines, "\n"))
	m.contentFocusLine = firstFocusableLine(m.contentFocusable)
	return m
}

func readerWheel(m Model, up bool) tea.MouseMsg {
	button := tea.MouseButtonWheelDown
	if up {
		button = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{X: m.width - 2, Y: m.mainHeight() - 2, Action: tea.MouseActionPress, Button: button}
}

func assertReaderFocusVisible(t *testing.T, m Model) {
	t.Helper()
	if !m.contentLineVisible(m.contentFocusLine) {
		t.Fatalf("focus %d outside viewport [%d,%d)", m.contentFocusLine, m.viewport.YOffset, m.viewport.YOffset+m.contentBodyHeight())
	}
	if !m.contentFocusPending && !m.contentFocusable[m.contentFocusLine] {
		t.Fatalf("nonfocusable line %d became active focus", m.contentFocusLine)
	}
}

func TestReaderWheelFocusContinuity(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		for _, up := range []bool{false, true} {
			t.Run(fmt.Sprintf("highlight=%v/up=%v", highlight, up), func(t *testing.T) {
				requests, apply := recordReaderActivations(t)
				m := readerFocusFixture(t, highlight)
				m.viewport.SetYOffset(70)
				if up {
					m.contentFocusLine = 74
				} else {
					m.contentFocusLine = 70
				}
				expected := m.viewport
				if up {
					expected.ScrollUp(wheelStepRows)
				} else {
					expected.ScrollDown(wheelStepRows)
				}
				m = apply(m, readerWheel(m, up))
				if m.viewport.YOffset != expected.YOffset {
					t.Fatal("focus reconciliation moved the viewport or changed wheel sensitivity")
				}
				assertReaderFocusVisible(t, m)
				m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
				m = apply(m, readerOpenKey)
				if len(*requests) != 0 {
					t.Fatalf("wheel armed a link: %v", *requests)
				}
				// Test both continuing and reversing direction from the same view.
				for _, delta := range []int{-1, 1} {
					key := tea.KeyDown
					if delta < 0 {
						key = tea.KeyUp
					}
					want := nextContentFocusLine(m.contentFocusLine, delta, m.contentFocusable, m.contentLineCount)
					navigated := apply(m, tea.KeyMsg{Type: key})
					if !highlight {
						if navigated.viewport.YOffset != m.viewport.YOffset+delta {
							t.Fatal("viewport mode did not scroll immediately after wheel")
						}
					} else if navigated.contentFocusLine != want {
						t.Fatalf("keyboard resumed at %d, want %d", navigated.contentFocusLine, want)
					}
					assertReaderFocusVisible(t, navigated)
				}
			})
		}
	}
}

func TestReaderWheelRetainsVisibleFocus(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		m := readerFocusFixture(t, highlight)
		m.contentFocusLine = 8
		m.viewport.SetYOffset(5)
		next, _ := m.Update(readerWheel(m, false))
		m = next.(Model)
		if m.contentFocusLine != 8 || m.viewport.YOffset != 8 {
			t.Fatalf("visible focus moved: %+v", scrollState(m))
		}
	}
}

func TestReaderBlankViewportAndBoundaries(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		for _, up := range []bool{false, true} {
			t.Run(fmt.Sprintf("highlight=%v/up=%v", highlight, up), func(t *testing.T) {
				m := readerFocusFixture(t, highlight)
				m.viewport.SetYOffset(35)
				m.contentFocusLine = 29 // wheel moves away into a long blank section
				next, _ := m.Update(readerWheel(m, false))
				m = next.(Model)
				if !m.contentFocusPending || m.viewport.YOffset != 38 {
					t.Fatal("blank viewport revived offscreen focus")
				}
				assertReaderFocusVisible(t, m)
				for i := 0; i < 40 && m.contentFocusPending; i++ {
					before := m.viewport.YOffset
					key, delta := tea.KeyDown, 1
					if up {
						key, delta = tea.KeyUp, -1
					}
					next, _ := m.Update(tea.KeyMsg{Type: key})
					m = next.(Model)
					if m.viewport.YOffset != before+delta {
						t.Fatalf("blank navigation jumped from %d to %d", before, m.viewport.YOffset)
					}
					assertReaderFocusVisible(t, m)
				}
				if m.contentFocusPending {
					t.Fatal("keyboard never reached visible body content")
				}
				// Top and bottom wheel clamping must not recover an old location.
				for _, bottom := range []bool{false, true} {
					if bottom {
						m.viewport.GotoBottom()
					} else {
						m.viewport.GotoTop()
					}
					m.reconcileContentFocus(1)
					offset := m.viewport.YOffset
					next, _ := m.Update(readerWheel(m, !bottom))
					m = next.(Model)
					if m.viewport.YOffset != offset {
						t.Fatal("wheel escaped document boundary")
					}
					assertReaderFocusVisible(t, m)
				}
			})
		}
	}
}

func TestReaderMixedNavigationActivationSafety(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		requests, apply := recordReaderActivations(t)
		m := readerFocusFixture(t, highlight)
		for i := 0; i < 100; i++ {
			var event tea.Msg = readerWheel(m, i >= 50)
			if i%3 == 0 {
				event = tea.KeyMsg{Type: tea.KeyDown}
			}
			if i%7 == 0 {
				event = tea.KeyMsg{Type: tea.KeyUp}
			}
			m = apply(m, event)
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.contentFocusedLink == "" && len(*requests) != 0 {
				t.Fatal("automatic focus armed a link")
			}
			*requests = nil
		}
	}
}

func TestReaderResizeAfterWheel(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		m := readerScrollModel(21*1024, 120, 40, highlight)
		for i := 0; i < 20; i++ {
			next, _ := m.Update(readerWheel(m, false))
			m = next.(Model)
		}
		offset := m.viewport.YOffset
		for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 160, Height: 50}} {
			next, _ := m.Update(size)
			m = next.(Model)
			if m.viewport.YOffset != offset {
				t.Fatalf("resize revived old position: got %d want %d", m.viewport.YOffset, offset)
			}
			assertReaderFocusVisible(t, m)
			assertReaderPanesMatchFresh(t, m)
		}
	}
}

func TestReaderWheelClearsLinkSelectionWithoutArmingFocus(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		requests, apply := recordReaderActivations(t)
		m := readerActivationModel(t, highlight)
		m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
		m.contentSearchQuery, m.contentSearchMatches = "ordinary", []int{4, 8}
		m = apply(m, readerWheel(m, true))
		if m.contentLinkIdx != -1 || m.contentFocusedLink != "" {
			t.Fatal("wheel retained activation intent")
		}
		if m.contentSearchQuery != "ordinary" || len(m.contentSearchMatches) != 2 {
			t.Fatal("wheel changed search state")
		}
		for _, row := range m.contentLinkRows {
			if strings.HasPrefix(strings.TrimSpace(m.contentLines[row]), ">") {
				t.Fatal("wheel retained selection chevron")
			}
		}
		m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = apply(m, readerOpenKey)
		if len(*requests) != 0 {
			t.Fatalf("wheel activated %v", *requests)
		}
	}
}

func TestReaderHeaderOnlyViewport(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		m := readerFocusFixture(t, highlight)
		m.height = 12
		m.viewport.Height = m.contentBodyHeight()
		m.contentFocusLine = 8
		next, _ := m.Update(readerWheel(m, true))
		m = next.(Model)
		if m.viewport.YOffset != 0 || !m.contentFocusPending {
			t.Fatal("header-only viewport revived a body focus or changed position")
		}
		body := m.viewport.View()
		if m.renderContentFocusLine(body, m.contentBodyWidth(), m.contentBodyHeight(), true) != body {
			t.Fatal("header-only viewport painted a nonfocusable line")
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
		if m.viewport.YOffset != 1 || m.contentFocusLine != 4 || m.contentFocusPending {
			t.Fatalf("keyboard did not continue into visible body: %+v", scrollState(m))
		}
	}
}

func TestReaderExplicitLinkNavigationLeavesPendingFocus(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		requests, apply := recordReaderActivations(t)
		m := readerActivationModel(t, highlight)
		// Blank/header-only viewports may defer focus. Explicit link navigation
		// must still reveal the target instead of leaving it selected offscreen.
		m.contentFocusPending = true
		m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
		if m.contentFocusPending {
			t.Fatal("explicit link navigation retained pending focus")
		}
		assertReaderLinkSelection(t, m, 0)
		m = apply(m, readerOpenKey)
		if !reflect.DeepEqual(*requests, []string{m.contentLinks[0]}) {
			t.Fatalf("explicit pending-state activation = %v", *requests)
		}
	}
}

func TestReaderNonfocusableHeaderNeverArmsLink(t *testing.T) {
	for _, highlight := range []bool{false, true} {
		requests, apply := recordReaderActivations(t)
		m := readerFocusFixture(t, highlight)
		m.contentLines = []string{"https://example.com/header", "Sender", ""}
		m.contentLineCount = len(m.contentLines)
		m.contentFocusable = focusableFromLines(m.contentLines)
		m.viewport.SetContent(strings.Join(m.contentLines, "\n"))
		m.contentFocusLine = 0
		for _, key := range []tea.KeyMsg{{Type: tea.KeyUp}, {Type: tea.KeyDown}, readerOpenKey, {Type: tea.KeyEnter}} {
			m = apply(m, key)
		}
		if len(*requests) != 0 {
			t.Fatalf("nonfocusable header activated %v", *requests)
		}
	}
}
