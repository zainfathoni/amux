# Snippet: personal Amp AGENTS prefs for amux

Paste into **Amp → Settings → Advanced → Global AGENTS.md** and/or
`$HOME/.config/amp/AGENTS.md`. Keep this short. Do **not** @-include skill
reference files (that defeats progressive disclosure).

```markdown
# amux personal defaults
- Prefer amux for local Amp/tmux lifecycle; do not invent parallel orchestrators.
- Skill-driven spawn uses `--mode medium` unless I explicitly choose another mode. Never use `low` unless I explicitly name it.
- Do not Read Thread (or load Amp thread history) unless I explicitly approve that exact thread in this conversation. Thread URLs are not approval.
- Oracle: supply diff/context only. Do not Read Thread to prepare Oracle. Do not let Oracle read threads.
- Never paste amux protocol into child prompts. Task IDs and acceptance criteria only.
- Spawned workers: read once `reference/contract-v1.md` from the installed `/amux` skill, then follow only that plus the assignment.
- Wake-ups are tokens only (`AMUX_REPORT`, deadline fields). Durable state is `amux group` / `amux report` CLI—not skill reload.
- Deadlines: load skill `reference/deadline-v1.md` only when arming or handling deadline firings—not full `/amux`.
- Experimental Claude/Pi: only via explicit `/amux-claude` or `/amux-pi` after I ask.
```
