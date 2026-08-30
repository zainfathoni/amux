---
status: partially-superseded
date: 2026-08-12
updated: 2026-08-13
supersedes: 0003, 0005, 0006
partially-superseded-by: 0008
---

# Retire Amux through native cutover and staged drain

> **Partially superseded:** [ADR 0008](0008-retain-machine-local-runner-host-and-drain-coordination.md) rejects this ADR's complete-product retirement destination. Amux remains active as the machine-local runner registry and launcher, including automatic `amux launch --all`, tmux/Amp process launch, diagnostics, maintenance, and OS activation integration. This ADR remains current for native ownership of new task coordination and the staged drain of legacy worker/coordination/provider state. References below to closing runner admission, native/OS replacement launch, a whole-product reader sunset, or product archive are historical superseded direction and are not implementation authority.

## Decision

Amux will retire as an Amp orchestration and local-client lifecycle product. Native Amp owns new Amp-to-Amp thread creation, Orb or exact live-runner placement, parent/child association, messaging and reply routing, waiting, and thread archival. Amux must not create a parallel durable representation for new work merely to reproduce those capabilities.

Retirement is a staged drain, not an immediate read-only freeze and not a new retirement workflow engine:

1. stop admitting new Amux-owned workers, runners, workspaces, shelves, groups, reports, callbacks, deadlines, spawn assignments, and maintenance registrations as each native replacement route cuts over;
2. cut over one operation family at a time, with one source of truth before and after the boundary and no dual-writing;
3. retain narrowly bounded drain-only mutations for durable state that existed before that operation family's cutover;
4. export and freeze each store only after every record is terminal, explicitly retained with an exact recovery owner and next action, or preserved as immutable indeterminate evidence;
5. remove writers after the freeze gate, retain a time-bounded read-only compatibility surface, then archive the lifecycle product; and
6. preserve Git/worktree removal-safety guidance outside the Amux lifecycle (default docs-only retain at product archive) rather than as an Amux resource lifecycle.

This decision supersedes ADR 0003's native-create → `amux worker adopt` new-work workflow, ADR 0005's permanent maintained-lifecycle mission, and ADR 0006's proposed Amux retirement-record/finalizer direction. ADR 0003 remains only historical rationale and evidence; none of its operational instructions are current. New native children remain unmanaged by Amux. Only an exact persisted pre-cutover drain-eligible adoption operation may continue its exact allowed next transition. The useful invariants from the superseded ADRs survive where this ADR names them: exact identity, no blind retry, preservation before destructive cleanup, independent authority for destructive actions, no implicit descendant mutation, and fail-closed handling of ambiguity. Their conclusion that those invariants require native-to-Amux enrollment or a permanent Amux orchestration substrate does not survive.

### Accepted refinements (2026-08-13)

