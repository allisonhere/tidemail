package ai

import (
	"context"
	"fmt"

	"github.com/allisonhere/tide/internal/config"
)

// Summarizer generates a short summary of an article.
type Summarizer interface {
	Summarize(ctx context.Context, title, content string) (string, error)
	CheckGrammar(ctx context.Context, text string) (string, error)
	ProviderName() string
}

// New returns the configured Summarizer, or an error if none is configured.
func New(cfg config.AIConfig) (Summarizer, error) {
	switch cfg.Provider {
	case "openai":
		if cfg.OpenAIKey == "" {
			return nil, fmt.Errorf("OpenAI API key not set")
		}
		return &openAI{key: cfg.OpenAIKey, model: cfg.OpenAIModel}, nil
	case "claude":
		if cfg.ClaudeKey == "" {
			return nil, fmt.Errorf("no Claude API key set")
		}
		return &claude{key: cfg.ClaudeKey, model: cfg.ClaudeModel}, nil
	case "gemini":
		if cfg.GeminiKey == "" {
			return nil, fmt.Errorf("no Gemini API key set")
		}
		return &gemini{key: cfg.GeminiKey, model: cfg.GeminiModel}, nil
	case "ollama":
		u := cfg.OllamaURL
		if u == "" {
			u = "http://localhost:11434"
		}
		m := cfg.OllamaModel
		if m == "" {
			m = "llama3.2"
		}
		return &ollama{url: u, model: m}, nil
	default:
		return nil, fmt.Errorf("no AI provider configured")
	}
}

const summaryPrompt = "Summarize this article in 3-5 sentences. Be concise and factual. Return only the summary, no preamble.\n\nTitle: %s\n\n%s"

const grammarPrompt = "Fix grammar, spelling, and punctuation in the following text. Return only the corrected version. Preserve line breaks and formatting. Do not add, remove, or change the meaning of any content.\n\n%s"

func truncateContent(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
