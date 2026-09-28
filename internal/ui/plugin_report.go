package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/pluginquery"
	tea "github.com/charmbracelet/bubbletea"
)

// Plugin reports: enter on a plugin that declares report.run in the plugin
// list runs its report. TideMail executes the report's read-only queries
// (internal/pluginquery) and shows the final output in the plugin result
// overlay, sanitized like any other plugin output.

// pluginReportMsg reports a finished report back to Update.
type pluginReportMsg struct {
	PluginID string
	Result   plugin.ReportResult
	Err      error
	Elapsed  time.Duration
}

// pluginHasReport reports whether the plugin declares report.run.
func (m Model) pluginHasReport(pluginID string) bool {
	p, ok := m.plugins.manager.Plugin(pluginID)
	return ok && p.Manifest.HasCapability(plugin.CapabilityReport)
}

// reportContext describes where the report was started: the folder or
// account selected in the sidebar, if any.
func (m Model) reportContext() plugin.ReportContext {
	now := time.Now()
	rc := plugin.ReportContext{Now: now.Format(time.RFC3339), Timezone: now.Location().String()}
	if mb := m.selectedMailbox(); mb != nil {
		rc.MailboxName = mb.Name
		if acc := m.accountByID(mb.AccountID); acc != nil {
			rc.AccountName = acc.Name
		}
	} else if id, ok := m.selectedAccountID(); ok {
		if acc := m.accountByID(id); acc != nil {
			rc.AccountName = acc.Name
		}
	}
	return rc
}

func (m Model) startPluginReport(pluginID string) (tea.Model, tea.Cmd) {
	if m.plugins.running != "" {
		m.setStatus("a plugin is already running", true)
		return m, m.clearStatusCmd()
	}
	m.plugins.running = pluginID
	m.setStatus("Running report "+m.pluginDisplayName(pluginID)+"…", false)
	exec := &pluginquery.Executor{DB: m.db, Me: myAddresses(m.accountIdentities()), Location: time.Local}
	return m, runPluginReportCmd(m.plugins.ctx, m.plugins.manager, m.db, exec, pluginID, m.reportContext())
}

// runPluginReportCmd runs the report off the Update loop. It captures only
// the manager, executor, and context, never the Model.
func runPluginReportCmd(ctx context.Context, mgr *plugin.Manager, database *db.DB, exec *pluginquery.Executor, pluginID string, rc plugin.ReportContext) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		if database == nil {
			return pluginReportMsg{PluginID: pluginID, Err: fmt.Errorf("plugin %q: no mail cache", pluginID)}
		}
		res, err := mgr.Report(ctx, pluginID, rc, exec)
		return pluginReportMsg{PluginID: pluginID, Result: res, Err: err, Elapsed: time.Since(start)}
	}
}

func (m Model) handlePluginReport(msg pluginReportMsg) (tea.Model, tea.Cmd) {
	if m.plugins.running == msg.PluginID {
		m.plugins.running = ""
	}
	if msg.Err != nil {
		name := m.pluginDisplayName(msg.PluginID)
		card := errorCard(name, msg.Err)
		if !strings.Contains(card.title, "timed out") && !strings.Contains(card.title, "cancelled") {
			card.title = name + " could not create its report"
		}
		details := append(m.pluginIdentityLines(msg.PluginID), "", "Error:")
		for _, l := range m.displayPluginErrorLines(msg.Err) {
			details = append(details, "  "+l)
		}
		m.plugins.result = &pluginResult{pluginID: msg.PluginID, pluginName: name, card: &card, details: details}
		if m.overlay == overlayNone || m.overlay == overlayPlugins {
			m.plugins.scroll = 0
			m.overlay = overlayPluginResult
		}
		m.setStatus(card.title, true)
		return m, m.clearStatusCmd()
	}
	m.plugins.result = &pluginResult{
		pluginID:   msg.PluginID,
		pluginName: sanitizePluginLine(m.pluginDisplayName(msg.PluginID)),
		heading:    "report: " + plural(msg.Result.Rounds, "round") + ", " + queriesText(msg.Result.Queries),
		body:       formatPluginData(msg.Result.Report),
		view:       msg.Result.View,
	}
	status := fmt.Sprintf("report %s completed (%v)", msg.PluginID, msg.Elapsed.Round(time.Millisecond))
	if m.overlay == overlayNone || m.overlay == overlayPlugins {
		m.plugins.scroll = 0
		m.overlay = overlayPluginResult
	} else {
		status += "; open Plugins to view the result"
	}
	m.setStatus(status, false)
	return m, m.clearStatusCmd()
}

func queriesText(n int) string {
	if n == 1 {
		return "1 query"
	}
	return fmt.Sprintf("%d queries", n)
}
