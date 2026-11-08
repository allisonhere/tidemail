package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var anthropicChatURL = "https://api.anthropic.com/v1/messages"

type claude struct {
	key   string
	model string
}

func (c *claude) ProviderName() string {
	return "Claude (" + ClaudeModel(c.model) + ")"
}

func (c *claude) Summarize(ctx context.Context, title, content string) (string, error) {
	return c.chat(ctx, fmt.Sprintf(summaryPrompt, title, truncateContent(content, 4000)), 1024)
}

func (c *claude) Complete(ctx context.Context, prompt string) (string, error) {
	return c.chat(ctx, prompt, completeMaxTokens)
}

func (c *claude) CheckGrammar(ctx context.Context, text string) (string, error) {
	return c.chat(ctx, fmt.Sprintf(grammarPrompt, truncateContent(text, 4000)), 2000)
}

func (c *claude) chat(ctx context.Context, prompt string, maxTokens int) (string, error) {
	model := ClaudeModel(c.model)
	req := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if currentClaudeModels[model] {
		// Current models think before answering; grammar fixes and short
		// summaries don't need much of it, and low effort keeps the
		// thinking from eating the max_tokens the answer needs.
		req["output_config"] = map[string]string{"effort": "low"}
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicChatURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.key)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude: %s", anthropicErrorBody(resp.StatusCode, raw))
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("claude: %w", err)
	}
	// The answer is the text block; a thinking block may come first.
	for _, block := range result.Content {
		if (block.Type == "text" || block.Type == "") && strings.TrimSpace(block.Text) != "" {
			return block.Text, nil
		}
	}
	if result.StopReason == "max_tokens" {
		return "", fmt.Errorf("claude: ran out of tokens before answering")
	}
	if result.StopReason == "refusal" {
		return "", fmt.Errorf("claude: declined the request")
	}
	return "", fmt.Errorf("claude: empty response")
}

// claudeDefaultModel is used when no model is configured: cheap and fast,
// which suits grammar fixes and summaries.
const claudeDefaultModel = "claude-haiku-5-5"

// currentClaudeModels are the models Settings offers.
var currentClaudeModels = map[string]bool{
	"claude-haiku-5-5":  true,
	"claude-sonnet-5-5": true,
	"claude-opus-5-5":   true,
}

// legacyClaudeModels maps names older Settings pickers saved, which the API
// never accepted, to the current model of the same tier.
var legacyClaudeModels = map[string]string{
	"claude-haiku":    "claude-haiku-5-5",
	"claude-sonnet-4": "claude-sonnet-5-5",
	"claude-opus-4":   "claude-opus-5-5",
}

// ClaudeModel returns the API model ID for a configured model name: the
// default when empty, the current tier for a legacy picker name, and any
// other (custom) ID unchanged.
func ClaudeModel(configured string) string {
	m := strings.TrimSpace(configured)
	if m == "" {
		return claudeDefaultModel
	}
	if mapped, ok := legacyClaudeModels[strings.ToLower(m)]; ok {
		return mapped
	}
	return m
}
