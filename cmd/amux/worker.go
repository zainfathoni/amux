package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func isWorkerConvenience(path []string) bool {
	if len(path) != 1 {
		return false
	}
	switch path[0] {
	case "spawn", "shelve", "unshelve", "teardown":
		return true
	}
	return false
}

const maxLegacySpawnDiagnostics = 100

func appendLegacySpawnDiagnostics(a app, dir config.Directory, env *result.Envelope, selectors selectors, rows []config.Row, jsonOutput bool) error {
	operations, err := config.LoadOperationsReadOnly(dir.OperationsPath())
	if err != nil {
		return err
	}
	selectedThreads := make(map[string]bool, len(rows))
	for _, row := range rows {
		selectedThreads[row.Thread] = true
	}
	count := 0
	for _, operation := range operations {
		if operation.Kind != "worker-spawn" || !legacySpawnOperationMatches(operation, selectors, selectedThreads) {
			continue
		}
		if count == maxLegacySpawnDiagnostics {
			out := result.Outcome{Resource: result.CommandResource(), Action: "doctor", Message: "additional legacy worker-spawn evidence omitted (bounded diagnostic limit reached)"}
			env.Successful = append(env.Successful, out)
			if !jsonOutput {
				fmt.Fprintln(a.stdout, out.Message)
			}
			break
		}
		message := fmt.Sprintf("legacy-operation key=%q state=%s phase=%s thread=%q disposition=immutable-read-only-no-retry", boundedDiagnosticField(operation.Key), operation.State, operation.Phase, boundedDiagnosticField(operation.Resource.Thread))
		out := result.Outcome{Resource: result.CommandResource(), Action: "doctor", Message: message}
		env.Successful = append(env.Successful, out)
		if !jsonOutput {
			fmt.Fprintln(a.stdout, message)
		}
		count++
	}
	return nil
}

func legacySpawnOperationMatches(operation config.OperationRecord, selectors selectors, selectedThreads map[string]bool) bool {
	if selectors.All {
		return true
	}
	threads := []string{operation.Resource.Thread}
	if operation.ThreadAdoption != nil {
		threads = append(threads, operation.ThreadAdoption.ProvisionedThread, operation.ThreadAdoption.ReceivingThread)
	}
	for _, thread := range threads {
		if selectors.Thread != "" && thread == selectors.Thread || selectors.Thread == "" && selectedThreads[thread] {
			return true
		}
	}
	return false
}

