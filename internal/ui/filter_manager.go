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
	fmList   filterMode = iota
	fmInput             // typing an English description
	fmReview            // reviewing an AI-generated rule before saving
)

type filterManager struct {
	mode      filterMode
	rules     []db.RuleRecord
	cursor    int
	input     textinput.Model
	draft     filter.Rule // generated rule under review
	draftEn   string      // the English that produced the draft
	draftAcct int64       // account the draft was generated/validated against (0 = all)
	status    string
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
		in := textinput.New()
		in.Placeholder = "e.g. move newsletters from substack to Reading"
		in.Focus()
		in.Width = 50
		m.filterManager.input = in
		m.filterManager.mode = fmInput
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
		// Capture the account whose folders validate this rule's move target so the
		// saved rule is scoped to it — a global rule could otherwise create/move
		// into a same-named folder on another account it was never validated for.
		m.filterManager.draftAcct = m.filterScopeAccountID()
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
		m.saveDraftRule()
		m.filterManager.mode = fmList
		m.filterManager.status = "saved"
		return m, nil
	case key.String() == "r":
		m.saveDraftRule()
		m.filterManager.mode = fmList
		if mb := m.selectedMailbox(); mb != nil {
			m.filterManager.status = "saved, running…"
			return m, m.applyRulesCmd([]int64{mb.ID}, false)
		}
		m.filterManager.status = "saved"
		return m, nil
	case key.String() == "R":
		m.saveDraftRule()
		m.filterManager.mode = fmList
		m.filterManager.status = "saved, running on all mail…"
		return m, m.applyRulesCmd(m.allMailboxIDs(), false)
	}
	return m, nil
}

func (m *Model) saveDraftRule() {
	blob, err := json.Marshal(m.filterManager.draft)
	if err != nil {
		m.filterManager.status = "save failed: " + err.Error()
		return
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
		return
	}
	m.reloadFilterRules()
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

	bodyH := max(1, height-lipgloss.Height(header)-lipgloss.Height(actions)-1)

	var bodyLines []string
	switch m.filterManager.mode {
	case fmInput:
		bodyLines = wrapBodyBlock("Describe a filter in plain English:\n\n"+m.filterManager.input.View(), width, chrome)
	case fmReview:
		bodyLines = wrapBodyBlock("Generated rule:\n\n"+m.filterManager.draft.Summary()+"\n\nFrom: "+m.filterManager.draftEn, width, chrome)
	default:
		bodyLines = m.filterListRows(width, chrome)
	}
	body := padBlock(bodyLines, bodyH, width, chrome.baseBg)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, status, actions)
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
		rows = append(rows, style.Render(clampView(mark+scope+" "+ruleLabel(rec), max(1, width-4), 1, chrome.baseBg)))
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
