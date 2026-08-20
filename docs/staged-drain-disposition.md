---
status: proposed-actions
decision: adr-0007
as-of: 2026-08-20
---

# Amux staged-drain disposition ledger

This ledger translates [ADR 0007](adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) into concrete recommendations. It does not close an issue, merge or close a pull request, archive a thread, publish a release, mutate runtime state, publish a skill, or authorize destructive cleanup.

## Pull requests

| PR | Exact recommendation | Completion or sunset condition |
| --- | --- | --- |
| [#361](https://github.com/zainfathoni/amux/pull/361) | **Retain as merged transition safety.** Keep prepare/arm/finalize and `spawn-assignments.json` readable and drain-writable for operations created before spawn cutover. Admit no successor protocol work on top of it. The separately bounded projectless physical-host exception may use only the minimum exact-host/workdir assignment path; it does not reopen generalized spawn admission. | Remove the general writer after native authenticated creation owns ordinary new work and every pre-cutover assignment is terminal, explicitly retained, or preserved indeterminate. Keep cursor semantics truthful: acceptance is not execution. Retain the exception only until native creation supports that exact projectless placement. |
| [#363](https://github.com/zainfathoni/amux/pull/363) | **Retain as merged transition safety.** Keep backup-before-removal and its separation from removal authority. | The Git-only safety rule may survive Amux. Remove Amux-specific manifest coupling after migration inventory and worktree drain no longer consume it. |
| [#360](https://github.com/zainfathoni/amux/pull/360) | **Retain as merged one-time transition inventory.** It is a strictly read-only `/amux sweep` skill workflow, not a Go CLI command, reconciler, or cleanup authority. Run it at most once with owner authorization and record either acceptance or an explicit incomplete/error disposition. | After that disposition and owner confirmation that no repeat is required, delete `scripts/sweep-inventory`, its sweep-only tests, and every `/amux sweep` route/reference before the next Amux release. Do not wait for all-store freeze or repurpose it as a recurring or final reconciler. |
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
| [#344](https://github.com/zainfathoni/amux/issues/344) | **Closed (Amux scope complete).** Increments shipped: #345/#357 teardown honesty, #346/#354 worker doctor workdir, #347/#355 classifier reference, #348/#358 finish removal gate, #349/#363 backup refs, #350/#359 stale-record reconcile, #351/#360 one-time sweep. No residual reusable Git/worktree gap beyond retained `removal-safety.md` + finish/backup paths. Do not add a permanent Amux/Tycho reconciler, extend `/amux sweep`, or hold sweep sunset on this issue. Default at Amux archive: docs-only retain of removal-safety guidance (ADR 0007 Q4). |
| [#351](https://github.com/zainfathoni/amux/issues/351) | **Completed by merged #360.** Preserve its one-time inventory result and exact sunset: after one accepted or explicitly dispositioned incomplete/error inventory and owner confirmation of no repeat, delete the helper, sweep-only tests, and all `/amux sweep` routes/references before the next Amux release. |
| [#352](https://github.com/zainfathoni/amux/issues/352) | **Closed for Amux scope.** Continuing `hq.yml` / purpose / phantom-key drift diagnostics belong to Tycho ownership and stay report-only. Amux does not join or mutate external project registries: teardown already emits `external_project_records=not_owned`; the one-time #360 sweep joins only Git worktrees, `workers.tsv`, validated reports, and explicit filesystem roots—never `hq.yml`. Do not extend `/amux sweep` into standing Tycho monitoring. |

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
| [#328](https://github.com/zainfathoni/amux/issues/328) | Rewrite around one exact [direct structured `complete|blocked` return](#owner-decisions-accepted-2026-08-13) (same-turn Amp-visible authenticated delivery), independent Amp verification, and Amp-only shared mutation. Until its field gate passes, use the current `/amux-tycho` bridge unchanged; never run both routes for one task. |
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
| Worker/spawn | Native authenticated Amp `create_thread` on the exact intended Orb/runner/workdir is the default, with a lean task prompt and native parent/reply routing only. Reject generalized new `pin`, `adopt`, `spawn`, launch configuration, and group attachment after the worker cutover generation. Never automatically adopt native-created threads; same-directory ownership conflicts remain unmanaged by Amux. The sole new-work exception is the explicit projectless physical-host route bound to one exact host/workdir, with no retry or fallback and indeterminate outcomes preserved. | Exact finalize of pre-cutover spawn state without resend; park/remove/teardown existing workers; complete already-recorded shelf transitions; exact reconcile removal where current evidence proves it. Contract and lifecycle instructions apply only when persisted provenance proves an exact pre-cutover flow is drain-eligible. The narrow projectless exception creates no group or unrelated lifecycle state. | Every pre-cutover `workers.tsv`, `shelves.tsv`, `operations.json`, and `spawn-assignments.json` record is terminal, retained with owner/next action, or immutable indeterminate; native creation has replaced the projectless exception before that final writer is removed. |
| Runner/maintenance | Reject new runner pin/launch configuration and maintenance install after #232 migration support lands. | Park/remove exact configured runners; uninstall existing scheduler state; reconcile only proven absence. | Joined inventory proves no unmanaged catalog-live process and all runner/maintenance records are terminal or retained. |
| Group/report/callback/deadline | Reject new groups, reports, callbacks, deadlines, and finish authorizations after their cutover generation. | Submit/acknowledge/authorize/finish only already-bound report workflows; clear existing callback leases; no new task membership. | No pending/open obligation lacks an owner; histories exported; callback transport no longer required by a live report. |
| Tycho bridge | Remains admission-open only until ADR 0007's authenticated direct structured-return gate passes. Then close receipt creation atomically with enabling the direct route; that route has no Amux consume/ack step. Long-term skill home after that gate: personal User Skills `amp-tycho` (see [Owner decisions accepted](#owner-decisions-accepted-2026-08-13)). | Existing receipts only: `created → valid_report → delivered → acknowledged|abandoned` through current submit/consume/acknowledge/abandon operations. Notification uncertainty and terminal cleanup status stay separate from receipt state. Keep `/amux-tycho` helper paths and original state/custody/abandonment directories stable until freeze. | Every receipt is terminal (`acknowledged|abandoned`), explicitly retained, or preserved indeterminate; owner-private stores are exported before helper removal. |
| Git/worktree safety | No Amux resource admission. | Owner-authorized backup refs and non-force removal safety remain independent operations. | Not tied to Amux archive; default at product archive is docs-only retain of safety guidance unless a later owner decision extracts a separately named skill. |

## Thread dispositions

| Thread | Role and recommendation |
| --- | --- |
| [Direction thread](https://ampcode.com/threads/T-019fe3d2-4119-75ad-bf74-90ea8f0c7033) | Owner/coordinator record for the retirement decision. It owns acceptance or revision of ADR 0007 and any later shared-action authorization. Keep active until the ADR/disposition branch is reviewed; it gains no merge, issue-mutation, or runtime-cleanup authority implicitly. |
| [Independent Grok review](https://ampcode.com/threads/T-019ff6fa-17a7-7660-8776-c25234c923fc) | Evidence-only challenge that established the drain-only-write correction. No continuing implementation or decision authority. Archive only on an explicit owner request after its findings are linked from the direction thread. |
| [ADR implementation thread](https://ampcode.com/threads/T-019ff6f0-862e-753b-887d-82b5ef8b9425) | Owns only this reviewable documentation branch and verification. After handoff it should perform no issue/PR mutation, runtime drain, push, merge, or unrelated repository work without a new explicit request. |
| [Shelf/Tycho destination grill](https://ampcode.com/threads/T-019ff865-243d-774d-bf74-87551642ad37) | Evidence-only architecture challenge of the owner proposal to retire Amux entirely, replace shelving with native visibility, and retain only the Tycho binding as personal User Skills `amp-tycho`. Established the Archive/Unarchive (not Hide/Unhide) mapping and the accepted refinements below. No implementation, merge, skill-publish, or runtime authority. |

New implementation threads should each own one disjoint cutover family or read-only migration artifact. Their prompts must name the cutover generation, allowed preexisting records, forbidden new resources, freeze gate, validation, and explicit non-goals. No child thread may independently advance another family's store or perform shared GitHub actions.

## Owner decisions accepted (2026-08-13)

Owner accepted the following refinements of ADR 0007 on 2026-08-13 (direction thread plus [shelf/Tycho destination grill](https://ampcode.com/threads/T-019ff865-243d-774d-bf74-87551642ad37)). They do not authorize code, skill publication, push/merge, issue mutation, or runtime/store changes by themselves.

| ID | Decision |
| --- | --- |
| **Visibility (Q1)** | After shelf admission closes, the remote half of former Amux shelve is **native Archive/Unarchive** only (`amp threads archive` / `amp threads archive --unarchive`). Do **not** document or implement “Hide/Unhide replaces shelve” as product language. `find_thread` `hidden:` / `snoozed:` remain search/index filters only until a distinct hide mutation API is proven different from archive. Amux `shelves.tsv` intent remains **drain-only** migration state (launch gate + reconcile truth while Amux launch exists); there is no bulk migrate-to-hidden and no second Amux visibility store. |
| **Direct return shape (Q2)** | The post-gate Tycho path returns one schema-valid bounded complete or blocked result as an **Amp-visible authenticated structured response on the same invoking turn**, correlated to that Amp request/thread and the intended task/artifact identity, through one exact owner-selected Tycho route with no fallback. No side inbox and no Amux receipt/consume/ack on the new route. Payload fields remain only `status`, `summary`, `findings`, `blockers`, and `verification` (reviewed bounds). Provider exit, logs, pane text, memory, and unbound prose are never the response. Amp independently verifies findings and remains the only authority for GitHub or other shared mutation. Interrupt or no-response yields no finding and no automatic retry. Old and new routes never run for the same task. |
| **Tycho skill home (Q3)** | Long-term home is the owner's **personal User Skills** repository as skill name/directory **`amp-tycho`**. Publish that skill **after or atomic with** acceptance of the ADR 0007 direct-return field gate. Until then keep **`/amux-tycho`** and its helper paths stable for receipt create (while admission remains open) and for drain of existing receipts. A thin one-release `amux-tycho` → `amp-tycho` load-name shim is optional only if load-name pain is high; never run two helpers or two state machines. Pre-split and in-flight receipts keep original state, custody, and abandonment directories byte-stable at their original paths. |
| **Worktree safety home (Q4)** | **Default at Amux product archive: docs-only retain** of Git/worktree removal-safety guidance (and #363 lineage) in historical documentation. Extract to a **separately named** skill only on a later explicit owner decision if worktree removal without Amux remains routine. Do not keep a permanent Amux lifecycle product to host this safety surface. |

**Shelf drain note:** Existing shelf rows still classify and drain under ADR 0007 (intent × remote active|archived|missing × worker row × pane). No bulk archive/unarchive migration script. After worker admission is closed and Amux launch is gone, deferred work uses native archive (and optional owner-local stop); clearing leftover shelf intent is bookkeeping only when remote state is known.

**Tycho family note:** `#328` and related docs should describe the direct-return shape above. Concrete API/surface name and the one ordinary field-cycle acceptance remain open (see below). Receipt helper stays Python-only for drain; no Bun port.

## Owner decisions still required

1. Whether #331 is rewritten in place as the staged-drain umbrella or closed and replaced by one narrowly named drain umbrella (ADR 0007 itself is accepted).
2. Choose the release/date expression for each operation-family cutover generation.
3. Accept one #360 inventory or explicitly disposition it incomplete/error, confirm whether no repeat is required, and enforce deletion of its helper, sweep-only tests, and every `/amux sweep` route/reference before the next Amux release. **Independent of closed #344/#352** — do not reopen those epics to hold the sunset.
4. Approve the #232 row-absent exact-evidence threshold and the OS supervision mechanism for replacement native runners.
5. Name the concrete Amp/Tycho API or surface that will carry the accepted same-turn authenticated structured return, run one ordinary owner-authorized field cycle that meets all seven ADR 0007 gate bullets, and record acceptance before Tycho receipt admission closes.
6. Choose the read-only compatibility duration and minimum retained reader release.
7. Separately authorize every push, merge, issue/PR close or rewrite, process stop, archive, receipt abandonment, worktree removal, state-file deletion, and skill publication (including personal User Skills `amp-tycho`).
