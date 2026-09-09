# amux skill trigger phrase checklist

| Trigger phrase | Route | Contract |
| --- | --- | --- |
| `Pin it` | `amux runner pin --workspace <name> --workdir <existing-directory>` or `--current` | Unqualified pin requests select the retained runner route. |
| `Pin this runner` | `amux runner pin --workspace <name> --workdir <existing-directory>` or `--current` | Retained runner admission. |
| `List runners` | `amux runner list --all` or scoped selector | Read-only runner registry/runtime view. |
| `Restore my workspace` | `amux launch --workspace <name>` | Launch configured runners only. |
| `Park it` | `amux runner park --current` or exact workdir | Preserve runner row. |
| `Restart unresponsive runners` | `amux runner restart` with exact scope | Preserve runner row; fail closed on ambiguity. |
| `Teardown this runner/worktree` | [`workflows.md#teardown-completed-native-thread-worktrees`](workflows.md#teardown-completed-native-thread-worktrees) | Exact local plan/apply; branch preserved; native thread archive remains separate. |
| `Doctor amux` | `amux doctor --all` or scoped runner doctor | Read-only diagnosis. |
| `Spawn a worker for` | native Amp `create_thread` on exact Orb or live runner/workdir | Native child, never Amux worker/spawn/adoption. |
| `Coordinate child threads` | native parent/reply routing, messaging, and waiting | No Amux coordination stores. |
| `/amux health` | [`workflows.md#health-runners`](workflows.md#health-runners) | Skill-only runner probes. |
| `/amux sprawl` | [`workflows.md#sprawl-independent-issue-threads`](workflows.md#sprawl-independent-issue-threads) | Native child fan-out only. |
| `/amux sweep` | [`workflows.md#sweep-worktree-inventory`](workflows.md#sweep-worktree-inventory) | Protected #360 read-only route; separate owner authorization required. |

Former worker, shelf, worker-teardown, group, report, callback, deadline, and finish trigger phrases have no Amux route. Unqualified teardown must resolve to the runner/worktree meaning before using the new runner-scoped route; top-level `amux teardown` remains a tombstone.