func boundedDiagnosticField(value string) string {
	const limit = 256
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func (a app) executeWorker(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if in.Command.Name == "spawn" {
		return a.workerSpawn(in, dir, &env)
	}
	if in.Command.Name == "adopt" {
		return a.workerAdopt(in, dir, &env)
	}
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return &env, result.Preflight(err)
	}
	shelves, err := config.LoadShelvesReadOnly(dir.ShelvesPath())
	if err != nil {
		return &env, result.Preflight(err)
	}
	shelved := map[string]bool{}
	for _, thread := range shelves {
		shelved[thread] = true
	}
	if in.Selectors.Current {
		identity, identityErr := workerIdentityFromEnv()
		if identityErr != nil {
			return &env, result.Preflight(fmt.Errorf("--current requires valid spawn-injected AMUX_* identity: %w", identityErr))
		}
		in.Selectors.Current = false
		in.Selectors.Workspace = identity.Workspace
		in.Selectors.Window = identity.Window
		in.Selectors.Thread = identity.Thread
		in.Selectors.Workdir, err = config.CanonicalWorkdir(os.Getenv("AMUX_WORKDIR"))
		if err != nil {
			return &env, result.Preflight(fmt.Errorf("--current requires valid AMUX_WORKDIR: %w", err))
		}
	}
	rows = selectWorkerRows(rows, in.Selectors)
	if in.Command.Name == "doctor" {
		if err := appendLegacySpawnDiagnostics(a, dir, &env, in.Selectors, rows, in.Options.JSON); err != nil {
			return &env, result.Preflight(err)
		}
	}
	var shelfOnly []string
	if in.Command.Name == "remove" && in.Selectors.All {
		configured := make(map[string]bool, len(rows))
		for _, row := range rows {
			configured[row.Thread] = true
		}
		for _, thread := range shelves {
			if !configured[thread] {
				shelfOnly = append(shelfOnly, thread)
			}
		}
		sort.Strings(shelfOnly)
	}
	if in.Command.Name == "list" {
		for _, row := range rows {
			if in.Selectors.Shelf == "shelved" && !shelved[row.Thread] || in.Selectors.Shelf == "unshelved" && shelved[row.Thread] {
				continue
			}
			out := workerOutcome(row, "list", map[bool]string{true: "shelved", false: "unshelved"}[shelved[row.Thread]])
			out.Worker = workerPlacementDetails(row, "catalog")
			env.Successful = append(env.Successful, out)
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\t%s\tassignment=%s\n", row.Workspace, row.Window, row.Workdir, row.Thread, out.Message, workerAssignmentState(row))
			}
		}
		return &env, nil
	}
	if in.Command.Name == "pin" {
		return a.workerPin(in, dir, rows, &env)
	}
	if len(rows) == 0 && in.Command.Name == "reconcile" && in.Selectors.Workdir != "" {
		return workerReconcileUnregisteredWorkdir(in, &env)
	}
	if len(rows) == 0 && in.Command.Name == "reconcile" && (in.Selectors.Thread != "" || in.Selectors.All) {
		resource := result.CommandResource()
		if in.Selectors.Thread != "" {
			resource, _ = result.WorkerResource(in.Selectors.Thread)
		}
		env.Skipped = append(env.Skipped, result.Outcome{Resource: resource, Action: "reconcile", Message: "already in desired state; no workers.tsv binding exists"})
		return &env, nil
	}
	if len(rows) == 0 && in.Command.Name == "remove" && in.Selectors.All && len(shelfOnly) == 0 {
		out := result.Outcome{Resource: result.CommandResource(), Action: "remove", Message: "already in desired state"}
		env.Skipped = append(env.Skipped, out)
		return &env, nil
	}
	if len(rows) == 0 && !(in.Command.Name == "remove" && in.Selectors.All) {
		if (in.Command.Name == "unpin" || in.Command.Name == "remove") && in.Selectors.Thread != "" {
			resource, _ := result.WorkerResource(in.Selectors.Thread)
			out := result.Outcome{Resource: resource, Action: in.Command.Name}
			if in.Command.Name == "remove" && shelved[in.Selectors.Thread] {
				if in.Options.DryRun {
					env.Planned = append(env.Planned, out)
					return &env, nil
				}
				changed, err := config.RemoveShelf(dir.ShelvesPath(), in.Selectors.Thread)
				if err != nil {
					return &env, result.Runtime(err)
				}
				if changed {
					env.Successful = append(env.Successful, out)
					return &env, nil
				}
			}
			out.Message = "already in desired state"
			env.Skipped = append(env.Skipped, out)
			return &env, nil
		}
		if in.Command.Name == "doctor" && len(env.Successful) > 0 {
			return &env, nil
		}
		return &env, result.Preflight(errors.New("no configured worker matches the selector"))
	}
	if in.Command.Name == "teardown" && len(rows) != 1 {
		return &env, result.Preflight(fmt.Errorf("teardown requires exactly one configured worker; selector matched %d", len(rows)))
	}
	inspections := make(map[string]workerInspection, len(rows))
	var reconcilePlans map[string]workerReconcilePlan
	if in.Command.Name == "reconcile" {
		reconcilePlans, err = planWorkerReconcile(dir, rows, &env)
		if err != nil {
			return &env, result.Preflight(err)
		}
	}
	if in.Command.Name == "launch" || in.Command.Name == "restart" {
		for _, row := range rows {
			if (in.Command.Name == "launch" || in.Command.Name == "restart") && shelved[row.Thread] {
				continue
			}
			workdir, canonicalErr := config.CanonicalWorkdir(row.Workdir)
			if canonicalErr != nil {
				return &env, result.Preflight(canonicalErr)
			}
			stat, statErr := os.Stat(workdir)
			if statErr != nil || !stat.IsDir() {
				return &env, result.Preflight(fmt.Errorf("missing workdir: %s", workdir))
			}
		}
	}
	if workerCommandNeedsTmux(in.Command.Name) {
		for _, row := range rows {
			if (in.Command.Name == "launch" || in.Command.Name == "restart") && shelved[row.Thread] {
				continue
			}
			inspection, inspectErr := inspectWorker(row)
			if inspectErr != nil {
				if in.Command.Name == "reconcile" {
					plan := reconcilePlans[row.Thread]
					plan.localState = "unknown"
					reconcilePlans[row.Thread] = plan
					out := workerReconcileOutcome(row, plan, "refuse_unknown_runtime")
					out.Message = "refused: local runtime ownership could not be inspected"
					out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: inspectErr.Error()}
					env.Failed = append(env.Failed, out)
					return &env, result.Preflight(errors.New("worker reconcile refused one or more unsafe repairs"))
				}
				return &env, result.Preflight(inspectErr)
			}
			if inspection.state == workerPaneConflict || inspection.state == workerPaneAmbiguous {
				if in.Command.Name == "reconcile" {
					plan := reconcilePlans[row.Thread]
					plan.localState = string(inspection.state)
					reconcilePlans[row.Thread] = plan
					out := workerReconcileOutcome(row, plan, "refuse_ambiguous_runtime")
					out.Message = "refused: local runtime ownership is " + string(inspection.state)
					out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: out.Message}
					env.Failed = append(env.Failed, out)
					return &env, result.Preflight(errors.New("worker reconcile refused one or more unsafe repairs"))
				}
				return &env, result.Preflight(fmt.Errorf("worker %s/%s has %s tmux identity", row.Workspace, row.Window, inspection.state))
			}
			inspections[row.Thread] = inspection
			if in.Command.Name == "reconcile" {
				plan := reconcilePlans[row.Thread]
				plan.localState = string(inspection.state)
				reconcilePlans[row.Thread] = plan
			}
		}
	}
	if in.Command.Name == "reconcile" {
		for _, row := range rows {
			plan := reconcilePlans[row.Thread]
			if plan.state != result.WorkdirMissing || inspections[row.Thread].state == workerPaneAbsent {
				continue
			}
			out := workerReconcileOutcome(row, plan, "refuse_live_runtime")
			out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: "missing worker workdir still has local runtime ownership"}
			env.Failed = append(env.Failed, out)
		}
		if len(env.Failed) > 0 {
			return &env, result.Preflight(errors.New("worker reconcile refused one or more unsafe repairs"))
		}
	}
	if lifecycleCommandStopsWorker(in.Command.Name) {
		panes := make([]tmux.WindowPane, 0, len(rows))
		for _, row := range rows {
			if inspection := inspections[row.Thread]; inspection.state == workerPaneExact {
				panes = append(panes, inspection.pane)
			}
		}
		if err := preflightLifecycleExecutor("worker "+in.Command.Name, panes); err != nil {
			return &env, result.Preflight(err)
		}
	}
	var doctorStatuses map[string]threadStatus
	var doctorStatusErr error
	if in.Command.Name == "doctor" && !in.Options.DryRun {
		if in.Selectors.All {
			doctorStatuses, doctorStatusErr = threadArchiveStatuses(rows)
		} else {
			doctorStatuses, doctorStatusErr = exactThreadArchiveStatuses(rows)
		}
	}
	for _, thread := range shelfOnly {
		resource, _ := result.WorkerResource(thread)
		out := result.Outcome{Resource: resource, Action: "remove"}
		if in.Options.DryRun {
			env.Planned = append(env.Planned, out)
			continue
		}
		changed, removeErr := config.RemoveShelf(dir.ShelvesPath(), thread)
		if removeErr != nil {
			out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: removeErr.Error()}
			env.Failed = append(env.Failed, out)
		} else if changed {
			env.Successful = append(env.Successful, out)
		} else {
			out.Message = "already in desired state"
			env.Skipped = append(env.Skipped, out)
		}
	}
	for _, row := range rows {
		out := workerOutcome(row, in.Command.Name, "")
		if in.Command.Name == "teardown" {
			out.Teardown = newTeardownDetails()
		}
		if in.Command.Name == "reconcile" {
			plan := reconcilePlans[row.Thread]
			if plan.state == result.WorkdirMissing {
				out = workerReconcileOutcome(row, plan, "remove_stale_registration")
				out.Message = "remove stale workers.tsv registration for proven missing canonical workdir"
				if in.Options.DryRun {
					env.Planned = append(env.Planned, out)
					continue
				}
				reconcileChanged, reconcileErr := revalidateAndRemoveWorkerBinding(dir, row, plan)
				if reconcileErr != nil {
					out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: reconcileErr.Error()}
					env.Failed = append(env.Failed, out)
					continue
				}
				if reconcileChanged {
					env.Successful = append(env.Successful, out)
				} else {
					out.Message = "already in desired state; workers.tsv binding is absent"
					env.Skipped = append(env.Skipped, out)
				}
				continue
			}
			out.Reconcile = workerReconcileDetails(plan, "synchronize_shelf_intent")
			out.Worker = workerPlacementDetails(row, string(inspections[row.Thread].state))
			out.Worker.Workdir = plan.workdir
			out.Worker.WorkdirState = plan.state
		}
		if (in.Command.Name == "launch" || in.Command.Name == "restart") && shelved[row.Thread] {
			out.Message = "worker is locally shelved"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if in.Command.Name == "launch" && inspections[row.Thread].state == workerPaneExact {
			out.Message = "local client already running; assignment=" + workerAssignmentState(row)
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if in.Command.Name == "restart" && inspections[row.Thread].state == workerPaneAbsent {
			out.Message = "worker is not running"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if (in.Command.Name == "park" && inspections[row.Thread].state == workerPaneAbsent) || (in.Command.Name == "unshelve" && !shelved[row.Thread]) {
			out.Message = "already in desired state"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if in.Options.DryRun {
			env.Planned = append(env.Planned, out)
			continue
		}
		var changed bool
		err = nil
		switch in.Command.Name {
		case "unpin":
			_, err = config.RemoveShelf(dir.ShelvesPath(), row.Thread)
			if err == nil {
				changed, err = config.Remove(dir.WorkersPath(), row.Workspace, row.Window)
			}
		case "shelve":
			changed, err = config.StoreShelf(dir.ShelvesPath(), row.Thread)
			if err == nil {
				err = archiveAmpThread(row.Thread)
			}
			if err == nil && inspections[row.Thread].state == workerPaneExact {
				err = revalidateWorkerBeforeMutation(row, inspections[row.Thread])
				if err == nil {
					err = tmux.Runner{}.KillWindow(inspections[row.Thread].pane.WindowID)
				}
				changed = true
			}
		case "unshelve":
			if !shelved[row.Thread] {
				out.Message = "already unshelved"
				env.Skipped = append(env.Skipped, out)
				continue
			}
			err = unarchiveAmpThread(row.Thread)
			if err == nil {
				changed, err = config.RemoveShelf(dir.ShelvesPath(), row.Thread)
			}
		case "remove":
			if inspections[row.Thread].state == workerPaneExact {
				err = revalidateWorkerBeforeMutation(row, inspections[row.Thread])
				if err == nil {
					err = tmux.Runner{}.KillWindow(inspections[row.Thread].pane.WindowID)
				}
			}
			if err == nil {
				_, err = config.RemoveShelf(dir.ShelvesPath(), row.Thread)
			}
			if err == nil {
				changed, err = config.Remove(dir.WorkersPath(), row.Workspace, row.Window)
			}
		case "park":
			if inspections[row.Thread].state == workerPaneExact {
				err = revalidateWorkerBeforeMutation(row, inspections[row.Thread])
				if err == nil {
					err = tmux.Runner{}.KillWindow(inspections[row.Thread].pane.WindowID)
				}
				changed = err == nil
			}
		case "restart":
			if inspections[row.Thread].state == workerPaneExact {
				err = revalidateWorkerBeforeMutation(row, inspections[row.Thread])
				if err == nil {
					err = tmux.Runner{}.KillWindow(inspections[row.Thread].pane.WindowID)
				}
			}
			if err == nil {
				err = createWorkerPane(row)
			}
			if err == nil {
				var after workerInspection
				after, err = inspectWorker(row)
				if err == nil && after.state != workerPaneExact {
					err = fmt.Errorf("restarted worker tmux identity is %s", after.state)
				}
			}
			changed = err == nil
		case "launch":
			err = createWorkerPane(row)
			if err == nil {
				var after workerInspection
				after, err = inspectWorker(row)
				if err == nil && after.state != workerPaneExact {
					err = fmt.Errorf("launched worker tmux identity is %s", after.state)
				}
			}
			changed = err == nil
		case "teardown":
			err = archiveAmpThread(row.Thread)
			if err != nil {
				setTeardownArtifact(out.Teardown, "remote_thread_archive", "failed", err.Error())
			} else {
				setTeardownArtifact(out.Teardown, "remote_thread_archive", "completed", "")
				var shelfRemoved bool
				shelfRemoved, err = config.RemoveShelf(dir.ShelvesPath(), row.Thread)
				if err != nil {
					setTeardownArtifact(out.Teardown, "shelf_intent", "failed", err.Error())
				} else {
					if shelfRemoved {
						setTeardownArtifact(out.Teardown, "shelf_intent", "removed", "")
					} else {
						setTeardownArtifact(out.Teardown, "shelf_intent", "already_absent", "shelf intent was already absent")
					}
				}
			}
			if err == nil && inspections[row.Thread].state == workerPaneExact {
				err = revalidateWorkerBeforeMutation(row, inspections[row.Thread])
				if err != nil {
					setTeardownArtifact(out.Teardown, "local_client", "failed", err.Error())
				} else {
					err = tmux.Runner{}.KillWindow(inspections[row.Thread].pane.WindowID)
					if err != nil {
						setTeardownArtifact(out.Teardown, "local_client", "failed", err.Error())
					} else {
						setTeardownArtifact(out.Teardown, "local_client", "stopped", "")
					}
				}
			} else if err == nil {
				setTeardownArtifact(out.Teardown, "local_client", "already_stopped", "verified local client was already absent")
			}
			if err == nil {
				var workerRemoved bool
				workerRemoved, err = config.Remove(dir.WorkersPath(), row.Workspace, row.Window)
				if err != nil {
					setTeardownArtifact(out.Teardown, "worker_configuration", "failed", err.Error())
				} else if workerRemoved {
					setTeardownArtifact(out.Teardown, "worker_configuration", "removed", "")
				} else {
					setTeardownArtifact(out.Teardown, "worker_configuration", "already_absent", "worker configuration was already absent")
				}
			}
			changed = err == nil
		case "doctor":
			remote := "unknown"
			if doctorStatusErr == nil {
				remote = string(doctorStatuses[canonicalThreadID(row.Thread)])
			}
			canonicalWorkdir, workdirState := workerWorkdirState(row.Workdir)
			out.Worker = workerPlacementDetails(row, string(inspections[row.Thread].state))
			if canonicalWorkdir != "" {
				out.Worker.Workdir = canonicalWorkdir
			}
			out.Worker.WorkdirState = workdirState
			out.Message = fmt.Sprintf("local=%s remote=%s workdir=%s intent=%t assignment=%s native_executor=%s native_runner_id=%s execution_affinity=%s", inspections[row.Thread].state, remote, workdirState, shelved[row.Thread], workerAssignmentState(row), unknownNativePlacement, unknownNativePlacement, unknownNativePlacement)
			env.Successful = append(env.Successful, out)
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "%s\t%s\n", row.Thread, out.Message)
			}
			continue
		case "reconcile":
			if shelved[row.Thread] {
				err = archiveAmpThread(row.Thread)
			} else {
				err = unarchiveAmpThread(row.Thread)
			}
			changed = err == nil
		}
		if err != nil {
			out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: err.Error()}
			if in.Command.Name == "teardown" {
				markTeardownDownstreamUnattempted(out.Teardown)
				out.Message = teardownMessage(out.Teardown)
				if !in.Options.JSON {
					fmt.Fprintf(a.stdout, "%s\t%s; error=%s\n", row.Thread, out.Message, err)
				}
			}
			env.Failed = append(env.Failed, out)
			continue
		}
		if in.Command.Name == "teardown" && inspections[row.Thread].state == workerPaneAbsent {
			out.Message = teardownMessage(out.Teardown)
			env.Skipped = append(env.Skipped, out)
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "%s\t%s\n", row.Thread, out.Message)
			}
			continue
		}
		if changed {
			if in.Command.Name == "teardown" {
				out.Message = teardownMessage(out.Teardown)
				if !in.Options.JSON {
					fmt.Fprintf(a.stdout, "%s\t%s\n", row.Thread, out.Message)
				}
			}
			env.Successful = append(env.Successful, out)
		} else {
			out.Message = "already in desired state"
			env.Skipped = append(env.Skipped, out)
		}
	}
	if doctorStatusErr != nil {
		env.Failed = append(env.Failed, result.Outcome{
			Resource: result.CommandResource(),
			Action:   "doctor",
			Error:    &result.Failure{Kind: result.ErrorRuntime, Message: doctorStatusErr.Error()},
		})
	}
	if len(env.Failed) > 0 {
		return &env, result.Runtime(errors.New("one or more worker operations failed"))
	}
	return &env, nil
}

