package ui

// Manual plugin runs over several selected messages. The plugin is chosen
// once; each message then goes through exactly the single-message path
// (safe metadata, permission checks, the call, validation, annotation
// storage) one at a time, so a bulk run keeps one process for its plugin and
// the manager's global limit still applies. Bulk runs never touch automatic
// events: they work while auto-processing is off or paused and never change
// its failure counts.

import (
	"context"
	"fmt"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	// bulkConfirmThreshold is the selection size that asks before running.
	bulkConfirmThreshold = 10
	// bulkRefreshEvery is how often (in finished messages) a bulk run
	// refreshes Needs You; badges update after every message.
	bulkRefreshEvery = 5
	// bulkReasonsShown caps the failure reasons listed in the summary.
	bulkReasonsShown = 5
)

// pluginBulkRun is a bulk run in progress.
type pluginBulkRun struct {
	pluginID string
	name     string
	queue    []plugin.MessageMetadata
	total    int
	done     int
	ok       int
	failed   int
	reasons  []string
}

// pluginBulkStepMsg is one finished message of a bulk run.
type pluginBulkStepMsg struct {
	Result pluginResultMsg
}

// pluginTargets are the messages a manual run acts on: the selection when
// there is one (only rows still listed), otherwise the current message.
func (m Model) pluginTargets() []db.Message {
	if m.hasSelection() {
		return m.selectedActionMessages()
	}
	if msg := m.commandMessage(); msg != nil {
		return []db.Message{*msg}
	}
	return nil
}

// pluginRunLabel names the palette command for the current targets.
func (m Model) pluginRunLabel() string {
	switch n := len(m.pluginTargets()); {
	case n > 1:
		return fmt.Sprintf("Run plugin on %d selected messages", n)
	case n == 1 && m.hasSelection():
		return "Run plugin on selected message"
	}
	return "Run plugin on current message"
}

// startPluginRun runs the chosen plugin on the picker's targets: the usual
// single-message run, or a bulk run (after one confirmation when large).
func (m Model) startPluginRun(p plugin.Plugin) (tea.Model, tea.Cmd) {
	metas := m.plugins.pickerMetas
	m.plugins.picker = nil
	m.overlay = overlayNone
	switch {
	case len(metas) == 0:
		return m, nil
	case len(metas) == 1:
		m.plugins.running = p.Manifest.ID
		m.setStatus("running plugin "+p.Manifest.ID+"…", false)
		return m, runPluginCmd(m.plugins.ctx, m.plugins.manager, m.db, p.Manifest.ID, metas[0])
	case len(metas) >= bulkConfirmThreshold:
		m.confirmPluginAction(pluginConfirmAction{
			kind: runPluginBulk, pluginID: p.Manifest.ID,
			label: sanitizePluginLine(p.Manifest.Name), count: len(metas),
		})
		m.plugins.confirmOrigin = overlayNone
		return m, nil
	}
	return m.startBulk(p.Manifest.ID)
}

// startBulk queues the picker's targets for pluginID.
func (m Model) startBulk(pluginID string) (Model, tea.Cmd) {
	metas := m.plugins.pickerMetas
	m.plugins.pickerMetas = nil
	if len(metas) == 0 {
		return m, nil
	}
	run := &pluginBulkRun{pluginID: pluginID, name: m.pluginDisplayName(pluginID), total: len(metas)}
	next := metas[0]
	run.queue = append([]plugin.MessageMetadata(nil), metas[1:]...)
	m.plugins.bulk = run
	m.plugins.running = pluginID
	m.showBulkProgress()
	return m, runBulkStepCmd(m.plugins.ctx, m.plugins.manager, m.db, pluginID, next)
}

func runBulkStepCmd(ctx context.Context, mgr *plugin.Manager, database *db.DB, pluginID string, meta plugin.MessageMetadata) tea.Cmd {
	step := runPluginCmd(ctx, mgr, database, pluginID, meta)
	return func() tea.Msg {
		res, _ := step().(pluginResultMsg)
		return pluginBulkStepMsg{Result: res}
	}
}

// showBulkProgress puts progress on the status line without adding a log
// entry per message.
func (m *Model) showBulkProgress() {
	r := m.plugins.bulk
	m.statusMsg = fmt.Sprintf("%s: processing %d messages… %d / %d", r.name, r.total, r.done, r.total)
	m.statusErr = false
}

