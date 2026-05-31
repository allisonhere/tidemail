package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

var geminiModelDefault = "gemini-1.5-flash"

type gemini struct {
	key    string
	model  string
	apiURL string // overridable for testing
}

func (g *gemini) ProviderName() string {
	if g.model != "" {
		return "Gemini (" + g.model + ")"
	}
	return "Gemini"
}

func (g *gemini) Summarize(ctx context.Context, title, content string) (string, error) {
	model := g.model
	if model == "" {
		model = geminiModelDefault
	}
	prompt := fmt.Sprintf(summaryPrompt, title, truncateContent(content, 4000))
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]int{"maxOutputTokens": 300},
	})

	url := g.apiURL
	if url == "" {
		url = "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent?key=" + g.key
	} else {
		url = url + g.key
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("gemini: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}

func (g *gemini) CheckGrammar(ctx context.Context, text string) (string, error) {
	model := g.model
	if model == "" {
		model = geminiModelDefault
	}
	prompt := fmt.Sprintf(grammarPrompt, truncateContent(text, 4000))
	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]int{"maxOutputTokens": 2000},
	})
	url := g.apiURL
	if url == "" {
		url = "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent?key=" + g.key
	} else {
		url = url + g.key
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf("gemini: %s", result.Error.Message)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini: empty response")
	}
	return result.Candidates[0].Content.Parts[0].Text, nil
}
