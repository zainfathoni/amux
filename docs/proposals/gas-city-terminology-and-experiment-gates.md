---
status: future-horizon-proposal
---

# Gas City terminology and experiment gates

This proposal records an in-progress product-strategy grill about what amux should learn from Gas City after Amp introduced native agent-to-agent work and subscriptions. It is a future-horizon artifact, not an accepted lifecycle decision, implementation plan, or Gas City adoption decision.

## Fixed boundaries

- [ADR 0001](../adr/0001-agent-first-client-lifecycle-cli.md) and the delivered #105/#118 lifecycle contracts remain fixed.
- [ADR 0002](../adr/0002-post-lifecycle-long-term-vision.md) remains a separate proposed horizon.
- The [interactive Claude Code delegation pilot](claude-code-interactive-delegation-pilot.md) and the native #147–#151 dependency graph remain separate and unchanged.
- This proposal does not make a Claude client an amux worker, runner, or work-group member.
- amux does not gain a resident supervisor, provider-neutral task graph, formula, order, named persistent persona, or separate UI through this proposal.
- No Gas City installation or runtime experiment is authorized by this document.

## Primary-source change in context

Amp's July 17, 2026 [From Agent to Agent](https://ampcode.com/news/from-agent-to-agent) release lets agents create Amp threads in orbs or live runners and exchange messages and files. Amp's July 18, 2026 [Subscriptions, At Last](https://ampcode.com/news/subscriptions) release adds included orb capacity and permits linked ChatGPT-subscription use for GPT-5.6.

These releases remove much of the prospective value of wrapping Amp-only spawning and communication. The preferred trusted path is now Amp as the harness, GPT-5.6 Sol as the model, a linked ChatGPT subscription as the billing source, and Amp orbs as the default remote execution topology. Live runners remain appropriate when work requires uncommitted state, local services, credentials, hardware, or other machine-specific capabilities.

Gas City's current official documentation presents a substantially different control-plane model:

- a [city](https://github.com/gastownhall/gascity/blob/main/docs/getting-started/how-gas-city-works.md) is a supervised deployment/configuration scope;
- a rig is a registered project and logical work namespace;
- an agent is configured worker identity rather than one process;
- a session is a live harness process;
- a bead is durable work state;
- a formula materializes a reusable dependency graph;
- an order triggers a formula or script;
- the normal runtime uses a machine-wide non-LLM supervisor;
- the [smallest quickstart](https://github.com/gastownhall/gascity/blob/main/docs/getting-started/quickstart.md) can use one city, one rig, one bead, and one on-demand agent;
- the [file bead provider](https://github.com/gastownhall/gascity/blob/main/README.md) avoids the default Dolt and `bd` data plane;
- Amp, Claude Code, and Codex have built-in harness profiles, but current source does not give Amp managed-hook parity with Claude or Codex.

Gas City therefore remains useful as a comparison and vocabulary source, but it is not presumed to be amux's successor.

## Current strategic decision

The six candidate outcomes remain distinct:

1. run no Gas City experiment;
2. study or borrow selected concepts only;
3. run a disposable one-agent compatibility experiment;
4. use Gas City as a heterogeneous control plane while retaining only differentiated amux/fleet adapters and skills;
5. sunset substantial amux lifecycle or orchestration surface;
6. adopt a broader Gas City control plane.

The current choice is outcome 2 while outcome 3 is deferred behind evidence. Outcomes 4–6 are neither assumed nor authorized.

Gas City is explicitly falsifiable. If Amp threads plus bounded coordinator checkpoints handle Amp-native work, and provider-specific seams handle the few trusted heterogeneous delegations, amux needs no additional control plane. A bounded checkpoint is an inert resumable pointer; it does not own assignment, claiming, completion, session routing, or continuous materialized task state.

### Amp-native baseline

Before reconsidering a runtime experiment, use three real Amp-native workflows:

1. one independent implementation or bug fix;
2. one verification, review, or QA side quest;
3. one multi-artifact or cross-project delegation using native messages or files.

The baseline is orb-first. The first two workflows use coordinator-created children. The third may permit one bounded second-level delegation, with no recursive fan-out beyond it. Necessary context should be supplied directly rather than recovered through unrelated thread reads. Model routing and billing assumptions must be observed rather than inferred.

Record only enough evidence to compare total coordination cost:

- topology choice;
- useful versus discarded results;
- coordinator turns and human intervention;
- report and artifact transfer outcomes;
- recovery or resumption friction;
- elapsed time;
- observed model and billing path where available;
- trusted provider capacity that still expires unused;
- a bounded checkpoint, when needed, containing thread references, current phase, expected artifact, stopping reason, and next action.

### Amp-orchestrated provider executors

The selected near-term heterogeneous topology keeps Amp as the only orchestrator and uses provider-specific harness invocations as bounded executors:

```text
Amp coordinator with GPT-5.6 Sol
├── fresh Amp Orb thread → Claude Code → Claude Opus
└── fresh Amp Orb thread → Pi → GPT-5.3-Codex-Spark
```

Each executor Orb is independent. It receives a self-contained packet through native Amp messaging or file transfer, invokes exactly one selected harness under its provider-specific authority and isolation contract, and returns bounded result and execution evidence. It does not independently re-plan the workflow, recursively delegate, retain provider-neutral task state, or decide whether its result is accepted. The originating Amp thread verifies and integrates useful output.

The executor split is intentionally asymmetric:

- Claude Code uses a `CLAUDE_CODE_OAUTH_TOKEN` Amp project secret and official headless invocation pinned to an owner-approved model. Project secrets are injected only when a fresh Orb is created. A bounded experiment verified first-party subscription OAuth and a successful `claude-opus-4-8` invocation without API-key credentials, browser onboarding, tools, repository access, or session persistence. Interactive Claude TUI onboarding is not part of this route.
- Pi uses owner-operated ChatGPT Codex OAuth because current official Pi sources do not expose a Codex OAuth environment-variable equivalent. Its credential file is mutable refresh state local to the Orb. Package installation, isolated Node-runtime compatibility, API-key exclusion, credential-state preflight, and the exact `openai-codex/gpt-5.3-codex-spark` selector have been verified; a real Spark invocation and quota debit remain unverified.

Neither provider executor becomes an ADR-0001 worker, runner, harness client registry entry, or #118 work-group member. Same-Orb colocation is unnecessary because Amp already supplies orchestration and transport. Gas City is unnecessary unless repeated provider-neutral coordination requirements outgrow native Amp threads plus these thin provider-specific recipes.

Implementation and evaluation are split into independently owned issues:

- [#205](https://github.com/zainfathoni/amux/issues/205) owns only the Claude/Opus Orb executor recipe;
- [#206](https://github.com/zainfathoni/amux/issues/206) owns only the Pi/Spark Orb executor recipe;
- [#207](https://github.com/zainfathoni/amux/issues/207) owns the later shared Amp coordinator workflow and must not absorb provider-specific mechanics.

### Runtime reconsideration gate

Do not reconsider Gas City merely because another subscription has unused capacity. Reconsider when the #147 Claude experiment's custom implementation begins growing into substantial receipt, hook, routing, session-lifecycle, or provider-neutral control-plane machinery. Growth is an architectural comparison trigger, not evidence that Gas City itself must be adopted.

A later comparison may conclude that amux should:

- borrow a Gas City concept or term;
- simplify or narrow the #147 seam;
- use a smaller provider-specific adapter;
- run one disposable Gas City–Claude compatibility experiment;
- stop the heterogeneous experiment;
- or propose a separate control-plane decision.

The official Codex CLI is not automatically the preferred harness. The owner does not consider its prior result quality or harness reliability sufficient for trusted work and prefers Amp with GPT-5.6 Sol for ordinary execution. However, unused GPT-5.3-Codex-Spark capacity remains a distinct model and subscription opportunity because Amp does not currently expose Spark. Harness trust, model utility, subscription accounting, and isolation must be evaluated separately rather than accepting or rejecting them as one product.

### Pi and Spark delegation horizon

[Pi](https://pi.dev/docs/latest) is the provisional Spark harness. Current official Pi sources support ChatGPT Plus/Pro Codex OAuth, expose `gpt-5.3-codex-spark`, and provide non-interactive modes without requiring a persistent TUI client. OpenCode or the official Codex CLI should be considered only if Pi fails a concrete requirement. Gas City is not a prerequisite for this route.

The selection policy maximizes useful subscription consumption rather than raw token use:

- keep Amp with GPT-5.6 Sol on critical-path judgment and trusted ordinary execution;
- use Spark when a bounded delegation has positive expected value after context preparation, verification, integration, and risk costs, even when Sol would perform better;
- lower the task-priority threshold as the separate Spark quota approaches expiry;
- never relax trust, authority, isolation, or verification because quota is expiring;
- do not invent valueless tasks merely to consume quota;
- empirically verify that Spark usage draws from the expected model-specific pool and does not constrain the Sol path.

The preferred topology uses an [Amp orb](https://ampcode.com/manual/orbs) as the outer machine boundary. One quota-window-scoped orb may process several bounded Spark delegations, then be logged out and archived at quota reset or experiment end. This explicit workflow lifetime amortizes authentication without creating a permanent persona, project resource, or machine-wide control plane.

Authentication uses Pi's manual device-code login from the running orb terminal after orb creation. Do not upload local Pi credentials, put refresh tokens in project secrets, commit credentials or login into setup files, or substitute an API key that would use API billing. Credential storage, logout, and revocation behavior must be inspected before claiming cleanup or revocation.

The orb's Amp thread is a thin launcher. It receives a complete bounded packet, invokes Pi exactly, and returns the result plus bounded execution metadata without independently solving the task, interacting with Pi repeatedly, normalizing the answer through another model turn, or retaining provider-neutral task state. Amp remains responsible for assessing the result.

The first experiment is deliberately no-tool and non-interactive:

- Amp supplies a self-contained evidence packet;
- Pi receives no filesystem, shell, extension, skill, discovered context, or persistent-session authority;
- Pi's final stdout is treated as untrusted data rather than instructions for Amp;
- synchronous execution has a bounded timeout, output limit, explicit exit/stderr interpretation, and no blind retry;
- no receipt, background client, report callback, or parking lifecycle is added before real need;
- no repository mutation, credential-bearing operation, or autonomous fan-out is permitted.

The first experiment succeeds only when one real task produces a result useful enough to affect Amp's analysis, plan, review, or implementation; the selected Pi, Spark, and subscription route are verified without retaining secrets; native Amp messaging returns the result; coordination and verification remain cheaper than the value produced; and no undeclared context or authority is exposed. Merely installing Pi, receiving a model response, reducing quota, matching Sol, or producing a patch is insufficient.

The first real Spark task reviews this proposal. Its self-contained packet is capped at approximately 20 KB and contains only the decision summary, Pi/Spark horizon, anti-conflation map, unresolved questions, and an explicit rubric. A short bibliography distinguishes provenance URLs from copied evidence; because Pi has no retrieval tools, it must not claim to have verified those sources. The fixed-heading Markdown result contains: verdict; material contradictions; unsupported assumptions; terminology leaks; safety and quota-economics gaps; recommended changes; and confidence and limitations. The verdict is `accept`, `accept-with-changes`, or `reconsider`. The packet does not grant repository access.

The invocation contract uses Pi print mode, an ephemeral session, a fixed system prompt, and explicit resource denials. The concrete launcher should be equivalent to:

```bash
printf '%s' "$PACKET" |
  PI_SKIP_VERSION_CHECK=1 pi -p \
    --model openai-codex/gpt-5.3-codex-spark \
    --thinking high \
    --no-session \
    --no-tools \
    --no-extensions \
    --no-skills \
    --no-prompt-templates \
    --no-themes \
    --no-context-files \
    --no-approve \
    --system-prompt "$FIXED_REVIEWER_PROMPT" \
    "Review the evidence packet from stdin according to its rubric."
```

Run the invocation from a newly created empty temporary directory. An outer launcher enforces a five-minute wall timeout, 64 KiB stdout limit, and 16 KiB stderr limit. Timeout or output overflow is a failed experiment. There is no automatic retry or model/harness fallback: classify the failure, return bounded evidence, and seek approval before any corrected invocation.

Before invoking the model, verify that Pi's model catalog resolves `openai-codex/gpt-5.3-codex-spark`. Return the Pi version, resolved provider and model, UTC start and end times, exit status, stdout and stderr byte counts, and a redacted stderr summary alongside the fixed-heading review. Do not return credentials, refresh tokens, raw authentication output, or a full event stream.

Spark's review is advisory evidence. The originating Amp thread and owner assess its claims before any proposal edit; neither Spark nor the thin orb launcher may mutate the repository. A successful first invocation proves compatibility, not a reusable workflow. If useful, select a different real bounded task for the second manual invocation rather than repeating the proposal review. Consider a progressive-disclosure skill or recipe only after at least three useful manual invocations demonstrate repetition.

Before and after the invocation, ask the existing macOS amux thread through Amp native messaging to run CodexBar and return Spark percentage, reset time, timestamp, and source confidence without account identity or credentials. Treat this as remote observation, not orchestration state, and preserve the returned values in the experiment record. This path was validated on 2026-07-20 when CodexBar directly reported the Codex OAuth source at exact confidence, with Spark at 0% used and 100% remaining. Repeat the observation immediately before the invocation rather than treating that validation reading as a later baseline. Do not install a Pi quota extension or call undocumented quota endpoints merely to automate the first observation.

Install Pi manually in the running orb terminal. Query and inspect the current version and package integrity metadata, then install that exact `@earendil-works/pi-coding-agent` version with npm's `--ignore-scripts` option and verify `pi --version`. Authenticate only after installation. Do not modify `.agents/setup`, install an implicit moving tag, use the curl installer, automatically install Pi on every orb, or build a custom image before the first result justifies repetition.

After login, verify only that `~/.pi/agent/auth.json` exists with mode `0600`, that sanitized output identifies the expected provider and credential type, and that no API-key environment variable supplied the route. Never print raw credential values. At the end of the quota-window-scoped orb, run Pi logout, verify that the Codex provider entry is absent, uninstall the pinned Pi package, inspect without blindly deleting any remaining Pi state, and archive the orb thread. If logout fails or the provider entry remains, stop and report rather than manually deleting state and claiming revocation. Record that local credential deletion does not prove provider-side token revocation. Do not retain the orb credential indefinitely or claim stronger cleanup than observed.

The owner later authorized bounded Claude and Pi Orb preflights. Claude/Opus headless subscription execution is verified. Pi installation and authentication preflight are verified, but owner-operated Codex OAuth login and a Spark model invocation remain separately gated. Future invocations still require the concrete packet, command, authority, and cleanup contract defined by their provider-specific recipe.

## Anti-conflation map

| amux | Gas City | Boundary |
| --- | --- | --- |
| Workspace | City | An amux workspace is a tmux lifecycle group. A city is a supervised deployment/configuration scope. |
| Runner | Rig | A runner makes a machine and workdir available to Amp. A rig registers the project where Gas City routes work. |
| Worker | Agent plus session | An amux worker is one configured, thread-bound Amp client. A Gas City agent is configuration that may have zero or more live sessions. |
| Thread | No direct equivalent | A thread is vendor conversation identity independent of a local client. |
| Delegation receipt | Bead | A receipt is bounded evidence and delivery state. A bead is authoritative durable work state. |
| Named recipe | Formula | A recipe selects configuration and instructions. A formula materializes dependent work. |
| Runner maintenance | Order | Runner maintenance is one externally scheduled bounded operation. An order is a general trigger-to-action resource. |

None of these pairs is a synonym.

## Terminology adoption rule

Borrow a Gas City term only when amux means substantially the same thing. Otherwise use ordinary amux language. Trial future terms in experimental workflows before promoting them to `CONTEXT.md` or public stable skill vocabulary.

Gas City's who/what/where/how/when decomposition is accepted as a design-review lens, not a new public resource model:

| Question | amux design lens |
| --- | --- |
| Who executes? | Harness and verified client |
| What is being done? | Delegation, or a future plain-language work item |
| Where does it run? | Project, repository, worktree, and canonical workdir |
| How should it proceed? | Instruction bundle or named recipe |
| When should it run? | Explicit invocation or a narrowly owned external trigger |

### Candidate experimental vocabulary

**Harness** — An agent application that executes model-backed work and supplies its own interaction, tools, and conversation lifecycle. Amp, Claude Code, and Codex are harnesses.

A harness is distinct from:

- its model provider or billing source;
- the selected model;
- one live client process;
- the client's durable vendor identity.

**Harness client** — One verified local interactive instance of a harness. Vendor-native durable identities remain explicit: for example, an Amp thread ID or Claude session ID. A harness client is not automatically an ADR-0001 worker.

**Harness invocation** — One bounded execution of a harness, interactive or non-interactive. An invocation does not imply durable task state, asynchronous scheduling, a client registry, or lifecycle ownership. A harness client is a longer-lived interactive instance with lifecycle identity; Pi print mode is an invocation without such a client resource.

**Control plane** — A system that owns provider-neutral work routing and durable orchestration state across harnesses. Gas City is such a control plane. Amp owns Amp-native orchestration. amux is currently a lifecycle substrate and workflow toolkit, not a heterogeneous control plane. A bounded delegation receipt does not by itself cross this boundary.

These terms remain experimental and do not yet belong in `CONTEXT.md`.

### Existing terms retained

- `backend` remains available for lower-level implementation or runtime mechanisms. Future cross-product text should use `harness`, not `backend`, when it means Amp versus Claude Code.
- `agent` may appear generically in prose, but identity and lifecycle rules use precise terms such as coordinator, worker, runner, harness, client, or thread.
- `delegate` remains the cross-client work verb.
- `recipe` remains configuration that selects instructions and conventions.
- amux retains its precise distinctions among durable report state, notification, delivery, acknowledgement, authorization, acceptance, and lifecycle.

### Terms not adopted

- `bead` is not an amux synonym for a task, receipt, report, or checkpoint. If amux ever needs durable provider-neutral work, prefer the plain term `work item` and make a separate ownership decision.
- `order` is not adopted. The operating system schedules runner maintenance; amux does not own a general scheduler resource.
- `mail` and `nudge` do not replace amux's existing report and transport vocabulary.
- `rig` is reserved unless amux someday registers and owns a project-scoped orchestration namespace. A repository, workdir, or resolved project context is not a rig.
- `formula` is reserved for a system that actually materializes dependent work items. A recipe or instruction bundle is not a formula.
- `sling` is reserved for an operation that creates and routes durable provider-neutral work. Current delegation does not have those semantics.

## Cross-harness workflow grammar

Experimental cross-harness workflows use these phases:

1. **Select** a harness based on capability, trust, capacity, and task fit.
2. **Acquire** one verified harness client by launching, resuming, teleporting, or explicitly adopting it.
3. **Delegate** bounded work to that client.
4. **Report** a semantic outcome.
5. **Deliver** and separately **acknowledge** the report.
6. **Park** or remove the verified client when authorized.

The verbs retain distinct meanings:

- **Spawn** provisions a new amux Amp worker and delivers its initial assignment under ADR 0001.
- **Launch** makes an already configured amux worker or runner available locally.
- **Delegate** entrusts bounded work to another client or thread without implying how it was created.
- **Create thread** names Amp's vendor-native operation.
- **Acquire** obtains verified lifecycle control over a local harness client; discovery alone is not acquisition.

Use **originating thread** for delegation provenance. Use **coordinator** only when a workflow grants explicit coordination authority, such as #118's work-group coordinator. Do not infer a persistent manager persona from origin alone.

There is no universal `complete` state. Preserve distinct facts:

- semantic outcome reported;
- report delivered;
- report acknowledged;
- declared handoff validated;
- result accepted or rejected;
- lifecycle action authorized;
- client parked or removed.

Idle, process exit, notification, and acknowledgement do not imply semantic completion or acceptance.

## Workflow roles are not personas

**Workflow role** — A bounded set of responsibilities or authority assumed by a client or thread within one explicitly bounded workflow. A role has no independent lifecycle, memory, conversation identity, or routed ownership.

A role may span several delegations inside one work group, but ends with that workflow. The workflow—not a pane, client, or persona—defines its lifetime. Roles use functional names such as coordinator, implementer, reviewer, verifier, or release owner.

A role defines responsibility or authority. A recipe selects instructions and launch configuration. A recipe may prepare a client for a role, but the two remain distinct.

The model distinguishes:

- a descriptive role used in one workflow;
- a reusable recipe that selects instructions;
- an authority-bearing coordinator role;
- a configured harness identity;
- and a persistent persona spanning tasks or conversations.

The initial strategy context rejected named persistent personas, not every named workflow role. Gas City's Mayor should not be described as rejected merely because persistent personas were rejected. Its exact identity, authority, persistence, and runtime semantics must be compared separately. amux does not adopt `Mayor`: it retains `originating thread` and authority-specific `coordinator`, while Mayor remains part of the Gas City translation map.

## Open questions for the next grill round

1. Is `harness` useful enough in real experimental instructions to promote later to `CONTEXT.md`?
2. Should the provider/model/billing-source distinctions also receive candidate terms, or remain ordinary descriptive language?
3. Which exact #147 implementation-growth signals should trigger a Gas City comparison?
4. If a comparison is triggered, should concept borrowing precede any disposable runtime experiment?
5. What repeated-use evidence should promote either provider-specific executor from an experimental reference into the stable `/amux` route?

## Optional paired-review convention

When the owner explicitly requests a heterogeneous review pair, one Claude/Opus executor and one Pi/Spark executor may support a recommendation only when both report no material disagreement and the decision is reversible, low-stakes, within an already owner-approved strategy, and adds no authority, persistent state, spend, shared mutation, or scope. Non-material caveats must be incorporated or recorded and remain visible to the owner. Owner preference, novel policy, risk tolerance, destructive or shared actions, and scope expansion always return to the owner. Cross-model agreement is useful evidence, not independent proof or acceptance authority.

## Preservation boundary

This document preserves shared understanding from the strategy grill and links the independently owned executor issues #205–#207. It does not modify accepted docs, alter #147 or its dependency graph, implement a skill, install Gas City, start a supervisor, or authorize a Gas City runtime experiment. Any promotion to canonical vocabulary or stable skill routing requires a later explicit decision and coordinated updates to the glossary, relevant proposal or ADR, skill references, and consistency checks.
