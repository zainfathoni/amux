---
status: accepted
date: 2026-09-03
---

# Add machine-local runner teardown without reviving worker teardown

`amux runner teardown` retires one exact local runner and its clean attached secondary Git worktree, then unpins its exact registry row. It is deliberately runner-scoped: Amp continues to own native thread archival, the local branch is preserved, historical worker/coordination state remains inert, and top-level `amux teardown` remains the removed worker-teardown tombstone.

The destructive action requires a fresh dry-run digest bound to runner process incarnation, worktree identity, branch/HEAD, and registry bytes. Apply revalidates each boundary and orders effects as stop runner → remove worktree → unpin row, so interruption leaves either a normal retained runner or a missing-workdir row recoverable through a new plan. Bulk/current selectors, primary or detached worktrees, hidden or undeclared content, unsafe Git states, and ambiguous runtime ownership are rejected rather than guessed.

## Deferred alias

A future top-level alias may be considered only after field use shows that the runner meaning is unambiguous and the former worker meaning no longer creates compatibility risk. No date or automatic alias is selected by this decision.
