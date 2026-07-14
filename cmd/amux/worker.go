package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

func (a app) executeWorker(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if in.Command.Name == "spawn" {
		return a.workerSpawn(in, dir, &env)
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
			env.Successful = append(env.Successful, out)
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\t%s\n", row.Workspace, row.Window, row.Workdir, row.Thread, out.Message)
			}
		}
		return &env, nil
	}
	if in.Command.Name == "pin" {
		return a.workerPin(in, dir, rows, &env)
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
		return &env, result.Preflight(errors.New("no configured worker matches the selector"))
	}
	inspections := make(map[string]workerInspection, len(rows))
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
				return &env, result.Preflight(inspectErr)
			}
			if inspection.state == workerPaneConflict || inspection.state == workerPaneAmbiguous {
				return &env, result.Preflight(fmt.Errorf("worker %s/%s has %s tmux identity", row.Workspace, row.Window, inspection.state))
			}
			if in.Command.Name == "teardown" && inspection.state == workerPaneAbsent {
				return &env, result.Preflight(fmt.Errorf("no live tmux window for thread %s matches restore row %s/%s", row.Thread, row.Workspace, row.Window))
			}
			inspections[row.Thread] = inspection
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
		if (in.Command.Name == "launch" || in.Command.Name == "restart") && shelved[row.Thread] {
			out.Message = "worker is locally shelved"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if in.Command.Name == "launch" && inspections[row.Thread].state == workerPaneExact {
			out.Message = "already running"
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
			if err == nil {
				_, err = config.RemoveShelf(dir.ShelvesPath(), row.Thread)
			}
			if err == nil {
				err = revalidateWorkerBeforeMutation(row, inspections[row.Thread])
				if err == nil {
					err = tmux.Runner{}.KillWindow(inspections[row.Thread].pane.WindowID)
				}
			}
			if err == nil {
				_, err = config.Remove(dir.WorkersPath(), row.Workspace, row.Window)
			}
			changed = err == nil
		case "doctor":
			status, statusErr := threadArchiveStatuses([]config.Row{row})
			err = statusErr
			if err == nil {
				out.Message = fmt.Sprintf("local=%s remote=%s intent=%t", inspections[row.Thread].state, status[row.Thread], shelved[row.Thread])
				env.Successful = append(env.Successful, out)
				continue
			}
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
			env.Failed = append(env.Failed, out)
			continue
		}
		if changed {
			env.Successful = append(env.Successful, out)
		} else {
			out.Message = "already in desired state"
			env.Skipped = append(env.Skipped, out)
		}
	}
	if len(env.Failed) > 0 {
		return &env, result.Runtime(errors.New("one or more worker operations failed"))
	}
	return &env, nil
}

