package ui

import (
	"encoding/json"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/filter"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filterMode int

const (
	fmList        filterMode = iota
	fmPickAccount            // choosing which account (or All) the new rule applies to
	fmInput                  // typing an English description
	fmReview                 // reviewing an AI-generated rule before saving
)

type filterSaveIntent int

const (
	filterSaveOnly filterSaveIntent = iota
	filterSaveRunScope
	filterSaveRunAll
)

type filterManager struct {
	mode       filterMode
	rules      []db.RuleRecord
	cursor     int
	input      textinput.Model
	draft      filter.Rule // generated rule under review
	draftEn    string      // the English that produced the draft
	draftAcct  int64       // account the draft was generated/validated against (0 = all)
	acctCursor int         // cursor within the account picker (0 = All accounts)
	status     string
	saving     bool // waiting for move-target folders to be prepared
}

func (m *Model) newFilterManager() filterManager {
	fmgr := filterManager{}
	if rules, err := m.db.ListRules(); err == nil {
		fmgr.rules = rules
	}
	return fmgr
}

func (m *Model) reloadFilterRules() error {
	rules, err := m.db.ListRules()
	if err != nil {
		return err
	}
	m.filterManager.rules = rules
	m.filterManager.cursor = clamp(m.filterManager.cursor, 0, max(0, len(m.filterManager.rules)-1))
	return nil
}

func (m Model) handleFilterManager(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch m.filterManager.mode {
	case fmPickAccount:
		return m.handleFilterPickAccount(key)
	case fmInput:
		return m.handleFilterInput(key)
	case fmReview:
		return m.handleFilterReview(key)
	default:
		return m.handleFilterList(key)
	}
}

func (m Model) handleFilterList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(key, m.keys.Cancel):
		m.overlay = overlayNone
		return m, nil
	case keyMatches(key, m.keys.Up):
		if m.filterManager.cursor > 0 {
			m.filterManager.cursor--
		}
		return m, nil
	case keyMatches(key, m.keys.Down):
		if m.filterManager.cursor < len(m.filterManager.rules)-1 {
			m.filterManager.cursor++
		}
		return m, nil
	case key.String() == "n":
		// First choose which account (or All) the rule applies to; the chosen
		// account's folders validate any move target.
		m.filterManager.mode = fmPickAccount
		m.filterManager.acctCursor = m.defaultAcctCursor()
		m.filterManager.status = ""
		return m, nil
	case keyMatches(key, m.keys.Space):
		if r := m.selectedRule(); r != nil {
			if err := m.db.SetRuleEnabled(r.ID, !r.Enabled); err != nil {
				m.filterManager.status = "update failed: " + err.Error()
				return m, nil
			}
			if err := m.reloadFilterRules(); err != nil {
				m.filterManager.status = "reload failed: " + err.Error()
				return m, nil
			}
			m.filterManager.status = ""
		}
		return m, nil
	case keyMatches(key, m.keys.Delete):
		if r := m.selectedRule(); r != nil {
			if err := m.db.DeleteRule(r.ID); err != nil {
				m.filterManager.status = "delete failed: " + err.Error()
				return m, nil
			}
			if err := m.reloadFilterRules(); err != nil {
				m.filterManager.status = "reload failed: " + err.Error()
				return m, nil
			}
			m.filterManager.status = ""
		}
		return m, nil
	case key.String() == "K":
		return m.reorderRule(-1), nil
	case key.String() == "J":
		return m.reorderRule(1), nil
	case key.String() == "t":
		return m.runSelectedRule(true)
	case key.String() == "r":
		return m.runSelectedRule(false)
	case key.String() == "a":
		m.filterManager.status = "running all rules on all mail…"
		return m, m.applyRulesCmd(m.allMailboxIDs(), false, 0)
	}
	return m, nil
}

// acctOptionCount is the number of rows in the account picker: "All" + accounts.
func (m Model) acctOptionCount() int { return len(m.accounts) + 1 }

