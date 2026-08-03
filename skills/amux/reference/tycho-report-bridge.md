# Experimental Tycho semantic-report bridge

This skill-owned adapter is an unstable machine-local receipt/inbox experiment for [#323](https://github.com/zainfathoni/amux/issues/323). It is not a stable `amux` command or schema and has no compatibility guarantee. It does not launch, stop, watch, poll, or otherwise own Tycho or a provider process.

## Authority boundary

- The immutable `origin_thread` is an exact canonical Amp `T-...` ID. That Amp thread remains coordinator, delivery consumer, and acknowledgement authority.
- Tycho is recorded as a typed external producer with exactly `report_only` authority. Its agent key is never written to an Amp thread, coordinator, member, authorizer, label, or callback field.
- An optional `group_reference` is correlation metadata only. It grants no group membership, finish, lifecycle, merge, release, or label authority.
- The adapter makes no arbitrary Amp Web-thread delivery claim. A `T-...` origin without a separately verified exact live local Amp pane has only a recoverable owner-private machine-local inbox.
- Tycho process exit, blocked process state, logs, pane text, prose, and hook execution are not reports. Only a valid structured `submit` operation can commit `valid_report`.

The state sequence is `created → valid_report → delivered → acknowledged`. Explicit `consume` is delivery. `acknowledge` is a separate action and requires the same delivered report plus the coordinator-only token. Notification is never delivery or acknowledgement.

## Binding and storage

The coordinator creates a receipt by piping one bounded JSON object to:

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" create
```

Creation requires an operation `event_id`, a separately generated unguessable `coordinator_token`, and an immutable `binding` containing:

- `receipt_id`, canonical `origin_thread`, opaque `correlation_id`, and an unguessable `producer_nonce`;
- exact `tycho_agent_key`, nullable `claude_session_id`, `run_id`, and task `task_message_id` causality;
- canonical absolute `workdir`, bounded `task_reference`, SHA-256 `task_digest`, `producer_role`, and exact `authority: "report_only"`;
- optional reference-only `group_reference` and an optional coordinator-selected `notification_target` containing the exact local tmux pane ID/incarnation.

Keep the coordinator token out of Tycho input. Give the producer only its exact binding/proof fields and producer nonce. Both tokens are stored only as hashes. Inputs reject unknown fields, duplicate JSON keys, malformed values, and oversized content.

The state directory is owner-only `0700`; its single `experimental.lock` and atomically replaced `receipts.json` are `0600`. Every append-only event mutation takes that one non-blocking lock domain. Writes validate the complete outgoing store, flush the temporary file, atomically replace the store, and flush the containing directory. The store is bounded to 1 MiB, 128 receipts, 16 events per receipt, and bounded report arrays/strings. Exact event replay is `duplicate`; reuse with conflicting content rejects before mutation. Caller event IDs cannot use the reserved fixed-length internal notification namespace. Exit `2` is a preflight rejection, including lock contention before a durable operation; retry only the identical operation.

## Submit, recover, consume, and acknowledge

`submit` requires all immutable producer proof fields again, one unique `event_id`, and exactly one bounded semantic report (`complete` or `blocked`, summary, findings, blockers, and verification). The producer cannot add, remove, or change notification routing. The adapter atomically commits `valid_report` plus any coordinator-bound notification intent before attempting notification.

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" submit < report.json
```

The bound Amp coordinator recovers and explicitly consumes the inbox item with its exact origin and coordinator token. Consumption both commits `delivered` and returns the report. Replaying the same consume event is duplicate-safe and rematerializes the same report.

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" consume < consume.json
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" acknowledge < acknowledge.json
```

Acknowledgement requires a later distinct event bound to the same report event. Wrong origin, coordinator token, producer nonce, producer/session/run/task/correlation identity, workdir, role, authority, digest, causality, or transition rejects before mutation.

## Optional notification and recovery

Notification is opt-in when the Amp coordinator binds the receipt at `create`; it is not a producer-selected submission field. The target must contain an exact tmux `pane_id` and `pane_created`; immediately before sending, the helper requires that exact incarnation to report `pane_current_command=amp`. It sends only:

```text
AMUX_TYCHO_REPORT receipt=<bounded-id> correlation=<bounded-event-id>
```

The helper commits notification intent in the same durable update as `valid_report`, before tmux I/O. Success, stale/non-Amp pane, failure, and timeout are append-only outcomes that never advance receipt state. An intent without a result—including result-write contention after tmux I/O—is indeterminate and does not turn the already committed submission into a preflight rejection. Exact report replay never resends a notification, including after a failed or indeterminate attempt. There is no resident watcher and polling or a Tycho hook queue never establishes delivery.

Recovery always starts with the private store:

1. Run `show --receipt-id <id>` and inspect the append-only event metadata. It redacts stored token hashes and withholds the semantic payload so metadata inspection cannot bypass explicit consumption.
2. If state is `valid_report`, explicitly `consume` with the bound Amp origin and coordinator token even if notification failed, was stale, succeeded, or is indeterminate.
3. Independently assess the returned report, then issue a separate `acknowledge` operation if appropriate.
4. On lock contention, retry the identical event. On malformed store, binding conflict, wrong target, invalid transition, or unknown notification outcome, preserve state and stop; do not invent a new ID, resend, infer delivery, or mutate stable group/report/callback registries.

The adapter currently has synthetic coverage only. Do not perform a live Tycho/provider run merely to promote it. Promotion requires two useful real cycles, one natural receipt-preserving failure recovery, supported versioned Tycho ingress, authorization/privacy review, a stable scope/ADR decision, and separate owner approval.
