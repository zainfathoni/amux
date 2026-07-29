package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

const maxSpawnPromptBytes = 1 << 20

var (
	spawnProcessIDs      = localProcessIDs
	spawnProcessArgs     = tmux.ProcessArgs
	spawnProcessIdentity = tmux.ProcessIdentity
	spawnProcessWorkdir  = tmux.ProcessWorkdir
	spawnCreateThread    = createLocalAmpThread
	spawnReadinessLimit  = 10 * time.Second
	spawnReadinessPoll   = 100 * time.Millisecond
	spawnReadinessSettle = 500 * time.Millisecond
	spawnInspectPaneByID = (tmux.Runner{}).RestartPaneByID
)

func (a app) workerSpawn(in invocation, dir config.Directory, env *result.Envelope) (*result.Envelope, error) {
	s := in.Selectors
	if s.RunnerID == "" || s.Workdir == "" || s.Workspace == "" || s.Window == "" || s.PromptFile == "" {
		return env, result.Request(errors.New("spawn requires --runner-id, --workdir, --workspace, --window, and --prompt-file <path|->"))
	}
	if len(in.Args) != 0 {
		return env, result.Request(errors.New("spawn accepts no positional prompt or identity arguments"))
	}
	if err := config.ValidateField("Amp runner ID", s.RunnerID); err != nil {
		return env, result.Request(err)
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
	if err := preflightPhysicalAmpRunner(s.RunnerID, workdir); err != nil {
		return env, result.Preflight(err)
	}
	row := config.Row{Workspace: s.Workspace, Window: s.Window, Workdir: workdir}
	memberships, err := preflightSpawnOwnership(dir, row, s.Group)
	if err != nil {
		return env, result.Preflight(err)
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
		out := result.Outcome{Resource: result.CommandResource(), Action: "spawn", Message: fmt.Sprintf("would use exact live local Amp runner %s at %s; run amp threads new --mode %s once in that cwd; create tmux worker %s/%s; paste the bounded prompt once and press Enter once; then persist the exact worker%s", s.RunnerID, workdir, mode, row.Workspace, row.Window, optionalGroupPlan(s.Group))}
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
			return env, postCreateSpawnError(exact, tmux.WindowPane{}, row, "create thread returned an error after an exact identity", createErr)
		}
		return env, result.Runtime(fmt.Errorf("amp threads new rejected before returning a thread: %w", createErr))
	}
	thread, err = config.CanonicalThreadID(thread)
	if err != nil {
		return env, result.Runtime(fmt.Errorf("amp threads new returned an invalid thread identity: %w", err))
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
	if err := runner.PasteLiteral(created.PaneID, prompt); err != nil {
		return env, postCreateSpawnError(thread, created, row, "paste prompt once", err)
	}
	if err := runner.SendEnter(created.PaneID); err != nil {
		return env, postCreateSpawnError(thread, created, row, "submit prompt once", err)
	}
	if _, err := config.Store(dir.WorkersPath(), row); err != nil {
		return env, postCreateSpawnError(thread, created, row, "persist exact worker", err)
	}
	if s.Group != "" {
		membership := config.GroupMembership{Group: s.Group, Thread: thread, Role: config.GroupMember}
		memberships = append(memberships, membership)
		if err := config.WriteGroups(dir.GroupsPath(), memberships); err != nil {
			return env, postCreateSpawnError(thread, created, row, "persist exact group member", err)
		}
	}
	out := workerOutcome(row, "spawn", "running")
	out.Message = "created one local thread, submitted one prompt, and persisted the exact worker"
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintln(a.stdout, thread)
	}
	return env, nil
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

func preflightPhysicalAmpRunner(runnerID, workdir string) error {
	pids, err := spawnProcessIDs()
	if err != nil {
		return fmt.Errorf("inspect local processes for Amp runner %q: %w", runnerID, err)
	}
	wantArgs := []string{"amp", "--no-tui", "--runner-id", runnerID}
	matchedID := false
	for _, pid := range pids {
		args, err := spawnProcessArgs(pid)
		if err != nil || len(args) != len(wantArgs) || filepath.Base(args[0]) != wantArgs[0] || args[1] != wantArgs[1] || args[2] != wantArgs[2] || args[3] != wantArgs[3] {
			continue
		}
		matchedID = true
		before, err := spawnProcessIdentity(pid)
		if err != nil || before == "" {
			continue
		}
		cwd, err := spawnProcessWorkdir(pid)
		if err != nil {
			continue
		}
		canonical, err := config.CanonicalWorkdir(cwd)
		revalidated, argsErr := spawnProcessArgs(pid)
		after, identityErr := spawnProcessIdentity(pid)
		if err == nil && canonical == workdir && identityErr == nil && before == after && argsErr == nil && strings.Join(args, "\x00") == strings.Join(revalidated, "\x00") {
			return nil
		}
	}
	if matchedID {
		return fmt.Errorf("exact live local Amp runner %q does not own canonical workdir %s", runnerID, workdir)
	}
	return fmt.Errorf("exact live local Amp runner %q was not found; expected argv amp --no-tui --runner-id %s", runnerID, runnerID)
}

func localProcessIDs() ([]int, error) {
	out, err := exec.Command("ps", "-ax", "-o", "pid=").Output()
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	pids := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			return nil, fmt.Errorf("unexpected process PID %q", field)
		}
		pids = append(pids, pid)
	}
	return pids, nil
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
			return nil, fmt.Errorf("workdir %s is already owned by amux Runner workspace %s; Amp-native runner ID and amux Runner identity are distinct", row.Workdir, existing.Workspace)
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
	identity := "not-created requested=" + row.Workspace + "/" + row.Window
	if pane.WindowID != "" || pane.PaneID != "" {
		identity = fmt.Sprintf("%s/%s window=%s pane=%s", row.Workspace, row.Window, pane.WindowID, pane.PaneID)
	}
	return result.Runtime(fmt.Errorf("post-create spawn stopped at %s: thread=%s tmux=%s; state was preserved without retry or cleanup: %w", step, thread, identity, cause))
}

func optionalGroupPlan(group string) string {
	if group == "" {
		return ""
	}
	return " and existing group " + group
}
