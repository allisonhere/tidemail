package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

func renderHelp(width int, styles Styles, keys KeyMap, query string) string {
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
				{keys.StarredFirst.Help().Key, "toggle starred-first sort outside the accounts pane"},
				{keys.ToggleThreads.Help().Key, "toggle threaded conversations"},
			},
		},
		{
			name: "Accounts Pane",
			entries: []entry{
				{keys.Enter.Help().Key + "/" + keys.Space.Help().Key, "expand or collapse selected account/section"},
				{keys.Sync.Help().Key, "sync selected mailbox; on Unified Inbox, sync all inboxes"},
				bind(keys.SyncAll),
				{"auto", "folder list refreshes on background sync (≈hourly): webmail-added labels appear, deleted ones are pruned"},
				{keys.MarkAllRead.Help().Key, "mark selected mailbox or account as read"},
				bind(keys.AccountManager),
			},
		},
		{
			name: "Message List",
			entries: []entry{
				{keys.Enter.Help().Key, "open selected message; in Drafts, reopen draft compose"},
				{"auto", "moving down past the last message loads older mail from the server"},
				{keys.Space.Help().Key, "select message and advance"},
				{keys.SelectAll.Help().Key, "select all messages in current view"},
				{keys.MarkRead.Help().Key, "toggle selected message(s) read/unread"},
				{keys.ToggleStar.Help().Key, "toggle star on selected message(s)"},
				{keys.Archive.Help().Key, "archive selected message(s)"},
				{keys.Move.Help().Key, "move selected message(s) to folder/label"},
				{keys.Delete.Help().Key, "delete selected message(s); in Drafts, delete draft"},
				{keys.Undo.Help().Key, "cancel queued send, else undo latest archive, move, or delete"},
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
				{keys.Unsubscribe.Help().Key, "unsubscribe from this sender's list"},
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
				{keys.Tab.Help().Key + "/shift+tab", "next/previous field; From opens the sender picker"},
				{keys.Enter.Help().Key, "open sender picker or advance header fields"},
				{"ctrl+s/ctrl+d", "send message"},
				{keys.AttachFile.Help().Key, "attach file"},
				{keys.RemoveAttach.Help().Key, "remove last attachment"},
				{keys.CycleSender.Help().Key, "cycle sender account; From opens the full picker"},
				{keys.PasteText.Help().Key, "paste clipboard into focused field"},
				{keys.GrammarCheck.Help().Key, "AI grammar & spell check"},
				{"autosave", "save the draft under the chosen sender while you type"},
				{keys.Undo.Help().Key, "cancel a queued send during the delay set in Settings"},
				{keys.Cancel.Help().Key, "close compose; prompts to save/discard draft"},
			},
		},
		{
			name: "Message Body Editor",
			entries: []entry{
				{"ctrl+a", "select all body text"},
				{"shift+arrows", "extend selection by character/line"},
				{"ctrl+shift+arrows", "extend selection by word"},
				{"ctrl+c / ctrl+x", "copy / cut selection to the system clipboard"},
				{"ctrl+v", "paste from the system clipboard"},
				{"ctrl+z / ctrl+y", "undo / redo"},
				{"ctrl+arrows", "move by word"},
				{"home/end", "jump to start/end of the logical line"},
			},
		},
		{
			name: "Message Body Editor — Vim Mode",
			entries: []entry{
				{"enable", "Settings → Editor → \"Vim keys in compose\" (off by default)"},
				{"i a o O I A", "enter Insert mode (the body starts in Insert)"},
				{"esc", "Insert → Normal; a second esc from Normal cancels (save/discard prompt)"},
				{"h j k l / arrows", "move; w/b/e by word; 0/^/$ line; gg/G document; counts e.g. 3j"},
				{"x dd yy p P D C s", "delete / yank / paste / change"},
				{"d c y + motion", "operators, e.g. dw, cw, d$"},
				{"v / V", "visual / visual-line selection"},
				{"u / ctrl+r", "undo / redo"},
				{":w :wq :x", "send the message"},
				{":q", "cancel compose (save/discard prompt)"},
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
				{"Signature", `use \n for a line break in the account form`},
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
				{"move", "new destination folders are created when the rule is saved"},
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
				{"Editor", "set compose keys and the ctrl+z send delay"},
				{keys.Cancel.Help().Key, "back to sections, then cancel from the section list"},
				{"q", "discard and close from the section list"},
			},
		},
		{
			name: "Pickers And Overlays",
			entries: []entry{
				{"Command palette", ": or ctrl+p opens it (contextual commands in compose, summary, save attachments); in a vim compose body : is the editor's command line, so use ctrl+p there"},
				{"Theme picker", "up/down previews; enter saves; esc cancels"},
				{"Move picker", "enter opens/moves; n creates folder; h/left parent; letters jump"},
				{"Attach file", "enter opens dir/attaches file; h/left parent; . toggles hidden files; letters jump"},
				{"Save attachments", "enter opens/selects folder; h/left parent; . toggles hidden files; letters jump"},
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

	// Filter by query: an entry matches on its key or description; a matching
	// section name keeps the whole section.
	query = strings.TrimSpace(strings.ToLower(query))
	matchCount := 0
	if query != "" {
		filtered := sections[:0]
		for _, s := range sections {
			if strings.Contains(strings.ToLower(s.name), query) {
				filtered = append(filtered, s)
				matchCount += len(s.entries)
				continue
			}
			var kept []entry
			for _, e := range s.entries {
				if strings.Contains(strings.ToLower(e.key), query) ||
					strings.Contains(strings.ToLower(e.desc), query) {
					kept = append(kept, e)
				}
			}
			if len(kept) > 0 {
				filtered = append(filtered, section{name: s.name, entries: kept})
				matchCount += len(kept)
			}
		}
		sections = filtered
	}

	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	contentW := max(1, width)
	keyW := min(20, max(8, contentW/3))
	descW := max(1, contentW-keyW)
	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(contentW).Render("")
	muted := func(s string) string {
		return padStyled(lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render("  "+s), contentW, chrome.baseBg)
	}

	var lines []string
	if query != "" {
		summary := fmt.Sprintf("%d shortcuts match %q.", matchCount, query)
		if matchCount == 0 {
			summary = fmt.Sprintf("No shortcuts match %q.", query)
		}
		lines = append(lines, muted(summary), blank)
	} else {
		lines = append(lines, muted("The status bar always shows: M accounts · C contacts · S settings · / search · ? help"), blank)
	}

	for _, s := range sections {
		lines = append(lines, renderSoftGroupTitle(s.name, contentW, chrome))
		for _, e := range s.entries {
			keyCell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(keyW).Render(truncate(" "+e.key, max(1, keyW-1)))
			descCell := truncateStyled(lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(e.desc), descW, chrome.baseBg)
			lines = append(lines, keyCell+padStyled(descCell, descW, chrome.baseBg))
		}
		lines = append(lines, blank)
	}

	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	return lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(content)
}
