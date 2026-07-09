# amux command reference

This is disclosed reference for [`../SKILL.md`](../SKILL.md). Load it when exact syntax, session defaulting, side effects, or command semantics matter.

## Command forms

```sh
amux list [--status] [--active|--shelved] [workspace]
amux workspaces [--include-runners]
amux shelved [workspace]
amux doctor [workspace] [session]
amux launch [workspace] [session] [--dry-run]
amux --attach launch [workspace] [session]
amux --no-attach launch [workspace] [session]
amux pin <workspace> <window> <workdir> <thread-id-or-url>
amux pin-current <thread-id-or-url>
amux pin-current <workspace> <thread-id-or-url> [window] [workdir]
amux unpin <workspace> <window>
amux unpin-current [workspace]
amux park [workspace] <window>
amux park-current [workspace]
amux shelve-current [workspace] [thread-id-or-url]
amux shelve [workspace] <window> [session]
amux shelve --thread <thread-id-or-url> [--session <session>]
amux shelve --workspace <workspace> [--session <session>]
amux unshelve [workspace] <window>
amux unshelve --thread <thread-id-or-url>
amux unshelve --workspace <workspace>
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
amux runner park [workspace] <window> [session]
amux update [--dry-run]
```

Compatibility aliases: `store`/`store-current` for `pin`/`pin-current`, and `remove`/`remove-current` for `unpin`/`unpin-current`.

## Workspace and session defaults

When a workspace is explicitly provided and session is omitted, the tmux session defaults to the workspace name:

```sh
amux launch amux          # workspace amux, session amux
amux doctor amux          # workspace amux, session amux
amux runner launch amux   # workspace amux, session amux
amux spawn worker ~/Code/repo "prompt" amux
```

Pass an explicit session for older shared-session layouts:

```sh
amux launch mac Amp
amux shelve mac worker Amp
amux shelve --workspace mac --session Amp
amux teardown mac worker Amp
amux runner launch mac Amp
amux runner park mac worker Amp
```

No-arg `launch` and `doctor` still use the legacy workspace `mac` and tmux session `Amp` where that compatibility exists.

## Command semantics

### list, shelved, and doctor

- `amux list [workspace]` reads restore rows from local config only. It must stay instant and must not call Amp.
- `amux workspaces` reads local restore config only and prints unique workspace names sorted one per line. It does not call Amp or tmux and does not create missing config files. Runner-only workspace names are excluded by default; use `--include-runners` only when runner inventory is explicitly relevant.
- `amux list --status [workspace]` appends `status` after the original restore columns: `active`, `shelved`, `missing`, or `unknown` when Amp status cannot be read.
- `amux list --active [workspace]` shows only confirmed launchable rows.
- `amux list --shelved [workspace]` and `amux shelved [workspace]` show deferred rows. Filtered modes fail closed if Amp cannot confirm status.
- `amux doctor [workspace] [session]` verifies dependencies, configured workdirs, selected workspace rows, runner rows, live tmux drift in the selected tmux session, and restore rows whose remote Amp threads are confirmed archived or missing.

### launch

- Reads restore config, skips archived/shelved rows, and may create live local tmux/Amp windows for unshelved rows.
- Does not create, archive, unarchive, or delete remote Amp threads.
- `launch --dry-run` inspects restore actions without creating windows.
- Auto-attaches by default only when the tmux session already existed, no restore work was needed, and its live window set plus pane paths match the configured workspace.
- Cold restores and partial restores do not auto-attach.
- Use `--attach launch` to force attach or `--no-attach launch` to suppress auto-attach.
- If attach is requested from inside tmux, `amux` switches the current client to the target session. If tmux reports no terminal, `amux` opens the session through Omarchy's terminal launcher with direct Alacritty fallback.

### pin and unpin

- `pin` and `pin-current` mutate restore config only.
- `unpin` and `unpin-current` mutate restore config only; they do not stop local tmux/Amp windows and do not archive remote Amp threads.
- Use `pin-current` and `unpin-current` from inside tmux when possible. They default to workspace `mac` plus the invoking pane's tmux window name and pane path, using `$TMUX_PANE` when available rather than the currently focused tmux client.

### park

- `park` and `park-current` stop only the resolved live local tmux/Amp window after a delay.
- They preserve restore config rows and do not archive, delete, or hide the remote Amp thread.
- For **Park it**, use the atomic command, then verify it disappeared locally when needed:

  ```sh
  amux park-current
  amux list mac
  tmux list-windows -t Amp
  ps -eo pid,ppid,stat,args | rg 'amp threads continue T-' || true
  ```

If the row still appears in `amux list` and the thread still appears in Amp history after parking, that is expected.

### shelve and unshelve

