---
name: amux
description: "Manages Amp tmux workspace sessions with amux: restore/inspect/park tmux workspaces, spawn fresh interactive Amp threads, pin/unpin current windows in restore config, fan out independent issue workers with skill-only /amux sprawl orchestration, and tear down spawned workers. Use for local tmux/Amp lifecycle and restore config, not as a replacement for Amp-native Agents Anywhere remote creation. Trigger phrases: 'Park it' closes the current local tmux/Amp session while keeping it restorable; 'Pin it' pins the current window for restore."
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
amux list [--status] [--active|--shelved] mac
amux shelved [workspace]
amux doctor mac
amux doctor mac Amp
amux launch mac --dry-run
amux --attach launch mac Amp
amux --no-attach launch mac Amp
amux pin mac <window> <workdir> <thread-id-or-url>
amux pin-current <thread-id-or-url>
amux pin-current mac <thread-id-or-url> [window] [workdir]
amux unpin mac <window>
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
amux runner park [workspace] <window>
amux update [--dry-run]
```

Compatibility decision: keep workspace-named sessions when a workspace is explicitly provided and the session is omitted. For `launch`, `doctor`, and `runner launch`, `amux <command> amux` uses workspace/session `amux`. For `spawn`, the optional trailing workspace does the same: `amux spawn worker ~/Code/repo "prompt" amux`. For workspace-based `shelve`, use `amux shelve amux worker` or `amux shelve --workspace amux`; for explicit `teardown`, use `amux teardown amux worker`. Pass an explicit session for older shared-session layouts, such as `amux launch mac Amp`, `amux shelve mac worker Amp`, `amux shelve --workspace mac --session Amp`, `amux teardown mac worker Amp`, or `amux runner launch mac Amp`. No-arg launch/doctor still use the legacy `mac` workspace and `Amp` tmux session.

Use `pin-current` from inside a tmux/Amp thread when possible. It defaults to workspace `mac` plus the invoking pane's tmux window name and pane path, using `$TMUX_PANE` when available rather than the currently focused tmux client. `store-current` remains a compatibility alias.
Use `unpin-current` from inside tmux when the invoking pane's window should no longer be restored. `remove-current` remains a compatibility alias.
Use `spawn` for a fresh interactive Amp session. It must use `amp threads new` plus `amp threads continue` inside tmux; do not use `amp -x` or piped stdin for this workflow. Use `spawn --mode <mode>` or `spawn -m <mode>` when the user wants the new remote Amp thread created with a specific Amp mode. Use `spawn --title-prefix <prefix>` when the user wants the tmux window and new Amp thread renamed with an issue/task prefix such as `#255 worker`. If a workspace is passed without a session, the spawned worker receives `AMUX_SESSION` set to the workspace name.
Use `spawn --dry-run` to inspect a new-session plan safely. It validates inputs and checks live tmux window conflicts, but must not create or rename an Amp thread, mutate tmux, send keys, or update the restore config.
Use `list [workspace]` to inspect restore rows from local config only; it must stay instant and should not call Amp. Use `amux list --status [workspace]` when remote thread status is needed; it appends `status` after the original restore columns: `active`, `shelved`, `missing`, or `unknown` when Amp status cannot be read. Use `amux list --active [workspace]` for only confirmed launchable rows, `amux list --shelved [workspace]` or `amux shelved [workspace]` for deferred rows. Filtered list commands fail closed if Amp cannot confirm status.
Use `shelve` when the user wants to defer work and hide it from the Amp sidebar while keeping it restorable in amux. `amux shelve <workspace> <window> [session]` targets one row, `amux shelve --thread <thread-id-or-url> [--session <session>]` targets one stored thread by ID/URL and searches all tmux sessions unless scoped, and `amux shelve --workspace <workspace> [--session <session>]` targets all rows in a workspace. For workspace-based shelve targets, omit `[session]` only when the live tmux session has the same name as the workspace; otherwise pass the session. Shelving archives the remote Amp thread(s), preserves restore config rows, and stops only verified matching live tmux windows. `amux launch <workspace> [session]` skips shelved rows; use `amux unshelve <workspace> <window>`, `amux unshelve --thread <thread-id-or-url>`, or `amux unshelve --workspace <workspace>` explicitly before launching deferred work again.
Use no-arg `teardown` only from inside an `amux spawn` worker with injected `AMUX_*` identity. It verifies the identity against restore config and live tmux before archiving the matching remote Amp thread, removing the restore row, and stopping the matched local tmux window. If a restored worker lacks `AMUX_*` but its thread is in `amux list` and live in tmux, use `amux teardown --thread <thread-id-or-url> [--session <session>]` instead; it resolves and verifies the row and tmux window by thread before cleanup. For explicit `amux teardown <workspace> <window>`, omit `[session]` only when the live tmux session has the same name as the workspace; otherwise pass the session.
Use `runner` subcommands when the target is a local Agents Anywhere runner: `runner pin` stores workspace/window/workdir intent in `runners.tsv`, `runner launch` starts `amp --no-tui` in tmux, `runner park` stops only the local runner window, and `runner unpin` removes runner config. Runner commands do not create, continue, archive, or list remote Amp threads.
Use `doctor` before or after suspicious restore/runner changes to verify dependencies, configured workdirs, selected workspace rows, runner rows, live tmux drift in the selected tmux session, and restore rows whose remote Amp threads are confirmed archived or missing.
Use `prune-archived [workspace]` when stale restore rows point at Amp threads that were already archived elsewhere. It removes only restore-config rows whose thread ID or URL is confirmed archived; it does not archive/delete remote threads or stop live tmux windows. If Amp cannot confirm archive state, or a thread is missing from both active and archived lists, it fails closed without changing config.
Use `update` for amux self-updates from a user-owned install path; `update --dry-run` previews the release asset without replacing the binary. `self-update` remains a compatibility alias for `update`.
Launch auto-attaches by default only when the tmux session already existed, no restore work was needed, and its live window set plus pane paths match the configured workspace. Cold restores and partial restores do not auto-attach. Use `launch --dry-run` to inspect restore actions without creating windows, `--attach launch` to force attach, or `--no-attach launch` to suppress auto-attach. If attach is requested from inside tmux, `amux` switches the current client to the target session; if tmux reports there is no terminal, `amux` opens the session through Omarchy's terminal launcher with direct Alacritty fallback.

