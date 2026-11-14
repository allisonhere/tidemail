package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Exporting the in-app log (Settings → Advanced → View Logs): c copies it to
// the clipboard, s saves it to a file. Both use the whole buffer regardless of
// scroll position, in chronological (oldest-first) order — the opposite of the
// newest-first order the pane displays in, which favors catching something
// right after it happens over reading a saved file top to bottom.

// logBufferPlainText renders the log buffer as plain text for the clipboard or
// a file: one line per entry, oldest first, with a leading "!" on errors so
// they are easy to grep for.
func logBufferPlainText(entries []logEntry) string {
	if len(entries) == 0 {
		return "(no log entries)\n"
	}
	var b strings.Builder
	for _, e := range entries {
		mark := " "
		if e.IsError {
			mark = "!"
		}
		fmt.Fprintf(&b, "%s %s %s\n", e.Time.Format("2006-01-02 15:04:05"), mark, e.Message)
	}
	return b.String()
}

type logsSavedMsg struct {
	Path string
	Err  error
}

// logFilename is "tidemail-logs-<timestamp>.txt".
func logFilename(now time.Time) string {
	return "tidemail-logs-" + now.Format("20060102-150405") + ".txt"
}

// saveLogsCmdTo writes text into dir, which the save picker already
// confirmed exists (or the ~/Downloads/tidemail-logs fallback below creates).
func saveLogsCmdTo(text, dir string) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return logsSavedMsg{Err: err}
		}
		path := filepath.Join(dir, logFilename(time.Now()))
		// O_EXCL: never overwrite a file or follow a planted symlink.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return logsSavedMsg{Err: err}
		}
		_, werr := f.WriteString(text)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return logsSavedMsg{Path: path, Err: werr}
	}
}

// saveLogsCmd is the fallback used when the folder picker can't be opened
// (home directory unresolvable, or its starting folder unreadable): a fixed
// destination so a save still succeeds without asking the user anything.
func saveLogsCmd(text string) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return logsSavedMsg{Err: err}
		}
		return saveLogsCmdTo(text, filepath.Join(home, "Downloads", "tidemail-logs"))()
	}
}

func (m Model) handleLogsSaved(msg logsSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus("couldn't save logs: "+msg.Err.Error(), true)
	} else {
		m.setStatus("saved logs to "+msg.Path, false)
	}
	return m, m.clearStatusCmd()
}