// defaultAcctCursor highlights the inferred account (selected mailbox's, else
// first) when the picker opens; index 0 is "All accounts".
func (m Model) defaultAcctCursor() int {
	want := m.filterScopeAccountID()
	for i, a := range m.accounts {
		if a.ID == want {
			return i + 1
		}
	}
	return 0
}

// acctCursorToID maps a picker cursor to an account ID (0 = all accounts).
func (m Model) acctCursorToID(cursor int) int64 {
	if cursor <= 0 || cursor > len(m.accounts) {
		return 0
	}
	return m.accounts[cursor-1].ID
}

func (m Model) handleFilterPickAccount(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(key, m.keys.Cancel):
		m.filterManager.mode = fmList
		return m, nil
	case keyMatches(key, m.keys.Up):
		if m.filterManager.acctCursor > 0 {
			m.filterManager.acctCursor--
		}
		return m, nil
	case keyMatches(key, m.keys.Down):
		if m.filterManager.acctCursor < m.acctOptionCount()-1 {
			m.filterManager.acctCursor++
		}
		return m, nil
	case keyMatches(key, m.keys.Confirm):
		m.filterManager.draftAcct = m.acctCursorToID(m.filterManager.acctCursor)
		in := textinput.New()
		in.Placeholder = "e.g. move newsletters from substack to Reading"
		in.Focus()
		// No fixed Width: it would pad the field with unstyled spaces that show
		// the terminal background through. lipgloss fills the row instead.
		m.filterManager.input = in
		m.filterManager.mode = fmInput
		return m, nil
	}
	return m, nil
}

func (m Model) handleFilterInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(key, m.keys.Cancel):
		m.filterManager.mode = fmList
		m.filterManager.input.Blur()
		return m, nil
	case keyMatches(key, m.keys.Confirm):
		text := m.filterManager.input.Value()
		if text == "" {
			return m, nil
		}
		m.filterManager.draftEn = text
		// draftAcct was chosen in the pick-account step; its folders validate the
		// move target and scope the saved rule (0 = all accounts).
		m.filterManager.status = "generating…"
		m.filterManager.input.Blur()
		return m, m.generateFilterCmd(text, m.filterManager.draftAcct)
	default:
		var cmd tea.Cmd
		m.filterManager.input, cmd = m.filterManager.input.Update(key)
		return m, cmd
	}
}

func (m Model) handleFilterReview(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filterManager.saving {
		return m, nil
	}
	switch {
	case keyMatches(key, m.keys.Cancel):
		m.filterManager.mode = fmList
		m.filterManager.status = "discarded"
		return m, nil
	case key.String() == "e":
		m.filterManager.input.Focus()
		m.filterManager.mode = fmInput
		return m, nil
	// ctrl+s saves here as it does in settings and the account manager; enter
	// stays as the plain confirm.
	case keyMatches(key, m.keys.Confirm), keyMatches(key, m.keys.Save):
		return m.beginSaveDraftRule(filterSaveOnly)
	case key.String() == "r":
		return m.beginSaveDraftRule(filterSaveRunScope)
	case key.String() == "a":
		return m.beginSaveDraftRule(filterSaveRunAll)
	}
	return m, nil
}

// beginSaveDraftRule prepares a move rule's destination folders before the
// rule is persisted. Other action types retain the existing synchronous save.
func (m Model) beginSaveDraftRule(intent filterSaveIntent) (tea.Model, tea.Cmd) {
	if m.filterManager.draft.Action.Type == filter.ActionMove {
		m.filterManager.saving = true
		m.filterManager.status = "creating destination folder…"
		return m, m.prepareFilterFoldersCmd(intent)
	}
	return m.finishSaveDraftRule(intent)
}

