---
status: proposed
base: ee922eb535a38992de5e2cfd5d3a9b063e976590
final_review: accept-with-narrow-changes-reconciled
---

# Issue 147 Claude delegation design grill

This proposal records the reviewed design direction for issues #147–#151. It is not an implementation specification or pilot result. The implementation and evaluation issues remain the authority for their respective gates.

## Verified inputs and limits

Amp reviewed the live native issue graph, accepted ADR 0001, proposed ADR 0002, the interactive delegation proposal, the `/amux` skill and trigger reference, generalized evidence from prior Amp–Claude collaborations, the current Claude Code CLI, and official Claude Code settings and sandbox documentation.

The assessment began from the required base:

```text
ee922eb535a38992de5e2cfd5d3a9b063e976590
```

The native graph is a strict sequence:

```text
#147 umbrella
   |
   v
#148 implement read-only seam
   |
   v
#149 run/evaluate read-only pilot
   |
   v
#150 implement isolated mutating seam
   |
   v
#151 run/evaluate mutating pilot
```

#148 blocks #149, #149 blocks #150, and #150 blocks #151. Closing an implementation issue does not substitute for the following evaluation gate.

ADR 0001 is fixed: an amux worker remains an Amp TUI client identified by a canonical Amp thread. A Claude thinker or mutating delegate is a receipt-scoped role, not an ADR-0001 worker, runner, or work-group member.

## Stakeholder grill record

Amp independently assessed the sources before consulting a fresh read-only-by-agreement Claude Code 2.1.211 stakeholder. Three numbered questions completed:

1. audit the transferred facts against actual Claude behavior;
2. choose the smallest skill surface among the four candidate shapes;
3. define the minimum read-only thinker contract.

The fourth question was not sent. Claude Code displayed an unsent `Amp here, question 4.` value or suggestion after turn three, and one bounded `Ctrl-U` correction did not clear it. Because the TUI exposed no supported empty-composer proof, Amp failed closed rather than pasting over ambiguous state. The remaining design below is repository- and documentation-derived; it must not be attributed to the stakeholder.

Communication incidents were bounded:

- the Amp executor disconnected once before baseline inspection and no state changed;
- questions 1 and 2 each required one verified additional Enter after the first Enter left the exact message in the composer;
- question 1 encountered a temporary API retry and completed without duplicate input;
- question 4 was abandoned after the ambiguous composer and failed single correction;
- no new character corruption occurred, although the transferred dossier contains one earlier dropped-first-character observation.

The stakeholder session was suitable for consultation, not proof of the proposed thinker boundary. Its footer visibly showed `accept edits on`, one inherited MCP, one hook, and inherited `CLAUDE.md` context. Prompt acknowledgement established consent but did not enforce read-only authority.

## Owner grill record

The owner subsequently approved these decisions one at a time:

1. keep one experimental read-only route inside `/amux` through #149;
2. give every Pilot 1 thinker a fresh dedicated disposable worktree while retaining enforced read-only authority;
3. combine bootstrap, evidence, Amp assessment, and question in the supported initial launch input;
4. use verified personal-first configuration isolation rather than requiring a separate OS account or container;
5. exclude guarded tmux injection from #148's supported contract;
6. narrow #148 to the durable core receipt loop and defer optional notification, input-request, follow-up, automatic parking, and cleanup machinery;
7. publish only generalized aggregate evidence by default and require itemized approval for identifying references;
8. use an exclusive clean-commit ownership handoff for a future #150 mutating delegate;
9. use one private, unstable correlated envelope across typed experimental messages without merging role authority; and
10. use bounded fail-closed recovery that never expands authority.

Claude did not review decisions 3–10 in the original session after the interactive channel failed closed. Four later fresh stakeholders reviewed the envelope, thinker enforcement/report seam, mutating handoff, and recovery contract through process-launch input only. The owner reconciled and approved the resulting refinements. Stakeholder agreement and owner approval resolve product choices but do not convert untested behavior into pilot evidence.

## Fresh stakeholder re-grill

