# Compose Editor Recovery Plan

## Summary

Restore compose body editing to Bubble Tea `textarea` as the stable default. The custom compose editor is not mature enough to own message composition yet because cursor rendering, wrapped-line movement, selection, clipboard behavior, and viewport behavior need broader test coverage before it can safely replace the existing textarea.

## Key Changes

- Revert the custom editor commit chain: `75d396c`, `d933dad`, `4f669be`, and `f68f527`.
- Restore `ComposeModel.bodyInput` to `textarea.Model` and return compose body updates to the Bubble Tea update path.
- Restore paste sanitization for textarea input so clipboard and terminal paste paths continue to avoid combining-mark corruption.
- Remove the unused `internal/ui/editor` package and its narrow test.
- Leave richer compose selection, copy, and cut behavior out of this recovery pass.

## Test Plan

- Verify compose body typing, newline insertion, reply/forward cursor placement, draft persistence, and paste behavior.
- Verify `ctrl+p` still reads the clipboard and inserts text into the focused compose body.
- Verify compose body rendering stays stable in narrow and short overlay sizes.
- Run `go test ./internal/ui ./...`.

## Assumptions

- Stable compose behavior is more important than keeping the experimental selection editor.
- Bubble Tea `textarea` is acceptable for the recovery path even without the attempted richer selection support.
- Future compose selection and clipboard work should be treated as a separate editor feature with focused tests before replacing `textarea` again.
