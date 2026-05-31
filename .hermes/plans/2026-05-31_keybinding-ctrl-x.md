# Plan: Keybinding — `ctrl+x` for Mark All Read

## `ctrl+x` assessment

**Good pick.** Zero conflicts — `ctrl+x` appears nowhere in the codebase. It pairs naturally with `x` (mark read single): `x` crosses off one, `ctrl+x` crosses off all. The old `R` gets freed.

## Full layout — two options depending on Reply

You have `r` in your CSV for both Reply and MarkRead. That's a conflict — whichever comes first in the switch statement wins. Need to pick one for `r`.

### Option A: `r` = Reply (recommended)

```
x       = mark read           (unchanged)
ctrl+x  = mark all read       (new, replaces R)
r       = reply               (unchanged, universal convention)
f       = forward             (your CSV)
s       = sync                (your CSV)
ctrl+s  = sync all            (your CSV, adds to F)
v       = AI summary          (your CSV)
```

5 changes from current. `R` freed. No muscle-memory break for Reply.

### Option B: `r` = Mark Read, Reply moves

```
r       = mark read           (your CSV)
ctrl+x  = mark all read       (new)
?       = reply               (needs a new key)
f       = forward             (your CSV)
s       = sync                (your CSV)
ctrl+s  = sync all            (your CSV)
v       = AI summary          (your CSV)
```

Reply needs a home. Options:
- `shift+r` — `R` is now freed (was mark all read, moved to `ctrl+x`). `shift+r` is Reply, `r` is read. Related actions on adjacent keys. Feels natural.
- `ctrl+r` — free in main view (compose uses it for RemoveAttach, but overlays don't conflict). Less discoverable.
- `g` — free. Not mnemonic.

If Option B, `shift+r` is the strongest candidate: `r`/`shift+r` are adjacent and both are "respond to this message" actions (read it / reply to it).

## Verdict

Option A is simpler (5 changes, no new conflicts). Option B has a certain elegance (`r` read, `shift+r` reply) but breaks universal email client convention. Your call.

Both options share these 4 changes regardless:
```
f       = forward    (was w)
s       = sync       (was f)
ctrl+s  = sync all   (adds to F)
v       = summary    (was s)
ctrl+x  = mark all   (was R)
```
