package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

const maxSpawnPromptBytes = 1 << 20

var (
	spawnCreateThread    = createLocalAmpThread
	spawnStoreAssignment = config.StoreSpawnAssignment
	spawnReadinessLimit  = 10 * time.Second
	spawnReadinessPoll   = 100 * time.Millisecond
	spawnReadinessSettle = 500 * time.Millisecond
	spawnInspectPaneByID = (tmux.Runner{}).RestartPaneByID
)

func (a app) workerSpawn(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.Workdir == "" || s.Workspace == "" || s.Window == "" || s.PromptFile == "" || s.AssignmentPhase == "" {
		return env, result.Request(errors.New("spawn requires --assignment-phase, --workdir, --workspace, --window, and --prompt-file <path|->"))
	}
	if len(in.Args) != 0 {
		return env, result.Request(errors.New("spawn accepts no positional prompt or identity arguments"))
	}
	mode := s.Mode
	if mode == "" {
		mode = "medium"
	}
	prompt, err := a.readSpawnPrompt(s.PromptFile)
	if err != nil {
		return env, result.Request(err)
	}
	workdir, err := config.CanonicalWorkdir(s.Workdir)
	if err != nil {
		return env, result.Preflight(err)
	}
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		return env, result.Preflight(fmt.Errorf("missing workdir: %s", workdir))
	}
	if !filepath.IsAbs(s.Workdir) || s.Workdir != workdir {
		return env, result.Preflight(fmt.Errorf("--workdir must be the canonical existing path %s", workdir))
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(prompt)))
	row := config.Row{Workspace: s.Workspace, Window: s.Window, Workdir: workdir}
	request := config.SpawnAssignmentRecord{Workspace: row.Workspace, Window: row.Window, Workdir: workdir, Mode: mode, Group: s.Group, PromptDigest: digest}
	if s.Thread != "" {
		request.Thread = s.Thread
	}
	if s.NativeCapability != "existing-thread-message-v1" {
		failure := result.Preflight(errors.New("native existing-thread message capability must be confirmed before spawn mutation with --native-capability existing-thread-message-v1"))
		out := result.Outcome{Resource: result.CommandResource(), Action: "spawn-preflight", Message: "creation rejected before mutation because native assignment is unsupported", Assignment: spawnAssignmentDetails("rejected", "absent", "absent", "unsupported", digest, "")}
		out.Error = &result.Failure{Kind: result.ErrorPreflight, Message: failure.Error()}
		env.Failed = append(env.Failed, out)
		return env, failure
	}
	switch s.AssignmentPhase {
	case "prepare":
		if s.Thread != "" || s.AssignmentOutcome != "" || s.LatestCursor != "" {
			return env, result.Request(errors.New("spawn prepare does not accept --thread, --assignment-outcome, or --latest-cursor"))
		}
	case "arm":
		if s.Thread == "" || s.AssignmentOutcome != "" || s.LatestCursor != "" {
			return env, result.Request(errors.New("spawn arm requires --thread and does not accept --assignment-outcome or --latest-cursor"))
		}
	case "finalize":
		if s.Thread == "" || s.AssignmentOutcome == "" {
			return env, result.Request(errors.New("spawn finalize requires --thread and --assignment-outcome"))
		}
		if s.AssignmentOutcome == string(config.SpawnAssignmentAuthenticatedAccepted) && s.LatestCursor == "" {
			return env, result.Request(errors.New("authenticated_accepted finalization requires --latest-cursor from native tool success"))
		}
		if s.AssignmentOutcome != string(config.SpawnAssignmentAuthenticatedAccepted) && s.LatestCursor != "" {
			return env, result.Request(errors.New("--latest-cursor is valid only with authenticated_accepted"))
		}
	default:
		return env, result.Request(errors.New("--assignment-phase must be prepare, arm, or finalize"))
	}
	if !in.Options.DryRun {
		held, err := acquireMutationLock(in.Path)
		if err != nil {
			return env, result.Preflight(err)
		}
		defer held.Release()
	}
	var memberships []config.GroupMembership
	var groupAmpPath string
	if s.AssignmentPhase == "prepare" {
		memberships, err = preflightSpawnOwnership(dir, row, s.Group)
		if err != nil {
			return env, result.Preflight(err)
		}
		if s.Group != "" {
			groupAmpPath, err = preflightGroupAmp()
			if err != nil {
				return env, result.Preflight(err)
			}
		}
		runner := tmux.Runner{}
		sessionExists, inspectErr := runner.SessionExists(row.Workspace)
		if inspectErr != nil {
			return env, result.Preflight(fmt.Errorf("inspect tmux workspace %s: %w", row.Workspace, inspectErr))
		}
		if sessionExists {
			windows, inspectErr := runner.WindowNames(row.Workspace)
			if inspectErr != nil {
				return env, result.Preflight(fmt.Errorf("inspect tmux workspace %s: %w", row.Workspace, inspectErr))
			}
			if tmux.WindowExists(windows, row.Window) {
				return env, result.Preflight(fmt.Errorf("tmux window %s/%s already exists", row.Workspace, row.Window))
			}
		}
	}
	if in.Options.DryRun {
		out := result.Outcome{Resource: result.CommandResource(), Action: "spawn-" + s.AssignmentPhase, Message: fmt.Sprintf("would execute durable native assignment phase %s for %s/%s; execution remains unproven", s.AssignmentPhase, row.Workspace, row.Window), Assignment: spawnAssignmentDetails("not_attempted", "absent", "absent", "not_attempted", digest, "")}
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}

	switch s.AssignmentPhase {
	case "prepare":
		return a.prepareNativeSpawn(in, dir, env, request, row, memberships, groupAmpPath)
	case "arm":
		return a.armNativeSpawn(in, dir, env, request, row)
	default:
		return a.finalizeNativeSpawn(in, dir, env, request, row)
	}
}