## Side-effect domains by command

- `list`, `shelved`, `path`, `version`, and `doctor`: inspect only; no restore-config, live-local, or remote-thread mutation. Plain `list` is local-only. `list --status`, `list --active`, `list --shelved`, and `shelved` inspect remote Amp archive state to show/filter status and use `unknown` only for unfiltered status output when Amp status is unavailable.
- `launch`: reads restore config, skips archived/shelved rows, and may create live local tmux/Amp windows for unshelved rows; it does not create, archive, or unarchive remote Amp threads.
- `pin` and `pin-current` (`store` and `store-current` aliases): mutate restore config only.
- `unpin` and `unpin-current` (`remove` and `remove-current` aliases): mutate restore config only; they do not stop local tmux/Amp windows and do not archive remote Amp threads.
- `park` and `park-current`: stop only the resolved live local tmux/Amp window after a delay; they preserve restore config rows and do not archive or delete the remote Amp thread.
- `shelve-current`: from the target tmux pane, pin or preserve the current window/path row, archive the identified current Amp thread, and stop the current local tmux/Amp window. It requires a supplied thread ID/URL unless `AMUX_THREAD_ID` is set.
- `shelve`: archives selected remote Amp thread(s), preserves restore config rows, and stops only verified matching live local tmux/Amp windows.
- `unshelve`: unarchives selected remote Amp thread(s) only; it preserves restore config rows and does not start tmux windows.
- `spawn`: creates a remote Amp thread, creates/selects a live local tmux window, submits the initial message, injects `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`, and stores the restore row.
- `teardown`: verifies `AMUX_*` identity, explicit workspace/window, or `--thread` restore/live-tmux agreement, then archives the verified remote Amp thread, removes the restore row, and stops the verified local tmux window.
- `prune-archived`: mutates restore config only, removing rows for already-archived Amp threads; it never archives/deletes threads and never stops tmux windows.
- `runner list`: inspects runner config only.
- `runner pin` and `runner unpin`: mutate runner config only.
- `runner launch`: reads runner config and may create live local `amp --no-tui` tmux windows; it does not create, continue, archive, or list remote Amp threads.
- `runner park`: stops only the resolved live local runner window; it preserves runner config and does not touch remote Amp thread state.
- `update` (`self-update` alias): fetches the latest GitHub release metadata and, unless `--dry-run` is set, replaces the current amux binary after checksum verification.

