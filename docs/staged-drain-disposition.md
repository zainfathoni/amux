---
status: executed-hygiene-2026-08-30
decision: adr-0007
as-of: 2026-08-30
---

# Amux staged-drain disposition ledger

This ledger translates [ADR 0007](adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) into concrete recommendations and records the **2026-08-30 hygiene pass** that closed superseded issues/PRs and rewrote the live drain umbrellas. It does not by itself authorize runtime drain, skill publication, push/merge of product code, or destructive cleanup.

## Pull requests

| PR | Exact recommendation | Status |
| --- | --- | --- |
| [#361](https://github.com/zainfathoni/amux/pull/361) | **Retain as merged transition safety.** Keep prepare/arm/finalize and `spawn-assignments.json` readable and drain-writable for pre-cutover operations. Admit no successor protocol. | Merged — retain |
| [#363](https://github.com/zainfathoni/amux/pull/363) | **Retain as merged transition safety.** Backup-before-removal stays separate from removal authority. | Merged — retain |
| [#360](https://github.com/zainfathoni/amux/pull/360) | **Retain as merged one-time transition inventory.** Owner-authorized run at most once; then delete helper before next release. | Merged — sunset still owner-gated |
| [#366](https://github.com/zainfathoni/amux/pull/366) | **Retain as merged runner-drain classifier.** Fail-closed evidence for the six ADR 0007 migration cases. Closed #232. | Merged — complete |
| [#373](https://github.com/zainfathoni/amux/pull/373) | **Retain as merged finish wind-down.** One read-only preflight + approval + adjacent revalidation; review-only completed workers share the same path. Closed #249 and #238. | Merged — complete |
| [#371](https://github.com/zainfathoni/amux/pull/371) | Docs closeout for #344/#352 Amux scope. | Merged — complete |
| [#362](https://github.com/zainfathoni/amux/pull/362) | **Do not merge; close as superseded.** No append-only retirement stream. | Closed superseded |
| [#353](https://github.com/zainfathoni/amux/pull/353) | **Do not merge; close as superseded.** Long-lived receipt-promotion framing conflicts with direct-return gate. | Closed superseded |
| [#341](https://github.com/zainfathoni/amux/pull/341) | **Close as already superseded.** No code transfer to a retirement ledger. | Closed superseded |
| [#322](https://github.com/zainfathoni/amux/pull/322) | **Close as superseded.** Tycho logs/memory are not semantic delivery; direct structured return replaces correspondence reading. | Closed superseded |

## Issues

### Staged drain and cleanup

| Issue | Status / recommendation |
| --- | --- |
| [#313](https://github.com/zainfathoni/amux/issues/313) | **Closed** — inventory completed by ADR 0007 + this ledger. No second inventory document. |
| [#331](https://github.com/zainfathoni/amux/issues/331) | **Rewritten in place** as the staged-drain umbrella. Tracks cutover generations, drain gates, store freezes, writer removal, compatibility release, and archive — **never** symmetric-retirement slices. |
| [#332](https://github.com/zainfathoni/amux/issues/332)–[#338](https://github.com/zainfathoni/amux/issues/338) | **Closed superseded** — no retirement stream, six-class engine, attachment-generation machine, independent finalizer, dirty-disposable ledger, or retirement records. Preservation rules live in `removal-safety.md` / finish (#337). |
| [#339](https://github.com/zainfathoni/amux/issues/339) | **Rewritten in place** as ADR 0007 rollout/documentation: deprecation warnings, per-family cutover generations, export/freeze instructions, release gates, compatibility sunset. |
| [#344](https://github.com/zainfathoni/amux/issues/344) | **Closed** — Amux removal-safety epic complete (#345–#351). |
| [#351](https://github.com/zainfathoni/amux/issues/351) | **Closed** by merged #360. Sweep sunset remains owner-gated and independent. |
| [#352](https://github.com/zainfathoni/amux/issues/352) | **Closed** for Amux scope; continuing `hq.yml` drift → Tycho ownership, report-only. |

### Correctness / finish (shipped)

| Issue | Status / recommendation |
| --- | --- |
| [#232](https://github.com/zainfathoni/amux/issues/232) | **Closed** by merged [#366](https://github.com/zainfathoni/amux/pull/366). Classifier covers the six migration cases. Remaining owner choice: OS supervision mechanism when starting **native** replacement runners after proven absence — not an Amux product feature. |
| [#238](https://github.com/zainfathoni/amux/issues/238) | **Closed** by merged [#373](https://github.com/zainfathoni/amux/pull/373). Completed review-only workers use the same finish wind-down; no new terminal report state. |
| [#249](https://github.com/zainfathoni/amux/issues/249) | **Closed** by merged [#373](https://github.com/zainfathoni/amux/pull/373). One read-only preflight + summary + approval + adjacent revalidation; no durable finish planner. |
| [#318](https://github.com/zainfathoni/amux/issues/318) | **Open, narrowed** — only selectors retained by the compatibility reader and drain commands. Do not normalize families scheduled for removal. |

### Tycho and provider paths

| Issue | Status / recommendation |
| --- | --- |
| [#328](https://github.com/zainfathoni/amux/issues/328) | **Open, rewritten** around one exact [direct structured `complete\|blocked` return](#owner-decisions-accepted-2026-08-13). Until the field gate passes, keep `/amux-tycho` unchanged; never dual-route. |
| [#356](https://github.com/zainfathoni/amux/issues/356) | **Closed** — do not port the receipt helper to Bun. Python only for drain compatibility. |
| [#206](https://github.com/zainfathoni/amux/issues/206) | **Closed** — out of Amux scope; Pi/Spark validation belongs to Tycho or a separate provider skill. |
| [#221](https://github.com/zainfathoni/amux/issues/221) | **Open, narrowed** — drain already-bound Claude pairs only; admit no new paired-shelving protocol. Close when those pairs are terminal or retained. |
| [#236](https://github.com/zainfathoni/amux/issues/236) | **Closed superseded** — no provider supersession/successor receipt state. |
| [#254](https://github.com/zainfathoni/amux/issues/254), [#255](https://github.com/zainfathoni/amux/issues/255) | **Closed** — out of Amux scope. Native Orbs and Tycho own future provider execution. |
| [#311](https://github.com/zainfathoni/amux/issues/311) | **Open, narrowed** — compatibility fix only if digest drift blocks draining an existing created receipt; otherwise close with the provider helper. |
| [#317](https://github.com/zainfathoni/amux/issues/317) | **Closed** — out of Amux scope; model guidance belongs to the selected provider route. |

### Native overlap and obsolete expansion

| Issue | Status / recommendation |
| --- | --- |
| [#174](https://github.com/zainfathoni/amux/issues/174), [#176](https://github.com/zainfathoni/amux/issues/176) | **Closed** — do not promote an Amux invocation-policy layer over native Amp. |
| [#212](https://github.com/zainfathoni/amux/issues/212)–[#216](https://github.com/zainfathoni/amux/issues/216) | **Closed** — Amux runner-ID productization retired (`b0e23c2`). Use native `--runner-id` only when starting replacement runners after old absence is proven. |
| [#276](https://github.com/zainfathoni/amux/issues/276) | **Closed** — do not expand callback persistence while callback admission is closing. |
| [#307](https://github.com/zainfathoni/amux/issues/307) | **Closed superseded** by merged #361 and spawn drain. Preserve historical `retained_indeterminate` truth. |
| [#314](https://github.com/zainfathoni/amux/issues/314) | **Closed** — no telemetry/scorecard program required for owner-approved drain. |
| [#374](https://github.com/zainfathoni/amux/issues/374) | **Open, parked** — optional long-running runner supervision is not a drain blocker. Prefer OS supervision of native runners; do not grow Amux into a service manager. |

## Live open set after hygiene (2026-08-30)

| Issue | Role |
| --- | --- |
| [#331](https://github.com/zainfathoni/amux/issues/331) | Staged-drain umbrella (rewritten) |
| [#339](https://github.com/zainfathoni/amux/issues/339) | ADR 0007 rollout / docs slice (rewritten) |
| [#328](https://github.com/zainfathoni/amux/issues/328) | Direct structured Tycho return field gate (rewritten; explicit-only) |
| [#221](https://github.com/zainfathoni/amux/issues/221) | Drain already-bound Claude pairs only |
| [#318](https://github.com/zainfathoni/amux/issues/318) | Thread selector boundary on retained drain commands |
| [#311](https://github.com/zainfathoni/amux/issues/311) | Digest-drift fix only if it blocks receipt drain |
| [#374](https://github.com/zainfathoni/amux/issues/374) | Parked optional supervision idea |

## Commands and stores by cutover family

| Family | Admission close | Drain-only writers | Freeze gate |
| --- | --- | --- | --- |
| Worker/spawn | Native authenticated Amp `create_thread` is the default. Reject generalized new `pin`, `adopt`, `spawn`, launch configuration, and group attachment after the worker cutover generation. Never automatically adopt native-created threads. Sole new-work exception: explicit projectless physical-host route bound to one exact host/workdir. | Exact finalize of pre-cutover spawn state; park/remove/teardown existing workers; complete recorded shelf transitions; exact reconcile where evidence proves it. | Every pre-cutover `workers.tsv`, `shelves.tsv`, `operations.json`, and `spawn-assignments.json` record is terminal, retained, or immutable indeterminate; native creation has replaced the projectless exception before that final writer is removed. |
| Runner/maintenance | Reject new runner pin/launch configuration and maintenance install after #232/#366 classifier lands (done). | Park/remove exact configured runners; uninstall existing scheduler state; reconcile only proven absence. | Joined inventory proves no unmanaged catalog-live process and all runner/maintenance records are terminal or retained. |
| Group/report/callback/deadline | Reject new groups, reports, callbacks, deadlines, and finish authorizations after their cutover generation. | Submit/acknowledge/authorize/finish only already-bound report workflows; clear existing callback leases. | No pending/open obligation lacks an owner; histories exported; callback transport no longer required by a live report. |
| Tycho bridge | Admission-open only until ADR 0007's authenticated direct structured-return gate passes (#328). Then close receipt creation atomically with enabling the direct route. Long-term skill home: personal User Skills `amp-tycho`. | Existing receipts only through current submit/consume/acknowledge/abandon. Keep helper paths stable until freeze. | Every receipt is terminal, retained, or preserved indeterminate; owner-private stores exported before helper removal. |
| Git/worktree safety | No Amux resource admission. | Owner-authorized backup refs and non-force removal safety remain independent. | Not tied to Amux archive; default at product archive is docs-only retain of safety guidance. |

## Thread dispositions

| Thread | Role and recommendation |
| --- | --- |
| [Direction thread](https://ampcode.com/threads/T-019fe3d2-4119-75ad-bf74-90ea8f0c7033) | Owner/coordinator record for the retirement decision. |
| [Independent Grok review](https://ampcode.com/threads/T-019ff6fa-17a7-7660-8776-c25234c923fc) | Evidence-only; no continuing implementation authority. |
| [ADR implementation thread](https://ampcode.com/threads/T-019ff6f0-862e-753b-887d-82b5ef8b9425) | Documentation branch ownership only after handoff. |
| [Shelf/Tycho destination grill](https://ampcode.com/threads/T-019ff865-243d-774d-bf74-87551642ad37) | Evidence-only; established Archive/Unarchive mapping and Q1–Q4. |

## Owner decisions accepted (2026-08-13)

| ID | Decision |
| --- | --- |
| **Visibility (Q1)** | After shelf admission closes, remote half of former Amux shelve is **native Archive/Unarchive** only. No Hide/Unhide product language. `shelves.tsv` remains drain-only migration state. |
| **Direct return shape (Q2)** | Post-gate Tycho path returns one schema-valid bounded `complete\|blocked` result as an **Amp-visible authenticated structured response on the same invoking turn**. No side inbox; no Amux receipt/consume/ack on the new route. Amp verifies findings and alone mutates shared state. Old and new routes never run for the same task. |
| **Tycho skill home (Q3)** | Long-term home: personal User Skills **`amp-tycho`**, published after or atomic with the direct-return field gate. Until then keep **`/amux-tycho`** stable. |
| **Worktree safety home (Q4)** | Default at Amux product archive: **docs-only retain** of Git/worktree removal-safety guidance. Extract a separately named skill only on a later explicit owner decision. |

**2026-08-30 hygiene decision:** rewrite #331 and #339 **in place** (do not create replacement issues). Close the symmetric-retirement slice graph and other disposition-listed supersessions.

## Owner decisions still required

1. ~~Whether #331 is rewritten in place or replaced~~ → **decided: rewritten in place (2026-08-30).**
2. Choose the release/date expression for each operation-family cutover generation.
3. Accept one #360 inventory or explicitly disposition it incomplete/error, confirm whether no repeat is required, and enforce deletion of its helper, sweep-only tests, and every `/amux sweep` route/reference before the next Amux release. Independent of closed #344/#352.
4. ~~#232 classifier~~ → **shipped (#366).** Remaining: choose the OS supervision mechanism when starting native replacement runners after proven absence (not an Amux runner-ID product).
5. Name the concrete Amp/Tycho API or surface for the accepted same-turn authenticated structured return, run one ordinary owner-authorized field cycle meeting all seven ADR 0007 gate bullets, and record acceptance before Tycho receipt admission closes (#328).
6. Choose the read-only compatibility duration and minimum retained reader release.
7. Separately authorize every push, merge, issue/PR close or rewrite, process stop, archive, receipt abandonment, worktree removal, state-file deletion, and skill publication (including personal User Skills `amp-tycho`).