// handleBulkStep records one finished message, updates its badges at once,
// and starts the next message; one failure never stops the run.
func (m Model) handleBulkStep(msg pluginBulkStepMsg) (tea.Model, tea.Cmd) {
	r := m.plugins.bulk
	res := msg.Result
	if r == nil || res.PluginID != r.pluginID {
		return m, nil
	}
	r.done++
	failure := ""
	switch {
	case res.Err != nil:
		failure = res.Err.Error()
	case res.Result.Outcome == plugin.AnnotationsRejected || res.Result.Outcome == plugin.AnnotationsNotStored:
		failure = annotationNote(res.Result)
	}
	if failure != "" {
		r.failed++
		if len(r.reasons) < bulkReasonsShown {
			subject := truncate(sanitizePluginLine(res.Subject), 40)
			r.reasons = append(r.reasons, subject+": "+truncate(sanitizePluginLine(failure), 120))
		}
	} else {
		r.ok++
	}
	if res.RefreshedOK {
		if m.plugins.annotations == nil {
			m.plugins.annotations = map[int64][]db.PluginAnnotation{}
		}
		if len(res.Refreshed) == 0 {
			delete(m.plugins.annotations, res.MessageID)
		} else {
			m.plugins.annotations[res.MessageID] = res.Refreshed
		}
	}

	var cmds []tea.Cmd
	if len(r.queue) == 0 {
		return m.finishBulk()
	}
	if r.done%bulkRefreshEvery == 0 {
		cmds = append(cmds, m.needsYouRefreshCmd())
	}
	next := r.queue[0]
	r.queue = r.queue[1:]
	m.showBulkProgress()
	cmds = append(cmds, runBulkStepCmd(m.plugins.ctx, m.plugins.manager, m.db, r.pluginID, next))
	return m, tea.Batch(cmds...)
}

// finishBulk reports one summary for the whole run.
func (m Model) finishBulk() (tea.Model, tea.Cmd) {
	r := m.plugins.bulk
	m.plugins.bulk = nil
	if m.plugins.running == r.pluginID {
		m.plugins.running = ""
	}
	dot := m.styles.InlineMidDot()
	summary := fmt.Sprintf("%d succeeded%s%d failed", r.ok, dot, r.failed)
	body := []string{r.name + " finished", summary}
	if len(r.reasons) > 0 {
		body = append(body, "", "Failures:")
		for _, reason := range r.reasons {
			body = append(body, "  "+reason)
		}
		if r.failed > len(r.reasons) {
			body = append(body, fmt.Sprintf("  …and %d more", r.failed-len(r.reasons)))
		}
	}
	body = append(body, "", "Each message's annotations are under Message annotations.")
	m.plugins.result = &pluginResult{
		pluginID: r.pluginID, pluginName: r.name,
		subject: fmt.Sprintf("%d messages", r.total), body: strings.Join(body, "\n"),
	}
	m.setStatus(r.name+" finished: "+summary, r.failed > 0)
	// One summary window, and only if nothing else was opened meanwhile.
	if m.overlay == overlayNone || m.overlay == overlayPlugins {
		m.plugins.scroll = 0
		m.overlay = overlayPluginResult
	}
	return m, tea.Batch(m.clearStatusCmd(), loadPluginAnnotationCountsCmd(m.db), m.needsYouRefreshCmd())
}

// pruneSelection drops selected IDs whose rows are no longer loaded, e.g.
// after messages leave Needs You, so the selection count stays accurate.
func (m *Model) pruneSelection() {
	if len(m.selectedMessages) == 0 {
		return
	}
	present := make(map[int64]bool, len(m.messages))
	for _, msg := range m.messages {
		present[msg.ID] = true
	}
	for id := range m.selectedMessages {
		if !present[id] {
			delete(m.selectedMessages, id)
		}
	}
}

// bulkProgressLine is the plugin list's running line during a bulk run.
func (m Model) bulkProgressLine() string {
	if r := m.plugins.bulk; r != nil {
		return fmt.Sprintf("running: %s (%d / %d)", r.pluginID, r.done, r.total)
	}
	return "running: " + m.plugins.running
}
