package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type ollama struct {
	url   string
	model string
}

func (o *ollama) ProviderName() string { return "Ollama (" + o.model + ")" }

func (o *ollama) Summarize(ctx context.Context, title, content string) (string, error) {
	prompt := fmt.Sprintf(summaryPrompt, title, truncateContent(content, 4000))
	body, _ := json.Marshal(map[string]any{
		"model":   o.model,
		"prompt":  prompt,
		"stream":  false,
		"options": map[string]int{"num_predict": 300},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		o.url+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("ollama: %s", result.Error)
	}
	return result.Response, nil
}
