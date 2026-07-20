# amux workflows

These are skill workflows. Only commands beginning with literal `amux` are CLI commands; `/amux health`, `/amux sprawl`, and `/amux finish` are orchestration labels.

## Spawn a fresh worker

Preflight, then create one interactive worker. Prefer a message file for structured assignments.

For an automatically selected mode, resolve the loaded skill directory and run its promoted gate before any dry-run or spawn command. Every automatic `amux spawn` command in this reference, including sprawl and durable coordination, inherits this shared preflight and must run it in the same shell operation as its spawn commands:

```sh
POLICY=<loaded-amux-skill-directory>/scripts/resolve-amp-invocation-policy
MODE=medium
POLICY_RESULT=$(printf '{"version":1,"action":"amux_spawn","mode":"%s","automatic":true}\n' "$MODE" | "$POLICY") || exit $?
[ "$POLICY_RESULT" = '{"action":"amux_spawn","capability":"skill_preflight_v1","reason":"automatic_medium","result":"allow","sources":["public"]}' ] || exit 2
```

Continue only after exit `0` and that exact deterministic `allow` document. Exit nonzero stops before `amux spawn`; never rewrite the rejected mode. Bind the same `MODE` value to resolver input and every subsequent spawn command. This automatic preflight does not manufacture approval for a different mode. When the user explicitly requested another built-in or plugin mode, preserve that exact request under the separate instruction rule instead of claiming `automatic:true`.

```sh
amux --dry-run spawn --workspace <workspace> --window <semantic-slug> --workdir <path> --mode "$MODE" --message-file <prompt> --idempotency-key <stable-key>
amux spawn --workspace <workspace> --window <semantic-slug> --workdir <path> --mode "$MODE" --message-file <prompt> --idempotency-key <stable-key>
amux worker list --thread <thread-id>
```

Every skill-driven spawn MUST pass `--mode medium` unless the user explicitly requests another mode. Substitute the exact requested built-in or plugin mode; never infer `high` or `ultra` from complexity, size, urgency, or duration. Reuse the same idempotency key only for the identical request. If creation becomes indeterminate, inspect the operation and external state; never change the key or resubmit blindly. Append `--reconcile` to the complete identical request only after read-only inspection proves either the complete assignment in the exact provisioned thread or one unambiguous fresh active alternate receiver with the expected workdir while the provisioned thread is empty. This narrow path verifies immutable request identity and exact delivery, then creates or verifies only the authoritative worker row and local tmux client without thread creation or resubmission. Ambiguous, stale, inactive, externally started, conflicting, or locally owned candidates fail closed. Other indeterminate outcomes remain terminal.

## Sprawl independent issue workers

Sprawl is worker-only orchestration around GitHub, dedicated Git worktrees, and `amux spawn`. It does not provision runners or create runner-owned remote agents.

### 1. Inspect before side effects

Fetch `origin/main`, read every requested issue and comment, and inspect native `blockedBy`, `blocking`, parent, and sub-issue relationships. Compare likely file/API overlap too. Create nothing until the independent set is known. Sequence blocked, prerequisite, overlapping, or user-ordered work and report the chain.

Apply the `amux-agent-first` label to accepted issue work. This label operation is add-only: confirm the desired label is present, but do not remove unrelated labels to force exact equality. Give each worker one narrow issue, one dedicated worktree, and one branch. When a coordinator supervises the batch, use the durable work-group workflow below; a group associates threads, not multiple issues in one worker assignment.

For issue work in the Amux repository, derive the durable group ID and additive Amp label as `amux-<issue-number>`, and each stable report ID as `amux-<issue-number>-worker-<ordinal>`. For another repository, use the equivalent `<repository-slug>-<issue-number>` and `<repository-slug>-<issue-number>-worker-<ordinal>` forms with its lowercase, group-safe repository slug. Use the resulting group ID wherever the commands below show `<durable-issue-group>`. This is an issue-coordination convention, not a generic `amux group` validation rule. Existing `issue-*` groups and reports and purpose-specific groups such as `pr-181-review` remain valid and untouched; do not migrate, rename, remove their labels, or rewrite their history.

### 2. Use stable issue identity

- Branch/worktree names include the issue number.
- The window is a semantic, issue-unprefixed slug such as `install-diagnostics`.
- Exact `--title-prefix '#<issue>'` owns issue identity. Never use `issue-123`, `issue-123-...`, `#123`, or `#123 ...` as the window.

