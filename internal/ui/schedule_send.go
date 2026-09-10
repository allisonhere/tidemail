package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const scheduledSendTimeLayout = "3:04 PM"

type scheduleChoice struct{ title string }

func (c scheduleChoice) Title() string       { return c.title }
func (c scheduleChoice) Description() string { return "" }
func (c scheduleChoice) FilterValue() string { return c.title }

// openScheduleSend keeps the compose intact while a local delivery time is
// chosen. The time is persisted in the outbox, so it survives a restart.
func (m *Model) openScheduleSend() {
	next := time.Now().Add(time.Hour).Round(time.Minute)
	m.scheduleSendInput = newComposeInput(scheduledSendTimeLayout)
	m.scheduleSendInput.SetValue(next.Format(scheduledSendTimeLayout))
	m.scheduleSendInput.CursorEnd()
	m.scheduleSendInput.Blur()
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	m.schedulePicker = list.New([]list.Item{
		scheduleChoice{"In one hour"},
		scheduleChoice{"Tomorrow at 9:00"},
		scheduleChoice{"Next Monday at 9:00"},
		scheduleChoice{"Custom date and time"},
	}, delegate, 48, 4)
	m.schedulePicker.SetShowTitle(false)
	m.schedulePicker.SetShowFilter(false)
	m.schedulePicker.SetShowStatusBar(false)
	m.schedulePicker.SetShowPagination(false)
	m.schedulePicker.SetShowHelp(false)
	m.schedulePicker.SetFilteringEnabled(false)
	m.scheduleCustom = false
	m.scheduleCalendar = time.Time{}
	m.scheduleDate = time.Time{}
	m.overlay = overlayScheduleSend
}

func (m Model) handleScheduleSend(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		m.scheduleSendInput, cmd = m.scheduleSendInput.Update(msg)
		return m, cmd
	}
	switch {
	case keyMatches(km, m.keys.Cancel):
		m.scheduleSendInput.Blur()
		m.overlay = overlayCompose
		return m, nil
	case keyMatches(km, m.keys.Confirm):
		if !m.scheduleCustom {
			choice, _ := m.schedulePicker.SelectedItem().(scheduleChoice)
			if choice.title == "Custom date and time" {
				m.scheduleCustom = true
				m.scheduleCalendar = time.Now().AddDate(0, 0, 1).Truncate(24 * time.Hour)
				m.scheduleDate = time.Time{}
				m.scheduleSendInput.SetValue("9:00 AM")
				return m, nil
			}
			return m.queueScheduledSend(scheduleChoiceTime(choice.title, time.Now()))
		}
		if m.scheduleDate.IsZero() {
			m.scheduleDate = m.scheduleCalendar
			m.scheduleSendInput.Focus()
			return m, nil
		}
		clock, err := time.ParseInLocation(scheduledSendTimeLayout, strings.ToUpper(strings.TrimSpace(m.scheduleSendInput.Value())), time.Local)
		if err != nil {
			m.scheduleSendInput.Err = err
			return m, nil
		}
		at := time.Date(m.scheduleDate.Year(), m.scheduleDate.Month(), m.scheduleDate.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local)
		if !at.After(time.Now()) {
			m.scheduleSendInput.Err = fmt.Errorf("choose a future time")
			return m, nil
		}
		return m.queueScheduledSend(at)
	default:
		if !m.scheduleCustom {
			var cmd tea.Cmd
			m.schedulePicker, cmd = m.schedulePicker.Update(msg)
			return m, cmd
		}
		if m.scheduleDate.IsZero() {
			switch km.String() {
			case "left", "h":
				m.scheduleCalendar = m.scheduleCalendar.AddDate(0, 0, -1)
			case "right", "l":
				m.scheduleCalendar = m.scheduleCalendar.AddDate(0, 0, 1)
			case "up", "k":
				m.scheduleCalendar = m.scheduleCalendar.AddDate(0, 0, -7)
			case "down", "j":
				m.scheduleCalendar = m.scheduleCalendar.AddDate(0, 0, 7)
			case "pgup":
				m.scheduleCalendar = m.scheduleCalendar.AddDate(0, -1, 0)
			case "pgdown":
				m.scheduleCalendar = m.scheduleCalendar.AddDate(0, 1, 0)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.scheduleSendInput, cmd = m.scheduleSendInput.Update(msg)
		m.scheduleSendInput.Err = nil
		return m, cmd
	}
}

func renderScheduleCalendar(selected time.Time, width int, chrome managerChrome) string {
	monthStart := time.Date(selected.Year(), selected.Month(), 1, 0, 0, 0, 0, selected.Location())
	gridStart := monthStart.AddDate(0, 0, -int(monthStart.Weekday()))
	header := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true).Render(selected.Format("January 2006"))
	weekdays := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render(" Su  Mo  Tu  We  Th  Fr  Sa")
	rows := []string{header, weekdays}
	for week := 0; week < 6; week++ {
		var cells strings.Builder
		for day := 0; day < 7; day++ {
			date := gridStart.AddDate(0, 0, week*7+day)
			label := fmt.Sprintf("%2d", date.Day())
			style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
			if date.Month() != selected.Month() {
				style = style.Foreground(chrome.muted)
			}
			if sameDay(date, selected) {
				style = lipgloss.NewStyle().Background(chrome.highlight).Foreground(chrome.highlightFg).Bold(true)
			}
			cells.WriteString(style.Render(label))
			if day < 6 {
				cells.WriteString(lipgloss.NewStyle().Background(chrome.baseBg).Render("  "))
			}
		}
		rows = append(rows, cells.String())
	}
	return lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Padding(1, 2).Render(strings.Join(rows, "\n"))
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func (m Model) queueScheduledSend(at time.Time) (tea.Model, tea.Cmd) {
	m.scheduleSendInput.Blur()
	m.overlay = overlayCompose
	// Run the normal compose validation and queueing path, then replace its
	// default undo-delay delivery timestamp with the chosen one.
	c, cmd, _ := m.compose.send()
	m.compose = c
	if cmd == nil {
		return m, cmd
	}
	queued := cmd().(SendQueuedMsg)
	queued.ScheduledAt = at
	return m, func() tea.Msg { return queued }
}

func scheduleChoiceTime(choice string, now time.Time) time.Time {
	switch choice {
	case "Tomorrow at 9:00":
		return time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
	case "Next Monday at 9:00":
		days := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		return time.Date(now.Year(), now.Month(), now.Day()+days, 9, 0, 0, 0, now.Location())
	default:
		return now.Add(time.Hour).Round(time.Minute)
	}
}