Four fresh Claude Code 2.1.211 processes each received one self-contained question at process launch. They ran in safe mode with no tools, strict empty MCP configuration, `dontAsk`, disabled slash commands, and no Chrome integration. Amp sent no input after launch. All four completed semantic answers; all four then displayed an unsent next-question suggestion despite `--prompt-suggestions false`, reinforcing that composer appearance is not a supported safe-input signal.

Three launcher attempts failed before creating a Claude process because shell/tmux parsing did not preserve a multiline argv value. No prompt was delivered in those attempts. A self-deleting Python launcher that passed an exact argv value succeeded for each stakeholder; no temporary launcher remained. This is environment-specific launch evidence, not a proposed protocol guarantee.

The reconciled refinements were:

- fold bootstrap acceptance into the semantic answer while preserving coordinator receipt acknowledgement as a separate durable event;
- add explicit `in_reply_to` causality, make timestamps informational, mark origin identity opaque to Claude, block base/workdir mismatch, and place Amp's assessment last for disagreement context while accepting that its presence anchors the answer;
- make no-Bash Read/Grep/Glob the default thinker surface and expose only one Amp-owned, schema-limited `submit_report` capability;
- treat `dontAsk` according to official documentation: unapproved tools auto-deny in interactive mode;
- define one mutating handoff commit as one commit beyond baseline at submission, permit amend/squash before submission, add a zero-commit blocked outcome, freeze writes at report submission, and state hook/cleanliness/attribution rules; and
- remove automated composer correction, treat resumed sessions as new incarnations, add usage-limit and capture-invalid states, and bound polling/escalation without automatic parking.

One stakeholder suggested launcher-side JSON/stdout capture instead of a report tool. That conflicts with the fixed full-interactive input and the accepted diagnostic-only status of pane output, so the design retains explicit semantic submission. Another claimed interactive `dontAsk` does not auto-deny; official Claude Code documentation explicitly says it does, so no denial hook is required for that behavior.

## Final independent review

A fifth fresh no-tool Claude stakeholder received the complete final design through process-launch input only. An independent Oracle review received upfront context and explicit file scope and was instructed not to read or rely on Amp thread history. Both returned **ACCEPT WITH NARROW CHANGES**, not `REOPEN`.

The reconciled corrections load the contract before preflight, make the complete thinker answer the `submit_report` payload, separate receipt creation from session acquisition and immutable origin from mutable routing, split acknowledgement from parking, define worktree-scoped read confidentiality, keep mutation-specific protocol types out of #148, record manual assistance, and make the pilot-proposal deltas exhaustive. The existing #149 issue gate remains the pre-registered pass rubric. Amp's assessment remains in the packet by owner decision, but ordering is readability—not an anchoring control—and anchoring is accepted evidence bias.

## Decision 1: keep the pilot inside `/amux`

| Option | Decision | Reason |
| --- | --- | --- |
| Experimental route inside `/amux` | **Choose for #148/#149** | Matches the issue contract, current single-skill packaging, Amp coordinator ownership, and ADR 0002's asymmetric seam. |
| One `/claude` umbrella with thinker/worker routes | Defer | Implies a stable Claude product surface and route parity before either pilot has passed. |
| Separate `claude-thinker` and `claude-worker` skills | Reject for now | Duplicates shared identity/report/recovery rules and presents an unproven mutating route as a peer. Literal names also do not follow the current gerund skill-naming convention without a justified exception. |
| `/cmux` orchestration skill | Reject | Obscures Amp's coordination ownership and invents a second orchestration product unsupported by the repository model. |

The minimum skill change is a clearly marked experimental read-only route in `skills/amux/SKILL.md`, one focused workflow pointer under `skills/amux/reference/` with contract and recovery details progressively disclosed behind it, one matching explicit-trigger row in `trigger-phrases.md`, and consistency tests. Helper mechanics remain experimental implementation invoked by that route; they should not expand the top-level skill body.

Activation must be explicit and task-specific. Suitable examples are:

- “Delegate this read-only analysis to Claude.”
- “Ask Claude for a second opinion on this design without making changes.”
- “Have Claude review this evidence read-only.”

