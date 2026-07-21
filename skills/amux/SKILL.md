---
name: amux
description: "Manage local Amp worker, runner, workspace, and durable work-group orchestration with amux. Use for 'Pin it', 'Unpin it', 'forget this on restore', 'Park it', 'Restart unresponsive clients', 'Shelve this', 'defer this workspace', 'hide it for now', 'Show shelved work', 'Unshelve this', 'Restore my workspace', 'Spawn a worker for', 'Coordinate issue workers', 'Delegate bounded work to Claude Opus in a fresh Amp Orb', 'Run Pi on Spark in an Amp Orb', 'Delegate read-only analysis to Claude', 'Delegate isolated mutating work to Claude', 'Recover indeterminate Claude worker evidence', 'Teardown this worker', 'Doctor amux', '/amux health', '/amux sprawl', and '/amux finish'. Health, sprawl, coordinator orchestration, experimental provider delegation, and finish are skill-only workflows, not CLI commands."
---

# amux

Use `amux` for local Amp/tmux lifecycle. A **worker** is an interactive thread-bound client. A **runner** is a non-interactive `amp --no-tui` client bound to a canonical workdir. A **workspace** groups workers and runners in one same-named tmux session.

Do not edit `workers.tsv`, `runners.tsv`, or `shelves.tsv` directly when the CLI can express the change. Use `--config-dir` or `AMUX_CONFIG_DIR` to select their directory. Run `amux help [command ...]` before assuming syntax.

## Preserve the agent contract

- Canonical worker identity is `--thread`; canonical runner identity is `--workdir`; `--workspace` selects a lifecycle group. Use long selectors in agent commands.
- Top-level `list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` aggregate workers and runners. Use `amux worker ...` or `amux runner ...` to narrow by mode.
- Bare `amux` launches workers only. `amux launch` launches both modes. Launch is the no-selector bulk exception; other machine-wide mutations require explicit `--all`.
- `spawn`, `shelve`, `unshelve`, and `teardown` are worker-only. `pin` and `unpin` require `worker` or `runner` namespace.
- Every skill-driven worker spawn MUST pass `--mode medium` unless the user explicitly requests another mode. An explicitly requested mode always wins. Do not infer `high` or `ultra` from task complexity, size, urgency, or expected duration.
- Before automatically selecting a spawn mode, creating a native Amp child, reading another Amp thread, or sending a native child message, load [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md). Run its resolver only for the supported automatic-spawn preflight. Never bypass a binding `ask` or `reject`; advisory `would_ask` and `would_reject` do not block.
- `/amux health`, `/amux sprawl`, and `/amux finish` are skill-only workflows. Never invoke or document `amux health`, `amux sprawl`, or `amux finish` as CLI commands.
- Prefer `--dry-run`; prefer `--json` for parsing. Treat exit `2` as request/preflight rejection and exit `1` as runtime failure. Never retry an indeterminate spawn blindly.
- Work-group membership and reports are durable local intent; callback leases and wake-ups are ephemeral. A `ready` report, notification, acknowledgement, stale/overdue diagnostic, or late callback never authorizes finish.
- Machine mutations are serialized. Exit `2` lock contention means no mutation occurred: wait for the prior lifecycle operation, then retry the identical operation with the same spawn key or report ID.

## Route triggers to the smallest side effect

