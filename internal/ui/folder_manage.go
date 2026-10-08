package ui

// Folder management from the sidebar: n creates a (sub)folder, r renames the
// focused folder, d deletes it after a confirmation. These act on real IMAP
// folders, so rename and delete reach the server.

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
)

// folderPrompt is the inline name prompt shown in the sidebar for create and
// rename.
type folderPrompt struct {
	active    bool
	rename    bool
	accountID int64
	mailboxID int64  // rename: the folder being renamed
	parent    string // create: full name of the parent folder, "" for top level
	input     textinput.Model
}

// FolderRenamedMsg reports the result of a rename.
type FolderRenamedMsg struct {
	AccountID int64
	OldName   string
	NewName   string
	Err       error
}

// FolderDeletedMsg reports the result of deleting a folder and its subfolders.
type FolderDeletedMsg struct {
	AccountID int64
	Name      string
	Removed   []int64
	Err       error
}

// folderDelimiter is the hierarchy delimiter in use for a mailbox.
func folderDelimiter(mb db.Mailbox) string { return moveMailboxDelimiter(mb) }

// folderLeaf is the last path segment of a mailbox name.
func folderLeaf(mb db.Mailbox) string {
	if i := strings.LastIndex(mb.Name, folderDelimiter(mb)); i >= 0 {
		return mb.Name[i+len(folderDelimiter(mb)):]
	}
	return mb.Name
}

// folderParentName is everything above the last segment ("" at the top level).
func folderParentName(mb db.Mailbox) string {
	if i := strings.LastIndex(mb.Name, folderDelimiter(mb)); i >= 0 {
		return mb.Name[:i]
	}
	return ""
}

// protectedFolderReason says why a folder may not be renamed or deleted, or ""
// when it may. Servers and TideMail itself depend on these.
func protectedFolderReason(mb db.Mailbox) string {
	lower := strings.ToLower(mb.Name)
	if lower == "inbox" {
		return "the Inbox can't be renamed or deleted"
	}
	for _, f := range []string{`\Sent`, `\Trash`, `\Drafts`, `\Junk`, `\Archive`, `\All`, `\Flagged`, `\Important`} {
		if hasFlag(mb.Flags, f) {
			return "system folders can't be renamed or deleted"
		}
	}
	if isGmailSystemFolder(mb.Name) || strings.HasPrefix(mb.Name, "[") {
		return "system folders can't be renamed or deleted"
	}
	// Only top-level standard names are system folders ("Work/Sent" is not).
	leaf := folderLeaf(mb)
	top := strings.EqualFold(mb.Name, leaf) || strings.EqualFold(strings.TrimPrefix(strings.ToUpper(mb.Name), "INBOX"+strings.ToUpper(folderDelimiter(mb))), strings.ToUpper(leaf))
	if top && mailboxRank(leaf) < 6 {
		return "system folders can't be renamed or deleted"
	}
	return ""
}

// validateFolderName checks one path segment typed by the user.
func validateFolderName(name, delimiter string, siblings []db.Mailbox, parent string, skipID int64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "folder name can't be empty"
	}
	if delimiter != "" && strings.Contains(name, delimiter) {
		return fmt.Sprintf("folder names can't contain %q", delimiter)
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '*' || r == '%' {
			return "folder name has invalid characters"
		}
	}
	full := name
	if parent != "" {
		full = parent + delimiter + name
	}
	for _, sib := range siblings {
		if sib.ID != skipID && strings.EqualFold(sib.Name, full) {
			return fmt.Sprintf("a folder named %q already exists", name)
		}
	}
	return ""
}

// folderSubtree is the folder plus every folder below it.
func (m Model) folderSubtree(mb db.Mailbox) []db.Mailbox {
	prefix := mb.Name + folderDelimiter(mb)
	var out []db.Mailbox
	for _, other := range m.mailboxes {
		if other.AccountID != mb.AccountID {
			continue
		}
		if other.ID == mb.ID || strings.HasPrefix(other.Name, prefix) {
			out = append(out, other)
		}
	}
	return out
}