Incidental mentions of Claude, general requests for review, available capacity, or task complexity must not activate delegation. No autonomous fan-out or quota filling is allowed. A future `/claude` or separate skill requires an explicit post-pilot promotion decision; it does not emerge automatically as evidence accumulates.

### Progressive disclosure for the `/amux` skill

The Claude route must follow the `writing-great-skills` information hierarchy rather than copying this design into `SKILL.md`. The shipped shape for #148 should be:

```text
skills/amux/
├── SKILL.md
└── reference/
    ├── trigger-phrases.md
    ├── claude-read-only-delegation.md
    ├── claude-delegation-contract.md
    └── claude-delegation-recovery.md
```

`SKILL.md` remains the router and primary step tier:

- Add one model-facing description branch for an explicit user request to delegate bounded read-only analysis to Claude. Do not list synonymous trigger phrases in frontmatter.
- Add one route under “Route triggers to the smallest side effect”: **Delegate read-only analysis to Claude** → load `reference/claude-read-only-delegation.md`.
- Keep only the branch gate inline: explicit user intent is required, and only the read-only route exists through #149. All branch-specific mechanics stay behind the pointer.

`reference/claude-read-only-delegation.md` is the ordered workflow. It loads `claude-delegation-contract.md` as a prerequisite before preflight so the workflow can validate against one normative source without copying its rules. Each step ends in a checkable completion criterion:

1. **Preflight:** validate exact base, capacity acknowledgement, fresh worktree, read roots, launch policy, settings/MCP isolation, and immutable receipt binding against the contract. Complete only when every required capability is supported and recorded; otherwise return the factual blocker without launching.
2. **Construct:** mint correlation fields and build the combined initial packet. Complete only when schema, authority, source identity, packet bounds, and exact packet digest validate.
3. **Launch:** pass the exact packet through the documented interactive process-launch interface, then append the observed session acquisition. Complete only when incarnation, workdir, version, and launch-policy digest match the receipt's expected values. Pane echo does not prove delivery or understanding; only a correlated report establishes semantic receipt.
4. **Receive:** accept the complete semantic answer only as the correlated `submit_report` payload through the machine-local inbox. Complete only when a valid report is durably stored and consumption materializes `delivered`; pane output remains diagnostic.
5. **Acknowledge:** independently assess and acknowledge the exact consumed report. Complete only when the durable receipt records coordinator acknowledgement; parking failure must not erase this state.
6. **Park:** reverify the exact process incarnation and explicitly park it. Complete only when the receipt records `verified_parked`; otherwise retain the acknowledged partial state and route to recovery.

`reference/claude-delegation-contract.md` is the single source of truth for definitions and flat #148 protocol rules: thinker authority, launch capability and read surface, envelope fields, launch-packet/report schemas, receipt transitions, input capability classes, privacy bounds, and diagnostic-versus-semantic evidence. The issue and proposal explain why the contract exists but must not duplicate its normative field lists after implementation.

`reference/claude-delegation-recovery.md` contains only failure branches and their positive next actions: fail-closed identity reacquisition, manual input handling, new-incarnation resume, usage-limit pause, capture invalidation, receipt replay, inbox recovery, bounded escalation, and explicit parking authority. The workflow loads it only when a preflight, launch, input, report, delivery, acknowledgement, or parking criterion fails.

Co-locate each concept's definition, rules, and caveats under one heading in its normative reference. In #148 use **thinker**, **receipt**, and **incarnation** consistently as leading terms rather than restating their definitions at every step. Reserve **freeze** and **handoff** for the future mutating branch. Tests should verify trigger/pointer consistency and exhaustive step outcomes, not reproduce protocol prose.

Do not add `claude-mutating-delegation.md` in #148. If #149 passes, #150 may add that separate sequence reference and point to the shared contract only for genuinely common envelope/receipt mechanics. Writer authority, submission freeze, commit handoff, abort, and Amp reacquisition remain co-located in the mutating branch so read-only and mutating authority never collapse through reuse.

## Decision 2: the thinker contract is launch-enforced, not promised

