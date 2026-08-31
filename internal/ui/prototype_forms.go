package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/config"
)

type prototypeScreen int

const (
	prototypeSettings prototypeScreen = iota
	prototypeAccount
	prototypeCompose
)

type PrototypeFormsModel struct {
	width  int
	height int
	styles Styles
	screen prototypeScreen
}

func NewPrototypeFormsModel(cfg config.Config) PrototypeFormsModel {
	theme, _ := MergedThemeFromConfig(cfg)
	return PrototypeFormsModel{
		width:  96,
		height: 28,
		styles: BuildStyles(theme, cfg.Display.Density, cfg.Display.PaneCorners),
		screen: prototypeSettings,
	}
}

func (m PrototypeFormsModel) Init() tea.Cmd { return nil }

func (m PrototypeFormsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch strings.ToLower(msg.String()) {
		case "1":
			m.screen = prototypeSettings
		case "2":
			m.screen = prototypeAccount
		case "3":
			m.screen = prototypeCompose
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m PrototypeFormsModel) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	chrome := newManagerChrome(max(1, min(m.width-4, 92)), m.styles.Theme, m.styles.PlainUI)
	view := m.render(chrome)
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Background(m.styles.Theme.Bg).
		Render(clampView(view, m.width, m.height, m.styles.Theme.Bg))
}

func (m PrototypeFormsModel) render(chrome managerChrome) string {
	outerW := max(36, min(m.width-4, 92))
	outerH := max(10, m.height-2)
	header := m.prototypeTopBar(outerW, chrome)
	footer := lipgloss.JoinVertical(lipgloss.Left,
		renderManagerActions(outerW, chrome, "enter", m.prototypePrimaryAction()),
		renderManagerActions(outerW, chrome, "1", "settings", "2", "account", "3", "compose"),
		renderManagerActions(outerW, chrome, "q", "quit", "esc", "cancel"),
	)
	bodyH := max(1, outerH-lipgloss.Height(header)-lipgloss.Height(footer)-2)

	var body string
	switch m.screen {
	case prototypeAccount:
		body = m.renderPrototypeAccount(outerW, bodyH, chrome)
	case prototypeCompose:
		body = m.renderPrototypeCompose(outerW, bodyH, chrome)
	default:
		body = m.renderPrototypeSettings(outerW, bodyH, chrome)
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	box := renderChromeOverlayBox(clampView(inner, outerW, outerH, chrome.baseBg), outerW, chrome, chrome.accent)
	return overlayOnBase("", box, m.width, m.height, m.styles.Theme.Bg, false)
}

func (m PrototypeFormsModel) prototypePrimaryAction() string {
	switch m.screen {
	case prototypeAccount:
		return "test"
	case prototypeCompose:
		return "send"
	default:
		return "save"
	}
}

func (m PrototypeFormsModel) prototypeTopBar(width int, chrome managerChrome) string {
	title := "FORM LAB"
	subtitle := "static terminal prototypes"
	name := "Settings"
	switch m.screen {
	case prototypeAccount:
		name = "Manage Account"
	case prototypeCompose:
		name = "Compose"
	}
	left := chrome.header.Width(max(1, width/2)).Render(title)
	right := lipgloss.NewStyle().
		Background(chrome.accent).
		Foreground(chrome.accentFg).
		Width(max(1, width-lipgloss.Width(left))).
		Align(lipgloss.Right).
		Padding(0, 1).
		Render(strings.ToUpper(name))
	note := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 1).
		Render(subtitle)
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Left, left, right), note)
}

func (m PrototypeFormsModel) renderPrototypeSettings(width, height int, chrome managerChrome) string {
	sideW := clamp(width/3, 16, 24)
	gap := 1
	detailW := max(20, width-sideW-gap)
	sidebar := m.prototypeSidebar(sideW, height, chrome, []string{
		"Display", "Mail", "Updates", "AI", "About",
	}, 0)
	rows := []string{
		m.prototypeSectionTitle(detailW, "Settings", "Tune the reading experience without hunting through rows.", chrome),
		m.prototypeToggleRow(detailW, "Reading density", "Compact", true, chrome),
		m.prototypeToggleRow(detailW, "Actionable links", "On", false, chrome),
		m.prototypeInputRow(detailW, "Browser command", "xdg-open"),
		m.prototypeSliderRow(detailW, "Reading width", "92 columns", chrome),
		m.prototypeStatusRow(detailW, "Saved locally", "No restart required", chrome),
		m.prototypeButtonRail(detailW, []string{"SAVE", "RESET", "PREVIEW"}, chrome),
	}
	detail := clampView(strings.Join(rows, "\n"), detailW, height, chrome.baseBg)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, strings.Repeat(" ", gap), detail)
}

func (m PrototypeFormsModel) renderPrototypeAccount(width, height int, chrome managerChrome) string {
	rows := []string{
		m.prototypeSectionTitle(width, "Manage Account", "Connection first. Server details stay compact.", chrome),
		m.prototypeAccountCard(width, "alliehere.com", "allie@alliehere.com", "imap.alliehere.com:993", "smtp.alliehere.com:587", "10 mailboxes discovered", chrome),
		m.prototypeAccountCard(width, "HR", "hr@example.com", "imap.hr.example.com:993", "smtp.hr.example.com:587", "5 mailboxes discovered", chrome),
	}
	return clampView(strings.Join(rows, "\n"), width, height, chrome.baseBg)
}

