package ui

import (
	"errors"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

func TestDeletePrototypeCountdownUndoAndSpinner(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	first := f.messages[0]
	m.scheduleDelete(f.messages[:1])
	if len(m.filteredMessages) != 3 || m.messageCursor != 1 {
		t.Fatal("delete must stay visible and advance cursor")
	}
	deadline := m.pendingDestructiveActions[0].Deadline
	for seconds := 6; seconds >= 1; seconds-- {
		now := deadline.Add(-time.Duration(seconds) * time.Second)
		if got := m.rowDeleteIndicator(first, messageThread{}, now); got != string(rune('0'+seconds)) {
			t.Fatalf("countdown %d: %q", seconds, got)
		}
	}
	m.scheduleDelete(f.messages[1:2])
	m.pendingDestructiveActions[1].Deadline = deadline.Add(time.Second)
	if got := m.rowDeleteIndicator(f.messages[1], messageThread{}, deadline.Add(-2*time.Second)); got != "3" {
		t.Fatal("independent deadline", got)
	}
	cursor := m.messageCursor
	m.undoLatestDestructive()
	if m.messageCursor != cursor || m.rowDeleteIndicator(f.messages[1], messageThread{}, time.Now()) != "" {
		t.Fatal("undo changed cursor or retained marker")
	}
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	if got := m.rowDeleteIndicator(first, messageThread{}, time.Now()); got != m.pendingReadIndicator() {
		t.Fatal("missing spinner", got)
	}
	if got := m.rowDeleteIndicator(f.messages[2], messageThread{Messages: []db.Message{first}}, time.Now()); got != m.pendingReadIndicator() {
		t.Fatal("thread missing spinner")
	}
}

func TestDeletePrototypeResultsAndStaleReload(t *testing.T) {
	for _, success := range []bool{false, true} {
		f := newUndoTestFixture(t, 3)
		m := f.model
		stale := MessagesLoadedMsg{ReadVersion: m.readVersion, MailboxID: f.source.ID, Messages: f.messages}
		m.scheduleDelete(f.messages[:1])
		currentID := m.currentRowMessage().ID
		id := m.pendingDestructiveActions[0].ID
		result := DestructiveActionResultMsg{ID: id, Kind: destructiveDelete}
		if success {
			result = m.beginDestructiveCommit(id)().(DestructiveActionResultMsg)
		} else {
			result.Failed = f.messages[:1]
			result.Err = errors.New("rejected")
		}
		m.handleDestructiveResult(result)
		want := 3
		if success {
			want = 2
		}
		if len(m.filteredMessages) != want || m.currentRowMessage().ID != currentID || m.messagePendingDestructive(f.messages[0].ID) {
			t.Fatal("bad result state")
		}
		if success {
			next, _ := m.Update(stale)
			m = next.(Model)
			if len(m.filteredMessages) != 2 || m.readVersion <= stale.ReadVersion {
				t.Fatal("stale reload resurrected delete")
			}
		} else if m.messages[0].Read != f.messages[0].Read {
			t.Fatal("failure changed read state")
		}
	}
}

func TestDeletePrototypeConflictingLocks(t *testing.T) {
	f := newUndoTestFixture(t, 3)
	m := f.model
	m.pendingReads = map[int64]pendingRead{f.messages[0].ID: {Token: 1}}
	m.scheduleDelete(f.messages[:1])
	m.scheduleArchive(f.messages[:1])
	m.scheduleMove(f.messages[:1], f.target)
	if len(m.pendingDestructiveActions) != 0 {
		t.Fatal("read pending allowed destructive action")
	}
	m.scheduleDelete(f.messages[1:2])
	m.scheduleDelete(f.messages[1:2])
	m.scheduleArchive(f.messages[1:2])
	m.scheduleMove(f.messages[1:2], f.target)
	if cmd := m.acceptReadChanges(f.messages[1:2], nil, false); cmd != nil {
		t.Fatal("delete allowed read change")
	}
	if len(m.pendingDestructiveActions) != 1 {
		t.Fatal("delete allowed conflicting destructive action")
	}
	m.scheduleDelete(f.messages[2:])
	if len(m.pendingDestructiveActions) != 2 {
		t.Fatal("unrelated message locked")
	}
}

func TestDeletePrototypeSubjectStrikethrough(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.width, m.height = 120, 40
	check := func(want bool) {
		t.Helper()
		if got := regexp.MustCompile("\x1b\\[(?:[0-9]+;)*9(?:;[0-9]+)*m").MatchString(m.renderMessagesPane()); got != want {
			t.Fatalf("subject strikethrough = %v, want %v", got, want)
		}
	}
	check(false)
	m.scheduleDelete(f.messages[:1])
	check(true)
	m.undoLatestDestructive()
	check(false)
	m.scheduleDelete(f.messages[:1])
	id := m.pendingDestructiveActions[0].ID
	m.beginDestructiveCommit(id)
	check(true)
	m.handleDestructiveResult(DestructiveActionResultMsg{ID: id, Kind: destructiveDelete, Failed: f.messages[:1], Err: errors.New("rejected")})
	check(false)
	m.pendingReads = map[int64]pendingRead{f.messages[0].ID: {Token: 1}}
	check(false)
}

func TestDeletePrototypeSubjectInheritsRowStyle(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	f := newUndoTestFixture(t, 2)
	m := f.model
	m.width, m.height = 120, 40
	m.messages[0].Read = false
	m.applyFilter()
	m.scheduleDelete(m.messages[:1])
	for _, cursor := range []int{1, 0} {
		m.messageCursor = cursor
		unread, _, selected, _, _, _ := m.messageRowStyles()
		style := applyMessageRowState(unread, selected, false, cursor == 0, m.styles.Theme)
		subject := style.UnsetPadding().UnsetWidth().UnsetMaxWidth().Strikethrough(true).Render(m.messageRowTitle(m.messages[0]))
		for _, segment := range strings.SplitAfter(subject, "\x1b[0m") {
			if !strings.Contains(m.renderMessagesPane(), segment) {
				t.Fatalf("pending subject lost row colors, cursor=%d", cursor)
			}
		}
	}
	if m.messages[0].Read {
		t.Fatal("rendering changed unread state")
	}
}

func TestDeletePrototypeRowBackgroundAfterSubject(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	f := newUndoTestFixture(t, 1)
	m := f.model
	unread, _, selected, _, _, _ := m.messageRowStyles()
	for _, style := range []lipgloss.Style{unread, selected} {
		inline := style.UnsetPadding().UnsetWidth().UnsetMaxWidth()
		title := inline.Strikethrough(true).Render("subject")
		row := m.renderMessageRow(style, "6 ", "", "", false, 0, title, nil, "", 40)
		// Seven subject cells and two marker cells leave 31 background cells.
		if !strings.Contains(row, inline.Render(strings.Repeat(" ", 31))) {
			t.Fatal("trailing row padding does not restore row colors")
		}
		if lipgloss.Width(row) != 40 {
			t.Fatal("row width changed")
		}
	}
}