The **thinker** is a fresh, delegation-scoped interactive Claude process that may inspect supplied and authorized sources and return analysis. It owns no edits, commits, integration, GitHub mutation, tmux topology, assignment, parking, or cleanup.

Required preflight:

1. Before launch, mint an unguessable delegation nonce and create the immutable receipt with protocol version, task ID, opaque origin Amp thread, repository/base/canonical workdir, exact packet digest, expected launch-policy digest, and authority owner.
2. Create a fresh dedicated disposable worktree for every Pilot 1 thinker. Isolation limits blast radius and simplifies attribution but does not itself prove read-only authority.
3. Launch a fresh full interactive process with only Read/Grep/Glob and one explicit `submit_report` tool. Bare denies remove Bash, Edit/Write/NotebookEdit, web, agent, GitHub, and every other mutating capability. `dontAsk` auto-denies anything not pre-approved.
4. Restrict built-in reads to the dedicated worktree root with no additional directories; deny known sensitive repository paths and minimize credential/environment exposure. Restrict setting sources, disable skills and browser integration, and use strict MCP configuration containing only the Amp-owned report server. If path, setting-source, or strict-MCP isolation cannot be verified, do not launch.
5. Resolve directory trust and login before considering the client ready. After launch, append a `session_acquired` observation with Claude version, process start/incarnation, tmux diagnostic address, observed workdir, and effective capability attestation. Amp verifies configured and observable state while explicitly trusting Claude Code runtime enforcement rather than claiming OS isolation.
6. Snapshot repository/worktree state before and after the consultation and behaviorally test denied capabilities in #148. Configuration output is supporting attestation, not OS proof.

Claude Code's default Bash sandbox is not a read-only-repository boundary: it permits current-workdir writes and linked-worktree shared Git writes and does not govern built-in Edit/Write. Pilot 1 therefore exposes no Bash; Amp supplies Git-history and command-derived evidence in the initial packet. Sandboxed Bash is a possible later capability upgrade only after exact deny-write behavior is proven.

The `submit_report` tool is a single-purpose local MCP method. Its Amp-owned server validates delegation identity, nonce, `in_reply_to`, schema, and size; performs duplicate/conflict checks; and atomically writes only private receipt state outside the repository. Its bounded payload is the complete authoritative thinker answer: role/capability acceptance, `complete` or `blocked`, verdict and rationale, evidence, assumptions, unsupported claims, blockers, and verification actually performed. Claude receives no receipt path, shell, generic write, or other MCP method. Any matching prose rendered in the pane is diagnostic. Bootstrap acceptance in the report proves comprehension only; the authoritative capability statement comes from launch policy and Amp-side verification.

Read confinement is a separate claim from mutation denial. Pilot 1 allows built-in reads only under the dedicated worktree and explicitly denied sensitive paths, with no added directories. #148 must prove representative allowed and denied reads. The report server enforces size and privacy fields, but it cannot guarantee that a model never quotes sensitive repository content; Pilot 1 therefore uses an owner-approved task/worktree and requires privacy review before any result is published.

## Recommendation 3: use one correlated semantic envelope

The original stakeholder did not review this schema after its channel failed closed. A fresh stakeholder reviewed it, and the owner approved the reconciled refinements.

The protocol should have one small versioned envelope rather than six unrelated formats. Logical message kinds may share implementation fragments without sharing authority.

### Common envelope

| Field | Purpose |
| --- | --- |
| `protocol_version` | Reject unsupported schema changes. |
| `delegation_id` | Immutable receipt/correlation identity. |
| `nonce` | Detect stale, crossed, or corrupted session messages. |
| `message_id`, `in_reply_to`, and `kind` | Idempotency, explicit causality, and typed semantics. |
| `task_id` and optional `question_id` | Correlate one task and bounded follow-up. |
| `origin_thread` | Immutable, opaque, echo-only Amp origin; Claude must not resolve or inspect it. |
| `repository`, `base`, `workdir` | Bind evidence to exact source state. |
| `producer_role` | Fixed to `thinker` in #148 without pretending Claude is an Amp worker. |
| `capabilities`, `authority`, and `launch_policy_digest` | Declare allowed operations and owners while pointing to Amp-verified enforcement. |
| `created_at` | Informational timestamp only; never identity, causality, or ordering authority. |

