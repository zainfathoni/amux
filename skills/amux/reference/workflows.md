# amux workflows

These are skill workflows. Only commands beginning with literal `amux` are CLI commands; `/amux health`, `/amux sprawl`, and `/amux finish` are orchestration labels.

## Create fresh native work

For ordinary new work, make one authenticated native Amp `create_thread` call and deliver the complete task-only assignment in that request. Select an explicit mode: with a known linked ChatGPT subscription and known target-mode availability, use `low` for small mechanical work, `medium` for ordinary implementation, or `high` for hard architecture, debugging, or review; otherwise use `medium`. Keep `ultra`, plugin, and other special modes owner-explicit.

Select the exact Workspace Project and Orb, or list live runners immediately before creation and select one exact runner ID whose working directory is the intended canonical workdir. Do not silently fall back between Orb and runner, between runner IDs, or between workdirs. When physical files or local state matter, use that exact live runner; an Orb is not a substitute.

Keep the child prompt lean: task, acceptance criteria, relevant context and constraints, validation, and the expected result. Preserve the authenticated native request, exact returned thread, parent/child route, and reply route. Do not put Amux policy or exclusions in the prompt. Native-created work has no Amux contract path, receipt, report, callback, group, deadline, finish authorization, or automatic adoption; create no Amux lifecycle representation for it. Same-directory Amux ownership remains separate and unmanaged. If native creation is rejected, indeterminate, or loses tool connectivity, stop without retry, fallback, search, adoption, or alternate creation.

### Lean native `create_thread` prompt example

```text
Task: <bounded task and why it matters>
Acceptance criteria:
- <observable outcome>
- <required scope boundary>
Relevant context and constraints: <files, repository, safety limits, and settled decisions>
Validation: <checks to run>
Expected result: complete the task and reply to the parent with changed files, checks, and blockers.
```

Use this shape for fresh work, sprawl, and new issue coordination. The parent owns executor selection and native routing; the child does not need orchestration policy.

Only when an exact owner instruction authorizes one projectless physical-host placement that native project-backed Orb/runner creation cannot express, run the bounded local exception on that host. Determine its byte-exact local hostname first; do not infer or alias it:

```sh
amux --json spawn --assignment-phase prepare --owner-authorized-projectless-physical-host --physical-host <exact-local-hostname> --native-capability existing-thread-message-v1 --workdir <canonical-path> --workspace <assignment-namespace> --window <assignment-key> --mode medium --prompt-file -
amux --json spawn --assignment-phase arm --owner-authorized-projectless-physical-host --physical-host <same-hostname> --native-capability existing-thread-message-v1 --thread <exact-prepared-thread> --workdir <same-path> --workspace <same-namespace> --window <same-key> --mode medium --prompt-file -
# issue one thread_interact action=message call to the exact prepared thread with the unchanged prompt
amux --json spawn --assignment-phase finalize --owner-authorized-projectless-physical-host --physical-host <same-hostname> --assignment-outcome <rejected|indeterminate|authenticated_accepted> [--latest-cursor <successful-tool-latestCursor>] --native-capability existing-thread-message-v1 --thread <exact-prepared-thread> --workdir <same-path> --workspace <same-namespace> --window <same-key> --mode medium --prompt-file -
```

Before `prepare`, require the authenticated native existing-thread message action; otherwise stop before Amux or thread mutation. The unchanged assignment uses the same lean task shape above and receives no contract or lifecycle instructions. The command checks the requested hostname equals the local hostname and writes schema-2 admission `spawn-native-cutover-v1/projectless-physical-host-exception`, host, canonical workdir, mode, assignment key, and prompt digest before one local `amp threads new`. It creates no worker, group, operation, shelf, pane, or adoption state. `arm` verifies the unchanged boundary and durably records assignment as indeterminate before the one native message. `finalize` updates only `spawn-assignments.json`. Success/latestCursor proves acceptance/queueing only, never execution. Any ambiguity preserves indeterminacy; never retry, fall back, reroute, rebind, adopt, search, clean up, or use another transport.

