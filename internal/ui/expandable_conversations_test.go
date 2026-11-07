package ui

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func expandableModel(t testing.TB) Model {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Display.ThreadedConversations = true
	cfg.Display.MarkReadOnFocus = false
	cfg.Display.MarkReadOnOpen = false
	cfg.Display.MouseSupport = true
	cfg.Display.DateFormat = "none"
	m := NewModel(nil, cfg, "test", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m = next.(Model)
	m.focused = paneMessages
	m.mailboxes = []db.Mailbox{{ID: 1, Name: "INBOX"}, {ID: 2, Name: "Archive"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: 1}, {kind: rowKindMailbox, mailboxID: 2}}
	for i := 1; i <= 6; i++ {
		root := "<a>"
		if i > 3 {
			root = "<b>"
		}
		m.messages = append(m.messages, db.Message{ID: int64(i), MailboxID: 1, MessageID: fmt.Sprintf("<%d>", i), InReplyTo: root, Date: time.Unix(int64(i), 0), From: fmt.Sprintf("Sender%d", i), Subject: fmt.Sprintf("Subject%d", i), BodyText: fmt.Sprintf("Unique body %d", i), Read: i%2 == 0})
	}
	m.applyFilter()
	m.setViewportForCurrentRow()
	return m
}

func visibleMessageIDs(m *Model) []int64 {
	ids := make([]int64, m.activeMessageRowCount())
	for i := range ids {
		ids[i] = m.rowMessage(i).ID
	}
	return ids
}

func conversationKey(m Model, key tea.KeyMsg) Model {
	next, _ := m.Update(key)
	return next.(Model)
}

func TestExpandableRowsAndNavigation(t *testing.T) {
	m := expandableModel(t)
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 3}) {
		t.Fatal(got)
	}
	before := m.contentMessageID
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 5, 4, 3}) {
		t.Fatal(got)
	}
	if m.focused != paneMessages || m.contentMessageID != before {
		t.Fatal("expansion changed focus or preview")
	}
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.currentRowMessage().ID != 5 || m.contentMessageID != 5 {
		t.Fatal("child preview identity")
	}
	content := strings.Join(m.contentLines, "\n")
	if !strings.Contains(content, "Unique body 5") || strings.Contains(content, "Unique body 6") {
		t.Fatal("concatenated reader")
	}
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.focused != paneContent {
		t.Fatal("child Enter did not focus reader")
	}
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.focused != paneMessages {
		t.Fatal("Escape did not return to list")
	}
	m.messageCursor = 3
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 5, 4, 3, 2, 1}) {
		t.Fatal(got)
	}
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 5, 4, 3}) {
		t.Fatal(got)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRight}, {Type: tea.KeyTab}} {
		m.messageCursor = 0
		m.focused = paneMessages
		m = conversationKey(m, key)
		if m.focused != paneContent {
			t.Fatal("parent pane navigation")
		}
	}
}

func TestExpandableOrderingAndIdentity(t *testing.T) {
	m := expandableModel(t)
	m.messageCursor = 1 // select parent 3
	m.setViewportForCurrentRow()
	m.cfg.Display.UnreadFirst = true
	m.applyFilter()
	if m.currentRowMessage().ID != 3 {
		t.Fatal("sorting changed identity")
	}
	// Parent 6 was read, so unread 5 now represents its conversation.
	if m.messageThreads[0].Representative.ID != 5 {
		t.Fatal("unread first parent")
	}
	m.messageCursor = m.indexOfFilteredMessage(5)
	m.setViewportForCurrentRow()
	// A new higher-ranked arrival displaces the selected parent. Reveal the old one.
	m.messages = append(m.messages, db.Message{ID: 7, MessageID: "<7>", InReplyTo: "<b>", Date: time.Unix(7, 0), BodyText: "new arrival"})
	next, _ := m.Update(MessagesLoadedMsg{MailboxID: 1, Messages: m.messages})
	m = next.(Model)
	if m.currentRowMessage().ID != 5 || m.contentMessageID != 5 {
		t.Fatal("arrival changed identity")
	}
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{7, 5, 6, 4, 3}) {
		t.Fatal(got)
	}
	// Confirming read changes also changes parents while retaining the selected email.
	for i := range m.messages {
		if m.messages[i].ID == 7 {
			m.messages[i].Read = true
		}
	}
	m.applyFilter()
	if m.currentRowMessage().ID != 5 || m.messageThreads[0].Representative.ID != 5 {
		t.Fatal("read reordering")
	}
	// Tie-break by DB ID, and honor starred-first when selecting parents.
	m.messages[3].Date = m.messages[5].Date
	m.messages[3].Read = false
	m.messages[3].Starred = true
	m.starredFirst = true
	m.applyFilter()
	if m.messageThreads[0].Representative.ID != 4 {
		t.Fatal("starred parent ranking")
	}
}

