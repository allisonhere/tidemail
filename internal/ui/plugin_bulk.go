package ui

// Reclassify: manual plugin runs over the current message, the selection, or
// every message in the current view. The plugin is chosen once; each message
// then goes through exactly the single-message path (safe metadata,
// permission checks, the call, validation, annotation storage) one at a time,
// so a run keeps one process for its plugin and the manager's global limit
// still applies. Manual runs never touch automatic events: they work while
// auto-processing is off or paused and never change its failure counts.
//
// Each message also records whether the plugin's stored annotation set
// changed, compared as normalized key=value pairs regardless of order and
// confidence, so a rerun after a plugin update shows what it altered.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
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

// pluginBulkRun is a multi-message Reclassify run in progress.
type pluginBulkRun struct {
	pluginID string
	name     string
	// scope says what is being rerun, e.g. "312 messages in Inbox".
	scope   string
	queue   []plugin.MessageMetadata
	total   int
	done    int
	ok      int
	failed  int
	reasons []string
	// changed and unchanged split the successes by whether the plugin's
	// stored annotation set differs afterwards.
	changed   int
	unchanged int
	// found tallies what the plugin concluded, from its own stored
	// annotations: category values, and automated senders.
	found map[string]int
	// ctx is this run's context; cancel kills the message in flight and
	// cancelled stops scheduling the rest. Quitting cancels the parent.
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled bool
}

// reclassifyScope is what the picker's plugin will be run on.
type reclassifyScope struct {
	// label describes the targets: "current message", "14 selected
	// messages", "312 messages in Inbox".
	label string
	// view is set for the explicit whole-view action, which always confirms.
	view bool
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
	return "Run plugin on " + m.contextScope(len(m.pluginTargets())) + "…"
}

// contextScope describes the context targets: the selection or the current
// message.
func (m Model) contextScope(n int) string {
	switch {
	case n > 1:
		return fmt.Sprintf("%d selected messages", n)
	case n == 1 && m.hasSelection():
		return "selected message"
	}
	return "current message"
}

// viewTargets are the messages currently represented by the list: the loaded
// rows of a mailbox, Unified Inbox, search, or virtual view (Needs You,
// Waiting on Them, Snoozed), never the IMAP folder as such. Only locally
// cached rows are used; nothing is downloaded, and rows without a database
// message are skipped.
func (m Model) viewTargets() []db.Message {
	if m.selectedOutboxRow() {
		return nil
	}
	seen := map[int64]bool{}
	var out []db.Message
	for _, msg := range m.filteredMessages {
		if msg.ID <= 0 || seen[msg.ID] {
			continue
		}
		seen[msg.ID] = true
		out = append(out, msg)
	}
	return out
}

// viewName names the current list for the Reclassify scope.
func (m Model) viewName() string {
	switch {
	case m.searchActive():
		return "search results"
	case m.selectedNeedsYou():
		return "Needs You"
	case m.selectedWaiting():
		return "Waiting on Them"
	case m.selectedSnoozed():
		return "Snoozed"
	case m.selectedUnifiedInbox():
		return "Unified Inbox"
	}
	if mb := m.selectedMailbox(); mb != nil && mb.Name != "" {
		return sanitizePluginLine(mb.Name)
	}
	return "this view"
}

// viewScopeLabel describes the whole-view targets.
func (m Model) viewScopeLabel(n int) string {
	noun := "messages"
	if n == 1 {
		noun = "message"
	}
	return fmt.Sprintf("%d %s in %s", n, noun, m.viewName())
}

