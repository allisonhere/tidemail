# Contributing to TideMail

Thanks for helping out! A few quick notes:

- **Architecture**: see [`CLAUDE.md`](CLAUDE.md) for the package map and key flows.
- **Before pushing**, run the same checks CI does:
  ```bash
  gofmt -w .
  go build ./...
  go vet ./...
  go test ./... -race
  golangci-lint run
  ```
  CI (`.github/workflows/ci.yml`) runs these on every push and pull request.
- **Lint** uses `only-new-issues`, so you don't need to fix the pre-existing backlog —
  just keep your own changes clean. Burning down the backlog is welcome in its own PR.
- **Error handling**: surface failures to the user (`setStatus`) or the fetch log rather
  than dropping them; mark deliberate ignores with `//nolint:errcheck`.
- **Tests**: prefer table-driven tests; the UI package sandboxes config/data dirs via
  `testmain_test.go`.
- **Commits/releases**: releases are cut from `v*` tags via `deploy.sh`.
