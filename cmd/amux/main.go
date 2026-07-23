package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const (
	defaultWorkspace     = "mac"
	defaultSession       = "Amp"
	terminalLauncherEnv  = "AMUX_TERMINAL_LAUNCHER"
	spawnPollInterval    = 100 * time.Millisecond
	maxSpawnMessageBytes = 1 << 20
)

var (
	version = "dev"
	commit  = ""
	built   = ""
)

type options struct {
	configPath       string
	dryRun           bool
	attachMode       attachMode
	terminalLauncher string
}

type attachMode int

const (
	attachAuto attachMode = iota
	attachAlways
	attachNever
)

type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func main() {
	a := app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	if err := a.execute(os.Args[1:]); err != nil {
		fmt.Fprintln(a.stderr, err)
		os.Exit(result.ExitCode(err))
	}
}

func run(args []string) error {
	return app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}.run(args)
}

func (a app) run(args []string) error {
	if a.stdin == nil {
		a.stdin = os.Stdin
	}
	if a.stdout == nil {
		a.stdout = io.Discard
	}
	if a.stderr == nil {
		a.stderr = io.Discard
	}

	opts, args, err := parseOptions(args)
	if err != nil {
		return err
	}
	if opts.terminalLauncher == "" {
		opts.terminalLauncher = os.Getenv(terminalLauncherEnv)
	}
	command := "launch"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "migrate-config":
		if opts.configPath != "" {
			return errors.New("migrate-config uses the default config locations and does not support --config")
		}
		if len(args) != 0 {
			return errors.New("usage: amux migrate-config")
		}
		migrated, err := config.MigrateDefaultDir()
		if err != nil {
			return err
		}
		if migrated {
			fmt.Fprintf(a.stdout, "Migrated config from ~/%s to ~/%s\n", filepath.Dir(config.LegacyDefaultRelativePath), filepath.Dir(config.DefaultRelativePath))
			fmt.Fprintln(a.stdout, "Old config files were left in place for rollback and older amux binaries.")
		} else {
			fmt.Fprintln(a.stdout, "No config migration needed.")
		}
		fmt.Fprintln(a.stdout, config.DefaultPath())
		return nil
	}

	if opts.configPath == "" && command == "workspaces" {
		opts.configPath = config.DefaultPathReadOnly()
	} else if opts.configPath == "" {
		opts.configPath = config.DefaultPath()
	}

	switch command {
	case "launch":
		return a.launch(opts, args)
	case "list":
		return a.list(opts, args)
	case "workspaces":
		return a.workspaces(opts, args)
	case "shelved":
		return a.list(opts, append([]string{"--shelved"}, args...))
	case "pin", "store":
		return a.store(opts, args)
	case "pin-current", "store-current":
		return a.storeCurrent(opts, args)
	case "unpin", "remove":
		return a.remove(opts, args)
	case "unpin-current", "remove-current":
		return a.removeCurrent(opts, args)
	case "park":
		return a.park(opts, args)
	case "park-current":
		return a.parkCurrent(opts, args)
	case "restart":
		return a.restart(opts, args)
	case "shelve-current":
		return a.shelveCurrent(opts, args)
	case "shelve":
		return a.shelve(opts, args)
	case "unshelve":
		return a.unshelve(opts, args)
	case "spawn":
		return a.spawn(opts, args)
	case "teardown":
		return a.teardown(opts, args)
	case "prune-archived":
		return a.pruneArchived(opts, args)
	case "runner":
		return a.runner(opts, args)
	case "completion":
		return a.completion(args)
	case "update", "self-update":
		return a.selfUpdate(opts, args)
	case "version", "--version":
		if len(args) != 0 {
			return fmt.Errorf("usage: amux %s", command)
		}
		fmt.Fprintln(a.stdout, versionString())
		return nil
	case "path":
		if len(args) != 0 {
			return errors.New("usage: amux path")
		}
		fmt.Fprintln(a.stdout, opts.configPath)
		return nil
	case "doctor":
		if len(args) > 2 {
			return errors.New("usage: amux doctor [workspace] [session]")
		}
		workspace, session := workspaceSessionFromArgs(args)
		return a.doctor(opts, workspace, session)
	case "help", "--help", "-h":
		a.usage()
		return nil
	default:
		// Compatibility with the Bash helper: `amux mac Amp` means launch.
		if len(args) <= 1 {
			return a.launch(opts, append([]string{command}, args...))
		}
		a.usage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func parseOptions(args []string) (options, []string, error) {
	var opts options
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			remaining = append(remaining, args[i:]...)
			break
		}
		switch args[i] {
		case "--dry-run":
			opts.dryRun = true
		case "--attach":
			opts.attachMode = attachAlways
		case "--no-attach":
			opts.attachMode = attachNever
		case "--terminal-launcher":
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, errors.New("--terminal-launcher requires a command")
			}
			opts.terminalLauncher = args[i]
		case "--config":
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, errors.New("--config requires a path")
			}
			opts.configPath = args[i]
		default:
			if strings.HasPrefix(args[i], "--config=") {
				opts.configPath = strings.TrimPrefix(args[i], "--config=")
				if opts.configPath == "" {
					return opts, nil, errors.New("--config requires a path")
				}
			} else if strings.HasPrefix(args[i], "--terminal-launcher=") {
				opts.terminalLauncher = strings.TrimPrefix(args[i], "--terminal-launcher=")
				if opts.terminalLauncher == "" {
					return opts, nil, errors.New("--terminal-launcher requires a command")
				}
			} else {
				remaining = append(remaining, args[i])
			}
		}
	}
	return opts, remaining, nil
}

func (a app) launch(opts options, args []string) error {
	if len(args) > 2 {
		return errors.New("usage: amux launch [workspace] [session]")
	}
	if len(args) == 0 {
		if opts.attachMode == attachAlways {
			return errors.New("--attach requires an explicit workspace when launch targets all workspaces")
		}
		rows, err := config.Load(opts.configPath)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("no rows found in %s", opts.configPath)
		}
		statuses, err := threadArchiveStatuses(rows)
		if err != nil {
			return fmt.Errorf("confirm shelved Amp threads before launch: %w", err)
		}
		byWorkspace := make(map[string][]config.Row)
		for _, row := range rows {
			byWorkspace[row.Workspace] = append(byWorkspace[row.Workspace], row)
		}
		names := make([]string, 0, len(byWorkspace))
		for workspace := range byWorkspace {
			names = append(names, workspace)
		}
		sort.Strings(names)
		opts.attachMode = attachNever
		for _, workspace := range names {
			if err := a.launchWorkspace(opts, workspace, workspace, byWorkspace[workspace], statuses); err != nil {
				return fmt.Errorf("launch workspace %q: %w", workspace, err)
			}
		}
		return nil
	}
	workspace, session := workspaceSessionFromArgs(args)

	rows, err := rowsForWorkspace(opts.configPath, workspace)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no rows found for workspace %q in %s", workspace, opts.configPath)
	}
	statuses, err := threadArchiveStatuses(rows)
	if err != nil {
		return fmt.Errorf("confirm shelved Amp threads before launch: %w", err)
	}
	return a.launchWorkspace(opts, workspace, session, rows, statuses)
}

func (a app) launchWorkspace(opts options, workspace, session string, rows []config.Row, statuses map[string]threadStatus) error {
	activeRows := make([]config.Row, 0, len(rows))
	for _, row := range rows {
		switch statuses[canonicalThreadID(row.Thread)] {
		case threadStatusArchived:
			fmt.Fprintf(a.stdout, "Skipping shelved row %s/%s (%s); run amux unshelve %s %s to make it launchable.\n", row.Workspace, row.Window, canonicalThreadID(row.Thread), row.Workspace, row.Window)
		case threadStatusMissing:
			return fmt.Errorf("Amp thread %s for %s/%s was not found in active or archived thread lists", canonicalThreadID(row.Thread), row.Workspace, row.Window)
		default:
			activeRows = append(activeRows, row)
		}
	}
	if len(activeRows) == 0 {
		fmt.Fprintf(a.stdout, "No unshelved rows found for workspace %s in %s\n", workspace, opts.configPath)
		return nil
	}
	rows = activeRows

	runner := tmux.Runner{DryRun: opts.dryRun, TerminalLauncher: opts.terminalLauncher, Output: a.stdout}
	sessionExists := runner.HasSession(session)
	sessionExistedBeforeLaunch := sessionExists
	windowNames, err := runner.WindowNames(session)
	if sessionExists && err != nil {
		return fmt.Errorf("list tmux windows for session %q: %w", session, err)
	}

	first := !sessionExists
	restoredDuringLaunch := false
	for _, row := range rows {
		workdir := config.ExpandHome(row.Workdir)
		if stat, err := os.Stat(workdir); err != nil || !stat.IsDir() {
			return fmt.Errorf("missing workdir for window %q: %s", row.Window, workdir)
		}
		command := tmux.ContinueCommand(workdir, row.Thread)
		if first {
			if err := runner.NewSession(session, row.Window, command); err != nil {
				return fmt.Errorf("create tmux session %q: %w", session, err)
			}
			restoredDuringLaunch = true
			first = false
			continue
		}
		if tmux.WindowExists(windowNames, row.Window) {
			continue
		}
		if err := runner.NewWindow(session, row.Window, command); err != nil {
			return fmt.Errorf("create tmux window %q: %w", row.Window, err)
		}
		restoredDuringLaunch = true
	}
	shouldAttach, err := shouldAttachAfterLaunch(opts, runner, session, sessionExistedBeforeLaunch, restoredDuringLaunch, rows)
	if err != nil {
		return err
	}
	return runner.SelectAndAttach(session, !shouldAttach)
}

func workspaceSessionFromArgs(args []string) (string, string) {
	workspace := defaultWorkspace
	session := defaultSession
	if len(args) >= 1 {
		workspace = args[0]
		session = args[0]
	}
	if len(args) == 2 {
		session = args[1]
	}
	return workspace, session
}

func shouldAttachAfterLaunch(opts options, runner tmux.Runner, session string, sessionExistedBeforeLaunch, restoredDuringLaunch bool, rows []config.Row) (bool, error) {
	switch opts.attachMode {
	case attachAlways:
		return true, nil
	case attachNever:
		return false, nil
	}
	if opts.dryRun || !sessionExistedBeforeLaunch || restoredDuringLaunch {
		return false, nil
	}
	matches, err := workspaceMatchesSession(runner, session, rows)
	if err != nil {
		return false, fmt.Errorf("check tmux session %q for auto-attach: %w", session, err)
	}
	return matches, nil
}

func workspaceMatchesSession(runner tmux.Runner, session string, rows []config.Row) (bool, error) {
	panes, err := runner.Panes(session)
	if err != nil {
		return false, err
	}
	if len(panes) != len(rows) {
		return false, nil
	}
	live := make(map[string]tmux.Pane, len(panes))
	for _, pane := range panes {
		if _, ok := live[pane.Window]; ok {
			return false, nil
		}
		live[pane.Window] = pane
	}
	for _, row := range rows {
		pane, ok := live[row.Window]
		if !ok {
			return false, nil
		}
		if pane.Path != config.ExpandHome(row.Workdir) {
			return false, nil
		}
	}
	return true, nil
}

func (a app) list(opts options, args []string) error {
	filter := listFilterAll
	showStatus := false
	positional := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--status":
			showStatus = true
		case "--active":
			if filter == listFilterShelved {
				return errors.New("amux list accepts either --active or --shelved, not both")
			}
			filter = listFilterActive
		case "--shelved":
			if filter == listFilterActive {
				return errors.New("amux list accepts either --active or --shelved, not both")
			}
			filter = listFilterShelved
		default:
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("unknown list option: %s", arg)
			}
			positional = append(positional, arg)
		}
	}
	if len(positional) > 1 {
		return errors.New("usage: amux list [--status] [--active|--shelved] [workspace]")
	}
	workspace := ""
	if len(positional) == 1 {
		workspace = positional[0]
	}
	rows, err := config.LoadReadOnly(opts.configPath)
	if err != nil {
		return err
	}
	selected := make([]config.Row, 0, len(rows))
	for _, row := range rows {
		if workspace == "" || row.Workspace == workspace {
			selected = append(selected, row)
		}
	}
	needsStatus := showStatus || filter != listFilterAll
	statuses := map[string]threadStatus{}
	if needsStatus && len(selected) > 0 {
		statuses, err = threadArchiveStatuses(selected)
	}
	if err != nil && filter != listFilterAll {
		return fmt.Errorf("confirm Amp thread status before filtering list: %w", err)
	}
	if needsStatus {
		fmt.Fprintln(a.stdout, "workspace\twindow\tworkdir\tthread-id-or-url\tstatus")
	} else {
		fmt.Fprintln(a.stdout, "workspace\twindow\tworkdir\tthread-id-or-url")
	}
	statusForRow := func(row config.Row) string {
		if err != nil {
			return "unknown"
		}
		return listStatusLabel(statuses[canonicalThreadID(row.Thread)])
	}
	for _, row := range selected {
		status := ""
		if needsStatus {
			status = statusForRow(row)
		}
		switch filter {
		case listFilterActive:
			if status != "active" {
				continue
			}
		case listFilterShelved:
			if status != "shelved" {
				continue
			}
		}
		if needsStatus {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\t%s\n", row.Workspace, row.Window, row.Workdir, row.Thread, status)
		} else {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", row.Workspace, row.Window, row.Workdir, row.Thread)
		}
	}
	return nil
}

type listFilter int

const (
	listFilterAll listFilter = iota
	listFilterActive
	listFilterShelved
)

func listStatusLabel(status threadStatus) string {
	switch status {
	case threadStatusActive:
		return "active"
	case threadStatusArchived:
		return "shelved"
	case threadStatusMissing:
		return "missing"
	default:
		return "unknown"
	}
}

func (a app) workspaces(opts options, args []string) error {
	includeRunners := false
	for _, arg := range args {
		switch arg {
		case "--include-runners":
			includeRunners = true
		default:
			return errors.New("usage: amux workspaces [--include-runners]")
		}
	}
	rows, err := config.LoadReadOnly(opts.configPath)
	if err != nil {
		return err
	}
	names := make(map[string]bool)
	for _, row := range rows {
		names[row.Workspace] = true
	}
	if includeRunners {
		runnerRows, err := config.LoadRunnersReadOnly(config.RunnerPath(opts.configPath))
		if err != nil {
			return err
		}
		for _, row := range runnerRows {
			names[row.Workspace] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		fmt.Fprintln(a.stdout, name)
	}
	return nil
}

func (a app) runner(opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: amux runner <list|pin|unpin|launch|park|restart> [args]")
	}
	switch args[0] {
	case "list":
		return a.runnerList(opts, args[1:])
	case "pin":
		return a.runnerPin(opts, args[1:])
	case "unpin":
		return a.runnerUnpin(opts, args[1:])
	case "launch":
		return a.runnerLaunch(opts, args[1:])
	case "park":
		return a.runnerPark(opts, args[1:])
	case "restart":
		return a.runnerRestart(opts, args[1:])
	default:
		return fmt.Errorf("unknown runner command: %s", args[0])
	}
}

func (a app) runnerList(opts options, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: amux runner list [workspace]")
	}
	workspace := ""
	if len(args) == 1 {
		workspace = args[0]
	}
	rows, err := config.LoadRunners(config.RunnerPath(opts.configPath))
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "workspace\twindow\tworkdir")
	for _, row := range rows {
		if workspace == "" || row.Workspace == workspace {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", row.Workspace, row.Window, row.Workdir)
		}
	}
	return nil
}

func (a app) runnerPin(opts options, args []string) error {
	if len(args) != 3 {
		return errors.New("usage: amux runner pin <workspace> <window> <workdir>")
	}
	row := config.RunnerRow{Workspace: args[0], Window: args[1], Workdir: args[2]}
	replaced, err := config.StoreRunner(config.RunnerPath(opts.configPath), row)
	if err != nil {
		return err
	}
	if replaced {
		fmt.Fprintf(a.stdout, "Updated runner %s/%s in %s\n", row.Workspace, row.Window, config.RunnerPath(opts.configPath))
	} else {
		fmt.Fprintf(a.stdout, "Pinned runner %s/%s in %s\n", row.Workspace, row.Window, config.RunnerPath(opts.configPath))
	}
	return nil
}

func (a app) runnerUnpin(opts options, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: amux runner unpin <workspace> <window>")
	}
	removed, err := config.RemoveRunner(config.RunnerPath(opts.configPath), args[0], args[1])
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(a.stdout, "Unpinned runner %s/%s from %s\n", args[0], args[1], config.RunnerPath(opts.configPath))
	} else {
		fmt.Fprintf(a.stdout, "No runner found for %s/%s in %s\n", args[0], args[1], config.RunnerPath(opts.configPath))
	}
	return nil
}

func (a app) runnerLaunch(opts options, args []string) error {
	if len(args) > 2 {
		return errors.New("usage: amux runner launch [workspace] [session]")
	}
	if len(args) == 0 {
		rows, err := config.LoadRunners(config.RunnerPath(opts.configPath))
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("no runner rows found in %s", config.RunnerPath(opts.configPath))
		}
		byWorkspace := make(map[string][]config.RunnerRow)
		workspaces := make([]string, 0)
		for _, row := range rows {
			if _, exists := byWorkspace[row.Workspace]; !exists {
				workspaces = append(workspaces, row.Workspace)
			}
			byWorkspace[row.Workspace] = append(byWorkspace[row.Workspace], row)
		}
		sort.Strings(workspaces)
		for _, workspace := range workspaces {
			if err := a.launchRunnerRows(opts, workspace, byWorkspace[workspace]); err != nil {
				return err
			}
		}
		return nil
	}
	workspace, session := workspaceSessionFromArgs(args)
	rows, err := runnerRowsForWorkspace(config.RunnerPath(opts.configPath), workspace)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no runner rows found for workspace %q in %s", workspace, config.RunnerPath(opts.configPath))
	}
	return a.launchRunnerRows(opts, session, rows)
}

