# amux troubleshooting

## Diagnose before mutation

Use canonical identity and JSON output:

```sh
amux --json install doctor
amux --json doctor --all
amux --json worker doctor --thread <id>
amux --json runner doctor --workdir <path>
tmux list-panes -a -F '#{session_name}\t#{window_name}\t#{pane_id}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_start_command}'
```

Do not treat a tmux server command, name similarity, or stale output as ownership proof.

## Partial success and retries

Exit `1` means mutation may have started. Inspect `successful`, `skipped`, and `failed`, then inspect only state authorized by the command-specific contract before any deliberate owner action. Exit `2` means request/preflight rejection before mutation. Generalized spawn prepare is closed. The owner-authorized projectless physical-host exception persists schema-2 `creation_armed`, `prepared`, `armed`, and `finalized` boundaries in `spawn-assignments.json`, bound to one exact host/workdir and no group. `creation_armed` forbids another create; `armed` forbids another message. Finalize updates only that assignment record and creates no pane or lifecycle state. Pre-cutover schema-1 records may only drain through their exact next transition. Never retry, fall back, reroute, rebind, adopt, paste, press Enter, archive, clean up, search, inspect pane/transcript/thread/preview state, reconcile, or use an alternate receiver. Existing worker rows and legacy operation records remain unchanged.

`shelve` records intent before remote archive and local park. `unshelve` removes intent only after remote unarchive. Visible partial synchronization is retryable desired state, not a reason to roll back by hand.

## Replace a stale worker

Health first; `no-response` alone does not authorize replacement. After explicit approval:

1. Preserve the old remote thread unless archival was requested.
2. Create and assign one replacement thread with authenticated native `create_thread` on the exact intended Orb or live runner/workdir. Send only a lean task prompt and retain native parent/reply routing; include no contract path or Amux lifecycle requirement, and do not adopt it.
3. Verify the authenticated native result and exact executor/workdir selection before removing the old local worker. If creation is indeterminate, keep the old worker and stop.
4. Use `amux worker remove --thread <old-id>` to stop/delete old local configuration without archiving. Use teardown only when archival is explicitly intended.
5. On interruption, report exact old/new thread, config, pane, and worktree state. Never retry native creation; retry another transition only when its operation-specific drain contract permits the identical request.

Reuse the task-appropriate mode selection rule; replacement urgency alone does not justify `high`, and replacement work never authorizes `ultra` or a special mode.

## Runner safety

Runner pin requires a canonical existing directory; Git repository, worktree, and lock state are irrelevant. Runner reconcile is a present-directory no-op and fails closed on a missing directory, retaining the row until authoritative process/catalog absence can be proved. Never delete marker files or remove runner configuration as part of worker finish.

## Mutation lock

All mutations and scheduled maintenance share one bounded machine-level lock. Exit `2` with a JSON busy-lock failure guarantees that the contending operation performed no mutation. Retain its owner metadata, wait for the prior pane/row/worktree lifecycle operation to finish, confirm its result, then retry the identical desired-state operation with the same report ID or adoption request identity. Never bypass the lock, change retry identity, edit registries concurrently, or start the next lifecycle mutation while the prior one is unresolved.

## Group/report/callback recovery

- **Missing, stale, or recycled callback:** the durable report is already pending. Keep the worker alive, inspect `amux report pending --group <id>`, and explicitly re-register the exact current coordinator with `amux callback register ...`. Never search for or guess another pane. A coordinator restart always requires registration of a new lease generation.
- **Busy composer:** production notification does not detect composer occupancy. Do not retry notification into a pane suspected or observed to contain draft text. The coordinator recovers directly from `report pending`/`report history` and acknowledges durable state. Retry the identical submission only after independently verifying that the exact registered pane is safe for input and a wake-up is still needed.
- **Failed send with a verified safe pane:** do not paste the report payload manually or infer delivery. Retry the identical `report submit` with the same report ID and unchanged binding/payload. Duplicate durable state is skipped while notification is retried.
- **Duplicate or reordered wake-up:** treat the token only as a hint to query `report pending`, `report history`, and `group show`. Durable state controls ordering and terminal non-regression; a late token cannot acknowledge, authorize, merge, or finish anything.
- **Conflicting report ID:** exit `2` means the ID is bound to another immutable request or payload. Do not choose a new ID to evade the conflict. Inspect history and resolve the discrepancy.
- **Coordinator restart:** group membership, reports, acknowledgement, authorization, and history survive. The old runtime lease fails closed. Re-register the verified new process/pane; do not reconstruct durable state from tmux.
- **Add-only label drift (member labels only):** a failed member add/reconcile retains local membership and exits `1`; retry add-only ensure later. Reconcile skips coordinator roles because coordinator identity is local-only. Reassigning a coordinator demotes the prior one to member, which reports `additive_ensure_required` drift `label_may_be_missing`; run `group reconcile` to add-only ensure that member label. Removal exits `0` locally but an existing Amp label may remain indefinitely, including after a labelled member is promoted to coordinator. Never use all-label replacement or claim exact external equality.
- **Drain-command mismatch:** if installed help lacks a command required by an existing group/report/callback/adoption drain, stop and report the exact compatibility blocker. Do not bootstrap a replacement, fall back to another binary, create or adopt a thread, infer membership, edit registries, or attach a provisioned/abandoned identity.

No recovery path may force-delete a branch, auto-release, infer finish from a late callback, or erase durable group history. Only an authorized `/amux` lifecycle or coordination operation may, after naming a concrete local/GitHub discrepancy, exhausting deterministic evidence, and separately establishing the exact relationship with durable/local/GitHub evidence, make one narrow query of that exact related thread. If that query fails, block rather than widening or chaining reads; report blocked and remain alive.
