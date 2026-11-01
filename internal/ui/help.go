package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

type helpEntry struct{ key, desc string }

type helpSection struct {
	name    string
	entries []helpEntry
}

// helpSections is the whole help text as data: the two-pane overlay shows one
// section at a time, and renderHelp flattens them all.
func helpSections(keys KeyMap) []helpSection {
	bind := func(b key.Binding) helpEntry {
		h := b.Help()
		return helpEntry{h.Key, h.Desc}
	}

	return []helpSection{
		{
			name: "Status bar",
			entries: []helpEntry{
				{"Pane headers on", "shows M accounts · C contacts · S settings · / search · ? help"},
				{"Pane headers off", "shows the focused pane title and shortcuts; Accounts also keeps the global shortcuts"},
			},
		},
		{
			name: "Global",
			entries: []helpEntry{
				bind(keys.Help),
				bind(keys.Command),
				bind(keys.AccountManager),
				bind(keys.ContactManager),
				bind(keys.Settings),
				bind(keys.Outbox),
				bind(keys.ThemePicker),
				bind(keys.Quit),
				bind(keys.UpdateInstall),
				bind(keys.UpdateIgnore),
			},
		},
		{
			name: "Main Panes",
			entries: []helpEntry{
				{keys.NextPane.Help().Key, "next pane"},
				{keys.PrevPane.Help().Key, "previous pane"},
				{keys.Left.Help().Key, "move left across panes"},
				{keys.Right.Help().Key, "move right across panes"},
				{keys.ResizeAccountsNarrow.Help().Key + "/" + keys.ResizeAccountsWide.Help().Key, "resize accounts pane"},
				{keys.ResizeMessagesShorter.Help().Key + "/" + keys.ResizeMessagesTaller.Help().Key, "resize messages/content split"},
				bind(keys.Up),
				bind(keys.Down),
				{keys.Search.Help().Key, "search messages (#github, #reply, #\"To Do\" match tags); find in content when content is focused"},
				{keys.UnreadOnly.Help().Key, "toggle unread-only view outside the accounts pane"},
				{keys.StarredFirst.Help().Key, "toggle starred-first sort outside the accounts pane"},
				{keys.ToggleThreads.Help().Key, "toggle threaded conversations"},
			},
		},
		{
			name: "Accounts Pane",
			entries: []helpEntry{
				{keys.Enter.Help().Key + "/" + keys.Space.Help().Key, "expand or collapse selected account/section"},
				{keys.Enter.Help().Key, "on a folder row, fetch that folder now; on a folder with subfolders, collapse or expand it"},
				{"←/→", "on a folder: ← collapses it, or jumps to its parent; → expands it"},
				bind(keys.NewFolder),
				bind(keys.RenameFolder),
				bind(keys.DeleteFolder),
				{"new folder", "n on a folder creates a subfolder, on an account or section header a top-level folder; flat accounts always create at the top level; inline prompt, enter to create, esc to cancel"},
				{"delete folder", "asks first and says how many subfolders go with it; deletes on the server. Inbox and system folders are protected, and a folder with a system folder inside it can't be deleted; while a folder change is still running, another is refused"},
				bind(keys.HideFolder),
				{keys.MoveAccountDown.Help().Key + "/" + keys.MoveAccountUp.Help().Key, "walk the folder down or up the tree one row at a time: past a sibling reorders (local to TideMail); onto an open parent it moves into it, past a parent's last or first subfolder it moves out (both real moves on the server). Collapsed folders are stepped over"},
				{keys.IndentFolder.Help().Key + "/" + keys.OutdentFolder.Help().Key, "nest the folder under the one above it (even an empty one) / move it out of its parent, to just after it; unavailable on flat accounts"},
				{"hidden folders", "hide is local only; \"Show hidden folders\" in the command palette lists them again"},
				{"Outbox row", "sits under Unified Inbox; enter/space opens it, its badge counts sends needing you"},
				{keys.Sync.Help().Key, "sync selected mailbox; on Unified Inbox, sync all inboxes"},
				bind(keys.SyncAll),
				{"pause", "rest on a stale folder to refresh it silently; scrolling past does not queue work"},
				{"auto", "folder list refreshes on background sync (≈hourly): webmail-added labels appear, deleted ones are pruned"},
				{keys.MarkAllRead.Help().Key, "mark selected mailbox or account as read"},
				bind(keys.AccountManager),
			},
		},
		{
			name: "Message List",
			entries: []helpEntry{
				{keys.Enter.Help().Key, "open selected message; in Drafts, reopen draft compose"},
				{"auto", "moving down past the last message loads older mail from the server"},
				{keys.Space.Help().Key, "select message and advance"},
				{keys.SelectAll.Help().Key, "select all messages in current view"},
				{keys.MarkRead.Help().Key, "toggle selected message(s) read/unread"},
				{keys.ToggleStar.Help().Key, "toggle star on selected message(s)"},
				{keys.Archive.Help().Key, "archive selected message(s)"},
				{keys.Move.Help().Key, "move selected message(s) to folder/label"},
				{keys.Delete.Help().Key, "delete selected message(s); in Drafts, delete draft"},
				{keys.NeedsYouDismiss.Help().Key, "in Needs You: dismiss; in Waiting on Them: stop waiting (mail and annotations are untouched)"},
				{keys.Snooze.Help().Key, "snooze message(s) or a waiting conversation from Needs You / Waiting on Them; in Snoozed: unsnooze"},
				{keys.RunPlugin.Help().Key, "run a plugin on the current message or selection (when plugins are installed)"},
				{"b (plugin list)", "browse the official plugins: i installs, u updates, x removes; also in the palette as Browse available plugins"},
				{"c / s / p (report result)", "copy a plugin report as text, save it to ~/Downloads/tidemail-reports/, or print it with lp/lpr"},
				{keys.EditTags.Help().Key, "correct classification; compatible categories also save their server tags"},
				{"space (Category)", "toggle the selected server tag; other tags stay saved. Settings → Display → Tag names and colors edits local labels"},
				{keys.PGPDecode.Help().Key, "decrypt and verify the current PGP message through a PGP plugin (shown when one is installed)"},
				{"u (result card)", "unsubscribe when TideMail offers it; uses the normal confirmed unsubscribe flow"},
				{keys.Undo.Help().Key, "cancel queued send, else undo the latest dismiss or stop waiting (in those views), else undo latest archive, move, or delete"},
				bind(keys.Compose),
				bind(keys.Reply),
				bind(keys.ReplyAll),
				bind(keys.Forward),
				{keys.Back.Help().Key, "clear message selection"},
			},
		},
		{
			name: "Content Pane",
			entries: []helpEntry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "Focus line on: move highlight; off: scroll one row"},
				{"LINKS", "Focus off: Down at bottom selects first; arrows step; Up from first exits; no wrap"},
				{keys.Back.Help().Key, "return to message list"},
				{keys.Enter.Help().Key, "open explicitly selected or keyboard-focused visible link"},
				{keys.OpenBrowser.Help().Key, "open explicitly selected or keyboard-focused visible link"},
				{keys.Unsubscribe.Help().Key, "unsubscribe from this sender's list"},
				{keys.NextLink.Help().Key, "next LINKS entry; scroll into view"},
				{keys.PrevLink.Help().Key, "previous LINKS entry; scroll into view"},
				{"[mailto: label]", "compose in TideMail as the source account (default if unavailable); review before sending"},
				{keys.ContentSearch.Help().Key + " or " + keys.Search.Help().Key, "find text in opened message"},
				{"v/V", "visual select line range / whole message"},
				{"y/ctrl+c", "copy selected message text; ctrl+c copies focus line (full URL on a LINKS row) without selection"},
				{keys.ToggleHeaders.Help().Key, "toggle full email headers"},
				{keys.ViewSource.Help().Key, "view the message source; c copies it (also from the message list)"},
				{keys.OpenInBrowser.Help().Key, "view the whole message in your browser, remote content blocked; press again to load images (also from the message list)"},
				{keys.EditTags.Help().Key, "correct this message's classification and manage its tags"},
				{keys.SaveAttach.Help().Key, "save all attachments to folder"},
				{keys.ToggleImages.Help().Key, "load remote images (unsupported in foot/WezTerm)"},
				{keys.ToggleQuote.Help().Key, "toggle quoted text collapse"},
				{keys.Summary.Help().Key, "open AI summary overlay"},
				bind(keys.Reply),
				bind(keys.ReplyAll),
				bind(keys.Forward),
			},
		},
		{
			name: "Compose Modal",
			entries: []helpEntry{
				{"layout", "expands up to 110 columns × 48 rows; a fixed 3-row footer: state, message actions, field keys; plain themes use bracketed keys"},
				{keys.Tab.Help().Key + "/shift+tab", "next/previous field; From opens the sender picker"},
				{"up/down", "previous/next header row (suggestions first); body moves the cursor"},
				{keys.Enter.Help().Key, "open sender picker or advance header fields"},
				{"ctrl+s/ctrl+d", "send message"},
				{"⁂", "signature position: move the symbol, or delete it to send unsigned"},
				{"ctrl+p → Schedule send", "choose a future local date and time"},
				{keys.AttachFile.Help().Key, "attach file"},
				{keys.MaximizeCompose.Help().Key, "maximize/restore compose (session only)"},
				{keys.RemoveAttach.Help().Key, "remove last attachment"},
				{keys.CycleSender.Help().Key, "cycle sender account; From opens the full picker"},
				{keys.PasteText.Help().Key, "paste clipboard into focused field"},
				{keys.GrammarCheck.Help().Key, "AI grammar & spell check (preview scrolls with arrows/pgup/pgdn; y accepts, n cancels)"},
				{keys.Templates.Help().Key, "templates: insert a canned reply at the cursor (see the Templates section)"},
				{keys.SpellCheck.Help().Key, "offline spell check: step through misspelled words (Settings → Editor → Spell check; pick English or Español under Spell check language)"},
				{keys.PGPSign.Help().Key, "sign this message with PGP (needs a PGP plugin; nothing is sent if signing fails)"},
				{keys.PGPEncrypt.Help().Key, "encrypt this message with PGP (needs a PGP plugin; nothing is sent if encryption fails)"},
				{"PGP: no key for…", "every recipient (To, Cc and Bcc) needs a public key in your GnuPG keyring: gpg --list-keys ADDRESS to check, gpg --locate-keys ADDRESS to fetch it, or have them send theirs and gpg --import it. For another address of your own, add it to your key with gpg --quick-add-uid KEYID \"Name <address>\". alt+e sends without encryption"},
				{"PGP passphrase", "asked by GnuPG's pinentry (graphical, from your desktop), not by TideMail; gpg-agent remembers it for a few minutes. If the prompt looks wrong, check pinentry-program in ~/.gnupg/gpg-agent.conf and run gpgconf --reload gpg-agent"},
				{"autosave", "save the draft under the chosen sender while you type"},
				{keys.Undo.Help().Key, "cancel a queued send during the delay set in Settings"},
				{keys.Cancel.Help().Key, "close compose; prompts to save/discard draft"},
			},
		},
		{
			name: "Templates",
			entries: []helpEntry{
				{keys.Templates.Help().Key, "in compose: open the template picker; the highlighted template is previewed in the message area, {tags} shown as written"},
				{"←→ / ↑↓ / tab", "choose a template"},
				{"enter", "insert at the cursor (one undo step) with the tags filled in; the subject is set only if it is empty"},
				{"e", "edit the highlighted template right there; ctrl+s saves it, esc discards the edit. Editing a built-in saves your own copy, which replaces it"},
				{"n", "save the message you are writing as a new template: type a name, enter saves. Quoted text in a reply or forward is left out; names can't repeat, start with \".\" or contain / \\ :"},
				{"esc", "close the picker"},
				{"built-ins", "Thanks, Got it, Out of office, Propose a meeting, Polite decline, Follow up"},
				{"folder", "your templates live in the templates folder next to config.toml (~/.config/tidemail/templates); the picker shows the path"},
				{"files", "one .md or .txt file per template; the file name is the template's name. A file named like a built-in replaces it; delete the file to get the built-in back"},
				{"subject", "start a file with a --- block holding subject: Your subject, then --- on its own line, to set the subject too"},
				{"skipped", "hidden files, files over 64 KB, files that aren't UTF-8 text, and symlinks pointing outside the folder"},
				{"{first_name}", "first word of the display name on the first To recipient, e.g. Priya; \"there\" when that address has no name or To is empty"},
				{"{name}", "the full display name on the first To recipient, e.g. Priya Natarajan (empty if the address has none)"},
				{"{my_name}", "the Name set on the account you are sending from; changes if you switch the From account"},
				{"{date}", "today's date, e.g. October 18, 2026"},
				{"tags", "only the first To recipient is used (not CC/BCC); values are read when you insert, so fill in To first. Unknown {tags} stay as written"},
			},
		},
		{
			name: "Message Body Editor",
			entries: []helpEntry{
				{"ctrl+f", "find in body; ctrl+r inside Find shows Replace and focuses replacement"},
				{"f3 / shift+f3", "next / previous match; wraps at document ends"},
				{"Find controls", "Enter in With replaces one; alt+enter replaces all; alt+c match case; alt+r regex — keys shown on each pill, no Tab needed; Esc closes Find, not compose"},
				{"Regex", "enable Advanced regex search in Settings → Editor to reveal the optional .* toggle; literal by default"},
				{"Regex anchors", "^ and $ match each logical line; \\A and \\z match document boundaries; (?-m) disables multiline mode"},
				{"Regex replacement", "$$ inserts a literal dollar; $0 is the whole match; $1 or ${name} inserts a capture; unknown groups block replacement"},
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
			entries: []helpEntry{
				{"enable", "Settings → Editor → \"Vim keys in compose\" (off by default)"},
				{"i a o O I A", "enter Insert mode (the body starts in Insert)"},
				{"esc", "Insert → Normal; a second esc from Normal cancels (save/discard prompt)"},
				{"h j k l / arrows", "move; w/b/e by word; 0/^/$ line; gg/G document; counts e.g. 3j"},
				{"x dd yy p P D C s", "delete / yank / paste / change"},
				{"d c y + motion", "operators, e.g. dw, cw, d$"},
				{"v / V", "visual / visual-line selection"},
				{"u / ctrl+r", "undo / redo (ctrl+r toggles Replace while Find is open)"},
				{"/ ? n N", "find forward/backward; repeat/reverse; Enter accepts, Esc cancels the search preview"},
				{":w :wq :x", "send the message"},
				{":q", "cancel compose (save/discard prompt)"},
			},
		},
		{
			name: "Account Manager Modal",
			entries: []helpEntry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move through accounts or form fields"},
				{keys.Add.Help().Key, "add account from the account list"},
				{keys.Edit.Help().Key, "edit selected account from the account list"},
				{keys.Delete.Help().Key, "delete selected account from the account list"},
				{keys.Space.Help().Key, "mark the selected account ★ [default] from the account list"},
				// Down first, so the pair reads the way it is spoken and matches
				// the shift+j/k hint under the account list.
				{keys.MoveAccountDown.Help().Key + "/" + keys.MoveAccountUp.Help().Key, "reorder accounts: move the selected account down or up"},
				{"Default sender", "the ★ [default] account: From for a new message, and focused at startup"},
				{"No default", "compose uses the first account; TideMail opens on the Unified Inbox"},
				{"Replies", "reply, reply all and forward send from the account the message arrived on"},
				{"Account order", "reordering also reorders the sidebar and the ctrl+u sender list"},
				{keys.Tab.Help().Key, "next form field"},
				{keys.Left.Help().Key + "/" + keys.Right.Help().Key, "change provider, color, or the Auth method"},
				{keys.Save.Help().Key, "save account form"},
				{keys.TestAccount.Help().Key, "test account form"},
				{keys.PasswordDialog.Help().Key, "on the Password row: enter the password in a native dialog (zenity or kdialog, desktop sessions only; not over SSH); ctrl+s saves it to the keychain like a typed one"},
				{keys.DiscoverServers.Help().Key, "look up Custom account servers (DNS SRV, MX, HTTPS autoconfig); replaces filled hosts"},
				{"Email / login", "leave with an address to look up empty Custom hosts; changing it clears settings from that lookup"},
				{"Form status", "stays below the form; a lookup fills settings without connecting or saving"},
				{keys.OAuthSignIn.Help().Key, "sign in (Gmail / Outlook provider with Auth: OAuth)"},
				{"From address", "blank sends as the username; a display name needs an address: Name <you@example.com>"},
				{"Signature", "enter starts a new line in the account form; tab moves on"},
				{"Auth row", "when available, Gmail/Outlook: ‹ / › picks App password or OAuth sign-in"},
				{"Gmail OAuth", "paused pending Google approval; use a Google App Password"},
				{"Outlook OAuth", "works out of the box; TIDEMAIL_MS_CLIENT_ID for a device code"},
				{"App password", "Gmail: 2-Step Verification → App Passwords; Outlook: M365 only"},
				{"Exchange (on-prem)", "use Custom provider with your server + password (no OAuth)"},
				{"Delete confirm", "y/enter confirms; n/esc cancels"},
			},
		},
		{
			name: "Contact Manager Modal",
			entries: []helpEntry{
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
			entries: []helpEntry{
				{"n", "new rule: choose account, then describe rule in plain English"},
				{keys.Space.Help().Key, "enable or disable selected rule"},
				{keys.Delete.Help().Key, "delete selected rule"},
				{"t", "test selected rule on its accounts' inboxes without changing mail"},
				{"r", "run selected rule on its accounts' inboxes"},
				{"a", "run all rules on all mail"},
				{"move", "new destination folders are created when the rule is saved"},
				{"J/K", "move selected rule down/up in priority"},
				{"Review: ctrl+s/enter", "save generated rule"},
				{"Review: r/a/e", "save+run, save+run all, or edit text"},
			},
		},
		{
			name: "Settings Modal",
			entries: []helpEntry{
				{keys.Up.Help().Key + "/" + keys.Down.Help().Key, "move through sections or fields"},
				{keys.Right.Help().Key + "/" + keys.Enter.Help().Key, "enter selected settings section"},
				{keys.Left.Help().Key, "return from detail to sections"},
				{keys.Tab.Help().Key, "next field"},
				{"shift+tab", "previous field"},
				{keys.Space.Help().Key + "/" + keys.Enter.Help().Key, "toggle or activate focused setting"},
				{keys.Left.Help().Key + "/" + keys.Right.Help().Key, "change picker fields"},
				{keys.Save.Help().Key, "save settings"},
				{"Editor", "set compose keys and the ctrl+z send delay"},
				{"CLI", "tidemail cli: quit after send, skip review, default signature (interactive use only)"},
				{"Personal dictionary", "Editor section: a adds a spell-check word, d removes the selected one, esc goes back"},
				{"Changelog", "read the bundled release notes; j/k or arrows scroll, PgUp/PgDn page, g/G jump to top/bottom"},
				{"Support", "select Open or Copy link with arrows; Enter activates; ctrl+c copies"},
				{"Pane header bars", "show titles and shortcuts above panes; off restores one row and moves focused-pane details to the status bar"},
				{"Needs You / Waiting on Them in sidebar", "off hides that row from the sidebar; nothing is deleted"},
				{keys.Cancel.Help().Key, "back to sections, then cancel from the section list"},
				{"q", "discard and close from the section list"},
			},
		},
		{
			name: "Pickers And Overlays",
			entries: []helpEntry{
				{"Command palette", ": or ctrl+p opens it (contextual commands in compose, summary, save attachments); in a vim compose body : is the editor's command line, so use ctrl+p there"},
				{"Theme picker", "up/down previews; / searches; enter saves; esc cancels"},
				{"Move picker", "enter opens/moves; n creates folder; h/left parent; letters jump"},
				{"Attach file", "enter opens dir/attaches file; h/left parent; . toggles hidden files; letters jump"},
				{"Save attachments", "enter opens/selects folder; h/left parent; . toggles hidden files; letters jump"},
				{"Summary overlay", "C copies; M saves .md; z toggles quoted text; esc closes"},
				{"Search", "/ opens a dedicated row above message subjects; type to filter globally, #tag matches classification or mail-tag names/keywords; quote names with spaces, enter keeps results, / edits again, esc exits; ctrl+f finds in content"},
				{"Confirm dialogs", "enter/y confirms; esc/n cancels"},
			},
		},
		{
			name: "Mouse",
			entries: []helpEntry{
				{"enable", "Settings → Display → Mouse support (off by default; keeps native text selection while off)"},
				{"click", "select a row or LINKS entry, or focus a pane; reader clicks never open links; click a search row to edit"},
				{"double-click", "open a message or draft (same as enter); on a sidebar row, expand/collapse or sync like enter"},
				{"scroll wheel", "move three rows; reader clears link selection; keep pane focus"},
				{"right-click", "select the row and open a menu of its actions (folder, account, or message); enter or click runs one, esc closes"},
				{"overlays", "mouse input is ignored while Settings, Accounts, Compose, or any other overlay is open"},
			},
		},
		{
			name: "Security And Storage",
			entries: []helpEntry{
				{"keychain", "passwords / API keys / OAuth refresh tokens stored in system keychain"},
				{"config", "~/.config/tidemail/config.toml (no plain-text secrets after save)"},
				{"secrets", "passwords, API keys, and OAuth tokens are redacted from errors/status"},
				{"Settings", "display, AI, updates, the changelog, optional support, and desktop notifications live in settings"},
			},
		},
	}
}

