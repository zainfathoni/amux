---
name: amux
description: "Drains existing local Amux worker, runner, and workspace state plus proven pre-cutover compatibility/drain state for work groups, reports, callback leases, deadlines, and finish authorization, while routing new Amp work to native child threads. Use for pin/unpin/park/restart/shelve/unshelve/launch, bounded projectless host placement, doctor, teardown, /amux health, /amux sprawl, the one-time staged-drain /amux sweep, pre-cutover /amux finish, child-thread coordination, 'forget this on restore', 'hide it for now', 'defer this workspace', 'Show shelved work', and 'Restore my workspace'. Experimental Tycho, Claude, or Pi routes remain separate explicit-only skills."
---

# amux

Local Amp/tmux lifecycle. **Worker** = interactive thread-bound client. **Runner** = `amp --no-tui` bound to a canonical workdir. **Workspace** = same-named tmux session grouping both.

Do not edit registries when the CLI can express the change. Run `amux help [command ...]` before assuming syntax. [`reference/contract-v1.md`](reference/contract-v1.md) is compatibility-only: use it only for an exact pre-cutover Amux-managed spawn, adopt, or group flow whose persisted provenance proves it is drain-eligible. Never put its path or lifecycle instructions in a native `create_thread` prompt.

## Preserve the agent contract

- Canonical worker identity is `--thread`; runner identity is `--workdir`; `--workspace` selects a lifecycle group. Use long selectors.
- Top-level `list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` aggregate both modes. Narrow with `amux worker ...` or `amux runner ...`.
- Bare `amux` launches workers only. `amux launch` launches both. Other machine-wide mutations need explicit `--all`.
- Generalized `amux spawn` admission closed at `spawn-native-cutover-v1`. When delegated work needs a native child, use authenticated native Amp `create_thread` on the exact intended Workspace Project and Orb, or exact live runner and its intended workdir. Give the child a lean task prompt—task, acceptance criteria, relevant constraints, validation, and expected reply—and retain only native parent/reply routing. Do not call it an Amux worker or spawned worker. One coherent issue or PR review may run on the direct coordinator without a child.
- Native-created work receives no Amux contract path, receipt, report, callback, group, deadline, finish authorization, or automatic adoption. Do not add these exclusions to the child prompt; enforce them in the parent by creating no Amux lifecycle state.
- Every native creation and bounded exception uses an explicit mode. When a linked ChatGPT subscription and target-mode availability are known, choose `low` for small mechanical work, `medium` for ordinary implementation, or `high` for hard architecture, debugging, or review. Otherwise use `medium`. An explicitly requested mode always wins; `ultra`, plugin, and other special modes remain explicit-only.
- **Invocation defaults:** mode labels are capability presets, not stable model or cost selectors. Preserve `medium` when mode or subscription routing is unknown; do not Read Thread for task context; give Oracle supplied diff/context only—never Read Thread to feed Oracle.
- Before automatic mode selection, creating a native Amp child, reading another Amp thread, or sending a native child message, load [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md). Never bypass a binding `ask` or `reject`. Read Thread remains instruction-only until a promoted gate exists—still do not use it without explicit permission.
- `/amux health`, `/amux sprawl`, `/amux sweep`, and `/amux finish` are skill-only. Never invoke `amux health|sprawl|sweep|finish` as CLI commands.
- Prefer `--dry-run` and `--json`. Exit `2` = preflight rejection; exit `1` = runtime failure. Use the projectless physical-host exception only after one exact owner authorization and only when native project-backed Orb/runner creation cannot express the placement. Bind `--owner-authorized-projectless-physical-host`, byte-exact `--physical-host`, and canonical `--workdir` before prepare; create no group. Repeat the authorization/host flags for arm/finalize, issue exactly one authenticated native message after arm, and never retry, fall back, reroute, rebind, adopt, paste, press Enter, clean up, archive, search, reconcile, or use another receiver.
- Pre-cutover work-group reports, callback leases, and finish authorization are compatibility/drain state only. Their wake-ups never authorize finish, and a `ready` report is not cleanup authority.

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
- **Spawn a worker for ...**: treat this wording as a request for a native child thread, not an Amux spawn or worker. Load [`reference/workflows.md`](reference/workflows.md), make one authenticated native `create_thread` call on the exact intended Orb or exact live runner/workdir, send only the lean task prompt, and leave the returned thread unmanaged by Amux. For an exact owner-authorized projectless physical-host placement only, follow the capability-gated exception workflow.
- **Coordinate child threads**: load [`reference/workflows.md`](reference/workflows.md#coordinate-native-child-threads-and-drain-a-durable-work-group).
- **Teardown this worker**: load [`reference/workflows.md`](reference/workflows.md#teardown-a-worker). If `/amux-claude` pairs may exist, run that skill's paired lifecycle preflight first; then `amux teardown` last.
- **/amux health**: [`workflows.md`](reference/workflows.md#health-workers-and-runners).
- **/amux sprawl**: [`workflows.md`](reference/workflows.md#sprawl-independent-issue-threads).
- **/amux sweep**: one-time staged-drain inventory only; [`workflows.md`](reference/workflows.md#sweep-worktree-inventory).
- **/amux finish**: pre-cutover existing-worker compatibility/drain only; [`workflows.md`](reference/workflows.md#finish-a-merged-worker).
- **Before any worktree remove or prune path**: load [`reference/removal-safety.md`](reference/removal-safety.md).

Deadlines (compatibility only): load [`reference/deadline-v1.md`](reference/deadline-v1.md) only when handling an existing pre-cutover deadline wake-up—not on every `/amux` load and never to create deadline state for native work.

Experimental external execution is explicit-only. Load **`/amux-tycho`** for the Tycho report bridge and any owner-authorized external Tycho second opinion on an authoritative Amp `/team-review`, or **`/amux-claude`** / **`/amux-pi`** for their provider-specific fallback routes; never activate them from incidental mentions. For `/amux-tycho`, a receipt's immutable real Amp origin remains coordinator and consume/acknowledgement authority. Tycho is a typed report-only producer with no group, member, callback, finish, label, provider-identity, or lifecycle authority. Owner-selected Tycho second opinions stay under that separate skill and never grant Tycho GitHub review mutation or readiness promotion.

## Load only what you need

- Selectors, side effects, install: [`reference/commands.md`](reference/commands.md)
- Spawn, health, sprawl, sweep, teardown, finish: [`reference/workflows.md`](reference/workflows.md)
- Proven pre-cutover drain workers may read once: [`reference/contract-v1.md`](reference/contract-v1.md)
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
