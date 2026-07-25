---
status: accepted-for-implementation
slice: S1-landed-in-tree
remaining: S2-home-AGENTS-wiring-optional
---

# Skill token efficiency design grill

This proposal records the reviewed design for slimming `/amux` progressive disclosure, cutting inter-thread protocol paste, and separating experimental provider skills. It is the authority for the S1 skill-tree change landed alongside it.

## Problem

The bundled `/amux` skill was under-progressive:

- `SKILL.md` carried contracts, experimental routes, and a full deadline state machine on every skill load.
- Sprawl/coordinator spawn prompts re-shipped Dos/Don’ts/How-To into every worker thread.
- Deadline schedule wake-ups said “Load `/amux` first,” re-injecting the fat skill into long-lived coordinators.
- Experimental Claude/Pi surfaces (including multi-thousand-line Python helpers) sat in the default skill package and description surface.

## Scope

**In scope:** interactive top-level coordinator threads, sprawled workers, local machines (macOS/Linux), other repos without committing amux policy (e.g. workplace repos), personal orbs when skills are installed.

**Out of scope:** Amp subagents, Puck, raw custom agents, bookthatapp Orbs (deferred), fleet as an installer.

## Locked decisions

| ID | Decision |
| --- | --- |
| Coverage | Coordinators + workers + local machines + other repos via user-wide skill; not subagents/Puck |
| Architecture | Five buckets: personal AGENTS prefs, on-demand skill, repo deltas, task-only spawn, token wake-ups |
| Personal always-on | Web Global AGENTS **and** `$HOME/.config/amp/AGENTS.md`, ~10 lines prefs |
| Worker protocol (Q2c′) | **(1c)** public `reference/contract-v1.md`; spawn mandates one read; no @-include of fat files into AGENTS |
| Wake-ups | Must **not** require loading full `/amux` |
| Deadlines | Separate `reference/deadline-v1.md`, load only when arming/firing |
| Canonical owner | Public amux skill references |
| Fleet | Knowledge base only; **not** distribution |
| Workplace repos | No amux workflow commits; rely on global skill + personal AGENTS |
| Skill change | Breaking slim-down upstream ASAP |
| Experimental | Separate skills `/amux-claude` and `/amux-pi` |
| First slice | **S1** in amux repo; remaining plan documented |

## Why not put Claude in the CLI core?

ADR 0001 keeps the lifecycle CLI Amp-worker/runner focused. ADR 0002 and the Claude pilot deliberately kept Claude as a **skill-owned experiment** with unstable receipts, no compatibility guarantee, and no first-class lifecycle resource until promotion evidence exists. The large Python helper encodes fail-closed process/tmux/receipt identity that was easier to iterate outside the Go CLI stability boundary. Promotion to `amux` CLI subcommands remains a **future ADR** if and only if repeated real use justifies a stable API—not a default for token efficiency.

Separating `/amux-claude` and `/amux-pi` was previously deferred to keep one package; token cost and description bloat now outweigh that packaging preference for the default skill.

## Target layout

```text
skills/amux/                    # core lifecycle (default install)
  SKILL.md                      # thin router
  reference/contract-v1.md      # workers read once
  reference/deadline-v1.md      # deadlines only
  reference/workflows.md
  reference/commands.md
  ...

skills/amux-claude/             # optional explicit install
  SKILL.md
  reference/*
  experimental/claude-delegation/

skills/amux-pi/                 # optional explicit install
  SKILL.md
  reference/pi-spark-orb-executor.md
```

## Provisioning (no fleet)

| Artifact | How |
| --- | --- |
| Core skill | `npx skills add zainfathoni/amux --skill amux --global` |
| Experimental | same with `--skill amux-claude` / `amux-pi` |
| Web Global AGENTS | Amp Settings; paste from `docs/snippets/global-agents-amux-prefs.md` |
| Home AGENTS | Optional; same prefs; nix-home/omarchy-home may template **prefs only** later (S2) |
| Workplace git trees | unchanged |

## Success metrics

1. Core `SKILL.md` body thin; description short (no experimental recovery encyclopedias).
2. Spawn message template task-only + mandatory contract-v1 read-once line.
3. Deadline/report wake-ups do not say “Load `/amux` first.”
4. bookthatapp (and peers) need no amux files in-repo.
5. Consistency tests cover thin router, contract-v1, deadline-v1, and experimental skill separation.

## Remaining plan (post-S1)

1. **S2 (optional):** nix-home / omarchy-home apply `docs/snippets/global-agents-amux-prefs.md` into home AGENTS on enrolled machines (prefs only, not contract body).
2. **Operator:** paste Web Global AGENTS prefs; reinstall skills (`amux`, optionally `amux-claude` / `amux-pi`); update coordinator deadline schedules to the new synthetic prompt in `deadline-v1.md`.
3. **docs/skill HTML:** refresh public skill page for multi-skill install and progressive disclosure (can ship with next Pages publish).
4. **Future ADR (only with evidence):** whether any Claude receipt seams graduate into Go CLI; until then helpers stay experimental and skill-owned.
5. **Do not** put policy text in fleet; fleet may only note that enrolled machines should have the global skill installed.

## S1 landed checklist

- [x] Thin `/amux` SKILL.md + trigger checklist without experimental routes
- [x] `contract-v1.md` and `deadline-v1.md`
- [x] Workflows: task-only spawn; deadline pointer; Claude teardown gated on `/amux-claude`
- [x] `skills/amux-claude` and `skills/amux-pi` packages
- [x] README / CONTRIBUTING
- [x] Consistency tests updated

## Operator reinstall (how-to)

On each Amp machine (macOS/Linux laptop, Amp host):

```sh
# Core skill (required for slim progressive disclosure)
npx skills add zainfathoni/amux --skill amux --global

# Optional experiments only when you use them
npx skills add zainfathoni/amux --skill amux-claude --global
npx skills add zainfathoni/amux --skill amux-pi --global
```

Then reload Amp (quit/reopen TUI or restart the client) so the skill index refreshes.

**Global AGENTS (account-wide top-level agents):** Amp → Settings → Advanced → Global AGENTS.md — paste `docs/snippets/global-agents-amux-prefs.md`.

**Home AGENTS (per machine):** same prefs block into `~/.config/amp/AGENTS.md` (optional S2 via nix-home/omarchy-home).

**Deadline schedules:** any existing schedule still saying "Load `/amux` first" must be updated to the synthetic prompt in `reference/deadline-v1.md`.

**Do not** `@`-include `contract-v1.md` from AGENTS.md (that makes the contract always-on and defeats progressive disclosure). Workers get "read contract once" from the spawn message.

## Tracking issues

- amux: land slim-down
- nix-home / omarchy-home: optional home AGENTS prefs
- fleet: documentation-only enrolled-machine expectation
