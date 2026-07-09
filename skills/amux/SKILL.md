---
name: amux
description: "Manages local Amp tmux workspace lifecycle with amux: restore, inspect, pin/unpin, park, shelve/unshelve, spawn interactive workers, runner lifecycle, health checks, and verified teardown. Use for local tmux/Amp restore orchestration and skill-only /amux health and /amux sprawl; not for Amp-native Agents Anywhere remote creation except runner setup. Triggers: 'Pin it', 'Unpin it', 'forget this on restore', 'Park it', 'Shelve this', 'defer this workspace', 'hide it for now', 'Restore my workspace', 'Spawn a worker for', 'Teardown this worker', 'Doctor amux', '/amux health', '/amux sprawl', '/amux finish'."
---

# amux

Use `amux` to manage local Amp/tmux workspace restore state instead of editing TSV files manually. Current config lives in `~/.config/amux/workspaces.tsv` and `~/.config/amux/runners.tsv`; legacy installs may still have `~/.config/amp-tmux`, migratable with `amux migrate-config`.

## Route by side-effect domain

Keep these domains separate and choose the smallest command that mutates only the requested domain:

- **Restore config**: `workspaces.tsv` rows describing what should be restored later.
- **Runner config**: `runners.tsv` rows describing local `amp --no-tui` runner intent.
- **Live local tmux/Amp**: tmux sessions/windows and local Amp CLI processes.
- **Remote Amp thread state**: hosted Amp threads; `spawn` creates one, `shelve`/`unshelve` archive-state toggles deferred work, verified `teardown` archives one.

Use `amux` for local tmux workspace lifecycle: list, doctor, launch, pin/unpin, park, shelve/unshelve, spawn, teardown, prune stale rows, and runner setup. If the user wants to create or control a new remote agent from ampcode.com, prefer Amp-native Agents Anywhere after a runner exists for the target machine/workdir.

Runner rows are not restore rows. Runner commands manage local `amp --no-tui` runner intent and windows; they do not create, continue, archive, or list remote Amp threads.

## Always preserve these invariants

- Plain `amux list [workspace]` is local-only and must stay instant; it does not call Amp.
- `amux workspaces` is the local-only discovery step for all-workspace health checks. It lists restore workspace names only by default; use `--include-runners` only for machine inventory that should include runner-only workspace names.
- `amux list --status`, `--active`, `--shelved`, and `amux shelved` inspect remote Amp archive state. Unfiltered `--status` may show `unknown`; filtered modes fail closed when Amp status cannot be confirmed.
- `amux launch` skips archived/shelved rows. Deferred work resumes only after explicit `amux unshelve ...`, then `amux launch ...` if live tmux restoration is desired.
- A single explicit workspace argument defaults the tmux session to that workspace: `amux launch amux`, `amux doctor amux`, `amux runner launch amux`, `amux runner park amux <window>`, and `amux spawn ... amux` target workspace/session `amux`. Older shared-session layouts still pass an explicit session, e.g. `amux launch mac Amp` or `amux runner park mac <window> Amp`. No-arg legacy defaults may still use workspace `mac` and session `Amp`, except `amux runner launch`, which starts all configured runner workspaces.
- `pin`/`unpin` mutate restore config only. `park` stops only verified local tmux/Amp. `shelve` archives/hides deferred remote thread(s), preserves restore rows, and stops verified local windows. `teardown` is full cleanup: archive verified thread, remove row, stop verified local window.
- Prefer `pin-current`, `unpin-current`, `park-current`, and `shelve-current` from inside the target tmux pane because they resolve `$TMUX_PANE` rather than the currently focused tmux client.
- Do not edit `workspaces.tsv` or `runners.tsv` manually unless the CLI cannot express the needed change.

## Common trigger routing

- **Park it**: `amux park-current`. This preserves the restore row and active remote thread; verify local disappearance if needed.
- **Pin it**: `amux pin-current <thread-id-or-url>`. Ask for the current thread ID/URL if it is not available.
- **Unpin it** / **forget this on restore**: `amux unpin-current`; do not stop tmux and do not archive the thread.
- **Shelve this** / **defer this workspace** / **hide it for now**: use `amux shelve-current [workspace] <thread-id-or-url>` from the pane, or `amux shelve --thread ...`, `amux shelve <workspace> <window> [session]`, or `amux shelve --workspace ...` for already-pinned work. Do not substitute `park-current` when the intended outcome is hidden/deferred remote work.
- **Show shelved work** / **list deferred work**: `amux shelved [workspace]` or `amux list --shelved [workspace]`.
- **Unshelve this** / **resume deferred work**: `amux unshelve ...`, then `amux launch <workspace> [session]` if tmux windows should be restored.
- **Restore my workspace**: `amux launch` for legacy defaults, `amux launch <workspace>` for same-named session, or `amux launch <workspace> <session>` for shared-session layouts.
- **Check amux** / **doctor amux**: `amux doctor`, `amux doctor <workspace>`, or `amux doctor <workspace> <session>` with the same session-defaulting rules.
- **/amux health <workspace>** / **check worker responsiveness before replacement**: load [`reference/workflows.md`](reference/workflows.md), then run the skill-only health workflow. For all-workspace health checks, start with `amux workspaces`. Inspect amux/tmux state, ping only verified Amp panes with one submitted read-only prompt using unique `AMUX_HEALTH_CHECK` tokens, report classifications first, and do not replace anything.
- **Spawn a worker for ...**: load [`reference/workflows.md`](reference/workflows.md), then use `amux spawn ...` only for fresh interactive local Amp/tmux workers; prefer Amp-native Agents Anywhere for remote agent creation after a runner exists.
- **Teardown this worker** / **archive and clean this up**: use `amux teardown` only when the user wants full verified worker cleanup.
- **/amux sprawl #12 #34 ...**: skill-only orchestration around `gh`, `git worktree`, and `amux spawn`; inspect dependencies before creating branches, worktrees, tmux windows, restore rows, or remote threads.
- **/amux finish** / **post-merge cleanup**: finish GitHub/git/release/worktree cleanup first, then run `amux teardown` last.

## Load detailed reference only for the active branch

- For exact CLI forms, session-defaulting examples, command semantics, self-update behavior, and the side-effect matrix, read [`reference/commands.md`](reference/commands.md).
- For health checks, spawn, sprawl, teardown, finish, current-session, and explicit workspace procedures, read [`reference/workflows.md`](reference/workflows.md).
- For stuck/misplaced worker replacement, verification commands, and mutation safety checks, read [`reference/troubleshooting.md`](reference/troubleshooting.md).
- To check whether trigger phrases still map to the intended behavior, read [`reference/trigger-phrases.md`](reference/trigger-phrases.md).

## Safety guardrails

- Do not store secrets in window names, workdirs, or thread identifiers; prefer thread IDs or `https://ampcode.com/threads/...` URLs.
- Before testing mutations, prefer a temp config with `--config "$tmp/workspaces.tsv"` so live restore rows are not changed accidentally.
- Do not run live `amux spawn`, `teardown`, `park-current`, `pin-current`/`store-current`, or `unpin-current`/`remove-current` against the default config unless the user asked to change that side-effect domain.
- If a thread/window looks missing, start with `amux doctor <workspace>` and `amux list <workspace>`. Prefer tmux window/pane metadata over `ps`; do not treat the tmux server command line as proof of a live Amp thread.
