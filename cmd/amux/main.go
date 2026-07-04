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

var (
	version = "dev"
	commit  = ""
	built   = ""
)

type options struct {
	configPath string
	dryRun     bool
	attachMode attachMode
}

type attachMode int

const (
	attachAuto attachMode = iota
	attachAlways
	attachNever
)

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
	case "teardown":
		return a.teardown(opts, args)
	case "self-update":
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
		workspace := defaultWorkspace
		session := defaultSession
		if len(args) == 1 {
			workspace = args[0]
		}
		if len(args) == 2 {
			workspace = args[0]
			session = args[1]
		}
		return a.doctor(opts, workspace, session)
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
		case "--attach":
			opts.attachMode = attachAlways
		case "--no-attach":
			opts.attachMode = attachNever
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
	fmt.Fprintf(a.stdout, "Scheduling tmux window %s (%s) to stop\n", target, window)
	fmt.Fprintln(a.stdout, "Amp thread history is not deleted; parking only removes restore state and stops the local tmux/Amp session.")
	if err := runner.RunShell(parkShutdownScript(target, parkShutdownDelay(), parkGracePeriod())); err != nil {
		return fmt.Errorf("schedule tmux window %s shutdown: %w", target, err)
	}
	fmt.Fprintf(a.stdout, "The local Amp process will be asked to exit in %s; tmux will force-close it only if graceful shutdown times out.\n", parkShutdownDelay())
	return nil
}

func parkShutdownScript(target string, delay, grace time.Duration) string {
	quotedTarget := shellSingleQuote(target)
	return strings.Join([]string{
		"target=" + quotedTarget,
		"sleep " + shellSeconds(delay),
		"tmux send-keys -t \"$target\" C-c >/dev/null 2>&1 || exit 0",
		"sleep 0.200",
		"tmux send-keys -t \"$target\" C-d >/dev/null 2>&1 || exit 0",
		"deadline=$(( $(date +%s) + " + fmt.Sprintf("%.0f", grace.Seconds()) + " ))",
		"while tmux display-message -p -t \"$target\" '#{pane_id}' >/dev/null 2>&1; do",
		"  if [ \"$(date +%s)\" -ge \"$deadline\" ]; then",
		"    tmux kill-window -t \"$target\" >/dev/null 2>&1 || true",
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
		fmt.Fprintf(a.stdout, "Removed %s/%s from %s\n", workspace, window, opts.configPath)
	} else {
		fmt.Fprintf(a.stdout, "No row found for %s/%s in %s\n", workspace, window, opts.configPath)
	}
	return nil
}

func (a app) spawn(opts options, args []string) error {
	spawnOpts, args, err := parseSpawnOptions(args)
	if err != nil {
		return err
	}
	if len(args) < 3 || len(args) > 5 {
		return errors.New("usage: amux spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] <window> <workdir> <initial-message> [workspace] [session]")
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
		if spawnOpts.titlePrefix != "" {
			fmt.Fprintf(a.stdout, "Would rename new Amp thread to %q\n", window)
		}
		if sessionExists {
			fmt.Fprintf(a.stdout, "Would create tmux window %q in session %q\n", window, session)
		} else {
			fmt.Fprintf(a.stdout, "Would create tmux session %q with window %q\n", session, window)
		}
		fmt.Fprintf(a.stdout, "Would start Amp in %s and submit initial message\n", expandedWorkdir)
		fmt.Fprintf(a.stdout, "Would store %s/%s in %s\n", workspace, window, opts.configPath)
		return nil
	}

	ampArgs := []string{"threads", "new"}
	if spawnOpts.mode != "" {
		ampArgs = append(ampArgs, "--mode", spawnOpts.mode)
	}
	threadBytes, err := exec.Command("amp", ampArgs...).Output()
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
	if spawnOpts.titlePrefix != "" {
		if err := renameAmpThread(thread, window); err != nil {
			fmt.Fprintf(a.stderr, "warning: rename Amp thread %s failed: %v; spawned worker was created and stored as %s/%s; retry with `amp threads rename %s %q`\n", thread, err, workspace, window, thread, window)
		}
	}
	fmt.Fprintln(a.stdout, thread)
	return nil
}

type teardownIdentity struct {
	Workspace string
	Session   string
	Window    string
	Thread    string
}

func (a app) teardown(opts options, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: amux teardown")
	}
	if opts.dryRun {
		return errors.New("teardown does not support --dry-run")
	}

	identity, err := teardownIdentityFromEnv()
	if err != nil {
		return err
	}
	row, err := verifiedTeardownRow(opts.configPath, identity)
	if err != nil {
		return err
	}
	runner := tmux.Runner{}
	pane, err := verifiedTeardownPane(runner, identity, row)
	if err != nil {
		return err
	}

	if err := archiveAmpThread(identity.Thread); err != nil {
		return fmt.Errorf("archive Amp thread %s: %w", identity.Thread, err)
	}
	if err := a.removeRow(opts, identity.Workspace, identity.Window); err != nil {
		return err
	}
	if err := runner.KillWindow(pane.WindowID); err != nil {
		return fmt.Errorf("stop tmux window %s (%s): %w", identity.Window, pane.WindowID, err)
	}
	fmt.Fprintf(a.stdout, "Archived Amp thread %s\n", identity.Thread)
	fmt.Fprintf(a.stdout, "Stopped tmux window %s/%s (%s)\n", identity.Session, identity.Window, pane.WindowID)
	fmt.Fprintf(a.stdout, "Teardown complete for %s/%s in %s\n", row.Workspace, row.Window, opts.configPath)
	return nil
}

