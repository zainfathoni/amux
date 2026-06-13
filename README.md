# amux

Amp Multiplexer: a small CLI for restoring Amp tmux workspaces from a TSV config.

## Current contract

```sh
amux                         # launch default mac/Amp workspace
amux launch [workspace] [session]
amux launch mac Amp --dry-run
amux list [workspace]
amux store <workspace> <window> <workdir> <thread-id-or-url>
amux store-current <thread-id-or-url>
amux store-current <workspace> <thread-id-or-url> [window] [workdir]
amux remove <workspace> <window>
amux remove-current [workspace]
amux park-current [workspace]
amux spawn <window> <workdir> <initial-message> [workspace] [session]
amux version
amux path
amux doctor
```

`--dry-run` validates inputs and checks tmux window conflicts without mutating state. For `spawn`, dry-run does not create an Amp thread, create tmux windows, send keys, or update `workspaces.tsv`; it only prints the intended actions.

`park-current` removes the current window from restore config, sends a graceful terminal shutdown sequence to the target pane, waits briefly for the local Amp process to exit, and only force-closes the tmux window if the graceful stop times out.

Defaults:

- workspace: `mac`
- session: `Amp`
- config: `~/.config/amp-tmux/workspaces.tsv`

Override the config path with either `--config <path>` or `AMP_TMUX_WORKSPACES`.

The TSV format is:

```text
workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
```

## Install

Build and install the CLI from this repository:

```sh
make build
install -m 0755 amux ~/.local/bin/amux
```

`make build` writes `./amux` by default. Override the output path with `BUILD_OUTPUT`:

```sh
BUILD_OUTPUT=/tmp/amux make build
```

Builds made through `make build` or `scripts/build-amux.sh` inject version metadata into `amux version`:

- tag releases use the tag name, for example `v0.1.0`
- `main` branch CI builds use `main.<github-run-number>` so every main build has a unique version
- pull request CI builds use `pr.<pull-request-number>.<github-run-number>`
- local scripted builds use `dev.<short-sha>` unless `VERSION=...` is provided
- `commit` is the short commit SHA
- `built` is the UTC build time, or `SOURCE_DATE_EPOCH` converted to UTC when set

## Release

GitHub publishes release artifacts when a pushed tag matches `v*`:

```sh
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

The tag push starts the Release workflow. The workflow builds platform archives and injects the tag name as the `amux version` value.

The standalone `amux` repository owns the installed `~/.local/bin/amux` binary. Dotfiles or machine-restore repositories should restore the workspace TSV, but should not track the compiled binary.

## Agent skill

The Amp skill source lives in this repository at:

```text
skills/amux/SKILL.md
```

Install or refresh the local skill symlink with:

```sh
ln -sfn "$PWD/skills/amux" ~/.agents/skills/amux
```
