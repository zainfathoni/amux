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

Exit `1` means mutation may have started. Inspect `successful`, `skipped`, and `failed`, then inspect config, tmux, Git worktrees, and remote thread state before retrying. Exit `2` means request/preflight rejection before mutation. A spawn operation marked indeterminate is terminal until identity is recovered; never change its stable idempotency key to force a duplicate creation.

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

Runner pin requires a locked Git worktree. Missing-worktree repair belongs to `amux runner reconcile --workdir <path>`, not launch. Reconcile fails closed on ambiguous Amp-owned PID markers. Never delete marker files, unlock a runner worktree, or remove runner configuration as part of worker finish.

## Mutation lock

All mutations and scheduled maintenance share one bounded machine-level lock. If JSON reports a busy lock, retain its owner metadata, wait or investigate the owning operation, and retry only after confirming the first operation's result. Never bypass the lock or edit registries concurrently.