Mutable `delivery_target`, attempted transport, and machine-local recovery state belong to Amp-owned receipt routing, not the immutable origin or Claude-facing packet. Exact routing changes preserve `origin_thread` and delegation identity.

### Message kinds

- **Bootstrap:** role, launch-policy digest and provenance, capability limitations, owner matrix, stop condition, answer/report requirements, follow-up budget, and privacy restrictions. It is a logical section of the launch packet, not a separate interactive turn.
- **Evidence/question:** self-contained facts and hypotheses, source references, precise decision request, scope, and expected response. Amp's independently formed assessment appears last with an instruction to form a verdict before using it for disagreement. Bootstrap plus evidence/question is one vendor-supported process-launch input.
- **Thinker report:** the complete semantic answer submitted through `submit_report`: exact nonce and `in_reply_to`, accepted role and exclusions, `complete` or `blocked`, verdict, evidence-grounded rationale, evidence, disagreement with Amp, assumptions, unsupported claims, capability denials or mismatches, blockers, safety/awkwardness, and verification actually performed. A base/workdir mismatch must return `blocked`. Helper validation establishes report validity, not correctness or acceptance. There is no separate delegate acknowledgement, external answer reference, or authoritative pane answer.

Pane text and captures are diagnostic. The helper-submitted thinker report is the durable event required by #148/#149. Machine-local inbox consumption materializes `delivered`; coordinator acknowledgement and identity-verified parking remain distinct later events. The single vocabulary is `valid_report → delivered → acknowledged → verified_parked`; notification is none of these. No new generic “completion receipt” should be claimed until the pilots prove it.

The #148 contract ships no mutating producer role, handoff kind, writer lease, freeze, commit schema, or abort schema. Those remain non-normative proposal guidance until #149 passes and #150 owns their sequence and protocol variants.

## Recommendation 4: classify input capabilities explicitly

A fresh stakeholder reviewed the classification after the original channel failed closed, and the owner approved the reconciled refinements.

| Class | Meaning | Pilot behavior |
| --- | --- | --- |
| Supported launch input | Full interactive Claude is started with the initial self-contained task through its documented CLI input | Allowed after identity and launch-policy preflight. |
| Supported semantic submission | Claude invokes only the Amp-owned, schema-limited `submit_report` MCP tool | Required for authoritative report validity once #148 implements it. |
| Manual interactive input | A human directly types/approves a follow-up | Default follow-up fallback; record `assisted=true`, bounded turn count, reason, and input class without retaining content. |
| Guarded tmux injection | Operator-authorized paste/capture observed outside the supported workflow | Experimental evidence only; no automated correction, no vendor-safe claim, and no #148 correctness dependency. |
| Automatic interactive input | Programmatic follow-up requiring proof of an empty, focused, atomically reserved composer | Unavailable until a supported capability proves those semantics. |

The transferred dropped character, repeated Enter failures, ambiguous auto-suggestions, and suggestions displayed despite `--prompt-suggestions false` demonstrate why exact echo is diagnostic but not atomic delivery. On occupied, ambiguous, corrupted, or apparently unsent input, the supported workflow sends nothing. Correction is manual only.

An assisted report still replies to the original question `message_id` and records that intervening manual context existed. #149 may consume it under its pre-registered gate but must report the assistance and cannot claim evidence for an unassisted path.

## Recommendation 5: keep mutating authority separate

A fresh stakeholder reviewed this mutating contract after the original channel failed closed. The owner approved the reconciled refinements for #150 if and only if #149 passes; no current evidence proves them.

No mutating route should appear in the shipped skill until #149 records **pass** and #150 implements the additional boundary. The **worker** label, if retained internally, means a Claude mutating delegate—not an ADR-0001 amux worker.

Recommended #150 ownership matrix:

