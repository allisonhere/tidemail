# Protocol reference (API v1)

## One process per call

For every call, TideMail:

1. starts the plugin's `command` directly (no shell, no arguments), in the
   plugin directory, with a reduced environment;
2. writes **one JSON request** followed by a newline to stdin, then closes
   stdin;
3. reads stdout until the process exits;
4. accepts **one JSON response** and exit status 0.

The process is expected to exit after answering. It is killed if it is still
running when the call's timeout (5 seconds) expires. There is no persistent
connection and no state between calls, other than what the plugin keeps
itself.

## stdout and stderr

```text
stdout  →  the protocol JSON response, and nothing else
stderr  →  diagnostics for humans
```

Never print banners, progress bars, debug output, or log lines to stdout.
Anything besides exactly one JSON value (surrounding whitespace is allowed)
makes the response invalid, and the call fails.

stderr is only read when a call fails: TideMail keeps up to 16 KiB of it,
strips control characters, and quotes at most 512 characters in the error it
shows. Do not write secrets there; see [security.md](security.md).

## Request envelope

<!-- drift:request -->
```json
{
  "api": 1,
  "type": "request",
  "request_id": "5c1e0b9a6f2d7e3a4b8c9d01",
  "method": "message.metadata",
  "settings": {"strict": false},
  "data": {"id": 1234, "subject": "Invoice 2026-09", "read": false, "starred": false, "has_attachment": false}
}
```

