---
name: amux-pi
description: "Experimental local and Amp Orb Pi executors for amux. Use only after an explicit owner request to run openai-codex/gpt-5.3-codex-spark through owner-operated ChatGPT Codex OAuth. Not an amux lifecycle resource."
---

# amux-pi (experimental)

Provider-specific experiments. **Not** amux CLI resources, workers, runners, or provider-neutral orchestration state.

## Route

- **Run Pi on Spark in an Amp Orb**: only after an explicit owner request, load [`reference/pi-spark-orb-executor.md`](reference/pi-spark-orb-executor.md).
- **Run a bounded local Pi Spark microtask**: only after an explicit owner request, load [`reference/pi-spark-local-executor.md`](reference/pi-spark-local-executor.md). This Darwin-only persistent-host route preserves owner-established authentication and gives Pi no tools; it applies only validated replacements to exact allowed files.

Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md).

## Safety

- Exact model `openai-codex/gpt-5.3-codex-spark` through owner-operated ChatGPT Codex OAuth.
- API keys, ambiguous billing, missing trusted quota evidence, automatic retry/fallback, repository authority, and credential transfer fail closed.
- The local route never reads, copies, rewrites, logs out, or deletes shared authentication and treats all output and edits as untrusted.
- Do not activate from incidental Pi/Spark mentions.
