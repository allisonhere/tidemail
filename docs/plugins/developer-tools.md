# Plugin developer tools

TideMail includes headless commands for the contributor loop:

```text
scaffold → build → validate → test → install
```

They do not open Bubble Tea, connect to IMAP/SMTP, read the TideMail database,
or require configured accounts.

## Scaffold

```sh
tidemail plugin scaffold invoice-tagger
```

This creates a small dependency-free Go plugin with `plugin.toml`, `main.go`,
`go.mod`, `README.md`, and `.gitignore`. It refuses to overwrite an existing
directory. Use `--id` and `--name` when the directory ID and display name need
to differ:

```sh
tidemail plugin scaffold --id invoices --name "Invoice Tags" invoice-tagger
```

The generated plugin handles `ping` and `message.metadata`. Other languages
are supported as long as they implement the Plugin API v1 JSON protocol.

## Validate

```sh
tidemail plugin validate ./invoice-tagger
tidemail plugin validate ./invoice-tagger/plugin.toml
tidemail plugin validate --json ./invoice-tagger
```

Validation uses TideMail's production manifest parser and checks the API,
executable path and permissions, events, capabilities, and settings. Human
failures return exit code 1; usage/input errors return 2. JSON output contains
only the report and never secrets.

## Test

```sh
tidemail plugin test ./invoice-tagger
tidemail plugin test --metadata ./fixture.json ./invoice-tagger
tidemail plugin test --set mode=local ./invoice-tagger
tidemail plugin test --secret-env api_key=TYPESAFE_API_KEY ./invoice-tagger
tidemail plugin test --json ./invoice-tagger
```

The command runs a real API v1 `ping`, then metadata and
`message.received` checks when the manifest permits or declares them. The
same production protocol and annotation validators are used by TideMail at
runtime. A metadata fixture must match the fields in [protocol.md](protocol.md).

Secret values are read from environment variables and are never printed. The
default test path is offline and does not run `plugin.test`; use
`--config-test --allow-network` only when explicitly testing a plugin's remote
configuration check. API v1 is not an OS sandbox, so only test plugins you
trust.

`--timeout` accepts a positive duration up to one minute. `--verbose` adds
elapsed times and sanitized diagnostics. Both human and JSON test failures
return exit code 1; usage/input errors return 2.

## CI

A plugin project can use the built TideMail binary in CI:

```yaml
- run: tidemail plugin validate .
- run: tidemail plugin test .
```

Build the generated Go starter with `-buildvcs=false` when the checkout is a
source directory without Git metadata.