const (
	nativeAdoptionReceiptSource = "amp_native_create_thread"
	unknownNativePlacement      = "unknown"
)

func (a app) workerAdopt(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.Thread == "" || s.Workspace == "" || s.Window == "" || s.Workdir == "" {
		return env, result.Request(errors.New("worker adopt requires --thread, --workspace, --window, and --workdir"))
	}
	workdir, err := config.CanonicalWorkdir(s.Workdir)
	if err != nil {
		return env, result.Preflight(err)
	}
	row := config.Row{Workspace: s.Workspace, Window: s.Window, Workdir: workdir, Thread: s.Thread}
	stat, err := os.Stat(row.Workdir)
	if err != nil || !stat.IsDir() {
		return env, result.Preflight(fmt.Errorf("missing workdir: %s", row.Workdir))
	}
	request := strings.Join([]string{row.Thread, row.Workspace, row.Window, row.Workdir, s.Group}, "\x00")
	sum := sha256.Sum256([]byte(request))
	requestHash := hex.EncodeToString(sum[:])
	operationKey := "worker-adopt:" + row.Thread
	operation, operationFound, err := config.LoadOperation(dir.OperationsPath(), operationKey)
	if err != nil {
		return env, result.Preflight(err)
	}
	if operationFound && (operation.Kind != "worker-adopt" || operation.RequestHash != requestHash || operation.Resource.Thread != row.Thread) {
		return env, result.Preflight(fmt.Errorf("thread %s already has a native adoption request bound to different workspace, window, workdir, or group intent", row.Thread))
	}
	operations, err := config.LoadOperationsReadOnly(dir.OperationsPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, existing := range operations {
		if existing.Key == operationKey || existing.State == config.OperationSucceeded || existing.State == config.OperationFailed {
			continue
		}
		matchesThread := existing.Resource.Thread == row.Thread
		if existing.ThreadAdoption != nil {
			matchesThread = matchesThread || existing.ThreadAdoption.ProvisionedThread == row.Thread || existing.ThreadAdoption.ReceivingThread == row.Thread
		}
		if matchesThread {
			return env, result.Preflight(fmt.Errorf("thread %s is still owned by immutable %s operation %q in state %s; inspect this read-only evidence with `amux worker doctor --all`; it cannot be retried or reconciled", row.Thread, existing.Kind, existing.Key, existing.State))
		}
	}
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	runners, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	exactRow := false
	for _, existing := range rows {
		if workerRowsEquivalent(existing, row) {
			exactRow = true
			continue
		}
		existingWorkdir, _ := config.CanonicalWorkdir(existing.Workdir)
		if existing.Workspace == row.Workspace && existing.Window == row.Window {
			return env, result.Preflight(fmt.Errorf("worker window %s/%s is already configured for thread %s", row.Workspace, row.Window, existing.Thread))
		}
		if existing.Thread == row.Thread {
			return env, result.Preflight(fmt.Errorf("thread %s is already configured as %s/%s", row.Thread, existing.Workspace, existing.Window))
		}
		if existingWorkdir == row.Workdir {
			return env, result.Preflight(fmt.Errorf("workdir %s is already owned by worker %s/%s", row.Workdir, existing.Workspace, existing.Window))
		}
	}
	for _, runner := range runners {
		if runner.Workdir == row.Workdir {
			return env, result.Preflight(fmt.Errorf("workdir %s is already owned by amux Runner workspace %s", row.Workdir, runner.Workspace))
		}
	}
	shelves, err := config.LoadShelvesReadOnly(dir.ShelvesPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, thread := range shelves {
		if thread == row.Thread {
			return env, result.Preflight(fmt.Errorf("thread %s is locally shelved; unshelve it explicitly before native adoption", row.Thread))
		}
	}
	inspection, err := verifyAdoptionThreadAndTmux(row)
	if err != nil {
		return env, result.Preflight(fmt.Errorf("verify exact Amp thread before adoption: %w", err))
	}
	memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	groupExact := s.Group == ""
	groupNeedsEnsure := false
	var membership config.GroupMembership
	var groupAmpPath string
	if s.Group != "" {
		membership = config.GroupMembership{Group: s.Group, Thread: row.Thread, Role: config.GroupMember}
		index := membershipIndex(memberships, s.Group, row.Thread)
		groupExact = index >= 0
		if groupExact {
			membership = memberships[index]
			if membership.Role != config.GroupMember {
				return env, result.Preflight(fmt.Errorf("thread %s already has %s role in group %s; worker adopt requires exact member intent", row.Thread, membership.Role, s.Group))
			}
		}
		groupNeedsEnsure = membership.Role == config.GroupMember && (!groupExact || inspection.state == workerPaneAbsent)
		if groupNeedsEnsure {
			groupAmpPath, err = preflightGroupAmp()
			if err != nil {
				return env, result.Preflight(err)
			}
		}
	}
	localExact := exactRow && inspection.state == workerPaneExact && groupExact
	if operationFound && operation.State == config.OperationSucceeded {
		if !localExact {
			return env, result.Preflight(fmt.Errorf("native adoption for thread %s already succeeded but local state changed; use worker launch or reconcile instead of replaying adoption", row.Thread))
		}
		out := adoptionOutcome(row, "adopt", "adopted")
		out.Message = "exact native-created thread already adopted"
		env.Skipped = append(env.Skipped, out)
		return env, nil
	}
	if in.Options.DryRun {
		operationOut := adoptionOutcome(row, "bind-adoption-request", "request_intent")
		if operationFound {
			operationOut.Message = "native adoption request already durably bound"
			env.Skipped = append(env.Skipped, operationOut)
		} else {
			operationOut.Message = "would durably bind exact workspace, window, workdir, thread, and optional group before mutation"
			env.Planned = append(env.Planned, operationOut)
		}
		workerOut := adoptionOutcome(row, "persist-worker", "catalog")
		if exactRow {
			workerOut.Message = "exact worker catalog intent already persisted"
			env.Skipped = append(env.Skipped, workerOut)
		} else {
			workerOut.Message = "would persist exact worker catalog intent"
			env.Planned = append(env.Planned, workerOut)
		}
		if s.Group != "" {
			groupOut := groupOutcome(membership, "persist-group")
			if groupExact {
				groupOut.Message = "group intent already persisted"
				env.Skipped = append(env.Skipped, groupOut)
			} else {
				groupOut.Message = "would persist exact group member intent before external mutation"
				env.Planned = append(env.Planned, groupOut)
			}
			if groupNeedsEnsure {
				labelOut := groupOutcome(membership, "ensure-label")
				labelOut.Group.ExternalSync = "additive_ensure_planned"
				labelOut.Message = "would add-only ensure the exact member label"
				env.Planned = append(env.Planned, labelOut)
			}
		}
		clientOut := adoptionOutcome(row, "create-client", string(inspection.state))
		if inspection.state == workerPaneExact {
			clientOut.Message = "exact tmux client already exists"
			env.Skipped = append(env.Skipped, clientOut)
		} else {
			clientOut.Message = "would create and post-verify local tmux client; no message delivery"
			env.Planned = append(env.Planned, clientOut)
		}
		return env, nil
	}
	if !operationFound {
		now := time.Now().UTC()
		operation = config.OperationRecord{Key: operationKey, Kind: "worker-adopt", RequestHash: requestHash, State: config.OperationStarted, Resource: config.OperationResource{Kind: "worker", Thread: row.Thread}, CreatedAt: now, UpdatedAt: now}
		if _, err := config.StoreOperation(dir.OperationsPath(), operation); err != nil {
			return env, result.Runtime(fmt.Errorf("persist exact native adoption request: %w", err))
		}
		operationOut := adoptionOutcome(row, "bind-adoption-request", "request_intent")
		operationOut.Message = "durably bound exact native adoption request before mutation"
		env.Successful = append(env.Successful, operationOut)
	}
	if !exactRow {
		if _, err := config.Store(dir.WorkersPath(), row); err != nil {
			return env, result.Runtime(fmt.Errorf("persist worker adoption intent: %w", err))
		}
		workerOut := adoptionOutcome(row, "persist-worker", "catalog")
		workerOut.Message = "persisted exact worker catalog intent"
		env.Successful = append(env.Successful, workerOut)
	}
	if s.Group != "" && !groupExact {
		updated := append(append([]config.GroupMembership(nil), memberships...), membership)
		if err := config.WriteGroups(dir.GroupsPath(), updated); err != nil {
			return env, result.Runtime(fmt.Errorf("persist adoption group intent: %w", err))
		}
		groupOut := groupOutcome(membership, "persist-group")
		groupOut.Message = "persisted exact group member intent"
		env.Successful = append(env.Successful, groupOut)
	}
	revalidated, err := verifyAdoptionThreadAndTmux(row)
	if err != nil || revalidated.state != inspection.state || revalidated.pane.WindowID != inspection.pane.WindowID {
		if err == nil {
			err = errors.New("active thread or tmux ownership changed after durable intent was persisted")
		}
		return env, result.Runtime(fmt.Errorf("revalidate native adoption before side effects: %w", err))
	}
	if groupNeedsEnsure {
		groupOut := groupOutcome(membership, "attach-group")
		if _, err := a.ensureGroupLabel(env, groupOut, groupAmpPath, membership, in.Options.JSON); err != nil {
			return env, result.Runtime(fmt.Errorf("worker adopted and group intent retained, but label synchronization failed: %w", err))
		}
	}
	if inspection.state == workerPaneAbsent {
		if err := createWorkerPane(row); err != nil {
			return env, result.Runtime(fmt.Errorf("create adopted worker tmux client after persisting intent: %w", err))
		}
		created, err := verifyAdoptionThreadAndTmux(row)
		if err != nil || created.state != workerPaneExact {
			if err == nil {
				err = fmt.Errorf("created worker tmux identity is %s", created.state)
			}
			return env, result.Runtime(fmt.Errorf("post-verify adopted worker tmux client: %w", err))
		}
		clientOut := adoptionOutcome(row, "create-client", "exact")
		clientOut.Message = "created and verified exact local tmux client without message delivery"
		env.Successful = append(env.Successful, clientOut)
	}
	operation.State = config.OperationSucceeded
	operation.UpdatedAt = time.Now().UTC()
	if _, err := config.StoreOperation(dir.OperationsPath(), operation); err != nil {
		return env, result.Runtime(fmt.Errorf("complete native adoption request: %w", err))
	}
	out := adoptionOutcome(row, "adopt", "adopted")
	out.Message = "adopted exact native-created thread without message delivery"
	env.Successful = append(env.Successful, out)
	return env, nil
}

func adoptionOutcome(row config.Row, action, localState string) result.Outcome {
	out := workerOutcome(row, action, "")
	out.Worker = workerPlacementDetails(row, localState)
	out.Worker.ReceiptSource = nativeAdoptionReceiptSource
	return out
}

func workerPlacementDetails(row config.Row, localState string) *result.WorkerDetails {
	return &result.WorkerDetails{
		Workspace:         row.Workspace,
		Window:            row.Window,
		Workdir:           row.Workdir,
		LocalState:        localState,
		AssignmentState:   workerAssignmentState(row),
		NativeExecutor:    unknownNativePlacement,
		NativeRunnerID:    unknownNativePlacement,
		ExecutionAffinity: unknownNativePlacement,
	}
}

func workerAssignmentState(row config.Row) string {
	if row.AssignmentState == config.WorkerAssignmentRetainedIndeterminate {
		return string(row.AssignmentState)
	}
	return "unknown"
}

func workerWorkdirState(workdir string) (string, result.WorkdirState) {
	expanded := config.ExpandHome(workdir)
	if !filepath.IsAbs(expanded) {
		return "", result.WorkdirAmbiguous
	}
	canonical, err := config.CanonicalWorkdir(workdir)
	if err != nil {
		return "", result.WorkdirUnknown
	}
	_, lstatErr := os.Lstat(canonical)
	if os.IsNotExist(lstatErr) {
		return canonical, result.WorkdirMissing
	}
	if lstatErr != nil {
		return canonical, result.WorkdirUnreadable
	}
	stat, err := os.Stat(canonical)
	if os.IsNotExist(err) {
		return canonical, result.WorkdirUnreadable
	}
	if err != nil {
		return canonical, result.WorkdirUnreadable
	}
	if !stat.IsDir() {
		return canonical, result.WorkdirNotDirectory
	}
	return canonical, result.WorkdirPresent
}

type workerReconcilePlan struct {
	workdir         string
	state           result.WorkdirState
	localState      string
	openObligations []string
}

func planWorkerReconcile(dir config.Directory, rows []config.Row, env *result.Envelope) (map[string]workerReconcilePlan, error) {
	reports, err := config.LoadReports(dir.ReportsPath())
	if err != nil {
		env.Failed = append(env.Failed, result.Outcome{
			Resource:  result.ConfigResource(dir.ReportsPath()),
			Action:    "reconcile",
			Message:   "refused: reports.json could not be read; open obligations are unknown",
			Reconcile: &result.ReconcileDetails{Authority: config.WorkersFile, WorkdirState: result.WorkdirUnknown, Decision: "refuse_unknown_obligations"},
			Error:     &result.Failure{Kind: result.ErrorPreflight, Message: err.Error()},
		})
		return nil, err
	}
	reportsByThread := make(map[string][]config.ReportRecord)
	for _, report := range reports {
		reportsByThread[report.MemberThread] = append(reportsByThread[report.MemberThread], report)
	}
	plans := make(map[string]workerReconcilePlan, len(rows))
	for _, row := range rows {
		workdir, state := workerWorkdirState(row.Workdir)
		plan := workerReconcilePlan{workdir: workdir, state: state, localState: "unknown"}
		for _, report := range reportsByThread[row.Thread] {
			if report.Status == config.ReportBlocked && report.AuthorizedAt.IsZero() {
				plan.openObligations = append(plan.openObligations, report.ReportID)
			}
		}
		sort.Strings(plan.openObligations)
		plans[row.Thread] = plan
		decision, refusal := "", ""
		switch state {
		case result.WorkdirPresent:
			continue
		case result.WorkdirMissing:
			if len(plan.openObligations) == 0 {
				continue
			}
			decision = "refuse_open_obligation"
			refusal = "blocked report without finish authorization"
		case result.WorkdirNotDirectory:
			decision = "refuse_not_a_directory"
			refusal = "registry workdir is not a directory"
		case result.WorkdirUnreadable:
			decision = "refuse_unreadable"
			refusal = "registry workdir is unreadable"
		case result.WorkdirAmbiguous:
			decision = "refuse_relative_ambiguity"
			refusal = "registry workdir is relative and has no stable machine identity"
		default:
			decision = "refuse_unknown_state"
			refusal = "registry workdir state is unknown"
		}
		out := workerReconcileOutcome(row, plan, decision)
		out.Message = "refused: " + refusal
		out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: refusal}
		env.Failed = append(env.Failed, out)
	}
	if len(env.Failed) > 0 {
		return plans, errors.New("worker reconcile refused one or more unsafe repairs")
	}
	return plans, nil
}

func workerReconcileUnregisteredWorkdir(in invocation, env *result.Envelope) (*result.Envelope, error) {
	workdir, state := workerWorkdirState(in.Selectors.Workdir)
	resource := result.ResourceID{Kind: "worker_workdir", Workdir: workdir}
	details := &result.ReconcileDetails{Authority: config.WorkersFile, RegistryBinding: false, Workdir: workdir, WorkdirState: state}
	out := result.Outcome{Resource: resource, Action: "reconcile", Reconcile: details}
	if state == result.WorkdirPresent || state == result.WorkdirMissing {
		details.Decision = "preserve_unregistered_directory"
		out.Message = "refused filesystem cleanup: no workers.tsv row binds a thread to this canonical workdir"
		env.Skipped = append(env.Skipped, out)
		return env, nil
	}
	details.Decision = "refuse_unproven_workdir_state"
	out.Message = "refused: unregistered workdir state is not safely actionable"
	out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: out.Message}
	env.Failed = append(env.Failed, out)
	return env, result.Preflight(errors.New(out.Message))
}

