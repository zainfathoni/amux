package main

import (
	"bytes"
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
	semanticWindow := s.Window
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
	for name, value := range map[string]string{"workspace": workspace, "window": semanticWindow, "workdir": s.Workdir} {
		if err := config.ValidateField(name, value); err != nil {
			return env, result.Preflight(err)
		}
	}
	naming, err := resolveSpawnGroupNaming(dir, s, semanticWindow)
	if err != nil {
		return env, result.Preflight(err)
	}
	if naming != nil {
		s.Groups = []string{naming.GroupID}
		in.Selectors.Groups = append([]string(nil), s.Groups...)
		outcome := result.Outcome{
			Resource:    result.ConfigResource(naming.ConfigSource),
			Action:      "resolve-group-naming",
			Message:     "resolved repository-scoped work-group naming before mutation",
			GroupNaming: naming,
		}
		if in.Options.DryRun {
			env.Planned = append(env.Planned, outcome)
		} else {
			env.Successful = append(env.Successful, outcome)
		}
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "GROUP_NAMING\t%s\t%s\t%s\t%s\t%s\t%s\n", naming.ProjectPrefix, naming.WorkItemID, naming.Slug, naming.GroupID, naming.ReportID, naming.ConfigSource)
		}
	}
	if s.TitlePrefix != "" {
		s.Window = prefixedSpawnName(s.TitlePrefix, semanticWindow)
		in.Selectors.Window = s.Window
	}
	requestFields := []string{workspace, s.Window, s.Workdir, s.Mode, message}
	if s.TitlePrefix != "" {
		requestFields = append(requestFields, s.TitlePrefix)
	}
	if len(s.Groups) != 0 {
		requestFields = append(requestFields, "groups")
		requestFields = append(requestFields, s.Groups...)
	}
	if naming != nil {
		requestFields = append(requestFields, "group-naming/v1", naming.Repository, naming.ReportID)
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
	preSubmissionRetry := found && recoverablePreSubmissionWorkerSpawn(existing, message)
	provisionedVerification := found && recoverableProvisionedVerificationFailure(existing)
	recoverable := preSubmissionRetry || provisionedVerification
	if recoverable && !s.Reconcile {
		return env, result.Preflight(fmt.Errorf("indeterminate provisioned-thread spawn recovery requires explicit --reconcile; refusing external work"))
	}
	if s.Reconcile && !recoverable {
		return env, result.Preflight(fmt.Errorf("spawn reconciliation requires an existing identical operation in the recoverable exact provisioned-thread state"))
	}
	if found && existing.State == config.OperationSucceeded {
		r := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: existing.Resource.Thread}
		out := workerOutcome(r, "spawn", "idempotency key already succeeded")
		env.Skipped = append(env.Skipped, out)
		return env, nil
	}
	if preSubmissionRetry {
		if in.Options.DryRun {
			resource, _ := result.WorkerResource(existing.Resource.Thread)
			env.Planned = append(env.Planned, result.Outcome{Resource: resource, Action: "spawn", Message: "would revalidate the exact empty provisioned thread and retry pre-submission delivery"})
			return env, nil
		}
		requested := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: existing.Resource.Thread}
		if err := preflightPreSubmissionWorkerSpawnRetry(dir, existing, requested, s.Groups); err != nil {
			return env, result.Preflight(err)
		}
		if err := preflightPreSubmissionWorkerSpawnRetry(dir, existing, requested, s.Groups); err != nil {
			return env, result.Preflight(fmt.Errorf("revalidate pre-submission worker spawn before retry: %w", err))
		}
		if existing.State == config.OperationIndeterminate {
			existing, err = config.RetryPreSubmissionWorkerSpawn(dir.OperationsPath(), existing.Key, existing.RequestHash, existing.Resource.Thread, strings.ContainsAny(message, "\r\n"))
			if err != nil {
				return env, result.Runtime(err)
			}
		}
	}
	if provisionedVerification {
		if in.Options.DryRun {
			resource, _ := result.WorkerResource(existing.Resource.Thread)
			env.Planned = append(env.Planned, result.Outcome{Resource: resource, Action: "spawn", Message: "would verify the exact provisioned thread and recover without resubmitting"})
			return env, nil
		}
		allowLineEndingNormalization := operationAllowsTerminalLineEndingNormalization(existing, messageSource)
		matches, _, verifyErr := ampThreadContainsExactAssignment(existing.Resource.Thread, message, s.Workdir, false, allowLineEndingNormalization)
		if verifyErr != nil {
			return env, result.Runtime(fmt.Errorf("verify indeterminate provisioned thread %s without resubmitting: %w", existing.Resource.Thread, verifyErr))
		}
		if !matches {
			var receiver string
			receiver, verifyErr = findIndeterminateSpawnReceiver(existing, message, s.Workdir, allowLineEndingNormalization)
			if verifyErr != nil {
				return env, result.Preflight(verifyErr)
			}
			pendingAdoption := existing.ThreadAdoption != nil
			if pendingAdoption && receiver != existing.ThreadAdoption.ReceivingThread {
				return env, result.Preflight(fmt.Errorf("pending adoption is bound to receiver %s, but current evidence identifies %s; recovery: preserve both identities and do not guess ownership", existing.ThreadAdoption.ReceivingThread, receiver))
			}
			requestedReceiver := config.Row{Workspace: workspace, Window: s.Window, Workdir: s.Workdir, Thread: receiver}
			rows, loadErr := config.LoadReadOnly(dir.WorkersPath())
			if loadErr != nil {
				return env, result.Preflight(loadErr)
			}
			for _, row := range rows {
				if row.Workspace == workspace && row.Window == s.Window {
					return env, result.Preflight(fmt.Errorf("indeterminate alternate receiver %s cannot be adopted because worker window %s/%s is configured for thread %s", receiver, workspace, s.Window, row.Thread))
				}
				if row.Thread == receiver {
					return env, result.Preflight(fmt.Errorf("indeterminate alternate receiver %s is already configured as %s/%s", receiver, row.Workspace, row.Window))
				}
				if row.Thread == existing.Resource.Thread {
					return env, result.Preflight(fmt.Errorf("indeterminate provisioned thread %s is already configured as %s/%s; refusing residue cleanup", existing.Resource.Thread, row.Workspace, row.Window))
				}
			}
			receiverInspection, inspectErr := inspectWorker(requestedReceiver)
			if inspectErr != nil {
				return env, result.Preflight(inspectErr)
			}
			provisionedRow := requestedReceiver
			provisionedRow.Thread = existing.Resource.Thread
			provisionedInspection := workerInspection{state: workerPaneAbsent}
			if receiverInspection.state == workerPaneConflict {
				provisionedInspection, inspectErr = inspectWorker(provisionedRow)
				if inspectErr != nil {
					return env, result.Preflight(inspectErr)
				}
			}
			if receiverInspection.state != workerPaneAbsent && receiverInspection.state != workerPaneExact && provisionedInspection.state != workerPaneExact {
				return env, result.Preflight(fmt.Errorf("indeterminate alternate receiver %s cannot be adopted because worker tmux identity is %s", receiver, receiverInspection.state))
			}
			receiverPanes, paneErr := managedThreadPanes(receiver)
			if paneErr != nil {
				return env, result.Preflight(fmt.Errorf("inspect existing tmux ownership for alternate receiver %s: %w", receiver, paneErr))
			}
			if len(receiverPanes) > 1 || len(receiverPanes) == 1 && (receiverInspection.state != workerPaneExact || receiverPanes[0].WindowID != receiverInspection.pane.WindowID) {
				return env, result.Preflight(fmt.Errorf("alternate receiver %s already has authoritative tmux ownership outside the requested worker identity; refusing a duplicate window", receiver))
			}
			confirmedReceiver, confirmErr := findIndeterminateSpawnReceiver(existing, message, s.Workdir, allowLineEndingNormalization)
			if confirmErr != nil {
				return env, result.Preflight(fmt.Errorf("reconfirm alternate receiver %s before durable adoption: %w", receiver, confirmErr))
			}
			if confirmedReceiver != receiver {
				return env, result.Preflight(fmt.Errorf("alternate receiver changed from %s to %s during reconciliation; recovery: preserve all identities and do not guess ownership", receiver, confirmedReceiver))
			}
			if !pendingAdoption {
				existing, err = config.BeginIndeterminateWorkerSpawnThreadAdoption(dir.OperationsPath(), existing.Key, existing.Resource.Thread, receiver)
				if err != nil {
					return env, result.Runtime(err)
				}
			}
			if provisionedInspection.state == workerPaneExact {
				command := teardownExpectedStartCommand(teardownIdentity{Workspace: requestedReceiver.Workspace, Session: requestedReceiver.Workspace, Window: requestedReceiver.Window, Thread: receiver}, requestedReceiver)
				if err = revalidateWorkerBeforeMutation(provisionedRow, provisionedInspection); err == nil {
					err = (tmux.Runner{}).RespawnPane(provisionedInspection.pane.WindowID, command)
				}
				if err == nil {
					receiverInspection, err = inspectWorker(requestedReceiver)
					if err == nil && receiverInspection.state != workerPaneExact {
						err = fmt.Errorf("adopted worker tmux identity is %s", receiverInspection.state)
					}
				}
			}
			provisioned := existing.ThreadAdoption.ProvisionedThread
			if err == nil {
				var stillEmpty bool
				stillEmpty, err = ampThreadHasNoMessages(provisioned)
				if err == nil && !stillEmpty {
					err = fmt.Errorf("provisioned thread %s gained content before residue cleanup", provisioned)
				}
			}
			if err == nil {
				var receiverMatches, receiverStarted bool
				receiverMatches, receiverStarted, err = ampThreadContainsExactAssignment(receiver, message, s.Workdir, true, allowLineEndingNormalization)
				if err == nil && (!receiverMatches || receiverStarted) {
					err = fmt.Errorf("alternate receiver %s changed before residue cleanup (exact=%t, external_work=%t)", receiver, receiverMatches, receiverStarted)
				}
			}
			if err == nil {
				var activeThreads map[string]bool
				activeThreads, err = strictAmpThreadIDSet(false)
				if err == nil && !activeThreads[receiver] {
					err = fmt.Errorf("alternate receiver %s became inactive before residue cleanup", receiver)
				}
				if err == nil && activeThreads[provisioned] {
					err = archiveAmpThread(provisioned)
				}
			}
			if err == nil {
				existing, err = config.CompleteOperationThreadAdoption(dir.OperationsPath(), existing.Key)
			}
			if err != nil {
				return env, result.Runtime(fmt.Errorf("reconcile alternate receiving thread %s for provisioned thread %s: %w; recovery: preserve both identities and do not resubmit", receiver, provisioned, err))
			}
			existing.Phase = config.OperationPhaseMessageVerified
			existing.UpdatedAt = time.Now().UTC()
			if _, err = config.StoreOperation(dir.OperationsPath(), existing); err != nil {
				return env, result.Runtime(err)
			}
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
		if matches {
			existing, err = config.RecoverIndeterminateWorkerSpawn(dir.OperationsPath(), existing.Key, existing.Resource.Thread)
			if err != nil {
				return env, result.Runtime(err)
			}
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
		if len(s.Groups) != 0 {
			// For a stable authoritative replay the receiving thread and durable
			// memberships are already known, so a group whose membership is already a
			// coordinator is a not-projected skip; every other requested group is a
			// member whose additive label ensure remains planned.
			authoritative := stableSpawnGroupReplay(found, existing)
			var memberships []config.GroupMembership
			if authoritative {
				memberships, err = config.LoadGroupsReadOnly(dir.GroupsPath())
				if err != nil {
					return env, result.Preflight(err)
				}
			}
			for _, group := range s.Groups {
				if authoritative {
					if index := membershipIndex(memberships, group, existing.Resource.Thread); index >= 0 && memberships[index].Role == config.GroupCoordinator {
						out := groupOutcome(memberships[index], "attach-group")
						out.Group.ID = group
						out.Group.ExternalSync = "not_projected"
						out.Message = "coordinator membership is local-only; the coordinator label is not projected to Amp"
						env.Skipped = append(env.Skipped, out)
						continue
					}
				}
				groupResource := result.CommandResource()
				if authoritative {
					groupResource, _ = result.GroupMembershipResource(group, existing.Resource.Thread)
				}
				env.Planned = append(env.Planned, result.Outcome{
					Resource: groupResource,
					Action:   "attach-group",
					Message:  "would persist membership for the authoritative receiving thread, then add-only ensure its Amp label",
					Group:    &result.GroupDetails{ID: group, Role: string(config.GroupMember), ExternalSync: "additive_ensure_planned"},
				})
			}
		}
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, message)
		}
		return env, nil
	}
	var groupAmpPath string
	if len(s.Groups) != 0 {
		memberships, loadErr := config.LoadGroupsReadOnly(dir.GroupsPath())
		if loadErr != nil {
			return env, result.Preflight(loadErr)
		}
		// A stable authoritative replay can classify roles now: coordinator
		// memberships are never projected, so Amp preflight is required only when at
		// least one requested group is (or will become) a projected member. Fresh and
		// early spawns have no authoritative future role yet and preflight eagerly.
		needsProjection := true
		if stableSpawnGroupReplay(found, existing) {
			needsProjection = false
			for _, group := range s.Groups {
				index := membershipIndex(memberships, group, existing.Resource.Thread)
				if index < 0 || memberships[index].Role != config.GroupCoordinator {
					needsProjection = true
					break
				}
			}
		}
		if needsProjection {
			groupAmpPath, err = preflightGroupAmp()
			if err != nil {
				return env, result.Preflight(err)
			}
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
	preSubmissionRetryArmed := record.Phase == config.OperationPhaseRetryArmed && record.Error == config.OperationErrorPreSubmissionRetryArmed
	preSubmissionRetryConsumed := record.Error == config.OperationErrorPreSubmissionRetryConsumed
	var err error
	configuredThisCall := false
	var preDeliveryThreads map[string]bool
	var inspection workerInspection
	if record.Phase == config.OperationPhaseThreadBound || record.Phase == config.OperationPhaseRetryArmed {
		var inspectErr error
		inspection, inspectErr = inspectWorker(row)
		err = inspectErr
		if preSubmissionRetryArmed {
			if err == nil && inspection.state != workerPaneAbsent && inspection.state != workerPaneExact {
				err = fmt.Errorf("pre-submission retry worker tmux identity is %s", inspection.state)
			}
			if err == nil {
				preDeliveryThreads, err = strictAmpThreadIDSet(true)
				if err != nil {
					err = fmt.Errorf("snapshot Amp threads before initial-message delivery: %w", err)
				}
			}
			if err != nil {
				return env, result.Runtime(err)
			}
			record, err = config.ConsumePreSubmissionWorkerSpawnRetry(dir.OperationsPath(), record.Key, record.RequestHash, record.Resource.Thread)
			preSubmissionRetryConsumed = err == nil
			if err == nil && inspection.state == workerPaneAbsent {
				err = createWorkerPane(row)
			}
			if err == nil {
				inspection, err = inspectWorker(row)
			}
			if err == nil && inspection.state != workerPaneExact {
				err = fmt.Errorf("spawned worker tmux identity is %s", inspection.state)
			}
			if err == nil {
				inspection.pane, err = preSubmissionRetryPane(row)
			}
		} else {
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
			}
		}
		if err == nil {
			var submission initialMessageSubmission
			if preSubmissionRetryConsumed {
				target := inspection.pane.PaneID
				if target == "" {
					err = errors.New("pre-submission retry exact pane identity is unavailable")
				}
				guard := initialMessageSubmissionGuard{
					beforeInput: func() error {
						if guardErr := validatePreSubmissionRetryMutation(row, inspection.pane); guardErr != nil {
							return errors.New("pre-submission retry guard rejected changed thread or pane evidence")
						}
						return nil
					},
					beforeEnter: func() error {
						if guardErr := validatePreSubmissionRetryMutation(row, inspection.pane); guardErr != nil {
							return errors.New("pre-submission retry guard rejected changed thread or pane evidence")
						}
						visible, available := composerContainsMessage(tmux.Runner{}, target, message)
						if !available || !visible {
							return errors.New("pre-submission retry guard rejected changed composer evidence")
						}
						return nil
					},
				}
				if err == nil {
					submission, err = submitInitialMessageGuarded(tmux.Runner{}, target, message, guard)
				}
			} else {
				target := submissionTarget(tmux.Runner{}, inspection.pane.WindowID)
				submission, err = submitInitialMessage(tmux.Runner{}, target, message)
			}
			if err != nil {
				record.SubmissionStatus = config.OperationSubmissionError
				record.DeliveryStatus = config.OperationDeliveryUnknown
			}
			if err == nil {
				record.SubmissionStatus = submission.status
				var publicError string
				switch submission.status {
				case config.OperationSubmissionComposerUnavailable:
					record.DeliveryStatus = config.OperationDeliveryUnknown
					publicError = "multiline composer visibility was unavailable before input; Enter was not attempted"
					record.Error = "multiline composer visibility unavailable before submission; Enter not attempted"
				case config.OperationSubmissionComposerCaptureUnknown:
					record.DeliveryStatus = config.OperationDeliveryUnknown
					publicError = "multiline composer visibility could not be observed before input; Enter was not attempted"
					record.Error = "multiline composer visibility unknown before submission; Enter not attempted"
				case config.OperationSubmissionInputNotVisible:
					record.DeliveryStatus = config.OperationDeliveryUnknown
					publicError = "multiline input was not visible in the composer after bounded paste attempts; Enter was not attempted"
					record.Error = "multiline input not visible before submission; Enter not attempted"
				case config.OperationSubmissionInputVisibilityUnknown:
					record.DeliveryStatus = config.OperationDeliveryUnknown
					publicError = "multiline input visibility could not be observed after paste; Enter was not attempted"
					record.Error = "multiline input visibility unknown before submission; Enter not attempted"
				}
				record.UpdatedAt = time.Now().UTC()
				if publicError != "" {
					if preSubmissionRetryConsumed {
						record.Error = config.OperationErrorPreSubmissionRetryConsumed
					}
					record.State = config.OperationIndeterminate
					if _, err = config.StoreOperation(dir.OperationsPath(), record); err == nil {
						return env, result.Runtime(errors.New(publicError))
					}
				} else {
					_, err = config.StoreOperation(dir.OperationsPath(), record)
				}
			}
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
			if err != nil {
				record.DeliveryStatus = config.OperationDeliveryUnknown
				if strings.HasPrefix(err.Error(), "initial assignment was not found in provisioned thread ") {
					record.DeliveryStatus = config.OperationDeliveryMissing
				}
			} else if authoritative == row.Thread {
				record.DeliveryStatus = config.OperationDeliveryPersisted
			} else {
				record.DeliveryStatus = config.OperationDeliveryAlternateReceiver
			}
			if err == nil {
				record.Error = ""
			}
			if err == nil && authoritative != row.Thread {
				provisioned := row.Thread
				var rows []config.Row
				record.UpdatedAt = time.Now().UTC()
				_, err = config.StoreOperation(dir.OperationsPath(), record)
				if err != nil {
					err = fmt.Errorf("store alternate receiver delivery evidence: %w", err)
				}
				if err == nil {
					rows, err = config.LoadReadOnly(dir.WorkersPath())
				}
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
					err = verifyAlternateReceiverBeforeCleanup(provisioned, authoritative, message, workdir, preDeliveryThreads, operationAllowsTerminalLineEndingNormalization(record, messageSourceFromSelectors(in.Selectors)))
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
			record.Error = ""
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
			if record.DeliveryStatus == "" {
				record.DeliveryStatus = config.OperationDeliveryUnknown
			}
		}
		if !preSubmissionRetryArmed || record.Phase != config.OperationPhaseRetryArmed {
			record.Error = err.Error()
		}
		if record.Phase == config.OperationPhaseDeliveryStarted {
			switch record.SubmissionStatus {
			case config.OperationSubmissionComposerUnavailable:
				record.Error = "multiline composer visibility unavailable before submission; Enter not attempted"
			case config.OperationSubmissionComposerCaptureUnknown:
				record.Error = "multiline composer visibility unknown before submission; Enter not attempted"
			case config.OperationSubmissionInputNotVisible:
				record.Error = "multiline input not visible before submission; Enter not attempted"
			case config.OperationSubmissionInputVisibilityUnknown:
				record.Error = "multiline input visibility unknown before submission; Enter not attempted"
			default:
				record.Error = "initial assignment delivery could not be verified; do not resubmit"
			}
			if preSubmissionRetryConsumed {
				record.Error = config.OperationErrorPreSubmissionRetryConsumed
			}
		}
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
	originalFailure := operation.State == config.OperationIndeterminate &&
		operation.Phase == config.OperationPhaseDeliveryStarted &&
		operation.Resource.Thread != "" &&
		operation.ThreadAdoption == nil &&
		(operation.DeliveryStatus == config.OperationDeliveryMissing || operation.DeliveryStatus == "" && operation.Error == want)
	pendingAdoption := operation.State == config.OperationStarted &&
		operation.Phase == config.OperationPhaseDeliveryStarted &&
		operation.ThreadAdoption != nil &&
		operation.Resource.Thread == operation.ThreadAdoption.ProvisionedThread
	return originalFailure || pendingAdoption
}

func recoverablePreSubmissionWorkerSpawn(operation config.OperationRecord, message string) bool {
	if !strings.ContainsAny(message, "\r\n") {
		return false
	}
	preSubmission := false
	switch operation.SubmissionStatus {
	case config.OperationSubmissionComposerUnavailable, config.OperationSubmissionComposerCaptureUnknown, config.OperationSubmissionInputNotVisible, config.OperationSubmissionInputVisibilityUnknown:
		preSubmission = true
	}
	originalFailure := operation.State == config.OperationIndeterminate &&
		operation.Phase == config.OperationPhaseDeliveryStarted &&
		operation.Resource.Thread != "" &&
		operation.ThreadAdoption == nil &&
		preSubmission &&
		operation.DeliveryStatus == config.OperationDeliveryUnknown &&
		preSubmissionFailureErrorMatches(operation.SubmissionStatus, operation.Error)
	armedRetry := operation.State == config.OperationStarted &&
		operation.Phase == config.OperationPhaseRetryArmed &&
		operation.Resource.Thread != "" &&
		operation.ThreadAdoption == nil &&
		operation.SubmissionStatus == "" &&
		operation.DeliveryStatus == "" &&
		operation.Error == config.OperationErrorPreSubmissionRetryArmed
	return originalFailure || armedRetry
}

func preSubmissionFailureErrorMatches(status config.OperationSubmissionStatus, message string) bool {
	switch status {
	case config.OperationSubmissionComposerUnavailable:
		return message == "multiline composer visibility unavailable before submission; Enter not attempted"
	case config.OperationSubmissionComposerCaptureUnknown:
		return message == "multiline composer visibility unknown before submission; Enter not attempted"
	case config.OperationSubmissionInputNotVisible:
		return message == "multiline input not visible before submission; Enter not attempted"
	case config.OperationSubmissionInputVisibilityUnknown:
		return message == "multiline input visibility unknown before submission; Enter not attempted"
	default:
		return false
	}
}

func preflightPreSubmissionWorkerSpawnRetry(dir config.Directory, operation config.OperationRecord, requested config.Row, groups []string) error {
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return err
	}
	for _, row := range rows {
		if workerRowsEquivalent(row, requested) {
			continue
		}
		if row.Workspace == requested.Workspace && row.Window == requested.Window {
			return fmt.Errorf("pre-submission spawn cannot retry because worker window %s/%s is configured for thread %s", requested.Workspace, requested.Window, row.Thread)
		}
		if row.Thread == requested.Thread {
			return fmt.Errorf("pre-submission spawn cannot retry because thread %s is configured as %s/%s", row.Thread, row.Workspace, row.Window)
		}
	}
	memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil {
		return err
	}
	requestedGroups := make(map[string]bool, len(groups))
	for _, group := range groups {
		requestedGroups[group] = true
	}
	for _, membership := range memberships {
		if membership.Thread == requested.Thread && (!requestedGroups[membership.Group] || membership.Role != config.GroupMember) {
			return fmt.Errorf("pre-submission spawn cannot retry because thread %s has conflicting group ownership", requested.Thread)
		}
	}
	inspection, err := inspectWorker(requested)
	if err != nil {
		return err
	}
	if inspection.state != workerPaneAbsent && inspection.state != workerPaneExact {
		return fmt.Errorf("pre-submission spawn cannot retry because worker tmux identity is %s", inspection.state)
	}
	panes, err := managedThreadPanes(requested.Thread)
	if err != nil {
		return fmt.Errorf("inspect existing tmux ownership for pre-submission thread %s: %w", requested.Thread, err)
	}
	if len(panes) > 1 || len(panes) == 1 && (inspection.state != workerPaneExact || panes[0].WindowID != inspection.pane.WindowID) || len(panes) == 0 && inspection.state == workerPaneExact {
		return fmt.Errorf("pre-submission thread %s has ambiguous tmux ownership", requested.Thread)
	}
	empty, workdirMatches, err := ampThreadEmptyInWorkdir(operation.Resource.Thread, requested.Workdir)
	if err != nil {
		return fmt.Errorf("verify pre-submission thread %s: %w", operation.Resource.Thread, err)
	}
	if !empty || !workdirMatches {
		return fmt.Errorf("pre-submission thread %s changed or does not match the requested workdir", operation.Resource.Thread)
	}
	active, err := strictAmpThreadIDSet(false)
	if err != nil {
		return fmt.Errorf("confirm pre-submission thread %s is active: %w", operation.Resource.Thread, err)
	}
	if !active[operation.Resource.Thread] {
		return fmt.Errorf("pre-submission thread %s is no longer active", operation.Resource.Thread)
	}
	return nil
}

func validatePreSubmissionRetryMutation(row config.Row, expected tmux.WindowPane) error {
	current, err := preSubmissionRetryPane(row)
	if err != nil {
		return err
	}
	if expected.Session == "" || expected.Window == "" || expected.WindowID == "" || expected.PaneID == "" || current.Session != expected.Session || current.Window != expected.Window || current.WindowID != expected.WindowID || current.PaneID != expected.PaneID {
		return errors.New("worker pane identity changed")
	}
	panes, err := managedThreadPanesWithPaneID(row.Thread)
	if err != nil {
		return err
	}
	if len(panes) != 1 || panes[0].Session != expected.Session || panes[0].Window != expected.Window || panes[0].WindowID != expected.WindowID || panes[0].PaneID != expected.PaneID {
		return errors.New("worker thread ownership changed")
	}
	active, err := strictAmpThreadIDSet(false)
	if err != nil {
		return err
	}
	if !active[row.Thread] {
		return errors.New("worker thread is no longer active")
	}
	empty, workdirMatches, err := ampThreadEmptyInWorkdir(row.Thread, row.Workdir)
	if err != nil {
		return err
	}
	if !empty || !workdirMatches {
		return errors.New("worker thread identity or emptiness changed")
	}
	return nil
}

func preSubmissionRetryPane(row config.Row) (tmux.WindowPane, error) {
	panes, err := (tmux.Runner{}).WindowPanesWithPaneID(row.Workspace, row.Window)
	if err != nil {
		return tmux.WindowPane{}, err
	}
	if len(panes) != 1 || panes[0].WindowID == "" || panes[0].PaneID == "" {
		return tmux.WindowPane{}, errors.New("worker pane identity is not exact")
	}
	expected := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	if normalizedTmuxStartCommand(panes[0].StartCommand) != expected {
		return tmux.WindowPane{}, errors.New("worker pane command changed")
	}
	return panes[0], nil
}

func managedThreadPanesWithPaneID(thread string) ([]tmux.WindowPane, error) {
	panes, err := (tmux.Runner{}).AllWindowPanesWithPaneID()
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

func resolveSpawnGroupNaming(dir config.Directory, s selectors, slug string) (*result.GroupNamingDetails, error) {
	if len(s.Groups) != 0 {
		return nil, nil
	}
	if s.WorkItemID == "" && s.WorkerOrdinal == "" {
		return nil, nil
	}
	if s.WorkItemID == "" || s.WorkerOrdinal == "" {
		return nil, errors.New("automatic group naming requires both --work-item-id and --worker-ordinal")
	}
	ordinal, err := strconv.Atoi(s.WorkerOrdinal)
	if err != nil || ordinal < 1 || strconv.Itoa(ordinal) != s.WorkerOrdinal {
		return nil, errors.New("worker ordinal must be a canonical positive integer")
	}
	repository, err := verifiedRepositoryIdentity(s.Workdir)
	if err != nil {
		return nil, err
	}
	configSource := dir.GroupNamingPath()
	if len(configSource) > 4096 || strings.ContainsAny(configSource, "\t\r\n") {
		return nil, errors.New("group naming config source path is invalid or exceeds 4096 characters")
	}
	namingConfig, err := config.LoadGroupNaming(configSource)
	if err != nil {
		return nil, err
	}
	project, err := namingConfig.Project(repository)
	if err != nil {
		return nil, err
	}
	groupID, reportID, err := config.DeriveGroupNaming(project.Prefix, s.WorkItemID, slug, ordinal)
	if err != nil {
		return nil, err
	}
	return &result.GroupNamingDetails{
		ProjectPrefix: project.Prefix,
		Repository:    repository,
		WorkItemID:    s.WorkItemID,
		Slug:          slug,
		GroupID:       groupID,
		ReportID:      reportID,
		ConfigSource:  configSource,
	}, nil
}

func verifiedRepositoryIdentity(workdir string) (string, error) {
	command := exec.Command("git", "-C", workdir, "remote", "get-url", "--all", "origin")
	var output boundedOutput
	command.Stdout = &output
	err := command.Run()
	if err != nil {
		return "", fmt.Errorf("verify repository identity from origin: %w", err)
	}
	if output.overflow || output.buffer.Len() == 0 || bytes.ContainsAny(output.buffer.Bytes(), "\x00\r") {
		return "", errors.New("verify repository identity from origin: invalid or over-limit remote URL")
	}
	remote := strings.TrimSuffix(output.buffer.String(), "\n")
	if remote == "" || strings.Contains(remote, "\n") {
		return "", errors.New("verify repository identity from origin: expected exactly one remote URL")
	}
	return canonicalRepositoryRemote(remote)
}

func canonicalRepositoryRemote(remote string) (string, error) {
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") {
		remote = "ssh://" + strings.Replace(remote, ":", "/", 1)
	}
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("verify repository identity from origin: unsupported remote URL %q", remote)
	}
	path := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), ".git")
	if strings.Count(path, "/") != 1 || strings.ContainsAny(path, " \t\r\n:@") {
		return "", fmt.Errorf("verify repository identity from origin: expected host/owner/repository in %q", remote)
	}
	identity := parsed.Hostname() + "/" + path
	if err := config.ValidateRepositoryIdentity(identity); err != nil {
		return "", fmt.Errorf("verify repository identity from origin: %w", err)
	}
	return identity, nil
}

type boundedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	remaining := 4096 - w.buffer.Len()
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = w.buffer.Write(p[:remaining])
	}
	if remaining < len(p) {
		w.overflow = true
	}
	return len(p), nil
}

// stableSpawnGroupReplay reports whether a found spawn operation has advanced far
// enough that its receiving thread identity is authoritative and its durable group
// memberships are meaningful, so effective per-group roles can be derived before
// dry-run planning or Amp preflight. Fresh spawns and early phases (before the
// initial message is verified) have no authoritative identity or future role yet
// and must preflight eagerly.
func stableSpawnGroupReplay(found bool, operation config.OperationRecord) bool {
	if !found || operation.Resource.Thread == "" {
		return false
	}
	switch operation.Phase {
	case config.OperationPhaseMessageVerified, config.OperationPhaseConfigured, config.OperationPhaseGroupIntent, config.OperationPhaseGrouped:
		return true
	default:
		return false
	}
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
		if membership.Role == config.GroupCoordinator {
			// The membership was promoted to coordinator after spawn; coordinator
			// identity is durable local metadata and is never projected to a label.
			out := groupOutcome(membership, "attach-group")
			out.Group.ExternalSync = "not_projected"
			out.Message = "membership became coordinator after spawn; coordinator labels are not projected to Amp"
			env.Skipped = append(env.Skipped, out)
			if !jsonOutput {
				fmt.Fprintf(a.stdout, "Skipped label projection for coordinator membership %s\t%s; coordinator labels are not projected.\n", membership.Group, membership.Thread)
			}
			continue
		}
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

func findIndeterminateSpawnReceiver(operation config.OperationRecord, message, workdir string, allowTerminalLineEndingNormalization bool) (string, error) {
	empty, err := ampThreadHasNoMessages(operation.Resource.Thread)
	if err != nil {
		return "", fmt.Errorf("verify provisioned thread %s is empty before alternate-receiver reconciliation: %w", operation.Resource.Thread, err)
	}
	if !empty {
		return "", fmt.Errorf("indeterminate assignment is not verified in exact provisioned thread %s; provisioned thread has conflicting content, so preserve it and do not guess an alternate receiver", operation.Resource.Thread)
	}
	provisionedTime, ok := ampUUIDv7ThreadTime(operation.Resource.Thread)
	if !ok || provisionedTime.Before(operation.CreatedAt) {
		return "", fmt.Errorf("provisioned thread %s has no trustworthy post-operation UUIDv7 freshness boundary; recovery: preserve the indeterminate operation and do not guess an alternate receiver", operation.Resource.Thread)
	}
	cutoff := operation.UpdatedAt
	if operation.ThreadAdoption != nil {
		receivingTime, receivingOK := ampUUIDv7ThreadTime(operation.ThreadAdoption.ReceivingThread)
		if !receivingOK || !receivingTime.After(provisionedTime) || operation.UpdatedAt.Before(receivingTime) {
			return "", fmt.Errorf("pending alternate receiver %s has invalid or inconsistent UUIDv7 freshness evidence; recovery: preserve the recorded adoption and do not widen its boundary", operation.ThreadAdoption.ReceivingThread)
		}
		cutoff = receivingTime
	}
	if cutoff.Before(provisionedTime) {
		return "", fmt.Errorf("operation freshness cutoff %s precedes provisioned thread %s; recovery: preserve the indeterminate operation and do not guess an alternate receiver", cutoff.Format(time.RFC3339Nano), operation.Resource.Thread)
	}
	allThreads, err := strictAmpThreadIDSet(true)
	if err != nil {
		return "", fmt.Errorf("list alternate receiver evidence for provisioned thread %s: %w", operation.Resource.Thread, err)
	}
	preDeliveryThreads := make(map[string]bool, len(allThreads))
	for candidate := range allThreads {
		candidateTime, candidateOK := ampUUIDv7ThreadTime(candidate)
		if !candidateOK || !candidateTime.After(provisionedTime) || candidateTime.After(cutoff) {
			preDeliveryThreads[candidate] = true
		}
	}
	fresh, activeThreads, err := scanFreshSpawnReceivingThreads(operation.Resource.Thread, message, workdir, preDeliveryThreads, allowTerminalLineEndingNormalization)
	if err != nil {
		return "", err
	}
	sort.Strings(fresh)
	if len(fresh) == 0 {
		return "", fmt.Errorf("no exact fresh alternate receiver was proved after provisioned thread %s; recovery: preserve the indeterminate operation and do not resubmit", operation.Resource.Thread)
	}
	if len(fresh) > 1 {
		return "", fmt.Errorf("multiple exact fresh alternate receivers %s were proved after provisioned thread %s; recovery: preserve all identities and do not guess ownership", strings.Join(fresh, ", "), operation.Resource.Thread)
	}
	if !activeThreads[fresh[0]] {
		return "", fmt.Errorf("exact fresh alternate receiver %s is archived; recovery: preserve the indeterminate operation and do not mutate thread state", fresh[0])
	}
	return fresh[0], nil
}

func verifyAlternateReceiverBeforeCleanup(provisioned, receiver, message, workdir string, preDeliveryThreads map[string]bool, allowTerminalLineEndingNormalization bool) error {
	empty, err := ampThreadHasNoMessages(provisioned)
	if err != nil {
		return fmt.Errorf("revalidate provisioned residue %s: %w", provisioned, err)
	}
	if !empty {
		return fmt.Errorf("provisioned residue %s is not empty; refusing alternate adoption cleanup", provisioned)
	}
	fresh, activeThreads, err := scanFreshSpawnReceivingThreads(provisioned, message, workdir, preDeliveryThreads, allowTerminalLineEndingNormalization)
	if err != nil {
		return err
	}
	sort.Strings(fresh)
	if len(fresh) != 1 || fresh[0] != receiver {
		return fmt.Errorf("alternate receiver evidence changed before cleanup: expected only %s, found %s", receiver, strings.Join(fresh, ", "))
	}
	if !activeThreads[receiver] {
		return fmt.Errorf("alternate receiver %s became inactive before cleanup", receiver)
	}
	return nil
}

func ampThreadHasNoMessages(thread string) (bool, error) {
	payload, err := ampThreadExport(thread)
	if err != nil {
		return false, err
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return false, fmt.Errorf("amp threads export for %s has no message list", thread)
	}
	return len(messages) == 0, nil
}

func ampThreadEmptyInWorkdir(thread, workdir string) (bool, bool, error) {
	payload, err := ampThreadExport(thread)
	if err != nil {
		return false, false, err
	}
	exportedThread, ok := payload["id"].(string)
	if !ok {
		return false, false, fmt.Errorf("amp threads export for %s has no thread identity", thread)
	}
	canonical, err := config.CanonicalThreadID(exportedThread)
	if err != nil || canonical != thread {
		return false, false, fmt.Errorf("amp threads export for %s returned a different thread identity", thread)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return false, false, fmt.Errorf("amp threads export for %s has no message list", thread)
	}
	trees, ok := threadEnvironmentTrees(payload["env"])
	return len(messages) == 0, ok && len(trees) == 1 && threadEnvironmentContainsWorkdir(payload["env"], workdir), nil
}

func ampThreadExport(thread string) (map[string]any, error) {
	out, err := exec.Command("amp", "threads", "export", thread).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("parse amp threads export for %s: %w", thread, err)
	}
	return payload, nil
}