func (a app) launchRunnerRows(opts options, session string, rows []config.RunnerRow) error {
	runner := tmux.Runner{DryRun: opts.dryRun}
	sessionExists := runner.HasSession(session)
	windowNames, err := runner.WindowNames(session)
	if sessionExists && err != nil {
		return fmt.Errorf("list tmux windows for session %q: %w", session, err)
	}
	first := !sessionExists
	for _, row := range rows {
		workdir := config.ExpandHome(row.Workdir)
		if stat, err := os.Stat(workdir); err != nil || !stat.IsDir() {
			return fmt.Errorf("missing runner workdir for window %q: %s", row.Window, workdir)
		}
		command := tmux.RunnerCommand(workdir)
		if first {
			if err := runner.NewSession(session, row.Window, command); err != nil {
				return fmt.Errorf("create tmux session %q: %w", session, err)
			}
			first = false
			continue
		}
		if tmux.WindowExists(windowNames, row.Window) {
			panes, err := runner.WindowPanesWithCommand(session, row.Window)
			if err != nil {
				return err
			}
			if len(panes) == 0 {
				return fmt.Errorf("runner window %q already exists in tmux session %s, but no pane metadata was found; refusing to reuse an ambiguous live process", row.Window, session)
			}
			if len(panes) > 1 {
				return fmt.Errorf("ambiguous runner window %q in tmux session %s: candidates %s; refusing runner launch", row.Window, session, formatPaneCandidates(panes))
			}
			if err := verifyRunnerParkPane(row, panes[0]); err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "Runner %s/%s already running in tmux session %s; skipping\n", row.Workspace, row.Window, session)
			continue
		}
		if err := runner.NewWindow(session, row.Window, command); err != nil {
			return fmt.Errorf("create runner tmux window %q: %w", row.Window, err)
		}
	}
	return nil
}

func (a app) runnerPark(opts options, args []string) error {
	workspace, window, session, err := parseRunnerParkArgs(args)
	if err != nil {
		return err
	}
	rows, err := runnerRowsForWorkspace(config.RunnerPath(opts.configPath), workspace)
	if err != nil {
		return err
	}
	var row config.RunnerRow
	found := false
	for _, candidate := range rows {
		if candidate.Window == window {
			row = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no runner row for %s/%s in %s", workspace, window, config.RunnerPath(opts.configPath))
	}
	// Inventory and revalidation are always real, read-only tmux queries. Only the
	// asynchronous shutdown scheduling is simulated by the dry-run runner.
	panes, err := (tmux.Runner{}).WindowPanesWithCommand(session, row.Window)
	if err != nil {
		return err
	}
	if len(panes) == 0 {
		return fmt.Errorf("runner window %q is not running in tmux session %s", row.Window, session)
	}
	if len(panes) > 1 {
		return fmt.Errorf("ambiguous runner window %q in tmux session %s: candidates %s; refusing runner park", row.Window, session, formatPaneCandidates(panes))
	}
	if panes[0].WindowID == "" {
		return fmt.Errorf("runner window %q in tmux session %q has no window id", row.Window, session)
	}
	if err := verifyRunnerParkPane(row, panes[0]); err != nil {
		return err
	}
	revalidated, err := (tmux.Runner{}).RestartWindowPanes(session, row.Window)
	if err != nil {
		return fmt.Errorf("revalidate runner tmux window %s/%s: %w", session, row.Window, err)
	}
	if len(revalidated) != 1 {
		return fmt.Errorf("runner park requires exactly one pane in tmux window %s/%s; found %d", session, row.Window, len(revalidated))
	}
	if err := verifyRunnerParkPane(row, revalidated[0]); err != nil {
		return err
	}
	if revalidated[0].PaneID == "" || revalidated[0].WindowID == "" {
		return fmt.Errorf("runner tmux window %s/%s has no pane or window id", session, row.Window)
	}
	plan := parkPlan{key: workspace + "/" + row.Window, row: config.Row{Workspace: workspace, Window: row.Window}, pane: revalidated[0]}
	if err := a.schedulePark(tmux.Runner{DryRun: opts.dryRun, Output: a.stdout}, plan); err != nil {
		return err
	}
	if opts.dryRun {
		fmt.Fprintf(a.stdout, "Planned runner park: %s\n", plan.key)
	} else {
		fmt.Fprintf(a.stdout, "Scheduled runner park: %s\n", plan.key)
	}
	return nil
}

func parseRunnerParkArgs(args []string) (workspace, window, session string, err error) {
	switch len(args) {
	case 1:
		workspace = defaultWorkspace
		window = args[0]
		session = defaultSession
	case 2:
		workspace = args[0]
		window = args[1]
		session = workspace
	case 3:
		workspace = args[0]
		window = args[1]
		session = args[2]
	default:
		return "", "", "", errors.New("usage: amux runner park [workspace] <window> [session]")
	}
	if err := config.ValidateField("workspace", workspace); err != nil {
		return "", "", "", err
	}
	if err := config.ValidateField("window", window); err != nil {
		return "", "", "", err
	}
	if session == "" {
		return "", "", "", errors.New("runner park session cannot be empty")
	}
	return workspace, window, session, nil
}

func verifyRunnerParkPane(row config.RunnerRow, pane tmux.WindowPane) error {
	workdir := config.ExpandHome(row.Workdir)
	if startCommandMatchesRunner(pane.StartCommand, workdir) {
		return nil
	}
	// Compatibility for runner panes started manually before tmux recorded a
	// start command: accept only an exact configured path plus the amp process.
	if pane.StartCommand == "" && pane.Path == workdir && pane.Command == "amp" {
		return nil
	}
	return fmt.Errorf("tmux window %q in session %q is not the expected runner for %s/%s at %s: start command is %s", row.Window, pane.Session, row.Workspace, row.Window, workdir, pane.StartCommand)
}

func (a app) runnerRestart(opts options, args []string) error {
	if len(args) == 0 {
		return a.restartAllRunners(opts)
	}
	workspace, window, session, err := parseRestartArgs("amux runner restart [workspace] <window> [session]", args)
	if err != nil {
		return err
	}
	rows, err := runnerRowsForWorkspaceReadOnly(config.RunnerPath(opts.configPath), workspace)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Window != window {
			continue
		}
		workdir := config.ExpandHome(row.Workdir)
		if stat, err := os.Stat(workdir); err != nil || !stat.IsDir() {
			return fmt.Errorf("missing runner workdir for window %q: %s", row.Window, workdir)
		}
		readRunner := tmux.Runner{}
		panes, err := readRunner.RestartWindowPanes(session, window)
		if err != nil {
			return err
		}
		if len(panes) != 1 {
			return fmt.Errorf("runner restart requires exactly one pane in tmux window %s/%s; found %d", session, window, len(panes))
		}
		if panes[0].Dead {
			return fmt.Errorf("runner pane %s in tmux window %s/%s is dead and not currently running", panes[0].PaneID, session, window)
		}
		if normalizedTmuxStartCommand(panes[0].StartCommand) != tmux.RunnerCommand(workdir) {
			return fmt.Errorf("tmux window %q in session %q is not the exact configured runner for %s/%s at %s; refusing restart", window, session, workspace, window, workdir)
		}
		if panes[0].PaneID == "" || panes[0].WindowID == "" {
			return fmt.Errorf("runner window %q in tmux session %q has no pane or window id", window, session)
		}
		plan := restartPlan{key: workspace + "/" + window, pane: panes[0], command: tmux.RunnerCommand(workdir), description: fmt.Sprintf("runner %s/%s in tmux session %s", workspace, window, session)}
		return a.executeRestartPlans(opts, []restartPlan{plan})
	}
	return fmt.Errorf("no runner row for %s/%s in %s", workspace, window, config.RunnerPath(opts.configPath))
}

func parseRestartArgs(usage string, args []string) (workspace, window, session string, err error) {
	workspace, window, session, err = parseRunnerParkArgs(args)
	if err != nil {
		return "", "", "", fmt.Errorf("usage: %s", usage)
	}
	return workspace, window, session, nil
}

func startCommandMatchesRunner(startCommand, workdir string) bool {
	normalized := normalizedTmuxStartCommand(startCommand)
	if normalized == tmux.RunnerCommand(workdir) {
		return true
	}
	return strings.Contains(normalized, "amp --no-tui") && strings.Contains(normalized, "cd "+shellSingleQuote(workdir)+" &&")
}

func (a app) store(opts options, args []string) error {
	if len(args) != 4 {
		return errors.New("usage: amux pin <workspace> <window> <workdir> <thread-id-or-url> (compatibility alias: store)")
	}
	row := config.Row{Workspace: args[0], Window: args[1], Workdir: args[2], Thread: args[3]}
	return a.storeRow(opts, row)
}

func (a app) storeCurrent(opts options, args []string) error {
	if len(args) < 1 || len(args) > 4 {
		return errors.New("usage: amux pin-current <thread-id-or-url> OR amux pin-current <workspace> <thread-id-or-url> [window] [workdir] (compatibility alias: store-current)")
	}

	workspace := defaultWorkspace
	thread := args[0]
	window := ""
	workdir := ""
	if len(args) >= 2 {
		workspace = args[0]
		thread = args[1]
	}
	if len(args) >= 3 {
		window = args[2]
	}
	if len(args) == 4 {
		workdir = args[3]
	}

	runner := tmux.Runner{}
	if window == "" {
		if os.Getenv("TMUX") == "" {
			return errors.New("current tmux window is unavailable: run inside tmux or pass window/workdir explicitly")
		}
		currentWindow, err := runner.CurrentWindow()
		if err != nil {
			return fmt.Errorf("current tmux window is unavailable: %w", err)
		}
		window = currentWindow
	}
	if workdir == "" {
		if os.Getenv("TMUX") == "" {
			return errors.New("current tmux pane path is unavailable: run inside tmux or pass window/workdir explicitly")
		}
		currentWorkdir, err := runner.CurrentWorkdir()
		if err != nil {
			return fmt.Errorf("current tmux pane path is unavailable: %w", err)
		}
		workdir = currentWorkdir
	}

	return a.storeRow(opts, config.Row{Workspace: workspace, Window: window, Workdir: workdir, Thread: thread})
}

func (a app) storeRow(opts options, row config.Row) error {
	replaced, err := config.Store(opts.configPath, row)
	if err != nil {
		return err
	}
	if replaced {
		fmt.Fprintf(a.stdout, "Updated %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
	} else {
		fmt.Fprintf(a.stdout, "Pinned %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
	}
	return nil
}

func (a app) remove(opts options, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: amux unpin <workspace> <window> (compatibility alias: remove)")
	}
	return a.removeRow(opts, args[0], args[1])
}

func (a app) removeCurrent(opts options, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: amux unpin-current [workspace] (compatibility alias: remove-current)")
	}
	workspace := defaultWorkspace
	if len(args) == 1 {
		workspace = args[0]
	}
	if os.Getenv("TMUX") == "" {
		return errors.New("current tmux window is unavailable: run inside tmux")
	}
	window, err := (tmux.Runner{}).CurrentWindow()
	if err != nil {
		return fmt.Errorf("current tmux window is unavailable: %w", err)
	}
	return a.removeRow(opts, workspace, window)
}

type parkPlan struct {
	key  string
	row  config.Row
	pane tmux.WindowPane
}

func (a app) park(opts options, args []string) error {
	workspaceScope := false
	positionalOnly := false
	if len(args) > 0 && args[0] == "--" {
		positionalOnly, args = true, args[1:]
		if len(args) == 0 {
			return errors.New("park -- requires at least one positional argument")
		}
	}
	if !positionalOnly && len(args) > 0 && strings.HasPrefix(args[0], "--workspace=") {
		workspaceScope, args = true, append([]string{strings.TrimPrefix(args[0], "--workspace=")}, args[1:]...)
	} else if len(args) > 0 && args[0] == "--workspace" {
		if len(args) < 2 {
			return errors.New("missing workspace")
		}
		workspaceScope, args = true, args[1:]
	}
	if workspaceScope && len(args) != 1 || !workspaceScope && len(args) > 3 {
		return errors.New("usage: amux park | amux park --workspace <workspace> | amux park <window> | amux park <workspace> <window> [session]")
	}
	for _, arg := range args {
		if positionalOnly {
			break
		}
		if strings.HasPrefix(arg, "-") {
			return fmt.Errorf("unknown park flag %q", arg)
		}
	}
	if workspaceScope && args[0] == "" {
		return errors.New("workspace cannot be empty")
	}

	rows, err := config.LoadReadOnly(opts.configPath)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no rows found in %s", opts.configPath)
	}
	workspace, window := "", ""
	if workspaceScope {
		workspace = args[0]
	} else if len(args) == 1 {
		workspace, window = defaultWorkspace, args[0]
	} else if len(args) >= 2 {
		workspace, window = args[0], args[1]
	}
	session := ""
	if len(args) > 2 {
		session = args[2]
	} else if window != "" {
		session = defaultSession
	}
	for name, value := range map[string]string{"workspace": workspace, "window": window, "session": session} {
		if value != "" {
			if err := config.ValidateField(name, value); err != nil {
				return err
			}
		}
	}
	selectedRows := 0
	for _, row := range rows {
		if workspace == "" || row.Workspace == workspace {
			selectedRows++
		}
	}
	if workspace != "" && selectedRows == 0 {
		return fmt.Errorf("no restore rows for workspace %q", workspace)
	}
	panes, err := (tmux.Runner{}).AllRestartWindowPanes()
	if err != nil {
		return fmt.Errorf("inventory tmux panes for park: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Workspace == rows[j].Workspace {
			return rows[i].Window < rows[j].Window
		}
		return rows[i].Workspace < rows[j].Workspace
	})
	plans := []parkPlan{}
	seenRows, seenPanes := map[restartConfigKey]bool{}, map[string]string{}
	for _, row := range rows {
		if workspace != "" && row.Workspace != workspace || window != "" && row.Window != window {
			continue
		}
		key := row.Workspace + "/" + row.Window
		ck := restartConfigKey{workspace: row.Workspace, window: row.Window}
		if seenRows[ck] {
			return fmt.Errorf("duplicate restore row %s; refusing park before changing any client", key)
		}
		seenRows[ck] = true
		matches := []tmux.WindowPane{}
		for _, pane := range panes {
			identity := teardownIdentity{Workspace: row.Workspace, Session: pane.Session, Window: row.Window, Thread: row.Thread}
			if !pane.Dead && pane.Window == row.Window && (session == "" || pane.Session == session) && explicitTeardownStartCommandMatches(identity, row, normalizedTmuxStartCommand(pane.StartCommand)) {
				matches = append(matches, pane)
			}
		}
		if len(matches) == 0 {
			fmt.Fprintf(a.stdout, "Skipping configured client %s: not running in tmux\n", key)
			continue
		}
		if len(matches) > 1 {
			return fmt.Errorf("ambiguous live tmux panes for %s: candidates %s; refusing park before changing any client", key, formatSessionPaneCandidates(matches))
		}
		pane := matches[0]
		paneCount := 0
		for _, candidate := range panes {
			if candidate.WindowID == pane.WindowID && !candidate.Dead {
				paneCount++
			}
		}
		if paneCount != 1 {
			return fmt.Errorf("park requires exactly one live pane in tmux window %s/%s; found %d", pane.Session, pane.Window, paneCount)
		}
		if pane.PaneID == "" || pane.WindowID == "" {
			return fmt.Errorf("tmux target for %s has an empty pane or window id; refusing park before changing any client", key)
		}
		if owner, ok := seenPanes[pane.PaneID]; ok {
			return fmt.Errorf("tmux pane %s is claimed by both %s and %s; refusing park before changing any client", pane.PaneID, owner, key)
		}
		seenPanes[pane.PaneID] = key
		plans = append(plans, parkPlan{key: key, row: row, pane: pane})
	}
	if window != "" && len(plans) == 0 {
		return fmt.Errorf("no verified live client for %s/%s in session %s", workspace, window, session)
	}
	if len(plans) == 0 {
		fmt.Fprintln(a.stdout, "No configured clients are currently running in tmux.")
		return nil
	}
	// Revalidate the complete plan before scheduling any asynchronous mutation.
	for _, plan := range plans {
		current, e := (tmux.Runner{}).RestartPaneByID(plan.pane.PaneID)
		if e != nil || current.Dead || current.PaneID != plan.pane.PaneID || current.WindowID != plan.pane.WindowID || current.Session != plan.pane.Session || current.Window != plan.pane.Window || normalizedTmuxStartCommand(current.StartCommand) != normalizedTmuxStartCommand(plan.pane.StartCommand) {
			return fmt.Errorf("park precondition changed for %s; scheduled: 0/%d, not scheduled: %d", plan.key, len(plans), len(plans))
		}
	}
	for i, plan := range plans {
		if err = a.schedulePark(tmux.Runner{DryRun: opts.dryRun, Output: a.stdout}, plan); err != nil {
			return fmt.Errorf("park failed for %s; scheduled: %d/%d, not scheduled: %d: %w", plan.key, i, len(plans), len(plans)-i, err)
		}
		if opts.dryRun {
			fmt.Fprintf(a.stdout, "Planned %d/%d: %s\n", i+1, len(plans), plan.key)
		} else {
			fmt.Fprintf(a.stdout, "Scheduled %d/%d: %s\n", i+1, len(plans), plan.key)
		}
	}
	return nil
}

