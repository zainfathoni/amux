# Domain glossary

## Workspace lifecycle

**Launch** — Make configured, active work available locally. Launching may create local tmux windows and Amp clients, but does not change remote thread state.

**Restart** — Replace a running local client in place while preserving its configuration and remote thread.

**Park** — Stop local execution while preserving both the restore configuration and remote thread. Parked work can be launched again without first changing its remote state.

**Shelve** — Defer work by hiding its remote thread and stopping local execution while preserving the restore configuration. Shelved work must be unshelved before it can be launched.

**Unshelve** — Make a shelved remote thread active again without launching it locally.

**Teardown** — Finish managed work by hiding its remote thread, removing its restore configuration, and stopping local execution.

**Pin** — Add work to restore configuration without changing local execution or remote thread state.

**Unpin** — Remove work from restore configuration without changing local execution or remote thread state.

**Machine scope** — Every configured interactive workspace on the current machine. Runner intent is a separate lifecycle and is not included.

**Workspace scope** — Every configured interactive window belonging to one named workspace.

**Window scope** — One configured interactive window within a workspace.
