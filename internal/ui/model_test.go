package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestSaveConfigSurfacesError(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	orig := configSave
	configSave = func(config.Config) error { return fmt.Errorf("disk full") }
	defer func() { configSave = orig }()

	m.saveConfig()
	if !m.statusErr {
		t.Fatal("expected the status line to be flagged as an error")
	}
	if !strings.Contains(m.statusMsg, "disk full") {
		t.Fatalf("expected the save error surfaced on the status line, got %q", m.statusMsg)
	}
}

func TestSaveConfigSuccessNoError(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	orig := configSave
	configSave = func(config.Config) error { return nil }
	defer func() { configSave = orig }()

	m.saveConfig()
	if m.statusErr {
		t.Fatalf("did not expect an error status on success, got %q", m.statusMsg)
	}
}

func TestSettingsSavePreservesSelectedMessageAfterUnreadFirstReload(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Acct", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []db.Message{
		{MailboxID: mailboxID, UID: 1, Subject: "Older unread", Date: time.Unix(10, 0)},
		{MailboxID: mailboxID, UID: 2, Subject: "Newest read", Date: time.Unix(30, 0), Read: true},
		{MailboxID: mailboxID, UID: 3, Subject: "Older read", Date: time.Unix(20, 0), Read: true},
	} {
		if err := database.UpsertMessage(msg); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.DefaultConfig()
	cfg.Display.UnreadFirst = false
	m := NewModel(database, cfg, "dev", false)
	m.accounts = []db.Account{{ID: accountID, Name: "Acct"}}
	m.mailboxes = []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX"}}
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == mailboxID {
			m.sidebarCursor = i
			break
		}
	}
	msgs, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	m.messages = msgs
	m.filteredMessages = msgs
	m.messageCursor = 0
	selectedID := msgs[0].ID
	m.overlay = overlaySettings
	m.settings = newSettings(cfg, m.settingsUpdateState())
	m.settings.unreadFirst = true

	origSave := configSave
	configSave = func(config.Config) error { return nil }
	defer func() { configSave = origSave }()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(Model)

	if m.pendingSelectMessageID != selectedID {
		t.Fatalf("expected settings save to remember selected message %d, got %d", selectedID, m.pendingSelectMessageID)
	}

	reloaded := []db.Message{msgs[2], msgs[0], msgs[1]}
	next, _ = m.Update(MessagesLoadedMsg{MailboxID: mailboxID, Messages: reloaded})
	m = next.(Model)

	if len(m.filteredMessages) == 0 || m.filteredMessages[m.messageCursor].ID != selectedID {
		t.Fatalf("expected selected message %d preserved after reload, cursor=%d messages=%+v", selectedID, m.messageCursor, m.filteredMessages)
	}
}

func TestDeleteKeyWaitsForDeleteResultBeforeRemovingMessage(t *testing.T) {
	msg := db.Message{ID: 42, MailboxID: 7, UID: 99, Subject: "Keep until confirmed"}
	m := Model{
		keys:             DefaultKeys,
		focused:          paneMessages,
		messages:         []db.Message{msg},
		filteredMessages: []db.Message{msg},
		selectedMessages: make(map[int64]bool),
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected delete key to start a delete command")
	}
	if len(m.messages) != 1 || len(m.filteredMessages) != 1 {
		t.Fatalf("expected message to stay visible until delete succeeds, got %d/%d", len(m.messages), len(m.filteredMessages))
	}

	next, _ = m.Update(MessagesDeletedMsg{Deleted: []MessageRef{{ID: msg.ID, MailboxID: msg.MailboxID}}})
	m = next.(Model)

	if len(m.messages) != 0 || len(m.filteredMessages) != 0 {
		t.Fatalf("expected successful delete result to remove message, got %d/%d", len(m.messages), len(m.filteredMessages))
	}
}