func (a app) parkCurrent(opts options, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: amux park-current [workspace]")
	}
	workspace := defaultWorkspace
	if len(args) == 1 {
		workspace = args[0]
	}
	if os.Getenv("TMUX") == "" {
		return errors.New("current tmux window is unavailable: run inside tmux")
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return errors.New("TMUX_PANE is unavailable; run amux from the pane you want to target")
	}
	runner := tmux.Runner{DryRun: opts.dryRun, Output: a.stdout}
	pane, err := (tmux.Runner{}).RestartPaneByID(paneID)
	if err != nil {
		return fmt.Errorf("current tmux target is unavailable: %w", err)
	}
	panes, err := (tmux.Runner{}).RestartWindowPanes(pane.Session, pane.Window)
	if err != nil {
		return fmt.Errorf("inspect invoking tmux window %s/%s: %w", pane.Session, pane.Window, err)
	}
	if len(panes) != 1 {
		return fmt.Errorf("park-current requires exactly one pane in invoking tmux window %s/%s; found %d", pane.Session, pane.Window, len(panes))
	}
	revalidated := panes[0]
	if revalidated.Dead || revalidated.PaneID != pane.PaneID || revalidated.WindowID != pane.WindowID || revalidated.Session != pane.Session || revalidated.Window != pane.Window || normalizedTmuxStartCommand(revalidated.StartCommand) != normalizedTmuxStartCommand(pane.StartCommand) {
		return fmt.Errorf("park-current precondition changed for invoking pane %s; refusing to schedule shutdown", paneID)
	}
	row, err := verifiedRestartRow(opts.configPath, teardownIdentity{Workspace: workspace, Window: pane.Window})
	if err != nil {
		return err
	}
	identity := teardownIdentity{Workspace: row.Workspace, Session: pane.Session, Window: row.Window, Thread: row.Thread}
	if !explicitTeardownStartCommandMatches(identity, row, normalizedTmuxStartCommand(revalidated.StartCommand)) {
		return fmt.Errorf("tmux pane %s start command does not match restore row %s/%s", paneID, row.Workspace, row.Window)
	}
	return a.schedulePark(runner, parkPlan{key: workspace + "/" + pane.Window, row: row, pane: revalidated})
}

func (a app) restart(opts options, args []string) error {
	if len(args) == 0 {
		return a.restartAllThreads(opts)
	}
	workspace, window, session, err := parseRestartArgs("amux restart [workspace] <window> [session]", args)
	if err != nil {
		return err
	}
	identity := teardownIdentity{Workspace: workspace, Session: session, Window: window}
	row, err := verifiedRestartRow(opts.configPath, identity)
	if err != nil {
		return err
	}
	identity.Thread = row.Thread
	workdir := config.ExpandHome(row.Workdir)
	if stat, err := os.Stat(workdir); err != nil || !stat.IsDir() {
		return fmt.Errorf("missing workdir for window %q: %s", row.Window, workdir)
	}
	panes, err := (tmux.Runner{}).RestartWindowPanes(session, window)
	if err != nil {
		return fmt.Errorf("find tmux window %s/%s: %w", session, window, err)
	}
	if len(panes) != 1 {
		return fmt.Errorf("restart requires exactly one pane in tmux window %s/%s; found %d", session, window, len(panes))
	}
	if panes[0].Dead {
		return fmt.Errorf("pane %s in tmux window %s/%s is dead and not currently running", panes[0].PaneID, session, window)
	}
	if !explicitTeardownStartCommandMatches(identity, row, normalizedTmuxStartCommand(panes[0].StartCommand)) {
		return fmt.Errorf("tmux window %q in session %q start command does not match restore row thread %s; refusing restart", window, session, row.Thread)
	}
	if panes[0].PaneID == "" || panes[0].WindowID == "" {
		return fmt.Errorf("tmux window %q in session %q has no pane or window id", window, session)
	}
	command := teardownExpectedStartCommand(identity, row)
	plan := restartPlan{key: workspace + "/" + window, pane: panes[0], command: command, description: fmt.Sprintf("Amp thread %s in tmux window %s/%s", canonicalThreadID(row.Thread), session, window)}
	return a.executeRestartPlans(opts, []restartPlan{plan})
}

func verifiedRestartRow(path string, identity teardownIdentity) (config.Row, error) {
	rows, err := config.LoadReadOnly(path)
	if err != nil {
		return config.Row{}, err
	}
	var matches []config.Row
	for _, row := range rows {
		if row.Workspace == identity.Workspace && row.Window == identity.Window {
			matches = append(matches, row)
		}
	}
	if len(matches) == 0 {
		return config.Row{}, fmt.Errorf("no restore row for %s/%s", identity.Workspace, identity.Window)
	}
	if len(matches) > 1 {
		return config.Row{}, fmt.Errorf("ambiguous restore rows for %s/%s; refusing restart", identity.Workspace, identity.Window)
	}
	return matches[0], nil
}

func (a app) respawnVerifiedPane(opts options, paneID, command, description string) error {
	if opts.dryRun {
		fmt.Fprintf(a.stdout, "Would restart %s\n", description)
		return (tmux.Runner{DryRun: true, Output: a.stdout}).RespawnPane(paneID, command)
	}
	fmt.Fprintf(a.stdout, "Restarting %s\n", description)
	runner := tmux.Runner{}
	if err := runner.RespawnPane(paneID, command); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	pane, err := runner.RestartPaneByID(paneID)
	if err != nil {
		return fmt.Errorf("old client was terminated, but verify replacement pane %s: %w", paneID, err)
	}
	if pane.Dead || normalizedTmuxStartCommand(pane.StartCommand) != command {
		return fmt.Errorf("old client was terminated, but the replacement in pane %s exited immediately; config and remote thread were preserved", paneID)
	}
	return nil
}

type restartPlan struct {
	key         string
	pane        tmux.WindowPane
	command     string
	description string
}

type restartConfigKey struct {
	workspace string
	window    string
}

func (a app) restartAllThreads(opts options) error {
	rows, err := config.LoadReadOnly(opts.configPath)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no rows found in %s", opts.configPath)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Workspace == rows[j].Workspace {
			return rows[i].Window < rows[j].Window
		}
		return rows[i].Workspace < rows[j].Workspace
	})
	panes, err := (tmux.Runner{}).AllRestartWindowPanes()
	if err != nil {
		return fmt.Errorf("inventory tmux panes for restart: %w", err)
	}
	plans := make([]restartPlan, 0, len(rows))
	seenKeys := make(map[restartConfigKey]bool)
	seenPanes := make(map[string]string)
	for _, row := range rows {
		key := row.Workspace + "/" + row.Window
		configKey := restartConfigKey{workspace: row.Workspace, window: row.Window}
		if seenKeys[configKey] {
			return fmt.Errorf("duplicate restore row %s; refusing system-wide restart before changing any client", key)
		}
		seenKeys[configKey] = true
		workdir := config.ExpandHome(row.Workdir)
		if stat, err := os.Stat(workdir); err != nil || !stat.IsDir() {
			return fmt.Errorf("missing workdir for %s: %s; refusing system-wide restart before changing any client", key, workdir)
		}
		matches := make([]tmux.WindowPane, 0, 1)
		for _, pane := range panes {
			identity := teardownIdentity{Workspace: row.Workspace, Session: pane.Session, Window: row.Window, Thread: row.Thread}
			if !pane.Dead && pane.Window == row.Window && explicitTeardownStartCommandMatches(identity, row, normalizedTmuxStartCommand(pane.StartCommand)) {
				matches = append(matches, pane)
			}
		}
		if len(matches) == 0 {
			fmt.Fprintf(a.stdout, "Skipping configured client %s: not running in tmux\n", key)
			continue
		}
		if len(matches) > 1 {
			return fmt.Errorf("ambiguous live tmux panes for %s: candidates %s; refusing system-wide restart before changing any client", key, formatSessionPaneCandidates(matches))
		}
		pane := matches[0]
		if pane.PaneID == "" || pane.WindowID == "" {
			return fmt.Errorf("tmux target for %s has an empty pane or window id; refusing system-wide restart before changing any client", key)
		}
		if owner, exists := seenPanes[pane.PaneID]; exists {
			return fmt.Errorf("tmux pane %s is claimed by both %s and %s; refusing system-wide restart before changing any client", pane.PaneID, owner, key)
		}
		seenPanes[pane.PaneID] = key
		identity := teardownIdentity{Workspace: row.Workspace, Session: pane.Session, Window: row.Window, Thread: row.Thread}
		plans = append(plans, restartPlan{key: key, pane: pane, command: teardownExpectedStartCommand(identity, row), description: fmt.Sprintf("Amp thread %s in tmux window %s/%s", canonicalThreadID(row.Thread), pane.Session, row.Window)})
	}
	return a.executeRestartPlans(opts, plans)
}

func (a app) restartAllRunners(opts options) error {
	rows, err := config.LoadRunnersReadOnly(config.RunnerPath(opts.configPath))
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no runner rows found in %s", config.RunnerPath(opts.configPath))
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Workspace == rows[j].Workspace {
			return rows[i].Window < rows[j].Window
		}
		return rows[i].Workspace < rows[j].Workspace
	})
	panes, err := (tmux.Runner{}).AllRestartWindowPanes()
	if err != nil {
		return fmt.Errorf("inventory tmux panes for runner restart: %w", err)
	}
	plans := make([]restartPlan, 0, len(rows))
	seenKeys := make(map[restartConfigKey]bool)
	seenPanes := make(map[string]string)
	for _, row := range rows {
		key := row.Workspace + "/" + row.Window
		configKey := restartConfigKey{workspace: row.Workspace, window: row.Window}
		if seenKeys[configKey] {
			return fmt.Errorf("duplicate runner row %s; refusing system-wide restart before changing any runner", key)
		}
		seenKeys[configKey] = true
		workdir := config.ExpandHome(row.Workdir)
		if stat, err := os.Stat(workdir); err != nil || !stat.IsDir() {
			return fmt.Errorf("missing runner workdir for %s: %s; refusing system-wide restart before changing any runner", key, workdir)
		}
		command := tmux.RunnerCommand(workdir)
		matches := make([]tmux.WindowPane, 0, 1)
		for _, pane := range panes {
			if !pane.Dead && pane.Window == row.Window && normalizedTmuxStartCommand(pane.StartCommand) == command {
				matches = append(matches, pane)
			}
		}
		if len(matches) == 0 {
			fmt.Fprintf(a.stdout, "Skipping configured runner %s: not running in tmux\n", key)
			continue
		}
		if len(matches) > 1 {
			return fmt.Errorf("ambiguous live tmux panes for runner %s: candidates %s; refusing system-wide restart before changing any runner", key, formatSessionPaneCandidates(matches))
		}
		pane := matches[0]
		if pane.PaneID == "" || pane.WindowID == "" {
			return fmt.Errorf("tmux target for runner %s has an empty pane or window id; refusing system-wide restart before changing any runner", key)
		}
		if owner, exists := seenPanes[pane.PaneID]; exists {
			return fmt.Errorf("tmux pane %s is claimed by both %s and %s; refusing system-wide restart before changing any runner", pane.PaneID, owner, key)
		}
		seenPanes[pane.PaneID] = key
		plans = append(plans, restartPlan{key: key, pane: pane, command: command, description: fmt.Sprintf("runner %s in tmux session %s", key, pane.Session)})
	}
	return a.executeRestartPlans(opts, plans)
}

func (a app) executeRestartPlans(opts options, plans []restartPlan) error {
	if len(plans) == 0 {
		fmt.Fprintln(a.stdout, "No configured clients are currently running in tmux.")
		return nil
	}
	for i, plan := range plans {
		current, err := (tmux.Runner{}).RestartPaneByID(plan.pane.PaneID)
		if err != nil || current.Dead || current.PaneID != plan.pane.PaneID || current.WindowID != plan.pane.WindowID || current.Session != plan.pane.Session || current.Window != plan.pane.Window || normalizedTmuxStartCommand(current.StartCommand) != normalizedTmuxStartCommand(plan.pane.StartCommand) {
			return fmt.Errorf("restart precondition changed for %s; completed: %d/%d, not attempted: %d", plan.key, i, len(plans), len(plans)-i)
		}
		if err := a.respawnVerifiedPane(opts, plan.pane.PaneID, plan.command, plan.description); err != nil {
			return fmt.Errorf("restart failed for %s; completed: %d/%d, not attempted: %d: %w", plan.key, i, len(plans), len(plans)-i-1, err)
		}
		if opts.dryRun {
			fmt.Fprintf(a.stdout, "Planned %d/%d: %s\n", i+1, len(plans), plan.key)
		} else {
			fmt.Fprintf(a.stdout, "Restarted %d/%d: %s\n", i+1, len(plans), plan.key)
		}
	}
	return nil
}

func (a app) schedulePark(runner tmux.Runner, plan parkPlan) error {
	verb := "Scheduling"
	if runner.DryRun {
		verb = "Would schedule"
	}
	fmt.Fprintf(a.stdout, "%s tmux window %s/%s (%s) to stop\n", verb, plan.pane.Session, plan.pane.Window, plan.pane.WindowID)
	fmt.Fprintf(a.stdout, "Restore config row %s/%s is preserved; use amux unpin %s %s to remove it.\n", plan.row.Workspace, plan.row.Window, plan.row.Workspace, plan.row.Window)
	fmt.Fprintln(a.stdout, "Amp thread history is not deleted; parking only stops the local tmux/Amp session.")
	if err := runner.RunShell(parkShutdownScript(plan.pane, parkShutdownDelay(), parkGracePeriod())); err != nil {
		return fmt.Errorf("schedule tmux window %s shutdown: %w", plan.pane.WindowID, err)
	}
	wording := "will be asked"
	if runner.DryRun {
		wording = "would be asked"
	}
	fmt.Fprintf(a.stdout, "The local Amp process %s to exit in %s; tmux will force-close it only if graceful shutdown times out.\n", wording, parkShutdownDelay())
	return nil
}

func (a app) shelve(opts options, args []string) error {
	shelveArgs, err := parseShelveArgs(args)
	if err != nil {
		return err
	}
	readRunner := tmux.Runner{}
	targets, err := shelveTargets(opts.configPath, readRunner, shelveArgs)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Fprintf(a.stdout, "No rows found to shelve in %s\n", opts.configPath)
		return nil
	}

	if opts.dryRun {
		for _, target := range targets {
			a.printShelvePlan("Would", opts.configPath, target)
		}
		return nil
	}
	runner := tmux.Runner{}
	for _, target := range targets {
		archiveThread := canonicalThreadID(target.row.Thread)
		if err := archiveAmpThread(archiveThread); err != nil {
			return fmt.Errorf("archive Amp thread %s: %w", archiveThread, err)
		}
		fmt.Fprintf(a.stdout, "Shelved Amp thread %s\n", archiveThread)
		fmt.Fprintf(a.stdout, "Restore config row %s/%s is preserved in %s\n", target.row.Workspace, target.row.Window, opts.configPath)
		if target.pane != nil {
			if err := runner.KillWindow(target.pane.WindowID); err != nil {
				return fmt.Errorf("Amp thread %s was archived, but stop tmux window %s (%s) failed: %w", archiveThread, target.identity.Window, target.pane.WindowID, err)
			}
			fmt.Fprintf(a.stdout, "Stopped tmux window %s/%s (%s)\n", target.identity.Session, target.identity.Window, target.pane.WindowID)
		} else if target.identity.Session != "" {
			fmt.Fprintf(a.stdout, "No live tmux window %s/%s found to stop\n", target.identity.Session, target.identity.Window)
		} else {
			fmt.Fprintf(a.stdout, "No live tmux window for %s/%s found to stop\n", target.row.Workspace, target.row.Window)
		}
		fmt.Fprintf(a.stdout, "Run amux unshelve %s %s, then %s to restore it.\n", target.row.Workspace, target.row.Window, shelveLaunchCommand(target))
	}
	return nil
}

