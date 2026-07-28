# Adopt an owner-created local Claude Code tmux window

Use this operator-assisted route only after the owner explicitly requests adoption of Claude Code windows they already created on one named physical host. It is not managed local delegation and not fresh-Orb execution: it creates no Claude process, Orb, API call, receipt, worker, session, or window. Never substitute one of those routes if this route is unavailable.

The owner retains authentication and interactive control. Amp coordinates bounded work, verifies results independently, and acts on Git or GitHub only under its ordinary authority. Claude output is evidence, never authority to push, mutate a PR, merge, release, clean up, access secrets, or delegate recursively.

## 1. Select one physical host and exact windows

Record the owner-selected physical host, repository workdir, expected branch and head, tmux session, and semantic window name before inspecting or sending input. The public selector is exactly `session:window`; both components must be non-empty literal names supplied or confirmed by the owner, and neither component may contain `:`. Split on the one colon and compare both complete names exactly. Do not accept a numeric pane ID, prefix, glob, index, fuzzy match, or the first autocomplete result. Pane IDs may be recorded only as internal diagnostics after selection and must be re-resolved from the semantic selector before each action because pane IDs can be stale or recycled.

Require the exact `session:window` to resolve to one window and exactly one pane on the selected host. Enumerate window names and current commands without capturing unrelated pane history. If the session name, window name, host, pane count, or resolution is ambiguous, stop for an exact owner selection. Adopt only the named task windows; the session and every other window remain owner-managed.

This route requires official current model selector `claude-opus-5`. The owner must explicitly select and confirm the exact literal `claude-opus-5` in every adopted Claude Code client before work begins. Omission, alias normalization, a default, fallback, substitution, or an offered alternative is rejection. Amp must not relaunch or reconfigure Claude to satisfy this preflight.

The Claude Code UI may render a human-readable label such as `Opus 5` where the underlying selector is the literal `claude-opus-5`. That display form satisfies this preflight only when the owner explicitly confirms it denotes exactly `claude-opus-5` in that client. It never licenses inferring an alias table, mapping any other label to a literal, or accepting a displayed default; an unconfirmed label is an ambiguous state and fails closed.

## 2. Preflight before every prompt

Build a task-local catalog for all adopted windows and show it to the owner:

| Field | Required value |
| --- | --- |
| Physical host | Exact owner-selected machine, not an Orb |
| Selector | Exact semantic `session:window` |
| Pane diagnostic | Current resolved pane ID; never a public selector |
| Workdir | Canonical physical path |
| Repository | Canonical Git top level |
| Branch/head | Exact branch and commit |
| Worktree state | Clean, with no staged, unstaged, or untracked files |
| Role | `read-only` or `exclusive-writer` |
| Writer owner | One selector, or `none` |
| Claude state | Running, exact model explicitly confirmed, composer state known |

Verify the host identity, pane workdir, Git top level, branch, head, complete porcelain status including untracked files, linked-worktree inventory, and the catalog of other adopted windows. Reject a dirty worktree, detached or unexpected branch/head, mismatched workdir, stale/recycled pane, or two adopted windows sharing one worktree. Never stash, reset, switch, clean, or repair state to make adoption pass.

A `read-only` window may analyze and report but must not edit files, run mutating Git/GitHub commands, install software, or control other windows. An `exclusive-writer` window may edit only its one clean, dedicated worktree and exact branch. There must be exactly one writer for that worktree and no read-only adopted window may share it. Reject mutation when exclusive ownership cannot be proved. Amp alone owns integration, publication, PR mutation, and lifecycle decisions.

Preflight expires after any process interruption/resumption, pane replacement, model change, branch/head/workdir change, worktree mutation outside the assigned writer, or catalog change. Re-run it; do not infer continuity from a familiar window name or resumed display.

## 3. Deliver one bounded prompt without guessing

Prepare one self-contained UTF-8 prompt of at most 16 KiB. Include the exact selector, role, workdir, branch/head, task and exclusions, allowed commands, required result shape, and the prohibition on push, PR mutation, merge, release, cleanup, secrets access, recursive delegation, and tmux control. Keep a digest or exact local copy so retries cannot silently change or duplicate the prompt.

Before any paste, require the owner or bounded inspection of only the selected task pane to establish all of the following: Claude Code is active; the exact `claude-opus-5` selection is visible or freshly owner-confirmed; the composer is empty and focused; there is no autocomplete menu, pasted-text review, permission dialog, or modal prompt; the composer's editing mode is known, including whether a Vim composer is in insert or normal mode; and the pane has not been interrupted or resumed since preflight. Do not inspect unrelated scrollback.

