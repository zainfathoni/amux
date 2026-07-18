package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
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
	runnerStartupTimeout  = 750 * time.Millisecond
	runnerPollInterval    = 50 * time.Millisecond
	runnerProcessAlive    = processAlive
	runnerProcessArgs     = tmux.ProcessArgs
	runnerProcessIdentity = tmux.ProcessIdentity
	runnerChildProcesses  = tmux.InspectChildProcesses
	runnerPaneByID        = (tmux.Runner{}).RestartPaneByID
	runnerCacheDir        = os.UserCacheDir
)

const runnerStartupErrorLimit = 4608

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
	if in.Command.Name == "doctor" && len(rows) == 0 && in.Selectors.All {
		details, doctorErr := maintenanceDoctorDetails(dir)
		if doctorErr != nil {
			return &env, result.Runtime(doctorErr)
		}
		if details == nil {
			return &env, result.Preflight(errors.New("no configured runner matches the selector"))
		}
		out := result.Outcome{Resource: result.ConfigResource(dir.MaintenancePath()), Action: "doctor", Maintenance: details, Message: maintenanceDoctorMessage(details)}
		if details.Error != "" {
			out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: details.Error}
			env.Failed = append(env.Failed, out)
		} else {
			env.Successful = append(env.Successful, out)
		}
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		if out.Error != nil {
			return &env, result.Runtime(errors.New(details.Error))
		}
		return &env, nil
	}
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
		if in.Command.Name == "launch" || in.Command.Name == "restart" {
			if err := requireRunnerDirectory(row.Workdir); err != nil {
				return &env, result.Preflight(err)
			}
		}
		if in.Command.Name == "reconcile" && inspection.state != runnerPaneAbsent {
			return &env, result.Preflight(fmt.Errorf("runner %s still has %s runtime ownership; reconcile will not remove its configuration", row.Workdir, inspection.state))
		}
	}

	restartFailed := false
	for _, row := range rows {
		out := runnerOutcome(row, in.Command.Name, "")
		if restartFailed {
			out.Message = "not attempted after earlier runner restart failure"
			env.Skipped = append(env.Skipped, out)
			continue
		}
		inspection := inspections[row.Workdir]
		pidDiagnostic := ""
		if in.Command.Name == "reconcile" {
			pidDiagnostic = staleAmpPIDDiagnostic(row.Workdir)
		}
		if in.Command.Name == "doctor" {
			workdirState, workdirErr := runnerDirectoryState(row.Workdir)
			if workdirErr != nil {
				workdirState = workdirErr.Error()
			}
			out.Message = fmt.Sprintf("local=%s workdir=%s%s", inspection.state, workdirState, staleAmpPIDDiagnostic(row.Workdir))
			out.Runner = &result.RunnerDetails{LocalState: string(inspection.state), ProcessStart: inspection.pane.StartTime}
			if inspection.pane.StartTime > 0 {
				out.Runner.ProcessAgeSeconds = time.Now().Unix() - inspection.pane.StartTime
			}
			out.Maintenance, err = maintenanceDoctorDetails(dir)
			if err != nil {
				out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: err.Error()}
				env.Failed = append(env.Failed, out)
				continue
			}
			if out.Maintenance != nil {
				out.Message += fmt.Sprintf("; maintenance owner=%s schedule=%s amp=%s version=%s latest=%s time=%s", out.Maintenance.Owner, out.Maintenance.Schedule, out.Maintenance.AmpPath, out.Maintenance.AmpVersion, out.Maintenance.Status, out.Maintenance.Time)
				if out.Maintenance.Error != "" {
					out.Message += fmt.Sprintf(" error=%q; remediate with `amux runner maintenance run`", out.Maintenance.Error)
					out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: out.Maintenance.Error}
				}
			}
			if inspection.pane.StartTime > 0 {
				out.Message += fmt.Sprintf("; process age=%ds", out.Runner.ProcessAgeSeconds)
			}
			if !in.Options.JSON {
				fmt.Fprintln(a.stdout, out.Message)
			}
			if out.Error != nil {
				env.Failed = append(env.Failed, out)
			} else {
				env.Successful = append(env.Successful, out)
			}
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
			valid := requireRunnerDirectory(row.Workdir) == nil
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
			if in.Command.Name == "restart" {
				restartFailed = true
			}
			continue
		}
		env.Successful = append(env.Successful, out)
	}
	if len(env.Failed) > 0 {
		failed := env.Failed[0]
		return &env, result.Runtime(fmt.Errorf("runner %s %s failed: %s", failed.Resource.Workdir, failed.Action, failed.Error.Message))
	}
	return &env, nil
}

