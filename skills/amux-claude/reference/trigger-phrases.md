# amux-claude trigger checklist

| Trigger phrase | Route | Contract |
| --- | --- | --- |
| `Delegate read-only analysis to Claude` | [`claude-read-only-delegation.md`](claude-read-only-delegation.md) | Explicit-only local experiment; never an Amp worker. |
| `Delegate isolated mutating work to Claude` | [`claude-mutating-delegation.md`](claude-mutating-delegation.md) | After Pilot 1 pass; dedicated worktree; clean commit or zero-commit blocked. |
| `Delegate bounded work to Claude Opus in a fresh Amp Orb` | [`claude-opus-orb-executor.md`](claude-opus-orb-executor.md) | Fresh-Orb OAuth; exact `claude-opus-4-8`. |
| `Recover indeterminate Claude worker evidence` | [`claude-delegation-recovery.md`](claude-delegation-recovery.md) | Owner-authorized recovery only; preserve unresolved evidence. |

When editing a trigger, update [`../SKILL.md`](../SKILL.md), this table, and consistency tests together.
