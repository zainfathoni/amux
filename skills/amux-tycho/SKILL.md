---
name: amux-tycho
description: "Experimental Tycho external-executor report bridge for amux. Use only after an explicit owner request to route a bounded task through an existing Tycho agent/project/harness/model and return one structured semantic report to the current Amp coordinator. Tycho may route Claude or Pi but receives report-only authority, never Amp or provider identity."
---

# amux-tycho (experimental)

Explicit-only external-executor bridge. Tycho may route Claude or Pi, but Tycho is only a typed `report_only` producer: it is not an Amp coordinator, worker, runner, group member, callback target, provider identity, or lifecycle principal.

The current real Amp `T-...` thread remains coordinator, consumer, delivery authority, and acknowledgement authority. Tycho receives no group/member/callback/finish/label/merge/release/cleanup authority. An optional group reference is correlation metadata only. Do not activate this skill from an incidental Tycho, Claude, Pi, model, harness, or capacity mention.

Before any field use, consult the [provider executor readiness matrix](https://github.com/zainfathoni/amux/blob/main/docs/provider-executor-readiness.md). This route is runtime-unverified. Do not interpret helper availability or synthetic tests as field readiness.

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

## Field-readiness limits

- No live Tycho/provider run is authorized merely by loading this skill. Current readiness is synthetic coverage only.
- Promotion still requires two useful real cycles, one natural receipt-preserving recovery, supported versioned Tycho ingress, authorization/privacy review, a stable scope/ADR decision, and separate owner approval.
- Owner-only filesystem permissions protect against other OS users, not another process with the same UID. The helper does not confine Tycho.
- There is no resident watcher, arbitrary Amp Web-thread return route, model/entitlement attestation, provider fallback, automatic retry, or stable Go command/schema.
- Stable `cmd/`, `internal/`, canonical Amp identity, group/report/callback, and lifecycle boundaries remain unchanged.

## Migrating pre-split receipts

Receipts created by the former core `/amux` helper remain compatible; they are not rewritten or upgraded. Install `/amux-tycho` explicitly, preserve the original state, custody, and abandonment directories byte-for-byte at their original canonical paths, and preserve every receipt ID, immutable binding, event ID, and capability. Continue with the helper at its new installed `/amux-tycho` path. Never recreate, copy, move, rebind, or upgrade a receipt or capability directory. For terminal cleanup `pending`, replay the identical terminal event against the same original capability directory.

## Load only what you need

- Canonical receipt, storage, submission, recovery, consumption, acknowledgement, abandonment, notification, and cleanup protocol: [`reference/tycho-report-bridge.md`](reference/tycho-report-bridge.md)
- Explicit activation checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md)
- Canonical helper: `experimental/tycho-report-bridge/tycho_report_bridge.py` within this installed skill

## Troubleshooting

- `created`: preserve custody and producer proof; no report exists. If custody is genuinely irrecoverable, use only the documented abandonment path.
- Existing pre-split receipt: preserve every original path and identity, require this thread to equal the bound Amp origin, and continue through the new helper path without migration mutation.
- `valid_report`: explicitly consume from the private store regardless of notification outcome.
- `delivered`: assess the returned report, then separately acknowledge if appropriate.
- `acknowledged` or `abandoned` with cleanup `pending`: replay the identical terminal event with the same capability directory; do not append another event.
- Lock contention: retry the identical operation. Malformed store, proof/custody conflict, wrong origin/target, invalid transition, or unknown notification outcome: preserve evidence and stop.

Never repair this route by fabricating Amp identity, changing immutable binding, granting provider/coordinator authority, editing stable registries, or treating notification/polling as delivery.
