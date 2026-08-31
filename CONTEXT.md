# Domain glossary

## Client modes

**Worker** — An interactive, TUI-based Amp client bound to a thread.
_Avoid_: Thread, interactive client

**Worker identity** — The canonical Amp thread ID. A thread may belong to only one configured worker on a machine; workspace, window, and workdir describe its local placement rather than its identity.

**Runner** — A non-interactive Amp client that makes a machine and working directory available for remote work. The retained Amux host layer registers, launches, and safely operates it.
_Avoid_: Worker, background worker

**Runner identity** — The canonical workdir. A directory may belong to only one configured runner workspace on a machine.

**Runner workdir** — A canonical existing directory owned by a runner. It may be a Git repository or worktree, but does not need to be; amux validates directory existence separately from tmux and process ownership.

**Runner window** — A tmux window whose name is derived deterministically from the runner workdir as `runner-<directory>-<path-hash>`. The canonical workdir, not the generated window name, is the runner's public identity.

**Runner maintenance** — A short-lived, machine-level scheduled operation that keeps Amp current and recycles verified runners when their installed Amp executable changes. The operating system schedules it; amux does not keep a resident supervisor.

**Thread** — The conversation identity underlying Amp work, independent of whether or how a local client is running.
_Avoid_: Worker when referring to the local TUI client

**Native child thread** — Work created directly with Amp `create_thread`, using a lean task prompt plus native parent/reply routing. It has no Amux contract or lifecycle identity unless exact persisted pre-cutover records independently prove an existing drain-eligible flow.
_Avoid_: Amux worker, spawned worker, automatically adopted thread

**Remote agent thread** — An Amp thread whose execution is enabled by a runner but whose lifecycle is owned by Amp or Agents Anywhere, not by the runner or amux.
_Avoid_: Runner-managed thread

**Workspace** — A named lifecycle group of workers and runners represented locally by one same-named tmux session. A workspace may span multiple repositories and workdirs.
_Avoid_: Session when referring to the configured lifecycle group

**Idempotency** — The guarantee that retrying the same desired-state operation converges without duplicating work. Conflicting state still fails, and creation retries stop as indeterminate rather than guessing when external identity cannot be recovered safely.

## Workspace lifecycle

**Launch** — Make configured, active work available locally. Launching may create local tmux windows and Amp clients, but does not change remote thread state.

**Aggregate launch** — Launch all configured workers and runners in scope. A workspace may contain workers, runners, or both; neither client mode is required to accompany the other.

**Health** — Active, mode-specific verification that configured clients are responsive or running as intended. Worker health uses a verified TUI response; runner health verifies its workdir, ownership, and `amp --no-tui` process.

**Sprawl** — A skill-only workflow that fans independent issues out through authenticated native Amp thread creation on exact Orbs or live runners/workdirs. Native-created children retain Amp parent/reply routing and are not automatically represented as Amux workers, groups, reports, shelves, or panes.

**Finish** — A skill-only completed-worker drain workflow for an existing Amux worker. One read-only preflight binds either merged-PR or explicit review-only completion to the exact worker/worktree, proves the index-only attached worktree can be removed while its branch is retained, and reports one exact action set for one approval. Finish parks a verified live worker first, never force-removes a worktree or implicitly deletes a branch, refuses dirty/shared/runner-owned/process-ambiguous state, and archives the exact thread through worker teardown only after normal worktree removal succeeds. Native-created unmanaged work does not acquire this lifecycle merely because it came from sprawl.

**Reconcile** — Explicitly repair drift between amux intent and external or runtime state. Worker reconciliation synchronizes shelf intent with remote archive state and removes a `workers.tsv` binding only when its canonical workdir is proven missing, no local worker runtime remains, and no blocked never-authorized report leaves an open obligation. Stale-registration removals preflight as one all-or-nothing plan; a refusal blocks every worker and aggregate runner removal in that plan but does not suppress independent shelf/archive synchronization for workers whose workdirs are present. A directory without a worker binding is reported and preserved because `workers.tsv`, not `reports.json` or `groups.tsv`, is the sole thread↔workdir authority. Runner reconciliation removes stale configuration for missing workdirs without silently adopting ambiguous processes.

**Restart** — Replace a running local client in place while preserving its configuration and remote thread.

**Park** — Stop local execution while preserving both the restore configuration and remote thread. Parked work can be launched again without first changing its remote state.

**Shelf intent** — An explicit local record that a configured worker is deliberately deferred. Shelf intent is authoritative for whether amux may launch the worker; Amp **archive** state separately controls remote thread visibility. Under [ADR 0007](docs/adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) as narrowed by [ADR 0008](docs/adr/0008-retain-machine-local-runner-host-and-drain-coordination.md), shelf intent is **drain-only migration state**, not a permanent product. Its worker coordination role can drain without removing retained Amux runner launch.
_Avoid_: Treating shelf intent as native Hide, or as a second long-lived visibility store beside Amp archive

**Shelve** — Historical Amux composite: record shelf intent, **archive** the remote thread (`amp threads archive`), and park (stop) verified local execution while preserving worker configuration. Shelved work must be unshelved before Amux may launch it. This is not native “Hide/Unhide”; `find_thread` `hidden:` / `snoozed:` are search filters only unless a distinct hide mutation API is later proven. After Amux shelf admission closes, defer remote visibility with native archive/unarchive alone (optional owner-local stop); do not add bulk migrate-to-hidden tooling.
_Avoid_: Hide, Unhide, snooze as synonyms for this operation

**Unshelve** — Historical Amux inverse: **unarchive** the remote thread (`amp threads archive --unarchive`), then remove shelf intent, without launching the local client.

**Archive (native)** — Amp operation that removes a thread from the active list / thread switcher while leaving it viewable by URL and includable via `--include-archived` or `archived:` search. This is the supported native replacement for the **remote** leg of Amux shelve after cutover.
_Avoid_: Hide when meaning this operation

**Unarchive (native)** — Amp inverse of archive; restores the thread to the active list without implying local Amux launch.

**Teardown** — Finish a worker by **archiving** its remote thread, removing its restore configuration and shelf intent, and stopping its verified local TUI client. Teardown never applies to a runner or implies teardown of remote agent threads.

**Remove** — Stop a worker or runner and remove its local configuration without changing remote thread state. Worker teardown additionally archives the worker's remote thread; remove does not.

**Pin** — Add work to restore configuration without changing local execution or remote thread state.

**Unpin** — Remove work from restore configuration without changing local execution or remote thread state.

**Machine scope** — Every configured worker and runner workspace on the current machine.

**Workspace scope** — Every configured worker and runner belonging to one workspace and its same-named tmux session.

**Window scope** — One configured interactive window within a workspace.

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