func (a app) prepareNativeSpawn(in invocation, dir config.Directory, env *result.Envelope, request config.SpawnAssignmentRecord, row config.Row, memberships []config.GroupMembership, groupAmpPath string) (*result.Envelope, error) {
	records, err := config.LoadSpawnAssignments(dir.SpawnAssignmentsPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, existing := range records {
		if existing.Workspace == request.Workspace && existing.Window == request.Window {
			return env, result.Preflight(fmt.Errorf("spawn assignment %s/%s already exists at phase %s; creation and messaging will not be retried", request.Workspace, request.Window, existing.Phase))
		}
	}
	request.Phase = config.SpawnAssignmentCreationArmed
	request.Outcome = config.SpawnAssignmentNotAttempted
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), request); err != nil {
		return env, result.Runtime(fmt.Errorf("durably arm exact thread creation before mutation: %w", err))
	}

	thread, createErr := spawnCreateThread(request.Workdir, request.Mode)
	thread = strings.TrimSpace(thread)
	exact, identityErr := config.CanonicalThreadID(thread)
	if createErr != nil || identityErr != nil {
		resource := result.CommandResource()
		identity := "no exact thread identity was returned"
		if identityErr == nil {
			resource.Thread = exact
			identity = "exact returned identity=" + exact
		}
		failure := result.Runtime(fmt.Errorf("thread creation is indeterminate for %s/%s (%s); durable creation arm prohibits retry, search, cleanup, archive, or alternate creation", request.Workspace, request.Window, identity))
		out := result.Outcome{Resource: resource, Action: "spawn-prepare", Message: failure.Error(), Assignment: spawnAssignmentDetails("indeterminate", "absent", "absent", "not_attempted", request.PromptDigest, "")}
		out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
		env.Failed = append(env.Failed, out)
		return env, failure
	}
	request.Thread = exact
	request.Phase = config.SpawnAssignmentPrepared
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), request); err != nil {
		return env, result.Runtime(fmt.Errorf("exact thread %s was allocated but prepare finalization failed; creation is indeterminate and must not be retried: %w", exact, err))
	}
	row.Thread = exact
	row.AssignmentState = config.WorkerAssignmentNativeNotAttempted
	if _, err := config.Store(dir.WorkersPath(), row); err != nil {
		return env, result.Runtime(fmt.Errorf("exact thread %s was allocated and prepared but local ownership persistence failed; assignment was not attempted: %w", exact, err))
	}
	if request.Group != "" {
		membership := config.GroupMembership{Group: request.Group, Thread: exact, Role: config.GroupMember}
		memberships = append(memberships, membership)
		if err := config.WriteGroups(dir.GroupsPath(), memberships); err != nil {
			return env, result.Runtime(fmt.Errorf("exact thread %s was prepared and retained but group persistence failed; assignment was not attempted: %w", exact, err))
		}
		labelOut := groupOutcome(membership, "ensure-label")
		if _, err := a.ensureGroupLabel(env, labelOut, groupAmpPath, membership, in.Options.JSON); err != nil {
			return env, result.Runtime(fmt.Errorf("exact thread %s was prepared and retained but group label ensure failed; assignment was not attempted: %w", exact, err))
		}
	}
	out := spawnWorkerOutcome(row, tmux.WindowPane{}, "spawn-prepare", "retained")
	out.Message = "exact local thread created once and ownership retained; assignment not attempted; use the exact returned thread only"
	out.Assignment = spawnAssignmentDetails("exact_thread_allocated", "retained", "absent", "not_attempted", request.PromptDigest, "")
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "SPAWN-PREPARED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=retained\tlocal-presentation=absent\tassignment=not_attempted\texecution=unproven\tprompt-digest=%s\n", exact, request.PromptDigest)
	}
	return env, nil
}

