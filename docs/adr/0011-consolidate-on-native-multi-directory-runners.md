---
status: accepted
date: 2026-09-17
supersedes: 0008
---

# Consolidate Amux on native multi-directory runners

## Decision

Keep Amux active as a small declarative configuration and operating-system activation layer for native Amp runners. Native Amp owns runner execution, its served-directory catalog, and automatic updates. Amux owns named runner profiles and generates systemd user services or launchd agents that execute Amp directly.

Use one native `amp --no-tui` runner per machine where practical. Native Amp serves directories through repeated `--dir`, dynamic `amp runner dirs add|list|remove`, and `--discover-dirs`; arbitrary non-Git directories such as Obsidian vaults are valid explicit directories. A second named profile is appropriate only when a separately identified or isolated runner is required.

Each profile declares its stable runner ID, startup directory, discovery choice, explicit served directories, and remote-terminal choice in `native-runners.json`. `amux runner service install` validates the directories, resolves the exact Amp executable, writes owned service artifacts, and starts them. The service manager invokes Amp directly; Amux is not a resident supervisor.

The old per-workdir runner registry, tmux process lifecycle, and scheduled Amp maintenance remain temporarily available for compatibility but are no longer the destination architecture. Their removal requires a separately reviewed migration after native service profiles have field evidence.

## Rationale

ADR 0008 retained Amux because native Amp did not provide multi-directory serving or self-updating long-running runners. Amp added those capabilities on September 17, 2026, eliminating the need for one Amux-managed process per workdir. Amp still does not own declarative machine configuration or login/boot activation, so fully retiring Amux would discard a useful reproducible boundary.

`amux runner teardown` remains a legacy worktree cleanup operation during the transition. It is not part of native runner profile or service ownership.

## Migration

1. Create `native-runners.json` with one profile for each required native process.
2. Use `--discover-dirs` semantics for nearby Git checkouts and explicit directories for unrelated or non-Git roots.
3. Dry-run and install the generated systemd or launchd services.
4. Confirm every required directory appears in the Amp location picker.
5. Retire equivalent per-workdir Amux runners and scheduled maintenance separately after field verification.

This ADR does not itself authorize a release, deployment, machine-configuration mutation, or retirement of existing runner processes. Those actions require their normal explicit execution and review.

## Consequences

- One profile may span code roots, Obsidian vaults, dotfiles, and other arbitrary directories.
- Profile names identify generated service artifacts; runner IDs remain native Amp identities.
- Service installation owns only artifacts recorded by digest and refuses unrecognized files.
- Amp updates itself; new runner profiles do not use Amux scheduled maintenance.
- Existing CLI commands and historical stores remain behaviorally compatible during the transition.
