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

Exit `1` means mutation may have started. Inspect `successful`, `skipped`, and `failed`, then inspect config, tmux, Git worktrees, and remote thread state before retrying. Exit `2` means request/preflight rejection before mutation. Never change an indeterminate spawn's stable idempotency key to force a duplicate creation. For the specific timeout reporting that the assignment was not found in the provisioned thread or one fresh receiver, first use read-only inspection to identify either the complete assignment in the exact provisioned thread or one unambiguous fresh active alternate receiver with the expected workdir while the provisioned thread is empty. Then rerun the complete identical spawn request with the same key and `--reconcile`. Reconciliation verifies the immutable request and exact assignment, rejects conflicting or externally started candidates, and completes the original operation against only the authoritative receiver without thread creation or message resubmission. Other indeterminate outcomes remain terminal.

`shelve` records intent before remote archive and local park. `unshelve` removes intent only after remote unarchive. Visible partial synchronization is retryable desired state, not a reason to roll back by hand.

## Replace a stale worker

Health first; `no-response` alone does not authorize replacement. After explicit approval:

1. Preserve the old remote thread unless archival was requested.
2. Preflight a new semantic window and stable key with explicit medium mode:

   ```sh
   amux --dry-run spawn --workspace <workspace> --window <replacement-slug> --workdir <path> --mode medium --message-file <prompt> --idempotency-key <stable-key>
   ```

3. Spawn and verify the new thread, worker row, tmux pane, workdir, and submitted assignment before removing the old local worker.
4. Use `amux worker remove --thread <old-id>` to stop/delete old local configuration without archiving. Use teardown only when archival is explicitly intended.
5. On interruption, report exact old/new thread, config, pane, and worktree state before any retry.

Substitute another mode only when the user explicitly requested it. Never infer a higher mode from replacement complexity or urgency.

## Runner safety

Runner pin requires a canonical existing directory; Git repository, worktree, and lock state are irrelevant. Missing-directory repair belongs to `amux runner reconcile --workdir <path>`, not launch. Reconcile fails closed on ambiguous Amp-owned PID markers. Never delete marker files or remove runner configuration as part of worker finish.

## Mutation lock

All mutations and scheduled maintenance share one bounded machine-level lock. Exit `2` with a JSON busy-lock failure guarantees that the contending operation performed no mutation. Retain its owner metadata, wait for the prior pane/row/worktree lifecycle operation to finish, confirm its result, then retry the identical desired-state operation with the same report ID or spawn key. Never bypass the lock, change retry identity, edit registries concurrently, or start the next lifecycle mutation while the prior one is unresolved.

## Group/report/callback recovery

- **Missing, stale, or recycled callback:** the durable report is already pending. Keep the worker alive, inspect `amux report pending --group <id>`, and explicitly re-register the exact current coordinator with `amux callback register ...`. Never search for or guess another pane. A coordinator restart always requires registration of a new lease generation.
- **Busy composer:** production notification does not detect composer occupancy. Do not retry notification into a pane suspected or observed to contain draft text. The coordinator recovers directly from `report pending`/`report history` and acknowledges durable state. Retry the identical submission only after independently verifying that the exact registered pane is safe for input and a wake-up is still needed.
- **Failed send with a verified safe pane:** do not paste the report payload manually or infer delivery. Retry the identical `report submit` with the same report ID and unchanged binding/payload. Duplicate durable state is skipped while notification is retried.
- **Duplicate or reordered wake-up:** treat the token only as a hint to query `report pending`, `report history`, and `group show`. Durable state controls ordering and terminal non-regression; a late token cannot acknowledge, authorize, merge, or finish anything.
- **Conflicting report ID:** exit `2` means the ID is bound to another immutable request or payload. Do not choose a new ID to evade the conflict. Inspect history and resolve the discrepancy.
- **Coordinator restart:** group membership, reports, acknowledgement, authorization, and history survive. The old runtime lease fails closed. Re-register the verified new process/pane; do not reconstruct durable state from tmux.
- **Add-only label drift:** a failed add/reconcile retains local membership and exits `1`; retry add-only ensure later. Removal exits `0` locally but the Amp label may remain indefinitely. Never use all-label replacement or claim exact external equality.
- **Bootstrap mismatch:** if installed help lacks `group`, `report`, `callback`, or spawn `--group`, use the exact bootstrap sequence in the coordinator workflow. Keep invoking the verified absolute binary path for every subsequent operation; do not fall back to stale bare `amux`. For an already-created worker, explicitly add the verified authoritative thread; do not respawn, infer membership, edit registries, or attach a provisioned/abandoned identity.

No recovery path may force-delete a branch, auto-release, infer finish from a late callback, or erase durable group history. Only an authorized `/amux` lifecycle or coordination operation may, after naming a concrete discrepancy, exhausting deterministic evidence, and establishing the exact relationship with durable/local/GitHub evidence, make one narrow query of that exact related thread. If that query fails, block rather than widening or chaining reads; report blocked and remain alive.
