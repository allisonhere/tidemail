package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestReaderLinkSelectionInputEquivalence(t *testing.T) {
	for _, focus := range []bool{false, true} {
		for _, width := range []int{40, 120} {
			for _, method := range []string{"click", "down", "up", "next", "previous"} {
				t.Run(fmt.Sprintf("focus=%v/width=%d/%s", focus, width, method), func(t *testing.T) {
					requests, apply := recordReaderActivations(t)
					html := `<p>Ordinary body text.</p>`
					for i := 0; i < 3; i++ {
						html += fmt.Sprintf(`<p><a href="https://same.example/%d">%s action %d</a></p>`, i, strings.Repeat("A long label 界 ", 12), i)
					}
					m := readerLinksModel(t, html, "")
					m.cfg.Display.FocusLine = focus
					m.cfg.Display.MouseSupport = true
					m = apply(m, tea.WindowSizeMsg{Width: width, Height: 40})
					row := m.contentLinkRows[1]
					m.viewport.SetYOffset(max(0, row-3))
					switch method {
					case "click":
						y := m.articlesPaneOuterHeight() + 1 + m.paneHeaderHeight() + row - m.viewport.YOffset
						click := leftClick(m.feedsPaneWidth()+4, y)
						m = apply(m, click)
						m = apply(m, click) // clicking the selection again keeps it
					case "down":
						m.contentFocusLine = m.contentLinkRows[0]
						m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
					case "up":
						m.contentFocusLine = m.contentLinkRows[2]
						m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
					case "next":
						for i := 0; i < 2; i++ {
							m = apply(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}, Alt: true})
						}
					case "previous":
						m = apply(m, readerPreviousKey)
						m = apply(m, readerPreviousKey)
					}
					if len(*requests) != 0 {
						t.Fatalf("selection launched browser: %v", *requests)
					}
					assertReaderLinkSelection(t, m, 1)
					m = apply(m, readerOpenKey)
					m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
					if !reflect.DeepEqual(*requests, []string{"https://same.example/1", "https://same.example/1"}) {
						t.Fatalf("wrong activation requests: %v", *requests)
					}
				})
			}
		}
	}
}

func TestReaderLinkSelectionRejectsNonEntryClicks(t *testing.T) {
	for _, focus := range []bool{false, true} {
		t.Run(fmt.Sprint(focus), func(t *testing.T) {
			requests, apply := recordReaderActivations(t)
			m := readerLinksModel(t, `<p>Ordinary body text.</p><a href="https://same.example/1">One</a><a href="https://same.example/2">Two</a>`, "")
			m.cfg.Display.FocusLine = focus
			m.cfg.Display.MouseSupport = true
			m.viewport.GotoBottom()
			row := m.contentLinkRows[1]
			y := m.articlesPaneOuterHeight() + 1 + m.paneHeaderHeight() + row - m.viewport.YOffset
			for _, point := range [][2]int{
				{m.feedsPaneWidth(), y}, {m.feedsPaneWidth() + 1, y}, // border/indent
				{m.feedsPaneWidth() + 3, y},        // prefix whitespace
				{m.width - 1, y}, {m.width - 2, y}, // border/trailing padding
				{m.feedsPaneWidth() + 4, m.articlesPaneOuterHeight()}, // pane header
			} {
				m = apply(m, tea.KeyMsg{Type: tea.KeyCtrlN})
				m = apply(m, leftClick(point[0], point[1]))
				if m.contentLinkIdx != -1 {
					t.Fatalf("non-entry click %v selected %d", point, m.contentLinkIdx)
				}
				m = apply(m, readerOpenKey)
				m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			// Conflicting row metadata cannot identify a particular entry.
			m.contentLinkRows[0] = m.contentLinkRows[1]
			m = apply(m, leftClick(m.feedsPaneWidth()+4, y))
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.contentLinkIdx != -1 || len(*requests) != 0 {
				t.Fatalf("non-entry/ambiguous click activated %v", *requests)
			}
		})
	}
}

func TestReaderLinkSelectionEnteringAndLeavingFooter(t *testing.T) {
	for _, focus := range []bool{false, true} {
		t.Run(fmt.Sprint(focus), func(t *testing.T) {
			requests, apply := recordReaderActivations(t)
			m := readerActivationModel(t, focus)
			row := m.contentLinkRows[0]
			m.viewport.SetYOffset(max(0, row-2))
			m.contentFocusLine = row - 1 // LINKS heading; positioning alone is inert
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = apply(m, tea.KeyMsg{Type: tea.KeyDown})
			assertReaderLinkSelection(t, m, 0)
			m = apply(m, tea.KeyMsg{Type: tea.KeyUp})
			if m.contentLinkIdx != -1 {
				t.Fatal("leaving the footer retained link selection")
			}
			m = apply(m, tea.KeyMsg{Type: tea.KeyEnter})
			m = apply(m, readerOpenKey)
			if len(*requests) != 0 {
				t.Fatalf("heading/body navigation activated %v", *requests)
			}
		})
	}
}
