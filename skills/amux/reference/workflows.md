# amux workflows

These are skill workflows. Only commands beginning with literal `amux` are CLI commands; `/amux health`, `/amux sprawl`, and `/amux finish` are orchestration labels.

## Spawn a fresh worker

For an Amp Workspace Project, create one thread with Amp's authenticated native creation, delivering the complete task-only assignment in that creation request. Select `medium` unless the owner explicitly names another mode, and select `orb`, local execution where supported, or one exact Amp runner ID without fallback. Create it directly on the executor and workdir intended for the work. When physical files or local state matter, the assignment names the exact physical runner ID and canonical workdir; do not use Orb creation followed by physical adoption as migration. Adoption neither changes nor verifies continued affinity. Include one line naming the absolute path to the loaded skill's `reference/contract-v1.md` for a one-time read.

Then adopt the exact returned thread:

```sh
amux --dry-run --json worker adopt --thread <exact-native-thread> --workspace <workspace> --window <semantic-slug> --workdir <path> [--group <exact-group>]
amux --json worker adopt --thread <exact-native-thread> --workspace <workspace> --window <semantic-slug> --workdir <path> [--group <exact-group>]
```

The coordinator makes one native creation invocation and preserves its authenticated request plus successful returned thread identity as the bounded receipt. Native creation does not return a prompt digest or separate delivery acknowledgement, so do not claim exactly-once delivery or independently re-prove it through amux. amux adoption owns only local catalog/group/tmux state: it sends no message, presses no Enter, parses no composer, reads no transcript, and does not re-home native execution. New adoption canonicalizes the owner-supplied workdir; doctor preserves legacy catalog spelling and never promotes a relative legacy value into a location claim. Adoption/doctor report native executor, runner ID, and execution affinity as `unknown` because those facts are not in amux's local source of truth; never infer them from its pane or workdir. If native creation is indeterminate, stop rather than creating another thread.

For a projectless repository that is not an Amp Workspace Project, select the intended physical executor and use the lean local exception there:

```sh
amux --dry-run --json spawn --workdir <canonical-path> --workspace <workspace> --window <semantic-slug> [--group <existing-group>] --mode medium --prompt-file -
amux --json spawn --workdir <canonical-path> --workspace <workspace> --window <semantic-slug> [--group <existing-group>] --mode medium --prompt-file -
```

The command validates and consumes the complete bounded prompt in dry-run. A real invocation also reads and bounds the prompt before taking the mutation lock; dry-run takes no lock. The real invocation canonicalizes the cwd, creates one empty thread there, opens one exact continue pane, attempts one literal paste, attempts one Enter only after paste success, then persists and reports local worker ownership. With a group it preflights additive Amp label support before creation, then persists/reports membership and add-only ensures/reports its label after the input attempts. An unparseable result from the sole creation command is creation-indeterminate and raw output is not echoed. It does not read pane text or thread history. Any post-create error is indeterminate: retain the printed thread/window and every reported completed phase; do not retry, repaste, submit, archive, kill, clean up, or search for another receiver. Executor selection, not a required local process `--runner-id` argv alias, routes the invocation to the intended physical host.

Runner identity is exact: Amp-native `runner(id)` selects that runner, while amux Runner means the separately configured local `amp --no-tui` process. Never substitute one for the other or fall back between them. Native creation currently provides no durable workdir/message receipt that amux can independently verify; retain the returned identity and stop on an indeterminate result.

Preserve the Read Thread discrepancy fence. Only after an authorized `/amux` lifecycle or coordination operation names a concrete local/GitHub discrepancy, deterministic evidence is exhausted, and durable/local/GitHub evidence independently establishes the exact relationship may one narrow query target that exact thread. Then block rather than widening or chaining.

## Sprawl independent issue workers

Fetch `origin/main`; inspect every requested issue, comment, native dependency, active branch, PR, worktree, and likely file/API overlap before side effects. Sequence blocked or overlapping work. Give each independent issue one dedicated branch/worktree and one native-created thread.