Pre-cutover schema-1 assignments are drain-only. Never run `prepare` for them. A prepared exact record may arm once and receive its one message; an already armed record may only finalize without resend. Supply the unchanged legacy group flag when the record contains one, but do not add or change group intent. Drain transitions write only the existing assignment store and create no pane or native replacement state. Only an exact existing spawn, adoption, or group record with persisted pre-cutover provenance can establish drain eligibility; ambiguity blocks. `contract-v1.md` and Amux lifecycle instructions are permitted only inside that proven legacy boundary and only when the existing binding requires them.

### Proven legacy drain-only prompt example

```text
Legacy Amux drain only: exact pre-cutover group, member, and report records <identities> were verified drain-eligible.
Read once: /absolute/path/to/installed/amux/reference/contract-v1.md
Continue only the already-bound assignment and exact existing group/report/callback identities. Create or rebind nothing.
```

Runner identity is exact: Amp-native `runner(id)` selects that runner, while amux Runner means the separately configured local `amp --no-tui` process. Never substitute one for the other or fall back between them. Native creation currently provides no durable workdir/message receipt that amux can independently verify; retain the returned identity and stop on an indeterminate result.

Preserve the Read Thread discrepancy fence. Only after an authorized `/amux` lifecycle or coordination operation names a concrete local/GitHub discrepancy, deterministic evidence is exhausted, and durable/local/GitHub evidence independently establishes the exact relationship may one narrow query target that exact thread. Then block rather than widening or chaining.

## Sprawl independent issue threads

Fetch `origin/main`; inspect every requested issue, comment, native dependency, active branch, PR, worktree, and likely file/API overlap before side effects. Sequence blocked or overlapping work. Give each independent issue one dedicated branch/worktree and one authenticated native-created thread. These are native child threads, not Amux workers or spawned workers.

Use an issue-bearing branch/worktree and an issue-unprefixed semantic title. Create the worktree from fresh `origin/main`, compose the lean task-only assignment above with issue ownership, acceptance criteria, tests, PR requirements, and expected reply, then create once natively with explicit executor and mode. For a physical worktree, the exact selected live runner must already be rooted at that canonical workdir. Verify the native returned thread, executor selection, worktree, branch, and assignment; create no Amux lifecycle representation. Never infer delivery from pane text or search for an alternate receiver.

## Coordinate native child threads and drain a durable work group

For **new** issue coordination, use Amp's native parent/child association, authenticated `create_thread`, reply routing, messaging, and waiting. Select every child's exact executor/workdir at creation, deliver the lean task-only assignment above, and leave it unmanaged by Amux. The parent uses only native identity and reply routing; it does not require any Amux lifecycle action from the child.

The procedure below is compatibility-only for a durable group, member worker, callback, and report identity that already exist and need to drain. Enter it only when exact persisted provenance proves the flow predates cutover and its next transition is drain-eligible. Durable group, report, and callback stores—not tmux text—remain authoritative for that pre-cutover state. Do not add a new member, task, group, report, or callback scope.

Every durable task-group Lead title starts with `🎖️ `. Reserve that prefix for Leads and never deliberately apply it to member workers. The task Lead is the exact thread coordinating this task, independent of the group's authoritative coordinator role. The marker is presentation only: it conveys neither executor placement nor authoritative group role. Apply this convention independently of executor choice.

### 1. Preflight existing authoritative state

Read the existing group, member, callback, and report records and verify the exact thread/workdir bindings. Resolve the loaded skill's `reference/contract-v1.md` to an absolute path only for an already-bound pre-cutover Amux worker whose records prove both drain eligibility and a remaining need for the contract. Verify current help contains the drain commands; do not bootstrap by creating a replacement worker or editing registries. If any exact identity or pre-cutover provenance is absent, stop.

Do not rename a thread merely to drain it. Existing presentation metadata conveys no new authority.

### 2. Revalidate the existing coordinator lease

Inspect the existing group coordinator and callback. Re-register only the same exact pre-cutover coordinator/group scope when recovery of that existing report workflow requires a new lease generation; never guess a pane or register one for new work.

### 3. Continue only the existing member

