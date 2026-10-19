package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Exporting a plugin result (c copy, s save, p print in the result overlay).
// This is host-owned: a plugin supplies the report's content and nothing else.
// It never picks a path or runs a printer, and everything written is the same
// sanitized text the overlay shows.

const reportExportWidth = 80

// exportable reports whether the overlay is showing a report or result
// with content to export (not an error card or the details view).
func (r *pluginResult) exportable() bool {
	return r != nil && r.card == nil && !r.showDetails && (r.view != nil || strings.TrimSpace(r.body) != "")
}

// pluginResultPlainText is the result as plain text for the clipboard, a file
// or a printer: a title block, then the body or the rendered view.
func (m Model) pluginResultPlainText(r *pluginResult) string {
	var b strings.Builder
	b.WriteString(sanitizePluginLine(r.pluginName) + "\n")
	if r.heading != "" {
		b.WriteString(sanitizePluginLine(r.heading) + "\n")
	}
	b.WriteString(time.Now().Format("2006-01-02 15:04") + "\n\n")
	if r.view != nil {
		chrome := newManagerChrome(reportExportWidth, m.styles.Theme, true)
		for _, line := range m.pluginViewLines(r.view, reportExportWidth, chrome) {
			b.WriteString(strings.TrimRight(ansi.Strip(line), " ") + "\n")
		}
	} else {
		b.WriteString(sanitizePluginText(r.body))
		b.WriteString("\n")
	}
	return b.String()
}

type reportSavedMsg struct {
	Path string
	Err  error
}

type reportPrintedMsg struct {
	Tool string
	Err  error
}

var unsafeNameRe = regexp.MustCompile(`[^a-z0-9._-]+`)

// reportFilename is "<plugin-id>-<timestamp>.txt" with the id reduced to safe
// characters, so a hostile id cannot name a path.
func reportFilename(pluginID string, now time.Time) string {
	id := strings.Trim(unsafeNameRe.ReplaceAllString(strings.ToLower(pluginID), "-"), ".-")
	if id == "" {
		id = "report"
	}
	return id + "-" + now.Format("20060102-150405") + ".txt"
}

func saveReportCmd(pluginID, text string) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return reportSavedMsg{Err: err}
		}
		dir := filepath.Join(home, "Downloads", "tidemail-reports")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return reportSavedMsg{Err: err}
		}
		path := filepath.Join(dir, reportFilename(pluginID, time.Now()))
		// O_EXCL: never overwrite a file or follow a planted symlink.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return reportSavedMsg{Err: err}
		}
		_, werr := f.WriteString(text)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return reportSavedMsg{Path: path, Err: werr}
	}
}

// printReport sends text to the default printer with lp (or lpr). It is a
// variable so tests don't need a printer.
var printReport = func(text string) (string, error) {
	tool := ""
	for _, name := range []string{"lp", "lpr"} {
		if p, err := exec.LookPath(name); err == nil {
			tool = p
			break
		}
	}
	if tool == "" {
		return "", fmt.Errorf("no lp or lpr found; press s to save the report and print the file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return filepath.Base(tool), fmt.Errorf("%s", sanitizePluginLine(msg))
		}
		return filepath.Base(tool), err
	}
	return filepath.Base(tool), nil
}

func printReportCmd(text string) tea.Cmd {
	return func() tea.Msg {
		tool, err := printReport(text)
		return reportPrintedMsg{Tool: tool, Err: err}
	}
}

// handleReportExportKey runs c, s or p on the result overlay.
func (m Model) handleReportExportKey(key string) (Model, tea.Cmd, bool) {
	r := m.plugins.result
	if !r.exportable() {
		return m, nil, false
	}
	switch key {
	case "c":
		return m, clipboardWriteCmd(m.pluginResultPlainText(r)), true
	case "s":
		return m, saveReportCmd(r.pluginID, m.pluginResultPlainText(r)), true
	case "p":
		m.setStatus("printing…", false)
		return m, printReportCmd(m.pluginResultPlainText(r)), true
	}
	return m, nil, false
}

func (m Model) handleReportSaved(msg reportSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus("couldn't save report: "+sanitizePluginLine(msg.Err.Error()), true)
	} else {
		m.setStatus("saved report to "+msg.Path, false)
	}
	return m, m.clearStatusCmd()
}

func (m Model) handleReportPrinted(msg reportPrintedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.setStatus("print failed: "+sanitizePluginLine(msg.Err.Error()), true)
	} else {
		m.setStatus("sent report to the printer ("+msg.Tool+")", false)
	}
	return m, m.clearStatusCmd()
}
