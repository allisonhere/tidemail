package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func selectionPerfModel(focus bool) Model {
	m := readerScrollModel(333*1024, 160, 50, focus)
	x, y, _, _ := m.readerMouseBounds()
	next, _ := m.Update(tea.MouseMsg{X: x + 2, Y: y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(Model)
	next, _ = m.Update(tea.MouseMsg{X: x + 25, Y: y + 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	m = next.(Model)
	_ = m.View()
	return m
}

func BenchmarkReaderSelectionStages(b *testing.B) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	for _, focus := range []bool{false, true} {
		m := selectionPerfModel(focus)
		x, y, _, _ := m.readerMouseBounds()
		motion := tea.MouseMsg{X: x + 30, Y: y + 6, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
		body := m.viewport.View()
		accounts, messages := m.renderAccountsPane(), m.renderMessagesPane()
		content := m.renderContentPane()
		main := lipgloss.JoinHorizontal(lipgloss.Top, accounts, lipgloss.JoinVertical(lipgloss.Left, messages, content))
		screen := lipgloss.JoinVertical(lipgloss.Left, main, m.renderStatusBar())
		cases := []struct {
			name string
			run  func()
		}{
			{"update", func() { next, _ := m.Update(motion); m = next.(Model) }},
			{"selection-state", func() { m.readerSelectionMouse(motion) }},
			{"coordinate", func() { m.readerMousePoint(x+30, y+6) }},
			{"highlight", func() { m.renderReaderMouseSelection(body) }},
			{"viewport", func() { m.viewport.View() }},
			{"content-pane", func() { m.renderContentPane() }},
			{"sidebar", func() { m.renderAccountsPane() }},
			{"message-list", func() { m.renderMessagesPane() }},
			{"composition", func() {
				lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, accounts, lipgloss.JoinVertical(lipgloss.Left, messages, content)), m.renderStatusBar())
			}},
			{"final-screen", func() {
				view := clampView(screen, m.width, m.height, m.styles.Theme.Bg)
				lipgloss.NewStyle().Width(m.width).Height(m.height).Background(m.styles.Theme.Bg).Render(view)
			}},
			{"view", func() { m.View() }},
		}
		for _, tc := range cases {
			b.Run(fmt.Sprintf("focus=%v/%s", focus, tc.name), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					tc.run()
				}
			})
		}
	}
}

type selectionPerfStats struct {
	sent                 []time.Time
	updates, views, lags []time.Duration
	processed            int
	unexpected           []string
	clears               []string
	release              time.Time
	last                 atomic.Int64
	mu                   sync.Mutex
	output               []time.Time
}
type selectionPerfOutput struct{ s *selectionPerfStats }

func (w selectionPerfOutput) Write(p []byte) (int, error) {
	i := int(w.s.last.Load()) - 1
	if i >= 0 {
		w.s.mu.Lock()
		if w.s.output[i].IsZero() {
			w.s.output[i] = time.Now()
		}
		w.s.mu.Unlock()
	}
	return len(p), nil
}

type selectionPerfProbe struct {
	m Model
	s *selectionPerfStats
}

func (p selectionPerfProbe) Init() tea.Cmd { return nil }
func (p selectionPerfProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "~" {
		return p, tea.Quit
	}
	start := time.Now()
	before := p.m.readerMouse
	next, cmd := p.m.Update(msg)
	p.m = next.(Model)
	if mouse, ok := msg.(tea.MouseMsg); ok {
		p.s.updates = append(p.s.updates, time.Since(start))
		p.s.processed++
		if mouse.Action == tea.MouseActionRelease {
			p.s.release = time.Now()
		}
	} else if k, ok := msg.(tea.KeyMsg); ok {
		p.s.unexpected = append(p.s.unexpected, fmt.Sprintf("%q", k.String()))
	}
	if before.moved && !p.m.readerMouse.moved {
		p.s.clears = append(p.s.clears, fmt.Sprintf("%T:%v", msg, msg))
	}
	return p, cmd
}
func (p selectionPerfProbe) View() string {
	start := time.Now()
	v := p.m.View()
	if p.s.processed > len(p.s.views) {
		i := p.s.processed - 1
		p.s.views = append(p.s.views, time.Since(start))
		if i < len(p.s.sent) {
			p.s.lags = append(p.s.lags, time.Since(p.s.sent[i]))
		}
		p.s.last.Store(int64(p.s.processed))
	}
	return v
}

