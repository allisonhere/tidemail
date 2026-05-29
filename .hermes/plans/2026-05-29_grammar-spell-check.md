# Plan: AI Grammar & Spell Check for Compose

## Goal

Add grammar and spell checking to the compose/reply view, powered by the existing AI integration. User can run a check on their draft and see corrections before sending.

## Current state

- AI integration exists for summarization (OpenAI, Claude, Gemini, Ollama)
- Compose modal exists with To, Subject, Body fields
- `internal/ai/` has `Summarizer` interface with `Summarize(body string) (string, error)`
- Compose is in `internal/ui/compose.go`

## Approaches

### Option 1: AI-powered correction (simplest, most capable)

Add a `CheckGrammar(body string) (string, error)` method to the AI provider. Prompt the LLM with "Fix grammar and spelling in this text, return only the corrected version." The corrected text replaces the body.

**Pros:** Catches everything — grammar, spelling, style, tone. No external dependencies. Can handle context-aware fixes ("their" vs "there").
**Cons:** Costs tokens. Adds 1-2s latency. May over-correct or change meaning.

### Option 2: Local spellcheck (offline, zero-cost)

Use `golang.org/x/text` or a hunspell library to check individual words against a dictionary. Highlight misspelled words in the compose body without sending to AI.

**Pros:** Free, instant, offline. No token cost.
**Cons:** Only catches spelling, not grammar. No context awareness. Needs dictionary files.

### Option 3: Hybrid — spellcheck proactively, AI grammar on demand

Spellcheck runs automatically as the user types (underline misspelled words). A keybinding (`ctrl+g`?) triggers AI grammar/style check on the full body.

**Pros:** Best UX — instant spell feedback, grammar check on demand. Feels like a modern editor.
**Cons:** More complex. Two systems to maintain.

## Files likely changed

| File | Change |
|------|--------|
| `internal/ai/openai.go` | Add `CheckGrammar` method |
| `internal/ai/claude.go` | Add `CheckGrammar` method |
| `internal/ai/gemini.go` | Add `CheckGrammar` method |
| `internal/ui/compose.go` | Keybinding + call + replace body with corrected text |
| `internal/ui/keys.go` | Add GrammarCheck keybinding |
| `internal/ui/help.go` | Document new shortcut |

## Simple implementation (Option 1)

1. Add `CheckGrammar(text string) (string, error)` to AI providers
   - System prompt: "Fix grammar, spelling, and punctuation. Return only the corrected text. Preserve line breaks."
   - Use the existing API call pattern from Summarize
2. Add `ctrl+g` keybinding in compose view
3. On trigger: show "checking..." spinner, call AI, replace body textarea content
4. Show status: "grammar checked" or error

## Effort estimate

- **Option 1:** ~2 hours. Trivial — copy the Summarize pattern, new prompt, wire keybinding.
- **Option 2:** ~8 hours. Dictionary integration, TUI underline rendering, word tokenization at cursor.
- **Option 3:** ~12 hours. Both systems plus interaction between them.

## Recommendation

Go with **Option 1** first. It's cheap to build, uses infrastructure we already have, and delivers the most value. Option 2/3 can be layered later if needed.
