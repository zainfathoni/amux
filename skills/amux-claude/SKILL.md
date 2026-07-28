---
name: amux-claude
description: "Experimental Amp-to-Claude delegation for amux. Use only after an explicit owner request to delegate read-only or mutating work to Claude, run Claude Opus in a fresh Amp Orb, or recover indeterminate Claude delegation evidence. Not an amux lifecycle CLI resource."
---

# amux-claude (experimental)

Unstable skill-owned Claude delegation. **Not** an `amux` worker, runner, group member, or compatibility promise. Do not activate from incidental Claude mentions, available capacity, or generic review requests.

Core Amp lifecycle remains `/amux`. Paired worker teardown and finish in `/amux` call into this skill's lifecycle helper when Claude pairs may exist.

## Route triggers

- **Delegate read-only analysis to Claude**: load [`reference/claude-read-only-delegation.md`](reference/claude-read-only-delegation.md).
- **Delegate isolated mutating work to Claude**: only after public Pilot 1 `pass`, load [`reference/claude-mutating-delegation.md`](reference/claude-mutating-delegation.md).
- **Delegate bounded read-only work to Claude Opus in a fresh Amp Orb**: load [`reference/claude-opus-orb-executor.md`](reference/claude-opus-orb-executor.md). Fresh Orb, project-secret OAuth, exact `claude-opus-4-8`; fail closed on API-key or ambiguous billing.
- **Delegate one bounded repository mutation to Claude Opus in a fresh Amp Orb**: only after a separate exact owner request, load [`reference/claude-opus-orb-mutating.md`](reference/claude-opus-orb-mutating.md). This is a distinct durable Orb lifecycle; never substitute local tmux parking or the read-only route.
- **Recover indeterminate Claude worker evidence**: only after explicit owner recovery authorization, load [`reference/claude-delegation-recovery.md`](reference/claude-delegation-recovery.md).

## Load only what you need

- Shared contract: [`reference/claude-delegation-contract.md`](reference/claude-delegation-contract.md)
- Recovery branches: [`reference/claude-delegation-recovery.md`](reference/claude-delegation-recovery.md)
- Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Helper: `experimental/claude-delegation/claude_delegation.py` within this installed skill
- Fresh-Orb mutating helper: `experimental/fresh-orb-mutating/fresh_orb_workflow.py`

## Safety

- Explicit-only. No autonomous fan-out or quota filling.
- Preserve unresolved receipts, reports, artifacts, worktrees, and origin fences unless a named recovery seam authorizes a specific terminal proof.
- Never infer ownership from names, cwd, PID, issue number, tmux placement, or Claude session ID alone.
- For `receipt_store_invalid_or_unavailable`, only this provider skill may plan, apply, or inspect quarantine. It requires an explicit owner-bound exact `park`; it never authorizes cleanup, detach, archive, removal, teardown, or evidence repair.
