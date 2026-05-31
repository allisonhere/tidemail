# Tidemail Assessment

## What's genuinely impressive

**It works.** A TUI email client that handles IMAP, SMTP, OAuth2, SQLite, attachments, multi-account, and self-updating is a serious engineering project. Most CLI email projects stall at "I can read my inbox." You have compose, reply, forward, archive, delete, multi-select, file picker, themes, AI features — the full loop.

**Security choices are correct.** System keychain for credentials, secret redaction in errors and logs, config permission warnings on startup. Most hobby email clients store passwords in plaintext config files. This one doesn't.

**The self-updater is well-built.** I read the code — proper atomic replace (write to `.new`, backup to `.bak`, rename, clean up), checksum-adjacent validation, writability checks before attempting install, manual-elevation fallback with `sudo install` command. That's production-quality.

**AI features are the right kind of weird.** Grammar check and AI summary in a terminal email client is unexpected and genuinely useful. The grammar preview overlay with diff-style before/after is a nice touch.

**The install/deploy story is solid.** `curl | sh` that works, redirect URLs instead of API calls, deploy.sh with step-by-step menu. Feels like a real product.

## Honest gaps

**No cross-mailbox search.** `/` searches the current mailbox only. For someone with 5+ folders, this hurts.

**No drafts.** Compose a message, hit escape, it's gone. Even a simple auto-save to SQLite would change the trust model.

**No conversation threading.** Messages are flat. For mailing lists or long threads, this matters.

**HTML email is best-effort.** html-to-markdown conversion loses layout, embedded images, and styling. Functional for 80% of email, but that last 20% (newsletters, receipts, styled corporate email) renders poorly.

**No offline queue.** Can't compose and "send later" when offline. The send just fails.

**Single binary, single machine.** No sync of read state or settings across computers. (This is a big feature, not a gap per se.)

## What you fixed today

The model.go split (5127→2105 lines, 10 new focused files) was the right call. Every future feature would have touched that monolith. The keybinding rethink makes the app feel more intentional — `f` for forward, `s` for sync, `v` for summary, `ctrl+x` for mark-all-read. Forward from the message list fills an obvious UX gap.

## Verdict

It's the best CLI mail app I've seen because it's the only one that feels like a complete product rather than a collection of IMAP experiments. It has opinions (keyboard-first, security-conscious, AI-augmented) and executes on them. The gaps are real but none are dealbreakers — you can read, write, and manage email end-to-end.

If I were prioritizing next: drafts (biggest trust gap), then cross-mailbox search (biggest workflow gap), then conversation threading (nicest UX win).
