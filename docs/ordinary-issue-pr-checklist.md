# Ordinary issue and PR-review checklist

Compose-only operating guidance for one coherent issue or PR review after [ADR 0007](adr/0007-retire-amux-through-native-cutover-and-staged-drain.md) native cutover. This is **not** an `/amux` skill route or activation surface. Native Amp owns ordinary execution; Amux remains drain-only for pre-cutover state. Prefer **direct coordinator** and **native child**—not permanent or portfolio Lead patterns.

## Checklist

1. **Default direct.** Run one coherent issue diagnosis/implementation or PR review on the **direct coordinator** with a **locked dedicated worktree** (exact-head for PR review; dedicated branch/worktree for issue work). Do not spawn a child for cleanup symmetry or thread aesthetics.
2. **Optional native child** only for concrete parallelism, context isolation, long-running implementation, or an explicit owner request. When a child is justified, use authenticated native `create_thread` with a lean task prompt and create no Amux lifecycle state. Child-creation mechanics: [`skills/amux/reference/workflows.md`](../skills/amux/reference/workflows.md#create-fresh-native-work) (load only when creating a child).
3. **Exclusive write ownership.** Exactly one writer owns that worktree—the coordinator **or** one child, never both. Do not edit a child-owned worktree from the coordinator.
4. **Three isolation layers.** Thread isolation ≠ Git worktree isolation ≠ runtime isolation. Commands may stay in the coordinator thread only if every code/test command targets the dedicated worktree. Never count verification from another worktree's Docker/Vite/runtime.
5. **Complete without ceremony.** Evidence-backed no-change, no-findings, and approval outcomes are valid completion without forcing a child. Task done ≠ worktree or provider teardown.

## Pointers only (do not restate)

| Concern | Owner |
| --- | --- |
| Review-only / no-PR finish | [#238](https://github.com/zainfathoni/amux/issues/238) |
| Amp `/team-review` + optional `/amux-tycho` second opinion | [#328](https://github.com/zainfathoni/amux/issues/328); load `/amux-tycho` when explicitly requested |
| Removal safety / retain-vs-remove disposition | [#344](https://github.com/zainfathoni/amux/issues/344), [#331](https://github.com/zainfathoni/amux/issues/331)–[#339](https://github.com/zainfathoni/amux/issues/339); [`skills/amux/reference/removal-safety.md`](../skills/amux/reference/removal-safety.md) |
| Lifecycle/orchestration surface inventory | [#313](https://github.com/zainfathoni/amux/issues/313) |

## Non-goals

- No new Amux lifecycle representation, scheduler, group/report/finish path, or generalized `amux spawn`/adoption for ordinary native children.
- No `/amux` activation or skill frontmatter claim over ordinary issue/PR work—this document is docs-only under ADR 0007.
- No permanent Lead pattern, provider-bridge protocol, retirement engine, or teardown procedure here—those stay with their owner issues above.
