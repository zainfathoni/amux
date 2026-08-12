---
status: accepted
date: 2026-08-12
supersedes: 0005, 0006
---

# Retire Amux through native cutover and staged drain

## Decision

Amux will retire as an Amp orchestration and local-client lifecycle product. Native Amp owns new Amp-to-Amp thread creation, Orb or exact live-runner placement, parent/child association, messaging and reply routing, waiting, and thread archival. Amux must not create a parallel durable representation for new work merely to reproduce those capabilities.

Retirement is a staged drain, not an immediate read-only freeze and not a new retirement workflow engine:

1. stop admitting new Amux-owned workers, runners, workspaces, shelves, groups, reports, callbacks, deadlines, spawn assignments, and maintenance registrations as each native replacement route cuts over;
2. cut over one operation family at a time, with one source of truth before and after the boundary and no dual-writing;
3. retain narrowly bounded drain-only mutations for durable state that existed before that operation family's cutover;
4. export and freeze each store only after every record is terminal, explicitly retained with an exact recovery owner and next action, or preserved as immutable indeterminate evidence;
5. remove writers after the freeze gate, retain a time-bounded read-only compatibility surface, then archive the lifecycle product; and
6. preserve Git/worktree removal safety as a small standalone safety capability rather than as an Amux resource lifecycle.

This decision supersedes ADR 0005's permanent maintained-lifecycle mission and ADR 0006's proposed Amux retirement-record/finalizer direction. Their useful invariants survive where this ADR names them: exact identity, no blind retry, preservation before destructive cleanup, independent authority for destructive actions, no implicit descendant mutation, and fail-closed handling of ambiguity. Their conclusion that those invariants require a permanent Amux orchestration substrate does not survive.

## Why the destination changed

Native Amp now exposes the capabilities that Amux previously compensated for: a child can be created in an Orb or on one exact live runner, the parent/child route is retained by Amp, existing threads can receive authenticated messages, child work can reply to its source or be awaited, and thread archive state is native. Continuing to mirror those operations through workers, groups, callbacks, report wake-ups, and a new append-only retirement stream would preserve the coordination cost after its product gap closed.

Amux still has evidence that native Amp does not replace directly: historical thread/workdir bindings, local tmux/process ownership, shelf intent, pending finish obligations, indeterminate sends, provider receipts, and worktree-loss evidence. Those are migration inputs. They justify a careful drain and compatibility reader, not indefinite admission of new Amux state.

## Admission boundary

### No new core Amux resources

After the owning implementation change cuts over an operation family, no command, skill, migration, or recovery path may create a new resource in that family. In particular:

- native authenticated Amp thread creation is the default for all new work and must select the exact intended Orb or live runner and workdir directly; generalized `amux spawn` is not a new-work route;
- a native-created thread is not automatically passed through `amux worker adopt`, pinned, grouped, shelved, or assigned an Amux report merely for local representation; if its workdir conflicts with an existing same-directory Amux ownership claim, Amux leaves it unmanaged rather than adopting, rebinding, or manufacturing exclusive ownership;
- no new Amux runner is pinned or maintenance schedule installed; local Amp runner processes move to explicit native runner IDs and owner-selected operating-system supervision;
- no new group, callback lease, deadline, finish authorization, or general report is created; and
- no replacement resource may be created to make an old indeterminate operation appear recoverable.

The only retained new-work exception is an explicit owner-authorized projectless physical-host route where native project-backed Orb/runner creation cannot express the requested placement. It is not a generalized spawn fallback: it must bind one exact physical host and workdir before launch, create no group or unrelated lifecycle state, and remain fail-closed on any identity, placement, ownership, launch, or delivery ambiguity. An indeterminate attempt is preserved as indeterminate and is never retried, adopted, rebound, or rerouted through another host, Orb, runner, or transport. Remove this exception when native authenticated creation can represent the same projectless physical-host placement.

Cutover is per operation family, not one global flag. The implementation and release notes must publish the exact cutover generation for each family. A client that cannot prove whether the target record predates that generation fails closed.

### Temporary Tycho admission exception

`/amux-tycho` remains available under its current explicit-only, report-only receipt contract until a direct route has been field-validated. This is the only temporary exception that may create new compatibility records after core admission starts closing, because deleting the bridge first would make provider output less trustworthy rather than more native.