func workerReconcileOutcome(row config.Row, plan workerReconcilePlan, decision string) result.Outcome {
	out := workerOutcome(row, "reconcile", "")
	out.Worker = workerPlacementDetails(row, plan.localState)
	if plan.workdir != "" {
		out.Worker.Workdir = plan.workdir
	}
	out.Worker.WorkdirState = plan.state
	out.Reconcile = workerReconcileDetails(plan, decision)
	return out
}

func revalidateAndRemoveWorkerBinding(dir config.Directory, row config.Row, plan workerReconcilePlan) (bool, error) {
	workdir, state := workerWorkdirState(row.Workdir)
	if state != result.WorkdirMissing || workdir != plan.workdir {
		return false, fmt.Errorf("worker reconcile precondition changed: canonical workdir %s is %s", plan.workdir, state)
	}
	inspection, err := inspectWorker(row)
	if err != nil {
		return false, fmt.Errorf("worker reconcile precondition changed: inspect local runtime: %w", err)
	}
	if inspection.state != workerPaneAbsent {
		return false, fmt.Errorf("worker reconcile precondition changed: local runtime ownership is %s", inspection.state)
	}
	reports, err := config.LoadReports(dir.ReportsPath())
	if err != nil {
		return false, fmt.Errorf("worker reconcile precondition changed: read reports: %w", err)
	}
	for _, report := range reports {
		if report.MemberThread == row.Thread && report.Status == config.ReportBlocked && report.AuthorizedAt.IsZero() {
			return false, fmt.Errorf("worker reconcile precondition changed: blocked report %q is never authorized", report.ReportID)
		}
	}
	removed, err := config.RemoveRows(dir.WorkersPath(), func(candidate config.Row) bool {
		candidateWorkdir, candidateErr := config.CanonicalWorkdir(candidate.Workdir)
		return candidateErr == nil && candidate.Workspace == row.Workspace && candidate.Window == row.Window && candidate.Thread == row.Thread && candidateWorkdir == plan.workdir
	})
	if err != nil {
		return false, err
	}
	if removed != 1 {
		return false, errors.New("worker reconcile precondition changed: exact workers.tsv binding no longer exists")
	}
	return true, nil
}