| Concern | Claude mutating delegate | Amp coordinator |
| --- | --- | --- |
| Worktree/branch preparation and baseline | No | Owns |
| Edits in prepared worktree | Exclusive owner during delegation | Must not edit concurrently |
| Local commit | May leave exactly one commit beyond baseline at handoff; amend/squash is allowed before submission | Validates identity, parent, scope, and clean state after submission freeze |
| Tests/checks in worktree | Performs declared checks | Independently reruns risk-appropriate verification |
| Push, PR, review, merge, release | No authority | Sole authority, still subject to project/human policy |
| Integration/cherry-pick | No | Sole authority after handoff |
| Parking and cleanup | No | Explicit, acknowledgement-gated; never implied by report |

The worktree must be dedicated, clean at baseline, bound to the receipt, and under an exclusive logical writer lease. Shared writable workdirs fail closed. Claude independently verifies branch, baseline, and clean state before editing and has read-only Git introspection for truthful handoff reporting. The lease prevents Amp edits by orchestration policy and branch-checkout collision but is not OS-enforced against editors, watchers, or unrelated processes.

The only successful handoff shape is one clean local commit beyond baseline. Clean means no staged, unstaged, or non-ignored untracked changes. Normal repository commit hooks run unless project policy explicitly authorizes otherwise. Claude may remove artifacts it created but may not tear down worktree/branch resources. Report submission freezes Claude's write authority and commit identity; Amp may inspect read-only, independently verify, acknowledge, park, and only then reacquire write ownership. Authorship is explicit in the semantic handoff rather than inferred solely from Git metadata.

A zero-commit `blocked` report is valid failure reporting, not a handoff. Claude must not reset, discard, or force-clean to manufacture a clean abort; any remaining dirty state stays unresolved for Amp inspection. The launch uses a fresh process and treats thinker findings as attributed evidence, never inherited truth. A valid handoff proves objective shape only—not correctness, acceptance, merge readiness, or cleanup authorization.

## Recommendation 6: recovery is an explicit bounded state machine

A fresh stakeholder reviewed this table after the original channel failed closed, and the owner approved the reconciled refinements.

| Failure | Required behavior |
| --- | --- |
| Directory trust, login expiry, permission prompt, or unsupported capability | Block before task input or leave unresolved for manual action; never auto-approve or auto-login. |
| Pane/process/workdir mismatch or nonce mismatch | Send nothing; mark the current process incarnation unavailable and require explicit reacquisition. |
| Occupied or ambiguous composer | Send nothing; manual fallback. |
| Corrupted, partially submitted, or apparently unsent input | Send nothing automatically. Any diagnosis or correction is manual; never repaste merely because capture looks incomplete. |
| Claude interrupted after useful work | A bounded verdict request is allowed only through direct manual input when context survived; otherwise retain evidence and report unresolved. Composer-state rules take precedence. |
| Process restart, PID reuse, or `--resume`/`--continue` | Old incarnation is invalid. Rebootstrap a new process and nonce; resumed history is context reacquisition, not continuity proof. |
| tmux server restart | Pane identity is lost. Recover only from durable receipt plus supported session context reacquisition; otherwise manual unresolved recovery. |
| Usage/rate-limit pause | Record unresolved `usage_limited`; send nothing and resume only through a later manual decision. |
| Unknown overlay, compaction/update notice, or model/TUI change | Mark capture diagnostic-invalid, send nothing, and reverify through supported state. |
| Missing or invalid semantic report | Never infer completion from output, idle, stop hook, or exit. Keep unresolved. |
| Notification failure | Keep the durably stored report in the machine-local inbox; wake-up remains best effort. |
| Delayed Amp assignment delivery | Reverify the exact provisioned thread and idempotency identity; do not create a replacement thread merely because local verification timed out. This remains an Amp-delivery lesson, not a proven Claude receipt transition. |
| Delivery without acknowledgement | Bound polling, then record unresolved `attention_required` and escalate. Do not auto-park; parking remains an explicit coordinator/owner decision. |
| Crash during receipt update | Replay the exact event idempotently under the experimental lock; conflicting reuse fails before mutation. |

