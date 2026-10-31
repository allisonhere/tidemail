# Local Ripple search integration

The compose search feature is being developed with the local Ripple checkout.
TideMail's `go.mod` still pins the released Ripple version; no release or
permanent module replacement is included in this change. Build and test with a
temporary workspace until Ripple is published and the dependency pin is updated.

With the checkouts at `/home/allie/Projects/ripple` and
`/home/allie/Projects/tidemail`, create an external workspace:

```sh
mkdir -p /tmp/tidemail-search-workspace
cd /tmp/tidemail-search-workspace
go work init /home/allie/Projects/ripple /home/allie/Projects/tidemail
export GOWORK=/tmp/tidemail-search-workspace/go.work
cd /home/allie/Projects/tidemail
go test ./...
go run .
```

If that workspace already exists, reuse it rather than running `go work init`
again. The editor feature lives in Ripple; TideMail supplies the theme, compose
key routing, and persisted `display.compose_search_regex` setting.

Before delivering TideMail without the workspace, publish a Ripple version
containing search, update TideMail's Ripple requirement to it, and rerun the
checks with `GOWORK=off`. Publishing and pinning are a separate step.
