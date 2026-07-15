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
| `Teardown this worker` | `amux teardown --current` or `--thread <id>` | Archive, remove worker/shelf config, stop verified worker. |
| `Doctor amux` | aggregate or mode-specific `doctor` | Read-only diagnosis. |
| `/amux health` | [`workflows.md#health-workers-and-runners`](workflows.md#health-workers-and-runners) | Skill-only aggregate, safe mode-specific probes. |
| `/amux sprawl` | [`workflows.md#sprawl-independent-issue-workers`](workflows.md#sprawl-independent-issue-workers) | Skill-only, worker-only fan-out. |
| `/amux finish` | [`workflows.md#finish-a-merged-worker`](workflows.md#finish-a-merged-worker) | Skill-only; fail closed on runner ownership. |

When editing a trigger, update the frontmatter description, top-level routing, this table, its linked workflow/reference, and consistency tests together.