// Opt in: real Bubble Tea decoding, Update, View and standard renderer; output
// completion is measured at a discard writer, not at a terminal's pixels.
func TestReaderSelectionEventLatency(t *testing.T) {
	if os.Getenv("TIDEMAIL_SELECTION_PERF") != "1" {
		t.Skip("opt-in diagnostic")
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	for _, focus := range []bool{false, true} {
		for _, rate := range []int{60, 120, 0} {
			t.Run(fmt.Sprintf("focus=%v/hz=%d", focus, rate), func(t *testing.T) {
				const count = 120
				m := selectionPerfModel(focus)
				m.clearReaderMouseSelection()
				x, y, _, _ := m.readerMouseBounds()
				s := &selectionPerfStats{sent: make([]time.Time, count+2), output: make([]time.Time, count+2)}
				input := &readerEventInput{packets: make(chan []byte, count+4)}
				packets := make([][]byte, count+2)
				packets[0] = []byte(fmt.Sprintf("\x1b[<0;%d;%dM", x+3, y+4))
				for i := 1; i <= count; i++ {
					packets[i] = []byte(fmt.Sprintf("\x1b[<32;%d;%dM", x+5+i%50, y+5+i%8))
				}
				packets[count+1] = []byte(fmt.Sprintf("\x1b[<0;%d;%dm", x+35, y+8))
				done := make(chan struct{})
				go func() {
					defer close(done)
					start := time.Now()
					for i, p := range packets {
						if rate > 0 {
							time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / time.Duration(rate))))
						}
						s.sent[i] = time.Now()
						input.packets <- p
					}
					input.packets <- []byte("~")
				}()
				program := tea.NewProgram(selectionPerfProbe{m, s}, tea.WithInput(input), tea.WithOutput(selectionPerfOutput{s}), tea.WithoutSignalHandler())
				final, err := program.Run()
				<-done
				if err != nil {
					t.Fatal(err)
				}
				result := final.(selectionPerfProbe).m
				backlog := 0
				for i, lag := range s.lags {
					n := 0
					end := s.sent[i].Add(lag)
					for j := i + 1; j < len(s.sent); j++ {
						if s.sent[j].Before(end) {
							n++
						}
					}
					backlog = max(backlog, n)
				}
				var outputLags []time.Duration
				for i, stamp := range s.output {
					if !stamp.IsZero() {
						outputLags = append(outputLags, stamp.Sub(s.sent[i]))
					}
				}
				outputSummary := "none"
				if len(outputLags) > 0 {
					outputSummary = readerLatencySummary(outputLags)
				}
				t.Logf("events=%d/%d update[%s] view[%s] input-to-view[%s] output-samples=%d input-to-output[%s] release=%s backlog=%d intact=%v unexpected=%v clears=%v", s.processed, len(packets), readerLatencySummary(s.updates), readerLatencySummary(s.views), readerLatencySummary(s.lags), len(outputLags), outputSummary, s.release.Sub(s.sent[len(packets)-1]), backlog, result.readerMouseText() != "" && !result.readerMouse.dragging, s.unexpected, s.clears)
				if s.processed != len(packets) || len(s.unexpected) > 0 || result.readerMouseText() == "" || result.readerMouse.dragging {
					t.Fatal("lost input or selection")
				}
			})
		}
	}
}

// Test different reader Read boundaries without logging in the hot path.
func TestReaderSelectionDecoderBoundaries(t *testing.T) {
	if os.Getenv("TIDEMAIL_SELECTION_PERF") != "1" {
		t.Skip("opt-in diagnostic")
	}
	for _, mode := range []string{"packets", "coalesced", "split"} {
		t.Run(mode, func(t *testing.T) {
			m := selectionPerfModel(false)
			m.clearReaderMouseSelection()
			x, y, _, _ := m.readerMouseBounds()
			packets := [][]byte{[]byte(fmt.Sprintf("\x1b[<0;%d;%dM", x+3, y+4))}
			for i := 0; i < 120; i++ {
				packets = append(packets, []byte(fmt.Sprintf("\x1b[<32;%d;%dM", x+5+i%50, y+5+i%8)))
			}
			packets = append(packets, []byte(fmt.Sprintf("\x1b[<0;%d;%dm", x+35, y+8)))
			chunks := packets
			if mode == "coalesced" {
				chunks = [][]byte{[]byte(strings.Join(byteStrings(packets), ""))}
			}
			if mode == "split" {
				chunks = nil
				for _, p := range packets {
					chunks = append(chunks, p[:5], p[5:])
				}
			}
			input := &readerEventInput{packets: make(chan []byte, len(chunks)+1)}
			for _, p := range chunks {
				input.packets <- p
			}
			input.packets <- []byte("~")
			s := &selectionPerfStats{sent: make([]time.Time, 1000), output: make([]time.Time, 1000)}
			final, err := tea.NewProgram(selectionPerfProbe{m, s}, tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutSignalHandler()).Run()
			if err != nil {
				t.Fatal(err)
			}
			result := final.(selectionPerfProbe).m
			t.Logf("mouse=%d expected=%d intact=%v unexpected-count=%d first-unexpected=%v clears=%v", s.processed, len(packets), result.readerMouseText() != "", len(s.unexpected), s.unexpected[:min(8, len(s.unexpected))], s.clears)
			if mode == "packets" && (s.processed != len(packets) || len(s.unexpected) != 0 || result.readerMouseText() == "") {
				t.Fatal("complete packets lost selection")
			}
		})
	}
}
func byteStrings(p [][]byte) []string {
	out := make([]string, len(p))
	for i, b := range p {
		out[i] = string(b)
	}
	return out
}

func BenchmarkReaderSelectionInvalidation(b *testing.B) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	for _, invalidate := range []bool{false, true} {
		m := selectionPerfModel(true)
		b.Run(fmt.Sprintf("display-change=%v", invalidate), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if invalidate {
					m.setStatus("background display refresh", false)
				}
				_ = m.View()
			}
		})
	}
}
