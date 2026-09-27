package plugin

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Default limits for automatic events. They keep a burst of new mail from
// starting a burst of processes or growing an unbounded backlog.
const (
	// EventQueueLimit is the most messages waiting per plugin. Further events
	// are dropped and counted.
	EventQueueLimit = 50
	// EventGlobalLimit is the most plugin processes running at once for
	// automatic events, across all plugins. Each plugin runs one at a time.
	EventGlobalLimit = 3
	// EventRateLimit calls per EventRateWindow is the most automatic calls one
	// plugin gets. Extra work waits in the queue; it is not dropped for this.
	EventRateLimit  = 30
	EventRateWindow = time.Minute
	// EventFailureLimit consecutive failures pause a plugin until the user
	// resumes it.
	EventFailureLimit = 3
	// eventUpdateBuffer is how many finished runs can wait for the UI.
	eventUpdateBuffer = 64
)

// EventOptions overrides the default limits. Zero fields use the defaults.
type EventOptions struct {
	QueueLimit   int
	GlobalLimit  int
	RateLimit    int
	RateWindow   time.Duration
	FailureLimit int
}

func (o EventOptions) withDefaults() EventOptions {
	if o.QueueLimit <= 0 {
		o.QueueLimit = EventQueueLimit
	}
	if o.GlobalLimit <= 0 {
		o.GlobalLimit = EventGlobalLimit
	}
	if o.RateLimit <= 0 {
		o.RateLimit = EventRateLimit
	}
	if o.RateWindow <= 0 {
		o.RateWindow = EventRateWindow
	}
	if o.FailureLimit <= 0 {
		o.FailureLimit = EventFailureLimit
	}
	return o
}

// EventUpdate reports one finished automatic run.
type EventUpdate struct {
	PluginID  string
	MessageID int64
	Result    MessageMetadataResult
	Err       error
	// Failed is true when the run counted as a failure: the call failed, or
	// its annotations were rejected or could not be stored.
	Failed bool
	// Paused is true when this failure paused the plugin.
	Paused bool
}

// RuntimeStatus is a snapshot of one plugin's automatic-event state.
type RuntimeStatus struct {
	Enabled             bool
	Queued              int
	Running             bool
	ConsecutiveFailures int
	Dropped             uint64
	Paused              bool
	LastError           string
}

// eventCall runs one automatic call; tests replace it.
type eventCall func(ctx context.Context, pluginID string, meta MessageMetadata) (MessageMetadataResult, error)

// EventManager delivers message.received events to plugins in the
// background. Every plugin that declares the event gets one worker, so it
// never runs more than one process at a time; a shared semaphore caps
// processes across plugins. It never touches the UI: finished runs are sent
// on Updates, and the UI reads Status snapshots.
type EventManager struct {
	opts    EventOptions
	call    eventCall
	now     func() time.Time
	ctx     context.Context
	cancel  context.CancelFunc
	sem     chan struct{}
	updates chan EventUpdate
	wg      sync.WaitGroup

	mu      sync.Mutex
	plugins map[string]*eventPlugin
}

type eventPlugin struct {
	enabled  bool
	paused   bool
	queue    []MessageMetadata
	pending  map[int64]bool // queued or running, for dedupe
	running  bool
	failures int
	dropped  uint64
	lastErr  string
	calls    []time.Time // call start times inside the rate window
	wake     chan struct{}
}

// NewEventManager starts workers for every plugin in mgr that declares
// message.received and may receive message metadata. All start disabled.
// Annotations reach store through the same permission and validation path as
// manual runs. Cancelling ctx, or calling Close, stops everything.
func NewEventManager(ctx context.Context, mgr *Manager, store AnnotationStore, opts EventOptions) *EventManager {
	call := func(ctx context.Context, pluginID string, meta MessageMetadata) (MessageMetadataResult, error) {
		return mgr.messageEvent(ctx, pluginID, meta, store)
	}
	var ids []string
	for _, p := range mgr.Plugins() {
		if EventCapable(p) {
			ids = append(ids, p.Manifest.ID)
		}
	}
	return newEventManager(ctx, ids, call, opts)
}

func newEventManager(ctx context.Context, pluginIDs []string, call eventCall, opts EventOptions) *EventManager {
	opts = opts.withDefaults()
	ctx, cancel := context.WithCancel(ctx)
	e := &EventManager{
		opts:    opts,
		call:    call,
		now:     time.Now,
		ctx:     ctx,
		cancel:  cancel,
		sem:     make(chan struct{}, opts.GlobalLimit),
		updates: make(chan EventUpdate, eventUpdateBuffer),
		plugins: map[string]*eventPlugin{},
	}
	for _, id := range pluginIDs {
		p := &eventPlugin{pending: map[int64]bool{}, wake: make(chan struct{}, 1)}
		e.plugins[id] = p
		e.wg.Add(1)
		go e.worker(id, p)
	}
	return e
}

// EventCapable reports whether a plugin can ever receive message.received:
// it declares the event and has the message_metadata permission. The user
// still has to enable it.
func EventCapable(p Plugin) bool {
	return p.Manifest.WantsEvent(EventMessageReceived) && p.Manifest.Permissions.MessageMetadata
}

// Updates delivers finished runs. The UI should keep reading it; a full
// buffer makes workers wait (bounded by the queue limit), never lose results.
func (e *EventManager) Updates() <-chan EventUpdate {
	if e == nil {
		return nil
	}
	return e.updates
}

