---
name: amux
description: "Manage local Amp worker, runner, workspace, and work-group orchestration with amux. Use for pin/unpin/park/restart/shelve/unshelve/launch/spawn, doctor, teardown, /amux health, /amux sprawl, /amux finish, and coordinate issue workers. Also 'forget this on restore', 'hide it for now', 'defer this workspace', 'Show shelved work', and 'Restore my workspace'. Experimental Tycho, Claude, or Pi routes are separate explicit-only skills (/amux-tycho, /amux-claude, /amux-pi)."
---

# amux

Local Amp/tmux lifecycle. **Worker** = interactive thread-bound client. **Runner** = `amp --no-tui` bound to a canonical workdir. **Workspace** = same-named tmux session grouping both.

Do not edit registries when the CLI can express the change. Run `amux help [command ...]` before assuming syntax. Durable worker/coordinator rules live in [`reference/contract-v1.md`](reference/contract-v1.md)—spawned workers read that file **once** via an **absolute** path the coordinator substitutes into the assignment; do not paste the contract into prompts or send a bare relative path.

## Preserve the agent contract

- Canonical worker identity is `--thread`; runner identity is `--workdir`; `--workspace` selects a lifecycle group. Use long selectors.
- Top-level `list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` aggregate both modes. Narrow with `amux worker ...` or `amux runner ...`.
- Bare `amux` launches workers only. `amux launch` launches both. Other machine-wide mutations need explicit `--all`.
- `spawn`, `shelve`, `unshelve`, and `teardown` are worker-only. `pin`/`unpin` require `worker` or `runner` namespace. Projectless local `spawn` is a coordinator-owned prepare → arm → one native message → finalize protocol; core amux never invokes the server tool.
- Every skill-driven spawn MUST pass an explicit mode. When a linked ChatGPT subscription and target-mode availability are known, choose `low` for small mechanical work, `medium` for ordinary implementation, or `high` for hard architecture, debugging, or review. Otherwise use `medium`. An explicitly requested mode always wins; `ultra`, plugin, and other special modes remain explicit-only.
- **Invocation defaults:** mode labels are capability presets, not stable model or cost selectors. Preserve `medium` when mode or subscription routing is unknown; do not Read Thread for task context; give Oracle supplied diff/context only—never Read Thread to feed Oracle. Details in [`reference/contract-v1.md`](reference/contract-v1.md).
- Before automatic spawn mode selection, creating a native Amp child, reading another Amp thread, or sending a native child message, load [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md). Never bypass a binding `ask` or `reject`. Read Thread remains instruction-only until a promoted gate exists—still do not use it without explicit permission.
- `/amux health`, `/amux sprawl`, and `/amux finish` are skill-only. Never invoke `amux health|sprawl|finish` as CLI commands.
- Prefer `--dry-run` and `--json`. Exit `2` = preflight rejection; exit `1` = runtime failure. For projectless local spawn, first require the authenticated native existing-thread `thread_interact` message action and reject before calling amux when unavailable. Run `prepare`, then `arm`, issue exactly one `thread_interact` call with `action: message`, the exact prepared thread, and the unchanged prompt, then `finalize`. Success with `latestCursor` proves only authenticated exact-thread acceptance/queueing; execution and physical executor/workdir affinity remain unproven. Any undocumented/tool-connection result after arm is indeterminate and never retried. Never paste, press Enter, clean up, archive, search, reconcile, or use another receiver.
- Work-group reports and wake-ups never authorize finish. A `ready` report is not cleanup authority.

## Route triggers

- **Pin it**: `amux worker pin --current` when complete `AMUX_*` identity exists; else explicit selectors. Config only. Never combine `--current` with another selector.
- **Unpin it** / **forget this on restore**: `amux worker unpin --current`.
- **Park it**: `amux worker park --current`.
- **Restart unresponsive clients**: `amux restart --all` or mode-specific restart.
- **Shelve this** / **defer this workspace** / **hide it for now**: `amux shelve` with `--current`, `--thread`, or `--workspace`.
- **Show shelved work**: `amux worker list --shelf shelved`.
- **Unshelve this**: `amux unshelve --current` or `--thread`. Launch separately.
- **Restore my workspace**: `amux launch --workspace <name>` (or bare `amux` for all workers).
- **Doctor amux**: `amux doctor --all` or scoped doctor.
- **Spawn a worker for ...**: load [`reference/workflows.md`](reference/workflows.md). Use native create → adopt for Amp Workspace Projects. Select the explicit task-appropriate mode under the rule above. For a projectless repository, select the intended physical executor first, then follow the capability-gated prepare/arm/native-message/finalize workflow with canonical `--workdir`, explicit `--mode`, and `--prompt-file`; `amux worker spawn` is the exact alias. The CLI requires no local process `--runner-id` alias, never substitutes an Orb or an amux Runner, and never transports prompt bytes through tmux. The task-only prompt includes the **absolute path to the loaded skill's** `reference/contract-v1.md` (never a bare relative path).
- **Coordinate issue workers**: load [`reference/workflows.md`](reference/workflows.md#coordinate-a-durable-issue-work-group).
- **Teardown this worker**: load [`reference/workflows.md`](reference/workflows.md#teardown-a-worker). If `/amux-claude` pairs may exist, run that skill's paired lifecycle preflight first; then `amux teardown` last.
- **/amux health**: [`workflows.md`](reference/workflows.md#health-workers-and-runners).
- **/amux sprawl**: [`workflows.md`](reference/workflows.md#sprawl-independent-issue-workers).
- **/amux finish**: [`workflows.md`](reference/workflows.md#finish-a-merged-worker).
- **Before any worktree remove or prune path**: load [`reference/removal-safety.md`](reference/removal-safety.md).

Deadlines (optional): load [`reference/deadline-v1.md`](reference/deadline-v1.md) only when arming or handling deadline wake-ups—not on every `/amux` load.

Experimental external execution is explicit-only. Load **`/amux-tycho`** for the Tycho report bridge and any owner-authorized external Tycho second opinion on an authoritative Amp `/team-review`, or **`/amux-claude`** / **`/amux-pi`** for their provider-specific fallback routes; never activate them from incidental mentions. For `/amux-tycho`, a receipt's immutable real Amp origin remains coordinator and consume/acknowledgement authority. Tycho is a typed report-only producer with no group, member, callback, finish, label, provider-identity, or lifecycle authority. Owner-selected Tycho second opinions stay under that separate skill and never grant Tycho GitHub review mutation or readiness promotion.

## Load only what you need

- Selectors, side effects, install: [`reference/commands.md`](reference/commands.md)
- Spawn, health, sprawl, teardown, finish: [`reference/workflows.md`](reference/workflows.md)
- Durable protocol workers must read once: [`reference/contract-v1.md`](reference/contract-v1.md)
- Coordinator deadlines: [`reference/deadline-v1.md`](reference/deadline-v1.md)
- Stuck clients / recovery: [`reference/troubleshooting.md`](reference/troubleshooting.md)
- Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Amp invocation preflight: [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md)
- Worktree removal and prune safety: [`reference/removal-safety.md`](reference/removal-safety.md)

## Safety

- No secrets in names, workdirs, or thread IDs. Prefer temporary `--config-dir` and `--dry-run` for tests.
- Mutations are idempotent under one machine lock. On partial failure, inspect before retrying.
- Never guess a callback pane, infer finish from a wake-up, auto-release, force-delete a branch, or erase group history during finish.
- Runner commands never own remote agent threads. Teardown never applies to runners.
