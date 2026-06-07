package ui

import (
	"encoding/json"
	"strings"

	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
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
}

func (m *Model) newFilterManager() filterManager {
	fmgr := filterManager{}
	if rules, err := m.db.ListRules(); err == nil {
		fmgr.rules = rules
	}
	return fmgr
}

func (m *Model) reloadFilterRules() {
	if rules, err := m.db.ListRules(); err == nil {
		m.filterManager.rules = rules
	}
	m.filterManager.cursor = clamp(m.filterManager.cursor, 0, max(0, len(m.filterManager.rules)-1))
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
			_ = m.db.SetRuleEnabled(r.ID, !r.Enabled)
			m.reloadFilterRules()
		}
		return m, nil
	case keyMatches(key, m.keys.Delete):
		if r := m.selectedRule(); r != nil {
			_ = m.db.DeleteRule(r.ID)
			m.reloadFilterRules()
		}
		return m, nil
	case key.String() == "K":
		return m.reorderRule(-1), nil
	case key.String() == "J":
		return m.reorderRule(1), nil
	case key.String() == "t":
		if mb := m.selectedMailbox(); mb != nil {
			m.filterManager.status = "testing…"
			return m, m.applyRulesCmd([]int64{mb.ID}, true)
		}
		m.filterManager.status = "select a mailbox first"
		return m, nil
	case key.String() == "r":
		if mb := m.selectedMailbox(); mb != nil {
			m.filterManager.status = "running…"
			return m, m.applyRulesCmd([]int64{mb.ID}, false)
		}
		m.filterManager.status = "select a mailbox first"
		return m, nil
	case key.String() == "R":
		m.filterManager.status = "running on all mail…"
		return m, m.applyRulesCmd(m.allMailboxIDs(), false)
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
	switch {
	case keyMatches(key, m.keys.Cancel):
		m.filterManager.mode = fmList
		m.filterManager.status = "discarded"
		return m, nil
	case key.String() == "e":
		m.filterManager.input.Focus()
		m.filterManager.mode = fmInput
		return m, nil
	case keyMatches(key, m.keys.Confirm), key.String() == "s":
		if err := m.saveDraftRule(); err != nil {
			return m, nil // stay in review; saveDraftRule set the failure status
		}
		m.filterManager.mode = fmList
		m.filterManager.status = "saved"
		return m, nil
	case key.String() == "r":
		if err := m.saveDraftRule(); err != nil {
			return m, nil
		}
		m.filterManager.mode = fmList
		if mb := m.selectedMailbox(); mb != nil {
			m.filterManager.status = "saved, running…"
			return m, m.applyRulesCmd([]int64{mb.ID}, false)
		}
		m.filterManager.status = "saved"
		return m, nil
	case key.String() == "R":
		if err := m.saveDraftRule(); err != nil {
			return m, nil
		}
		m.filterManager.mode = fmList
		m.filterManager.status = "saved, running on all mail…"
		return m, m.applyRulesCmd(m.allMailboxIDs(), false)
	}
	return m, nil
}

// saveDraftRule persists the rule under review. On failure it sets an error
// status and returns the error so callers do not falsely report "saved" or run a
// rule that was never stored.
func (m *Model) saveDraftRule() error {
	blob, err := json.Marshal(m.filterManager.draft)
	if err != nil {
		m.filterManager.status = "save failed: " + err.Error()
		return err
	}
	priority := len(m.filterManager.rules)
	if _, err := m.db.UpsertRule(db.RuleRecord{
		AccountID: m.filterManager.draftAcct,
		Priority:  priority,
		Enabled:   true,
		Name:      m.filterManager.draftEn,
		JSON:      string(blob),
	}); err != nil {
		m.filterManager.status = "save failed: " + err.Error()
		return err
	}
	m.reloadFilterRules()
	return nil
}

