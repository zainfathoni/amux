# amux

`amux` is the local tmux workspace and lifecycle layer for [Amp](https://ampcode.com/). It restores named Amp workspaces from a small TSV config file, and gives agents safe commands for pinning, parking, spawning, and tearing down work.

It is built for people who keep long-running Amp threads and local Amp processes around while moving between projects. Instead of manually reopening tmux windows and continuing threads, you store the windows you care about and let `amux` restore them later.

Website: [amux.zainf.dev](https://amux.zainf.dev)

## Status

`amux` is an early public release. The core CLI is tested and used daily, but a few defaults still reflect the author's setup:

- default workspace: `mac`
- default tmux session: `Amp`
- default config path: `~/.config/amux/workspaces.tsv`
- fallback terminal launching is tuned for Omarchy/Alacritty environments

If you already use Amp in tmux, the main workflow is ready to try. Start with `--dry-run`, keep the bundled `/amux` skill installed for agent operation, and file issues for rough edges.

## Features

- Complement Amp Agents Anywhere with a local tmux workspace layer.
- Restore Amp threads into named tmux windows.
- Store and launch per-directory `amp --no-tui` runner intent separately from thread rows.
- Pin or unpin the current tmux window in the restore config.
- Spawn a fresh Amp thread in a new tmux window.
- Tear down an `amux spawn` worker from its injected identity.
- Validate planned restore actions with `--dry-run`.
- Inspect config/live tmux drift with `doctor`.
- Build versioned release artifacts through GitHub Actions.

## Amp Agents Anywhere

Amp's [Agents Anywhere](https://ampcode.com/news/agents-anywhere) gives Amp first-party remote thread creation and per-directory runner mode with `amp --no-tui`. `amux` does not replace that. It manages the local tmux side of an Amp workflow: named windows, restore config, live-local parking, verified teardown, and an agent skill that keeps those side effects explicit.

`amux launch` restores known Amp threads with `amp threads continue <thread-id-or-url>`, and `amux spawn` creates an interactive tmux-backed worker with a known thread identity. `amux runner ...` commands manage first-class `amp --no-tui` runner intent separately in `runners.tsv` so runner rows are not confused with thread restore rows.

## Requirements

- `tmux`, for workspace/session management.
- Amp CLI, for continuing and creating Amp threads.

For building from source, you also need Go.

Optional:

- Omarchy/Alacritty, used only as fallback terminal launchers when `amux` is asked to attach but the caller is not an interactive terminal.

## Install from a release

### Homebrew

On macOS or Linux, install the release binary with Homebrew:

```sh
brew tap zainfathoni/amux https://github.com/zainfathoni/amux
brew install zainfathoni/amux/amux
```

Update Homebrew installs with Homebrew, not `amux update` or `amux self-update`:

```sh
brew update
brew upgrade amux
```

Homebrew installs live under Homebrew-managed Cellar paths, which are treated as
immutable. `amux update` refuses to replace binaries from those paths so package
management stays owned by Homebrew.

### Manual release archive

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
If multiple `amux` binaries exist on `PATH`, self-update warns when the binary
it updates is shadowed by another install.

Update a user-local release install with:

```sh
amux update
```

Preview the update without replacing the binary:

```sh
amux update --dry-run
```

The older `amux self-update` command remains available as a compatibility alias.

## Shell completions

`amux` can print static shell completions for bash, zsh, and fish. These cover
commands, aliases, flags, runner subcommands, and common positional forms. They
do not inspect tmux sessions, configured workspaces, or Amp threads.

Install bash completions:

```sh
mkdir -p ~/.local/share/bash-completion/completions
amux completion bash > ~/.local/share/bash-completion/completions/amux
```

Install zsh completions:

```sh
mkdir -p ~/.zfunc
amux completion zsh > ~/.zfunc/_amux
```

Then ensure `~/.zfunc` is on your `fpath`, for example in `~/.zshrc`:

```sh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

Install fish completions:

```fish
mkdir -p ~/.config/fish/completions
amux completion fish > ~/.config/fish/completions/amux.fish
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
mkdir -p ~/.config/amux
cat > ~/.config/amux/workspaces.tsv <<'EOF'
# workspace	window	workdir	thread-id-or-url
mac	my-project	~/Code/my-project	https://ampcode.com/threads/T-example
EOF
```

Use a real Amp thread ID or thread URL from your own Amp history in place of
`https://ampcode.com/threads/T-example`.

Preview the restore plan:

```sh
amux launch mac --dry-run
```

Restore the workspace:

```sh
amux launch mac
```

Pin the current tmux window in the restore config for future restores:

```sh
amux pin-current https://ampcode.com/threads/T-example
```

Unpin the current tmux window from the restore config without stopping it:

```sh
amux unpin-current
```

## Commands

```sh
amux                         # launch default mac/Amp workspace; auto-attach if already restored
amux launch [workspace] [session] # one workspace arg also selects the same-named tmux session
amux --attach launch mac Amp
amux --no-attach launch mac Amp
amux --terminal-launcher "kitty -e" --attach launch mac Amp
amux launch mac --dry-run
amux list [--status] [--active|--shelved] [workspace]
amux shelved [workspace]
amux pin <workspace> <window> <workdir> <thread-id-or-url>
amux pin-current <thread-id-or-url>
amux pin-current <workspace> <thread-id-or-url> [window] [workdir]
amux unpin <workspace> <window>
amux unpin-current [workspace]
amux park [workspace] <window>
amux park-current [workspace]
amux shelve-current [workspace] [thread-id-or-url]
amux shelve [workspace] <window> [session]
amux shelve --thread <thread-id-or-url> [--session <session>]
amux shelve --workspace <workspace> [--session <session>]
amux unshelve [workspace] <window>
amux unshelve --thread <thread-id-or-url>
amux unshelve --workspace <workspace>
amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] <window> <workdir> <initial-message> [workspace] [session]
amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] --message-file <path> <window> <workdir> [workspace] [session]
amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] --message-stdin <window> <workdir> [workspace] [session]
amux teardown
amux teardown --thread <thread-id-or-url> [--session <session>]
amux teardown <workspace> <window> [session]
amux prune-archived [workspace]
amux workspaces [--include-runners]
amux runner list [workspace]
amux runner pin <workspace> <window> <workdir>
amux runner unpin <workspace> <window>
amux runner launch [workspace] [session]
amux runner park [workspace] <window>
amux completion <bash|zsh|fish>
amux version
amux update
amux path
amux doctor [workspace] [session]
```

Compatibility aliases remain available: `store` for `pin`, `store-current` for `pin-current`, `remove` for `unpin`, `remove-current` for `unpin-current`, and `self-update` for `update`.

`amux spawn --mode <mode>` (or `-m <mode>`) creates the new Amp thread with the selected Amp mode. Omitting `--mode` preserves the default Amp thread behavior.

The positional `amux spawn ... <initial-message>` remains intentionally single-line: tabs and newlines are rejected before any Amp thread is created. For structured prompts, use `--message-file prompt.md` or `--message-stdin < prompt.md`; these explicit sources allow multi-line content, are mutually exclusive with each other and with positional `<initial-message>`, and are read and validated before `spawn` creates the new Amp thread. Multi-line content is delivered through a tmux paste buffer with bracketed paste enabled when the Amp composer requests it, then verified against the newly created Amp thread before the restore row is stored. Prompts larger than 1 MiB are rejected.

`amux spawn --title-prefix <prefix>` names the spawned tmux window `"<prefix> <window>"` and renames only the newly created Amp thread to that same name after the initial message is submitted, so Amp sees a non-empty thread. For issue-oriented work, use an explicit prefix such as `--title-prefix '#255'` to get a tmux window, restore row, `AMUX_WINDOW`, and Amp thread title like `#255 worker-window`. If the Amp thread rename fails after the worker is created, `spawn` reports a warning with a retry command and leaves the created/stored worker intact. Omitting `--title-prefix` preserves the existing spawn behavior and does not rename any Amp thread or prefix the tmux window.

`amux spawn` injects a stable identity contract into the spawned Amp process: `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`. From that spawned process, no-arg `amux teardown` verifies the `AMUX_WORKSPACE`/`AMUX_SESSION`/`AMUX_WINDOW`/`AMUX_THREAD_ID` identity against the restore config and live tmux window, archives the matching Amp thread, removes the restore row, and stops the uniquely matched tmux window. If the identity, config row, or tmux window is missing, mismatched, or ambiguous, teardown refuses to archive or stop anything.

`amux list [workspace]` prints local restore rows only, without calling Amp, so it remains instant even with many remote threads. Use `amux list --status [workspace]` when you want a trailing `status` column: `active` when the thread is in Amp's active list, `shelved` when it is archived remotely but preserved in `workspaces.tsv`, `missing` when Amp confirms it is in neither active nor archived lists, and `unknown` when Amp thread status cannot be read. Use `amux list --active [workspace]` for only confirmed active rows, `amux list --shelved [workspace]` or `amux shelved [workspace]` for only confirmed shelved rows. Filtered listing fails closed if Amp status is unavailable instead of guessing.

`amux workspaces` prints unique workspace names from `workspaces.tsv`, sorted one per line, without calling Amp or tmux and without creating missing config files. It intentionally excludes runner-only workspaces by default because interactive health checks target restore workspaces. Use `amux workspaces --include-runners` when machine-level inventory should also include workspace names that exist only in `runners.tsv`.

`amux` keeps four side-effect domains separate:

- **Restore config**: rows in `workspaces.tsv` that describe what should be restored later.
- **Runner config**: rows in `runners.tsv` that describe local `amp --no-tui` runners to keep restorable by workspace/window/workdir.
- **Live local tmux/Amp**: tmux sessions/windows and the local Amp CLI processes running inside them.
- **Remote Amp thread state**: hosted Amp threads, including creation by `spawn` and archival by verified `teardown`.

Command side effects:

| Command | Restore config | Runner config | Live local tmux/Amp | Remote Amp thread state |
| --- | --- | --- | --- | --- |
| `launch` | Read only | No change | Creates missing thread tmux windows/processes for unshelved rows only | Inspect archive state and continue active threads; skips shelved/archived rows |
| `list` / `shelved` | Read only | No change | Inspect only | Plain `list` is local-only; `list --status`, filtered list, and `shelved` inspect remote thread status |
| `workspaces` | Read only | Inspect only with `--include-runners` | No change | No change |
| `path`, `version` | Read only | No change | Inspect only | No change |
| `doctor` | Read only | Read only | Inspect only | Inspect only |
| `pin`, `pin-current` (`store`, `store-current`) | Add or replace rows | No change | No change | No change |
| `unpin`, `unpin-current` (`remove`, `remove-current`) | Remove rows | No change | No change | No change |
| `park`, `park-current` | No change; rows are preserved for future restore | No change | Gracefully stop the resolved local tmux/Amp window | No change; Amp thread history is not archived or deleted |
| `shelve-current` | Add or update the current window's restore row before archiving | No change | Stop the current tmux/Amp window | Archive the identified current Amp thread so it leaves the Amp sidebar |
| `shelve` | No change; rows are preserved for future restore | No change | Stop verified matching local tmux/Amp windows when present | Archive the selected thread, thread row, or workspace's threads so they leave the Amp sidebar |
| `unshelve` | No change; rows are preserved for future restore | No change | No change | Unarchive the selected thread, thread row, or workspace's threads |
| `spawn` | Store the new row under the final window name | No change | Create/select a tmux window and submit the initial message | Create a new Amp thread, optionally with `--mode`; optionally rename the new thread with `--title-prefix` |
| `teardown` | Remove the verified row | No change | Stop the verified tmux window | Archive the verified thread |
| `prune-archived` | Remove rows whose threads are confirmed archived | No change | No change | Inspect only; does not archive/delete threads |
| `runner pin`, `runner unpin` | No change | Add/replace/remove runner rows | No change | No change |
| `runner launch` | No change | Read only | Creates missing `amp --no-tui` runner windows | No change |
| `runner park` | No change | No change; rows are preserved for future restore | Gracefully stop the resolved local runner window | No change |

Compatibility decision: keep workspace-named sessions when a workspace is explicitly provided and the session is omitted. For `launch`, `doctor`, and `runner launch`, `amux <command> amux` uses workspace/session `amux`. For `spawn`, the optional trailing workspace does the same: `amux spawn worker ~/Code/repo "prompt" amux` or `amux spawn --message-file prompt.md worker ~/Code/repo amux`. For workspace-based `shelve`, use `amux shelve amux worker` or `amux shelve --workspace amux`; for explicit `teardown`, use `amux teardown amux worker`. This is the preferred layout for new per-workspace sessions. Older shared-session layouts remain supported by passing the session explicitly, for example `amux launch mac Amp`, `amux spawn worker ~/Code/repo "prompt" mac Amp`, `amux spawn --message-file prompt.md worker ~/Code/repo mac Amp`, `amux shelve mac worker Amp`, `amux shelve --workspace mac --session Amp`, `amux teardown mac worker Amp`, `amux runner launch mac Amp`, or `amux doctor mac Amp`. With no workspace argument where the command supports one, the compatibility default is still workspace `mac` and session `Amp`.

`amux doctor [workspace] [session]` is read-only and compares the selected workspace against the selected live tmux session. It also reports restore rows whose Amp threads are confirmed archived or missing, and runner registry drift when `runners.tsv` is present.

For `launch` and `spawn`, `--dry-run` validates inputs and checks tmux window conflicts without mutating state. For `spawn`, dry-run does not create or rename an Amp thread, create tmux windows, send keys, or update `workspaces.tsv`; it only prints the intended actions, including the selected mode and planned title rename when provided.

Launch uses auto-attach by default: cold restores create the tmux session and return, while an already-running session attaches only when its live window set and pane paths match the configured workspace. Use `--attach` to always attach after restoring, or `--no-attach` to never attach.

When launch attaches from inside an existing tmux client, `amux` switches that client to the target session. From a normal interactive terminal, it attaches in-place. If tmux reports that the caller is not a terminal, `amux` opens the target session through the configured terminal launcher, then Omarchy's terminal launcher, then direct Alacritty fallback. Configure the first launcher with `--terminal-launcher "kitty -e"` or `AMUX_TERMINAL_LAUNCHER="kitty -e"`; `amux` appends `tmux attach -t <session>` to the launcher command. Quoted launcher arguments are supported. Without that option or env var, the existing Omarchy/Alacritty order is unchanged.

`park [workspace] <window>` and `park-current [workspace]` are live-local-only. They resolve the intended live tmux window, schedule a delayed graceful terminal shutdown sequence for the target pane, then return immediately. This gives Amp time to receive the command result and send a final response before the local process exits. The delayed shutdown only force-closes the tmux window if graceful stop times out. Parking preserves restore config rows and never archives the remote Amp thread. Use `unpin`/`unpin-current` when you only want to stop restoring a row, `shelve-current` or `shelve` when you want to hide/defer a thread while keeping it restorable, and `teardown` when you intentionally want to archive the verified remote Amp thread, remove the row, and stop the local window.

`shelve-current [workspace] [thread-id-or-url]` is the current-window path for live tmux/Amp work that may not be pinned yet. Run it from the pane you want to defer; it derives the current window and workdir, requires an explicit thread ID/URL unless `AMUX_THREAD_ID` is already set, writes or preserves a useful restore row, archives the thread so it leaves the Amp sidebar, and stops the current tmux window. It refuses to guess a thread and does not replace an existing same workspace/window row that points at a different thread.

`shelve` is deferral without forgetting for existing restore rows. It archives Amp thread(s) so they leave the Amp sidebar, preserves the restore row(s), and stops verified matching local tmux/Amp windows when they are live. Target one row with `amux shelve <workspace> <window> [session]`, one thread regardless of workspace with `amux shelve --thread <thread-id-or-url> [--session <session>]`, or every row in a workspace with `amux shelve --workspace <workspace> [--session <session>]`. Workspace-based shelving uses the workspace-named session unless a session is passed; thread shelving searches all tmux sessions unless `--session` is provided. If the current live window is unpinned, `shelve` fails closed and points you to `shelve-current` or `pin-current` rather than treating live tmux state as enough to archive. `amux launch <workspace> [session]` skips shelved rows; run `amux unshelve <workspace> <window>`, `amux unshelve --thread <thread-id-or-url>`, or `amux unshelve --workspace <workspace>` explicitly before launching deferred work again.

`teardown` is explicit full lifecycle cleanup: archive the verified Amp thread, remove the restore row, and stop the uniquely verified local tmux window. With no args it only runs from an `amux spawn` worker that has matching `AMUX_*` identity. From a restored worker that does not have `AMUX_*` but whose thread is stored and live, use `amux teardown --thread <thread-id-or-url> [--session <session>]`; it resolves the restore row by thread, then cross-checks the live tmux start command before mutating anything. From outside the worker when you know the row, use `amux teardown <workspace> <window> [session]`; if `[session]` is omitted, it defaults to the workspace name. All teardown forms fail closed if the target is missing, mismatched, or ambiguous.

`prune-archived [workspace]` is explicit stale-restore cleanup. It removes confirmed archived rows only when you truly want to forget them; archived rows may also represent intentionally shelved work. Active rows are kept; missing threads, Amp CLI failures, or unreadable thread-list output fail closed without changing config. Unlike `teardown`, it does not archive/delete remote threads or stop live tmux windows.

`amux runner ...` commands manage local runner intent for Amp Agents Anywhere. Runner rows live in `runners.tsv` next to `workspaces.tsv` and use `workspace<TAB>window<TAB>workdir`; they intentionally contain no thread ID. `amux runner launch [workspace] [session]` starts configured runners with `amp --no-tui` inside tmux windows and refuses to reuse an existing same-name window; with one workspace arg, it uses the same name for the tmux session. `amux runner park [workspace] <window>` stops only the live local runner window while preserving runner config. Runner commands never create, continue, archive, or list remote Amp threads.

## Post-merge worker cleanup

`amux teardown` intentionally does not merge PRs, publish releases, remove git worktrees, or delete branches. For a finished worker, use the bundled `/amux` skill's **Finish a merged worker** workflow so agents perform the broader GitHub/git lifecycle before the final amux cleanup:

1. Verify the PR is merged with `gh pr view <pr-number> --json merged,mergeCommit,headRefName,url`.
2. If a release is expected, make the release type or tag explicit, update `main`, confirm the tag does not already exist, then create/push the tag and watch the release workflow.
3. Remove the worker worktree only after the worker worktree is clean, the PR is merged, and the main worktree is updated; do not force-remove dirty worktrees as routine cleanup.
4. Delete local and remote worker branches only when they are confirmed merged, confirmed merged by the PR, or already deleted by the PR merge; prefer `git branch -d`, not `-D`, and require explicit confirmation before force-deleting a squash-merged local branch.
5. Run `amux teardown` or `amux teardown --thread <thread-id-or-url> [--session <session>]` last, after git/GitHub cleanup is complete.

This differs from parking and shelving: `park` stops only the live local tmux/Amp process while preserving the restore row and active remote thread; `shelve` archives/hides deferred Amp threads while preserving restore rows; `teardown` archives the verified thread, removes the row, and stops the verified local window.

## Configuration

Defaults:

- workspace: `mac`
- session: `Amp`
- when a workspace is explicitly passed without a session, that workspace name is used as the tmux session
- config: `~/.config/amux/workspaces.tsv`
- runner config: `~/.config/amux/runners.tsv`

Override the config path with either `--config <path>` or `AMUX_WORKSPACES`. The legacy `AMP_TMUX_WORKSPACES` variable remains supported for older installs and scripts.

Override the non-interactive attach terminal launcher with either `--terminal-launcher <command>` or `AMUX_TERMINAL_LAUNCHER`. The launcher command is parsed into shell-style words and `amux` appends `tmux attach -t <session>`. For example, `AMUX_TERMINAL_LAUNCHER="foot -e"` launches `foot -e tmux attach -t <session>`. If the configured launcher is unset or fails to start, `amux` keeps the default fallback order: Omarchy (`uwsm-app -- xdg-terminal-exec -e`) when available, then `alacritty -e`.

Older amux releases used `~/.config/amp-tmux`. Current amux uses `~/.config/amux` and automatically copies `workspaces.tsv`, `runners.tsv`, and future config files from the legacy directory when the new files do not exist. The old directory is left in place for rollback and older binaries. Run `amux migrate-config` explicitly to perform the same copy and print the resolved path.

The TSV format is:

```text
workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
```

Runner TSV format is separate:

```text
workspace<TAB>window<TAB>workdir
```

Example:

```text
# workspace	window	workdir	thread-id-or-url
mac	my-project	~/Code/my-project	https://ampcode.com/threads/T-example
mac	docs	~/Code/docs	T-docs-example
```

Example runner config:

```text
# workspace	window	workdir
mac	my-project-runner	~/Code/my-project
```

Do not store secrets in workspace names, window names, workdirs, or thread identifiers. Treat the config as shareable operational metadata, not a secret store.

## Agent skill

`amux` is designed to be agent-operated. For best results, install the bundled Amp skill before asking agents to manage sessions. The skill teaches agents the safe command vocabulary: when to **pin** restore config, **park** only a live local tmux/Amp session, **teardown** a verified worker, and run `doctor` before guessing.

Install or refresh the skill globally:

```sh
ln -sfn "$PWD/skills/amux" ~/.agents/skills/amux
```

Run that command from a checkout of this repository. If you installed only a
release archive, clone the repo or copy the `skills/amux` directory first.

Then reload or restart Amp if needed so the skill index picks up the new skill.

After installing it, ask Amp for the `/amux` skill or use natural trigger phrases:

```text
Pin it                 -> amux pin-current <thread-id-or-url>
Unpin it               -> amux unpin-current
Park it                -> amux park-current
Shelve this            -> amux shelve-current [workspace] [thread-id-or-url] / amux shelve <workspace> <window> / --thread / --workspace
Show shelved work      -> amux shelved [workspace] / amux list --shelved [workspace]
Unshelve this          -> amux unshelve <workspace> <window> / --thread / --workspace
Restore my workspace   -> amux launch
Spawn a worker for ... -> amux spawn [--mode <mode>] [--title-prefix <prefix>] ...
/amux sprawl #1 #2     -> skill-only fan-out after issue dependency inspection; not a CLI command
/amux finish           -> post-merge GitHub/git cleanup, then final amux teardown
Teardown this worker   -> amux teardown / teardown --thread / teardown <workspace> <window>
Doctor amux            -> amux doctor
```

`/amux sprawl` is a higher-level skill workflow for independent issue work: the agent reads each requested issue, refuses to fan out dependent or likely-conflicting issues, creates one issue-scoped worktree/branch per independent issue from current `origin/main`, and then uses `amux spawn --title-prefix '#<issue>'` to start one restorable worker per worktree. Completed spawned workers should use `/amux finish` / the skill's post-merge finish workflow so GitHub/git cleanup happens before final `amux teardown`.

The skill source lives at [`skills/amux/SKILL.md`](skills/amux/SKILL.md). Keep it in sync with command semantics when adding new lifecycle behavior; for agent use, the skill is part of the product surface, not just documentation.

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
After the release assets are published, the workflow updates `Formula/amux.rb`
on `main` with the new tag and versioned archive checksums so Homebrew users can
upgrade with `brew upgrade amux`. Homebrew-managed installs should continue to
use `brew upgrade`; the in-binary updater is only for user-writable manual
release installs.

The standalone `amux` repository owns the installed `~/.local/bin/amux` binary.
Dotfiles or machine-restore repositories should restore the workspace TSV and
ensure `~/.local/bin` is on `PATH`, but should not track the compiled binary or
install `amux` through an immutable package-manager store if self-update is the
desired update flow.

## Roadmap

- Shell completions.
- More portable attach/open-terminal behavior outside the author's environment.
- Expanded examples for common Amp/tmux workflows.
- Config migration/versioning if the TSV contract changes.

## License

`amux` is available under the [MIT License](LICENSE).