func (m Model) finishSaveDraftRule(intent filterSaveIntent) (tea.Model, tea.Cmd) {
	id, err := m.saveDraftRule()
	if err != nil {
		return m, nil
	}
	m.filterManager.mode = fmList
	switch intent {
	case filterSaveRunScope:
		// Run the rule we just saved over the scope it was saved with, not
		// whatever the sidebar is pointing at.
		if saved := m.ruleByID(id); saved != nil {
			if ids := m.filterRunMailboxIDs(saved); len(ids) > 0 {
				m.filterManager.status = "saved, running…"
				return m, m.applyRulesCmd(ids, false, id)
			}
		}
		m.filterManager.status = "saved"
	case filterSaveRunAll:
		m.filterManager.status = "saved, running on all mail…"
		return m, m.applyRulesCmd(m.allMailboxIDs(), false, 0)
	default:
		m.filterManager.status = "saved"
	}
	return m, nil
}

// saveDraftRule persists the rule under review and returns its id. On failure it
// sets an error status and returns the error so callers do not falsely report
// "saved" or run a rule that was never stored.
func (m *Model) saveDraftRule() (int64, error) {
	blob, err := json.Marshal(m.filterManager.draft)
	if err != nil {
		m.filterManager.status = "save failed: " + err.Error()
		return 0, err
	}
	priority := len(m.filterManager.rules)
	id, err := m.db.UpsertRule(db.RuleRecord{
		AccountID: m.filterManager.draftAcct,
		Priority:  priority,
		Enabled:   true,
		Name:      m.filterManager.draftEn,
		JSON:      string(blob),
	})
	if err != nil {
		m.filterManager.status = "save failed: " + err.Error()
		return 0, err
	}
	if err := m.reloadFilterRules(); err != nil {
		m.filterManager.status = "reload failed: " + err.Error()
		return 0, err
	}
	return id, nil
}

func (m Model) reorderRule(delta int) Model {
	i := m.filterManager.cursor
	j := i + delta
	if i < 0 || i >= len(m.filterManager.rules) || j < 0 || j >= len(m.filterManager.rules) {
		return m
	}
	a := m.filterManager.rules[i]
	b := m.filterManager.rules[j]
	if err := m.db.SwapRulePriorities(a.ID, b.Priority, b.ID, a.Priority); err != nil {
		m.filterManager.status = "reorder failed: " + err.Error()
		return m
	}
	if err := m.reloadFilterRules(); err != nil {
		m.filterManager.status = "reload failed: " + err.Error()
		return m
	}
	m.filterManager.cursor = j
	m.filterManager.status = ""
	return m
}

func (m Model) ruleByID(id int64) *db.RuleRecord {
	for i := range m.filterManager.rules {
		if m.filterManager.rules[i].ID == id {
			return &m.filterManager.rules[i]
		}
	}
	return nil
}

func (m Model) selectedRule() *db.RuleRecord {
	if m.filterManager.cursor < 0 || m.filterManager.cursor >= len(m.filterManager.rules) {
		return nil
	}
	return &m.filterManager.rules[m.filterManager.cursor]
}

// runSelectedRule tests or applies the highlighted rule over its own account
// scope. Only that rule runs, so the reported match count answers the question
// the user asked ("what does *this* rule do?") rather than folding in every
// other enabled rule.
func (m Model) runSelectedRule(dryRun bool) (tea.Model, tea.Cmd) {
	rule := m.selectedRule()
	if rule == nil {
		m.filterManager.status = "no rule selected"
		return m, nil
	}
	if !rule.Enabled {
		m.filterManager.status = "rule is disabled — press space to enable"
		return m, nil
	}
	ids := m.filterRunMailboxIDs(rule)
	if len(ids) == 0 {
		m.filterManager.status = "no inbox found for this rule's accounts"
		return m, nil
	}
	if dryRun {
		m.filterManager.status = "testing…"
	} else {
		m.filterManager.status = "running…"
	}
	return m, m.applyRulesCmd(ids, dryRun, rule.ID)
}