func (a app) shelveCurrent(opts options, args []string) error {
	workspace, thread, err := parseShelveCurrentArgs(args)
	if err != nil {
		return err
	}
	row, rowExists, err := shelveCurrentExistingThreadRow(opts.configPath, thread)
	if err != nil {
		return err
	}
	if os.Getenv("TMUX") == "" {
		return errors.New("current tmux window is unavailable: run inside tmux")
	}

	runner := tmux.Runner{DryRun: opts.dryRun}
	windowID, err := runner.CurrentWindowID()
	if err != nil {
		return fmt.Errorf("current tmux window id is unavailable: %w", err)
	}
	if !rowExists {
		window, err := runner.CurrentWindow()
		if err != nil {
			return fmt.Errorf("current tmux window is unavailable: %w", err)
		}
		workdir, err := runner.CurrentWorkdir()
		if err != nil {
			return fmt.Errorf("current tmux pane path is unavailable: %w", err)
		}
		row = config.Row{Workspace: workspace, Window: window, Workdir: workdir, Thread: thread}
		if err := row.Validate(); err != nil {
			return err
		}
		storedRow, storedRowExists, err := shelveCurrentStoredRow(opts.configPath, row)
		if err != nil {
			return err
		}
		row = storedRow
		rowExists = storedRowExists
	}

	archiveThread := canonicalThreadID(thread)
	if opts.dryRun {
		if rowExists {
			fmt.Fprintf(a.stdout, "Would preserve existing restore row %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
		} else {
			fmt.Fprintf(a.stdout, "Would pin current restore row %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
		}
		fmt.Fprintf(a.stdout, "Would archive Amp thread %s\n", archiveThread)
		fmt.Fprintf(a.stdout, "Would stop current tmux window %s (%s)\n", row.Window, windowID)
		return nil
	}
	if rowExists {
		fmt.Fprintf(a.stdout, "Restore config row %s/%s already exists in %s\n", row.Workspace, row.Window, opts.configPath)
	} else {
		if _, err := config.Store(opts.configPath, row); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "Pinned %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
	}
	if err := archiveAmpThread(archiveThread); err != nil {
		return fmt.Errorf("archive Amp thread %s: %w; restore row %s/%s is preserved and current tmux window %s was not stopped", archiveThread, err, row.Workspace, row.Window, windowID)
	}
	fmt.Fprintf(a.stdout, "Shelved Amp thread %s\n", archiveThread)
	fmt.Fprintf(a.stdout, "Restore config row %s/%s is preserved in %s\n", row.Workspace, row.Window, opts.configPath)
	if err := runner.KillWindow(windowID); err != nil {
		return fmt.Errorf("Amp thread %s was archived, but stop current tmux window %s (%s) failed: %w", archiveThread, row.Window, windowID, err)
	}
	fmt.Fprintf(a.stdout, "Stopped current tmux window %s (%s)\n", row.Window, windowID)
	fmt.Fprintf(a.stdout, "Run amux unshelve %s %s, then %s to restore it.\n", row.Workspace, row.Window, shelveLaunchCommand(shelveTarget{row: row}))
	return nil
}

func parseShelveCurrentArgs(args []string) (string, string, error) {
	if len(args) > 2 {
		return "", "", errors.New("usage: amux shelve-current [workspace] [thread-id-or-url]")
	}
	workspace := defaultWorkspace
	if envWorkspace := os.Getenv("AMUX_WORKSPACE"); envWorkspace != "" {
		workspace = envWorkspace
	}
	thread := os.Getenv("AMUX_THREAD_ID")
	if len(args) == 1 {
		if looksLikeThreadIDOrURL(args[0]) {
			thread = args[0]
		} else if thread != "" {
			workspace = args[0]
		} else {
			return "", "", errors.New("shelve-current with one non-thread argument requires AMUX_THREAD_ID; use amux shelve-current <thread-id-or-url> or amux shelve-current <workspace> <thread-id-or-url>")
		}
	} else if len(args) == 2 {
		workspace = args[0]
		thread = args[1]
	}
	if thread == "" {
		return "", "", errors.New("shelve-current requires a thread id or URL; run amux shelve-current <thread-id-or-url> or set AMUX_THREAD_ID")
	}
	if err := config.ValidateField("workspace", workspace); err != nil {
		return "", "", err
	}
	if err := config.ValidateField("thread", thread); err != nil {
		return "", "", err
	}
	return workspace, thread, nil
}

func looksLikeThreadIDOrURL(value string) bool {
	return strings.HasPrefix(canonicalThreadID(value), "T-") || strings.Contains(value, "ampcode.com/threads/")
}

func shelveCurrentExistingThreadRow(path, thread string) (config.Row, bool, error) {
	rows, err := config.LoadReadOnly(path)
	if err != nil {
		return config.Row{}, false, err
	}
	threadID := canonicalThreadID(thread)
	matches := make([]config.Row, 0, 1)
	for _, row := range rows {
		if canonicalThreadID(row.Thread) == threadID {
			matches = append(matches, row)
		}
	}
	if len(matches) == 0 {
		return config.Row{}, false, nil
	}
	if len(matches) > 1 {
		return config.Row{}, false, fmt.Errorf("ambiguous restore rows for thread %s: candidates %s; refusing shelve-current. Use amux shelve <workspace> <window> to shelve a specific restore row", thread, formatRowCandidates(matches))
	}
	return matches[0], true, nil
}

func shelveCurrentStoredRow(path string, row config.Row) (config.Row, bool, error) {
	rows, err := config.LoadReadOnly(path)
	if err != nil {
		return config.Row{}, false, err
	}
	for _, existing := range rows {
		if existing.Workspace != row.Workspace || existing.Window != row.Window {
			continue
		}
		if canonicalThreadID(existing.Thread) != canonicalThreadID(row.Thread) {
			return config.Row{}, false, fmt.Errorf("restore row %s/%s already points at thread %s; refusing to shelve current thread %s without replacing it. Use amux pin-current %s %s %s %s if you intentionally want to repoint the row, then retry shelve", row.Workspace, row.Window, existing.Thread, row.Thread, row.Workspace, row.Thread, row.Window, row.Workdir)
		}
		return existing, true, nil
	}
	return row, false, nil
}

func (a app) unshelve(opts options, args []string) error {
	if opts.dryRun {
		return errors.New("unshelve does not support --dry-run")
	}
	unshelveArgs, err := parseUnshelveArgs(args)
	if err != nil {
		return err
	}
	rows, err := rowsForShelveSelection(opts.configPath, unshelveArgs)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintf(a.stdout, "No rows found to unshelve in %s\n", opts.configPath)
		return nil
	}
	for _, row := range rows {
		thread := canonicalThreadID(row.Thread)
		if err := unarchiveAmpThread(thread); err != nil {
			return fmt.Errorf("unarchive Amp thread %s: %w", thread, err)
		}
		fmt.Fprintf(a.stdout, "Unshelved Amp thread %s\n", thread)
		fmt.Fprintf(a.stdout, "Restore config row %s/%s remains in %s\n", row.Workspace, row.Window, opts.configPath)
	}
	return nil
}

func parseUnshelveArgs(args []string) (shelveArgs, error) {
	parsed, err := parseShelveArgs(args)
	if err != nil {
		return parsed, errors.New(strings.ReplaceAll(err.Error(), "shelve", "unshelve"))
	}
	if parsed.sessionSet {
		return parsed, errors.New("unshelve does not support --session")
	}
	if parsed.thread == "" && parsed.workspace == "" && len(parsed.positional) == 3 {
		return parsed, errors.New("usage: amux unshelve [workspace] <window> OR amux unshelve --thread <thread-id-or-url> OR amux unshelve --workspace <workspace>")
	}
	return parsed, nil
}

type shelveArgs struct {
	positional []string
	thread     string
	workspace  string
	session    string
	sessionSet bool
}

type shelveTarget struct {
	row      config.Row
	identity teardownIdentity
	pane     *tmux.WindowPane
}

func parseShelveArgs(args []string) (shelveArgs, error) {
	parsed := shelveArgs{positional: make([]string, 0, len(args)), session: defaultSession}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--thread":
			i++
			if i >= len(args) || args[i] == "" {
				return parsed, errors.New("--thread requires a thread id or URL")
			}
			parsed.thread = args[i]
		case "--workspace":
			i++
			if i >= len(args) || args[i] == "" {
				return parsed, errors.New("--workspace requires a workspace")
			}
			parsed.workspace = args[i]
		case "--session":
			i++
			if i >= len(args) || args[i] == "" {
				return parsed, errors.New("--session requires a tmux session name")
			}
			parsed.session = args[i]
			parsed.sessionSet = true
		default:
			if strings.HasPrefix(args[i], "--thread=") {
				parsed.thread = strings.TrimPrefix(args[i], "--thread=")
				if parsed.thread == "" {
					return parsed, errors.New("--thread requires a thread id or URL")
				}
			} else if strings.HasPrefix(args[i], "--workspace=") {
				parsed.workspace = strings.TrimPrefix(args[i], "--workspace=")
				if parsed.workspace == "" {
					return parsed, errors.New("--workspace requires a workspace")
				}
			} else if strings.HasPrefix(args[i], "--session=") {
				parsed.session = strings.TrimPrefix(args[i], "--session=")
				if parsed.session == "" {
					return parsed, errors.New("--session requires a tmux session name")
				}
				parsed.sessionSet = true
			} else if strings.HasPrefix(args[i], "--") {
				return parsed, fmt.Errorf("unknown shelve option %s", args[i])
			} else {
				parsed.positional = append(parsed.positional, args[i])
			}
		}
	}
	if parsed.thread != "" && parsed.workspace != "" {
		return parsed, errors.New("shelve accepts either --thread or --workspace, not both")
	}
	if (parsed.thread != "" || parsed.workspace != "") && len(parsed.positional) != 0 {
		return parsed, errors.New("usage: amux shelve [workspace] <window> [session] OR amux shelve --thread <thread-id-or-url> [--session <session>] OR amux shelve --workspace <workspace> [--session <session>]")
	}
	if parsed.thread == "" && parsed.workspace == "" {
		switch len(parsed.positional) {
		case 2:
			parsed.session = parsed.positional[0]
		case 3:
			parsed.session = parsed.positional[2]
		}
	}
	if parsed.thread != "" {
		if err := config.ValidateField("thread", parsed.thread); err != nil {
			return parsed, err
		}
	}
	if parsed.workspace != "" {
		if err := config.ValidateField("workspace", parsed.workspace); err != nil {
			return parsed, err
		}
		if !parsed.sessionSet {
			parsed.session = parsed.workspace
		}
	}
	if parsed.session != "" {
		if err := config.ValidateField("session", parsed.session); err != nil {
			return parsed, err
		}
	}
	if parsed.thread == "" && parsed.workspace == "" && (len(parsed.positional) == 0 || len(parsed.positional) > 3) {
		return parsed, errors.New("usage: amux shelve [workspace] <window> [session]")
	}
	return parsed, nil
}

func shelveTargets(path string, runner tmux.Runner, args shelveArgs) ([]shelveTarget, error) {
	rows, err := rowsForShelveSelection(path, args)
	if err != nil {
		return nil, err
	}
	targets := make([]shelveTarget, 0, len(rows))
	for _, row := range rows {
		identity := teardownIdentity{Workspace: row.Workspace, Window: row.Window, Thread: row.Thread, Session: args.session}
		if args.thread != "" && !args.sessionSet {
			identity.Session = ""
		}
		target, err := verifiedShelveTarget(runner, identity, row)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func rowsForShelveSelection(path string, args shelveArgs) ([]config.Row, error) {
	if args.thread != "" {
		row, err := verifiedTeardownRowByThread(path, args.thread)
		if err != nil {
			return nil, err
		}
		return []config.Row{row}, nil
	}
	if args.workspace != "" {
		rows, err := rowsForWorkspaceReadOnly(path, args.workspace)
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
	identity, err := teardownIdentityFromArgs(shelvePositionalArgs(args.positional))
	if err != nil {
		return nil, err
	}
	row, err := verifiedTeardownRow(path, identity)
	if err != nil {
		return nil, shelveSelectionError(err)
	}
	return []config.Row{row}, nil
}

func shelveSelectionError(err error) error {
	if err == nil || !strings.Contains(err.Error(), "no restore row for ") {
		return err
	}
	if os.Getenv("TMUX") == "" {
		return err
	}
	runner := tmux.Runner{}
	window, windowErr := runner.CurrentWindow()
	workdir, workdirErr := runner.CurrentWorkdir()
	if windowErr != nil || workdirErr != nil {
		return fmt.Errorf("%w. Shelve is row-based and will not archive a live unpinned window; run amux pin-current <thread-id-or-url> first, or run amux shelve-current <thread-id-or-url> from the target tmux pane", err)
	}
	return fmt.Errorf("%w. Current tmux window %q at %q is not pinned in restore config; shelve is row-based and will not archive a live unpinned window. Run amux shelve-current <thread-id-or-url> to pin, archive, and stop the current window, or run amux pin-current <thread-id-or-url> before shelving by row", err, window, workdir)
}

func shelvePositionalArgs(args []string) []string {
	if len(args) == 1 {
		return []string{defaultWorkspace, args[0], defaultSession}
	}
	return args
}

func verifiedShelveTarget(runner tmux.Runner, identity teardownIdentity, row config.Row) (shelveTarget, error) {
	panes, err := livePanesForShelve(runner, identity)
	if err != nil && !runner.DryRun {
		return shelveTarget{}, fmt.Errorf("find tmux window %s/%s: %w", identity.Session, identity.Window, err)
	}
	if len(panes) == 0 {
		return shelveTarget{identity: identity, row: row}, nil
	}
	if identity.Session == "" {
		verified := make([]tmux.WindowPane, 0, 1)
		for _, pane := range panes {
			candidate := identity
			candidate.Session = pane.Session
			if explicitTeardownStartCommandMatches(candidate, row, normalizedTmuxStartCommand(pane.StartCommand)) {
				verified = appendUniqueTeardownWindow(verified, pane)
			}
		}
		if len(verified) == 0 {
			return shelveTarget{}, fmt.Errorf("no live tmux window for thread %s matches restore row %s/%s; pass --session if it is in a specific tmux session", row.Thread, row.Workspace, row.Window)
		}
		if len(verified) > 1 {
			return shelveTarget{}, fmt.Errorf("ambiguous live tmux windows for thread %s: candidates %s; pass --session to choose one", row.Thread, formatSessionPaneCandidates(verified))
		}
		identity.Session = verified[0].Session
		return shelveTarget{identity: identity, row: row, pane: &verified[0]}, nil
	}
	verified, err := verifiedTeardownPane(runner, identity, row)
	if err != nil {
		return shelveTarget{}, err
	}
	return shelveTarget{identity: identity, row: row, pane: &verified}, nil
}

func livePanesForShelve(runner tmux.Runner, identity teardownIdentity) ([]tmux.WindowPane, error) {
	if identity.Session != "" {
		if !runner.HasSession(identity.Session) {
			return nil, nil
		}
		return runner.WindowPanes(identity.Session, identity.Window)
	}
	panes, err := livePanesForTeardown(runner, identity)
	if err != nil && isTmuxUnavailable(err) {
		return nil, nil
	}
	return panes, err
}

func isTmuxUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no server running") ||
		strings.Contains(message, "failed to connect") ||
		strings.Contains(message, "can't find session") ||
		strings.Contains(message, "no such file or directory")
}

func (a app) printShelvePlan(prefix, path string, target shelveTarget) {
	archiveThread := canonicalThreadID(target.row.Thread)
	fmt.Fprintf(a.stdout, "%s archive Amp thread %s and preserve restore row %s/%s in %s\n", prefix, archiveThread, target.row.Workspace, target.row.Window, path)
	if target.pane != nil {
		fmt.Fprintf(a.stdout, "%s stop tmux window %s/%s (%s)\n", prefix, target.identity.Session, target.identity.Window, target.pane.WindowID)
	} else if target.identity.Session != "" {
		fmt.Fprintf(a.stdout, "No live tmux window %s/%s found to stop\n", target.identity.Session, target.identity.Window)
	} else {
		fmt.Fprintf(a.stdout, "No live tmux window for %s/%s found to stop\n", target.row.Workspace, target.row.Window)
	}
}

func shelveLaunchCommand(target shelveTarget) string {
	if target.identity.Session != "" {
		return fmt.Sprintf("amux launch %s %s", target.row.Workspace, target.identity.Session)
	}
	return fmt.Sprintf("amux launch %s", target.row.Workspace)
}

func parkShutdownScript(pane tmux.WindowPane, delay, grace time.Duration) string {
	return strings.Join([]string{
		"target=" + shellSingleQuote(pane.WindowID), // compatibility label for diagnostics
		"pane=" + shellSingleQuote(pane.PaneID),
		"window=" + shellSingleQuote(pane.WindowID),
		"session=" + shellSingleQuote(pane.Session),
		"start=" + shellSingleQuote(pane.StartCommand),
		"expected=$(printf '%s\\t%s\\t%s\\t%s\\t%s' \"$session\" \"$window\" \"$pane\" '1' \"$start\")",
		"sleep " + shellSeconds(delay),
		"verify() { [ \"$(tmux display-message -p -t \"$pane\" '#{session_name}\t#{window_id}\t#{pane_id}\t#{window_panes}\t#{pane_start_command}' 2>/dev/null)\" = \"$expected\" ]; }",
		"verify || exit 0",
		"tmux send-keys -t \"$pane\" C-c >/dev/null 2>&1 || exit 0",
		"sleep 0.200",
		"verify || exit 0",
		"tmux send-keys -t \"$pane\" C-d >/dev/null 2>&1 || exit 0",
		"deadline=$(( $(date +%s) + " + fmt.Sprintf("%.0f", grace.Seconds()) + " ))",
		"while verify; do",
		"  if [ \"$(date +%s)\" -ge \"$deadline\" ]; then",
		"    verify || exit 0",
		"    tmux kill-window -t \"$window\" >/dev/null 2>&1 || true",
		"    if tmux display-message -p -t \"$window\" '#{window_id}' >/dev/null 2>&1; then",
		"      tmux display-message \"amux warning: tmux window $window is still live after park shutdown\" >/dev/null 2>&1 || true",
		"    fi",
		"    exit 0",
		"  fi",
		"  sleep 0.100",
		"done",
	}, "\n")
}

func parkShutdownDelay() time.Duration {
	value := os.Getenv("AMUX_PARK_SHUTDOWN_DELAY")
	if value == "" {
		return 5 * time.Second
	}
	delay, err := time.ParseDuration(value)
	if err == nil {
		return delay
	}
	seconds, err := time.ParseDuration(value + "s")
	if err == nil {
		return seconds
	}
	return 5 * time.Second
}

func shellSeconds(duration time.Duration) string {
	return fmt.Sprintf("%.3f", duration.Seconds())
}

func shellSingleQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func parkGracePeriod() time.Duration {
	value := os.Getenv("AMUX_PARK_GRACE_PERIOD")
	if value == "" {
		return 5 * time.Second
	}
	delay, err := time.ParseDuration(value)
	if err == nil {
		return delay
	}
	seconds, err := time.ParseDuration(value + "s")
	if err == nil {
		return seconds
	}
	return 5 * time.Second
}

func (a app) removeRow(opts options, workspace, window string) error {
	removed, err := config.Remove(opts.configPath, workspace, window)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(a.stdout, "Unpinned %s/%s from %s\n", workspace, window, opts.configPath)
	} else {
		fmt.Fprintf(a.stdout, "No row found for %s/%s in %s\n", workspace, window, opts.configPath)
	}
	return nil
}

func (a app) pruneArchived(opts options, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: amux prune-archived [workspace]")
	}
	workspace := defaultWorkspace
	if len(args) == 1 {
		workspace = args[0]
	}
	rows, err := rowsForWorkspaceReadOnly(opts.configPath, workspace)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintf(a.stdout, "No rows found for workspace %s in %s\n", workspace, opts.configPath)
		return nil
	}
	statuses, err := threadArchiveStatuses(rows)
	if err != nil {
		return fmt.Errorf("confirm archived Amp threads: %w", err)
	}
	archived := make(map[string]bool)
	unconfirmed := make([]string, 0)
	for _, row := range rows {
		status := statuses[canonicalThreadID(row.Thread)]
		switch status {
		case threadStatusArchived:
			archived[row.Workspace+"\x00"+row.Window+"\x00"+row.Thread] = true
		case threadStatusActive:
			// Active rows are confirmed non-archived and are kept.
		default:
			unconfirmed = append(unconfirmed, row.Window+" ("+row.Thread+": "+string(status)+")")
		}
	}
	if len(unconfirmed) > 0 {
		return fmt.Errorf("cannot confirm archive state for %s; refusing to prune", formatCandidates(unconfirmed))
	}
	if len(archived) == 0 {
		fmt.Fprintf(a.stdout, "No archived-thread rows found for workspace %s\n", workspace)
		return nil
	}
	if opts.dryRun {
		for _, row := range rows {
			if archived[row.Workspace+"\x00"+row.Window+"\x00"+row.Thread] {
				fmt.Fprintf(a.stdout, "Would unpin archived thread row %s/%s (%s)\n", row.Workspace, row.Window, canonicalThreadID(row.Thread))
			}
		}
		return nil
	}
	removed, err := config.RemoveRows(opts.configPath, func(row config.Row) bool {
		return archived[row.Workspace+"\x00"+row.Window+"\x00"+row.Thread]
	})
	if err != nil {
		return err
	}
	for _, row := range rows {
		if archived[row.Workspace+"\x00"+row.Window+"\x00"+row.Thread] {
			fmt.Fprintf(a.stdout, "Unpinned archived thread row %s/%s (%s)\n", row.Workspace, row.Window, canonicalThreadID(row.Thread))
		}
	}
	fmt.Fprintf(a.stdout, "Pruned %d archived-thread row(s) from %s\n", removed, opts.configPath)
	return nil
}

