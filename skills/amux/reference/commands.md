# amux command reference

Long selectors are canonical: `--workspace`, `--workdir`, `--json`, and `--dry-run`.

```sh
# Runner-only top-level aliases
amux list|launch|park|restart|remove|doctor|reconcile [runner selectors]

# Explicit runner routes
amux runner pin --workspace <name> --workdir <existing-directory>
amux runner pin --current
amux runner list|launch|park|restart|remove|doctor|reconcile [runner selectors]
amux runner unpin --workdir <path>|--current

# Workspace, maintenance, installation
amux workspace list
amux workspaces
amux runner maintenance install --update-owner <self|external>
amux runner maintenance run [--scheduled]
amux runner maintenance remove
amux install doctor
amux migrate-config
amux update
```

Bare `amux` and no-selector `launch` launch all configured runners. Other machine-wide mutations require `--all`. `workspace list` and `workspaces` report runner workspaces only.

Runner pin is active admission and requires an existing canonical directory. Git repository, worktree, and lock state are not runner requirements.

Runner park preserves the row. Runner remove, unpin, and missing-workdir reconcile currently retain and reject because current Amp APIs cannot prove authoritative process/native-catalog absence. Present-workdir reconcile is a no-op. Never delete a row or process on unreadable, conflicting, or unproven evidence.

The former `worker`, `spawn`, `shelve`, `unshelve`, `teardown`, `group`, `report`, and `callback` routes are removed and fail before effects. Historical coordination stores are inert. The `report` tombstone is not `/amux-tycho`; that explicit-only skill uses a separate receipt store and protocol.

`--config-dir <path>` and `AMUX_CONFIG_DIR` select the directory containing active `runners.tsv`. Historical worker/coordination files in that directory are not part of active runner operation and must remain untouched.

`--json` emits one v1 envelope. `--dry-run` puts prospective changes under `planned`. Exit `0` means no failures, exit `1` means runtime failure after mutation may have begun, and exit `2` means preflight rejection before mutation. Mutations and scheduled maintenance share one bounded machine lock.

At login/boot, systemd or launchd runs `amux launch --all`. Verified patterns are systemd `Type=oneshot` plus `RemainAfterExit=yes`, and a RunAtLoad LaunchAgent with `AbandonProcessGroup=true`. The OS activates Amux; it does not directly supervise replacement Amp runners.