// sidebarFolderTarget resolves the sidebar cursor to an account and, when it is
// on a folder, that folder.
func (m Model) sidebarFolderTarget() (accountID int64, mb *db.Mailbox, ok bool) {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return 0, nil, false
	}
	row := m.sidebarRows[m.sidebarCursor]
	switch row.kind {
	case rowKindMailbox:
		if f := m.mailboxByID(row.mailboxID); f != nil {
			return f.AccountID, f, true
		}
	case rowKindAccount, rowKindSysFolderHeader, rowKindPersonalFolderHeader:
		return row.accountID, nil, row.accountID != 0
	}
	return 0, nil, false
}

func newFolderInput(value string) textinput.Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 200
	in.SetValue(value)
	in.CursorEnd()
	in.Focus()
	return in
}

func (m Model) startNewFolder() (tea.Model, tea.Cmd) {
	accountID, mb, ok := m.sidebarFolderTarget()
	if !ok {
		m.setStatus("move to an account or folder to create a folder", false)
		return m, m.clearStatusCmd()
	}
	parent := ""
	// A subfolder of the Inbox is a top-level folder in the account's namespace.
	if mb != nil && !strings.EqualFold(mb.Name, "INBOX") {
		parent = mb.Name
	}
	m.folderPrompt = folderPrompt{active: true, accountID: accountID, parent: parent, input: newFolderInput("")}
	m.setStatus("new folder name: enter creates, esc cancels", false)
	return m, nil
}

func (m Model) startRenameFolder() (tea.Model, tea.Cmd) {
	_, mb, ok := m.sidebarFolderTarget()
	if !ok || mb == nil {
		m.setStatus("select a folder to rename", false)
		return m, m.clearStatusCmd()
	}
	if reason := protectedFolderReason(*mb); reason != "" {
		m.setStatus(reason, true)
		return m, m.clearStatusCmd()
	}
	m.folderPrompt = folderPrompt{active: true, rename: true, accountID: mb.AccountID, mailboxID: mb.ID, input: newFolderInput(folderLeaf(*mb))}
	m.setStatus("rename folder: enter confirms, esc cancels", false)
	return m, nil
}

func (m Model) startDeleteFolder() (tea.Model, tea.Cmd) {
	_, mb, ok := m.sidebarFolderTarget()
	if !ok || mb == nil {
		m.setStatus("select a folder to delete", false)
		return m, m.clearStatusCmd()
	}
	if reason := protectedFolderReason(*mb); reason != "" {
		m.setStatus(reason, true)
		return m, m.clearStatusCmd()
	}
	m.pendingFolderDelete = mb.ID
	m.overlay = overlayFolderDeleteConfirm
	return m, nil
}

// folderDeleteConfirmText states what deleting will do, including subfolders.
func (m Model) folderDeleteConfirmText() string {
	mb := m.mailboxByID(m.pendingFolderDelete)
	if mb == nil {
		return "Delete this folder?"
	}
	title := cleanDisplayName(mb.Name)
	subs := len(m.folderSubtree(*mb)) - 1
	text := fmt.Sprintf("Delete %q from the server?\n\nEvery message in it is deleted.", title)
	if subs == 1 {
		text = fmt.Sprintf("Delete %q and its 1 subfolder from the server?\n\nEvery message in them is deleted.", title)
	} else if subs > 1 {
		text = fmt.Sprintf("Delete %q and its %d subfolders from the server?\n\nEvery message in them is deleted.", title, subs)
	}
	return text + "\nThis can't be undone."
}

func (m Model) handleFolderDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
		id := m.pendingFolderDelete
		m.overlay = overlayNone
		m.pendingFolderDelete = 0
		mb := m.mailboxByID(id)
		if mb == nil {
			return m, nil
		}
		m.setStatus("deleting folder...", false)
		return m, m.deleteFolderCmd(*mb)
	case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
		m.overlay = overlayNone
		m.pendingFolderDelete = 0
	}
	return m, nil
}

