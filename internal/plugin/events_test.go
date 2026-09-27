package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// fakeCalls is a controllable eventCall. Each call blocks until release is
// closed (when set), and records concurrency.
type fakeCalls struct {
	mu        sync.Mutex
	calls     []string // "plugin:messageID"
	perPlugin map[string]int
	maxPer    map[string]int
	active    atomic.Int32
	maxActive atomic.Int32
	release   chan struct{}
	result    func(pluginID string, n int) (MessageMetadataResult, error)
}

func newFakeCalls() *fakeCalls {
	return &fakeCalls{perPlugin: map[string]int{}, maxPer: map[string]int{}}
}

func (f *fakeCalls) call(ctx context.Context, pluginID string, meta MessageMetadata) (MessageMetadataResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, pluginID+":"+strconv.FormatInt(meta.ID, 10))
	n := len(f.calls)
	f.perPlugin[pluginID]++
	f.maxPer[pluginID] = max(f.maxPer[pluginID], f.perPlugin[pluginID])
	release := f.release
	result := f.result
	f.mu.Unlock()

	now := f.active.Add(1)
	for {
		peak := f.maxActive.Load()
		if now <= peak || f.maxActive.CompareAndSwap(peak, now) {
			break
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	f.active.Add(-1)
	f.mu.Lock()
	f.perPlugin[pluginID]--
	f.mu.Unlock()
	if result != nil {
		return result(pluginID, n)
	}
	return MessageMetadataResult{Outcome: AnnotationsStored}, nil
}

func (f *fakeCalls) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// waitFor polls cond until it holds or fails the test after 3s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// drain collects n updates.
func drain(t *testing.T, e *EventManager, n int) []EventUpdate {
	t.Helper()
	var out []EventUpdate
	for len(out) < n {
		select {
		case u := <-e.Updates():
			out = append(out, u)
		case <-time.After(3 * time.Second):
			t.Fatalf("got %d of %d updates", len(out), n)
		}
	}
	return out
}

func startFake(t *testing.T, ids []string, f *fakeCalls, opts EventOptions) *EventManager {
	t.Helper()
	e := newEventManager(context.Background(), ids, f.call, opts)
	t.Cleanup(func() {
		if f.release != nil {
			select {
			case <-f.release:
			default:
				close(f.release)
			}
		}
		e.Close(3 * time.Second)
	})
	return e
}

func msgs(ids ...int64) []MessageMetadata {
	out := make([]MessageMetadata, len(ids))
	for i, id := range ids {
		out[i] = MessageMetadata{ID: id}
	}
	return out
}

func TestEventsDisabledByDefault(t *testing.T) {
	f := newFakeCalls()
	e := startFake(t, []string{"p"}, f, EventOptions{})
	e.Enqueue(msgs(1)...)
	time.Sleep(50 * time.Millisecond)
	if f.count() != 0 || e.Status()["p"].Enabled {
		t.Fatal("a plugin must not run until the user enables it")
	}
}

func TestEventsEnqueueRuns(t *testing.T) {
	f := newFakeCalls()
	e := startFake(t, []string{"p"}, f, EventOptions{})
	e.SetEnabled("p", true)
	e.Enqueue(msgs(1, 2)...)
	ups := drain(t, e, 2)
	if ups[0].MessageID != 1 || ups[1].MessageID != 2 || ups[0].Failed {
		t.Fatalf("updates = %+v", ups)
	}
}

func TestEventsDedupeQueuedAndRunning(t *testing.T) {
	f := newFakeCalls()
	f.release = make(chan struct{})
	e := startFake(t, []string{"p"}, f, EventOptions{})
	e.SetEnabled("p", true)
	e.Enqueue(msgs(1)...)
	waitFor(t, "message 1 running", func() bool { return e.Status()["p"].Running })
	e.Enqueue(msgs(1, 2, 2)...) // 1 is running; 2 appears twice
	if st := e.Status()["p"]; st.Queued != 1 || st.Dropped != 0 {
		t.Fatalf("status = %+v", st)
	}
	close(f.release)
	drain(t, e, 2)
	if f.count() != 2 {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestEventsQueueLimitDropsNewEvents(t *testing.T) {
	f := newFakeCalls()
	f.release = make(chan struct{})
	e := startFake(t, []string{"p"}, f, EventOptions{QueueLimit: 3})
	e.SetEnabled("p", true)
	e.Enqueue(msgs(1)...)
	waitFor(t, "first call running", func() bool { return e.Status()["p"].Running })
	for id := int64(2); id <= 11; id++ {
		e.Enqueue(msgs(id)...)
		if q := e.Status()["p"].Queued; q > 3 {
			t.Fatalf("queue grew to %d", q)
		}
	}
	st := e.Status()["p"]
	if st.Queued != 3 || st.Dropped != 7 {
		t.Fatalf("status = %+v", st)
	}
	close(f.release)
	drain(t, e, 4)
	// The oldest queued messages were kept; the newest were dropped.
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 4 || f.calls[1] != "p:2" || f.calls[3] != "p:4" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestEventsOneProcessPerPluginAndGlobalCap(t *testing.T) {
	f := newFakeCalls()
	f.release = make(chan struct{})
	ids := []string{"a", "b", "c", "d", "e"}
	e := startFake(t, ids, f, EventOptions{GlobalLimit: 2})
	for _, id := range ids {
		e.SetEnabled(id, true)
	}
	e.Enqueue(msgs(1, 2, 3)...)
	waitFor(t, "two calls running", func() bool { return f.active.Load() == 2 })
	time.Sleep(50 * time.Millisecond) // give any extra worker a chance to start
	if got := f.active.Load(); got != 2 {
		t.Fatalf("active = %d, want the global cap of 2", got)
	}
	close(f.release)
	drain(t, e, 15)
	if f.maxActive.Load() > 2 {
		t.Fatalf("max active = %d", f.maxActive.Load())
	}
	for id, n := range f.maxPer {
		if n > 1 {
			t.Fatalf("plugin %s ran %d at once", id, n)
		}
	}
}

func TestEventsRateLimitDelaysWithoutDropping(t *testing.T) {
	f := newFakeCalls()
	e := startFake(t, []string{"p"}, f, EventOptions{RateLimit: 2, RateWindow: 300 * time.Millisecond})
	e.SetEnabled("p", true)
	start := time.Now()
	e.Enqueue(msgs(1, 2, 3, 4)...)
	drain(t, e, 2)
	time.Sleep(100 * time.Millisecond)
	if n := f.count(); n != 2 {
		t.Fatalf("calls within the window = %d, want 2", n)
	}
	if st := e.Status()["p"]; st.Queued != 2 || st.Dropped != 0 {
		t.Fatalf("rate-limited work should wait, not drop: %+v", st)
	}
	drain(t, e, 2)
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("all four ran in %v; the rate limit was not applied", elapsed)
	}
}

func failingResult(errs ...bool) func(string, int) (MessageMetadataResult, error) {
	return func(_ string, n int) (MessageMetadataResult, error) {
		if n <= len(errs) && errs[n-1] {
			return MessageMetadataResult{}, errors.New("plugin \"p\" timed out after 5s")
		}
		return MessageMetadataResult{Outcome: AnnotationsStored}, nil
	}
}

func TestEventsFailuresCountAndSuccessResets(t *testing.T) {
	f := newFakeCalls()
	f.result = failingResult(true, true, false, true)
	e := startFake(t, []string{"p"}, f, EventOptions{})
	e.SetEnabled("p", true)
	e.Enqueue(msgs(1)...)
	drain(t, e, 1)
	e.Enqueue(msgs(2)...)
	drain(t, e, 1)
	if st := e.Status()["p"]; st.ConsecutiveFailures != 2 || st.Paused || st.LastError == "" {
		t.Fatalf("after two failures: %+v", st)
	}
	e.Enqueue(msgs(3)...)
	drain(t, e, 1)
	if st := e.Status()["p"]; st.ConsecutiveFailures != 0 || st.LastError != "" {
		t.Fatalf("success should reset: %+v", st)
	}
	e.Enqueue(msgs(4)...)
	drain(t, e, 1)
	if st := e.Status()["p"]; st.ConsecutiveFailures != 1 {
		t.Fatalf("after one more failure: %+v", st)
	}
}

func TestEventsPauseAfterFailuresAndResume(t *testing.T) {
	f := newFakeCalls()
	f.result = failingResult(true, true, true)
	e := startFake(t, []string{"p"}, f, EventOptions{})
	e.SetEnabled("p", true)
	var last EventUpdate
	for id := int64(1); id <= 3; id++ {
		e.Enqueue(msgs(id)...)
		last = drain(t, e, 1)[0]
	}
	st := e.Status()["p"]
	if !st.Paused || !last.Paused || st.ConsecutiveFailures != 3 || st.Queued != 0 {
		t.Fatalf("status = %+v, last = %+v", st, last)
	}

	// Paused: new work is refused and counted, and nothing runs.
	e.Enqueue(msgs(4, 5)...)
	time.Sleep(50 * time.Millisecond)
	if st := e.Status()["p"]; st.Dropped != 2 || st.Queued != 0 || f.count() != 3 {
		t.Fatalf("paused plugin kept working: %+v, calls %d", st, f.count())
	}

	e.Resume("p")
	st = e.Status()["p"]
	if st.Paused || st.ConsecutiveFailures != 0 || st.Dropped != 2 || st.LastError != "" {
		t.Fatalf("after resume: %+v", st)
	}
	time.Sleep(50 * time.Millisecond)
	if f.count() != 3 {
		t.Fatal("resume must not replay dropped events")
	}
	e.Enqueue(msgs(6)...)
	if u := drain(t, e, 1)[0]; u.MessageID != 6 || u.Failed {
		t.Fatalf("update = %+v", u)
	}
}

func TestEventsAnnotationProblemsCountAsFailures(t *testing.T) {
	for _, outcome := range []AnnotationOutcome{AnnotationsRejected, AnnotationsNotStored} {
		if runFailure(MessageMetadataResult{Outcome: outcome, AnnotationErr: errors.New("bad")}, nil) == "" {
			t.Errorf("outcome %v should be a failure", outcome)
		}
	}
	for _, outcome := range []AnnotationOutcome{AnnotationsStored, AnnotationsNotPermitted} {
		if runFailure(MessageMetadataResult{Outcome: outcome}, nil) != "" {
			t.Errorf("outcome %v should be a success", outcome)
		}
	}
}

func TestEventsDisableClearsQueue(t *testing.T) {
	f := newFakeCalls()
	f.release = make(chan struct{})
	e := startFake(t, []string{"p"}, f, EventOptions{})
	e.SetEnabled("p", true)
	e.Enqueue(msgs(1, 2, 3)...)
	waitFor(t, "first call", func() bool { return e.Status()["p"].Running })
	e.SetEnabled("p", false)
	if st := e.Status()["p"]; st.Queued != 0 {
		t.Fatalf("status = %+v", st)
	}
	close(f.release)
	drain(t, e, 1) // the running call finishes
	time.Sleep(50 * time.Millisecond)
	if f.count() != 1 {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestEventsCloseStopsWorkers(t *testing.T) {
	f := newFakeCalls()
	f.release = make(chan struct{}) // never released: only cancellation ends the call
	e := newEventManager(context.Background(), []string{"a", "b"}, f.call, EventOptions{})
	e.SetEnabled("a", true)
	e.Enqueue(msgs(1)...)
	waitFor(t, "call running", func() bool { return f.active.Load() == 1 })
	start := time.Now()
	if !e.Close(3 * time.Second) {
		t.Fatal("workers did not exit")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("close took %v", elapsed)
	}
	if f.active.Load() != 0 {
		t.Fatal("the running call was not cancelled")
	}
}

// ── Real plugin processes ────────────────────────────────────────────────────

const eventManifest = "events = [\"message.received\"]\n[permissions]\nmessage_metadata = true\nannotations = true\n"

func realEventManager(t *testing.T, command, manifestExtra string, timeout time.Duration) (*EventManager, *recordingStore, string) {
	t.Helper()
	root := t.TempDir()
	dir := installPlugin(t, root, "p", "p", command, manifestExtra)
	m, err := Discover(root)
	if err != nil || len(m.Errors()) > 0 {
		t.Fatalf("discover: %v %v", err, m.Errors())
	}
	m.Timeout = timeout
	store := &recordingStore{}
	e := NewEventManager(context.Background(), m, store, EventOptions{})
	t.Cleanup(func() { e.Close(5 * time.Second) })
	return e, store, dir
}

func TestEventsRealPluginStoresAnnotations(t *testing.T) {
	e, store, _ := realEventManager(t, "tidemail-plugin-annotate", eventManifest, DefaultTimeout)
	e.SetEnabled("p", true)
	e.Enqueue(MessageMetadata{ID: 42, Subject: "hi"})
	u := drain(t, e, 1)[0]
	if u.Err != nil || u.Failed || u.Result.Outcome != AnnotationsStored {
		t.Fatalf("update = %+v", u)
	}
	if len(store.calls) != 1 || store.calls[0].messageID != 42 || len(store.calls[0].anns) != 2 {
		t.Fatalf("store = %+v", store.calls)
	}
}

func TestEventsRealBadAnnotationsStoreNothing(t *testing.T) {
	e, store, _ := realEventManager(t, "tidemail-plugin-badconf", eventManifest, DefaultTimeout)
	e.SetEnabled("p", true)
	e.Enqueue(MessageMetadata{ID: 42})
	if u := drain(t, e, 1)[0]; !u.Failed || u.Result.Outcome != AnnotationsRejected {
		t.Fatalf("update = %+v", u)
	}
	if len(store.calls) != 0 {
		t.Fatal("rejected annotations reached the store")
	}
}

func TestEventsRealMetadataOnlyPluginStoresNothing(t *testing.T) {
	e, store, _ := realEventManager(t, "tidemail-plugin-annotate", "events = [\"message.received\"]\n"+metadataPermission, DefaultTimeout)
	e.SetEnabled("p", true)
	e.Enqueue(MessageMetadata{ID: 42})
	if u := drain(t, e, 1)[0]; u.Failed || u.Result.Outcome != AnnotationsNotPermitted {
		t.Fatalf("update = %+v", u)
	}
	if len(store.calls) != 0 {
		t.Fatal("metadata-only plugin stored annotations")
	}
}

func TestEventsRealTimeoutsPausePlugin(t *testing.T) {
	e, _, _ := realEventManager(t, "tidemail-plugin-hang", eventManifest, 100*time.Millisecond)
	e.SetEnabled("p", true)
	for id := int64(1); id <= EventFailureLimit; id++ {
		e.Enqueue(MessageMetadata{ID: id})
		if u := drain(t, e, 1)[0]; !u.Failed {
			t.Fatalf("update = %+v", u)
		}
	}
	if st := e.Status()["p"]; !st.Paused || st.LastError == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestEventsOnlyEventCapablePluginsGetWorkers(t *testing.T) {
	root := t.TempDir()
	installPlugin(t, root, "evt", "evt", "tidemail-plugin-annotate", eventManifest)
	installPlugin(t, root, "noevt", "noevt", "tidemail-plugin-annotate", bothPermissions)
	installPlugin(t, root, "nometa", "nometa", "tidemail-plugin-annotate", "events = [\"message.received\"]\n")
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEventManager(context.Background(), m, &recordingStore{}, EventOptions{})
	defer e.Close(time.Second)
	if ids := e.PluginIDs(); len(ids) != 1 || ids[0] != "evt" {
		t.Fatalf("event plugins = %v", ids)
	}
	// Enabling an ineligible plugin is a no-op.
	e.SetEnabled("noevt", true)
	if _, ok := e.Status()["noevt"]; ok {
		t.Fatal("ineligible plugin tracked")
	}
	// The event method itself refuses a plugin that did not declare it.
	if _, err := m.messageEvent(context.Background(), "noevt", MessageMetadata{ID: 1}, nil); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.Call(context.Background(), "evt", EventMessageReceived, nil); err == nil {
		t.Fatal("Call must refuse message.received")
	}
}

func TestEventsCloseKillsRunningProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix signals to check the process")
	}
	e, _, dir := realEventManager(t, "tidemail-plugin-hangpid", eventManifest, time.Minute)
	e.SetEnabled("p", true)
	e.Enqueue(MessageMetadata{ID: 1})
	var pid int
	waitFor(t, "plugin to start", func() bool {
		data, err := os.ReadFile(filepath.Join(dir, "PID"))
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(string(data))
		return err == nil
	})
	start := time.Now()
	if !e.Close(5 * time.Second) {
		t.Fatal("workers did not exit")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("close took %v", elapsed)
	}
	waitFor(t, "plugin process to exit", func() bool {
		return syscall.Kill(pid, 0) != nil
	})
}
