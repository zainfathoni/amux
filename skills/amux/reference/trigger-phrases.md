# amux skill trigger phrase checklist

This table is the complete activation and routing contract for [`../SKILL.md`](../SKILL.md). Skill-only rows must never be represented as CLI commands.

| Trigger phrase | Route | Contract |
| --- | --- | --- |
| `Pin it` | `amux worker pin --current` with complete `AMUX_*` identity; otherwise full explicit selectors | Worker config only; never combine current with another selector. |
| `Unpin it` | `amux worker unpin --current` | Remove worker row and shelf intent; no stop/archive. |
| `forget this on restore` | `amux worker unpin --current` | Same as unpin. |
| `Park it` | `amux worker park --current` | Stop verified local worker; preserve config/thread. |
| `Restart unresponsive clients` | aggregate `amux restart --all` or mode-specific restart | Preserve config and remote state. |
| `Shelve this` | `amux shelve --current` or `--thread <id>` | Record intent, archive, park; preserve worker config. |
| `defer this workspace` | `amux shelve --workspace <name>` | Worker-only workspace deferral. |
| `hide it for now` | worker shelve route | Do not substitute park. |
| `Show shelved work` | `amux worker list --shelf shelved` | Local shelf intent only. |
| `Unshelve this` | `amux unshelve --current` or `--thread <id>` | Unarchive, then remove intent; do not launch. |
| `Restore my workspace` | `amux launch --workspace <name>` | Aggregate by default; worker route narrows. |
| `Spawn a worker for` | workflow, then `amux spawn --mode medium ...` | Explicit medium unless user chose another mode. |
| `Coordinate issue workers` | [`workflows.md#coordinate-a-durable-issue-work-group`](workflows.md#coordinate-a-durable-issue-work-group) | Durable group/report/auth workflow; callback is wake-up only. |
| `Delegate bounded work to Claude Opus in a fresh Amp Orb` | [`claude-opus-orb-executor.md`](claude-opus-orb-executor.md) | Explicit-only provider-specific experiment; fresh-Orb OAuth preflight, exact official `claude-opus-4-8`, bounded sanitized native Amp reporting, and no provider-neutral state. |
| `Run Pi on Spark in an Amp Orb` | [`pi-spark-orb-executor.md`](pi-spark-orb-executor.md) | Explicit-only disposable provider recipe; exact Spark model through owner-operated ChatGPT Codex OAuth, with API keys and ambiguous billing blocked. |
| `Delegate read-only analysis to Claude` | [`claude-read-only-delegation.md`](claude-read-only-delegation.md) | Explicit-only, skill-owned local experiment; never creates an Amp worker or runs autonomously. |
| `Delegate isolated mutating work to Claude` | [`claude-mutating-delegation.md`](claude-mutating-delegation.md) | Explicit-only separate writer experiment after Pilot 1 pass; dedicated worktree and clean commit handoff, never integration or cleanup authority. |
| `Recover indeterminate Claude worker evidence` | [`claude-delegation-recovery.md`](claude-delegation-recovery.md) | Explicit owner-authorized absence detach or exact-live validated-report retirement only; preserve unresolved evidence and fence, never rewrite, infer identity, retry launch, acquire, park, or clean. |
| `Teardown this worker` | paired lifecycle route in [`workflows.md`](workflows.md), then `amux teardown --current` or `--thread <id>` | Fail closed on every unsafe Claude pair; archive, remove worker/shelf config, and stop the verified worker last. |
| `Doctor amux` | aggregate or mode-specific `doctor` | Read-only diagnosis. |
| `/amux health` | [`workflows.md#health-workers-and-runners`](workflows.md#health-workers-and-runners) | Skill-only aggregate, safe mode-specific probes. |
| `/amux sprawl` | [`workflows.md#sprawl-independent-issue-workers`](workflows.md#sprawl-independent-issue-workers) | Skill-only, worker-only fan-out. |
| `/amux finish` | [`workflows.md#finish-a-merged-worker`](workflows.md#finish-a-merged-worker) | Skill-only; fail closed on runner ownership. |

When editing a trigger, update the frontmatter description, top-level routing, this table, its linked workflow/reference, and consistency tests together.
