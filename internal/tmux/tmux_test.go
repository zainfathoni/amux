package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContinueCommandQuotesShellArgs(t *testing.T) {
	got := ContinueCommand("/tmp/with space/that's", "T-'thread'")
	want := "cd '/tmp/with space/that'\\''s' && exec amp threads continue 'T-'\\''thread'\\'''"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestContinueCommandWithEnvInjectsAmuxIdentity(t *testing.T) {
	got := ContinueCommandWithEnv("/tmp/work dir", "T-thread", map[string]string{
		"AMUX_WORKSPACE": "mac",
		"AMUX_SESSION":   "Amp",
		"AMUX_WINDOW":    "worker one",
		"AMUX_THREAD_ID": "T-thread",
		"AMUX_WORKDIR":   "/tmp/work dir",
	})
	want := "cd '/tmp/work dir' && AMUX_WORKSPACE='mac' AMUX_SESSION='Amp' AMUX_WINDOW='worker one' AMUX_THREAD_ID='T-thread' AMUX_WORKDIR='/tmp/work dir' exec amp threads continue 'T-thread'"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTmuxErrorsIncludeCommandOutput(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
echo 'open terminal failed: not a terminal' >&2
exit 1
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := (Runner{}).WindowNames("Amp")
	if err == nil {
		t.Fatal("WindowNames succeeded, want tmux error")
	}
	if !strings.Contains(err.Error(), "open terminal failed: not a terminal") {
		t.Fatalf("tmux error did not include stderr: %v", err)
	}
}

func TestTmuxOutputIgnoresStderrOnSuccess(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
echo 'warning on stderr' >&2
printf 'one\ntwo\n'
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	windows, err := (Runner{}).WindowNames("Amp")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(windows, ","), "one,two"; got != want {
		t.Fatalf("got windows %q, want %q", got, want)
	}
}

func TestSendEnterUsesTmuxEnterKeyName(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = send-keys ]; then
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (Runner{}).SendEnter("@1"); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logBytes)), "send-keys -t @1 Enter"; got != want {
		t.Fatalf("SendEnter sent %q, want %q", got, want)
	}
}

func TestClearLineUsesTmuxControlU(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = send-keys ]; then
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (Runner{}).ClearLine("%1"); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logBytes)), "send-keys -t %1 C-u"; got != want {
		t.Fatalf("ClearLine sent %q, want %q", got, want)
	}
}

func TestCapturePaneJoinsWrappedLines(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = capture-pane ]; then
  printf 'pane text\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	contents, err := (Runner{}).CapturePane("%1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := contents, "pane text"; got != want {
		t.Fatalf("CapturePane returned %q, want %q", got, want)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logBytes)), "capture-pane -J -p -t %1"; got != want {
		t.Fatalf("CapturePane sent %q, want %q", got, want)
	}
}

func TestSelectAndAttachInvokesAttachAfterSelectingWindow(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ] || [ "$1" = attach ]; then
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "select-window -t Amp:1") || !strings.Contains(log, "attach -t Amp") {
		t.Fatalf("SelectAndAttach did not select and attach\nlog:\n%s", log)
	}
}

func TestSelectAndAttachSwitchesClientWhenAlreadyInsideTmux(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ] || [ "$1" = switch-client ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), `#!/bin/sh
printf 'uwsm-app %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux select-window -t Amp:1",
		"tmux switch-client -t Amp",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Contains(log, "attach") || strings.Contains(log, "uwsm-app") {
		t.Fatalf("inside-tmux attach used attach/external terminal instead of switch-client\nlog:\n%s", log)
	}
}

func TestSelectAndAttachOpensOmarchyTerminalWhenTmuxCannotAttachWithoutTerminal(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = attach ]; then
  echo 'open terminal failed: not a terminal' >&2
  exit 1
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), `#!/bin/sh
printf 'uwsm-app %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), `#!/bin/sh
printf 'xdg-terminal-exec %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "alacritty"), `#!/bin/sh
printf 'alacritty %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	log := waitForLogContaining(t, logPath, "uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp")
	for _, want := range []string{
		"tmux select-window -t Amp:1",
		"tmux attach -t Amp",
		"uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Contains(log, "alacritty") {
		t.Fatalf("used alacritty despite Omarchy terminal launcher being available\nlog:\n%s", log)
	}
}

