# Contributing to TideMail

Thanks for helping out! A few quick notes:

- **Architecture**: see [`docs/architecture.md`](docs/architecture.md) for the package map and key flows.
- **Before pushing**, run the same checks CI does:
  ```bash
  gofmt -w .
  go build ./...
  go vet ./...
  go test ./... -race
  golangci-lint run
  ```
  CI (`.github/workflows/ci.yml`) runs these on every push and pull request.
- **Lint** (`golangci-lint`) is enforced and the tree is clean — keep it that way by
  running `golangci-lint run` before pushing.
- **Error handling**: surface failures to the user (`setStatus`) or the fetch log rather
  than dropping them; mark deliberate ignores with `//nolint:errcheck`.
- **Tests**: prefer table-driven tests; the UI package sandboxes config/data dirs via
  `testmain_test.go`.
- **UI preview flags**: see [`docs/flags.md`](docs/flags.md), including the
  App Password-only Gmail preview.
- **Commits/releases**: releases are cut from `v*` tags via `deploy.sh`.
- **Nix flake**: no Nix knowledge is needed day to day. After a `go.mod` or
  `go.sum` change lands on `main`, the Nix workflow
  (`.github/workflows/nix.yml`) opens a `build(nix): update vendorHash` PR, and
  once a month it opens a `build(nix): update flake.lock` PR; merge them when
  they appear. Those PRs don't trigger CI on their own; close and reopen one to
  run it. Releases need no flake changes. To fix the hash locally instead, run
  `bash scripts/update-nix-vendor-hash.sh` (requires Nix). The one manual edit:
  when `go.mod` moves to a new Go minor version (e.g. 1.27), change both
  `go_1_26` references in `flake.nix` to match.
