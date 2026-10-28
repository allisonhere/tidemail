package ui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestRenderHelpersPreserveModelValueState(t *testing.T) {
	for _, theme := range []Theme{CatppuccinMocha, VT52} {
		for _, width := range []int{80, 160} {
			for _, focus := range []pane{paneAccounts, paneMessages, paneContent} {
				t.Run(fmt.Sprintf("%s/%d/%d", theme.Name, width, focus), func(t *testing.T) {
					m := readerLinksModel(t, `<p>Ordinary body.</p><a href="https://example.com/action">Action</a>`, "")
					next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
					m = next.(Model)
					m.setStyles(BuildStyles(theme, "comfortable", "square"))
					m.setViewportForCurrentRow()
					m.selectContentLinkAt(m.contentLinkRows[0])
					m.revealContentLink()
					m.focused = focus
					for _, searching := range []bool{false, true} {
						if searching {
							m.overlay = overlayContentSearch
							m.contentSearchQuery = "Ordinary"
							m.contentSearchMatches = []int{m.contentFocusLine}
						}
						// Snapshot display and navigation state, excluding runtime callbacks
						// (which are not DeepEqual-comparable) and pointer-owned render caches.
						snapshot := func() []any {
							return []any{m.cfg, m.styles, m.viewport, m.focused, m.messageCursor, m.listOffset, m.sidebarCursor, m.width, m.height, m.overlay, m.contentFocusLine, m.contentFocusPending, m.contentLinkIdx, m.contentMessageID, m.contentDraftID, m.contentSearchInput.Value(), slices.Clone(m.contentLines), slices.Clone(m.contentLinks), slices.Clone(m.contentLinkRows), slices.Clone(m.contentSearchMatches)}
						}
						before := snapshot()
						first := m.renderAccountsPane() + m.renderMessagesPane() + m.renderContentPane() + m.renderStatusBar() + m.View()
						second := m.renderAccountsPane() + m.renderMessagesPane() + m.renderContentPane() + m.renderStatusBar() + m.View()
						if first != second {
							t.Fatal("read-only helpers changed their rendered output")
						}
						if !reflect.DeepEqual(before, snapshot()) {
							t.Fatal("rendering changed Model value state")
						}
					}
				})
			}
		}
	}
}

type listPerfCase struct {
	Name                           string
	Width, Height, Rate, Events    int
	HTML, Wheel, Unified, Up, Warm bool
}

func listPerfModel(tb testing.TB, c listPerfCase) Model {
	tb.Helper()
	tb.Setenv("XDG_DATA_HOME", tb.TempDir())
	tb.Setenv("XDG_CONFIG_HOME", tb.TempDir())
	tb.Setenv("TIDEMAIL_IMAGE_PROTOCOL", "none")
	database, err := db.Open()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { database.Close() })
	cfg := config.DefaultConfig()
	cfg.Display.MouseSupport = true
	cfg.Display.ThreadedConversations = false
	cfg.Display.MarkReadOnFocus = false
	cfg.Display.ShowHeaders = true
	cfg.Display.ShowSender = true
	cfg.Display.ActionableLinks = true
	m := NewModel(database, cfg, "performance-test", false)
	m.accounts = []db.Account{{ID: 1, Name: "Personal", Color: "#78a9ff"}, {ID: 2, Name: "Work", Color: "#ff9e64"}}
	m.mailboxes = []db.Mailbox{{ID: 1, AccountID: 1, Name: "INBOX"}, {ID: 2, AccountID: 2, Name: "INBOX"}}
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if c.Unified && row.kind == rowKindUnified || !c.Unified && row.kind == rowKindMailbox && row.mailboxID == 1 {
			m.sidebarCursor = i
			break
		}
	}
	body := ""
	if c.HTML {
		body = benchHTMLBody(21 * 1024)
	}
	for i := 0; i < max(80, 3*c.Events+1); i++ {
		msg := benchMessage(int64(i+1), body)
		msg.Subject = fmt.Sprintf("Nav %03d", i)
		msg.Date = time.Unix(1700000000-int64(i), 0)
		msg.Read = true
		msg.BodyText = "Hello reader,\n\nA short plain-text message.\nRegards."
		msg.MailboxID = 1
		if c.Unified {
			msg.MailboxID = int64(i%2 + 1)
		}
		m.messages = append(m.messages, msg)
	}
	m.applyFilter()
	next, _ := m.Update(tea.WindowSizeMsg{Width: c.Width, Height: c.Height})
	m = next.(Model)
	m.focused = paneMessages
	m.contentShowHeaders = true
	if c.Up {
		m.messageCursor = c.Events
		if c.Wheel {
			m.messageCursor *= wheelStepRows
		}
		m.listOffset = max(0, m.messageCursor-m.articleRowsVisible()+1)
	}
	if c.Warm {
		start, offset := m.messageCursor, m.listOffset
		for i := 0; i <= start; i++ {
			m.messageCursor = i
			m.setViewportForCurrentRow()
		}
		m.messageCursor, m.listOffset = start, offset
	}
	m.setViewportForCurrentRow()
	_ = m.View()
	return m
}