func workerReconcileDetails(plan workerReconcilePlan, decision string) *result.ReconcileDetails {
	return &result.ReconcileDetails{
		Authority:       config.WorkersFile,
		RegistryBinding: true,
		Workdir:         plan.workdir,
		WorkdirState:    plan.state,
		Decision:        decision,
		OpenObligations: append([]string(nil), plan.openObligations...),
	}
}

func verifyAdoptionThreadAndTmux(row config.Row) (workerInspection, error) {
	statuses, err := threadArchiveStatuses([]config.Row{row})
	if err != nil {
		return workerInspection{}, err
	}
	if status := statuses[row.Thread]; status != threadStatusActive {
		return workerInspection{}, fmt.Errorf("thread %s is %s; worker adopt requires the exact active native-created thread", row.Thread, status)
	}
	inspection, err := inspectWorker(row)
	if err != nil {
		return workerInspection{}, err
	}
	if inspection.state != workerPaneAbsent && inspection.state != workerPaneExact {
		return workerInspection{}, fmt.Errorf("requested worker %s/%s has %s tmux identity", row.Workspace, row.Window, inspection.state)
	}
	threadPanes, err := managedThreadPanes(row.Thread)
	if err != nil {
		return workerInspection{}, fmt.Errorf("inspect existing tmux ownership for thread %s: %w", row.Thread, err)
	}
	if inspection.state == workerPaneExact && len(threadPanes) == 0 || len(threadPanes) > 1 || len(threadPanes) == 1 && (inspection.state != workerPaneExact || threadPanes[0].WindowID != inspection.pane.WindowID) {
		return workerInspection{}, fmt.Errorf("thread %s has inconsistent tmux ownership outside requested worker %s/%s", row.Thread, row.Workspace, row.Window)
	}
	return inspection, nil
}

