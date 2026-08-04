# Experimental Tycho semantic-report bridge

This skill-owned adapter is an unstable machine-local receipt/inbox experiment for [#323](https://github.com/zainfathoni/amux/issues/323). It is not a stable `amux` command or schema and has no compatibility guarantee. It does not launch, stop, watch, poll, or otherwise own Tycho or a provider process.

## Authority boundary

- The immutable `origin_thread` is an exact canonical Amp `T-...` ID. That Amp thread remains coordinator, delivery consumer, and acknowledgement authority.
- Tycho is recorded as a typed external producer with exactly `report_only` authority. Its agent key is never written to an Amp thread, coordinator, member, authorizer, label, or callback field.
- An optional `group_reference` is correlation metadata only. It grants no group membership, finish, lifecycle, merge, release, or label authority.
- The adapter makes no arbitrary Amp Web-thread delivery claim. A `T-...` origin without a separately verified exact live local Amp pane has only a recoverable owner-private machine-local inbox.
- Tycho process exit, blocked process state, logs, pane text, prose, and hook execution are not reports. Only a valid structured `submit` operation can commit `valid_report`.

The successful state sequence is `created → valid_report → delivered → acknowledged`. A created-only receipt whose coordinator token is irrecoverably lost may instead transition once to terminal `abandoned`. Explicit `consume` is delivery. `acknowledge` is a separate action and requires the same delivered report plus restart-safe coordinator custody. Notification is never delivery or acknowledgement.

## Binding and storage

The coordinator creates a receipt by piping one bounded JSON object to:

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" \
  --custody-dir "$PRIVATE_COORDINATOR_CUSTODY_DIR" \
  --abandonment-dir "$PRIVATE_ABANDONMENT_CAPABILITY_DIR" create
```

Creation requires an operation `event_id`, separately generated unguessable `coordinator_token` and `abandonment_token` values, and an immutable `binding` containing:

- `receipt_id`, canonical `origin_thread`, opaque `correlation_id`, and an unguessable `producer_nonce`;
- exact `tycho_agent_key`, nullable `claude_session_id`, `run_id`, and task `task_message_id` causality;
- canonical absolute `workdir`, bounded `task_reference`, SHA-256 `task_digest`, `producer_role`, and exact `authority: "report_only"`;
- optional reference-only `group_reference` and an optional coordinator-selected `notification_target` containing the exact local tmux pane ID/incarnation.

Keep both owner tokens and both owner-directory paths out of Tycho input. Give the producer only its exact binding/proof fields, state-directory path, and producer nonce. The receipt store retains all capabilities only as hashes. Before committing a new receipt, `create` atomically installs separate exact receipt/origin records for coordinator custody and abandonment capability using no-clobber file creation; successful create output explicitly includes `state:"created"`. The coordinator process may exit after create: later `consume` and `acknowledge` processes recover the token from custody instead of receiving it through JSON or argv. Inputs reject unknown fields, duplicate JSON keys, malformed values, and oversized content.

The state, custody, and abandonment directories are pairwise separate, non-nested, owner-only `0700` directories; path aliases and nesting reject before mutation. The producer commands `submit` and `show` reject owner-directory arguments. The state directory's `experimental.lock` and atomically replaced `receipts.json` are `0600`; each capability record is also atomic and `0600`, bound to one exact receipt and Amp origin. Every append-only event mutation takes the receipt lock. Writes flush the temporary file, atomically install or replace the destination as appropriate, and flush its containing directory. Coordinator custody is removed only after durable acknowledgement; the distinct abandonment capability is removed only after durable abandonment. Both terminal commands run one shared idempotent cleanup phase after the event is durable, on the recorded and the duplicate path alike. Because the transition is already committed, no cleanup failure can turn it into a rejection: every unlink, record-open, directory-open, and fsync failure returns the terminal durable state with cleanup `pending`. Replaying that same terminal event retries cleanup — including the directory flush when the record is already gone — and appends no second event. Always replay a terminal event with the same exact capability directory the original operation used: cleanup status describes only the directory this invocation was given and never proves global absence, so replaying a `pending` acknowledgement or abandonment against a different directory reports `removed` while the real record survives at its own `0600` path. That leftover record authorizes nothing on an already terminal receipt, but it is not cleaned up. A separately discovered leftover `0600` custody or abandonment record may be removed only after `show` verifies that its exact bound receipt is already terminal (`acknowledged` or `abandoned`); never remove it merely because another invocation reported cleanup `removed`. These owner-only permissions are a privacy boundary against other OS users, not confinement from another process running as the same owner; never give Tycho either owner path or permission to search for it. The receipt store is bounded to 1 MiB, 128 receipts, 16 events per receipt, and bounded report arrays/strings. Exact event replay is `duplicate`; reuse with conflicting content rejects before mutation. Caller event IDs cannot use the reserved fixed-length internal notification namespace. Exit `2` is a preflight rejection, including lock contention before a durable operation; retry only the identical operation.

## Submit, recover, consume, and acknowledge

`submit` requires all immutable producer proof fields again, one unique `event_id`, and exactly one bounded semantic report (`complete` or `blocked`, summary, findings, blockers, and verification). The producer cannot add, remove, or change notification routing. The adapter atomically commits `valid_report` plus any coordinator-bound notification intent before attempting notification.

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" submit < report.json
```

The bound Amp coordinator recovers and explicitly consumes the inbox item with its exact origin and coordinator token. Consumption both commits `delivered` and returns the report. Replaying the same consume event is duplicate-safe and rematerializes the same report.

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" \
  --custody-dir "$PRIVATE_COORDINATOR_CUSTODY_DIR" consume < consume.json
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" \
  --custody-dir "$PRIVATE_COORDINATOR_CUSTODY_DIR" acknowledge < acknowledge.json
```

Acknowledgement requires a later distinct event bound to the same report event. Wrong origin, custody token, producer nonce, producer/session/run/task/correlation identity, workdir, role, authority, digest, causality, or transition rejects before mutation. Custody remains after consumption and is removed only after the acknowledgement event is durably committed. Replaying acknowledgement after a process exit is duplicate-safe and finishes any pending custody removal.

## Created-only lost-token abandonment

If a legacy or crashed create has no recoverable coordinator token, do not submit a report, recreate custody, rebind the identity, or delete/rewrite the receipt. With exact owner authority, append one terminal event using the bound Amp origin and exact reason:

```sh
python3 skills/amux/experimental/tycho-report-bridge/tycho_report_bridge.py \
  --state-dir "$PRIVATE_STATE_DIR" \
  --custody-dir "$PRIVATE_COORDINATOR_CUSTODY_DIR" \
  --abandonment-dir "$PRIVATE_ABANDONMENT_CAPABILITY_DIR" abandon <<'JSON'
{"receipt_id":"...","event_id":"...","origin_thread":"T-...","reason":"coordinator_token_lost"}
JSON
```

Only a created-only receipt with missing coordinator custody and a matching independently stored abandonment capability can become `abandoned`. Receipt ID and origin alone are not authority.

`create` binds the canonical identity of that receipt's coordinator custody directory into the separately protected abandonment capability record, as a domain-separated digest of the resolved path and never as a plaintext path in the receipt store, in any output, or in producer input. `abandon` re-derives the identity of the custody directory it was given and must match that binding before it tests custody absence. A caller-selected empty, fake, alternate, or aliased custody directory therefore rejects instead of faking irrecoverable token loss, and any custody entry present at the bound path — including a dangling link — blocks abandonment. Only a definite absence at the bound path is absence: an unsearchable or replaced custody directory rejects instead. That rejection holds while the capability record is intact; if the record itself is deleted from a still-`created` receipt, a `create` replay re-binds whatever custody directory it is then given, and also re-installs a coordinator custody record there, which itself blocks abandonment until removed. Such a replay requires an exact `coordinator_token` match, which is strictly more authority than abandonment confers and contradicts the loss premise, and the resulting record still cannot abandon without the original `abandonment_token`, because `abandon` compares against the immutable hash in the receipt. Never delete a capability record to retry. This binds the directory a caller names, not the file inside it: an owner running as the same UID who moves the real custody record elsewhere is indistinguishable from genuine token loss, consistent with the same-owner limit above. Possession of the `abandonment_token` is the abandonment authority; the abandonment directory's own path is deliberately not bound, so the capability record stays relocatable by its owner. The operation is append-only and replay-safe, removes the abandonment capability after its durable event, and permanently rejects submit, consume, acknowledge, create replay, rebind, and identity reuse. It never deletes receipt history. A legacy receipt created before abandonment-capability binding cannot be retrofitted, rebound, or abandoned; preserve it as evidence.

## Optional notification and recovery

Notification is opt-in when the Amp coordinator binds the receipt at `create`; it is not a producer-selected submission field. The target must contain an exact tmux `pane_id` and `pane_created`; immediately before sending, the helper requires that exact incarnation to report `pane_current_command=amp`. It sends only:

```text
AMUX_TYCHO_REPORT receipt=<bounded-id> correlation=<bounded-event-id>
```

The helper commits notification intent in the same durable update as `valid_report`, before tmux I/O. Success, stale/non-Amp pane, failure, and timeout are append-only outcomes that never advance receipt state. An intent without a result—including result-write contention after tmux I/O—is indeterminate and does not turn the already committed submission into a preflight rejection. Exact report replay never resends a notification, including after a failed or indeterminate attempt. There is no resident watcher and polling or a Tycho hook queue never establishes delivery.

Recovery always starts with the private store:

1. Run `show --receipt-id <id>` and inspect the append-only event metadata. It redacts stored token hashes and withholds the semantic payload so metadata inspection cannot bypass explicit consumption.
2. If state is `valid_report`, explicitly `consume` with the bound Amp origin and its separate custody directory even if notification failed, was stale, succeeded, or is indeterminate.
3. Independently assess the returned report, then issue a separate `acknowledge` operation if appropriate.
4. If state is created-only and custody is missing, do not invoke Tycho; preserve it or use explicit owner-authorized `abandon` once only when the independently bound abandonment capability exists.
5. On lock contention, retry the identical event. On malformed store, custody conflict, binding conflict, wrong target, invalid transition, or unknown notification outcome, preserve state and stop; do not invent a new ID, resend, infer delivery, or mutate stable group/report/callback registries.

The adapter and its tests require Python 3.10 or newer. The adapter currently has synthetic coverage only. Do not perform a live Tycho/provider run merely to promote it. Promotion requires two useful real cycles, one natural receipt-preserving failure recovery, supported versioned Tycho ingress, authorization/privacy review, a stable scope/ADR decision, and separate owner approval.