func ampUUIDv7ThreadTime(thread string) (time.Time, bool) {
	if len(thread) != 38 || !strings.HasPrefix(thread, "T-") || thread[10] != '-' || thread[15] != '-' || thread[20] != '-' || thread[25] != '-' || thread[16] != '7' {
		return time.Time{}, false
	}
	hexID := strings.ReplaceAll(thread[2:], "-", "")
	decoded, err := hex.DecodeString(hexID)
	if err != nil || len(decoded) != 16 || decoded[8]&0xc0 != 0x80 {
		return time.Time{}, false
	}
	milliseconds := int64(decoded[0])<<40 | int64(decoded[1])<<32 | int64(decoded[2])<<24 | int64(decoded[3])<<16 | int64(decoded[4])<<8 | int64(decoded[5])
	return time.UnixMilli(milliseconds).UTC(), true
}

func resolveSpawnReceivingThread(boundThread, message, workdir string, preDeliveryThreads map[string]bool, allowTerminalLineEndingNormalization bool) (string, error) {
	deadline := time.Now().Add(spawnAssignmentVisibilityTimeout())
	var authoritative string
	for {
		boundContainsMessage, _, err := ampThreadContainsExactAssignment(boundThread, message, workdir, false, allowTerminalLineEndingNormalization)
		if err != nil {
			return "", fmt.Errorf("verify provisioned thread %s: %w; recovery: inspect thread %s and do not resubmit", boundThread, err, boundThread)
		}
		fresh, activeThreads, err := scanFreshSpawnReceivingThreads(boundThread, message, workdir, preDeliveryThreads, allowTerminalLineEndingNormalization)
		if err != nil {
			return "", err
		}
		if !boundContainsMessage {
			boundContainsMessage, _, err = ampThreadContainsExactAssignment(boundThread, message, workdir, false, allowTerminalLineEndingNormalization)
			if err != nil {
				return "", fmt.Errorf("recheck provisioned thread %s after fresh-thread discovery: %w; recovery: inspect thread %s and do not resubmit", boundThread, err, boundThread)
			}
			if boundContainsMessage {
				fresh, activeThreads, err = scanFreshSpawnReceivingThreads(boundThread, message, workdir, preDeliveryThreads, allowTerminalLineEndingNormalization)
				if err != nil {
					return "", err
				}
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

func scanFreshSpawnReceivingThreads(boundThread, message, workdir string, preDeliveryThreads map[string]bool, allowTerminalLineEndingNormalization bool) ([]string, map[string]bool, error) {
	allThreads, err := strictAmpThreadIDSet(true)
	if err != nil {
		return nil, nil, fmt.Errorf("list fresh receiving threads after delivery: %w; recovery: inspect provisioned thread %s and do not resubmit", err, boundThread)
	}
	activeThreads, err := strictAmpThreadIDSet(false)
	if err != nil {
		return nil, nil, fmt.Errorf("confirm fresh receiving threads are active: %w; recovery: inspect provisioned thread %s and do not resubmit", err, boundThread)
	}
	var fresh []string
	for candidate := range allThreads {
		if candidate == canonicalThreadID(boundThread) || preDeliveryThreads[candidate] {
			continue
		}
		matches, started, exportErr := ampThreadContainsExactAssignment(candidate, message, workdir, true, allowTerminalLineEndingNormalization)
		if exportErr != nil {
			return nil, nil, fmt.Errorf("verify fresh receiving candidate %s: %w; recovery: inspect %s and provisioned thread %s before choosing an identity", candidate, exportErr, candidate, boundThread)
		}
		if matches {
			if started {
				return nil, nil, fmt.Errorf("fresh receiving thread %s already started external work; recovery: inspect %s and provisioned thread %s, and do not create a second authoritative worker", candidate, candidate, boundThread)
			}
			fresh = append(fresh, candidate)
		}
	}
	return fresh, activeThreads, nil
}

func spawnAssignmentVisibilityTimeout() time.Duration {
	return 3 * spawnSubmitTimeout()
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
	matches := messagesContainProvisionedAssignment(payload["messages"], message, allowTerminalLineEndingNormalization)
	if !matches {
		return false, false, nil
	}
	if requireWorkdir && !threadEnvironmentContainsWorkdir(payload["env"], workdir) {
		return false, false, nil
	}
	return true, messagesContainWorkBeyondAssignment(payload["messages"], message, allowTerminalLineEndingNormalization), nil
}

func messagesContainWorkBeyondAssignment(value any, message string, allowTerminalLineEndingNormalization bool) bool {
	messages, ok := value.([]any)
	if !ok {
		return false
	}
	exactAssignments := 0
	for _, item := range messages {
		entry, ok := item.(map[string]any)
		if ok && entry["role"] == "user" && messageContentIsOnlyProvisionedAssignment(entry["content"], message, allowTerminalLineEndingNormalization) {
			exactAssignments++
			continue
		}
		return true
	}
	return exactAssignments != 1
}

func messageContentIsOnlyProvisionedAssignment(value any, message string, allowTerminalLineEndingNormalization bool) bool {
	switch value := value.(type) {
	case string:
		return provisionedAssignmentMatches(value, message, allowTerminalLineEndingNormalization)
	case []any:
		return len(value) == 1 && messageContentIsOnlyProvisionedAssignment(value[0], message, allowTerminalLineEndingNormalization)
	case map[string]any:
		text, ok := value["text"].(string)
		kind, _ := value["type"].(string)
		return ok && (kind == "" || kind == "text") && provisionedAssignmentMatches(text, message, allowTerminalLineEndingNormalization)
	default:
		return false
	}
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
		trimmed := strings.TrimSuffix(want, "\r\n")
		return trimmed != "" && got == trimmed
	case strings.HasSuffix(want, "\n"):
		trimmed := strings.TrimSuffix(want, "\n")
		return trimmed != "" && got == trimmed
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
	trees, ok := threadEnvironmentTrees(value)
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

func threadEnvironmentTrees(value any) ([]any, bool) {
	env, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	initial, ok := env["initial"].(map[string]any)
	if !ok {
		return nil, false
	}
	trees, ok := initial["trees"].([]any)
	return trees, ok
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