func (a app) spawn(opts options, args []string) error {
	spawnOpts, args, err := parseSpawnOptions(args)
	if err != nil {
		return err
	}
	if spawnOpts.messageFile != "" || spawnOpts.messageStdin {
		if len(args) < 2 {
			return errors.New(spawnUsage())
		}
	} else if len(args) < 3 || len(args) > 5 {
		return errors.New(spawnUsage())
	}
	window := args[0]
	workdir := args[1]
	initialMessage, workspaceArgs, err := a.spawnInitialMessage(spawnOpts, args[2:])
	if err != nil {
		return err
	}
	workspace, session := workspaceSessionFromArgs(workspaceArgs)

	if err := config.ValidateField("workspace", workspace); err != nil {
		return err
	}
	if err := config.ValidateField("window", window); err != nil {
		return err
	}
	if err := config.ValidateField("workdir", workdir); err != nil {
		return err
	}
	if spawnOpts.messageFile == "" && !spawnOpts.messageStdin {
		if err := config.ValidateField("initial-message", initialMessage); err != nil {
			return err
		}
	} else if err := validateSpawnMessage("initial-message", initialMessage); err != nil {
		return err
	}
	if spawnOpts.mode != "" {
		if err := config.ValidateField("mode", spawnOpts.mode); err != nil {
			return err
		}
	}
	if spawnOpts.titlePrefix != "" {
		if err := config.ValidateField("title-prefix", spawnOpts.titlePrefix); err != nil {
			return err
		}
		if strings.TrimSpace(spawnOpts.titlePrefix) == "" {
			return errors.New("title-prefix must not be blank")
		}
		if err := validateIssueWindowTitle(spawnOpts.titlePrefix, window); err != nil {
			return err
		}
		window = spawnOpts.prefixedName(window)
	}
	row := config.Row{Workspace: workspace, Window: window, Workdir: workdir}
	expandedWorkdir := config.ExpandHome(workdir)
	if stat, err := os.Stat(expandedWorkdir); err != nil || !stat.IsDir() {
		return fmt.Errorf("missing workdir: %s", expandedWorkdir)
	}
	runner := tmux.Runner{}
	sessionExists := runner.HasSession(session)
	if sessionExists {
		windowNames, err := runner.WindowNames(session)
		if err != nil {
			return fmt.Errorf("list tmux windows for session %q: %w", session, err)
		}
		if tmux.WindowExists(windowNames, window) {
			return fmt.Errorf("window %q already exists in tmux session %q", window, session)
		}
	}
	if opts.dryRun {
		if spawnOpts.mode == "" {
			fmt.Fprintf(a.stdout, "Would create Amp thread for %s/%s\n", workspace, window)
		} else {
			fmt.Fprintf(a.stdout, "Would create Amp thread for %s/%s with mode %q\n", workspace, window, spawnOpts.mode)
		}
		if sessionExists {
			fmt.Fprintf(a.stdout, "Would create tmux window %q in session %q\n", window, session)
		} else {
			fmt.Fprintf(a.stdout, "Would create tmux session %q with window %q\n", session, window)
		}
		fmt.Fprintf(a.stdout, "Would start Amp in %s and submit initial message\n", expandedWorkdir)
		if spawnOpts.titlePrefix != "" {
			fmt.Fprintf(a.stdout, "Would rename new Amp thread to %q\n", window)
		}
		fmt.Fprintf(a.stdout, "Would store %s/%s in %s\n", workspace, window, opts.configPath)
		return nil
	}

	ampArgs := []string{"threads", "new"}
	if spawnOpts.mode != "" {
		ampArgs = append(ampArgs, "--mode", spawnOpts.mode)
	}
	cmd := exec.Command("amp", ampArgs...)
	cmd.Dir = expandedWorkdir
	threadBytes, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("create Amp thread: %w", err)
	}
	thread := strings.TrimRight(string(threadBytes), "\r\n")
	row.Thread = thread
	if err := row.Validate(); err != nil {
		return err
	}

	command := tmux.ContinueCommandWithEnv(expandedWorkdir, thread, map[string]string{
		"AMUX_WORKSPACE": workspace,
		"AMUX_SESSION":   session,
		"AMUX_WINDOW":    window,
		"AMUX_THREAD_ID": thread,
		"AMUX_WORKDIR":   expandedWorkdir,
	})
	var windowID string
	if sessionExists {
		windowID, err = runner.NewWindowID(session, window, command)
		if err != nil {
			return fmt.Errorf("create tmux window %q: %w", window, err)
		}
	} else {
		var err error
		windowID, err = runner.NewSessionWindowID(session, window, command)
		if err != nil {
			return fmt.Errorf("create tmux session %q: %w", session, err)
		}
	}

	target := submissionTarget(runner, windowID)
	submission, err := submitInitialMessage(runner, target, initialMessage)
	if err != nil {
		return err
	}
	if submission.status == config.OperationSubmissionCaptureUnknown {
		fmt.Fprintf(a.stderr, "warning: initial message may not have been submitted; check tmux window %s/%s or send Enter manually\n", session, window)
	}
	delivery, err := verifyInitialMessageDelivery(runner, target, thread, initialMessage)
	if err != nil {
		return err
	}
	if delivery.status != initialMessageDelivered {
		cleanup := cleanupUnverifiedSpawn(runner, windowID, thread)
		message := fmt.Sprintf("initial message was not verified in stored thread %s: %s; removed unverified tmux window and archived unstored thread; refusing to store restore row for %s/%s", thread, delivery.description, workspace, window)
		if cleanup != "" {
			message += "; cleanup warning: " + cleanup
		}
		return errors.New(message)
	}
	if err := runner.SelectWindow(windowID); err != nil {
		return fmt.Errorf("select spawned window: %w", err)
	}

	if err := a.storeRow(opts, row); err != nil {
		return err
	}
	if spawnOpts.titlePrefix != "" {
		if err := renameAmpThreadWithEmptyThreadRetry(thread, window); err != nil {
			fmt.Fprintf(a.stderr, "warning: rename Amp thread %s failed: %v; spawned worker was created and stored as %s/%s; retry with `amp threads rename %s %q`\n", thread, err, workspace, window, thread, window)
		}
	}
	fmt.Fprintln(a.stdout, thread)
	return nil
}

func submissionTarget(runner tmux.Runner, windowID string) string {
	paneID, err := runner.PaneID(windowID)
	if err != nil || paneID == "" {
		return windowID
	}
	return paneID
}

func cleanupUnverifiedSpawn(runner tmux.Runner, windowID, thread string) string {
	var failures []string
	if err := archiveAmpThread(canonicalThreadID(thread)); err != nil {
		failures = append(failures, "archive Amp thread "+thread+": "+err.Error())
	}
	if err := runner.KillWindow(windowID); err != nil {
		failures = append(failures, "kill tmux window "+windowID+": "+err.Error())
	}
	return strings.Join(failures, "; ")
}

func (a app) spawnInitialMessage(opts spawnOptions, args []string) (string, []string, error) {
	if opts.messageFile != "" && opts.messageStdin {
		return "", nil, errors.New("--message-file and --message-stdin are mutually exclusive")
	}
	if opts.messageFile == "" && !opts.messageStdin {
		return args[0], args[1:], nil
	}
	if len(args) > 2 {
		return "", nil, errors.New("--message-file and --message-stdin are mutually exclusive with positional <initial-message>")
	}
	var reader io.Reader
	if opts.messageFile != "" {
		file, err := os.Open(opts.messageFile)
		if err != nil {
			return "", nil, fmt.Errorf("read --message-file: %w", err)
		}
		defer file.Close()
		reader = file
	} else {
		reader = a.stdin
	}
	message, err := readSpawnMessage(reader)
	if err != nil {
		return "", nil, err
	}
	return message, args, nil
}

func readSpawnMessage(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxSpawnMessageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxSpawnMessageBytes {
		return "", fmt.Errorf("initial-message exceeds %d bytes", maxSpawnMessageBytes)
	}
	return string(data), nil
}

func validateSpawnMessage(name, value string) error {
	if value == "" {
		return fmt.Errorf("missing %s", name)
	}
	return nil
}

type initialMessageSubmission struct {
	status config.OperationSubmissionStatus
}

func submitInitialMessage(runner tmux.Runner, target, message string) (initialMessageSubmission, error) {
	return submitInitialMessageGuarded(runner, target, message, initialMessageSubmissionGuard{})
}

type initialMessageSubmissionGuard struct {
	beforeInput func() error
	beforeEnter func() error
}

func submitInitialMessageGuarded(runner tmux.Runner, target, message string, guard initialMessageSubmissionGuard) (initialMessageSubmission, error) {
	multiline := strings.ContainsAny(message, "\r\n")
	ready, captureAvailable, captureEverAvailable := waitForComposerReady(runner, target)
	if multiline && !ready {
		if !captureAvailable {
			return initialMessageSubmission{status: config.OperationSubmissionComposerCaptureUnknown}, nil
		}
		return initialMessageSubmission{status: config.OperationSubmissionComposerUnavailable}, nil
	}
	if guard.beforeInput != nil {
		if err := guard.beforeInput(); err != nil {
			return initialMessageSubmission{}, err
		}
	}
	if !multiline && !captureEverAvailable {
		if err := sendInitialMessageText(runner, target, message); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("send initial message: %w", err)
		}
		if err := runner.SendEnter(target); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("submit initial message: %w", err)
		}
		return initialMessageSubmission{status: config.OperationSubmissionCaptureUnknown}, nil
	}
	if err := sendInitialMessageText(runner, target, message); err != nil {
		return initialMessageSubmission{}, fmt.Errorf("send initial message: %w", err)
	}
	if multiline {
		visible, available := waitForComposerMessageState(runner, target, message)
		if !available {
			return initialMessageSubmission{status: config.OperationSubmissionInputVisibilityUnknown}, nil
		}
		if !visible {
			if guard.beforeInput != nil {
				if err := guard.beforeInput(); err != nil {
					return initialMessageSubmission{}, err
				}
			}
			if err := runner.ClearLine(target); err != nil {
				return initialMessageSubmission{}, fmt.Errorf("clear initial message: %w", err)
			}
			if guard.beforeInput != nil {
				if err := guard.beforeInput(); err != nil {
					return initialMessageSubmission{}, err
				}
			}
			if err := sendInitialMessageText(runner, target, message); err != nil {
				return initialMessageSubmission{}, fmt.Errorf("send initial message: %w", err)
			}
			visible, available = waitForComposerMessageState(runner, target, message)
			if !available {
				return initialMessageSubmission{status: config.OperationSubmissionInputVisibilityUnknown}, nil
			}
			if !visible {
				if guard.beforeInput != nil {
					if err := guard.beforeInput(); err != nil {
						return initialMessageSubmission{}, err
					}
				}
				if err := runner.ClearLine(target); err != nil {
					return initialMessageSubmission{}, fmt.Errorf("clear unverified initial message: %w", err)
				}
				return initialMessageSubmission{status: config.OperationSubmissionInputNotVisible}, nil
			}
		}
		time.Sleep(spawnInputSettleDelay())
		visible, available = composerContainsMessage(runner, target, message)
		if !available {
			return initialMessageSubmission{status: config.OperationSubmissionInputVisibilityUnknown}, nil
		}
		if !visible {
			return initialMessageSubmission{status: config.OperationSubmissionInputNotVisible}, nil
		}
		if guard.beforeEnter != nil {
			if err := guard.beforeEnter(); err != nil {
				return initialMessageSubmission{}, err
			}
		}
		if err := runner.SendEnter(target); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("submit initial message: %w", err)
		}
		contains, available, visible := paneMessageState(runner, target, message)
		switch {
		case !available:
			return initialMessageSubmission{status: config.OperationSubmissionCaptureUnknown}, nil
		case contains:
			return initialMessageSubmission{status: config.OperationSubmissionTypedOnly}, nil
		case visible:
			return initialMessageSubmission{status: config.OperationSubmissionTransitioned}, nil
		default:
			return initialMessageSubmission{status: config.OperationSubmissionEnterAttempted}, nil
		}
	}
	if !waitForComposerMessage(runner, target, message) {
		if err := runner.ClearLine(target); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("clear initial message: %w", err)
		}
		if err := sendInitialMessageText(runner, target, message); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("send initial message: %w", err)
		}
		if !waitForComposerMessage(runner, target, message) {
			time.Sleep(spawnInputSettleDelay())
			if err := runner.SendEnter(target); err != nil {
				return initialMessageSubmission{}, fmt.Errorf("submit initial message: %w", err)
			}
			return initialMessageSubmission{status: config.OperationSubmissionEnterAttempted}, nil
		}
	}
	time.Sleep(spawnInputSettleDelay())
	retypedAfterLostPrompt := false
	for attempt := 0; attempt < 3; attempt++ {
		if err := runner.SendEnter(target); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("submit initial message: %w", err)
		}
		time.Sleep(spawnPollInterval)
		contains, available, visible := paneMessageState(runner, target, message)
		if !available {
			return initialMessageSubmission{status: config.OperationSubmissionCaptureUnknown}, nil
		}
		if contains {
			continue
		}
		if visible {
			return initialMessageSubmission{status: config.OperationSubmissionTransitioned}, nil
		}
		if retypedAfterLostPrompt {
			return initialMessageSubmission{status: config.OperationSubmissionEnterAttempted}, nil
		}
		if err := sendInitialMessageText(runner, target, message); err != nil {
			return initialMessageSubmission{}, fmt.Errorf("send initial message: %w", err)
		}
		if !waitForComposerMessage(runner, target, message) {
			return initialMessageSubmission{status: config.OperationSubmissionEnterAttempted}, nil
		}
		time.Sleep(spawnInputSettleDelay())
		retypedAfterLostPrompt = true
	}
	return initialMessageSubmission{status: config.OperationSubmissionTypedOnly}, nil
}

func sendInitialMessageText(runner tmux.Runner, target, message string) error {
	if strings.ContainsAny(message, "\r\n") {
		return runner.PasteLiteral(target, message)
	}
	return runner.SendLiteral(target, message)
}

