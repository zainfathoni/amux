# amux

`amux` is the declarative machine-local configuration and activation layer for [native Amp runners](https://ampcode.com/docs/cli/runners). Amp owns multi-directory serving, dynamic directory additions, remote work, and its native update behavior. Amux records how each runner should start and installs its systemd user service or launchd agent.

Prefer one native runner profile per machine. A profile can discover Git repositories beneath a code root while explicitly serving unrelated non-Git directories such as an Obsidian vault. Define a second profile only when you need a separately identified or isolated native runner.

## Native runner profiles

Create `~/.config/amux/native-runners.json`:

```json
{
  "schema_version": 1,
  "runners": [
    {
      "name": "main",
      "runner_id": "laptop-main",
      "startup_directory": "/home/me/Code",
      "discover_dirs": true,
      "discover_depth": 3,
      "dirs": [
        "/home/me/Obsidian/Vault",
        "/home/me/.dotfiles"
      ],
      "remote_control_terminal": true
    }
  ]
}
```

Preview, install, and inspect the services:

```sh
amux --dry-run runner service install
amux runner service install
amux runner service doctor
```

On Linux, Amux installs one `com.zainfathoni.amux.runner.<name>.service` systemd user unit per profile. On macOS, it installs the equivalent LaunchAgent. Each artifact executes the resolved Amp binary directly with `--no-tui`, the stable runner ID, discovery and explicit-directory flags, and optional remote terminal access. Amux records artifact digests and refuses to replace or remove unrecognized files. Installation also rejects a case-insensitive runner-ID collision with a retained `runners.tsv` row and rejects installed or activation-pending self-owned legacy maintenance; it does not rewrite either legacy record.

Dynamic `amp runner dirs add|list|remove` remains available. Amp persists those additions against the profile's stable startup directory; keep declarative machine-critical paths in `native-runners.json` and use dynamic additions for local, temporary choices.

When `discover_dirs` is true, Amp scans two levels beneath `startup_directory` by default. Set `discover_depth` from 1 through 10 when repositories are nested more or less deeply; for example, an owner/repository layout beneath a code root uses the default depth 2. Amux requires discovery to be explicitly enabled when a depth is configured. Use `dirs` for repositories or non-Git directories outside the discovery root.

During coexistence, use a dedicated persistent startup directory, for example `~/.local/share/amux/native-main` (create it first), set `discover_dirs` to `false`, and list the existing workdirs explicitly in `dirs`. Amp can reject a second headless instance starting in the same directory even with a different runner ID. Installation rejects startup directories matching retained legacy TSV workdirs, including parked rows; native profiles must also have distinct startup directories. Symlink aliases are resolved for these checks. Sharing explicit served directories is allowed; it does not migrate existing threads. A successful `service doctor` is a point-in-time check, not proof of sustained health.

The previous per-workdir registry, tmux lifecycle, and scheduled Amp updater remain available during migration, but they are no longer the destination architecture. Native Amp updates a running runner itself. Self-owned legacy maintenance cannot be installed or run while native runner services are installed or activation pending. External or package-manager update ownership remains an explicit compatibility-tail option.

### Primary-service rollout recovery companion

Operators who need a recovery channel while replacing the primary native runner service may retain one narrowly scoped legacy tmux runner per host. Use `<host>-maintenance` for both its workspace and runner ID, bind it to the canonical Amux checkout, and launch it with the bare `amp --no-tui --runner-id <host>-maintenance` shape produced by `amux runner launch`. Do not add discovery, explicit directory, or remote-terminal flags, and do not declare this runner in `native-runners.json` or `runner-services.json`.

Before a primary-service rollout, inspect the exact row, dry-run the launch, and verify that the detached tmux runner has the expected canonical workdir, argv, and a process group independent from the primary service. If an obsolete row owns that workdir and its runner is positively absent, migrate it only through supported dry-run-first `runner unpin` then `runner pin` operations; never edit `runners.tsv`, tmux, or PID markers manually. Keep the companion live through primary replacement, then verify both runners with `runner list` and `runner doctor`. This is a replacement-survival channel, not a second native profile, a general delegated-work topology, or reboot persistence; after logout, reboot, or tmux loss, re-establish and verify it before relying on it.

For cutover, first disable any login automation that invokes bare `amux` or `amux launch --all`. Give native and legacy runners distinct IDs, remove self-owned legacy maintenance (or explicitly retain external ownership), install and verify native service coverage, then **park → soak → unpin** each legacy runner. Include a login or reboot in the soak. Never use `runner teardown` for this migration: preserve the worktree, and roll back before unpin by removing the native services and relaunching the retained legacy row.

The former worker, spawn/adoption, shelf, group, report, callback, deadline, and finish-authorization commands have been removed. Their historical files are inert compatibility evidence: current commands neither migrate nor mutate them. The protected one-time #360 inventory remains read-only under its existing owner gate. `/amux-tycho` is a separate receipt bridge and is unaffected by removal of worker reports.

See [ADR 0011](docs/adr/0011-consolidate-on-native-multi-directory-runners.md), which supersedes the retained-host architecture in [ADR 0008](docs/adr/0008-retain-machine-local-runner-host-and-drain-coordination.md). [ADR 0010](docs/adr/0010-add-machine-local-runner-teardown.md), [ADR 0009](docs/adr/0009-remove-active-legacy-coordination-surfaces.md), and the [disposition ledger](docs/staged-drain-disposition.md) remain migration context.

Website: [amux.zainf.dev](https://amux.zainf.dev) · Skill guide: [amux.zainf.dev/skill/](https://amux.zainf.dev/skill/)

## Install

Requirements: Amp CLI. Legacy per-workdir lifecycle additionally requires tmux. Building from source requires the Go version in `go.mod`.

Install the latest Linux or macOS release at `~/.local/bin/amux`:

```sh
curl -fsSL https://amux.zainf.dev/install.sh | sh
```

For a pinned published release:

```sh
curl -fsSL https://amux.zainf.dev/install.sh | AMUX_VERSION=v0.6.0 sh
```

Homebrew users can instead run `brew install zainfathoni/tap/amux`. Homebrew owns that installation; `amux update` deliberately refuses Homebrew, mise, asdf, Nix, and system-package paths.

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

Experimental provider skills remain explicit-only and separately installed. For a clean local `main` checkout, the installer can link bundled skills without updating that checkout:

```sh
AMUX_REPO="$HOME/Code/GitHub/zainfathoni/amux"
git -C "$AMUX_REPO" pull --ff-only origin main
curl -fsSL https://amux.zainf.dev/install.sh |
  AMUX_SKILLS_SOURCE="$AMUX_REPO" sh
```

`/amux` covers retained runner operations and routes requests for new delegated work to native Amp child threads. It never creates or adopts Amux workers or enrolls native work in legacy coordination state.

`/amux-tycho` is a separate, unstable receipt-based report bridge. Its consume/acknowledge protocol is not the removed `amux report` worker-group mechanism. Keep it unchanged until issue #328's authenticated same-turn direct-return gate passes.

## Legacy per-workdir lifecycle

Pin and launch a runner bound to an existing workdir:

```sh
amux runner pin --workspace amux --workdir ~/Code/amux-runner --runner-id macbook-amux
amux runner launch --workdir ~/Code/amux-runner
```

`--runner-id` is optional. When supplied, Amux persists it with the canonical-workdir binding, launches `amp --no-tui --runner-id <id>`, and requires that exact argv when inspecting or stopping the runner. The canonical workdir remains the Amux runner identity and selector.

Reconfigure a pinned runner's native ID with `amux runner pin -w amux -d ~/Code/amux-runner -i macbook-amux-new`. A stopped runner stays stopped. If the exact runner is live and the ID changes, add `--restart`; without it Amux refuses and leaves both process and configuration untouched. Omitting `--runner-id` preserves the stored ID.

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

Runner teardown stops only the exact verified local runner, removes only the exact clean attached secondary worktree, and unpins only its exact row. It preserves the local branch and never archives Amp threads. Primary, detached, locked, prunable, dirty, hidden-change, ambiguous, unreadable, symlinked, non-root, and current-directory targets reject before worktree removal. It also fails closed whenever `runner-services.json` records installed or activation-pending native services: tmux absence is not proof that native Amp is absent. Use park → soak → unpin, never teardown, for native-service cutover.

Runner workdirs may be Git worktrees or any other existing directories. Runner lifecycle never creates, continues, archives, or manages remote Amp threads.

For new delegated work, use Amp's authenticated native `create_thread` on the exact intended Workspace Project/Orb or exact live runner/workdir. Keep only native parent/reply routing. Do not create Amux worker, adoption, group, report, callback, deadline, shelf, or finish state.

## Command model

Run `amux help` or `amux help runner <command>` for contextual help.

```sh
amux list [--workspace <name>|--workdir <path>|--current-dir|--current|--all]
amux launch [--workspace <name>|--workdir <path>|--current-dir|--current|--all]
amux park|restart|remove|doctor|reconcile [runner selectors]

amux runner service install|remove|doctor
amux runner pin --workspace <name> (--workdir <existing-directory>|--current-dir) [--runner-id <id>] [--restart]
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

Runner service commands operate every profile in `native-runners.json`; service installation is machine-wide and protected by Amux's mutation lock. The remaining top-level lifecycle routes are legacy per-workdir aliases. Bare `amux` retains its legacy behavior of launching configured `runners.tsv` rows during migration, so disable any login automation that invokes it or `launch --all` before parking legacy runners.

Runner pin is active admission. `runner unpin` removes only the exact selected registry binding after proving its local tmux runner is absent; it never stops a process. `runner teardown` is the explicit worktree-owning retirement route described above. `runner remove` and missing-workdir `runner reconcile` fail closed pending authoritative process/catalog absence evidence. Use `runner park` to stop an exact owned process while retaining its row.

Runner commands that accept `--workdir` (`-d`) also accept `--current-dir` (`-c`), exactly equivalent to `-d .` in the command's cwd. It never reads the tmux pane cwd and may be combined with `--workspace`, but not with `-d`, `--current`, or `--all`. `--current` retains its tmux-based behavior. Before the command name, `-c <path>` remains the global `--config-dir` shorthand.

Removed `worker`, `spawn`, `shelve`, `unshelve`, top-level `teardown`, `group`, `report`, and `callback` routes fail before process or store effects. The active command is runner-scoped and has none of the former worker teardown's remote-thread or legacy-store behavior. `report` does not route to `/amux-tycho`; use that separate skill explicitly.

## Configuration and safety

Active configuration is directory-based. `native-runners.json` declares native multi-directory profiles and `runner-services.json` records installed artifact ownership under `~/.config/amux` by default. Legacy `runners.tsv` rows remain readable during migration. Select another directory with `--config-dir` (`-c`) or `AMUX_CONFIG_DIR`.

Historical files such as `workers.tsv`, `shelves.tsv`, `groups.tsv`, `reports.json`, operation records, and spawn assignments may still exist. They are inert: active Amux commands do not enroll, drain, migrate, rewrite, or delete them. Do not edit or delete them as part of runner operation. The separately owner-gated #360 inventory may inspect them read-only.

`--json` (`-j`) emits one versioned result envelope. `--dry-run` (`-n`) validates and plans without mutation. Exit `0` means no failure, exit `1` means a runtime failure after mutation may have begun, and exit `2` means preflight rejection before mutation. Service and legacy mutations share one bounded machine lock.

## Automatic launch

`amux runner service install` writes and activates one OS service per native profile. launchd starts it at macOS GUI login. A systemd user service starts with the user's service manager; starting it without login requires systemd lingering configured outside Amux. Amux does not stay resident or supervise the process.

The generated process arguments come entirely from the validated profile:

- `--runner-id` from `runner_id`;
- `--discover-dirs` when `discover_dirs` is true;
- `--discover-depth` from optional `discover_depth` (1 through 10; omitted keeps Amp's default 2);
- repeated `--dir` arguments from `dirs`; and
- `--remote-control-terminal` when enabled.

Native Amp owns runner update and idle-restart behavior. Automatic update availability still depends on the Amp installation and settings—for example, package-manager installations or disabled updates remain externally managed. The legacy `runner maintenance` scheduler remains available only for old per-workdir tmux runners during migration.

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
