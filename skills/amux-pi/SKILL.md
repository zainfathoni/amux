---
name: amux-pi
description: "Experimental Pi on Spark in an Amp Orb for amux. Use only after an explicit owner request to run Pi with openai-codex/gpt-5.3-codex-spark via owner-operated ChatGPT Codex OAuth. Not an amux lifecycle resource."
---

# amux-pi (experimental)

Disposable provider-specific experiment. **Not** an amux CLI resource, worker, runner, or provider-neutral orchestration state.

## Route

- **Run Pi on Spark in an Amp Orb**: only after an explicit owner request, load [`reference/pi-spark-orb-executor.md`](reference/pi-spark-orb-executor.md).
- **Spike one bounded local file replacement**: use [`experimental/pi-spark-local`](experimental/pi-spark-local) only after explicit owner authorization. It admits the exact Pi 0.80.10 package/bin and Spark model, launches one ordinary text print-mode attempt, and applies one strictly bound replacement in an otherwise clean worktree. It does not parse Pi lifecycle events or use quota as a runtime gate.

Trigger checklist: [`reference/trigger-phrases.md`](reference/trigger-phrases.md).

## Safety

- Exact model `openai-codex/gpt-5.3-codex-spark` through owner-operated ChatGPT Codex OAuth.
- API keys, ambiguous billing, automatic retry/fallback, repository authority, and credential transfer fail closed. The fresh-Orb recipe requires trusted quota evidence; the local spike treats quota observations only as optional smoke evidence, never runtime admission.
- The local spike checks auth-file metadata without reading auth contents, requires retry/provider-retry/compaction disabled in owner-managed settings, disables Pi tools/session/context extras, bounds both output streams and time, verifies process-group termination, and rejects any Pi-created worktree diff.
- Deterministic tasks may supply an exact expected replacement SHA-256, which must match before apply without newline normalization. Open-ended replacements remain explicitly untrusted and require coordinator diff review and tests.
- The bounded task packet is supplied over stdin, never process argv. The 512 KiB prompt and default stdout bounds admit the worst-case JSON escaping of a 64 KiB file plus the bounded task. Empty replacement bytes are intentional; deterministic deletion still requires the expected replacement hash. The response must echo `original_sha256` as canonical lowercase hexadecimal.
- Receipts record requested-model launch evidence, not provider-side model or billing proof; the existing `stderr` receipt field is only `empty` or `present_redacted`. Post-run admission and transactional rollback must succeed before completion; exit 3 with `indeterminate_applied_state` means rollback could not establish a clean state.
- Every Git command has bounded output and disables pagers, hooks, fsmonitor, optional locks, external diffs, and text conversion where applicable. Repository-local clean/process filters or `filter` attribute bindings fail admission before status/diff/index checks.
- Process-group cleanup signals the group before reaping its exact leader, then never probes or signals the released PGID. Pi 0.80.10 uses `PI_OFFLINE` to suppress startup/model-catalog refresh network; its provider stream path remains enabled, as also demonstrated by the preserved authorized real helper attempt through this exact environment.
- Do not activate from incidental Pi/Spark mentions.
