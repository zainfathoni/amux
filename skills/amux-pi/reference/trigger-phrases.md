# amux-pi trigger checklist

| Trigger phrase | Route | Contract |
| --- | --- | --- |
| `Run Pi on Spark in an Amp Orb` | [`pi-spark-orb-executor.md`](pi-spark-orb-executor.md) | Explicit-only; exact Spark model; owner-operated Codex OAuth; API keys blocked. |
| `Run Pi on Spark on a physical runner in a dedicated local worktree/tmux with no Orb` | [`../experimental/pi-spark-local`](../experimental/pi-spark-local) | Explicit owner authorization only; selects the existing physical-host helper, never the Orb recipe; exact Pi 0.80.10 and Spark model; one attempt with no fallback; one tracked-file replacement in a clean dedicated worktree, not arbitrary local work. |
| `Spike one bounded local file replacement` | [`../experimental/pi-spark-local`](../experimental/pi-spark-local) | Explicit owner authorization only; exact Pi 0.80.10 and Spark model; one tracked file; clean worktree; open-ended results untrusted. |

An explicit owner request that names a physical runner, dedicated local tmux/worktree, or no Orb selects the bounded local route above. Those phrases describe executor placement; they do not broaden its one-tracked-file replacement authority or waive its OAuth privacy, no-recursive-delegation, no-publication, independent-review, cleanup, or rollback safeguards.

When editing a trigger, update [`../SKILL.md`](../SKILL.md), this table, and consistency tests together.
