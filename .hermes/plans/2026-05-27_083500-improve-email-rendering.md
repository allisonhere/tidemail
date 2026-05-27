# Plan: Improve Rendered Email Appearance

## Goal

Make email bodies in the content pane easier to read and more visually structured. Currently all email text uses a single foreground color with no inline formatting, heading hierarchy, or code-block distinction.

## Current Context

- **Text pipeline**: `renderMessageContent` in `model.go` → `renderHTMLBody` (html-to-markdown via `JohannesKaufmann/html-to-markdown`) or `formatArticleBody` for plain text → `renderMarkdown` (goldmark AST → styled text)
- **HTML path**: HTML → markdown (third-party converter) → goldmark AST → `mdBlock()`/`mdInlineText()` in `render_markdown.go`
- **Plain-text path**: Paragraph splitting → `formatArticleParagraph()` with markdown-like detection (`#`, `>`, `-`) → `wrapWords()`
- **Inline styling**: `mdInlineText()` currently **strips all formatting** — emphasis, strong, strikethrough nodes just recurse into children without applying any ANSI codes
- **Code blocks**: Rendered with `    ` indent, no background distinction
- **Blockquotes**: Simple `│` prefix, same color as body
- **Headings**: Plain text, same size/weight as body
- **Attachments**: Simple filename + size list
- **Subject/meta**: Uses `ContentTitle` and `ContentMeta` styles — these are fine
- lipgloss is fundamentally line-level, not inline. Inline ANSI within a single line is possible (`lipgloss.JoinHorizontal`) but complex for reflowing text

## Proposed Approach — Phased

### Phase 1: Block-level styling (highest impact, lowest risk)

Apply distinct line-level styles to markdown block elements using existing theme colors (`Theme.Dimmed`, `Theme.Border`, `Theme.BorderFocus`).

| Element | Current | Proposed |
|---------|---------|----------|
| Headings (`#`, `##`, etc.) | plain text | **Bold** + accent color (`BorderFocus`) + underline separator |
| Blockquotes | `│` prefix, body color | `│` prefix in `Dimmed` color + dimmed body text |
| Code blocks | `    ` indent | `Dimmed` background + monospace feel |
| Horizontal rules | `────` | Already good, keep |
| Lists | `•` / `1.` prefix | Good, keep |

### Phase 2: Inline formatting

`mdInlineText()` currently discards emphasis/strong. Apply ANSI codes:

| Node | Proposed ANSI |
|------|--------------|
| `Strong` | Bold (ANSI `\033[1m`) |
| `Emphasis` | Italic (ANSI `\033[3m`) or dimmed |
| `CodeSpan` | Invert/highlight background or dimmed |
| `Strikethrough` | Strikethrough (ANSI `\033[9m`) |

**Risk**: ANSI codes inside `wrapWords()` — word-wrap can break mid-ANSI-sequence. Need to either (a) apply wrapping on plain text then add ANSI per-line, or (b) apply ANSI inside `mdInlineText()` and handle wrap carefully.

**Recommendation**: Apply inline ANSI inside `mdInlineText()` and ensure `wrapWords()` handles ANSI by measuring lipgloss.Width (which strips ANSI). Actually, `lipgloss.Width()` does strip ANSI correctly, so `wrapWords` should already work — but we need to verify.

### Phase 3: Structural improvements

- **Collapsible quoted text** in replies — detect lines starting with `>` and collapse them behind a toggle
- **Inline image placeholders** — detect `<img>` tags or CID references and show `[image: filename]`
- **Better attachment list** — show file type icons, better layout
- **Reading width improvements** — optional narrower reading column (like `fmt` or `mutt`)

### Phase 4 (optional, stretch)

- Custom HTML-to-markdown rules (pass through `<b>`, `<i>`, `<code>` as markdown, not raw HTML)
- Email address and link highlighting in plain-text bodies
- Quoted text folding (collapse > blocks in long threads)

## Files Likely to Change

| File | Changes |
|------|---------|
| `internal/ui/render_markdown.go` | Phase 1 (mdBlock styling), Phase 2 (mdInlineText inline ANSI) |
| `internal/ui/format.go` | Phase 1 (formatArticleParagraph heading/quote styling), Phase 3 (quoted text detection) |
| `internal/ui/model.go` | Phase 3 (attachment list improvements), minor `renderMessageContent` tweaks |
| `internal/ui/styles.go` | May need new style fields (e.g. `ContentHeading`, `ContentCode`, `ContentQuote`) |
| `internal/ui/themes.go` | No changes unless we add new theme fields (likely not needed) |

## Implementation Order

```
Phase 1a: Blockquote + dimmed text in render_markdown.go   [~30 min]
Phase 1b: Heading bold + accent in render_markdown.go        [~20 min]
Phase 1c: Code block dimmed bg in render_markdown.go         [~20 min]
Phase 1d: Same treatment in format.go for plain-text path    [~20 min]
─── test ───
Phase 2a: Strong → bold in mdInlineText                      [~15 min]
Phase 2b: Emphasis → italic/dimmed in mdInlineText           [~10 min]
Phase 2c: CodeSpan → highlight in mdInlineText               [~10 min]
─── test ───
Phase 3a: Better attachment list (icons, layout)             [~20 min]
Phase 3b: Quoted text collapse toggle (if desired)           [~40 min]
```

## Tests / Validation

- `go test ./internal/ui/` — existing tests for formatArticleBody, renderMarkdown
- Visual: open an email with headings, lists, code blocks, blockquotes, bold/italic
- Visual: open a plain-text email with `> ` quoted lines
- Visual: open an HTML email with mixed formatting
- Verify `wrapWords` doesn't break ANSI sequences (test with `lipgloss.Width`)

## Risks & Tradeoffs

1. **ANSI + word-wrap**: If `wrapWords()` or `clampView()` splits an ANSI sequence, the rest of the view gets garbled. Mitigation: Only apply ANSI per-line, not mid-line. Test thoroughly.
2. **Performance**: Applying ANSI to every inline node adds escape sequences to the render string. Should be negligible at terminal scrollback sizes.
3. **Theme compatibility**: ANSI bold/italic may not look good on all themes. Keep it subtle — use `Bold(true)` from lipgloss (which emits ANSI) rather than raw escape codes.
4. **Plain-text path duplication**: `formatArticleBody` has its own heading/quote detection separate from `renderMarkdown`. Changes need to happen in both places.

## Open Questions

1. Should we add new style fields (`ContentHeading`, `ContentCode`, `ContentQuote`) to `Styles`, or reuse existing theme colors directly? **Lean: reuse theme colors directly** to avoid bloating the styles struct.
2. Collapsible quoted text — is the complexity worth it for a v1? **Leave for Phase 3b**, skip for now.
3. Inline ANSI in `wrapWords` — need to verify `lipgloss.Width` handles ANSI correctly. Quick test first before committing to the approach.
