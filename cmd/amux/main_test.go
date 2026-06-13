package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpawnCreatesInteractiveAmpWindowAndStoresThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "work dir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-new-thread\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "new win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tnew win\t"+workdir+"\tT-new-thread\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain spawned row\ngot:  %q\nwant: %q", got, want)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"new-session -d -P -F #{window_id} -s Amp -n new win cd '" + workdir + "' && exec amp threads continue 'T-new-thread'",
		"send-keys -t @1 -l hello Amp",
		"send-keys -t @1 C-m",
		"select-window -t @1",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("tmux log missing %q\nlog:\n%s", want, log)
		}
	}
}

func TestSpawnRefusesExistingWindowBeforeCreatingThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 0
fi
if [ "$1" = list-windows ]; then
  printf 'existing\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := run([]string{"--config", configPath, "spawn", "existing", workdir, "hello"})
	if err == nil {
		t.Fatal("spawn succeeded, want existing-window error")
	}
	if !strings.Contains(err.Error(), `window "existing" already exists in tmux session "Amp"`) {
		t.Fatalf("got error %q, want existing-window error", err)
	}
	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("amp threads new was called before existing-window check")
	}
}

func TestStoreCurrentInfersWindowAndWorkdirFromTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ]; then
  case "$3" in
    '#W') printf 'current-window\n'; exit 0 ;;
    '#{pane_current_path}') printf '/tmp/current workdir\n'; exit 0 ;;
  esac
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")

	if err := run([]string{"--config", configPath, "store-current", "T-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tcurrent-window\t/tmp/current workdir\tT-current\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain inferred current row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStoreCurrentWithExplicitWindowAndWorkdirDoesNotRequireTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	t.Setenv("TMUX", "")

	if err := run([]string{"--config", configPath, "store-current", "mac", "T-current", "explicit-window", "/tmp/explicit-workdir"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\texplicit-window\t/tmp/explicit-workdir\tT-current\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain explicit current row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRemoveCurrentInfersWindowFromTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent-window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = '#W' ]; then
  printf 'current-window\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")

	if err := run([]string{"--config", configPath, "remove-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(configBytes)
	if strings.Contains(got, "current-window") {
		t.Fatalf("config still contains removed current window: %q", got)
	}
	if want := "mac\tother\t/tmp\tT-other\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not preserve other row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRemoveCurrentRequiresTmux(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMUX", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "remove-current"})
	if err == nil {
		t.Fatal("remove-current succeeded outside tmux, want error")
	}
	if !strings.Contains(err.Error(), "current tmux window is unavailable: run inside tmux") {
		t.Fatalf("got error %q, want outside-tmux error", err)
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
