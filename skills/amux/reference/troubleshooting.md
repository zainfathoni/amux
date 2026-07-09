# amux troubleshooting and safety checks

This is disclosed reference for [`../SKILL.md`](../SKILL.md). Load it when a worker is stuck/misplaced, a mutation partly succeeded, or safety verification matters.

## Replace a stuck or misplaced worker

Use this when a thread is stuck loading, was created under the wrong Amp project, or needs a replacement while preserving the same worktree/task. Keep local replacement separate from remote archival: archive old remote threads only when the user explicitly asks.

1. Identify the exact restore row, tmux session/window, workdir, and old thread:

   ```sh
   amux list <workspace>
   tmux list-panes -a -F '#{session_name}\t#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_pid}\t#{pane_start_command}' | rg '<old-thread-id>|<window>|<workdir>'
   ps -eo pid,ppid,stat,args | rg '<old-thread-id>|amp threads archive' || true
   git -C <workdir> status --short --branch
   ```

2. Remove only the stale restore row. Do not use `teardown` unless the user asked to archive the old thread too:

   ```sh
   amux unpin <workspace> <window>
   ```

3. If the stale local tmux window still exists, stop only the verified local window after matching session, window, workdir, and start command. Do not stop similarly named windows in other tmux sessions:

   ```sh
   tmux kill-window -t '<session>:<window>'
   ```

4. Spawn the replacement into the same workspace/session/workdir. If workspace and tmux session have the same name, omit the final session argument. Run `amux spawn` from any directory only if the installed `amux` is new enough to create the Amp thread in the target workdir; otherwise run it from `<workdir>` so Amp groups the thread under the correct project:

   ```sh
   amux version
   amux spawn [--title-prefix '<prefix>'] <window> <workdir> "<replacement prompt>" <workspace> [session]
   ```

   Keep the replacement prompt's first sentence title-neutral and task-specific. Avoid starting with "This is a replacement worker..." because Amp may auto-title the new thread from that phrase. Put replacement context in the second sentence/paragraph. Example:

   ```text
   Work on issue-236 backup pull host.

   This replaces old stuck thread T-... . Do not archive the old thread unless explicitly asked. Inspect the worktree and current issue/task context, verify whether any cleanup or follow-up remains, and report status. Keep changes minimal and only act if needed.
   ```

   If the generated Amp title is still wrong, rename only the new thread:

   ```sh
   amp threads rename <new-thread-id> "<desired title>"
   ```

5. Verify the replacement row and live pane:

   ```sh
   amux list <workspace>
   tmux list-panes -a -F '#{session_name}\t#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_pid}\t#{pane_start_command}' | rg '<new-thread-id>|<window>|<workdir>'
   amp threads export <new-thread-id> | head -80
   ```

Confirm the exported thread's initial tree points at `<workdir>` when project grouping matters.

## Partial-success checks before retrying

If `spawn`, `shelve`, `teardown`, `prune-archived`, or a worktree orchestration step fails, inspect for side effects before retrying. Do not duplicate remote threads, restore rows, tmux windows, branches, or worktrees.

Useful checks:

```sh
amux list <workspace>
amux doctor <workspace> [session]
tmux list-windows -t <session>
tmux list-panes -a -F '#{session_name}\t#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_pid}\t#{pane_start_command}' | rg '<thread-id>|<window>|<workdir>'
git worktree list
git -C <workdir> status --short --branch
```

Use `amp threads export <thread-id> | head -80` when project grouping or thread identity must be verified.

## Mutation safety

- Before testing mutations, prefer a temp config with `--config "$tmp/workspaces.tsv"` so live restore rows are not changed accidentally.
- Do not run live `amux spawn`, `teardown`, `park-current`, `pin-current`/`store-current`, or `unpin-current`/`remove-current` against the default config unless the user asked to change that side-effect domain.
- For missing-looking threads/windows, start with `amux doctor <workspace>` and `amux list <workspace>`.
- Prefer tmux window/pane metadata over `ps`; do not treat the tmux server command line as proof of a live Amp thread.
- Use `prune-archived` only for restore rows whose remote Amp threads are confirmed archived; if Amp cannot confirm archive state, it must fail closed.
