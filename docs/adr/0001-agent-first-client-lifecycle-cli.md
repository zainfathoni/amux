---
status: accepted
---

# Make the client lifecycle CLI agent-first

amux will model interactive thread-bound clients as workers and non-interactive workdir-bound clients as runners. Canonical mode-specific commands live under `amux worker` and `amux runner`; top-level lifecycle commands aggregate both modes when they share the same semantics, while inherently worker-only commands may retain concise top-level forms. This replaces legacy implicit worker semantics with a predictable resource hierarchy designed primarily for agents while preserving a few simple human commands.

## Accepted interface rules

- A workspace is a lifecycle group represented by one same-named tmux session; the canonical API has no separate session selector.
- A worker's machine-wide identity is its canonical Amp thread ID. A runner's machine-wide identity is its canonical workdir.
- Runner pinning requires stable Git worktree ownership: the repository's primary worktree is inherently stable, while a linked worktree must already be locked. amux verifies this invariant but does not own Git worktree lock or unlock operations.
- Runner window names are generated as `runner-<directory>-<canonical-path-hash>` and are not public identifiers.
- `list`, `launch`, `park`, `restart`, `remove`, and `doctor` aggregate workers and runners at the top level; their mode-specific forms narrow scope.
- `spawn`, `shelve`, `unshelve`, and `teardown` are worker-only and may have concise top-level forms. Teardown changes a worker's remote thread state and never applies to a runner or remote agent thread.
- `pin` and `unpin` require an explicit worker or runner namespace because their identity contracts differ.
- Bare `amux` remains the human convenience for launching workers only, while `amux launch` launches workers and runners.
- Bulk mutation requires an explicit `--all`; launch and read-only discovery may naturally target all configured resources.
- Commands never prompt. `--dry-run` (`-n`) exposes plans without mutation.
- Human-readable output is the default. `--json` (`-j`) is the stable agent contract, with results separated into successful, skipped, and failed resources.
- JSON output uses one versioned envelope from its first release; canonical worker and runner identities appear as discriminated resource IDs, and agents must ignore unknown optional fields within a schema version.
- Dry-run JSON classifies prospective mutations under `planned`; `successful` is reserved for actions actually completed, while known no-ops and preflight errors remain `skipped` and `failed`.
- Bulk operations preflight the complete plan, then continue independent actions after runtime failures. Exit `0` means no failures, `1` means partial or complete runtime failure, and `2` means request or preflight rejection before mutation.
- Lifecycle operations are idempotent desired-state commands. Agent-driven spawn requires a stable idempotency key and a persisted operation record; an interrupted external creation with unrecoverable identity is reported as indeterminate and never blindly retried.
- Mutating commands and scheduled maintenance share one bounded machine-level operation lock held from preflight through persisted results; concurrent mutation fails with structured ownership metadata instead of racing configuration writes or side effects.
- Canonical agent selectors use long flags. Human shorthands are fixed across commands: `-w` workspace, `-W` window, `-d` workdir, `-t` thread, `-m` mode, `-j` JSON, `-n` dry-run, and `-h` help.
- Help is contextual at every command-tree level and is also addressable through `amux help ...`.
- Missing ephemeral worktree runners are repaired explicitly through runner reconciliation; launch never silently mutates runner configuration.
- `shelves.tsv` explicitly records shelf intent by canonical worker thread ID. Local shelf intent controls launch eligibility, while Amp archive state remains separate synchronized remote state and drift is reported rather than inferred away.
- Shelving records intent before archiving and parking; unshelving removes intent only after unarchiving. Partial synchronization remains visible and idempotently retryable instead of being rolled back.
- `reconcile` is an aggregate lifecycle command with mode-specific worker and runner implementations. It accepts current, canonical resource, workspace, or explicit machine-wide scope; workspace reconciliation jointly preflights both modes.
- Runner reconciliation diagnoses conflicting Amp-owned PID markers but never deletes them without a supported conditional ownership protocol. Runner launch verifies that the exact created pane survives startup as the expected `amp --no-tui` process before reporting success.
- Automatic runner maintenance is one short-lived machine-level job scheduled by a systemd user timer or macOS LaunchAgent, not a resident supervisor per runner. It updates Amp once, recycles verified runners only when needed, records outcomes, and accepts possible interruption until Amp exposes drain support.
- Runner maintenance installation is explicit and dry-runnable. It schedules a persistent six-hour check with bounded jitter and records whether Amp updates are owned by Amp's self-updater or an external package manager; only changed executables trigger verified running-runner restarts.
- Canonical `amux workspace list` and its exact `amux workspaces` convenience alias list the union of worker and runner workspaces; mode filters narrow the read-only result.
- `list` is configuration-only and deterministic. Worker filters use explicit local shelf intent (`shelved` or `unshelved`); observed tmux, workdir, and remote state belongs to `doctor` or skill-only health checks.
- Configuration is selected by directory through `--config-dir` (`-c`) or `AMUX_CONFIG_DIR`, containing `workers.tsv`, `runners.tsv`, and `shelves.tsv`; no mode-specific file path represents the whole configuration.
- Legacy configuration is migrated only through explicit, dry-runnable `migrate-config`; ordinary commands report required migration without writing files, and legacy files remain available for rollback.
- Legacy command aliases (`store`, unpin-style `remove`, `shelved`, and `self-update`) are removed. Old positional forms fail with remediation, while the new lifecycle `remove` accepts only unambiguous selectors or `--all`.
- `~/.local/bin/amux` is the canonical self-updating installation across shells and scheduled maintenance. Self-update refuses package-manager/toolchain-owned paths including mise, and installation diagnostics expose every PATH candidate and version.
- Worker teardown completes archive and configuration cleanup when its verified local process is already absent; absence is a benign skip, while ambiguous or unmanaged identity still fails closed.
- The bundled `/amux` skill is versioned as part of the product surface. Normal global installation uses the `skills` CLI from the public repository; local symlinking is documented only as a development workflow.
- GitHub Pages keeps a concise skill introduction and links to dedicated skill documentation at `https://amux.zainf.dev/skill/`, including terminology, trigger routing, skill-only workflows, side effects, and agent-facing examples.
- Skill-only `/amux health` aggregates workers and runners by default, while using mode-specific probes and supporting explicit mode and workspace filters.
- Skill-only `/amux sprawl` remains worker-only until Amp exposes reliable CLI-driven creation and instruction of remote agent threads through runners.
- Skill-only `/amux finish` is worker-only and fails closed if the worktree has independently acquired runner ownership; runner removal is never implicit.

## Consequences

The redesign intentionally breaks legacy positional defaults, explicit workspace/session divergence, and implicit worker-only meanings for aggregate lifecycle verbs. Existing configuration and the bundled `/amux` skill must migrate together. Machine-readable identities and result schemas become compatibility contracts even though human formatting may evolve.