func selectWorkerRows(rows []config.Row, s selectors) []config.Row {
	selected := make([]config.Row, 0, len(rows))
	for _, r := range rows {
		workdirMatches := true
		if s.Workdir != "" {
			selectedWorkdir, selectedErr := config.CanonicalWorkdir(s.Workdir)
			rowWorkdir, rowErr := config.CanonicalWorkdir(r.Workdir)
			workdirMatches = selectedErr == nil && rowErr == nil && selectedWorkdir == rowWorkdir
		}
		if s.Thread != "" && r.Thread != s.Thread || s.Workspace != "" && r.Workspace != s.Workspace || s.Window != "" && r.Window != s.Window || !workdirMatches {
			continue
		}
		selected = append(selected, r)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Workspace == selected[j].Workspace {
			return selected[i].Window < selected[j].Window
		}
		return selected[i].Workspace < selected[j].Workspace
	})
	return selected
}
func workerOutcome(r config.Row, action, message string) result.Outcome {
	id, _ := result.WorkerResource(r.Thread)
	return result.Outcome{Resource: id, Action: action, Message: message}
}

func newTeardownDetails() *result.TeardownDetails {
	details := &result.TeardownDetails{Artifacts: make([]result.TeardownArtifactDetails, 0, 8)}
	for _, artifact := range []string{"remote_thread_archive", "shelf_intent", "local_client", "worker_configuration"} {
		details.Artifacts = append(details.Artifacts, result.TeardownArtifactDetails{
			Artifact: artifact,
			Outcome:  "unattempted",
			Reason:   "teardown artifact has not run",
		})
	}
	for _, artifact := range []struct {
		name   string
		reason string
	}{
		{"worktree_directory", "amux teardown does not own worktree directory cleanup"},
		{"branch_refs", "amux teardown does not own Git branch refs"},
		{"stashes", "amux teardown does not own Git stashes"},
		{"external_project_records", "amux teardown does not own external project registries"},
	} {
		details.Artifacts = append(details.Artifacts, result.TeardownArtifactDetails{Artifact: artifact.name, Outcome: "not_owned", Reason: artifact.reason})
	}
	return details
}