Use an issue-bearing branch/worktree and an issue-unprefixed semantic window. Create the worktree from fresh `origin/main`, compose a task-only assignment with issue ownership, acceptance criteria, tests, PR requirements, exact group/report identity when coordinating, and the absolute contract path. Create once natively with explicit executor and mode, then use `worker adopt` as above. Verify the returned thread, worker row, tmux pane, worktree, branch, and assignment. Never infer delivery from pane text or search for an alternate receiver.

## Coordinate a durable issue work group

Durable group, report, and callback stores—not tmux text—are authoritative.

Every durable task-group Lead title starts with `🎖️ `. Reserve that prefix for Leads and never deliberately apply it to member workers. The task Lead is the exact thread coordinating this task, independent of the group's authoritative coordinator role. The marker is presentation only: it conveys neither executor placement nor authoritative group role. Apply this convention independently of executor choice.

### 1. Preflight authoritative state and bootstrap the CLI

Fetch `origin/main`; read issue bodies/comments and native parent/sub-issue/blocked-by/blocking relationships; compare active branches, PRs, worktrees, and likely file/API overlap. Establish one exact durable group and stable report ID before mutation. Resolve the loaded skill's `reference/contract-v1.md` to an absolute path (including installed roots such as `~/.agents/skills/amux`, `~/.config/agents/skills/amux`, or `~/.config/amp/skills/amux`); never send an unresolved relative path. Verify current help contains `group`, `callback`, `report`, and `worker adopt`; if not, build one absolute CLI path from a fresh `origin/main` archive and use it consistently—do not fall back to stale bare `amux`. Do not hand-edit registries. Treat the resolved group/report identity as immutable coordination input. No thread, worktree, group, callback, label, or report mutation may precede this step. Create branches/worktrees only after identity is fixed, and serialize mutations protected by the machine lock.

After this preflight succeeds, set the exact durable task-group Lead title with `amp threads rename <lead-thread> "🎖️ <task-title>"`; do not rely on a generated title. If rename fails, preserve the exact native thread identity and caller-side receipt, create no replacement, and stop before group or adoption mutations.

### 2. Declare the group and register the verified coordinator lease

```sh
amux --json group declare --group <durable-issue-group> --thread <coordinator-thread>
amux --json callback register --group <durable-issue-group> --thread <coordinator-thread> --pane <coordinator-pane>
```

Independently verify the coordinator pane and returned lease identity. A restart or identity change requires explicit registration of a new generation; never guess a pane.

### 3. Native-create and adopt the authoritative thread

Create a dedicated worktree from fresh `origin/main`. Native-create one thread on the executor/workdir intended for the work, with the task-only assignment naming any required physical runner ID and canonical workdir, explicit mode, exact group/report binding, and the absolute path to `reference/contract-v1.md`. Then adopt the exact returned identity; adoption neither changes nor verifies continued affinity. For dirty physical-worktree recovery, create on that exact physical runner or make a separate explicit handoff that preserves immutable physical-worker ownership; never imply that Orb-create → physical-adopt migrates execution.

```sh
amux --dry-run --json worker adopt --thread <member-thread> --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --group <durable-issue-group>
amux --json worker adopt --thread <member-thread> --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --group <durable-issue-group>
```

Give the child the stable report ID and exact group/thread/issue/reference binding. Require the child to remain alive after every status. `ready` means implementation, focused tests/checks, one review with findings addressed, PR, and normal CI are complete. A blocker uses the same report identity and `--pr none` when no PR exists. The task-only assignment must state that `reference/contract-v1.md` is the worker's only protocol source and grants no merge, release, finish, or cleanup authority.

### 4. Persist ready, wake, acknowledge, and independently verify

```sh
amux report submit --report-id <stable-report-id> --group <durable-issue-group> --thread <member-thread> --status ready --issue '#<issue>' --pr <pr-url> --summary implementation-tests-review-pr-ci-complete
amux report pending --group <durable-issue-group>
amux report history --report-id <stable-report-id>
amux report acknowledge --report-id <stable-report-id>
```

Successful human submission is exactly:

```text
<stable-report-id><TAB>ready<TAB>recorded<TAB><member-thread>
CALLBACK<TAB><durable-issue-group><TAB><stable-report-id><TAB>notified
```