// Fixed, warmed Views isolate allocation/copying cost from fixture construction
// and cold body conversion. Use -benchmem -benchtime=30x for matched comparisons.
func BenchmarkMessageListView(b *testing.B) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	for _, size := range [][2]int{{80, 24}, {160, 50}} {
		b.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(b *testing.B) {
			m := listPerfModel(b, listPerfCase{Width: size[0], Height: size[1], Events: 60})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runtime.KeepAlive(m.View())
			}
			b.StopTimer()
			b.ReportMetric(float64(unsafe.Sizeof(m)), "model-bytes")
		})
	}
}

// Whole escape sequences preserve the decoder's packet semantics. The producer
// is buffered and independent of Update, so slow handling accumulates real input.
type listPerfInput struct {
	packets chan []byte
	pending []byte
}

func (r *listPerfInput) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		v, ok := <-r.packets
		if !ok {
			return 0, io.EOF
		}
		r.pending = v
	}
	n := copy(b, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

type listPerfStats struct {
	mu                              sync.Mutex // producer timestamps and renderer writes cross goroutines
	sent                            []time.Time
	terminal                        time.Time
	finalTitle                      string
	updates, views, cycles, arrival []time.Duration // event-loop-owned
	positions                       []int
	finalView                       time.Time
}
type listPerfOutput struct{ s *listPerfStats }

func (w listPerfOutput) Write(b []byte) (int, error) {
	now := time.Now()
	text := ansi.Strip(string(b))
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "→") && strings.Contains(line, w.s.finalTitle) {
			w.s.terminal = now
		}
	}
	return len(b), nil
}

type listPerfProbe struct {
	m Model
	s *listPerfStats
}

func (p listPerfProbe) Init() tea.Cmd { return nil }
func (p listPerfProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "~" {
		return p, tea.Quit
	}
	tracked := false
	switch msg := msg.(type) {
	case tea.KeyMsg:
		tracked = msg.Type == tea.KeyUp || msg.Type == tea.KeyDown
	case tea.MouseMsg:
		tracked = tea.MouseEvent(msg).IsWheel()
	}
	start := time.Now()
	next, cmd := p.m.Update(msg)
	p.m = next.(Model)
	if tracked {
		p.s.updates = append(p.s.updates, time.Since(start))
		p.s.positions = append(p.s.positions, p.m.messageCursor)
	}
	return p, cmd
}
func (p listPerfProbe) View() string {
	start := time.Now()
	view := p.m.View()
	end := time.Now()
	if len(p.s.views) < len(p.s.updates) {
		i := len(p.s.views)
		duration := end.Sub(start)
		p.s.views = append(p.s.views, duration)
		p.s.cycles = append(p.s.cycles, p.s.updates[i]+duration)
		p.s.mu.Lock()
		sent := p.s.sent[i]
		p.s.mu.Unlock()
		p.s.arrival = append(p.s.arrival, end.Sub(sent))
		p.s.finalView = end
	}
	return view
}
func listPerfLatency(v []time.Duration) map[string]float64 {
	sorted := append([]time.Duration(nil), v...)
	slices.Sort(sorted)
	var sum time.Duration
	for _, x := range v {
		sum += x
	}
	percentile := func(p float64) float64 {
		return float64(sorted[max(0, int(math.Ceil(float64(len(sorted))*p))-1)]) / 1e6
	}
	return map[string]float64{"mean": float64(sum) / float64(len(v)) / 1e6, "p95": percentile(.95), "p99": percentile(.99)}
}

