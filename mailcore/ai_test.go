package mailcore

import "testing"

func TestAIConfigMapsEachProvider(t *testing.T) {
	c := AIConfig{Provider: " OpenAI ", Key: "k", Model: "m"}.config()
	if c.Provider != "openai" || c.OpenAIKey != "k" || c.OpenAIModel != "m" || c.ClaudeKey != "" {
		t.Errorf("openai mapping wrong: %+v", c)
	}
	c = AIConfig{Provider: "claude", Key: "k", Model: "m"}.config()
	if c.ClaudeKey != "k" || c.ClaudeModel != "m" || c.OpenAIKey != "" {
		t.Errorf("claude mapping wrong: %+v", c)
	}
	c = AIConfig{Provider: "gemini", Key: "k"}.config()
	if c.GeminiKey != "k" {
		t.Errorf("gemini mapping wrong: %+v", c)
	}
	c = AIConfig{Provider: "ollama", URL: "http://h:1", Model: "llama"}.config()
	if c.OllamaURL != "http://h:1" || c.OllamaModel != "llama" || c.OllamaURL == "" {
		t.Errorf("ollama mapping wrong: %+v", c)
	}
}

func TestPolishTextNeedsAProvider(t *testing.T) {
	if _, err := PolishText(t.Context(), AIConfig{}, "hello", "polish"); err == nil {
		t.Error("expected an error without a provider")
	}
}