func (m Model) reorderRule(delta int) Model {
	i := m.filterManager.cursor
	j := i + delta
	if i < 0 || i >= len(m.filterManager.rules) || j < 0 || j >= len(m.filterManager.rules) {
		return m
	}
	a := m.filterManager.rules[i]
	b := m.filterManager.rules[j]
	_ = m.db.SetRulePriority(a.ID, b.Priority)
	_ = m.db.SetRulePriority(b.ID, a.Priority)
	m.reloadFilterRules()
	m.filterManager.cursor = j
	return m
}

func (m Model) selectedRule() *db.RuleRecord {
	if m.filterManager.cursor < 0 || m.filterManager.cursor >= len(m.filterManager.rules) {
		return nil
	}
	return &m.filterManager.rules[m.filterManager.cursor]
}

func (m Model) allMailboxIDs() []int64 {
	ids := make([]int64, 0, len(m.mailboxes))
	for _, mb := range m.mailboxes {
		ids = append(ids, mb.ID)
	}
	return ids
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
	header := renderManagerHeader("FILTERS", width, chrome)

	// Build the footer first so the body can fill the remaining height exactly.
	// Two-line action groups keep every shortcut visible (a single line would be
	// truncated) and give the modal a stable height.
	var actions string
	switch m.filterManager.mode {
	case fmPickAccount:
		actions = renderManagerActionGroups(width, chrome,
			[]string{"enter", "choose", "esc", "back"}, nil)
	case fmInput:
		actions = renderManagerActionGroups(width, chrome,
			[]string{"enter", "generate", "esc", "back"}, nil)
	case fmReview:
		actions = renderManagerActionGroups(width, chrome,
			[]string{"s/enter", "save", "r", "save+run", "esc", "discard"},
			[]string{"R", "save+run all", "e", "edit text"})
	default:
		actions = renderManagerActionGroups(width, chrome,
			[]string{"n", "new", "space", "on/off", "t", "test", "esc", "close"},
			[]string{"r", "run", "R", "run all", "J/K", "reorder", "d", "delete"})
	}

	// Reserve exactly one status line (blank when empty) so the modal never
	// changes height as status text appears/clears.
	statusText := m.filterManager.status
	status := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 2).
		Render(clampView(statusText, max(1, width-4), 1, chrome.baseBg))

	// Blank spacer between the header and the body for breathing room.
	headerGap := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")

	bodyH := max(1, height-lipgloss.Height(header)-lipgloss.Height(headerGap)-lipgloss.Height(actions)-1)

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
		bodyLines = wrapBodyBlock("Generated rule:\n\n"+m.filterManager.draft.Summary()+"\n\nFrom: "+m.filterManager.draftEn, width, chrome)
	default:
		bodyLines = m.filterListRows(width, chrome)
	}
	body := padBlock(bodyLines, bodyH, width, chrome.baseBg)

	return lipgloss.JoinVertical(lipgloss.Left, header, headerGap, body, status, actions)
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
	rows := make([]string, 0, len(m.filterManager.rules))
	for i, rec := range m.filterManager.rules {
		mark := "○ "
		if rec.Enabled {
			mark = "● "
		}
		scope := "[all]"
		if rec.AccountID != 0 {
			scope = "[" + m.accountName(rec.AccountID) + "]"
		}
		style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(0, 2)
		if i == m.filterManager.cursor {
			style = style.Background(chrome.accent).Foreground(contrastFg(chrome.accent))
		}
		// Let lipgloss fill the full width with the row's own background (accent
		// when selected). Pre-padding with clampView would bake in the base
		// background, leaving the highlight not spanning the row.
		rows = append(rows, style.Render(truncate(mark+scope+" "+ruleLabel(rec), max(1, width-4))))
	}
	return rows
}

func (m Model) filterAccountRows(width int, chrome managerChrome) []string {
	labels := make([]string, 0, m.acctOptionCount())
	labels = append(labels, "All accounts")
	for _, a := range m.accounts {
		labels = append(labels, a.Name)
	}
	rows := make([]string, 0, len(labels))
	for i, label := range labels {
		style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(0, 2)
		if i == m.filterManager.acctCursor {
			style = style.Background(chrome.accent).Foreground(contrastFg(chrome.accent))
		}
		rows = append(rows, style.Render(truncate("Apply to: "+label, max(1, width-4))))
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
