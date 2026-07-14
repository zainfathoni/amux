package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

var (
	runnerStartupTimeout = 750 * time.Millisecond
	runnerPollInterval   = 50 * time.Millisecond
	runnerProcessAlive   = processAlive
)

type runnerPaneState string

const (
	runnerPaneAbsent    runnerPaneState = "absent"
	runnerPaneExact     runnerPaneState = "exact"
	runnerPaneConflict  runnerPaneState = "conflict"
	runnerPaneAmbiguous runnerPaneState = "ambiguous"
)

type runnerInspection struct {
	state runnerPaneState
	pane  tmux.WindowPane
}

func (a app) executeRunner(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if in.Selectors.Current {
		runner := tmux.Runner{}
		workdir, err := runner.CurrentWorkdir()
		if err != nil {
			return &env, result.Preflight(fmt.Errorf("resolve --current runner: %w", err))
		}
		in.Selectors.Current = false
		in.Selectors.Workdir, err = config.CanonicalWorkdir(workdir)
		if err != nil {
			return &env, result.Preflight(err)
		}
		if in.Command.Name == "pin" {
			in.Selectors.Workspace, err = runner.CurrentSession()
			if err != nil {
				return &env, result.Preflight(fmt.Errorf("resolve --current runner workspace: %w", err))
			}
		}
	}

	rows, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return &env, result.Preflight(err)
	}
	rows = selectRunnerRows(rows, in.Selectors)
	if in.Command.Name == "list" {
		for _, row := range rows {
			env.Successful = append(env.Successful, runnerOutcome(row, "list", row.Workspace))
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "%s\t%s\n", row.Workspace, row.Workdir)
			}
		}
		return &env, nil
	}
	if in.Command.Name == "pin" {
		return a.runnerPinV1(in, dir, rows, &env)
	}
	if len(rows) == 0 {
		if (in.Command.Name == "unpin" || in.Command.Name == "remove" || in.Command.Name == "reconcile") && in.Selectors.Workdir != "" {
			resource, _ := result.RunnerResource(in.Selectors.Workdir)
			message := "already in desired state"
			if in.Command.Name == "reconcile" {
				message += staleAmpPIDDiagnostic(in.Selectors.Workdir)
			}
			env.Skipped = append(env.Skipped, result.Outcome{Resource: resource, Action: in.Command.Name, Message: message})
			return &env, nil
		}
		if in.Selectors.All && (in.Command.Name == "park" || in.Command.Name == "restart" || in.Command.Name == "remove" || in.Command.Name == "reconcile") {
			env.Skipped = append(env.Skipped, result.Outcome{Resource: result.CommandResource(), Action: in.Command.Name, Message: "already in desired state"})
			return &env, nil
		}
		return &env, result.Preflight(errors.New("no configured runner matches the selector"))
	}

	if in.Command.Name == "launch" || in.Command.Name == "restart" {
		if err := preflightRunnerWindowCollisions(dir); err != nil {
			return &env, result.Preflight(err)
		}
	}
	inspections := make(map[string]runnerInspection, len(rows))
	for _, row := range rows {
		if !runnerCommandNeedsTmux(in.Command.Name) {
			continue
		}
		inspection, inspectErr := inspectRunner(row)
		if inspectErr != nil {
			return &env, result.Preflight(inspectErr)
		}
		if in.Command.Name != "doctor" && (inspection.state == runnerPaneConflict || inspection.state == runnerPaneAmbiguous) {
			return &env, result.Preflight(fmt.Errorf("runner %s has %s tmux identity in workspace %s", row.Workdir, inspection.state, row.Workspace))
		}
		inspections[row.Workdir] = inspection
	}
	for _, row := range rows {
		inspection := inspections[row.Workdir]
		if in.Command.Name == "launch" || in.Command.Name == "restart" && inspection.state == runnerPaneExact {
			if err := requireLockedWorktree(row.Workdir); err != nil {
				return &env, result.Preflight(err)
			}
		}
		if in.Command.Name == "reconcile" && inspection.state != runnerPaneAbsent {
			return &env, result.Preflight(fmt.Errorf("runner %s still has %s runtime ownership; reconcile will not remove its configuration", row.Workdir, inspection.state))
		}
	}

	for _, row := range rows {
		out := runnerOutcome(row, in.Command.Name, "")
		inspection := inspections[row.Workdir]
		pidDiagnostic := ""
		if in.Command.Name == "reconcile" {
			pidDiagnostic = staleAmpPIDDiagnostic(row.Workdir)
		}
		if in.Command.Name == "doctor" {
			lockState := "locked"
			if lockErr := requireLockedWorktree(row.Workdir); lockErr != nil {
				lockState = lockErr.Error()
			}
			out.Message = fmt.Sprintf("local=%s worktree=%s%s", inspection.state, lockState, staleAmpPIDDiagnostic(row.Workdir))
			env.Successful = append(env.Successful, out)
			continue
		}
		if in.Command.Name == "launch" && inspection.state == runnerPaneExact {
			out.Message = "already running"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if (in.Command.Name == "park" || in.Command.Name == "restart") && inspection.state == runnerPaneAbsent {
			out.Message = "already stopped"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		if in.Command.Name == "reconcile" {
			valid := requireLockedWorktree(row.Workdir) == nil
			if valid {
				if pidDiagnostic == "" {
					out.Message = "already in desired state"
				} else {
					out.Message = "already in desired state" + pidDiagnostic
				}
				env.Skipped = append(env.Skipped, out)
				continue
			}
		}
		if in.Options.DryRun {
			out.Message = strings.TrimPrefix(pidDiagnostic, "; ")
			env.Planned = append(env.Planned, out)
			continue
		}

		var runtimeErr error
		switch in.Command.Name {
		case "unpin":
			_, runtimeErr = config.RemoveRunnerWorkdir(dir.RunnersPath(), row.Workdir)
		case "park":
			runtimeErr = stopRunner(row, inspection)
		case "restart":
			runtimeErr = stopRunner(row, inspection)
			if runtimeErr == nil {
				row.Window = config.RunnerWindow(row.Workdir)
				row.LegacyWindow = false
				_, runtimeErr = launchRunner(row)
			}
		case "launch":
			_, runtimeErr = launchRunner(row)
		case "remove":
			if inspection.state == runnerPaneExact {
				runtimeErr = stopRunner(row, inspection)
			}
			if runtimeErr == nil {
				_, runtimeErr = config.RemoveRunnerWorkdir(dir.RunnersPath(), row.Workdir)
			}
		case "reconcile":
			_, runtimeErr = config.RemoveRunnerWorkdir(dir.RunnersPath(), row.Workdir)
			out.Message = strings.TrimPrefix(pidDiagnostic, "; ")
		}
		if runtimeErr != nil {
			out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: runtimeErr.Error()}
			env.Failed = append(env.Failed, out)
			continue
		}
		env.Successful = append(env.Successful, out)
	}
	if len(env.Failed) > 0 {
		return &env, result.Runtime(errors.New("one or more runner operations failed"))
	}
	return &env, nil
}

