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
