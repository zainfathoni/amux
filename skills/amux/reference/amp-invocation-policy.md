# Native Amp invocation policy

## Delegate work

1. Choose `low` for mechanical tasks, `medium` for ordinary implementation, or `high` for hard architecture, debugging, or review. Other modes require an explicit owner request. Native selection requires no subscription or availability evidence preflight and no compatibility resolver call.
2. Use one authenticated native `create_thread` call with the selected mode on the exact intended Workspace Project/Orb or exact live runner/workdir. Send a lean task prompt with acceptance criteria, relevant constraints, validation, and expected reply. Keep native parent/reply routing only; create no Amux lifecycle state.
3. If creation or the selected mode is unavailable, rejected, or indeterminate, stop and report it. Do not retry, downgrade the mode, or switch executors.

## Read and coordinate threads

Read accessible Amp threads as needed for the task, without separate owner approval or an Amux resolver preflight. Use native tools for messages and replies. Permission to read does not authorize teardown, archival, or other mutations.

## Compatibility maintenance only

When maintaining or diagnosing the historical resolver or observation-only permission adapter, read [`amp-invocation-compatibility.md`](amp-invocation-compatibility.md). Its schemas and evidence gates do not govern native work.