func maintenanceDoctorMessage(d *result.MaintenanceDetails) string {
	message := fmt.Sprintf("maintenance owner=%s schedule=%s amux=%s amp=%s target=%s version=%s scheduler=%s latest=%s time=%s artifacts=%s", d.Owner, d.Schedule, d.AmuxPath, d.AmpPath, d.AmpTarget, d.AmpVersion, d.SchedulerState, d.Status, d.Time, strings.Join(d.ArtifactPaths, ","))
	if d.Error != "" {
		message += fmt.Sprintf(" error=%q; remediation: reinstall or run maintenance", d.Error)
	}
	return message
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
	if err := requireRunnerDirectory(row.Workdir); err != nil {
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

func requireRunnerDirectory(workdir string) error {
	_, err := runnerDirectoryState(workdir)
	return err
}

func runnerDirectoryState(workdir string) (string, error) {
	stat, err := os.Stat(workdir)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("runner workdir %s is missing", workdir)
	}
	if err != nil {
		return "", fmt.Errorf("inspect runner workdir %s: %w", workdir, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("runner workdir %s is not a directory", workdir)
	}
	return "directory", nil
}

func runnerStartCommand(workdir string) string {
	return "cd " + shellSingleQuote(workdir) + " && amp --no-tui; status=$?; sleep 2; exit $status"
}

func runnerStartCommandMatches(actual, expected string) bool {
	if normalizedTmuxStartCommand(actual) == expected {
		return true
	}
	const suffix = "exit $status"
	if !strings.HasSuffix(expected, suffix) {
		return false
	}
	measured := `"` + strings.TrimSuffix(expected, suffix) + `exit \$status"`
	return actual == measured
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
	retainedShell := expectedStart == runnerStartCommand(row.Workdir)
	exactProcess, processErr := runnerPaneHasExactProcess(pane, retainedShell)
	if processErr != nil {
		return runnerInspection{}, fmt.Errorf("inspect runner process for pane %s pid %d: %w", pane.PaneID, pane.PID, processErr)
	}
	if exactProcess && !retainedShell {
		unchanged, confirmErr := legacyRunnerPaneUnchanged(pane)
		if confirmErr != nil {
			return runnerInspection{}, fmt.Errorf("revalidate legacy runner pane %s: %w", pane.PaneID, confirmErr)
		}
		if !unchanged {
			exactProcess = false
		}
	}
	if !runnerPaneWorkdirMatches(pane.Path, row.Workdir) || pane.Dead || !exactProcess || !runnerStartCommandMatches(pane.StartCommand, expectedStart) {
		return runnerInspection{state: runnerPaneConflict, pane: pane}, nil
	}
	return runnerInspection{state: runnerPaneExact, pane: pane}, nil
}

func runnerPaneWorkdirMatches(panePath, workdir string) bool {
	path, pathErr := config.CanonicalWorkdir(panePath)
	if pathErr != nil {
		return false
	}
	if path == workdir {
		return true
	}
	paneInfo, paneErr := os.Stat(path)
	workdirInfo, workdirErr := os.Stat(workdir)
	return paneErr == nil && workdirErr == nil && os.SameFile(paneInfo, workdirInfo)
}

func runnerPaneHasExactProcess(pane tmux.WindowPane, retainedShell bool) (bool, error) {
	exact, _, err := observeRunnerPaneProcess(pane, retainedShell)
	return exact, err
}

func observeRunnerPaneProcess(pane tmux.WindowPane, retainedShell bool) (bool, string, error) {
	if !retainedShell {
		if pane.Command != "amp" {
			return false, fmt.Sprintf("pane-pid=%d current-command=%q", pane.PID, pane.Command), nil
		}
		before, err := runnerProcessIdentity(pane.PID)
		if err != nil {
			return false, fmt.Sprintf("pane-pid=%d current-command=%q", pane.PID, pane.Command), err
		}
		args, err := runnerProcessArgs(pane.PID)
		if err != nil {
			return false, fmt.Sprintf("pane-pid=%d current-command=%q incarnation=%q", pane.PID, pane.Command, before), err
		}
		after, err := runnerProcessIdentity(pane.PID)
		if err != nil {
			return false, fmt.Sprintf("pane-pid=%d current-command=%q incarnation=%q argv=%s", pane.PID, pane.Command, before, runnerArgsDiagnostic(args)), err
		}
		return runnerArgsAreExact(args) && before == after, fmt.Sprintf("pane-pid=%d current-command=%q incarnation=%q argv=%s revalidated-incarnation=%q", pane.PID, pane.Command, before, runnerArgsDiagnostic(args), after), nil
	}
	children, err := runnerChildProcesses(pane.PID)
	diagnostic := fmt.Sprintf("retained-shell-pid=%d direct-children=%s", pane.PID, runnerProcessListDiagnostic(children))
	if err != nil {
		return false, diagnostic, err
	}
	if len(children) != 1 {
		return false, diagnostic, nil
	}
	child := children[0]
	if child.ParentPID != pane.PID || child.Name != "amp" {
		return false, diagnostic, nil
	}
	args, err := runnerProcessArgs(child.PID)
	diagnostic += " argv=" + runnerArgsDiagnostic(args)
	if err != nil {
		return false, diagnostic, err
	}
	if !runnerArgsAreExact(args) {
		return false, diagnostic, nil
	}
	after, err := runnerChildProcesses(pane.PID)
	diagnostic += " revalidated-direct-children=" + runnerProcessListDiagnostic(after)
	if err != nil {
		return false, diagnostic, err
	}
	return len(after) == 1 && after[0] == child, diagnostic, nil
}

func runnerArgsAreExact(args []string) bool {
	return len(args) == 2 && filepath.Base(args[0]) == "amp" && args[1] == "--no-tui"
}

func runnerProcessListDiagnostic(processes []tmux.ProcessMetadata) string {
	if len(processes) == 0 {
		return "[]"
	}
	items := make([]string, 0, len(processes))
	for _, process := range processes {
		item := fmt.Sprintf("{pid=%d ppid=%d name=%q incarnation=%q}", process.PID, process.ParentPID, boundedValue(process.Name, 64), boundedValue(process.Identity, 128))
		if len(strings.Join(items, ", "))+len(item) > 480 {
			items = append(items, "…")
			break
		}
		items = append(items, item)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func runnerArgsDiagnostic(args []string) string {
	parts := make([]string, 0, len(args))
	length := 2
	for _, arg := range args {
		part := fmt.Sprintf("%q", boundedValue(arg, 256))
		if length+len(part)+1 > 480 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, part)
		length += len(part) + 1
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func boundedValue(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit-len("…")] + "…"
}

func legacyRunnerPaneUnchanged(pane tmux.WindowPane) (bool, error) {
	confirmed, err := runnerPaneByID(pane.PaneID)
	if err != nil {
		return false, err
	}
	return confirmed == pane, nil
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
	startupBegan := time.Now()
	deadline := startupBegan.Add(runnerStartupTimeout)
	observedExact := false
	lastProcessError := ""
	lastDetailedObservation := ""
	lastObservation := ""
	for {
		pane, inspectErr := runner.RestartPaneByID(created.PaneID)
		if inspectErr == nil && pane.WindowID == created.WindowID && pane.PaneID == created.PaneID && pane.Session == row.Workspace && pane.Window == row.Window {
			exactProcess, processDiagnostic, processErr := observeRunnerPaneProcess(pane, true)
			if processErr != nil {
				lastProcessError = boundedDiagnostic(processErr.Error(), 1024)
			}
			workdirMatches := runnerPaneWorkdirMatches(pane.Path, row.Workdir)
			startMatches := runnerStartCommandMatches(pane.StartCommand, runnerStartCommand(row.Workdir))
			lastObservation = fmt.Sprintf("last observed at +%s: session=%q window-name=%q pane=%s window=%s dead=%t start-command-match=%t workdir-equivalent=%t; %s; path=%q current-command=%q start-command=%q", time.Since(startupBegan).Round(time.Millisecond), pane.Session, pane.Window, pane.PaneID, pane.WindowID, pane.Dead, startMatches, workdirMatches, boundedValue(processDiagnostic, 512), boundedValue(pane.Path, 128), boundedValue(pane.Command, 64), boundedValue(pane.StartCommand, 256))
			lastDetailedObservation = lastObservation
			if processErr == nil && !pane.Dead && exactProcess && workdirMatches && startMatches {
				observedExact = true
				if !time.Now().Before(deadline) {
					return pane, nil
				}
			}
			if pane.Dead || processErr == nil && observedExact && !exactProcess {
				diagnostic, _ := runner.CapturePaneHistory(created.PaneID, 100)
				_ = runner.KillWindow(created.WindowID)
				return tmux.WindowPane{}, runnerStartupError("runner exited during startup: %s; %s%s", boundedDiagnostic(diagnostic, 2560), runnerObservationDiagnostic(lastDetailedObservation, lastObservation), staleAmpPIDDiagnostic(row.Workdir))
			}
		} else if inspectErr == nil {
			lastObservation = fmt.Sprintf("last observed at +%s: created pane identity drifted: expected session=%q window-name=%q window=%s pane=%s; observed session=%q window-name=%q window=%s pane=%s", time.Since(startupBegan).Round(time.Millisecond), row.Workspace, row.Window, created.WindowID, created.PaneID, pane.Session, pane.Window, pane.WindowID, pane.PaneID)
		} else {
			lastObservation = fmt.Sprintf("last observed at +%s: exact pane %s unavailable: %s", time.Since(startupBegan).Round(time.Millisecond), created.PaneID, boundedDiagnostic(inspectErr.Error(), 1024))
		}
		if !time.Now().Before(deadline) {
			diagnostic, _ := runner.CapturePaneHistory(created.PaneID, 100)
			_ = runner.KillWindow(created.WindowID)
			processDiagnostic := ""
			if lastProcessError != "" {
				processDiagnostic = "; process inspection failed: " + lastProcessError
			}
			return tmux.WindowPane{}, runnerStartupError("runner did not survive startup as exact pane %s/window %s: %s; %s%s%s", created.PaneID, created.WindowID, boundedDiagnostic(diagnostic, 2560), runnerObservationDiagnostic(lastDetailedObservation, lastObservation), processDiagnostic, staleAmpPIDDiagnostic(row.Workdir))
		}
		time.Sleep(runnerPollInterval)
	}
}

func runnerObservationDiagnostic(detailed, final string) string {
	if detailed != "" && detailed != final {
		return fmt.Sprintf("last detailed observation: %s; final observation: %s", boundedDiagnostic(detailed, 1024), boundedDiagnostic(final, 512))
	}
	return boundedDiagnostic(final, 1024)
}

func runnerStartupError(format string, args ...any) error {
	message := fmt.Sprintf(format, args...)
	if len(message) > runnerStartupErrorLimit {
		message = message[:runnerStartupErrorLimit-len("…")] + "…"
	}
	return errors.New(message)
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
	cache, err := runnerCacheDir()
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