```sh
git fetch origin main
git worktree add -b <type>/issue-<issue>-<slug> <dedicated-worktree> origin/main
```

Run the shared automatic-spawn preflight above before this block.

```sh
amux --dry-run spawn --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --mode "$MODE" --title-prefix '#<issue>' --group <durable-issue-group> --message-file <prompt> --idempotency-key issue-<issue>
amux spawn --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --mode "$MODE" --title-prefix '#<issue>' --group <durable-issue-group> --message-file <prompt> --idempotency-key issue-<issue>
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

## Coordinate a durable issue work group

This is the proven coordinator protocol layered on the implemented group, spawn, report, and callback commands. The durable stores—not tmux text—are authoritative.

### 1. Preflight authoritative state and bootstrap the CLI

1. Fetch `origin/main`. Read issue bodies/comments and native parent/sub-issue/blocked-by/blocking relationships, then compare active branches, PRs, worktrees, and likely files/APIs for overlap. Sequence dependencies and overlapping work; do not spawn first and reconcile later.
2. Verify current help contains `group`, `callback`, `report`, and spawn `--group`. If an installed binary predates those commands, use a separate fresh `origin/main` worktree and one absolute binary path:

   ```sh
   git fetch origin main
   git worktree add --detach <bootstrap-worktree> origin/main
   make -C <bootstrap-worktree> build BUILD_OUTPUT=<absolute-amux-path>
   <absolute-amux-path> help group
   <absolute-amux-path> help callback
   <absolute-amux-path> help report
   <absolute-amux-path> help spawn
   ```

   Invoke `<absolute-amux-path>` instead of bare `amux` for every subsequent example. Do not hand-edit registries or pretend unavailable commands succeeded. If a worker already exists, use `group add` only after verifying its authoritative thread; do not respawn it.
3. Create every branch/worktree from the freshly fetched `origin/main`. Use an issue-bearing branch/worktree but an issue-unprefixed semantic window. Pass explicit `--mode medium` unless the user chose another mode.
4. Serialize mutations. Wait for each group/callback/spawn/pane/row/worktree mutation to finish and verify its outcome before starting the next operation that needs the machine lock.

Repository policy may additionally require an add-only issue label, exactly one focused Oracle diff review, squash merge, named CI jobs, or Pages. Keep those in the assignment/project workflow; they are not generic amux CLI promises. Do not read Amp thread history by default. Only an authorized `/amux` lifecycle or coordination operation may, after naming a concrete local/GitHub discrepancy, exhausting deterministic evidence, and separately establishing the exact relationship with durable/local/GitHub evidence, ask one narrow query of that exact related thread. If it does not resolve the discrepancy, block rather than widening or chaining reads.

### 2. Declare the group and register the verified coordinator lease

```sh
amux --json group declare --group amux-135 --thread <coordinator-thread>
amux --json callback register --group amux-135 --thread <coordinator-thread> --pane <coordinator-pane>
```

Before registration, independently verify the pane belongs to the configured coordinator worker. Parse the successful callback outcome and confirm its config directory, group/thread, pane, session/window IDs, PID, generation, and registration time against fresh tmux/process metadata. Registration human output is tab-separated: `<group><TAB>registered<TAB><generation><TAB><pane>`. A restart or any identity change invalidates the lease; explicitly register a new generation. Never guess a pane.

### 3. Spawn and attach the authoritative receiving thread

```sh
git fetch origin main
git worktree add -b <type>/issue-<issue>-<slug> <dedicated-worktree> origin/main
```

Run the shared automatic-spawn preflight above before this block.

```sh
amux --dry-run spawn --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --mode "$MODE" --title-prefix '#135' --group amux-135 --message-file <assignment> --idempotency-key issue-135
amux --json spawn --workspace <workspace> --window <semantic-window> --workdir <dedicated-worktree> --mode "$MODE" --title-prefix '#135' --group amux-135 --message-file <assignment> --idempotency-key issue-135
```

Spawn resolves #104 alternate-thread delivery before persisting group intent. Verify the worker and membership outcomes name only the final receiving thread; never add the abandoned provisioned identity. If label ensure fails after creation, the worker and local membership remain, exit is `1`, and retry with the identical idempotency key resumes grouping without recreating or resubmitting.

Give the child one stable report ID (for example `amux-135-worker-1`) and the exact group/thread/issue/reference binding. Require the child to remain alive after every status. `ready` means implementation, focused tests/checks, one review, addressed findings, PR, and normal CI are complete. A blocker uses the same report identity and `--pr none` when no PR exists:

```sh
amux report submit --report-id <stable-report-id> --group <group> --thread <member-thread> --status blocked --issue '#<issue>' --pr none --summary <concise-hyphenated-blocker>
```

### 4. Persist ready, wake, acknowledge, and independently verify

```sh
amux report submit --report-id <stable-report-id> --group <group> --thread <member-thread> --status ready --issue '#<issue>' --pr <pr-url> --summary implementation-tests-review-pr-ci-complete
amux report pending --group <group>
amux report history --report-id <stable-report-id>
amux report acknowledge --report-id <stable-report-id>
```

Successful human submission is exactly:

```text
<stable-report-id><TAB>ready<TAB>recorded<TAB><member-thread>
CALLBACK<TAB><group><TAB><stable-report-id><TAB>notified
```

The wake-up text is only `AMUX_REPORT group=<group> report=<stable-report-id>` plus Enter. If callback verification/send fails, submission exits `1` after recording the report and prints `CALLBACK<TAB><group><TAB><stable-report-id><TAB>failed`; keep the child alive. An identical retry prints the report line with `duplicate`, leaves one durable status event, and may retry notification only after the exact pane is independently known safe for input. If the composer is suspected or observed busy, do not send again: recover from `report pending`/`history` and acknowledge the durable report. Duplicate/late/reordered tokens cannot alter durable state.

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
amux report submit --report-id <stable-report-id> --group <group> --thread <member-thread> --status merged --issue '#<issue>' --pr <pr-url> --summary implementation-tests-review-pr-ci-complete
```

