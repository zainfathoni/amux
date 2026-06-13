---
name: amux
description: "Manages Amp tmux workspace sessions with amux: spawn fresh interactive Amp threads, store/remove current windows, and update restore config. Use when the user asks to add, store, save, remember, restore, remove, spawn, or reset Amp/tmux sessions, current Amp sessions, thread IDs, or restored sessions. Also use for trigger phrases: 'Park it' means remove the current window from amux restore and close it; 'Pin it' means store the current window for restore."
---

# amux

Manage `~/.config/amp-tmux/workspaces.tsv` through `amux` instead of editing the TSV manually.

## Commands

```sh
amux list mac
amux doctor mac
amux launch mac Amp --dry-run --no-attach
amux store mac <window> <workdir> <thread-id-or-url>
amux store-current <thread-id-or-url>
amux store-current mac <thread-id-or-url> [window] [workdir]
amux remove mac <window>
amux remove-current [workspace]
amux spawn <window> <workdir> <initial-message> [workspace] [session]
```

Use `store-current` from inside a tmux/Amp thread when possible. It defaults to workspace `mac` plus the current tmux window name and pane path.
Use `remove-current` from inside tmux when the current window should no longer be restored.
Use `spawn` for a fresh interactive Amp session. It must use `amp threads new` plus `amp threads continue` inside tmux; do not use `amp -x` or piped stdin for this workflow.
Use `doctor` before or after suspicious restore changes to verify dependencies, configured workdirs, selected workspace rows, and current tmux queryability when inside tmux.
Use `launch --dry-run --no-attach` to inspect restore actions without creating or attaching windows.

## Trigger phrases

These phrases are user-level shorthand and should work from any project when this global skill is available.

- **Park it**: remove the current tmux window from amux restore config, then close the current tmux window.
- **Pin it**: store the current tmux window in amux restore config. Ask for the thread ID/URL if it is not available in context.

For **Park it**, run the removal before closing the window:

```sh
amux remove-current
tmux kill-window
```

For **Pin it**, prefer:

```sh
amux store-current <thread-id-or-url>
```

## Spawn a fresh interactive session

Use this when the user wants a fresh context window, a remote-started session, or an interactive reset.

```sh
amux spawn <window> <workdir> "<initial-message>"
amux list mac
```

The initial message is submitted via `tmux send-keys` into a normal interactive Amp window. If the live config changed intentionally, sync it into `omarchy-home`, then commit and push that repo.

`spawn` refuses to overwrite an existing tmux window and validates inputs before creating a new Amp thread. If a spawn fails, verify whether a new thread or window was created before retrying.

## Current-session workflow

Use this when the user asks to remember, save, store, remove, or stop restoring the current Amp/tmux session.

1. Confirm the current tmux context:

   ```sh
   tmux display-message -p 'window=#{window_name} path=#{pane_current_path}'
   ```

2. Store the current window with the current Amp thread ID or URL:

   ```sh
   amux store-current <thread-id-or-url>
   ```

3. Or remove the current window from restore config:

   ```sh
   amux remove-current
   ```

4. Verify the row state and sync intentional config changes:

   ```sh
   amux list mac
   amux doctor mac
   ~/Code/omarchy-home/scripts/snapshot-live.sh --apply home
   git -C ~/Code/omarchy-home diff -- home/.config/amp-tmux/workspaces.tsv
   ```

## Explicit workspace edits

1. List current rows:

   ```sh
   amux list mac
   ```

2. Store or remove a non-current window explicitly:

   ```sh
   amux store mac <window> <workdir> <thread-id-or-url>
   amux remove mac <window>
   ```

3. Verify and sync intentional config changes:

   ```sh
   amux list mac
   amux doctor mac
   ~/Code/omarchy-home/scripts/snapshot-live.sh --apply home
   git -C ~/Code/omarchy-home diff -- home/.config/amp-tmux/workspaces.tsv
   ```

4. Commit and push `~/Code/omarchy-home` if the change is intentional.

## Safety

- Do not store secrets in window names, workdirs, or thread identifiers.
- Prefer thread IDs or `https://ampcode.com/threads/...` URLs only.
- Do not edit `workspaces.tsv` manually unless the helper cannot express the needed change.
- Before testing mutations, prefer a temp config with `--config "$tmp/workspaces.tsv"` so live restore rows are not changed accidentally.
- Do not run live `amux spawn`, `store-current`, or `remove-current` against the default config unless the user asked to change the restore state.
- If a thread/window looks missing, compare all three sources before changing anything: `amux list mac`, `tmux list-windows -t Amp`, and `ps -eo pid,ppid,stat,args | rg 'amp threads continue T-'`.
