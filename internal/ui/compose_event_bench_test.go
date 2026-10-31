package ui

import (
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func preserveComposeEditorState(tb testing.TB) {
	vim, spell, gen, names := editorVimMode, editorSpell, editorSpellGen, editorSpellNames
	tb.Cleanup(func() { editorVimMode, editorSpell, editorSpellGen, editorSpellNames = vim, spell, gen, names })
}

func composeEventModel(width, height int, spell bool) Model {
	m, _ := newMailboxListModel(7, 20)
	m.width, m.height = width, height
	m.cfg.Display.ComposeSpell = spell
	setEditorSpell(spell, "en", nil)
	acfg := config.AccountConfig{ID: "test", User: "me@example.com"}
	m.cfg.Accounts = []config.AccountConfig{acfg}
	m.compose = NewCompose(acfg, m.cfg.Accounts, nil)
	m.compose.bodyInput.SetValue(strings.Repeat("A paragraph of words to edit and wrap naturally. ", 1000))
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()
	m.overlay = overlayCompose
	return m
}

// This separates actual full-model event costs rather than only the editor.
func BenchmarkComposeEventPath(b *testing.B) {
	preserveComposeEditorState(b)
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	b.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	for _, phase := range []string{"update", "editor-view", "compose-view", "full-view", "full-event"} {
		b.Run(phase, func(b *testing.B) {
			m := composeEventModel(160, 50, false)
			key := tea.KeyMsg{Type: tea.KeyBackspace}
			// Warm the normal model geometry and rendering paths.
			next, _ := m.Update(key)
			m = next.(Model)
			_ = m.View()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				switch phase {
				case "update":
					next, _ := m.Update(key)
					_ = next
				case "editor-view":
					_ = m.compose.bodyInput.View()
				case "compose-view":
					_ = m.compose.View(composeOverlayWidth(m.width), composeOverlayHeight(m.height), m.styles)
				case "full-view":
					_ = m.View()
				case "full-event":
					next, _ := m.Update(key)
					_ = next.(Model).View()
				}
			}
		})
	}
}

type composeEventStats struct {
	keys, results, views int
	updateTime, viewTime time.Duration
	lastKey              time.Time
	lastKeyView          time.Time
	saveErr              error
}

type composeEventProbe struct {
	model    Model
	stats    *composeEventStats
	count    int
	stopping bool
}

func (p composeEventProbe) Init() tea.Cmd { return nil }
func (p composeEventProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "~" {
		p.stopping = true
		if p.stats.results == p.count {
			return p, tea.Quit
		}
		return p, nil
	}
	start := time.Now()
	next, cmd := p.model.Update(msg)
	p.model = next.(Model)
	p.stats.updateTime += time.Since(start)
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyBackspace {
		p.stats.keys++
		if p.stats.keys == p.count {
			p.stats.lastKey = time.Now()
		}
	}
	if saved, ok := msg.(DraftSavedMsg); ok {
		p.stats.results++
		if saved.Err != nil {
			p.stats.saveErr = saved.Err
		}
	}
	if p.stopping && p.stats.results == p.count {
		return p, tea.Batch(cmd, tea.Quit)
	}
	return p, cmd
}
func (p composeEventProbe) View() string {
	start := time.Now()
	view := p.model.View()
	p.stats.viewTime += time.Since(start)
	p.stats.views++
	if p.stats.keys == p.count && p.stats.lastKeyView.IsZero() {
		p.stats.lastKeyView = time.Now()
	}
	return view
}

type composeKeyInput struct{ bytes chan byte }

func (r composeKeyInput) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	ch, ok := <-r.bytes
	if !ok {
		return 0, io.EOF
	}
	buf[0] = ch
	return 1, nil
}

// Diagnostic regression: real Bubble Tea input decoding, event loop, commands and
// complete TideMail views. The buffered input models terminal bytes arriving
// independently of processing; no keys are discarded or coalesced.
func TestComposeBackspaceQueueDrain(t *testing.T) {
	preserveComposeEditorState(t)
	if testing.Short() {
		t.Skip("event-loop diagnostic")
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	for _, scenario := range []struct{ spell, database bool }{{false, false}, {true, false}, {false, true}} {
		t.Run(fmt.Sprintf("spell=%v/database=%v", scenario.spell, scenario.database), func(t *testing.T) {
			const count = 60
			m := composeEventModel(160, 50, scenario.spell)
			if scenario.database {
				t.Setenv("XDG_DATA_HOME", t.TempDir())
				database, err := db.Open()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = database.Close() })
				m.db = database
			}
			original := len([]rune(m.compose.bodyInput.Value()))
			stats := &composeEventStats{}
			input := composeKeyInput{bytes: make(chan byte, count+1)}
			released := make(chan time.Time, 1)
			go func() {
				start := time.Now()
				for i := 0; i < count; i++ {
					time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / 30)))
					input.bytes <- 127
				}
				released <- time.Now()
				input.bytes <- '~'
				close(input.bytes)
			}()
			program := tea.NewProgram(composeEventProbe{model: m, stats: stats, count: count}, tea.WithInput(input), tea.WithOutput(io.Discard), tea.WithoutSignalHandler())
			start := time.Now()
			final, err := program.Run()
			elapsed := time.Since(start)
			release := <-released
			if err != nil {
				t.Fatal(err)
			}
			probe := final.(composeEventProbe)
			if stats.keys != count || len([]rune(probe.model.compose.bodyInput.Value())) != original-count {
				t.Fatalf("lost input: %+v", stats)
			}
			if stats.saveErr != nil {
				t.Fatalf("draft save failed: %v", stats.saveErr)
			}
			drain := max(time.Duration(0), stats.lastKey.Sub(release))
			t.Logf("keys=%d results=%d views=%d elapsed=%s post-release-drain=%s last-key-view-ready=%s update-total=%s view-total=%s", stats.keys, stats.results, stats.views, elapsed, drain, max(time.Duration(0), stats.lastKeyView.Sub(release)), stats.updateTime, stats.viewTime)
		})
	}
}