func TestSelectAndAttachFallsBackToAlacrittyWithoutOmarchyTerminalLauncher(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = attach ]; then
  echo 'open terminal failed: not a terminal' >&2
  exit 1
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "alacritty"), `#!/bin/sh
printf 'alacritty %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp)
	t.Setenv("TMUX", "")

	if err := (Runner{}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	log := waitForLogContaining(t, logPath, "alacritty -e tmux attach -t Amp")
	if !strings.Contains(log, "alacritty -e tmux attach -t Amp") {
		t.Fatalf("log missing alacritty fallback\nlog:\n%s", log)
	}
}

func TestSelectAndAttachUsesConfiguredTerminalLauncherBeforeDefaults(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = attach ]; then
  echo 'open terminal failed: not a terminal' >&2
  exit 1
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "custom-terminal"), `#!/bin/sh
printf 'custom-terminal %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), `#!/bin/sh
printf 'uwsm-app %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), `#!/bin/sh
printf 'xdg-terminal-exec %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "alacritty"), `#!/bin/sh
printf 'alacritty %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := (Runner{TerminalLauncher: "custom-terminal --new-window -e"}).SelectAndAttach("Amp", false); err != nil {
		t.Fatal(err)
	}

	log := waitForLogContaining(t, logPath, "custom-terminal --new-window -e tmux attach -t Amp")
	if !strings.Contains(log, "custom-terminal --new-window -e tmux attach -t Amp") {
		t.Fatalf("log missing configured terminal launcher\nlog:\n%s", log)
	}
	if strings.Contains(log, "uwsm-app") || strings.Contains(log, "alacritty") {
		t.Fatalf("used default fallback after configured terminal launcher started\nlog:\n%s", log)
	}
}

func TestTerminalAttachCommandsPutConfiguredLauncherBeforeOmarchyAndAlacritty(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", tmp)

	commands, err := terminalAttachCommands("Amp", "custom-terminal --class 'Amp Window' -e")
	if err != nil {
		t.Fatal(err)
	}
	got := commandStrings(commands)
	want := []string{
		"custom-terminal --class Amp Window -e tmux attach -t Amp",
		"uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp",
		"alacritty -e tmux attach -t Amp",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n got %q\nwant %q", got, want)
	}
}

func TestTerminalAttachCommandsPreserveDefaultFallbackOrder(t *testing.T) {
	tmp := t.TempDir()
	writeExecutable(t, filepath.Join(tmp, "uwsm-app"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "xdg-terminal-exec"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", tmp)

	commands, err := terminalAttachCommands("Amp", "")
	if err != nil {
		t.Fatal(err)
	}
	got := commandStrings(commands)
	want := []string{
		"uwsm-app -- xdg-terminal-exec -e tmux attach -t Amp",
		"alacritty -e tmux attach -t Amp",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n got %q\nwant %q", got, want)
	}
}

func commandStrings(commands [][]string) []string {
	formatted := make([]string, 0, len(commands))
	for _, command := range commands {
		formatted = append(formatted, strings.Join(command, " "))
	}
	return formatted
}

func waitForLogContaining(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		logBytes, err := os.ReadFile(path)
		if err == nil {
			log := string(logBytes)
			if strings.Contains(log, want) {
				return log
			}
		}
		if !time.Now().Before(deadline) {
			if err != nil {
				t.Fatal(err)
			}
			logBytes, _ := os.ReadFile(path)
			return string(logBytes)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
