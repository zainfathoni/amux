---
name: amux-pi
description: "Experimental Pi on Spark in an Amp Orb for amux. Use only after an explicit owner request to run Pi with openai-codex/gpt-5.3-codex-spark via owner-operated ChatGPT Codex OAuth. Not an amux lifecycle resource."
---

# amux-pi (experimental)

Disposable provider-specific experiment. **Not** an amux CLI resource, worker, runner, or provider-neutral orchestration state.

## Route

- **Run Pi on Spark in an Amp Orb**: only after an explicit owner request, load [`reference/pi-spark-orb-executor.md`](reference/pi-spark-orb-executor.md).

Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md).

## Safety

- Exact model `openai-codex/gpt-5.3-codex-spark` through owner-operated ChatGPT Codex OAuth.
- API keys, ambiguous billing, missing trusted quota evidence, automatic retry/fallback, repository authority, and credential transfer fail closed.
- Do not activate from incidental Pi/Spark mentions.
