# Apply the experimental Amp invocation policy

This is the progressively disclosed policy for issue #175. It does not make amux an Amp-wide control plane, change the independent #147/#177 Claude delegation route, or perform the blocked #176 canary. Run [`../scripts/resolve-amp-invocation-policy`](../scripts/resolve-amp-invocation-policy) only at the explicit skill preflight or permission boundary; it performs no action itself.

## Supported surface

The only promoted tuple is the skill-owned automatic `/amux spawn` preflight. Pass schema version `1`, action `amux_spawn`, `automatic:true`, and the selected mode before any spawn command. `medium` is allowed; every other automatic mode is rejected without rewriting it. User-requested non-medium modes remain governed by the existing exact instruction rule rather than a model-asserted helper grant.

Current Amp-native child creation, Read Thread, specialist, generic Task, and native-message tuples are **observed** or **instruction-only**. Their exact tool schemas, effective defaults, trusted approval fields, one-call/replay behavior, or permission interception were not all proven for the probed client. Never treat their `would_allow`, `would_ask`, or `would_reject` advisory result as binding, and never claim that the observed permission adapter covers plugin-created threads or other bypasses. Amp-native `runner(id)` is an executor identity, not amux's canonical-workdir runner.

## Read Thread

Task context requires exact approval for one target; a URL is provenance, not authorization. Preserve the existing discrepancy-recovery exception: after an authorized `/amux` lifecycle or coordination operation names a concrete local/GitHub discrepancy, deterministic evidence is exhausted, and durable/local/GitHub evidence separately establishes the exact relationship, one narrow query of that exact thread is eligible. After one query, or when any prerequisite is absent, block rather than widening or chaining. The resolver records this intent as observed because the current native tuple is unproven.

## Capacity and diagnostics

Capacity remains separate from action approval. Provider and provider version, schema version, pool, window, unit, observed amount, freshness, confidence, charge route, reservation state, and reserve status are independently required. Missing fields, schema drift, stale or weak evidence, an unknown charge route, no held reservation, or unknown reserve status produce `would_ask`; one known pool never substitutes for an unknown potentially charged pool. Because no capacity tuple is promoted, even complete-looking evidence remains `would_ask` rather than allowing the model-backed action. Keep providers, units, and windows separate, publish no reserve default, scrape no undocumented state, and redirect no denial to another provider.

Caller diagnostics are public-safe and bounded to action, result, reason, capability, and redacted source classes. Raw delegated arguments are never logged. Do not expose paths, floors, account identities or capabilities, balances, overlays, raw targets, or cross-call correlators. No decision ledger ships because the probed native surfaces expose no stable typed action identity suitable for one.

The permission adapter is observation-only for every current native tool. Configure it only to collect bounded capability results; its public result is explicitly advisory (`would_allow`, `would_ask`, or `would_reject`) while its process exit remains allow. Only the explicit automatic-spawn resolver call can return binding `allow` or `reject`. Never bypass a binding `ask` or `reject` from a future independently promoted tuple.