// startPluginRun runs the chosen plugin on the picker's targets: the usual
// single-message run, or a bulk run (after one confirmation when large).
func (m Model) startPluginRun(p plugin.Plugin) (tea.Model, tea.Cmd) {
	m.plugins.last = p.Manifest.ID
	metas := m.plugins.pickerMetas
	m.plugins.picker = nil
	m.overlay = overlayNone
	switch {
	case len(metas) == 0:
		return m, nil
	case m.plugins.pickerScope.view || len(metas) >= bulkConfirmThreshold:
		// A whole view always asks first, even for one message.
		m.confirmPluginAction(pluginConfirmAction{
			kind: runPluginBulk, pluginID: p.Manifest.ID,
			label: sanitizePluginLine(p.Manifest.Name), count: len(metas),
			scope: m.plugins.pickerScope.label, view: m.plugins.pickerScope.view,
		})
		m.plugins.confirmOrigin = overlayNone
		return m, nil
	case len(metas) == 1:
		source := m.pluginResultMessage(metas[0].ID)
		m.plugins.pickerMetas = nil
		m.plugins.running = p.Manifest.ID
		m.setStatus("Running "+m.pluginDisplayName(p.Manifest.ID)+" on "+m.plugins.pickerScope.label+"…", false)
		m.plugins.pickerScope = reclassifyScope{}
		return m, runPluginCmd(m.plugins.ctx, m.plugins.manager, m.db, p.Manifest.ID, metas[0], source)
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
	scope := m.plugins.pickerScope.label
	m.plugins.pickerScope = reclassifyScope{}
	if scope == "" {
		scope = m.contextScope(len(metas))
	}
	// The run's own context lets Cancel kill just this run; quitting cancels
	// the parent, so no plugin process outlives TideMail.
	ctx, cancel := context.WithCancel(m.plugins.ctx)
	run := &pluginBulkRun{
		pluginID: pluginID, name: m.pluginDisplayName(pluginID), scope: scope,
		total: len(metas), ctx: ctx, cancel: cancel,
	}
	next := metas[0]
	run.queue = append([]plugin.MessageMetadata(nil), metas[1:]...)
	m.plugins.bulk = run
	m.plugins.running = pluginID
	m.showBulkProgress()
	return m, runBulkStepCmd(ctx, m.plugins.manager, m.db, pluginID, next)
}

// cancelBulk stops a Reclassify run: nothing more is scheduled and the
// message in flight is killed. The run then finishes with a summary of what
// completed.
func (m Model) cancelBulk() (tea.Model, tea.Cmd) {
	r := m.plugins.bulk
	if r == nil || r.cancelled {
		return m, nil
	}
	r.cancelled = true
	r.queue = nil
	r.cancel()
	m.setStatus(r.name+": cancelling…", false)
	return m, nil
}

func runBulkStepCmd(ctx context.Context, mgr *plugin.Manager, database *db.DB, pluginID string, meta plugin.MessageMetadata) tea.Cmd {
	step := runPluginCmd(ctx, mgr, database, pluginID, meta, nil)
	return func() tea.Msg {
		res, _ := step().(pluginResultMsg)
		return pluginBulkStepMsg{Result: res}
	}
}

// showBulkProgress puts progress on the status line without adding a log
// entry per message.
func (m *Model) showBulkProgress() {
	r := m.plugins.bulk
	m.statusMsg = fmt.Sprintf("%s: running on %s… %d / %d", r.name, r.scope, r.done, r.total)
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
	if r.cancelled && errors.Is(res.Err, context.Canceled) {
		// The message killed by Cancel is neither a success nor a failure.
		return m.finishBulk()
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
		if res.Compared {
			if res.Changed {
				r.changed++
			} else {
				r.unchanged++
			}
		}
	}
	if res.RefreshedOK && failure == "" {
		for _, label := range foundLabels(res.Refreshed, r.pluginID) {
			if r.found == nil {
				r.found = map[string]int{}
			}
			r.found[label]++
		}
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
	if len(r.queue) == 0 || r.cancelled {
		return m.finishBulk()
	}
	if r.done%bulkRefreshEvery == 0 {
		cmds = append(cmds, m.needsYouRefreshCmd())
	}
	next := r.queue[0]
	r.queue = r.queue[1:]
	m.showBulkProgress()
	cmds = append(cmds, runBulkStepCmd(r.ctx, m.plugins.manager, m.db, r.pluginID, next))
	return m, tea.Batch(cmds...)
}

// finishBulk reports one summary for the whole run.
func (m Model) finishBulk() (tea.Model, tea.Cmd) {
	r := m.plugins.bulk
	m.plugins.bulk = nil
	r.cancel()
	if m.plugins.running == r.pluginID {
		m.plugins.running = ""
	}
	dot := m.styles.InlineMidDot()
	summary := fmt.Sprintf("%d succeeded%s%d failed", r.ok, dot, r.failed)
	card := resultCard{title: r.name + " completed", status: plugin.StatusSuccess}
	switch {
	case r.cancelled:
		card.title, card.status = r.name+" was cancelled", plugin.StatusNeutral
	case r.failed > 0:
		card.title, card.status = r.name+" completed with some problems", plugin.StatusWarning
	}
	title := card.title
	card.summary = "Ran on " + r.scope + "."
	card.facts = []plugin.Fact{
		{Label: "Processed", Value: fmt.Sprint(r.done)},
		{Label: "Changed", Value: fmt.Sprint(r.changed)},
		{Label: "Unchanged", Value: fmt.Sprint(r.unchanged)},
		{Label: "Failed", Value: fmt.Sprint(r.failed)},
	}
	if r.cancelled {
		card.facts = append(card.facts, plugin.Fact{Label: "Not run", Value: fmt.Sprint(r.total - r.done)})
	}
	if len(r.found) > 0 {
		labels := make([]string, 0, len(r.found))
		for l := range r.found {
			labels = append(labels, l)
		}
		sort.Slice(labels, func(i, j int) bool {
			if r.found[labels[i]] != r.found[labels[j]] {
				return r.found[labels[i]] > r.found[labels[j]]
			}
			return labels[i] < labels[j]
		})
		sec := cardSection{title: "Found"}
		for _, l := range labels[:min(len(labels), 8)] {
			sec.facts = append(sec.facts, plugin.Fact{Label: l, Value: fmt.Sprint(r.found[l])})
		}
		card.sections = append(card.sections, sec)
	}
	details := append(m.pluginIdentityLines(r.pluginID), "Scope: "+r.scope,
		fmt.Sprintf("Succeeded: %d, failed: %d, changed: %d, unchanged: %d", r.ok, r.failed, r.changed, r.unchanged))
	if len(r.reasons) > 0 {
		details = append(details, "", "Failures:")
		for _, reason := range r.reasons {
			details = append(details, "  "+reason)
		}
		if r.failed > len(r.reasons) {
			details = append(details, fmt.Sprintf("  …and %d more", r.failed-len(r.reasons)))
		}
	}
	details = append(details, "", "Each message's raw annotations are under Message annotations.")
	m.plugins.result = &pluginResult{
		pluginID: r.pluginID, pluginName: r.name,
		subject: r.scope, card: &card, details: details,
	}
	m.setStatus(title+": "+summary+dot+fmt.Sprintf("%d changed", r.changed), r.failed > 0)
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
		return fmt.Sprintf("running: %s, reclassifying %s (%d / %d)", r.pluginID, r.scope, r.done, r.total)
	}
	return "running: " + m.plugins.running
}

// annotationSet is a plugin's stored annotations for one message in
// normalized form: key=value pairs, value trimmed and lower-cased (the same
// folding the attention rules use), sorted, confidence ignored. Two sets are
// the same classification exactly when these are equal.
func annotationSet(anns []db.PluginAnnotation, pluginID string) []string {
	var out []string
	for _, a := range anns {
		if a.PluginID == pluginID {
			out = append(out, a.Key+"="+strings.ToLower(strings.TrimSpace(a.Value)))
		}
	}
	slices.Sort(out)
	return out
}

// annotationSetChanged compares a plugin's annotations before and after a run.
func annotationSetChanged(before, after []db.PluginAnnotation, pluginID string) bool {
	return !slices.Equal(annotationSet(before, pluginID), annotationSet(after, pluginID))
}

// foundLabels are the friendly conclusions one message's stored annotations
// from pluginID express, for the bulk summary: its category, or that the
// sender is automated.
func foundLabels(anns []db.PluginAnnotation, pluginID string) []string {
	var category string
	automated := false
	for _, a := range anns {
		if a.PluginID != pluginID {
			continue
		}
		switch a.Key {
		case "category":
			category = friendlyValue("category", a.Value)
		case "automated_sender":
			automated = strings.EqualFold(strings.TrimSpace(a.Value), "true")
		}
	}
	switch {
	case category != "":
		return []string{category}
	case automated:
		return []string{"Automated sender"}
	}
	return nil
}
