# Removal safety classifier

This reference is the source of truth for the removal-safety verdict ladder. It classifies whether Git commits survive removing a worktree; it does not authorize removal, cleanup, branch deletion, or any lifecycle action.

For the ordinary present, index-only, attached-worktree finish route, use the fast path below and the finish workflow's one-summary/one-approval gate. For detached or missing worktrees, backup decisions, remove-on-missing, and repository-wide prune, follow [`workflows.md` general removal preflight](workflows.md#removal-preflight-for-finish-remove-on-missing-and-prune). A classifier verdict alone never authorizes mutation.

`git worktree remove` deletes exactly one ref: that worktree's `HEAD`. A branch ref survives it. The question is therefore **"does any other ref hold this commit?"**, not **"is it merged?"**.

## Ordinary completed-worker fast path

Use this path only for one exact, present Git worktree that is still bound to one exact completed Amux worker. It deliberately keeps the attached local branch. That narrower disposition does not need baseline resolution, remote-ref pruning, patch-equivalence, backup-ref creation, stash attribution, or branch deletion: the exact surviving local branch itself preserves every commit at `HEAD`, including unique or unpushed commits, and repository stashes are untouched.

Before the concise finish summary and again adjacent to mutation, run observational local Git commands with `GIT_OPTIONAL_LOCKS=0` so preflight does not refresh the index:

1. Parse the exact entry from `git worktree list --porcelain`, canonicalize and `stat` the path separately, and require the worker binding, repository, present path, and `HEAD` to agree. Another worker, runner, or unexpected process using the path blocks.
2. Require an attached `refs/heads/<branch>` whose ref resolves byte-for-byte to the worktree `HEAD`. Read its configured upstream, if any, and query that exact remote/ref with `git ls-remote --heads <remote> <ref>`; report `present`, `absent`, or `unavailable` without fetching or changing local refs. No upstream is explicit `none`. Remote absence/unavailability is not work-loss authority and does not require branch cleanup: this path relies only on the retained exact local branch and must not claim that local-only commits are pushed. A detached head, missing branch, ref mismatch, missing directory, or ambiguous registration blocks this fast path with every resource retained.
3. Require `git -C <worktree> status --porcelain=v1 --untracked-files=all` to be empty. Any tracked, staged, or untracked entry or command error blocks. Status can hide a modified tracked file when index flags suppress inspection, so read `git -C <worktree> ls-files --cached -v -z` NUL-safely and block every lowercase `assume-unchanged` tag and every `S`/`s` `skip-worktree` tag. Separately read and NUL-parse `git -C <worktree> ls-files --stage -z`; require every entry to be stage zero and compare every exact worktree object to its index OID rather than trusting the stat cache. Regular modes `100644` and `100755` must have the corresponding file type and Git owner-executable bit (`0100`), and `git -C <worktree> hash-object --path=<git-path> -- <git-path>` (without `-w`) must return the index OID. Mode `120000` must be a symlink whose exact `readlink` bytes return the index OID through `git hash-object --stdin`. Missing paths, content or mode mismatches, unsupported modes (including gitlinks), filter/hash failures, and malformed, unavailable, or inconsistent index output block.

   Normal removal can also silently delete ignored content, so require a strictly index-only filesystem. Read the NUL-delimited tracked set with `git -C <worktree> ls-files --cached -z`, derive its ancestor directories, and walk the entire worktree with `lstat` without following symlinks. Permit only tracked index entries, their ancestor directories, and the exact linked-worktree `.git` administrative file. Any other filesystem object blocks, including an ignored file or directory, an otherwise empty directory, nested-repository or present submodule content, a special file, or anything unreadable or unresolved. Run all observations with `GIT_OPTIONAL_LOCKS=0`. The index reads, content hashes, and filesystem walk must all complete; a count or ignored-file listing is not a complete deletion inventory. Independently remove or preserve generated ignored output, clear any suppressing index flags through a separately understood Git action, then start a fresh preflight.
