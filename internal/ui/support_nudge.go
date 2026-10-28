package ui

import (
	"strconv"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	supportFirstSeenKey = "support.first_seen"
	supportNudgedKey    = "support.nudged"

	// supportNudgeAfterUse is how long TideMail must have been in use before the
	// one-time support note can appear, and supportNudgeDelay keeps it out of the
	// way of the first minutes of any session.
	supportNudgeAfterUse = 7 * 24 * time.Hour
	supportNudgeDelay    = 2 * time.Minute
)

// SupportNudgeMsg asks the model to show the one-time support note.
type SupportNudgeMsg struct{}

// supportNudgeCmd schedules the note once TideMail has been in use for a week.
// It records the first launch, never schedules again after the note has been
// shown, and does nothing when the user turned the reminder off.
func supportNudgeCmd(database *db.DB, enabled bool, now time.Time) tea.Cmd {
	if database == nil || !enabled {
		return nil
	}
	if done, _ := database.GetSetting(supportNudgedKey); done == "1" {
		return nil
	}
	raw, err := database.GetSetting(supportFirstSeenKey)
	if err != nil {
		return nil
	}
	first, perr := strconv.ParseInt(raw, 10, 64)
	if raw == "" || perr != nil {
		_ = database.SetSetting(supportFirstSeenKey, strconv.FormatInt(now.Unix(), 10))
		return nil
	}
	if now.Sub(time.Unix(first, 0)) < supportNudgeAfterUse {
		return nil
	}
	return tea.Tick(supportNudgeDelay, func(time.Time) tea.Msg { return SupportNudgeMsg{} })
}

// handleSupportNudge shows the note once. If a dialog is open it tries again
// later instead of covering it, and the note is recorded as shown only when it
// was actually displayed.
func (m Model) handleSupportNudge() (tea.Model, tea.Cmd) {
	if !m.cfg.Display.SupportReminder {
		return m, nil
	}
	if m.overlay != overlayNone {
		return m, tea.Tick(supportNudgeDelay, func(time.Time) tea.Msg { return SupportNudgeMsg{} })
	}
	m.setStatus("Enjoying TideMail? You can support it in Settings ("+m.keyHint(m.keys.Settings)+") → Support. This note won't show again.", false)
	if m.db != nil {
		_ = m.db.SetSetting(supportNudgedKey, "1")
	}
	return m, m.clearStatusCmd()
}
