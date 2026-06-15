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
