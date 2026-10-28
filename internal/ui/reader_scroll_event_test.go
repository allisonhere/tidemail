package ui

import (
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func readerScrollModel(size, width, height int, focus bool) Model {
	m, _ := newMailboxListModel(7, 40)
	m.cfg.Display.MouseSupport = true
	m.cfg.Display.FocusLine = focus
	m.cfg.Display.ShowHeaders = true
	m.cfg.Display.ThreadedConversations = false
	m.messages[0].BodyHTML = benchHTMLBody(size)
	m.messages[0].From = "Sender <sender@example.com>"
	m.messages[0].To = "reader@example.com"
	m.applyFilter()
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(Model)
	m.focused = paneContent
	m.contentShowHeaders = true
	m.setViewportForCurrentRow()
	_ = m.View()
	return m
}

// Complete packets exercise real decoding without splitting an escape sequence
// at a synthetic Read boundary. The buffered producer never waits for Update.
type readerEventInput struct {
	packets chan []byte
	pending []byte
}

func (r *readerEventInput) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		var ok bool
		r.pending, ok = <-r.packets
		if !ok {
			return 0, io.EOF
		}
	}
	n := copy(buf, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

type readerScrollState struct {
	offset, focus, link int
	pending             bool
}

func scrollState(m Model) readerScrollState {
	return readerScrollState{m.viewport.YOffset, m.contentFocusLine, m.contentLinkIdx, m.contentFocusPending}
}

type readerEventStats struct {
	sent                   []time.Time
	updates, views, cycles []time.Duration
	lags                   []time.Duration
	states                 []readerScrollState
	finalUpdate, finalView time.Time
}

type readerEventProbe struct {
	m        Model
	s        *readerEventStats
	uncached bool // test-only matched baseline; production behavior is unchanged
}

func (p readerEventProbe) Init() tea.Cmd { return nil }
func (p readerEventProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "~" {
		return p, tea.Quit
	}
	tracked := false
	switch msg := msg.(type) {
	case tea.KeyMsg:
		tracked = msg.Type == tea.KeyDown || msg.Type == tea.KeyUp
	case tea.MouseMsg:
		tracked = tea.MouseEvent(msg).IsWheel()
	}
	start := time.Now()
	next, cmd := p.m.Update(msg)
	p.m = next.(Model)
	if p.uncached && p.m.readerPanes != nil {
		p.m.readerPanes.valid = false
	}
	if tracked {
		p.s.updates = append(p.s.updates, time.Since(start))
		p.s.states = append(p.s.states, scrollState(p.m))
		p.s.finalUpdate = time.Now()
	}
	return p, cmd
}

func (p readerEventProbe) View() string {
	start := time.Now()
	view := p.m.View()
	if len(p.s.updates) > len(p.s.views) {
		duration := time.Since(start)
		p.s.views = append(p.s.views, duration)
		i := len(p.s.views) - 1
		p.s.cycles = append(p.s.cycles, p.s.updates[i]+duration)
		p.s.finalView = time.Now()
		p.s.lags = append(p.s.lags, p.s.finalView.Sub(p.s.sent[i]))
	}
	return view
}

func readerPercentile(samples []time.Duration, percentile float64) time.Duration {
	ordered := slices.Clone(samples)
	slices.Sort(ordered)
	return ordered[max(0, int(math.Ceil(float64(len(ordered))*percentile))-1)]
}

func readerLatencySummary(samples []time.Duration) string {
	var total time.Duration
	for _, sample := range samples {
		total += sample
	}
	return fmt.Sprintf("mean=%s p95=%s p99=%s", total/time.Duration(len(samples)), readerPercentile(samples, .95), readerPercentile(samples, .99))
}

// Like the composer diagnostic, this includes decoding, Update, commands, full
// views and the standard renderer. Discarded output is not a smoothness test.
func TestReaderScrollEventQueueDrain(t *testing.T) {
	if testing.Short() {
		t.Skip("real-time input/render diagnostic")
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	for _, tc := range []struct {
		size, width, height, rate int
		wheel, focus, up          bool
	}{
		{21 * 1024, 120, 40, 30, false, true, false},
		{21 * 1024, 120, 40, 30, true, true, false},
		{21 * 1024, 120, 40, 120, true, true, false},
		{333 * 1024, 160, 50, 30, false, true, false},
		{333 * 1024, 160, 50, 30, false, true, true},
		{333 * 1024, 160, 50, 30, true, true, true},
		{333 * 1024, 160, 50, 120, true, true, false},
		{333 * 1024, 80, 24, 30, false, true, false},
		{333 * 1024, 80, 24, 120, true, true, false},
		{333 * 1024, 160, 50, 30, false, false, false},
		{333 * 1024, 160, 50, 120, true, false, false},
	} {
		name := fmt.Sprintf("bytes=%d/%dx%d/wheel=%v/focus=%v/up=%v/hz=%d", tc.size, tc.width, tc.height, tc.wheel, tc.focus, tc.up, tc.rate)
		t.Run(name, func(t *testing.T) {
			const count = 60
			m := readerScrollModel(tc.size, tc.width, tc.height, tc.focus)
			if tc.up {
				m.contentFocusLine = len(m.contentLines) - 1
				m.viewport.GotoBottom()
			}
			key, packet := tea.KeyDown, []byte("\x1b[B")
			button, code := tea.MouseButtonWheelDown, 65
			if tc.up {
				key, packet = tea.KeyUp, []byte("\x1b[A")
				button, code = tea.MouseButtonWheelUp, 64
			}
			var event tea.Msg = tea.KeyMsg{Type: key}
			if tc.wheel {
				x, y := m.feedsPaneWidth()+5, m.articlesPaneOuterHeight()+2
				event = tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
				packet = []byte(fmt.Sprintf("\x1b[<%d;%d;%dM", code, x+1, y+1))
			}
			// Verify every transition against a sequential run, not just a final
			// position that could conceal discarded input at a document boundary.
			reference := m
			var expected []readerScrollState
			for i := 0; i < count; i++ {
				next, _ := reference.Update(event)
				reference = next.(Model)
				expected = append(expected, scrollState(reference))
			}
			stats := &readerEventStats{sent: make([]time.Time, count)}
			input := &readerEventInput{packets: make(chan []byte, count+1)}
			released := make(chan time.Time, 1)
			go func() {
				start := time.Now()
				for i := 0; i < count; i++ {
					time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / time.Duration(tc.rate))))
					stats.sent[i] = time.Now()
					input.packets <- packet
				}
				released <- time.Now()
				input.packets <- []byte("~")
				close(input.packets)
			}()
			program := tea.NewProgram(readerEventProbe{m: m, s: stats, uncached: os.Getenv("TIDEMAIL_READER_NO_PANE_REUSE") == "1"}, tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
			start := time.Now()
			_, err := program.Run()
			elapsed, release := time.Since(start), <-released
			if err != nil {
				t.Fatal(err)
			}
			if len(stats.states) != count || len(stats.views) != count || !reflect.DeepEqual(stats.states, expected) {
				t.Fatalf("lost or changed input: delivered=%d processed=%d views=%d", count, len(stats.states), len(stats.views))
			}
			drain := max(time.Duration(0), stats.finalView.Sub(release))
			t.Logf("delivered=%d processed=%d elapsed=%s update-drain=%s view-drain=%s update[%s] view[%s] cycle[%s] arrival-to-view[%s]", count, len(stats.states), elapsed, max(time.Duration(0), stats.finalUpdate.Sub(release)), drain, readerLatencySummary(stats.updates), readerLatencySummary(stats.views), readerLatencySummary(stats.cycles), readerLatencySummary(stats.lags))
			// Opt in only on a controlled performance runner; ordinary CI still
			// enforces all event counts and navigation transitions without flakes.
			if tc.rate == 30 && os.Getenv("TIDEMAIL_READER_PERF_LIMITS") == "1" {
				if readerPercentile(stats.cycles, .95) >= 20*time.Millisecond || drain >= 50*time.Millisecond {
					t.Fatal("reader exceeded the 30 Hz responsiveness target")
				}
			}
		})
	}
}
