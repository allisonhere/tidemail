# TideMail Smart (JEV) acceptance checklist

This checklist is for a release candidate. Run it with a test account or a
deliberately chosen subset of mail. Do not automate it against personal mail,
and do not paste real message bodies into bug reports.

## Before starting

- [ ] Back up the TideMail data directory and confirm the test account is
      disposable or has a recent server-side backup.
- [ ] Build or install the intended TideMail release candidate.
- [ ] Install TideMail Smart (JEV) under
      `$XDG_CONFIG_HOME/tidemail/plugins/smart/`, or under
      `~/.config/tidemail/plugins/smart/` when `XDG_CONFIG_HOME` is unset.
- [ ] Confirm the manifest displays as **TideMail Smart (JEV)** while the
      plugin ID remains `smart`.

## Plugin basics

- [ ] TideMail discovers Smart and shows its version, API, permissions, and
      `message.received` event in **Plugins (experimental)**.
- [ ] Plugin settings show the local/hybrid/Jev mode, Jev model, question
      toggles, and masked API-key field.
- [ ] Enter the TypeSafe key, save it, and confirm the key is not displayed
      in the settings view, result view, status line, or plugin diagnostics.
- [ ] Run **Test plugin configuration** and confirm success is shown without
      exposing the key.
- [ ] Enable automatic processing only after the confirmation prompt.

## Incoming mail and attention views

Using controlled messages or test-account mail, verify:

- [ ] A personal request can produce `needs_reply`, `importance`, or both.
- [ ] A marketing question does not automatically become a reply request.
- [ ] A GitHub notification gets the expected category.
- [ ] A receipt and shipping notice get their expected categories without
      incorrectly becoming urgent.
- [ ] A security warning is visible in **Needs You** when its effective
      signals qualify.
- [ ] A newsletter gets its expected category and does not qualify solely by
      category.
- [ ] **Waiting on Them** remains conversation-state based and is unaffected
      by classification-only changes.
- [ ] **Snoozed** hides the message or waiting cycle until its wake time.

## Corrections and reclassification

- [ ] Correct a false-positive **Needs reply** result to **No**; the reply
      pill and Needs You membership update immediately.
- [ ] Correct a missed **Needs reply** result to **Yes**; the pill, count,
      row, and ranking update immediately.
- [ ] Correct a category and confirm the effective pill shows the corrected
      category while annotation details retain the Smart/Jev result.
- [ ] Reset one field to **Use plugin decision** and confirm the plugin value
      returns immediately.
- [ ] Reset all corrections and confirm every field returns to plugin output.
- [ ] Rerun Smart on a corrected message and confirm the correction survives.
- [ ] Clear Smart annotations and confirm the correction remains effective.
- [ ] Remove Smart and confirm the correction remains until explicitly reset.

## Failure and fallback

- [ ] Remove or invalidate the TypeSafe key.
- [ ] Run Smart in hybrid mode and confirm local classification still returns
      annotations where local rules apply.
- [ ] Make TypeSafe unavailable and confirm the plugin reports a sanitized
      error, falls back locally, and does not pause automatic processing merely
      because Jev was unavailable.
- [ ] Restore the key or service and confirm a later run succeeds.
- [ ] Force repeated plugin failures and confirm the event status shows the
      queue, failure count, last error, and paused state.
- [ ] Resume the paused plugin and confirm dropped events are not replayed
      unexpectedly.
- [ ] Run a manual plugin action while automatic processing is paused; it
      should still work.

## Reclassification scopes

- [ ] Run Smart on one message.
- [ ] Run Smart on a selection.
- [ ] Run Smart on the current mailbox/view.
- [ ] Confirm progress remains responsive and the final summary reports
      successes, failures, and changed/unchanged plugin annotations.

## Cross-account and external-client checks

With at least two configured identities:

- [ ] Send self-mail and confirm the correct account identity is used.
- [ ] Send personal-to-work and work-to-external messages.
- [ ] Reply from the intended account and confirm **Waiting on Them** uses the
      right identity.
- [ ] Send a reply from webmail, a phone, or another mail client, sync
      TideMail, and confirm the waiting conversation updates and any related
      snooze ends correctly.

## Release sign-off

- [ ] Plugin annotations, corrections, and settings remain local.
- [ ] Only the documented Smart/Jev metadata fields are sent to TypeSafe;
      bodies, raw headers, recipients, attachments, account/folder names,
      dates, flags, IDs, and credentials are not sent.
- [ ] The result overlay is readable structured output, not raw JSON/code.
- [ ] No API key, authorization header, subject, or message content appears
      in diagnostics or failure text.