## Trigger phrases

These phrases are user-level shorthand and should work from any project when this global skill is available.

- **Park it**: gracefully stop the current local tmux/Amp window/process while keeping its restore config row. This does not archive or delete the remote Amp thread, and it does not prevent future restore. `amux park-current` schedules a delayed interrupt/EOF for the target pane, returns so the agent can send its final response, then force-closes the tmux window only if the graceful stop times out. Use `unpin-current` for config-only cleanup, or `teardown` for archive+unpin+stop cleanup.
- **Pin it**: pin the current tmux window in amux restore config. Ask for the thread ID/URL if it is not available in context.
- **Unpin it** / **forget this on restore**: remove only the current restore-config row with `amux unpin-current`; do not stop tmux and do not archive the Amp thread.
- **Shelve this** / **defer this workspace** / **hide it for now**: when acting from the pane to defer, use `amux shelve-current [workspace] <thread-id-or-url>` (or omit the thread only if `AMUX_THREAD_ID` is set). It pins/preserves the current row, archives/hides the identified remote thread, and stops the current tmux window. For already-pinned work, use `amux shelve --thread <thread-id-or-url>` for a single known thread, `amux shelve <workspace> <window> [session]` for a named row, or `amux shelve --workspace <workspace> [--session <session>]` for all rows in a workspace. If `shelve` reports no restore row for a live window, do not use `park-current` as a substitute; use `shelve-current` or `pin-current` with the thread ID. Future resume is explicit: `amux unshelve ...`, then `amux launch ...`.
- **Show shelved work** / **list deferred work**: use `amux shelved [workspace]` or `amux list --shelved [workspace]`; use `amux list --active [workspace]` to show only launchable rows.
- **Unshelve this** / **resume deferred work**: use `amux unshelve` with the same target shape as `shelve`, then `amux launch <workspace> [session]` if live tmux windows should be restored. Do not rely on `launch` alone to unarchive shelved threads.
- **/amux sprawl #12 #34 ...** / **sprawl these issues**: skill-only issue orchestration for fanning out independent issues into separate worktrees and `amux spawn` workers. This is not an `amux` CLI command. Inspect every issue and dependency first; do not spawn dependent, ordered, or likely-conflicting work in parallel. For accepted independent issues, create issue-scoped worktrees from current `origin/main`, spawn one worker per worktree with an issue title prefix, pin restore rows through `spawn`, and report thread IDs, worktree paths, and branches.
- **Teardown this worker** / **archive and clean this up**: use `amux teardown` only when the user explicitly wants full cleanup of the verified worker/thread. This archives the remote Amp thread, removes the row, and stops the local tmux window.
- **/amux finish** / **Finish this worker after merge** / **post-merge cleanup**: do the GitHub/git lifecycle first, then use `amux teardown` last. This is not the same as park or shelve: parking preserves the restore row and active remote thread; shelving archives the remote thread but preserves the restore row for later; post-merge finish verifies merge/release/worktree/branch cleanup before final amux teardown.
- **Restore my workspace**: use `amux launch` for the legacy default workspace/session, `amux launch <workspace>` for the same-named tmux session, or `amux launch <workspace> <session>` for an older shared-session layout.
- **Check amux** / **doctor amux**: use `amux doctor` for the legacy default workspace/session, `amux doctor <workspace>` for the same-named tmux session, or `amux doctor <workspace> <session>` for an older shared-session layout.

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