// filterHelpSections keeps the entries matching query (on key or description);
// a matching section name keeps the whole section. It also returns how many
// entries matched.
func filterHelpSections(sections []helpSection, query string) ([]helpSection, int) {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return sections, 0
	}
	var out []helpSection
	count := 0
	for _, s := range sections {
		if strings.Contains(strings.ToLower(s.name), query) {
			out = append(out, s)
			count += len(s.entries)
			continue
		}
		var kept []helpEntry
		for _, e := range s.entries {
			if strings.Contains(strings.ToLower(e.key), query) ||
				strings.Contains(strings.ToLower(e.desc), query) {
				kept = append(kept, e)
			}
		}
		if len(kept) > 0 {
			out = append(out, helpSection{name: s.name, entries: kept})
			count += len(kept)
		}
	}
	return out, count
}

// helpSectionLines renders one section: its title, then a row per entry. With
// wrap, long descriptions continue on following lines instead of being cut off.
func helpSectionLines(s helpSection, width int, chrome managerChrome, wrap bool) []string {
	keyW := min(20, max(8, width/3))
	descW := max(1, width-keyW)
	lines := []string{renderSoftGroupTitle(s.name, width, chrome)}
	for _, e := range s.entries {
		parts := []string{e.desc}
		if wrap {
			parts = strings.Split(wrapWords(e.desc, max(1, descW-1)), "\n")
		}
		for i, part := range parts {
			k := ""
			if i == 0 {
				k = e.key
			}
			keyCell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(keyW).Render(truncate(" "+k, max(1, keyW-1)))
			descCell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(part)
			if !wrap {
				descCell = truncateStyled(descCell, descW, chrome.baseBg)
			}
			lines = append(lines, keyCell+padStyled(descCell, descW, chrome.baseBg))
		}
	}
	return lines
}

