# Domain glossary

## Client modes

**Worker** — An interactive, TUI-based Amp client bound to a thread.
_Avoid_: Thread, interactive client

**Worker identity** — The canonical Amp thread ID. A thread may belong to only one configured worker on a machine; workspace, window, and workdir describe its local placement rather than its identity.

**Runner** — A non-interactive Amp client that makes a machine and working directory available for remote work.
_Avoid_: Worker, background worker

**Runner identity** — The canonical workdir. A directory may belong to only one configured runner workspace on a machine.

**Runner workdir** — A canonical existing directory owned by a runner. It may be a Git repository or worktree, but does not need to be; amux validates directory existence separately from tmux and process ownership.

**Runner window** — A tmux window whose name is derived deterministically from the runner workdir as `runner-<directory>-<path-hash>`. The canonical workdir, not the generated window name, is the runner's public identity.

**Runner maintenance** — A short-lived, machine-level scheduled operation that keeps Amp current and recycles verified runners when their installed Amp executable changes. The operating system schedules it; amux does not keep a resident supervisor.

**Thread** — The conversation identity underlying Amp work, independent of whether or how a local client is running.
_Avoid_: Worker when referring to the local TUI client

**Remote agent thread** — An Amp thread whose execution is enabled by a runner but whose lifecycle is owned by Amp or Agents Anywhere, not by the runner or amux.
_Avoid_: Runner-managed thread

**Workspace** — A named lifecycle group of workers and runners represented locally by one same-named tmux session. A workspace may span multiple repositories and workdirs.
_Avoid_: Session when referring to the configured lifecycle group

**Idempotency** — The guarantee that retrying the same desired-state operation converges without duplicating work. Conflicting state still fails, and creation retries stop as indeterminate rather than guessing when external identity cannot be recovered safely.

## Workspace lifecycle

**Launch** — Make configured, active work available locally. Launching may create local tmux windows and Amp clients, but does not change remote thread state.

**Aggregate launch** — Launch all configured workers and runners in scope. A workspace may contain workers, runners, or both; neither client mode is required to accompany the other.

**Health** — Active, mode-specific verification that configured clients are responsive or running as intended. Worker health uses a verified TUI response; runner health verifies its workdir, ownership, and `amp --no-tui` process.

**Sprawl** — A skill-only workflow that fans independent issues out into instructed interactive workers in separate worktrees. Sprawl does not provision runners or create remote agent threads.

**Finish** — A skill-only post-merge workflow for a sprawled worker. Finish refuses to delete a worktree that is unexpectedly owned by a runner and performs worker teardown only after Git and worktree cleanup succeeds.

**Reconcile** — Explicitly repair drift between amux intent and external or runtime state. Worker reconciliation synchronizes shelf intent with remote archive state; runner reconciliation removes stale configuration for missing workdirs without silently adopting ambiguous processes.

**Restart** — Replace a running local client in place while preserving its configuration and remote thread.

**Park** — Stop local execution while preserving both the restore configuration and remote thread. Parked work can be launched again without first changing its remote state.

**Shelf intent** — An explicit local record that a configured worker is deliberately deferred. Shelf intent is authoritative for whether amux may launch the worker; Amp archive state separately controls remote thread visibility.

**Shelve** — Defer a worker by recording shelf intent, hiding its remote thread, and stopping local execution while preserving worker configuration. Shelved work must be unshelved before it can be launched.

**Unshelve** — Make a shelved remote thread active again without launching it locally.

**Teardown** — Finish a worker by hiding its remote thread, removing its restore configuration, and stopping its verified local TUI client. Teardown never applies to a runner or implies teardown of remote agent threads.

**Remove** — Stop a worker or runner and remove its local configuration without changing remote thread state. Worker teardown additionally hides the worker's remote thread; remove does not.

**Pin** — Add work to restore configuration without changing local execution or remote thread state.

**Unpin** — Remove work from restore configuration without changing local execution or remote thread state.

**Machine scope** — Every configured worker and runner workspace on the current machine.

**Workspace scope** — Every configured worker and runner belonging to one workspace and its same-named tmux session.

**Window scope** — One configured interactive window within a workspace.
