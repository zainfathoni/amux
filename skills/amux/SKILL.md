---
name: amux
description: "Operates the retained machine-local Amux runner registry, exact workdir bindings, automatic tmux/Amp launch, diagnostics, maintenance, and fail-closed runner lifecycle. Routes new delegated work to native Amp child threads without Amux workers, spawn/adoption, groups, reports, callbacks, deadlines, shelves, or finish state. Use for runner pin/list/launch/doctor/park/restart/remove/reconcile, runner workspaces, 'Pin it', /amux health, /amux sprawl, and the separately owner-gated read-only /amux sweep. Experimental Tycho, Claude, and Pi routes are separate explicit-only skills."
---

# amux

Thin machine-local Amp/tmux runner host. **Runner** = `amp --no-tui` process bound to one canonical workdir. **Workspace** = same-named tmux session grouping runners.

## Contract

- Canonical runner identity is `--workdir`; `--workspace` selects a runner lifecycle group. Use long selectors.
- Top-level `list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` are runner-only aliases. Bare `amux` launches all configured runners. Other machine-wide mutations require explicit `--all`.
- Runner pin/list/launch/doctor/park/restart/remove and minimum fail-closed reconcile are active operations. Graphical-login/boot integration runs `amux launch --all`; systemd/launchd activate Amux and retain its process group rather than replacing it.
- The `worker`, `spawn`, shelf, teardown, group, report, callback, deadline, and finish surfaces are removed. Never attempt their historical syntax, edit their stores, or manufacture a compatibility transition.
- Native-created work receives no Amux worker, adoption, group, report, callback, deadline, shelf, finish authorization, or lifecycle instructions.
- For delegated work, use authenticated native Amp `create_thread` on the exact intended Workspace Project and Orb, or exact live runner and intended workdir. Keep the native parent/reply route. Do not call the child an Amux worker.
- Before automatic mode selection, native child creation, another-thread reads, or native child messages, load [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md). Never bypass a binding `ask` or `reject`.
- Prefer `--dry-run` and `--json`. Exit `2` means preflight rejection; exit `1` means runtime failure after mutation may have begun.
- `/amux health`, `/amux sprawl`, and `/amux sweep` are skill-only. Never invoke them as CLI commands.
- `/amux sweep` is the protected one-time #360 read-only inventory. Run it only after a separate exact owner authorization. Its sunset remains inactive until owner acceptance/disposition and explicit no-repeat confirmation.

## Route triggers

- **Pin it** / **Pin this runner**: `amux runner pin --workspace <name> --workdir <existing-directory>` or `--current`.
- **List runners**: `amux runner list --all` or a canonical scope.
- **Restore my workspace**: `amux launch --workspace <name>`.
- **Park it**: `amux runner park --current` or exact workdir.
- **Restart unresponsive runners**: use the exact runner scope; preserve rows and fail closed on ambiguity.
- **Doctor amux**: `amux doctor --all` or scoped runner doctor.
- **Remove/unpin/reconcile a runner**: dry-run first. Current row deletion fails closed without authoritative process/catalog absence evidence; preserve the row on any blocker.
- **Spawn a worker for / delegate work**: this means native `create_thread`, never Amux spawn/adoption. Load [`reference/workflows.md`](reference/workflows.md).
- **Coordinate child threads**: use native parent/reply routing, messaging, and waiting only.
- **/amux health**: [`workflows.md`](reference/workflows.md#health-runners).
- **/amux sprawl**: [`workflows.md`](reference/workflows.md#sprawl-independent-issue-threads).
- **/amux sweep**: [`workflows.md`](reference/workflows.md#sweep-worktree-inventory).

Experimental external execution is explicit-only. Load `/amux-tycho`, `/amux-claude`, or `/amux-pi` only on an explicit owner request. For `/amux-tycho`, the receipt's immutable real Amp origin remains coordinator and consume/acknowledgement authority. Tycho is a typed report-only producer with no group, member, callback, finish, label, provider-identity, or lifecycle authority. An owner-authorized external Tycho second opinion must never grant Tycho GitHub review mutation or readiness promotion. Its receipts are separate from the removed worker-report store and remain until #328 passes.

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