// renderHelp renders every section (filtered by query) as one scrolling text.
func renderHelp(width int, styles Styles, keys KeyMap, query string) string {
	sections, matchCount := filterHelpSections(helpSections(keys), query)
	query = strings.TrimSpace(strings.ToLower(query))

	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	contentW := max(1, width)
	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(contentW).Render("")

	var lines []string
	if query != "" {
		summary := fmt.Sprintf("%d shortcuts match %q.", matchCount, query)
		if matchCount == 0 {
			summary = fmt.Sprintf("No shortcuts match %q.", query)
		}
		lines = append(lines, padStyled(lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render("  "+summary), contentW, chrome.baseBg), blank)
	}
	for _, s := range sections {
		// The status-bar intro is prose; wrap it rather than clip the sentence.
		lines = append(lines, helpSectionLines(s, contentW, chrome, s.name == "Status bar")...)
		lines = append(lines, blank)
	}
	content := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	return lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(content)
}

// helpLayout is the two-pane overlay's geometry: a category list on the left,
// the selected category on the right.
func helpLayout(termW, termH int) (winW, leftW, rightW, paneH int) {
	winW = max(30, min(termW-6, 104))
	leftW = 24
	if winW < 70 {
		leftW = 18
	}
	rightW = max(10, winW-leftW-1)
	winH := min(termH-4, 38)
	paneH = max(1, winH-4) // title line + bottom border + blank + footer
	return
}

