---
status: design-grill-complete-awaiting-owner-review
issue: 247
base: 629ce55e5c5b89f02675e9d1866c6a9abb2da0ad
promotion: not-authorized
---

# Issue 247 mutating Claude Opus workflow design grill

This document resolves the design questions for [issue #247](https://github.com/zainfathoni/amux/issues/247) without implementing or promoting the mutating workflow. It preserves [issue #151's `stop/narrow` decision](issue-151-mutating-claude-pilot-2-evaluation.md). A separate evidence-backed owner decision is still required before that outcome can change.

## Conclusion

The complete goal in #247 is not currently implementable from a supported public contract. A useful narrower stage is designable, but not authorized for implementation by this grill:

1. **Stage A — exact-Opus local Darwin mutating workflow with acknowledged unknown capacity.** Extend exact `claude-opus-4-8` binding across the existing isolated Darwin mutating lifecycle while retaining the dedicated worktree, exclusive writer, frozen handoff, durable delivery, separate acknowledgement, explicit parking, and fresh unknown-capacity acknowledgement. This stage must not claim autonomous capacity admission or promotion.
2. **Stage B — trusted autonomous admission.** Remain externally blocked until an authoritative source proves the capacity pool and charge route, and the provider supplies a conservative way to express maximum launch impact in the same units as every governing capacity window.
3. **Promotion — separate decision.** Remain blocked until the full evidence threshold in #247 exists and an owner accepts a distinct `promote`, `repeat`, or `stop/narrow` decision.

Implementation completion, an exact model argv, a fresh utilization percentage, or one successful handoff cannot collapse these stages.

## Scope and non-claims

This grill covers:

- all eight open questions in #247;
- exact Opus binding;
- capacity and charge-route authority;
- bounded launch impact in provider units;
- single-pool admission and unknown-capacity behavior;
- same-origin pre-semantic retry;
- skill, helper, and CLI boundaries; and
- evidence required to supersede #151.

It does not:

- implement any workflow or helper change;
- authorize a mutating Claude launch;
- design or authorize Linux, cross-platform, or cross-machine mutating execution;
- make Claude an amux worker, runner, or work-group member;
- authorize provider fallback, automatic retry, automatic parking, integration, push, PR mutation, merge, release, cleanup, or teardown;
- establish trusted capacity from the owner-authorized read-only consultation used for this grill; or
- count that consultation as promotion evidence without a separate curated evidence decision.

## Verified starting point

### Repository contracts

At the recorded base:

- Exact `claude-opus-4-8` selection is supported only for the local **read-only** thinker. The mutating binding and launch request reject model selection.
- The mutating path already separates an exclusive writer handoff from Amp-owned preparation, validation, delivery, acknowledgement, parking, integration, and cleanup.
- A capacity decision can protect configured floors against **current** reported utilization, or require a digest-bound owner acknowledgement when capacity is unknown.
- The current decision does not subtract a conservative admitted impact from remaining capacity.
- Mutating writer exclusion is worktree-scoped, not capacity-pool-scoped.
- The diagnostic correctly classifies the recognized CodexBar payload as unsupported rather than treating it as autonomous capacity evidence.

These facts are visible in the [delegation helper](../../skills/amux/experimental/claude-delegation/claude_delegation.py), the [shared delegation contract](../../skills/amux/reference/claude-delegation-contract.md), and the [mutating workflow reference](../../skills/amux/reference/claude-mutating-delegation.md).

### Public provider evidence

Anthropic's public Claude Code guidance says that sign-in route determines whether an Enterprise seat consumes its included rolling pool or an API key uses pay-as-you-go billing. Its Pro and Max guidance separately says that Claude Code and other Claude surfaces consume the same plan limits, with optional usage credits billed at standard API rates after included limits are reached.

Those distinctions are authoritative but insufficient for autonomous admission. The public guidance does not define a machine-readable contract that binds a separately launched local process to one stable subscription capacity-pool identity, proves whether usage credits or another billing route can be selected, or converts a bounded agent run into a conservative percentage of its five-hour, weekly, and model-specific limits.

Anthropic's documented Usage and Cost Admin API is also not that contract. It reports historical organization API usage and cost in tokens and currency, requires an Admin API key, and is distinct from a local subscription's rolling usage windows. Its documented API-key and workspace dimensions can attribute API usage after the fact, but they do not prove the subscription charge route of a separate Claude Code launch.

CodexBar's current public Claude integration observes usage through OAuth, web, CLI, or Admin API strategies. Its public JSON identifies the provider and a source label, but it has no explicit usage-output schema version, no provider-defined stable capacity-pool identity, and no attestation that another local Claude process will consume the observed pool. Its configuration file's `version` and the separate `claude-swap` schema version are not versions of the public usage snapshot contract.

Therefore:

```text
fresh utilization observation
        + exact model argv
        + known authentication categories
        != proven capacity pool and charge route
```

## Resolved design branches

### 1. Require exact Opus throughout mutating execution

The mutating workflow must require the literal model identifier:

```text
claude-opus-4-8
```

Omission, aliases, normalization, a default model, provider substitution, model fallback, and automatic retry all fail before launch mutation. Exact binding must cover:

1. explicit request validation;
2. the mutating launch policy;
3. exact Claude argv;
4. packet, policy, request, and launch-command digests where applicable;
5. immutable receipt binding;
6. durable launch intent and startup verification;
7. session acquisition and live process identity;
8. report and frozen-handoff provenance;
9. recovery and same-origin supersession checks; and
10. acknowledgement-gated identity revalidation before parking.

CLI support for `--model` proves syntax only. Authentication, entitlement, availability, capacity, charge route, and actual provider acceptance remain separate evidence classes. A pre-semantic provider prompt must preserve the acquired/no-report evidence and enter only a supported recovery path; the workflow must not press a choice or substitute a model.

### 2. Treat capacity and charge-route proof as one admission prerequisite

A trusted capacity observation must bind all of the following without exposing account or credential material:

- exact provider;
- source-contract version and schema discriminator;
- retrieval time, authoritative data-as-of watermark, maximum accepted age, bounded reporting lag or consistency guarantee, and reset semantics;
- all applicable five-hour, weekly, and model-specific windows;
- a stable, provider-defined non-secret capacity-pool identity;
- the exact charge route intended for the launch;
- whether API billing, cloud billing, extra usage, fallback credentials, another account, or another pool could be selected; and
- a provenance/confidence class accepted by amux policy.

A display email, organization label, credential digest, token-account index, fetch-strategy label, or quota-window ID is not a capacity-pool identity. A source adapter may transport provider evidence, but it cannot invent missing provider guarantees.

The launch path must independently bind the intended charge route to the exact launch environment and process identity. Observing a pool through one credential while launching through an unproved credential or endpoint is a route mismatch and must fail closed.

### 3. Protect floors after maximum admitted impact

For every required capacity window, autonomous admission requires:

```text
remaining capacity - maximum admitted impact >= configured reserve floor
```

All three quantities must use the same authoritative provider unit and denominator. A percentage observation is insufficient if the provider does not define the denominator or conservatively convert a bounded run into percentage-point impact.

The following are useful operational bounds but are not capacity reservations by themselves:

- wall-clock timeout;
- report or packet size;
- context-window size;
- prompt-declared token budget;
- maximum output tokens;
- nominal monetary budget; and
- task or file-count bounds.

They become admission evidence only if the governing provider contract establishes a conservative conversion into every applicable capacity-window unit and prevents the process from exceeding it.

### 4. Prohibit autonomous admission when impact is unbounded

Fresh utilization answers “how much is currently reported as remaining?” It does not answer “how much will remain after this delegation?” Without a conservative admitted-impact bound, autonomous mutating admission remains prohibited.

This does not turn all mutating use into a known floor violation. It routes the exact request to the separate unknown-capacity fallback. Known floor violations and missing configured floors remain non-overridable.

### 5. Start with one active admission per proven capacity pool

The first autonomous version should allow at most one active mutating delegation per proven pool. That active-operation lock prevents concurrent launches but is not sufficient by itself: admission must also retain outstanding consumed-capacity debits until provider evidence incorporates them. It must serialize this sequence under one pool-scoped admission boundary:

```text
observe
  → validate provenance, route, freshness, and windows
  → reserve maximum admitted impact
  → revalidate observation and launch identity
  → persist durable launch intent
```

The active-operation lock ends only at a defined terminal or recovery state; a process launch failure cannot silently release an indeterminate lock or debit. The maximum admitted-impact debit survives operation termination until an authoritative later observation's data-as-of watermark and consistency guarantee prove that it includes the delegation's consumption, or until the governing window resets. Parallel or sequential decisions that would reuse unincorporated headroom must be rejected.

Until a pool identity is proven, no pool-scoped autonomous admission exists. Stage A may conservatively permit only one machine-local mutating delegation at a time after an exact owner acknowledgement, but this is containment for unknown impact—not evidence that the machine represents one provider capacity pool.

Do not add speculative multi-launch arithmetic, cross-machine reservations, automatic quota filling, or a general scheduler in the first version.

### 6. Keep a fresh, decision-bound unknown-capacity fallback

Unknown, stale, partial, unsupported, route-ambiguous, pool-ambiguous, or unbounded-impact evidence requires one fresh owner acknowledgement bound to:

- the exact provider and model;
- workflow and task identity;
- the full capacity observation and provenance class;
- configured floors;
- the intended charge route and its uncertainty;
- the capacity decision digest; and
- a short expiry or one-time launch consumption rule.

The workflow should generate the exact privacy-safe acknowledgement action. The owner should not construct private JSON or copy a digest manually on the normal operator path.

Acknowledgement:

- authorizes only the exact request;
- remains `autonomous_selection:false`;
- creates no quota evidence;
- cannot be cached across models, providers, pools, charge routes, workflows, floors, observations, or tasks;
- cannot override a known floor violation;
- cannot replace a missing required floor; and
- is distinct from report acknowledgement and integration acceptance.

### 7. Exclude same-origin retry from the first stage

An exact acquired process that encounters authentication, entitlement, credit, quota, or model availability failure before a semantic report cannot be relaunched by weakening terminal retirement.

If same-origin retry is later required, it depends on the non-terminal supersession contract in [issue #236](https://github.com/zainfathoni/amux/issues/236):

- preserve the old receipt and process evidence;
- stop only the exact acquired incarnation;
- mark only that delegation superseded without manufacturing semantic completion;
- authorize exactly one linked successor from the same immutable origin;
- rerun every model, worktree, policy, capacity, route, and process preflight; and
- consume the successor authorization atomically.

Until that dependency is accepted and implemented, the delegation remains blocked unless it can reach an already supported terminal or recovery state. A separately authorized fresh delegation is permitted only after the prior writer lease and machine/pool admission boundary have been durably released; otherwise there is no replacement or relaunch route. The workflow must not auto-retry, reuse the old process, inject login input, or select another model.

### 8. Keep orchestration in the skill and deterministic enforcement in the helper

The smallest reusable surface remains progressively disclosed under `/amux`.

| Concern | Owner |
| --- | --- |
| Explicit trigger recognition and task suitability | `/amux` skill workflow |
| Owner decisions, acknowledgement presentation, and escalation | `/amux` skill workflow |
| Worktree/branch preparation and exclusive-writer handoff | `/amux` skill workflow |
| Packet authoring and approved-source selection | `/amux` skill workflow |
| Independent review, integration, PR, merge, release, and cleanup decisions | Amp coordinator through the skill workflow |
| Exact field validation, canonical digests, receipt transitions, and replay rejection | Experimental helper |
| Capacity provenance validation, floor arithmetic, pool reservation, and admission locking | Experimental helper |
| Exact process/model/workdir identity and parking/recovery checks | Experimental helper |
| Provider observation adapter | Helper-side adapter only after its source contract is supported |

No new top-level `amux claude` resource or provider-neutral executor abstraction is justified. A stable CLI surface should be considered only after repeated real use demonstrates that the skill-owned helper cannot provide a reliable operator experience or that another trusted consumer needs the same contract.

## Answers to the eight open questions

### Q1. Can Anthropic provide an authoritative machine-readable capacity and charge-route contract?

**Not from current public evidence.** Anthropic documents metering categories and separate organization usage APIs, but no public contract found here proves local subscription-window utilization, stable pool identity, launch charge route, and maximum admitted impact together. Treat this as an external dependency until a specific documented contract is reviewed.

### Q2. Can CodexBar expose sufficient evidence instead?

**Not today.** CodexBar can observe useful session, weekly, and model-specific windows, but its public usage JSON is not an explicitly versioned source contract and does not prove capacity-pool or launch charge-route identity. It could become an adapter if its upstream provider evidence and its own output contract gain the required guarantees; it cannot manufacture them independently.

### Q3. Can maximum mutating-run impact be bounded in the provider capacity unit?

**Not from current evidence.** The existing limits constrain coordination and artifacts, not consumption of the provider's rolling subscription windows. A provider-defined conversion or enforceable reservation is required.

### Q4. If impact cannot be bounded, should autonomous admission remain prohibited?

**Yes.** Route the exact request to the owner-acknowledged unknown-capacity path. Do not interpret freshness as post-launch floor safety.

### Q5. Is one active mutating delegation per pool sufficient initially?

**Yes, when combined with outstanding-debit accounting.** It is the smallest defensible autonomous concurrency policy, but one active process alone does not prevent sequential reuse of utilization data that has not incorporated the prior run. Unknown pool identity means no autonomous pool admission; the acknowledged fallback should be machine-locally single-active for containment.

### Q6. Should same-origin pre-semantic retry depend on #236?

**Yes, and it should remain excluded until then.** Terminal retirement must not acquire relaunch authority implicitly.

### Q7. Which responsibilities belong to the skill, helper, or CLI?

**Skill for orchestration and owner authority; helper for deterministic validation and lifecycle evidence; no new stable CLI yet.** The table above is the ownership boundary.

### Q8. What evidence can supersede #151?

Only a separate accepted promotion decision after all of the following exist:

1. at least two useful real read-only runs under applicable contracts;
2. at least two useful real mutating runs under the candidate contract;
3. at least one real delivery or lifecycle failure recovered through a supported candidate path;
4. at least one real exact-Opus mutating run admitted autonomously from trusted pool, route, window, and impact evidence;
5. synthetic failures covering capacity drift, route mismatch, pool mismatch, floor exhaustion, parallel admission, model mismatch, provider blockers, invalid handoffs, process replacement, interrupted delivery, and interrupted parking;
6. measured coordination cost that is bounded and justified by utility;
7. privacy review of the curated evidence; and
8. an explicit owner decision selecting `promote`, `repeat`, or `stop/narrow`.

The successful run in #151 proves one useful objective handoff and normal lifecycle. It also records material coordination cost, a post-run capacity-policy failure, and no real recovery path. Those findings remain active. This grill's read-only Opus consultation does not automatically count toward the threshold; evidence inclusion requires separate curation and acceptance.

## Promotion state

The current state remains:

```diagram
┌──────────────────────────────┐
│ #151 decision: stop/narrow   │
└──────────────┬───────────────┘
               │ preserved
               ▼
┌──────────────────────────────┐
│ Stage A design available     │
│ exact Opus + unknown ack     │
└──────────────┬───────────────┘
               │ does not imply
               ▼
┌──────────────────────────────┐
│ Stage B externally blocked   │
│ pool + route + impact proof  │
└──────────────┬───────────────┘
               │ evidence gate
               ▼
┌──────────────────────────────┐
│ Separate promotion decision  │
└──────────────────────────────┘
```

No stable skill wording should present mutating Opus as promoted while Stage B and the evidence gate remain unsatisfied. #229's shared persistent-host Claude arm therefore remains read-only.

## Read-only Opus consultation

The owner authorized exactly one fresh local `claude-opus-4-8` thinker despite the disclosed capacity diagnostic: CodexBar capacity was recognized but its schema/version remained unsupported, so capacity was unavailable. That acknowledgement authorized only this read-only consultation and supplied no quota evidence.

The thinker ran in a dedicated clean detached worktree at the recorded base with only Read/Grep/Glob and bounded semantic submission. Its immutable receipt bound the exact model, policy, packet, launch command, origin, repository, base, and workdir. The lifecycle completed through exact launch, acquisition, one valid report, inbox delivery, separate acknowledgement, and explicit identity-verified parking. No artifacts changed.

The thinker and Amp independently reached the same conclusions on all material branches. Amp then checked the claims against the repository contracts, issue precedents, Anthropic's public usage and billing guidance, its Usage and Cost Admin API documentation, and CodexBar's public Claude provider and JSON-output contracts. No disagreement required owner escalation.

This consultation validates the quality of the design review and the existing read-only lifecycle only. It is not mutating-run evidence, capacity proof, or promotion authority.

## ADR decision

No ADR is added. The staged design deliberately remains experimental and externally gated; no hard-to-reverse implementation or stable public interface has been accepted. If a provider capacity/charge-route contract becomes available and a stable admission interface is chosen, that later trade-off may justify an ADR.

## Public references

- [Issue #247](https://github.com/zainfathoni/amux/issues/247)
- [Read-only Pilot 1, issue #149](https://github.com/zainfathoni/amux/issues/149)
- [Mutating implementation, issue #150](https://github.com/zainfathoni/amux/issues/150)
- [Mutating Pilot 2, issue #151](https://github.com/zainfathoni/amux/issues/151)
- [Capacity hardening, issue #188](https://github.com/zainfathoni/amux/issues/188)
- [Exact read-only Opus, issue #211](https://github.com/zainfathoni/amux/issues/211)
- [Persistent-host integration, issue #229](https://github.com/zainfathoni/amux/issues/229)
- [Same-origin supersession, issue #236](https://github.com/zainfathoni/amux/issues/236)
- [CodexBar diagnostic, issue #240](https://github.com/zainfathoni/amux/issues/240)
- [Anthropic: models, usage, and limits in Claude Code](https://support.claude.com/en/articles/14552983-models-usage-and-limits-in-claude-code)
- [Anthropic: usage and length limits](https://support.claude.com/en/articles/11647753-how-do-usage-and-length-limits-work)
- [Anthropic: usage credits for paid plans](https://support.claude.com/en/articles/12429409-manage-usage-credits-for-paid-claude-plans)
- [Anthropic Usage and Cost API](https://platform.claude.com/docs/en/manage-claude/usage-cost-api)
- [CodexBar Claude provider](https://github.com/steipete/CodexBar/blob/main/docs/claude.md)
- [CodexBar CLI payload](https://github.com/steipete/CodexBar/blob/main/Sources/CodexBarCLI/CLIPayloads.swift)
