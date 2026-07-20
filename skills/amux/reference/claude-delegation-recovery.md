# Experimental Claude delegation recovery

Use only the branch matching the failed workflow criterion. Preserve the receipt, private state, process, and worktree unless an explicit owner action says otherwise.

## Receipt replay or conflict

After an interrupted helper mutation, inspect the receipt and retry the identical operation with the **same event ID** and exact payload. `duplicate` is success. A conflict means that identity already denotes different content: stop and investigate; never erase, rewrite, migrate, or replace historical evidence to force progress.

## Launch or acquisition mismatch

Stop before receipt creation when a read-only request omits `expected_launch_policy_digest` or the helper rejects it as malformed or different from the selected policy. Recompute neither value by hand and do not rewrite or inject content after planning. Stop before launch when repository, base, clean linked-worktree status, private packet mode, policy digest, or required capability differs. If durable `launch_intent` lacks `launch_completed`, the outcome is indeterminate: do not retry launch, guess, or create another window. Inspect tmux and the receipt manually; acquire only one exact Claude session incarnation or leave the receipt recoverable.

## Missing or invalid semantic message

Pane prose, idle state, stop hooks, and process exit are not reports. Do not infer completion or blockage. Keep the receipt unresolved and inspect only bounded structural diagnostics. Never preserve a pane capture or transcript as a substitute report.

## Input request

Consume the request to mark it seen without resolving it. Handle the response manually and within the declared follow-up bound. Do not automatically inject text into Claude's composer. Record acceptance only after explicit correlated Claude-side confirmation; otherwise leave the request unresolved or let a later explicit source event supersede it.

## Notification unavailable or failed

Do not search for a convenient Amp pane or weaken identity checks. Recover the report through `receipt show` and `inbox consume`; leave the receipt recoverable until that succeeds. Exact replay of an attempt missing its result records `unavailable` without resending. A wake-up token never establishes delivery.

## Delivery, acknowledgement, or parking rejection

Consume the current valid report before acknowledging that same message, and resolve or explicitly supersede any input request. Keep Claude active until acknowledgement. If exact incarnation re-verification fails, do not kill a pane by name, PID, workdir, or session ID alone. Preserve the acknowledged receipt and inspect whether tmux, process, workdir, launch command, or incarnation changed. If `park_intent` is durable without a result, retry the same event with `"recover":true`; only exact matching identity or confirmed exact-pane absence may complete it. Do not automatically park or clean up.

## Paired worker teardown blocked

Treat every paired worker teardown blocker as authoritative: preserve the Amp worker, Claude pane, private artifacts/worktree, receipt, report, group history, canonical lifecycle registry, and durable origin fence. Use the returned non-content blocker code to inspect the existing branch above: consume and acknowledge reports deliberately, resolve input, repair an unavailable owner-private registered store, or recover the exact durable park intent. Never adopt a legacy or unrelated pane, rewrite the origin binding, issue a new launch after indeterminate intent, or infer ownership from names, cwd, PID, issue number, tmux placement, or Claude session ID. Retry paired teardown only after the durable state is safe; run Amp worker teardown last. If Amp teardown itself fails and the worker must resume, explicitly run `lifecycle worker-teardown-release` only after every registered pair still validates as safely parked. Never release the fence automatically or merely to bypass a blocker.

## Explicit legacy registration and indeterminate worker detach

Use this branch only after an owner explicitly requests recovery, supplies the exact private store path and immutable origin, and the coordinator supplies terminal Amp work authorization. Never search for a store or derive ownership. Register a pre-registry store once:

```sh
python3 "$HELPER" lifecycle register-legacy-store --origin-thread <thread-id> --store-path <exact-private-store>
```

Stop on any bounded registration blocker. Do not chmod, relink, copy, normalize, repair, or rewrite evidence to make validation pass. Exact `duplicate` is success. Registration does not detach, resolve, acquire, launch, park, or authorize teardown.

For one registered modern launch-indeterminate receipt, submit this owner-only JSON on stdin to `lifecycle detach-indeterminate-worker`:

```json
{"delegation_id":"<exact-private-id>","event_id":"<stable-detach-operation-id>","origin_thread":"<exact-thread-id>","authorization":{"terminal_state":"merged","report_sha256":"<sha256>","coordinator_authorization_sha256":"<sha256>"}}
```

Only when the registered receipt's launch intent is the exact historical pre-identity shape defined by the contract, add the explicit compatibility selector; never add missing fields to the receipt or use this selector for modern or mixed evidence:

```json
{"delegation_id":"<exact-private-id>","event_id":"<stable-detach-operation-id>","origin_thread":"<exact-thread-id>","compatibility":"pre_identity_launch_intent_v1","authorization":{"terminal_state":"merged","report_sha256":"<sha256>","coordinator_authorization_sha256":"<sha256>"}}
```

