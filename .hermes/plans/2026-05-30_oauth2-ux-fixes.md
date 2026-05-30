# Plan: Fix OAuth2 Sign-In UX

## Goal

Fix three OAuth2 UX issues: double-enter on sign-in row, Google "already has access" page hanging, and no way to cancel during browser wait.

## Design insight

The double-enter problem is a focus trap: after signing in, the sign-in row becomes a non-interactive status display ("✓ signed in with Google") but focus stays on it. An accidental Enter there either does nothing or re-triggers the flow.

**Fix:** After OAuth2DoneMsg success, advance focus to `amFieldFrom`. The sign-in row is no longer actionable after success, so focus should never stay there.

This also handles the case where the user must Ctrl+S separately — they can just tab through From → Sync and save naturally, rather than being stuck on a dead field.

## Three fixes

### Fix 1 — Auto-advance focus after sign-in (handles double-enter)

In `updateForm`, after setting `oauth2Signed = true`:

```go
am.statusMsg = "signed in with Google"
am.focusField(amFieldFrom)  // jump past the non-actionable sign-in row
return am, nil, false
```

That's it. Focus moves to From. The sign-in row is no longer focusable/actionable in practice (it's still in the tab cycle but the user won't be on it after success).

### Fix 2 — Allow Escape during OAuth2 browser wait

The app locks up during "opening browser for Google sign-in..." because `am.busy` causes `updateForm` to return early. The user can't cancel.

**Fix:** In `updateForm`, add a key handler BEFORE the `am.busy` early-return that lets Escape break out:

```go
// Allow cancel during OAuth2 flow
if am.busy && am.busyMsg == "opening browser for Google sign-in..." {
    km, ok := msg.(tea.KeyMsg)
    if ok && keyMatches(km, keys.Cancel) {
        am.busy = false
        am.busyMsg = ""
        am.statusMsg = "sign-in cancelled"
        return am, nil, false
    }
    return am, nil, false
}
```

The goroutine still runs in the background but its result gets dropped (OAuth2DoneMsg handler checks and ignores if `!am.startedOAuth2` — see below).

### Fix 3 — Add `prompt=consent` to force fresh consent screen

In `internal/auth/google.go`, `StartGmailOAuthFlow`:

```go
authURL := conf.AuthCodeURL(state,
    oauth2.AccessTypeOffline,
    oauth2.SetAuthURLParam("prompt", "consent"),
)
```

This prevents the "already has access" dead-end page. Google always shows a fresh consent screen that auto-redirects after approval, even on repeat sign-ins.

### Fix 4 — Add `startedOAuth2` flag for cancel safety

When Escape cancels the flow, the OAuth2DoneMsg still arrives from the goroutine. To prevent it from re-setting state:

Add `startedOAuth2 bool` to AccountManager. Set it to `true` before dispatching `startOAuth2Flow`. Clear it on cancel and in OAuth2DoneMsg handler. Check it before handling OAuth2DoneMsg:

```go
if oa2, ok := msg.(OAuth2DoneMsg); ok {
    if !am.startedOAuth2 {
        return am, nil, false  // cancelled
    }
    am.startedOAuth2 = false
    ...
}
```

## Files changed

| File | Change |
|------|--------|
| `internal/auth/google.go` | Add `prompt=consent` to auth URL |
| `internal/ui/account_manager.go` | Escape handler during busy; `startedOAuth2` flag; auto-advance focus after sign-in; cancel/cleanup in OAuth2DoneMsg |

## Step-by-step

1. **Auth: prompt=consent** — one-line change in `google.go`
2. **Add `startedOAuth2` field** to AccountManager struct, init to false
3. **Set flag in startOAuth2Flow** — `am.startedOAuth2 = true` before dispatch
4. **Escape handler** — insert before `am.busy` check in `updateForm`
5. **Clear flag on cancel** — in Escape handler: `am.startedOAuth2 = false`
6. **Check flag in OAuth2DoneMsg** — skip handling if not started (cancelled)
7. **Auto-advance focus** — in OAuth2DoneMsg success: `am.focusField(amFieldFrom)`
8. **Reset in resetForm** — clear `startedOAuth2`
9. Build, test

## Tests

- Escape during browser wait → status shows "sign-in cancelled", form responsive
- Sign-in success → focus jumps to From field, sign-in row shows "✓ signed in"
- Double-enter impossible after sign-in (focus moved past sign-in row)
- Repeat sign-in flow works (prompt=consent, no "already has access" dead end)

## Effort

~30 minutes. Small focused changes, all in two files.

## Risks

Low. The auto-advance is one line. The escape handler is a guard clause. The prompt=consent is a known OAuth2 pattern.
