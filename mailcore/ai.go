package mailcore

import (
	"context"
	"fmt"
	"strings"

	"github.com/allisonhere/tidemail/internal/ai"
	"github.com/allisonhere/tidemail/internal/config"
)

// AIConfig selects an AI provider and carries the user's own key. Provider is "openai", "claude",
// "gemini" or "ollama"; URL is only used by ollama (defaults to http://localhost:11434).
type AIConfig struct {
	Provider string
	Key      string
	Model    string
	URL      string
}

func (c AIConfig) config() config.AIConfig {
	out := config.AIConfig{Provider: strings.ToLower(strings.TrimSpace(c.Provider))}
	switch out.Provider {
	case "openai":
		out.OpenAIKey, out.OpenAIModel = c.Key, c.Model
	case "claude":
		out.ClaudeKey, out.ClaudeModel = c.Key, c.Model
	case "gemini":
		out.GeminiKey, out.GeminiModel = c.Key, c.Model
	case "ollama":
		out.OllamaURL, out.OllamaModel = c.URL, c.Model
	}
	return out
}

// PolishText rewrites a draft the user typed. tone is "polish", "formal", "friendly", "shorten" or
// "elaborate" (anything else means "polish"). Send only the user's own words: never quoted or forwarded
// mail, recipients or subjects.
func PolishText(ctx context.Context, cfg AIConfig, text, tone string) (string, error) {
	summarizer, err := ai.New(cfg.config())
	if err != nil {
		return "", fmt.Errorf("AI is not set up: %w", err)
	}
	return ai.Polish(ctx, summarizer, text, ai.ParseTone(tone))
}

// CheckAI makes one lightweight request to confirm the provider accepts the credentials.
func CheckAI(ctx context.Context, cfg AIConfig) error {
	return ai.ValidateCredentials(ctx, cfg.config())
}
