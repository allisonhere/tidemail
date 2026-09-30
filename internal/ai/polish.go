package ai

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Tone is how Polish should rewrite a draft.
type Tone string

const (
	TonePolish    Tone = "polish"
	ToneFormal    Tone = "formal"
	ToneFriendly  Tone = "friendly"
	ToneShorten   Tone = "shorten"
	ToneElaborate Tone = "elaborate"
)

// MaxPolishRunes caps how much of a draft is sent for rewriting.
const MaxPolishRunes = 8000

var toneInstructions = map[Tone]string{
	TonePolish:    "Polish this email draft: fix grammar, spelling and punctuation and improve clarity and flow, while keeping the author's own voice, meaning and language.",
	ToneFormal:    "Rewrite this email draft in a formal, professional tone, keeping the meaning and language.",
	ToneFriendly:  "Rewrite this email draft in a warm, friendly, conversational tone, keeping the meaning and language.",
	ToneShorten:   "Make this email draft shorter and more direct, keeping every important point and the language.",
	ToneElaborate: "Expand this email draft with a little more detail and courteous phrasing, keeping the meaning and language.",
}

const polishRules = "Do not add facts, names, dates, greetings or sign-offs that are not already in the draft. Keep line breaks between paragraphs. Return only the revised text, with no preamble, explanation or quotation marks.\n\nDraft:\n"

// ParseTone maps a name to a Tone, defaulting to plain polishing for an empty or unknown name.
func ParseTone(name string) Tone {
	t := Tone(strings.ToLower(strings.TrimSpace(name)))
	if _, ok := toneInstructions[t]; ok {
		return t
	}
	return TonePolish
}

// BuildPolishPrompt is the full prompt for rewriting text in the given tone.
func BuildPolishPrompt(text string, tone Tone) string {
	instruction, ok := toneInstructions[tone]
	if !ok {
		instruction = toneInstructions[TonePolish]
	}
	return instruction + " " + polishRules + text
}

// Polish asks the model to rewrite the user's own draft. The text must be only what the user typed;
// callers must not include quoted or forwarded mail.
func Polish(ctx context.Context, s Summarizer, text string, tone Tone) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("nothing to polish")
	}
	if utf8.RuneCountInString(text) > MaxPolishRunes {
		return "", fmt.Errorf("the draft is too long to polish (limit %d characters)", MaxPolishRunes)
	}
	out, err := s.Complete(ctx, BuildPolishPrompt(text, tone))
	if err != nil {
		return "", err
	}
	out = cleanModelText(out)
	if out == "" {
		return "", fmt.Errorf("the AI returned no text")
	}
	return out, nil
}

// cleanModelText removes a code fence or wrapping quotes some models add around their answer.
func cleanModelText(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") && strings.HasSuffix(s, "```") && len(s) >= 6 {
		s = strings.TrimSuffix(strings.TrimPrefix(s, "```"), "```")
		if nl := strings.IndexByte(s, '\n'); nl >= 0 && !strings.ContainsAny(s[:nl], " \t") {
			s = s[nl+1:] // drop a language tag such as "text"
		}
		s = strings.TrimSpace(s)
	}
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"') && strings.Count(s, "\"") == 2 {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}
