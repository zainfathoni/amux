# amux

`amux` restores named [Amp](https://ampcode.com/) workspaces inside tmux from a small TSV config file.

It is built for people who keep long-running Amp threads around while moving between projects. Instead of manually reopening tmux windows and continuing threads, you store the windows you care about and let `amux` restore them later.

Website: [amux.zainf.dev](https://amux.zainf.dev)

## Status

`amux` is currently a small personal workflow tool being prepared for broader open-source use. The core CLI is tested, but the defaults still reflect the author's setup:

- default workspace: `mac`
- default tmux session: `Amp`
- default config path: `~/.config/amp-tmux/workspaces.tsv`
- fallback terminal launching is tuned for Omarchy/Alacritty environments

The project is public-source friendly, but not yet a polished cross-platform product.

## Features

- Restore Amp threads into named tmux windows.
- Store or remove the current tmux window from the restore config.
- Spawn a fresh Amp thread in a new tmux window.
- Tear down an `amux spawn` worker from its injected identity.
- Validate planned restore actions with `--dry-run`.
- Inspect config/live tmux drift with `doctor`.
- Build versioned release artifacts through GitHub Actions.

## Requirements

- Go, for building from source.
- `tmux`, for workspace/session management.
- Amp CLI, for continuing and creating Amp threads.

Optional:

- Omarchy/Alacritty, used only as fallback terminal launchers when `amux` is asked to attach but the caller is not an interactive terminal.

## Install from a release

Download the archive for your platform from the [latest release](https://github.com/zainfathoni/amux/releases/latest), verify its checksum, and install the binary somewhere on your `PATH`:

```sh
curl -LO https://github.com/zainfathoni/amux/releases/latest/download/amux-linux-amd64.tar.gz
curl -LO https://github.com/zainfathoni/amux/releases/latest/download/amux-linux-amd64.tar.gz.sha256
sha256sum -c amux-linux-amd64.tar.gz.sha256
tar -xzf amux-linux-amd64.tar.gz
install -m 0755 amux-linux-amd64/amux ~/.local/bin/amux
```

Release archives are published for Linux and macOS on amd64 and arm64.

To let `amux` manage future updates itself, install the binary to a user-owned
path such as `~/.local/bin/amux` and keep that directory on your `PATH`.
Package-managed locations such as the Nix store or Homebrew Cellar are treated
as immutable; `amux self-update` refuses to replace binaries from those paths.

Update a user-local release install with:

```sh
amux self-update
```

Preview the update without replacing the binary:

```sh
amux --dry-run self-update
```

## Install from source

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

- tag releases use the tag name, for example `v0.1.1`
- `main` branch CI builds use `main.<github-run-number>` so every main build has a unique version
- pull request CI builds use `pr.<pull-request-number>.<github-run-number>`
- local scripted builds use `dev.<short-sha>` unless `VERSION=...` is provided
- `commit` is the short commit SHA
- `built` is the UTC build time, or `SOURCE_DATE_EPOCH` converted to UTC when set

## Quick start

Create a config file:

```sh
mkdir -p ~/.config/amp-tmux
cat > ~/.config/amp-tmux/workspaces.tsv <<'EOF'
# workspace	window	workdir	thread-id-or-url
mac	my-project	~/Code/my-project	https://ampcode.com/threads/T-example
EOF
```

Preview the restore plan:

```sh
amux launch mac Amp --dry-run
```

Restore the workspace:

```sh
amux launch mac Amp
```

Store the current tmux window for future restores:

```sh
amux store-current https://ampcode.com/threads/T-example
```

Remove the current tmux window from future restores:

```sh
amux remove-current
```

## Commands

```sh
amux                         # launch default mac/Amp workspace; auto-attach if already restored
amux launch [workspace] [session]
amux --attach launch mac Amp
amux --no-attach launch mac Amp
amux launch mac Amp --dry-run
amux list [workspace]
amux store <workspace> <window> <workdir> <thread-id-or-url>
amux store-current <thread-id-or-url>
amux store-current <workspace> <thread-id-or-url> [window] [workdir]
amux remove <workspace> <window>
amux remove-current [workspace]
amux park-current [workspace]
amux spawn [--mode <mode> | -m <mode>] <window> <workdir> <initial-message> [workspace] [session]
amux teardown
amux version
amux self-update
amux path
amux doctor [workspace] [session]
```

`amux spawn --mode <mode>` (or `-m <mode>`) creates the new Amp thread with the selected Amp mode. Omitting `--mode` preserves the default Amp thread behavior.

`amux spawn` injects a stable identity contract into the spawned Amp process: `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`. From that spawned process, no-arg `amux teardown` verifies the `AMUX_WORKSPACE`/`AMUX_SESSION`/`AMUX_WINDOW`/`AMUX_THREAD_ID` identity against the restore config and live tmux window, archives the matching Amp thread, removes the restore row, and stops the uniquely matched tmux window. If the identity, config row, or tmux window is missing, mismatched, or ambiguous, teardown refuses to archive or stop anything.

`amux` keeps three side-effect domains separate:

- **Restore config**: rows in `workspaces.tsv` that describe what should be restored later.
- **Live local tmux/Amp**: tmux sessions/windows and the local Amp CLI processes running inside them.
- **Remote Amp thread state**: hosted Amp threads, including creation by `spawn` and archival by verified `teardown`.

Command side effects:

| Command | Restore config | Live local tmux/Amp | Remote Amp thread state |
| --- | --- | --- | --- |
| `launch` | Read only | Creates missing tmux windows/processes | Read/continue existing threads only |
| `list`, `path`, `version`, `doctor` | Read only | Inspect only | No change |
| `store`, `store-current` | Add or replace rows | No change | No change |
| `remove`, `remove-current` | Remove rows | No change | No change |
| `park-current` | Remove current-window row | Gracefully stop the current local tmux/Amp window | No change; Amp thread history is not archived or deleted |
| `spawn` | Store the new row | Create/select a tmux window and submit the initial message | Create a new Amp thread, optionally with `--mode` |
| `teardown` | Remove the verified spawned row | Stop the verified tmux window | Archive the verified `AMUX_THREAD_ID` |

`amux doctor [workspace] [session]` is read-only and compares the selected workspace against the selected live tmux session. Omitting the session preserves the default `Amp` behavior, so `amux doctor mac` remains equivalent to `amux doctor mac Amp`.

For `launch` and `spawn`, `--dry-run` validates inputs and checks tmux window conflicts without mutating state. For `spawn`, dry-run does not create an Amp thread, create tmux windows, send keys, or update `workspaces.tsv`; it only prints the intended actions, including the selected mode when provided.

Launch uses auto-attach by default: cold restores create the tmux session and return, while an already-running session attaches only when its live window set and pane paths match the configured workspace. Use `--attach` to always attach after restoring, or `--no-attach` to never attach.

When launch attaches from inside an existing tmux client, `amux` switches that client to the target session. From a normal interactive terminal, it attaches in-place. If tmux reports that the caller is not a terminal, `amux` opens the target session through Omarchy's terminal launcher, with direct Alacritty fallback.

`park-current` removes the current window from restore config, schedules a delayed graceful terminal shutdown sequence for the target pane, then returns immediately. This gives Amp time to receive the command result and send a final response before the local process exits. The delayed shutdown only force-closes the tmux window if graceful stop times out. Parking is local cleanup only; use `teardown` from an `amux spawn` worker when you intentionally want to archive that verified remote Amp thread too.

## Configuration

Defaults:

- workspace: `mac`
- session: `Amp`
- config: `~/.config/amp-tmux/workspaces.tsv`

Override the config path with either `--config <path>` or `AMP_TMUX_WORKSPACES`.

The TSV format is:

```text
workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
```

Example:

```text
# workspace	window	workdir	thread-id-or-url
mac	my-project	~/Code/my-project	https://ampcode.com/threads/T-example
mac	docs	~/Code/docs	T-docs-example
```

Do not store secrets in workspace names, window names, workdirs, or thread identifiers. Treat the config as shareable operational metadata, not a secret store.

## Agent skill

The optional Amp skill source lives in this repository at:

```text
skills/amux/SKILL.md
```

Install or refresh the local skill symlink with:

```sh
ln -sfn "$PWD/skills/amux" ~/.agents/skills/amux
```

## Development

Run the test suite:

```sh
go test ./...
```

Run the same build script CI uses:

```sh
make build
```

Check formatting:

```sh
gofmt -l .
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

## Release

GitHub publishes release artifacts when a pushed tag matches `v*`:

```sh
git tag -a v0.1.1 -m "v0.1.1"
git push origin v0.1.1
```

The tag push starts the Release workflow. The workflow builds platform archives and injects the tag name as the `amux version` value.
Each release publishes versioned artifacts such as `amux-v0.1.1-linux-amd64.tar.gz` and stable aliases such as `amux-linux-amd64.tar.gz` for `releases/latest/download` links.

The standalone `amux` repository owns the installed `~/.local/bin/amux` binary.
Dotfiles or machine-restore repositories should restore the workspace TSV and
ensure `~/.local/bin` is on `PATH`, but should not track the compiled binary or
install `amux` through an immutable package-manager store if self-update is the
desired update flow.

## Roadmap

- Better installation path, such as packaged release instructions or a package manager tap.
- Shell completions.
- More portable attach/open-terminal behavior outside the author's environment.
- Expanded examples for common Amp/tmux workflows.
- Config migration/versioning if the TSV contract changes.

## License

`amux` is available under the [MIT License](LICENSE).