func runnerCommandNeedsTmux(name string) bool {
	switch name {
	case "launch", "park", "restart", "remove", "doctor", "reconcile":
		return true
	}
	return false
}

func preflightRunnerWindowCollisions(dir config.Directory) error {
	rows, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return err
	}
	windows := make(map[string]string, len(rows))
	for _, row := range rows {
		row.Window = config.RunnerWindow(row.Workdir)
		key := row.Workspace + "\x00" + row.Window
		if owner, exists := windows[key]; exists && owner != row.Workdir {
			return fmt.Errorf("derived runner window %s in workspace %s collides for workdirs %s and %s", row.Window, row.Workspace, owner, row.Workdir)
		}
		windows[key] = row.Workdir
	}
	workers, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if workdir, exists := windows[worker.Workspace+"\x00"+worker.Window]; exists {
			return fmt.Errorf("derived runner window %s in workspace %s for %s collides with configured worker", worker.Window, worker.Workspace, workdir)
		}
	}
	return nil
}

func (a app) runnerPinV1(in invocation, dir config.Directory, selected []config.RunnerRow, env *result.Envelope) (*result.Envelope, error) {
	if in.Selectors.Workspace == "" || in.Selectors.Workdir == "" {
		return env, result.Request(errors.New("runner pin requires --workspace and --workdir"))
	}
	row := config.RunnerRow{Workspace: in.Selectors.Workspace, Workdir: in.Selectors.Workdir, Window: config.RunnerWindow(in.Selectors.Workdir)}
	out := runnerOutcome(row, "pin", "")
	if err := requireLockedWorktree(row.Workdir); err != nil {
		return env, result.Preflight(err)
	}
	all, err := config.LoadRunnersReadOnly(dir.RunnersPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, existing := range all {
		if existing.Workdir == row.Workdir {
			if existing.Workspace == row.Workspace {
				out.Message = "already pinned"
				env.Skipped = append(env.Skipped, out)
				return env, nil
			}
			return env, result.Preflight(fmt.Errorf("runner workdir %s is already configured in workspace %s", row.Workdir, existing.Workspace))
		}
		if existing.Workspace == row.Workspace && existing.Window == row.Window {
			return env, result.Preflight(fmt.Errorf("derived runner window %s collides with workdir %s", row.Window, existing.Workdir))
		}
	}
	workers, err := config.LoadReadOnly(dir.WorkersPath())
	if err != nil {
		return env, result.Preflight(err)
	}
	for _, worker := range workers {
		if worker.Workspace == row.Workspace && worker.Window == row.Window {
			return env, result.Preflight(fmt.Errorf("derived runner window %s collides with configured worker", row.Window))
		}
	}
	inspection, err := inspectRunner(row)
	if err != nil {
		return env, result.Preflight(err)
	}
	if inspection.state != runnerPaneAbsent {
		return env, result.Preflight(fmt.Errorf("derived runner window %s already exists in workspace %s", row.Window, row.Workspace))
	}
	if in.Options.DryRun {
		env.Planned = append(env.Planned, out)
		return env, nil
	}
	_, err = config.StoreRunner(dir.RunnersPath(), row)
	if err != nil {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, out)
	return env, nil
}

func selectRunnerRows(rows []config.RunnerRow, s selectors) []config.RunnerRow {
	selected := make([]config.RunnerRow, 0, len(rows))
	for _, row := range rows {
		if s.Workspace != "" && row.Workspace != s.Workspace || s.Workdir != "" && row.Workdir != s.Workdir {
			continue
		}
		selected = append(selected, row)
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Workspace == selected[j].Workspace {
			return selected[i].Workdir < selected[j].Workdir
		}
		return selected[i].Workspace < selected[j].Workspace
	})
	return selected
}

