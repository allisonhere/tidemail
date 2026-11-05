package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

func rowTextColumn(t *testing.T, line, text string) int {
	t.Helper()
	plain := ansi.Strip(line)
	at := strings.Index(plain, text)
	if at < 0 {
		t.Fatalf("missing %q in %q", text, plain)
	}
	return lipgloss.Width(plain[:at])
}

func TestConversationRowColumnsMatchOrdinaryMessages(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	for _, plain := range []bool{false, true} {
		for _, showSender := range []bool{false, true} {
			for _, starred := range []bool{false, true} {
				t.Run(fmt.Sprintf("plain=%t/sender=%t/star=%t", plain, showSender, starred), func(t *testing.T) {
					m := expandableModel(t)
					if plain {
						m.setStyles(BuildStyles(VT52, "compact", "square"))
					}
					m.cfg.Display.ShowSender = showSender
					m.messages[5].Starred = starred
					m.messages[4].From = "李明 💬"
					m.messages = append(m.messages, db.Message{ID: 7, MessageID: "<single>", Date: time.Unix(0, 0), From: "SingleSender", Subject: "SingleSubject", Read: true})
					m.applyFilter()
					flat := m
					flat.cfg.Display.ThreadedConversations = false
					flat.applyFilter()
					flat.messageCursor = flat.indexOfFilteredMessage(6)
					baseline := rowLine(t, flat, "Subject6")
					subjectColumn := rowTextColumn(t, baseline, "Subject6")
					senderColumn := subjectColumn
					if showSender {
						senderColumn = rowTextColumn(t, baseline, "Sender6")
					}
					for _, expanded := range []bool{false, true} {
						if expanded {
							m.toggleCurrentConversation()
						}
						parent := rowLine(t, m, "Subject6")
						chevron := "▸"
						if expanded {
							chevron = "▾"
						}
						if got := rowTextColumn(t, parent, chevron); got != senderColumn-1 {
							t.Fatalf("chevron at %d, want existing spacer %d", got, senderColumn-1)
						}
						if got := rowTextColumn(t, parent, "Subject6"); got != subjectColumn {
							t.Fatalf("subject shifted: %d, want %d", got, subjectColumn)
						}
						if showSender && rowTextColumn(t, parent, "Sender6") != senderColumn {
							t.Fatal("parent sender shifted")
						}
						// The status marker occupies exactly the same cells as in flat mode.
						if got, want := ansi.Cut(ansi.Strip(parent), 1, 3), ansi.Cut(ansi.Strip(baseline), 1, 3); got != want {
							t.Fatalf("status changed: %q, want %q", got, want)
						}
						if starred {
							star := "★"
							if plain {
								star = "*"
							}
							if rowTextColumn(t, parent, star) != rowTextColumn(t, baseline, star) {
								t.Fatal("star shifted or lost")
							}
						}
						single := rowLine(t, m, "SingleSubject")
						if rowTextColumn(t, single, "SingleSubject") != subjectColumn || strings.ContainsAny(ansi.Strip(single), "▸▾") {
							t.Fatal("singleton columns or disclosure")
						}
						if !expanded {
							continue
						}
						for _, child := range []struct{ subject, sender, tree string }{{"Subject5", "李明 💬", "├─ "}, {"Subject4", "Sender4", "└─ "}} {
							line := rowLine(t, m, child.subject)
							treeColumn := subjectColumn
							if showSender {
								treeColumn = senderColumn
							}
							if got := rowTextColumn(t, line, child.tree); got != treeColumn {
								t.Fatalf("sender-side tree at %d, want parent sender %d", got, treeColumn)
							}
							if got := rowTextColumn(t, line, child.tree+child.subject); got != subjectColumn {
								t.Fatalf("subject tree shifted: %d, want %d", got, subjectColumn)
							}
							if showSender && rowTextColumn(t, line, child.sender) != senderColumn+lipgloss.Width(child.tree) {
								t.Fatal("only child sender text should move right")
							}
							flatLine := rowLine(t, flat, child.subject)
							if got, want := ansi.Cut(ansi.Strip(line), 1, 3), ansi.Cut(ansi.Strip(flatLine), 1, 3); got != want {
								t.Fatalf("child status moved: %q, want %q", got, want)
							}
						}
					}
				})
			}
		}
	}
}

// Decode the actual ANSI output into terminal cells; merely finding a selected
// background escape somewhere in the row cannot detect resets between spans.
func assertConversationSelectedRowBackground(t *testing.T, m Model) {
	t.Helper()
	selected := m.currentRowMessage()
	line := rowLine(t, m, selected.Subject)
	width := m.messagesPaneContentWidth()
	if got := lipgloss.Width(line); got != width+2 {
		t.Fatalf("row width %d, want %d", got, width+2)
	}
	cells := cellbuf.NewBuffer(width+2, 1)
	cellbuf.SetContent(cells, line)
	probe := cellbuf.NewBuffer(1, 1)
	cellbuf.SetContent(probe, m.styles.ArticleSelected.UnsetPadding().UnsetWidth().Render("x"))
	want := probe.Cell(0, 0).Style.Bg
	if want == nil {
		t.Fatal("selected style has no background")
	}
	for x := 1; x <= width; x++ {
		cell := cells.Cell(x, 0)
		if cell == nil || !reflect.DeepEqual(cell.Style.Bg, want) {
			t.Fatalf("selected row background breaks at terminal column %d (%q): got %+v, want %+v; row=%q", x, ansi.Cut(ansi.Strip(line), x, x+1), cell, want, line)
		}
	}
}

func TestConversationChildHighlightCoversEveryTerminalCell(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	for _, showSender := range []bool{false, true} {
		for _, density := range []string{"compact", "comfortable"} {
			for _, tagStyle := range []string{"none", config.TagStylePlain, config.TagStyleCompact} {
				t.Run(fmt.Sprintf("sender=%t/density=%s/tags=%s", showSender, density, tagStyle), func(t *testing.T) {
					m := expandableModel(t)
					m.cfg.Display.ShowSender = showSender
					m.cfg.Display.DateFormat = "absolute"
					m.setStyles(BuildStyles(CatppuccinMocha, density, "square"))
					if tagStyle != "none" {
						m.cfg.Display.TagStyle = tagStyle
						m.plugins.annotations = map[int64][]db.PluginAnnotation{5: {cacheAnn("test", "needs_reply", "true")}, 4: {cacheAnn("test", "needs_reply", "true")}}
					}
					m.toggleCurrentConversation()
					for _, key := range []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyDown}, {Type: tea.KeyUp}} {
						m = conversationKey(m, key)
						assertConversationSelectedRowBackground(t, m)
					}
					// Mouse selection must repaint the terminal row with the same style.
					y := 1 + m.paneHeaderHeight() + m.messageSearchRowHeight() + 2*m.styles.ListItemLineStride()
					next, _ := m.Update(tea.MouseMsg{X: m.feedsPaneWidth() + 3, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
					m = next.(Model)
					if m.currentRowMessage().ID != 4 {
						t.Fatal("mouse did not select last child")
					}
					assertConversationSelectedRowBackground(t, m)
				})
			}
		}
	}
}
