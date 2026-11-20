# TideMail bug review — 2026-11-19

Scope: the uncommitted contacts / People view work and the SMTP User change in
the working tree at `feat/notification-actions` (HEAD `bbb7813`), since that
code is new and has not been through the earlier audit.

Read-only review: no repository files were modified. `go build ./...` and
`go vet ./internal/ui ./internal/plugin ./internal/smtp ./internal/config`
pass on the current tree.

## Summary

| # | Bug | Location | Severity | Confidence |
|---|---|---|---|---|
| 1 | Editing a contact silently truncates its note at 256 characters | `internal/ui/plugin_contacts.go:85,310,409` | medium | high |
| 2 | After a search, Enter can compose to the wrong contact | `internal/ui/plugin_contacts.go:157-175,221-231` | medium | high |
| 3 | Double-Enter creates the contact twice; late save result wipes a reopened form | `internal/ui/plugin_contacts.go:177-191,387-419` | medium-low | high |
| 4 | `contacts_write` missing from the plugin install/consent checklist | `internal/ui/plugins.go:1108-1139` | medium | high |
| 5 | Stale load/search replies clobber the list (no request identity) | `internal/ui/plugin_contacts.go:89-125,157-175` | low-medium | high |
| 6 | `Manager.Call` doesn't refuse the new `contacts.list`/`contacts.save` | `internal/plugin/manager.go:221-224` | low (in-tree) | high |
| 7 | `SaveContactRequest.Revision` is `omitempty`, so revision 0 is never sent | `internal/plugin/contacts.go:183-187` | low | medium |
| 8 | `mailcore`'s public API can't use `smtp_user` | `mailcore/mailcore.go:25-56` | low | high |
| 9 | New feature undocumented (repo docs rule) | `docs/` | low | high |

---

## 1. Note truncated at 256 characters on edit — address-book data loss

`internal/ui/plugin_contacts.go:85`, `:310`, `:409`;
`internal/ui/account_manager.go:427`; `internal/plugin/contacts.go:36`

`noteInput` is created with `newAMInput`, which sets `CharLimit = 256`
(`account_manager.go:427`). `beginEdit` does `pc.noteInput.SetValue(c.Note)`,
and bubbles' `SetValue` truncates to `CharLimit` (`setValueInternal`). So:

1. Open a contact whose note is longer than 256 runes.
2. Change only the phone number.
3. Press Enter.

`submitForm` sends the truncated note and the plugin writes it back. The rest
of the note is lost, and because `editRevision` is unchanged the plugin cannot
detect it. The plugin itself allows 2000 characters (`maxContactNote`).

An address longer than 256 characters truncates the same way
(`maxContactAddr` is 320), though that is far rarer.

**Fix direction:** give the note input its own `CharLimit` (2000, matching
`maxContactNote`) or use a textarea; do not build it from `newAMInput`'s
256-char default.

---

## 2. Enter after a search can compose to the wrong contact

`internal/ui/plugin_contacts.go:157-175` (only `clampCursor`), `:221-231`

`applyLoaded` never resets `pc.cursor` for search results. With the cursor on
row 50 of the full list, search for something that returns 2 rows:
`clampCursor` drops the cursor to index 1, so pressing Enter composes to the
**second** result instead of the first. A one-result search happens to land on
0, which hides the bug in casual testing.

**Fix direction:** set `cursor = 0` when the loaded list came from
`contacts.search` (which also requires the request-kind tagging from item 5).

---

## 3. Double-Enter saves twice; a late save result wipes a reopened form

`internal/ui/plugin_contacts.go:387-388`, `:398-419`, `:177-191`

`submitForm` sets `pc.saving = true`, but nothing ever checks it — `saving` is
only read to render "Saving…" (`:578`). Two Enter presses within the 5 s
plugin timeout each launch a `saveCmd`, and both carry `wasAdd: true`, so the
plugin's address book gets two copies.

Related: `applySaved` sets `pc.mode = pcList` unconditionally. If the user
pressed Esc and then `n` to start another contact while a save was in flight,
the arriving result closes and discards the new form.

**Fix direction:** early-return from `submitForm` (or ignore Enter in
`updateForm`) while `saving`; in `applySaved`, only leave form mode if the
model is still in the mode that submitted the save.

---

## 4. `contacts_write` is invisible on the plugin install/consent screen

`internal/ui/plugins.go:1108-1139` (used at `:799` and
`internal/ui/plugin_catalog.go:274`)