func setTeardownArtifact(details *result.TeardownDetails, artifact, outcome, reason string) {
	for i := range details.Artifacts {
		if details.Artifacts[i].Artifact == artifact {
			details.Artifacts[i].Outcome = outcome
			details.Artifacts[i].Reason = reason
			return
		}
	}
}

func markTeardownDownstreamUnattempted(details *result.TeardownDetails) {
	failedArtifact := ""
	for i := range details.Artifacts {
		artifact := &details.Artifacts[i]
		if artifact.Outcome == "failed" {
			failedArtifact = artifact.Artifact
			continue
		}
		if failedArtifact != "" && artifact.Outcome == "unattempted" {
			artifact.Reason = "not attempted because " + failedArtifact + " failed"
		}
	}
}

func teardownMessage(details *result.TeardownDetails) string {
	parts := make([]string, 0, len(details.Artifacts))
	for _, artifact := range details.Artifacts {
		part := artifact.Artifact + "=" + artifact.Outcome
		if artifact.Reason != "" {
			part += " (" + artifact.Reason + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func workerRowsEquivalent(left, right config.Row) bool {
	if left.Workspace != right.Workspace || left.Window != right.Window {
		return false
	}
	leftThread, leftThreadErr := config.CanonicalThreadID(left.Thread)
	rightThread, rightThreadErr := config.CanonicalThreadID(right.Thread)
	leftWorkdir, leftWorkdirErr := config.CanonicalWorkdir(left.Workdir)
	rightWorkdir, rightWorkdirErr := config.CanonicalWorkdir(right.Workdir)
	return leftThreadErr == nil && rightThreadErr == nil && leftWorkdirErr == nil && rightWorkdirErr == nil && leftThread == rightThread && leftWorkdir == rightWorkdir
}

func (a app) workerPin(in invocation, dir config.Directory, existing []config.Row, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.Thread == "" || s.Workspace == "" || s.Window == "" || s.Workdir == "" {
		return env, result.Request(errors.New("worker pin requires --thread, --workspace, --window, and --workdir"))
	}
	r := config.Row{Workspace: s.Workspace, Window: s.Window, Workdir: s.Workdir, Thread: s.Thread}
	out := workerOutcome(r, "pin", "")
	if len(existing) == 1 && workerRowsEquivalent(existing[0], r) {
		out.Message = "already pinned"
		env.Skipped = append(env.Skipped, out)
		return env, nil
	}
	if in.Options.DryRun {
		env.Planned = append(env.Planned, out)
		return env, nil
	}
	_, err := config.Store(dir.WorkersPath(), r)
	if err != nil {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, out)
	return env, nil
}

type workerPaneState string

const (
	workerPaneAbsent    workerPaneState = "absent"
	workerPaneExact     workerPaneState = "exact"
	workerPaneConflict  workerPaneState = "conflict"
	workerPaneAmbiguous workerPaneState = "ambiguous"
)

type workerInspection struct {
	state workerPaneState
	pane  tmux.WindowPane
}

func workerCommandNeedsTmux(name string) bool {
	switch name {
	case "launch", "park", "restart", "remove", "shelve", "teardown", "doctor", "reconcile":
		return true
	}
	return false
}

func inspectWorker(row config.Row) (workerInspection, error) {
	runner := tmux.Runner{}
	exists, err := runner.SessionExists(row.Workspace)
	if err != nil {
		return workerInspection{}, fmt.Errorf("inspect tmux worker %s/%s: %w", row.Workspace, row.Window, err)
	}
	if !exists {
		return workerInspection{state: workerPaneAbsent}, nil
	}
	panes, err := runner.WindowPanes(row.Workspace, row.Window)
	if err != nil {
		return workerInspection{}, fmt.Errorf("inspect tmux worker %s/%s: %w", row.Workspace, row.Window, err)
	}
	if len(panes) == 0 {
		return workerInspection{state: workerPaneAbsent}, nil
	}
	if len(panes) > 1 {
		return workerInspection{state: workerPaneAmbiguous}, nil
	}
	expected := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	actual := normalizedTmuxStartCommand(panes[0].StartCommand)
	if actual != expected {
		return workerInspection{state: workerPaneConflict, pane: panes[0]}, nil
	}
	return workerInspection{state: workerPaneExact, pane: panes[0]}, nil
}

func managedThreadPanes(thread string) ([]tmux.WindowPane, error) {
	panes, err := (tmux.Runner{}).AllWindowPanes()
	if err != nil {
		return nil, err
	}
	var matches []tmux.WindowPane
	suffix := "exec amp threads continue " + shellSingleQuote(thread)
	for _, pane := range panes {
		if strings.HasSuffix(normalizedTmuxStartCommand(pane.StartCommand), suffix) {
			matches = append(matches, pane)
		}
	}
	return matches, nil
}

func revalidateWorkerBeforeMutation(row config.Row, before workerInspection) error {
	after, err := inspectWorker(row)
	if err != nil {
		return err
	}
	if after.state != workerPaneExact || after.pane.WindowID != before.pane.WindowID {
		return fmt.Errorf("worker %s/%s changed after preflight", row.Workspace, row.Window)
	}
	return nil
}

func createWorkerPane(row config.Row) error {
	runner := tmux.Runner{}
	command := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	exists, err := runner.SessionExists(row.Workspace)
	if err != nil {
		return err
	}
	if exists {
		return runner.NewWindow(row.Workspace, row.Window, command)
	}
	return runner.NewSession(row.Workspace, row.Window, command)
}

func workerIdentityFromEnv() (teardownIdentity, error) {
	identity, err := teardownIdentityFromEnv()
	if err != nil {
		return identity, err
	}
	workdir := os.Getenv("AMUX_WORKDIR")
	if workdir == "" {
		return identity, errors.New("AMUX_WORKDIR is required")
	}
	_, err = config.CanonicalWorkdir(workdir)
	if err != nil {
		return identity, err
	}
	if identity.Workspace != identity.Session {
		return identity, errors.New("AMUX_WORKSPACE must equal AMUX_SESSION")
	}
	thread, err := config.CanonicalThreadID(identity.Thread)
	if err != nil {
		return identity, err
	}
	identity.Thread = thread
	return identity, nil
}
