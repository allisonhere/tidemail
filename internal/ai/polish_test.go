package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeCompleter struct {
	reply  string
	err    error
	prompt string
}

func (f *fakeCompleter) Summarize(context.Context, string, string) (string, error) { return "", nil }
func (f *fakeCompleter) CheckGrammar(context.Context, string) (string, error)      { return "", nil }
func (f *fakeCompleter) ProviderName() string                                      { return "fake" }
func (f *fakeCompleter) Complete(_ context.Context, prompt string) (string, error) {
	f.prompt = prompt
	return f.reply, f.err
}

func TestParseToneDefaultsToPolish(t *testing.T) {
	for in, want := range map[string]Tone{"": TonePolish, "FORMAL": ToneFormal, " shorten ": ToneShorten, "nonsense": TonePolish} {
		if got := ParseTone(in); got != want {
			t.Errorf("ParseTone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPromptCarriesToneAndDraftOnly(t *testing.T) {
	p := BuildPolishPrompt("hi bob, cant make it", ToneFormal)
	if !strings.Contains(p, "formal") || !strings.HasSuffix(p, "hi bob, cant make it") || !strings.Contains(p, "Return only the revised text") {
		t.Fatalf("unexpected prompt: %q", p)
	}
}

func TestPolishTrimsAndCleansTheAnswer(t *testing.T) {
	f := &fakeCompleter{reply: "  \"Hi Bob, I can't make it.\"  "}
	got, err := Polish(context.Background(), f, "  hi bob, cant make it ", TonePolish)
	if err != nil || got != "Hi Bob, I can't make it." {
		t.Fatalf("got %q, %v", got, err)
	}
	if !strings.HasSuffix(f.prompt, "hi bob, cant make it") {
		t.Errorf("draft not trimmed in prompt: %q", f.prompt)
	}
	f.reply = "```text\nHello there.\n```"
	if got, _ := Polish(context.Background(), f, "x", TonePolish); got != "Hello there." {
		t.Errorf("fence not removed: %q", got)
	}
}

func TestPolishRejectsEmptyLongAndProviderErrors(t *testing.T) {
	f := &fakeCompleter{reply: "ok"}
	if _, err := Polish(context.Background(), f, "   ", TonePolish); err == nil {
		t.Error("empty draft should fail")
	}
	if _, err := Polish(context.Background(), f, strings.Repeat("a", MaxPolishRunes+1), TonePolish); err == nil {
		t.Error("over-long draft should fail")
	}
	if f.prompt != "" {
		t.Error("nothing should be sent for rejected drafts")
	}
	f.err = errors.New("boom")
	if _, err := Polish(context.Background(), f, "hello", TonePolish); err == nil || err.Error() != "boom" {
		t.Errorf("provider error should pass through, got %v", err)
	}
	f.err, f.reply = nil, "   "
	if _, err := Polish(context.Background(), f, "hello", TonePolish); err == nil {
		t.Error("blank answer should fail")
	}
}