func (m Model) handleFolderPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.folderPrompt = folderPrompt{}
		m.setStatus("", false)
		return m, nil
	case keyMatches(msg, m.keys.Confirm):
		return m.submitFolderPrompt()
	}
	var cmd tea.Cmd
	m.folderPrompt.input, cmd = m.folderPrompt.input.Update(msg)
	return m, cmd
}

func (m Model) submitFolderPrompt() (tea.Model, tea.Cmd) {
	p := m.folderPrompt
	name := strings.TrimSpace(p.input.Value())
	accountMbs := accountMailboxes(m.mailboxes, p.accountID)

	if p.rename {
		mb := m.mailboxByID(p.mailboxID)
		if mb == nil {
			m.folderPrompt = folderPrompt{}
			return m, nil
		}
		if name == folderLeaf(*mb) {
			m.folderPrompt = folderPrompt{}
			return m, nil
		}
		if msg := validateFolderName(name, folderDelimiter(*mb), accountMbs, folderParentName(*mb), mb.ID); msg != "" {
			m.setStatus(msg, true)
			return m, nil
		}
		newName := name
		if parent := folderParentName(*mb); parent != "" {
			newName = parent + folderDelimiter(*mb) + name
		}
		m.folderPrompt = folderPrompt{}
		m.setStatus("renaming folder...", false)
		return m, m.renameFolderCmd(*mb, newName)
	}

	delimiter := moveDelimiter(m.mailboxes, p.accountID, p.parent)
	if msg := validateFolderName(name, delimiter, accountMbs, p.parent, 0); msg != "" {
		m.setStatus(msg, true)
		return m, nil
	}
	m.folderPrompt = folderPrompt{}
	m.setStatus("creating folder...", false)
	return m, m.createFolderOnServerCmd(p.accountID, p.parent, name)
}

// accountCfgForAccountID resolves the configured account behind a DB account.
func (m Model) accountCfgForAccountID(accountID int64) (config.AccountConfig, error) {
	acc := m.accountByID(accountID)
	if acc == nil {
		return config.AccountConfig{}, fmt.Errorf("%w (account %d)", errNoAccountConfig, accountID)
	}
	return m.accountConfigFor(*acc)
}

// createFolderOnServerCmd creates parentPath/name, or a top-level folder in the
// account's namespace when parentPath is empty.
func (m Model) createFolderOnServerCmd(accountID int64, parentPath, name string) tea.Cmd {
	delimiter := moveDelimiter(m.mailboxes, accountID, parentPath)
	fullName := name
	if parentPath != "" {
		fullName = parentPath + delimiter + name
	} else {
		fullName, delimiter = qualifyFolderName(name, accountMailboxes(m.mailboxes, accountID))
	}
	acfg, err := m.accountCfgForAccountID(accountID)
	if err != nil {
		return func() tea.Msg { return FolderCreatedMsg{AccountID: accountID, Name: fullName, Err: err} }
	}
	database, sessions := m.db, m.sessions
	return func() tea.Msg {
		if acfg.IMAPHost != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.CreateMailbox(ctx, fullName)
			}); err != nil {
				return FolderCreatedMsg{AccountID: accountID, Name: fullName, Err: err}
			}
		}
		id, err := database.UpsertMailbox(db.Mailbox{
			AccountID:   accountID,
			Name:        fullName,
			DisplayName: cleanDisplayName(fullName),
			Delimiter:   delimiter,
		})
		if err != nil {
			return FolderCreatedMsg{AccountID: accountID, Name: fullName, Err: err}
		}
		return FolderCreatedMsg{AccountID: accountID, MailboxID: id, Name: fullName, Delimiter: delimiter}
	}
}