The two authorization digests are operator-supplied trusted references, not authority independently fetched or verified by the helper. Confirm that trust boundary before invoking the command; the helper only shape-validates and binds the references to the stable detach event. Use the same event ID and exact request after interruption; `duplicate` is success and conflicting reuse blocks. `matching_live_process`, `launch_identity_ambiguous_or_mismatched`, `matching_legacy_target`, `matching_legacy_session`, `process_inspection_ambiguous`, `process_inspection_unavailable`, `tmux_inspection_ambiguous`, `tmux_inspection_unavailable`, `launch_transport_active_or_indeterminate`, or any generic proof blocker preserves the receipt and durable origin fence. The legacy selector requires absence of both the exact target and exact historical session and inspection of every still-live same-owner process; it never reconstructs executable or argv identity. The launch gate serializes transport execution with absence proof and revocation commit, but detach tries it only for a short bounded interval while holding lifecycle and receipt locks; a busy gate blocks promptly and must be retried later, never waited out in place. Retained transport is authorized only for the exact pre-completion launch-indeterminate state and is revoked by completion, parking, an origin fence, or detach. Final transport authorization and `execve` are one lifecycle-then-receipt locked ordering boundary, so a concurrent fence writer commits either before authorization or after execution starts. Do not bypass the gate blocker or bounded subprocess inventory. After success the receipt is sealed: all launch, route, report, input, delivery, notification, park, recovery, and failure mutation routes reject, leaving only inspection, exact detach replay, and paired teardown. Do not kill a pane, inject input, retry launch, acquire a session, park, clean artifacts, or release the fence. After `detached`, rerun paired `worker-teardown --dry-run` and require the hashed pair to report `state:worker_detached`, `action:none`, and overall `ready`; then run the separate Amp worker dry-run. This authorizes only later Amp worker teardown, not receipt resolution or cleanup.

## Explicit live report-bearing pair retirement

Use this separate branch only for a validated modern `launch_intent + valid_report` receipt when terminal Amp work and trusted coordinator authorization are explicit and the exact intent-bound Claude target is still live. Never use absence detach, acquisition, acknowledgement, or ordinary parking to approximate this state. Submit the exact owner-private identity and stable operation on stdin:

```sh
python3 "$HELPER" lifecycle retire-live-indeterminate-pair <<'JSON'
{"delegation_id":"<exact-private-id>","event_id":"<stable-retirement-operation-id>","origin_thread":"<exact-thread-id>","authorization":{"terminal_state":"merged","report_sha256":"<canonical-validated-report-sha256>","coordinator_authorization_sha256":"<sha256>"}}
JSON
```

For an otherwise exact modern read-only launch intent whose sole absent field is the historical `expected_launcher_argv0_digest`, add `"compatibility":"historical_modern_read_only_launch_intent_v1"` to that request. Do not supplement or reconstruct the missing digest. The selector is invalid for a current-modern intent, `pre_identity_launch_intent_v1`, a mixed schema, or any shape with another missing or extra field. It preserves validation of the immutable `expected_argv_digest` and every other live-retirement identity requirement; the supplied terminal report and coordinator-authorization digests remain the same operator-trusted references described above.

Any privacy-safe blocker preserves the durable fence, report, receipt, runtime, packet, worktree, branch, artifacts, and process. Do not search for another target or manually kill a pane/PID. The final snapshot, process verification, and stop must remain on one retained control-mode connection to the original tmux server; connection or server loss blocks, and a replacement server must never be consulted for the durable pane ID. Control responses preserve ordinary bytes, require matching complete frame tuples, and carry identity fields only through tmux's reversible `q` command-argument quoting after explicit LF/CR substitution. Each snapshot and the exact kill acknowledgement require a fresh unpredictable 256-bit correlation token; malformed framing, encoded newlines masquerading as control markers, queued frames, empty acknowledgements, wrong tokens, or ambiguous decoding blocks before terminal proof. If interruption leaves `retirement_intent` without `pair_retired`, inspect the existing evidence and replay the identical request with only `"recover":true` added. Recovery confirms exact absence first and otherwise revalidates the complete durable identity before the one exact stop; it never blindly kills again. The separately stored process-start incarnation makes a same-PID/same-start executable or argv change a blocker rather than absence. Conflicting event ID, authorization, report bytes, target identity, or receipt state blocks. After success require paired `worker-teardown --dry-run` to return the hashed pair as `state:pair_retired`, `action:none` and overall `ready`. This authorizes only the separate Amp worker teardown; it does not resolve the Claude evidence, permit cleanup, release the origin fence, or authorize merge/release/tag/worktree mutation.

## Capacity or usage interruption

Record the factual `unavailable` or `untested` capability and pause. Capacity diagnostics are implementation inputs, not permission to select Claude autonomously, exceed a reserve, or claim a useful result.
