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
amux path
amux doctor
```

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
go build -o amux ./cmd/amux
install -m 0755 amux ~/.local/bin/amux
```

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