The wake-up text is only `AMUX_REPORT group=<durable-issue-group> report=<stable-report-id>` plus Enter. If callback verification/send fails, submission exits `1` after recording the report and prints `CALLBACK<TAB><durable-issue-group><TAB><stable-report-id><TAB>failed`; keep the child alive. An identical retry prints the report line with `duplicate`, leaves one durable status event, and may retry notification only after the exact pane is independently known safe for input. If the composer is suspected or observed busy, do not send again: recover from `report pending`/`history` and acknowledge the durable report. Duplicate/late/reordered tokens cannot alter durable state.

Acknowledgement is receipt only. Before merge, the coordinator independently verifies the exact PR URL, head branch/SHA, issue scope and diff, mergeability, closing-issue metadata, worker/worktree identity and cleanliness, review evidence, and every required CI check. Do not substitute the child's summary, callback success, or acknowledgement for this evidence.

### 5. Merge, verify post-merge CI, then authorize finish

After a separately authorized merge, verify the resulting `main` commit and all required post-merge CI (including Pages when project paths trigger it). Do not auto-release, tag, or start dependent work while required post-merge evidence is pending. Only then record authorization:

```sh
amux report authorize-finish --report-id <stable-report-id> --thread <coordinator-thread> --reference <verified-main-commit-or-run>
amux report history --report-id <stable-report-id>
```

Human output is `<stable-report-id><TAB>authorized`. Authorization is durable, separate from acknowledgement, and accepted only from the current group coordinator while status is `ready`. Ready, blocked, notification, acknowledgement, deadline expiry, and a late callback never authorize finish.

### 6. Submit merged and run `/amux finish`

The child confirms the durable authorization and independently verifies merge, then progresses the same report ID without changing its immutable binding or authorized payload:

```sh
amux report submit --report-id <stable-report-id> --group <durable-issue-group> --thread <member-thread> --status merged --issue '#<issue>' --pr <pr-url> --summary implementation-tests-review-pr-ci-complete
```

`merged` is terminal. The callback remains a wake-up; the coordinator inspects and acknowledges the merged event. Then the coordinator explicitly directs `/amux finish`. Finish verifies GitHub/Git/worktree/runner ownership, cleans the worktree and safe branch state, and invokes `amux teardown --thread <member-thread>` last. Group membership and report history survive teardown unless a separate explicit group removal is requested. Never force-delete a branch, infer finish from a callback, or release automatically.

Run the final park/remove/teardown from a verified independent executor, never from the worker or runner transport being stopped. amux checks exact process incarnation and ancestry before mutation and fails closed for the whole invocation when any target relationship is ambiguous; pane names, cwd, and other presentation are not independence evidence. A rejected maintenance safety preflight reports the error without rewriting its prior checkpoint.

### 7. Coordinator-owned deadline queue

Soft budgets to `ready` are Small 30m, Medium 1h (default), Large 2h; split XL before spawning. Stale is 15m; one review warns after 10m; demonstrated external CI waits alert after 20m; authorized finish alerts after 10m. Expiry is diagnostic and non-destructive. Use one nearest-deadline queue, not one timer process per child. Never force-delete a branch, auto-release, or erase group history.

Load the full procedure only when arming, firing, or reconciling deadlines: [`deadline-v1.md`](deadline-v1.md). The current CLI exposes no command to create or update deadline records. Do not edit `reports.json` directly. Schedule wake-ups must **not** load the full `/amux` skill; they follow `deadline-v1` and re-read durable `amux report pending/history` state.

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

Never send text to a runner pane. Use `amux runner doctor --workdir <path>` plus tmux metadata to verify the canonical workdir, configured ownership, generated window, and exact live `amp --no-tui` process. Classify as `running`, `not-running`, `directory-missing`, `not-a-directory`, `mismatched`, or `ambiguous`. Do not infer health from a tmux server process or similarly named window.

Report one aggregate table with mode, workspace, canonical identity, local target, classification, and evidence. Health performs no archive, unpin, remove, park, kill, reconcile, launch, restart, or spawn. Ask for explicit authorization before a replacement or repair workflow.

## Teardown a worker