`permissionChecklist` lists `ContactsRead` ("Look up contacts") but not the new
`ContactsWrite`. That checklist is what the catalog install prompt renders
under "It will be able to:", and what the plugin detail pane shows. A plugin
can therefore request write access to the user's address book without the user
ever seeing it.

`Permissions.Names()` does include it (`"contacts write"`), so the CLI
`plugins info` output and the UI disagree.

**Fix direction:** add `mark(p.ContactsWrite, "Add and edit contacts")` to the
checklist and update its doc comment.

---

## 5. Stale load/search replies can clobber the list

`internal/ui/plugin_contacts.go:89-125`, `:157-175`, `:213-219`, `:239-242`

Every load and search returns the same `pluginContactsLoadedMsg`, and
`applyLoaded` decides where to put it using the *current* `searchActive` flag
rather than what the request was. Consequences:

- Press `r`, then quickly `/` + Enter. If the reload lands last, it replaces
  the visible search results with the full list while `searchActive` stays
  `true` — so Esc "clears" a filter that is not shown.
- Start a search, press Esc before it returns (which sets
  `searchActive = false` and restores `all`), then the search reply arrives and
  is stored as the new full list. `pc.all` is now a filtered subset until the
  next reload.

**Fix direction:** attach a request generation (or a kind/query field) to
`pluginContactsLoadedMsg` and drop replies that no longer match the current
request.

---

## 6. New permission-checked methods missing from `Manager.Call`'s refusal list

`internal/plugin/manager.go:221-224`; methods defined at
`internal/plugin/contacts.go:18`, `:23`

`Call`'s own comment says methods with data-bearing entry points "are refused
here, so the check cannot be bypassed", and it refuses `contacts.search` — but
not `contacts.list` or `contacts.save`. Any caller of the exported
`Manager.Call` can send those to a plugin that never declared
`contacts_read`/`contacts_write`, skipping the capability check that
`cryptoCall` performs.

Nothing in-tree does this today (only `Ping` uses `Call`), so it is a
defense-in-depth gap rather than a live exploit.

**Fix direction:** add `MethodContactsList` and `MethodContactsSave` to the
refusal condition.

---

## 7. `SaveContactRequest.Revision` is `omitempty`

`internal/plugin/contacts.go:183-187`

`PluginContact.Revision` uses `json:"revision"`, but the save request uses
`json:"revision,omitempty"`. Editing a contact whose current revision is 0 (a
normal value; the test fixture itself uses it) sends no `revision` field at
all. Whether that is a bug depends on the external plugin treating an absent
revision as 0 for edits; if it requires the field, such contacts cannot be
edited.

**Fix direction:** drop `omitempty` from `SaveContactRequest.Revision` (or
confirm the plugin's contract first) and make the two structs agree.

---

## 8. `mailcore`'s public API can't use `smtp_user`

`mailcore/mailcore.go:25-56`

`mailcore.Account` has no `SMTPUser` field and its `config()` never sets it, so
a mailcore-based client on a delegated mailbox still authenticates SMTP as
`User`. No in-tree caller uses `mailcore.Send`, so this only affects external
clients of the public package.

---

## 9. Documentation gaps

`AGENTS.md` asks for help text and docs to change with user-facing behavior.
The new People view and the `contacts_write` / `contacts.list` /
`contacts.save` surface appear nowhere in `docs/guide.md`, `README.md`,
`STATUS.md`, `CHANGELOG.md`, `docs/plugins/permissions.md`,
`docs/plugins/manifest.md`, or `docs/plugins/protocol.md` — only `smtp_user` is
documented. `docs/plugins/permissions.md` is where a plugin author will look
and currently describes only `contacts_read`.

---

## Worth knowing, not filed as bugs

- `tideContactsDefaultView` (`internal/ui/model.go:3901`) hard-codes the
  setting key `default_view` and the value `"TideContacts"`, and returns
  "use plugin" when the manifest has no such setting. That is fine for the
  plugin as currently shipped, but it is an assumption this repo cannot verify
  and it silently overrides `C` for anyone with a plugin whose manifest id is
  `tidecontacts`.
- `overlayPluginContacts` is not in the non-key-message routing at
  `internal/ui/model.go:2062-2075`, so `PluginContacts` never receives non-key
  messages. No practical impact today (the inputs do not blink and paste
  arrives as a key message), but it is inconsistent with the other manager
  overlays.
