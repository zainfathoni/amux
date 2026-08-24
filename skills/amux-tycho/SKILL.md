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
- **Authoritative Amp `/team-review` with one Opus second opinion**: load [`reference/team-review-second-opinion.md`](reference/team-review-second-opinion.md). Amp finishes an independent first pass; owner approves one exact existing or owner-authorized prepared route (normally exact `claude-opus-5`); create one immutable receipt before Tycho; accept only one typed `complete`/`blocked` report; Amp alone verifies candidates and mutates the single current-user PENDING GitHub review.
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

## Optional formal promotion policy

- Loading this skill never authorizes a provider run; every run requires explicit owner authorization and exact route selection.
- [#327](https://github.com/zainfathoni/amux/issues/327)'s historical local-worker assignment gate was completed by merged PR #361. It is not a current blocker for [#328](https://github.com/zainfathoni/amux/issues/328) with a native Amp coordinator; exact #327 installation proof applies only when deliberately draining that superseded local-worker workflow.
- [#323](https://github.com/zainfathoni/amux/issues/323) closed after the accepted [Karsa/nix-home lifecycle](https://github.com/zainfathoni/nix-home/issues/13#issuecomment-5248690973) proved create, separate-process recovery, one typed report, consume, separate acknowledgement, and terminal cleanup.
- Multiple cycles, natural-failure recovery, supported versioned ingress, privacy review, ADR work, and formal readiness promotion are optional formal-promotion policy—not ordinary-use or #323 closure gates.
- Owner-only filesystem permissions protect against other OS users, not another process with the same UID. The helper does not confine Tycho.
- The semantic receipt is temporary compatibility transport. Earlier removal wording paired “native authenticated structured delivery and separate acknowledgement”; ADR 0007 supersedes that pairing because separate acknowledgement belongs only to existing receipts, not the future direct route. Stop creating receipts after one ordinary field run returns exactly one schema-valid bounded `complete|blocked` result, authenticated and correlated to the invoking Amp request/thread, directly to that caller through the exact selected route without transcript/log mining or an Amux receipt. That future direct return itself establishes delivery and has no separate Amux consume/acknowledge step. Existing receipts still drain only through `created → valid_report → delivered → acknowledged|abandoned`; notification uncertainty and terminal cleanup status remain separate. Do not promote helper fields into a stable Go command/schema merely to preserve the experiment.
- There is no resident watcher, arbitrary Amp Web-thread return route, model/entitlement attestation, provider fallback, or automatic retry.
- Stable `cmd/`, `internal/`, canonical Amp identity, group/report/callback, and lifecycle boundaries remain unchanged.

## Migrating pre-split receipts

Receipts created by the former core `/amux` helper remain compatible; they are not rewritten or upgraded. Install `/amux-tycho` explicitly, preserve the original state, custody, and abandonment directories byte-for-byte at their original canonical paths, and preserve every receipt ID, immutable binding, event ID, and capability. Continue with the helper at its new installed `/amux-tycho` path. Never recreate, copy, move, rebind, or upgrade a receipt or capability directory. For terminal cleanup `pending`, replay the identical terminal event against the same original capability directory.

## Load only what you need

- Canonical receipt, storage, submission, recovery, consumption, acknowledgement, abandonment, notification, and cleanup protocol: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md)
- Authoritative Amp `/team-review` Opus second-opinion workflow and #328 design decisions: [`reference/team-review-second-opinion.md`](reference/team-review-second-opinion.md)
- Explicit activation checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Canonical helper: `experimental/tycho-report-bridge/tycho_report_bridge.py` within this installed skill

## Troubleshooting

- `created`: preserve custody and producer proof; no report exists. If custody is genuinely irrecoverable, use only the documented abandonment path.
- Existing pre-split receipt: preserve every original path and identity, require this thread to equal the bound Amp origin, and continue through the new helper path without migration mutation.
- Provider stop or exit without `submit`: no Tycho finding; do not recover candidates from logs, state, or prose.
- `valid_report`: explicitly consume from the private store regardless of notification outcome.
- `delivered`: independently verify every candidate, then separately acknowledge if appropriate. Only Amp mutates the PENDING GitHub review.
- `acknowledged` or `abandoned` with cleanup `pending`: replay the identical terminal event with the same capability directory; do not append another event.
- Lock contention: retry the identical operation. Malformed store, proof/custody conflict, wrong origin/target, invalid transition, or unknown notification outcome: preserve evidence and stop.

Never repair this route by fabricating Amp identity, changing immutable binding, granting provider/coordinator authority, editing stable registries, treating notification/polling as delivery, or promoting readiness from an incomplete cycle.
