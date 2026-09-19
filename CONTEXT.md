# Domain glossary

## Client modes

**Worker (historical)** — A removed interactive, TUI-based Amux client formerly bound to a thread. Worker rows are inert evidence, not active clients.
_Avoid_: Using worker for a native Amp thread or retained runner

**Worker identity (historical)** — The canonical Amp thread ID stored in old worker evidence. It grants no current lifecycle authority.

**Runner** — A non-interactive native Amp client that makes one startup directory plus zero or more discovered or explicit directories available for remote work.
_Avoid_: Worker, background worker

**Runner profile** — Declarative machine configuration for one Runner: its name, native Runner ID, startup directory, discovery choice, explicit served directories, and remote-terminal choice.
_Avoid_: Workspace, per-workdir runner

**Runner ID** — Stable native Amp identity for one Runner. Runner IDs are unique on a machine and independent of served directory paths.
_Avoid_: Profile name, workdir identity

**Startup directory** — Stable directory from which a Runner starts and against which native Amp persists dynamically added directories.
_Avoid_: Primary workdir

**Served directory** — Existing Git or non-Git directory made available by a Runner, either as its startup directory or through explicit native directory configuration.
_Avoid_: Runner identity

**Discovered directory** — Git checkout selected by native Amp beneath a Runner's startup directory when directory discovery is enabled.

**Runner service** — A systemd user service or launchd agent generated from one Runner profile that executes native Amp directly when that user's service manager starts it.
_Avoid_: Amux supervisor

**Legacy runner workdir** — Historical canonical directory identity used by the per-workdir Amux registry and tmux lifecycle.
_Avoid_: Served directory for new profiles

**Runner window** — A tmux window whose name is derived deterministically from the runner workdir as `runner-<directory>-<path-hash>`. The canonical workdir, not the generated window name, is the runner's public identity.

**Runner maintenance (historical)** — Legacy scheduled operation that updated Amp and recycled per-workdir tmux runners. Native multi-directory runners update themselves.

**Thread** — The conversation identity underlying Amp work, independent of whether or how a local client is running.
_Avoid_: Worker when referring to the local TUI client

**Native child thread** — Work created directly with Amp `create_thread`, using a lean task prompt plus native parent/reply routing. It has no Amux contract or lifecycle identity; historical Amux records remain inert evidence.
_Avoid_: Amux worker, spawned worker, automatically adopted thread

**Remote agent thread** — An Amp thread whose execution is enabled by a runner but whose lifecycle is owned by Amp or Agents Anywhere, not by the runner or amux.
_Avoid_: Runner-managed thread

**Workspace** — A named group of retained runners represented locally by one same-named tmux session. It may span multiple repositories and workdirs.
_Avoid_: Session when referring to the configured lifecycle group

**Idempotency** — The guarantee that retrying the same desired-state operation converges without duplicating work. Conflicting state still fails, and creation retries stop as indeterminate rather than guessing when external identity cannot be recovered safely.

## Workspace lifecycle

**Launch** — Start configured machine-local runners in tmux without changing remote thread state. Bare `amux` and `amux launch --all` launch every configured runner.

**Health** — Verify each retained runner's exact workdir, tmux ownership, and `amp --no-tui` process.

**Sprawl** — A skill-only workflow that fans independent issues out through authenticated native Amp thread creation on exact Orbs or live runners/workdirs. Native-created children retain Amp parent/reply routing and are not automatically represented as Amux workers, groups, reports, shelves, or panes.

**Reconcile** — Inspect runner registry/runtime drift. Preserve rows and processes unless exact retained-runner safety evidence authorizes a change; never adopt an ambiguous process or consult historical worker coordination state as authority.

**Restart** — Replace a running local client in place while preserving its configuration and remote thread.

**Park** — Stop one exact runner while preserving its registry row and remote thread state.

**Remove** — A retained runner operation reserved for future authoritative process/catalog absence evidence. It currently fails closed and never operates on historical worker rows.

**Unpin** — Remove one exact runner binding after local inspection positively proves that runner absent. It never stops a process or removes a workdir.

**Runner teardown** — Retire one exact machine-local runner and its clean secondary Git worktree while preserving its branch. Native Amp thread archival is a separate native action.
_Avoid_: Worker teardown, thread teardown

**Pin** — Add or update one exact canonical runner workdir binding without changing remote thread state. Updating a live Runner's launch configuration safely replaces its exact local process when explicitly requested.

**Machine scope** — Every configured runner workspace on the current machine.

**Workspace scope** — Every configured runner in one same-named tmux session.

Historical worker, shelf, group, report, callback, deadline, finish, and worker-teardown terms describe inert evidence only. They are not active Amux operations and must not be revived from older documentation.

## Delegation admission

**Capacity observation** — A time-bounded report of provider usage limits and remaining availability, with enough provenance to determine which capacity it describes. An observation is evidence about current capacity, not permission to launch.

**Capacity pool** — One provider-governed allowance against which related model use is charged. A pool has a stable, non-secret identity that is distinct from an account label, credential, organization name, or displayed usage window.

**Charge route** — The provider-governed path that determines which capacity pool or billing mechanism a model invocation consumes. Authentication method, entitlement, and model selection constrain a charge route but do not individually prove it.

**Reserve floor** — The minimum capacity that must remain in a capacity window after an admitted operation's maximum impact. A floor protects owner capacity; it is not a target for utilization.

**Admitted impact** — A conservative upper bound on how much of each governing capacity window an admitted operation may consume, expressed in the same authoritative unit as that window.

**Autonomous admission** — Permission to begin a delegation without a contemporaneous owner capacity decision because trusted evidence proves every reserve floor remains protected after admitted impact.

**Unknown-capacity acknowledgement** — A fresh owner decision accepting that one exact delegation may have unquantified reserve impact. It never turns unknown capacity into trusted evidence or overrides a known reserve-floor violation.

**Exact model binding** — The guarantee that one explicitly selected provider model remains unchanged across admission, execution, evidence, and lifecycle handling. Aliases, defaults, normalization, fallback, and substitution do not satisfy exact binding.

**Frozen handoff** — A delegation result whose writer authority has ended and whose declared artifact state is preserved for independent review. A frozen handoff is neither acceptance nor integration authorization.

## Removal safety

**Removal safety verdict** — A classification of whether a worktree's tip commit remains reachable after removing that worktree. The verdict is based on ref coverage and the accepted ladder; it is not authorization to remove a worktree, delete a branch, or finish lifecycle work.
_Avoid_: Merge status, removal authorization

**Ref coverage** — The local branch, remote-tracking branch, and tag refs that contain a commit, determined only from `refs/heads`, `refs/remotes`, and `refs/tags`. Coverage asks whether another ref holds the commit after a worktree `HEAD` is removed.
_Avoid_: Stash coverage, merge status

**Vanished worktree** — A Git worktree registration whose directory is absent. It remains a classification target: its recorded tip is assessed for coverage before any unlock or prune, and its dirty state is unknowable rather than clean.
_Avoid_: Prunable-only worktree, clean worktree
