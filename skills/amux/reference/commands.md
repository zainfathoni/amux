# amux command reference

This reference follows `amux help` from the agent-first CLI. Long selectors are canonical for agents: `--workspace`, `--window`, `--workdir`, `--thread`, `--mode`, `--json`, and `--dry-run`.

## Resource routes

```sh
# Aggregate worker + runner routes
amux list [--workspace <name>] [--thread <id>] [--workdir <path>] [--current|--all]
amux launch [--workspace <name>] [--thread <id>] [--workdir <path>] [--current|--all]
amux park|restart|remove|doctor|reconcile [selectors]

# Mode-specific routes
amux worker list|launch|park|restart|remove|doctor|reconcile [worker selectors]
amux runner list|launch|park|restart|remove|doctor|reconcile [runner selectors]
amux worker pin --workspace <name> --window <name> --workdir <path> --thread <id>
amux worker pin --current
amux worker unpin --thread <id>
amux worker unpin --current
amux runner pin --workspace <name> --workdir <existing-directory>
amux runner pin --current
amux runner unpin --workdir <path>
amux runner unpin --current

# Worker-only concise routes
amux spawn --workspace <name> --window <slug> --workdir <path> --mode medium --message <text> --idempotency-key <key> [--reconcile]
amux shelve|unshelve|teardown [--workspace <name>|--thread <id>|--current|--all]

# Durable group intent, reports, and ephemeral callbacks
amux group declare|add|remove|coordinator --group <id> --thread <id>
amux group list [--group <id>|--thread <id>|--all]
amux group show --group <id>
amux group reconcile (--group <id>|--thread <id>|--all)
amux callback register --group <id> --thread <coordinator-id> --pane <pane-id>
amux callback clear --group <id>
amux report submit --report-id <id> --group <id> --thread <member-id> --status <ready|blocked|merged> --issue <value> --pr <url> --summary <text>
amux report pending [--group <id>|--thread <id>|--all]
amux report history --report-id <id>
amux report acknowledge --report-id <id>
amux report authorize-finish --report-id <id> --thread <coordinator-id> --reference <value>

# Workspace and maintenance routes
amux workspace list [--mode worker|runner]
amux workspaces [--mode worker|runner]
amux runner maintenance install --update-owner <self|external>
amux runner maintenance run [--scheduled]
amux runner maintenance remove
amux install doctor
amux migrate-config
amux update
```

`amux workspaces` is an exact alias for `amux workspace list`; both list the union of worker and runner workspaces unless filtered. No separate session selector exists. A workspace maps to one same-named tmux session.

Removed commands and positional forms fail with remediation. Do not use `store`, `pin-current`, `unpin-current`, `park-current`, `shelve-current`, `shelved`, `prune-archived`, `self-update`, positional workspace/window/session arguments, or the old `--config` file selector.

## Selection and scope

- Worker selectors: canonical `--thread`; or `--workspace`, `--current`, and explicit `--all` where help permits. `--window` and `--workdir` are creation metadata for worker pin/spawn, not canonical worker identity.
- Runner selectors: canonical `--workdir`; or `--workspace`, `--current`, and explicit `--all` where help permits. Runner windows are generated implementation details.
- Aggregate routes accept both `--thread` and `--workdir`. A workspace selection jointly preflights both modes.
- Read-only discovery may naturally cover all configured resources. No-selector `launch` is the bulk-mutation exception; other machine-wide mutations require `--all`.

## Side-effect contracts

| Operation | Worker config / shelf intent | Runner config | Live local client | Remote worker thread |
| --- | --- | --- | --- | --- |
| `list`, `workspace list` | inspect | inspect | none | none |
| `doctor` | inspect | inspect | inspect | inspect only where needed |
| `launch` | read; skip shelved workers | read | create/verify selected clients | none |
| mode-specific `pin` / `unpin` | pin mutates worker registry; unpin removes worker and matching shelf intent | mutate runner registry only | none | none |
| `park` | preserve | preserve | stop verified selected clients | none |
| `restart` | preserve | preserve | replace verified selected clients | none |
| `remove` | remove selected config; remove worker shelf intent | remove selected config | stop verified selected clients | none |
| `shelve` | record intent first; preserve worker | none | park verified workers | archive selected threads |
| `unshelve` | remove intent only after unarchive | none | none | unarchive selected threads |
| `spawn` | add worker after verified delivery | none | create interactive worker | create/rename one thread |
| `teardown` | remove worker and shelf intent | none | stop verified worker; absence is benign | archive verified thread |
| `reconcile` | synchronize shelf/remote drift | repair stale runner ownership | only verified repairs | worker archive synchronization only |

`remove` differs from `unpin`: remove stops the selected verified local client and deletes configuration, while unpin never stops it. Worker unpin removes the worker row and matching shelf intent; runner unpin removes only the runner row. Neither changes remote thread state. Worker `remove` never archives. `teardown` is worker-only remove plus verified remote archival.

