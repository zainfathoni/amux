package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Runner struct {
	DryRun           bool
	TerminalLauncher string
	Output           io.Writer
}

func (r Runner) output() io.Writer {
	if r.Output != nil {
		return r.Output
	}
	return os.Stdout
}

type Pane struct {
	Window string
	Path   string
}

type WindowPane struct {
	Session      string
	Window       string
	WindowID     string
	PaneID       string
	Path         string
	Command      string
	StartCommand string
	Dead         bool
}

const restartPaneFormat = "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_dead}"

func parseRestartPanes(out []byte) ([]WindowPane, error) {
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	var panes []WindowPane
	for _, line := range strings.Split(text, "\n") {
		fields := strings.SplitN(line, "\t", 8)
		if len(fields) != 8 || (fields[7] != "0" && fields[7] != "1") {
			return nil, fmt.Errorf("unexpected tmux restart pane row %q", line)
		}
		panes = append(panes, WindowPane{Session: fields[0], Window: fields[1], WindowID: fields[2], PaneID: fields[3], Path: fields[4], Command: fields[5], StartCommand: fields[6], Dead: fields[7] == "1"})
	}
	return panes, nil
}

func (r Runner) RestartWindowPanes(session, window string) ([]WindowPane, error) {
	out, err := tmuxOutput("list-panes", "-s", "-t", exactSessionTarget(session), "-F", restartPaneFormat)
	if err != nil {
		return nil, err
	}
	panes, err := parseRestartPanes(out)
	if err != nil {
		return nil, err
	}
	filtered := panes[:0]
	for _, pane := range panes {
		if pane.Session != session || pane.Window != window {
			continue
		}
		filtered = append(filtered, pane)
	}
	return filtered, nil
}

func (r Runner) AllRestartWindowPanes() ([]WindowPane, error) {
	out, err := tmuxOutput("list-panes", "-a", "-F", restartPaneFormat)
	if err != nil {
		return nil, err
	}
	return parseRestartPanes(out)
}

func (r Runner) RestartPaneByID(paneID string) (WindowPane, error) {
	out, err := tmuxOutput("list-panes", "-t", paneID, "-F", restartPaneFormat)
	if err != nil {
		return WindowPane{}, err
	}
	panes, err := parseRestartPanes(out)
	if err != nil {
		return WindowPane{}, err
	}
	for _, pane := range panes {
		if pane.PaneID == paneID {
			return pane, nil
		}
	}
	return WindowPane{}, fmt.Errorf("tmux pane %s was not found", paneID)
}

func exactSessionTarget(session string) string {
	return "=" + session
}

func nextWindowTarget(session string) string {
	return exactSessionTarget(session) + ":"
}

func (r Runner) HasSession(session string) bool {
	if r.DryRun {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", exactSessionTarget(session)).Run() == nil
}

// SessionExists distinguishes a confirmed missing session from failures to
// contact tmux. Callers which mutate state must not treat those failures as
// absence.
func (r Runner) SessionExists(session string) (bool, error) {
	if r.DryRun {
		return false, nil
	}
	cmd := exec.Command("tmux", "has-session", "-t", exactSessionTarget(session))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return false, fmt.Errorf("tmux has-session: %w", err)
	}
	message := strings.TrimSpace(stderr.String())
	if message == "" || strings.Contains(message, "can't find session") || strings.Contains(message, "no server running") ||
		(strings.Contains(message, "error connecting to ") && strings.Contains(message, "No such file or directory")) {
		return false, nil
	}
	return false, fmt.Errorf("tmux has-session: %s", message)
}

func (r Runner) WindowNames(session string) ([]string, error) {
	if r.DryRun {
		return nil, nil
	}
	out, err := tmuxOutput("list-windows", "-t", exactSessionTarget(session), "-F", "#{window_name}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func (r Runner) Panes(session string) ([]Pane, error) {
	if r.DryRun {
		return nil, nil
	}
	out, err := tmuxOutput("list-panes", "-s", "-t", session, "-F", "#{window_name}\t#{pane_current_path}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	panes := make([]Pane, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			return nil, fmt.Errorf("unexpected tmux pane row %q", line)
		}
		panes = append(panes, Pane{Window: fields[0], Path: fields[1]})
	}
	return panes, nil
}

func (r Runner) WindowPanes(session, window string) ([]WindowPane, error) {
	if r.DryRun {
		return nil, nil
	}
	out, err := tmuxOutput("list-panes", "-s", "-t", session, "-F", "#{window_name}\t#{window_id}\t#{pane_start_command}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	panes := make([]WindowPane, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("unexpected tmux pane row %q", line)
		}
		if fields[0] != window {
			continue
		}
		panes = append(panes, WindowPane{Session: session, Window: fields[0], WindowID: fields[1], StartCommand: fields[2]})
	}
	return panes, nil
}

