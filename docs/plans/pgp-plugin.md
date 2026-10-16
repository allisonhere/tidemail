# PGP through the plugin system: design record

Status: implemented on `feat/pgp-plugin`. User-facing behavior is in
[guide.md › PGP encryption and signing](../guide.md#pgp-encryption-and-signing);
the plugin protocol is in [plugins/protocol.md](../plugins/protocol.md).

## Decision

PGP is a plugin, not core code. TideMail owns everything that depends on mail
structure and storage; a plugin owns the cryptography. No key material, and no
dependency on a PGP library, enters TideMail.

## What is where

| Concern | Where |
| --- | --- |
| Permissions `message_raw`, `message_compose`; methods `message.decode`, `message.prepare`; reply validation; per-manifest `timeout` (max 120 s) | `internal/plugin/crypto.go`, `permissions.go`, `manifest.go` |
| Finding the encrypted/signed part of a received message, exact signed bytes, layered decode | `internal/pgpmail` |
| One message's raw bytes (`BODY.PEEK[]`), body parsing shared with sync | `internal/imap` (`FetchRaw`, `DecodeBody`) |
| Reading pane: `D`, status line, in-memory-only result | `internal/ui/pgp.go` |
| PGP/MIME assembly (`multipart/signed`, `multipart/encrypted`), deterministic boundaries | `internal/smtp/protection.go` |
| Protect before queueing, fail closed | `internal/mailapp/protect.go`, `internal/ui/pgp_compose.go` |
| The GnuPG plugin | `plugins/pgp` in the tidemail-plugins repository (end-to-end test: `internal/pgpmail/e2e_test.go`) |

## Deviations from the first plan

- **TideMail parses MIME, the plugin does not.** The plan sent the plugin a whole
  RFC 822 message and had it return a finished MIME body. Instead TideMail sends
  only the part to decrypt or verify, and only the entity to sign or encrypt, and
  builds `multipart/signed` / `multipart/encrypted` itself. The plugin shrinks to
  a thin gpg wrapper and the fiddly, security-relevant byte handling is in one
  tested place.
- **No prepared-MIME field.** `OutgoingMessage.Protected` holds the plugin's
  result (signature or ciphertext); `smtp.BuildRaw` assembles around it, so the
  Sent copy is byte-identical to what was delivered.
- **Plugins inherit `GNUPGHOME`, `DISPLAY`, `WAYLAND_DISPLAY`, `XDG_RUNTIME_DIR`.**
  gpg needs them to find the keyring and open a graphical pinentry.
- **Decrypt is manual (`D`)**, not on every open, so a passphrase prompt never
  appears while paging through mail.

## Invariants

- Decrypted text is memory only: never in `body_text`, the FTS index, or the
  image cache.
- Encrypt happens before enqueue; the queued JSON contains no plaintext body,
  HTML, or attachments. A retry never needs the plugin.
- Fail closed: any plugin error, missing recipient key, or timeout queues
  nothing and keeps compose open. The toggles are restored when a queued
  message is edited, so an edit cannot silently drop encryption.
- A plugin's reply must have exactly the requested shape (ciphertext when
  encryption was asked for, a signature with a valid `micalg` when signing);
  anything else is an error.

## Known limits

- Subject and the From/To/Date headers are not encrypted (no protected headers).
- Bcc recipients' key IDs are visible in the encrypted message.
- Local drafts and the Outbox keep plaintext until the message is sent.
- Attachments inside decrypted mail are not shown yet.
- Decrypted responses are capped by the 1 MiB plugin output limit.
- Encrypted mail is not searchable (by design).
- `tidemail send` (CLI) does not sign or encrypt.
- No key management or key-server lookups; use `gpg`.

## Testing

Unit tests per layer, plus end-to-end tests that drive real `gpg` with throwaway
keyrings through the real plugin runtime: encrypt+sign round trip, detached
signature, tampering reads as a bad signature, unknown key, missing recipient
key stops the send.
