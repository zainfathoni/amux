---
status: superseded
date: 2026-07-29
supersedes: 0002
superseded-by: 0005
---

# Freeze orchestration expansion and retire permanent Leads

> **Historical decision:** ADR 0005 reversed this ADR's blanket retirement direction while retaining its constraints against permanent Lead hierarchies, broad orchestration, and token-heavy coordination. [ADR 0007](0007-retire-amux-through-native-cutover-and-staged-drain.md) now supersedes ADR 0005 with a staged drain that preserves existing recovery evidence without adding a retirement engine. This document remains available as diagnosis and decision history.

## Decision

amux is entering maintenance and gradual retirement for its orchestration architecture. New framework expansion is frozen. Work is admitted only when it is:

1. a critical correctness, safety, security, or compatibility fix;
2. cleanup or simplification that does not broaden authority or behavior;
3. documentation, measurement, or migration work needed to retire a surface safely; or
4. an owner-approved task-scoped experiment that uses existing boundaries and records the metrics below.

New durable task graphs, provider-neutral routing, standing role identities, coordinator tenure machinery, generalized attention/control planes, schedulers, supervisor loops, and new orchestration state are not planned. A proposed exception requires a separate owner decision supported by measured evidence; unused provider capacity or implementation availability is not sufficient.

The permanent **Lead** pattern is retired as a target architecture. In particular, amux will not establish one standing per-machine or cross-project coordinator thread, rotate that thread on a tenure schedule, or transfer an indefinite portfolio of work between Lead generations. Planning, delegation, verification, and acceptance should instead belong to the thread handling the bounded task. A complex task may grant a thread a **task-scoped coordinator role**, but that role begins and ends with the task or explicitly bounded workflow. An originating thread is not implicitly a coordinator, and a role is not a persona or durable identity.

This is an architecture and product-direction decision, not a runtime change. The implemented worker, runner, work-group, `coordinator` field, report, callback, deadline, and lifecycle contracts remain available exactly as documented today. Existing durable state is not migrated, rewritten, or deleted by this ADR. “Retired” describes the permanent Lead pattern and future expansion direction; “deprecated” will apply to a command, field, or behavior only after a later compatibility decision and release plan.

ADR 0001 and ADR 0003 continue to describe implemented lifecycle and native-adoption behavior. This ADR supersedes ADR 0002's proposed post-lifecycle expansion horizons. Useful constraints and experimental evidence from ADR 0002 remain historical input, but its generalized attention, finish-pipeline, and named-recipe horizons are no longer roadmap items.

## Why

Recent project history has already moved toward a smaller boundary:

