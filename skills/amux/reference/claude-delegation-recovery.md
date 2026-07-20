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

For one registered launch-indeterminate receipt, submit this owner-only JSON on stdin to `lifecycle detach-indeterminate-worker`:

```json
{"delegation_id":"<exact-private-id>","event_id":"<stable-detach-operation-id>","origin_thread":"<exact-thread-id>","authorization":{"terminal_state":"merged","report_sha256":"<sha256>","coordinator_authorization_sha256":"<sha256>"}}
```

Use the same event ID and exact request after interruption; `duplicate` is success and conflicting reuse blocks. `matching_live_process`, `launch_identity_ambiguous_or_mismatched`, `tmux_inspection_ambiguous`, `tmux_inspection_unavailable`, or any generic proof blocker preserves the receipt and durable origin fence. Do not kill a pane, inject input, retry launch, acquire a session, park, clean artifacts, or release the fence. After `detached`, rerun paired `worker-teardown --dry-run` and require the hashed pair to report `state:worker_detached`, `action:none`, and overall `ready`; then run the separate Amp worker dry-run. This authorizes only later Amp worker teardown, not receipt resolution or cleanup.

## Capacity or usage interruption

Record the factual `unavailable` or `untested` capability and pause. Capacity diagnostics are implementation inputs, not permission to select Claude autonomously, exceed a reserve, or claim a useful result.
