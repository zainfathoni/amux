# Experimental Claude delegation recovery

Use only the branch matching the failed workflow criterion. Preserve the receipt, private state, process, and worktree unless an explicit owner action says otherwise.

## Receipt replay or conflict

After an interrupted helper mutation, inspect the receipt and retry the identical operation with the **same event ID** and exact payload. `duplicate` is success. A conflict means that identity already denotes different content: stop and investigate; never erase, rewrite, migrate, or replace historical evidence to force progress.

## Launch or acquisition mismatch

Stop before receipt creation when the pre-packet `launch policy-digest` result differs from the final `launch plan` policy digest. Recompute neither value by hand and do not rewrite or inject content after planning. Stop before launch when repository, base, clean linked-worktree status, private packet mode, policy digest, or required capability differs. If durable `launch_intent` lacks `launch_completed`, the outcome is indeterminate: do not retry launch, guess, or create another window. Inspect tmux and the receipt manually; acquire only one exact Claude session incarnation or leave the receipt recoverable.

## Missing or invalid semantic message

Pane prose, idle state, stop hooks, and process exit are not reports. Do not infer completion or blockage. Keep the receipt unresolved and inspect only bounded structural diagnostics. Never preserve a pane capture or transcript as a substitute report.

## Input request

Consume the request to mark it seen without resolving it. Handle the response manually and within the declared follow-up bound. Do not automatically inject text into Claude's composer. Record acceptance only after explicit correlated Claude-side confirmation; otherwise leave the request unresolved or let a later explicit source event supersede it.

## Notification unavailable or failed

Do not search for a convenient Amp pane or weaken identity checks. Recover the report through `receipt show` and `inbox consume`; leave the receipt recoverable until that succeeds. Exact replay of an attempt missing its result records `unavailable` without resending. A wake-up token never establishes delivery.

## Delivery, acknowledgement, or parking rejection

Consume the current valid report before acknowledging that same message, and resolve or explicitly supersede any input request. Keep Claude active until acknowledgement. If exact incarnation re-verification fails, do not kill a pane by name, PID, workdir, or session ID alone. Preserve the acknowledged receipt and inspect whether tmux, process, workdir, launch command, or incarnation changed. If `park_intent` is durable without a result, retry the same event with `"recover":true`; only exact matching identity or confirmed exact-pane absence may complete it. Do not automatically park or clean up.

## Capacity or usage interruption

Record the factual `unavailable` or `untested` capability and pause. Capacity diagnostics are implementation inputs, not permission to select Claude autonomously, exceed a reserve, or claim a useful result.
