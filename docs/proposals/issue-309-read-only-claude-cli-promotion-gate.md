---
status: completed
issue: 309
base: b6999abd09e5e58215baf67730325d8b19a87423
decision: repeat-keep-experimental
---

# Read-only Claude Go CLI promotion gate

## Decision

**Repeat / keep experimental.** The local Darwin read-only `/amux-claude` route is operational and useful, but it remains skill-owned behind the experimental Python helper. Do not add a stable Go `amux` command, JSON contract, receipt format, migration, or first-class Claude resource yet.

This is a narrow read-only decision. It does not change the [#151 `stop/narrow` mutating decision](issue-151-mutating-claude-pilot-2-evaluation.md), authorize general Claude or provider-neutral promotion, or broaden the blocked fresh-Orb and Linux mutation routes.

## Candidate held for later reconsideration

A future stable candidate may cover only one explicit-owner, same-machine, policy-constrained read-only delegation:

- one dedicated clean worktree;
- no Bash, Edit, Write, Web, agent, mutation, fallback, or retry authority;
- one exact owner-authorized approved model, or explicit model omission that remains unattested;
- immutable launch and process identity, one valid durable report, separate consumption and acknowledgement, and exact verified parking;
- no autonomous selection, automatic input, automatic parking, teleport, generic Claude resource, or OS-sandbox claim.

The later decision must choose a Darwin-only stable surface or require accepted Linux runtime evidence before claiming portability.

## Evidence accepted

| Evidence | Accepted conclusion | Does not prove |
|---|---|---|
| [#149 Darwin read-only pilot](issue-149-read-only-claude-pilot-result.md) | Exact client identity, duplicate/conflict handling, durable semantic report, delivery, separate acknowledgement, useful output, and verified parking worked in one real task. | Current recovery behavior, stable storage/API ownership, Linux runtime, or promotion. |
| [#305 v0.3.6 exact Opus 5 run](https://github.com/zainfathoni/amux/issues/305#issuecomment-5115358465) | Exact `claude-opus-5` survived policy, argv and command digests, immutable receipt, acquisition, valid report, consumption, acknowledgement, and verified parking; the result was actionable. | Later entitlement or availability, autonomous capacity, mutation, recovery, or portability. |
| Focused synthetic coverage | Exact model drift, receipt transitions, process replacement, replay, malformed evidence, and many fail-closed branches are mechanically checked. | A real current-contract failure recovered without data loss or wrong-process action. |

The normal local Darwin read-only loop is therefore established. Another ordinary successful Darwin happy-path run adds little promotion information by itself.

## Why Go is not yet the reliability fix

The current helper completed both accepted real runs. No field failure has been attributed to Python availability, packaging, interpreter behavior, or helper type errors. A Go implementation could eventually provide one binary, compile-time types, and the stable amux JSON envelope, but it would not by itself resolve provider entitlement, capacity ambiguity, policy confinement, process replacement, interrupted delivery, interrupted parking, or Linux runtime proof.

The apparent read-only slice also shares one lock and receipt state machine with mutating receipts, teardown fences, quarantine, legacy-store registration, indeterminate launch recovery, retirement, parking, and cleanup eligibility. Moving only the normal read-only path would either split that safety state across implementations or force the stable CLI to absorb experimental recovery and historical compatibility obligations. The stable ownership boundary must be chosen before a port begins.

“Read-only” remains a tool and policy authority claim: no Bash/Edit/Write authority is granted. It is not proof of OS-level filesystem confinement, and a future CLI must not strengthen that wording.

## Required gates before reconsideration

1. **Current recovery evidence:** one accepted real delivery or lifecycle failure recovered through a current supported receipt-preserving path. Do not manufacture a failure merely to pass this gate.
2. **Stable public boundary:** the smallest command and versioned JSON contract, including which outcomes remain experimental or unavailable.
3. **Durable-state ownership:** explicit existing-receipt coexistence or migration semantics and the recovery events that become compatibility obligations.
4. **Material Go benefit:** one demonstrated reliability problem or trusted additional consumer that the skill-owned helper cannot serve reliably.
5. **Platform scope:** choose Darwin-only stability, or accept a bounded Linux field run before making a portable claim.
6. **Privacy and coordination evidence:** curate the recovery evidence and confirm that coordination cost remains justified by utility.
7. **Separate owner decision:** explicitly select `promote`, `repeat`, or `stop/narrow` after the preceding gates are satisfied.

The broader promotion threshold in the [#247 design grill](issue-247-mutating-opus-workflow-design-grill.md) and the [post-lifecycle vision](../adr/0002-post-lifecycle-long-term-vision.md) remains authoritative for general or mutating Claude promotion. This narrower decision does not weaken it.

## Consequences

- Keep the Darwin read-only route **Proven experimental** in the [provider executor readiness matrix](../provider-executor-readiness.md).
- Continue explicit authorization and live model, entitlement, capacity, executable, worktree, and process preflight for every run.
- Keep the helper schema and command surface without a compatibility guarantee.
- Prefer naturally occurring current-contract recovery evidence over another happy-path smoke.
- Add no Go command, receipt migration, provider call, or new delegation run from this decision.
