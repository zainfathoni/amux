---
status: accepted-for-implementation
issue: 320
base: 9bea1aede3b4945f207d158c90c127a81bccb240
implementation: roadmap-tracked
roadmap: 331
---

# Symmetric creation and per-resource retirement dispositions

This proposal resolves the remaining planning choices in [issue #320](https://github.com/zainfathoni/amux/issues/320) under accepted [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md), [ADR 0005](../adr/0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md), and [ADR 0006](../adr/0006-bound-thread-delegation-and-require-preservation-before-retirement.md). It defines a contract for later implementation; it changes no command, schema, skill, durable state, or runtime behavior.

The design is deliberately not a workflow engine. It has one narrow append-only retirement record, one fixed dependency table, no persisted scheduling state, and no daemon, scheduler, resident finalizer, custom DAG, or generic state machine. Prepare derives current facts afresh. Finalize makes a bounded pass over the fixed table from a verified independent executor.

## Decision summary

### Settled owner decisions

These constraints come from #320 and are not reopened here:

1. Retirement authority lives in a dedicated, narrow append-only record keyed by exact worker/thread identity. Spawn, adopt, and catalog state reference it, and it survives catalog-row removal. Reports and provider receipts are evidence, never retirement authority.
2. A worktree may have multiple exact thread attachments. No exclusive worktree owner or transfer ceremony is introduced. Attachment, worker ownership, execution transport, and archive authority are separate axes.
3. The final departing attachment must either complete safe worktree removal or append an explicit retained/blocked disposition with a next action and review point. A verified independent finalizer need not have been attached.
4. Ordering uses the fixed table in this document: preserve useful work first, retire each independently proven-safe resource next, and remove the catalog/recovery pointer last only if no retained resource needs it.
5. Dirty disposable work is never discarded automatically or through a prompt. A dry run generates a privacy-safe manifest and stable digest; a later noninteractive invocation carries that digest. Adjacent mismatch requires a new digest and owner authorization.
6. After exact attachment, runner, and process checks; useful-work preservation or exact dirty-discard authorization; and adjacent revalidation, the owner controls physical-worktree disposition despite an unrelated provider hold. That authority does not extend to provider evidence, provider processes, other processes, archive authority, or full-retirement claims.

### Approved decisions

The owner accepted the following smallest safe answers by merging [PR #330](https://github.com/zainfathoni/amux/pull/330). Implementation is tracked in [issue #331](https://github.com/zainfathoni/amux/issues/331):

1. **Plan lifetime and drift:** persist immutable identity/intent bindings and append outcomes; regenerate observations and proposed actions on every prepare. Bind finalize to a canonical manifest digest. Identity, scope, authority, attachment generation, evidence commitment, or dirty-state drift requires a new prepare; authority-bearing drift also requires new owner authorization. Exact no-drift retry does not.
2. **Provider contract:** consume one provider-owned, versioned, bounded disposition assertion containing evidence identity/commitment/location class, disposition, blocker, recovery dependence, prohibited actions, owner decision where applicable, and review data. Core verifies shape and commitments but does not parse provider receipts or infer provider state.
3. **Recovery loss:** when worktree removal abandons a provider recovery route, append the exact provider evidence identity, abandoned capability, worktree identity/state digest, owner decision binding, time, consequence, still-retrievable evidence commitment, next review point, and prohibited later actions. Later code may not claim recovery, resume/retry through the lost route, mutate evidence to manufacture completion, release a provider fence, or claim provider/full retirement from that record.
4. **Legacy admission:** create a record by explicit, dry-runnable admission after exact rediscovery. Unknown attachments, descendants, ownership, or preservation become `unproven`. Admission imports no destructive authority, does not recreate an absent worker, and does not infer intent from a clean directory, merged PR, report, receipt, or missing process.
5. **Final-departure recovery:** serialize attachment departure under a bounded worktree-identity lock and generation. The winning final departure appends responsibility before releasing the attachment; any verified independent executor may later finalize that exact operation. The departing thread need not remain alive as transport. A concurrent loser re-reads the appended result and exits as an idempotent no-op.
6. **Record mechanics:** use a versioned, line-oriented append-only encoding in Amux's existing private configuration/state boundary and existing bounded machine mutation lock, with a distinct lock key for exact worktree attachment generation. Do not create a second database. Exact location, filenames, lock composition, and command spelling remain owner-approved implementation details.

### Slice-reserved implementation mechanics

The approved roadmap may proceed only through separately reviewed slices under #331. The architecture and ordered rollout are accepted; the following exact mechanics remain reserved for code-level owner review in the slices that introduce them:

- exact command names and stable JSON fields/envelopes;
- exact Go and on-disk schema representations, filenames, and locations;
- canonical byte-encoding details within the approved normative model and digest boundary; and
- exact composition and ordering of the existing machine mutation lock and distinct worktree attachment-generation lock.

Provider-specific semantics, provider transport, provider routing, provider process mutation, retention duration policy, automatic review scheduling, and whether a particular retained resource should later be disposed remain with their existing owners. This proposal does not choose them.

## Contract vocabulary and invariants

The normative words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** describe the proposed later runtime contract.

- **Retirement record:** the append-only authority and audit stream for one exact retirement subject.
- **Subject:** an exact worker/thread binding, including an explicit `worker_absent` binding when legacy rediscovery proves no worker row exists. Absence is a fact, not recreated ownership.
- **Attachment:** one exact thread-to-worktree association with a generation and liveness evidence. It grants neither worker ownership nor execution/archive authority.
- **Operation:** one prepare/finalize attempt with stable identity and a digest-bound manifest.
- **Disposition:** the requested and observed result for one exact resource.
- **Evidence commitment:** a stable digest and independently retrievable locator/class supplied by the evidence owner; it is not parsed into authority by core Amux.
- **Review point:** an explicit condition plus a durable reference, and a time only where an existing policy requires one. This design adds no scheduler.
- **Adjacent revalidation:** re-reading current truth while holding the relevant bounded mutation/attachment lock immediately before each mutation.

The following are always distinct:

| Axis | What it establishes | What it never establishes |
| --- | --- | --- |
| Attachment | An exact thread is associated with an exact worktree generation | Exclusive ownership, process control, archive authority |
| Worker ownership | A catalog binding owns exact worker lifecycle resources | Current attachment, current transport, archive authority |
| Execution transport | An exact process/runner currently executes in a worktree | Worker ownership, attachment ownership, archive authority |
| Archive authority | A verified executor has separate exact authority to archive a thread | Worktree/provider/process disposition authority |

No aggregate state such as `merged`, `clean`, `worker_absent`, `provider_blocked`, or `retired` substitutes for per-resource proof. Descendants are exact resources, never an implied subtree action.

## Normative data model

The carrier is one logical append-only stream per `retirement_record_id`. The pseudostructure below defines semantics, not a committed Go type or on-disk schema.

```text
RetirementRecordHeader v1 {
  retirement_record_id       // stable random ID; immutable
  subject {
    thread_id                 // canonical exact Amp thread ID
    worker_binding            // exact catalog identity/version, or worker_absent evidence
    created_by                // spawn | adopt | legacy_admission
    workspace_identity
    initial_worktree_identity // canonical path + repository/worktree identity, if any
  }
  physical_worktree_owner     // exact owner principal/decision authority
  initial_plan[6] {
    class
    items[] {
      resource_id             // exact identity, explicit_none, or explicit_unknown
      expected_disposition
      decision_owner
    }
  }                           // creation intent only; never advance mutation authority
  initial_descendant_state   // exact initial bindings, explicit_none, or explicit_unknown
  created_at
}

DescendantBindingEvent v1 {
  parent_retirement_record_id
  parent_thread_id
  child_retirement_record_id
  child_thread_id
  binding                    // bound; no transfer/removal event exists
  delegation_operation_id
  bound_at
}

AttachmentEvent v1 {
  retirement_record_id
  worktree_identity
  attachment_generation      // monotonic for this exact worktree identity
  thread_id
  event                      // attach | departure_claimed | detached | stale_marked
  liveness_evidence          // versioned exact evidence or explicit unproven
  operation_id
  observed_at
}

PrepareManifest v1 {
  retirement_record_id
  operation_id
  attempt                    // append sequence for the same operation ID
  prepared_by_exact_thread
  intended_finalizer_class   // verified independent executor; not an identity wildcard
  departure_target {         // exact attachment acted on; never inferred from finalizer
    thread_id
    worktree_identity
    attachment_identity
    observed_generation
  }
  attachment_snapshot {
    worktree_identity
    generation
    exact_live_attachments[]
    stale_or_unproven_attachments[]
  }
  required_authority_scopes[] // principal/resource/action/operation; not a later grant
  evidence_commitments[]     // reports/provider/preservation outside-worktree proof
  dirty_discard_manifest_commitment // exact nested commitment when applicable
  resources[6] {             // exactly six class containers
    class
    items[]                  // ResourcePlan entries below
    derived_status
  }
  canonicalization_version
  manifest_digest
  prepared_at
}

ResourcePlan v1 {
  class                      // one of the six exact classes
  resource_id                // exact discriminated identity
  observed_state_commitment
  requested_disposition
  status                     // planned | already_satisfied | retained | blocked
  blocker                    // absent, or exactly one taxonomy value
  reason_code
  depends_on[]               // only edges allowed by the fixed table
  decision_owner
  required_authority_commitment
  next_safe_action
  review_point
}

FinalizeEvent v1 {
  retirement_record_id
  operation_id
  manifest_digest
  finalizer_identity
  resource_class
  resource_id
  precondition_commitment
  outcome                    // completed | already_satisfied | retained | blocked | interrupted
  blocker
  evidence_commitment
  reason_code
  decision_owner
  retained_dependencies[]
  next_safe_action
  review_point
  effective_recovery_loss_authorization_ids[] // only on proven worktree absence
  occurred_at
}

RecoveryLossAuthorization v1 {
  recovery_loss_authorization_id
  retirement_record_id
  operation_id
  provider_evidence_id
  abandoned_capability
  proposed_worktree_identity
  worktree_state_digest
  owner_authority_commitment
  accepted_at
  consequence
  retained_evidence_commitment
  independent_locator_class
  retrieval_proof_commitment
  prohibited_later_actions[]
  next_safe_action
  review_point
}

// Recovery loss becomes effective only when a linked FinalizeEvent proves
// the exact worktree absent after removal. Authorization alone records intent.

OperationSummary v1 {
  retirement_record_id
  operation_id
  manifest_digest
  retirement_completion      // full | partial | none
  plan_satisfaction          // satisfied | incomplete
  resource_outcomes[6]
  emitted_at
}
```

### Identity, digest, and append rules

- `retirement_record_id` is allocated once. Spawn/adopt/catalog references are pointers, not the authority stream itself. Removing a catalog row MUST NOT remove the stream.
- `operation_id` is a caller-supplied stable idempotency key scoped to the retirement record and requested action set. Retrying the same intent uses the same ID. Changed scope or requested dispositions require a new ID.
- `manifest_digest` covers the canonical version, record and operation IDs, every item in all six classes, requested dispositions, observed-state commitments, departure target, attachment generation/snapshot, evidence commitments, required authority scopes, blockers/dependencies, and proposed actions.
- A supplied authority grant is an external envelope that references the completed manifest digest. Its signature/digest-reference bytes are not themselves included in that digest. The manifest commits the expected principal, exact resource, action, and operation scope, eliminating any digest/signature cycle.
- Human descriptions, timestamps that do not affect safety, and privacy-sensitive raw path/content details are excluded from canonicalization. Their privacy-safe identity digests and discriminators are included.
- Append entries are immutable and sequence-checked. Correction is a new superseding entry; no prior entry is rewritten or deleted.
- A descendant created or adopted after its parent's header MUST append `DescendantBindingEvent` under the bounded machine mutation lock before delegation proceeds. Failure to preserve both exact record/thread identities fails delegation closed. Prepare derives ancestry from the immutable initial state plus these events; later child disposition neither removes nor transfers ancestry.
- A digest is correlation, not ambient authority. Finalize requires the explicit digest, exact operation ID, verified independent-executor fence, and every separate authority required by an action.
- The stream stores outcomes and audit commitments, not a queue, wake-up, deadline, scheduler cursor, or general transition graph.

## Six resource classes and output contract

Every prepare and finalize output MUST include exactly these six top-level class containers. Each container has zero or more exact `items[]` and a derived class outcome; no seventh Runner or attachment class is added. An item absent at finalize is `already_satisfied` only when exact former identity and no residual obligation are proven. An immutable creation-bound `explicit_none` is also `already_satisfied`; `explicit_unknown` is `unproven` until rediscovered.

| Class | Required exact identity and facts | Permitted disposition examples |
| --- | --- | --- |
| `amp_thread` | Canonical thread ID, current archive state, separate finish/archive authority, independent-executor proof | `archive`, `retain`, `already_absent_or_archived` |
| `tmux_client_process` | Exact tmux session/window/pane/client and process incarnation/ancestry; runner and unexpected-process distinctions | `stop_exact`, `retain`, `already_absent` |
| `git_worktree` | Exact repository/worktree identity, canonical path digest, `HEAD`, index, dirty-state commitment, branch/commit preservation, attachments/runners/processes | `remove_normal`, `retain_preserved`, `retain_blocked`, `already_absent` |
| `catalog_recovery_pointer` | Exact catalog row/version and pointer obligations to retry/evidence/attachments | `remove`, `retain`, `already_absent` |
| `provider_evidence` | Provider-owned evidence ID, commitment, independent locator class, bounded assertion and recovery dependence | `retain`, `quarantined`, `owner_abandoned`, `settled` |
| `descendant` | Each exact child thread and its own retirement record/disposition; explicit `none` is allowed | `retain`, `reference_child_result`, `blocked`; never aggregate archive/teardown |

`tmux_client_process` expands the exact worker client/process, each Runner transport, and each unexpected process into separate items. `provider_evidence` has one item per exact evidence object. `descendant` has one item per exact child. Other plural resources also expand rather than aggregate divergent outcomes. A class outcome is derived from all its items, and a parent operation never performs a descendant mutation.

### Per-resource status and blocker shape

Prepare uses:

- `planned`: proof and authority currently permit the proposed mutation;
- `already_satisfied`: exact desired state and lack of residual obligation are proven;
- `retained`: retention is the requested safe disposition and has owner, reason, next action, and review point; or
- `blocked`: requested disposition cannot currently proceed and includes the blocker contract.

Finalize uses:

- `completed`: this invocation performed and verified the exact disposition;
- `already_satisfied`: an earlier event or independently verified exact state proves no repeat effect is needed;
- `retained`: explicit managed retention;
- `blocked`: no mutation for that resource; or
- `interrupted`: mutation outcome cannot yet be proven, so exact recovery is required and no blind repeat occurs.

Every `retained`, `blocked`, or `interrupted` result MUST include exact resource identity, blocker where applicable, reason code, decision owner, dependencies retained by this result, exact next safe/recovery action, and review point.

The only blocker values are:

| Blocker | Meaning | Dependency consequence |
| --- | --- | --- |
| `unsafe` | Positive evidence shows the action would violate identity, preservation, ownership, or concurrency safety | Blocks this action and genuine dependent actions |
| `unproven` | Required evidence is missing, ambiguous, stale, or unreadable | Blocks this action and genuine evidence-dependent actions |
| `unauthorized` | Identity and safety are proven, but exact authority is absent | Blocks only the unauthorized action; independent safe work continues |
| `unavailable` | Identity and safety are proven, but the exact action capability is absent or unreachable | Retains only its own action/resource; absence is a no-op success only with exact former binding and no residual obligation |

Every blocker may leave its own exact action unresolved and its own resource retained. Only `unsafe` and `unproven` can propagate a blocker to another resource, and only when the fixed table names a genuine evidence dependency. If unavailability prevents proving identity, safety, or absence of residual obligations, classify that affected proof as `unproven`. `unauthorized` and `unavailable` never propagate cross-resource blockers. An explicitly managed retained resource may retain the catalog/recovery pointer it factually needs; that is a fixed locator obligation, not blocker propagation.

### Plan satisfaction and retirement completion

The output reports two non-overlapping aggregates:

- `plan_satisfaction=satisfied` means every item received its requested disposition, including a requested managed retention. `incomplete` means at least one requested action is blocked or interrupted.
- `retirement_completion=full` means every item in all six classes, including every descendant item, is verifiably retired or `already_satisfied`; no resource is retained, quarantined, owner-abandoned, blocked, or interrupted.
- `retirement_completion=partial` means at least one item was retired or newly proven `already_satisfied`, while at least one resource remains retained, quarantined, owner-abandoned, blocked, or interrupted.
- `retirement_completion=none` means no item was retired or newly proven `already_satisfied`.

Prepare reports `plan_readiness=ready | partially_ready | blocked`, derived respectively from all, some, or none of the requested mutations being `planned`/`already_satisfied`; it never predicts retirement completion. A provider `retained`, `quarantined`, or `owner_abandoned` assertion is not provider retirement, even when it satisfies the requested management plan.

## Fixed dependency table

The table below is exhaustive. An implementation MUST NOT accept custom edges. “May retain” requires an `unsafe` or `unproven` blocker plus the named factual dependence; it is not automatic ordering. The sole non-blocker locator rule is that explicit managed retention retains its catalog/recovery pointer when that pointer is its only exact locator or retry authority.

| Order | Action/resource | Must be proven immediately before action | May retain another resource | Never retains |
| --- | --- | --- | --- | --- |
| 1 | Preserve useful Git work | Exact worktree state; retrievable commit/branch, patch, bundle, or exact retained worktree; validation and recovery owner | Physical worktree and Amp-thread archive when preservation is unsafe/unproven; catalog pointer when needed to locate the retained work | Exact tmux client or provider evidence merely because preservation is blocked |
| 2 | Record provider/report/preservation evidence independently | Required evidence commitment and retrieval route are outside or independently retrievable from removable worktree | Worktree only when required future recovery genuinely uses that worktree and owner has not accepted exact recovery loss; catalog pointer only when needed to locate/retry evidence | Exact tmux client, unrelated thread archive, independently preserved worktree |
| 3 | Resolve attachment departure and execution transport | Exact attachment generation/liveness; runner ownership; expected and unexpected process identity | Worktree while a live/unproven attachment, runner, or unexpected process uses it | Provider evidence, preserved Git references, unrelated catalog pointer |
| 4 | Stop exact tmux client/process | Exact incarnation and ancestry; independent executor; action authority | None | Worktree, provider evidence, thread, descendants |
| 5 | Dispose physical worktree | Final-departure claim; no exact runner/unexpected process; one of five Git cases; independent evidence; normal non-force removability; adjacent revalidation | Catalog pointer if retained worktree recovery needs it | Provider hold without worktree-dependent recovery; unauthorized/unavailable provider action; stopped/retained thread or tmux |
| 6 | Record each descendant result | Exact child identity and child-owned disposition reference | Parent archive/completion claim while a child result is unsafe/unproven; no implicit child mutation | Independently safe parent tmux/worktree action unless that child is a live/unproven attachment |
| 7 | Archive Amp thread | Exact thread; useful-work preservation; separate finish/archive authority where required; verified independent executor; descendant results explicitly recorded | None | Worktree, tmux, provider evidence, descendants |
| 8 | Remove catalog/recovery pointer | Every retained resource has an independent locator and retry authority; no attachment or recovery-loss follow-up needs the pointer | None | Any earlier independently safe disposition |

Within orders 2–7, implementations SHOULD continue every action whose prerequisites are independently proven. The order is a preservation and pointer-removal boundary, not a requirement to stop after the first unrelated failure.

## Worktree disposition contract

All cases require exact worktree identity, attachment-generation revalidation, runner and unexpected-process checks, independent evidence proof, normal non-force removability, and an exact owner decision. Unknown facts fail closed.

| Case | Required proof and authority | Result |
| --- | --- | --- |
| Clean removable | Clean `HEAD`/index/worktree; integration or proof of no unique work; no attachment/runner/process dependency | Plan `remove_normal`; otherwise state the missing proof |
| Clean preserved-unmerged | Clean state; exact unique commit/branch preserved at an independently retrievable reference; recovery owner and review point | Worktree may be removed while the Git reference is retained |
| Dirty useful | Privacy-safe state commitment plus explicit retrievable commit, patch, bundle, or exact-worktree retention; validation, owner, recovery/review point | Remove only after approved preservation succeeds and is revalidated; merely leaving dirt is not preservation |
| Dirty disposable | Privacy-safe discard manifest/digest, exact physical-worktree owner authorization carried noninteractively, and adjacent digest match | Normal removal of only the exact authorized state; no prompt, force, branch/path wildcard, or automatic discard |
| Unknown/unsafe | Identity, cleanliness, uniqueness, attachment, runner/process, evidence, preservation, or normal-removal proof is absent/contradictory | `blocked` with `unsafe` or `unproven`, exact missing/contradictory fact, next action, and review point |

### Dirty-discard manifest

The privacy-safe manifest MUST bind without emitting file contents, secret values, full patch text, or unbounded path data:

```text
DirtyDiscardManifest v1 {
  repository_identity
  worktree_identity
  canonical_path_commitment
  head_oid
  branch_or_detached_discriminator
  index_tree_oid_or_stable_index_commitment
  status_entries[] {
    path_hmac_or_repository_scoped_digest
    path_type
    tracked_state
    staged_state
    unstaged_state
    ignored_state
    content_or_object_commitment // mandatory for every byte/object removal would discard
  }
  submodule_and_nested_repo_commitments[]
  untracked_count
  ignored_items_policy_discriminator
  ignored_item_commitments[]     // mandatory when normal removal would discard them
  attachment_generation
  manifest_version
}
```

Dry run emits the stable `DirtyDiscardManifest` commitment and a bounded human summary, but no authority-bearing prepare digest. Every tracked, untracked, ignored, nested-repository, submodule, symlink, or other filesystem object that normal removal would discard MUST contribute an exact content/object commitment; inability to commit one is `unproven`. Recorded prepare places that commitment in `PrepareManifest.dirty_discard_manifest_commitment`, then computes the one authority-bearing `PrepareManifest.manifest_digest`. A separate invocation supplies that manifest digest and exact operation ID. The external owner authorization envelope binds owner identity, worktree identity, the `PrepareManifest.manifest_digest` (which transitively binds the dirty state), allowed `remove_normal` action, and operation ID. It grants no future-state authority and is not recursively included in the digest it references.

Finalize regenerates the manifest while holding the worktree/attachment mutation boundary. Any difference in `HEAD`, index, tracked/untracked/ignored policy result, nested repository, identity, attachment generation, runner/process use, or authorization fails before removal. The remedy is a new prepare and new owner authorization; there is no prompt and no “close enough” retry.

## Provider disposition boundary

Provider evidence remains append-only and independently retrievable. Prepare MUST prove each report, receipt, manifest, patch/bundle, provider artifact, and retirement entry needed later is outside the removable worktree or available through a tested independent retrieval commitment. Core Amux does not parse provider receipts to establish this.

A provider-owned surface may return only this bounded assertion:

```text
ProviderDispositionAssertion v1 {
  provider_contract
  provider_contract_version
  retirement_record_id
  operation_id
  evidence_id
  evidence_commitment
  independent_locator_class
  retrieval_proof_commitment
  disposition               // retained | quarantined | owner_abandoned | settled
  blocker                   // optional exact taxonomy value
  reason_code
  worktree_recovery_dependency {
    depends_on_worktree      // true | false | unproven
    exact_capability
    consequence_of_removal
  }
  origin_fence_state_commitment
  prohibited_actions[]
  decision_owner             // required for every disposition
  owner_authority_commitment // additionally required only for owner_abandoned
  next_safe_action
  review_point
  asserted_at
  assertion_commitment
}
```

Core validates version, exact operation/record binding, evidence and retrieval commitments, allowed enum values, required fields, and trusted provider-surface identity. It MUST NOT interpret raw receipt state, mutate evidence/provider processes, infer semantic completion, or release origin fences.

- `retained` means evidence and recovery capability remain available under the provider contract.
- `quarantined` means evidence remains append-only/retrievable but provider-owned mutation is prohibited pending its exact next action.
- `owner_abandoned` means the owner accepts loss of one named recovery capability. Evidence remains append-only and retrievable. It does not mean deleted, completed, or retired.
- `settled` means the provider-owned surface proves its exact provider disposition complete. Core may record that bounded result but does not derive it by parsing receipts or perform the provider mutation.

If `depends_on_worktree=false`, any provider blocker cannot retain the worktree. If true, worktree removal is blocked only by `unsafe`/`unproven` unless the physical-worktree owner supplies exact recovery-loss authority. If `unproven`, removal remains `unproven`; abandonment is permitted only after the provider surface enumerates every exact potentially dependent capability and consequence for the owner's bounded authorization. An unknown or open-ended capability set cannot be abandoned. `unauthorized`/`unavailable` provider actions do not block independently safe worktree removal.

### Minimum recovery-loss record

Before removing a worktree that would abandon provider recovery, finalize MUST append and verify `RecoveryLossAuthorization` outside the worktree. It names exactly one capability and consequence and has a unique `recovery_loss_authorization_id`; broad “all recovery abandoned” authority is invalid. The authorization records conditional intent, not a completed loss. Loss and the prohibitions below become effective only when the worktree-absence `FinalizeEvent` lists that exact ID in `effective_recovery_loss_authorization_ids`. Multiple capabilities require separately named authorizations and links. If interrupted, retry first proves presence or absence and never assumes loss from authorization alone.

After removal, later operations MUST NOT:

- claim that the abandoned capability remains available;
- resume, retry, or supersede through the removed worktree route;
- rewrite/delete evidence or manufacture provider completion;
- release an origin fence or mutate a provider process based on abandonment;
- infer authorization for another worktree, operation, provider resource, or process; or
- report provider or full retirement solely from recovery abandonment.

They MAY use a different independently authorized recovery route if its identity and evidence are newly proven.

## Prepare algorithm

Prepare has two explicit noninteractive modes so ADR 0001 dry-run remains non-mutating:

- **Preview** (`--dry-run` in a later command design) performs discovery and emits the six-class plan plus any `DirtyDiscardManifest` commitment, but appends nothing and produces no finalizable `PrepareManifest.manifest_digest`.
- **Record** repeats discovery under the existing bounded machine mutation lock, appends the exact `PrepareManifest`, and emits its finalizable digest. Recording is a durable mutation, not dry-run, but it MUST NOT perform any resource disposition or terminate its own executor.

A preview result cannot be finalized. Record recomputes all facts rather than promoting or trusting preview output.

```text
prepare(record_id, operation_id, requested_dispositions, mode=preview|record):
  1. if record, acquire the existing bounded machine mutation lock;
     if preview, perform only non-mutating reads and reject conflicting mutation
  2. read and verify the complete retirement stream
  3. resolve exact subject; never recreate missing worker ownership
  4. enumerate every item in all six classes, including each exact descendant
     and creation-bound explicit_none or explicit_unknown
  5. read exact attachments, liveness evidence, worktree generation,
     worker/catalog ownership, runner transport, and unexpected processes separately
  6. classify worktree into exactly one of the five cases
  7. ask each provider owner for only the bounded assertion
  8. prove all required future evidence is independently retrievable
  9. apply the fixed dependency table and blocker taxonomy
 10. generate any dirty-discard manifest; do not solicit or prompt for authority
 11. if preview, emit all six class containers, dirty commitment, and
     ready/partially_ready/blocked plan readiness; append nothing; return
 12. if record, recompute and compute canonical manifest digest over identities,
     departure target, generation, evidence, dirty commitment, required authority
     scopes, blockers, dependencies, and proposed actions
 13. append PrepareManifest and fsync/verify according to existing state guarantees
 14. emit all six class containers, plan readiness, and finalizable manifest digest
 15. release machine mutation lock
```

Prepare facts are observations, not durable truth. A later finalize trusts only immutable identity/authority commitments that still match freshly observed truth.

## Finalize algorithm

Finalize is one noninteractive invocation from a verified independent executor. It requires exact `record_id`, `operation_id`, and `manifest_digest`; no “latest” selector is valid.

```text
finalize(record_id, operation_id, manifest_digest, supplied_authorities):
  1. verify independent-executor identity/ancestry before mutation
  2. acquire existing bounded machine mutation lock
  3. verify stream integrity, exact manifest, operation ID, and digest
  4. recognize and verify prior completed events for exact replay
  5. acquire exact worktree attachment-generation lock when attachment/worktree changes
  6. re-read all six resources and provider assertion commitments
  7. globally reject only stream-integrity, subject/operation/digest, or
     independent-executor failure; classify other drift per item, retain genuine
     fixed-table dependents, and continue independent safe items
  8. resolve departure:
       a. act only on manifest.departure_target, never finalizer/caller identity
       b. non-final: append/remove only that target's exact attachment
       c. final: atomically append final-departure claim at current generation;
          claimant or a later independent finalizer owns remove-or-retain recording
       d. losing claimant: return already_satisfied/no-op only after a verified
          terminal detach/remove/retain result; claim-only is interrupted/recoverable
  9. follow the fixed table once:
       a. verify useful-work preservation first
       b. record independent evidence/conditional recovery-loss authority before removal
       c. for each independently authorized action, adjacent-revalidate,
          mutate once, verify desired state, append FinalizeEvent
       d. after a resource failure, continue only independent proven-safe actions
       e. handle each descendant by reference only; never mutate it implicitly
       f. remove catalog/retry pointer last only if no retained dependency needs it
 10. append explicit retained/blocked results for every unfinished resource
 11. append OperationSummary with plan satisfaction and honest retirement completion
 12. release locks
```

If the final departure cannot remove the worktree, its durable retained/blocked event MUST be appended before detachment is considered complete. The thread itself may then exit; finalization responsibility belongs to the operation record, not a live attachment. `departure_claimed` and `detached` generation changes produced by this exact operation are recognized replay transitions and do not invalidate its digest. Any attachment event from another operation, especially a new attachment, invalidates final status and requires fresh prepare before worktree removal.

## Authority matrix

No authority in one row implies authority in another.

| Actor/authority | Attach/detach own thread | Decide physical worktree | Stop process/tmux | Archive Amp thread | Assert/mutate provider state | Preserve/discard useful work | Claim full retirement |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Attached thread | Own exact attachment only | No, unless separately the exact owner | No, unless existing exact authority and independent fence permit | No self-archive | No | May propose/preserve within existing Git authority; no discard without owner digest | No |
| Physical-worktree owner | May authorize exact attachment resolution | Yes, after all required checks and revalidation | No | No | May accept exact recovery loss only; no mutation/completion | May select preservation or authorize one dirty digest | No |
| Verified independent finalizer | Apply exact recorded departure target | Execute only exact owner-authorized disposition | Execute existing exact teardown authority | Execute separate finish/archive authority | Consume bounded assertion only | Execute exact preservation/discard authority | Only when every item in all six classes is retired/already satisfied |
| Finish/archive authority holder | No | No | Only if separately authorized | Exact thread/workflow only | No | No | No |
| Provider owner/surface | No | May report exact recovery dependence, not veto unrelated safe removal | Provider process only under provider contract, never from this record alone | No | Append bounded assertion and provider-owned effects | No | No |
| Descendant owner | Own child only | Only child's separately owned worktree | Child exact resource only | Child exact thread only | Child/provider contract only | Child exact work only | Child result only |

## Drift and retry matrix

| Change after prepare | Finalize behavior | New prepare? | New owner authority? |
| --- | --- | --- | --- |
| No relevant change; exact replay | Verify prior events, skip completed effects, continue remaining exact actions | No | No |
| Cosmetic/human rendering only, excluded from canonical form | Continue after fresh safety checks | No | No |
| Exact operation's own `departure_claimed`/`detached` transition | Recognize expected generation advance and continue/recover exact operation | No | No |
| New/removed/stale attachment or generation change | Reject attachment/worktree mutation; classify precise blocker | Yes | Yes if physical disposition authority must bind new generation |
| `HEAD`, index, dirty entries, nested repo, or manifest policy change | Reject discard/removal before mutation | Yes | Yes for dirty discard; preservation authority as required |
| Worktree/repository identity or canonical path commitment changes | Reject affected and dependent actions | Yes, normally a new operation ID | Yes |
| Runner or unexpected process appears/changes | Reject worktree/process action; independently safe evidence actions continue | Yes | Not unless requested authority changes |
| Provider assertion/evidence commitment changes | Reject provider action; retain worktree only for a genuine unsafe/unproven dependency | Yes | Only for new abandonment/disposition authority |
| Required evidence becomes independently retrievable with same identity | Fresh prepare may clear blocker | Yes | No unless action scope changes |
| Finish/archive authority appears | Fresh prepare binds it; unrelated completed effects stand | Yes | Exact archive authority only |
| Requested disposition/resource scope changes | Reject as different intent | Yes, new operation ID | Yes where newly destructive |
| Catalog row disappears but record and exact former binding survive | Report already absent only if no residual pointer obligation; continue exact recovery | Usually yes to bind current fact | No destructive authority inferred |

## Interruption matrix

Every mutation is preceded by revalidation and followed by append-only verification. If interruption leaves effect uncertain, the resource is `interrupted`/`unproven`; retry inspects exact desired state and never blindly repeats.

| Interruption point | Durable truth/recovery rule | Independent work on retry |
| --- | --- | --- |
| Before manifest append | No finalizable operation exists; rerun prepare | None was authorized |
| After manifest append, before finalize | Exact digest may be supplied later; all facts are re-read | All still-matching actions |
| During preservation | Verify independent artifact/reference commitment; do not assume completion | Nondependent evidence reads only until useful work is safe |
| After preservation, before event append | Re-prove exact artifact/reference and append `already_satisfied` or `interrupted` | Any action whose preservation proof succeeds |
| After provider/recovery-loss authorization, before worktree removal | Evidence and conditional authority are durable; recovery loss is not yet effective; revalidate worktree and authority | Worktree removal if digest still matches; other independent actions |
| During exact process stop | Re-inspect exact incarnation/ancestry; absence succeeds only for exact former identity and no residual obligation | Worktree only if process absence and all other checks prove safe |
| During worktree removal | Reinspect repository registration/path identity; never force or broaden path | Thread/provider actions that do not need uncertain worktree result |
| After worktree removal, before event append | Prove exact worktree is absent and preservation/evidence remain retrievable; append linked result that makes authorized recovery loss effective | Independent thread/process/provider actions |
| During archive | Re-read exact remote state and separate authority; never substitute thread identity | Other independent resources |
| Before catalog-pointer removal | Retain pointer; retry last | All prior independent results stand |
| After pointer removal, before event append | Use surviving retirement record and independent locators to prove absence; never recreate row ceremonially | Append exact already-satisfied result if proven |
| Before summary append | Reconstruct summary from immutable per-resource events | No mutation needs repetition |

## Attachment liveness and concurrency matrix

Attachment liveness is proven with exact thread identity plus the best available exact local/remote generation evidence defined by the later implementation. Unknown liveness is `unproven`; process absence, worker-row absence, tmux absence, or archive state alone is insufficient. Although retirement streams are per subject, every subject sharing a worktree uses one worktree-keyed lock and globally monotonic attachment generation. Surviving retirement streams and attachment events remain discoverable independently after any catalog-row removal.

| Race/failure | Required outcome |
| --- | --- |
| Two non-final departures | Serialize generation updates; each removes only itself and observes the other's current generation |
| Two callers both prepared as final | Under lock, one appends the final-departure claim; the loser returns `already_satisfied`/no-op only after the winner has a verified terminal result; a claim-only winner is recoverable, not satisfied |
| Both initially see another attachment | Each serialized departure advances generation; the actual final departure receives responsibility, or appends retained/blocked before detaching |
| New attachment arrives after prepare | Generation changes; final worktree mutation rejects and requires fresh prepare/authority |
| Attached thread dies without detaching | A verified independent actor may append `stale_marked` only from exact liveness evidence; unknown remains `unproven`; no worker recreation |
| Winning finalizer is interrupted | Claim persists in the retirement stream; any verified independent executor can retry the exact operation/digest and its exact departure target after revalidation |
| Worker/catalog row is absent | Attachment and transport remain independently reportable; absent owned resources are honest no-ops only when proven |
| Shared runner still uses canonical worktree | Retain runner/worktree; independently resolve absent worker/tmux/temp resources; archive remains separately authorized |

## Legacy admission and migration

Existing workers receive no inferred plan or destructive authority. Ordinary lifecycle commands continue current behavior until a separately approved implementation exposes explicit admission.

Legacy admission MUST:

1. be explicit, noninteractive, and dry-runnable;
2. allocate a new record ID and bind an exact canonical thread;
3. rediscover worker row, worktree, attachments, runner/process transport, descendants, evidence, and owner independently;
4. represent an absent worker as `worker_absent` with evidence rather than recreating it;
5. mark every ambiguity `unproven` and retain its genuine dependents;
6. import evidence only by immutable commitment/reference, never rewrite reports or receipts;
7. assign no dirty discard, archive, provider mutation, descendant, or full-retirement authority;
8. require a later prepare and separate exact authority for mutations; and
9. leave legacy rows/files intact until their own exact dispositions succeed.

Cleanliness, merge status, a report acknowledgement, a provider receipt, process absence, or historical catalog membership is evidence only. None implies ownership, attachment absence, unique-work absence, finish authority, or permission to discard.

There is no bulk backfill that guesses attachments. A read-only inventory MAY identify candidates, but each admitted subject has an explicit record and honest unknowns.

## Reconciliation with field evidence

The field cases motivate separations; they do not create special-case code.

- **Absent-worker teardown:** attachment to a shared canonical worktree, absent worker ownership, live shared Runner transport, and missing archive authority become four independent facts. The absent worker row is a `catalog_recovery_pointer` item; the absent worker client is a `tmux_client_process` item; the live Runner is a separate retained item in that same class; temporary evidence is represented by its owning `provider_evidence` item or independent evidence commitment. Already-absent items may be satisfied while worktree/Runner remain retained and archive is `unauthorized` or `unproven`; no worker is recreated for ceremony.
- **BTA #11862/#11863/#11864/#11877:** clean worktrees with divergent unique review commits classify as clean preserved-unmerged only after exact retrievable references and recovery ownership are proven. Worker presence/absence and a live process affect only their exact classes/dependencies.
- **BTA #11871/#11873:** provider evidence unavailable is `unavailable` only if the external capability is unreachable, or `unproven` if evidence safety cannot be established. A final-head-matching, independently safe worktree is not retained unless exact recovery genuinely depends on it.
- **BTA #11870:** unique Git work and created-only provider debt produce two independent blockers. Preserving the commit can clear the Git dependency while append-only provider evidence remains retained; neither result manufactures full retirement.

No bespoke issue/PR-number logic, provider parser, or special teardown route follows from these examples.

| Evidence scenario | Expected decisive outcomes |
| --- | --- |
| Absent worker, live shared Runner | Catalog/worker-client items already satisfied if proven; Runner/worktree retained; thread archive separately blocked; partial retirement |
| #11862 divergent commit, shelved worker | Worktree preservation `unproven` until exact retrievable reference; archive/worktree held, unrelated provider/process items independent |
| #11863 divergent commit, worker absent | Catalog/client absence may be satisfied; Git preservation still independently blocks worktree/archive |
| #11864 divergent commit, worker absent | Same class-level result as #11863 from current evidence, without shared special-case behavior |
| #11877 divergent commit, live process | Git preservation and exact process identity are separate blockers; each independent safe outcome still proceeds |
| #11871 final-head match, provider unavailable | Provider item retained/unavailable or unproven; worktree removable if provider recovery does not depend on it |
| #11873 final-head match, provider unavailable | Same dependency rule as #11871; missing provider action authority/capability does not create a worktree veto |
| #11870 divergent commit plus created-only receipt | Independent Git-preservation and provider-evidence blockers; clearing either does not clear the other or claim full retirement |

## Output example

The eventual stable JSON envelope remains governed by ADR 0001. The inner shape below is illustrative; exact command/schema adoption remains owner-reserved.

```json
{
  "schema_version": 1,
  "retirement_record_id": "ret_...",
  "operation_id": "caller-stable-key",
  "manifest_digest": "sha256:...",
  "plan_satisfaction": "satisfied",
  "retirement_completion": "partial",
  "resources": [
    {
      "class": "git_worktree",
      "derived_outcome": "completed",
      "items": [{
        "resource_id": {"kind": "git_worktree", "identity": "digest:..."},
        "requested_disposition": "remove_normal",
        "outcome": "completed",
        "blocker": null
      }]
    },
    {
      "class": "provider_evidence",
      "derived_outcome": "retained",
      "items": [{
        "resource_id": {"kind": "provider_evidence", "identity": "receipt:..."},
        "requested_disposition": "retained",
        "outcome": "retained",
        "blocker": "unproven",
        "decision_owner": "provider-surface:...",
        "next_safe_action": "recover through exact append-only provider route",
        "review_point": {"condition": "provider assertion becomes retrievable", "reference": "issue-or-record:..."}
      }]
    }
  ]
}
```

All six classes MUST appear even when this abbreviated example shows two.

## Focused implementation slices

These are the owner-approved roadmap boundaries, not implementation in this document. Each slice remains independently reviewable under #331, including any slice-reserved public or mechanical details.

1. **Record and canonical commitments:** approve encoding/location; implement append-only header/events, stable operation ID, canonical digest, privacy-safe rendering, integrity and crash tests. No mutations.
2. **Creation/admission references:** have spawn/adopt reference records; add explicit legacy dry-run/admission with unknown-safe behavior. No retirement mutations.
3. **Read-only prepare:** six-class discovery, five worktree classifications, evidence-independence proof, provider assertion validation, fixed dependency evaluation, dry-run JSON. No finalizer.
4. **Attachment generations:** exact attach/departure events, liveness evidence, bounded lock, final-departure claim, concurrent loser/no-op tests. No worktree removal.
5. **Independent finalize for non-worktree resources:** exact replay/interruption handling around existing process/thread mechanics, with separate finish/archive authority and no descendant mutation.
6. **Preservation and clean worktree dispositions:** preserved-unmerged/dirty-useful evidence plus normal non-force clean removal, adjacent checks, recovery pointer ordering.
7. **Dirty-disposable and provider recovery loss:** privacy-safe manifest/digest, noninteractive owner authority, provider bounded assertion, recovery-loss prohibitions, no provider parsing.
8. **Operating guidance and rollout:** separately reviewed skill/docs adoption required by ADR 0006, compatibility diagnostics, field-case exercises, and staged enablement. Do not combine with schema/runtime review merely for convenience.

Recommended rollout starts read-only, exercises the absent-worker and seven BTA cases as fixtures/scenarios, then enables one disposition class at a time. No migration should silently enable destructive behavior.

## Required verification for implementation

Later implementation tests MUST cover at least:

- all six resource classes and exact descendant expansion;
- every fixed-table dependency and rejection of custom edges;
- all four blockers, especially no cross-resource hold from `unauthorized`/`unavailable`;
- each of the five worktree cases;
- dirty manifest privacy, stable digest, noninteractive handoff, mismatch rejection, and no automatic/force discard;
- owner-authorized worktree removal across provider retained/quarantined/abandoned outcomes;
- evidence outside-worktree proof and recovery-loss prohibited actions;
- exact retry before/after each interruption row;
- attachment liveness unknowns, new attachment drift, simultaneous final departures, and idempotent loser;
- absent worker without ceremonial recreation;
- no implicit descendant mutation or archival;
- catalog pointer retained while needed and removed last otherwise;
- honest `full`/`partial`/`none` summaries; and
- independent-executor and separate finish/archive authority failures before mutation.

Documentation-only review of this proposal should check internal links, Markdown rendering, repository tests, and direct consistency with ADRs 0001/0005/0006 and #320.

## Release and version impact

This proposal has no runtime, schema, command, skill, or durable-state effect. It should not trigger a release or version change. Implementation should not be rushed into an unrelated release: the record/read-only prepare slices should land first, and the first mutating slice should receive its own compatibility and release decision. Any stable JSON/schema or command addition is at least a user-visible feature change; destructive disposition enablement merits explicit release notes and staged rollout, but no major version is implied while backward-compatible and opt-in.

## Approval and implementation gate

The owner accepted this planning deliverable through merged PR #330. Issue #331 records the ordered implementation and release roadmap. Approval permits separately reviewed implementation slices; it does not itself select slice-reserved command/schema/filename/lock mechanics, implement behavior, or authorize a release.

- no implementation slice may infer legacy authority;
- no implementation slice may add a provider parser or generic workflow machinery;
- each slice must preserve its predecessor and release gates from #331; and
- no documentation, design, incomplete foundation, merge, or issue completion authorizes a release.
