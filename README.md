# amux

`amux` is the local tmux lifecycle layer for [Amp](https://ampcode.com/). It manages interactive **workers**, non-interactive **runners**, and named **workspaces** with explicit, agent-safe side effects.

- A **worker** is an interactive Amp client identified machine-wide by its canonical thread ID.
- A **runner** is an `amp --no-tui` client identified machine-wide by its canonical workdir. It enables Amp Agents Anywhere but does not own remote agent threads.
- A **workspace** groups workers and runners in one same-named tmux session.

Website: [amux.zainf.dev](https://amux.zainf.dev) · Skill guide: [amux.zainf.dev/skill/](https://amux.zainf.dev/skill/)

## Install

Requirements: Amp CLI and tmux. Building from source also requires the Go version in `go.mod`.

### Shell installer

Install the latest Linux or macOS release at the canonical self-updating path, `~/.local/bin/amux`:

```sh
curl -fsSL https://amux.zainf.dev/install.sh | sh
```

The installer detects arm64/amd64, downloads the matching GitHub release archive and published checksum, verifies SHA-256, and atomically replaces the canonical binary without touching an existing installation on failure. It reports any PATH setup or shadowing and prints the exact `amux install doctor` command to run next.

For automation that needs a pinned release, set a published tag:

```sh
curl -fsSL https://amux.zainf.dev/install.sh | AMUX_VERSION=v0.2.1 sh
```

### Homebrew

```sh
brew install zainfathoni/tap/amux
brew upgrade amux
```

Homebrew owns that installation. `amux update` deliberately refuses Homebrew, mise, asdf, Nix, and system-package paths.

### Manual release fallback

If the shell installer cannot be used, release archives are available for Linux and macOS on amd64 and arm64. Select the matching archive, verify its separately published checksum, then install the binary at the canonical self-updating path. For example, on Linux amd64:

```sh
curl -LO https://github.com/zainfathoni/amux/releases/latest/download/amux-linux-amd64.tar.gz
curl -LO https://github.com/zainfathoni/amux/releases/latest/download/amux-linux-amd64.tar.gz.sha256
sha256sum -c amux-linux-amd64.tar.gz.sha256
tar -xzf amux-linux-amd64.tar.gz
install -D -m 0755 amux-linux-amd64/amux ~/.local/bin/amux
```

Keep `~/.local/bin` early on `PATH`. Then:

```sh
amux install doctor
amux update
amux --json install doctor
```

`install doctor` reports the running executable, canonical target, every PATH candidate and version, shadowing, and scheduled-maintenance drift. Clients older than the agent-first CLI must be bootstrapped by replacing `~/.local/bin/amux` directly rather than invoking removed `self-update` syntax.

### Source

```sh
make build
install -D -m 0755 amux ~/.local/bin/amux
```

## Install the `/amux` skill

Install globally from the public repository with [skills.sh](https://skills.sh/):

```sh
npx skills add zainfathoni/amux --skill amux --global
```

The skill teaches agents the canonical selectors, side-effect boundaries, and skill-only health/sprawl/finish workflows. See the [dedicated skill guide](https://amux.zainf.dev/skill/). Local symlinking is only for contributors developing the bundled skill; see [CONTRIBUTING.md](CONTRIBUTING.md#develop-the-bundled-skill).

The skill also includes an explicit-only, capability-gated Darwin/Linux **experimental read-only Claude delegation** route. It is an unstable skill-owned helper, not an `amux` lifecycle command, worker, runner, group member, or compatibility promise. It launches one policy-confined interactive thinker only after explicit invocation, accepts bounded semantic reports through a strict local MCP surface, recovers them from a private machine-local receipt, and keeps notification, delivery, acknowledgement, and identity-verified parking separate. It does not provide OS-level read confinement, Linux mutation, automatic follow-up injection, autonomous selection without trustworthy capacity, teleport/adoption, synthetic Amp identity, automatic cleanup, or evidence that a real delegation pilot succeeded. Load the bundled skill's experimental workflow and contract before use.

## Quick start

The CLI writes schema-marked registries under `~/.config/amux` by default. Select another directory with `--config-dir` (`-c`) or `AMUX_CONFIG_DIR`. Do not create or edit registry rows manually when a command exists.

Pin a known worker explicitly, then manage it by canonical thread identity:

```sh
amux worker pin --workspace amux --window docs --workdir ~/Code/amux --thread T-example
amux worker list --thread T-example
amux worker park --thread T-example
amux worker launch --thread T-example
```

Pin and unpin change configuration only. Park stops a verified local client but preserves configuration and remote state. Launch restores local execution without changing remote thread state.

For a workdir-bound runner:

```sh
amux runner pin --workspace amux --workdir ~/Code/amux-runner
amux runner launch --workdir ~/Code/amux-runner
```

Runner workdirs may be Git worktrees or any other existing directory, such as a notes vault or tool configuration directory.

## Command model

Run `amux help`, `amux help worker`, or `amux help runner <command>` for current contextual help.

### Aggregate routes

Top-level lifecycle routes operate on workers and runners:

```sh
amux list --all
amux launch --workspace amux
amux park --all
amux restart --all
amux remove --all
amux doctor --all
amux reconcile --workspace amux
```

`list`, `launch`, `park`, `restart`, `remove`, `doctor`, and `reconcile` aggregate both modes. Use `amux worker ...` or `amux runner ...` to narrow. Bare `amux` remains a convenience that launches all configured workers only; `amux launch` launches both modes. Launch is the no-selector bulk exception; other machine-wide mutations require explicit `--all`.

### Canonical selectors

Agents should use long flags:

| Selector | Meaning |
| --- | --- |
| `--thread`, `-t` | canonical worker identity |
| `--workdir`, `-d` | canonical runner identity |
| `--workspace`, `-w` | worker/runner lifecycle group and same-named tmux session |
| `--window`, `-W` | worker creation placement metadata |
| `--mode`, `-m` | spawn mode or workspace-list filter |
| `--current` | resource owning the invoking pane/workdir |
| `--all` | explicit machine-wide scope |

Worker and runner pin/unpin require a namespace because their identities differ:

```sh
amux worker pin --workspace amux --window docs --workdir ~/Code/amux --thread T-example
amux worker unpin --thread T-example
amux runner pin --workspace amux --workdir ~/Code/amux-runner
amux runner unpin --workdir ~/Code/amux-runner
```

`amux workspace list` and its exact `amux workspaces` alias list the worker/runner workspace union. Add `--mode worker` or `--mode runner` to filter.

Removed aliases and positional forms fail with remediation. In particular, do not use `store`, top-level `pin`, `pin-current`, `unpin-current`, `park-current`, `shelve-current`, `shelved`, `prune-archived`, `self-update`, positional session selectors, `--config`, or legacy config environment variables.

## Worker-only lifecycle

`spawn`, `shelve`, `unshelve`, and `teardown` are worker-only and have concise top-level routes.

```sh
amux --dry-run spawn --workspace amux --window install-diagnostics --workdir ~/Code/amux-issue-110 --mode medium --title-prefix '#110' --group amux-110 --message-file /tmp/issue-110.md --idempotency-key issue-110
```

An exact `#<issue>` title prefix owns issue identity. The window must be an issue-unprefixed semantic slug; obvious duplicates such as `issue-110-install-diagnostics` are rejected before side effects. `--message`, `--message-file`, and `--message-stdin` are mutually exclusive. Spawn requires a stable idempotency key. If verification times out after delivery starts, rerun the complete identical spawn request with `--reconcile` only after read-only inspection identifies either the exact assignment in the provisioned thread or one unambiguous fresh active alternate receiver. amux verifies the immutable request hash, exact assignment, workdir, freshness, empty provisioned residue, and unstarted receiver; it then creates or verifies only the authoritative worker row and local tmux client without creating a thread or resubmitting the message. Alternate adoption is rejected when ownership, content, freshness, activity, or local identity is ambiguous. Other indeterminate outcomes remain terminal.

```sh
amux shelve --thread T-example
amux worker list --shelf shelved
amux unshelve --thread T-example
amux worker launch --thread T-example
amux teardown --thread T-example
```

- Shelve records local shelf intent before archiving and parking, preserving worker configuration.
- Unshelve unarchives and removes intent only after success; it does not launch.
- Teardown archives the verified thread, removes worker and shelf configuration, and stops the verified local client. A verified already-absent local process is a benign skip; ambiguity still fails closed.
- Remove stops a worker and deletes local configuration without archiving. Unpin only deletes configuration and does not stop it.
- Reconcile explicitly synchronizes worker shelf/remote drift or repairs stale runner ownership. Launch never performs hidden reconciliation.

## Durable work groups

Work groups are explicit, durable many-to-many associations between Amp thread IDs and byte-preserving group IDs. Declare a group with one coordinator, then add any worker, archived, recovered, evidence, duplicate, or runner-managed thread by its canonical ID:

```sh
amux group declare --group amux-131 --thread T-coordinator
amux group add --group amux-131 --thread T-worker
amux group coordinator --group amux-131 --thread T-worker
amux group list
amux group show --group amux-131
amux group reconcile --group amux-131
amux group reconcile --thread T-worker
amux group reconcile --all
amux group remove --group amux-131 --thread T-worker
```

Group IDs map byte-for-byte to Amp labels and must match `^[a-z0-9]+(?:-[a-z0-9]+)*$`; amux never normalizes or infers them from titles, branches, issue numbers, or existing labels. Local `groups.tsv` intent is authoritative and survives worker/tmux/worktree lifecycle changes. `group list` and `group show` are deterministic local-only reads.

The bundled issue-coordination workflow uses repository-qualified identities. For this repository, issue `#131` uses group/Amp label `amux-131`, and its first worker uses report ID `amux-131-worker-1`; another repository uses the equivalent `<repository-slug>-131` and `<repository-slug>-131-worker-1`. This convention does not narrow the generic group-ID contract. Legacy `issue-*` identities and purpose-specific groups such as `pr-181-review` remain valid and are never migrated, renamed, removed externally, or rewritten.

Worker spawn accepts repeatable `--group <id>`. amux validates and deterministically sorts/deduplicates the complete set before creation, binds memberships only to the final authoritative receiving thread, persists all local intent before add-only label synchronization, and resumes a partial grouping failure with the same idempotency key without recreating or resubmitting the worker.

External synchronization is deliberately add-only. Declare, add, coordinator changes, and reconcile use Amp's additive label command only after a version and exact semantic-help capability check. Additive failures retain local intent as visible drift. Local removal cannot remove the Amp label, succeeds with a warning that the external label may remain indefinitely, and never claims exact synchronization. Use `--dry-run` to preflight and inspect any group mutation.

### Durable worker reports and finish authorization

Reports are persisted locally before callback notification. A stable report ID can progress between `ready` and `blocked`; `merged` is terminal and is accepted only after the group coordinator records a separate durable finish authorization. Acknowledgement never implies authorization, and neither `ready`, `blocked`, callback success, nor deadline expiry authorizes cleanup.

```sh
amux report submit --report-id amux-133-worker-1 --group amux-133 --thread T-worker \
  --status ready --issue '#133' --pr https://github.com/owner/repo/pull/123 \
  --summary implementation-tests-review-pr-ci-complete
amux report pending --group amux-133
amux report history --report-id amux-133-worker-1
amux report acknowledge --report-id amux-133-worker-1
amux report authorize-finish --report-id amux-133-worker-1 \
  --thread T-coordinator --reference coordinator-verification
```

Register an exact live interactive coordinator pane explicitly, and clear it when it should no longer receive wake-ups:

```sh
amux callback register --group amux-133 --thread T-coordinator --pane %16
amux callback clear --group amux-133
```

The single lease for each config-directory/group is machine runtime state, not portable group/report history. Registration captures the exact pane, session/window IDs and names, start/current command, canonical workdir, PID, process start identity, generation, and registration time. Every report submission—including an identical retry—freshly verifies all metadata before sending `AMUX_REPORT group=<group> report=<id>` plus Enter. Missing or changed leases fail separately after the durable report is confirmed; amux never guesses another pane. A sent token is only a best-effort wake-up and never acknowledgement or finish authorization.

Identical replay is a benign durable-state skip that may retry notification; conflicting reuse and illegal transitions reject before mutation. `reports.json` also carries coordinator-owned soft-deadline generations, demonstrated external-wait evidence, and durable stale/overdue/blocker diagnostics. These records provide a nearest-deadline scheduling seam only: amux creates no supervisor, sleeping worker timer, polling loop, or destructive expiry action.

### Coordinator workflow

The bundled `/amux` skill provides the complete coordinator procedure. In summary: inspect native dependencies and active PR/branch/worktree/API overlap; fetch and create dedicated worktrees from fresh `origin/main`; use semantic issue-unprefixed windows and explicit `--mode medium` unless overridden; declare the group and register the exact verified coordinator pane; then spawn with `--group` so membership binds only after authoritative alternate-thread adoption.

Workers use one stable report ID for `blocked`, `ready`, and terminal `merged`. `ready` means implementation, tests, one review, PR, and normal CI are complete. A callback token only wakes the coordinator. The coordinator acknowledges receipt separately, independently verifies PR URL/head/scope/mergeability/closing issue, worktree and CI, merges only with separate authority, verifies post-merge CI (and Pages when triggered), and records durable finish authorization. The child then submits `merged` with the same binding/payload and runs `/amux finish` only when explicitly directed; worktree/Git safety comes first and `amux teardown` is last. Group/report history survives finish.

All lifecycle mutations share one lock. Exit `2` contention writes nothing and requires waiting for the current operation before retrying the identical operation/report ID. Stale/recycled/missing callback leases fail closed and are repaired only by explicit registration; callback failure leaves the durable report pending and the worker alive. Never retry notification into a suspected busy composer: recover from durable pending/history state and acknowledge it directly. Duplicate/reordered wake-ups and coordinator restarts are likewise recovered from durable state, never inferred tmux delivery. Do not force-delete branches, auto-release, infer finish from a late callback, or repeatedly read unrelated Amp threads.

Coordinator soft budgets to `ready` are Small 30m, Medium 1h (default), Large 2h; XL must be split. Stale is 15m, one review warns after 10m, demonstrated external CI waits alert after 20m, and authorized finish alerts after 10m. Only demonstrated external service waits pause active time. One coordinator-approved extension may add at most half the original budget under a new generation. Expiry is diagnostic and non-destructive; use one nearest-deadline queue, not one timer process per child. This is coordinator policy: the current CLI has no deadline mutation command, so agents must not edit `reports.json` to implement it.

## Side effects

| Operation | Worker config / shelf | Runner config | Local clients | Remote worker thread |
| --- | --- | --- | --- | --- |
| `list`, `workspace list` | inspect | inspect | none | none |
| `doctor` | inspect | inspect | inspect | inspect only |
| `launch` | read; skip shelved | read | create/verify | none |
| `pin` / `unpin` | pin worker; unpin worker and shelf intent | mutate runner registry | none | none |
| `park` / `restart` | preserve | preserve | stop/restart verified | none |
| `remove` | remove worker/shelf | remove runner | stop verified | none |
| `shelve` / `unshelve` | preserve worker; mutate intent | none | shelve parks only | archive/unarchive |
| `spawn` | add worker after verification | none | create worker | create/rename |
| `teardown` | remove worker/shelf | none | stop verified worker | archive |
| `reconcile` | synchronize drift | repair stale ownership | verified repairs only | worker sync only |
| `callback register` / `clear` | none; mutate machine runtime lease only | none | inspect exact pane/process | none |
| `report submit` | persist report, then best-effort verified wake-up | none | optionally send short token | none |
| `group list` / `group show` | inspect durable group intent | none | none | none |
| `group declare` / `add` / `coordinator` / `reconcile` | persist/inspect durable group intent | none | none | add-only label command |
| `group remove` | remove durable group intent | none | none | unsupported; label may remain |
| `report pending` / `history` | inspect durable reports | none | none | none |
| `report submit` / `acknowledge` / `authorize-finish` | mutate durable report state | none | none | none |

Runner lifecycle never creates, continues, archives, or manages remote agent threads.

## JSON v1, dry-run, exits, and locking

`--json` (`-j`) emits exactly one versioned document. Schema v1 contains:

```json
{
  "schema_version": 1,
  "command": "park",
  "dry_run": true,
  "planned": [],
  "successful": [],
  "skipped": [],
  "failed": []
}
```

Workers are identified by `{ "kind": "worker", "thread": "T-..." }`; runners by `{ "kind": "runner", "workdir": "/canonical/path" }`; group memberships by `{ "kind": "group_membership", "group": "issue-131", "thread": "T-..." }`; reports by `{ "kind": "report", "path": "stable-report-id", "group": "issue-133", "thread": "T-..." }`. Agents must ignore unknown optional fields within schema v1.

- `--dry-run` (`-n`) validates and plans without mutation. Prospective changes appear under `planned`, never `successful`.
- Exit `0`: no failures. Exit `1`: runtime failure; some independent actions may have completed. Exit `2`: request/preflight rejection before mutation.
- Bulk operations preflight the complete plan, then continue independent actions after runtime failures. Runner restart is the containment exception: after one replacement fails, later runner restarts are skipped so a shared launch defect cannot stop the remaining healthy fleet; independent worker actions may still continue.
- Lifecycle commands are idempotent desired-state operations. Known no-ops are `skipped`, not errors.
- Mutations and scheduled maintenance share one bounded machine-level lock. Contention fails with structured owner metadata and no mutation.

## Configuration migration

Current config is directory-based:

```text
~/.config/amux/workers.tsv
~/.config/amux/runners.tsv
~/.config/amux/shelves.tsv
~/.config/amux/groups.tsv
~/.config/amux/reports.json
```

Ephemeral callback leases are stored separately under `$XDG_RUNTIME_DIR/amux/callback-leases.json` (or the user cache directory fallback) and are intentionally not portable configuration.

Ordinary commands never migrate legacy config implicitly. They reject with guidance. Preview and run explicit migration:

```sh
amux --dry-run migrate-config
amux migrate-config
```

Legacy files remain available for rollback.

## Runner maintenance

Runner maintenance is a short-lived machine-level systemd user timer on Linux or LaunchAgent on macOS—not a resident supervisor. It checks every six hours with bounded jitter, updates Amp once according to declared ownership, and restarts verified running runners only when the Amp executable changed.

```sh
amux --dry-run runner maintenance install --update-owner self
amux runner maintenance install --update-owner external
amux runner maintenance run
amux runner maintenance remove
```

Use `self` when Amp's updater owns updates and `external` when a package manager does. Installation is explicit and dry-runnable. Maintenance uses the same operation lock and records diagnostics consumed by `amux install doctor`.

## Shell completions

```sh
amux completion bash > ~/.local/share/bash-completion/completions/amux
amux completion zsh > ~/.zfunc/_amux
amux completion fish > ~/.config/fish/completions/amux.fish
```

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md). The standard checks are:

```sh
go test ./...
go vet ./...
make build
gofmt -l .
git diff --check
```

## License

[MIT](LICENSE)
