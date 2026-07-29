---
name: amux-pi
description: "Experimental Pi on Spark for amux, either in a fresh Amp Orb or through the bounded physical-host helper in a dedicated local worktree/tmux with no Orb. Use only after an explicit owner request to run Pi with openai-codex/gpt-5.3-codex-spark via owner-operated ChatGPT Codex OAuth. Not an amux lifecycle resource."
---

# amux-pi (experimental)

Disposable provider-specific experiment. **Not** an amux CLI resource, worker, runner, or provider-neutral orchestration state.

Before choosing an executor, consult the repository's [provider executor readiness matrix](https://github.com/zainfathoni/amux/blob/main/docs/provider-executor-readiness.md). Static recipe support and a physical-host smoke do not promote the distinct fresh-Orb or general-mutation routes.

## Route

- **Run Pi on Spark in an Amp Orb**: only after an explicit owner request, load [`reference/pi-spark-orb-executor.md`](reference/pi-spark-orb-executor.md).
- **Run Pi on Spark on a physical runner in a dedicated local worktree/tmux with no Orb**: only after an explicit owner request naming that local execution intent, load [`experimental/pi-spark-local`](experimental/pi-spark-local). This is the existing bounded local helper/contract, not the fresh-Orb recipe and not authority for arbitrary local work: it admits the exact Pi 0.82.1 package/bin and its exact Node `>=22.19.0` engine contract, plus the Spark model; launches one ordinary text print-mode attempt; and applies one strictly bound replacement to one tracked file in an otherwise clean dedicated worktree. It does not parse Pi lifecycle events or use quota as a runtime gate.
- **Spike one bounded local file replacement**: the same explicit-owner-only [`experimental/pi-spark-local`](experimental/pi-spark-local) route and one-tracked-file bound apply.

Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md).

## Safety

- Exact model `openai-codex/gpt-5.3-codex-spark` through owner-operated ChatGPT Codex OAuth.
- API keys, ambiguous billing, automatic retry/fallback, repository authority, and credential transfer fail closed. The fresh-Orb recipe requires trusted quota evidence; the local spike treats quota observations only as optional smoke evidence, never runtime admission.
- The local spike checks auth-file metadata without reading auth contents, requires retry/provider-retry/compaction disabled in owner-managed settings, disables Pi tools/session/context extras, bounds both output streams and time, verifies process-group termination, and rejects any Pi-created worktree diff.
- Pi 0.82.1's normal managed provider-catalog cache is allowed on this explicitly trusted host when its path is a bounded regular file that is not group/world-writable. The exact selector and receipt prove the requested model, not provider-side execution or billing; Amp still treats the replacement as untrusted and verifies it independently.
- Deterministic tasks may supply an exact expected replacement SHA-256, which must match before apply without newline normalization. Open-ended replacements remain explicitly untrusted and require coordinator diff review and tests.
- The bounded task packet is supplied over stdin, never process argv. The 512 KiB prompt and default stdout bounds admit the worst-case JSON escaping of a 64 KiB file plus the bounded task. Empty replacement bytes are allowed; without an expected replacement hash, an empty replacement remains `replacement_applied_untrusted` and requires coordinator review. The response must echo `original_sha256` as canonical lowercase hexadecimal.
- Receipts record requested-model launch evidence, not provider-side model or billing proof; the existing `stderr` receipt field is only `empty` or `present_redacted`. Post-run admission and transactional rollback must succeed before completion; exit 3 with `indeterminate_applied_state` means rollback could not establish a clean state.
- Every Git command has bounded output and disables pagers, hooks, fsmonitor, optional locks, external diffs, and text conversion where applicable. Repository Git-command stdout above 1 MiB is outside this spike's bound and fails closed. Effective repository configuration—including local/worktree scopes and their includes—is scanned under disabled global/system config; clean/process filters and `core.attributesFile` fail admission. A private randomized lower-precedence `filter` fallback makes active, unset, and unspecified repository attribute forms fail before status/diff/index checks.
- One inert `/bin/cat` guardian anchors the process group while the single Pi attempt runs. Pi Wait, timeout, overflow, and bounded stream drains are observed independently; every terminal path kills the anchored group, with exact Pi/guardian PID fallback and one final anchored-group retry if that signal fails, then reaps the guardian last. Cleanup collection is bounded and fails closed if group termination cannot be verified. The PGID is never signaled or probed after guardian Wait. Pi 0.82.1 uses `PI_OFFLINE` to suppress startup-management and model-catalog refresh network; it does not suppress the explicitly selected provider inference request, whose stream path remains enabled.
- Do not activate from incidental Pi/Spark mentions.
