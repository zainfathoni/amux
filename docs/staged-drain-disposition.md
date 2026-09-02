---
status: active-thin-host-2026-08-30
decisions: adr-0007, adr-0008, adr-0009
as-of: 2026-08-30
---

# Amux thin-host and staged-drain disposition ledger

This ledger applies [ADR 0009](adr/0009-remove-active-legacy-coordination-surfaces.md) and [ADR 0008](adr/0008-retain-machine-local-runner-host-and-drain-coordination.md) to the still-valid boundaries in [ADR 0007](adr/0007-retire-amux-through-native-cutover-and-staged-drain.md). Amux is not being fully deprecated or archived. Native Amp owns new task coordination; Amux retains automatic machine-local runner launch and safety; core legacy coordination commands are removed and their stores are inert.

The 2026-08-30 PR #375/#376 hygiene remains valid: the issue graph stays narrow, #374 stays closed, #366 is preflight-only, #328's direct-return gate remains open, and legacy coordination admission does not reopen. The obsolete full-retirement, runner-admission closure, and native/OS replacement-launch conclusions are withdrawn.

**Release boundary:** v0.3.10 / `e7b0a67` shipped macOS-over-SSH lifecycle behavior. PR #375 / `3a38a93` and PR #376 / `a196659` are post-v0.3.10 documentation/issue hygiene. No release or runtime transition follows from this ledger.

## Destination by ownership layer

| Layer | Destination |
| --- | --- |
| New tasks | Native authenticated Amp creation, exact Orb/runner placement, parent/reply routing, messaging, waiting, and archive state. No generalized Amux spawn/adoption or automatic lifecycle enrollment. |
| Machine-local runners | Retained Amux workdir registry; pin/list/doctor/park/remove; minimum safe reconcile; `amux launch --all`; tmux/Amp process launch; install/update/maintenance diagnostics. |
| OS activation | systemd/launchd activates Amux and retains its process group. Verified patterns: systemd `Type=oneshot` + `RemainAfterExit=yes`; RunAtLoad LaunchAgent + `AbandonProcessGroup=true`. Direct OS supervision does not replace Amux launch. |
| Worker/coordination stores | Existing workers, shelves, groups, reports, callbacks, deadlines, finish authorization, operations, and spawn assignments are inert evidence. Active commands do not enroll, migrate, drain, rewrite, or delete them. |
| Provider bridges | Drain only after each existing replacement gate is proven. `/amux-tycho` remains unchanged until #328 passes; never dual-route. |
| Git/worktree safety | Independent safety guidance; not a reason to retain coordination state and not contingent on full-product archive. |

## Pull-request dispositions retained

| PR | Disposition |
| --- | --- |
| [#360](https://github.com/zainfathoni/amux/pull/360) | Merged one-time read-only inventory. This direction does **not** run it; a future invocation needs a separate explicit owner request. After that separately authorized run, sunset activates only when the owner accepts one complete inventory or explicitly dispositions an incomplete/error result **and** confirms no repeat is needed; only then delete before the next release. |
| [#361](https://github.com/zainfathoni/amux/pull/361) | Historical spawn safety evidence retained in history; the active prepare/arm/finalize command route is removed by ADR 0009. |
| [#363](https://github.com/zainfathoni/amux/pull/363) | Retain backup-before-removal safety, separate from removal authority. |
| [#366](https://github.com/zainfathoni/amux/pull/366) | Retain as preflight-only evidence for runner operations. It performs no stop, row mutation/removal, admission closure, or migration away from Amux. |
| [#373](https://github.com/zainfathoni/amux/pull/373) | Historical finish safety evidence retained in history; the active worker finish/teardown route is removed by ADR 0009. |

The closed symmetric-retirement graph and superseded provider/productization PRs remain closed. Nothing in ADR 0008 revives an append-only retirement stream, six-class planner, finalizer, provider-neutral control plane, or permanent Lead hierarchy.

## Live issue disposition

The narrow six-issue open set remains #221, #311, #318, #328, #331, and #339.

| Issue | Current role |
| --- | --- |
| [#331](https://github.com/zainfathoni/amux/issues/331) | Keep open as the umbrella for the retained runner-launch core, removed active coordination surfaces, and preservation/replacement gates for inert historical state. |
| [#339](https://github.com/zainfathoni/amux/issues/339) | Keep open as rollout/documentation alignment for the retained host boundary and preservation/replacement gates. No replacement dates yet. |
| [#328](https://github.com/zainfathoni/amux/issues/328) | Keep narrowly open for the authenticated same-turn structured-return gate. Until it passes, keep `/amux-tycho` unchanged and never dual-route. |
| [#221](https://github.com/zainfathoni/amux/issues/221) | Preserve already-bound Claude pairs and track a separately proven replacement/disposition. No currently authorized drain mutation or new paired protocol. |
| [#311](https://github.com/zainfathoni/amux/issues/311) | Historical compatibility evidence only; do not mutate receipts for digest drift without a separately proven replacement and owner disposition. |
| [#318](https://github.com/zainfathoni/amux/issues/318) | Reassess against removed core compatibility commands; provider-specific selectors remain under their own skills. No issue mutation is authorized by this ledger. |

Closed #232 remains safety evidence implemented by preflight-only PR #366. Closed #374 remains closed; its long-running-supervisor idea is still not required, and its obsolete rationale that OS/native launch replaces Amux received the [authorized non-reopening correction recorded here](issue-direction-corrections.md#applied-corrective-comment-on-closed-374-not-reopened). Closed #212–#216 remain closed because ADR 0008 retains workdir identity and does not revive their separate Amux runner-ID product graph.

## Withdrawn dates and gates

The proposed 2026-09-01 cutover and 2026-11-30 reader-window date were never landed and are withdrawn. No replacement date is selected.

There is no runner-admission closure gate and no complete-product archive gate. Any future date applies only to a named legacy coordination/provider family and requires a separate owner decision. The retained runner registry, launch, maintenance, and diagnostics are not governed by a legacy-store reader window.

## Owner decisions still required

1. For #360, accept one complete inventory or explicitly disposition incomplete/error and separately confirm no repeat before the existing deletion condition activates.
2. Decide implementation scope and release sequencing for completing retained runner remove/reconcile behavior using #232/#366 safety evidence.
3. Name and field-validate #328's concrete authenticated same-turn Tycho return route before receipt admission closes.
4. Disposition provider-specific receipts whose final paired step formerly required core worker teardown; preserve evidence and fail closed meanwhile.
5. Authorize any further issue-body rewrite/comment, release, process/store mutation, or external deployment change separately.

The exact #331/#339 rewrites and closed-#374 corrective comment applied on 2026-08-30 are recorded in [the issue direction corrections](issue-direction-corrections.md).