func (m PrototypeFormsModel) prototypeAccountCard(width int, name, email, imapServer, smtpServer, status string, chrome managerChrome) string {
	rows := []string{
		m.prototypeInputRow(width, "Name", name),
		m.prototypeInputRow(width, "Email", email),
		m.prototypeInputRow(width, "IMAP", imapServer+"  TLS on"),
		m.prototypeInputRow(width, "SMTP", smtpServer+"  STARTTLS on"),
		m.prototypeStatusRow(width, "Connected", status, chrome),
	}
	return strings.Join(rows, "\n")
}

func (m PrototypeFormsModel) renderPrototypeCompose(width, height int, chrome managerChrome) string {
	actions := m.prototypeButtonRail(width, []string{"SEND", "SAVE DRAFT", "CANCEL"}, chrome)
	bodyH := max(1, height-4-lipgloss.Height(actions))
	rows := []string{
		m.prototypeSectionTitle(width, "Compose", "A writing surface with commands pinned in place.", chrome),
		m.prototypeInputRow(width, "To", "maya@example.com  sam@example.com"),
		m.prototypeInputRow(width, "Subject", "Friday launch notes"),
	}
	body := lipgloss.NewStyle().
		Background(chrome.fieldBg).
		Foreground(chrome.text).
		Width(width).
		Height(bodyH).
		Padding(0, 2).
		Render("Hey team,\n\nHere is the clean version of the rollout note. The body owns the space; actions never drift away.")
	body = clampView(body, width, bodyH, chrome.fieldBg)
	rows = append(rows, body, actions)
	return clampView(strings.Join(rows, "\n"), width, height, chrome.baseBg)
}

func (m PrototypeFormsModel) prototypeSidebar(width, height int, chrome managerChrome, labels []string, active int) string {
	rows := make([]string, 0, len(labels)+1)
	rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 1).Render("Sections"))
	for i, label := range labels {
		style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(0, 1)
		prefix := "  "
		if i == active {
			style = style.Background(chrome.accent).Foreground(chrome.accentFg)
			prefix = "> "
		}
		rows = append(rows, style.Render(prefix+label))
	}
	return clampView(strings.Join(rows, "\n"), width, height, chrome.baseBg)
}

func (m PrototypeFormsModel) prototypeSectionTitle(width int, title, subtitle string, chrome managerChrome) string {
	titleLine := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Bold(true).Width(width).Padding(0, 1).Render(title)
	subtitleLine := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 1).Render(truncate(subtitle, max(1, width-2)))
	return lipgloss.JoinVertical(lipgloss.Left, titleLine, subtitleLine)
}

func (m PrototypeFormsModel) prototypeInputRow(width int, label, value string) string {
	labelW := clamp(width/3, 10, 18)
	valueW := max(8, width-labelW)
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	left := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(labelW).Padding(0, 1).Render(label)
	right := lipgloss.NewStyle().Background(chrome.fieldBg).Foreground(chrome.text).Width(valueW).Padding(0, 1).Render(truncate(value, max(1, valueW-2)))
	return lipgloss.JoinHorizontal(lipgloss.Left, left, right)
}

func (m PrototypeFormsModel) prototypeToggleRow(width int, label, value string, selected bool, chrome managerChrome) string {
	state := "[ ]"
	if strings.EqualFold(value, "on") || strings.EqualFold(value, "compact") {
		state = "[x]"
	}
	if selected {
		return lipgloss.NewStyle().Background(chrome.fieldBg).Foreground(chrome.text).Width(width).Padding(0, 1).Render(fmt.Sprintf("%s %s  %s", state, label, value))
	}
	return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(0, 1).Render(fmt.Sprintf("%s %s  %s", state, label, value))
}

func (m PrototypeFormsModel) prototypeSliderRow(width int, label, value string, chrome managerChrome) string {
	barW := max(4, width-lipgloss.Width(label)-lipgloss.Width(value)-8)
	bar := strings.Repeat("=", max(1, barW/2)) + ">" + strings.Repeat("-", max(1, barW-barW/2-1))
	return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(0, 1).Render(fmt.Sprintf("%s  %s  %s", label, bar, value))
}

func (m PrototypeFormsModel) prototypeStatusRow(width int, title, detail string, chrome managerChrome) string {
	return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.successFg).Width(width).Padding(0, 1).Render(truncate("OK  "+title+" - "+detail, max(1, width-2)))
}

func (m PrototypeFormsModel) prototypeButtonRail(width int, labels []string, chrome managerChrome) string {
	pairs := make([]string, 0, len(labels)*2)
	for i, label := range labels {
		pairs = append(pairs, fmt.Sprintf("f%d", i+1), strings.ToLower(label))
	}
	return renderManagerActions(width, chrome, pairs...)
}
