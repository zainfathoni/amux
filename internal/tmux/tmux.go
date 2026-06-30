package tmux

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Runner struct {
	DryRun bool
}

type Pane struct {
	Window string
	Path   string
}

type WindowPane struct {
	Window       string
	WindowID     string
	StartCommand string
}

func (r Runner) HasSession(session string) bool {
	if r.DryRun {
		return false
	}
	return exec.Command("tmux", "has-session", "-t", session).Run() == nil
}

func (r Runner) WindowNames(session string) ([]string, error) {
	if r.DryRun {
		return nil, nil
	}
	out, err := tmuxOutput("list-windows", "-t", session, "-F", "#{window_name}")
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
		panes = append(panes, WindowPane{Window: fields[0], WindowID: fields[1], StartCommand: fields[2]})
	}
	return panes, nil
}

func (r Runner) NewSession(session, window, command string) error {
	args := []string{"new-session", "-d", "-s", session, "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) NewWindow(session, window, command string) error {
	args := []string{"new-window", "-t", session, "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
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
	args := []string{"new-window", "-P", "-F", "#{window_id}", "-t", session, "-n", window, command}
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

func (r Runner) SendEnter(target string) error {
	args := []string{"send-keys", "-t", target, "Enter"}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
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

func (r Runner) RunShell(command string) error {
	args := []string{"run-shell", "-b", command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return tmuxRun(args...)
}

func (r Runner) SelectAndAttach(session string, noAttach bool) error {
	if noAttach {
		return nil
	}
	if r.DryRun {
		fmt.Printf("tmux select-window -t %s:1\n", shellQuote(session))
		fmt.Printf("tmux attach -t %s\n", shellQuote(session))
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
			return startTerminalAttach(session)
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

func startTerminalAttach(session string) error {
	if _, err := exec.LookPath("uwsm-app"); err == nil {
		if _, err := exec.LookPath("xdg-terminal-exec"); err == nil {
			return exec.Command("uwsm-app", "--", "xdg-terminal-exec", "-e", "tmux", "attach", "-t", session).Start()
		}
	}
	return exec.Command("alacritty", "-e", "tmux", "attach", "-t", session).Start()
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

func (r Runner) CurrentWorkdir() (string, error) {
	return displayCurrentMessage("#{pane_current_path}")
}

func displayCurrentMessage(format string) (string, error) {
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		return displayMessageForTarget(pane, format)
	}
	return displayMessage(format)
}

func displayMessage(format string) (string, error) {
	out, err := tmuxOutput("display-message", "-p", format)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
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