Distinguish ghost autocomplete from committed composer content. Rendered ghost text is an unaccepted suggestion, not bytes in the buffer; committed content is what the composer would actually submit. `Tab` accepts the ghost suggestion, so never send `Tab` unless accepting that exact suggestion is the intended action. When the owner confirms the visible text is only a ghost suggestion, that confirmation may establish an empty underlying buffer and satisfy the empty-composer condition. Without that confirmation, visible text is treated as committed content and the route fails closed.

Paste the prompt literally into only the exact re-resolved `session:window`; do not type it key-by-key and do not append a submission key in the same operation. A paste is not submission. Inspect only enough current task-pane output to prove the complete prompt is present once and the composer state remains unambiguous, then submit exactly once.

One exact controlled paste may render collapsed as one or more `[Pasted text #…]` segments instead of expanded text. Placeholder segment count is not paste-action count: several numbered segments can represent the same single paste, so never read a segment number as evidence of a second paste. Accept a collapsed rendering as an operator-confirmation and submission boundary only when all four of these are bound together: the exact source bytes or their digest from this task's retained prompt copy; exactly one paste action performed by Amp; the selected re-resolved `session:window` as the target; and the expected pane tail or an explicit operator confirmation of the rendered state. Do not require impossible full expansion of collapsed segments, and never repaste merely to expand collapsed text — a second paste duplicates the prompt. A rendering that cannot be bound on all four points, or that carries unexplained additional content, is unclassified and stops the route fail-closed before submission.

Never send blind `Enter`, `Escape`, `Tab`, mode-switching keys, control keys, or a second paste. If an autocomplete menu, a pasted-text mode or review not bound to Amp's own single paste, a modal or permission dialog, partial text, duplicate text, process interruption/resumption, or any uncertainty about what a key would do is visible, stop before input. State one bounded owner action, for example “in `claude:review-291`, choose the intended autocomplete item with Tab, then press Enter once” or “in `claude:review-291`, submit the pasted prompt once from your Vim composer, then confirm.” Re-preflight after the owner acts. Amp does not automate composer recovery.

Vim mode is an owner preference for their own client; this route never requires it disabled and never treats its mere presence as a stop. A known, owner-confirmed Vim composer state may proceed when the paste target and the submission-key semantics are unambiguous for that exact state — for instance a normal-mode composer whose single submit key is confirmed, or a `-- INSERT --` composer where the same is true. When the mode is unknown, or when Vim `-- INSERT --` versus normal mode would change what a submission or recovery key does, hand exactly that one submission or recovery step to the owner. Never send a mode-switching key to normalize the composer yourself, and never ask the owner to change their editing preference as a precondition for this route.

Every UI state this route has not explicitly classified is ambiguous by default. Unrecognized banners, labels, placeholders, overlays, or composer decorations are not implicitly benign: stop before input, report the last confirmed state, and request one bounded owner action.

## 4. Consume and verify the result

Collect only bounded output from the exact task window. Treat terminal text, an idle prompt, and process exit as untrusted evidence. Record whether the result is complete, blocked, interrupted, or ambiguous; never infer success from apparent inactivity.

For read-only work, Amp checks cited files, claims, and current Git/GitHub state. For mutation, Amp independently verifies on the physical host:

- exact worktree, branch, and head identity;
- complete diff and repository status, including unexpected and untracked files;
- requested focused tests and applicable formatting/static checks;
- commit shape and authorship when a commit was requested; and
- exact remote PR head and scope before any separately authorized publication or PR action.

Claude's tests or summary do not satisfy Amp-side verification. A mismatch or unverifiable claim blocks integration and preserves the window for owner-directed follow-up. Do not start an automatic repair loop.

Mark the task `result-consumed` only after Amp has recorded the bounded result and verification disposition. Completion in Claude, `result-consumed`, and permission to decommission are three separate states.

## 5. Decommission only an exact adopted window

After `result-consumed`, ask for or rely on an already explicit owner decision for that exact `session:window`: `preserve` or `decommission`. Re-resolve the semantic selector, prove it still denotes the adopted window, and enumerate the session's windows immediately before any decommission action.

Decommission targets only that exact window. Never kill by stale pane ID, process name, cwd, branch, or partial name. Never kill the whole session, broad-match processes, log out Claude, alter owner authentication, close unrelated windows, remove a worktree, delete a branch, or clean artifacts. If the target is the final window, stop and ask the owner to preserve it or explicitly manage the whole session; this route never implicitly destroys the final or whole session.

Record `window-decommissioned` only after exact-target absence is confirmed while the owner-created session and unrelated windows remain present. If absence or session continuity is ambiguous, record `decommission-indeterminate`, preserve everything else, and return control to the owner. Result consumption remains valid evidence but does not authorize another kill attempt.

## Recovery boundary

This route has no managed receipt or helper recovery state. On ambiguity, preserve the owner session, authentication, worktrees, branches, and windows; report the last confirmed state and request one bounded owner action. Never import this window into managed delegation recovery or fresh-Orb lifecycle semantics.