func (a app) armNativeSpawn(in invocation, dir config.Directory, env *result.Envelope, request config.SpawnAssignmentRecord, row config.Row) (*result.Envelope, error) {
	record, err := exactSpawnAssignment(dir.SpawnAssignmentsPath(), request)
	if err != nil {
		return env, result.Preflight(err)
	}
	if record.Phase != config.SpawnAssignmentPrepared {
		return env, result.Preflight(fmt.Errorf("spawn assignment for exact thread %s is phase %s; native messaging will not be attempted", request.Thread, record.Phase))
	}
	record.Phase = config.SpawnAssignmentArmed
	record.Outcome = config.SpawnAssignmentIndeterminate
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), record); err != nil {
		return env, result.Runtime(fmt.Errorf("arm native assignment for exact thread %s: %w", request.Thread, err))
	}
	row.Thread = request.Thread
	row.AssignmentState = config.WorkerAssignmentNativeIndeterminate
	if _, err := config.Store(dir.WorkersPath(), row); err != nil {
		return env, result.Runtime(fmt.Errorf("assignment for exact thread %s was durably armed and is indeterminate, but worker display state failed to persist; the message must not be attempted: %w", request.Thread, err))
	}
	out := spawnWorkerOutcome(row, tmux.WindowPane{}, "spawn-arm", "retained")
	out.Message = "native assignment armed for the exact thread; one coordinator message may now be attempted; interruption is indeterminate and never retryable"
	out.Assignment = spawnAssignmentDetails("exact_thread_allocated", "retained", "absent", "indeterminate", request.PromptDigest, "")
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "SPAWN-ARMED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=retained\tlocal-presentation=absent\tassignment=indeterminate\texecution=unproven\n", request.Thread)
	}
	return env, nil
}

