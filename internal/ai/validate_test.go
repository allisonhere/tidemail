package ai

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

func TestValidateOpenAI_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-x" {
			t.Fatalf("missing bearer")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	old := openAIModelsURL
	openAIModelsURL = srv.URL + "/v1/models"
	defer func() { openAIModelsURL = old }()

	err := validateOpenAI(context.Background(), "sk-x")
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateOpenAI_AuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer srv.Close()
	old := openAIModelsURL
	openAIModelsURL = srv.URL + "/v1/models"
	defer func() { openAIModelsURL = old }()

	err := validateOpenAI(context.Background(), "sk-bad")
	if err == nil || err.Error() != `openai: HTTP 401: bad key` {
		t.Fatalf("expected HTTP 401 error, got %v", err)
	}
}

func TestProviderRequestErrorDNS(t *testing.T) {
	err := providerRequestError("openai", &net.DNSError{
		Err:    "server misbehaving",
		Name:   "api.openai.com",
		Server: "127.0.0.53:53",
	})
	got := err.Error()
	for _, want := range []string{
		"openai: DNS lookup failed for api.openai.com",
		"via 127.0.0.53:53",
		"server misbehaving",
		"check your DNS/network connection",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in %q", want, got)
		}
	}
}

func TestValidateClaude_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"text":"x"}]}`))
	}))
	defer srv.Close()
	old := anthropicMessagesURL
	anthropicMessagesURL = srv.URL
	defer func() { anthropicMessagesURL = old }()

	err := validateClaude(context.Background(), "k-ant-test")
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateGemini_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "AIzatest" {
			t.Fatalf("missing key query")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()
	old := geminiListModelsURL
	geminiListModelsURL = srv.URL
	defer func() { geminiListModelsURL = old }()

	err := validateGemini(context.Background(), "AIzatest")
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateOllama_ModelPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "llama3.2"}},
		})
	}))
	defer srv.Close()

	err := validateOllama(context.Background(), srv.URL, "llama3.2")
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateOllama_ModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{{"name": "other"}},
		})
	}))
	defer srv.Close()

	err := validateOllama(context.Background(), srv.URL, "missing")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateCredentials_NoProvider(t *testing.T) {
	err := ValidateCredentials(context.Background(), config.AIConfig{})
	if err == nil || err.Error() != "no AI provider selected" {
		t.Fatalf("got %v", err)
	}
}
