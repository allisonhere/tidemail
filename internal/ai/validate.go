package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/allisonhere/tide/internal/config"
)

// Endpoints are package-level for tests (httptest override).
var (
	openAIModelsURL      = "https://api.openai.com/v1/models"
	anthropicMessagesURL = "https://api.anthropic.com/v1/messages"
	geminiListModelsURL  = "https://generativelanguage.googleapis.com/v1beta/models"
)

const claudeValidateModel = "claude-haiku-4-5-20251001"

// ValidateCredentials checks that the configured provider accepts the current
// credentials (one lightweight HTTP call per backend).
func ValidateCredentials(ctx context.Context, cfg config.AIConfig) error {
	switch cfg.Provider {
	case "openai":
		return validateOpenAI(ctx, cfg.OpenAIKey)
	case "claude":
		return validateClaude(ctx, cfg.ClaudeKey)
	case "gemini":
		return validateGemini(ctx, cfg.GeminiKey)
	case "ollama":
		return validateOllama(ctx, cfg.OllamaURL, cfg.OllamaModel)
	case "":
		return fmt.Errorf("no AI provider selected")
	default:
		return fmt.Errorf("unknown AI provider %q", cfg.Provider)
	}
}

func validateOpenAI(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("OpenAI API key is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openAIModelsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return providerRequestError("openai", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("openai: %s", openAIErrorBody(resp.StatusCode, b))
}

func openAIErrorBody(code int, body []byte) string {
	var wrap struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wrap) == nil && wrap.Error != nil && wrap.Error.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", code, wrap.Error.Message)
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return fmt.Sprintf("HTTP %d: %s", code, s)
	}
	return fmt.Sprintf("HTTP %d", code)
}

func validateClaude(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("Claude API key is empty")
	}
	body, _ := json.Marshal(map[string]any{
		"model":      claudeValidateModel,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicMessagesURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("claude: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("claude: %s", anthropicErrorBody(resp.StatusCode, b))
}

func anthropicErrorBody(code int, body []byte) string {
	var wrap struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wrap) == nil && wrap.Error != nil && wrap.Error.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", code, wrap.Error.Message)
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return fmt.Sprintf("HTTP %d: %s", code, s)
	}
	return fmt.Sprintf("HTTP %d", code)
}

func validateGemini(ctx context.Context, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("Gemini API key is empty")
	}
	u, err := url.Parse(geminiListModelsURL)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("key", key)
	q.Set("pageSize", "1")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("gemini: %s", geminiErrorBody(resp.StatusCode, b))
}

func geminiErrorBody(code int, body []byte) string {
	var wrap struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wrap) == nil && wrap.Error != nil && wrap.Error.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", code, wrap.Error.Message)
	}
	if s := strings.TrimSpace(string(body)); s != "" {
		return fmt.Sprintf("HTTP %d: %s", code, s)
	}
	return fmt.Sprintf("HTTP %d", code)
}

func validateOllama(ctx context.Context, baseURL, model string) error {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = "http://localhost:11434"
	}
	base = strings.TrimSuffix(base, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = "llama3.2"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ollama: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("ollama: %w", err)
	}
	for _, m := range result.Models {
		if m.Name == model {
			return nil
		}
	}
	return fmt.Errorf("ollama: model %q not found on server", model)
}
