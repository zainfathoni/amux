# amux command reference

Long selectors are canonical: `--workspace`, `--workdir`, `--json`, and `--dry-run`.

```sh
# Native multi-directory runner profiles
amux --dry-run runner service install
amux runner service install|remove|doctor

# Runner-only top-level aliases
amux list|launch|park|restart|remove|doctor|reconcile [runner selectors]

# Explicit runner routes
amux runner pin --workspace <name> --workdir <existing-directory> [--runner-id <id>]
amux runner pin --current [--runner-id <id>]
amux runner list|launch|park|restart|remove|doctor|reconcile [runner selectors]
amux runner unpin --workdir <path>|--current
amux --json --dry-run runner teardown --workdir <secondary-worktree>
amux --json runner teardown --workdir <secondary-worktree> --confirm-plan <sha256>

# Legacy workspace, maintenance, installation
amux workspace list
amux workspaces
amux runner maintenance install --update-owner <self|external>
amux runner maintenance run [--scheduled]
amux runner maintenance remove
amux install doctor
amux migrate-config
amux update
```

`native-runners.json` is the source of truth for native profiles. Each profile declares a name, stable native `runner_id`, existing `startup_directory`, optional `discover_dirs`, explicit existing `dirs`, and optional `remote_control_terminal`. Prefer one profile per machine. Use discovery for nearby Git checkouts and explicit directories for unrelated or non-Git roots such as an Obsidian vault.

`runner service install` validates every directory, resolves the exact Amp executable, generates an owned systemd user service or launchd agent, and activates it. The service executes Amp directly; Amux is not resident. `runner service doctor` compares the current profile, recorded artifact digest, and active service state. `runner service remove` stops and removes only exact recorded artifacts. Use `--dry-run`; unrecognized or modified artifacts fail closed.

The commands below this point operate the legacy per-workdir registry and tmux lifecycle. They remain available during migration, but should not be used for new runner topology when one native profile suffices.

Bare `amux` and no-selector `launch` launch all configured runners. Other machine-wide mutations require `--all`. `workspace list` and `workspaces` report runner workspaces only.

Runner pin is active admission and requires an existing canonical directory. Git repository, worktree, and lock state are not runner requirements.

Runner park preserves the row. Runner unpin removes only the exact selected registry binding after proving its local tmux runner is absent; it never stops a process. Runner remove and missing-workdir reconcile retain and reject because current Amp APIs cannot prove authoritative process/native-catalog absence. Present-workdir reconcile is a no-op. Never delete a row or process on unreadable, conflicting, or unproven evidence.

Runner teardown is exact and machine-local: stop the positively identified runner if live, remove its exact clean attached secondary Git worktree, then unpin its exact row. It accepts only `--workdir`; there is no `--all` or `--current`. Dry-run returns a SHA-256 plan digest and apply requires that fresh digest. It preserves the branch ref and rejects primary, detached, locked, prunable, dirty, hidden-change, symlinked, non-root, current-directory, conflicting, ambiguous, and unreadable targets. It never archives native Amp threads, deletes branches, or reads/writes historical coordination stores.

The former `worker`, `spawn`, `shelve`, `unshelve`, top-level `teardown`, `group`, `report`, and `callback` routes are removed and fail before effects. Historical coordination stores are inert. The new runner-scoped teardown does not revive worker teardown. The `report` tombstone is not `/amux-tycho`; that explicit-only skill uses a separate receipt store and protocol.

`--config-dir <path>` and `AMUX_CONFIG_DIR` select the directory containing active `native-runners.json`, service ownership metadata, and legacy `runners.tsv`. Historical worker/coordination files in that directory are not part of active runner operation and must remain untouched.

`--runner-id` is optional native Amp launch configuration persisted with the canonical-workdir row. It is not an Amux selector: lifecycle commands continue selecting runners by workdir or workspace.

`--json` emits one v1 envelope. `--dry-run` puts prospective changes under `planned`. Exit `0` means no failures, exit `1` means runtime failure after mutation may have begun, and exit `2` means preflight rejection before mutation. Mutations and scheduled maintenance share one bounded machine lock.

For native profiles, systemd or launchd runs the resolved Amp executable directly and keeps it alive. Native Amp owns automatic updates and idle restarts. The old `amux launch --all` activation and scheduled maintenance model applies only to legacy per-workdir runners during migration.