func (m Model) renameFolderCmd(mb db.Mailbox, newName string) tea.Cmd {
	acfg, err := m.accountCfgForAccountID(mb.AccountID)
	if err != nil {
		return func() tea.Msg {
			return FolderRenamedMsg{AccountID: mb.AccountID, OldName: mb.Name, NewName: newName, Err: err}
		}
	}
	database, sessions := m.db, m.sessions
	delimiter := folderDelimiter(mb)
	return func() tea.Msg {
		fail := func(err error) tea.Msg {
			return FolderRenamedMsg{AccountID: mb.AccountID, OldName: mb.Name, NewName: newName, Err: err}
		}
		if acfg.IMAPHost != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				return client.RenameMailbox(ctx, mb.Name, newName)
			}); err != nil {
				return fail(err)
			}
		}
		if err := database.RenameMailboxTree(mb.AccountID, mb.Name, newName, delimiter, cleanDisplayName); err != nil {
			return fail(err)
		}
		return FolderRenamedMsg{AccountID: mb.AccountID, OldName: mb.Name, NewName: newName}
	}
}

func (m Model) deleteFolderCmd(mb db.Mailbox) tea.Cmd {
	acfg, err := m.accountCfgForAccountID(mb.AccountID)
	if err != nil {
		return func() tea.Msg { return FolderDeletedMsg{AccountID: mb.AccountID, Name: mb.Name, Err: err} }
	}
	tree := m.folderSubtree(mb)
	// Deepest first: a server refuses to delete a folder that still has
	// subfolders below it.
	slices.SortStableFunc(tree, func(a, b db.Mailbox) int { return len(b.Name) - len(a.Name) })
	database, sessions := m.db, m.sessions
	return func() tea.Msg {
		var removed []int64
		var firstErr error
		if acfg.IMAPHost != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			firstErr = sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
				for _, f := range tree {
					if err := client.DeleteMailbox(ctx, f.Name); err != nil {
						return err
					}
					removed = append(removed, f.ID)
				}
				return nil
			})
		} else {
			for _, f := range tree {
				removed = append(removed, f.ID)
			}
		}
		// Forget exactly what the server deleted, even after a partial failure.
		var kept []int64
		for _, id := range removed {
			if err := database.ResetMailboxCache(id); err != nil {
				continue
			}
			if err := database.DeleteMailbox(id); err != nil {
				continue
			}
			kept = append(kept, id)
		}
		for _, f := range tree {
			if slices.Contains(kept, f.ID) {
				_ = database.DeleteMailboxPrefs(f.AccountID, f.Name)
			}
		}
		return FolderDeletedMsg{AccountID: mb.AccountID, Name: mb.Name, Removed: kept, Err: firstErr}
	}
}

func (m Model) handleFolderRenamed(msg FolderRenamedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus("rename folder failed: "+msg.Err.Error(), true)
		return m, m.clearStatusCmd()
	}
	var selected int64
	if m.sidebarCursor >= 0 && m.sidebarCursor < len(m.sidebarRows) {
		selected = m.sidebarRows[m.sidebarCursor].mailboxID
	}
	for i := range m.mailboxes {
		mb := &m.mailboxes[i]
		if mb.AccountID != msg.AccountID {
			continue
		}
		prefix := msg.OldName + folderDelimiter(*mb)
		switch {
		case mb.Name == msg.OldName:
			mb.Name = msg.NewName
		case strings.HasPrefix(mb.Name, prefix):
			mb.Name = msg.NewName + folderDelimiter(*mb) + mb.Name[len(prefix):]
		default:
			continue
		}
		mb.DisplayName = cleanDisplayName(mb.Name)
	}
	// Hide/order prefs are keyed by name too (the DB already moved them).
	for name, pref := range m.mailboxPrefs[msg.AccountID] {
		if name == msg.OldName || strings.HasPrefix(name, msg.OldName+"/") || strings.HasPrefix(name, msg.OldName+".") {
			delete(m.mailboxPrefs[msg.AccountID], name)
			m.mailboxPrefs[msg.AccountID][msg.NewName+name[len(msg.OldName):]] = pref
		}
	}
	// Collapse state is keyed by name, so carry it to the new names.
	for key, collapsed := range m.collapsedSections {
		oldKey := fmt.Sprintf("folder:%d:", msg.AccountID)
		if !strings.HasPrefix(key, oldKey) || !collapsed {
			continue
		}
		name := key[len(oldKey):]
		if name == msg.OldName || strings.HasPrefix(name, msg.OldName+"/") || strings.HasPrefix(name, msg.OldName+".") {
			delete(m.collapsedSections, key)
			m.collapsedSections[oldKey+msg.NewName+name[len(msg.OldName):]] = true
		}
	}
	m.saveCollapseState()
	m.rebuildSidebar()
	if selected != 0 {
		m.cursorToMailbox(selected)
	}
	m.setStatus("folder renamed: "+cleanDisplayName(msg.NewName), false)
	return m, m.clearStatusCmd()
}