// This paced diagnostic is opt-in: ordinary CI should verify correctness without
// adding cold HTML timing runs or imposing machine-specific performance gates.
func TestMessageListNavigationEventQueueDrain(t *testing.T) {
	if testing.Short() || os.Getenv("TIDEMAIL_LIST_PERF") != "1" {
		t.Skip("enable TIDEMAIL_LIST_PERF=1 for real-event latency diagnostics")
	}
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)
	cases := []listPerfCase{}
	for _, html := range []bool{false, true} {
		for _, width := range []int{80, 160} {
			for _, rate := range []int{30, 120} {
				kind := "plain"
				count := 60
				if html {
					kind = "html21KB"
					count = 30
				}
				height := 24
				if width == 160 {
					height = 50
				}
				cases = append(cases, listPerfCase{Name: fmt.Sprintf("%s-%dx%d-key%d", kind, width, height, rate), Width: width, Height: height, Rate: rate, Events: count, HTML: html, Unified: rate == 120})
			}
		}
	}
	cases = append(cases, listPerfCase{Name: "plain-wide-wheel120", Width: 160, Height: 50, Rate: 120, Events: 60, Wheel: true, Unified: true}, listPerfCase{Name: "html-warm-up120", Width: 160, Height: 50, Rate: 120, Events: 12, HTML: true, Up: true, Warm: true})
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			m := listPerfModel(t, c)
			startCursor := m.messageCursor
			step := 1
			if c.Wheel {
				step = wheelStepRows
			}
			if c.Up {
				step = -step
			}
			want := startCursor + c.Events*step
			stats := &listPerfStats{sent: make([]time.Time, c.Events), finalTitle: fmt.Sprintf("Nav %03d", want)}
			packet := []byte("\x1b[B")
			if c.Up {
				packet = []byte("\x1b[A")
			}
			if c.Wheel {
				code := 65
				if c.Up {
					code = 64
				}
				packet = []byte(fmt.Sprintf("\x1b[<%d;%d;3M", code, m.feedsPaneWidth()+5))
			}
			input := &listPerfInput{packets: make(chan []byte, c.Events+1)}
			released := make(chan time.Time, 1)
			go func() {
				start := time.Now()
				for i := 0; i < c.Events; i++ {
					time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / time.Duration(c.Rate))))
					stats.mu.Lock()
					stats.sent[i] = time.Now()
					stats.mu.Unlock()
					input.packets <- packet
				}
				released <- time.Now()
				input.packets <- []byte("~")
				close(input.packets)
			}()
			began := time.Now()
			program := tea.NewProgram(listPerfProbe{m, stats}, tea.WithInput(input), tea.WithOutput(listPerfOutput{stats}), tea.WithoutSignalHandler())
			final, err := program.Run()
			elapsed := time.Since(began)
			release := <-released
			if err != nil {
				t.Fatal(err)
			}
			if len(stats.positions) != c.Events || len(stats.views) != c.Events {
				t.Fatalf("lost events: processed=%d views=%d want=%d", len(stats.positions), len(stats.views), c.Events)
			}
			for i, got := range stats.positions {
				if got != startCursor+(i+1)*step {
					t.Fatalf("event %d cursor=%d", i, got)
				}
			}
			end := final.(listPerfProbe).m
			if end.messageCursor != want || end.currentRowMessage().ID != end.contentMessageID {
				t.Fatal("final selection/preview identity mismatch")
			}
			stats.mu.Lock()
			terminal := stats.terminal
			stats.mu.Unlock()
			if terminal.IsZero() {
				t.Fatal("renderer did not write the final selected row")
			}
			result := map[string]any{"case": c, "processed": len(stats.positions), "views": len(stats.views), "update": listPerfLatency(stats.updates), "view": listPerfLatency(stats.views), "cycle": listPerfLatency(stats.cycles), "arrival_to_view": listPerfLatency(stats.arrival), "selection_drain_ms": float64(max(time.Duration(0), stats.finalView.Sub(release))) / 1e6, "terminal_drain_ms": float64(max(time.Duration(0), terminal.Sub(release))) / 1e6, "elapsed_ms": float64(elapsed) / 1e6, "frame_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(end.View())))}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("LIST_PERF %s", encoded)
		})
	}
}