func (a app) finalizeNativeSpawn(in invocation, dir config.Directory, env *result.Envelope, request config.SpawnAssignmentRecord, row config.Row) (*result.Envelope, error) {
	record, err := exactSpawnAssignment(dir.SpawnAssignmentsPath(), request)
	if err != nil {
		return env, result.Preflight(err)
	}
	if record.Phase != config.SpawnAssignmentArmed {
		return env, result.Preflight(fmt.Errorf("spawn assignment for exact thread %s is phase %s; finalization cannot change it", request.Thread, record.Phase))
	}
	outcome := config.SpawnAssignmentOutcome(in.Selectors.AssignmentOutcome)
	switch outcome {
	case config.SpawnAssignmentRejected, config.SpawnAssignmentIndeterminate, config.SpawnAssignmentAuthenticatedAccepted:
	default:
		return env, result.Request(errors.New("--assignment-outcome must be rejected, indeterminate, or authenticated_accepted"))
	}
	record.Phase = config.SpawnAssignmentFinalized
	record.Outcome = outcome
	record.ReceiptCursor = in.Selectors.LatestCursor
	if err := spawnStoreAssignment(dir.SpawnAssignmentsPath(), record); err != nil {
		failure := result.Runtime(fmt.Errorf("native result for exact thread %s could not be durably finalized; assignment remains indeterminate and must not be resent: %w", request.Thread, err))
		resource := result.CommandResource()
		resource.Thread = request.Thread
		out := result.Outcome{Resource: resource, Action: "spawn-finalize", Message: failure.Error(), Assignment: spawnAssignmentDetails("exact_thread_allocated", "retained", "absent", "indeterminate", request.PromptDigest, "")}
		out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
		env.Failed = append(env.Failed, out)
		return env, failure
	}
	row.Thread = request.Thread
	switch outcome {
	case config.SpawnAssignmentRejected:
		row.AssignmentState = config.WorkerAssignmentNativeRejected
	case config.SpawnAssignmentIndeterminate:
		row.AssignmentState = config.WorkerAssignmentNativeIndeterminate
	default:
		row.AssignmentState = config.WorkerAssignmentAuthenticatedAccepted
	}
	if _, err := config.Store(dir.WorkersPath(), row); err != nil {
		return appendFinalizedPresentationFailure(env, row, tmux.WindowPane{}, outcome, request.PromptDigest, "persist worker assignment display", err)
	}

	runner := tmux.Runner{}
	sessionExists, err := runner.SessionExists(row.Workspace)
	if err != nil {
		return appendFinalizedPresentationFailure(env, row, tmux.WindowPane{}, outcome, request.PromptDigest, "inspect tmux workspace", err)
	}
	command := tmux.ContinueCommandWithEnv(row.Workdir, row.Thread, map[string]string{
		"AMUX_WORKSPACE": row.Workspace, "AMUX_SESSION": row.Workspace, "AMUX_WINDOW": row.Window,
		"AMUX_THREAD_ID": row.Thread, "AMUX_WORKDIR": row.Workdir,
	})
	created, err := runner.NewWorkerPane(row.Workspace, row.Window, command, !sessionExists)
	if err != nil {
		return appendFinalizedPresentationFailure(env, row, created, outcome, request.PromptDigest, "create exact presentation-only tmux worker", err)
	}
	if err := waitForSpawnPane(created, row, command); err != nil {
		return appendFinalizedPresentationFailure(env, row, created, outcome, request.PromptDigest, "verify exact presentation-only tmux worker", err)
	}
	receipt := ""
	if outcome == config.SpawnAssignmentAuthenticatedAccepted {
		receipt = "native_latest_cursor"
	}
	out := spawnWorkerOutcome(row, created, "spawn-finalize", "exact_client_established")
	out.Message = fmt.Sprintf("assignment=%s finalized for the exact thread; local client established for presentation only; execution remains unproven", outcome)
	out.Assignment = spawnAssignmentDetails("exact_thread_allocated", "retained", "exact_client_established", string(outcome), request.PromptDigest, receipt)
	if outcome == config.SpawnAssignmentAuthenticatedAccepted {
		env.Successful = append(env.Successful, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "SPAWN-FINALIZED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=retained\tlocal-presentation=exact_client_established\tassignment=%s\texecution=unproven\treceipt=native_latest_cursor\n", request.Thread, outcome)
		}
		return env, nil
	}
	failure := result.Runtime(fmt.Errorf("assignment=%s for exact thread %s; local presentation was established, execution remains unproven, and no message retry or TUI fallback is permitted", outcome, request.Thread))
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
	env.Failed = append(env.Failed, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "SPAWN-FINALIZED\tthread=%s\tcreation=exact_thread_allocated\tlocal-ownership=retained\tlocal-presentation=exact_client_established\tassignment=%s\texecution=unproven\n", request.Thread, outcome)
	}
	return env, failure
}

func exactSpawnAssignment(path string, request config.SpawnAssignmentRecord) (config.SpawnAssignmentRecord, error) {
	records, err := config.LoadSpawnAssignments(path)
	if err != nil {
		return config.SpawnAssignmentRecord{}, err
	}
	for _, record := range records {
		if record.Workspace != request.Workspace || record.Window != request.Window {
			continue
		}
		if record.Thread != request.Thread || record.Workdir != request.Workdir || record.Mode != request.Mode || record.Group != request.Group || record.PromptDigest != request.PromptDigest {
			return config.SpawnAssignmentRecord{}, errors.New("spawn assignment boundary does not exactly match the prepared thread, cwd, mode, group, and prompt digest")
		}
		return record, nil
	}
	return config.SpawnAssignmentRecord{}, fmt.Errorf("no prepared spawn assignment exists for %s/%s", request.Workspace, request.Window)
}

func spawnAssignmentDetails(creation, ownership, presentation, assignment, digest, receipt string) *result.SpawnAssignmentDetails {
	return &result.SpawnAssignmentDetails{Creation: creation, LocalOwnership: ownership, LocalPresentation: presentation, Assignment: assignment, Execution: "unproven", PromptDigest: digest, Receipt: receipt}
}