// filterRunMailboxIDs returns the mailboxes a rule-triggered run ("t"/"r")
// covers. The scope comes from the rule itself — the account picked when it was
// created, or every account for an "All accounts" rule — not from whatever the
// sidebar happens to be pointing at.
//
// Within each in-scope account we run the inboxes only. That is what a filter
// means: it processes arriving mail. Sweeping every folder would drag messages
// back out of Sent, Archive, and Trash into the rule's target. "a" remains the
// deliberate run-over-all-mail escape hatch.
func (m Model) filterRunMailboxIDs(rule *db.RuleRecord) []int64 {
	if rule == nil {
		return nil
	}
	ids := make([]int64, 0, len(m.accounts))
	for _, mb := range m.mailboxes {
		if rule.AccountID != 0 && mb.AccountID != rule.AccountID {
			continue
		}
		if strings.EqualFold(mb.Name, "INBOX") {
			ids = append(ids, mb.ID)
		}
	}
	return ids
}

func (m Model) allMailboxIDs() []int64 {
	ids := make([]int64, 0, len(m.mailboxes))
	for _, mb := range m.mailboxes {
		ids = append(ids, mb.ID)
	}
	return ids
}

func (m Model) filterFolderCreationNotice() string {
	if m.filterManager.draft.Action.Type != filter.ActionMove {
		return ""
	}
	target := strings.TrimSpace(m.filterManager.draft.Action.Target)
	if target == "" {
		return ""
	}
	missing := false
	for _, account := range m.accounts {
		if m.filterManager.draftAcct != 0 && account.ID != m.filterManager.draftAcct {
			continue
		}
		found := false
		for _, mailbox := range m.mailboxes {
			if mailbox.AccountID == account.ID && (strings.EqualFold(mailbox.Name, target) || strings.EqualFold(mailbox.DisplayName, target)) {
				found = true
				break
			}
		}
		if !found {
			missing = true
			break
		}
	}
	if !missing {
		return ""
	}
	if m.filterManager.draftAcct == 0 {
		return "Saving will create folder " + target + " in all accounts."
	}
	return "Saving will create folder " + target + " in " + m.accountName(m.filterManager.draftAcct) + "."
}

// filterScopeAccountID picks the account whose folders are offered to the AI for
// "move" targets: the selected mailbox's account, else the first account.
func (m Model) filterScopeAccountID() int64 {
	if mb := m.selectedMailbox(); mb != nil {
		return mb.AccountID
	}
	if len(m.accounts) > 0 {
		return m.accounts[0].ID
	}
	return 0
}

func (m Model) renderFilterManager(width, height int, chrome managerChrome) string {
	// Build the footer first so the body can fill the remaining height exactly.
	// Two-line hint groups keep every shortcut visible (a single line would be
	// truncated) and give the modal a stable height.
	var actions string
	switch m.filterManager.mode {
	case fmPickAccount:
		actions = renderSoftHints(width, chrome, "enter", "choose", "esc", "back")
	case fmInput:
		actions = renderSoftHints(width, chrome, "enter", "generate", "esc", "back")
	case fmReview:
		actions = lipgloss.JoinVertical(lipgloss.Left,
			renderSoftHints(width, chrome, "^s/enter", "save", "r", "run", "esc", "discard"),
			renderSoftHints(width, chrome, "a", "run all", "e", "edit text"))
	default:
		actions = lipgloss.JoinVertical(lipgloss.Left,
			renderSoftHints(width, chrome, "n", "new", "space", "on/off", "t", "test", "esc", "close"),
			renderSoftHints(width, chrome, "r", "run", "a", "run all", "J/K", "reorder", "d", "delete"))
	}

	// Reserve exactly one status line (blank when empty) so the modal never
	// changes height as status text appears/clears.
	statusText := m.filterManager.status
	status := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 2).
		Render(clampView(statusText, max(1, width-4), 1, chrome.baseBg))

	// Blank spacer at the top for breathing room (the title lives in the border).
	headerGap := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")

	bodyH := max(1, height-lipgloss.Height(headerGap)-lipgloss.Height(actions)-1)

	var bodyLines []string
	switch m.filterManager.mode {
	case fmPickAccount:
		bodyLines = m.filterAccountRows(width, chrome)
	case fmInput:
		// Paint the input's prompt/text/placeholder/cursor with the modal
		// background; otherwise the textinput emits foreground-only styling and
		// the terminal background shows through behind the hint text.
		in := m.filterManager.input
		bg := lipgloss.NewStyle().Background(chrome.baseBg)
		in.PromptStyle = bg.Foreground(chrome.text)
		in.TextStyle = bg.Foreground(chrome.text)
		in.PlaceholderStyle = bg.Foreground(chrome.muted)
		in.Cursor.Style = bg
		in.Cursor.TextStyle = bg.Foreground(chrome.text)
		bodyLines = wrapBodyBlock("Describe a filter in plain English:\n\n"+in.View(), width, chrome)
	case fmReview:
		review := "Generated rule:\n\n" + m.filterManager.draft.Summary()
		if notice := m.filterFolderCreationNotice(); notice != "" {
			review += "\n\n" + notice
		}
		review += "\n\nFrom: " + m.filterManager.draftEn
		bodyLines = wrapBodyBlock(review, width, chrome)
	default:
		bodyLines = m.filterListRows(width, chrome)
	}
	body := padBlock(bodyLines, bodyH, width, chrome.baseBg)

	return lipgloss.JoinVertical(lipgloss.Left, headerGap, body, status, actions)
}