func TestMessagesDeletedMsgKeepsRemoteFailuresVisible(t *testing.T) {
	deleted := db.Message{ID: 1, MailboxID: 2, Subject: "Gone"}
	failed := db.Message{ID: 3, MailboxID: 2, Subject: "Still on server"}
	m := Model{
		messages:         []db.Message{deleted, failed},
		filteredMessages: []db.Message{deleted, failed},
		messageCursor:    0,
		selectedMessages: make(map[int64]bool),
	}

	next, _ := m.Update(MessagesDeletedMsg{
		Deleted: []MessageRef{{ID: deleted.ID, MailboxID: deleted.MailboxID}},
		Failed:  1,
		Err:     errors.New("too many simultaneous connections"),
	})
	m = next.(Model)

	if len(m.messages) != 1 || m.messages[0].ID != failed.ID {
		t.Fatalf("expected only the failed message to stay, got %d messages", len(m.messages))
	}
	if !m.statusErr {
		t.Fatal("expected an error status when some deletes fail")
	}
	if !strings.Contains(m.statusMsg, "1 failed") {
		t.Fatalf("expected status to report the failure count, got %q", m.statusMsg)
	}
}

func TestRightFromMessageListMarksUnreadMessageRead(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	accountID, err := database.AddAccount("Acct", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", UnreadCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: 1, Subject: "Unread", Read: false}); err != nil {
		t.Fatal(err)
	}
	msgs, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected one message, got %d", len(msgs))
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.focused = paneMessages
	m.messages = msgs
	m.filteredMessages = msgs
	m.mailboxes = []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX", UnreadCount: 1}}
	m.selectedMessages = make(map[int64]bool)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.focused != paneContent {
		t.Fatalf("expected focus to move to content, got %v", m.focused)
	}
	if cmd == nil {
		t.Fatal("expected right arrow into content to mark unread message read")
	}

	readMsg, ok := cmd().(MessageReadUpdatedMsg)
	if !ok {
		t.Fatalf("expected MessageReadUpdatedMsg, got %T", readMsg)
	}
	next, _ = m.Update(readMsg)
	m = next.(Model)

	if !m.messages[0].Read || !m.filteredMessages[0].Read {
		t.Fatalf("expected in-memory message marked read, got messages=%+v filtered=%+v", m.messages[0], m.filteredMessages[0])
	}
	stored, err := database.GetMessage(msgs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Read {
		t.Fatal("expected database message marked read")
	}
	if m.mailboxes[0].UnreadCount != 0 {
		t.Fatalf("expected unread count decremented to 0, got %d", m.mailboxes[0].UnreadCount)
	}
}

func TestRightFromMessageListRespectsMarkReadOnOpenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnOpen = false
	msg := db.Message{ID: 1, MailboxID: 2, Subject: "Unread", Read: false}
	m := NewModel(nil, cfg, "dev", false)
	m.focused = paneMessages
	m.messages = []db.Message{msg}
	m.filteredMessages = []db.Message{msg}
	m.selectedMessages = make(map[int64]bool)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if m.focused != paneContent {
		t.Fatalf("expected focus to move to content, got %v", m.focused)
	}
	if cmd != nil {
		t.Fatal("did not expect mark-read command when mark_read_on_open is disabled")
	}
}

func TestMessageRowCollapsesFoldedHeadersToOneLine(t *testing.T) {
	row := renderArticleRowWithSender(
		"⬤ ",
		senderDisplay("Alice\r\n Bob <alice@example.com>"),
		unescapeDisplayText("this &amp; that\r\n folded	 subject"),
		"now",
		42,
		12,
	)

	if strings.ContainsAny(row, "\r\n	") {
		t.Fatalf("expected row text to be single-line, got %q", row)
	}
	if got := lipgloss.Width(row); got != 42 {
		t.Fatalf("expected row width 42, got %d: %q", got, row)
	}
	if !strings.Contains(row, "this & that folded") {
		t.Fatalf("expected folded subject collapsed and unescaped, got %q", row)
	}
}

func TestRemoteDeletePlanMovesToTrashWhenAvailable(t *testing.T) {
	source := db.Mailbox{ID: 1, AccountID: 10, Name: "INBOX"}
	trash := db.Mailbox{ID: 2, AccountID: 10, Name: "[Gmail]/Trash", Flags: []string{"\\Trash"}}

	action, target := remoteDeletePlan(source, &trash)

	if action != remoteDeleteMoveToTrash {
		t.Fatalf("expected move-to-trash delete plan, got %v", action)
	}
	if target == nil || target.ID != trash.ID {
		t.Fatalf("expected trash target, got %+v", target)
	}
}

func TestRemoteDeletePlanExpungesWhenAlreadyInTrash(t *testing.T) {
	source := db.Mailbox{ID: 2, AccountID: 10, Name: "[Gmail]/Trash", Flags: []string{"\\Trash"}}

	action, target := remoteDeletePlan(source, &source)

	if action != remoteDeleteExpunge {
		t.Fatalf("expected expunge delete plan, got %v", action)
	}
	if target != nil {
		t.Fatalf("expected no target for expunge, got %+v", target)
	}
}