func waitForComposerReady(runner tmux.Runner, target string) (bool, bool, bool) {
	deadline := time.Now().Add(spawnSubmitTimeout())
	everAvailable := false
	for {
		ready, available := composerReady(runner, target)
		everAvailable = everAvailable || available
		if ready {
			return true, true, true
		}
		if !sleepUntilNextSpawnPoll(deadline) {
			return false, available, everAvailable
		}
	}
}

func waitForComposerMessage(runner tmux.Runner, target, message string) bool {
	visible, _ := waitForComposerMessageState(runner, target, message)
	return visible
}

func waitForComposerMessageState(runner tmux.Runner, target, message string) (bool, bool) {
	deadline := time.Now().Add(spawnSubmitTimeout())
	for {
		contains, available := composerContainsMessage(runner, target, message)
		if contains {
			return true, true
		}
		if !sleepUntilNextSpawnPoll(deadline) {
			return false, available
		}
	}
}

func composerReady(runner tmux.Runner, target string) (bool, bool) {
	contents, err := runner.CapturePane(target)
	if err != nil {
		return false, false
	}
	return hasComposerFrame(contents), true
}

func composerContainsMessage(runner tmux.Runner, target, message string) (bool, bool) {
	contents, err := runner.CapturePane(target)
	if err != nil {
		return false, false
	}
	contains, available := composerTextMatchesMessage(contents, message)
	return contains, available
}

func paneMessageState(runner tmux.Runner, target, message string) (bool, bool, bool) {
	contents, err := runner.CapturePane(target)
	if err != nil {
		return false, false, false
	}
	contains, available := composerTextMatchesMessage(contents, message)
	return contains, available, containsCollapsedWhitespace(contents, message)
}

func composerTextMatchesMessage(contents, message string) (bool, bool) {
	if strings.ContainsAny(message, "\r\n") {
		return textComposerEqualsMessage(contents, message)
	}
	return textContainsComposerMessage(contents, message)
}

func textComposerEqualsMessage(contents, message string) (bool, bool) {
	rows, available := bottomComposerFrameRows(contents)
	if !available {
		return false, false
	}
	var composerLines []string
	for _, row := range rows {
		body := strings.TrimSuffix(strings.TrimPrefix(row, "│"), "│")
		if strings.HasPrefix(body, " ") {
			body = body[1:]
		}
		body = strings.TrimRight(body, " ")
		composerLines = append(composerLines, body)
	}
	return exactInitialMessageContent(strings.Join(composerLines, "\n"), message), true
}

func bottomComposerFrameRows(contents string) ([]string, bool) {
	lines := strings.Split(strings.TrimRight(contents, "\r\n"), "\n")
	if len(lines) < 2 {
		return nil, false
	}
	end := len(lines) - 1
	bottom := strings.TrimSpace(lines[end])
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		return nil, false
	}
	for start := end - 1; start >= 0; start-- {
		top := strings.TrimSpace(lines[start])
		if !strings.HasPrefix(top, "╭") || !strings.HasSuffix(top, "╮") {
			continue
		}
		rows := make([]string, 0, end-start-1)
		for _, line := range lines[start+1 : end] {
			row := strings.TrimSpace(line)
			if !strings.HasPrefix(row, "│") || !strings.HasSuffix(row, "│") {
				return nil, false
			}
			rows = append(rows, row)
		}
		return rows, true
	}
	return nil, false
}

func textContainsComposerMessage(contents, message string) (bool, bool) {
	lines := strings.Split(contents, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "╭") {
			return containsCollapsedWhitespace(strings.Join(lines[i:], "\n"), message), true
		}
	}
	return false, false
}

func containsCollapsedWhitespace(contents, message string) bool {
	needle := collapsePaneText(message)
	return needle != "" && strings.Contains(collapsePaneText(contents), needle)
}

func collapsePaneText(text string) string {
	text = strings.Map(func(r rune) rune {
		switch r {
		case '│', '┃', '╭', '╮', '╰', '╯', '─':
			return ' '
		default:
			return r
		}
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

func hasComposerFrame(contents string) bool {
	_, available := bottomComposerFrameRows(contents)
	return available
}

type initialMessageDeliveryStatus string

const (
	initialMessageDelivered       initialMessageDeliveryStatus = "delivered"
	initialMessageTypedOnly       initialMessageDeliveryStatus = "typed-only"
	initialMessageDifferentThread initialMessageDeliveryStatus = "different-thread"
	initialMessageLostOrEmpty     initialMessageDeliveryStatus = "lost-or-empty"
	initialMessageDeliveryUnknown initialMessageDeliveryStatus = "unknown"
)

type initialMessageDelivery struct {
	status      initialMessageDeliveryStatus
	description string
}

func verifyInitialMessageDelivery(runner tmux.Runner, target, thread, message string) (initialMessageDelivery, error) {
	deadline := time.Now().Add(spawnSubmitTimeout())
	var lastErr error
	for {
		contains, _, err := ampThreadContainsMessage(thread, message)
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
		}
		if contains {
			return initialMessageDelivery{status: initialMessageDelivered, description: "stored thread contains initial message"}, nil
		}
		if !sleepUntilNextSpawnPoll(deadline) {
			break
		}
	}
	if lastErr != nil {
		return initialMessageDelivery{status: initialMessageDeliveryUnknown, description: lastErr.Error()}, nil
	}

	contains, available := composerContainsMessage(runner, target, message)
	if available && contains {
		return initialMessageDelivery{status: initialMessageTypedOnly, description: "initial message is still visible in the tmux composer and was not submitted to the stored thread"}, nil
	}

	otherThread, err := findDifferentThreadWithMessage(thread, message)
	if err != nil {
		return initialMessageDelivery{status: initialMessageLostOrEmpty, description: "stored thread is empty or missing the initial message; could not search for another receiving thread: " + err.Error()}, nil
	}
	if otherThread != "" {
		return initialMessageDelivery{status: initialMessageDifferentThread, description: "initial message appears in thread " + otherThread + " instead"}, nil
	}
	return initialMessageDelivery{status: initialMessageLostOrEmpty, description: "stored thread is empty or missing the initial message, tmux composer is empty, and no different receiving thread was found"}, nil
}

func ampThreadContainsMessage(thread, message string) (bool, bool, error) {
	cmd := exec.Command("amp", "threads", "export", thread)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			return false, false, err
		}
		return false, false, fmt.Errorf("%w: %s", err, text)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return false, false, fmt.Errorf("parse amp threads export for %s: %w", thread, err)
	}
	return jsonValueContainsMessage(payload["messages"], message), true, nil
}

func findDifferentThreadWithMessage(storedThread, message string) (string, error) {
	query := `"` + strings.ReplaceAll(message, `"`, `\"`) + `"`
	cmd := exec.Command("amp", "threads", "search", "--json", "--limit", "5", query)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		text := strings.TrimSpace(stderr.String())
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, text)
	}
	var payload any
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", fmt.Errorf("parse amp threads search: %w", err)
	}
	storedID := canonicalThreadID(storedThread)
	for _, id := range collectThreadIDs(payload) {
		if id == "" || canonicalThreadID(id) == storedID {
			continue
		}
		contains, _, err := ampThreadContainsMessage(id, message)
		if err != nil {
			continue
		}
		if contains {
			return id, nil
		}
	}
	return "", nil
}

func jsonValueContainsMessage(value any, message string) bool {
	switch v := value.(type) {
	case string:
		return exactInitialMessageContent(v, message)
	case []any:
		for _, item := range v {
			if jsonValueContainsMessage(item, message) {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if jsonValueContainsMessage(item, message) {
				return true
			}
		}
	}
	return false
}

func exactInitialMessageContent(got, want string) bool {
	got = strings.ReplaceAll(got, "\r\n", "\n")
	want = strings.ReplaceAll(want, "\r\n", "\n")
	if got == want {
		return true
	}
	if strings.HasSuffix(want, "\n") && !strings.HasSuffix(strings.TrimSuffix(want, "\n"), "\n") {
		return got == strings.TrimSuffix(want, "\n")
	}
	return false
}

func collectThreadIDs(value any) []string {
	var ids []string
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			if id, ok := v["id"].(string); ok {
				ids = append(ids, id)
			}
			if id, ok := v["threadID"].(string); ok {
				ids = append(ids, id)
			}
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(value)
	return ids
}

func spawnSubmitTimeout() time.Duration {
	timeout := 10 * spawnDelay()
	if timeout <= 0 {
		return spawnPollInterval
	}
	return timeout
}

func spawnInputSettleDelay() time.Duration {
	delay := spawnDelay()
	if delay <= 0 {
		return spawnPollInterval
	}
	return delay
}

func sleepUntilNextSpawnPoll(deadline time.Time) bool {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return false
	}
	if remaining > spawnPollInterval {
		remaining = spawnPollInterval
	}
	time.Sleep(remaining)
	return true
}

type teardownIdentity struct {
	Workspace string
	Session   string
	Window    string
	Thread    string
	FromEnv   bool
}

type teardownArgs struct {
	positional []string
	thread     string
	session    string
}

type teardownThreadTarget struct {
	identity teardownIdentity
	row      config.Row
	pane     *tmux.WindowPane
}

func (a app) teardown(opts options, args []string) error {
	if opts.dryRun {
		return errors.New("teardown does not support --dry-run")
	}
	teardownArgs, err := parseTeardownArgs(args)
	if err != nil {
		return err
	}

	var identity teardownIdentity
	var row config.Row
	var verifiedPane *tmux.WindowPane
	if teardownArgs.thread != "" {
		target, err := teardownTargetFromThread(opts.configPath, teardownArgs.thread, teardownArgs.session)
		if err != nil {
			return err
		}
		identity = target.identity
		row = target.row
		verifiedPane = target.pane
	} else if len(teardownArgs.positional) == 0 {
		identity, err = teardownIdentityFromEnv()
		if err != nil {
			return err
		}
	} else {
		identity, err = teardownIdentityFromArgs(teardownArgs.positional)
		if err != nil {
			return err
		}
	}
	if row.Workspace == "" {
		row, err = verifiedTeardownRow(opts.configPath, identity)
		if err != nil {
			return err
		}
	}
	if identity.Thread == "" {
		// Explicit teardown uses the verified restore row as the thread authority.
		identity.Thread = row.Thread
	}
	runner := tmux.Runner{}
	var pane tmux.WindowPane
	if verifiedPane != nil {
		pane = *verifiedPane
	} else {
		pane, err = verifiedTeardownPane(runner, identity, row)
		if err != nil {
			return err
		}
	}

	archiveThread := canonicalThreadID(identity.Thread)
	if err := archiveAmpThread(archiveThread); err != nil {
		return fmt.Errorf("archive Amp thread %s: %w", archiveThread, err)
	}
	if err := a.removeRow(opts, identity.Workspace, identity.Window); err != nil {
		return err
	}
	if err := runner.KillWindow(pane.WindowID); err != nil {
		return fmt.Errorf("stop tmux window %s (%s): %w", identity.Window, pane.WindowID, err)
	}
	fmt.Fprintf(a.stdout, "Archived Amp thread %s\n", archiveThread)
	fmt.Fprintf(a.stdout, "Stopped tmux window %s/%s (%s)\n", identity.Session, identity.Window, pane.WindowID)
	fmt.Fprintf(a.stdout, "Teardown complete for %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
	return nil
}

func parseTeardownArgs(args []string) (teardownArgs, error) {
	parsed := teardownArgs{positional: make([]string, 0, len(args))}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--thread":
			i++
			if i >= len(args) || args[i] == "" {
				return parsed, errors.New("--thread requires a thread id or URL")
			}
			parsed.thread = args[i]
		case "--session":
			i++
			if i >= len(args) || args[i] == "" {
				return parsed, errors.New("--session requires a tmux session name")
			}
			parsed.session = args[i]
		default:
			if strings.HasPrefix(args[i], "--thread=") {
				parsed.thread = strings.TrimPrefix(args[i], "--thread=")
				if parsed.thread == "" {
					return parsed, errors.New("--thread requires a thread id or URL")
				}
			} else if strings.HasPrefix(args[i], "--session=") {
				parsed.session = strings.TrimPrefix(args[i], "--session=")
				if parsed.session == "" {
					return parsed, errors.New("--session requires a tmux session name")
				}
			} else if strings.HasPrefix(args[i], "--") {
				return parsed, fmt.Errorf("unknown teardown option %s", args[i])
			} else {
				parsed.positional = append(parsed.positional, args[i])
			}
		}
	}
	if parsed.thread != "" {
		if len(parsed.positional) != 0 {
			return parsed, errors.New("usage: amux teardown --thread <thread-id-or-url> [--session <session>]")
		}
		if err := config.ValidateField("thread", parsed.thread); err != nil {
			return parsed, err
		}
		if parsed.session != "" {
			if err := config.ValidateField("session", parsed.session); err != nil {
				return parsed, err
			}
		}
		return parsed, nil
	}
	if parsed.session != "" {
		return parsed, errors.New("--session requires --thread")
	}
	if len(parsed.positional) != 0 && len(parsed.positional) != 2 && len(parsed.positional) != 3 {
		return parsed, errors.New("usage: amux teardown [<workspace> <window> [session]] OR amux teardown --thread <thread-id-or-url> [--session <session>]")
	}
	return parsed, nil
}

func teardownIdentityFromEnv() (teardownIdentity, error) {
	identity := teardownIdentity{
		Workspace: os.Getenv("AMUX_WORKSPACE"),
		Session:   os.Getenv("AMUX_SESSION"),
		Window:    os.Getenv("AMUX_WINDOW"),
		Thread:    os.Getenv("AMUX_THREAD_ID"),
		FromEnv:   true,
	}
	missing := make([]string, 0, 4)
	if identity.Workspace == "" {
		missing = append(missing, "AMUX_WORKSPACE")
	}
	if identity.Session == "" {
		missing = append(missing, "AMUX_SESSION")
	}
	if identity.Window == "" {
		missing = append(missing, "AMUX_WINDOW")
	}
	if identity.Thread == "" {
		missing = append(missing, "AMUX_THREAD_ID")
	}
	if len(missing) > 0 {
		return identity, fmt.Errorf("teardown requires spawn-injected identity; missing %s. If this worker was restored without AMUX_* but its thread is stored and live, use amux teardown --thread <thread-id-or-url> [--session <session>]", strings.Join(missing, ", "))
	}
	if err := config.ValidateField("AMUX_WORKSPACE", identity.Workspace); err != nil {
		return identity, err
	}
	if err := config.ValidateField("AMUX_SESSION", identity.Session); err != nil {
		return identity, err
	}
	if err := config.ValidateField("AMUX_WINDOW", identity.Window); err != nil {
		return identity, err
	}
	if err := config.ValidateField("AMUX_THREAD_ID", identity.Thread); err != nil {
		return identity, err
	}
	return identity, nil
}

func teardownIdentityFromArgs(args []string) (teardownIdentity, error) {
	identity := teardownIdentity{
		Workspace: args[0],
		Window:    args[1],
		Session:   args[0],
	}
	if len(args) == 3 {
		identity.Session = args[2]
	}
	if err := config.ValidateField("workspace", identity.Workspace); err != nil {
		return identity, err
	}
	if err := config.ValidateField("window", identity.Window); err != nil {
		return identity, err
	}
	if err := config.ValidateField("session", identity.Session); err != nil {
		return identity, err
	}
	return identity, nil
}

func teardownTargetFromThread(path, thread, session string) (teardownThreadTarget, error) {
	row, err := verifiedTeardownRowByThread(path, thread)
	if err != nil {
		return teardownThreadTarget{}, err
	}
	identity := teardownIdentity{
		Workspace: row.Workspace,
		Window:    row.Window,
		Thread:    row.Thread,
		Session:   session,
	}
	if identity.Session != "" {
		return teardownThreadTarget{identity: identity, row: row}, nil
	}
	panes, err := livePanesForTeardown(tmux.Runner{}, identity)
	if err != nil {
		return teardownThreadTarget{}, err
	}
	verified := make([]tmux.WindowPane, 0, 1)
	for _, pane := range panes {
		candidate := identity
		candidate.Session = pane.Session
		startCommand := normalizedTmuxStartCommand(pane.StartCommand)
		if explicitTeardownStartCommandMatches(candidate, row, startCommand) {
			verified = appendUniqueTeardownWindow(verified, pane)
		}
	}
	if len(verified) == 0 {
		return teardownThreadTarget{}, fmt.Errorf("no live tmux window for thread %s matches restore row %s/%s; pass --session if it is in a specific tmux session", thread, row.Workspace, row.Window)
	}
	if len(verified) > 1 {
		return teardownThreadTarget{}, fmt.Errorf("ambiguous live tmux windows for thread %s: candidates %s; pass --session to choose one", thread, formatSessionPaneCandidates(verified))
	}
	identity.Session = verified[0].Session
	return teardownThreadTarget{identity: identity, row: row, pane: &verified[0]}, nil
}

func verifiedTeardownRowByThread(path, thread string) (config.Row, error) {
	rows, err := config.Load(path)
	if err != nil {
		return config.Row{}, err
	}
	threadID := canonicalThreadID(thread)
	matches := make([]config.Row, 0, 1)
	for _, row := range rows {
		if canonicalThreadID(row.Thread) == threadID {
			matches = append(matches, row)
		}
	}
	if len(matches) == 0 {
		return config.Row{}, fmt.Errorf("no restore row for thread %s", thread)
	}
	if len(matches) > 1 {
		return config.Row{}, fmt.Errorf("ambiguous restore rows for thread %s: candidates %s; refusing teardown", thread, formatRowCandidates(matches))
	}
	return matches[0], nil
}

