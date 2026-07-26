---
name: amux
description: "Manage local Amp worker, runner, workspace, and work-group orchestration with amux. Use for pin/unpin/park/restart/shelve/unshelve/launch/spawn, doctor, teardown, /amux health, /amux sprawl, /amux finish, and coordinate issue workers. Also 'forget this on restore', 'hide it for now', 'defer this workspace', 'Show shelved work', and 'Restore my workspace'. Experimental Claude or Pi delegation is a separate skill (/amux-claude, /amux-pi), not this skill."
---

# amux

Local Amp/tmux lifecycle. **Worker** = interactive thread-bound client. **Runner** = `amp --no-tui` bound to a canonical workdir. **Workspace** = same-named tmux session grouping both.

Do not edit registries when the CLI can express the change. Run `amux help [command ...]` before assuming syntax. Durable worker/coordinator rules live in [`reference/contract-v1.md`](reference/contract-v1.md)—spawned workers read that file **once** via an **absolute** path the coordinator substitutes into the assignment; do not paste the contract into prompts or send a bare relative path.

## Preserve the agent contract

- Canonical worker identity is `--thread`; runner identity is `--workdir`; `--workspace` selects a lifecycle group. Use long selectors.
- Top-level `list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` aggregate both modes. Narrow with `amux worker ...` or `amux runner ...`.
- Bare `amux` launches workers only. `amux launch` launches both. Other machine-wide mutations need explicit `--all`.
- `spawn`, `shelve`, `unshelve`, and `teardown` are worker-only. `pin`/`unpin` require `worker` or `runner` namespace.
- Every skill-driven spawn MUST pass `--mode medium` unless the user explicitly requests another mode. An explicitly requested mode always wins. Do not infer `low`, `high`, or `ultra`.
- **Credit defaults (explicit owner permission required to override):** no automatic/`low` mode; no Read Thread for task context; Oracle reviews get supplied diff/context only—never Read Thread to feed Oracle. Details in [`reference/contract-v1.md`](reference/contract-v1.md).
- Before automatic spawn mode selection, creating a native Amp child, reading another Amp thread, or sending a native child message, load [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md). Never bypass a binding `ask` or `reject`. Read Thread remains instruction-only until a promoted gate exists—still do not use it without explicit permission.
- `/amux health`, `/amux sprawl`, and `/amux finish` are skill-only. Never invoke `amux health|sprawl|finish` as CLI commands.
- Prefer `--dry-run` and `--json`. Exit `2` = preflight rejection; exit `1` = runtime failure. Never retry indeterminate spawn blindly. Lock contention is exit `2`: retry the identical operation.
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
- **Spawn a worker for ...**: load [`reference/workflows.md`](reference/workflows.md); prefer one native Amp thread creation with explicit executor and `medium`, then `amux worker adopt` using the returned exact thread. The task-only initial native message includes one line naming the **absolute** path to the loaded skill's `reference/contract-v1.md` (read once; never a bare relative path). Legacy `amux spawn` is deprecated compatibility only.
- **Coordinate issue workers**: load [`reference/workflows.md`](reference/workflows.md#coordinate-a-durable-issue-work-group).
- **Teardown this worker**: load [`reference/workflows.md`](reference/workflows.md#teardown-a-worker). If `/amux-claude` pairs may exist, run that skill's paired lifecycle preflight first; then `amux teardown` last.
- **/amux health**: [`workflows.md`](reference/workflows.md#health-workers-and-runners).
- **/amux sprawl**: [`workflows.md`](reference/workflows.md#sprawl-independent-issue-workers).
- **/amux finish**: [`workflows.md`](reference/workflows.md#finish-a-merged-worker).

Deadlines (optional): load [`reference/deadline-v1.md`](reference/deadline-v1.md) only when arming or handling deadline wake-ups—not on every `/amux` load.

Experimental provider delegation: load **`/amux-claude`** or **`/amux-pi`** only after an explicit owner request for those skills. Do not activate them from incidental mentions.

## Load only what you need

- Selectors, side effects, install: [`reference/commands.md`](reference/commands.md)
- Spawn, health, sprawl, teardown, finish: [`reference/workflows.md`](reference/workflows.md)
- Durable protocol workers must read once: [`reference/contract-v1.md`](reference/contract-v1.md)
- Coordinator deadlines: [`reference/deadline-v1.md`](reference/deadline-v1.md)
- Stuck clients / recovery: [`reference/troubleshooting.md`](reference/troubleshooting.md)
- Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Amp invocation preflight: [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md)

## Safety

- No secrets in names, workdirs, or thread IDs. Prefer temporary `--config-dir` and `--dry-run` for tests.
- Mutations are idempotent under one machine lock. On partial failure, inspect before retrying.
- Never guess a callback pane, infer finish from a wake-up, auto-release, force-delete a branch, or erase group history during finish.
- Runner commands never own remote agent threads. Teardown never applies to runners.