4. Classify porcelain lock state exactly. `locked` permits one exact normal unlock after approval and adjacent revalidation; `unlocked` skips unlock; missing, unreadable, or ambiguous lock evidence blocks. Unlock is not removal authority.
5. Emit the finish workflow's one complete summary, receive one exact approval, and revalidate all facts. Run only `git worktree remove <exact-path>` with no `--force`, then verify both registration and path absence. If Git refuses or absence is ambiguous, retain the worker/thread and stop.

Worktree removal never deletes the retained branch or repository stashes. Branch deletion, remote mutation, prune, and backup-ref creation are outside this fast path and require their own independently requested disposition.

## Classification inputs

For worktree `W` at commit `C`, determine ref coverage with this required include-list:

```sh
git for-each-ref --contains <C> refs/heads refs/remotes refs/tags
```

The include-list is semantic, not cosmetic. Bare `for-each-ref` also returns `refs/stash`: a stash commit's first parent is `HEAD` at stash time, so treating it as coverage would be false safety. The include-list also excludes unrelated namespaces such as `refs/notes/*` and `refs/pull/*`. A detached `HEAD` is not a ref and is already omitted; no exclusion clause is needed. An attached worktree's branch is a covering local ref and must remain included.

Before classifying, run and record successful `git fetch --prune origin`. Plain `git fetch origin` is insufficient: it can retain a remote-tracking ref after its upstream branch was deleted, creating phantom rule-2a coverage. A deleted upstream ref must not satisfy rule 2a; classify only after pruning remote-tracking refs, and fail closed if the pruning fetch fails.

Resolve the baseline in this order:

1. An explicit `--baseline` override, when provided.
2. `git symbolic-ref refs/remotes/origin/HEAD`.
3. `gh repo view --json defaultBranchRef`.

Record the resolved baseline, the winning source (`override`, `origin/HEAD`, or `GitHub default branch`), and the exact successful pruning-fetch evidence. Map a GitHub default branch name explicitly to `refs/remotes/origin/<name>`. Never hardcode a branch name.

## Verdict ladder

| # | Condition | Verdict |
| --- | --- | --- |
| 0 | Worktree directory absent: `prunable`, or `locked` **and** the path is absent | `MISSING_WORKTREE` — annotate it, take the tip SHA from `git worktree list --porcelain`, and run rules 2a–5 unchanged |
| 1 | Tracked changes present: `git status --porcelain --untracked-files=no` is non-empty | `BLOCKED` |
| 2a | A remote ref contains `C` | `SAFE` |
| 2b | Only a local ref contains `C` | `SAFE_KEEP_BRANCH` |
| 3 | `C` is an ancestor of the resolved baseline | `SAFE` — merged |
| 4 | Every commit in `baseline..C` is patch-equivalent | `SAFE` — squash-landed |
| 5 | Otherwise | `NEEDS_BACKUP` |

Rule 0 requires a filesystem `stat`. In `git worktree list --porcelain`, `prunable` appears only for an unlocked missing directory. A locked-and-missing worktree is byte-identical to a locked-and-present worktree, so `prunable` alone silently omits that case. A missing worktree has unknowable dirty and generated-file state; it is never reported as clean.

Rule 1 blocks only tracked changes. Untracked and ignored files are not safety evidence for this ladder; report their possible destruction separately rather than treating them as tracked changes.

Rule 4 uses one-directional `git cherry` evidence. A `-` is evidence that a change landed; a `+` means only "not detected" and is not evidence that the change is unique. Rule 5 is never silently upgraded to `SAFE`.

Generated-artifact exclusions are presentation-only and must never filter rules 2a–5. In particular, do not filter `git cherry` or patch-equivalence: doing so could make commits that differ only in generated output appear equivalent and silently upgrade `NEEDS_BACKUP` to `SAFE`.

## Removal ordering context

For a vanished worktree, preserve the order:

```text
classify → backup → unlock → prune
```

Do not unlock before classification and backup: unlocking makes a locked missing entry prunable. `NEEDS_BACKUP` requires a verified durable branch at exactly the classified tip before any removal proceeds. Use the complete-set backup procedure in [`workflows.md`](workflows.md#back-up-needs_backup-rows); its helper creates refs only and never authorizes or performs removal. Worktree removal and branch deletion are separate operations; this classifier never deletes a branch, and `SAFE_KEEP_BRANCH` explicitly preserves the local branch.