Recovery never expands authority. A restarted thinker remains read-only; a mutating delegate may reacquire writer ownership only after Amp verifies the original writer is inactive and the worktree baseline/handoff state is unambiguous.

## Evidence maturity and issue allocation

| Finding | Destination |
| --- | --- |
| Role separation, self-contained packets, independent Amp assessment, pane diagnostics, transport failures, and current composer ambiguity | Curated pre-pilot evidence in the dossier and proposal rationale; mention in #149 only as prior calibration, not gate evidence. |
| Launch-policy enforcement, capability provenance, nonce/incarnation identity, helper schemas, locking, idempotency, durable-before-notify, inbox recovery, acknowledgement/parking ordering, and diagnostic capability output | #148 implementation and focused tests. |
| One natural read-only task using the merged seam, including capacity, exact identity, duplicate/conflict behavior, durable report, recovery/delivery, acknowledgement, parking, utility, and untested paths | #149 evidence and explicit pass/repeat/stop decision. Nothing in this grill satisfies that gate. |
| Dedicated worktree, exclusive writer lease, clean-commit handoff, capacity floors, baseline and handoff validation, and separate edit/integration authority | #150 implementation only after #149 passes. |
| One natural mutating task, Claude and independent Amp verification, at least one real recovery path across repeated evidence, and promotion/stop/repeat decision | #151. Nothing currently proves this contract. |

Guarded input, safe composer state, receipt lifecycle, capacity policy, semantic report delivery, acknowledgement/parking, login/process/tmux recovery, and all mutation remain unproven until their assigned implementation and evaluation stages.

## Proposed document and issue deltas

These are recommendations for owner review, not edits performed by this grill.

### #147

- Preserve the strict native sequence and explicit `/amux` boundary.
- Clarify that the first skill surface is one explicit read-only route and cannot activate from incidental Claude mentions or capacity alone.
- State that any new skill or mutating route requires its own gate/promotion decision.

### #148

- Add the launch-enforced thinker contract, settings/tool/MCP/hook/credential provenance, nonce plus process-incarnation identity, and explicit capability degradation.
- Keep `SKILL.md` to one explicit trigger, one branch gate, and one context pointer. Put the ordered workflow, shared contract, and failure branches in progressively disclosed references with checkable completion criteria and one normative home per rule.
- Default to Read/Grep/Glob with no Bash, Edit/Write, web, agent, or GitHub tools. Supply Git-history and command-derived evidence in the initial packet rather than tuning the default writable Bash sandbox.
- Restrict setting sources and skills, use `dontAsk`, and fail closed unless strict MCP configuration exposes only the Amp-owned, schema-limited `submit_report` method.
- Treat configuration output as attestation and add behavioral negative tests for denied capabilities and report-server schema/target isolation.
- Treat documented initial process input as the only automatable task input. Keep follow-ups manual unless a supported safe-input capability is detected.
- Define `in_reply_to` causality in the correlated envelope, fold bootstrap acceptance into the semantic answer, and preserve coordinator receipt acknowledgement, delivery, and parking as separate transitions.
- Narrow implementation to the durable core: immutable receipt binding, serialized crash-durable writes, duplicate/conflict handling, explicit semantic report validation, machine-local inbox consumption, acknowledgement, and identity-verified explicit parking.
- Defer automatic notification or wake-up, typed input-request lifecycle, automatic follow-up and parking, cleanup execution, generalized receipt APIs, and teleport/adoption.
- Keep the implementation unstable and alongside `/amux`; expose one focused workflow pointer and disclose contract/recovery references only from that branch.

### #149

- Retain the existing issue gate as the pre-registered pass/repeat/stop rubric and identify the owner/coordinator as its evidence-based decision maker. Add effective launch-policy provenance and before/after mutation detection to required evidence.
- Record the safe-input class actually used and every manual escalation.
- Record `assisted`, bounded follow-up count, and reason without content; an assisted run may satisfy the existing gate but proves no unassisted follow-up path.
- Treat machine-local inbox consumption as sufficient delivery evidence; do not require optional tmux wake-up or automatic notification machinery.
- Explicitly exclude this pre-pilot grill and prior PR consultations from satisfying the gate.

