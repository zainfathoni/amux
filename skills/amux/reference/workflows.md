# amux workflows

These are skill workflows. Only commands beginning with literal `amux` are CLI commands; `/amux health`, `/amux sprawl`, and `/amux finish` are orchestration labels.

## Spawn a fresh worker

For an Amp Workspace Project, create one thread with Amp's authenticated native creation, delivering the complete task-only assignment in that creation request. Select an explicit mode: with a known linked ChatGPT subscription and known target-mode availability, use `low` for small mechanical work, `medium` for ordinary implementation, or `high` for hard architecture, debugging, or review; otherwise use `medium`. Keep `ultra`, plugin, and other special modes owner-explicit. Select `orb`, local execution where supported, or one exact Amp runner ID without fallback. Create it directly on the executor and workdir intended for the work. When physical files or local state matter, the assignment names the exact physical runner ID and canonical workdir; do not use Orb creation followed by physical adoption as migration. Adoption neither changes nor verifies continued affinity. Include one line naming the absolute path to the loaded skill's `reference/contract-v1.md` for a one-time read.

Then adopt the exact returned thread:

```sh
amux --dry-run --json worker adopt --thread <exact-native-thread> --workspace <workspace> --window <semantic-slug> --workdir <path> [--group <exact-group>]
amux --json worker adopt --thread <exact-native-thread> --workspace <workspace> --window <semantic-slug> --workdir <path> [--group <exact-group>]
```

The coordinator makes one native creation invocation and preserves its authenticated request plus successful returned thread identity as the bounded receipt. Native creation does not return a prompt digest or separate delivery acknowledgement, so do not claim exactly-once delivery or independently re-prove it through amux. amux adoption owns only local catalog/group/tmux state: it sends no message, presses no Enter, parses no composer, reads no transcript, and does not re-home native execution. New adoption canonicalizes the owner-supplied workdir; doctor preserves legacy catalog spelling and never promotes a relative legacy value into a location claim. Adoption/doctor report native executor, runner ID, and execution affinity as `unknown` because those facts are not in amux's local source of truth; never infer them from its pane or workdir. If native creation is indeterminate, stop rather than creating another thread.

For a projectless repository that is not an Amp Workspace Project, select the intended physical executor and use the lean local exception there:

```sh
amux --json spawn --assignment-phase prepare --native-capability existing-thread-message-v1 --workdir <canonical-path> --workspace <workspace> --window <semantic-slug> [--group <existing-group>] --mode medium --prompt-file -
amux --json spawn --assignment-phase arm --native-capability existing-thread-message-v1 --thread <exact-prepared-thread> --workdir <same-path> --workspace <same-workspace> --window <same-slug> [--group <same-group>] --mode medium --prompt-file -
# issue one thread_interact action=message call to the exact prepared thread with the unchanged prompt
amux --json spawn --assignment-phase finalize --assignment-outcome <rejected|indeterminate|authenticated_accepted> [--latest-cursor <successful-tool-latestCursor>] --native-capability existing-thread-message-v1 --thread <exact-prepared-thread> --workdir <same-path> --workspace <same-workspace> --window <same-slug> [--group <same-group>] --mode medium --prompt-file -
```

Before `prepare`, require that this coordinator exposes the authenticated native existing-thread `thread_interact` message action; otherwise stop before amux, thread creation, or tmux mutation. Each phase re-reads and bounds the complete prompt and binds it by digest without persisting prompt text. `prepare` durably arms creation, invokes local `amp threads new --mode <exact-mode>` exactly once in the canonical cwd, retains its exact returned ID and ownership, but creates no pane. `arm` verifies exact thread/cwd/mode/group/digest equality and durably records assignment as indeterminate before remote mutation. Only after successful arm, call `thread_interact` once with `action: message`, `thread: <exact-prepared-ID>`, and the unchanged prompt. Explicit rejection finalizes `rejected`; tool connection, undocumented, or uncertain results finalize `indeterminate`; success plus returned `latestCursor` finalizes `authenticated_accepted`. Finalize persists assignment truth before a presentation-only exact pane. It performs no load-buffer, paste-buffer, Enter, capture, search, export, alternate receiver, cleanup, or archive. Success/latestCursor proves only authenticated exact-thread acceptance/queueing, never message ID, inference, execution, or physical executor/workdir affinity. Do not automatically retry any creation or armed message. Executor selection—not an Orb fallback or amux Runner—routes the local command to the intended physical host.

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

