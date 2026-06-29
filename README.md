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
amux spawn <window> <workdir> <initial-message> [workspace] [session]
amux version
amux path
amux doctor
```

`--dry-run` validates inputs and checks tmux window conflicts without mutating state. For `spawn`, dry-run does not create an Amp thread, create tmux windows, send keys, or update `workspaces.tsv`; it only prints the intended actions.

Launch uses auto-attach by default: cold restores create the tmux session and return, while an already-running session attaches only when its live window set and pane paths match the configured workspace. Use `--attach` to always attach after restoring, or `--no-attach` to never attach.

When launch attaches from inside an existing tmux client, `amux` switches that client to the target session. From a normal interactive terminal, it attaches in-place. If tmux reports that the caller is not a terminal, `amux` opens the target session through Omarchy's terminal launcher, with direct Alacritty fallback.

`park-current` removes the current window from restore config, schedules a delayed graceful terminal shutdown sequence for the target pane, then returns immediately. This gives Amp time to receive the command result and send a final response before the local process exits. The delayed shutdown only force-closes the tmux window if graceful stop times out.

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

The standalone `amux` repository owns the installed `~/.local/bin/amux` binary. Dotfiles or machine-restore repositories should restore the workspace TSV, but should not track the compiled binary.

## Roadmap

- Better installation path, such as packaged release instructions or a package manager tap.
- Shell completions.
- More portable attach/open-terminal behavior outside the author's environment.
- Expanded examples for common Amp/tmux workflows.
- Config migration/versioning if the TSV contract changes.

## License

`amux` is available under the [MIT License](LICENSE).