func teardownIdentityFromEnv() (teardownIdentity, error) {
	identity := teardownIdentity{
		Workspace: os.Getenv("AMUX_WORKSPACE"),
		Session:   os.Getenv("AMUX_SESSION"),
		Window:    os.Getenv("AMUX_WINDOW"),
		Thread:    os.Getenv("AMUX_THREAD_ID"),
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
		return identity, fmt.Errorf("teardown requires spawn-injected identity; missing %s", strings.Join(missing, ", "))
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
	if row.Thread != identity.Thread {
		return config.Row{}, fmt.Errorf("restore row thread mismatch for %s/%s: AMUX_THREAD_ID=%s config=%s", identity.Workspace, identity.Window, identity.Thread, row.Thread)
	}
	return row, nil
}

func verifiedTeardownPane(runner tmux.Runner, identity teardownIdentity, row config.Row) (tmux.WindowPane, error) {
	panes, err := runner.WindowPanes(identity.Session, identity.Window)
	if err != nil {
		return tmux.WindowPane{}, fmt.Errorf("find tmux window %s/%s: %w", identity.Session, identity.Window, err)
	}
	if len(panes) == 0 {
		return tmux.WindowPane{}, fmt.Errorf("no live tmux window %q in session %q", identity.Window, identity.Session)
	}
	if len(panes) > 1 {
		return tmux.WindowPane{}, fmt.Errorf("ambiguous tmux window %q in session %q: candidates %s; refusing teardown", identity.Window, identity.Session, formatPaneCandidates(panes))
	}
	pane := panes[0]
	if pane.WindowID == "" {
		return tmux.WindowPane{}, fmt.Errorf("tmux window %q in session %q has no window id", identity.Window, identity.Session)
	}
	expectedCommand := teardownExpectedStartCommand(identity, row)
	if pane.StartCommand != expectedCommand {
		return tmux.WindowPane{}, fmt.Errorf("tmux window %q in session %q is not the expected amux-spawned command for AMUX_THREAD_ID=%s; start command: %s", identity.Window, identity.Session, identity.Thread, pane.StartCommand)
	}
	return pane, nil
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

func formatCandidates(candidates []string) string {
	if len(candidates) == 0 {
		return "none"
	}
	return strings.Join(candidates, ", ")
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

type spawnOptions struct {
	mode        string
	titlePrefix string
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
			} else {
				remaining = append(remaining, args[i])
			}
		}
	}
	return opts, remaining, nil
}