// renderHelpCategories draws the left pane: one row per section, the selected
// one marked, scrolled so it stays visible.
func renderHelpCategories(sections []helpSection, cat int, focused bool, width, height int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	start := 0
	if cat >= height {
		start = cat - height + 1
	}
	rows := make([]string, 0, height)
	for i := start; i < len(sections) && len(rows) < height; i++ {
		label := truncate(sections[i].name, max(1, width-4))
		switch {
		case i == cat && focused:
			rows = append(rows, padStyled(base.Foreground(chrome.accent).Bold(true).Render(" ▸ "+label), width, chrome.baseBg))
		case i == cat:
			rows = append(rows, padStyled(base.Foreground(chrome.text).Bold(true).Render(" ▸ "+label), width, chrome.baseBg))
		default:
			rows = append(rows, padStyled(base.Foreground(chrome.muted).Render("   "+label), width, chrome.baseBg))
		}
	}
	for len(rows) < height {
		rows = append(rows, padStyled(base.Render(""), width, chrome.baseBg))
	}
	return strings.Join(rows, "\n")
}

// helpPaneContent is the right pane's text for the selected section.
func helpPaneContent(sections []helpSection, cat int, query string, width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	if len(sections) == 0 {
		return padStyled(base.Foreground(chrome.muted).Render(fmt.Sprintf("  No shortcuts match %q.", strings.TrimSpace(query))), width, chrome.baseBg)
	}
	return strings.Join(helpSectionLines(sections[min(max(cat, 0), len(sections)-1)], width, chrome, true), "\n")
}