func (a app) workerSpawn(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.IdempotencyKey == "" {
		return env, result.Request(errors.New("worker spawn requires --idempotency-key"))
	}
	if s.Window == "" || s.Workdir == "" {
		return env, result.Request(errors.New("worker spawn requires --window and --workdir"))
	}
	workspace := s.Workspace
	if workspace == "" {
		workspace = defaultWorkspace
	}
	message := s.Message
	if s.MessageFile != "" {
		file, err := os.Open(s.MessageFile)
		if err != nil {
			return env, result.Preflight(fmt.Errorf("read --message-file: %w", err))
		}
		message, err = readSpawnMessage(file)
		closeErr := file.Close()
		if err != nil {
			return env, result.Request(err)
		}
		if closeErr != nil {
			return env, result.Preflight(fmt.Errorf("close --message-file: %w", closeErr))
		}
	} else if s.MessageStdin {
		var err error
		message, err = readSpawnMessage(a.stdin)
		if err != nil {
			return env, result.Request(err)
		}
	}
	if message == "" {
		return env, result.Request(errors.New("worker spawn requires --message, --message-file, or --message-stdin"))
	}
	if len(message) > maxSpawnMessageBytes {
		return env, result.Request(fmt.Errorf("initial-message exceeds %d bytes", maxSpawnMessageBytes))
	}
	workdir, err := filepath.Abs(config.ExpandHome(s.Workdir))
	if err != nil {
		return env, result.Preflight(err)
	}
	stat, err := os.Stat(workdir)
	if err != nil || !stat.IsDir() {
		return env, result.Preflight(fmt.Errorf("missing workdir: %s", workdir))
	}
	s.Workdir = filepath.Clean(workdir)
	for name, value := range map[string]string{"workspace": workspace, "window": s.Window, "workdir": s.Workdir} {
		if err := config.ValidateField(name, value); err != nil {
			return env, result.Preflight(err)
		}
	}
	request := strings.Join([]string{workspace, s.Window, s.Workdir, s.Mode, message}, "\x00")
	sum := sha256.Sum256([]byte(request))
	hash := hex.EncodeToString(sum[:])
	existing, found, err := config.LoadOperation(dir.OperationsPath(), s.IdempotencyKey)
	if err != nil {
		return env, result.Preflight(err)
	}
	if found && existing.RequestHash != hash {
		return env, result.Preflight(fmt.Errorf("idempotency key %q is already bound to a different request", s.IdempotencyKey))
	}
	if found && existing.State == config.OperationSucceeded {
		r := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: existing.Resource.Thread}
		out := workerOutcome(r, "spawn", "idempotency key already succeeded")
		env.Skipped = append(env.Skipped, out)
		return env, nil
	}
	if found && existing.State != config.OperationStarted {
		return env, result.Preflight(fmt.Errorf("spawn operation is terminal in state %s; refusing new work", existing.State))
	}
	if in.Options.DryRun {
		message := "would create worker"
		resource := result.CommandResource()
		if found {
			message = fmt.Sprintf("would resume worker spawn from %s", existing.Phase)
			if existing.Resource.Thread != "" {
				resource, _ = result.WorkerResource(existing.Resource.Thread)
			}
		}
		env.Planned = append(env.Planned, result.Outcome{Resource: resource, Action: "spawn", Message: message})
		return env, nil
	}
	if found {
		if existing.Resource.Thread == "" {
			existing.State = config.OperationIndeterminate
			existing.Error = "previous spawn started without a bound thread identity; refusing to create another thread"
			existing.UpdatedAt = time.Now().UTC()
			_, storeErr := config.StoreOperation(dir.OperationsPath(), existing)
			if storeErr != nil {
				return env, result.Runtime(storeErr)
			}
			return env, result.Runtime(errors.New(existing.Error))
		}
		rows, loadErr := config.LoadReadOnly(dir.WorkersPath())
		if loadErr != nil {
			return env, result.Preflight(loadErr)
		}
		for _, row := range rows {
			requested := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: existing.Resource.Thread}
			if workerRowsEquivalent(row, requested) {
				switch existing.Phase {
				case "", config.OperationPhaseMessageVerified, config.OperationPhaseConfigured:
					existing.Phase = config.OperationPhaseConfigured
					existing.State = config.OperationSucceeded
					existing.UpdatedAt = time.Now().UTC()
					_, storeErr := config.StoreOperation(dir.OperationsPath(), existing)
					if storeErr != nil {
						return env, result.Runtime(storeErr)
					}
					env.Skipped = append(env.Skipped, workerOutcome(row, "spawn", "recovered completed spawn"))
					return env, nil
				}
				continue
			}
			if row.Workspace == workspace && row.Window == s.Window {
				return env, result.Preflight(fmt.Errorf("bound spawn cannot resume because worker window %s/%s is configured for thread %s", workspace, s.Window, row.Thread))
			}
			if row.Thread == existing.Resource.Thread {
				return env, result.Preflight(fmt.Errorf("bound spawn cannot resume because thread %s is configured as %s/%s", row.Thread, row.Workspace, row.Window))
			}
		}
		if existing.Phase == "" {
			// Old bound records without an exact configured row may have crossed
			// the delivery boundary. Never resubmit when upgrading them.
			existing.Phase = config.OperationPhaseDeliveryStarted
		}
		return a.resumeBoundSpawn(in, dir, env, existing, workspace, s.Workdir, message)
	}
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, row := range rows {
		if row.Workspace == workspace && row.Window == s.Window {
			return env, result.Preflight(fmt.Errorf("worker window %s/%s is already configured for thread %s", workspace, s.Window, row.Thread))
		}
	}
	preflight, err := inspectWorker(config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: "T-preflight"})
	if err != nil {
		return env, result.Preflight(err)
	}
	if preflight.state != workerPaneAbsent {
		return env, result.Preflight(fmt.Errorf("window %q already exists in tmux session %q", s.Window, workspace))
	}
	now := time.Now().UTC()
	record := config.OperationRecord{Key: s.IdempotencyKey, Kind: "worker-spawn", RequestHash: hash, State: config.OperationStarted, Phase: config.OperationPhaseCreatingThread, Resource: config.OperationResource{Kind: "worker"}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(dir.OperationsPath(), record); err != nil {
		return env, result.Preflight(err)
	}
	ampArgs := []string{"threads", "new"}
	if s.Mode != "" {
		ampArgs = append(ampArgs, "--mode", s.Mode)
	}
	cmd := exec.Command("amp", ampArgs...)
	cmd.Dir = s.Workdir
	output, err := cmd.Output()
	if err != nil {
		record.State = config.OperationIndeterminate
		record.Error = err.Error()
		record.UpdatedAt = time.Now().UTC()
		_, _ = config.StoreOperation(dir.OperationsPath(), record)
		return env, result.Runtime(fmt.Errorf("create Amp thread: %w", err))
	}
	thread := strings.TrimSpace(string(output))
	canonical, err := config.CanonicalThreadID(thread)
	if err != nil {
		record.State = config.OperationIndeterminate
		record.Error = "Amp created a thread but returned an invalid identity: " + err.Error()
		record.UpdatedAt = time.Now().UTC()
		_, _ = config.StoreOperation(dir.OperationsPath(), record)
		return env, result.Runtime(errors.New(record.Error))
	}
	record.Resource.Thread = canonical
	record.Phase = config.OperationPhaseThreadBound
	record.UpdatedAt = time.Now().UTC()
	if _, err := config.StoreOperation(dir.OperationsPath(), record); err != nil {
		return env, result.Runtime(err)
	}
	return a.resumeBoundSpawn(in, dir, env, record, workspace, s.Workdir, message)
}

