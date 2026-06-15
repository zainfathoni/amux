package tmux

import (
	"fmt"
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
	out, err := exec.Command("tmux", "list-windows", "-t", session, "-F", "#{window_name}").Output()
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
	out, err := exec.Command("tmux", "list-panes", "-a", "-t", session, "-F", "#{window_name}\t#{pane_current_path}").Output()
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

func (r Runner) NewSession(session, window, command string) error {
	args := []string{"new-session", "-d", "-s", session, "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return exec.Command("tmux", args...).Run()
}

func (r Runner) NewWindow(session, window, command string) error {
	args := []string{"new-window", "-t", session, "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return exec.Command("tmux", args...).Run()
}

func (r Runner) NewSessionWindowID(session, window, command string) (string, error) {
	args := []string{"new-session", "-d", "-P", "-F", "#{window_id}", "-s", session, "-n", window, command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return "", nil
	}
	out, err := exec.Command("tmux", args...).Output()
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
	out, err := exec.Command("tmux", args...).Output()
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
	return exec.Command("tmux", args...).Run()
}

func (r Runner) SendEnter(target string) error {
	args := []string{"send-keys", "-t", target, "C-m"}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return exec.Command("tmux", args...).Run()
}

func (r Runner) SelectWindow(target string) error {
	args := []string{"select-window", "-t", target}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return exec.Command("tmux", args...).Run()
}

func (r Runner) RunShell(command string) error {
	args := []string{"run-shell", "-b", command}
	if r.DryRun {
		fmt.Printf("tmux %s\n", shellJoin(args))
		return nil
	}
	return exec.Command("tmux", args...).Run()
}

func (r Runner) SelectAndAttach(session string, noAttach bool) error {
	if r.DryRun || noAttach {
		if r.DryRun {
			fmt.Printf("tmux select-window -t %s:1\n", shellQuote(session))
			if !noAttach {
				fmt.Printf("tmux attach -t %s\n", shellQuote(session))
			}
		}
		return nil
	}
	if err := exec.Command("tmux", "select-window", "-t", session+":1").Run(); err != nil {
		return err
	}
	if os.Getenv("TMUX") != "" {
		return exec.Command("tmux", "switch-client", "-t", session).Run()
	}
	if output, err := exec.Command("tmux", "attach", "-t", session).CombinedOutput(); err != nil {
		if isNoTerminalAttachError(output) {
			return startTerminalAttach(session)
		}
		return err
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

func isNoTerminalAttachError(output []byte) bool {
	message := strings.ToLower(string(output))
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
	out, err := exec.Command("tmux", "display-message", "-p", format).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func displayMessageForTarget(target, format string) (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", target, format).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
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