## Sprawl independent issue workers

Use this when the user asks for `/amux sprawl #12 #34 ...`, asks to sprawl issues, or asks to fan out several issue-scoped workers. Sprawl is skill orchestration around `gh`, `git worktree`, and `amux spawn`; it is not an `amux` CLI command and should not be represented in command help.

Dependency inspection is mandatory before creating any branch, worktree, tmux window, restore row, or remote Amp thread:

1. Sync the base view of `main` without mutating unrelated worktrees:

   ```sh
   git fetch origin main
   ```

2. Read every requested issue before deciding parallelism:

   ```sh
   gh issue view <issue> --json number,title,body,comments,labels,assignees,url
   ```

3. Compare the issues for explicit and likely dependencies. Do **not** sprawl issues in parallel when any of these are true:

   - one issue explicitly blocks, depends on, or must follow another issue
   - one issue changes APIs, types, commands, config, lifecycle terms, docs, or generated artifacts that another issue consumes
   - one issue is a prerequisite migration/refactor for another
   - the issues are likely to edit the same high-churn files or product surface in conflicting ways
   - the user requests ordered work

   When uncertain, prefer sequencing or ask the user which issue should go first. If only a subset is independent, sprawl only that subset and report the deferred dependency chain.

For each accepted independent issue:

1. Choose unique names that include the issue number, for example branch `feature/issue-123-short-slug`, worktree `../<repo>-issue-123`, tmux window `issue-123`, and title prefix `#123`.
2. Create the issue worktree from current `origin/main`:

   ```sh
   git worktree add -b <branch> <worktree-path> origin/main
   ```

3. Spawn one interactive worker for that worktree, using an issue prefix so the tmux window, restore row, `AMUX_WINDOW`, and Amp thread title are recognizable:

   ```sh
   amux spawn --title-prefix '#<issue>' <window> <worktree-path> "<initial-message>" <workspace> [session]
   ```

   The initial message should identify the issue URL/number, state that this worker owns only that issue/worktree, require dependency re-checking if it discovers overlap, and tell the worker to open a PR against `main` and report the PR URL plus any blockers back to the originating thread.

4. Verify and capture the restore row/thread identity:

   ```sh
   amux list <workspace>
   git -C <worktree-path> status --short --branch
   ```

Report the sprawl result back to the originating thread with:

- the dependency policy applied, including any issues intentionally sequenced or skipped
- for each spawned worker: issue number/title, Amp thread ID or URL, worktree path, branch, tmux window, and title prefix
- the cleanup path: after each worker PR is merged, use `/amux finish` / **Finish a merged worker** so GitHub/git cleanup happens before final `amux teardown`

If any spawn partially succeeds, stop and inspect for a created remote thread, tmux window, restore row, branch, or worktree before retrying. Do not duplicate workers for the same issue.

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

4. Spawn the replacement into the same workspace/session/workdir. If the workspace and tmux session have the same name, omit the final session argument. Run `amux spawn` from any directory only if the installed `amux` is new enough to create the Amp thread in the target workdir; otherwise run it from `<workdir>` so Amp groups the thread under the correct project:

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

## Finish a merged worker

Use this when an `amux spawn` worker has opened a PR and the user wants the worker fully cleaned up after merge. Keep the ownership boundary explicit: GitHub merge state, release tags, git worktrees, and branch deletion are outside `amux teardown`. Do those checks first, then run `amux teardown` as the final Amp/tmux cleanup step.

Do not skip directly to `amux teardown` unless the user only asked to archive the worker thread, remove its restore row, and stop its local tmux window. For unfinished or paused work, use the right smaller lifecycle instead:

