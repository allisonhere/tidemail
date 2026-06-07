package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIComplete_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		json.NewDecoder(r.Body).Decode(&reqBody)
		msgs, _ := reqBody["messages"].([]any)
		if len(msgs) == 0 {
			t.Fatalf("expected a user message in request")
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"match":"all"}`}},
			},
		})
	}))
	defer srv.Close()

	old := openAIChatURL
	openAIChatURL = srv.URL
	defer func() { openAIChatURL = old }()

	o := &openAI{key: "sk-test"}
	got, err := o.Complete(context.Background(), "make a filter")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "match") {
		t.Fatalf("unexpected completion: %q", got)
	}
}
