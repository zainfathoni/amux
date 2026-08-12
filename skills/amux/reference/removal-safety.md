# Removal safety classifier

This reference is the source of truth for the removal-safety verdict ladder. It classifies whether Git commits survive removing a worktree; it does not authorize removal, cleanup, branch deletion, or any lifecycle action.

For the mutation gate, adjacent revalidation, PR/stash/file-loss reporting, and repository-wide prune procedure, follow [`workflows.md` removal preflight](workflows.md#removal-preflight-for-finish-remove-on-missing-and-prune). A classifier verdict alone never authorizes mutation.

`git worktree remove` deletes exactly one ref: that worktree's `HEAD`. A branch ref survives it. The question is therefore **"does any other ref hold this commit?"**, not **"is it merged?"**.

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
