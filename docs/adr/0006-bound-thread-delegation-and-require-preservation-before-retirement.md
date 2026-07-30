---
status: proposed
date: 2026-07-30
depends-on: 0005
---

# Bound thread delegation and require preservation before retirement

> **Proposed direction:** this ADR is not operative. Its rules below are written in the present tense as the proposed contract, not as current policy, and they bind nothing until the owner accepts this ADR. Acceptance requires the explicit owner decision that [ADR 0005](0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md) reserved for delegation admission. Nothing here changes instructions, skills, commands, schemas, or runtime behavior on merge.

## Context

Low-friction thread creation caused recursive delegation, duplicated context, unclear executor placement, and difficult cleanup. Useful work can be lost or stranded when a worker cannot safely retire itself. Provider execution is available through machine-local systems such as Tycho, and Amux should not duplicate machine or provider routing.

This decision depends on [ADR 0005](0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md) and narrows delegation and retirement behavior within its maintained local lifecycle and recovery mission.

[#317](https://github.com/zainfathoni/amux/issues/317) and [#318](https://github.com/zainfathoni/amux/issues/318) are independent existing work, not dependencies created by this ADR.

## Decision

- Every task starts with a child-thread budget of zero.
- Delegation is permitted only for a required executor, machine, repository, or permission boundary; an explicitly requested independent review; or owner-approved parallelism.
- If the current executor can do the work, it performs the work directly. Children inherit a zero budget unless the owner explicitly grants otherwise.
- Runner-backed threads verify their executor, host, repository, and current working directory. If placement is wrong, they stop and report instead of spawning another runner.
- The creator owns its complete descendant tree across projects and runners, including instructions, result collection, cleanup, and archival. Project or runner membership does not break ancestry.
- Before retirement, a worker verifies that useful changes are committed and pushed or preserved by another explicit method, validation is recorded, temporary artifacts are removed, and descendants are settled or explicitly dispositioned. Indeterminate or unpreserved work fails closed.
- Current-worker retirement is a two-stage operation. First, the worker prepares its exact thread, workspace, window, and workdir identity with preservation evidence. Then a trusted external post-report finalizer archives the thread and stops the exact client. Retries are idempotent, and descendants are never archived implicitly.
- Provider execution remains outside maintained Amux core. Tycho may own machine and provider routing for Claude Code and Pi/Codex Spark. A later design may define a provider-neutral Amp↔Tycho package. `/amux-claude` and `/amux-pi` remain experimental, explicit-request-only fallback and reference paths, and Forgex remains experimental and orthogonal, not an Amux replacement. This ADR accepts no stable Tycho or provider contract and relaxes none of ADR 0005's provider qualifiers.

## Adoption surface

This ADR selects no carrier for the rules above. Consistent with ADR 0004's phase-2 constraint, any operating-guidance change reaches agents only through a separately reviewed documentation or skill change — for example `docs/snippets/global-agents-amux-prefs.md` or `skills/amux/SKILL.md` — and any enforced behavior would require its own accepted decision. Merging this ADR authorizes neither.

## Consequences

Fewer threads are created, execution locality is auditable, descendant ownership is explicit, and useful work must be preserved before retirement. Delegation and cleanup become more deliberate, with fail-closed outcomes when placement or preservation cannot be established.

The cost is more owner decisions in the loop for delegation and parallelism, more stop-and-report outcomes when placement cannot be verified, and retirement that can block on indeterminate preservation instead of completing. Blocked retirement leaving a worker and its descendants alive is the accepted trade for not discarding unpreserved work.

## Non-goals

This ADR does not define a scheduler, task graph, permanent Lead, provider routing, package schema or transport, provider promotion or deletion, or any runtime behavior change.

## Follow-ups

None of the following is filed as an issue yet, and each still owes definitions this ADR deliberately leaves open — notably how a child-thread budget is counted and tracked, where preservation evidence and recorded validation live among existing report and receipt identities, and which actor the trusted external post-report finalizer is relative to the existing independent-executor teardown contract:

- delegation defaults;
- runner attestation;
- descendant ownership and cleanup;
- prepare/finalize retirement;
- a provider-neutral Amp↔Tycho package; and
- explicit status for experimental fallback paths.
