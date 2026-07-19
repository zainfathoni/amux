---
status: experimental-proposal
---

# Amp internal invocation policy

## Decision boundary

This proposal records a personal, experimental policy for preventing unintended Amp model invocations and execution placement. It does not change [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md) worker or runner identity, amend [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md), alter the [Claude delegation pilot](https://github.com/zainfathoni/amux/issues/147), or make amux an Amp-wide control plane.

The policy is intentionally asymmetric:

- public amux artifacts may provide a generic deterministic mechanism and progressively disclosed workflow;
- the maintainer's private `fleet` configuration owns account-specific reserve floors, executor allowlists, machine overlays, and installation;
- Amp remains the owner of threads, inference, executors, subscriptions, and billing;
- enforcement claims stop at boundaries Amp exposes through permissions, plugins, or explicit `/amux` actions.

This is an Amp-internal policy only. Amp-to-Claude communication, Claude capacity, and heterogeneous backend routing remain in their separate experiments.

## Problem and evidence

Two recurring actions consumed Amp credits contrary to the maintainer's intent:

1. a workflow used Read Thread while asking Oracle to review, even though the relevant context could have been supplied directly; and
2. a skill-driven worker spawn selected `low` mode instead of the intended `medium` default.

Amp's published model mapping on July 18, 2026 assigned GLM-5.2 to both `low` mode and Read Thread, while Oracle used GPT-5.6 Sol. Those assignments are diagnostic context, not stable policy keys. Amp may change them independently of amux.

Later use exposed a related executor mistake: an agent-created strategy thread defaulted to an orb when the maintainer wanted a local interactive tmux client. Capacity available in another execution pool did not make the two environments interchangeable.

The maintainer is cost-sensitive but does not want a cheapest-model router. Multiple capacity pools may exist, and private policy may require deliberate use of each without publishing account holdings or balances. The policy therefore protects spending authority and execution intent rather than automatically choosing another provider.

## Goals

- Keep automatic Amp worker and child-thread creation on `medium`.
- Prevent implicit Read Thread use and indirect transcript-loading workarounds.
- Require an explicit executor for native Amp child threads.
- Observe and, where typed identities later permit, bound automatic fan-out and model-triggering native Amp child conversation.
- Preserve a configurable reserve for controllable Amp model-backed invocations.
- Explain every decision, governing source, degraded capability, and enforcement boundary.
- Make private policy portable across machines through `fleet` without copying the generic mechanism.
- Gather bounded evidence before promoting any helper or schema into stable lifecycle CLI core.

## Non-goals

- Predicting Amp's internal model routing or billing pool.
- Controlling the already-running parent turn or unavoidable Amp system models.
- Automatically redirecting denied work to Claude, Codex, an orb, or another runner.
- Inspecting prompts with another model to decide whether an invocation is justified.
- Adding a resident policy daemon, supervisor, task graph, or inference router.
- Treating thread URLs as permission to read their contents.
- Replacing project safety, privacy, merge, or approval policy.
- Changing the [#147 Claude delegation pilot](https://github.com/zainfathoni/amux/issues/147) or using this work as evidence for its gates.

## Stable action vocabulary

This is the bounded intended action vocabulary. An action becomes mechanically enforceable only for a client/version, tool, executor when applicable, and enforcement mechanism whose trusted inputs and pre-side-effect interception have passed the matrix probes below.

### Create an Amp worker or child thread

- `medium` is the only automatically selectable mode.
- `low`, `high`, `ultra`, deprecated aliases, and plugin modes require exact action-specific user approval.
- On a proven adapter, an unauthorized mode is blocked, not silently rewritten to `medium`; elsewhere this remains observed or instruction-only.
- The intended policy never relies on an Amp executor default. On a proven adapter, native `local`, `orb`, or an exact live Amp `runner(id)` target must be declared and approved for that action.
- Native `local` remains a background child in the current Amp client; it is not an ADR-0001 amux worker.
- A request for a local interactive worker remains a separate `/amux spawn --mode medium` workflow; a runner or orb is not substituted.
- Capacity in another pool does not relax executor approval.

Amp `runner(id)` is an Amp-native executor identifier. It is not [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md)'s canonical-workdir amux runner identity.

On a proven adapter, an unqualified request such as “start another agent” asks for an executor decision before creation. Different Amp surfaces currently document different executor defaults, so every claimed adapter must prove and expose the effective value rather than inheriting it silently.

### Read another Amp thread

- Task-context Read Thread requires exact approval naming one thread, except for the bounded shipped discrepancy-recovery path below.
- A thread URL is provenance, not authorization.
- Approval for Oracle, review, a work group, or a related task does not imply approval for task-context thread access.
- Raw `amp threads export`, transcript copying, or another retrieval path must not be used automatically to evade the same policy intent.
- If required context exists only in another thread, the agent asks for exact approval or asks the maintainer to restate the relevant decision.

Operational lifecycle recovery may inspect exact machine-readable thread state only under the separately authorized amux operation's shipped recovery contract: a concrete discrepancy must exist, deterministic evidence must be exhausted, candidate relationship must be established from durable/local evidence, and inspection must remain bounded to identity and exact delivery verification before failing closed. This is not task-context Read Thread authority and does not permit semantic transcript mining.

The currently shipped [`/amux` coordination workflow](../../skills/amux/reference/workflows.md) and [troubleshooting contract](../../skills/amux/reference/troubleshooting.md) also permit one narrow read of one exact related thread only after a concrete local/GitHub discrepancy is named and deterministic evidence cannot resolve it. This is a compatibility exception to task-context approval: authorization for the enclosing `/amux` lifecycle or coordination operation covers only that bounded discrepancy query against the exact related thread whose relationship to the named discrepancy is established by durable, local, or GitHub evidence. A promoted Read Thread gate must preserve this fail-closed recovery path without widening it into general task-context access or silently denying an already-authorized lifecycle operation.

### Invoke specialist subagents

- Oracle may be used at agent discretion, including repeated use when genuinely useful.
- Oracle receives intent, constraints, relevant files or diff, and known evidence upfront. Oracle and its caller may not perform task-context thread reads without separate exact approval; the bounded shipped discrepancy-recovery path remains the only compatibility exception.
- Search, Review, and Librarian follow a deterministic-tool-first escalation rule: direct file reads, `rg`, git, and focused primary documentation are preferred when sufficient; model-backed synthesis is used for chained behavioral discovery, review, or external-code understanding.
- The helper does not attempt to judge context quality or whether a deterministic lookup was sufficient. These remain progressively disclosed skill rules.
- Read Thread remains separately gated even when another specialist recommends it.

### Create generic subagents and fan out

- The intended personal baseline is one persistent/native child or generic Task implementation subagent at a time when its other policy checks pass.
- Additional or parallel children should require one exact batch authorization naming count, purpose, modes, and executors.
- Descendant scope and slot release remain observed and instruction-only until Amp exposes stable parent, child, turn-state, and replay identities at the interception boundary.
- Specialist Oracle, Search, Review, and Librarian calls remain in their own policy class rather than consuming the generic child slot.
- The first experiment therefore claims only to bound generic child creation at demonstrated surfaces, not all specialist inference concurrency or total Amp spending.

### Send native Amp agent-to-agent child messages

- The intended baseline allows a concise child report to its declared origin and one coordinator-to-child follow-up within the declared task route.
- Assignment, follow-up, report, retry, and route semantics are instruction-only until native messaging exposes stable typed action and message identities; prose is never parsed to manufacture those types.
- Further turns should require an explicit extension. Exhaustion leaves the child active and inspectable; it does not authorize a replacement child.
- Arbitrary peer messages, unrelated work requests, peer broadcast, and status polling require exact approval or use existing durable/local inspection instead.
- This action excludes `amux report`, callback and work-group lifecycle traffic, Claude receipts and notification/report/input routes, ordinary human-authored messages, and exact lifecycle-recovery communication.

## Enforcement classification

Policy intent is broader than the first mechanically enforceable surface. Every adapter must publish this matrix with concrete observed tool names and schemas before promotion:

| Action | Candidate surface | Required visible inputs | Initial classification | Known bypass or limit |
| --- | --- | --- | --- | --- |
| `/amux spawn` mode | Explicit `/amux` preflight and eventual shell call | Requested mode and normalized spawn identity | Deterministic only when the workflow invokes the resolver | Direct CLI or shell invocation can bypass skill preflight unless separately permissioned. |
| Native child creation | Amp agent-to-agent creation tool or plugin API | Mode, explicit `local`/`orb`/Amp `runner(id)`, parent, request identity | Observe, then ask-at-call after a pre-side-effect interception probe | Direct plugin APIs may not create nested permission calls; schemas and defaults vary by surface. |
| Read Thread | Amp Read Thread tool | Exact target thread ID | Observe, then ask-at-call after proving interception before retrieval | Automatic thread-mention extraction, direct plugin message APIs, shell/API export, and user-installed tools require separate coverage. |
| Oracle/Search/Review/Librarian | Each model-backed specialist tool | Tool identity and bounded request metadata | Capacity observed until each tool is proven permissioned | Internal specialist fan-out may not cross the client permission boundary. Semantic suitability remains instruction-only. |
| Generic Task | Generic subagent tool | Parent, mode or agent class, request identity | Observe; child counting requires stable identity | Descendants and independently running threads may not be visible or attributable after restart. |
| Native child message | Native message tool | Source, target, stable action/message ID, parent route | Instruction-only until typed identities are proven | Plugin `append`, retries, and semantic “report” prose cannot be counted safely. |
| Capacity observation | Capacity provider and version | Provider identity, schema version, pool and window identity, observed amount, freshness, confidence, charge-route evidence, and reservation/decision-lock state | Observe; each source contract is promoted independently | Provider schema drift, unknown charge routing, and observations without reservations prevent automatic allow. |

The four classifications are:

- **deterministic**: the helper sees sufficient trusted fields before side effects;
- **ask-at-call**: Amp's native permission UI confirms that exact intercepted call;
- **observed**: diagnostics record what the surface exposes but do not block;
- **instruction-only**: the skill states intent without claiming mechanical enforcement.

The resolver's advisory decision and the adapter's enforcement result are separate. In observed mode, intended `ask` and `reject` decisions are recorded as `would_ask` and `would_reject` while the adapter allows the call. Only a deterministic gate or an ask-at-call surface promoted after its probes may map those intended decisions to binding permission results.

Implementation must record known bypasses rather than claiming universal Amp coverage.

## Approval semantics

Approval is action-specific:

- `spawn(mode=low)` does not authorize another low-mode spawn;
- `read-thread(T-123)` does not authorize another thread;
- an executor approval does not propagate to descendants;
- a batch allowance binds exact count, purpose, modes, executors, and descendant scope;
- an identical idempotent replay of the same spawn remains the same action.

An unambiguous user instruction is semantically sufficient for the agent to understand intent; no magic phrase is required. It is not, by itself, a deterministic helper grant: the permissions helper does not receive trusted user-message authorship or provenance. Amp's native permission UI is the candidate initial hard action-specific approval path. It becomes trusted only after a probe proves that Amp, rather than model-controlled arguments, owns the decision; displays and binds every normalized security-relevant field; confirms before side effects; scopes the grant to one call; and has safe replay behavior.

Until a trusted UI command or plugin issues an opaque, one-use grant bound to the normalized action, `/amux` may carry an action-bound attestation only as instruction-level evidence. Semantic user instructions and batch allowances likewise remain instruction-level evidence rather than hard grants. Model-controlled arguments cannot promote an attestation into a trusted allow because they may be invented or replayed. Persistent evidence stores only bounded keyed digests, never the full user message or raw identifier.

Project safety or privacy rules cannot be relaxed by a personal spending approval.

## Capacity policy

### Initial observable policy

- CodexBar is one candidate machine-readable source for Amp capacity.
- Capacity must be refreshed when older than five minutes.
- The public mechanism ships no account-specific reserve floor; private policy supplies any configured value.
- All observable controllable model-backed invocations are evaluated against the floor: created children, generic Task, Oracle, Search, Review, Librarian, and an otherwise approved Read Thread.
- Direct shell, file, git, and other non-model tools in the current parent remain available.
- The current parent turn and Amp system models such as compaction and titling are outside the enforceable boundary.
- During observation, a below-floor, missing, stale-after-refresh, or low-confidence decision records `would_ask` while allowing the call rather than claiming a safe automatic allow. After each tool surface, data contract, and approval path is proven, that intended decision may become a binding call-time `ask`, through which one exact invocation may proceed while acknowledging the reserve impact.
- Missing or uncertain data never implies full capacity.

When a potentially eligible subscription balance or the pre-invocation charge route is unavailable through a documented machine-readable source, another pool being above its private floor is insufficient for automatic allow. The capacity decision is `would_ask` during observation and may become a binding call-time `ask` only on a proven surface.

### Future multiple-pool policy

When trustworthy additional capacity becomes observable, each potentially charged pool retains its own provider, amount, window, confidence, and reserve. Pools with different providers, windows, units, balances, or runtime-only quotas remain separate and are never combined into one synthetic percentage. Non-inference runtime capacity never authorizes model inference.

If Amp does not expose the charge route before invocation, every pool it may debit must be known and above its own reserve; otherwise the decision is `ask`, not `allow`. A five-minute observation does not reserve capacity, so parallel decisions remain advisory until the helper serializes a conservative bounded reservation or refresh under one decision lock. Diagnostics must not claim which pool Amp will charge without authoritative routing. Undocumented web endpoints, settings scraping, private cache parsing, and model estimates are prohibited capacity sources.

This policy never selects `low` merely because it is advertised as cheaper, and never redirects a denial to another provider.

## Policy sources and resolution

Sources compose by typed operators rather than generic precedence:

1. public amux defaults provide generic safe behavior;
2. repository declarations may add portable project constraints;
3. private `fleet` policy adds personal floors, executor allowlists, account capabilities, and machine-specific tightening.

No source is discovered by scanning neighboring files. Owner-local evidence may retain a source digest under the private bounds below, while caller-visible diagnostics expose only a redacted source class and reason code. Resolution uses these operators:

- safety, privacy, and approval denials combine by union, with deny winning;
- mode and executor candidates combine by intersection;
- reserve floors use the strictest applicable value within the same pool and window;
- effective capabilities intersect declared policy with machine-proven support;
- operational context is additive and grants no authority;
- only declared workflow preferences may use typed replacement.

A private source may add context or tighten a rule but cannot weaken a repository safety or privacy constraint. Irreconcilable constraints block the action with the conflicting source classes reported, without exposing their private values or locations.

If an expected private policy is missing or unreadable, degraded restrictions are intersected with every readable source. They may leave an otherwise permitted explicit `medium` local interactive amux spawn available, and leave Oracle-with-supplied-context eligible for an explicit call-time capacity decision rather than automatically allowed without a configured floor. They deny non-medium creation, native child execution, and thread access. Degraded mode never grants an action denied elsewhere. Caller-visible diagnostics identify the missing source class, not its private path.

Machines share a personal baseline. A machine overlay may tighten behavior when a capability is absent, but does not silently weaken the baseline. Capability diagnostics distinguish deterministic enforcement from instruction-only guidance.

## Enforcement architecture

The first implementation is a pure skill-owned experimental helper. It accepts a proposed normalized action and resolved policy inputs, then returns `allow`, `ask`, or `reject` with machine-readable reasons. It performs no spawn, thread read, message send, or provider routing itself.

Amp's existing `amp.permissions` delegation is the preferred first enforcement boundary. A thin adapter reads tool arguments as JSON from stdin and the tool name from `AGENT_TOOL_NAME`, then invokes the normalized resolver. On a promoted deterministic or ask-at-call gate it maps exit `0`, `1`, or `2` to allow, ask, or reject. In observed mode it records `would_ask` or `would_reject` and exits allow. Raw delegated arguments are never logged. Implementation must verify the current protocol, schemas, approval provenance, and built-in tool coverage rather than assume every server-provided or plugin action is intercepted.

An Amp plugin is a fallback or later strengthening mechanism only if experiments prove that required built-in tools bypass permissions or require trusted active-thread context unavailable to the helper. Duplicate permission and plugin gates are not installed from day one.

The `/amux` skill treats a non-allow enforcement result from a promoted gate as binding. Advisory `would_ask` and `would_reject` observations do not block. The skill remains responsible for semantic workflow rules that a pure helper cannot judge, including context sufficiency and deterministic-tool-first escalation.

There is no resident process. The helper runs only at a permission or explicit preflight boundary.

## Progressive skill structure

The experiment should add to the existing progressive skill structure while keeping the top-level skill as a router:

```text
skills/amux/
├── SKILL.md
├── reference/
│   ├── … existing workflow references …
│   └── amp-invocation-policy.md
└── scripts/
    └── resolve-amp-invocation-policy
```

`SKILL.md` contains only the non-negotiable branch:

- load the invocation-policy reference before creating native Amp children, reading threads, or sending native Amp agent-to-agent child work;
- run the deterministic resolver for hard-gated actions;
- never bypass `ask` or `reject` outcomes.

The reference owns the action table, approval scope, capacity behavior, degraded mode, source composition, diagnostics, exclusions, and recovery. The script owns mechanical parsing and resolution. Existing #118 work-group/report/callback and #147 Claude routes continue loading their own references without acquiring a second message budget. Tests verify the trigger/pointer relationship, exclusions, action outcomes, and source provenance rather than duplicating policy prose across files. The existing spawn route calls the resolver only after its mode boundary is demonstrated, and then keeps one normative mode rule.

Private `fleet` provisions policy values and Amp permission wiring to the public helper. It does not fork or reimplement the helper.

### Diagnostics boundary

Caller-visible diagnostics are public-safe and bounded. They may contain the normalized action class, advisory or enforcement result, stable public reason codes, redacted policy-source classes, freshness/confidence classes, and supported/unsupported capability names. They must not emit private paths, configured floors, account identities or capabilities, balances, machine-overlay values, raw target identifiers, or stable correlators that could join events across calls.

The ephemeral, human-owned native permission prompt is not caller-visible diagnostics. It may display the exact normalized target and other security-relevant fields needed for one-call authorization, but it must not log, persist, or export those raw identifiers.

Richer evidence, where needed for owner diagnosis, remains machine-local under restrictive permissions and the retention limits below. It may use keyed digests for bounded correlation but is never copied into public diagnostics, issue comments, PRs, or reports. A curated identifying reference still requires the explicit owner selection and privacy/access review defined below.

## Decision evidence and privacy

The experiment keeps a private machine-local decision history for at most 30 days, 10,000 records, or 5 MiB, whichever limit is reached first:

- normalized action category and keyed digests of targets and provenance where correlation is required;
- allow, ask, or reject result and reason codes;
- policy source identities or digests;
- capacity source, freshness, confidence, and bounded values;
- approval type, scope, and bounded provenance;
- calling workflow and enforcement capability.

Prompts, raw delegated arguments, raw thread/message/workdir identifiers, user-message text, transcripts, thread contents, secrets, model output, and tool output are excluded. Decision events expire at the bounds even when an error remains unresolved; one aggregate current error state may remain without event history. Private parent directories and files use restrictive permissions, all mutation and cleanup share one serialized lock domain, and cleanup is deterministic. Any curated identifying thread or artifact reference requires explicit owner selection plus privacy and access review.

## Rollout

Rollout is staged:

1. Preserve the existing skill-level `medium` default while inventorying each concrete tool and API surface.
2. Observe mode, executor, Read Thread, specialist, generic child, capacity, route-budget, and descendant decisions until their helper-visible schemas and pre-side-effect interception points are proven.
3. Canary `ask` or `reject` on the reporting macOS machine only for fields demonstrated before side effects in that exact client/version, action/tool, executor when applicable, enforcement mechanism, and capacity `provider/version/schema/pool/window/freshness/confidence/charge-route/reservation` tuple.
4. Promote mode, executor, Read Thread, fan-out, messaging, and each capacity contract independently after deterministic tests and several owner-confirmed real decisions. Untested tuples remain observed or instruction-only; parity is never inferred across clients, versions, actions, executors, mechanisms, or capacity contracts.

Development and deterministic helper tests may run on any platform. Linux without trustworthy capacity data reports that capability as unavailable rather than simulating macOS parity.

## Evaluation gates

The canary records whether:

- each claimed `client/version × action/tool × executor when applicable × enforcement mechanism` tuple produces a pre-side-effect interception with the documented tool name and argument schema;
- an unauthorized `low` or other non-medium creation is first observed and then, only on a proven surface, blocked before thread creation;
- omitted and explicit `local`, `orb`, and Amp `runner(id)` executor behavior is observed per surface, with policy refusing to rely on defaults;
- a candidate permission UI proves trusted provenance, exact displayed and bound fields, one-call scope, replay behavior, and pre-side-effect ordering before an exact approved mode and executor proceed once without widening approval;
- a thread URL alone does not authorize Read Thread, and automatic mention extraction is classified separately from an explicit Read Thread tool call;
- exact call-time Read Thread approval binds only its target on a surface proven to intercept before retrieval;
- ordinary task-context access requires exact one-target approval, while an authorized `/amux` lifecycle or coordination operation permits exactly one narrow discrepancy query against the evidenced related thread and then blocks rather than widening or chaining reads;
- Oracle remains useful with supplied context and no thread read;
- child, descendant, and native-message schemas expose enough stable typed identity to support accounting, or those rules remain instruction-only without parsing prose;
- missing or stale capacity data produces an observed `would_ask` decision without blocking direct parent tools;
- every promoted capacity decision binds an evidenced provider/version/schema/pool/window/freshness/confidence/charge-route/reservation contract, with schema drift or an unknown required field failing safely;
- unknown charge routing never produces automatic `allow` merely because one pool is above reserve;
- permission and skill behavior agree without duplicate prompts;
- diagnostics expose redacted source classes and enforcement gaps without leaking private provenance;
- bounded history contains no prohibited content;
- the policy prevents accidental spending without adding more correction turns than it saves.

Promotion requires a real interception test for every claimed tuple and field; repeated real decisions across the evidenced action classes; no unreported bypass at claimed enforcement boundaries; and owner confirmation that false blocks and approval friction are acceptable. Each action tuple and capacity contract receives its own promote, revise, narrow, keep observed/instruction-only, or remove outcome.

## Experiment issue graph

The experiment is tracked in this native issue graph:

1. [#174](https://github.com/zainfathoni/amux/issues/174) is the Amp internal invocation-policy experiment umbrella;
2. [#175](https://github.com/zainfathoni/amux/issues/175) is its implementation child. It begins with contract probes and has an explicit stop-or-rescope gate: failed probes prohibit enforcement work for that tuple. Only supported tuples proceed to the pure resolver, thin permissions adapter, progressive skill integration, capability diagnostics, bounded ledger where typed identity supports it, and tests;
3. [#176](https://github.com/zainfathoni/amux/issues/176) is its macOS canary evaluation child. It records promote, revise, narrow, keep observed/instruction-only, or remove for each tested tuple.

GitHub records [#176](https://github.com/zainfathoni/amux/issues/176) as blocked by [#175](https://github.com/zainfathoni/amux/issues/175). This graph remains independent of the [#147 Claude delegation pilot](https://github.com/zainfathoni/amux/issues/147) and does not authorize changes to [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md), [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md), lifecycle CLI core, or Claude delegation.

## Relationship to ADR 0002

This experiment is compatible with [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md)'s personal-first direction, private `fleet` layering, deterministic skill-owned resolvers, capability diagnostics, no-resident-supervisor rule, bounded evidence, explicit routing, and instruction-consistency horizon.

It also identifies a distinct concern: inference spending and executor authority apply before a worker or delegation exists. The experiment should therefore remain a separate cross-cutting proposal rather than being folded into the Claude capacity policy or named-recipe horizon.

No result from this experiment silently changes ADR 0002. Promotion requires a later explicit decision identifying which generic mechanism, if any, belongs in stable amux.