func (m Model) handleFolderDeleted(msg FolderDeletedMsg) (tea.Model, tea.Cmd) {
	removed := make(map[int64]bool, len(msg.Removed))
	for _, id := range msg.Removed {
		removed[id] = true
	}
	var selectedGone bool
	if m.sidebarCursor >= 0 && m.sidebarCursor < len(m.sidebarRows) {
		selectedGone = removed[m.sidebarRows[m.sidebarCursor].mailboxID]
	}
	if len(removed) > 0 {
		kept := m.mailboxes[:0:0]
		for _, mb := range m.mailboxes {
			if !removed[mb.ID] {
				kept = append(kept, mb)
			} else {
				delete(m.mailboxPrefs[mb.AccountID], mb.Name)
			}
		}
		m.mailboxes = kept
		m.rebuildSidebar()
		if selectedGone {
			m.clearMessages()
		}
	}
	if msg.Err != nil {
		m.setStatus("delete folder failed: "+msg.Err.Error(), true)
		return m, m.clearStatusCmd()
	}
	m.setStatus("folder deleted: "+cleanDisplayName(msg.Name), false)
	return m, m.clearStatusCmd()
}

// folderPromptRow renders the inline name prompt.
func (m Model) folderPromptRow(depth int, width int) string {
	label := "New folder: "
	if m.folderPrompt.rename {
		label = "Rename: "
	}
	line := strings.Repeat("  ", depth) + "  " + label + m.folderPrompt.input.View()
	return m.sidebarSelectedStyle("").Width(width).Render(truncateStyledPlain(line, width))
}

func truncateStyledPlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return truncateStyled(s, width, "")
}

// ── Local view prefs: hide and reorder ─────────────────────────────────────

func (m *Model) loadMailboxPrefs() {
	if m.db == nil {
		return
	}
	if prefs, err := m.db.ListMailboxPrefs(); err == nil {
		m.mailboxPrefs = prefs
	}
}

func (m *Model) setPref(accountID int64, name string, f func(*db.MailboxPref)) {
	if m.mailboxPrefs == nil {
		m.mailboxPrefs = map[int64]map[string]db.MailboxPref{}
	}
	if m.mailboxPrefs[accountID] == nil {
		m.mailboxPrefs[accountID] = map[string]db.MailboxPref{}
	}
	p := m.mailboxPrefs[accountID][name]
	f(&p)
	m.mailboxPrefs[accountID][name] = p
}

