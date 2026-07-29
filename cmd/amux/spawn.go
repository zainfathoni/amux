package main

import (
	"bytes"
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
	spawnReadinessLimit  = 10 * time.Second
	spawnReadinessPoll   = 100 * time.Millisecond
	spawnReadinessSettle = 500 * time.Millisecond
	spawnInspectPaneByID = (tmux.Runner{}).RestartPaneByID
)

func (a app) workerSpawn(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.Workdir == "" || s.Workspace == "" || s.Window == "" || s.PromptFile == "" {
		return env, result.Request(errors.New("spawn requires --workdir, --workspace, --window, and --prompt-file <path|->"))
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
	row := config.Row{Workspace: s.Workspace, Window: s.Window, Workdir: workdir, AssignmentState: config.WorkerAssignmentRetainedIndeterminate}
	if !in.Options.DryRun {
		held, err := acquireMutationLock(in.Path)
		if err != nil {
			return env, result.Preflight(err)
		}
		defer held.Release()
	}
	memberships, err := preflightSpawnOwnership(dir, row, s.Group)
	if err != nil {
		return env, result.Preflight(err)
	}
	var groupAmpPath string
	if s.Group != "" {
		groupAmpPath, err = preflightGroupAmp()
		if err != nil {
			return env, result.Preflight(err)
		}
	}
	runner := tmux.Runner{}
	sessionExists, err := runner.SessionExists(row.Workspace)
	if err != nil {
		return env, result.Preflight(fmt.Errorf("inspect tmux workspace %s: %w", row.Workspace, err))
	}
	if sessionExists {
		windows, err := runner.WindowNames(row.Workspace)
		if err != nil {
			return env, result.Preflight(fmt.Errorf("inspect tmux workspace %s: %w", row.Workspace, err))
		}
		if tmux.WindowExists(windows, row.Window) {
			return env, result.Preflight(fmt.Errorf("tmux window %s/%s already exists", row.Workspace, row.Window))
		}
	}
	if in.Options.DryRun {
		out := result.Outcome{Resource: result.CommandResource(), Action: "spawn", Message: fmt.Sprintf("would locally run amp threads new --mode %s once in canonical cwd %s; create tmux worker %s/%s; attempt one bounded literal prompt paste and, only after paste success, one Enter; persist and report exact retained local ownership%s; then return retained-indeterminate because no supported delivery acknowledgement is available", mode, workdir, row.Workspace, row.Window, optionalGroupPlan(s.Group))}
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return env, nil
	}

	thread, createErr := spawnCreateThread(workdir, mode)
	thread = strings.TrimSpace(thread)
	if createErr != nil {
		if exact, identityErr := config.CanonicalThreadID(thread); identityErr == nil {
			row.Thread = exact
			return env, postCreateSpawnErrorBeforeTmux(exact, row, "create thread returned an error after an exact identity", errors.New("amp threads new returned nonzero after an exact thread identity"))
		}
		return env, creationIndeterminateSpawnError(row)
	}
	thread, err = config.CanonicalThreadID(thread)
	if err != nil {
		return env, creationIndeterminateSpawnError(row)
	}
	row.Thread = thread
	command := tmux.ContinueCommandWithEnv(workdir, thread, map[string]string{
		"AMUX_WORKSPACE": row.Workspace,
		"AMUX_SESSION":   row.Workspace,
		"AMUX_WINDOW":    row.Window,
		"AMUX_THREAD_ID": thread,
		"AMUX_WORKDIR":   workdir,
	})
	created, err := runner.NewWorkerPane(row.Workspace, row.Window, command, !sessionExists)
	if err != nil {
		return env, postCreateSpawnError(thread, created, row, "create exact tmux worker", err)
	}
	if err := waitForSpawnPane(created, row, command); err != nil {
		return env, postCreateSpawnError(thread, created, row, "wait for exact tmux worker readiness", err)
	}
	inputStatus := "one-paste-one-enter-completed"
	inputOut := spawnWorkerOutcome(row, created, "attempt-input", "input_attempt_completed")
	if err := runner.PasteLiteral(created.PaneID, prompt); err != nil {
		inputStatus = "paste-failed-enter-not-attempted"
		inputOut.Message = "the sole prompt paste command failed; Enter was not attempted; assignment delivery and task execution are unproven"
		inputOut.Error = &result.Failure{Kind: result.ErrorRuntime, Message: "local input attempt failed at the sole paste command; Enter was not attempted"}
		env.Failed = append(env.Failed, inputOut)
	} else if err := runner.SendEnter(created.PaneID); err != nil {
		inputStatus = "paste-completed-enter-failed-indeterminate"
		inputOut.Message = "the sole prompt paste completed locally and the sole Enter command failed; assignment delivery and task execution are unproven"
		inputOut.Error = &result.Failure{Kind: result.ErrorRuntime, Message: "local input attempt became indeterminate when the sole Enter command failed"}
		env.Failed = append(env.Failed, inputOut)
	} else {
		inputOut.Message = "one prompt paste and one Enter completed locally; assignment delivery and task execution are not acknowledged"
		env.Successful = append(env.Successful, inputOut)
	}
	workerOut := spawnWorkerOutcome(row, created, "persist-worker", "retained")
	workerOut.Message = "persisted exact local worker ownership; assignment delivery and task execution remain unproven"
	if _, err := config.Store(dir.WorkersPath(), row); err != nil {
		failure := retainedIndeterminateSpawnPhaseError(thread, created, row, inputStatus, nil, "persist exact worker", err)
		appendSpawnFailure(env, workerOut, failure)
		return env, failure
	}
	env.Successful = append(env.Successful, workerOut)
	if !in.Options.JSON {
		fmt.Fprintln(a.stdout, thread)
	}
	if s.Group != "" {
		membership := config.GroupMembership{Group: s.Group, Thread: thread, Role: config.GroupMember}
		memberships = append(memberships, membership)
		groupOut := groupOutcome(membership, "persist-group")
		groupOut.Message = "persisted exact group member intent"
		if err := config.WriteGroups(dir.GroupsPath(), memberships); err != nil {
			failure := retainedIndeterminateSpawnPhaseError(thread, created, row, inputStatus, []string{"persist-worker"}, "persist exact group member", err)
			appendSpawnFailure(env, groupOut, failure)
			return env, failure
		}
		env.Successful = append(env.Successful, groupOut)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\tlocal membership persisted\n", membership.Group, membership.Thread, membership.Role)
		}
		labelOut := groupOutcome(membership, "ensure-label")
		if _, err := a.ensureGroupLabel(env, labelOut, groupAmpPath, membership, in.Options.JSON); err != nil {
			failure := retainedIndeterminateSpawnPhaseError(thread, created, row, inputStatus, []string{"persist-worker", "persist-group"}, "add-only ensure exact group member label", err)
			if len(env.Failed) != 0 && env.Failed[len(env.Failed)-1].Error != nil {
				env.Failed[len(env.Failed)-1].Error.Message = failure.Error()
			}
			return env, failure
		}
	}
	completed := []string{"persist-worker"}
	if s.Group != "" {
		completed = append(completed, "persist-group", "ensure-label")
	}
	failure := retainedIndeterminateSpawnError(thread, created, row, inputStatus, completed)
	out := spawnWorkerOutcome(row, created, "acknowledge-delivery", "retained_indeterminate")
	out.Message = "input attempt and local ownership persistence completed, but assignment delivery was not acknowledged"
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
	env.Failed = append(env.Failed, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "RETAINED-INDETERMINATE\t%s\t%s/%s\t%s\t%s\tinput=%s\tcompleted=%s\tdelivery=unacknowledged\n", thread, row.Workspace, row.Window, created.WindowID, created.PaneID, inputStatus, strings.Join(completed, ","))
	}
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