Use only the already-recorded member thread, workdir, group, and stable report ID. Do not native-create a replacement, adopt, rebind, or add membership. The existing child remains alive after every status. `ready`, `blocked`, and `merged` retain their current meanings and exact immutable binding.

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

Retain the exact size and generation already assigned to each pre-cutover report: Small 30m, Medium 1h (default), or Large 2h; any XL work should already have been split before its original assignment. Do not add deadline state for native-created work. Stale is 15m; one review warns after 10m; demonstrated external CI waits alert after 20m; authorized finish alerts after 10m. Expiry is diagnostic and non-destructive. Use one nearest-deadline queue, not one timer process per child. Never force-delete a branch, auto-release, or erase group history.

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

## Sweep worktree inventory

`/amux sweep` is a one-time read-only migration inventory for the staged Amux drain. It remains a read-only skill workflow, not a recurring lifecycle surface and not an `amux sweep` CLI command. Run it only when the owner authorizes the drain inventory. Run the loaded skill's `scripts/sweep-inventory` helper with the repository, Amux config directory, and every explicit filesystem root whose immediate child checkouts comprise the inventory boundary:

```sh
python3 <loaded-amux-skill>/scripts/sweep-inventory --repo <repository> --config-dir <amux-config-directory> --amux <current-amux-binary> --filesystem-root <checkout-parent> [--filesystem-root <other-parent>] [--presentation-baseline <commit-ish> --generated-exclude <repo-glob> --canonical-worktree <path>] --json
```

The helper performs a stable full outer join over four independent authorities: `git worktree list --porcelain` for Git registrations, `workers.tsv` for thread↔workdir ownership, `reports.json member_thread joined through workers.tsv` for lifecycle evidence through an authoritative worker thread only, and `lstat` plus the explicit roots for filesystem presence. Before extraction, the selected current Amux binary validates the complete report/deadline store through its authoritative read-only loader. Existing ancestor components are canonicalized consistently while a final-component symlink remains ambiguous. Ambiguous duplicate worker threads, canonical paths, or workspace/windows quarantine every participant and authorize no lifecycle join. The sweep emits every Git registration, worker record, lifecycle record, and discovered checkout, including directory-without-worker-record, worker-record-without-directory, Git-registration-without-directory, directory-without-Git-registration, lifecycle-without-worker, partial, and malformed rows. `report_id` and `groups.tsv` never establish a workdir.

Filesystem coverage is explicit and uncapped: pass every intended root. The output records the roots and an empty `omitted` list; an unreadable root, malformed source, duplicate binding, relative worker path, symlink root/path, non-directory path, unsupported Git porcelain, unavailable executable, or command failure remains a rank-zero error row and returns exit `2`. It is never dropped or interpreted as absence. Rows are ordered by irreversibility rank, then path, thread, and classification, independent of source order. Human and JSON modes expose the same row facts.

Presentation evidence is optional and repository-configurable. `--presentation-baseline` is resolved to an exact commit OID before a `--end-of-options`-guarded diff reports raw committed divergence paths; repeated `--generated-exclude` globs produce a separately filtered path list and filtered stash path lists, including untracked stash content. Stash subjects are matched only by Git's exact branch prefix; unmatched evidence remains explicitly `unassigned`, and paths are read from the recorded immutable stash commit rather than its moving selector. `--precious-pattern` selects ignored precious paths (default `\.env|local\.json|auth`), and a present non-symlink `--canonical-worktree` enables exact per-path `duplicate-of-canonical`, `unique`, or `symlink-to-external` reporting. These exclusions affect presentation fields only—never Git registration, worker/lifecycle joins, `open_obligation`, errors, ordering, or `removal_verdict`.

This workflow performs no fetch, cleanup, reconciliation, removal, unlock, prune, backup-ref mutation, branch or stash mutation, report mutation, or external-project mutation. Every Git subprocess forces `GIT_OPTIONAL_LOCKS=0`, including status inspection, so observational commands cannot refresh an external index. Accordingly every row says `removal_verdict=NOT_EVALUATED`: a read-only inventory cannot truthfully satisfy the pruning-fetch and adjacent-revalidation contract in [`removal-safety.md`](removal-safety.md). Inventory classifications describe observed join state only and never authorize removal. A blocked report with absent or literal-zero `authorized_at` is carried as `open_obligation=true`; reports without a worker remain visible but cannot invent a workdir. Preservation-locked historical resources are inventory evidence only and are never changed.