func TestMessageRowStylesKeepReverseVideoSelectedColorWithAccountAccent(t *testing.T) {
	styles := BuildStyles(BuiltinThemes[0], "compact")
	m := Model{
		styles: styles,
		accounts: []db.Account{
			{ID: 10, Name: "Personal", Color: "#c41e3a"},
		},
		mailboxes: []db.Mailbox{
			{ID: 20, AccountID: 10, Name: "INBOX"},
		},
		sidebarRows: []sidebarRow{
			{kind: rowKindMailbox, mailboxID: 20, accountID: 10},
		},
	}

	_, _, selected, headerActive, _, borderFocus := m.messageRowStyles()

	if selected.GetForeground() != styles.ArticleSelected.GetForeground() {
		t.Fatalf("selected row foreground should stay base reverse-video color, got %q want %q", selected.GetForeground(), styles.ArticleSelected.GetForeground())
	}
	if headerActive.GetBackground() != lipgloss.Color("#c41e3a") {
		t.Fatalf("header should still use account accent, got %q", headerActive.GetBackground())
	}
	if borderFocus != lipgloss.Color("#c41e3a") {
		t.Fatalf("border focus should still use account accent, got %q", borderFocus)
	}
}

func TestSpaceSelectAdvanceKeepsCursorVisible(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focused = paneMessages

	visible := m.articleRowsVisible()
	if visible < 2 {
		t.Fatalf("expected at least 2 visible message rows, got %d", visible)
	}
	msgs := make([]db.Message, visible+2)
	for i := range msgs {
		msgs[i] = db.Message{ID: int64(i + 1), Subject: fmt.Sprintf("Message %d", i+1)}
	}
	m.messages = msgs
	m.filteredMessages = msgs
	m.messageCursor = visible - 1
	m.listOffset = 0

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)

	if m.messageCursor != visible {
		t.Fatalf("expected cursor to advance to %d, got %d", visible, m.messageCursor)
	}
	if m.listOffset != 1 {
		t.Fatalf("expected list offset to scroll to 1, got %d", m.listOffset)
	}
}

