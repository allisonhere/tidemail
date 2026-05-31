package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) openSummary() (tea.Model, tea.Cmd) {
	if len(m.filteredMessages) == 0 {
		return m, nil
	}
	msg := m.filteredMessages[m.messageCursor]

	if msg.Summary != "" {
		m.summaryMessage = msg
		m.summaryGenerating = false
		m.summaryErr = ""
		m.overlay = overlaySummary
		return m, nil
	}

	if m.summarizer == nil {
		m.setStatus("AI not configured — press S to open settings", false)
		return m, m.clearStatusCmd()
	}

	m.summaryMessage = msg
	m.summaryGenerating = true
	m.summaryErr = ""
	m.overlay = overlaySummary
	return m, m.aiSummarizeCmd(msg)
}

func (m *Model) aiSummarizeCmd(msg db.Message) tea.Cmd {
	summarizer := m.summarizer
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		summary, err := summarizer.Summarize(ctx, msg.Subject, msg.BodyText)
		return AISummaryFetchedMsg{MessageID: msg.ID, Summary: summary, Err: err}
	}
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		candidates := [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
			{"pbcopy"},
		}
		for _, args := range candidates {
			path, err := exec.LookPath(args[0])
			if err != nil {
				continue
			}
			cmd := exec.Command(path, args[1:]...)
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return ClipboardCopiedMsg{}
			}
		}
		return ClipboardCopiedMsg{Err: fmt.Errorf("no clipboard tool found (wl-copy/xclip/xsel/pbcopy)")}
	}
}

func saveSummaryMDCmd(msg db.Message, summary, savePath string) tea.Cmd {
	return func() tea.Msg {
		if savePath == "" {
			savePath = "~/"
		}
		if strings.HasPrefix(savePath, "~/") {
			home, _ := os.UserHomeDir()
			savePath = filepath.Join(home, savePath[2:])
		}
		if err := os.MkdirAll(savePath, 0o755); err != nil {
			return SummarySavedMsg{Err: err}
		}

		filename := summaryFilename(msg.Subject)
		fullPath := filepath.Join(savePath, filename)

		content := fmt.Sprintf("# %s\n\n**From:** %s\n**Date:** %s\n\n---\n\n%s\n",
			msg.Subject,
			msg.From,
			msg.Date.Format("Mon, 02 Jan 2006"),
			summary,
		)
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return SummarySavedMsg{Err: err}
		}
		return SummarySavedMsg{Path: fullPath}
	}
}

func (m *Model) grammarCheckCmd(body string) tea.Cmd {
	summarizer := m.summarizer
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		corrected, err := summarizer.CheckGrammar(ctx, body)
		return GrammarCheckedMsg{Corrected: corrected, Err: err}
	}
}
