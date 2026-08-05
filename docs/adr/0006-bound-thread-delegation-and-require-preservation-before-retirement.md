---
status: accepted
date: 2026-07-30
depends-on: 0005
---

# Bound thread delegation and require preservation before retirement

> **Accepted direction:** this ADR establishes the bounded delegation and preservation-before-retirement policy that [ADR 0005](0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md) reserved for a later owner decision. Acceptance does not itself change instructions, skills, commands, schemas, or runtime behavior; those adoption changes require separate review.

## Context

Low-friction thread creation caused recursive delegation, duplicated context, unclear executor placement, and difficult cleanup. Useful work can be lost or stranded when a worker cannot safely retire itself. Provider execution is available through machine-local systems such as Tycho, and Amux should not duplicate machine or provider routing.

This decision depends on [ADR 0005](0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md) and narrows delegation and retirement behavior within its maintained local lifecycle and recovery mission.

[#317](https://github.com/zainfathoni/amux/issues/317) and [#318](https://github.com/zainfathoni/amux/issues/318) are independent existing work, not dependencies created by this ADR.

## Decision

- Every task starts with a child-thread budget of zero. Zero is the default admission state, not an absolute ban: admitting one of the exceptions below grants only the children required for that bounded purpose.
- Delegation is permitted only for a required executor, machine, repository, or permission boundary; an explicitly requested independent review; or owner-approved parallelism.
- If the current executor can do the work, it performs the work directly. Children inherit a zero budget unless the owner explicitly grants otherwise.
- Runner-backed threads verify their executor, host, repository, and current working directory. If placement is wrong, they stop and report instead of spawning another runner.
- The creator owns its complete descendant tree across projects and runners, including instructions, result collection, cleanup, and archival. Each delegation retains exact parent and child thread identities in available durable task evidence; if ancestry cannot be retained, delegation fails closed. Project or runner membership does not break or transfer ancestry.
- Before retirement, a worker verifies that useful changes are committed and pushed or preserved by another explicit, retrievable method, validation is recorded, temporary artifacts are removed, and descendants are settled or explicitly dispositioned. Retirement does not require intentionally dirty useful work to be cleaned or committed: an exact retained worktree is an acceptable preservation method when its identity, state, and recovery owner are recorded. Merely leaving a dirty worktree behind without that evidence is not preservation. Indeterminate or unpreserved work fails closed.
- Physical-worktree retention is the owner's disposition after exact worktree identity, shared-use and runner checks, useful-work preservation or an owner-authorized bounded dirty-discard scope, and adjacent revalidation. An unresolved provider lifecycle may record lost recovery and retain its evidence, but it does not by itself require the independently safe worktree to remain. Provider evidence stays append-only and independently retrievable; worktree removal neither weakens exact resource or process identity nor claims complete retirement.
- Current-worker retirement is a two-stage operation. First, the worker prepares its exact thread, workspace, window, and workdir identity with preservation evidence. Then a trusted external post-report finalizer archives the thread and stops the exact client. The finalizer is a one-shot action from a verified independent executor and may reuse the existing teardown contract; it is not a resident watcher, scheduler, supervisor, or control plane. Existing finish authorization remains a separate prerequisite where its workflow requires it. Retries are idempotent, and descendants are never archived implicitly.
- Provider execution remains outside maintained Amux core. Tycho may own machine and provider routing for Claude Code and Pi/Codex Spark. The existing `/amux-tycho` route is an experimental, runtime-unverified, explicit-request-only semantic-report bridge in which the immutable real Amp origin retains coordinator, consume, and acknowledgement authority and Tycho has report-only authority; it does not establish a provider-neutral task/result package or stable Tycho/provider contract. A later design may define such a package, but this ADR selects no routing authority, package schema, or transport. `/amux-claude` and `/amux-pi` remain experimental, explicit-request-only fallback and reference paths. Forgex remains experimental and orthogonal, not an Amux replacement.

## Adoption surface

This ADR selects policy, not a carrier or enforcement mechanism. Consistent with ADR 0004's retained constraints, operating guidance reaches agents only through a separately reviewed documentation or skill change, and enforced behavior requires its own accepted design and implementation. [Issue #320](https://github.com/zainfathoni/amux/issues/320) owns the symmetric creation/retirement proposal and explicit per-resource disposition design. Its later operating-guidance adoption site is the paired-lifecycle and worktree-removal sequence under **Finish a merged worker** in `skills/amux/reference/workflows.md`; that workflow remains current until the proposal is reviewed, approved, and adopted separately.

## Consequences

Fewer threads are created, execution locality is auditable, descendant ownership is explicit, and useful work must be preserved before retirement. Delegation and cleanup become more deliberate, with fail-closed outcomes when placement or preservation cannot be established.

The cost is more owner decisions for delegation and parallelism, more stop-and-report outcomes when placement cannot be verified, and retirement that can block on indeterminate preservation. Leaving a worker and its descendants alive is the accepted trade-off when safe preservation or exact finalization cannot be established, except that an owner may separately dispose an independently safe physical worktree under the exact preservation and revalidation boundary above without claiming full retirement.

## Non-goals

This ADR does not define a scheduler, task graph, permanent Lead, provider routing, package schema or transport, provider promotion or deletion, or any runtime behavior change.

This ADR authorizes no automatic dirty-work discard. Any discard requires exact owner authorization bound to one specific validated state.

## Follow-ups

The following work still requires separate design and implementation. In particular, this ADR does not choose how budgets are represented, which existing report or receipt records preservation and ancestry evidence, or how the one-shot finalizer is invoked:

- delegation defaults;
- runner attestation;
- descendant ownership and cleanup through #320;
- prepare/finalize retirement through #320;
- a provider-neutral Amp↔Tycho package; and
- explicit status for experimental fallback paths.