func (opts spawnOptions) prefixedName(window string) string {
	return strings.TrimSpace(strings.TrimSpace(opts.titlePrefix) + " " + window)
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
	if err == nil && len(rows) > 0 {
		runner := tmux.Runner{}
		checkWorkspaceDrift(check, runner, workspace, session, rows)
	}
	if failed {
		return errors.New("doctor found problems")
	}
	return nil
}

func checkWorkspaceDrift(check func(string, error), runner tmux.Runner, workspace, session string, rows []config.Row) {
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
		if _, ok := configured[pane.Window]; !ok {
			check("stored window "+pane.Window, fmt.Errorf("running in tmux session %s but not configured in workspace %s", session, workspace))
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

func (a app) usage() {
	program := filepath.Base(os.Args[0])
	fmt.Fprintf(a.stdout, `Usage: %s [--config path] [--dry-run] [--attach] [--no-attach] [command] [args]

Commands:
  launch [workspace] [session]
      Launch or attach a tmux session. Defaults: workspace=mac session=Amp.
      Cold launches do not attach; existing config-matching sessions attach.
      Side effects: reads restore config, may create live local tmux/Amp
      windows, and does not create or archive remote Amp threads.
      Use --attach to always attach or --no-attach to never attach.
      If no command is given, launch is assumed.

  list [workspace]
      Print configured rows.
      Side effects: none; reads restore config only.

  store <workspace> <window> <workdir> <thread-id-or-url>
      Add or replace one workspace row.
      Side effects: mutates restore config only.

  store-current <thread-id-or-url>
  store-current <workspace> <thread-id-or-url> [window] [workdir]
      Add or replace a row using the current tmux window and pane path.
      Side effects: mutates restore config only.

  remove <workspace> <window>
      Remove one configured window from a workspace.
      Side effects: mutates restore config only.

  remove-current [workspace]
      Remove the current tmux window from a workspace.
      Side effects: mutates restore config only.

  park-current [workspace]
      Remove the current tmux window from restore config, schedule delayed
      pane shutdown, and return before the local Amp process exits.
      The delayed shutdown force-closes tmux only if graceful exit times out.
      Side effects: mutates restore config and live local tmux/Amp only.
      Amp thread history is not archived or deleted.

  spawn [--mode <mode> | -m <mode>] [--title-prefix <prefix>] <window> <workdir> <initial-message> [workspace] [session]
      Create an empty Amp thread, open it in an interactive tmux window,
      submit the initial message with tmux send-keys, and store the row.
      The spawned Amp process receives AMUX_WORKSPACE, AMUX_SESSION,
      AMUX_WINDOW, AMUX_THREAD_ID, and AMUX_WORKDIR identity variables.
      Use --mode or -m to create the remote Amp thread with an Amp mode.
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

  teardown
      From an amux-spawned Amp process, verify AMUX_* identity, archive the
      matching Amp thread, remove the restore row, and stop the matched tmux
      window. Refuses to run if identity or tmux/config state is ambiguous.
      Side effects: mutates all three domains: remote Amp thread state,
      restore config, and live local tmux/Amp.

  self-update
      Download the latest GitHub release for this platform, verify its
      checksum, and replace the current binary. Refuses package-managed paths.
      With --dry-run, only print the planned update.

  doctor [workspace] [session]
      Check dependencies, config readability, configured workdirs, and drift
      between the selected workspace and tmux session. Defaults: workspace=mac
      session=Amp.
      Side effects: none; inspects restore config and live local tmux only.

  version, --version
      Print the amux version and build metadata.

  path
      Print the config path.

Config default: %s
Format: workspace<TAB>window<TAB>workdir<TAB>thread-id-or-url
`, program, config.DefaultPath())
}
