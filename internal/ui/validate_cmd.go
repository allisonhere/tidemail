package ui

import (
	"context"
	"time"

	"github.com/allisonhere/tide/internal/ai"
	"github.com/allisonhere/tide/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

func validateAICredentialsCmd(cfg config.AIConfig) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		err := ai.ValidateCredentials(ctx, cfg)
		return AIValidateDoneMsg{Err: err}
	}
}
