---
status: corrected-thin-host-2026-08-30
decisions: adr-0007, adr-0008
as-of: 2026-08-30
---

# Amux thin-host and staged-drain disposition ledger

This ledger applies [ADR 0008](adr/0008-retain-machine-local-runner-host-and-drain-coordination.md) to the valid staged-drain boundaries in [ADR 0007](adr/0007-retire-amux-through-native-cutover-and-staged-drain.md). Amux is not being fully deprecated or archived. Native Amp owns new task coordination; Amux retains automatic machine-local runner launch and safety; legacy coordination/provider stores are drain-only.

The 2026-08-30 PR #375/#376 hygiene remains valid: the issue graph stays narrow, #374 stays closed, #366 is preflight-only, #328's direct-return gate remains open, and legacy coordination admission does not reopen. The obsolete full-retirement, runner-admission closure, and native/OS replacement-launch conclusions are withdrawn.

**Release boundary:** v0.3.10 / `e7b0a67` shipped macOS-over-SSH lifecycle behavior. PR #375 / `3a38a93` and PR #376 / `a196659` are post-v0.3.10 documentation/issue hygiene. No release or runtime transition follows from this ledger.

## Destination by ownership layer

| Layer | Destination |
| --- | --- |
| New tasks | Native authenticated Amp creation, exact Orb/runner placement, parent/reply routing, messaging, waiting, and archive state. No generalized Amux spawn/adoption or automatic lifecycle enrollment. |
| Machine-local runners | Retained Amux workdir registry; pin/list/doctor/park/remove; minimum safe reconcile; `amux launch --all`; tmux/Amp process launch; install/update/maintenance diagnostics. |
| OS activation | systemd/launchd activates Amux and retains its process group. Verified patterns: systemd `Type=oneshot` + `RemainAfterExit=yes`; RunAtLoad LaunchAgent + `AbandonProcessGroup=true`. Direct OS supervision does not replace Amux launch. |
| Worker/coordination stores | Existing workers, shelves, groups, reports, callbacks, deadlines, finish authorization, operations, and spawn assignments are compatibility/drain-only. No ordinary native work is enrolled. |
| Provider bridges | Drain only after each existing replacement gate is proven. `/amux-tycho` remains unchanged until #328 passes; never dual-route. |
| Git/worktree safety | Independent safety guidance; not a reason to retain coordination state and not contingent on full-product archive. |

## Pull-request dispositions retained

| PR | Disposition |
| --- | --- |
| [#360](https://github.com/zainfathoni/amux/pull/360) | Merged one-time read-only inventory. This direction does **not** run it; a future invocation needs a separate explicit owner request. After that separately authorized run, sunset activates only when the owner accepts one complete inventory or explicitly dispositions an incomplete/error result **and** confirms no repeat is needed; only then delete before the next release. |
| [#361](https://github.com/zainfathoni/amux/pull/361) | Retain exact pre-cutover prepare/arm/finalize drain safety; admit no successor coordination protocol. |
| [#363](https://github.com/zainfathoni/amux/pull/363) | Retain backup-before-removal safety, separate from removal authority. |
| [#366](https://github.com/zainfathoni/amux/pull/366) | Retain as preflight-only evidence for runner operations. It performs no stop, row mutation/removal, admission closure, or migration away from Amux. |
| [#373](https://github.com/zainfathoni/amux/pull/373) | Retain completed-worker finish wind-down for existing workers. It does not enroll native work. |

The closed symmetric-retirement graph and superseded provider/productization PRs remain closed. Nothing in ADR 0008 revives an append-only retirement stream, six-class planner, finalizer, provider-neutral control plane, or permanent Lead hierarchy.

## Live issue disposition

The narrow six-issue open set remains #221, #311, #318, #328, #331, and #339.

| Issue | Current role |
| --- | --- |
| [#331](https://github.com/zainfathoni/amux/issues/331) | Keep open and rewrite in place as the umbrella for thinning Amux while retaining the runner-launch core and draining legacy coordination/provider state. |
| [#339](https://github.com/zainfathoni/amux/issues/339) | Keep open and rewrite in place as rollout/documentation alignment for the retained host boundary and evidence-based legacy-store drain. No replacement dates yet. |
| [#328](https://github.com/zainfathoni/amux/issues/328) | Keep narrowly open for the authenticated same-turn structured-return gate. Until it passes, keep `/amux-tycho` unchanged and never dual-route. |
| [#221](https://github.com/zainfathoni/amux/issues/221) | Drain already-bound Claude pairs only; no new paired-shelving protocol. |
| [#311](https://github.com/zainfathoni/amux/issues/311) | Compatibility fix only if digest drift blocks an existing receipt drain. |
| [#318](https://github.com/zainfathoni/amux/issues/318) | Strict selector boundary only on retained compatibility/drain commands. Canonical evidence does not require broader wording. |

Closed #232 remains safety evidence implemented by preflight-only PR #366. Closed #374 remains closed; its long-running-supervisor idea is still not required, but its body rationale that OS/native launch replaces Amux is obsolete and should receive a non-reopening correction comment. Closed #212–#216 remain closed because ADR 0008 retains workdir identity and does not revive their separate Amux runner-ID product graph.

## Withdrawn dates and gates

The proposed 2026-09-01 cutover and 2026-11-30 reader-window date were never landed and are withdrawn. No replacement date is selected.

There is no runner-admission closure gate and no complete-product archive gate. Any future date applies only to a named legacy coordination/provider family and requires a separate owner decision. The retained runner registry, launch, maintenance, and diagnostics are not governed by a legacy-store reader window.

## Owner decisions still required

1. Select cutover generations only for remaining legacy worker/coordination/provider admission families; no dates are recorded yet.
2. Select compatibility-reader duration and minimum retained reader release per legacy store, not for the retained runner host.
3. For #360, accept one complete inventory or explicitly disposition incomplete/error and separately confirm no repeat before the existing deletion condition activates.
4. Decide implementation scope and release sequencing for completing retained runner remove/reconcile behavior using #232/#366 safety evidence.
5. Name and field-validate #328's concrete authenticated same-turn Tycho return route before receipt admission closes.
6. Authorize any further issue-body rewrite/comment, release, process/store mutation, or external deployment change separately.

The exact #331/#339 rewrites and closed-#374 corrective comment applied on 2026-08-30 are recorded in [the issue direction corrections](issue-direction-corrections.md).