func appendFinalizedPresentationFailure(env *result.Envelope, row config.Row, pane tmux.WindowPane, outcome config.SpawnAssignmentOutcome, digest, step string, cause error) (*result.Envelope, error) {
	failure := result.Runtime(fmt.Errorf("assignment=%s was durably finalized for exact thread %s before presentation failed at %s; acceptance/rejection truth is preserved, execution is unproven, and the message must not be resent: %w", outcome, row.Thread, step, cause))
	out := spawnWorkerOutcome(row, pane, "spawn-finalize-presentation", "presentation_failed")
	out.Message = "durable assignment truth preserved before local presentation failure"
	receipt := ""
	if outcome == config.SpawnAssignmentAuthenticatedAccepted {
		receipt = "native_latest_cursor"
	}
	out.Assignment = spawnAssignmentDetails("exact_thread_allocated", "retained", "presentation_failed", string(outcome), digest, receipt)
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
	env.Failed = append(env.Failed, out)
	return env, failure
}

func (a app) readSpawnPrompt(path string) (string, error) {
	var reader io.Reader
	if path == "-" {
		reader = a.stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return "", fmt.Errorf("read --prompt-file: %w", err)
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxSpawnPromptBytes+1))
	if err != nil {
		return "", fmt.Errorf("read --prompt-file: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("spawn prompt must not be empty")
	}
	if len(data) > maxSpawnPromptBytes {
		return "", fmt.Errorf("spawn prompt exceeds %d bytes", maxSpawnPromptBytes)
	}
	return string(data), nil
}

func preflightSpawnOwnership(dir config.Directory, row config.Row, group string) ([]config.GroupMembership, error) {
	rows, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return nil, err
	}
	for _, existing := range rows {
		existingWorkdir, _ := config.CanonicalWorkdir(existing.Workdir)
		if existing.Workspace == row.Workspace && existing.Window == row.Window {
			return nil, fmt.Errorf("worker window %s/%s is already configured for thread %s", row.Workspace, row.Window, existing.Thread)
		}
		if existingWorkdir == row.Workdir {
			return nil, fmt.Errorf("workdir %s is already owned by worker %s/%s", row.Workdir, existing.Workspace, existing.Window)
		}
	}
	runners, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return nil, err
	}
	for _, existing := range runners {
		if existing.Workdir == row.Workdir {
			return nil, fmt.Errorf("workdir %s is already owned by amux Runner workspace %s", row.Workdir, existing.Workspace)
		}
	}
	memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil {
		return nil, err
	}
	if group != "" {
		exists := false
		for _, membership := range memberships {
			exists = exists || membership.Group == group
		}
		if !exists {
			return nil, fmt.Errorf("group %s does not exist; spawn accepts only one existing group", group)
		}
	}
	return memberships, nil
}

func createLocalAmpThread(workdir, mode string) (string, error) {
	cmd := exec.Command("amp", "threads", "new", "--mode", mode)
	cmd.Dir = workdir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return strings.TrimSpace(string(out)), fmt.Errorf("%w: %s", err, message)
		}
		return strings.TrimSpace(string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}

func waitForSpawnPane(created tmux.WindowPane, row config.Row, command string) error {
	deadline := time.Now().Add(spawnReadinessLimit)
	var readySince time.Time
	for {
		pane, err := spawnInspectPaneByID(created.PaneID)
		if err == nil && pane.Session == row.Workspace && pane.Window == row.Window && pane.WindowID == created.WindowID && pane.PaneID == created.PaneID && !pane.Dead && pane.Command == "amp" && normalizedTmuxStartCommand(pane.StartCommand) == normalizedTmuxStartCommand(command) && runnerPaneWorkdirMatches(pane.Path, row.Workdir) {
			if readySince.IsZero() {
				readySince = time.Now()
			}
			if time.Since(readySince) >= spawnReadinessSettle {
				return nil
			}
		} else {
			readySince = time.Time{}
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				return err
			}
			return errors.New("exact pane did not become ready before timeout")
		}
		time.Sleep(spawnReadinessPoll)
	}
}

func spawnWorkerOutcome(row config.Row, pane tmux.WindowPane, action, localState string) result.Outcome {
	out := workerOutcome(row, action, "")
	out.Worker = workerPlacementDetails(row, localState)
	out.Worker.WindowID = pane.WindowID
	out.Worker.PaneID = pane.PaneID
	return out
}