Owner-accepted product refinements recorded in [the staged-drain disposition ledger](../staged-drain-disposition.md#owner-decisions-accepted-2026-08-13). They tighten destination language without reopening the staged-drain mission. These refinements establish contract and sequencing only; they do not themselves authorize code, skill publication, runtime or store changes, push/merge, issue mutation, or other shared-state mutation.

1. **Shelve replacement is Archive/Unarchive, not Hide/Unhide.** After shelf admission closes, deferred remote thread visibility uses native Amp archive and unarchive (`amp threads archive` and `amp threads archive --unarchive`). Amux shelf intent (`shelves.tsv`) is drain-only migration state while Amux launch still exists; it is not a permanent product and must not become a second visibility store. Do not treat `find_thread` `hidden:` / `snoozed:` as a mutation API or document “Hide/Unhide replaces shelve” unless a distinct hide mutation surface is later proven different from archive. Existing shelf rows drain in place under this ADR; there is no bulk migrate-to-hidden path.

2. **Tycho long-term home is personal User Skills `amp-tycho`.** The only intentional post-core-admission compatibility producer remains the explicit report-only Tycho binding. After the direct structured-return field gate in this ADR passes, new work uses that route without Amux receipts, consume, or acknowledge. Existing receipts continue only through `created → valid_report → delivered → acknowledged|abandoned`. The long-term skill name/directory is `amp-tycho` in the owner's personal User Skills repository, to be published only after or atomic with that gate and only under separate owner authorization; keep `/amux-tycho` and original helper, state, custody, and abandonment paths stable until receipts are terminal, explicitly retained, or indeterminate-preserved. A thin load-name shim is optional; two helpers or two state machines are not.

3. **Direct return is same-turn Amp-visible structured delivery.** The accepted response shape is one schema-valid bounded `complete` or `blocked` object (`status`, `summary`, `findings`, `blockers`, `verification`) returned as an authenticated structured result on the invoking Amp turn, correlated to that request/thread and task/artifact identity, through one exact owner-selected Tycho route with no fallback, side inbox, log mining, or automatic retry. Amp alone verifies findings and alone performs shared mutation. Concrete API naming and field-cycle acceptance remain release follow-ups in the disposition ledger.

4. **Worktree safety defaults to docs-only retain at Amux archive.** Git/worktree removal-safety guidance remains outside the Amux lifecycle (below), not an Amux resource product. Unless a later owner decision extracts a separately named skill because worktree removal without Amux remains routine, retain the guidance in historical documentation when the orchestration product is archived.

## Why the destination changed

Native Amp now exposes the capabilities that Amux previously compensated for: a child can be created in an Orb or on one exact live runner, the parent/child route is retained by Amp, existing threads can receive authenticated messages, child work can reply to its source or be awaited, and thread archive state is native. Continuing to mirror those operations through workers, groups, callbacks, report wake-ups, and a new append-only retirement stream would preserve the coordination cost after its product gap closed.

Amux still has evidence that native Amp does not replace directly: historical thread/workdir bindings, local tmux/process ownership, shelf intent, pending finish obligations, indeterminate sends, provider receipts, and worktree-loss evidence. Those are migration inputs. They justify a careful drain and compatibility reader, not indefinite admission of new Amux state.

## Admission boundary

### No new core Amux resources

After the owning implementation change cuts over an operation family, no command, skill, migration, or recovery path may create a new resource in that family. In particular:

- native authenticated Amp thread creation is the default for all new work and must select the exact intended Orb or live runner and workdir directly; generalized `amux spawn` is not a new-work route;
- native `create_thread` receives a lean task prompt and retains Amp's parent/reply route only; it receives no `contract-v1.md` path, receipt, report, callback, adoption, group, deadline, finish authorization, or other Amux lifecycle requirement, and active guidance calls it a native child/thread rather than an Amux worker;
- a native-created thread is not automatically passed through `amux worker adopt`, pinned, grouped, shelved, or assigned an Amux report merely for local representation; if its workdir conflicts with an existing same-directory Amux ownership claim, Amux leaves it unmanaged rather than adopting, rebinding, or manufacturing exclusive ownership;
- no new Amux runner is pinned or maintenance schedule installed; local Amp runner processes move to explicit native runner IDs and owner-selected operating-system supervision;
- no new group, callback lease, deadline, finish authorization, or general report is created; and
- no replacement resource may be created to make an old indeterminate operation appear recoverable.

The only retained new-work exception is an explicit owner-authorized projectless physical-host route where native project-backed Orb/runner creation cannot express the requested placement. It is not a generalized spawn fallback: it must bind one exact physical host and workdir before launch, create no group or unrelated lifecycle state, and remain fail-closed on any identity, placement, ownership, launch, or delivery ambiguity. An indeterminate attempt is preserved as indeterminate and is never retried, adopted, rebound, or rerouted through another host, Orb, runner, or transport. Remove this exception when native authenticated creation can represent the same projectless physical-host placement.

Cutover is per operation family, not one global flag. The implementation and release notes must publish the exact cutover generation for each family. A client that cannot prove whether the target record predates that generation fails closed.

`contract-v1.md` and all Amux lifecycle instructions are drain-only compatibility material. They may be supplied only to an exact existing Amux-managed spawn, adoption, or group flow whose persisted provenance proves both pre-cutover admission and an allowed next drain transition. Native creation, the post-cutover projectless-host exception, ambiguous records, and unrecorded work do not qualify.

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

The target response contains only `status`, `summary`, `findings`, `blockers`, and `verification`, with reviewed bounds. Returning that authenticated structured response directly to the invoking caller on the same turn establishes delivery; the replacement uses no Amux receipt, consume, acknowledge step, or side inbox. Once this single field gate is accepted, new Tycho receipt creation stops at a published generation. Existing receipts drain only through their current exact lifecycle, `created → valid_report → delivered → acknowledged|abandoned`. `acknowledged` and `abandoned` are terminal receipt states; notification uncertainty and terminal capability-cleanup status remain separate metadata and never become receipt states. The old and new routes never run for the same task. After the gate, the durable skill home for the binding is personal User Skills `amp-tycho` as specified in Accepted refinements above; `/amux-tycho` remains the drain path for pre-gate receipts until freeze.

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

Only after old absence is proven may the owner start a native runner with a stable `--runner-id` under the selected OS service. Starting that runner is a native/OS operation, not an Amux write. Issue #232 is **closed** as the classification defect: PR #366 shipped a **preflight-only** fail-closed classifier covering configured-live, configured-absent, row-absent-live, conflicting, interrupted-stop, and exact-replay cases. #366 does **not** wire drain commands, stop processes, mutate or remove runner rows, or close runner admission. **Classifier completion is not operational drain completion.** Runner admission closes only after owner-operated drain execution against that evidence and native/OS replacement supervision after proven absence. Do not treat the classifier landing as “runner gate complete” or “admission closed.”

## Git and worktree safety

PR #363 is retained transition safety. Ordinary finish of one exact present attached worktree may take the narrow keep-branch path: exact completion and worker/worktree identity, a strictly index-only filesystem, sole ownership, complete process evidence, lock classification, one concise approval, parking the exact live worker, normal non-force removal, and adjacent revalidation. Any ignored or other non-index object blocks this fast path because normal removal can delete it; preserve or remove that content independently, then start a fresh preflight. Keeping the attached branch preserves unique or unpushed commits; repository stashes are untouched, so baseline/patch classification, backup refs, stash attribution, and branch deletion add no safety to that disposition. Detached, missing, backup-requiring, remove-on-missing, and repository-wide prune cases retain the full ref-coverage, backup, stash, purpose, ownership, and adjacent-revalidation procedure. Branch deletion always remains separate and opt-in.

The removal-safety guidance must not grow into a retirement ledger, provider reconciler, or durable worktree owner. A backup ref preserves Git objects; it does not authorize worktree removal, branch deletion, provider cleanup, or Amp lifecycle mutation. At Amux product archive, default disposition is docs-only retain of this guidance in historical documentation; extract a separately named skill only on a later explicit owner decision if removal without Amux remains routine.

## Rollout sequence

1. **Adopt this direction:** publish the superseding ADR and active disposition ledger. Make no runtime claim from documentation alone.
2. **Run and remove the bounded inventory:** merged PR #360 provides one strictly read-only migration inventory and completed #351. The sunset is **conditional**: no helper/route deletion is authorized merely because a future release is contemplated. Deletion activates only after the owner accepts one complete inventory **or** explicitly dispositions it incomplete/error **and** confirms no repeat is needed; **once activated**, delete the helper, sweep-only tests, and every `/amux sweep` route/reference **before the next release**. Do not wait for store freeze or reuse it as a recurring or final reconciler.
3. **Close the runner safety gap:** PR #366 shipped the #232 **preflight-only** classifier (evidence for the six migration cases). It does not perform drain mutations or close admission. Before closing runner admission, complete owner-operated drain execution against that classifier and hand stable native `--runner-id` starts to OS supervision after proven absence.
4. **Validate direct Tycho return:** retain the current bridge until the field gate above passes.
5. **Close admission per family:** native Amp becomes the sole owner for new work; publish each cutover generation and reject new Amux resources.
6. **Drain:** allow only the transitions defined above. Never dual-write or manufacture terminal truth.
7. **Export and freeze:** freeze one store at a time after its inventory gate passes.
8. **Remove writers:** remove mutating commands, skill routes, schedulers, callbacks, and receipt helpers after their stores freeze.
9. **Read-only window:** ship the compatibility reader and migration diagnostics for the declared window.
10. **Archive:** remove schema readers only after the release gate, retain historical documentation and exports, and archive the orchestration product.

## Consequences

The staged drain is slower than deleting the CLI immediately, but it avoids stranding the exact recovery evidence Amux was built to preserve. It is materially smaller than implementing the symmetric-retirement roadmap: no new retirement stream, attachment generation, six-class planner, provider assertion framework, finalizer, or second database is introduced.

Native Amp becomes the ordinary execution and coordination layer. Amux temporarily remains capable of finishing its own already-recorded operations, then becomes a reader, then ends. The minimal Tycho structured-response contract survives as personal User Skills `amp-tycho` after the direct-return gate without retaining the Amux lifecycle model. Worktree safety guidance defaults to docs-only retain at product archive unless later extracted as a separately named skill. Deferred remote visibility after shelf drain is native archive/unarchive, not an Amux shelf product and not an unproven Hide/Unhide API.

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
