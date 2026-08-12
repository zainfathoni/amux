---
status: proposed-actions
decision: adr-0007
as-of: 2026-08-12
---

# Amux staged-drain disposition ledger

This ledger translates [ADR 0007](adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) into concrete recommendations. It does not close an issue, merge or close a pull request, archive a thread, publish a release, mutate runtime state, or authorize destructive cleanup.

## Pull requests

| PR | Exact recommendation | Completion or sunset condition |
| --- | --- | --- |
| [#361](https://github.com/zainfathoni/amux/pull/361) | **Retain as merged transition safety.** Keep prepare/arm/finalize and `spawn-assignments.json` readable and drain-writable for operations created before spawn cutover. Admit no successor protocol work on top of it. | Remove its writer only after native creation is the sole new-work path and every assignment is terminal, explicitly retained, or preserved indeterminate. Keep cursor semantics truthful: acceptance is not execution. |
| [#363](https://github.com/zainfathoni/amux/pull/363) | **Retain as merged transition safety.** Keep backup-before-removal and its separation from removal authority. | The Git-only safety rule may survive Amux. Remove Amux-specific manifest coupling after migration inventory and worktree drain no longer consume it. |
| [#360](https://github.com/zainfathoni/amux/pull/360) | **Do not merge as a permanent sweep. Narrow to a bounded migration inventory, then re-review.** It may read Git worktrees, filesystem roots, frozen/current Amux stores, and external records only to produce the drain inventory. It must not become a recurring reconciler or cleanup authority. Current failing `scan` must be resolved on the narrowed head. | Ship only with an explicit sunset: stop invoking it when all source stores are frozen and the accepted final inventory has no unowned blocker; delete the helper in the first writer-removal release after that snapshot, while retaining the exported snapshot and generic worktree-safety reference. |
| [#362](https://github.com/zainfathoni/amux/pull/362) | **Do not merge; close as superseded after owner approval.** Preserve the branch/PR as design evidence. Do not rebase its append-only retirement stream into the drain architecture. | ADR 0007 intentionally avoids its new schema, command, locks, canonical commitments, and compatibility obligation. No code is salvaged unless independently required by the read-only exporter and re-reviewed without the retirement ledger. |
| [#353](https://github.com/zainfathoni/amux/pull/353) | **Supersede and replace, not merge.** Its practical-readiness history remains evidence, but its long-lived receipt-promotion framing conflicts with the direct-return gate. | Carry only verified field-history facts into Tycho replacement documentation. |
| [#341](https://github.com/zainfathoni/amux/pull/341) | **Close as already superseded.** | No code transfer to #362 or a replacement ledger. |
| [#322](https://github.com/zainfathoni/amux/pull/322) | **Close as superseded; retain one design fact.** Tycho logs/memory are not semantic delivery. | The direct structured-return contract replaces correspondence reading. |

## Issues

### Staged drain and cleanup

| Issue | Exact recommendation |
| --- | --- |
| [#313](https://github.com/zainfathoni/amux/issues/313) | Close as completed by the retirement audit plus this disposition ledger once accepted. Do not create a second inventory document. |
| [#331](https://github.com/zainfathoni/amux/issues/331) | Rewrite as the staged-drain umbrella, or close and create one narrowly named drain umbrella if issue history must remain immutable. It must track cutover generations, drain gates, store freezes, writer removal, compatibility release, and archive—never the symmetric-retirement slices. |
| [#332](https://github.com/zainfathoni/amux/issues/332) | Close as superseded; no retirement stream. |
| [#333](https://github.com/zainfathoni/amux/issues/333) | Close as superseded; no new creation references or legacy admission ledger. Existing state is inventoried and drained in place. |
| [#334](https://github.com/zainfathoni/amux/issues/334) | Close as superseded; no six-class prepare/provider-assertion engine. Salvage only read-only inventory facts required by the drain. |
| [#335](https://github.com/zainfathoni/amux/issues/335) | Close as superseded; no attachment-generation/final-departure state machine. Shared-use checks remain adjacent worktree safety. |
| [#336](https://github.com/zainfathoni/amux/issues/336) | Close as superseded; existing exact lifecycle commands may drain existing records, but no new independent finalizer is built. |
| [#337](https://github.com/zainfathoni/amux/issues/337) | Close after its preservation rules are confirmed present in the worktree-safety route; do not implement retirement records. |
| [#338](https://github.com/zainfathoni/amux/issues/338) | Close as superseded; no dirty-disposable/provider-recovery-loss ledger. Exact destructive authorization remains outside ordinary drain. |
| [#339](https://github.com/zainfathoni/amux/issues/339) | Rewrite as the rollout/documentation slice for ADR 0007: deprecation warnings, per-family cutover generations, export/freeze instructions, release gates, and compatibility sunset. |
| [#344](https://github.com/zainfathoni/amux/issues/344) | Narrow to completing the reusable Git/worktree safety extraction and the one-time migration inventory. Do not add a permanent Amux/Tycho reconciler. Close when #360's bounded snapshot, #363 backup behavior, and the safety reference cover the accepted drain. |
| [#351](https://github.com/zainfathoni/amux/issues/351) | Narrow to the one-time inventory required by ADR 0007 and PR #360. Its acceptance criteria must include the explicit helper deletion condition. |
| [#352](https://github.com/zainfathoni/amux/issues/352) | Move any continuing drift diagnostic to Tycho ownership and keep it report-only. For Amux, include external rows only in the bounded migration snapshot, then close. |

### Correctness required before cutover

| Issue | Exact recommendation |
| --- | --- |
| [#232](https://github.com/zainfathoni/amux/issues/232) | **Keep open and reprioritize as a runner-drain blocker.** Implement the ADR 0007 join and fail-closed removal preflight. Retain rows on live/conflicting/unproven evidence; stop only exact proven processes; verify absence before row removal; handle already orphaned row-absent processes without inferred adoption; then hand stable native runner IDs to OS supervision. Close when all six migration cases in ADR 0007 pass. |
| [#238](https://github.com/zainfathoni/amux/issues/238) | Narrow to preservation/removal guidance for an existing review-only worker during drain. Do not add a new terminal report state or permanent finish route. |
| [#249](https://github.com/zainfathoni/amux/issues/249) | Close as superseded by the smaller drain and existing adjacent worktree gates. Do not add a durable finish-plan generation. |
| [#318](https://github.com/zainfathoni/amux/issues/318) | Narrow to selectors retained by the compatibility reader and drain commands. Do not normalize command families scheduled for removal merely for completeness. |

### Tycho and provider paths

| Issue | Exact recommendation |
| --- | --- |
| [#328](https://github.com/zainfathoni/amux/issues/328) | Rewrite around one exact direct structured `complete|blocked` return, independent Amp verification, and Amp-only shared mutation. Until its field gate passes, use the current `/amux-tycho` bridge unchanged; never run both routes for one task. |
| [#356](https://github.com/zainfathoni/amux/issues/356) | Do not port the receipt helper to Bun. Keep Python only for compatibility fixes necessary to drain safely; close after direct return is validated and receipt admission closes. |
| [#206](https://github.com/zainfathoni/amux/issues/206) | Close as out of Amux scope; any continuing Pi/Spark validation belongs to Tycho or a separately owned provider skill. |
| [#221](https://github.com/zainfathoni/amux/issues/221) | Narrow to drain of already-bound pairs only; admit no new paired shelving protocol. Close when those pairs are terminal or retained. |
| [#236](https://github.com/zainfathoni/amux/issues/236) | Close as superseded; do not add provider supersession/successor receipt state. Existing acquired/no-report state remains preserved or owner-dispositioned. |
| [#254](https://github.com/zainfathoni/amux/issues/254), [#255](https://github.com/zainfathoni/amux/issues/255) | Close as out of Amux scope. Native Orbs and Tycho own any future provider execution. |
| [#311](https://github.com/zainfathoni/amux/issues/311) | Permit only a compatibility fix if digest drift prevents an existing created receipt from draining safely; otherwise close with the provider helper. |
| [#317](https://github.com/zainfathoni/amux/issues/317) | Close as out of Amux scope; exact model guidance belongs to the selected provider route. |

### Native overlap and obsolete expansion

| Issue | Exact recommendation |
| --- | --- |
| [#174](https://github.com/zainfathoni/amux/issues/174), [#176](https://github.com/zainfathoni/amux/issues/176) | Close; do not promote an Amux invocation-policy layer over native Amp modes, tools, and executor selection. |
| [#212](https://github.com/zainfathoni/amux/issues/212)–[#216](https://github.com/zainfathoni/amux/issues/216) | Close the Amux runner-ID productization graph. Use native stable `--runner-id` only when starting replacement runners after old absence is proven. |
| [#276](https://github.com/zainfathoni/amux/issues/276) | Close; do not expand callback persistence while callback admission is closing. Existing reports remain recoverable from durable report history. |
| [#307](https://github.com/zainfathoni/amux/issues/307) | Close as superseded by merged #361 and the spawn drain. Preserve historical `retained_indeterminate` truth. |
| [#314](https://github.com/zainfathoni/amux/issues/314) | Close; no telemetry/scorecard program is required to execute an owner-approved retirement. Historical evidence remains documentation. |

## Commands and stores by cutover family

| Family | Admission close | Drain-only writers | Freeze gate |
| --- | --- | --- | --- |
| Worker/spawn | Reject new `pin`, `adopt`, `spawn`, launch configuration, and group attachment after the worker cutover generation. | Exact finalize without resend; park/remove/teardown existing workers; complete already-recorded shelf transitions; exact reconcile removal where current evidence proves it. | Every `workers.tsv`, `shelves.tsv`, `operations.json`, and `spawn-assignments.json` record is terminal, retained with owner/next action, or immutable indeterminate. |
| Runner/maintenance | Reject new runner pin/launch configuration and maintenance install after #232 migration support lands. | Park/remove exact configured runners; uninstall existing scheduler state; reconcile only proven absence. | Joined inventory proves no unmanaged catalog-live process and all runner/maintenance records are terminal or retained. |
| Group/report/callback/deadline | Reject new groups, reports, callbacks, deadlines, and finish authorizations after their cutover generation. | Submit/acknowledge/authorize/finish only already-bound report workflows; clear existing callback leases; no new task membership. | No pending/open obligation lacks an owner; histories exported; callback transport no longer required by a live report. |
| Tycho bridge | Remains admission-open only until direct structured return passes ADR 0007's field gate. Then close receipt creation atomically with enabling the direct route. | Existing submit/consume/acknowledge/abandon and cleanup replay only. | Every receipt is terminal, explicitly retained, or preserved indeterminate; owner-private stores are exported before helper removal. |
| Git/worktree safety | No Amux resource admission. | Owner-authorized backup refs and non-force removal safety remain independent operations. | Not tied to Amux archive; generic safety guidance may remain permanently. |

## Thread dispositions

| Thread | Role and recommendation |
| --- | --- |
| [Direction thread](https://ampcode.com/threads/T-019fe3d2-4119-75ad-bf74-90ea8f0c7033) | Owner/coordinator record for the retirement decision. It owns acceptance or revision of ADR 0007 and any later shared-action authorization. Keep active until the ADR/disposition branch is reviewed; it gains no merge, issue-mutation, or runtime-cleanup authority implicitly. |
| [Independent Grok review](https://ampcode.com/threads/T-019ff6fa-17a7-7660-8776-c25234c923fc) | Evidence-only challenge that established the drain-only-write correction. No continuing implementation or decision authority. Archive only on an explicit owner request after its findings are linked from the direction thread. |
| [ADR implementation thread](https://ampcode.com/threads/T-019ff6f0-862e-753b-887d-82b5ef8b9425) | Owns only this reviewable documentation branch and verification. After handoff it should perform no issue/PR mutation, runtime drain, push, merge, or unrelated repository work without a new explicit request. |

New implementation threads should each own one disjoint cutover family or read-only migration artifact. Their prompts must name the cutover generation, allowed preexisting records, forbidden new resources, freeze gate, validation, and explicit non-goals. No child thread may independently advance another family's store or perform shared GitHub actions.

## Owner decisions still required

1. Approve ADR 0007 and whether #331 is rewritten in place or replaced by a new drain umbrella.
2. Choose the release/date expression for each operation-family cutover generation.
3. Approve a narrowed #360 head and its exact deletion release; current #360 is not merge-ready while `scan` fails.
4. Approve the #232 row-absent exact-evidence threshold and the OS supervision mechanism for replacement native runners.
5. Select the Tycho direct structured-return owner/API and accept one field cycle before bridge admission closes.
6. Choose the read-only compatibility duration and minimum retained reader release.
7. Decide whether generic worktree safety remains in this repository after the Amux binary is archived or moves to a separately named skill.
8. Separately authorize every push, merge, issue/PR close or rewrite, process stop, archive, receipt abandonment, worktree removal, and state-file deletion.