func runnerOutcome(row config.RunnerRow, action, message string) result.Outcome {
	id, _ := result.RunnerResource(row.Workdir)
	return result.Outcome{Resource: id, Action: action, Message: message}
}

func requireLockedWorktree(workdir string) error {
	stat, err := os.Stat(workdir)
	if err != nil || !stat.IsDir() {
		return fmt.Errorf("runner workdir %s is missing", workdir)
	}
	top, err := exec.Command("git", "-C", workdir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return fmt.Errorf("runner workdir %s is not a Git worktree", workdir)
	}
	topdir, err := config.CanonicalWorkdir(strings.TrimSpace(string(top)))
	if err != nil || topdir != workdir {
		return fmt.Errorf("runner workdir %s must be the Git worktree root", workdir)
	}
	out, err := exec.Command("git", "-C", workdir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("inspect Git worktree lock for %s: %w", workdir, err)
	}
	for _, record := range strings.Split(strings.TrimSpace(string(out)), "\n\n") {
		lines := strings.Split(record, "\n")
		if len(lines) == 0 || !strings.HasPrefix(lines[0], "worktree ") {
			continue
		}
		candidate, canonicalErr := config.CanonicalWorkdir(strings.TrimPrefix(lines[0], "worktree "))
		if canonicalErr != nil || candidate != workdir {
			continue
		}
		for _, line := range lines[1:] {
			if line == "locked" || strings.HasPrefix(line, "locked ") {
				return nil
			}
		}
		return fmt.Errorf("runner worktree %s is not locked; lock it before pinning or launch", workdir)
	}
	return fmt.Errorf("runner workdir %s is not registered as a Git worktree", workdir)
}

func runnerStartCommand(workdir string) string {
	return "cd " + shellSingleQuote(workdir) + " && amp --no-tui; status=$?; sleep 2; exit $status"
}

func inspectRunner(row config.RunnerRow) (runnerInspection, error) {
	legacy := row.LegacyWindow && row.Window != config.RunnerWindow(row.Workdir)
	primary, err := inspectRunnerWindow(row, row.Window, map[bool]string{true: tmux.RunnerCommand(row.Workdir), false: runnerStartCommand(row.Workdir)}[legacy])
	if err != nil || !legacy {
		return primary, err
	}
	canonical, err := inspectRunnerWindow(row, config.RunnerWindow(row.Workdir), runnerStartCommand(row.Workdir))
	if err != nil {
		return runnerInspection{}, err
	}
	if primary.state == runnerPaneConflict || canonical.state == runnerPaneConflict {
		return runnerInspection{state: runnerPaneConflict}, nil
	}
	if primary.state == runnerPaneAmbiguous || canonical.state == runnerPaneAmbiguous || primary.state == runnerPaneExact && canonical.state == runnerPaneExact {
		return runnerInspection{state: runnerPaneAmbiguous}, nil
	}
	if primary.state == runnerPaneExact {
		return primary, nil
	}
	if canonical.state == runnerPaneExact {
		return canonical, nil
	}
	return runnerInspection{state: runnerPaneAbsent}, nil
}

