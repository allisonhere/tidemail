package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestOpenAISummarize_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("missing/invalid Authorization header")
		}
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["model"] != "gpt-4o-mini" {
			t.Fatalf("expected model gpt-4o-mini, got %v", reqBody["model"])
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": "This is a test summary from OpenAI."}},
			},
		})
	}))
	defer srv.Close()

	old := openAIChatURL
	openAIChatURL = srv.URL
	defer func() { openAIChatURL = old }()

	o := &openAI{key: "sk-test"}
	got, err := o.Summarize(context.Background(), "Test Title", "Test content")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "OpenAI") {
		t.Fatalf("expected summary containing 'OpenAI', got %q", got)
	}
}

func TestOpenAISummarize_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid_api_key"},
		})
	}))
	defer srv.Close()

	old := openAIChatURL
	openAIChatURL = srv.URL
	defer func() { openAIChatURL = old }()

	o := &openAI{key: "sk-bad"}
	_, err := o.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "invalid_api_key") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestOpenAISummarize_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{},
		})
	}))
	defer srv.Close()

	old := openAIChatURL
	openAIChatURL = srv.URL
	defer func() { openAIChatURL = old }()

	o := &openAI{key: "sk-test"}
	_, err := o.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}

func TestClaudeSummarize_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "k-ant-test" {
			t.Fatalf("missing/invalid x-api-key")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("missing anthropic-version")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{
				{"text": "This is a test summary from Claude."},
			},
		})
	}))
	defer srv.Close()

	old := anthropicChatURL
	anthropicChatURL = srv.URL
	defer func() { anthropicChatURL = old }()

	c := &claude{key: "k-ant-test"}
	got, err := c.Summarize(context.Background(), "Test Title", "Test content")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Claude") {
		t.Fatalf("expected summary containing 'Claude', got %q", got)
	}
}

func TestClaudeSummarize_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "invalid x-api-key"},
		})
	}))
	defer srv.Close()

	old := anthropicChatURL
	anthropicChatURL = srv.URL
	defer func() { anthropicChatURL = old }()

	c := &claude{key: "k-bad"}
	_, err := c.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestClaudeSummarize_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]string{},
		})
	}))
	defer srv.Close()

	old := anthropicChatURL
	anthropicChatURL = srv.URL
	defer func() { anthropicChatURL = old }()

	c := &claude{key: "k-ant-test"}
	_, err := c.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}

func TestGeminiSummarize_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.RawQuery, "key=AIza") {
			t.Fatalf("missing key in query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{
					"parts": []map[string]string{{"text": "This is a test summary from Gemini."}},
				}},
			},
		})
	}))
	defer srv.Close()

	g := &gemini{key: "AIzatest", apiURL: srv.URL + "?key="}
	got, err := g.Summarize(context.Background(), "Test Title", "Test content")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Gemini") {
		t.Fatalf("expected summary containing 'Gemini', got %q", got)
	}
}

func TestGeminiSummarize_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "API key not valid"},
		})
	}))
	defer srv.Close()

	g := &gemini{key: "AIzabad", apiURL: srv.URL + "?key="}
	_, err := g.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "API key not valid") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func TestGeminiSummarize_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{},
		})
	}))
	defer srv.Close()

	g := &gemini{key: "AIzatest", apiURL: srv.URL + "?key="}
	_, err := g.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}

func TestOllamaSummarize_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/generate" {
			t.Fatalf("expected /api/generate, got %s", r.URL.Path)
		}
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["model"] != "llama3.2" {
			t.Fatalf("expected model llama3.2, got %v", reqBody["model"])
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"response": "This is a test summary from Ollama.",
		})
	}))
	defer srv.Close()

	o := &ollama{url: srv.URL, model: "llama3.2"}
	got, err := o.Summarize(context.Background(), "Test Title", "Test content")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Ollama") {
		t.Fatalf("expected summary containing 'Ollama', got %q", got)
	}
}

func TestOllamaSummarize_ModelError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"error": "model 'missing' not found",
		})
	}))
	defer srv.Close()

	o := &ollama{url: srv.URL, model: "missing"}
	_, err := o.Summarize(context.Background(), "T", "C")
	if err == nil || !strings.Contains(err.Error(), "model 'missing' not found") {
		t.Fatalf("expected model error, got %v", err)
	}
}

func TestOllamaSummarize_ConnectionError(t *testing.T) {
	o := &ollama{url: "http://127.0.0.1:1", model: "test"}
	_, err := o.Summarize(context.Background(), "T", "C")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestNew_NoProvider(t *testing.T) {
	_, err := New(config.AIConfig{})
	if err == nil || err.Error() != "no AI provider configured" {
		t.Fatalf("expected 'no AI provider configured', got %v", err)
	}
}

func TestTruncateContent(t *testing.T) {
	short := "hello"
	if got := truncateContent(short, 10); got != short {
		t.Fatalf("expected %q, got %q", short, got)
	}
	long := "this is a long string that should be truncated"
	if got := truncateContent(long, 10); len(got) != 10 {
		t.Fatalf("expected 10 chars, got %d: %q", len(got), got)
	}
}
