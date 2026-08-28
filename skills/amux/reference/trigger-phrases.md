# amux skill trigger phrase checklist

This table is the complete activation and routing contract for [`../SKILL.md`](../SKILL.md). Skill-only rows must never be represented as CLI commands. Experimental Tycho/Claude/Pi triggers live in `/amux-tycho`, `/amux-claude`, and `/amux-pi`, not here.

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
| `Spawn a worker for` | interpret as authenticated native `create_thread` on the exact Orb or live runner/workdir; projectless host exception only with exact owner authorization | Native child thread, not Amux spawn/worker; lean task prompt and native parent/reply routing only; no Amux contract or lifecycle state. |
| `Coordinate child threads` | native parent/child creation, reply routing, messaging, and waiting; existing durable groups are proven drain-only | Lean task prompts and native routing only; contract/lifecycle instructions stay inside proven pre-cutover drain flows. |
| `Teardown this worker` | [`workflows.md#teardown-a-worker`](workflows.md#teardown-a-worker), then `amux teardown` | Paired Claude preflight only if `/amux-claude` may apply; Amp teardown last. |
| `Doctor amux` | aggregate or mode-specific `doctor` | Read-only diagnosis. |
| `/amux health` | [`workflows.md#health-workers-and-runners`](workflows.md#health-workers-and-runners) | Skill-only aggregate, safe mode-specific probes. |
| `/amux sprawl` | [`workflows.md#sprawl-independent-issue-threads`](workflows.md#sprawl-independent-issue-threads) | Skill-only native fan-out; no Amux adoption/lifecycle state. |
| `/amux finish` | [`workflows.md#finish-a-completed-worker`](workflows.md#finish-a-completed-worker) | Skill-only pre-cutover completed-worker drain; merged or review-only completion, one preflight/approval, retained branch, non-force removal, exact-thread archive. |

When editing a trigger, update the frontmatter description, top-level routing, this table, its linked workflow/reference, and consistency tests together.