- [#264](https://github.com/zainfathoni/amux/issues/264) split provider experiments out of core `/amux`, made spawn packets task-only, and reduced always-loaded coordination instructions.
- [#207](https://github.com/zainfathoni/amux/issues/207) was closed as not planned because native Amp threads, messages, and files already provide transport; a shared provider-selection workflow would recreate the control plane it intended to avoid.
- [#223](https://github.com/zainfathoni/amux/issues/223) rejected presentation machinery for promoted permanent coordinators in favor of task-scoped roles.
- [#228](https://github.com/zainfathoni/amux/issues/228) preserved the lesson that coordination needs bounds while rejecting scheduler/control-system enforcement in core.
- [#267](https://github.com/zainfathoni/amux/issues/267) demonstrated the cost and ambiguity of trying to establish week-lived Leads before any tenure or handoff value could be measured.

The remaining useful product is narrower: deterministic local lifecycle and cleanup, plus optional bounded task execution where it proves cheaper and good enough. Native Amp capabilities should own Amp-thread creation and transport. Provider-specific tools may remain explicit, narrow executors; amux should not grow a framework around them without evidence that native task-scoped work cannot meet the need.

## Evidence retained from Claude and Pi pilots

The retirement decision does not reset provider learning or require new pilots merely to reproduce known results.

1. **Bounded delegation can be useful, but task fit and coordination cost decide its value.** The [#149 read-only Claude run](../proposals/issue-149-read-only-claude-pilot-result.md) produced useful independent analysis with zero follow-ups or human escalations during the run and completed its receipt-to-park loop in 221.863 seconds. The [#151 mutating run](../proposals/issue-151-mutating-claude-pilot-2-evaluation.md) produced a valid clean handoff, but coordination cost was material for a small documentation task and the decision was `stop/narrow`. Later [#309 evidence](../proposals/issue-309-read-only-claude-cli-promotion-gate.md) establishes that another ordinary Darwin happy-path run adds little by itself. Future evaluation should therefore measure marginal value, not restart the pilot sequence.
2. **Thin, provider-specific, one-attempt seams are preferable to a shared orchestration framework.** The Claude Orb work in [#205](https://github.com/zainfathoni/amux/issues/205) found concrete exact-model and bounded-output requirements while keeping native Amp report-back. The Pi path in [#230](https://github.com/zainfathoni/amux/issues/230) kept one bounded attempt and independent Amp acceptance; its owner-authorized run correctly rejected a newline-changing result instead of manufacturing success. These results support self-contained packets, explicit authority, no blind retry, and main-thread verification. They do not support a permanent coordinator, provider-neutral registry, or generalized receipt/control plane.

## Required scorecard for any future delegation approach

The unit of evaluation is one bounded task attempt, from the main thread's decision to delegate (or begin direct work) until it accepts, rejects, or stops the result. Record task class and scope so unlike work is not compared as if equivalent. Preserve only aggregates and public-safe references; do not retain prompts, transcripts, credentials, raw tool streams, or private account data.

| Metric | Definition |
| --- | --- |
| Main-thread token cost | Main-thread input plus output tokens attributable to preparing, coordinating, checking, and integrating the task. Record provider-reported values when available. If unavailable, record `unavailable`; packet/report bytes may be recorded separately as a proxy but must never be labeled tokens. Child/executor tokens are reported separately. |
| Human turns | Owner messages or approval/choice actions after the initial task request and before terminal acceptance/rejection. Record coordinator-to-worker turns separately; do not combine them with human turns. |
| Wall time | Monotonic elapsed time from task start to accepted, rejected, or stopped outcome. Also record time to first usable result when known. Parallel work does not erase elapsed time. |
| Result quality | Independent main-thread assessment on a five-point rubric: `0` unusable or harmful; `1` major rework; `2` partially useful; `3` accepted with minor correction/integration; `4` accepted as supplied after verification. Tests, review, and artifact validity are evidence; report delivery or model agreement is not quality. |
| Retry/failure rate | Per approach, count all admitted attempts. A retry is another execution needed for the same bounded assignment. A failure is an admission, execution, delivery, identity, handoff, or validation failure that prevents a usable result. Report retries and failures separately, including recovered failures; no blind retry is permitted. |

Historical observations seed the scorecard where these fields exist. Unknown values remain unknown. Future evidence collection starts from [#314](https://github.com/zainfathoni/amux/issues/314), not from a new provider pilot.

Before an approach may become recommended or gain stable framework surface, evaluate at least ten delegated tasks and ten comparable direct tasks across at least three real task classes. A smaller cohort may justify stopping early, never promotion. The delegated cohort must:

- keep median result quality at or above the direct baseline and have at least 80% of results score `3` or `4`;
- reduce median main-thread token cost or median wall time by at least 20%, without worsening the other by more than 10%;
- not increase median human turns, and require no more than one human turn at the 90th percentile;
- keep failed attempts at or below 10% and retries at or below 10%; and
- contain no unresolved safety, authority, data-loss, or wrong-target event.

These are promotion thresholds, not quotas to manufacture work. Missing trustworthy token data blocks a token-savings claim but still permits a wall-time claim. The owner may stop or narrow an approach at any time; changing thresholds or accepting a material trade-off requires an explicit recorded owner decision.

## Gradual migration and retirement plan

### Phase 0 — direction freeze (this ADR)

- Set public expectations in the README and mark ADR 0002 superseded.
- Close permanent-Lead expansion as not planned.
- Make no runtime, schema, command, skill-routing, or behavior change.
- Continue critical fixes and safety-preserving cleanup.

### Phase 1 — inventory and evidence

- [#313](https://github.com/zainfathoni/amux/issues/313) inventories orchestration surfaces and classifies open work under the freeze.
- [#314](https://github.com/zainfathoni/amux/issues/314) carries existing Claude/Pi evidence into the common scorecard and defines privacy-safe future records.
- Record actual users, durable data, safety value, and compatibility constraints before proposing deprecation.

### Phase 2 — task-scoped operating default

- Update instructions in a separately reviewed documentation/skill change so new work uses the current task's thread and bounded roles rather than a standing Lead.
- Keep work groups and coordinator metadata available where existing workflows need correlation, reports, callbacks, or safe finish authorization.
- Do not migrate active groups automatically. Do not reinterpret old state as evidence of a permanent role.

This phase changes operating guidance and therefore is not implemented by this ADR alone.

### Phase 3 — evidence-gated deprecation design

- [#315](https://github.com/zainfathoni/amux/issues/315) defines retain/deprecate/remove dispositions, read compatibility, warnings, migration/export, rollback, and release gates.
- Prefer deleting optional orchestration guidance and adapters before changing durable lifecycle schemas.
- Preserve deterministic lifecycle, diagnostics, and cleanup surfaces when their value is independent of delegation.

No command, field, or file format becomes deprecated until this phase produces a separate accepted decision.

### Phase 4 — implementation and removal, if approved

- Implement one small compatibility step per reviewable change, with targeted tests and release notes.
- Keep old durable state readable through the declared compatibility window; never silently convert, retry, delete, or infer authority.
- Require a separate owner-approved ADR or equivalent decision before schema removal or a breaking command change.
- Stop after any phase if retirement costs or safety risks exceed the demonstrated benefit. Retention in maintenance mode is an acceptable final state.

## Consequences

amux remains usable as implemented while its product direction becomes intentionally smaller. Existing users receive no surprise behavior change from a documentation decision. New feature ideas bear the burden of measured proof, and task-scoped native work is the default comparison rather than a permanent orchestration hierarchy.

The cost is that some current surfaces may remain in maintenance mode for a long compatibility window, and some useful workflow automation will stay instruction-level or provider-specific. This is preferable to adding more durable framework state before its coordination value is demonstrated.

## Owner decisions reserved

The following require explicit owner input after the inventory and scorecard, not during this documentation-only phase:

- which current orchestration surfaces are retained indefinitely for lifecycle safety;
- whether any task-scoped role should keep the user-facing name “Lead,” or use only ordinary functional role names;
- whether the default promotion thresholds above should be tightened for mutating or credential-bearing work; and
- the first release, if any, that may emit deprecation warnings.
