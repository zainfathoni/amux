---
status: accepted
date: 2026-07-30
supersedes: 0004
---

# Maintain amux as a local worker lifecycle and recovery tool

## Decision

Amux is maintained with a narrowed mission: it is a local Amp worker lifecycle and recovery tool. This supersedes ADR 0004's blanket maintenance-and-retirement direction, but not its valid scope constraints.

Native Amp thread creation remains preferred when it is available. Amux remains appropriate for:

- adopting an exact native-created thread while preserving its identity;
- recovering exact local worker ownership and indeterminate lifecycle operations without substitution or blind retry;
- shelving, unshelving, and teardown;
- fail-closed doctor, preflight, and dry-run operations;
- local tmux and worktree lifecycle; and
- creating a local worker when native thread creation is unavailable.

Thread, group, report, and receipt identities remain durable evidence and must be preserved across lifecycle and recovery work. Existing implemented lifecycle contracts continue to be maintained. This ADR changes product direction and documentation only; it does not change runtime behavior, schemas, commands, or compatibility.

## Evidence and retained diagnosis

ADR 0004 correctly diagnosed the costs of permanent Lead hierarchies, broad orchestration, and token-heavy coordination. Those patterns create stale context, ambiguous authority, unnecessary handoff state, and coordination cost that can exceed the bounded work itself. That diagnosis remains in force. ADR 0004 was accepted before the recovery evidence below surfaced, which is why its blanket retirement direction is reversed here so soon after acceptance while its constraints are retained.

Recent production evidence from [Amp thread T-019fad29-0354-746a-8523-5d188a167c99](https://ampcode.com/threads/T-019fad29-0354-746a-8523-5d188a167c99) also demonstrated that the narrower lifecycle substrate is actively valuable. In that recovery, exact `amux worker adopt`, fail-closed dry runs and doctor checks, local tmux/worktree recovery, and preservation of thread/group/report/receipt identity supported safe continuation without inventing replacement identities or discarding durable evidence. This operational value invalidates blanket retirement of Amux, not the constraints on orchestration expansion.

ADR 0001 remains the accepted agent-first lifecycle contract. ADR 0003 remains the preferred native-create/explicit-adoption architecture and its projectless local-creation exception remains narrow. [ADR 0006](0006-bound-thread-delegation-and-require-preservation-before-retirement.md) supplies the bounded delegation and preservation-before-retirement policy that this ADR reserved for a later owner decision. ADR 0002's generalized expansion horizons remain superseded through ADR 0004 and this ADR; they are not restored as roadmap commitments.

## Scope boundaries

### Maintained mission

Amux may improve deterministic local lifecycle, exact adoption and recovery, diagnostics, preflight, cleanup safety, and identity-preserving state needed for those responsibilities. Work groups, reports, callbacks, receipts, and related metadata may remain where they preserve correlation, recovery evidence, or safe lifecycle authorization. Their existence does not imply a permanent coordinator architecture.

### Non-goals

Amux will not pursue:

- permanent per-machine or cross-project Lead hierarchies, Lead tenure, rotation, or indefinite portfolio handoff;
- universal delegation or an assumption that every task needs an Amux coordinator;
- broad orchestration, durable generalized task graphs, schedulers, supervisor loops, or a provider-neutral control plane;
- expanding provider-policy, capacity-routing, or provider-neutral receipt machinery merely because another provider or quota is available; or
- a parallel replacement for native Amp thread creation, messaging, or execution placement.

Task-scoped coordination may use existing group and report contracts when their durable identity or finish-safety properties are useful. It must remain bounded to the task or explicit workflow, and must not become a standing Lead persona or hierarchy.

## Native Amp and provider boundary

Use native Amp thread creation when available, then use exact Amux adoption when local lifecycle ownership or recovery is needed. Use Amux's local creation path only where native creation is unavailable and preserve its fail-closed, retained-indeterminate contract.

Provider execution is outside Amux's maintained lifecycle and recovery core. Tycho may own machine and provider routing for Claude Code and Pi/Codex Spark. `/amux-tycho` is an experimental, runtime-unverified, explicit-request-only semantic-report bridge: the immutable real Amp origin retains coordinator, consume, and acknowledgement authority, while Tycho has report-only authority. `/amux-claude` and `/amux-pi` remain experimental, explicit-request-only fallback and reference paths. None of these routes widens core Amux into provider-policy machinery. Forgex remains experimental and orthogonal, not an Amux replacement.

ADR 0006 now defines delegation admission without selecting its operating-guidance carrier or enforcement mechanism. A later design may define a provider-neutral Amp↔Tycho task/result package; neither this ADR nor ADR 0006 specifies its routing, package schema, authority, transport, or promotion criteria.

## Issue governance

Inventory and metrics remain useful when they help prioritize lifecycle safety, compatibility, or scope decisions. They are not prerequisites for continuing to maintain Amux and are not automatic deprecation gates. Any future deprecation or removal still requires an explicit owner decision with compatibility and durable-state consequences assessed at that time.

Permanent Lead tenure remains closed as not planned. New proposals bear the burden of showing that they fit the maintained local lifecycle/recovery mission rather than rebuilding the retired orchestration direction under a new name.

## Consequences

Users can rely on Amux as a maintained local Amp lifecycle and recovery tool while preferring native Amp creation and transport. Safe identity-preserving recovery is a first-class product value rather than merely a temporary migration aid.

The narrower mission rejects both extremes: Amux is neither on blanket retirement nor a universal agent-orchestration framework. This preserves the useful safety substrate without reviving permanent Lead tenure or open-ended provider and coordination machinery.

## Owner decisions reserved

The following still require explicit owner decisions:

- any command, schema, or durable-state deprecation or removal;
- any operating-guidance, schema, or runtime adoption of ADR 0006, and any provider-neutral Amp↔Tycho task/result package;
- any proposed stable Forgex integration or change in its relationship to Amux;
- any expansion that introduces new orchestration authority rather than lifecycle/recovery safety; and
- promotion of `/amux-tycho`, `/amux-claude`, or `/amux-pi` from experimental status.
