package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

var openAIChatURL = "https://api.openai.com/v1/chat/completions"

type openAI struct {
	key   string
	model string
}

func (o *openAI) ProviderName() string {
	if o.model != "" {
		return "OpenAI (" + o.model + ")"
	}
	return "OpenAI"
}

func (o *openAI) Summarize(ctx context.Context, title, content string) (string, error) {
	model := o.model
	if model == "" {
		model = "gpt-4o-mini"
	}
	prompt := fmt.Sprintf(summaryPrompt, title, truncateContent(content, 4000))
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 300,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		openAIChatURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", providerRequestError("openai", err)
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return result.Choices[0].Message.Content, nil
}

func (o *openAI) CheckGrammar(ctx context.Context, text string) (string, error) {
	model := o.model
	if model == "" {
		model = "gpt-4o-mini"
	}
	prompt := fmt.Sprintf(grammarPrompt, truncateContent(text, 4000))
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 2000,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIChatURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", providerRequestError("openai", err)
	}
	defer resp.Body.Close()
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("openai: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: empty response")
	}
	return result.Choices[0].Message.Content, nil
}
