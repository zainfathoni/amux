# amux

`amux` is the local tmux lifecycle layer for [Amp](https://ampcode.com/). It manages interactive **workers**, non-interactive **runners**, and named **workspaces** with explicit, agent-safe side effects.

> **Retirement direction:** new Amp-to-Amp work moves to native Amp creation, exact runner/Orb placement, messaging, replies, waiting, and archive lifecycle. Amux will close new resource admission one operation family at a time, retain drain-only writes for state that predates each cutover, then export, freeze, remove writers, provide a bounded read-only compatibility window, and archive the lifecycle product. The current Tycho bridge remains until a direct bounded structured-return route is field-validated; Git/worktree removal safety remains independent transition safety. Documentation does not itself change current command behavior. See [ADR 0007](docs/adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) and the [active disposition ledger](docs/staged-drain-disposition.md).

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

The installer detects arm64/amd64, downloads the matching GitHub release archive and published checksum, verifies SHA-256, and atomically replaces the canonical binary. Any failure before that replacement leaves the existing binary untouched. It reports any PATH setup or shadowing and prints the exact `amux install doctor` command to run next.

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

Optional experimental skills (explicit owner request only):

```sh
npx skills add zainfathoni/amux --skill amux-claude --global
npx skills add zainfathoni/amux --skill amux-pi --global
npx skills add zainfathoni/amux --skill amux-tycho --global
```

For a machine that maintains a clean local `main` checkout, the shell installer can instead link all four bundled skills directly to that checkout. Update the checkout explicitly first; the installer never pulls or changes it:

```sh
AMUX_REPO="$HOME/Code/GitHub/zainfathoni/amux"
git -C "$AMUX_REPO" pull --ff-only origin main
curl -fsSL https://amux.zainf.dev/install.sh | AMUX_SKILLS_SOURCE="$AMUX_REPO" sh
```

This opt-in mode creates absolute links under `~/.agents/skills`. Existing real installations and same-name legacy duplicates are preserved as timestamped sibling backups rather than deleted. The skill migration is sequential: a later filesystem failure exits nonzero without rolling back already reported links or backups. Future skill updates need only another fast-forward pull followed by an Amp reload or a new thread; the `amux` binary still updates separately.

`/amux` teaches canonical selectors, side-effect boundaries, skill-only health/sprawl/finish workflows, and compatibility-only progressive disclosure. `reference/contract-v1.md` is read once only by an already-bound pre-cutover Amux worker whose persisted spawn/adoption/group provenance proves it is drain-eligible; `reference/deadline-v1.md` applies only to an already-bound pre-cutover deadline. Native-created work receives neither file nor any Amux lifecycle instructions. See the [dedicated skill guide](https://amux.zainf.dev/skill/).

`/amux-tycho` is the separate unstable external-executor bridge for explicitly owner-selected Tycho agent/project/harness/model routes. A route may already exist or be one explicitly owner-authorized dormant route prepared without provider execution and bound to the receipt before its first run. Tycho may route Claude or Pi, but it is a report-only producer; the current real Amp thread retains coordination, consumption, and acknowledgement authority. PR review may bind either same-head local attachments or an immutable remote artifact by repository, PR, full head SHA, and full tree SHA, with no fallback after binding. `/amux-claude` and `/amux-pi` remain separate provider-specific fallback/reference skills. None installs with core `/amux` unless requested through skills.sh; the opt-in local-checkout installer links all bundled skills. Consult the [provider executor readiness matrix](docs/provider-executor-readiness.md) before selecting a route; helper or CLI support alone does not prove model, runtime, or mutation readiness.

To resume a Tycho receipt created before this skill split, install `/amux-tycho` explicitly and use its new helper path while preserving the original state, custody, and abandonment directories byte-for-byte at their original canonical paths. Preserve all receipt IDs, immutable bindings, event IDs, and capabilities; do not recreate, copy, move, rebind, or upgrade them. Cleanup `pending` must be replayed as the identical terminal event against the same original capability directory.

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
| `--workdir`, `-d` | canonical runner identity; explicit worker-reconcile drift scope |
| `--workspace`, `-w` | worker/runner lifecycle group and same-named tmux session |
| `--window`, `-W` | worker pin/adoption placement metadata |
| `--mode`, `-m` | workspace-list filter |
| `--current` | resource owning the invoking pane/workdir |
| `--all` | explicit machine-wide scope |

Worker and runner pin/unpin require a namespace because their identities differ:

```sh
amux worker pin --workspace amux --window docs --workdir ~/Code/amux --thread T-example
amux worker unpin --thread T-example
amux runner pin --workspace amux --workdir ~/Code/amux-runner
```

Runner row deletion through `remove`, `unpin`, or missing-workdir `reconcile` currently fails closed pending authoritative process/catalog absence evidence. Use `runner park` to stop an exact owned process while retaining its row.

`amux workspace list` and its exact `amux workspaces` alias list the worker/runner workspace union. Add `--mode worker` or `--mode runner` to filter.

Removed aliases and positional forms fail with remediation. In particular, do not use `store`, top-level `pin`, `pin-current`, `unpin-current`, `park-current`, `shelve-current`, `shelved`, `prune-archived`, `self-update`, positional session selectors, `--config`, or legacy config environment variables.

## Worker-only lifecycle

`shelve`, `unshelve`, and `teardown` remain worker-only concise routes. At cutover generation `spawn-native-cutover-v1`, `amux spawn` and its exact `amux worker spawn` alias stopped admitting generalized new work.

For ordinary new work, use Amp's authenticated native `create_thread` directly. Select the exact intended Workspace Project and Orb, or select one exact live runner whose working directory is the intended canonical workdir; do not fall back between executors or create elsewhere and try to re-home the thread later. Deliver one lean prompt containing the task, acceptance criteria, relevant constraints, validation, and expected reply. Keep only the returned native parent/child and reply route. Do not inject `contract-v1.md` or require an Amux receipt, report, callback, adoption, group, deadline, finish authorization, or any other lifecycle state. If the native result is indeterminate, stop without retrying or searching for a substitute.

The sole new-work exception is an exact owner-authorized projectless physical-host placement that native project-backed Orb/runner creation cannot express. Run it on that physical host, pass the byte-exact local hostname and canonical workdir, and create no group:

```sh
amux spawn --mode medium --assignment-phase prepare \
  --owner-authorized-projectless-physical-host --physical-host <exact-local-hostname> \
  --native-capability existing-thread-message-v1 \
  --workdir <canonical-path> --workspace <workspace> \
  --window <assignment-key> --prompt-file <path|->
```

The coordinator first capability-checks its authenticated native existing-thread message action. `prepare` writes one schema-2 assignment record containing admission `spawn-native-cutover-v1/projectless-physical-host-exception`, exact host, canonical workdir, mode, key, and prompt digest before it runs `amp threads new` once in that cwd. It writes no worker, group, operation, shelf, or tmux state. Repeat the owner-authorization and exact-host flags on `arm` and `finalize`; `arm` makes the one message attempt indeterminate before the coordinator sends exactly one unchanged native message, and `finalize` records its result without opening a pane or dual-writing another Amux store. Any host, identity, placement, ownership, launch, or delivery ambiguity remains indeterminate with no retry, fallback, reroute, rebind, adoption, search, or cleanup.

Pre-cutover schema-1 assignment records remain drain-writable only through their exact existing boundary: a prepared record may arm once and an armed record may finalize without resend. Those transitions update only `spawn-assignments.json`; they do not update `workers.tsv`, create a pane, or write native replacement state. Records whose schema cannot prove pre-cutover admission or the exact bounded exception fail closed. Contract and lifecycle instructions are available only to exact pre-cutover Amux-managed spawn, adoption, or group records that prove their next transition is drain-eligible. Existing legacy worker rows and operation records remain unchanged.

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
- Worker remove stops the worker and deletes local configuration without archiving; worker unpin only deletes configuration. Configured runner remove and unpin fail closed and retain the row.
- Reconcile explicitly synchronizes worker shelf/remote drift. Runner reconcile is a present-workdir no-op and retains a missing-workdir row pending authoritative process/catalog absence evidence. Launch never performs hidden reconciliation.

## Pre-cutover durable work-group compatibility

Existing work groups are explicit, durable many-to-many associations between Amp thread IDs and byte-preserving group IDs. The commands below are compatibility/drain surfaces for exact persisted pre-cutover identity, not a route to declare a new group, coordinator, or member:

This section documents existing identity and recovery contracts during the staged drain, not a recommendation to create new coordinators or groups. Use these surfaces only to finish state that predates the applicable cutover generation. See [ADR 0007](docs/adr/0007-retire-amux-through-native-cutover-and-staged-drain.md).

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

Group IDs map byte-for-byte to Amp labels and must match `^[a-z0-9]+(?:-[a-z0-9]+)*$`. Generic `amux group` commands never normalize or infer them from titles, branches, issue numbers, or existing labels. Local `groups.tsv` intent is authoritative and survives worker/tmux/worktree lifecycle changes. `group list` and `group show` are deterministic local-only reads.

The pre-cutover compatibility workflow uses explicit durable identity. For this repository, issue `#131` uses group/Amp label `amux-131`, and its first worker uses report ID `amux-131-worker-1`; another repository may use an explicit equivalent lowercase, group-safe repository slug. This workflow convention does not narrow the generic group-ID contract. Existing `amux-*`, repository-slug, `issue-*`, purpose-specific groups such as `pr-181-review`, and explicit groups remain valid and are never migrated, renamed, removed externally, or rewritten.

Worker adoption accepts one exact `--group <id>` for compatibility with existing explicit adoption operations. Active guidance uses it only to continue an already-persisted pre-cutover operation whose exact state proves the next transition is drain-eligible; it is not native-thread onboarding. It persists local member intent before add-only label synchronization.

Ordinary native-created threads are no longer adopted into Amux at `spawn-native-cutover-v1`. `amux worker adopt` is a compatibility/drain surface only for an exact persisted pre-cutover adoption operation whose next transition is proven drain-eligible; do not call it automatically or use it in new-work workflows. See [ADR 0007](docs/adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) and the [spawn cutover inventory](docs/spawn-cutover-inventory.md).

External synchronization is deliberately add-only and member-only. Coordinator identity remains authoritative local metadata and is not projected to an Amp label, so a long-lived coordinator does not accumulate labels for every group it supervises. Member add, worker adoption, and reconcile use Amp's additive label command only after a version and exact semantic-help capability check; reconcile reports coordinator memberships as skipped. Additive failures retain local intent as visible drift. Local removal cannot remove an existing Amp label, succeeds with a warning that the external label may remain indefinitely, and never claims exact synchronization. Promoting an already-labelled member to coordinator cannot remove its prior label. Use `--dry-run` to preflight and inspect any group mutation.

### Pre-cutover reports, callback leases, and finish authorization

For an exact persisted pre-cutover drain, reports are persisted locally before callback notification. A stable report ID can progress between `ready` and `blocked`; `merged` is terminal and is accepted only after the group coordinator records a separate durable finish authorization. Acknowledgement never implies authorization, and neither `ready`, `blocked`, callback success, nor deadline expiry authorizes cleanup. Native-created threads receive no report, callback lease, deadline, or finish authorization.

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

### Task-scoped coordinator workflow

The bundled `/amux` skill directs ordinary new coordination to authenticated native `create_thread`, messaging, reply routing, and waiting on the exact executor/workdir. Each child receives only a lean task prompt, and the parent retains only native identity and reply routing. It creates no Amux contract or lifecycle representation for those threads. The legacy durable group/report procedure remains documented only to drain exact members and reports whose persisted pre-cutover provenance proves them drain-eligible; it is not a new-thread onboarding route.

For a proven pre-cutover group/report drain, the already-bound worker uses one stable report ID for `blocked`, `ready`, and terminal `merged`. `ready` means implementation, tests, one review, PR, and normal CI are complete. A callback token only wakes the coordinator. The coordinator acknowledges receipt separately, independently verifies PR URL/head/scope/mergeability/closing issue, worktree and CI, merges only with separate authority, verifies post-merge CI (and Pages when triggered), and records durable finish authorization. The child then submits `merged` with the same binding/payload and runs `/amux finish` only when explicitly directed; worktree/Git safety comes first and `amux teardown` is last. Group/report history survives finish.

All lifecycle mutations share one lock. Exit `2` contention writes nothing and requires waiting for the current operation before retrying the identical operation/report ID. Stale/recycled/missing callback leases fail closed and are repaired only by explicit registration; callback failure leaves the durable report pending and the worker alive. Never retry notification into a suspected busy composer: recover from durable pending/history state and acknowledge it directly. Duplicate/reordered wake-ups and coordinator restarts are likewise recovered from durable state, never inferred tmux delivery. Do not force-delete branches, auto-release, infer finish from a late callback, or repeatedly read unrelated Amp threads.

Coordinator soft budgets to `ready` are Small 30m, Medium 1h (default), Large 2h; XL must be split. Stale is 15m, one review warns after 10m, demonstrated external CI waits alert after 20m, and authorized finish alerts after 10m. Only demonstrated external service waits pause active time. One coordinator-approved extension may add at most half the original budget under a new generation. Expiry is diagnostic and non-destructive; use one nearest-deadline queue, not one timer process per child. Full deadline procedure lives in the skill's `reference/deadline-v1.md` and must not reload the entire `/amux` skill on schedule fire. This is coordinator policy: the current CLI has no deadline mutation command, so agents must not edit `reports.json` to implement it.

## Side effects

| Operation | Worker config / shelf | Runner config | Local clients | Remote worker thread |
| --- | --- | --- | --- | --- |
| `list`, `workspace list` | inspect | inspect | none | none |
| `doctor` | inspect | inspect | inspect | inspect only |
| `launch` | read; skip shelved | read | create/verify | none |
| `pin` / `unpin` | pin worker; unpin worker and shelf intent | pin mutates; runner unpin currently retains and rejects | none | none |
| `park` / `restart` | preserve | preserve | stop/restart verified | none |
| `remove` | remove worker/shelf | runner removal currently retains and fails closed until bound process/catalog absence is provable | stop verified workers only | none |
| `shelve` / `unshelve` | preserve worker; mutate intent | none | shelve parks only | archive/unarchive |
| `spawn` | generalized admission rejected; exact assignment-store drain or owner-authorized schema-2 host/workdir exception only | no worker/group/operation/shelf writes | none | exception only: one empty exact-host local create plus one coordinator-native exact-thread message; execution unproven |
| `teardown` | remove worker/shelf | none | stop verified worker | archive |
| `reconcile` | synchronize shelf/archive drift; remove only proven-missing safe worker bindings | present-directory no-op; retain missing rows pending positive process/catalog absence | verified worker repairs only | worker sync only for present bindings |
| `callback register` / `clear` | none; mutate machine runtime lease only | none | inspect exact pane/process | none |
| `report submit` | persist report, then best-effort verified wake-up | none | optionally send short token | none |
| `group list` / `group show` | inspect durable group intent | none | none | none |
| `group declare` / `coordinator` | persist durable coordinator intent | none | none | none; coordinators are not projected |
| `group add` / `reconcile` | persist/inspect durable member intent | none | none | add-only member label command |
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
~/.config/amux/group-naming.json
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
