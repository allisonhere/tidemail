package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

var anthropicChatURL = "https://api.anthropic.com/v1/messages"

type claude struct {
	key   string
	model string
}

func (c *claude) ProviderName() string {
	if c.model != "" {
		return "Claude (" + c.model + ")"
	}
	return "Claude"
}

func (c *claude) Summarize(ctx context.Context, title, content string) (string, error) {
	model := c.model
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	prompt := fmt.Sprintf(summaryPrompt, title, truncateContent(content, 4000))
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 300,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			anthropicChatURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("claude: %s", result.Error.Message)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("claude: empty response")
	}
	return result.Content[0].Text, nil
}
