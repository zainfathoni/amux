# Contributing to amux

Thanks for your interest in improving `amux`.

This project is currently a small CLI being prepared for broader open-source use. Contributions are welcome, but please keep changes small and aligned with the existing behavior unless an issue discusses a larger design change.

## Development setup

Requirements:

- Go, matching the version in `go.mod`
- `tmux`, for end-to-end/manual workflow checks
- Amp CLI, for manual checks that create or continue Amp threads

Build the CLI:

```sh
make build
```

Run tests:

```sh
go test ./...
```

Check formatting:

```sh
gofmt -l .
```

CI runs formatting, tests, a build, and a Destructive Command Guard scan.

## Pull request guidelines

- Keep pull requests focused on one behavior or documentation improvement.
- Add or update tests when changing CLI behavior, config parsing, tmux command construction, or release/build logic.
- Prefer existing package boundaries and helper patterns over new abstractions.
- Avoid committing local binaries, personal workspace config, real Amp thread URLs, or machine-specific paths.
- Update `README.md` when user-facing behavior changes.

## Manual testing tips

Use a temporary config while experimenting:

```sh
tmp=$(mktemp -d)
AMUX_WORKSPACES="$tmp/workspaces.tsv" amux list mac
```

Prefer `--dry-run` before mutating tmux or workspace config:

```sh
amux launch mac Amp --dry-run
amux spawn --dry-run demo ~/Code/demo "Start here"
```

Do not use real private thread IDs in tests or examples. Use placeholders such as `T-example` or `https://ampcode.com/threads/T-example`.
