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
			name: "Articles / Content",
			entries: []entry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move content focus line; scroll only when needed"},
				{keys.MarkRead.Help().Key, "mark read; next article in list"},
				{keys.MarkAllRead.Help().Key, "mark current feed as read"},
				{keys.OpenBrowser.Help().Key, "open selected link (content) or article URL"},
				{keys.NextLink.Help().Key, "next actionable link in content"},
				{keys.PrevLink.Help().Key, "previous actionable link in content"},
				{keys.Search.Help().Key, "search articles in current feed"},
				{keys.UnreadOnly.Help().Key, "toggle unread-only view"},
			},
		},
		{
			name: "Feeds",
			entries: []entry{
				bind(keys.Refresh),
				bind(keys.RefreshAll),
				bind(keys.FeedManager),
				{keys.Add.Help().Key, "add feed from anywhere"},
			},
		},
		{
			name: "Feed Manager",
			entries: []entry{
				{keys.Add.Help().Key, "add feed or GReader source"},
				{keys.AddFolder.Help().Key, "add folder"},
				{keys.Enter.Help().Key, "browse remote feed / edit local feed / enter form"},
				{keys.Left.Help().Key, "from field start, back to left list"},
				{keys.Edit.Help().Key, "edit local feed or GReader settings"},
				{keys.Move.Help().Key, "move feed to a folder"},
				{keys.Delete.Help().Key, "delete selected local feed"},
				bind(keys.Import),
				bind(keys.Export),
			},
		},
		{
			name: "Display",
			entries: []entry{
				{"S → Display", "layout density: comfortable or compact (default compact)"},
				{"S → Display", "focus line: highlight current readable content line"},
				{"S → Display", "actionable article links: show links block and enable link navigation"},
			},
		},
		{
			name: "Summary Overlay",
			entries: []entry{
				{keys.Summary.Help().Key, "AI summary from articles or content"},
				{keys.CopyText.Help().Key, "copy summary"},
				{keys.SaveMD.Help().Key, "save summary as .md"},
			},
		},
		{
			name: "App",
			entries: []entry{
				bind(keys.ThemePicker),
				bind(keys.Settings),
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
			"The status bar always shows these shortcuts on the left: m feed manager · S settings · / search · ? help.",
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