// SetEnabled turns automatic events on or off for a plugin. Disabling drops
// any waiting work; a call already running finishes.
func (e *EventManager) SetEnabled(pluginID string, on bool) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.plugins[pluginID]
	if p == nil {
		return
	}
	p.enabled = on
	if !on {
		p.clearQueue()
	}
	p.signal()
}

// Enqueue offers new messages to every enabled plugin. Messages already
// waiting or running for a plugin are skipped; a paused plugin or a full
// queue drops the event and counts it.
func (e *EventManager) Enqueue(metas ...MessageMetadata) {
	if e == nil || len(metas) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range e.plugins {
		if !p.enabled {
			continue
		}
		for _, meta := range metas {
			switch {
			case p.pending[meta.ID]:
				// Already waiting or running.
			case p.paused, len(p.queue) >= e.opts.QueueLimit:
				p.dropped++
			default:
				p.queue = append(p.queue, meta)
				p.pending[meta.ID] = true
			}
		}
		p.signal()
	}
}

// Resume un-pauses a plugin and resets its failure count. Dropped events are
// not replayed and the dropped count is kept.
func (e *EventManager) Resume(pluginID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if p := e.plugins[pluginID]; p != nil {
		p.paused = false
		p.failures = 0
		p.lastErr = ""
		p.signal()
	}
}

// Status returns a snapshot for every event-capable plugin.
func (e *EventManager) Status() map[string]RuntimeStatus {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]RuntimeStatus, len(e.plugins))
	for id, p := range e.plugins {
		out[id] = RuntimeStatus{
			Enabled: p.enabled, Queued: len(p.queue), Running: p.running,
			ConsecutiveFailures: p.failures, Dropped: p.dropped, Paused: p.paused, LastError: p.lastErr,
		}
	}
	return out
}

// PluginIDs lists the event-capable plugins, sorted.
func (e *EventManager) PluginIDs() []string {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ids := make([]string, 0, len(e.plugins))
	for id := range e.plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Close stops the workers and kills running plugin processes (through their
// context). It waits at most timeout for workers to exit and reports whether
// they did.
func (e *EventManager) Close(timeout time.Duration) bool {
	if e == nil {
		return true
	}
	e.cancel()
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (p *eventPlugin) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *eventPlugin) clearQueue() {
	for _, meta := range p.queue {
		delete(p.pending, meta.ID)
	}
	p.queue = nil
}

// worker runs one plugin's queue, one call at a time.
func (e *EventManager) worker(id string, p *eventPlugin) {
	defer e.wg.Done()
	for {
		meta, wait, ok := e.next(p)
		if !ok {
			// Nothing runnable: sleep until new work, a state change, or (when
			// rate limited) the window frees a slot.
			var t *time.Timer
			var timer <-chan time.Time
			if wait > 0 {
				t = time.NewTimer(wait)
				timer = t.C
			}
			select {
			case <-e.ctx.Done():
				if t != nil {
					t.Stop()
				}
				return
			case <-p.wake:
			case <-timer:
			}
			if t != nil {
				t.Stop()
			}
			continue
		}

		select {
		case e.sem <- struct{}{}:
		case <-e.ctx.Done():
			return
		}
		e.mu.Lock()
		p.calls = append(p.calls, e.now())
		e.mu.Unlock()

		result, err := e.call(e.ctx, id, meta)
		<-e.sem
		if e.ctx.Err() != nil {
			return // shutting down; the result no longer matters
		}

		update := EventUpdate{PluginID: id, MessageID: meta.ID, Result: result, Err: err}
		e.mu.Lock()
		p.running = false
		delete(p.pending, meta.ID)
		if failure := runFailure(result, err); failure != "" {
			update.Failed = true
			p.failures++
			p.lastErr = failure
			if p.failures >= e.opts.FailureLimit && !p.paused {
				p.paused = true
				update.Paused = true
				p.clearQueue()
			}
		} else {
			p.failures = 0
			p.lastErr = ""
		}
		e.mu.Unlock()

		select {
		case e.updates <- update:
		case <-e.ctx.Done():
			return
		}
	}
}

// next pops the plugin's next message if it may run now. Otherwise it says how
// long to wait before the rate window frees a slot (0 means wait for a
// signal).
func (e *EventManager) next(p *eventPlugin) (MessageMetadata, time.Duration, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !p.enabled || p.paused || len(p.queue) == 0 {
		return MessageMetadata{}, 0, false
	}
	now := e.now()
	cutoff := now.Add(-e.opts.RateWindow)
	keep := p.calls[:0]
	for _, t := range p.calls {
		if t.After(cutoff) {
			keep = append(keep, t)
		}
	}
	p.calls = keep
	if len(p.calls) >= e.opts.RateLimit {
		return MessageMetadata{}, p.calls[0].Add(e.opts.RateWindow).Sub(now) + time.Millisecond, false
	}
	meta := p.queue[0]
	p.queue = p.queue[1:]
	p.running = true
	return meta, 0, true
}

// runFailure describes why a run counts as a failure, or returns "".
func runFailure(result MessageMetadataResult, err error) string {
	switch {
	case err != nil:
		return err.Error()
	case result.Outcome == AnnotationsRejected, result.Outcome == AnnotationsNotStored:
		if result.AnnotationErr != nil {
			return "annotations: " + result.AnnotationErr.Error()
		}
		return "annotations were not stored"
	}
	return ""
}
