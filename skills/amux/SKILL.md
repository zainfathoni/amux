---
name: amux
description: "Manages Amp tmux workspace sessions with amux: restore/inspect/park tmux workspaces, spawn fresh interactive Amp threads, pin/unpin current windows in restore config, and tear down spawned workers. Use for local tmux/Amp lifecycle and restore config, not as a replacement for Amp-native Agents Anywhere remote creation. Trigger phrases: 'Park it' closes the current local tmux/Amp session while keeping it restorable; 'Pin it' pins the current window for restore."
---

# amux

Manage `~/.config/amux/workspaces.tsv` and `~/.config/amux/runners.tsv` through `amux` instead of editing the TSV files manually. Legacy installs may still have `~/.config/amp-tmux`; use `amux migrate-config` to copy legacy files into the current directory without deleting the old files.

Keep the four side-effect domains distinct:

- **Restore config**: rows in `workspaces.tsv` that describe what should be restored later.
- **Runner config**: rows in `runners.tsv` that describe local `amp --no-tui` runner intent.
- **Live local tmux/Amp**: tmux sessions/windows and local Amp CLI processes running inside them.
- **Remote Amp thread state**: hosted Amp threads; `spawn` creates one and verified `teardown` archives one.

## Agents Anywhere decision rules

- If the user wants to create or control a new remote agent from ampcode.com, prefer Amp-native Agents Anywhere once a runner exists for the target machine and workdir.
- Use `amux` when the request is about local tmux workspace restore/lifecycle: list, doctor, launch, pin/unpin, park, spawn a local interactive worker, teardown, or prune stale restore rows.
- A runner is per machine and workdir. If the user asks whether remote creation is available for a repo, inspect/check for a live runner in that workdir; start one only when the user explicitly asks with `amux runner pin`/`amux runner launch` or an equivalent direct runner command.
- Do not fake runner rows as thread restore rows. Keep restore-config, runner-config, live-local tmux/Amp, and remote-thread mutations separate, and choose the command whose side effects exactly match the request.

## Commands

```sh
amux list mac
amux doctor mac
amux doctor mac Amp
amux launch mac Amp --dry-run
amux --attach launch mac Amp
amux --no-attach launch mac Amp
amux pin mac <window> <workdir> <thread-id-or-url>
amux pin-current <thread-id-or-url>
amux pin-current mac <thread-id-or-url> [window] [workdir]
amux unpin mac <window>
amux unpin-current [workspace]
amux park [workspace] <window>
amux park-current [workspace]
amux spawn [--mode <mode> | -m <mode>] <window> <workdir> <initial-message> [workspace] [session]
amux spawn --title-prefix <prefix> <window> <workdir> <initial-message> [workspace] [session]
amux teardown
amux teardown --thread <thread-id-or-url> [--session <session>]
amux teardown <workspace> <window> [session]
amux prune-archived [workspace]
amux runner list [workspace]
amux runner pin <workspace> <window> <workdir>
amux runner unpin <workspace> <window>
amux runner launch [workspace] [session]
amux runner park [workspace] <window>
```