// toggleHideSelectedFolder hides, or when hidden ones are shown unhides, the
// folder under the cursor. The server is untouched.
func (m Model) toggleHideSelectedFolder() (tea.Model, tea.Cmd) {
	_, mb, ok := m.sidebarFolderTarget()
	if !ok || mb == nil {
		m.setStatus("select a folder to hide", false)
		return m, m.clearStatusCmd()
	}
	if strings.EqualFold(mb.Name, "INBOX") {
		m.setStatus("the Inbox can't be hidden", true)
		return m, m.clearStatusCmd()
	}
	hide := !m.mailboxPrefs[mb.AccountID][mb.Name].Hidden
	if m.db != nil {
		if err := m.db.SetMailboxHidden(mb.AccountID, mb.Name, hide); err != nil {
			m.setStatus("hide folder failed: "+err.Error(), true)
			return m, m.clearStatusCmd()
		}
	}
	m.setPref(mb.AccountID, mb.Name, func(p *db.MailboxPref) { p.Hidden = hide })
	prev := m.sidebarCursor
	m.rebuildSidebar()
	if hide && !m.showHiddenFolders {
		m.sidebarCursor = clamp(prev, 0, max(0, len(m.sidebarRows)-1))
		m.clearMessages()
		m.setStatus("folder hidden (only in TideMail). Use \"Show hidden folders\" in the command palette to bring it back", false)
	} else if hide {
		m.cursorToMailbox(mb.ID)
		m.setStatus("folder hidden", false)
	} else {
		m.cursorToMailbox(mb.ID)
		m.setStatus("folder shown", false)
	}
	return m, m.clearStatusCmd()
}

func (m Model) toggleShowHiddenFolders() (tea.Model, tea.Cmd) {
	m.showHiddenFolders = !m.showHiddenFolders
	m.rebuildSidebar()
	if m.showHiddenFolders {
		m.setStatus("showing hidden folders", false)
	} else {
		m.setStatus("hidden folders are hidden again", false)
	}
	return m, m.clearStatusCmd()
}

// folderSiblings returns the sidebar row indexes of the folders that share the
// cursor row's parent, in display order.
func (m Model) folderSiblings(i int) []int {
	depth := m.sidebarRows[i].depth
	start := i
	for start > 0 {
		prev := m.sidebarRows[start-1]
		if prev.kind != rowKindMailbox || prev.depth < depth {
			break
		}
		start--
	}
	end := i
	for end+1 < len(m.sidebarRows) {
		next := m.sidebarRows[end+1]
		if next.kind != rowKindMailbox || next.depth < depth {
			break
		}
		end++
	}
	var out []int
	for j := start; j <= end; j++ {
		if m.sidebarRows[j].depth == depth {
			out = append(out, j)
		}
	}
	return out
}

// moveSelectedFolder moves the folder under the cursor up (-1) or down (+1)
// among its siblings. The order is local to TideMail.
func (m Model) moveSelectedFolder(delta int) (tea.Model, tea.Cmd) {
	_, mb, ok := m.sidebarFolderTarget()
	if !ok || mb == nil || strings.EqualFold(mb.Name, "INBOX") {
		return m, nil
	}
	sibs := m.folderSiblings(m.sidebarCursor)
	pos := slices.Index(sibs, m.sidebarCursor)
	target := pos + delta
	if pos < 0 || target < 0 || target >= len(sibs) {
		m.setStatus("already at the "+map[bool]string{true: "top", false: "bottom"}[delta < 0], false)
		return m, m.clearStatusCmd()
	}
	order := make([]db.Mailbox, 0, len(sibs))
	for _, idx := range sibs {
		if f := m.mailboxByID(m.sidebarRows[idx].mailboxID); f != nil {
			order = append(order, *f)
		}
	}
	order[pos], order[target] = order[target], order[pos]
	// Re-number every sibling (steps of 10) so the swap sticks and leaves room.
	orders := make(map[string]int, len(order))
	for n, f := range order {
		orders[f.Name] = (n + 1) * 10
	}
	if m.db != nil {
		if err := m.db.SetMailboxOrders(mb.AccountID, orders); err != nil {
			m.setStatus("reorder failed: "+err.Error(), true)
			return m, m.clearStatusCmd()
		}
	}
	for name, o := range orders {
		m.setPref(mb.AccountID, name, func(p *db.MailboxPref) { p.Order = o })
	}
	m.rebuildSidebar()
	m.cursorToMailbox(mb.ID)
	return m, nil
}

func (m Model) showHiddenFoldersLabel() string {
	if m.showHiddenFolders {
		return "Hide hidden folders again"
	}
	return "Show hidden folders"
}

