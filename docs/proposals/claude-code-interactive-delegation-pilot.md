---
status: experimental-proposal
---

# Interactive Claude Code delegation pilot

## Decision boundary

This proposal extracts the smallest evidence-producing experiment from [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md). It is not an accepted lifecycle redesign and does not add a Claude Code worker, runner, daemon, remote-control service, or stable public API.

The agent-first lifecycle in [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md) and the delivered contracts from [#105](https://github.com/zainfathoni/amux/issues/105) and [#118](https://github.com/zainfathoni/amux/issues/118) are fixed inputs. Implementing this pilot requires its own isolated worktree and explicit approval. It does not reopen those issues or enter their dependency graph.

## Goal

From an originating Amp thread in the Web UI, use `/amux` to delegate one real task to one full interactive, subscription-backed Claude Code client in tmux and reliably recover a concise result. Capture enough bounded evidence to improve later APIs without first designing a third lifecycle resource or pretending Amp Web implies a live local delivery pane.

The pilot answers five questions:

1. Which real tasks benefit from otherwise-idle Claude subscription capacity?
2. Which identity is sufficient to verify, address, park, and recover an interactive Claude client?
3. Which parts of #118's durable report and callback design generalize beyond Amp work groups?
4. How much coordinator interaction and context does a useful delegation consume?
5. Does lifecycle friction justify a first-class Claude client registry later?

## Why #118 is a foundation, not the pilot API

The delivered implementation proves several invariants worth retaining:

- persist durable state before sending a wake-up;
- use stable IDs and reject conflicting retries;
- keep notification, acknowledgement, and finish authorization distinct;
- verify exact tmux, process, workdir, and coordinator identity before notification;
- leave failed notification pending and recoverable;
- avoid polling unrelated threads or adding a resident supervisor.

Its public commands deliberately encode a narrower workflow. `amux report submit` requires a canonical Amp group-member thread, accepts `ready | blocked | merged`, and couples `ready` to pull-request and CI expectations. `amux callback` requires an Amp work-group coordinator and verifies an interactive Amp process. Claude has no canonical Amp thread ID and its delegation outcomes are not finish authorization.

The pilot therefore uses a sibling experimental receipt adapter. It must not create fake Amp IDs, synthetic groups, or misleading #118 reports. Its evidence may later justify extracting a producer-neutral delivery envelope, but that is an outcome rather than a prerequisite.

## Scope

### Included

- macOS-first, same-machine operation;
- explicit invocation through `/amux` from Amp Web;
- one delegation and one interactive Claude client at a time;
- a new local Claude session for the first two checkpoints;
- explicitly deferred managed/adopted teleport experiments after the local checkpoints;
- CodexBar capacity capture;
- receipt-scoped identity and launch metadata;
- durable structured reports and input requests;
- optional verified best-effort notification when a live local route exists;
- manual response interaction unless a future vendor capability proves exact safe-input state;
- manual recovery from the local receipt store;
- acknowledgement-gated explicit parking;
- one real read-only pilot followed by one isolated mutating pilot;
- bounded private evidence retained for 30 days after acknowledgement and parking.

### Excluded

- headless or print-mode Claude execution;
- autonomous fan-out or quota filling;
- cross-machine Claude selection;
- first-class Claude lifecycle configuration or CLI commands;
- Amp worker or runner identity changes;
- Claude membership in an Amp work group;
- a universal task/report status vocabulary;
- automatic merge, release, or worktree cleanup;
- a resident watcher, scheduler, polling supervisor, or separate UI;
- mutation of Claude Web sessions after teleport;
- compatibility guarantees for experimental storage or helper commands.

## Experimental components

Implementation, when separately approved, should add only skill-owned experimental components alongside `/amux`:

1. **Delegation orchestration** resolves an explicit task, origin Amp thread, routing target, workdir policy, capacity observation, and handoff contract.
2. **Receipt helper** serializes and crash-durably stores correlation-keyed events and bounded evidence before attempting notification.
3. **Claude adapter and hooks** pass receipt identity into the interactive client, validate explicit semantic reports, and distinguish supported session initialization or waiting signals from unsupported automatic-input safety.
4. **Inspection and acknowledgement workflow** lets `/amux` recover pending events, acknowledge a report, and explicitly park the exact client.

These components live behind an obvious experimental namespace or directory. They do not modify lifecycle CLI core merely to simplify the pilot.

## Receipt model

The schema is intentionally unstable, but every record needs enough information to diagnose identity, delivery, and usefulness.

### Immutable binding

- correlation ID;
- acquisition mode: `local` or `teleport`;
- originating Amp thread as durable provenance when Amp initiated the delegation;
- independently replaceable delivery route: verified local Amp pane, machine-local inbox, or manual `/amux` recovery;
- optional #118 work-group reference when one already exists;
- Claude session or cloud teleport ID when available;
- canonical workdir;
- declared read-only or mutating ownership;
- declared handoff shape;
- task type and bounded task reference;
- initial capacity source and confidence.

### Verified local identity

- tmux session, window ID/name, and pane ID;
- pane creation identity and current process identity;
- launch-command digest;
- detected adapter capabilities;
- acquisition state and timestamp;
- Claude Code version, hook versions, and supported/unavailable capability states.

### Events

- session acquired, agent waiting, or acquisition failed;
- `input_request` and correlated response;
- semantic completion report;
- validation failure;
- notification attempt and result;
- acknowledgement;
- park result;
- recovery action.

Each event has a stable identity. Exact replay produces a duplicate outcome; conflicting reuse is rejected. Events are append-only within the pilot receipt, transitions are validated, and acknowledgement or park cannot precede the report state they reference. A materialized state may be derived for inspection, but it must preserve source status and provenance.

### Durability and concurrency

Claude hooks and `/amux` may write concurrently, so atomic rename alone is insufficient. Every mutation and cleanup operation uses one declared experimental lock domain. Contention authorizes no mutation. Immutable receipt bindings and event identities are checked under that lock; exact retries are idempotent and conflicting retries fail closed.

Private parent directories and files use restrictive permissions. A successful commit means file contents were flushed before atomic replacement and the containing directory was flushed afterward. Notification occurs only after that durable commit. The exact path, lock primitive, and helper implementation language remain experimental.

### Report payload

- outcome;
- concise summary;
- blockers or questions;
- changed artifacts;
- verification;
- handoff commit or reference where applicable;
- drill-down references.

Prompts, transcripts, pane captures, tool streams, secrets, and complete artifact contents are excluded.

## Local-session flow

1. The Amp coordinator explicitly requests Claude delegation through `/amux`.
2. The skill captures CodexBar capacity and explains whether the request can proceed.
3. For read-only work it verifies shared-workdir safety. For mutating work it requires a dedicated worktree and clean-commit handoff.
4. The helper creates the receipt before tmux launch.
5. `/amux` launches full interactive `claude "<initial task>"` in a dedicated tmux window with correlation and adapter context.
6. A correlated supported session-start signal records session acquisition and local session ID. A waiting signal is factual agent state, not proof that the composer is safe for injection; an unverified prompt leaves acquisition pending.
7. Claude may submit a durable `input_request`. The coordinator's answer consumes the bounded follow-up budget but is handled through manual interaction by default. Automatic tmux response delivery remains capability-gated and unavailable unless a stronger vendor contract proves exact safe-input state.
8. Claude explicitly submits its semantic report. A hook validates required fields and objective handoff invariants but never invents meaning from a stop event.
9. The helper persists the report before optionally attempting a short wake-up to a separately verified live local Amp route. An origin Web thread may have no such pane; unavailable or failed notification leaves the report in the local inbox for `/amux` recovery and is not described as Web-thread delivery.
10. The coordinator consumes and acknowledges the report. Claude remains active until acknowledgement.
11. `/amux` re-verifies receipt-scoped identity and explicitly parks the local client while preserving the Claude session ID.

## Teleport flow

Teleport is a deferred alternate acquisition path after both local checkpoints, not remote Claude lifecycle ownership or a prerequisite for the minimum loop.

### Managed teleport

1. `/amux` receives `session_...`, the expected repository, and an explicit routing target.
2. Before teleport, the skill takes exclusive ownership of the repository, dedicated worktree path, checked-out branch slot, Claude session, and tmux pane. It blocks if the cloud branch is already owned by another worktree or another actor may edit the target. It never offers or accepts an automatic stash.
3. It launches `claude --teleport <session-id>` in a dedicated tmux window.
4. Authentication, repository confirmation, missing pushed branch, and other vendor prompts leave acquisition pending for explicit input.
5. Immediately after supported session acquisition and before managed local continuation, `/amux` captures the branch, HEAD, worktree status, remote tracking, and known review reference as the acquisition baseline.
6. The coordinator declares the local continuation's handoff. Reports distinguish pre-teleport artifacts from local continuation changes.

### Explicit adoption

An already teleported pane may be adopted only after verification of:

- exact tmux identity and exclusive ownership;
- interactive Claude process and session provenance;
- expected repository and canonical workdir;
- clean acquisition boundary;
- exclusive worktree and checked-out branch ownership;
- explicit Amp coordinator or deliberate local-inbox routing.

Discovery never implies adoption. Finding a pane running `claude` is insufficient.

Parking or removing the local client does not archive, delete, or synchronize the Claude Web session. The cloud ID remains provenance only.

## Capacity policy

The first read-only pilot records and displays CodexBar's five-hour, weekly, and applicable model-specific windows and confidence, then requires explicit acknowledgement. It may proceed before reserve floors are tuned.

Before the mutating pilot, machine configuration must contain provisional hard reserve floors for each available window. The tightest window governs and is recorded. Missing or low-confidence data blocks autonomous selection; an explicit user request may proceed only after acknowledging unknown reserve impact.

The pilot records capacity before and after each delegation but does not attempt automatic quota filling.

## Pilot sequence and gates

### Pilot 1: real bounded read-only task

Use naturally occurring review, research, diagnosis, or design work. Sharing the origin workdir is allowed only when mutation is prohibited and verifiable.

Advancement requires:

- capacity captured and acknowledged;
- client identity remained verifiable;
- an exact receipt-event replay produced a duplicate outcome and a conflicting reuse was rejected before mutation;
- semantic report was durably stored;
- notification or manual recovery reached the origin Amp thread;
- result was useful enough to consume;
- acknowledgement-gated parking succeeded.

Optional paths that did not occur are marked untested rather than synthetically forced.

### Pilot 2: real isolated mutating task

Use a dedicated worktree with exclusive ownership, configured reserve floors, and a declared clean-commit handoff. Completion means a valid report, objective invariant validation, and delivered handoff—not correctness or merge readiness.

### Optional Pilot 3: teleport or adoption

Only after both local checkpoints, exercise managed teleport or explicit adoption on real work. A mutating adopted session qualifies only when `/amux` can establish a clean acquisition baseline and exclusive ownership of the complete repository, worktree, branch, session, and pane tuple. Otherwise it remains active but unmanaged or read-only and is recorded as untested for mutation.

## Evidence and retention

Each pilot records:

- task and handoff type;
- why Claude was selected;
- capacity source, confidence, before/after values, and governing window;
- Claude Code version, hook versions, authentication or billing source without secrets, and confidence that subscription capacity was used;
- initial delegation, follow-up, and report sizes or token counts when supported, without retaining content;
- coordination-turn count and human escalations;
- actual delivery route and delivery, acknowledgement, recovery, and parking outcomes;
- duplicate replay and conflicting-reuse outcomes for deterministic receipt operations;
- elapsed time;
- whether the result was useful;
- whether Amp accepted, revised, or discarded it;
- which readiness, waiting, input, delivery, and lifecycle capabilities were supported, unavailable, or untested.

Unresolved or recoverable receipts do not expire. Acknowledged and parked raw receipts become cleanup-eligible after 30 days. Only curated aggregates and representative artifact or thread references belong in a later proposal.

## Safety and privacy

- Fail closed on ambiguous pane, process, workdir, routing, or write ownership.
- Do not automatically send text to a Claude pane unless a supported vendor capability proves exact target and safe-input semantics; current hooks do not, so the pilot defaults to manual interaction after the initial task.
- Never infer semantic completion from idle, pane output, process exit, or a stop hook.
- Never stash, discard, force-delete, merge, or clean worktrees as an implicit delegation side effect.
- Keep raw receipt files private and exclude secrets, prompts, transcripts, and tool streams.
- Treat successful tmux input as notification only, never acknowledgement.
- Leave the Claude client active whenever delivery, validation, or routing remains unresolved.

## Promotion and stopping rules

The pilot may inform a future proposal for a producer-neutral delivery envelope, a persistent Claude client resource, capacity-aware routing, or automatic parking. None is implied.

Stop or narrow the experiment if:

- interactive subscription use is not reliably preserved;
- safe input readiness cannot be established without output scraping;
- Claude results do not repay coordination and context cost;
- workdir ownership remains ambiguous;
- private data cannot be bounded and protected;
- the helper starts duplicating #118 without exposing a genuinely different producer contract.

Promotion requires repeated useful read-only and mutating evidence, at least one real recovery path, bounded coordination cost, and a separate accepted decision identifying the smallest stable API justified by that evidence.

## Open implementation questions

- Which supported Claude session-start hook fields are stable enough to correlate acquisition?
- Can a stable local Claude session ID be captured without scraping terminal output?
- Will a future Claude capability prove that tmux input is safe? Current supported hooks do not.
- Should the helper be a small standalone binary or a script with atomic-file primitives?
- Which private local directory and lock primitive should implement the fixed experimental serialization domain?
- What provisional reserve floors should govern the first mutating pilot?
- Which naturally occurring task should be Pilot 1?
