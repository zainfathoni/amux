# amux workflows

This is disclosed reference for [`../SKILL.md`](../SKILL.md). Load only for the workflow branch the user is asking for.

## Spawn a fresh interactive session

Use this when the user wants a fresh context window, a remote-started session, or an interactive reset.

```sh
amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] <window> <workdir> "<initial-message>" [workspace] [session]
amux list <workspace>
```

The initial message is submitted via `tmux send-keys` into a normal interactive Amp window. The spawned process receives `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, `AMUX_THREAD_ID`, and `AMUX_WORKDIR`; no-arg `amux teardown` depends on this identity.

`spawn` refuses to overwrite an existing tmux window and validates inputs before creating a new Amp thread. If a spawn fails, verify whether a new remote thread, local tmux window, or restore row was created before retrying. If the user keeps amux config in dotfiles or a machine-restore repository, remind them to sync changed `workspaces.tsv` there.

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

3. Compare the issues for explicit and likely dependencies. Do not sprawl issues in parallel when any of these are true:

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

   The initial message should identify the issue URL/number, state that this worker owns only that issue/worktree, require dependency re-checking if it discovers overlap, and tell the worker to open a PR against `main` and report the PR URL plus blockers back to the originating thread.

4. Verify and capture the restore row/thread identity:

   ```sh
   amux list <workspace>
   git -C <worktree-path> status --short --branch
   ```

Report: dependency policy applied, any sequenced/skipped issues, each spawned worker's issue/title, Amp thread ID or URL, worktree path, branch, tmux window, title prefix, and the cleanup path (`/amux finish` after each worker PR is merged).

If any spawn partially succeeds, stop and inspect for a created remote thread, tmux window, restore row, branch, or worktree before retrying. Do not duplicate workers for the same issue.

## Tear down a spawned worker

Use this only inside an Amp process created by `amux spawn` with injected `AMUX_*` variables:

```sh
amux teardown
```

If the worker was restored later and does not have `AMUX_WORKSPACE`, `AMUX_SESSION`, `AMUX_WINDOW`, or `AMUX_THREAD_ID`, but you know its stored Amp thread ID/URL, use:

```sh
amux teardown --thread <thread-id-or-url> [--session <session>]
```

`teardown` is explicit full-lifecycle cleanup. It archives the verified thread, removes the matching restore-config row, and stops the verified local tmux window. Do not use `park-current` when the desired outcome is remote Amp thread archival or restore-config cleanup; parking intentionally leaves both remote thread history and restore config alone.

## Finish a merged worker

Use this when an `amux spawn` worker has opened a PR and the user wants the worker fully cleaned up after merge. GitHub merge state, release tags, git worktrees, and branch deletion are outside `amux teardown`; do those checks first, then run `amux teardown` as the final Amp/tmux cleanup step.

For unfinished or paused work, use the right smaller lifecycle instead:

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

4. Remove the worker worktree only after the PR is merged, `git status --short` in the worker worktree is empty, the worker branch is not currently checked out elsewhere, and the main worktree is updated. Inspect first:

   ```sh
   git worktree list
   git -C <worker-worktree> status --short --branch
   git -C <main-worktree> branch --contains <worker-branch>
   git worktree remove <worker-worktree>
   git worktree list
   ```

   If the worker worktree is dirty, has unpushed commits, or the PR is not merged, stop and ask the user. Do not force-remove a worktree as routine cleanup.

5. Delete branches only when confirmed merged, confirmed merged by the PR, or already deleted by the PR merge. Check local and remote state first:

   ```sh
   git -C <main-worktree> fetch --prune origin
   gh pr view <pr-number> --json merged,mergeCommit,headRefName,url
   git -C <main-worktree> branch --merged main | rg '^[ *]*<worker-branch>$' || true
   git -C <main-worktree> branch -d <worker-branch>
   git -C <main-worktree> ls-remote --exit-code --heads origin <worker-branch> >/dev/null && git -C <main-worktree> push origin --delete <worker-branch> || true
   ```

   Use `git branch -d`, not `-D`. If squash merge makes `branch -d` refuse, do not force-delete automatically; first verify the PR is merged, the branch has no unpushed work that must be preserved, and the user explicitly wants the local branch removed.

6. Run `amux teardown` last, from inside the spawned worker when possible:

   ```sh
   amux teardown
   ```

   If restored without `AMUX_*`, use the verified thread form instead:

   ```sh
   amux teardown --thread <thread-id-or-url> [--session <session>]
   ```

7. Report PR URL, merge status, release decision/result, worktree path removed, branch deletion result, and `amux teardown` result. If release, worktree removal, or branch deletion was intentionally skipped, say why.

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

4. Verify the row state and remind the user to sync intentional config changes into dotfiles or a machine-restore repository if they use one:

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

3. Verify and remind the user to sync intentional config changes into dotfiles or a machine-restore repository if they use one:

   ```sh
   amux list mac
   amux doctor mac
   ```

4. Commit and push the user's restore-config repository if the change is intentional and they have one.
