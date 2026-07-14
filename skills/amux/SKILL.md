---
name: amux
description: "Manage local Amp worker, runner, and workspace lifecycle with amux. Use for 'Pin it', 'Unpin it', 'forget this on restore', 'Park it', 'Restart unresponsive clients', 'Shelve this', 'defer this workspace', 'hide it for now', 'Show shelved work', 'Unshelve this', 'Restore my workspace', 'Spawn a worker for', 'Teardown this worker', 'Doctor amux', '/amux health', '/amux sprawl', and '/amux finish'. Health, sprawl, and finish are skill-only workflows, not CLI commands."
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
- `/amux health`, `/amux sprawl`, and `/amux finish` are skill-only workflows. Never invoke or document `amux health`, `amux sprawl`, or `amux finish` as CLI commands.
- Prefer `--dry-run`; prefer `--json` for parsing. Treat exit `2` as request/preflight rejection and exit `1` as runtime failure. Never retry an indeterminate spawn blindly.

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
- **Teardown this worker**: `amux teardown --current` or `amux teardown --thread <id>`. Teardown archives the verified thread, removes worker and shelf configuration, and stops its verified local client.
- **/amux health**: load [`reference/workflows.md`](reference/workflows.md); aggregate worker responsiveness and runner process probes, with optional workspace/mode filters.
- **/amux sprawl #12 #34 ...**: load [`reference/workflows.md`](reference/workflows.md); worker-only issue orchestration with dependency inspection before side effects.
- **/amux finish**: load [`reference/workflows.md`](reference/workflows.md); verify merge and runner ownership, clean Git/worktree state safely, then teardown the worker last.

## Load only the needed reference

- Exact routes, selectors, output, side effects, installation, and maintenance: [`reference/commands.md`](reference/commands.md).
- Spawn, health, sprawl, teardown, callback, and finish procedures: [`reference/workflows.md`](reference/workflows.md).
- Partial failures, stuck clients, and safe replacement: [`reference/troubleshooting.md`](reference/troubleshooting.md).
- Complete activation/routing checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md).

## Safety

- Do not store secrets in names, workdirs, or thread identifiers.
- Do not mutate the default config merely to test a command; use a temporary `--config-dir` and `--dry-run`.
- Mutations are idempotent desired-state operations under one bounded machine lock. Lock contention and preflight errors authorize no mutation.
- On partial failure, inspect JSON outcomes and external state before retrying. Do not duplicate threads, windows, worktrees, or operation keys.
- Runner commands never own remote agent threads. Teardown never applies to runners.