- Use `shelve` when the user wants to defer work and hide it from the Amp sidebar while keeping it restorable in amux.
- `amux shelve <workspace> <window> [session]` targets one row.
- `amux shelve --thread <thread-id-or-url> [--session <session>]` targets one stored thread by ID/URL and searches all tmux sessions unless scoped.
- `amux shelve --workspace <workspace> [--session <session>]` targets all rows in a workspace.
- For workspace-based shelve targets, omit `[session]` only when the live tmux session has the same name as the workspace; otherwise pass the session.
- Shelving archives selected remote Amp thread(s), preserves restore config rows, and stops only verified matching live tmux windows.
- `shelve-current` pins or preserves the current window/path row, archives the identified current Amp thread, and stops the current local tmux/Amp window. It requires a supplied thread ID/URL unless `AMUX_THREAD_ID` is set.
- `unshelve` unarchives selected remote Amp thread(s) only; it preserves restore config rows and does not start tmux windows.
- `amux launch <workspace> [session]` skips shelved rows. Resume deferred work explicitly with `amux unshelve ...`, then `amux launch ...`.

### spawn

- Use `spawn` for a fresh interactive Amp session.
- It must use `amp threads new` plus `amp threads continue` inside tmux; do not use `amp -x` or piped stdin for this workflow.
- `spawn --mode <mode>` or `spawn -m <mode>` creates the new remote Amp thread with a specific Amp mode.
- `spawn --title-prefix <prefix>` renames the tmux window and new Amp thread with an issue/task prefix such as `#255 worker`.
- If a workspace is passed without a session, the spawned worker receives `AMUX_SESSION` set to the workspace name.
- `spawn --dry-run` validates inputs and checks live tmux window conflicts, but must not create or rename an Amp thread, mutate tmux, send keys, or update restore config.

### teardown

- No-arg `teardown` is only for an `amux spawn` worker with injected `AMUX_*` identity.
- It verifies identity against restore config and live tmux before archiving the matching remote Amp thread, removing the restore row, and stopping the matched local tmux window.
- If a restored worker lacks `AMUX_*` but its thread is in `amux list` and live in tmux, use `amux teardown --thread <thread-id-or-url> [--session <session>]`; it resolves and verifies the row and tmux window by thread before cleanup.
- For explicit `amux teardown <workspace> <window>`, omit `[session]` only when the live tmux session has the same name as the workspace; otherwise pass the session.
- If any identity, config, or tmux check is missing, mismatched, or ambiguous, teardown fails closed and should not archive or stop anything.

### prune-archived

- Removes only restore-config rows whose thread ID or URL is confirmed archived.
- Does not archive/delete remote threads and does not stop live tmux windows.
- If Amp cannot confirm archive state, or a thread is missing from both active and archived lists, it fails closed without changing config.

### runner

- `runner list` inspects runner config only.
- `runner pin` stores workspace/window/workdir intent in `runners.tsv`.
- `runner unpin` removes runner config.
- `runner launch` starts `amp --no-tui` in tmux from runner config.
- `runner park` stops only the verified live local runner window, using the workspace-named session unless a session is passed explicitly.
- Runner commands do not create, continue, archive, unarchive, or list remote Amp threads.

### update

- Use `update` for amux self-updates from a user-owned install path.
- `update --dry-run` previews the release asset without replacing the binary.
- `self-update` remains a compatibility alias for `update`.
- Without `--dry-run`, `update` fetches latest GitHub release metadata and replaces the current amux binary after checksum verification.

## Side-effect matrix

| Command | Restore config | Runner config | Live local tmux/Amp | Remote Amp thread state |
| --- | --- | --- | --- | --- |
| plain `list`, `workspaces`, `path`, `version` | inspect only | `workspaces --include-runners` inspects only | none | none |
| `list --status`, `list --active`, `list --shelved`, `shelved`, `doctor` | inspect only | inspect where relevant | inspect only | inspect Amp archive/missing status only; no mutation |
| `launch` | read | none | may create/attach windows | none; skips archived/shelved rows |
| `pin`, `pin-current` | mutate rows | none | none | none |
| `unpin`, `unpin-current` | mutate rows | none | none | none |
| `park`, `park-current` | preserve rows | none | stop verified window | none |
| `shelve-current`, `shelve` | preserve rows; current may pin/preserve | none | stop verified windows | archive/hide selected threads |
| `unshelve` | preserve rows | none | none | unarchive selected threads |
| `spawn` | store row | none | create/select window and send initial message | create thread, optionally set mode/title prefix |
| `teardown` | remove verified row | none | stop verified window | archive verified thread |
| `prune-archived` | remove confirmed archived rows | none | none | inspect only |
| `runner list` | none | inspect | none | none |
| `runner pin`, `runner unpin` | none | mutate | none | none |
| `runner launch` | none | read | may create runner windows | none |
| `runner park` | none | preserve | stop verified runner window | none |
| `update` (`self-update` alias) | none | none | replace current amux binary unless `--dry-run` | fetch latest GitHub release metadata; no Amp thread state |