Use `pin-current` from inside a tmux/Amp thread when possible. It defaults to workspace `mac` plus the invoking pane's tmux window name and pane path, using `$TMUX_PANE` when available rather than the currently focused tmux client. `store-current` remains a compatibility alias.
Use `unpin-current` from inside tmux when the invoking pane's window should no longer be restored. `remove-current` remains a compatibility alias.
Use `spawn` for a fresh interactive Amp session. It must use `amp threads new` plus `amp threads continue` inside tmux; do not use `amp -x` or piped stdin for this workflow. Use `spawn --mode <mode>` or `spawn -m <mode>` when the user wants the new remote Amp thread created with a specific Amp mode. Use `spawn --title-prefix <prefix>` when the user wants the tmux window and new Amp thread renamed with an issue/task prefix such as `#255 worker`.
Use `spawn --dry-run` to inspect a new-session plan safely. It validates inputs and checks live tmux window conflicts, but must not create or rename an Amp thread, mutate tmux, send keys, or update the restore config.
Use no-arg `teardown` only from inside an `amux spawn` worker with injected `AMUX_*` identity. It verifies the identity against restore config and live tmux before archiving the matching remote Amp thread, removing the restore row, and stopping the matched local tmux window. If a restored worker lacks `AMUX_*` but its thread is in `amux list` and live in tmux, use `amux teardown --thread <thread-id-or-url> [--session <session>]` instead; it resolves and verifies the row and tmux window by thread before cleanup.
Use `runner` subcommands when the target is a local Agents Anywhere runner: `runner pin` stores workspace/window/workdir intent in `runners.tsv`, `runner launch` starts `amp --no-tui` in tmux, `runner park` stops only the local runner window, and `runner unpin` removes runner config. Runner commands do not create, continue, archive, or list remote Amp threads.
Use `doctor` before or after suspicious restore/runner changes to verify dependencies, configured workdirs, selected workspace rows, runner rows, live tmux drift in the default `Amp` session, and restore rows whose remote Amp threads are confirmed archived or missing.
Use `prune-archived [workspace]` when stale restore rows point at Amp threads that were already archived elsewhere. It removes only restore-config rows whose thread ID or URL is confirmed archived; it does not archive/delete remote threads or stop live tmux windows. If Amp cannot confirm archive state, or a thread is missing from both active and archived lists, it fails closed without changing config.
Launch auto-attaches by default only when the tmux session already existed, no restore work was needed, and its live window set plus pane paths match the configured workspace. Cold restores and partial restores do not auto-attach. Use `launch --dry-run` to inspect restore actions without creating windows, `--attach launch` to force attach, or `--no-attach launch` to suppress auto-attach. If attach is requested from inside tmux, `amux` switches the current client to the target session; if tmux reports there is no terminal, `amux` opens the session through Omarchy's terminal launcher with direct Alacritty fallback.

## Side-effect domains by command

- `list`, `path`, `version`, and `doctor`: inspect only; no restore-config, live-local, or remote-thread mutation.
- `launch`: reads restore config and may create live local tmux/Amp windows; it does not create or archive remote Amp threads.
- `pin` and `pin-current` (`store` and `store-current` aliases): mutate restore config only.
- `unpin` and `unpin-current` (`remove` and `remove-current` aliases): mutate restore config only; they do not stop local tmux/Amp windows and do not archive remote Amp threads.
- `park` and `park-current`: stop only the resolved live local tmux/Amp window after a delay; they preserve restore config rows and do not archive or delete the remote Amp thread.
- `spawn`: creates a remote Amp thread, creates/selects a live local tmux window, submits the initial message, injects `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`, and stores the restore row.
- `teardown`: verifies `AMUX_*` identity, explicit workspace/window, or `--thread` restore/live-tmux agreement, then archives the verified remote Amp thread, removes the restore row, and stops the verified local tmux window.
- `prune-archived`: mutates restore config only, removing rows for already-archived Amp threads; it never archives/deletes threads and never stops tmux windows.
- `runner list`: inspects runner config only.
- `runner pin` and `runner unpin`: mutate runner config only.
- `runner launch`: reads runner config and may create live local `amp --no-tui` tmux windows; it does not create, continue, archive, or list remote Amp threads.
- `runner park`: stops only the resolved live local runner window; it preserves runner config and does not touch remote Amp thread state.

## Trigger phrases

These phrases are user-level shorthand and should work from any project when this global skill is available.

- **Park it**: gracefully stop the current local tmux/Amp window/process while keeping its restore config row. This does not archive or delete the remote Amp thread, and it does not prevent future restore. `amux park-current` schedules a delayed interrupt/EOF for the target pane, returns so the agent can send its final response, then force-closes the tmux window only if the graceful stop times out. Use `unpin-current` for config-only cleanup, or `teardown` for archive+unpin+stop cleanup.
- **Pin it**: pin the current tmux window in amux restore config. Ask for the thread ID/URL if it is not available in context.
- **Unpin it** / **forget this on restore**: remove only the current restore-config row with `amux unpin-current`; do not stop tmux and do not archive the Amp thread.
- **Teardown this worker** / **archive and clean this up**: use `amux teardown` only when the user explicitly wants full cleanup of the verified worker/thread. This archives the remote Amp thread, removes the row, and stops the local tmux window.
- **Restore my workspace**: use `amux launch mac Amp`, or `amux launch <workspace> <session>` if the user names a workspace/session.
- **Check amux** / **doctor amux**: use `amux doctor mac Amp` unless the user names another workspace/session.

For **Park it**, use the atomic command, then verify it disappeared locally:

```sh
amux park-current
amux list mac
tmux list-windows -t Amp
ps -eo pid,ppid,stat,args | rg 'amp threads continue T-' || true
```

If the row still appears in `amux list` and the thread still appears in Amp history after parking, that is expected. Parking is not restore-config cleanup or remote thread archival/deletion.

For **Pin it**, prefer:

```sh
amux pin-current <thread-id-or-url>
```

## Spawn a fresh interactive session

Use this when the user wants a fresh context window, a remote-started session, or an interactive reset.

```sh
amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] <window> <workdir> "<initial-message>"
amux list mac
```

The initial message is submitted via `tmux send-keys` into a normal interactive Amp window. The spawned process receives `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`; no-arg `amux teardown` depends on this identity. If the user keeps their amux config in a dotfiles or machine-restore repository, remind them to sync the changed `workspaces.tsv` there.

`spawn` refuses to overwrite an existing tmux window and validates inputs before creating a new Amp thread. If a spawn fails, verify whether a new remote thread, local tmux window, or restore row was created before retrying.

## Replace a stuck or misplaced worker

Use this when a thread is stuck loading, was created under the wrong Amp project, or needs a replacement while preserving the same worktree/task. Keep the local replacement workflow separate from remote archival: archive old remote threads only when the user explicitly asks.

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

4. Spawn the replacement into the same workspace/session/workdir. Run `amux spawn` from any directory only if the installed `amux` is new enough to create the Amp thread in the target workdir; otherwise run it from `<workdir>` so Amp groups the thread under the correct project:

   ```sh
   amux version
   amux spawn [--title-prefix '<prefix>'] <window> <workdir> "<replacement prompt>" <workspace> <session>
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

## Tear down a spawned worker

Use this only inside an Amp process that was created by `amux spawn` and has the injected `AMUX_*` variables.

```sh
amux teardown
```

If the worker was restored later and does not have `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, or `AMUX_THREAD_ID`, but you know its Amp thread ID/URL and the row is stored, use:

```sh
amux teardown --thread <thread-id-or-url> [--session <session>]
```

`teardown` is the explicit full-lifecycle cleanup command. It archives the verified thread, removes the matching restore-config row, and stops the verified local tmux window. If any identity, config, or tmux check is missing, mismatched, or ambiguous, it fails closed and should not archive or stop anything. Do not use `park-current` when the desired outcome is remote Amp thread archival or restore-config cleanup; parking intentionally leaves both remote thread history and restore config alone.

## Current-session workflow

Use this when the user asks to remember, save, pin, store, unpin, remove, or stop restoring the current Amp/tmux session.

1. Confirm the current tmux context:

   ```sh
   tmux display-message -p -t "$TMUX_PANE" 'window=#{window_name} path=#{pane_current_path}'
   ```

2. Pin the current window with the current Amp thread ID or URL:

   ```sh
   amux pin-current <thread-id-or-url>
   ```

3. Or unpin the current window from restore config without stopping it:

   ```sh
   amux unpin-current
   ```

4. Verify the row state and remind the user to sync intentional config changes into their dotfiles or machine-restore repository if they use one:

   ```sh
   amux list mac
   amux doctor mac
   ```

## Explicit workspace edits

1. List current rows:

   ```sh
   amux list mac
   ```

2. Pin or unpin a non-current window explicitly:

   ```sh
   amux pin mac <window> <workdir> <thread-id-or-url>
   amux unpin mac <window>
   ```

3. Verify and remind the user to sync intentional config changes into their dotfiles or machine-restore repository if they use one:

   ```sh
   amux list mac
   amux doctor mac
   ```

4. Commit and push the user's restore-config repository if the change is intentional and they have one.

## Safety

- Do not store secrets in window names, workdirs, or thread identifiers.
- Prefer thread IDs or `https://ampcode.com/threads/...` URLs only.
- Do not edit `workspaces.tsv` manually unless the helper cannot express the needed change.
- Before testing mutations, prefer a temp config with `--config "$tmp/workspaces.tsv"` so live restore rows are not changed accidentally.
- Do not run live `amux spawn`, `teardown`, `park-current`, `pin-current`/`store-current`, or `unpin-current`/`remove-current` against the default config unless the user asked to change that side-effect domain.
- If a thread/window looks missing, start with `amux doctor mac` and `amux list mac`. Prefer tmux window/pane metadata over `ps`; do not treat the tmux server command line as proof of a live Amp thread.