**Sunset condition:** after the owner records acceptance of one complete staged-drain inventory (or records its explicit incomplete/error disposition) and confirms that no repeat inventory is required, delete `scripts/sweep-inventory`, its sweep-only tests, and every `/amux sweep` route/reference before the next Amux release. Do not promote the helper into the CLI, retain it as a standing diagnostic, schedule it, or extend it for post-drain monitoring.

## Finish a merged worker

Finish is compatibility/drain-only post-merge orchestration for an existing pre-cutover worker. It never applies to native-created work, removes a runner implicitly, or treats `status=ready` as cleanup authority.

1. Re-verify the exact PR lifecycle with `gh pr view <pr> --json state,mergedAt,headRefName,headRefOid`, and verify the reported head commit with the ref-coverage check below. An open PR, null `mergedAt`, mismatched head, or unavailable/ambiguous GitHub evidence blocks finish. Neither `already_stopped` nor a covering branch proves that review is finished.
2. Fail closed on unexpected runner ownership **before worktree removal**. List runner configuration first:

   ```sh
   amux --json runner list --workdir <worker-worktree>
   ```

   An unreadable list or any configured runner match blocks finish. Only for a matched runner, use `amux --json runner doctor --workdir <worker-worktree>` to collect evidence; do not unpin/remove it or unlock its worktree. An empty list is the normal owner-free case. Then inspect tmux/process metadata for an unexpected `amp --no-tui` process using that workdir; ambiguous or positive ownership blocks, while a clean inspection may proceed.