| Field | Type | Notes |
| --- | --- | --- |
| `api` | number | Always `1` in this version. |
| `type` | string | Always `"request"`. |
| `request_id` | string | Random, unique per call. Copy it into the response. |
| `method` | string | See [Methods](#methods). |
| `settings` | object | Present only if the manifest declares settings: your own resolved non-secret values, keyed by setting `key`. See [settings.md](settings.md). |
| `data` | object | The method's input. Absent for `ping` and `plugin.test`. |

Ignore fields you do not recognize.

## Response envelope

Success:

<!-- drift:response -->
```json
{
  "api": 1,
  "type": "response",
  "request_id": "5c1e0b9a6f2d7e3a4b8c9d01",
  "ok": true,
  "data": {"annotations": [{"key": "category", "value": "billing", "confidence": 0.9}]}
}
```

Error:

<!-- drift:response -->
```json
{
  "api": 1,
  "type": "response",
  "request_id": "5c1e0b9a6f2d7e3a4b8c9d01",
  "ok": false,
  "error": {"code": "unsupported_method", "message": "frobnicate"}
}
```

| Field | Type | Notes |
| --- | --- | --- |
| `api` | number | Must be `1`. |
| `type` | string | Must be `"response"`. |
| `request_id` | string | Must equal the request's. |
| `ok` | boolean | `true` with `data`, or `false` with `error`. |
| `data` | object | The method's result; may be omitted when there is nothing to say. |
| `error` | object | Required when `ok` is `false`, forbidden when `true`. `code` is a short machine-readable string of your choosing; `message` is for people. TideMail shows them as `code: message` and does not interpret the code. |

TideMail rejects the response, and the call fails, when:

- `api` is not 1, or `type` is not `"response"`;
- `request_id` differs from the request's;
- `ok` and `error` disagree (`ok: true` with an error, or `ok: false` without
  one);
- stdout is not exactly one JSON value, or is larger than 1 MiB;
- the process exits with a non-zero status, or runs past the timeout.

A failed call never changes stored annotations.

### Requests you cannot handle

- **Unknown method:** answer `ok: false` with an error such as
  `unsupported_method`. New methods may be added to API v1 over time, and only
  plugins that declare them (or that TideMail calls by hand) receive them.
- **Unsupported `api`:** answer `ok: false` (for example `unsupported_api`).
  A plugin whose manifest says `api = 1` only receives API 1 requests from
  this TideMail.
- **Unreadable input:** there is no request ID to answer, so write a line to
  stderr and exit non-zero.

## Methods

API v1 has seven methods:

<!-- drift:methods -->
```text
ping
message.metadata
message.received
plugin.test
report.run
message.decode
message.prepare
```

| Method | Caller and timing | Requires | `data` sent | Your `data` |
| --- | --- | --- | --- | --- |
| `ping` | health check through TideMail's plugin runtime; the current UI does not send it | nothing | none | `{"message": "pong"}` |
| `message.metadata` | the user, by hand: **Run plugin** (`p`) on the current message, a selection, or a whole view | `message_metadata` | one message's [metadata](#messagemetadata) | any object; `annotations` is stored (with the `annotations` permission), an optional `presentation` explains the result to people ([presentation.md](presentation.md)), anything else is shown under Details |
| `message.received` | TideMail, automatically, for newly arrived mail | `message_metadata`, `events = ["message.received"]`, and the user's opt-in | the same metadata | the same as `message.metadata` |
| `plugin.test` | the user, from the plugin's settings form | `capabilities = ["plugin.test"]` | none | `{"ok": true, "message": "Connected"}`; shown only |
| `report.run` | the user, with `enter` in the plugin list; once per round | `capabilities = ["report.run"]` | the round, context, your state, and query results | `{"queries": …, "state": …}`, `{"report": …}`, or `{"view": …}`; see [queries.md](queries.md) and [views.md](views.md) |
| `message.decode` | the user opening a received message that is encrypted or signed; once per protected part | `message_raw` | one [decode request](#messagedecode) | the plaintext and a signature verdict |
| `message.prepare` | the user sending a message with Sign or Encrypt on | `message_compose` | one [prepare request](#messageprepare) | an armored signature or an armored encrypted message |

`message.metadata` and `message.received` go through the same permission
checks, validation, and storage. Treat them the same way; the method name
only tells you why you were called. Both may be sent many times for the same
message (see
[annotations.md › Reclassification](annotations.md#reclassification-and-idempotency)).

For `plugin.test`, TideMail shows your `message` on the status line and the
whole `data` in a result window. The inner `ok` is for your own reporting;
TideMail does not act on it.

### `message.metadata`

The `data` of `message.metadata` and `message.received`:

<!-- drift:metadata -->
```json
{
  "id": 1234,
  "message_id": "<abc@example.com>",
  "from": "Ann <ann@example.com>",
  "to": "me@example.com",
  "cc": "team@example.com",
  "reply_to": "billing@example.com",
  "subject": "Invoice 2026-09",
  "date": "2026-09-26T10:00:00Z",
  "read": false,
  "starred": false,
  "has_attachment": true,
  "flags": ["\\Seen"],
  "account_name": "Work",
  "mailbox_name": "INBOX",
  "auth": {"spf": "pass", "dkim": "pass", "dmarc": "pass", "spam_flag": false}
}
```

| Field | Type | Notes |
| --- | --- | --- |
| `id` | number | TideMail's local message number. Use it only to match results; it is not stable across cache resets. |
| `message_id` | string | The `Message-ID` header, when present. |
| `from`, `to`, `cc`, `reply_to` | string | Header values as display strings (`Name <addr>`, comma-separated). |
| `subject` | string | Decoded subject. |
| `date` | string | RFC 3339. |
| `read`, `starred`, `has_attachment` | boolean | Always present. |
| `flags` | array of strings | IMAP flags. |
| `account_name` | string | The account's display name. |
| `mailbox_name` | string | The folder the message is in. |
| `auth` | object | What the user's mail server recorded about the sender: `spf`, `dkim`, `dmarc` (each `pass`, `fail`, `softfail`, `neutral`, `none`, `temperror`, `permerror`, `policy`, or `other`; omitted when not reported) and `spam_flag` (true when the server's own spam filter marked it). Omitted entirely when the message has none of these headers. |

Empty string and array fields are omitted.

`auth` holds only those enum values, never the headers they came from. They are
**evidence, not proof**: a mail server that doesn't strip a forged
`Authentication-Results` header lets a sender claim `pass`, and forwarding can
make a genuine message `fail` SPF. Treat an absent `auth` as "unknown".

TideMail **does not send**: the body text, HTML, raw headers, attachments,
AI summaries, OAuth tokens, passwords, API keys, other plugins' settings or
secrets, TideMail's configuration, or file paths. The `message_body`
permission is reserved. The only methods that send message content are
`message.decode` and `message.prepare`, which are described below and need
their own permissions.

### `message.decode`

Sent, with the `message_raw` permission, when the user opens a message that
has an encrypted or signed part. TideMail does all the MIME parsing and sends
only the part to decrypt or verify, one request per part. Byte fields are
base64 strings.

| Field | Type | Notes |
| --- | --- | --- |
| `id` | integer | TideMail's identifier for the message. |
| `kind` | string | `decrypt` or `verify`. |
| `data` | base64 | `decrypt`: an ASCII-armored encrypted or clear-signed message. `verify`: the signed MIME entity, exactly as signed. |
| `signature` | base64 | `verify` only: the detached ASCII-armored signature. |

Answer with:

```json
{"data": "<base64 plaintext>", "signature": {"status": "good", "signer": "Ann <ann@example.com>", "fingerprint": "0123…"}}
```

- `data` is the decrypted MIME entity, or text for an inline message. Omit it
  for `verify`; TideMail rejects a `verify` reply that carries data.
- `signature.status` is one of `none`, `good`, `bad`, `unknown_key`,
  `expired`. Anything else is rejected. `signer` (200 characters) and
  `fingerprint` (80) are shown to the user, so they must be plain text.
- To report a failure, answer `ok=false` with a code such as `no_secret_key`
  or `needs_passphrase` and a short message. TideMail shows the message.

TideMail keeps the plaintext in memory only: it is never stored in the
database or the search index, and the message is decoded again each time it
is opened.

### `message.prepare`

Sent, with the `message_compose` permission, when the user sends a message
with Sign or Encrypt on. TideMail builds the MIME entity and the final
`multipart/signed` or `multipart/encrypted` message itself; you only perform
the cryptographic step.

| Field | Type | Notes |
| --- | --- | --- |
| `from` | string | The sender's address, to choose the signing key. |
| `recipients` | array of strings | Every To, Cc, and Bcc address; empty when only signing. |
| `entity` | base64 | The canonical MIME entity to protect (CRLF line endings), exactly the bytes to sign or encrypt. |
| `sign`, `encrypt` | boolean | At least one is true. |

Answer with exactly one of:

```json
{"signature": "<base64 armored detached signature>", "micalg": "pgp-sha256"}
{"encrypted": "<base64 armored PGP MESSAGE>"}
```

The first is for `sign` without `encrypt`. The second is for `encrypt`; when
`sign` is also true, sign inside the encrypted message. A reply of the wrong
shape (plaintext, or a signature when encryption was asked for) is an error.
**Any error stops the send**: TideMail never falls back to sending the message
unprotected.

Both methods may wait on a passphrase or smartcard prompt, so they get the
manifest's `timeout` (up to 120 seconds) instead of the 5-second default.