- **Pin it**: `amux worker pin --current` when complete `AMUX_*` identity is available; otherwise use explicit `--workspace`, `--window`, `--workdir`, and `--thread` selectors. Pin changes worker configuration only. Never combine `--current` with another selector.
- **Unpin it** / **forget this on restore**: `amux worker unpin --current`. Worker unpin removes worker configuration and matching shelf intent; it does not stop or archive.
- **Park it**: `amux worker park --current`. Park stops the verified local worker while preserving configuration, shelf intent, and remote thread state.
- **Restart unresponsive clients**: use aggregate `amux restart --all`, or mode-specific `amux worker restart ...` / `amux runner restart ...`. Restart preserves configuration and remote state.
- **Shelve this** / **defer this workspace** / **hide it for now**: `amux shelve --current`, `amux shelve --thread <id>`, or `amux shelve --workspace <name>`. Shelve records shelf intent, archives the remote thread, and parks verified local workers while preserving worker configuration.
- **Show shelved work**: `amux worker list --shelf shelved` with optional `--workspace` or `--thread`.
- **Unshelve this**: `amux unshelve --current` or `--thread <id>`. Unshelve unarchives first and removes shelf intent only after success; launch separately.
- **Restore my workspace**: `amux launch --workspace <name>` for both modes, `amux worker launch --workspace <name>` for workers only, or bare `amux` for all configured workers.
- **Doctor amux**: `amux doctor --all`, `amux doctor --workspace <name>`, or a mode-specific doctor route. Doctor inspects only.
- **Spawn a worker for ...**: load [`reference/workflows.md`](reference/workflows.md), then use `amux spawn --mode medium ...` unless the user explicitly requested another mode.
- **Coordinate issue workers**: load [`reference/workflows.md`](reference/workflows.md#coordinate-a-durable-issue-work-group); inspect dependencies/overlap, use fresh `origin/main` worktrees, attach only authoritative threads to a durable group, register and verify the coordinator lease, and drive reports through acknowledgement, independent verification, explicit finish authorization, post-merge CI, merged reporting, and teardown-last finish.
- **Delegate bounded work to Claude Opus in a fresh Amp Orb**: only after an explicit provider-specific delegation request, load [`reference/claude-opus-orb-executor.md`](reference/claude-opus-orb-executor.md). Require a fresh Orb with project-secret OAuth, fail closed on API-key or ambiguous billing, and use only official headless Claude Code pinned to `claude-opus-4-8`. This is not the local interactive Claude route, Pi work, an amux CLI resource, or provider-neutral orchestration.
- **Run Pi on Spark in an Amp Orb**: only after an explicit owner request, load [`reference/pi-spark-orb-executor.md`](reference/pi-spark-orb-executor.md). This is a disposable provider-specific ChatGPT Codex OAuth experiment using exactly `openai-codex/gpt-5.3-codex-spark`; API keys, ambiguous billing, missing trusted quota evidence, automatic retry/fallback, repository authority, and credential transfer fail closed. It adds no amux resource or provider-neutral orchestration state.
- **Delegate read-only analysis to Claude**: only after an explicit delegation request, load [`reference/claude-read-only-delegation.md`](reference/claude-read-only-delegation.md). This is an unstable capability-gated Darwin/Linux local experiment, not an `amux` CLI resource or an Amp worker. Never activate it from an incidental Claude mention, available capacity, or a generic review request. Linux mutating delegation remains unavailable.
- **Delegate isolated mutating work to Claude**: only after an explicit mutating delegation request and a public Pilot 1 `pass`, load [`reference/claude-mutating-delegation.md`](reference/claude-mutating-delegation.md). This separate unstable route requires configured capacity floors, a dedicated clean worktree, exclusive writer ownership, and a one-clean-commit or zero-commit-blocked report. It never authorizes Pilot 2, push, PR mutation, merge, release, automatic parking, cleanup, or teardown.
- **Recover indeterminate Claude worker evidence**: only after explicit owner recovery authorization, load [`reference/claude-delegation-recovery.md`](reference/claude-delegation-recovery.md). Register only an exact supplied owner-private store. Use absence-only detach solely for its exact contract, retire one exact live modern report-bearing target through its own seam, or retire one exact launch-completed/acquired/no-report target through its separate seam. Both live routes require durable intent, complete identity revalidation, exact stop, and bounded terminal proof; the pre-identity acquired shape is explicitly permanently non-retirable and keeps paired teardown prohibited. Preserve the unresolved receipt, reports if any, artifacts, worktree, immutable origin, and durable origin fence; never infer, adopt, retry launch, manufacture a report or input, park, or clean.
- **Teardown this worker**: load [`reference/workflows.md`](reference/workflows.md), run its paired Claude lifecycle preflight/cleanup for the exact worker thread, then use `amux teardown --current` or `amux teardown --thread <id>` only after paired success. Teardown archives the verified thread, removes worker and shelf configuration, and stops its verified local client as the final action.
- **/amux health**: load [`reference/workflows.md`](reference/workflows.md); aggregate worker responsiveness and runner process probes, with optional workspace/mode filters.
- **/amux sprawl #12 #34 ...**: load [`reference/workflows.md`](reference/workflows.md); worker-only issue orchestration with dependency inspection before side effects.
- **/amux finish**: load [`reference/workflows.md`](reference/workflows.md); verify merge and runner ownership, clean Git/worktree state safely, then teardown the worker last.

## Workflow Orchestration

For a scheduled coordinator deadline wake-up, this diagram is the source of truth. The firing is only a wake-up; load [`reference/workflows.md`](reference/workflows.md#7-coordinator-owned-deadline-queue) and use durable group and `amux report pending/history` state to make every decision.

```diagram
┌──────────────────────┐
│ Schedule wakes thread│
└──────────┬───────────┘
           ▼
┌────────────────────────────┐
│ Load /amux; inspect group, │
│ pending, history, identity │
└──────────┬─────────────────┘
           ▼
┌────────────────────────────┐ no   ┌───────────────────────────┐
│ Exact active generation and│─────▶│ No steering; reconcile the│
│ member binding still match?│      │ nearest schedule or clear │
└──────────┬─────────────────┘      └───────────────────────────┘
           │ yes
           ▼
┌────────────────────────────┐ yes  ┌───────────────────────────┐
│ Required stage satisfied or│─────▶│ No steering; reconcile the│
│ acknowledged before expiry?│      │ nearest schedule or clear │
└──────────┬─────────────────┘      └───────────────────────────┘
           │ no
           ▼
┌────────────────────────────┐ no   ┌───────────────────────────┐
│ Deadline expired?          │─────▶│ Re-arm this nearest active│
└──────────┬─────────────────┘      │ deadline once             │
           │ yes                     └───────────────────────────┘
           ▼
┌────────────────────────────┐ yes  ┌───────────────────────────┐
│ Stop attempt already       │─────▶│ Reconcile next unhandled  │
│ recorded for generation?   │      │ generation, or clear      │
└──────────┬─────────────────┘      └───────────────────────────┘
           │ no
           ▼
┌────────────────────────────┐
│ Record one attempt; send one│
│ bounded stop instruction   │
└──────────┬─────────────────┘
           ▼
┌────────────────────────────┐
│ Reconcile next unhandled   │
│ generation, or clear       │
└────────────────────────────┘
```

## Load only the needed reference

- Exact routes, selectors, output, side effects, installation, and maintenance: [`reference/commands.md`](reference/commands.md).
- Spawn, health, sprawl, teardown, callback, and finish procedures: [`reference/workflows.md`](reference/workflows.md).
- Partial failures, stuck clients, and safe replacement: [`reference/troubleshooting.md`](reference/troubleshooting.md).
- Complete activation/routing checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md).
- Experimental Amp invocation actions, supported probes, advisory outcomes, and non-bypass rules: [`reference/amp-invocation-policy.md`](reference/amp-invocation-policy.md).
- Experimental fresh-Orb official Claude Code/Opus executor recipe: [`reference/claude-opus-orb-executor.md`](reference/claude-opus-orb-executor.md). Keep it provider-specific and separate from local interactive Claude and Pi artifacts.
- Experimental Pi/Spark Amp Orb executor preflight, OAuth, invocation, evidence, and cleanup: [`reference/pi-spark-orb-executor.md`](reference/pi-spark-orb-executor.md).
- Experimental read-only Claude definitions and protocol: [`reference/claude-delegation-contract.md`](reference/claude-delegation-contract.md); load its recovery branches only when needed from [`reference/claude-delegation-recovery.md`](reference/claude-delegation-recovery.md).
- Experimental isolated mutating Claude workflow and authority contract: [`reference/claude-mutating-delegation.md`](reference/claude-mutating-delegation.md). Keep this separate from thinker authority.

## Safety

- Do not store secrets in names, workdirs, or thread identifiers.
- Do not mutate the default config merely to test a command; use a temporary `--config-dir` and `--dry-run`.
- Mutations are idempotent desired-state operations under one bounded machine lock. Lock contention and preflight errors authorize no mutation.
- On partial failure, inspect JSON outcomes and external state before retrying. Do not duplicate threads, windows, worktrees, or operation keys.
- Never guess a missing/recycled callback pane, infer authorization from a wake-up, auto-release, force-delete a branch, or erase group history during finish. Callback failure leaves the report pending and the worker alive.
- Runner commands never own remote agent threads. Teardown never applies to runners.
