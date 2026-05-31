# Plan: Remove field navigation wrapping in Account Manager form

## Goal

In the account manager add/edit account form, stop the vertical field navigation from wrapping around (looping from last field back to first and vice versa). Keep the existing vertical scroll behavior that keeps the focused field visible.

## Current context / assumptions

- `AccountManager.advanceField()` in `internal/ui/account_manager.go` (line 794) uses modulo arithmetic to wrap field navigation: pressing Up at the first field goes to the last, pressing Down/Tab at the last field goes to the first.
- The vertical scroll (`settingsScrollOffset`) correctly keeps the focused field visible — this should stay as-is.
- The user explicitly said "the setting scroll vertically which is what i want but it wraps and I don't want."

## Root cause

In `advanceField()` (lines 794-818), this line causes the wrap:

```go
next = ((next % int(amFieldCount)) + int(amFieldCount)) % int(amFieldCount)
```

When `next` goes below 0 or above `amFieldCount-1`, the modulo wraps it around instead of clamping.

## Proposed approach

Clamp `next` to the valid range `[0, amFieldCount)` instead of wrapping. If the delta would move the cursor out of bounds, don't advance at all — keep the current field.

## Step-by-step plan

### Step 1: Modify `advanceField()` in `account_manager.go`

Replace the modulo-based wrapping with boundary clamping.

**File**: `internal/ui/account_manager.go`

**Change**: In `advanceField()`, replace:

```go
func (am *AccountManager) advanceField(delta int) {
    next := int(am.focusedField) + delta
    for i := 0; i < int(amFieldCount); i++ {
        next = ((next % int(amFieldCount)) + int(amFieldCount)) % int(amFieldCount)
```

with:

```go
func (am *AccountManager) advanceField(delta int) {
    next := int(am.focusedField) + delta
    if next < 0 || next >= int(amFieldCount) {
        return // don't wrap — stay on current field
    }
    // Skip hidden fields for preset providers
    if am.provider != "Custom" {
        for i := 0; i < int(amFieldCount); i++ {
```

And remove the inner loop's modulo on line 797 (the `next = ((next % ...` line), replacing it with `next += delta` to continue skipping hidden fields in the same direction, then clamping again when done:

```go
    if am.provider != "Custom" {
        f := amField(next)
        for f >= amFieldIMAPHost && f <= amFieldSMTPTLS {
            next += delta
            if next < 0 || next >= int(amFieldCount) {
                return // hit boundary while skipping
            }
            f = amField(next)
        }
    }
```

### Step 2: Build and verify

```bash
go build -o tidemail .
```

### Step 3: Run existing tests

```bash
go test ./internal/ui/... -run AccountManager -v
```

## Files likely to change

- `internal/ui/account_manager.go` — `advanceField()` only

## Tests / validation

- Existing `TestAccountManager*` tests in `internal/ui/account_manager_model_test.go` should still pass.
- Manual check: pressing Up at Provider field does NOT jump to Sync field; pressing Down/Tab at Sync field does NOT jump back to Provider.

## Risks, tradeoffs, and open questions

- **Low risk**: Only changes navigation boundary behavior; the actual field list is unchanged.
- The existing skip logic for hidden preset-provider fields (Custom-only fields when a preset is selected) must still work — the new code preserves that.
- No config or keybinding changes needed.