The authenticated direct structured-return acceptance gate requires one ordinary owner-authorized task to prove all of the following in one route:

1. one exact owner-selected Tycho route is spawned without fallback;
2. the request and returned result are authenticated and correlated to the invoking Amp request/thread and the intended task and artifact identity;
3. that caller receives exactly one schema-valid, bounded structured `complete` or `blocked` response directly through the selected route;
4. provider exit, logs, pane text, memory, and unbound prose are not accepted as the response;
5. `blocked` carries its exact blockers and incomplete scope;
6. the Amp caller independently verifies any finding and remains the only authority for GitHub or other shared mutation; and
7. interruption and no-response behavior produce no finding and no automatic retry.

The target response contains only `status`, `summary`, `findings`, `blockers`, and `verification`, with reviewed bounds. Returning that authenticated structured response directly to the invoking caller establishes delivery; the replacement uses no Amux receipt, consume, or acknowledge step. Once this single field gate is accepted, new Tycho receipt creation stops at a published generation. Existing receipts drain only through their current exact lifecycle, `created → valid_report → delivered → acknowledged|abandoned`. `acknowledged` and `abandoned` are terminal receipt states; notification uncertainty and terminal capability-cleanup status remain separate metadata and never become receipt states. The old and new routes never run for the same task.

## Drain-only mutation contract

Read-only-at-once is unsafe. Existing armed sends, pending reports, shelf transitions, runner ownership, and Tycho receipts can require mutation to reach a truthful terminal state. A drain-only mutation is allowed only when all of these hold:

- the exact resource and operation identity existed before its family's cutover generation;
- the request is an exact replay or an already-defined next transition for that resource;
- the transition creates no successor, replacement, descendant, broadened scope, new task, or new authority;
- all current identity, process, workdir, archive, and authorization preconditions are revalidated;
- rejection or uncertainty retains the old evidence and does not fall back to another transport;
- the result is terminal or strictly closer to terminal under the existing state machine; and
- the operation is recorded by the existing store only, never also by a new native or Amux store.

Allowed examples include finalizing an already armed pre-cutover spawn without resending; acknowledging or finishing an already authorized report; completing an already recorded shelve/unshelve transition; safely parking or removing an already configured worker or runner; replaying terminal Tycho capability cleanup; and explicitly abandoning an eligible created-only Tycho receipt. Listing, doctor, history, pending, export, and integrity checks remain read-only. The projectless physical-host exception above is a separately bounded admission exception, not a reason to treat other new spawns as drain work.

Forbidden examples include pinning a replacement worker to drain an old one, creating a new group/report to describe migration, upgrading indeterminate delivery to accepted, resending after an armed or accepted attempt, recreating a missing worker, or writing both an Amux transition and a native replacement record for one operation.

## Store freeze and compatibility

Each store follows the same lifecycle:

```text
admission open → admission closed / drain writable → exported and frozen
→ compatibility readable → writer and schema support removed
```

Before freeze, per-store accounting must classify every record as terminal, explicitly retained, indeterminate-preserved, or blocked with an owner and next action. Export is versioned, privacy-safe, deterministic, and lossless for identity, event history, assignment state, authorization, and uncertainty. Export never claims that a cursor proves execution or that absent runtime proves successful cleanup. This accounting does not extend or rerun the one-time `/amux sweep` inventory.

Frozen source files stay at their original paths through the compatibility window. They are not automatically deleted, moved, compacted, upgraded, or imported into a second database. The compatibility reader performs no reconciliation or mutation. A release that removes a writer must state the minimum reader version, downgrade boundary, sunset date, and recovery path for unsupported or corrupt state.

The exact duration of the read-only compatibility window is a release decision. Its completion gate is evidence-based: at least one released reader can inspect every frozen schema, the per-store accounting has no unowned blocker, and release notes identify where the immutable export and original files remain.

## Runner orphan migration and issue #232

Runner retirement must not repeat the #232 failure mode in which configuration removal leaves a catalog-live `amp --no-tui` process unmanaged.

Before runner admission closes, a bounded migration inventory joins each `runners.tsv` row with canonical workdir evidence, tmux pane/process incarnation, executable and argv, and native Amp live-runner evidence. Removal preflight must retain the row whenever process or catalog evidence is live, conflicting, unreadable, or unproven, even if the old local classifier says `absent`.