func canonicalThreadID(thread string) string {
	thread = strings.TrimSpace(thread)
	thread = strings.TrimRight(thread, "/")
	if i := strings.LastIndex(thread, "/"); i >= 0 {
		return thread[i+1:]
	}
	return thread
}

func verifiedTeardownRow(path string, identity teardownIdentity) (config.Row, error) {
	rows, err := config.Load(path)
	if err != nil {
		return config.Row{}, err
	}
	matches := make([]config.Row, 0, 1)
	candidates := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Workspace == identity.Workspace {
			candidates = append(candidates, row.Window+" ("+row.Thread+")")
		}
		if row.Workspace == identity.Workspace && row.Window == identity.Window {
			matches = append(matches, row)
		}
	}
	if len(matches) == 0 {
		return config.Row{}, fmt.Errorf("no restore row for %s/%s; candidates in workspace %q: %s", identity.Workspace, identity.Window, identity.Workspace, formatCandidates(candidates))
	}
	if len(matches) > 1 {
		return config.Row{}, fmt.Errorf("ambiguous restore rows for %s/%s; refusing teardown", identity.Workspace, identity.Window)
	}
	row := matches[0]
	if identity.Thread != "" && row.Thread != identity.Thread {
		return config.Row{}, fmt.Errorf("restore row thread mismatch for %s/%s: AMUX_THREAD_ID=%s config=%s", identity.Workspace, identity.Window, identity.Thread, row.Thread)
	}
	return row, nil
}

func verifiedTeardownPane(runner tmux.Runner, identity teardownIdentity, row config.Row) (tmux.WindowPane, error) {
	panes, err := livePanesForTeardown(runner, identity)
	if err != nil {
		return tmux.WindowPane{}, fmt.Errorf("find tmux window %s/%s: %w", identity.Session, identity.Window, err)
	}
	if len(panes) == 0 {
		return tmux.WindowPane{}, fmt.Errorf("no live tmux window %q in session %q", identity.Window, identity.Session)
	}
	windows := uniqueTeardownWindows(panes)
	if len(windows) > 1 {
		return tmux.WindowPane{}, fmt.Errorf("ambiguous tmux window %q in session %q: candidates %s; refusing teardown", identity.Window, identity.Session, formatPaneCandidates(panes))
	}
	pane := windows[0]
	if pane.WindowID == "" {
		return tmux.WindowPane{}, fmt.Errorf("tmux window %q in session %q has no window id", identity.Window, identity.Session)
	}
	if identity.FromEnv && !anyTeardownPaneStartCommandMatches(panes, func(startCommand string) bool {
		return startCommand == teardownExpectedStartCommand(identity, row)
	}) {
		return tmux.WindowPane{}, fmt.Errorf("tmux window %q in session %q is not the expected amux-spawned command for AMUX_THREAD_ID=%s; candidates %s", identity.Window, identity.Session, identity.Thread, formatPaneCandidates(panes))
	}
	if !identity.FromEnv && !anyTeardownPaneStartCommandMatches(panes, func(startCommand string) bool {
		return explicitTeardownStartCommandMatches(identity, row, startCommand)
	}) {
		return tmux.WindowPane{}, fmt.Errorf("tmux window %q in session %q start command does not match restore row thread %s; candidates %s", identity.Window, identity.Session, row.Thread, formatPaneCandidates(panes))
	}
	return pane, nil
}

func uniqueTeardownWindows(panes []tmux.WindowPane) []tmux.WindowPane {
	windows := make([]tmux.WindowPane, 0, len(panes))
	for _, pane := range panes {
		windows = appendUniqueTeardownWindow(windows, pane)
	}
	return windows
}

func appendUniqueTeardownWindow(windows []tmux.WindowPane, pane tmux.WindowPane) []tmux.WindowPane {
	for _, window := range windows {
		if window.Session == pane.Session && window.WindowID == pane.WindowID {
			return windows
		}
	}
	return append(windows, pane)
}

func anyTeardownPaneStartCommandMatches(panes []tmux.WindowPane, matches func(string) bool) bool {
	for _, pane := range panes {
		if matches(normalizedTmuxStartCommand(pane.StartCommand)) {
			return true
		}
	}
	return false
}

func livePanesForTeardown(runner tmux.Runner, identity teardownIdentity) ([]tmux.WindowPane, error) {
	if identity.Session != "" {
		return runner.WindowPanes(identity.Session, identity.Window)
	}
	panes, err := runner.AllWindowPanes()
	if err != nil {
		return nil, err
	}
	matches := make([]tmux.WindowPane, 0, 1)
	for _, pane := range panes {
		if pane.Window == identity.Window {
			matches = append(matches, pane)
		}
	}
	return matches, nil
}

func normalizedTmuxStartCommand(startCommand string) string {
	if strings.HasPrefix(startCommand, "\"") && strings.HasSuffix(startCommand, "\"") {
		unquoted, err := strconv.Unquote(startCommand)
		if err == nil {
			return unquoted
		}
	}
	return startCommand
}

func explicitTeardownStartCommandMatches(identity teardownIdentity, row config.Row, startCommand string) bool {
	expandedWorkdir := config.ExpandHome(row.Workdir)
	if startCommand == tmux.ContinueCommand(expandedWorkdir, row.Thread) {
		return true
	}
	return startCommand == teardownExpectedStartCommand(identity, row)
}

func teardownExpectedStartCommand(identity teardownIdentity, row config.Row) string {
	expandedWorkdir := config.ExpandHome(row.Workdir)
	return tmux.ContinueCommandWithEnv(expandedWorkdir, identity.Thread, map[string]string{
		"AMUX_WORKSPACE": identity.Workspace,
		"AMUX_SESSION":   identity.Session,
		"AMUX_WINDOW":    identity.Window,
		"AMUX_THREAD_ID": identity.Thread,
		"AMUX_WORKDIR":   expandedWorkdir,
	})
}

func archiveAmpThread(thread string) error {
	cmd := exec.Command("amp", "threads", "archive", thread)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

func unarchiveAmpThread(thread string) error {
	cmd := exec.Command("amp", "threads", "archive", "--unarchive", thread)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

type threadStatus string

const (
	threadStatusActive   threadStatus = "active"
	threadStatusArchived threadStatus = "archived"
	threadStatusMissing  threadStatus = "missing"
)

func threadArchiveStatuses(rows []config.Row) (map[string]threadStatus, error) {
	targets := make(map[string]bool, len(rows))
	for _, row := range rows {
		if id := canonicalThreadID(row.Thread); id != "" {
			targets[id] = true
		}
	}
	active, err := ampThreadIDSet(false, targets)
	if err != nil {
		return nil, err
	}
	includingArchived, err := ampThreadIDSet(true, targets)
	if err != nil {
		return nil, err
	}
	statuses := make(map[string]threadStatus, len(rows))
	for _, row := range rows {
		id := canonicalThreadID(row.Thread)
		switch {
		case active[id]:
			statuses[id] = threadStatusActive
		case includingArchived[id]:
			statuses[id] = threadStatusArchived
		default:
			statuses[id] = threadStatusMissing
		}
	}
	return statuses, nil
}

func ampThreadIDSet(includeArchived bool, targets map[string]bool) (map[string]bool, error) {
	ids := make(map[string]bool)
	for offset := 0; ; offset += 500 {
		args := []string{"threads", "list", "--json"}
		if includeArchived {
			args = append(args, "--include-archived")
		}
		args = append(args, "--limit", "500", "--offset", strconv.Itoa(offset))
		cmd := exec.Command("amp", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			message := strings.TrimSpace(string(out))
			if message == "" {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		var payload any
		if err := json.Unmarshal(out, &payload); err != nil {
			return nil, fmt.Errorf("parse amp threads list: %w", err)
		}
		pageIDs := collectThreadIDs(payload)
		for _, id := range pageIDs {
			id = canonicalThreadID(id)
			if id != "" {
				ids[id] = true
			}
		}
		if len(targets) > 0 && containsAllThreadIDs(ids, targets) {
			return ids, nil
		}
		if len(pageIDs) < 500 {
			return ids, nil
		}
	}
}

func containsAllThreadIDs(ids, targets map[string]bool) bool {
	for id := range targets {
		if !ids[id] {
			return false
		}
	}
	return true
}

func formatCandidates(candidates []string) string {
	if len(candidates) == 0 {
		return "none"
	}
	return strings.Join(candidates, ", ")
}

func formatRowCandidates(rows []config.Row) string {
	candidates := make([]string, 0, len(rows))
	for _, row := range rows {
		candidates = append(candidates, row.Workspace+"/"+row.Window)
	}
	return formatCandidates(candidates)
}

func formatPaneCandidates(panes []tmux.WindowPane) string {
	candidates := make([]string, 0, len(panes))
	for _, pane := range panes {
		if pane.WindowID == "" {
			candidates = append(candidates, "unknown-window-id")
			continue
		}
		candidates = append(candidates, pane.WindowID)
	}
	return formatCandidates(candidates)
}

func formatSessionPaneCandidates(panes []tmux.WindowPane) string {
	candidates := make([]string, 0, len(panes))
	for _, pane := range panes {
		windowID := pane.WindowID
		if windowID == "" {
			windowID = "unknown-window-id"
		}
		if pane.Session == "" {
			candidates = append(candidates, windowID)
			continue
		}
		candidates = append(candidates, pane.Session+"/"+windowID)
	}
	return formatCandidates(candidates)
}

type spawnOptions struct {
	mode         string
	titlePrefix  string
	messageFile  string
	messageStdin bool
}

func parseSpawnOptions(args []string) (spawnOptions, []string, error) {
	var opts spawnOptions
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mode", "-m":
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, errors.New("--mode requires a mode")
			}
			opts.mode = args[i]
		case "--title-prefix":
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, errors.New("--title-prefix requires a prefix")
			}
			opts.titlePrefix = args[i]
		case "--message-file":
			i++
			if i >= len(args) || args[i] == "" {
				return opts, nil, errors.New("--message-file requires a path")
			}
			opts.messageFile = args[i]
		case "--message-stdin":
			opts.messageStdin = true
		default:
			if strings.HasPrefix(args[i], "--mode=") {
				opts.mode = strings.TrimPrefix(args[i], "--mode=")
				if opts.mode == "" {
					return opts, nil, errors.New("--mode requires a mode")
				}
			} else if strings.HasPrefix(args[i], "--title-prefix=") {
				opts.titlePrefix = strings.TrimPrefix(args[i], "--title-prefix=")
				if opts.titlePrefix == "" {
					return opts, nil, errors.New("--title-prefix requires a prefix")
				}
			} else if strings.HasPrefix(args[i], "--message-file=") {
				opts.messageFile = strings.TrimPrefix(args[i], "--message-file=")
				if opts.messageFile == "" {
					return opts, nil, errors.New("--message-file requires a path")
				}
			} else {
				remaining = append(remaining, args[i])
			}
		}
	}
	return opts, remaining, nil
}

func (opts spawnOptions) prefixedName(window string) string {
	return prefixedSpawnName(opts.titlePrefix, window)
}

func spawnUsage() string {
	return "usage: amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] [--message-file <path> | --message-stdin] <window> <workdir> [<initial-message>] [workspace] [session]"
}

func renameAmpThread(thread, title string) error {
	cmd := exec.Command("amp", "threads", "rename", thread, title)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

func renameAmpThreadWithEmptyThreadRetry(thread, title string) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = renameAmpThread(thread, title)
		if err == nil {
			return nil
		}
		if !isEmptyThreadRenameError(err) {
			return err
		}
		if attempt < 2 {
			time.Sleep(spawnDelay())
		}
	}
	return err
}

func isEmptyThreadRenameError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Cannot rename an empty thread")
}

func spawnDelay() time.Duration {
	value := os.Getenv("AMP_TMUX_SPAWN_DELAY")
	if value == "" {
		return time.Second
	}
	delay, err := time.ParseDuration(value)
	if err == nil {
		return delay
	}
	seconds, err := time.ParseDuration(value + "s")
	if err == nil {
		return seconds
	}
	return time.Second
}

func versionString() string {
	parts := []string{"amux", version}
	if commit != "" {
		parts = append(parts, "commit="+commit)
	}
	if built != "" {
		parts = append(parts, "built="+built)
	}
	return strings.Join(parts, " ")
}

func (a app) doctor(opts options, workspace, session string) error {
	failed := false
	check := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(a.stdout, "FAIL %s: %v\n", name, err)
		} else {
			fmt.Fprintf(a.stdout, "OK   %s\n", name)
		}
	}

	_, err := exec.LookPath("tmux")
	check("tmux on PATH", err)
	_, err = exec.LookPath("amp")
	check("amp on PATH", err)

	check("config path", ensureConfigReadable(opts.configPath))
	rows, err := rowsForWorkspaceReadOnly(opts.configPath, workspace)
	check("read workspace "+workspace, err)
	runnerRows, runnerErr := runnerRowsForWorkspaceReadOnly(config.RunnerPath(opts.configPath), workspace)
	check("read runner workspace "+workspace, runnerErr)
	if err == nil {
		if len(rows) == 0 && len(runnerRows) == 0 {
			check("workspace "+workspace+" rows", fmt.Errorf("no rows found in %s", opts.configPath))
		}
		for _, row := range rows {
			workdir := config.ExpandHome(row.Workdir)
			stat, statErr := os.Stat(workdir)
			if statErr != nil {
				check("workdir "+row.Window, fmt.Errorf("%s: %w", workdir, statErr))
			} else if !stat.IsDir() {
				check("workdir "+row.Window, fmt.Errorf("%s is not a directory", workdir))
			} else {
				check("workdir "+row.Window, nil)
			}
		}
	}
	if runnerErr == nil {
		for _, row := range runnerRows {
			workdir := config.ExpandHome(row.Workdir)
			stat, statErr := os.Stat(workdir)
			if statErr != nil {
				check("runner workdir "+row.Window, fmt.Errorf("%s: %w", workdir, statErr))
			} else if !stat.IsDir() {
				check("runner workdir "+row.Window, fmt.Errorf("%s is not a directory", workdir))
			} else {
				check("runner workdir "+row.Window, nil)
			}
		}
	}
	if os.Getenv("TMUX") != "" {
		runner := tmux.Runner{}
		_, windowErr := runner.CurrentWindow()
		check("current tmux window", windowErr)
		_, workdirErr := runner.CurrentWorkdir()
		check("current tmux pane path", workdirErr)
	}
	if err == nil && len(rows) > 0 {
		statuses := checkArchivedThreadRows(check, rows)
		activeRows := rowsWithThreadStatus(rows, statuses, threadStatusActive)
		runner := tmux.Runner{}
		checkWorkspaceDrift(check, runner, workspace, session, activeRows, runnerRows)
		checkShelvedLiveDrift(check, runner, workspace, session, rowsWithThreadStatus(rows, statuses, threadStatusArchived))
	}
	if runnerErr == nil && len(runnerRows) > 0 {
		runner := tmux.Runner{}
		checkRunnerDrift(check, runner, workspace, session, rows, runnerRows)
	}
	if failed {
		return errors.New("doctor found problems")
	}
	return nil
}

func checkWorkspaceDrift(check func(string, error), runner tmux.Runner, workspace, session string, rows []config.Row, runnerRows []config.RunnerRow) {
	panes, err := runner.Panes(session)
	if err != nil {
		check("tmux session "+session+" panes", err)
		return
	}
	check("tmux session "+session+" panes", nil)

	configured := make(map[string]config.Row, len(rows))
	for _, row := range rows {
		configured[row.Window] = row
	}
	configuredRunner := make(map[string]config.RunnerRow, len(runnerRows))
	for _, row := range runnerRows {
		configuredRunner[row.Window] = row
	}
	liveRunnerWindows := liveRunnerWindowNames(runner, session)
	live := make(map[string]tmux.Pane, len(panes))
	for _, pane := range panes {
		if _, ok := live[pane.Window]; !ok {
			live[pane.Window] = pane
		}
	}

	for _, row := range rows {
		pane, ok := live[row.Window]
		if !ok {
			check("live window "+row.Window, fmt.Errorf("configured in workspace %s but not running in tmux session %s", workspace, session))
			continue
		}
		configuredWorkdir := config.ExpandHome(row.Workdir)
		if pane.Path != configuredWorkdir {
			check("pane path "+row.Window, fmt.Errorf("configured %s but live pane path is %s", configuredWorkdir, pane.Path))
		} else {
			check("pane path "+row.Window, nil)
		}
	}

	for _, pane := range panes {
		_, threadConfigured := configured[pane.Window]
		_, runnerConfigured := configuredRunner[pane.Window]
		_, liveRunner := liveRunnerWindows[pane.Window]
		if !threadConfigured && !runnerConfigured && !liveRunner {
			check("stored window "+pane.Window, fmt.Errorf("running in tmux session %s but not configured in workspace %s", session, workspace))
		}
	}
}

func liveRunnerWindowNames(runner tmux.Runner, session string) map[string]bool {
	windowPanes, err := runnerWindowPanesByName(runner, session)
	if err != nil {
		return nil
	}
	windows := make(map[string]bool)
	for window, panes := range windowPanes {
		if strings.Contains(strings.Join(windowPaneCommands(panes), "\n"), "amp --no-tui") {
			windows[window] = true
		}
	}
	return windows
}