// buildFolderMoveEntries lists the folders under currentPath as places to move
// a folder into. Unlike a message move, any level can be chosen, including the
// top level, and a folder with no subfolders is still a valid parent. The moved
// folder's own subtree (exclude) is not offered.
func buildFolderMoveEntries(mailboxes []db.Mailbox, accountID int64, currentPath string, exclude map[int64]bool) []moveEntry {
	delimiter := moveDelimiter(mailboxes, accountID, currentPath)
	entries := []moveEntry{{label: "move here", isConfirm: true}}
	if currentPath != "" {
		entries = append(entries, moveEntry{label: "..", path: moveParentPath(currentPath, delimiter), isDir: true})
	}
	currentParts := splitMovePath(currentPath, delimiter)
	seen := map[string]moveEntry{}
	for _, mb := range mailboxes {
		if mb.AccountID != accountID || mb.Name == "" || exclude[mb.ID] || strings.EqualFold(mb.Name, "INBOX") {
			continue
		}
		parts := splitMovePath(mb.Name, moveMailboxDelimiter(mb))
		if !sameMovePrefix(parts, currentParts) || len(parts) <= len(currentParts) {
			continue
		}
		child := parts[len(currentParts)]
		path := strings.Join(parts[:len(currentParts)+1], moveMailboxDelimiter(mb))
		seen[strings.ToLower(child)] = moveEntry{label: child, path: path, isDir: true}
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		entries = append(entries, seen[k])
	}
	return entries
}

func (m Model) startMoveFolder() (tea.Model, tea.Cmd) {
	_, mb, ok := m.sidebarFolderTarget()
	if !ok || mb == nil {
		m.setStatus("select a folder to move", false)
		return m, m.clearStatusCmd()
	}
	if reason := protectedFolderReason(*mb); reason != "" {
		m.setStatus(reason, true)
		return m, m.clearStatusCmd()
	}
	exclude := map[int64]bool{}
	for _, f := range m.folderSubtree(*mb) {
		exclude[f.ID] = true
	}
	m.movePicker = movePicker{
		accountID:   mb.AccountID,
		accountName: m.accountName(mb.AccountID),
		sources:     exclude,
		moveFolder:  mb.ID,
	}
	m.refreshMovePickerEntries()
	m.overlay = overlayMoveMessage
	return m, nil
}

// confirmFolderMove re-parents the folder being moved under parent ("" = top
// level), which on IMAP is a rename to the new path.
func (m Model) confirmFolderMove(parent string) (tea.Model, tea.Cmd) {
	mb := m.mailboxByID(m.movePicker.moveFolder)
	m.movePicker = movePicker{}
	m.overlay = overlayNone
	if mb == nil {
		return m, nil
	}
	leaf := folderLeaf(*mb)
	delimiter := folderDelimiter(*mb)
	newName := leaf
	if parent == "" {
		newName, _ = qualifyFolderName(leaf, accountMailboxes(m.mailboxes, mb.AccountID))
	} else {
		newName = parent + delimiter + leaf
	}
	if newName == mb.Name {
		m.setStatus("folder is already there", false)
		return m, m.clearStatusCmd()
	}
	if msg := validateFolderName(leaf, delimiter, accountMailboxes(m.mailboxes, mb.AccountID), strings.TrimSuffix(newName, delimiter+leaf), mb.ID); msg != "" && parent != "" {
		m.setStatus("can't move there: "+msg, true)
		return m, m.clearStatusCmd()
	}
	for _, other := range m.mailboxes {
		if other.AccountID == mb.AccountID && other.ID != mb.ID && strings.EqualFold(other.Name, newName) {
			m.setStatus(fmt.Sprintf("can't move there: a folder named %q already exists", leaf), true)
			return m, m.clearStatusCmd()
		}
	}
	m.setStatus("moving folder...", false)
	return m, m.renameFolderCmd(*mb, newName)
}