## Sweep worktree inventory

`/amux sweep` is a read-only skill workflow, not an `amux sweep` CLI command. Run the loaded skill's `scripts/sweep-inventory` helper with the repository, Amux config directory, and every explicit filesystem root whose immediate child checkouts comprise the inventory boundary:

```sh
python3 <loaded-amux-skill>/scripts/sweep-inventory --repo <repository> --config-dir <amux-config-directory> --amux <current-amux-binary> --filesystem-root <checkout-parent> [--filesystem-root <other-parent>] [--presentation-baseline <commit-ish> --generated-exclude <repo-glob> --canonical-worktree <path>] --json
```

The helper performs a stable full outer join over four independent authorities: `git worktree list --porcelain` for Git registrations, `workers.tsv` for thread↔workdir ownership, `reports.json member_thread joined through workers.tsv` for lifecycle evidence through an authoritative worker thread only, and `lstat` plus the explicit roots for filesystem presence. Before extraction, the selected current Amux binary validates the complete report/deadline store through its authoritative read-only loader. Existing ancestor components are canonicalized consistently while a final-component symlink remains ambiguous. Ambiguous duplicate worker threads, canonical paths, or workspace/windows quarantine every participant and authorize no lifecycle join. The sweep emits every Git registration, worker record, lifecycle record, and discovered checkout, including directory-without-worker-record, worker-record-without-directory, Git-registration-without-directory, directory-without-Git-registration, lifecycle-without-worker, partial, and malformed rows. `report_id` and `groups.tsv` never establish a workdir.

Filesystem coverage is explicit and uncapped: pass every intended root. The output records the roots and an empty `omitted` list; an unreadable root, malformed source, duplicate binding, relative worker path, symlink root/path, non-directory path, unsupported Git porcelain, unavailable executable, or command failure remains a rank-zero error row and returns exit `2`. It is never dropped or interpreted as absence. Rows are ordered by irreversibility rank, then path, thread, and classification, independent of source order. Human and JSON modes expose the same row facts.

Presentation evidence is optional and repository-configurable. `--presentation-baseline` reports raw committed divergence paths; repeated `--generated-exclude` globs produce a separately filtered path list and filtered stash path lists, including untracked stash content. Stash subjects are matched only by Git's exact branch prefix; unmatched evidence remains explicitly `unassigned`. `--precious-pattern` selects ignored precious paths (default `\.env|local\.json|auth`), and `--canonical-worktree` enables exact per-path `duplicate-of-canonical`, `unique`, or `symlink-to-external` reporting. These exclusions affect presentation fields only—never Git registration, worker/lifecycle joins, `open_obligation`, errors, ordering, or `removal_verdict`.

This workflow performs no fetch, cleanup, reconciliation, removal, unlock, prune, backup-ref mutation, branch or stash mutation, report mutation, or external-project mutation. Every Git subprocess forces `GIT_OPTIONAL_LOCKS=0`, including status inspection, so observational commands cannot refresh an external index. Accordingly every row says `removal_verdict=NOT_EVALUATED`: a read-only inventory cannot truthfully satisfy the pruning-fetch and adjacent-revalidation contract in [`removal-safety.md`](removal-safety.md). Inventory classifications describe observed join state only and never authorize removal. A blocked report with absent or literal-zero `authorized_at` is carried as `open_obligation=true`; reports without a worker remain visible but cannot invent a workdir. Preservation-locked historical resources are inventory evidence only and are never changed.

## Finish a merged worker

Finish is worker-only post-merge orchestration. It never removes a runner implicitly and never treats `status=ready` as cleanup authority.

