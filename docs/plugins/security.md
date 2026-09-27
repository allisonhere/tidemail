# Security and trust model

> **TideMail plugins are executable software and are not a complete OS
> sandbox. Only install plugins you trust.**

A plugin runs with the user's own privileges. It can read the user's files
and use the network regardless of its manifest. What TideMail controls is what
it *hands* a plugin, how it *runs* it, and what it *keeps* from the result.

## What TideMail hands a plugin

- Only the request: message metadata for metadata methods (see
  [protocol.md](protocol.md#messagemetadata)), and the plugin's own settings.
- Its own secrets, as `TIDEMAIL_SECRET_*` environment variables, and nothing
  else secret.
- Never message bodies, HTML, raw headers, attachments, AI summaries,
  database handles, account passwords, OAuth tokens, API keys, other
  plugins' data, TideMail's configuration, or file paths.
- Data only after the [permission checks](permissions.md), which run in
  TideMail's plugin runtime for every call.

## How TideMail runs a plugin

| Restriction | Detail |
| --- | --- |
| No shell | The command is executed directly. There is no command string to inject into. |
| Relative executable | `command` must stay inside the plugin directory; absolute paths and `..` are rejected. |
| No `$PATH` lookup | `command = "python3"` means the file in the plugin directory. |
| No arguments | Input arrives only on stdin. |
| Working directory | The plugin directory. |
| Trimmed environment | Only `PATH`, `HOME`, `USER`, `LANG`, `LC_ALL`, `LC_CTYPE`, `LC_MESSAGES`, `TMPDIR`, `TZ`, plus the plugin's own secrets. TideMail's own environment (client secrets, provider keys) is not passed on. |
| Timeout | 5 seconds per call, from start to exit. |
| Process-tree kill | On timeout, cancellation, or quit, the whole process group is killed (Unix), so child processes do not linger. |
| Output limit | stdout above 1 MiB fails the call. |
| stderr limit | 16 KiB kept, 512 characters quoted in errors. |
| Bounded concurrency | One process per plugin at a time, and at most 3 plugin processes for message calls overall. |

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
