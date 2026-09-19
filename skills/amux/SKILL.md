---
name: amux
description: "Configures named native Amp multi-directory runner profiles and installs their systemd or launchd services. Also operates legacy Amux runner bindings during migration. Use for native-runners.json, runner service install/remove/doctor, mixed code and non-Git roots, and legacy runner recovery."
---

# amux

Thin machine-local configuration and activation layer for native Amp multi-directory runners. Prefer one Runner profile per machine, combining `--discover-dirs` with explicit unrelated directories such as Obsidian vaults. Legacy per-workdir tmux operations remain available only during migration.

## Contract

- For new configuration, prefer one profile in `native-runners.json` with one stable startup directory, native discovery for nearby Git repositories, and explicit `dirs` for unrelated or non-Git roots. Add a second profile only for a separately identified or isolated native process.
- `amux runner service install|remove|doctor` owns systemd/launchd artifacts by exact recorded digest. The generated service executes Amp directly; Amux is not a resident supervisor or updater. Install rejects self-owned legacy maintenance and case-insensitive runner-ID collisions with retained `runners.tsv` rows; it never rewrites those records.
- For legacy per-workdir operations, canonical runner identity is `--workdir`; `--workspace` selects a runner lifecycle group. Use long selectors.
- Top-level `list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` are legacy per-workdir aliases. Bare `amux` retains legacy launch behavior during migration. Other legacy machine-wide mutations require explicit `--all`.
- Legacy runner pin/list/launch/doctor/park/restart/teardown/remove and minimum fail-closed reconcile remain active during migration. Do not create new per-workdir bindings when one native multi-directory profile suffices. Before native cutover, disable login automation that invokes bare `amux` or `amux launch --all`, remove self-owned maintenance (external ownership is an explicit tail option), then park → soak through login or reboot → unpin. Preserve the worktree; never teardown for cutover.
- The `worker`, `spawn`, shelf, top-level worker teardown, group, report, callback, deadline, and finish surfaces are removed. Never attempt their historical syntax, edit their stores, or manufacture a compatibility transition. `runner teardown` is a new machine-local command, not a compatibility transition.
- Native-created work receives no Amux worker, adoption, group, report, callback, deadline, shelf, finish authorization, or lifecycle instructions.
- For delegated work, use authenticated native Amp `create_thread` on the exact intended Workspace Project and Orb, or exact live runner and intended workdir. Keep the native parent/reply route. Do not call the child an Amux worker.
- Before automatic mode selection, native child creation, or native child messages, load [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md).
- Prefer `--dry-run` and `--json`. Exit `2` means preflight rejection; exit `1` means runtime failure after mutation may have begun.
- `/amux health`, `/amux sprawl`, and `/amux sweep` are skill-only. Never invoke them as CLI commands.
- `/amux sweep` is the protected one-time #360 read-only inventory. Run it only after a separate exact owner authorization. Its sunset remains inactive until owner acceptance/disposition and explicit no-repeat confirmation.

## Route triggers

- **Serve my code and vault**: create or update `native-runners.json`; prefer discovery for nearby Git checkouts and explicit `dirs` for everything else.
- **Start this runner at login**: dry-run, then run `amux runner service install` to install and activate exact systemd/launchd artifacts.
- **Check runner services**: run `amux runner service doctor` for read-only verification.
- **Remove runner services**: dry-run before `amux runner service remove`.
- **Pin it** / **Pin this runner**: `amux runner pin --workspace <name> --workdir <existing-directory> [--runner-id <id>]` or `--current [--runner-id <id>]`.
- **List runners**: `amux runner list --all` or a canonical scope.
- **Restore my workspace**: `amux launch --workspace <name>`.
- **Park it**: `amux runner park --current` or exact workdir.
- **Restart unresponsive runners**: use the exact runner scope; preserve rows and fail closed on ambiguity.
- **Doctor amux**: `amux doctor --all` or scoped runner doctor.
- **Remove/unpin/reconcile a runner**: dry-run first. Unpin removes only an exact selected binding whose local tmux runner is positively absent and never stops a process. Remove and missing-workdir reconcile fail closed without authoritative process/catalog absence evidence; preserve the row on any blocker.
- **Teardown this runner/worktree or completed thread worktree**: load [`reference/workflows.md`](reference/workflows.md#teardown-completed-native-thread-worktrees). Teardown requires one exact workdir, a fresh state-bound plan, and explicit application of that digest. It is unavailable while native service metadata records installed or activation-pending services; use park → soak → unpin for cutover. Archive native threads separately and only when requested.
- **Spawn a worker for / delegate work**: this means native `create_thread`, never Amux spawn/adoption. Load [`reference/workflows.md`](reference/workflows.md).
- **Coordinate child threads**: use native parent/reply routing, messaging, and waiting only.
- **/amux health**: [`workflows.md`](reference/workflows.md#health-runners).
- **/amux sprawl**: [`workflows.md`](reference/workflows.md#sprawl-independent-issue-threads).
- **/amux sweep**: [`workflows.md`](reference/workflows.md#sweep-worktree-inventory).

Load `/amux-tycho` only on an explicit owner request. The receipt's immutable real Amp origin remains coordinator and consume/acknowledgement authority. Tycho is a typed report-only producer with no group, member, callback, finish, label, provider-identity, or lifecycle authority. An owner-authorized external Tycho second opinion must never grant Tycho GitHub review mutation or readiness promotion. Its receipts are separate from the removed worker-report store and remain until #328 passes.

## Load only what you need

- Runner selectors, side effects, installation: [`reference/commands.md`](reference/commands.md)
- Native creation, health, sprawl, protected sweep: [`reference/workflows.md`](reference/workflows.md)
- Runner recovery: [`reference/troubleshooting.md`](reference/troubleshooting.md)
- Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Amp invocation preflight: [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md)

## Safety

- No secrets in names or workdirs. Prefer temporary `--config-dir` and `--dry-run` for tests.
- Mutations are idempotent under one machine lock. On partial failure, inspect before retrying.
- Historical worker/coordination files are inert. Do not migrate, rewrite, drain, or delete them.
- Runner commands never own remote Amp threads.