func TestExpandableMouseAndScroll(t *testing.T) {
	m := expandableModel(t)
	y := 1 + m.paneHeaderHeight() + m.messageSearchRowHeight()
	next, _ := m.handleMessagesClick(y)
	m = next.(Model)
	next, _ = m.handleMessagesClick(y)
	m = next.(Model)
	if m.activeMessageRowCount() != 4 || m.focused != paneMessages {
		t.Fatal("double-click parent")
	}
	next, _ = m.handleMessagesClick(y + m.styles.ListItemLineStride())
	m = next.(Model)
	if m.currentRowMessage().ID != 5 || m.contentMessageID != 5 {
		t.Fatal("child click")
	}
	next, _ = m.handleMessagesClick(y + m.styles.ListItemLineStride())
	m = next.(Model)
	if m.focused != paneContent {
		t.Fatal("double-click child")
	}
	next, _ = m.handleMouseWheel(tea.MouseMsg{X: m.feedsPaneWidth() + 3, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	m = next.(Model)
	if m.currentRowMessage().ID != 3 || m.focused != paneContent {
		t.Fatal("wheel visible rows or focus")
	}
	m.listOffset = 2
	m.messageCursor = 0
	m.keepMessageCursorVisible()
	if m.listOffset != 0 {
		t.Fatal("cursor hidden")
	}
}

func TestExpandableLifecycle(t *testing.T) {
	m := expandableModel(t)
	m.toggleCurrentConversation()
	m.applyFilter()
	if m.activeMessageRowCount() != 4 {
		t.Fatal("refresh lost expansion")
	}
	m.sidebarCursor = 1
	m.applyFilter()
	if m.activeMessageRowCount() != 2 {
		t.Fatal("new folder retained expansion")
	}
	m.toggleCurrentConversation()
	m.searchMode = true
	m.searchQuery = "Sender"
	m.applyFilter()
	if m.activeMessageRowCount() != 2 {
		t.Fatal("new search retained expansion")
	}
	m.toggleCurrentConversation()
	m.cfg.Display.ThreadedConversations = false
	m.applyFilter()
	if len(m.messageRows) != 0 || len(m.expandedMessages) != 0 || m.activeMessageRowCount() != 6 {
		t.Fatal("flat mode")
	}
	m.cfg.Display.ThreadedConversations = true
	m.applyFilter()
	if m.activeMessageRowCount() != 2 {
		t.Fatal("re-enabling threading retained expansion")
	}
}

func TestExpandableRendering(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	for _, plain := range []bool{false, true} {
		for _, sender := range []bool{false, true} {
			for _, width := range []int{24, 40, 80, 160} {
				t.Run(fmt.Sprintf("plain=%t/sender=%t/width=%d", plain, sender, width), func(t *testing.T) {
					m := expandableModel(t)
					if plain {
						m.setStyles(BuildStyles(VT52, "comfortable", "square"))
					}
					m.cfg.Display.ShowSender = sender
					m.width = width
					m.toggleCurrentConversation()
					raw := m.renderMessagesPane()
					view := ansi.Strip(raw)
					if strings.Contains(view, "(3)") {
						t.Fatal("message count")
					}
					for _, line := range strings.Split(view, "\n") {
						if lipgloss.Width(line) > m.messagesPaneContentWidth()+2 {
							t.Fatalf("row overflow: %q", line)
						}
					}
					if width == 160 {
						for _, token := range []string{"▾", "▸", "Subject6", "Subject5", "Subject4"} {
							if !strings.Contains(view, token) {
								t.Fatalf("missing %q", token)
							}
						}
						want := 1
						if sender {
							want = 2
						}
						if strings.Count(view, "├─ ") != want || strings.Count(view, "└─ ") != want {
							t.Fatalf("tree columns:\n%s", view)
						}
						var subjectColumns []int
						for _, line := range strings.Split(view, "\n") {
							for _, subject := range []string{"Subject6", "├─ Subject5", "└─ Subject4", "Subject3"} {
								if at := strings.Index(line, subject); at >= 0 {
									subjectColumns = append(subjectColumns, lipgloss.Width(line[:at]))
								}
							}
						}
						if len(subjectColumns) != 4 {
							t.Fatal(subjectColumns)
						}
						for _, col := range subjectColumns {
							if col != subjectColumns[0] {
								t.Fatalf("unaligned columns %v", subjectColumns)
							}
						}
						if !plain {
							probe := lipgloss.NewStyle().Foreground(messageMutedColor(m.styles.Theme)).Render("x")
							muted := regexp.MustCompile(`38;2;[0-9]+;[0-9]+;[0-9]+`).FindString(probe)
							for _, line := range strings.Split(raw, "\n") {
								if strings.Contains(ansi.Strip(line), "Subject5") && (muted == "" || !strings.Contains(line, muted)) {
									t.Fatal("tree connector lost muted color")
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestExpandableParentActionsAreSingleMessage(t *testing.T) {
	for _, action := range []string{"delete", "archive", "move", "space", "plugin", "dismiss", "star"} {
		for _, child := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/child=%t", action, child), func(t *testing.T) {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				f := newUndoTestFixture(t, 3)
				m := f.model
				m.cfg.Display.ThreadedConversations = true
				m.cfg.Display.MarkReadOnFocus = false
				m.cfg.Display.MarkReadOnOpen = false
				m.focused = paneMessages
				for i := range m.messages {
					m.messages[i].MessageID = "<root>"
				}
				m.applyFilter()
				if child {
					m.toggleCurrentConversation()
					m.messageCursor = 1
				}
				m.setViewportForCurrentRow()
				id := m.currentRowMessage().ID
				switch action {
				case "delete", "archive":
					key := 'd'
					if action == "archive" {
						key = 'a'
					}
					m = conversationKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
					if len(m.pendingDestructiveActions) != 1 || len(m.pendingDestructiveActions[0].Entries) != 1 || m.pendingDestructiveActions[0].Entries[0].Message.ID != id {
						t.Fatal("ordinary action targeted hidden members")
					}
					// Delete and archive both hide their target at once.
					if len(m.filteredMessages) != 2 {
						t.Fatal("ordinary action hid siblings")
					}
					if !child && m.currentRowMessage().ID != f.messages[1].ID {
						t.Fatal("parent not replaced by next-ranked survivor")
					}
					// Undo returns to the restored message.
					wantID := id
					m.undoLatestDestructive()
					if m.currentRowMessage().ID != wantID {
						t.Fatalf("undo did not restore exact identity: got %d want %d", m.currentRowMessage().ID, wantID)
					}
				case "move":
					m = conversationKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
					if len(m.movePicker.messages) != 1 || m.movePicker.messages[0].ID != id {
						t.Fatal("move picker targeted hidden members")
					}
				case "space":
					m = conversationKey(m, tea.KeyMsg{Type: tea.KeySpace})
					if len(m.selectedMessages) != 1 || !m.selectedMessages[id] {
						t.Fatal("Space selected hidden members")
					}
				case "plugin":
					targets := m.pluginTargets()
					if len(targets) != 1 || targets[0].ID != id {
						t.Fatal("plugin targeted hidden members")
					}
				case "dismiss":
					_, cmd := m.dismissCurrent()
					result := cmd().(needsYouDismissMsg)
					if result.Err != nil || !reflect.DeepEqual(result.MessageIDs, []int64{id}) {
						t.Fatalf("dismiss targets: %+v", result)
					}
				case "star":
					next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
					m = settle(t, next.(Model), cmd)
					for _, msg := range f.messages {
						stored, err := m.db.GetMessage(msg.ID)
						if err != nil || stored.Starred != (msg.ID == id) {
							t.Fatalf("star affected sibling: %+v, %v", stored, err)
						}
					}
				}
			})
		}
	}
}

func TestExpandableReadActionScope(t *testing.T) {
	for _, mode := range []string{"parent-unread", "parent-read", "child", "selection"} {
		t.Run(mode, func(t *testing.T) {
			m, _, _ := readPendingModel(t)
			m.cfg.Display.ThreadedConversations = true
			for i := range m.messages {
				m.messages[i].MessageID = "<root>"
				m.messages[i].Read = mode == "parent-read"
			}
			m.applyFilter()
			if mode == "child" {
				m.toggleCurrentConversation()
				m.messageCursor = 1
			}
			id := m.currentRowMessage().ID
			if mode == "selection" {
				m.selectedMessages[m.messages[0].ID] = true
				m.selectedMessages[m.messages[1].ID] = true
				m.messages[1].Read = true
				m.applyFilter()
			}
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
			m = next.(Model)
			switch mode {
			case "parent-unread":
				if len(m.pendingReads) != 3 {
					t.Fatal("parent did not target conversation")
				}
				for _, pending := range m.pendingReads {
					if !pending.Read {
						t.Fatal("parent unread target")
					}
				}
			case "parent-read":
				if len(m.pendingReads) != 3 {
					t.Fatal("collapsed read parent did not toggle all members unread")
				}
				for _, pending := range m.pendingReads {
					if pending.Read {
						t.Fatal("collapsed read target")
					}
				}
			case "child":
				if len(m.pendingReads) != 1 {
					t.Fatal("single toggle targeted siblings")
				}
				pending, ok := m.pendingReads[id]
				if !ok || pending.Read != (mode == "child") {
					t.Fatal("incorrect single toggle")
				}
			case "selection":
				if len(m.pendingReads) != 2 || !m.pendingReads[m.messages[0].ID].Read || m.pendingReads[m.messages[1].ID].Read {
					t.Fatal("explicit per-message toggles changed")
				}
			}
		})
	}
}

func TestExpandableSelectionRemainsOnSurvivingChild(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f := newUndoTestFixture(t, 3)
	m := f.model
	m.cfg.Display.ThreadedConversations = true
	for i := range m.messages {
		m.messages[i].MessageID = "<root>"
	}
	m.applyFilter()
	m.toggleCurrentConversation()
	m.messageCursor = 2
	id := m.currentRowMessage().ID
	parent := *m.rowMessage(0)
	m.scheduleDelete([]db.Message{parent})
	if m.currentRowMessage().ID != id {
		t.Fatal("deleting another row changed selected child")
	}
}

func TestExpandableExpansionPreservesReaderAndPaneReuse(t *testing.T) {
	m := expandableModel(t)
	m.cfg.Display.ActionableLinks = true
	m.messages[5].BodyText = "https://example.com/action\n\n" + strings.Repeat("Reader paragraph.\n\n", 80)
	m.applyFilter()
	m.setViewportForCurrentRow()
	m.stepContentLink(1)
	m.revealContentLink()
	if m.contentLinkIdx < 0 {
		t.Fatal("fixture has no selected link")
	}
	line, offset, index := m.contentFocusLine, m.viewport.YOffset, m.contentLinkIdx
	bodies, viewports := m.bodyCache.len(), m.viewportCache.len()
	_ = m.View()
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.contentFocusLine != line || m.viewport.YOffset != offset || m.contentLinkIdx != index || m.bodyCache.len() != bodies || m.viewportCache.len() != viewports {
		t.Fatal("expansion rebuilt or reset reader")
	}
	if m.readerPanes.valid {
		t.Fatal("expansion retained stale list pane")
	}
	_ = m.View()
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyRight})
	_ = m.View()
	accounts, messages := m.readerPanes.accounts, m.readerPanes.messages
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.readerPanes.valid || m.readerPanes.accounts != accounts || m.readerPanes.messages != messages {
		t.Fatal("reader scrolling discarded surrounding panes")
	}
	assertReaderPanesMatchFresh(t, m)
}

func TestExpandableInputReorderAndVirtualOrdering(t *testing.T) {
	m := expandableModel(t)
	m.toggleCurrentConversation()
	m.messageCursor = 1
	id := m.currentRowMessage().ID
	for i, j := 0, len(m.messages)-1; i < j; i, j = i+1, j-1 {
		m.messages[i], m.messages[j] = m.messages[j], m.messages[i]
	}
	m.applyFilter()
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 5, 4, 3}) || m.currentRowMessage().ID != id {
		t.Fatalf("input reorder: %v", got)
	}
	m.sidebarRows = []sidebarRow{{kind: rowKindNeedsYou}}
	m.sidebarCursor = 0
	m.cfg.Display.UnreadFirst = true
	m.applyFilter()
	// Source order is the attention ranking; do not replace it with unread/date order.
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 3}) {
		t.Fatalf("virtual ranking: %v", got)
	}
	m.messages[0], m.messages[1] = m.messages[1], m.messages[0]
	m.applyFilter()
	if m.messageThreads[0].Representative.ID != 5 {
		t.Fatal("virtual parent did not follow source rank")
	}
}

func TestExpandableExplicitSelectionDoesNotIncludeSiblings(t *testing.T) {
	m := expandableModel(t)
	m.selectedMessages[6] = true
	m.selectedMessages[1] = true
	if got := messageIDs(m.selectedActionMessages()); !reflect.DeepEqual(got, []int64{1, 6}) {
		t.Fatal(got)
	}
	if m.messageRowSelected(*m.rowMessage(1), m.messageThreads[1]) {
		t.Fatal("hidden selected child marked parent selected")
	}
}

func TestExpandableDateTiesAndSingleton(t *testing.T) {
	m := expandableModel(t)
	for i := range m.messages {
		m.messages[i].Date = time.Unix(10, 0)
	}
	m.applyFilter()
	m.toggleCurrentConversation()
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 5, 4, 3}) {
		t.Fatal(got)
	}
	m.messages = append(m.messages, db.Message{ID: 7, MessageID: "<singleton>", Date: time.Unix(11, 0), BodyText: "Single email"})
	m.applyFilter()
	m.messageCursor = m.indexOfFilteredMessage(7)
	m.setViewportForCurrentRow()
	m = conversationKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.focused != paneContent || m.contentMessageID != 7 {
		t.Fatal("singleton Enter")
	}
}

func TestExpandableWideTextAndTaggedRowSizing(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	m := expandableModel(t)
	for _, sender := range []bool{false, true} {
		for _, width := range []int{3, 8, 14, 24, 40, 80} {
			tags := []annotationTag{{kind: tagCategory, category: "project", color: "#ff0000"}, {kind: tagReply}}
			row := m.renderMessageRow(m.styles.ArticleUnread, m.messageRowPrefix(false), m.messageRowStar(true), "李明 💬", sender, min(22, width/3), "進捗✨ "+strings.Repeat("界", 40), tags, "Nov 4", width, "├─ ")
			lines := strings.Split(ansi.Strip(row), "\n")
			if len(lines) != m.styles.ListItemLineStride() {
				t.Fatalf("width=%d sender=%t wrapped into %d lines", width, sender, len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > width {
					t.Fatalf("width=%d row overflow: %q", width, line)
				}
			}
		}
	}
}

func TestExpandableCollapseClearsHiddenUnreadFilterMarkers(t *testing.T) {
	m := expandableModel(t)
	// Each conversation has an unread newest parent, unread child, read child.
	for i := range m.messages {
		m.messages[i].Read = m.messages[i].ID%3 == 1
	}
	m.applyFilter()
	m.toggleCurrentConversation() // B
	m.messageCursor = m.indexOfMessageSelection(3, true)
	m.toggleCurrentConversation() // A remains independently expanded
	m.showUnreadOnly = true
	m.applyFilter()
	m.messageCursor = m.indexOfMessageSelection(6, true)
	m.toggleCurrentConversation() // explicitly collapse B with its read child hidden
	m.showUnreadOnly = false
	m.applyFilter()
	for _, thread := range m.messageThreads {
		want := thread.Representative.ID == 3
		if m.threadExpanded(thread) != want {
			t.Fatalf("conversation %d expanded=%v want %v", thread.Representative.ID, m.threadExpanded(thread), want)
		}
	}
	if got := visibleMessageIDs(&m); !reflect.DeepEqual(got, []int64{6, 3, 2, 1}) {
		t.Fatalf("visible rows after disabling Unread Only: %v", got)
	}
}
