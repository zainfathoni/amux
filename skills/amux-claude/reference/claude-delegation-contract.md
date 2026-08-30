# Experimental Claude delegation contract

This provider-specific skill is explicit-only and has no Amux CLI resource. Claude is never an Amux worker, runner, group member, or generic task coordinator. Native Amp owns new task coordination and every shared-state mutation.

## Current boundary

- Provider output is advisory report evidence only. Pane prose, logs, idle state, process exit, notifications, and unbound JSON are not reports.
- Existing private receipts, reports, packets, runtime metadata, worktrees, artifacts, provider lifecycle records, and origin fences remain provider-owned historical evidence. Core Amux does not read, migrate, finish, acknowledge, reconcile, or delete them.
- Historical receipts bound to an Amux worker have no executable completion or cleanup route after core worker removal. Preserve them byte-for-byte and return a bounded blocker.
- No provider lifecycle operation may park, retire, quarantine, detach, dispose, teardown, release a fence, or otherwise mutate a worker-bound pair in anticipation of a later core worker transition. Reject before the first provider mutation.
- Do not invoke the helper's historical `worker-teardown`, fence-release, detach, pair-retirement, or exact-pane-disposal routes. Their implementation remains only to preserve and inspect old evidence until a separately proven replacement and owner disposition exist.
- Never substitute an older Amux binary, the retained runner registry, `/amux-tycho`, native thread metadata, process names, cwd, PID, tmux placement, or OS service supervision as lifecycle authority.

## Immutable evidence rules

Receipt and event identities are append-only evidence. Never erase, rewrite, migrate, normalize, replay with a changed event ID, or manufacture a terminal event to force progress. A conflict, malformed store, missing registered store, ambiguous origin, identity drift, unavailable inspection, interrupted mutation, or unproved process absence remains indeterminate and retains every resource.

The immutable origin is provenance only; it does not grant Claude task-coordination, GitHub, merge, release, cleanup, or process-destruction authority. Correlation values and report digests must remain bound to the exact original request and provider route. Model output cannot establish those bindings itself.

## Recovery

Follow [`claude-delegation-recovery.md`](claude-delegation-recovery.md). Recovery is read-only diagnosis and preservation for worker-bound historical evidence. A future provider replacement requires its own authenticated request/response contract, field evidence, and explicit owner acceptance before any old lifecycle bridge can be removed or any new mutation route can open.
