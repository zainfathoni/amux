# Machine-local Claude read-only delegation

This historical helper route is closed to new work. Its immutable receipt binds lifecycle ownership to an Amux worker, and core Amux workers, reports, finish, and teardown have been removed. Starting a new receipt would create evidence with no valid completion or cleanup route.

Do not create a receipt, launch or acquire Claude, consume or acknowledge a report, inject input, park a pane, or invoke provider lifecycle recovery. Use native Amp child threads for new task coordination. A separately documented fresh-Orb Claude route may be used only under its own explicit trigger and authority contract; it is not a fallback from this blocked route.

For historical receipts, follow [`claude-delegation-recovery.md`](claude-delegation-recovery.md): inspect only exact owner-supplied evidence, preserve every process and artifact, and return the worker-lifecycle-removed blocker.
