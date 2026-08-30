# amux troubleshooting

## Diagnose before mutation

```sh
amux --json install doctor
amux --json runner doctor --all
tmux list-panes -a -F '#{session_name}\t#{window_name}\t#{pane_id}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_start_command}'
```

Do not treat a tmux name, cwd similarity, stale output, or missing catalog evidence as ownership proof.

## Partial success and retries

Exit `1` means mutation may have started. Inspect `successful`, `skipped`, and `failed` before retrying. Exit `2` means preflight rejected without mutation. Preserve the same desired-state request and never bypass the machine lock.

Runner pin requires a canonical existing directory. Present-workdir reconcile is a no-op. Missing-workdir reconcile, remove, and unpin fail closed and retain the row until authoritative process/catalog absence can be proved. Use `runner park` to stop an exact verified process while preserving configuration.

If a removed worker/coordination command is required by old operational notes, stop. Do not find an older binary, edit registries, synthesize a terminal transition, or redirect `amux report` to `/amux-tycho`. Historical stores are inert evidence.

Provider-specific receipts remain governed by their separate explicit-only skills. A Claude route that requires later core Amux worker teardown is blocked because that teardown no longer exists; preserve its process, receipt, evidence, artifacts, and fence.
