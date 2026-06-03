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
			name: "Navigation",
			entries: []entry{
				{keys.NextPane.Help().Key, "next pane"},
				{keys.PrevPane.Help().Key, "previous pane"},
				{keys.Left.Help().Key, "move left across panes"},
				{keys.Right.Help().Key, "move right across panes"},
				bind(keys.Up),
				bind(keys.Down),
				{keys.Enter.Help().Key, "open article / enter pane"},
				bind(keys.Back),
			},
		},
		{
			name: "Messages / Content",
			entries: []entry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move content focus line; scroll only when needed"},
				{keys.Space.Help().Key, "select message and advance; d/a/x for bulk delete/archive/mark-read"},
				{keys.SelectAll.Help().Key, "select all messages in current view"},
				{keys.MarkRead.Help().Key, "mark read; next message in list"},
				{keys.MarkAllRead.Help().Key, "mark current mailbox as read"},
				{keys.OpenBrowser.Help().Key, "open selected link"},
				{keys.NextLink.Help().Key, "next actionable link in content"},
				{keys.PrevLink.Help().Key, "previous actionable link in content"},
				{keys.Search.Help().Key, "search messages in current mailbox"},
				{keys.UnreadOnly.Help().Key, "toggle unread-only view"},
				bind(keys.Archive),
				{keys.Move.Help().Key, "move selected message(s) to folder/label"},
				{keys.Delete.Help().Key, "delete selected message"},
				bind(keys.Compose),
				bind(keys.Reply),
				bind(keys.Forward),
				{keys.GrammarCheck.Help().Key, "AI grammar & spell check (in compose)"},
				{keys.ToggleHeaders.Help().Key, "toggle full email headers"},
				{keys.SaveAttach.Help().Key, "save all attachments to folder"},
				{keys.ToggleQuote.Help().Key, "toggle quoted text collapse"},
			},
		},
		{
			name: "Accounts / Contacts",
			entries: []entry{
				bind(keys.Sync),
				{keys.Sync.Help().Key + " (on Unified)", "sync all inboxes"},
				bind(keys.SyncAll),
				bind(keys.AccountManager),
				{keys.Add.Help().Key, "add account"},
				{keys.Edit.Help().Key, "edit account"},
				{keys.Delete.Help().Key, "delete account"},
				{"Gmail: Auth toggle", "switch between Password / OAuth2 in account form"},
				{"Gmail: Sign in", "press Enter on [Sign in with Google] to launch OAuth2"},
				{"Settings → Accounts", "set sync interval per account for auto-refresh"},
				bind(keys.ContactManager),
				{"Contacts: Space/c", "select contacts, then compose to selected contact(s)"},
				{"Contacts: n/e/d", "new, edit, or delete contacts used by compose autocomplete"},
				{"Contacts: f", "add addresses already seen in mail"},
				{"Contacts: i/x", "import/export vCard name, email, phone, org, title, note"},
			},
		},
		{
			name: "Security",
			entries: []entry{
				{"keychain", "passwords / API keys / OAuth2 tokens stored in system keychain"},
				{"config", "~/.config/tidemail/config.toml (no plain-text secrets after save)"},
				{"secrets", "passwords, API keys, and OAuth2 tokens are redacted from errors/status"},
			},
		},
		{
			name: "Display",
			entries: []entry{
				{"S → Display", "layout density: comfortable or compact (default compact)"},
				{"S → Display", "focus line: highlight current readable content line"},
				{"S → Display", "actionable links: show links block and enable link navigation"},
				{"S → Display", "show email headers: show headers when opening a message"},
				{"S → Display", "desktop notifications: notify on new unread mail from auto-sync"},
			},
		},
		{
			name: "Summary Overlay",
			entries: []entry{
				{keys.Summary.Help().Key, "AI summary from messages or content"},
				{keys.CopyText.Help().Key, "copy summary"},
				{keys.SaveMD.Help().Key, "save summary as .md"},
			},
		},
		{
			name: "App",
			entries: []entry{
				bind(keys.ThemePicker),
				bind(keys.Settings),
				bind(keys.ContactManager),
				bind(keys.Command),
				bind(keys.UpdateInstall),
				bind(keys.UpdateIgnore),
				bind(keys.Help),
				bind(keys.Quit),
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
			"The status bar always shows these shortcuts on the left: M accounts · S settings · / search · ? help.",
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