`merged` is terminal. The callback remains a wake-up; the coordinator inspects and acknowledges the merged event. Then the coordinator explicitly directs `/amux finish`. Finish verifies GitHub/Git/worktree/runner ownership, cleans the worktree and safe branch state, and invokes `amux teardown --thread <member-thread>` last. Group membership and report history survive teardown unless a separate explicit group removal is requested. Never force-delete a branch, infer finish from a callback, or release automatically.

### 7. Coordinator-owned deadline queue

Assign size and generation at spawn: Small 30m, Medium 1h (default), Large 2h, and split XL before spawning. The soft deadline covers through `ready`: implementation, focused checks, one focused review and fixes, PR, and normal CI. Merge, post-merge checks, and finish are later coordinator-owned stages.

This is coordinator policy, not a spawn/report CLI option. The current CLI exposes no command to create or update deadline records. Do not edit `reports.json` directly. The coordinator's external durable scheduler records generations, waits, and diagnostics and owns the single queue; where expiry reveals an actual work blocker, the worker also submits the same stable report ID as `blocked`. Expiry alone does not manufacture a report status transition.

- No meaningful progress for 15m is `stale`; one Oracle/review over 10m warns and must not become a review loop; demonstrated external CI/service wait over 20m alerts; authorized finish over 10m alerts.
- Thinking, research, review loops, and thread reads do not pause active time. Only a demonstrated external CI queue/service outage pauses active time; retain its visible wall-clock evidence.
- The coordinator may grant at most one explicit extension, no more than half the original budget (Small +15m, Medium +30m, Large +1h), with a reason and new timer generation. The child never self-extends. Superseded generations are harmless.
- Expiry is diagnostic and non-destructive. Record/retain overdue or blocker evidence, report it, and leave the worker alive. It never authorizes archive, park, replacement, merge, teardown, unpin, or finish.
- Maintain one coordinator-owned nearest-deadline queue and arm only its next wake-up—not one timer process per child. Never create a sleeping process or persistent supervisor per child.

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
6. Follow the originating report protocol exactly. For a work-group worker, confirm the durable authorization, submit `merged` with the same report ID/binding/payload, and let amux verify the callback lease and send only its wake-up token. If durable reporting or notification fails, do not guess another pane and do not teardown; the report remains inspectable and the worker remains alive. For a legacy non-group assignment, follow its explicit callback format after re-verifying the immutable pane/session/window/process identity.
7. After durable merged reporting and the coordinator's explicit finish direction, run worker teardown as the final action:

   ```sh
   amux teardown --thread <thread-id>
   ```

The pre-teardown report/legacy callback covers merge, worktree, local/remote branch, runner-ownership check, and the pending final teardown. Teardown stops the worker, so no post-teardown callback is required. Durable group/report history remains. Only then may the worker stop.
