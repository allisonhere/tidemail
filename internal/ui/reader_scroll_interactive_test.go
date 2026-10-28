package ui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type readerTerminalStats struct{ keys, wheels int }
type readerTerminalProbe struct {
	m Model
	s *readerTerminalStats
}

func (p readerTerminalProbe) Init() tea.Cmd { return nil }
func (p readerTerminalProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.Type == tea.KeyCtrlC {
			return p, tea.Quit
		}
		if keyMatches(msg, p.m.keys.Up, p.m.keys.Down) {
			p.s.keys++
		}
	case tea.MouseMsg:
		if tea.MouseEvent(msg).IsWheel() {
			p.s.wheels++
		}
	}
	next, cmd := p.m.Update(msg)
	p.m = next.(Model)
	return p, cmd
}
func (p readerTerminalProbe) View() string { return p.m.View() }

// Opt-in terminal check with real input/output and isolated synthetic mail.
// No user configuration, mail account or database is opened by this fixture.
func TestReaderScrollInteractive(t *testing.T) {
	if os.Getenv("TIDEMAIL_READER_INTERACTIVE") != "1" {
		t.Skip("interactive terminal required; enable TIDEMAIL_READER_INTERACTIVE")
	}
	requests, _ := recordReaderActivations(t)
	stats := &readerTerminalStats{}
	program := tea.NewProgram(readerTerminalProbe{readerScrollModel(333*1024, 120, 40, true), stats}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithoutSignalHandler())
	final, err := program.Run()
	if err != nil {
		t.Fatal(err)
	}
	m := final.(readerTerminalProbe).m
	t.Logf("terminal keys=%d wheels=%d activations=%d offset=%d focus=%d", stats.keys, stats.wheels, len(*requests), m.viewport.YOffset, m.contentFocusLine)
}
