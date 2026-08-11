---
name: amux-tycho
description: "Experimental Tycho external-executor report bridge for amux. Use only after an explicit owner request to route a bounded task through an existing Tycho agent/project/harness/model and return one structured semantic report to the current Amp coordinator, including the authoritative Amp /team-review Opus second-opinion workflow. Tycho may route Claude or Pi but receives report-only authority, never Amp or provider identity."
---

# amux-tycho (experimental)

Explicit-only external-executor bridge. Tycho may route Claude or Pi, but Tycho is only a typed `report_only` producer: it is not an Amp coordinator, worker, runner, group member, callback target, provider identity, or lifecycle principal.

The current real Amp `T-...` thread remains coordinator, consumer, delivery authority, and acknowledgement authority. Tycho receives no group/member/callback/finish/label/merge/release/cleanup authority. An optional group reference is correlation metadata only. Do not activate this skill from an incidental Tycho, Claude, Pi, model, harness, capacity, or generic review mention.

Before use, consult the [provider executor readiness matrix](https://github.com/zainfathoni/amux/blob/main/docs/provider-executor-readiness.md). Repeated real owner use establishes this experimental route as practically usable for normal explicit owner-authorized work with an existing Amp coordinator. Practical use does not by itself close #323: that requires one genuine complete bridge lifecycle under its remaining acceptance criterion.

## Route triggers

- **Route a bounded task through an existing Tycho agent**: follow [Explicit-only workflow](#explicit-only-workflow) and the canonical [bridge protocol](reference/tycho-report-bridge.md).
- **Authoritative Amp `/team-review` with one Opus second opinion**: load [`reference/team-review-second-opinion.md`](reference/team-review-second-opinion.md). Amp finishes an independent first pass; owner approves one exact existing Tycho route (normally exact `claude-opus-5`); create one immutable receipt before Tycho; accept only one typed `complete`/`blocked` report; Amp alone verifies candidates and mutates the single current-user PENDING GitHub review.
- **Recover this `/amux-tycho` receipt**: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md#optional-notification-and-recovery).
- **Abandon a created-only receipt with lost custody**: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md#created-only-lost-token-abandonment).

## Explicit-only workflow

1. **Keep Amp in charge.** For a new receipt, bind the current canonical Amp thread as immutable `origin_thread` and retain its custody through delivery and acknowledgement. For an existing receipt, inspect `show` and require the current canonical Amp thread to exactly match the already-bound origin; otherwise stop. Owner authorization and custody possession never transfer coordinator, consume, or acknowledgement authority. Never substitute a Tycho key, provider session, project, run, task message, pane, or invented `T-...` identity.
2. **Select an existing Tycho route.** With explicit owner authorization, identify the exact existing Tycho agent, project, harness, and model/provider route. Record what Tycho reports without normalizing aliases or inferring provider identity. This selection is a coordinator assertion, not bridge attestation of project, harness, provider, or model identity. Missing, ambiguous, unavailable, or owner-unapproved selection blocks. Do not create a Tycho agent/project, switch harnesses/models, transfer credentials, accept fallback, or retry another provider under this skill.
3. **Freeze the task and binding.** Before external execution, write one bounded task with acceptance criteria and a stable reference, compute its SHA-256 digest, and fix the immutable receipt identity: canonical origin Amp thread, correlation ID, producer nonce, exact Tycho agent key, nullable provider session ID, run ID, task message ID, canonical workdir, task reference/digest, producer role, exact `report_only` authority, and optional reference-only group and coordinator-selected notification target. A changed task, route, producer, session, run, message, workdir, or role requires a newly authorized operation; never rebind an existing receipt.
4. **Establish restart-safe custody.** Create the receipt before asking Tycho to execute. Keep the coordinator token, abandonment token, coordinator-custody directory, and abandonment-capability directory owner-private and out of Tycho input. Give the producer only the exact proof fields, producer nonce, and state-directory path required for `submit`. Use the canonical helper and storage contract in [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md).
5. **Run through Tycho, not as Tycho.** The selected Tycho system owns machine/provider routing and may invoke Claude or Pi. This skill does not launch or control that provider process and does not grant Tycho Claude/Pi provider identity or `/amux-claude`/`/amux-pi` authority. Process exit, logs, blocked state, pane text, hook execution, and model prose are not completion.
6. **Submit one semantic report.** The producer submits one bounded structured `complete` or `blocked` report with summary, findings, blockers, and verification while repeating all immutable proof fields. Only a valid durable `submit` creates `valid_report`. A hook may invoke that idempotent operation; its queue is never durable truth.
7. **Recover explicitly.** After restart, notification failure, or uncertain Tycho state, inspect the private receipt with `show`. Preserve the original event IDs and binding. Retry only an identical operation after lock contention. Never poll Tycho as delivery, invent a new receipt to evade conflict, resend after notification intent, or mutate stable Amux group/report/callback state.
8. **Consume, assess, then acknowledge.** The bound Amp coordinator explicitly `consume`s `valid_report`, which durably records `delivered` and returns the semantic payload. Independently assess it, then use a separate event to `acknowledge` that exact delivered report. Consumption is not acceptance; acknowledgement grants no merge, finish, or cleanup authority.
9. **Handle notification as wake-up only.** A coordinator-selected exact live Amp pane may receive only the bounded correlation token. Success, failure, stale target, timeout, or indeterminate outcome never establishes delivery or acknowledgement and never authorizes automatic resend. No verified pane means a recoverable owner-private inbox, not arbitrary Amp Web-thread delivery.
10. **Abandon only the narrow lost-custody case.** A created-only receipt with irrecoverably missing bound coordinator custody may be terminally abandoned only with the separately stored matching abandonment capability and exact owner authorization. Never abandon a submitted report, recreate custody, delete/rewrite history, move a capability to fake absence, or retrofit a legacy receipt.
11. **Finish cleanup truthfully.** Acknowledgement removes coordinator custody only after the terminal event is durable; abandonment similarly removes its capability. Cleanup `pending` means replay the same terminal event against the same original capability directory. Never infer global absence from `removed`, and remove a separately found leftover record only after `show` proves its exact receipt is already terminal.

## Optional long-run wake-up

For a long Tycho run, the Amp coordinator may own a single one-time Amp schedule whose prompt only re-checks the exact bound local Tycho agent's status/result. Clear it as soon as the run reaches a terminal or recovered state. The schedule firing is only a wake-up token—never durable truth, delivery, consume, or acknowledgement—and grants no retry, resend, lifecycle, or authority change. Do not turn it into a recurring watcher.

## Evidence and promotion limits

- No live Tycho/provider run is authorized merely by loading this skill; each run still requires explicit owner authorization and exact route selection.
- [#327](https://github.com/zainfathoni/amux/issues/327) blocks only [#328](https://github.com/zainfathoni/amux/issues/328)'s newly spawned local-Amp-worker assignment workflow. It does not block generic `/amux-tycho` use or #323 field credit with an existing coordinator. Do not start #327 or #328 merely to close #323.
- #323 closes after one genuine complete create → separate-process recover → `valid_report` → consume/`delivered` → separate acknowledge → terminal-cleanup lifecycle. Audit recent genuine runs first; if none used the receipt bridge, capture this lifecycle during the next ordinary owner-authorized task rather than creating an artificial canary.
- Multiple cycles, natural-failure recovery, versioned ingress, privacy review, ADR work, and formal readiness promotion are optional promotion policy. They do not gate normal use or #323 closure.
- Owner-only filesystem permissions protect against other OS users, not another process with the same UID. The helper does not confine Tycho.
- There is no resident watcher, arbitrary Amp Web-thread return route, model/entitlement attestation, provider fallback, automatic retry, or stable Go command/schema.
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