func (a app) resumeBoundSpawn(in invocation, dir config.Directory, env *result.Envelope, record config.OperationRecord, workspace, workdir, message string) (*result.Envelope, error) {
	row := config.Row{Workspace: workspace, Window: in.Selectors.Window, Workdir: workdir, Thread: record.Resource.Thread}
	var err error
	if record.Phase == config.OperationPhaseThreadBound {
		inspection, inspectErr := inspectWorker(row)
		err = inspectErr
		if err == nil && inspection.state == workerPaneAbsent {
			err = createWorkerPane(row)
			if err == nil {
				inspection, err = inspectWorker(row)
			}
		}
		if err == nil && inspection.state != workerPaneExact {
			err = fmt.Errorf("spawned worker tmux identity is %s", inspection.state)
		}
		if err != nil {
			record.Error = err.Error()
			record.UpdatedAt = time.Now().UTC()
			_, _ = config.StoreOperation(dir.OperationsPath(), record)
			return env, result.Runtime(err)
		}
		deliveryRecord := record
		deliveryRecord.Phase = config.OperationPhaseDeliveryStarted
		deliveryRecord.Error = ""
		deliveryRecord.UpdatedAt = time.Now().UTC()
		if _, err = config.StoreOperation(dir.OperationsPath(), deliveryRecord); err == nil {
			record = deliveryRecord
			_, err = submitInitialMessage(tmux.Runner{}, submissionTarget(tmux.Runner{}, inspection.pane.WindowID), message)
		}
	}
	if err == nil && record.Phase == config.OperationPhaseDeliveryStarted {
		var delivered bool
		delivered, err = verifyBoundThreadMessage(row.Thread, message)
		if err == nil && !delivered {
			err = errors.New("initial message delivery was not verified in the bound thread")
		}
		if err == nil {
			record.Phase = config.OperationPhaseMessageVerified
			record.UpdatedAt = time.Now().UTC()
			_, err = config.StoreOperation(dir.OperationsPath(), record)
		}
	}
	if err == nil && record.Phase == config.OperationPhaseMessageVerified {
		_, err = config.Store(dir.WorkersPath(), row)
		if err == nil {
			record.Phase = config.OperationPhaseConfigured
			record.UpdatedAt = time.Now().UTC()
			_, err = config.StoreOperation(dir.OperationsPath(), record)
		}
	}
	if err != nil {
		if record.Phase == config.OperationPhaseDeliveryStarted {
			record.State = config.OperationIndeterminate
		}
		record.Error = err.Error()
		record.UpdatedAt = time.Now().UTC()
		_, _ = config.StoreOperation(dir.OperationsPath(), record)
		return env, result.Runtime(err)
	}
	record.State = config.OperationSucceeded
	record.UpdatedAt = time.Now().UTC()
	_, err = config.StoreOperation(dir.OperationsPath(), record)
	if err != nil {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, workerOutcome(row, "spawn", "spawned worker"))
	return env, nil
}

func verifyBoundThreadMessage(thread, message string) (bool, error) {
	deadline := time.Now().Add(spawnSubmitTimeout())
	var lastErr error
	for {
		contains, _, err := ampThreadContainsMessage(thread, message)
		if err == nil && contains {
			return true, nil
		}
		lastErr = err
		if !sleepUntilNextSpawnPoll(deadline) {
			break
		}
	}
	if lastErr != nil {
		return false, fmt.Errorf("verify initial message in bound thread %s: %w", thread, lastErr)
	}
	return false, nil
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
	case "launch", "park", "restart", "remove", "shelve", "teardown", "doctor":
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