// wrapBodyBlock renders a text block at the modal width with consistent bg and
// returns its lines (each padded to width).
func wrapBodyBlock(text string, width int, chrome managerChrome) []string {
	block := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(1, 2).
		Render(text)
	return strings.Split(clampView(block, width, lipgloss.Height(block), chrome.baseBg), "\n")
}

// padBlock clamps/pads a slice of lines to exactly h rows, each width wide with
// the modal background, so the body never leaves transparent gaps.
func padBlock(lines []string, h, width int, bg lipgloss.Color) string {
	blank := lipgloss.NewStyle().Background(bg).Width(width).Render("")
	if len(lines) > h {
		lines = lines[:h]
	}
	out := make([]string, 0, h)
	out = append(out, lines...)
	for len(out) < h {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

func (m Model) filterListRows(width int, chrome managerChrome) []string {
	if len(m.filterManager.rules) == 0 {
		return []string{lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 2).
			Render(clampView("No filters yet. Press n to create one from plain English.", max(1, width-4), 1, chrome.baseBg))}
	}
	labelW := max(1, width-2) // minus the 2-cell rail
	rows := make([]string, 0, len(m.filterManager.rules))
	for i, rec := range m.filterManager.rules {
		selected := i == m.filterManager.cursor
		mark := "○ "
		if rec.Enabled {
			mark = "● "
		}
		scope := "[all]"
		if rec.AccountID != 0 {
			scope = "[" + m.accountName(rec.AccountID) + "]"
		}
		fg := chrome.text
		if !rec.Enabled {
			fg = chrome.muted // dim disabled rules; the ○ glyph also signals off
		}
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + truncate(mark+scope+" "+ruleLabel(rec), max(1, labelW-1)))
		rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	return rows
}

func (m Model) filterAccountRows(width int, chrome managerChrome) []string {
	labels := make([]string, 0, m.acctOptionCount())
	labels = append(labels, "All accounts")
	for _, a := range m.accounts {
		labels = append(labels, a.Name)
	}
	labelW := max(1, width-2) // minus the 2-cell rail
	rows := make([]string, 0, len(labels))
	for i, label := range labels {
		selected := i == m.filterManager.acctCursor
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + truncate("Apply to: "+label, max(1, labelW-1)))
		rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	return rows
}

func ruleLabel(rec db.RuleRecord) string {
	var r filter.Rule
	if err := json.Unmarshal([]byte(rec.JSON), &r); err == nil {
		return r.Summary()
	}
	if rec.Name != "" {
		return rec.Name
	}
	return "(invalid rule)"
}
