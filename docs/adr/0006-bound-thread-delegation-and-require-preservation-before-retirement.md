---
status: proposed
date: 2026-07-30
depends-on: 0005
---

# Bound thread delegation and require preservation before retirement

## Context

Low-friction thread creation caused recursive delegation, duplicated context, unclear executor placement, and difficult cleanup. Useful work can be lost or stranded when a worker cannot safely retire itself. Provider execution is available through machine-local systems such as Tycho, and Amux should not duplicate machine or provider routing.

This decision depends on [ADR 0005](0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md) and narrows delegation and retirement behavior within its maintained local lifecycle and recovery mission.

## Decision

- Every task starts with a child-thread budget of zero.
- Delegation is permitted only for a required executor, machine, repository, or permission boundary; an explicitly requested independent review; or owner-approved parallelism.
- If the current executor can do the work, it performs the work directly. Children inherit a zero budget unless the owner explicitly grants otherwise.
- Runner-backed threads verify their executor, host, repository, and current working directory. If placement is wrong, they stop and report instead of spawning another runner.
- The creator owns its complete descendant tree across projects and runners, including instructions, result collection, cleanup, and archival. Project or runner membership does not break ancestry.
- Before retirement, a worker verifies that useful changes are committed and pushed or preserved by another explicit method, validation is recorded, temporary artifacts are removed, and descendants are settled or explicitly dispositioned. Indeterminate or unpreserved work fails closed.
- Current-worker retirement is a two-stage operation. First, the worker prepares its exact thread, workspace, window, and workdir identity with preservation evidence. Then a trusted external post-report finalizer archives the thread and stops the exact client. Retries are idempotent, and descendants are never archived implicitly.
- Provider execution remains outside maintained Amux core. Tycho may execute Claude Code and Pi, including Codex Spark. A later design may define a provider-neutral Amp↔Tycho package. `/amux-claude`, `/amux-pi`, and Forgex remain experimental fallback or reference paths. This ADR accepts no stable Tycho or provider contract.

## Consequences

Fewer threads are created, execution locality is auditable, descendant ownership is explicit, and useful work must be preserved before retirement. Delegation and cleanup become more deliberate, with fail-closed outcomes when placement or preservation cannot be established.

## Non-goals

This ADR does not define a scheduler, task graph, permanent Lead, provider routing, package schema or transport, provider promotion or deletion, or any runtime behavior change.

## Follow-ups

- delegation defaults;
- runner attestation;
- descendant ownership and cleanup;
- prepare/finalize retirement;
- a provider-neutral Amp↔Tycho package; and
- explicit status for experimental fallback paths.

[#317](https://github.com/zainfathoni/amux/issues/317) and [#318](https://github.com/zainfathoni/amux/issues/318) are independent existing work, not dependencies created by this ADR.