func checkRunnerDrift(check func(string, error), runner tmux.Runner, workspace, session string, rows []config.Row, runnerRows []config.RunnerRow) {
	panes, err := runner.Panes(session)
	if err != nil {
		check("runner tmux session "+session+" panes", err)
		return
	}
	check("runner tmux session "+session+" panes", nil)
	windowPanes, err := runnerWindowPanesByName(runner, session)
	if err != nil {
		check("runner tmux session "+session+" start commands", err)
		return
	}
	check("runner tmux session "+session+" start commands", nil)

	threadWindows := make(map[string]bool, len(rows))
	for _, row := range rows {
		threadWindows[row.Window] = true
	}
	live := make(map[string]tmux.Pane, len(panes))
	for _, pane := range panes {
		if _, ok := live[pane.Window]; !ok {
			live[pane.Window] = pane
		}
	}
	configuredRunner := make(map[string]config.RunnerRow, len(runnerRows))
	for _, row := range runnerRows {
		configuredRunner[row.Window] = row
		if threadWindows[row.Window] {
			check("runner conflict "+row.Window, fmt.Errorf("runner window also exists as a thread restore row in workspace %s", workspace))
		}
		pane, ok := live[row.Window]
		if !ok {
			check("runner live window "+row.Window, fmt.Errorf("configured in runner workspace %s but not running in tmux session %s; run amux runner launch %s %s", workspace, session, workspace, session))
			continue
		}
		configuredWorkdir := config.ExpandHome(row.Workdir)
		if pane.Path != configuredWorkdir {
			check("runner pane path "+row.Window, fmt.Errorf("configured %s but live pane path is %s", configuredWorkdir, pane.Path))
		} else {
			check("runner pane path "+row.Window, nil)
		}
		runnerPanes, err := runner.WindowPanesWithCommand(session, row.Window)
		if err != nil {
			check("runner start command "+row.Window, err)
		} else if len(runnerPanes) == 0 {
			check("runner start command "+row.Window, fmt.Errorf("no tmux pane start command found for runner window"))
		} else if len(runnerPanes) > 1 {
			check("runner start command "+row.Window, fmt.Errorf("ambiguous runner window panes: candidates %s", formatPaneCandidates(runnerPanes)))
		} else if err := verifyRunnerParkPane(row, runnerPanes[0]); err != nil {
			check("runner start command "+row.Window, err)
		} else {
			check("runner start command "+row.Window, nil)
		}
	}
	for _, pane := range panes {
		if strings.Contains(strings.Join(windowPaneCommands(windowPanes[pane.Window]), "\n"), "amp --no-tui") {
			if _, ok := configuredRunner[pane.Window]; !ok {
				check("runner stored window "+pane.Window, fmt.Errorf("runner is live in tmux session %s but not configured in runner workspace %s; run amux runner pin %s %s %s", session, workspace, workspace, pane.Window, pane.Path))
			}
		}
	}
}

func runnerWindowPanesByName(runner tmux.Runner, session string) (map[string][]tmux.WindowPane, error) {
	panes, err := runner.AllWindowPanes()
	if err != nil {
		return nil, err
	}
	byName := make(map[string][]tmux.WindowPane)
	for _, pane := range panes {
		if pane.Session == session {
			byName[pane.Window] = append(byName[pane.Window], pane)
		}
	}
	return byName, nil
}

func windowPaneCommands(panes []tmux.WindowPane) []string {
	commands := make([]string, 0, len(panes))
	for _, pane := range panes {
		commands = append(commands, pane.StartCommand)
	}
	return commands
}

func checkArchivedThreadRows(check func(string, error), rows []config.Row) map[string]threadStatus {
	statuses, err := threadArchiveStatuses(rows)
	if err != nil {
		check("Amp thread archive state", err)
		return nil
	}
	check("Amp thread archive state", nil)
	for _, row := range rows {
		status := statuses[canonicalThreadID(row.Thread)]
		switch status {
		case threadStatusArchived:
			// Archived restore rows are intentional for shelved workspaces.
			// They remain valid restore rows, but launch skips them until unshelve.
			check("thread "+row.Window, nil)
		case threadStatusMissing:
			check("thread "+row.Window, fmt.Errorf("Amp thread %s was not found in active or archived thread lists", canonicalThreadID(row.Thread)))
		default:
			check("thread "+row.Window, nil)
		}
	}
	return statuses
}

func rowsWithThreadStatus(rows []config.Row, statuses map[string]threadStatus, status threadStatus) []config.Row {
	if statuses == nil {
		return rows
	}
	filtered := make([]config.Row, 0, len(rows))
	for _, row := range rows {
		if statuses[canonicalThreadID(row.Thread)] == status {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func checkShelvedLiveDrift(check func(string, error), runner tmux.Runner, workspace, session string, rows []config.Row) {
	if len(rows) == 0 {
		return
	}
	panes, err := runner.Panes(session)
	if err != nil {
		check("shelved tmux session "+session+" panes", err)
		return
	}
	live := make(map[string]bool, len(panes))
	for _, pane := range panes {
		live[pane.Window] = true
	}
	for _, row := range rows {
		if live[row.Window] {
			check("shelved live window "+row.Window, fmt.Errorf("shelved row in workspace %s is still running in tmux session %s", workspace, session))
		}
	}
}

func ensureConfigWritable(path string) error {
	if err := config.Ensure(path); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

func ensureConfigReadable(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return file.Close()
}

func rowsForWorkspace(path, workspace string) ([]config.Row, error) {
	rows, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	filtered := make([]config.Row, 0, len(rows))
	for _, row := range rows {
		if row.Workspace == workspace {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func rowsForWorkspaceReadOnly(path, workspace string) ([]config.Row, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows, err := config.Parse(file)
	if err != nil {
		return nil, err
	}
	filtered := make([]config.Row, 0, len(rows))
	for _, row := range rows {
		if row.Workspace == workspace {
			filtered = append(filtered, row)
		}
	}
	return filtered, nil
}

func runnerRowsForWorkspace(path, workspace string) ([]config.RunnerRow, error) {
	rows, err := config.LoadRunners(path)
	if err != nil {
		return nil, err
	}
	return filterRunnerRows(rows, workspace), nil
}

func runnerRowsForWorkspaceReadOnly(path, workspace string) ([]config.RunnerRow, error) {
	rows, err := config.LoadRunnersReadOnly(path)
	if err != nil {
		return nil, err
	}
	return filterRunnerRows(rows, workspace), nil
}

func filterRunnerRows(rows []config.RunnerRow, workspace string) []config.RunnerRow {
	filtered := make([]config.RunnerRow, 0, len(rows))
	for _, row := range rows {
		if row.Workspace == workspace {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (a app) usage() {
	program := filepath.Base(os.Args[0])
	fmt.Fprintf(a.stdout, `Usage: %s [--config path] [--dry-run] [--attach] [--no-attach] [--terminal-launcher command] [command] [args]

Commands:
  launch [workspace] [session]
      Launch or attach a tmux session. With one workspace arg, session defaults
      to the workspace name; with no args, launches every configured workspace
      into its same-named tmux session without attaching. Use an explicit workspace
      with --attach.
      Multi-workspace launch runs in sorted order and stops at the first failure;
      the error identifies that workspace and earlier workspaces may be restored.
      Cold launches do not attach; existing config-matching sessions attach.
      Side effects: reads restore config, may create live local tmux/Amp
      windows for unshelved rows, and skips archived/shelved rows. It does not
      create, archive, or unarchive remote Amp threads.
      Use --attach with a workspace to always attach or --no-attach to never attach.
      If tmux cannot attach because the caller is not a terminal,
      --terminal-launcher (or AMUX_TERMINAL_LAUNCHER) runs before the default
      Omarchy launcher and Alacritty fallback.
      If no command is given, launch is assumed.

  list [--status] [--active|--shelved] [workspace]
      Print configured rows from local restore config. By default this is local-only
      and does not call Amp. --status appends a status column: active, shelved,
      missing, or unknown. --active prints only confirmed active rows; --shelved
      prints only confirmed shelved rows. If Amp thread status cannot be confirmed,
      --status shows unknown and filtered list fails closed.
      Side effects: none; status/filter modes inspect remote Amp thread state.

  workspaces [--include-runners]
      Print unique configured workspace names, sorted one per line. By default
      this reads local restore config only and does not include runner-only
      workspaces. Use --include-runners to also read runners.tsv.
      Side effects: none; reads local config only and does not call Amp or tmux.

  shelved [workspace]
      Shortcut for list --shelved [workspace].
      Side effects: none; reads restore config and inspects remote Amp thread state.

  pin <workspace> <window> <workdir> <thread-id-or-url>
      Add or replace one restore-config row.
      Side effects: mutates restore config only.
      Compatibility alias: store.

  pin-current <thread-id-or-url>
  pin-current <workspace> <thread-id-or-url> [window] [workdir]
      Add or replace a restore-config row using the current tmux window and pane path.
      Side effects: mutates restore config only.
      Compatibility alias: store-current.

  unpin <workspace> <window>
      Remove one restore-config row from a workspace.
      Side effects: mutates restore config only.
      Compatibility alias: remove.

  unpin-current [workspace]
      Remove the current tmux window from a workspace's restore config.
      Side effects: mutates restore config only.
      Compatibility alias: remove-current.

  park
  park --workspace <workspace>
  park <window>
  park <workspace> <window> [session]
  park -- <window>
  park -- <workspace> <window> [session]
      Park every configured client on this machine, every client in one
      workspace, or one workspace window. Positional forms preserve the legacy
      mac/Amp defaults; pass a session to target another tmux session.
      Put -- before all positional arguments when a name begins with a dash.
      Preflight verifies every selected client.
      Schedule delayed pane shutdown and return before local Amp processes exit.
      The delayed shutdown force-closes tmux only if graceful exit times out.
      Side effects: mutates live local tmux/Amp only. Restore config rows are
      preserved; use unpin for config-only cleanup or teardown for full cleanup.
      Amp thread history is not archived or deleted.

  park-current [workspace]
      Resolve the invoking pane's live tmux window, schedule delayed pane
      shutdown, and return before the local Amp process exits.
      The delayed shutdown force-closes tmux only if graceful exit times out.
      Side effects: mutates live local tmux/Amp only. Restore config rows are
      preserved; use unpin-current for config-only cleanup or teardown for full cleanup.
      Amp thread history is not archived or deleted.

  restart [[workspace] <window> [session]]
      Force-restart a configured, verified Amp client in place, continuing the
      same remote thread in the same tmux window. With no args, discover verified
      configured clients across all tmux sessions, preflight them, skip rows that
      are not running, then restart live panes in sorted config order. With an
      explicit workspace and no session, session defaults to the workspace name.
      This does not change restore config or remote Amp thread state. Refuses
      duplicate, mismatched, or ambiguous targets and stops at runtime failure.

  shelve-current [workspace] [thread-id-or-url]
      From inside the target tmux/Amp pane, pin the current window/path if needed,
      archive the identified Amp thread so it leaves the Amp sidebar, preserve
      the restore row, and stop the current tmux window. The thread argument may
      be omitted only when AMUX_THREAD_ID is set. This is shelving; park-current
      is local-only and never archives remote Amp thread state.
      Side effects: may add/update one restore-config row, archives one remote
      Amp thread, and stops the current live local tmux/Amp window.

  shelve [workspace] <window> [session]
  shelve --thread <thread-id-or-url> [--session <session>]
  shelve --workspace <workspace> [--session <session>]
      Archive Amp thread(s) so they leave the Amp sidebar while preserving the
      restore-config row(s) for future explicit unshelve+launch. If a matching live tmux window
      exists, verify its start command matches the stored thread before stopping
      it. With --thread, resolve one stored row by thread ID/URL and search all
      tmux sessions unless --session is provided. With --workspace, shelve every
      row in that workspace using the workspace-named session unless --session
      is provided. With one positional window and no workspace, defaults remain
      workspace=mac session=Amp.
      Side effects: archives remote Amp thread(s), may stop verified live local
      tmux/Amp windows, and does not remove restore-config rows.

  unshelve [workspace] <window>
  unshelve --thread <thread-id-or-url>
  unshelve --workspace <workspace>
      Explicitly unarchive shelved Amp thread(s) while preserving restore-config
      row(s). Run launch afterwards to restore live tmux/Amp windows.
      Side effects: unarchives remote Amp thread(s) only.

  spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] [--message-file <path> | --message-stdin] <window> <workdir> [<initial-message>] [workspace] [session]
      Create an empty Amp thread, open it in an interactive tmux window,
      submit the initial message through tmux, and store the row.
      Positional <initial-message> is single-line only. Use --message-file or
      --message-stdin for explicit multi-line prompts; these options are
      mutually exclusive with positional <initial-message> and each other.
      With one workspace arg, session defaults to the workspace name; with no
      workspace arg, defaults remain workspace=mac session=Amp.
      The spawned Amp process receives AMUX_WORKSPACE, AMUX_SESSION,
      AMUX_WINDOW, AMUX_THREAD_ID, and AMUX_WORKDIR identity variables.
      Use --mode or -m to create the remote Amp thread with an Amp mode.
      Built-in Dial modes are low, medium, high, and ultra; custom/plugin
      mode strings are passed through unchanged.
      Use --title-prefix to name the spawned tmux window "<prefix> <window>"
      and rename only the newly created Amp thread to that same name after the
      initial message is submitted, for example "#255 worker".
      If the Amp thread rename fails after the worker is created, spawn reports
      a warning with a retry command and leaves the created/stored worker intact.
      Side effects: creates a remote Amp thread, mutates live local tmux/Amp,
      may rename the new remote Amp thread, and stores the restore-config row
      under the final window name.
      With --dry-run, only validate and print intended actions; do not create
      or rename an Amp thread, mutate tmux, send keys, or update the config.

  teardown [<workspace> <window> [session]]
  teardown --thread <thread-id-or-url> [--session <session>]
      With no args, from an amux-spawned Amp process, verify AMUX_* identity,
      archive the matching Amp thread, remove the restore row, and stop the
      matched tmux window. With explicit workspace/window, verify the restore
      row and live tmux window start command agree on the same thread before
      archiving/removing/stopping; session defaults to the workspace name unless
      passed explicitly. With --thread, resolve the stored row and
      verified live tmux window by thread id or Amp thread URL; pass --session
      when more than one tmux session could contain the window. Refuses to run
      if identity or tmux/config state is ambiguous.
      Side effects: mutates all three domains: remote Amp thread state,
      restore config, and live local tmux/Amp.

  prune-archived [workspace]
      Remove restore-config rows whose Amp thread is confirmed archived.
      Active rows are kept. Missing threads, Amp CLI failures, or unreadable
      thread-list output are unconfirmed states and make the command fail
      without changing config. Defaults: workspace=mac.
      Side effects: mutates restore config only; does not archive/delete remote
      Amp threads and does not stop live local tmux/Amp windows.

  migrate-config
      Copy legacy ~/.config/amp-tmux config files into ~/.config/amux when the
      new files are missing. Leaves the legacy directory in place for rollback
      and older amux binaries. Side effects: config files only; no tmux or Amp
      thread changes.

  runner list [workspace]
      Print configured amp --no-tui runner rows from runners.tsv.
      Side effects: none; reads runner config only.

  runner pin <workspace> <window> <workdir>
      Add or replace one runner row. Runner rows contain no thread ID and are
      stored separately from thread restore rows.
      Side effects: mutates runner config only.

  runner unpin <workspace> <window>
      Remove one runner row from runners.tsv.
      Side effects: mutates runner config only.

  runner launch [workspace] [session]
      Start configured local amp --no-tui runners inside tmux windows. Skips an
      already-running window only when it verifies as the expected runner; fails
      closed on same-name mismatches. With one workspace arg, session defaults
      to the workspace name; with no args, launches every configured runner
      workspace into its same-named tmux session.
      Side effects: reads runner config and may create live local tmux/Amp
      runner windows; it does not create, continue, archive, or list Amp threads.

  runner park [workspace] <window> [session]
      Stop a live local runner tmux window while preserving runner config.
      With one workspace arg, session defaults to the workspace name; pass an
      explicit session such as Amp for older shared-session layouts. Refuses to
      stop a mismatched pane that is not an amp --no-tui runner for the row workdir.
      Side effects: mutates live local tmux/Amp only; no remote Amp thread state.

  runner restart [[workspace] <window> [session]]
      Force-restart a configured, verified amp --no-tui runner in place while
      preserving runner config. With no args, discover configured runners across
      all tmux sessions, preflight them, skip rows that are not running, then
      restart live panes in sorted config order. Refuses duplicate, mismatched,
      or ambiguous targets and stops at the first runtime failure.
      Side effects: mutates live local tmux/Amp only; no remote Amp thread state.

  completion <bash|zsh|fish>
      Print a static shell completion script for commands and flags. Completions
      intentionally do not inspect tmux sessions, workspaces, or Amp threads.

  update
      Download the latest GitHub release for this platform, verify its
      checksum, and replace the current binary. Refuses package-managed paths.
      With --dry-run, only print the planned update.
      Compatibility alias: self-update.

  doctor [workspace] [session]
      Check dependencies, config readability, configured workdirs, and drift
      between the selected workspace and tmux session. Also reports restore
      Amp thread archive state and missing thread rows, plus runner registry
      drift when runners.tsv is present. With one workspace arg, session
      defaults to the workspace name; with no args, defaults remain
      workspace=mac session=Amp.
      Side effects: none; inspects restore config, live local tmux, and remote
      Amp thread state.

  version, --version
      Print the amux version and build metadata.

  path
      Print the config path.

Config default: %s
Format: workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
List output: workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
Workspaces output: workspace, one per line, sorted
Status list output: workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url<TAB>status
`, program, config.DefaultPath())
}