### #150

- Make the exclusive writer lease and ownership matrix explicit.
- Require fresh process state, baseline self-verification, read-only Git introspection, no inherited thinker conclusions, no push/GitHub authority, and handoff-before-Amp-reacquisition ordering.
- Define success as one clean commit beyond baseline at submission, permitting amend/squash beforehand; specify normal hook policy, cleanliness modulo ignored files, explicit attribution, and submission freeze.
- Add a zero-commit `blocked` outcome that preserves dirty evidence for Amp inspection without admitting dirty or patch handoffs.

### #151

- Record both Claude-performed and independent Amp verification without treating either as merge approval.
- Require repeated useful evidence plus one real recovery before proposing promotion; one successful run may still justify `repeat`.
- Keep integration, GitHub mutation, and cleanup outside delegation completion.

### Interactive delegation proposal

- Add the input capability classes and fail-closed fallback.
- Add launch-enforced authority and configuration provenance, `in_reply_to`, nonce/incarnation identity, and diagnostic-versus-semantic completion.
- Prefer a combined bootstrap/evidence/question initial launch packet; carry bootstrap acceptance in the semantic answer rather than requiring a separate delegate acknowledgement turn.
- Make no-Bash Read/Grep/Glob the Pilot 1 default and describe sandboxed Bash only as a later capability upgrade requiring separate proof.
- Add usage-limit, capture-invalid, resumed-new-incarnation, submission-freeze, and zero-commit-blocked semantics.
- Require a fresh dedicated disposable worktree for Pilot 1; remove shared-workdir execution from its supported path.
- Remove typed `input_request`, automatic notification/wake-up, automatic follow-up/parking, and cleanup execution from #148's required implementation. Machine-local inbox consumption is sufficient delivery evidence.
- Replace hook-dependent semantic validation with the Amp-owned `submit_report` server unless a separately declared Amp-owned hook is later proven necessary.
- State that full interactivity is a fixed experiment input for subscription-backed vendor capability, not the mechanically smallest transport; the pilot measures whether its value justifies the resulting recovery surface.

### ADRs and skill documents

- Do not edit ADR 0001.
- ADR 0002 remains proposed. It may be narrowed to cite the launch-enforced thinker boundary and explicit input classes, while keeping `/amux` primary and all promotion gates.
- If #148 proceeds, update `skills/amux/SKILL.md`, `reference/trigger-phrases.md`, the progressively disclosed workflow/contract/recovery references, and consistency tests together. Keep normative mechanics out of the trigger table and top-level skill. Do not add `/claude`, split Claude skills, `/cmux`, or a mutating sequence reference yet.

## Disagreements and reconciliations

- The original stakeholder described OS sandboxing as the read-only proof for Bash. Official documentation shows that the default sandbox permits current-workdir and linked-worktree Git writes. The reconciled Pilot 1 contract exposes no Bash and treats sandboxed Bash as a later, separately proven capability.
- The original stakeholder favored a distinct bootstrap acknowledgement before the evidence packet. The transport evidence shows that an extra interactive round trip creates an unsupported input dependency. Bootstrap acceptance now lives in the semantic answer; coordinator receipt acknowledgement remains a separate durable event.
- A fresh stakeholder claimed interactive `dontAsk` does not auto-deny. Official documentation explicitly says it auto-denies tools unless pre-approved, so the design needs no denial hook for that behavior. `dontAsk` still requires an exact allowlist and effective inherited-policy audit and is not filesystem enforcement.
- A fresh stakeholder proposed launcher-side JSON/stdout capture as the narrowest report seam. That would require non-interactive output or terminal interpretation, conflicting with the fixed full-interactive pilot and diagnostic-only pane output. The reconciled design uses one explicit schema-limited report tool.

## Evidence publication boundary

This proposal intentionally retains only generalized failure shapes, aggregate outcomes, and design conclusions. The detailed source dossier remains private and outside the repository. Publishing an identifying source, operational address, local path, or verbatim interaction requires separate itemized owner approval.