- **Park** (`amux park-current`): stop only the live local tmux/Amp process; keep the restore row and active remote Amp thread.
- **Shelve** (`amux shelve ...`): archive/hide deferred remote Amp thread(s), stop verified local windows, and keep restore rows for explicit `unshelve` later.
- **Teardown** (`amux teardown ...`): archive the verified remote thread, remove the restore row, and stop the verified local window; it does not merge PRs, release, remove worktrees, or delete branches.

Recommended post-merge sequence:

1. Identify the worker PR, branch, worktree, and thread from the worker worktree:

   ```sh
   gh pr status
   gh pr view <pr-number> --json number,state,merged,mergeCommit,headRefName,headRepositoryOwner,url
   git status --short --branch
   git rev-parse --show-toplevel
   git branch --show-current
   amux list <workspace>
   ```

2. Merge only when requested and after normal review/tests are complete. Prefer the repository's usual merge button or `gh pr merge <pr-number> --squash --delete-branch`/`--merge`/`--rebase` as appropriate. Afterward, verify `merged: true`:

   ```sh
   gh pr view <pr-number> --json merged,mergeCommit,headRefName,url
   ```

3. If a release is expected, make it an explicit choice (`patch`, `minor`, a concrete tag, or `none`). Before tagging, verify the release source and that the tag does not already exist:

   ```sh
   git -C <main-worktree> fetch --tags origin
   git -C <main-worktree> switch main
   git -C <main-worktree> pull --ff-only origin main
   git -C <main-worktree> status --short --branch
   git -C <main-worktree> tag --list 'v*'
   git -C <main-worktree> rev-parse <new-tag> >/dev/null 2>&1 && echo "tag exists" || true
   ```

   Create and push a release tag only after the user confirms the version/tag:

   ```sh
   git -C <main-worktree> tag -a <new-tag> -m "<new-tag>"
   git -C <main-worktree> push origin <new-tag>
   gh run list --workflow Release --limit 5
   ```

   If no release is needed, record that decision and continue.

4. Remove the worker worktree only after all checks are true: the PR is merged, `git status --short` in the worker worktree is empty, the worker branch is not currently checked out anywhere else, and the main worktree is updated. Inspect first, then remove the exact path:

   ```sh
   git worktree list
   git -C <worker-worktree> status --short --branch
   git -C <main-worktree> branch --contains <worker-branch>
   git worktree remove <worker-worktree>
   git worktree list
   ```

   If the worker worktree is dirty, has unpushed commits, or the PR is not merged, stop and ask the user. Do not force-remove a worktree as routine cleanup.

5. Delete branches only when they are confirmed merged, confirmed merged by the PR, or already deleted by the PR merge. Check both local and remote state first:

   ```sh
   git -C <main-worktree> fetch --prune origin
   gh pr view <pr-number> --json merged,mergeCommit,headRefName,url
   git -C <main-worktree> branch --merged main | rg '^[ *]*<worker-branch>$' || true
   git -C <main-worktree> branch -d <worker-branch>
   git -C <main-worktree> ls-remote --exit-code --heads origin <worker-branch> >/dev/null && git -C <main-worktree> push origin --delete <worker-branch> || true
   ```

   Use `git branch -d`, not `-D`, for normal cleanup. If the PR was squash-merged and `branch -d` refuses because the exact commits are not ancestors of `main`, do not force-delete automatically; first verify the PR is merged, the branch has no unpushed work that must be preserved, and the user explicitly wants the local branch removed. Delete the remote branch only if it belongs to the worker PR and the user/repo policy allows it; `gh pr merge --delete-branch` may have already handled it.

6. Run `amux teardown` last, from inside the spawned worker when possible:

   ```sh
   amux teardown
   ```

   If the worker was restored without `AMUX_*`, use the verified thread form instead:

   ```sh
   amux teardown --thread <thread-id-or-url> [--session <session>]
   ```

7. Report the PR URL, merge status, release decision/result, worktree path removed, branch deletion result, and the `amux teardown` result back to the originating thread. If release, worktree removal, or branch deletion was intentionally skipped, say why.

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
