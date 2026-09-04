# amux

`amux` is the thin machine-local runner and tmux host layer for [Amp](https://ampcode.com/). Native Amp owns new task creation and coordination; Amux owns automatic machine-local runners bound to exact workdirs.

Amux is not deprecated or archived. The active product surface is intentionally small:

- a machine-local runner registry and exact canonical-workdir bindings;
- runner pin, list, launch, doctor, park, restart, teardown, remove, and fail-closed reconcile;
- automatic `amux launch --all` at graphical login or boot;
- tmux and `amp --no-tui` process launch;
- install, update, maintenance, and diagnostics; and
- systemd/launchd activation and process-group retention.

The former worker, spawn/adoption, shelf, group, report, callback, deadline, and finish-authorization commands have been removed. Their historical files are inert compatibility evidence: current commands neither migrate nor mutate them. The protected one-time #360 inventory remains read-only under its existing owner gate. `/amux-tycho` is a separate receipt bridge and is unaffected by removal of worker reports.

See [ADR 0010](docs/adr/0010-add-machine-local-runner-teardown.md), [ADR 0009](docs/adr/0009-remove-active-legacy-coordination-surfaces.md), [ADR 0008](docs/adr/0008-retain-machine-local-runner-host-and-drain-coordination.md), and the [disposition ledger](docs/staged-drain-disposition.md).

Website: [amux.zainf.dev](https://amux.zainf.dev) · Skill guide: [amux.zainf.dev/skill/](https://amux.zainf.dev/skill/)

## Install

Requirements: Amp CLI and tmux. Building from source also requires the Go version in `go.mod`.

Install the latest Linux or macOS release at `~/.local/bin/amux`:

```sh
curl -fsSL https://amux.zainf.dev/install.sh | sh
```

For a pinned published release:

```sh
curl -fsSL https://amux.zainf.dev/install.sh | AMUX_VERSION=v0.2.1 sh
```

Homebrew users can instead run:

```sh
brew install zainfathoni/tap/amux
brew upgrade amux
```

Homebrew owns that installation. `amux update` deliberately refuses Homebrew, mise, asdf, Nix, and system-package paths.

To build from source:

```sh
make build
install -D -m 0755 amux ~/.local/bin/amux
```

Diagnose installation, PATH shadowing, and maintenance drift with:

```sh
amux install doctor
amux --json install doctor
amux update
```

## Install the `/amux` skill

```sh
npx skills add zainfathoni/amux --skill amux --global
```

Experimental provider skills are explicit-only and installed separately:

```sh
npx skills add zainfathoni/amux --skill amux-claude --global
npx skills add zainfathoni/amux --skill amux-pi --global
npx skills add zainfathoni/amux --skill amux-tycho --global
```

For a clean local `main` checkout, the installer can link bundled skills without updating that checkout:

```sh
AMUX_REPO="$HOME/Code/GitHub/zainfathoni/amux"
git -C "$AMUX_REPO" pull --ff-only origin main
curl -fsSL https://amux.zainf.dev/install.sh | AMUX_SKILLS_SOURCE="$AMUX_REPO" sh
```

`/amux` covers retained runner operations and routes requests for new delegated work to native Amp child threads. It never creates or adopts Amux workers or enrolls native work in legacy coordination state.

`/amux-tycho` is a separate, unstable receipt-based report bridge. Its consume/acknowledge protocol is not the removed `amux report` worker-group mechanism. Keep it unchanged until issue #328's authenticated same-turn direct-return gate passes.

## Quick start

Pin and launch a runner bound to an existing workdir:

```sh
amux runner pin --workspace amux --workdir ~/Code/amux-runner
amux runner launch --workdir ~/Code/amux-runner
```

List and diagnose runners:

```sh
amux runner list --all
amux runner doctor --all
amux workspace list
```

Park or restart verified local processes without changing their registry rows:

```sh
amux runner park --workdir ~/Code/amux-runner
amux runner restart --workdir ~/Code/amux-runner
```

Retire one exact runner and its clean secondary Git worktree with a state-bound two-step plan:

```sh
amux --json --dry-run runner teardown --workdir ~/Code/amux-runner
amux --json runner teardown --workdir ~/Code/amux-runner --confirm-plan <sha256-from-dry-run>
```

Runner teardown stops only the exact verified local runner, removes only the exact clean attached secondary worktree, and unpins only its exact row. It preserves the local branch and never archives Amp threads. Primary, detached, locked, prunable, dirty, hidden-change, ambiguous, unreadable, symlinked, non-root, and current-directory targets reject before worktree removal.

Runner workdirs may be Git worktrees or any other existing directories. Runner lifecycle never creates, continues, archives, or manages remote Amp threads.

For new delegated work, use Amp's authenticated native `create_thread` on the exact intended Workspace Project/Orb or exact live runner/workdir. Keep only native parent/reply routing. Do not create Amux worker, adoption, group, report, callback, deadline, shelf, or finish state.

## Command model

Run `amux help` or `amux help runner <command>` for contextual help.

```sh
amux list [--workspace <name>|--workdir <path>|--current|--all]
amux launch [--workspace <name>|--workdir <path>|--current|--all]
amux park|restart|remove|doctor|reconcile [runner selectors]

amux runner pin --workspace <name> --workdir <existing-directory>
amux runner teardown --workdir <secondary-worktree> --confirm-plan <sha256>
amux runner list|launch|park|restart|remove|doctor|reconcile [runner selectors]
amux workspace list
amux workspaces

amux runner maintenance install --update-owner <self|external>
amux runner maintenance run [--scheduled]
amux runner maintenance remove
amux install doctor
amux migrate-config
amux update
```

Top-level lifecycle routes are runner-only aliases. Bare `amux` is equivalent to automatic runner launch across all configured rows. Mutating machine-wide routes other than launch require explicit `--all`.

Runner pin is active admission. `runner unpin` removes only the exact selected registry binding after proving its local tmux runner is absent; it never stops a process. `runner teardown` is the explicit worktree-owning retirement route described above. `runner remove` and missing-workdir `runner reconcile` fail closed pending authoritative process/catalog absence evidence. Use `runner park` to stop an exact owned process while retaining its row.

Removed `worker`, `spawn`, `shelve`, `unshelve`, top-level `teardown`, `group`, `report`, and `callback` routes fail before process or store effects. The active command is runner-scoped and has none of the former worker teardown's remote-thread or legacy-store behavior. `report` does not route to `/amux-tycho`; use that separate skill explicitly.

## Configuration and safety

Active configuration is directory-based and contains `runners.tsv` under `~/.config/amux` by default. Select another directory with `--config-dir` (`-c`) or `AMUX_CONFIG_DIR`.

Historical files such as `workers.tsv`, `shelves.tsv`, `groups.tsv`, `reports.json`, operation records, and spawn assignments may still exist. They are inert: active Amux commands do not enroll, drain, migrate, rewrite, or delete them. Do not edit or delete them as part of runner operation. The separately owner-gated #360 inventory may inspect them read-only.

`--json` (`-j`) emits one versioned result envelope. `--dry-run` (`-n`) validates and plans without mutation. Exit `0` means no failure, exit `1` means a runtime failure after mutation may have begun, and exit `2` means preflight rejection before mutation. Mutations and scheduled maintenance share one bounded machine lock.

## Automatic launch and maintenance

At graphical login or boot, machine configuration runs `amux launch --all`. Amux reads the registry and launches the tmux/Amp process group. The OS service manager activates Amux; it does not replace Amux with direct runner supervision.

Verified deployment patterns are:

- systemd user service: `Type=oneshot` and `RemainAfterExit=yes`;
- macOS RunAtLoad LaunchAgent: `AbandonProcessGroup=true`.

Runner maintenance is a separate short-lived scheduled Amux operation. It checks every six hours with bounded jitter, updates Amp according to declared ownership, and restarts verified running runners only when the executable changed.

## Shell completions

```sh
amux completion bash > ~/.local/share/bash-completion/completions/amux
amux completion zsh > ~/.zfunc/_amux
amux completion fish > ~/.config/fish/completions/amux.fish
```

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md). Standard checks:

```sh
go test ./...
go vet ./...
make build
gofmt -l .
git diff --check
```

## License

[MIT](LICENSE)