If the `/amux-claude` skill may have created pairs for this origin thread, load that skill and run its paired Claude lifecycle preflight/cleanup first (helper: installed `amux-claude` skill `experimental/claude-delegation/claude_delegation.py`). Recovery branches live only under `/amux-claude`. When no Claude skill is installed and no pairs are possible, skip the helper and use Amp teardown alone.

```sh
# Only when /amux-claude pairs may exist:
python3 "$HELPER" lifecycle worker-teardown --origin-thread <thread-id> --dry-run
amux --dry-run teardown --current
python3 "$HELPER" lifecycle worker-teardown --origin-thread <thread-id>
amux teardown --current
```

Use `--thread <id>` for `amux` when current identity is unavailable. When the helper runs, both dry-runs must succeed before mutation. Paired execution fences the origin, may park only terminal-safe verified pairs, and never removes artifacts, worktrees, receipts, reports, or group history. Any unsafe pair blocks; stop without Amp teardown. Indeterminate recovery is owner-authorized via `/amux-claude` only.

After paired success (or when no pair preflight applies), invoke `amux teardown` immediately. Teardown is worker-only and fails closed on ambiguous Amp identity. It archives the verified remote thread, removes worker and shelf configuration, and stops the verified local client; an already absent verified local process is a benign skip. Worker teardown remains the final action.

Invoke teardown from a verified independent executor. The target worker cannot safely teardown the Amp transport executing its own command, and unavailable or changed process-incarnation/ancestry evidence blocks before archive, catalog, shelf, or tmux mutation.

## Finish a merged worker

Finish is worker-only post-merge orchestration. It never removes a runner implicitly and never treats `status=ready` as cleanup authority.

1. Re-verify the exact PR is merged, the head branch/worktree match the worker, and the worktree is clean. Stop if unmerged, dirty, or unpushed.
2. Fail closed on unexpected runner ownership **before worktree removal**. List runner configuration first:

   ```sh
   amux --json runner list --workdir <worker-worktree>
   ```

   An unreadable list or any configured runner match blocks finish. Only for a matched runner, use `amux --json runner doctor --workdir <worker-worktree>` to collect evidence; do not unpin/remove it or unlock its worktree. An empty list is the normal owner-free case. Then inspect tmux/process metadata for an unexpected `amp --no-tui` process using that workdir; ambiguous or positive ownership blocks, while a clean inspection may proceed.
3. If `/amux-claude` pairs may exist, run the paired Claude lifecycle dry-run and execution from **Teardown a worker** before any worktree, branch, report, or Amp worker mutation. A blocker preserves lifecycle evidence and stops finish; finish must not continue to worktree removal. Owner-authorized recovery seams live only in `/amux-claude`. When no Claude pairs are possible, skip this step.
4. Update the designated main worktree with `git pull --ff-only`. Remove the clean worker worktree without force.
5. Preserve squash-merge safety. Try `git branch -d <branch>` only after merge verification. If it refuses because the PR was squash-merged, do not use `-D` automatically; verify the PR head, remote state, and absence of unique/unpushed work, then require explicit authorization for force deletion. Delete a remote branch only when its merged PR proves it safe and the user authorized shared mutation.
6. Do not tag or release unless separately and explicitly requested. Finish does not imply either.
7. Follow the originating report protocol exactly. For a work-group worker, confirm the durable authorization, submit `merged` with the same report ID/binding/payload, and let amux verify the callback lease and send only its wake-up token. If durable reporting or notification fails, do not guess another pane and do not teardown; the report remains inspectable and the worker remains alive. For a legacy non-group assignment, follow its explicit callback format after re-verifying the immutable pane/session/window/process identity.
8. After durable merged reporting and the coordinator's explicit finish direction, rerun paired Claude lifecycle revalidation when `/amux-claude` pairs may exist. Only after that succeeds (or when no pairs apply), run worker teardown as the final action:

    ```sh
    amux teardown --thread <thread-id>
    ```

The pre-teardown report/legacy callback covers merge, worktree, local/remote branch, runner-ownership check, and the pending final teardown. Teardown stops the worker, so no post-teardown callback is required. Durable group/report history remains. Only then may the worker stop. Never force-delete a branch, auto-release, or erase group history.
