---
name: amux-tycho
description: "Experimental Tycho external-executor report bridge for amux. Use only after an explicit owner request to route a bounded task through an exact existing or owner-authorized prepared Tycho route and return one structured semantic report to the current Amp coordinator, including the authoritative Amp /team-review Opus second-opinion workflow. Tycho may route Claude or Pi but receives report-only authority, never Amp or provider identity."
---

# amux-tycho (experimental)

Explicit-only external-executor bridge. Tycho may route Claude or Pi, but Tycho is only a typed `report_only` producer: it is not an Amp coordinator, worker, runner, group member, callback target, provider identity, or lifecycle principal.

The current real Amp `T-...` thread remains coordinator, consumer, delivery authority, and acknowledgement authority. Tycho receives no group/member/callback/finish/label/merge/release/cleanup authority. An optional group reference is correlation metadata only. Do not activate this skill from an incidental Tycho, Claude, Pi, model, harness, capacity, or generic review mention.

Before use, consult the [provider executor readiness matrix](https://github.com/zainfathoni/amux/blob/main/docs/provider-executor-readiness.md). The accepted Karsa/nix-home lifecycle establishes this experimental route as practically usable for normal explicit owner-authorized work with an existing Amp coordinator.

## Route triggers

- **Route a bounded task through an exact Tycho route**: follow [Explicit-only workflow](#explicit-only-workflow) and the canonical [bridge protocol](reference/tycho-report-bridge.md). Use an existing route or, only when the owner explicitly requests it, one owner-authorized prepared route created without provider execution and bound before its first run.
- **Authoritative Amp `/team-review` with one Opus second opinion**: load [`reference/team-review-second-opinion.md`](reference/team-review-second-opinion.md). Native Amp review is the default. Use Tycho only when an independent Opus judgment could change a high-impact conclusion; Amp finishes its first pass before any Tycho input and remains authoritative.
- **Recover this `/amux-tycho` receipt**: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md#optional-notification-and-recovery).
- **Abandon a created-only receipt with lost custody**: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md#created-only-lost-token-abandonment).

## Explicit-only workflow

1. **Bind the minimum semantic receipt.** Bind the existing real Amp coordinator, the exact owner-selected producer route, and the SHA-256 digest of the bounded task and reviewed artifact identity. Encode artifact identity in the task bytes covered by `task_digest`; do not add another field. The current helper's correlation, capability, and route-coordinate fields are compatibility transport mechanics, not extra semantic authority.
2. **Submit one typed report.** Tycho receives only `report_only` authority and submits exactly one bounded `complete` or `blocked` report. Process exit, logs, blocked state, pane text, hook execution, and model prose are not a report.
3. **Consume separately.** The bound Amp coordinator explicitly `consume`s the report to establish delivery and assess the returned payload. Consumption is not acceptance.
4. **Acknowledge separately.** After handling, the same coordinator performs a distinct `acknowledge`. Acknowledgement grants no merge, finish, publication, or cleanup authority beyond the compatibility receipt's own custody cleanup.

## Route selection

With explicit owner authorization, select the exact existing Tycho agent/project/harness/model and host route. When the owner instead explicitly authorizes a fresh route, `/tycho` may create exactly one owner-authorized prepared route without provider execution; freeze and adjacent-revalidate the returned project, agent, workdir, harness, and exact model before receipt creation, then create the immutable receipt before the first provider run. Route availability, model identity, entitlement, and host suitability decide where execution may run; they are not receipt fields or evidence of report delivery. Route preparation must not start the provider. If creation is rejected or indeterminate, stop with no retry, alias normalization, inferred provider identity, credential transfer, or fallback route.

## Task-specific validation

Apply only checks required by the task. For example, a code or PR review may use either same-head local attachments or an immutable remote artifact bound by repository, PR number, full head SHA, full tree SHA, and exact fetched content identity. Worktree-cleanliness checks still bind every local comparison attachment. Those checks validate the reviewed artifact; they do not attest Tycho's model/host route and are not generic receipt ceremony. The #328 workflow owns its stricter review-specific checks and permits no fallback from one artifact mode to another after receipt creation.

## Exceptional recovery

Use `show`, notification recovery, created-only abandonment, and cleanup replay only after restart, failure, indeterminate notification, lost custody, or terminal cleanup `pending`. Preserve original bindings, event IDs, and capability directories. Retry only an identical operation after lock contention; never poll as delivery, rebind, resend after notification intent, or invent a new receipt to evade conflict. Details live in the canonical [bridge protocol](reference/tycho-report-bridge.md), not the ordinary happy path.

## Optional long-run wake-up

For a long Tycho run, the Amp coordinator may own a single one-time Amp schedule whose prompt only re-checks the exact bound local Tycho agent's status/result. Clear it as soon as the run reaches a terminal or recovered state. The schedule firing is only a wake-up token—never durable truth, delivery, consume, or acknowledgement—and grants no retry, resend, lifecycle, or authority change. Do not turn it into a recurring watcher.

## Branch references

- Canonical receipt, storage, submission, recovery, consumption, acknowledgement, abandonment, notification, and cleanup protocol: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md)
- Authoritative Amp `/team-review` ordinary Opus second-opinion lane: [`reference/team-review-second-opinion.md`](reference/team-review-second-opinion.md)
- Explicit activation checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Canonical helper: `experimental/tycho-report-bridge/tycho_report_bridge.py` within this installed skill
- Compatibility migration activates only for a proven pre-split receipt; formal-promotion policy activates only for an explicit promotion decision. Both live in the canonical bridge protocol and readiness matrix, not this ordinary lane.
