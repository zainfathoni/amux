package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/tmux"
)

const (
	defaultWorkspace = "mac"
	defaultSession   = "Amp"
)

type options struct {
	configPath string
	dryRun     bool
	noAttach   bool
}

type app struct {
	stdout io.Writer
	stderr io.Writer
}

func main() {
	a := app{stdout: os.Stdout, stderr: os.Stderr}
	if err := a.run(os.Args[1:]); err != nil {
		fmt.Fprintln(a.stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return app{stdout: os.Stdout, stderr: os.Stderr}.run(args)
}

func (a app) run(args []string) error {
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
	if opts.configPath == "" {
		opts.configPath = config.DefaultPath()
	}

	command := "launch"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "launch":
		return launch(opts, args)
	case "list":
		return a.list(opts, args)
	case "store":
		return a.store(opts, args)
	case "store-current":
		return a.storeCurrent(opts, args)
	case "remove":
		return a.remove(opts, args)
	case "remove-current":
		return a.removeCurrent(opts, args)
	case "park-current":
		return a.parkCurrent(opts, args)
	case "spawn":
		return a.spawn(opts, args)
	case "path":
		if len(args) != 0 {
			return errors.New("usage: amux path")
		}
		fmt.Fprintln(a.stdout, opts.configPath)
		return nil
	case "doctor":
		if len(args) > 1 {
			return errors.New("usage: amux doctor [workspace]")
		}
		workspace := defaultWorkspace
		if len(args) == 1 {
			workspace = args[0]
		}
		return a.doctor(opts, workspace)
	case "help", "--help", "-h":
		a.usage()
		return nil
	default:
		// Compatibility with the Bash helper: `amux mac Amp` means launch.
		if len(args) <= 1 {
			return launch(opts, append([]string{command}, args...))
		}
		a.usage()
		return fmt.Errorf("unknown command: %s", command)
	}
}

func parseOptions(args []string) (options, []string, error) {
	var opts options
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.dryRun = true
		case "--no-attach":
			opts.noAttach = true
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
			} else {
				remaining = append(remaining, args[i])
			}
		}
	}
	return opts, remaining, nil
}

func launch(opts options, args []string) error {
	if len(args) > 2 {
		return errors.New("usage: amux launch [workspace] [session]")
	}
	workspace := defaultWorkspace
	session := defaultSession
	if len(args) >= 1 {
		workspace = args[0]
	}
	if len(args) == 2 {
		session = args[1]
	}

	rows, err := rowsForWorkspace(opts.configPath, workspace)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("no rows found for workspace %q in %s", workspace, opts.configPath)
	}

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
			return fmt.Errorf("missing workdir for window %q: %s", row.Window, workdir)
		}
		command := tmux.ContinueCommand(workdir, row.Thread)
		if first {
			if err := runner.NewSession(session, row.Window, command); err != nil {
				return fmt.Errorf("create tmux session %q: %w", session, err)
			}
			first = false
			continue
		}
		if tmux.WindowExists(windowNames, row.Window) {
			continue
		}
		if err := runner.NewWindow(session, row.Window, command); err != nil {
			return fmt.Errorf("create tmux window %q: %w", row.Window, err)
		}
	}
	return runner.SelectAndAttach(session, opts.noAttach)
}

func (a app) list(opts options, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: amux list [workspace]")
	}
	workspace := ""
	if len(args) == 1 {
		workspace = args[0]
	}
	rows, err := config.Load(opts.configPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, "workspace\twindow\tworkdir\tthread-id-or-url")
	for _, row := range rows {
		if workspace == "" || row.Workspace == workspace {
			fmt.Fprintf(a.stdout, "%s\t%s\t%s\t%s\n", row.Workspace, row.Window, row.Workdir, row.Thread)
		}
	}
	return nil
}

func (a app) store(opts options, args []string) error {
	if len(args) != 4 {
		return errors.New("usage: amux store <workspace> <window> <workdir> <thread-id-or-url>")
	}
	row := config.Row{Workspace: args[0], Window: args[1], Workdir: args[2], Thread: args[3]}
	return a.storeRow(opts, row)
}

