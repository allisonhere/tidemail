# Plan: Make Unread Messages Visually Distinct

## Goal

Unread messages in the Messages pane are hard to distinguish from read ones. The only cues are bold text and a `●` prefix — both subtle in a dense terminal list. The user needs a quick, unmistakable visual signal.

## Current state

Current differences between unread and read:

| Property | Unread | Read |
|----------|--------|------|
| Bold | Yes | No |
| Foreground | `t.Fg` (full brightness) | `t.Dimmed` (muted) |
| Background | `t.Bg` (same as read) | `t.Bg` |
| Prefix | `● ` (filled circle) | `· ` (middle dot) |
| Style used | `ArticleUnread` | `ArticleRead` |

The `ArticleUnread` foreground may also get accent-colored (e.g. catppuccin-mocha green `#a6e3a1`) when accent from account color is applied (see `messageRowStyles()`).

The entire row renders through one `style.Width(w-2).Render(renderArticleRow(...))` call — the style applies to the full row content, including prefix + subject + time.

## Proposed approach: background highlight for unread rows

Three options ranked by likely effectiveness:

### Option A — Soft highlight background (recommended)

Give `ArticleUnread` a subtly different background, similar to how `ArticleSelected` uses `adjustLightness(t.Bg, 0.08)` but at maybe half that intensity (`0.04` or `0.05`). This makes unread rows pop visually without needing to change text color or prefix. The read rows remain on `t.Bg`.

**Pros:** Most noticeable change; works regardless of terminal's bold rendering; doesn't affect selected row highlight (selected has its own even-stronger bg).
**Cons:** Slightly more visual noise; could feel busy if many messages are unread.

**Implementation:**
- `styles.go` line 214: change `ArticleUnread` background from `t.Bg` to `adjustLightness(t.Bg, 0.04)` (dark themes) / `adjustLightness(t.Bg, -0.04)` (light themes)
- Or use a fixed small delta like the selected style does, but smaller

### Option B — Accent-colored `●` prefix using `UnreadDot` style

The prefix dot is already styled via `UnreadDot` (foreground `t.Unread`) but `messageRowPrefix()` doesn't apply it — the prefix is just a plain string concat inside `renderArticleRow()`. We could render the prefix separately with `UnreadDot` style for unread messages.

**Pros:** Subtle but clear; the green dot already exists in the styles; minimal code change.
**Cons:** Only affects one character — may not be enough; the dot is already there (filled vs hollow) but the green color would add distinction.

**Implementation:**
- In the message loop (model.go ~1739), conditionally style the dot with `m.styles.UnreadDot.Render(dot)` instead of plain `dot`
- Keep `dot` as-is for read messages
- May need to account for width: the styled dot might be wider than "● "

### Option C — Combo: accent prefix + restyle row

Use `t.Unread` (accent color) for the entire unread subject text instead of `t.Fg`, while keeping `t.Dimmed` for read. Keep bold on unread. No background change.

**Pros:** Accent color (green in catppuccin-mocha) is unmistakably different; no background changes.
**Cons:** Green on dark (#a6e3a1 on #1e1e2e) — readable but could look odd for all unread text; the accent color is already used for the mailbox unread badge, reuse here might conflict visually.

## Recommendation

Start with **Option A + Option B combined** — the soft background gives the main "these are different" signal, and styling the prefix dot with the accent `UnreadDot` color reinforces it. If it's too much, we can drop the background.

## Files changed

- `internal/ui/styles.go` — `ArticleUnread` gets a subtle background variant
- `internal/ui/model.go` — `messageRowPrefix` rendering applies `UnreadDot` style for unread

## Tests / validation

- `go build ./...`, `go test ./...`, `go vet ./...` — all must pass
- Manual visual check in the TUI with mixed read/unread messages

## Risks

- Background contrast must stay ≥ 1.5:1 against `t.Bg` to be barely visible but distinct. The `focusLineBg()` helper already uses this threshold — we can reuse its approach.
- Selected row highlight (which is `adjustLightness(t.Bg, 0.08)`) must be noticeably stronger than the unread highlight to not confuse selection vs. unread state.
- Some terminals don't render bold well — background changes still register.
