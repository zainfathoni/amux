# amux workflows

These are skill workflows. Only commands beginning with literal `amux` are CLI commands; `/amux health`, `/amux sprawl`, and `/amux finish` are orchestration labels.

## Spawn a fresh worker

Preflight, then create one interactive worker. Prefer a message file for structured assignments.

```sh
amux --dry-run spawn --workspace <workspace> --window <semantic-slug> --workdir <path> --mode medium --message-file <prompt> --idempotency-key <stable-key>
amux spawn --workspace <workspace> --window <semantic-slug> --workdir <path> --mode medium --message-file <prompt> --idempotency-key <stable-key>
amux worker list --thread <thread-id>
```

Every skill-driven spawn MUST pass `--mode medium` unless the user explicitly requests another mode. Substitute the exact requested built-in or plugin mode; never infer `high` or `ultra` from complexity, size, urgency, or duration. Reuse the same idempotency key only for the identical request. If creation becomes indeterminate, inspect the operation and external state; do not retry blindly.

## Sprawl independent issue workers

Sprawl is worker-only orchestration around GitHub, dedicated Git worktrees, and `amux spawn`. It does not provision runners or create runner-owned remote agents.

### 1. Inspect before side effects

Fetch `origin/main`, read every requested issue and comment, and inspect native `blockedBy`, `blocking`, parent, and sub-issue relationships. Compare likely file/API overlap too. Create nothing until the independent set is known. Sequence blocked, prerequisite, overlapping, or user-ordered work and report the chain.

Apply the `amux-agent-first` label to accepted issue work. Give each worker one narrow issue, one dedicated worktree, and one branch. Do not group issues (#118 is outside this workflow).

### 2. Use stable issue identity

- Branch/worktree names include the issue number.
- The window is a semantic, issue-unprefixed slug such as `install-diagnostics`.
- Exact `--title-prefix '#<issue>'` owns issue identity. Never use `issue-123`, `issue-123-...`, `#123`, or `#123 ...` as the window.

```sh
git worktree add -b <type>/issue-<issue>-<slug> <dedicated-worktree> origin/main
amux --dry-run spawn --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --mode medium --title-prefix '#<issue>' --message-file <prompt> --idempotency-key issue-<issue>
amux spawn --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --mode medium --title-prefix '#<issue>' --message-file <prompt> --idempotency-key issue-<issue>
```

Use explicit `--mode medium` unless the user requested another mode. The worker prompt must include:

- ownership of only that issue, branch, and worktree;
- accepted contracts and native blockers to re-check, with overlap reported rather than absorbed;
- required tests, focused commit/PR against `main`, and `Closes #<issue>`;
- one focused Oracle review of issue intent plus current diff, with Amp thread history and unrelated history prohibited;
- callback destination metadata, an exact one-report format, and instructions to remain alive after `status=ready`;
- no merge/release/tag/cleanup authority; `/amux finish` only after independently verified merge and explicit authorization.

### 3. Verify and report

Verify branch/worktree, JSON worker identity, tmux pane, and initial assignment. Report accepted/deferred issues, dependency policy, issue/title, thread URL, worktree, branch, workspace/window, mode, callback route, and `/amux finish` cleanup path. On partial success, stop and inspect before retrying.

## Health workers and runners

Health is aggregate by default and accepts conceptual filters `workspace=<name>` and `mode=<worker|runner>`. Translate filters into canonical CLI selectors; do not invoke an `amux health` command.

Start with configuration and doctor output:

```sh
amux --json workspace list
amux --json list --all
amux --json doctor --all
amux --json worker list --all       # worker mode filter
amux --json runner list --all       # runner mode filter
```

Use `--workspace <name>` instead of `--all` when filtered. Probe each mode differently:

### Worker probe

Match exactly one configured worker to its same-named workspace session, window, workdir, and interactive Amp process. Do not ping shelved, missing, mismatched, ambiguous, busy, reconnecting, tool-running, or user-input panes. For a verified idle pane, send exactly one submitted prompt with a fresh token:

```text
AMUX_HEALTH_CHECK <token>. Reply exactly: AMUX_HEALTH_OK <token>. Do not inspect files, run commands, or change anything.
```

Use one literal send plus one Enter, wait at most 60 seconds by default, and accept only the exact current token. `no-response` means candidate stale, not safe to replace.

### Runner probe

Never send text to a runner pane. Use `amux runner doctor --workdir <path>` plus tmux metadata to verify the canonical workdir, configured ownership, generated window, and exact live `amp --no-tui` process. Classify as `running`, `not-running`, `worktree-missing`, `unlocked`, `mismatched`, or `ambiguous`. Do not infer health from a tmux server process or similarly named window.

Report one aggregate table with mode, workspace, canonical identity, local target, classification, and evidence. Health performs no archive, unpin, remove, park, kill, reconcile, launch, restart, or spawn. Ask for explicit authorization before a replacement or repair workflow.

## Teardown a worker

```sh
amux --dry-run teardown --current
amux teardown --current
```

Use `--thread <id>` when current identity is unavailable. Teardown is worker-only and fails closed on ambiguous identity. It archives the verified remote thread, removes worker and shelf configuration, and stops the verified local client; an already absent verified local process is a benign skip.

## Finish a merged worker

Finish is worker-only post-merge orchestration. It never removes a runner implicitly and never treats `status=ready` as cleanup authority.

1. Re-verify the exact PR is merged, the head branch/worktree match the worker, and the worktree is clean. Stop if unmerged, dirty, or unpushed.
2. Fail closed on unexpected runner ownership **before worktree removal**. List runner configuration first:

   ```sh
   amux --json runner list --workdir <worker-worktree>
   ```

   An unreadable list or any configured runner match blocks finish. Only for a matched runner, use `amux --json runner doctor --workdir <worker-worktree>` to collect evidence; do not unpin/remove it or unlock its worktree. An empty list is the normal owner-free case. Then inspect tmux/process metadata for an unexpected `amp --no-tui` process using that workdir; ambiguous or positive ownership blocks, while a clean inspection may proceed.
3. Update the designated main worktree with `git pull --ff-only`. Remove the clean worker worktree without force.
4. Preserve squash-merge safety. Try `git branch -d <branch>` only after merge verification. If it refuses because the PR was squash-merged, do not use `-D` automatically; verify the PR head, remote state, and absence of unique/unpushed work, then require explicit authorization for force deletion. Delete a remote branch only when its merged PR proves it safe and the user authorized shared mutation.
5. Do not tag or release unless separately and explicitly requested. Finish does not imply either.
6. Follow the originating callback protocol exactly. Re-verify pane/session/window/process metadata; send one literal merged/finish-authorized report plus Enter to the immutable pane. If verification or delivery fails, do not guess another pane and do not teardown. Remain alive and report the concrete blocker through the available channel.
7. After successful callback delivery, run worker teardown as the final action:

   ```sh
   amux teardown --thread <thread-id>
   ```

The pre-teardown callback reports merge, worktree, local/remote branch, runner-ownership check, and the pending final teardown. Teardown stops the worker, so no post-teardown callback is required. Only then may the worker stop.
