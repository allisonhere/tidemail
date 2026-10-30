//go:build linux || darwin

package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// These hooks are the only variant-specific part of the diagnostic. The
// baseline overlay leaves them empty; the animated revision starts its clock.
func prepareReadFeedbackPerformance(m *Model) {
	m.pendingReads = make(map[int64]pendingRead)
	for _, msg := range m.messages[:12] {
		m.pendingReads[msg.ID] = pendingRead{Token: 1, Read: msg.Read}
	}
}
func startReadFeedbackPerformance(*Model) tea.Cmd { return nil }

type readFeedbackPerfStats struct {
	sent                      []time.Time
	arrivals, rendered, views []time.Duration
	processed, viewCalls      int
	bytes                     atomic.Int64
}
type readFeedbackPerfOutput struct{ s *readFeedbackPerfStats }

func (w readFeedbackPerfOutput) Write(b []byte) (int, error) {
	w.s.bytes.Add(int64(len(b)))
	return len(b), nil
}

type readFeedbackPerfProbe struct {
	m Model
	s *readFeedbackPerfStats
}

func (p *readFeedbackPerfProbe) Init() tea.Cmd { return startReadFeedbackPerformance(&p.m) }
func (p *readFeedbackPerfProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "~" {
		return p, tea.Quit
	}
	tracked := false
	switch msg := msg.(type) {
	case tea.KeyMsg:
		tracked = msg.Type == tea.KeyDown
	case tea.MouseMsg:
		tracked = tea.MouseEvent(msg).IsWheel()
	}
	next, cmd := p.m.Update(msg)
	p.m = next.(Model)
	if tracked {
		p.s.arrivals = append(p.s.arrivals, time.Since(p.s.sent[p.s.processed]))
		p.s.processed++
	}
	return p, cmd
}
func (p *readFeedbackPerfProbe) View() string {
	start := time.Now()
	view := p.m.View()
	p.s.views = append(p.s.views, time.Since(start))
	for len(p.s.rendered) < p.s.processed {
		p.s.rendered = append(p.s.rendered, time.Since(p.s.sent[len(p.s.rendered)]))
	}
	p.s.viewCalls++
	return view
}
func readFeedbackCPU() time.Duration {
	var r syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
	return time.Duration(r.Utime.Sec+r.Stime.Sec)*time.Second + time.Duration(r.Utime.Usec+r.Stime.Usec)*time.Microsecond
}

func TestPendingReadPerformance(t *testing.T) {
	if os.Getenv("TIDEMAIL_PENDING_PERF") != "1" {
		t.Skip("opt-in paced rendering/CPU diagnostic")
	}
	seconds := 3
	if v, err := strconv.Atoi(os.Getenv("TIDEMAIL_PENDING_PERF_SECONDS")); err == nil && v > 0 {
		seconds = v
	}
	for _, html := range []bool{false, true} {
		for _, mode := range []string{"idle", "keyboard", "wheel"} {
			t.Run(fmt.Sprintf("%s/html=%v", mode, html), func(t *testing.T) {
				events := seconds * 30
				c := listPerfCase{Width: 160, Height: 50, Events: events, HTML: html, Unified: true}
				m := listPerfModel(t, c)
				m.initialLoading = false
				prepareReadFeedbackPerformance(&m)
				stats := &readFeedbackPerfStats{sent: make([]time.Time, events)}
				input := &listPerfInput{packets: make(chan []byte, events+1)}
				// Precomputed timestamps avoid a producer/event-loop data race.
				began := time.Now().Add(20 * time.Millisecond)
				for i := range stats.sent {
					stats.sent[i] = began.Add(time.Duration(i) * time.Second / 30)
				}
				go func() {
					if mode != "idle" {
						for i := 0; i < events; i++ {
							time.Sleep(time.Until(stats.sent[i]))
							packet := []byte("\x1b[B")
							if mode == "wheel" {
								packet = []byte(fmt.Sprintf("\x1b[<65;%d;3M", m.feedsPaneWidth()+5))
							}
							input.packets <- packet
						}
					}
					time.Sleep(time.Until(began.Add(time.Duration(seconds) * time.Second)))
					input.packets <- []byte("~")
					close(input.packets)
				}()
				runtime.GC()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				cpu := readFeedbackCPU()
				start := time.Now()
				final, err := tea.NewProgram(&readFeedbackPerfProbe{m, stats}, tea.WithInput(input), tea.WithOutput(readFeedbackPerfOutput{stats}), tea.WithoutSignalHandler()).Run()
				elapsed := time.Since(start)
				cpu = readFeedbackCPU() - cpu
				runtime.ReadMemStats(&after)
				if err != nil {
					t.Fatal(err)
				}
				want := events
				if mode == "idle" {
					want = 0
				}
				if stats.processed != want {
					t.Fatalf("lost input: %d/%d", stats.processed, want)
				}
				step := 1
				if mode == "wheel" {
					step = wheelStepRows
				}
				if final.(*readFeedbackPerfProbe).m.messageCursor != want*step {
					t.Fatal("incorrect final cursor")
				}
				var sum time.Duration
				for _, v := range stats.views {
					sum += v
				}
				result := map[string]any{"mode": mode, "html": html, "pending": 12, "seconds": elapsed.Seconds(), "cpu_ms": float64(cpu) / 1e6, "cpu_percent_one_core": 100 * float64(cpu) / float64(elapsed), "views": stats.viewCalls, "view_ms_total": float64(sum) / 1e6, "view": listPerfLatency(stats.views), "input_events": stats.processed, "terminal_bytes": stats.bytes.Load(), "allocated_bytes": after.TotalAlloc - before.TotalAlloc}
				if len(stats.arrivals) > 0 {
					result["input_to_update"] = listPerfLatency(stats.arrivals)
					result["input_to_view"] = listPerfLatency(stats.rendered)
				}
				encoded, _ := json.Marshal(result)
				t.Logf("PENDING_PERF %s", encoded)
			})
		}
	}
}

func BenchmarkPendingReadView(b *testing.B) {
	m := listPerfModel(b, listPerfCase{Width: 160, Height: 50, HTML: true, Unified: true, Events: 90})
	m.initialLoading = false
	prepareReadFeedbackPerformance(&m)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runtime.KeepAlive(m.View())
	}
}
