package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

func renderHelp(width int, styles Styles, keys KeyMap) string {
	type entry struct{ key, desc string }
	type section struct {
		name    string
		entries []entry
	}

	bind := func(b key.Binding) entry {
		h := b.Help()
		return entry{h.Key, h.Desc}
	}

	sections := []section{
		{
			name: "Global",
			entries: []entry{
				bind(keys.Help),
				bind(keys.Command),
				bind(keys.AccountManager),
				bind(keys.ContactManager),
				bind(keys.Settings),
				bind(keys.ThemePicker),
				bind(keys.Quit),
				bind(keys.UpdateInstall),
				bind(keys.UpdateIgnore),
			},
		},
		{
			name: "Main Panes",
			entries: []entry{
				{keys.NextPane.Help().Key, "next pane"},
				{keys.PrevPane.Help().Key, "previous pane"},
				{keys.Left.Help().Key, "move left across panes"},
				{keys.Right.Help().Key, "move right across panes"},
				{keys.ResizeAccountsNarrow.Help().Key + "/" + keys.ResizeAccountsWide.Help().Key, "resize accounts pane"},
				{keys.ResizeMessagesShorter.Help().Key + "/" + keys.ResizeMessagesTaller.Help().Key, "resize messages/content split"},
				bind(keys.Up),
				bind(keys.Down),
				{keys.Search.Help().Key, "search messages; find in content when content is focused"},
				{keys.UnreadOnly.Help().Key, "toggle unread-only view outside the accounts pane"},
				{keys.ToggleThreads.Help().Key, "toggle threaded conversations"},
			},
		},
		{
			name: "Accounts Pane",
			entries: []entry{
				{keys.Enter.Help().Key + "/" + keys.Space.Help().Key, "expand or collapse selected account/section"},
				{keys.Sync.Help().Key, "sync selected mailbox; on Unified Inbox, sync all inboxes"},
				bind(keys.SyncAll),
				{keys.MarkAllRead.Help().Key, "mark selected mailbox or account as read"},
				bind(keys.AccountManager),
			},
		},
		{
			name: "Message List",
			entries: []entry{
				{keys.Enter.Help().Key, "open selected message; in Drafts, reopen draft compose"},
				{keys.Space.Help().Key, "select message and advance"},
				{keys.SelectAll.Help().Key, "select all messages in current view"},
				{keys.MarkRead.Help().Key, "toggle selected message(s) read/unread"},
				{keys.Archive.Help().Key, "archive selected message(s)"},
				{keys.Move.Help().Key, "move selected message(s) to folder/label"},
				{keys.Delete.Help().Key, "delete selected message(s); in Drafts, delete draft"},
				bind(keys.Compose),
				bind(keys.Reply),
				bind(keys.Forward),
				{keys.Back.Help().Key, "clear message selection"},
			},
		},
		{
			name: "Content Pane",
			entries: []entry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move focus line; scroll only when needed"},
				{keys.Back.Help().Key, "return to message list"},
				{keys.OpenBrowser.Help().Key, "open link on focus line, else selected link"},
				{keys.NextLink.Help().Key, "next actionable link"},
				{keys.PrevLink.Help().Key, "previous actionable link"},
				{keys.ContentSearch.Help().Key + " or " + keys.Search.Help().Key, "find text in opened message"},
				{"v/V", "visual select line range / whole message"},
				{"y/ctrl+c", "copy selected message text; ctrl+c copies focus line without selection"},
				{keys.ToggleHeaders.Help().Key, "toggle full email headers"},
				{keys.SaveAttach.Help().Key, "save all attachments to folder"},
				{keys.ToggleQuote.Help().Key, "toggle quoted text collapse"},
				{keys.Summary.Help().Key, "open AI summary overlay"},
				bind(keys.Reply),
				bind(keys.Forward),
			},
		},
		{
			name: "Compose Modal",
			entries: []entry{
				{keys.Tab.Help().Key, "next field; accept recipient suggestion when pending"},
				{keys.Enter.Help().Key, "advance header fields"},
				{"ctrl+s/ctrl+d", "send message"},
				{keys.AttachFile.Help().Key, "attach file"},
				{keys.RemoveAttach.Help().Key, "remove last attachment"},
				{keys.CycleSender.Help().Key, "change sender account; existing draft moves with sender"},
				{keys.PasteText.Help().Key, "paste clipboard into focused field"},
				{keys.GrammarCheck.Help().Key, "AI grammar & spell check"},
				{"autosave", "compose saves to the account's Drafts automatically as you type"},
				{keys.Cancel.Help().Key, "close compose; prompts to save/discard draft"},
			},
		},
		{
			name: "Account Manager Modal",
			entries: []entry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move through accounts or form fields"},
				{keys.Add.Help().Key, "add account from the account list"},
				{keys.Edit.Help().Key, "edit selected account from the account list"},
				{keys.Delete.Help().Key, "delete selected account from the account list"},
				{keys.Tab.Help().Key, "next form field"},
				{keys.Left.Help().Key + "/" + keys.Right.Help().Key, "change provider or color fields"},
				{keys.SaveAccount.Help().Key, "save account form"},
				{keys.TestAccount.Help().Key, "test account form"},
				{"Gmail", "use a Google App Password (2-Step Verification → App passwords)"},
				{"Delete confirm", "y/enter confirms; n/esc cancels"},
			},
		},
		{
			name: "Contact Manager Modal",
			entries: []entry{
				{keys.Space.Help().Key, "select contact and advance"},
				{"c", "compose to selected contact(s)"},
				{"n", "new contact for compose autocomplete"},
				{keys.Edit.Help().Key, "edit selected contact"},
				{keys.Delete.Help().Key, "delete selected contact(s)"},
				{"f", "add addresses already seen in mail"},
				{"i/x", "import/export vCard name, email, phone, org, title, note"},
				{keys.Tab.Help().Key, "next contact form field"},
				{"Seen picker", "type to filter; space selects; enter/a adds"},
				{"File picker", "enter opens/selects; h/left goes parent; letters jump"},
			},
		},
		{
			name: "Filters Modal",
			entries: []entry{
				{"n", "new rule: choose account, then describe rule in plain English"},
				{keys.Space.Help().Key, "enable or disable selected rule"},
				{keys.Delete.Help().Key, "delete selected rule"},
				{"t", "test rules on selected mailbox without changing mail"},
				{"r/R", "run rules on selected mailbox / all mail"},
				{"J/K", "move selected rule down/up in priority"},
				{"Review: s/enter", "save generated rule"},
				{"Review: r/R/e", "save+run, save+run all, or edit text"},
			},
		},
		{
			name: "Settings Modal",
			entries: []entry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move through sections or fields"},
				{keys.Right.Help().Key + "/" + keys.Enter.Help().Key, "enter selected settings section"},
				{keys.Left.Help().Key, "return from detail to sections"},
				{keys.Tab.Help().Key, "next field"},
				{"shift+tab", "previous field"},
				{keys.Space.Help().Key + "/" + keys.Enter.Help().Key, "toggle or activate focused setting"},
				{keys.Left.Help().Key + "/" + keys.Right.Help().Key, "change picker fields"},
				{keys.SaveAccount.Help().Key, "save settings"},
				{keys.Cancel.Help().Key, "back to sections, then cancel from the section list"},
				{"q", "discard and close from the section list"},
			},
		},
		{
			name: "Pickers And Overlays",
			entries: []entry{
				{"Command palette", ": or ctrl+p opens it from the main UI; : also opens contextual commands in compose, summary, and save attachments"},
				{"Theme picker", "up/down previews; enter saves; esc cancels"},
				{"Move picker", "enter opens/moves; n creates folder; h/left parent; letters jump"},
				{"Save attachments", "enter opens/selects folder; h/left parent; letters jump"},
				{"Summary overlay", "C copies; M saves .md; z toggles quoted text; esc closes"},
				{"Search overlays", "/ enters persistent search mode — type to filter globally, enter stops editing, esc exits; ctrl+f opens in-content find overlay"},
				{"Confirm dialogs", "enter/y confirms; esc/n cancels"},
			},
		},
		{
			name: "Security And Storage",
			entries: []entry{
				{"keychain", "passwords / API keys stored in system keychain"},
				{"config", "~/.config/tidemail/config.toml (no plain-text secrets after save)"},
				{"secrets", "passwords and API keys are redacted from errors/status"},
				{"Settings", "display, AI, update, sync, and desktop notifications live in settings"},
			},
		},
	}

	contentW := max(1, width)
	bodyInnerW := max(1, contentW-styles.HelpSectionBody.GetHorizontalFrameSize())
	keyW := min(20, max(8, bodyInnerW/3))
	descW := max(1, bodyInnerW-keyW)
	sectionBg := terminalColorAsColor(styles.HelpSectionBody.GetBackground())

	lines := []string{
		lipgloss.NewStyle().
			Width(contentW).
			Render("  Help — Keyboard Shortcuts"),
		"",
		styles.HelpSectionBody.Width(contentW).Render(
			"The status bar always shows these shortcuts on the left: M accounts · C contacts · S settings · / search · ? help.",
		),
		"",
	}

	for _, s := range sections {
		rows := []string{styles.HelpSection.Width(contentW).Render(s.name)}
		for _, e := range s.entries {
			line := styles.HelpKey.Width(keyW).Render(" " + e.key)
			line += styles.HelpDesc.Width(descW).Render(e.desc)
			line = clampView(line, bodyInnerW, 1, sectionBg)
			rows = append(rows, styles.HelpSectionBody.Width(contentW).Render(line))
		}
		rows = append(rows, styles.HelpSectionBody.Width(contentW).Render(""))
		lines = append(lines, strings.Join(rows, "\n"), "")
	}

	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	return lipgloss.NewStyle().
		Width(width).
		PaddingTop(1).
		Render(content)
}