For each configured runner:

1. **Exact owned process:** revalidate pane, PID/start incarnation, executable, argv, canonical workdir, and native runner evidence; park the exact process through the existing lifecycle, prove absence, then remove the row and export the outcome.
2. **Already absent:** prove process and native catalog absence, then remove the row as a drain transition.
3. **Conflicting or ambiguous:** retain the row and emit a migration blocker. Do not kill, adopt, unpin, launch a replacement, or infer ownership from cwd, name, heartbeat, or idleness.
4. **Previously orphaned with row absent:** reconstruct a temporary drain binding only when an immutable prior Amux outcome plus exact current process-incarnation evidence proves the same resource. Otherwise record it in the migration inventory as an external orphan; the owner must stop that exact process through the owning native/OS surface, after which Amux verifies absence but never claims it performed the stop.

Only after old absence is proven may the owner start a native runner with a stable `--runner-id` under the selected OS service. Starting that runner is a native/OS operation, not an Amux write. Work on #232 is complete for retirement when this preflight and recovery path are tested for configured-live, configured-absent, row-absent-live, conflicting, interrupted-stop, and exact-replay cases; it need not make runner lifecycle a permanent product.

## Git and worktree safety

PR #363 is retained transition safety. Backup refs, ref-coverage classification, separate branch deletion, untracked and ignored-precious inspection, stash reporting, purpose/PR checks, ownership checks, and adjacent revalidation remain required before worktree removal. These rules survive Amux because Git and native Amp do not make filesystem deletion safe automatically.

The safety surface must not grow into a retirement ledger, provider reconciler, or durable worktree owner. A backup ref preserves Git objects; it does not authorize worktree removal, branch deletion, provider cleanup, or Amp lifecycle mutation.

## Rollout sequence

1. **Adopt this direction:** publish the superseding ADR and active disposition ledger. Make no runtime claim from documentation alone.
2. **Run and remove the bounded inventory:** merged PR #360 provides one strictly read-only migration inventory and completed #351. After the owner accepts one inventory or explicitly dispositions it incomplete/error and confirms no repeat is required, delete the helper, sweep-only tests, and every `/amux sweep` route/reference before the next Amux release. Do not wait for store freeze or reuse it as a recurring or final reconciler.
3. **Close the runner safety gap:** implement and test the #232 migration preflight before closing runner admission.
4. **Validate direct Tycho return:** retain the current bridge until the field gate above passes.
5. **Close admission per family:** native Amp becomes the sole owner for new work; publish each cutover generation and reject new Amux resources.
6. **Drain:** allow only the transitions defined above. Never dual-write or manufacture terminal truth.
7. **Export and freeze:** freeze one store at a time after its inventory gate passes.
8. **Remove writers:** remove mutating commands, skill routes, schedulers, callbacks, and receipt helpers after their stores freeze.
9. **Read-only window:** ship the compatibility reader and migration diagnostics for the declared window.
10. **Archive:** remove schema readers only after the release gate, retain historical documentation and exports, and archive the orchestration product.

## Consequences

The staged drain is slower than deleting the CLI immediately, but it avoids stranding the exact recovery evidence Amux was built to preserve. It is materially smaller than implementing the symmetric-retirement roadmap: no new retirement stream, attachment generation, six-class planner, provider assertion framework, finalizer, or second database is introduced.

Native Amp becomes the ordinary execution and coordination layer. Amux temporarily remains capable of finishing its own already-recorded operations, then becomes a reader, then ends. Worktree safety and the minimal Tycho structured-response contract may survive as independent skills without retaining the Amux lifecycle model.

## Non-goals

- deleting or rewriting user state from this ADR;
- automatically stopping processes, archiving threads, abandoning receipts, or removing worktrees;
- merging, closing, or mutating issues and pull requests;
- preserving every current command until one global end date;
- introducing a new durable retirement, migration, task, or provider state machine;
- treating native message acceptance as execution or completion;
- treating a Tycho process exit or log as a semantic report; or
- touching unrelated repositories or provider-owned state during Amux migration.

## Follow-up authority

The concrete issue, pull-request, and thread recommendations are maintained in [the staged-drain disposition ledger](../staged-drain-disposition.md). That ledger records recommendations, not shared-state mutations. Each implementation slice still requires review and the destructive/shared actions named there require separate owner approval.
