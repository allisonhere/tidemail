# Security and trust model

> **TideMail plugins are executable software and are not a complete OS
> sandbox. Only install plugins you trust.**

A plugin runs with the user's own privileges. It can read the user's files
and use the network regardless of its manifest. What TideMail controls is what
it *hands* a plugin, how it *runs* it, and what it *keeps* from the result.

## Installing from the catalog

Plugins from the in-app catalog (Settings → Plugins → `b`) are downloaded only
from GitHub over HTTPS, checked against the release's SHA-256, unpacked as plain
files, and validated against the catalog before they replace anything. The
plugin is not run during install, and the confirm step lists the permissions
first. The checksum comes from the same release, so it does not protect against a
compromised plugins repository. Details: [distribution.md](distribution.md).

## What TideMail hands a plugin

- Only the request: message metadata for metadata methods (see
  [protocol.md](protocol.md#messagemetadata)), the results of a report's
  read-only queries (see [queries.md](queries.md)), and the plugin's own
  settings.
- Its own secrets, as `TIDEMAIL_SECRET_*` environment variables, and nothing
  else secret.
- Message content only through `message.decode` and `message.prepare`, and
  only to plugins that declare `message_raw` or `message_compose`: the
  encrypted or signed part of a message you open, or the content of a
  message you send with Sign or Encrypt on. TideMail keeps decrypted text in
  memory and never stores it.
- Sender-check results (`auth` in the metadata: SPF, DKIM and DMARC as pass/fail
  values, and the server's spam flag) but not the headers they come from.
- Otherwise never message bodies, HTML, raw headers, attachments, AI summaries,
  database paths or handles, SQL, account passwords, OAuth tokens, API keys,
  other plugins' settings or secrets, TideMail's configuration, or file
  paths. Other plugins' stored annotations only through `query.annotations`,
  with the `annotations_query` permission.
- Data only after the [permission checks](permissions.md), which run in
  TideMail's plugin runtime for every call and every query.

> Query permissions allow TideMail to disclose selected structured data to a
> plugin. They do not grant direct database access. Aggregate analytics
> permissions disclose computed statistics without necessarily disclosing
> individual message records.

## How TideMail runs a plugin

| Restriction | Detail |
| --- | --- |
| No shell | The command is executed directly. There is no command string to inject into. |
| Relative executable | `command` must stay inside the plugin directory; absolute paths and `..` are rejected. |
| No `$PATH` lookup | `command = "python3"` means the file in the plugin directory. |
| No arguments | Input arrives only on stdin. |
| Working directory | The plugin directory. |
| Trimmed environment | Only `PATH`, `HOME`, `USER`, `LANG`, `LC_ALL`, `LC_CTYPE`, `LC_MESSAGES`, `TMPDIR`, `TZ`, the non-secret `GNUPGHOME`, `DISPLAY`, `WAYLAND_DISPLAY`, and `XDG_RUNTIME_DIR` (so a GnuPG plugin finds your keyring and can open a passphrase prompt), plus the plugin's own secrets. TideMail's own environment (client secrets, provider keys) is not passed on. |
| Timeout | 5 seconds per call, from start to exit. A report (several rounds) must finish within 30 seconds. |
| Process-tree kill | On timeout, cancellation, or quit, the whole process group is killed (Unix), so child processes do not linger. |
| Output limit | stdout above 1 MiB fails the call. |
| stderr limit | 16 KiB kept, 512 characters quoted in errors. |
| Bounded concurrency | One process per plugin at a time, and at most 3 plugin processes for message calls and report rounds overall. |
| Bounded reports | At most 8 rounds of at most 8 queries; 64 KiB of state; 4 MiB of results per round; at most 500 rows per page. |

The headless developer commands use the same direct process runner and output
limits. `tidemail plugin test` is an offline protocol check by default; it
does not run a remote configuration test. `--config-test` requires the
explicit `--allow-network` flag. This is an opt-in workflow guard, not an OS
network sandbox: a trusted executable with network permission can still make
its own network calls.

`--secret-env setting=ENV_VAR` supplies one declared secret setting from the
named environment variable. The value is passed only to that plugin and is
redacted from surfaced process diagnostics and JSON reports.

## What TideMail keeps and shows

- **Protocol validation:** exactly one JSON value, matching `api`, `type`,
  and `request_id`, consistent `ok`/`error`.
- **Annotation validation:** all or nothing, with limits on count, key
  syntax, value length and characters, and confidence
  ([annotations.md](annotations.md)). Stored with parameterized SQL; never
  used as code, commands, URLs, file paths, colors, or styles.
- **Sanitized display:** response data, errors, and stderr are stripped of
  control and format characters (terminal escapes, bidi overrides,
  zero-width characters) before drawing, and capped (16 KiB and 200 lines
  for result windows). Error messages replace the plugin directory path with
  `plugins/`.
- **Secret masking:** a plugin's secret values are replaced with `********`
  in its stdout and stderr before anything else sees them.
- **Read-only queries:** TideMail runs a report's queries itself, from fixed
  allowlists of methods, fields, scopes, and filters. No query can change
  mail or TideMail's state.
- **No actions:** nothing in a response is treated as an instruction.
  TideMail never moves, deletes, flags, or sends mail, opens URLs, runs
  commands, or changes settings because of plugin output.

## Advice for plugin authors

- Declare `network = true` if you contact any online service, and say in your
  settings help exactly what you send and where.
- Keep secrets out of stdout, stderr, logs, and response data.
- Fail closed: on unexpected input, answer with an error or exit non-zero;
  a failure never damages stored annotations.
- Keep calls fast. Five seconds covers a network round trip, not a batch job.