1. Re-verify the exact PR lifecycle with `gh pr view <pr> --json state,mergedAt,headRefName,headRefOid`, and verify the reported head commit with the ref-coverage check below. An open PR, null `mergedAt`, mismatched head, or unavailable/ambiguous GitHub evidence blocks finish. Neither `already_stopped` nor a covering branch proves that review is finished.
2. Fail closed on unexpected runner ownership **before worktree removal**. List runner configuration first:

   ```sh
   amux --json runner list --workdir <worker-worktree>
   ```

   An unreadable list or any configured runner match blocks finish. Only for a matched runner, use `amux --json runner doctor --workdir <worker-worktree>` to collect evidence; do not unpin/remove it or unlock its worktree. An empty list is the normal owner-free case. Then inspect tmux/process metadata for an unexpected `amp --no-tui` process using that workdir; ambiguous or positive ownership blocks, while a clean inspection may proceed.
3. If `/amux-claude` pairs may exist, run the paired Claude lifecycle dry-run and execution from **Teardown a worker** before any worktree, branch, report, or Amp worker mutation. A blocker preserves lifecycle evidence and stops finish; finish must not continue to worktree removal. Owner-authorized recovery seams live only in `/amux-claude`. When no Claude pairs are possible, skip this step.
4. Update the designated main worktree with `git pull --ff-only` before classification. Pull/fetch may advance the baseline or remove a stale remote-tracking ref that previously supplied `SAFE` evidence, so no earlier verdict survives this step.
5. Run the removal preflight below against the exact worker worktree and its recorded `HEAD`. Complete the report before any removal, unlock, or prune mutation. `BLOCKED`, `NEEDS_BACKUP`, incomplete evidence, or any preflight command failure stops finish. This increment never creates a backup: `NEEDS_BACKUP` remains a hard stop until the separately owned backup workflow exists. Then perform the adjacent revalidation below with no intervening command. [Remove the worker worktree without force only when that immediately preceding revalidation reports a proceeding verdict and no independent blocker](removal-safety.md#removal-ordering-context). An untracked-file prediction means this unforced remove is expected to refuse; report it before attempting removal rather than presenting the raw Git error as a surprise. If worktree removal ever gains `--force`, untracked files must become `BLOCKED` in that same change.
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

   Any `refs/remotes/*` result is `SAFE` and its exact ref is the evidence. Otherwise any `refs/heads/*` or `refs/tags/*` result is `SAFE_KEEP_BRANCH`; report every exact covering ref and the explicit remedy `keep <ref>` (a tag is also durable local-only coverage). Otherwise, if `git merge-base --is-ancestor <C> <resolved-remote-baseline>` succeeds, report `SAFE` with that exact baseline. Otherwise run one-directional `git cherry <resolved-remote-baseline> <C>`: only a non-empty result consisting entirely of `-` rows is `SAFE` patch-equivalence evidence. An empty result is already covered by ancestry; any `+`, malformed row, or command error falls through. Rule 5 is always `NEEDS_BACKUP`, with the exact remedy `create refs/heads/backup/<worktree-name>-before-remove-<date> at <C>`; do not create it in this increment and never upgrade the verdict because differences look generated.
4. **Report working-tree loss independently of Git-object safety.** Before any removal of a present path, run `git status --porcelain --untracked-files=all --ignored`. Report the complete, unfiltered count and paths of `??` untracked entries, plus that unforced removal is predicted to refuse. Separately match ignored `!!` paths against the repository-configured precious-file pattern; when absent, use the defaults `\.env|local\.json|auth`. Generated-artifact exclusions are configurable presentation filters for this precious-file list only. Never apply them to tracked status, ref coverage, ancestry, `git cherry`, or any verdict rule.

   Resolve every matched precious path individually without following an untrusted parent path blindly:

   - use `lstat` first;
   - for a symlink, resolve its immediate target and canonical path and report `symlink-to-external` only when the resolved target is outside the worktree; otherwise report the contained target as `unique` unless canonical comparison proves a duplicate;
   - for a regular file, compare bytes to the same relative path under an explicitly selected, separately canonicalized canonical worktree and report `duplicate-of-canonical` only on an exact byte match;
   - report canonical-copy absence, type mismatch, or paths escaping through a parent symlink as `unique`; report resolution failures and broken links as unresolved `unique` entries that block removal.

   Emit the relative path, ignored marker, resolution status, and resolved/canonical comparison target where applicable—never only a count. Any scan or resolution error blocks ordinary removal because ignored files have no Git interlock. A `unique` precious path also blocks until it is preserved or receives a separate exact discard disposition; duplicate and external-symlink evidence is reporting, not a claim that the target contents were deleted. For `MISSING_WORKTREE`, keep the scan void and dirty unknowable rather than emitting an empty reassuring list.
5. **Report affected stashes.** Inspect every stash with `git stash list --format='%gd%x09%H%x09%P%x09%gs'`, which carries the stash selector, stash commit, parents (the first is its base), and subject. For an attached branch, report stashes whose subject identifies the departing branch, including exact `stash@{n}`, base, and subject; never count `refs/stash` as commit coverage. For a detached target, report that branch attribution is unavailable and list any stash whose base parent equals the target tip as possibly affected. A stash-inspection error or malformed row blocks removal.
6. **Emit one complete decision before mutation.** Include target identity and presence, fetch/source/baseline evidence, PR `state` and `mergedAt` or the explicit non-review `not applicable` context, verdict and exact rule evidence, tracked dirty state, untracked prediction, each resolved precious path (or missing-worktree void), affected stashes, runner/worker ownership checks, and every blocker/non-cleanup reason. `SAFE_KEEP_BRANCH` proceeds only with the explicit `do not delete <ref>` note. `BLOCKED`, `NEEDS_BACKUP`, untracked predicted refusal, and unresolved unique precious files stop before mutation. `SAFE` is not permission to skip PR, ownership, ignored-file, stash, or report checks.
7. **Revalidate adjacent to targeted removal.** After the report and immediately before `git worktree remove <path>`, with no pull, fetch, unlock, prune, or other command between revalidation and removal, re-read the exact target from `git worktree list --porcelain` and `stat`. Re-run exact worker/runner/process ownership, `HEAD`, attached/detached branch, lock/prunable/path state, tracked status, untracked and ignored precious-file scans, stash report, resolved baseline/source, and rules 2a–5 ref coverage. Re-record the current baseline even when it advanced; advancement alone is not a blocker, but the current verdict must still proceed. Any changed target/path-tip binding, registration, ownership, covering refs, worktree state, blocker report, command error, or downgraded verdict invalidates the decision and restarts the full preflight. Never reuse rule-2a evidence for a remote-tracking ref that disappeared after fetch.

8. **Gate repository-wide prune as a set.** A single-target verdict never authorizes `git worktree prune`. Before prune, snapshot the complete porcelain inventory and independently run rules 0–5 plus all applicable ownership/report checks for every entry marked `prunable`, as well as every locked-and-absent entry that the operation plans to unlock. Any blocked, `NEEDS_BACKUP`, incomplete, or errored row blocks the entire repository-wide operation. Preserve `classify → backup → unlock → prune` per entry. After all planned unlocks and immediately before prune, re-read the complete inventory and freshly re-derive rules 2a–5 for every authorized row using the current pruned refs and re-resolved baseline; never compare or reuse a stale verdict. The exact set of prunable paths, tips, branch/detached state, locks, and newly derived verdict evidence must match the authorized set, with no unclassified new entry. Any set or evidence change restarts classification; never drop or implicitly authorize another row.

For a vanished worktree preserve the mutation order `classify → backup → unlock → prune`. Because backup creation belongs to the follow-up workflow, this gate stops at `NEEDS_BACKUP`; it must not unlock or prune first. Worktree removal never implies branch deletion, and prune never licenses deletion of branches or stashes.
