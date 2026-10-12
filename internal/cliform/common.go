package cliform

import (
	"fmt"
	"os"
	"strings"

	"github.com/allisonhere/tideui"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

var ErrCanceled = fmt.Errorf("canceled")

func OpenTerminal() (*os.File, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("interactive commands require a terminal; use flags or --no-interactive")
	}
	if !term.IsTerminal(int(tty.Fd())) {
		_ = tty.Close()
		return nil, fmt.Errorf("interactive commands require a terminal")
	}
	return tty, nil
}

func run(model tea.Model, tty *os.File) (tea.Model, error) {
	return tea.NewProgram(model, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen()).Run()
}

type appearance struct{ title, accent, muted, danger lipgloss.Style }

func styles(tty *os.File) appearance {
	r := lipgloss.NewRenderer(tty)
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		r.SetColorProfile(termenv.Ascii)
	} else {
		r.SetColorProfile(termenv.TrueColor)
	}
	return appearance{r.NewStyle().Foreground(lipgloss.Color("#7C3AED")).Bold(true), r.NewStyle().Foreground(lipgloss.Color("#06B6D4")), r.NewStyle().Foreground(lipgloss.Color("#9CA3AF")), r.NewStyle().Foreground(lipgloss.Color("#EF4444"))}
}
func safe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 32 && r != 127 && (r < 128 || r > 159) {
			return r
		}
		return -1
	}, s)
}
func oneLine(s string) string { return strings.Join(strings.Fields(safe(s)), " ") }
func bounded(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > max(1, height) {
		lines = lines[:max(1, height)]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(1, width), "")
	}
	return strings.Join(lines, "\n")
}
func frame(a appearance, title, content, hint string, width, height int) string {
	if height < 6 {
		return bounded(a.title.Render(safe(title))+"\n"+content, width, height)
	}
	return bounded(a.title.Render("TideMail  "+title)+"\n\n"+content+"\n\n"+a.muted.Render(hint), width, height)
}

type Choice struct{ ID, Label string }
type promptModel struct {
	title                 string
	choices               []Choice
	input                 textinput.Model
	text                  string
	cursor, width, height int
	mode                  string
	done, canceled        bool
	a                     appearance
	scroll                tideui.PaneScroller
}

func (m promptModel) Init() tea.Cmd {
	if m.mode == "input" {
		return textinput.Blink
	}
	return nil
}
func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.input.Width = max(1, v.Width-4)
	case tea.KeyMsg:
		key := v.String()
		if key == "ctrl+c" || key == "esc" || key == "q" && m.mode == "choice" {
			m.canceled = true
			return m, tea.Quit
		}
		switch m.mode {
		case "choice":
			switch key {
			case "up", "k":
				m.cursor = max(0, m.cursor-1)
			case "down", "j":
				m.cursor = min(len(m.choices)-1, m.cursor+1)
			case "enter":
				if len(m.choices) > 0 {
					m.done = true
					return m, tea.Quit
				}
			}
		case "input":
			if key == "enter" {
				m.text = m.input.Value()
				m.done = true
				return m, tea.Quit
			}
		case "view":
			switch key {
			case "enter", "q":
				m.done = true
				return m, tea.Quit
			case "up", "k":
				m.scroll.ScrollUp(1)
			case "down", "j":
				m.scroll.ScrollDown(1)
			case "pgdown":
				m.scroll.ScrollDown(max(1, m.height-6))
			case "pgup":
				m.scroll.ScrollUp(max(1, m.height-6))
			}
		}
	}
	if m.mode == "view" {
		m.scroll.ClampTo(len(strings.Split(ansi.Wrap(safe(m.text), max(1, m.width), ""), "\n")), max(1, m.height-6))
	}
	if m.mode == "input" {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m promptModel) View() string {
	content := ""
	hint := "↑/↓ choose  enter continue  q quit"
	switch m.mode {
	case "choice":
		visible := max(1, m.height-6)
		start := max(0, m.cursor-visible+1)
		for i := start; i < min(len(m.choices), start+visible); i++ {
			line := "  " + oneLine(m.choices[i].Label)
			if i == m.cursor {
				line = m.a.accent.Render("> " + oneLine(m.choices[i].Label))
			}
			content += line + "\n"
		}
	case "input":
		content = m.input.View()
		hint = "enter continue  esc back"
	case "view":
		lines := strings.Split(ansi.Wrap(safe(m.text), max(1, m.width), ""), "\n")
		visible := max(1, m.height-6)
		m.scroll.ClampTo(len(lines), visible)
		start := m.scroll.Offset()
		content = strings.Join(lines[start:min(len(lines), start+visible)], "\n")
		hint = "↑/↓ scroll  enter or q back"
	}
	if m.width < 50 {
		switch m.mode {
		case "choice":
			hint = "↑/↓ choose  enter  q quit"
		case "input":
			hint = "enter next  esc back"
		case "view":
			hint = "↑/↓ scroll  enter back"
		}
	}
	return frame(m.a, m.title, strings.TrimRight(content, "\n"), hint, m.width, m.height)
}
func Select(tty *os.File, title string, choices []Choice) (string, error) {
	m, err := run(promptModel{title: title, choices: choices, mode: "choice", width: 80, height: 24, a: styles(tty)}, tty)
	if err != nil {
		return "", err
	}
	p := m.(promptModel)
	if p.canceled || !p.done {
		return "", ErrCanceled
	}
	if len(p.choices) == 0 {
		return "", ErrCanceled
	}
	return p.choices[p.cursor].ID, nil
}
func Input(tty *os.File, title, value string) (string, error) {
	input := textinput.New()
	input.SetValue(value)
	input.Focus()
	input.Width = 70
	m, err := run(promptModel{title: title, input: input, mode: "input", width: 80, height: 24, a: styles(tty)}, tty)
	if err != nil {
		return "", err
	}
	p := m.(promptModel)
	if p.canceled || !p.done {
		return "", ErrCanceled
	}
	return p.text, nil
}
func Show(tty *os.File, title, text string) error {
	_, err := run(promptModel{title: title, text: text, mode: "view", width: 80, height: 24, a: styles(tty)}, tty)
	return err
}