func inspectRunnerWindow(row config.RunnerRow, window, expectedStart string) (runnerInspection, error) {
	runner := tmux.Runner{}
	exists, err := runner.SessionExists(row.Workspace)
	if err != nil {
		return runnerInspection{}, fmt.Errorf("inspect runner tmux session %s: %w", row.Workspace, err)
	}
	if !exists {
		return runnerInspection{state: runnerPaneAbsent}, nil
	}
	panes, err := runner.RestartWindowPanes(row.Workspace, window)
	if err != nil {
		return runnerInspection{}, err
	}
	if len(panes) == 0 {
		return runnerInspection{state: runnerPaneAbsent}, nil
	}
	if len(panes) != 1 {
		return runnerInspection{state: runnerPaneAmbiguous}, nil
	}
	pane := panes[0]
	path, pathErr := config.CanonicalWorkdir(pane.Path)
	if pathErr != nil || path != row.Workdir || pane.Dead || pane.Command != "amp" || normalizedTmuxStartCommand(pane.StartCommand) != expectedStart {
		return runnerInspection{state: runnerPaneConflict, pane: pane}, nil
	}
	return runnerInspection{state: runnerPaneExact, pane: pane}, nil
}

func launchRunner(row config.RunnerRow) (tmux.WindowPane, error) {
	runner := tmux.Runner{}
	row.Window = config.RunnerWindow(row.Workdir)
	exists, err := runner.SessionExists(row.Workspace)
	if err != nil {
		return tmux.WindowPane{}, err
	}
	created, err := runner.NewRunnerPane(row.Workspace, row.Window, runnerStartCommand(row.Workdir), !exists)
	if err != nil {
		return tmux.WindowPane{}, err
	}
	deadline := time.Now().Add(runnerStartupTimeout)
	observedExact := false
	for {
		pane, inspectErr := runner.RestartPaneByID(created.PaneID)
		if inspectErr == nil && pane.WindowID == created.WindowID && pane.PaneID == created.PaneID && pane.Session == row.Workspace && pane.Window == row.Window {
			path, _ := config.CanonicalWorkdir(pane.Path)
			if !pane.Dead && pane.Command == "amp" && path == row.Workdir && normalizedTmuxStartCommand(pane.StartCommand) == runnerStartCommand(row.Workdir) {
				observedExact = true
				if !time.Now().Before(deadline) {
					return pane, nil
				}
			}
			if pane.Dead || observedExact && pane.Command != "amp" {
				diagnostic, _ := runner.CapturePaneHistory(created.PaneID, 100)
				_ = runner.KillWindow(created.WindowID)
				return tmux.WindowPane{}, fmt.Errorf("runner exited during startup: %s%s", boundedDiagnostic(diagnostic, 4096), staleAmpPIDDiagnostic(row.Workdir))
			}
		}
		if !time.Now().Before(deadline) {
			diagnostic, _ := runner.CapturePaneHistory(created.PaneID, 100)
			_ = runner.KillWindow(created.WindowID)
			return tmux.WindowPane{}, fmt.Errorf("runner did not survive startup as exact pane %s/window %s: %s%s", created.PaneID, created.WindowID, boundedDiagnostic(diagnostic, 4096), staleAmpPIDDiagnostic(row.Workdir))
		}
		time.Sleep(runnerPollInterval)
	}
}

func stopRunner(row config.RunnerRow, before runnerInspection) error {
	after, err := inspectRunner(row)
	if err != nil {
		return err
	}
	if after.state != runnerPaneExact || after.pane.WindowID != before.pane.WindowID || after.pane.PaneID != before.pane.PaneID {
		return fmt.Errorf("runner %s changed after preflight", row.Workdir)
	}
	return (tmux.Runner{}).KillWindow(after.pane.WindowID)
}

func boundedDiagnostic(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "no pane diagnostics available"
	}
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func staleAmpPIDDiagnostic(workdir string) string {
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	canonical, err := config.CanonicalWorkdir(workdir)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(canonical))
	marker := filepath.Join(cache, "amp", "pids", fmt.Sprintf("runner-%x.pid", sum[:8]))
	data, readErr := os.ReadFile(marker)
	if os.IsNotExist(readErr) {
		return ""
	}
	if readErr != nil {
		return fmt.Sprintf("; Amp-owned PID marker %s could not be read: %v; left unchanged", marker, readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
	if parseErr != nil || pid <= 0 {
		return fmt.Sprintf("; Amp-owned PID marker %s has an invalid PID; left unchanged", marker)
	}
	alive := runnerProcessAlive(pid)
	state := "stale"
	if alive {
		state = "live but ownership is ambiguous"
	}
	return fmt.Sprintf("; Amp-owned PID marker %s for this workdir points to %s pid %d; left unchanged", marker, state, pid)
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return processSignalMayBeAlive(process.Signal(syscall.Signal(0)))
}

func processSignalMayBeAlive(err error) bool {
	return err == nil || !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH)
}