func TestSelectAllKeySelectsFilteredMessages(t *testing.T) {
	msgs := []db.Message{
		{ID: 1, Subject: "Visible 1"},
		{ID: 2, Subject: "Visible 2"},
		{ID: 3, Subject: "Hidden"},
	}
	m := Model{
		keys:             DefaultKeys,
		focused:          paneMessages,
		messages:         msgs,
		filteredMessages: msgs[:2],
		selectedMessages: make(map[int64]bool),
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = next.(Model)

	if len(m.selectedMessages) != 2 {
		t.Fatalf("expected 2 selected messages, got %d: %+v", len(m.selectedMessages), m.selectedMessages)
	}
	for _, id := range []int64{1, 2} {
		if !m.selectedMessages[id] {
			t.Fatalf("expected message %d selected, got %+v", id, m.selectedMessages)
		}
	}
	if m.selectedMessages[3] {
		t.Fatalf("did not expect hidden message selected, got %+v", m.selectedMessages)
	}
}

func TestThreadToggleGroupsMessageRows(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	msgs := []db.Message{
		{ID: 2, MessageID: "<reply@example.com>", InReplyTo: "<root@example.com>", Subject: "Re: Plan", Date: time.Unix(200, 0)},
		{ID: 1, MessageID: "<root@example.com>", Subject: "Plan", Date: time.Unix(100, 0), Read: true},
	}
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.focused = paneMessages
	m.messages = msgs
	m.filteredMessages = msgs
	m.selectedMessages = make(map[int64]bool)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = next.(Model)

	if !m.cfg.Display.ThreadedConversations {
		t.Fatal("expected threaded conversations enabled")
	}
	if got := m.activeMessageRowCount(); got != 1 {
		t.Fatalf("expected one thread row, got %d", got)
	}
	if len(m.messageThreads) != 1 || m.messageThreads[0].Count != 2 {
		t.Fatalf("expected grouped thread, got %+v", m.messageThreads)
	}
	if msg := m.currentRowMessage(); msg == nil || msg.ID != 2 {
		t.Fatalf("expected newest reply representative, got %+v", msg)
	}
}

func TestAccountsPaneComfortableDensityDoesNotExceedMainHeight(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.Density = "comfortable"
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 12
	m.focused = paneAccounts
	m.accounts = []db.Account{{ID: 1, Name: "Work"}}
	for i := 0; i < 20; i++ {
		m.mailboxes = append(m.mailboxes, db.Mailbox{
			ID:        int64(i + 1),
			AccountID: 1,
			Name:      fmt.Sprintf("Folder %02d", i),
		})
	}
	m.rebuildSidebar()

	view := ansi.Strip(m.renderAccountsPane())
	if got, want := strings.Count(view, "\n")+1, m.mainHeight(); got > want {
		t.Fatalf("expected accounts pane height <= %d lines, got %d:\n%s", want, got, view)
	}
}

func TestMessagesPaneComfortableDensityDoesNotExceedPaneHeight(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.Density = "comfortable"
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 16
	m.focused = paneMessages
	m.selectedMessages = make(map[int64]bool)
	for i := 0; i < 30; i++ {
		msg := db.Message{
			ID:      int64(i + 1),
			Subject: fmt.Sprintf("Message %02d", i),
			From:    "sender@example.com",
			Date:    time.Unix(int64(i), 0),
		}
		m.messages = append(m.messages, msg)
		m.filteredMessages = append(m.filteredMessages, msg)
	}
	m.messageCursor = 10
	m.listOffset = 8

	view := ansi.Strip(m.renderMessagesPane())
	if got, want := strings.Count(view, "\n")+1, m.articlesPaneOuterHeight(); got > want {
		t.Fatalf("expected messages pane height <= %d lines, got %d:\n%s", want, got, view)
	}
	if got, want := lipgloss.Width(m.renderMessagesPane()), m.articlesPaneWidth(); got > want {
		t.Fatalf("expected messages pane width <= %d columns, got %d", want, got)
	}
}

func TestScrollingMessageListKeepsPaneHeadersVisible(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.Density = "comfortable"
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 16
	m.focused = paneMessages
	m.accounts = []db.Account{{ID: 1, Name: "Work"}}
	m.mailboxes = []db.Mailbox{{ID: 1, AccountID: 1, Name: "INBOX"}}
	m.selectedMessages = make(map[int64]bool)
	for i := 0; i < 30; i++ {
		msg := db.Message{
			ID:        int64(i + 1),
			MailboxID: 1,
			Subject:   fmt.Sprintf("Message %02d", i),
			From:      "sender@example.com",
			Date:      time.Unix(int64(i), 0),
		}
		m.messages = append(m.messages, msg)
		m.filteredMessages = append(m.filteredMessages, msg)
	}
	m.rebuildSidebar()
	m.messageCursor = 10
	m.listOffset = 8
	m.setViewportForCurrentRow()

	next, _ := m.handleUp()
	m = next.(Model)

	view := ansi.Strip(m.View())
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("expected full view height %d lines, got %d:\n%s", want, got, view)
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if got, want := lipgloss.Width(line), m.width; got > want {
			t.Fatalf("expected rendered line %d width <= %d columns, got %d: %q", i, want, got, ansi.Strip(line))
		}
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "Accounts") || !strings.Contains(lines[0], "Unified Inbox") {
		t.Fatalf("expected pane headers to remain visible on top line, got:\n%s", view)
	}
}

func TestValidateAccountForConnect(t *testing.T) {
	base := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "u", IMAPPort: 993, SMTPPort: 587}
	if got := validateAccountForConnect(base); got != "" {
		t.Fatalf("valid config rejected: %q", got)
	}
	cases := []struct {
		name string
		mut  func(*config.AccountConfig)
	}{
		{"missing name", func(c *config.AccountConfig) { c.Name = "" }},
		{"missing imap host", func(c *config.AccountConfig) { c.IMAPHost = "" }},
		{"missing user", func(c *config.AccountConfig) { c.User = "" }},
		{"imap port too high", func(c *config.AccountConfig) { c.IMAPPort = 70000 }},
		{"smtp port zero", func(c *config.AccountConfig) { c.SMTPPort = 0 }},
		{"imap port negative", func(c *config.AccountConfig) { c.IMAPPort = -1 }},
	}
	for _, tc := range cases {
		cfg := base
		tc.mut(&cfg)
		if got := validateAccountForConnect(cfg); got == "" {
			t.Errorf("%s: expected a validation error, got none", tc.name)
		}
	}
}