func (a app) storeCurrent(opts options, args []string) error {
	if len(args) < 1 || len(args) > 4 {
		return errors.New("usage: amux store-current <thread-id-or-url> OR amux store-current <workspace> <thread-id-or-url> [window] [workdir]")
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
		fmt.Fprintf(a.stdout, "Stored %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
	}
	return nil
}

func (a app) remove(opts options, args []string) error {
	if len(args) != 2 {
		return errors.New("usage: amux remove <workspace> <window>")
	}
	return a.removeRow(opts, args[0], args[1])
}

func (a app) removeCurrent(opts options, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: amux remove-current [workspace]")
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

	runner := tmux.Runner{DryRun: opts.dryRun}
	target, err := runner.CurrentTarget()
	if err != nil {
		return fmt.Errorf("current tmux target is unavailable: %w", err)
	}
	window, err := runner.CurrentWindow()
	if err != nil {
		return fmt.Errorf("current tmux window is unavailable: %w", err)
	}

	if err := a.removeRow(opts, workspace, window); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Closing tmux window %s (%s)\n", target, window)
	fmt.Fprintln(a.stdout, "Amp thread history is not deleted; parking only removes restore state and stops the local tmux/Amp session.")
	if err := runner.KillWindow(target); err != nil {
		return fmt.Errorf("close tmux window %s: %w", target, err)
	}
	return nil
}

func (a app) removeRow(opts options, workspace, window string) error {
	removed, err := config.Remove(opts.configPath, workspace, window)
	if err != nil {
		return err
	}
	if removed {
		fmt.Fprintf(a.stdout, "Removed %s/%s from %s\n", workspace, window, opts.configPath)
	} else {
		fmt.Fprintf(a.stdout, "No row found for %s/%s in %s\n", workspace, window, opts.configPath)
	}
	return nil
}

func (a app) spawn(opts options, args []string) error {
	if len(args) < 3 || len(args) > 5 {
		return errors.New("usage: amux spawn <window> <workdir> <initial-message> [workspace] [session]")
	}
	window := args[0]
	workdir := args[1]
	initialMessage := args[2]
	workspace := defaultWorkspace
	session := defaultSession
	if len(args) >= 4 {
		workspace = args[3]
	}
	if len(args) == 5 {
		session = args[4]
	}

	if err := config.ValidateField("workspace", workspace); err != nil {
		return err
	}
	if err := config.ValidateField("window", window); err != nil {
		return err
	}
	if err := config.ValidateField("workdir", workdir); err != nil {
		return err
	}
	if err := config.ValidateField("initial-message", initialMessage); err != nil {
		return err
	}
	row := config.Row{Workspace: workspace, Window: window, Workdir: workdir}
	expandedWorkdir := config.ExpandHome(workdir)
	if stat, err := os.Stat(expandedWorkdir); err != nil || !stat.IsDir() {
		return fmt.Errorf("missing workdir: %s", expandedWorkdir)
	}
	runner := tmux.Runner{DryRun: opts.dryRun}
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

	threadBytes, err := exec.Command("amp", "threads", "new").Output()
	if err != nil {
		return fmt.Errorf("create Amp thread: %w", err)
	}
	thread := strings.TrimRight(string(threadBytes), "\r\n")
	row.Thread = thread
	if err := row.Validate(); err != nil {
		return err
	}

	command := tmux.ContinueCommand(expandedWorkdir, thread)
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

	time.Sleep(spawnDelay())
	if err := runner.SendLiteral(windowID, initialMessage); err != nil {
		return fmt.Errorf("send initial message: %w", err)
	}
	if err := runner.SendEnter(windowID); err != nil {
		return fmt.Errorf("submit initial message: %w", err)
	}
	if err := runner.SelectWindow(windowID); err != nil {
		return fmt.Errorf("select spawned window: %w", err)
	}

	if err := a.storeRow(opts, row); err != nil {
		return err
	}
	fmt.Fprintln(a.stdout, thread)
	return nil
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

func (a app) doctor(opts options, workspace string) error {
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

	check("config path", ensureConfigWritable(opts.configPath))
	rows, err := rowsForWorkspace(opts.configPath, workspace)
	check("read workspace "+workspace, err)
	if err == nil {
		if len(rows) == 0 {
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
	if os.Getenv("TMUX") != "" {
		runner := tmux.Runner{}
		_, windowErr := runner.CurrentWindow()
		check("current tmux window", windowErr)
		_, workdirErr := runner.CurrentWorkdir()
		check("current tmux pane path", workdirErr)
	}
	if failed {
		return errors.New("doctor found problems")
	}
	return nil
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

func (a app) usage() {
	program := filepath.Base(os.Args[0])
	fmt.Fprintf(a.stdout, `Usage: %s [--config path] [--dry-run] [--no-attach] [command] [args]

Commands:
  launch [workspace] [session]
      Launch or attach a tmux session. Defaults: workspace=mac session=Amp.
      If no command is given, launch is assumed.

  list [workspace]
      Print configured rows.

  store <workspace> <window> <workdir> <thread-id-or-url>
      Add or replace one workspace row.

  store-current <thread-id-or-url>
  store-current <workspace> <thread-id-or-url> [window] [workdir]
      Add or replace a row using the current tmux window and pane path.

  remove <workspace> <window>
      Remove one configured window from a workspace.

  remove-current [workspace]
      Remove the current tmux window from a workspace.

  park-current [workspace]
      Remove the current tmux window from restore config and close it locally.
      Amp thread history is not deleted.

  spawn <window> <workdir> <initial-message> [workspace] [session]
      Create an empty Amp thread, open it in an interactive tmux window,
      submit the initial message with tmux send-keys, and store the row.

  doctor [workspace]
      Check dependencies, config readability, and configured workdirs.

  path
      Print the config path.

Config default: %s
Format: workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
`, program, config.DefaultPath())
}