Runner pin requires a canonical existing directory. Git repository, worktree, and lock state are not runner requirements. Runner reconcile may remove stale config for a missing directory, but never adopts or deletes ambiguous Amp-owned processes.

## Work-group, report, and callback contract

- Group IDs are at most 32 characters and match `^[a-z0-9]+(?:-[a-z0-9]+)*$` byte-for-byte. Local `groups.tsv` intent is authoritative and survives worker teardown and finish. External labels project member roles only; coordinator identity remains local so long-lived coordinators do not accumulate supervised-group labels. Member labels are add-only: reconcile skips coordinators, removal is local-only and reports `external_sync: unsupported` plus `drift: may_remain_indefinitely`, and promoting an already-labelled member cannot remove its prior label. Coordinator reassignment demotes the prior coordinator to member and reports it separately as `external_sync: additive_ensure_required` plus `drift: label_may_be_missing`; run `group reconcile` to add-only ensure that member's label. Repeated `group declare`/`coordinator` and `group add` targeting an existing coordinator are skipped no-ops that never probe Amp.
- Spawn validates/sorts groups before creation, then attaches only the final authoritative receiving thread. Retry the same key after partial grouping; never attach the abandoned provisioned thread.
- Report identity is the stable `--report-id` plus immutable group/thread/issue/reference binding. Exact duplicate submission is a skipped replay and retries callback notification. Conflicting reuse and illegal transitions are exit `2`. `ready` requires a PR and means implementation, tests, one review, PR, and normal CI are complete. `blocked` may use `--pr none`; `merged` requires prior durable finish authorization.
- Submission persists or confirms the report before callback verification. Human fields are tab-separated: recorded submission is `<report><TAB><status><TAB>recorded<TAB><thread>`, exact replay substitutes `duplicate`, and dry-run substitutes `planned`. Its next line is `CALLBACK<TAB><group><TAB><report><TAB>notified`; callback failure substitutes `failed`. Callback failure is exit `1`, with a successful/skipped report outcome and separate failed callback outcome; the report remains pending.
- `acknowledge` prints `<report><TAB>acknowledged` (or `<report><TAB>duplicate`), but does not authorize finish. `authorize-finish` prints `<report><TAB>authorized` (or `<report><TAB>duplicate`) and is accepted only from the current durable group coordinator for a `ready` report.
- Callback registration prints `<group><TAB>registered<TAB><generation><TAB><pane>`; each registration creates a new lease generation. The lease is config-directory/group-scoped runtime state. Every notification freshly verifies the exact coordinator, pane/session/window IDs, start/current command, canonical workdir, PID/process/start identity. Missing, stale, recycled, or restarted targets fail closed; explicit registration is the only recovery.
- A wake-up is exactly `AMUX_REPORT group=<group> report=<id>` plus Enter in one tmux operation. It is notification only—not report delivery, acknowledgement, verification, authorization, or lifecycle authority.

## Output, failure, and concurrency

- Human output is default. `--json` emits exactly one v1 envelope with `schema_version`, `command`, `dry_run`, and `planned`, `successful`, `skipped`, and `failed` arrays.
- Resource IDs are discriminated: workers by canonical thread, runners by canonical workdir, workspaces by name. Ignore unknown optional fields within schema v1.
- With `--dry-run`, prospective mutations appear under `planned`; completed actions belong under `successful`; no-ops under `skipped`; errors under `failed`.
- Exit `0`: no failures. Exit `1`: at least one runtime failure after mutation may have begun. Exit `2`: request/preflight rejection before mutation.
- Bulk operations preflight the whole plan, then continue independent actions after runtime failures.
- Mutations share one bounded machine-level lock. A busy result includes structured lock ownership and performs no mutation.
- Lock contention is exit `2`, authorizes no side effect, and must be retried as the identical desired-state operation after the current lock owner finishes. Preserve the same report ID or spawn key.
- Desired-state operations are idempotent. Spawn additionally requires a stable `--idempotency-key`; an unrecoverable interrupted external creation becomes indeterminate and must not be blindly retried.

## Installation and maintenance

`~/.local/bin/amux` is the canonical self-updating path. `amux update` refuses package-manager/toolchain-owned paths. Diagnose every PATH candidate, target, version, canonical shadowing, and scheduled-maintenance drift with:

```sh
amux install doctor
amux --json install doctor
```

Runner maintenance is an explicit short-lived systemd user timer (Linux) or LaunchAgent (macOS), scheduled every six hours with bounded jitter. Choose who updates Amp:

```sh
amux --dry-run runner maintenance install --update-owner self
amux runner maintenance install --update-owner external
amux runner maintenance run
amux runner maintenance remove
```

`self` lets Amp's updater own updates; `external` records package-manager ownership. Maintenance restarts verified running runners only when the Amp executable fingerprint changed. It uses the same operation lock as other mutations.