func (r Runner) WindowPanesWithCommand(session, window string) ([]WindowPane, error) {
	if r.DryRun {
		return nil, nil
	}
	out, err := tmuxOutput("list-panes", "-s", "-t", session, "-F", "#{window_name}\t#{window_id}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_start_command}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	panes := make([]WindowPane, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("unexpected tmux pane row %q", line)
		}
		if fields[0] != window {
			continue
		}
		panes = append(panes, WindowPane{Session: session, Window: fields[0], WindowID: fields[1], Path: fields[2], Command: fields[3], StartCommand: fields[4]})
	}
	return panes, nil
}

func (r Runner) AllWindowPanes() ([]WindowPane, error) {
	if r.DryRun {
		return nil, nil
	}
	out, err := tmuxOutput("list-panes", "-a", "-F", "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_start_command}")
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	panes := make([]WindowPane, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) != 4 {
			return nil, fmt.Errorf("unexpected tmux pane row %q", line)
		}
		panes = append(panes, WindowPane{Session: fields[0], Window: fields[1], WindowID: fields[2], StartCommand: fields[3]})
	}
	return panes, nil
}

func (r Runner) NewSession(session, window, command string) error {
	args := []string{"new-session", "-d", "-s", session, "-n", window, command}
	if r.DryRun {
		fmt.Fprintf(r.output(), "tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) NewWindow(session, window, command string) error {
	args := []string{"new-window", "-t", nextWindowTarget(session), "-n", window, command}
	if r.DryRun {
		fmt.Fprintf(r.output(), "tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) NewSessionWindowID(session, window, command string) (string, error) {
	args := []string{"new-session", "-d", "-P", "-F", "#{window_id}", "-s", session, "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return "", nil
	}
	out, err := tmuxOutput(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (r Runner) NewWindowID(session, window, command string) (string, error) {
	args := []string{"new-window", "-P", "-F", "#{window_id}", "-t", nextWindowTarget(session), "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return "", nil
	}
	out, err := tmuxOutput(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (r Runner) SendLiteral(target, text string) error {
	args := []string{"send-keys", "-t", target, "-l", text}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) PasteLiteral(target, text string) error {
	bufferName := fmt.Sprintf("amux-spawn-message-%d", os.Getpid())
	loadArgs := []string{"load-buffer", "-b", bufferName, "-"}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(loadArgs))
	} else if err := tmuxRunInput(text, loadArgs...); err != nil {
		return err
	}
	pasteArgs := []string{"paste-buffer", "-dpr", "-b", bufferName, "-t", target}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(pasteArgs))
		return nil
	}
	return tmuxRun(pasteArgs...)
}

func (r Runner) SendEnter(target string) error {
	args := []string{"send-keys", "-t", target, "Enter"}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) ClearLine(target string) error {
	args := []string{"send-keys", "-t", target, "C-u"}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) CapturePane(target string) (string, error) {
	args := []string{"capture-pane", "-J", "-p", "-t", target}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return "", nil
	}
	out, err := tmuxOutput(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (r Runner) PaneID(target string) (string, error) {
	if r.DryRun {
		fmt.Printf("tmux display-message -p -t %s '#{pane_id}'\n", shellQuote(target))
		return "", nil
	}
	return displayMessageForTarget(target, "#{pane_id}")
}

func (r Runner) SelectWindow(target string) error {
	args := []string{"select-window", "-t", target}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) KillWindow(target string) error {
	args := []string{"kill-window", "-t", target}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) RespawnPane(target, command string) error {
	args := []string{"respawn-pane", "-k", "-t", target, command}
	if r.DryRun {
		fmt.Fprintf(r.output(), "tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) RunShell(command string) error {
	args := []string{"run-shell", "-b", command}
	if r.DryRun {
		fmt.Fprintf(r.output(), "tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) SelectAndAttach(session string, noAttach bool) error {
	if noAttach {
		return nil
	}
	if r.DryRun {
		fmt.Fprintf(r.output(), "tmux select-window -t %s:1\n", shellQuote(session))
		fmt.Fprintf(r.output(), "tmux attach -t %s\n", shellQuote(session))
		return nil
	}
	if err := tmuxRun("select-window", "-t", session+":1"); err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		return tmuxRun("switch-client", "-t", session)
	}
	if err := tmuxAttach(session); err != nil {
		if isNoTerminalAttachError(err) {
			return startTerminalAttach(session, r.TerminalLauncher)
		}
		return err
	}
	return nil
}

func tmuxAttach(session string) error {
	cmd := exec.Command("tmux", "attach", "-t", session)
	var stderr bytes.Buffer
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	if err := cmd.Run(); err != nil {
		return commandError([]string{"attach", "-t", session}, nil, stderr.Bytes(), err)
	}
	return nil
}

func startTerminalAttach(session, terminalLauncher string) error {
	commands, err := terminalAttachCommands(session, terminalLauncher)
	if err != nil {
		return err
	}
	var lastErr error
	for _, args := range commands {
		if len(args) == 0 {
			continue
		}
		if err := exec.Command(args[0], args[1:]...).Start(); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func terminalAttachCommands(session, terminalLauncher string) ([][]string, error) {
	attachArgs := []string{"tmux", "attach", "-t", session}
	commands := make([][]string, 0, 3)
	if strings.TrimSpace(terminalLauncher) != "" {
		launcherArgs, err := shellFields(terminalLauncher)
		if err != nil {
			return nil, fmt.Errorf("parse terminal launcher: %w", err)
		}
		commands = append(commands, append(append([]string{}, launcherArgs...), attachArgs...))
	}
	if _, err := exec.LookPath("uwsm-app"); err == nil {
		if _, err := exec.LookPath("xdg-terminal-exec"); err == nil {
			commands = append(commands, []string{"uwsm-app", "--", "xdg-terminal-exec", "-e", "tmux", "attach", "-t", session})
		}
	}
	commands = append(commands, []string{"alacritty", "-e", "tmux", "attach", "-t", session})
	return commands, nil
}

func shellFields(input string) ([]string, error) {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false
	inField := false
	for _, r := range input {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
			inField = true
		case r == '\\':
			escaped = true
			inField = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			inField = true
		case r == '\'' || r == '"':
			quote = r
			inField = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inField {
				fields = append(fields, current.String())
				current.Reset()
				inField = false
			}
		default:
			current.WriteRune(r)
			inField = true
		}
	}
	if escaped {
		return nil, errors.New("unfinished escape")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if inField {
		fields = append(fields, current.String())
	}
	return fields, nil
}

func isNoTerminalAttachError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not a terminal") || strings.Contains(message, "open terminal failed")
}

func (r Runner) CurrentWindow() (string, error) {
	return displayCurrentMessage("#W")
}

func (r Runner) CurrentTarget() (string, error) {
	return displayCurrentMessage("#S:#I")
}

func (r Runner) CurrentWindowID() (string, error) {
	return displayCurrentMessage("#{window_id}")
}

func (r Runner) CurrentWorkdir() (string, error) {
	return displayCurrentMessage("#{pane_current_path}")
}

func displayCurrentMessage(format string) (string, error) {
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		return displayMessageForTarget(pane, format)
	}
	return "", fmt.Errorf("TMUX_PANE is unavailable; run amux from the pane you want to target instead of relying on tmux's active client")
}

func displayMessageForTarget(target, format string) (string, error) {
	out, err := tmuxOutput("display-message", "-p", "-t", target, format)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func tmuxRun(args ...string) error {
	_, err := tmuxOutput(args...)
	return err
}

func tmuxRunInput(input string, args ...string) error {
	cmd := exec.Command("tmux", args...)
	cmd.Stdin = strings.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return commandError(args, stdout.Bytes(), stderr.Bytes(), err)
	}
	return nil
}

func tmuxOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("tmux", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, commandError(args, stdout.Bytes(), stderr.Bytes(), err)
	}
	return stdout.Bytes(), nil
}

func commandError(args []string, stdout, stderr []byte, err error) error {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		message = strings.TrimSpace(string(stdout))
	}
	if message == "" {
		return err
	}
	return fmt.Errorf("tmux %s: %w: %s", shellJoin(args), err, message)
}

func WindowExists(names []string, window string) bool {
	for _, name := range names {
		if name == window {
			return true
		}
	}
	return false
}

func ContinueCommand(workdir, thread string) string {
	return "cd " + shellQuote(workdir) + " && exec amp threads continue " + shellQuote(thread)
}

func RunnerCommand(workdir string) string {
	return "cd " + shellQuote(workdir) + " && exec amp --no-tui"
}

func ContinueCommandWithEnv(workdir, thread string, env map[string]string) string {
	assignments := []string{
		"AMUX_WORKSPACE=" + shellQuote(env["AMUX_WORKSPACE"]),
		"AMUX_SESSION=" + shellQuote(env["AMUX_SESSION"]),
		"AMUX_WINDOW=" + shellQuote(env["AMUX_WINDOW"]),
		"AMUX_THREAD_ID=" + shellQuote(env["AMUX_THREAD_ID"]),
		"AMUX_WORKDIR=" + shellQuote(env["AMUX_WORKDIR"]),
	}
	return "cd " + shellQuote(workdir) + " && " + strings.Join(assignments, " ") + " exec amp threads continue " + shellQuote(thread)
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
