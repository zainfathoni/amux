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

2. Prepare and preflight the replacement before stopping or unpinning anything. Prefer `--message-file` for a structured replacement prompt; use `--message-stdin` when another command generates the prompt. Keep positional `<initial-message>` as a short single-line fallback only. Validate the exact command with `--dry-run` first:

   ```sh
   amux --dry-run spawn [--title-prefix '<prefix>'] --message-file <replacement-prompt.md> <candidate-window> <workdir> <workspace> [session]
   ```

   Keep the replacement prompt's first sentence title-neutral and task-specific. Avoid starting with "This is a replacement worker..." because Amp may auto-title the new thread from that phrase. Put replacement context after the task sentence. Example prompt file:

   ```text
   Work on issue-236 backup pull host.

   This replaces old stuck thread T-.... Do not archive the old thread unless explicitly asked.

   Inspect the worktree and current issue/task context, verify whether any cleanup or follow-up remains, and report status. Keep changes minimal and only act if needed.
   ```

   If a prompt file is not practical, use the positional fallback with one single-line prompt:

   ```sh
   amux --dry-run spawn [--title-prefix '<prefix>'] <candidate-window> <workdir> "<single-line replacement prompt>" <workspace> [session]
   ```

3. Prefer a temporary-name replacement so the old live window stays available until the replacement is verified:

   ```sh
   amux spawn [--title-prefix '<prefix>'] --message-file <replacement-prompt.md> <window>-replacement <workdir> <workspace> [session]
   amux list <workspace>
   tmux list-panes -a -F '#{session_name}\t#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_pid}\t#{pane_start_command}' | rg '<new-thread-id>|<window>-replacement|<workdir>'
   amp threads export <new-thread-id> | head -80
   ```

   After the replacement thread, restore row, and live pane are verified, stop only the verified old local window if that is still desired. Do not archive the old remote thread unless the user explicitly requested archival:

   ```sh
   tmux kill-window -t '<session>:<old-window>'
   ```

   If the final window name matters, rename only after the old window is gone and the replacement is known-good:

   ```sh
   tmux rename-window -t '<session>:<window>-replacement' '<window>'
   amux unpin <workspace> <window>-replacement
   amux pin <workspace> <window> <workdir> <new-thread-id-or-url>
   amux list <workspace>
   ```

4. Use the same-name path only when a temporary window cannot work. Never combine kill and spawn in one shell command; do not run `tmux kill-window ... && amux spawn ...`. Checkpoint after each mutation so an interruption leaves a reportable state:

   a. Preflight the exact same-name spawn command before killing anything. If the old tmux window still exists, `--dry-run` may stop at the expected existing-window conflict; treat that only as confirmation that prompt/workdir/argument validation reached the tmux-conflict check. Do not continue on `initial-message`, workdir, workspace/session, or option validation errors.

   b. Remove only the stale restore row. Do not use `teardown` unless the user asked to archive the old thread too:

      ```sh
      amux unpin <workspace> <window>
      ```

   c. If the stale local tmux window still exists, stop only the verified local window after matching session, window, workdir, and start command. Do not stop similarly named windows in other tmux sessions:

      ```sh
      tmux kill-window -t '<session>:<window>'
      ```

   d. Re-check that the old row is gone and the old live window is gone. Then run the exact same-name `amux --dry-run spawn ...` again and require it to pass before spawning the replacement into the same workspace/session/workdir. If workspace and tmux session have the same name, omit the final session argument. Run `amux spawn` from any directory only if the installed `amux` is new enough to create the Amp thread in the target workdir; otherwise run it from `<workdir>` so Amp groups the thread under the correct project:

      ```sh
      amux version
      amux list <workspace>
      tmux list-windows -t <session>
      amux --dry-run spawn [--title-prefix '<prefix>'] --message-file <replacement-prompt.md> <window> <workdir> <workspace> [session]
      amux spawn [--title-prefix '<prefix>'] --message-file <replacement-prompt.md> <window> <workdir> <workspace> [session]
      ```

   e. Verify the replacement row and live pane:

      ```sh
      amux list <workspace>
      tmux list-panes -a -F '#{session_name}\t#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_pid}\t#{pane_start_command}' | rg '<new-thread-id>|<window>|<workdir>'
      amp threads export <new-thread-id> | head -80
      ```

      Confirm the exported thread's initial tree points at `<workdir>` when project grouping matters. If the generated Amp title is wrong, rename only the new thread:

      ```sh
      amp threads rename <new-thread-id> "<desired title>"
      ```

5. If the agent, shell, or Amp client is interrupted at any point, stop and report exact partial state before continuing. Use these checks, then report these four facts explicitly:

   ```sh
   amux list <workspace>
   amux doctor <workspace> [session]
   tmux list-windows -t <session>
   tmux list-panes -a -F '#{session_name}\t#{window_id}\t#{window_name}\t#{pane_current_path}\t#{pane_pid}\t#{pane_start_command}' | rg '<old-thread-id>|<new-thread-id>|<window>|<workdir>'
   amp threads export <old-thread-id> | head -20
   amp threads export <new-thread-id> | head -20
   ```

   Report whether the old live tmux window is still present or gone, whether a replacement thread was created, whether a replacement restore row was created, and whether the old remote thread is still active or was explicitly archived. Do not retry blindly or duplicate a replacement worker.

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