func postCreateSpawnError(thread string, pane tmux.WindowPane, row config.Row, step string, cause error) error {
	identity := "creation-indeterminate requested=" + row.Workspace + "/" + row.Window
	if pane.WindowID != "" || pane.PaneID != "" {
		identity = fmt.Sprintf("%s/%s window=%s pane=%s", row.Workspace, row.Window, pane.WindowID, pane.PaneID)
	}
	return result.Runtime(fmt.Errorf("post-create spawn stopped at %s: thread=%s tmux=%s; state was preserved without retry or cleanup: %w", step, thread, identity, cause))
}

func postCreateSpawnErrorBeforeTmux(thread string, row config.Row, step string, cause error) error {
	return result.Runtime(fmt.Errorf("post-create spawn stopped at %s: thread=%s tmux=not-created requested=%s/%s; state was preserved without retry or cleanup: %w", step, thread, row.Workspace, row.Window, cause))
}

func creationIndeterminateSpawnError(row config.Row) error {
	return result.Runtime(fmt.Errorf("thread creation was indeterminate: thread=creation-indeterminate tmux=not-created requested=%s/%s; state was preserved without retry or cleanup", row.Workspace, row.Window))
}

func retainedIndeterminateSpawnError(thread string, pane tmux.WindowPane, row config.Row, inputStatus string, completed []string) error {
	return retainedIndeterminateSpawnPhaseError(thread, pane, row, inputStatus, completed, "", nil)
}

func retainedIndeterminateSpawnPhaseError(thread string, pane tmux.WindowPane, row config.Row, inputStatus string, completed []string, step string, cause error) error {
	phases := strings.Join(completed, ",")
	if phases == "" {
		phases = "none"
	}
	message := fmt.Sprintf("spawn retained-indeterminate: thread=%s tmux=%s/%s window=%s pane=%s; input-attempt=%s delivery-acknowledgement=unavailable completed-persistence-phases=%s", thread, row.Workspace, row.Window, pane.WindowID, pane.PaneID, inputStatus, phases)
	if step != "" {
		message += fmt.Sprintf(" stopped-at=%s: %v", step, cause)
	}
	message += "; assignment delivery and task execution are unproven; automatic retry, repaste, submit, cleanup, archive, search, reconciliation, and alternate receivers are prohibited; the composer may contain unsent prompt bytes"
	return result.Runtime(errors.New(message))
}

func spawnWorkerOutcome(row config.Row, pane tmux.WindowPane, action, localState string) result.Outcome {
	out := workerOutcome(row, action, "")
	out.Worker = workerPlacementDetails(row, localState)
	out.Worker.WindowID = pane.WindowID
	out.Worker.PaneID = pane.PaneID
	return out
}

func appendSpawnFailure(env *result.Envelope, out result.Outcome, failure error) {
	out.Message = "post-create phase failed; completed state was preserved"
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: failure.Error()}
	env.Failed = append(env.Failed, out)
}

func optionalGroupPlan(group string) string {
	if group == "" {
		return ""
	}
	return ", then persist and report existing group " + group + " and add-only ensure its Amp label"
}
