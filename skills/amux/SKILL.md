---
name: amux
description: "Manages Amp tmux workspace sessions with amux: spawn fresh interactive Amp threads, pin/unpin current windows in restore config, tear down spawned workers, and update restore config. Use when the user asks to add, pin, store, save, remember, restore, unpin, remove, spawn, tear down, or reset Amp/tmux sessions, current Amp sessions, thread IDs, or restored sessions. Also use for trigger phrases: 'Park it' means remove the current window from amux restore and close the local tmux/Amp session; 'Pin it' means pin the current window for restore."
---

# amux

Manage `~/.config/amp-tmux/workspaces.tsv` through `amux` instead of editing the TSV manually.

Keep the three side-effect domains distinct:

- **Restore config**: rows in `workspaces.tsv` that describe what should be restored later.
- **Live local tmux/Amp**: tmux sessions/windows and local Amp CLI processes running inside them.
- **Remote Amp thread state**: hosted Amp threads; `spawn` creates one and verified `teardown` archives one.

## Commands

```sh
amux list mac
amux doctor mac
amux launch mac Amp --dry-run
amux --attach launch mac Amp
amux --no-attach launch mac Amp
amux pin mac <window> <workdir> <thread-id-or-url>
amux pin-current <thread-id-or-url>
amux pin-current mac <thread-id-or-url> [window] [workdir]
amux unpin mac <window>
amux unpin-current [workspace]
amux park-current [workspace]
amux spawn [--mode <mode> | -m <mode>] <window> <workdir> <initial-message> [workspace] [session]
amux teardown
amux teardown --thread <thread-id-or-url> [--session <session>]
```

Use `pin-current` from inside a tmux/Amp thread when possible. It defaults to workspace `mac` plus the invoking pane's tmux window name and pane path, using `$TMUX_PANE` when available rather than the currently focused tmux client. `store-current` remains a compatibility alias.
Use `unpin-current` from inside tmux when the invoking pane's window should no longer be restored. `remove-current` remains a compatibility alias.
Use `spawn` for a fresh interactive Amp session. It must use `amp threads new` plus `amp threads continue` inside tmux; do not use `amp -x` or piped stdin for this workflow. Use `spawn --mode <mode>` or `spawn -m <mode>` when the user wants the new remote Amp thread created with a specific Amp mode.
Use `spawn --dry-run` to inspect a new-session plan safely. It validates inputs and checks live tmux window conflicts, but must not create an Amp thread, mutate tmux, send keys, or update the restore config.
Use no-arg `teardown` only from inside an `amux spawn` worker with injected `AMUX_*` identity. It verifies the identity against restore config and live tmux before archiving the matching remote Amp thread, removing the restore row, and stopping the matched local tmux window. If a restored worker lacks `AMUX_*` but its thread is in `amux list` and live in tmux, use `amux teardown --thread <thread-id-or-url> [--session <session>]` instead; it resolves and verifies the row and tmux window by thread before cleanup.
Use `doctor` before or after suspicious restore changes to verify dependencies, configured workdirs, selected workspace rows, and live tmux drift in the default `Amp` session. It compares config rows with `tmux list-panes`, reports configured windows that are not running, live windows that are not stored, and pane paths that differ from configured workdirs.
Launch auto-attaches by default only when the tmux session already existed, no restore work was needed, and its live window set plus pane paths match the configured workspace. Cold restores and partial restores do not auto-attach. Use `launch --dry-run` to inspect restore actions without creating windows, `--attach launch` to force attach, or `--no-attach launch` to suppress auto-attach. If attach is requested from inside tmux, `amux` switches the current client to the target session; if tmux reports there is no terminal, `amux` opens the session through Omarchy's terminal launcher with direct Alacritty fallback.

## Side-effect domains by command

- `list`, `path`, `version`, and `doctor`: inspect only; no restore-config, live-local, or remote-thread mutation.
- `launch`: reads restore config and may create live local tmux/Amp windows; it does not create or archive remote Amp threads.
- `pin` and `pin-current` (`store` and `store-current` aliases): mutate restore config only.
- `unpin` and `unpin-current` (`remove` and `remove-current` aliases): mutate restore config only; they do not stop local tmux/Amp windows and do not archive remote Amp threads.
- `park-current`: removes the current-window restore row and stops the current local tmux/Amp window after a delay; it does not archive or delete the remote Amp thread.
- `spawn`: creates a remote Amp thread, creates/selects a live local tmux window, submits the initial message, injects `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`, and stores the restore row.
- `teardown`: verifies `AMUX_*` identity, explicit workspace/window, or `--thread` restore/live-tmux agreement, then archives the verified remote Amp thread, removes the restore row, and stops the verified local tmux window.

## Trigger phrases

These phrases are user-level shorthand and should work from any project when this global skill is available.

- **Park it**: remove the current tmux window from amux restore config, then gracefully stop the current local tmux/Amp window/process. This does not archive or delete the remote Amp thread; it only stops the local tmux/Amp session and prevents restore. `amux park-current` schedules a delayed interrupt/EOF for the target pane, returns so the agent can send its final response, then force-closes the tmux window only if the graceful stop times out.
- **Pin it**: pin the current tmux window in amux restore config. Ask for the thread ID/URL if it is not available in context.

For **Park it**, use the atomic command, then verify it disappeared locally:

```sh
amux park-current
amux list mac
tmux list-windows -t Amp
ps -eo pid,ppid,stat,args | rg 'amp threads continue T-' || true
```

If the thread still appears in Amp history after parking, that is expected. Parking is not remote thread archival or deletion.

For **Pin it**, prefer:

```sh
amux pin-current <thread-id-or-url>
```

## Spawn a fresh interactive session

Use this when the user wants a fresh context window, a remote-started session, or an interactive reset.

```sh
amux spawn [--mode <mode> | -m <mode>] <window> <workdir> "<initial-message>"
amux list mac
```

The initial message is submitted via `tmux send-keys` into a normal interactive Amp window. The spawned process receives `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`; no-arg `amux teardown` depends on this identity. If the user keeps their amux config in a dotfiles or machine-restore repository, remind them to sync the changed `workspaces.tsv` there.

`spawn` refuses to overwrite an existing tmux window and validates inputs before creating a new Amp thread. If a spawn fails, verify whether a new remote thread, local tmux window, or restore row was created before retrying.

## Tear down a spawned worker

Use this only inside an Amp process that was created by `amux spawn` and has the injected `AMUX_*` variables.

```sh
amux teardown
```

If the worker was restored later and does not have `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, or `AMUX_THREAD_ID`, but you know its Amp thread ID/URL and the row is stored, use:

```sh
amux teardown --thread <thread-id-or-url> [--session <session>]
```

`teardown` is the explicit full-lifecycle cleanup command. It archives the verified thread, removes the matching restore-config row, and stops the verified local tmux window. If any identity, config, or tmux check is missing, mismatched, or ambiguous, it fails closed and should not archive or stop anything. Do not use `park-current` when the desired outcome is remote Amp thread archival; parking intentionally leaves remote thread history alone.

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
