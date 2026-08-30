---
name: amux-claude
description: "Experimental Amp-to-Claude delegation for amux. Use only after an explicit owner request to delegate read-only or mutating work to Claude, adopt owner-created Claude Code tmux windows on a physical host, run Claude Opus in a fresh Amp Orb, or recover indeterminate Claude delegation evidence. Not an amux lifecycle CLI resource."
---

# amux-claude (experimental)

Unstable skill-owned Claude delegation. **Not** an `amux` worker, runner, group member, or compatibility promise. Do not activate from incidental Claude mentions, available capacity, or generic review requests.

Core Amux worker teardown and finish have been removed. Any worker-bound provider route must reject before receipt creation, launch, parking, retirement, teardown, or other provider mutation. Preserve existing provider evidence and do not substitute an older binary, direct process kill, evidence rewrite, or alternate cleanup authority.

Before choosing an executor, consult the repository's [provider executor readiness matrix](https://github.com/zainfathoni/amux/blob/main/docs/provider-executor-readiness.md). A CLI flag, model name, or prior run does not broaden the route-specific authority recorded there.

## Route triggers

- **Delegate machine-local read-only analysis to Claude**: the historical helper route is closed because it requires removed Amux worker lifecycle; load [`reference/claude-read-only-delegation.md`](reference/claude-read-only-delegation.md) only to report that blocker.
- **Delegate isolated machine-local mutating work to Claude**: unavailable for the same reason; load [`reference/claude-mutating-delegation.md`](reference/claude-mutating-delegation.md) only to report that blocker.
- **Adopt owner-created Claude Code tmux windows on an explicitly selected physical host**: load [`reference/claude-local-tmux-adoption.md`](reference/claude-local-tmux-adoption.md). This is operator-assisted coordination of exact semantic `session:window` targets, not managed local delegation or a fresh Orb. Never substitute an Orb, API call, new Claude process, or managed receipt route.
- **Delegate bounded read-only work to Claude Opus in a fresh Amp Orb**: load [`reference/claude-opus-orb-executor.md`](reference/claude-opus-orb-executor.md). Fresh Orb, project-secret OAuth, exact `claude-opus-4-8`; fail closed on API-key or ambiguous billing.
- **A fresh-Orb repository mutation is requested**: load [`reference/claude-opus-orb-mutating.md`](reference/claude-opus-orb-mutating.md) and block because the required native authority adapters do not exist. Never substitute local tmux parking, caller assertions, or the read-only route.
- **Recover indeterminate Claude worker evidence**: only after explicit owner recovery authorization, load [`reference/claude-delegation-recovery.md`](reference/claude-delegation-recovery.md).

## Load only what you need

- Shared contract: [`reference/claude-delegation-contract.md`](reference/claude-delegation-contract.md)
- Operator-assisted local tmux adoption: [`reference/claude-local-tmux-adoption.md`](reference/claude-local-tmux-adoption.md)
- Recovery branches: [`reference/claude-delegation-recovery.md`](reference/claude-delegation-recovery.md)
- Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Helper: `experimental/claude-delegation/claude_delegation.py` within this installed skill

## Safety

- Explicit-only. No autonomous fan-out or quota filling.
- Preserve unresolved receipts, reports, artifacts, worktrees, processes, and origin fences byte-for-byte.
- Never infer ownership from names, cwd, PID, issue number, tmux placement, or Claude session ID alone.
- Worker-bound recovery is read-only diagnosis. Do not plan or apply quarantine, park, detach, retire, dispose, teardown, or fence release.