3. If `/amux-claude` pairs may exist, run the paired Claude lifecycle dry-run and execution from **Teardown a worker** before any worktree, branch, report, or Amp worker mutation. A blocker preserves lifecycle evidence and stops finish; finish must not continue to worktree removal. Owner-authorized recovery seams live only in `/amux-claude`. When no Claude pairs are possible, skip this step.
4. Update the designated main worktree with `git pull --ff-only` before classification. Pull/fetch may advance the baseline or remove a stale remote-tracking ref that previously supplied `SAFE` evidence, so no earlier verdict survives this step.
5. Run the removal preflight below against the exact worker worktree and its recorded `HEAD`. Complete the report before any removal, unlock, or prune mutation. `BLOCKED`, incomplete evidence, or any preflight command failure stops finish. For every `NEEDS_BACKUP` row, run the complete-set backup procedure below; an absent, declined, conflicting, or unverifiable backup remains a hard stop. Backup creation does not change the classifier verdict and does not authorize removal. Restart the complete preflight. Then perform the adjacent revalidation below with no intervening command. [Remove the worker worktree without force only when that immediately preceding revalidation reports a proceeding verdict, every current `NEEDS_BACKUP` tip has its exact verified backup ref, and no independent blocker remains](removal-safety.md#removal-ordering-context). An untracked-file prediction means this unforced remove is expected to refuse; report it before attempting removal rather than presenting the raw Git error as a surprise. If worktree removal ever gains `--force`, untracked files must become `BLOCKED` in that same change.
6. Keep branch deletion separate from worktree removal. A `SAFE_KEEP_BRANCH` verdict means preserve the local branch and do not attempt branch deletion. For other verdicts, try `git branch -d <branch>` only after merge verification and only as a distinct authorized action. If it refuses because the PR was squash-merged, do not use `-D` automatically; verify the PR head, remote state, and absence of unique/unpushed work, then require explicit authorization for force deletion. Delete a remote branch only when its merged PR proves it safe and the user authorized shared mutation.
7. Do not tag or release unless separately and explicitly requested. Finish does not imply either.
8. Follow the originating report protocol exactly. For a work-group worker, confirm the durable authorization, submit `merged` with the same report ID/binding/payload, and let amux verify the callback lease and send only its wake-up token. If durable reporting or notification fails, do not guess another pane and do not teardown; the report remains inspectable and the worker remains alive. For a legacy non-group assignment, follow its explicit callback format after re-verifying the immutable pane/session/window/process identity.
9. After durable merged reporting and the coordinator's explicit finish direction, rerun paired Claude lifecycle revalidation when `/amux-claude` pairs may exist. Only after that succeeds (or when no pairs apply), run worker teardown as the final action:

    ```sh
    amux teardown --thread <thread-id>
    ```

The pre-teardown report/legacy callback covers merge, worktree, local/remote branch, runner-ownership check, and the pending final teardown. Teardown stops the worker, so no post-teardown callback is required. Durable group/report history remains. Only then may the worker stop. Never force-delete a branch, auto-release, or erase group history.

### Removal preflight for finish, remove-on-missing, and prune

Load [`removal-safety.md`](removal-safety.md) before applying this gate. Use it not only for finish, but before every skill-owned path that can run `git worktree remove` against an absent directory, `git worktree unlock`, or `git worktree prune`. A missing directory removes Git's dirty-file interlock; it never makes classification optional. Prefer targeted `git worktree remove <path>` for one absent entry: `git worktree prune` is repository-wide and has no target-path selector.

Run the gate from a verified independent executor. It is read-only through the final decision and records the command result for every item; an unavailable command, malformed output, changed target identity, or unresolved path fails closed rather than dropping a row.

1. **Bind the target and fetch evidence.** Parse the exact target entry and tip from `git worktree list --porcelain`, then `stat` the recorded path separately. Record path, `HEAD`, attached branch or `detached`, `locked`, `prunable`, and path `present|absent|unreadable`. Run `git fetch --prune origin` before classification and record that exact command and its successful result. Plain `git fetch origin` is insufficient because it can retain a deleted upstream branch as a phantom remote-tracking ref and falsely satisfy rule 2a. For finish or any review-worktree cleanup, the PR lifecycle evidence above is mandatory and fail-closed. A non-review remove-on-missing or prune records `PR: not applicable` plus its separate removal authorization/context; it does not fabricate or require a PR, while every Git, ownership, file-loss, and stash check below remains mandatory. Resolve the remote default baseline in this order:

   1. explicit `--baseline` override;
   2. `git symbolic-ref refs/remotes/origin/HEAD`;
   3. `gh repo view --json defaultBranchRef`.

   Map the GitHub fallback's branch name explicitly to `refs/remotes/origin/<name>`; never classify against a local branch or the unqualified GitHub name. Record the resolved full ref and winning source as `override`, `origin/HEAD`, or `GitHub default branch`. A fetch failure or unresolved/non-commit baseline blocks every removal and prune mutation.
2. **Apply rule 0 and tracked-only rule 1.** `MISSING_WORKTREE` means `prunable`, or `locked` with `stat` proving the path absent. Use its recorded porcelain tip for rules 2a–5, report `dirty: unknowable`, `precious scan: void—worktree absent`, and do not claim clean. For a present path, run `git -C <worktree> status --porcelain --untracked-files=no`; non-empty output is `BLOCKED`. A status error is not empty output and blocks. Untracked and ignored files never change this tracked-only verdict.
3. **Apply the ref verdict ladder exactly.** Run:

   ```sh
   git for-each-ref --contains <C> --format='%(refname)' refs/heads refs/remotes refs/tags
   ```

   Any `refs/remotes/*` result is `SAFE` and its exact ref is the evidence. Otherwise any `refs/heads/*` or `refs/tags/*` result is `SAFE_KEEP_BRANCH`; report every exact covering ref and the explicit remedy `keep <ref>` (a tag is also durable local-only coverage). Otherwise, if `git merge-base --is-ancestor <C> <resolved-remote-baseline>` succeeds, report `SAFE` with that exact baseline. Otherwise run one-directional `git cherry <resolved-remote-baseline> <C>`: only a non-empty result consisting entirely of `-` rows is `SAFE` patch-equivalence evidence. An empty result is already covered by ancestry; any `+`, malformed row, or command error falls through. Rule 5 is always `NEEDS_BACKUP`, with the exact remedy `create refs/heads/backup/<worktree-name>-before-remove-<date> at <C>`; create and verify it only through the complete-set procedure below, and never upgrade the verdict because a backup now exists or differences look generated.
4. **Report working-tree loss independently of Git-object safety.** Before any removal of a present path, run `git status --porcelain --untracked-files=all --ignored`. Report the complete, unfiltered count and paths of `??` untracked entries, plus that unforced removal is predicted to refuse. Separately match ignored `!!` paths against the repository-configured precious-file pattern; when absent, use the defaults `\.env|local\.json|auth`. Generated-artifact exclusions are configurable presentation filters for this precious-file list only. Never apply them to tracked status, ref coverage, ancestry, `git cherry`, or any verdict rule.

   Resolve every matched precious path individually without following an untrusted parent path blindly:

   - use `lstat` first;
   - for a symlink, resolve its immediate target and canonical path and report `symlink-to-external` only when the resolved target is outside the worktree; otherwise report the contained target as `unique` unless canonical comparison proves a duplicate;
   - for a regular file, compare bytes to the same relative path under an explicitly selected, separately canonicalized canonical worktree and report `duplicate-of-canonical` only on an exact byte match;
   - report canonical-copy absence, type mismatch, or paths escaping through a parent symlink as `unique`; report resolution failures and broken links as unresolved `unique` entries that block removal.

   Emit the relative path, ignored marker, resolution status, and resolved/canonical comparison target where applicable—never only a count. Any scan or resolution error blocks ordinary removal because ignored files have no Git interlock. A `unique` precious path also blocks until it is preserved or receives a separate exact discard disposition; duplicate and external-symlink evidence is reporting, not a claim that the target contents were deleted. For `MISSING_WORKTREE`, keep the scan void and dirty unknowable rather than emitting an empty reassuring list.
5. **Report affected stashes.** Inspect every stash with `git stash list --format='%gd%x09%H%x09%P%x09%gs'`, which carries the stash selector, stash commit, parents (the first is its base), and subject. For an attached branch, report stashes whose subject identifies the departing branch, including exact `stash@{n}`, base, and subject; never count `refs/stash` as commit coverage. For a detached target, report that branch attribution is unavailable and list any stash whose base parent equals the target tip as possibly affected. A stash-inspection error or malformed row blocks removal.
6. **Emit one complete decision before mutation.** Include target identity and presence, fetch/source/baseline evidence, PR `state` and `mergedAt` or the explicit non-review `not applicable` context, verdict and exact rule evidence, tracked dirty state, untracked prediction, each resolved precious path (or missing-worktree void), affected stashes, runner/worker ownership checks, and every blocker/non-cleanup reason. `SAFE_KEEP_BRANCH` proceeds only with the explicit `do not delete <ref>` note. `BLOCKED`, an unbacked `NEEDS_BACKUP`, untracked predicted refusal, and unresolved unique precious files stop before mutation. `SAFE` is not permission to skip PR, ownership, ignored-file, stash, or report checks.
7. **Revalidate adjacent to targeted removal.** After the report and immediately before `git worktree remove <path>`, with no pull, fetch, backup, unlock, prune, or other command between revalidation and removal, re-read the exact target from `git worktree list --porcelain` and `stat`. Re-run exact worker/runner/process ownership, `HEAD`, attached/detached branch, lock/prunable/path state, tracked status, untracked and ignored precious-file scans, stash report, resolved baseline/source, and rules 2a–5 ref coverage. Re-record the current baseline even when it advanced; advancement alone is not a blocker, but the current verdict must still proceed. For a current `NEEDS_BACKUP`, require the deterministic backup ref for the current path/date to resolve to the exact current tip; ignore only that expected exact ref while re-deriving the original rule-5 verdict, then record it separately as verified backup evidence. Any changed target/path-tip binding, registration, ownership, other covering refs, worktree state, blocker report, command error, or downgraded verdict invalidates the decision and restarts the full preflight. Never reuse rule-2a evidence for a remote-tracking ref that disappeared after fetch, and never reuse a backup for a prior tip.

8. **Gate repository-wide prune as a set.** A single-target verdict never authorizes `git worktree prune`. Before prune, snapshot the complete porcelain inventory and independently run rules 0–5 plus all applicable ownership/report checks for every entry marked `prunable`, as well as every locked-and-absent entry that the operation plans to unlock. Any blocked, unbacked `NEEDS_BACKUP`, incomplete, or errored row blocks the entire repository-wide operation. Preserve `classify → backup → unlock → prune` per entry. Backup every `NEEDS_BACKUP` row in one complete all-or-nothing helper operation before any unlock. After all planned unlocks and immediately before prune, re-read the complete inventory and freshly re-derive rules 2a–5 for every authorized row using the current pruned refs and re-resolved baseline; never compare or reuse a stale verdict. Ignore only each expected backup ref at its exact tip while re-deriving rule 5, then verify it separately. The exact set of prunable paths, tips, branch/detached state, locks, newly derived verdict evidence, and required backup refs must match the authorized set, with no unclassified new entry. Any set or evidence change restarts classification; never drop or implicitly authorize another row.

### Back up `NEEDS_BACKUP` rows

After one complete removal preflight has classified the exact authorized target set, write its facts to an owner-only manifest and invoke the loaded skill's helper. Use `targeted_remove` with exactly one row for targeted removal; use `prune` with every current prunable and planned locked-and-missing row, including rows that need no backup, so omission cannot turn a repository-wide operation into a partial plan.

```sh
python3 <loaded-amux-skill>/scripts/backup-removal-refs --manifest <owner-only-manifest> --dry-run --json
python3 <loaded-amux-skill>/scripts/backup-removal-refs --manifest <same-owner-only-manifest> --json
```

The strict schema-v1 manifest contains `operation`, the canonical `repository`, exact successful pruning-fetch evidence, the exact baseline ref/tip/source, and the complete ordered `rows`. Each row binds path, tip, attached branch or null, lock/prunable/path state, verdict, every covering ref, exact rule evidence, exact worker/runner/process ownership evidence from the preflight, and—only for `NEEDS_BACKUP`—the full `refs/heads/backup/<worktree-name>-before-remove-<YYYY-MM-DD>` ref plus date. Each ownership item has exactly `status` (`absent|clear|owned|blocked`) and one non-empty bounded evidence summary; `owned` or `blocked` fails closed. The helper validates and binds this ownership evidence but does not replace live ownership probes: the mandatory complete preflight restart after backup reruns them before removal. Unknown, duplicate, relative, symlinked, path-escaping, malformed, missing, or changed helper-owned evidence fails closed. Keep the manifest private because ownership evidence may be machine-specific; do not put secrets in it.

Dry-run reruns `git fetch --prune origin` and all helper-owned validation, prints the same facts as execution, and creates no backup ref. Duplicate deterministic backup names in one complete set fail during dry-run rather than producing an unexecutable plan. Human output ends with the exact JSON facts envelope, so both modes carry identical evidence. Execution immediately repeats helper-owned preflight, then uses one `git update-ref --stdin` transaction with `option no-deref` across the complete set: an absent direct ref is created at the exact tip, a direct ref already at that tip is verified as an idempotent no-op, and any symbolic ref, ref elsewhere, or concurrent mismatch aborts the whole transaction without force-updating or dereferencing any ref. It verifies every resulting direct ref before success. `--no-backup` explicitly declines creation, exits preflight-rejected when any row needs backup, and never permits removal.

The helper creates or verifies refs only. It never removes files or worktrees, unlocks, prunes, deletes branches, mutates registries, or returns removal authority (`removal_authorized` is always false). After success, discard the stale decision, rerun the complete preflight, and apply step 7 or step 8 revalidation adjacent to the separately authorized removal mutation. A changed date may select a new deterministic ref only through a fresh complete preflight; never rename or force-update the old ref.

For a vanished worktree preserve the mutation order `classify → backup → unlock → prune`. Worktree removal never implies branch deletion, and prune never licenses deletion of branches or stashes.
