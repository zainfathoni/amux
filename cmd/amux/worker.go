package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
	if in.Command.Name == "teardown" && len(rows) != 1 {
		return &env, result.Preflight(fmt.Errorf("teardown requires exactly one configured worker; selector matched %d", len(rows)))
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
			if err == nil && inspections[row.Thread].state == workerPaneExact {
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
		if in.Command.Name == "teardown" && inspections[row.Thread].state == workerPaneAbsent {
			out.Message = "already_stopped"
			env.Skipped = append(env.Skipped, out)
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
	if err := validateIssueWindowTitle(s.TitlePrefix, s.Window); err != nil {
		return env, result.Preflight(err)
	}
	if s.TitlePrefix != "" {
		s.Window = prefixedSpawnName(s.TitlePrefix, s.Window)
		in.Selectors.Window = s.Window
	}
	message := s.Message
	messageSource := config.OperationMessageSourceMessage
	if s.MessageFile != "" {
		messageSource = config.OperationMessageSourceFile
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
		messageSource = config.OperationMessageSourceStdin
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
	requestFields := []string{workspace, s.Window, s.Workdir, s.Mode, message}
	if s.TitlePrefix != "" {
		requestFields = append(requestFields, s.TitlePrefix)
	}
	if len(s.Groups) != 0 {
		requestFields = append(requestFields, "groups")
		requestFields = append(requestFields, s.Groups...)
	}
	request := strings.Join(requestFields, "\x00")
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
	if found && recoverableProvisionedVerificationFailure(existing) {
		if in.Options.DryRun {
			resource, _ := result.WorkerResource(existing.Resource.Thread)
			env.Planned = append(env.Planned, result.Outcome{Resource: resource, Action: "spawn", Message: "would verify the exact provisioned thread and recover without resubmitting"})
			return env, nil
		}
		matches, _, verifyErr := ampThreadContainsExactAssignment(existing.Resource.Thread, message, s.Workdir, false, operationAllowsTerminalLineEndingNormalization(existing, messageSource))
		if verifyErr != nil {
			return env, result.Runtime(fmt.Errorf("verify indeterminate provisioned thread %s without resubmitting: %w", existing.Resource.Thread, verifyErr))
		}
		if !matches {
			return env, result.Preflight(fmt.Errorf("indeterminate assignment is not verified in exact provisioned thread %s; refusing to resubmit or adopt another thread", existing.Resource.Thread))
		}
		requested := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: existing.Resource.Thread}
		rows, loadErr := config.LoadReadOnly(dir.WorkersPath())
		if loadErr != nil {
			return env, result.Preflight(loadErr)
		}
		for _, row := range rows {
			if workerRowsEquivalent(row, requested) {
				continue
			}
			if row.Workspace == workspace && row.Window == s.Window {
				return env, result.Preflight(fmt.Errorf("indeterminate spawn cannot recover because worker window %s/%s is configured for thread %s", workspace, s.Window, row.Thread))
			}
			if row.Thread == existing.Resource.Thread {
				return env, result.Preflight(fmt.Errorf("indeterminate spawn cannot recover because thread %s is configured as %s/%s", row.Thread, row.Workspace, row.Window))
			}
		}
		inspection, inspectErr := inspectWorker(requested)
		if inspectErr != nil {
			return env, result.Preflight(inspectErr)
		}
		if inspection.state != workerPaneAbsent && inspection.state != workerPaneExact {
			return env, result.Preflight(fmt.Errorf("indeterminate spawn cannot recover because worker tmux identity is %s", inspection.state))
		}
		existing, err = config.RecoverIndeterminateWorkerSpawn(dir.OperationsPath(), existing.Key, existing.Resource.Thread)
		if err != nil {
			return env, result.Runtime(err)
		}
	}
	if found && existing.State != config.OperationStarted {
		return env, result.Preflight(fmt.Errorf("spawn operation is terminal in state %s; refusing new work", existing.State))
	}
	if in.Options.DryRun {
		message := fmt.Sprintf("would create worker %s/%s", workspace, s.Window)
		resource := result.CommandResource()
		if found {
			message = fmt.Sprintf("would resume worker spawn from %s", existing.Phase)
			if existing.Resource.Thread != "" {
				resource, _ = result.WorkerResource(existing.Resource.Thread)
			}
		}
		env.Planned = append(env.Planned, result.Outcome{Resource: resource, Action: "spawn", Message: message})
		for _, group := range s.Groups {
			groupResource := result.CommandResource()
			if existing.Resource.Thread != "" && (existing.Phase == config.OperationPhaseMessageVerified || existing.Phase == config.OperationPhaseConfigured || existing.Phase == config.OperationPhaseGroupIntent || existing.Phase == config.OperationPhaseGrouped) {
				groupResource, _ = result.GroupMembershipResource(group, existing.Resource.Thread)
			}
			env.Planned = append(env.Planned, result.Outcome{
				Resource: groupResource,
				Action:   "attach-group",
				Message:  "would persist membership for the authoritative receiving thread, then add-only ensure its Amp label",
				Group:    &result.GroupDetails{ID: group, Role: string(config.GroupMember), ExternalSync: "additive_ensure_planned"},
			})
		}
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, message)
		}
		return env, nil
	}
	var groupAmpPath string
	if len(s.Groups) != 0 {
		if _, loadErr := config.LoadGroupsReadOnly(dir.GroupsPath()); loadErr != nil {
			return env, result.Preflight(loadErr)
		}
		groupAmpPath, err = preflightGroupAmp()
		if err != nil {
			return env, result.Preflight(err)
		}
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
		workerAlreadyConfigured := false
	rowsLoop:
		for _, row := range rows {
			requested := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: existing.Resource.Thread}
			if workerRowsEquivalent(row, requested) {
				switch existing.Phase {
				case "":
					existing.Phase = config.OperationPhaseConfigured
					existing.State = config.OperationSucceeded
					existing.UpdatedAt = time.Now().UTC()
					_, storeErr := config.StoreOperation(dir.OperationsPath(), existing)
					if storeErr != nil {
						return env, result.Runtime(storeErr)
					}
					env.Skipped = append(env.Skipped, workerOutcome(row, "spawn", "recovered completed spawn"))
					return env, nil
				case config.OperationPhaseMessageVerified:
					message := "worker already created; resuming verified spawn"
					if len(s.Groups) != 0 {
						message = "worker already created; resuming grouping only"
					}
					env.Skipped = append(env.Skipped, workerOutcome(row, "spawn", message))
					if !in.Options.JSON {
						fmt.Fprintf(a.stdout, "Worker %s already exists; resuming verified spawn.\n", row.Thread)
					}
					workerAlreadyConfigured = true
					break rowsLoop
				case config.OperationPhaseConfigured, config.OperationPhaseGroupIntent:
					if len(s.Groups) == 0 {
						existing.State = config.OperationSucceeded
						existing.UpdatedAt = time.Now().UTC()
						if _, storeErr := config.StoreOperation(dir.OperationsPath(), existing); storeErr != nil {
							return env, result.Runtime(storeErr)
						}
						env.Skipped = append(env.Skipped, workerOutcome(row, "spawn", "recovered completed spawn"))
						return env, nil
					}
					env.Skipped = append(env.Skipped, workerOutcome(row, "spawn", "worker already created; resuming grouping only"))
					if !in.Options.JSON {
						fmt.Fprintf(a.stdout, "Worker %s already exists; resuming durable grouping only.\n", row.Thread)
					}
					workerAlreadyConfigured = true
					break rowsLoop
				case config.OperationPhaseGrouped:
					existing.State = config.OperationSucceeded
					existing.UpdatedAt = time.Now().UTC()
					if _, storeErr := config.StoreOperation(dir.OperationsPath(), existing); storeErr != nil {
						return env, result.Runtime(storeErr)
					}
					env.Skipped = append(env.Skipped, workerOutcome(row, "spawn", "recovered completed grouped spawn"))
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
		if len(s.Groups) != 0 && (existing.Phase == config.OperationPhaseConfigured || existing.Phase == config.OperationPhaseGroupIntent) && !workerAlreadyConfigured {
			return env, result.Preflight(fmt.Errorf("bound spawn cannot resume grouping because its configured worker row is missing"))
		}
		return a.resumeBoundSpawn(in, dir, env, existing, workspace, s.Workdir, message, groupAmpPath, workerAlreadyConfigured)
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
	record := config.OperationRecord{Key: s.IdempotencyKey, Kind: "worker-spawn", RequestHash: hash, MessageSource: messageSource, State: config.OperationStarted, Phase: config.OperationPhaseCreatingThread, Resource: config.OperationResource{Kind: "worker"}, CreatedAt: now, UpdatedAt: now}
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
	return a.resumeBoundSpawn(in, dir, env, record, workspace, s.Workdir, message, groupAmpPath, false)
}

func (a app) resumeBoundSpawn(in invocation, dir config.Directory, env *result.Envelope, record config.OperationRecord, workspace, workdir, message, groupAmpPath string, workerAlreadyConfigured bool) (*result.Envelope, error) {
	row := config.Row{Workspace: workspace, Window: in.Selectors.Window, Workdir: workdir, Thread: record.Resource.Thread}
	var err error
	configuredThisCall := false
	var preDeliveryThreads map[string]bool
	var inspection workerInspection
	if record.Phase == config.OperationPhaseThreadBound {
		var inspectErr error
		inspection, inspectErr = inspectWorker(row)
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
		if err == nil {
			preDeliveryThreads, err = strictAmpThreadIDSet(true)
			if err != nil {
				err = fmt.Errorf("snapshot Amp threads before initial-message delivery: %w", err)
			}
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
		if preDeliveryThreads == nil {
			if record.ThreadAdoption != nil {
				err = fmt.Errorf("thread adoption was interrupted between provisioned thread %s and receiving thread %s; recovery: inspect both threads and the tmux window, then pin only the authoritative receiver; do not resubmit", record.ThreadAdoption.ProvisionedThread, record.ThreadAdoption.ReceivingThread)
			} else {
				err = fmt.Errorf("initial message delivery was not verified before interruption; recovery: inspect bound thread %s and any fresh receiving threads, and do not resubmit this idempotency key", row.Thread)
			}
		} else {
			var authoritative string
			provisionedRow := row
			authoritative, err = resolveSpawnReceivingThread(row.Thread, message, workdir, preDeliveryThreads, operationAllowsTerminalLineEndingNormalization(record, messageSourceFromSelectors(in.Selectors)))
			if err == nil && authoritative != row.Thread {
				provisioned := row.Thread
				var rows []config.Row
				rows, err = config.LoadReadOnly(dir.WorkersPath())
				if err == nil {
					for _, configured := range rows {
						if configured.Thread == authoritative {
							err = fmt.Errorf("receiving thread %s is already configured as %s/%s; recovery: keep that existing worker authoritative and do not create another managed identity", authoritative, configured.Workspace, configured.Window)
							break
						}
					}
				}
				if err == nil {
					record, err = config.BeginOperationThreadAdoption(dir.OperationsPath(), record.Key, provisioned, authoritative)
				}
				if err == nil {
					row.Thread = authoritative
					command := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
					err = tmux.Runner{}.RespawnPane(inspection.pane.WindowID, command)
				}
				if err == nil {
					var adopted workerInspection
					adopted, err = inspectWorker(row)
					if err == nil && adopted.state != workerPaneExact {
						err = fmt.Errorf("adopted worker tmux identity is %s", adopted.state)
					}
				}
				if err == nil {
					err = archiveAmpThread(provisioned)
				}
				if err == nil {
					record, err = config.CompleteOperationThreadAdoption(dir.OperationsPath(), record.Key)
				}
				if err != nil {
					err = fmt.Errorf("adopt receiving thread %s for provisioned thread %s: %w; recovery: receiving assignment remains in %s; do not retry with a new idempotency key", authoritative, provisioned, err, authoritative)
				}
			}
			if err != nil {
				cleanup := cleanupFailedCanonicalSpawn(provisionedRow, row, inspection)
				if cleanup != "" {
					err = fmt.Errorf("%w; cleanup warning: %s", err, cleanup)
				}
			}
		}
		if err == nil {
			record.Phase = config.OperationPhaseMessageVerified
			record.UpdatedAt = time.Now().UTC()
			_, err = config.StoreOperation(dir.OperationsPath(), record)
		}
	}
	if err == nil && record.Phase == config.OperationPhaseMessageVerified {
		inspection, err = inspectWorker(row)
		if err == nil && inspection.state == workerPaneAbsent {
			err = createWorkerPane(row)
			if err == nil {
				inspection, err = inspectWorker(row)
			}
		}
		if err == nil && inspection.state != workerPaneExact {
			err = fmt.Errorf("verified worker tmux identity is %s", inspection.state)
		}
	}
	if err == nil && record.Phase == config.OperationPhaseMessageVerified {
		if in.Selectors.TitlePrefix != "" {
			if renameErr := renameAmpThread(row.Thread, row.Window); renameErr != nil {
				err = fmt.Errorf("rename Amp thread %s to %q: %w", row.Thread, row.Window, renameErr)
			}
		}
	}
	if err == nil && record.Phase == config.OperationPhaseMessageVerified {
		_, err = config.Store(dir.WorkersPath(), row)
		if err == nil {
			record.Phase = config.OperationPhaseConfigured
			record.UpdatedAt = time.Now().UTC()
			_, err = config.StoreOperation(dir.OperationsPath(), record)
			configuredThisCall = err == nil
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
	if len(in.Selectors.Groups) != 0 && record.Phase == config.OperationPhaseConfigured && configuredThisCall && !workerAlreadyConfigured {
		env.Successful = append(env.Successful, workerOutcome(row, "spawn", "spawned worker; durable group intent follows"))
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "Spawned worker %s; persisting durable group intent before label synchronization.\n", row.Thread)
		}
	}
	if len(in.Selectors.Groups) != 0 && (record.Phase == config.OperationPhaseConfigured || record.Phase == config.OperationPhaseGroupIntent) {
		return a.resumeSpawnGrouping(dir, env, record, row, in.Selectors.Groups, groupAmpPath, in.Options.JSON)
	}
	record.State = config.OperationSucceeded
	record.UpdatedAt = time.Now().UTC()
	_, err = config.StoreOperation(dir.OperationsPath(), record)
	if err != nil {
		return env, result.Runtime(err)
	}
	if workerAlreadyConfigured {
		return env, nil
	}
	env.Successful = append(env.Successful, workerOutcome(row, "spawn", "spawned worker"))
	return env, nil
}

func recoverableProvisionedVerificationFailure(operation config.OperationRecord) bool {
	want := fmt.Sprintf("initial assignment was not found in provisioned thread %s or one unambiguous fresh receiving thread; recovery: inspect thread %s and do not resubmit", operation.Resource.Thread, operation.Resource.Thread)
	return operation.State == config.OperationIndeterminate &&
		operation.Phase == config.OperationPhaseDeliveryStarted &&
		operation.Resource.Thread != "" &&
		operation.ThreadAdoption == nil &&
		operation.Error == want
}

func messageSourceFromSelectors(selectors selectors) config.OperationMessageSource {
	switch {
	case selectors.MessageFile != "":
		return config.OperationMessageSourceFile
	case selectors.MessageStdin:
		return config.OperationMessageSourceStdin
	default:
		return config.OperationMessageSourceMessage
	}
}

func operationAllowsTerminalLineEndingNormalization(operation config.OperationRecord, replaySource config.OperationMessageSource) bool {
	if operation.MessageSource == "" {
		return replaySource == config.OperationMessageSourceFile
	}
	return operation.MessageSource == config.OperationMessageSourceFile
}

func (a app) resumeSpawnGrouping(dir config.Directory, env *result.Envelope, record config.OperationRecord, row config.Row, groups []string, ampPath string, jsonOutput bool) (*result.Envelope, error) {
	if record.Phase == config.OperationPhaseConfigured {
		memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
		if err != nil {
			return env, result.Runtime(err)
		}
		updated := append([]config.GroupMembership(nil), memberships...)
		for _, group := range groups {
			if membershipIndex(updated, group, row.Thread) < 0 {
				updated = append(updated, config.GroupMembership{Group: group, Thread: row.Thread, Role: config.GroupMember})
			}
		}
		if err := config.WriteGroups(dir.GroupsPath(), updated); err != nil {
			return env, result.Runtime(err)
		}
		record.Phase = config.OperationPhaseGroupIntent
		record.Error = ""
		record.UpdatedAt = time.Now().UTC()
		if _, err := config.StoreOperation(dir.OperationsPath(), record); err != nil {
			return env, result.Runtime(err)
		}
	}

	memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil {
		return env, result.Runtime(err)
	}
	failed := false
	for _, group := range groups {
		index := membershipIndex(memberships, group, row.Thread)
		if index < 0 {
			return env, result.Runtime(fmt.Errorf("durable spawn group intent %s/%s is missing; refusing label synchronization", group, row.Thread))
		}
		membership := memberships[index]
		out := groupOutcome(membership, "attach-group")
		if _, err := a.ensureGroupLabel(env, out, ampPath, membership, jsonOutput); err != nil {
			failed = true
		}
	}
	if failed {
		record.Error = "one or more additive Amp label commands failed; worker and durable group intent were retained"
		record.UpdatedAt = time.Now().UTC()
		if _, err := config.StoreOperation(dir.OperationsPath(), record); err != nil {
			return env, result.Runtime(fmt.Errorf("%s; persist resumable grouping failure: %w", record.Error, err))
		}
		return env, result.Runtime(errors.New(record.Error))
	}
	record.Phase = config.OperationPhaseGrouped
	record.State = config.OperationSucceeded
	record.Error = ""
	record.UpdatedAt = time.Now().UTC()
	if _, err := config.StoreOperation(dir.OperationsPath(), record); err != nil {
		return env, result.Runtime(err)
	}
	return env, nil
}

func validateIssueWindowTitle(titlePrefix, window string) error {
	issue, issuePrefix := issueNumberTitlePrefix(titlePrefix)
	if !issuePrefix {
		return nil
	}
	issueWindow := "issue-" + issue
	hashWindow := "#" + issue
	suggestion := ""
	switch {
	case window == issueWindow:
		suggestion = "<semantic-slug>"
	case strings.HasPrefix(window, issueWindow+"-"):
		suggestion = strings.TrimPrefix(window, issueWindow+"-")
	case window == hashWindow:
		suggestion = "<semantic-slug>"
	case strings.HasPrefix(window, hashWindow+" "):
		suggestion = strings.TrimPrefix(window, hashWindow+" ")
	default:
		return nil
	}
	if suggestion == "" {
		suggestion = "<semantic-slug>"
	}
	return fmt.Errorf("window %q duplicates issue identity %s owned by --title-prefix; use corrected issue-unprefixed window %q instead", window, titlePrefix, suggestion)
}

func issueNumberTitlePrefix(titlePrefix string) (string, bool) {
	if len(titlePrefix) < 2 || titlePrefix[0] != '#' {
		return "", false
	}
	for _, digit := range titlePrefix[1:] {
		if digit < '0' || digit > '9' {
			return "", false
		}
	}
	return titlePrefix[1:], true
}

func prefixedSpawnName(titlePrefix, window string) string {
	return strings.TrimSpace(strings.TrimSpace(titlePrefix) + " " + window)
}

func resolveSpawnReceivingThread(boundThread, message, workdir string, preDeliveryThreads map[string]bool, allowTerminalLineEndingNormalization bool) (string, error) {
	deadline := time.Now().Add(spawnSubmitTimeout())
	var authoritative string
	for {
		boundContainsMessage, _, err := ampThreadContainsExactAssignment(boundThread, message, workdir, false, allowTerminalLineEndingNormalization)
		if err != nil {
			return "", fmt.Errorf("verify provisioned thread %s: %w; recovery: inspect thread %s and do not resubmit", boundThread, err, boundThread)
		}
		allThreads, err := strictAmpThreadIDSet(true)
		if err != nil {
			return "", fmt.Errorf("list fresh receiving threads after delivery: %w; recovery: inspect provisioned thread %s and do not resubmit", err, boundThread)
		}
		activeThreads, err := strictAmpThreadIDSet(false)
		if err != nil {
			return "", fmt.Errorf("confirm fresh receiving threads are active: %w; recovery: inspect provisioned thread %s and do not resubmit", err, boundThread)
		}
		var fresh []string
		for candidate := range allThreads {
			if candidate == canonicalThreadID(boundThread) || preDeliveryThreads[candidate] {
				continue
			}
			matches, _, exportErr := ampThreadContainsExactAssignment(candidate, message, workdir, true, false)
			if exportErr != nil {
				return "", fmt.Errorf("verify fresh receiving candidate %s: %w; recovery: inspect %s and provisioned thread %s before choosing an identity", candidate, exportErr, candidate, boundThread)
			}
			if matches {
				fresh = append(fresh, candidate)
			}
		}
		if !boundContainsMessage {
			boundContainsMessage, _, err = ampThreadContainsExactAssignment(boundThread, message, workdir, false, allowTerminalLineEndingNormalization)
			if err != nil {
				return "", fmt.Errorf("recheck provisioned thread %s after fresh-thread discovery: %w; recovery: inspect thread %s and do not resubmit", boundThread, err, boundThread)
			}
		}
		sort.Strings(fresh)
		if boundContainsMessage && len(fresh) > 0 {
			return "", fmt.Errorf("initial assignment has an identity conflict between provisioned thread %s and fresh receiving thread(s) %s; recovery: stop duplicate work and choose the authoritative thread manually", boundThread, strings.Join(fresh, ", "))
		}
		if len(fresh) > 1 {
			return "", fmt.Errorf("initial assignment has multiple fresh receiving threads %s; recovery: stop duplicate work and choose the authoritative thread manually", strings.Join(fresh, ", "))
		}
		authoritative = ""
		if boundContainsMessage {
			authoritative = boundThread
		} else if len(fresh) == 1 {
			candidate := fresh[0]
			if !activeThreads[candidate] {
				return "", fmt.Errorf("fresh receiving thread %s contains the exact assignment but is archived; recovery: inspect and unarchive %s before pinning it manually", candidate, candidate)
			}
			authoritative = candidate
		}
		if !sleepUntilNextSpawnPoll(deadline) {
			break
		}
	}
	if authoritative == "" {
		return "", fmt.Errorf("initial assignment was not found in provisioned thread %s or one unambiguous fresh receiving thread; recovery: inspect thread %s and do not resubmit", boundThread, boundThread)
	}
	return authoritative, nil
}

func strictAmpThreadIDSet(includeArchived bool) (map[string]bool, error) {
	const pageSize = 500
	ids := make(map[string]bool)
	for offset := 0; ; offset += pageSize {
		args := []string{"threads", "list", "--json"}
		if includeArchived {
			args = append(args, "--include-archived")
		}
		args = append(args, "--limit", strconv.Itoa(pageSize), "--offset", strconv.Itoa(offset))
		out, err := exec.Command("amp", args...).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		var page []map[string]any
		if err := json.Unmarshal(out, &page); err != nil {
			return nil, fmt.Errorf("parse amp threads list: %w", err)
		}
		for i, item := range page {
			raw, ok := item["id"].(string)
			if !ok {
				return nil, fmt.Errorf("amp threads list item %d has no string id", offset+i)
			}
			id, err := config.CanonicalThreadID(raw)
			if err != nil {
				return nil, fmt.Errorf("amp threads list item %d: %w", offset+i, err)
			}
			ids[id] = true
		}
		if len(page) < pageSize {
			return ids, nil
		}
	}
}

func ampThreadContainsExactAssignment(thread, message, workdir string, requireWorkdir bool, allowTerminalLineEndingNormalization bool) (bool, bool, error) {
	cmd := exec.Command("amp", "threads", "export", thread)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, false, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return false, false, fmt.Errorf("parse amp threads export for %s: %w", thread, err)
	}
	matches := messagesContainExactUserAssignment(payload["messages"], message)
	if !requireWorkdir {
		matches = messagesContainProvisionedAssignment(payload["messages"], message, allowTerminalLineEndingNormalization)
	}
	if !matches {
		return false, true, nil
	}
	if requireWorkdir && !threadEnvironmentContainsWorkdir(payload["env"], workdir) {
		return false, true, nil
	}
	return true, true, nil
}

func messagesContainExactUserAssignment(value any, message string) bool {
	messages, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range messages {
		entry, ok := item.(map[string]any)
		if !ok || entry["role"] != "user" {
			continue
		}
		if jsonValueContainsExactText(entry["content"], message) {
			return true
		}
	}
	return false
}

func messagesContainProvisionedAssignment(value any, message string, allowTerminalLineEndingNormalization bool) bool {
	messages, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range messages {
		entry, ok := item.(map[string]any)
		if !ok || entry["role"] != "user" {
			continue
		}
		if jsonValueContainsProvisionedText(entry["content"], message, allowTerminalLineEndingNormalization) {
			return true
		}
	}
	return false
}

func jsonValueContainsProvisionedText(value any, want string, allowTerminalLineEndingNormalization bool) bool {
	switch value := value.(type) {
	case string:
		return provisionedAssignmentMatches(value, want, allowTerminalLineEndingNormalization)
	case []any:
		for _, item := range value {
			if jsonValueContainsProvisionedText(item, want, allowTerminalLineEndingNormalization) {
				return true
			}
		}
	case map[string]any:
		text, ok := value["text"].(string)
		kind, _ := value["type"].(string)
		if ok && (kind == "" || kind == "text") {
			return provisionedAssignmentMatches(text, want, allowTerminalLineEndingNormalization)
		}
	}
	return false
}

func provisionedAssignmentMatches(got, want string, allowTerminalLineEndingNormalization bool) bool {
	if duplicatedPrefixAssignment(got, want) {
		return true
	}
	if !allowTerminalLineEndingNormalization {
		return false
	}
	switch {
	case strings.HasSuffix(want, "\r\n"):
		return got == strings.TrimSuffix(want, "\r\n")
	case strings.HasSuffix(want, "\n"):
		return got == strings.TrimSuffix(want, "\n")
	default:
		return false
	}
}

func duplicatedPrefixAssignment(got, want string) bool {
	if got == want {
		return want != ""
	}
	contentEnd := len(want)
	switch {
	case strings.HasSuffix(want, "\r\n"):
		contentEnd -= 2
	case strings.HasSuffix(want, "\r"), strings.HasSuffix(want, "\n"):
		contentEnd--
	}
	if contentEnd <= 0 {
		return false
	}
	lastLine := strings.LastIndexAny(want[:contentEnd], "\r\n") + 1
	if lastLine <= 0 || lastLine == contentEnd {
		return false
	}
	return got == want[:lastLine]+want
}

func jsonValueContainsExactText(value any, want string) bool {
	switch value := value.(type) {
	case string:
		return want != "" && value == want
	case []any:
		for _, item := range value {
			if jsonValueContainsExactText(item, want) {
				return true
			}
		}
	case map[string]any:
		text, ok := value["text"].(string)
		kind, _ := value["type"].(string)
		if ok && (kind == "" || kind == "text") {
			return text == want
		}
	}
	return false
}

func threadEnvironmentContainsWorkdir(value any, workdir string) bool {
	env, ok := value.(map[string]any)
	if !ok {
		return false
	}
	initial, ok := env["initial"].(map[string]any)
	if !ok {
		return false
	}
	trees, ok := initial["trees"].([]any)
	if !ok {
		return false
	}
	want, err := config.CanonicalWorkdir(workdir)
	if err != nil {
		return false
	}
	for _, item := range trees {
		tree, ok := item.(map[string]any)
		uri, uriOK := tree["uri"].(string)
		if !ok || !uriOK {
			continue
		}
		parsed, err := url.Parse(uri)
		if err != nil || parsed.Scheme != "file" || parsed.Host != "" {
			continue
		}
		candidate, err := config.CanonicalWorkdir(filepath.FromSlash(parsed.Path))
		if err == nil && candidate == want {
			return true
		}
	}
	return false
}

func cleanupFailedCanonicalSpawn(provisionedRow, currentRow config.Row, inspection workerInspection) string {
	var failures []string
	stopped := false
	for _, expected := range []config.Row{currentRow, provisionedRow} {
		if stopped || expected.Thread == "" {
			continue
		}
		after, err := inspectWorker(expected)
		if err != nil {
			failures = append(failures, "revalidate unconfigured worker window: "+err.Error())
			continue
		}
		if after.state == workerPaneAbsent {
			stopped = true
			continue
		}
		if after.state != workerPaneExact || after.pane.WindowID != inspection.pane.WindowID {
			continue
		}
		if err := (tmux.Runner{}).KillWindow(after.pane.WindowID); err != nil {
			failures = append(failures, "stop unconfigured worker window "+after.pane.WindowID+": "+err.Error())
		} else {
			stopped = true
		}
	}
	if !stopped && inspection.pane.WindowID != "" {
		failures = append(failures, "unconfigured worker window identity changed; left it running for manual recovery")
	}
	return strings.Join(failures, "; ")
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
