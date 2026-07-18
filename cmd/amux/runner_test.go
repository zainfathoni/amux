package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

func TestMain(m *testing.M) {
	runnerProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	runnerProcessIdentity = func(pid int) (string, error) { return fmt.Sprintf("start-%d", pid), nil }
	runnerChildProcesses = func(parentPID int) ([]tmux.ProcessMetadata, error) {
		return []tmux.ProcessMetadata{{PID: parentPID + 10000, ParentPID: parentPID, Name: "amp", Identity: fmt.Sprintf("start-%d", parentPID)}}, nil
	}
	os.Exit(m.Run())
}

func TestRunnerPinAcceptsExistingNonGitDirectoryAndIsCanonicalIdempotent(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\ntouch '"+called+"'\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	args := []string{"--json", "--config-dir", dir, "runner", "pin", "--workspace", "alpha", "--workdir", filepath.Join(workdir, ".")}
	first := executeRunnerJSON(t, args...)
	if len(first.Successful) != 1 || first.Successful[0].Resource.Workdir != workdir {
		t.Fatalf("first pin = %+v", first)
	}
	second := executeRunnerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Message != "already pinned" {
		t.Fatalf("second pin = %+v", second)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
	if err != nil || !strings.Contains(string(data), "alpha\t"+workdir+"\n") || strings.Contains(string(data), config.RunnerWindow(workdir)+"\t") {
		t.Fatalf("runner registry = %q, err=%v", data, err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("runner pin used unexpected tmux mutation: %v", err)
	}
}

func TestRunnerPinRejectsMissingAndFileWorkdirsBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		workdir func(*testing.T) string
	}{
		{name: "missing", workdir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
		{name: "file", workdir: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			bin := t.TempDir()
			mutated := filepath.Join(bin, "mutated")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\ntouch '"+mutated+"'\nexit 2\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "pin", "--workspace", "alpha", "--workdir", tc.workdir(t))
			if err == nil || result.ExitCode(err) != result.ExitRejected {
				t.Fatalf("pin error = %v, exit=%d", err, result.ExitCode(err))
			}
			if _, statErr := os.Stat(filepath.Join(dir, config.RunnersFile)); !os.IsNotExist(statErr) {
				t.Fatalf("failed preflight wrote runner config: %v", statErr)
			}
			if _, statErr := os.Stat(mutated); !os.IsNotExist(statErr) {
				t.Fatalf("failed preflight mutated tmux: %v", statErr)
			}
		})
	}
}

func TestRunnerPinCurrentDerivesWorkspaceAndWorkdirFromInvokingPane(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = display-message ]; then
  case "$5" in
    '#{pane_current_path}') echo '`+workdir+`' ;;
    '#{session_name}') echo alpha ;;
  esac
  exit 0
fi
if [ "$1" = has-session ]; then exit 1; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX_PANE", "%16")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "pin", "--current")
	if len(got.Successful) != 1 || got.Successful[0].Resource.Workdir != workdir {
		t.Fatalf("pin current = %+v", got)
	}
	rows, err := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile))
	if err != nil || len(rows) != 1 || rows[0].Workspace != "alpha" {
		t.Fatalf("pin current rows = %+v err=%v", rows, err)
	}
}

func TestRunnerListIsDeterministicLocalOnly(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(t.TempDir(), "one"), filepath.Join(t.TempDir(), "two")
	writeRunnerRegistry(t, dir, "zeta\t"+two+"\nalpha\t"+one+"\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	for _, name := range []string{"amp", "tmux", "git"} {
		writeExecutable(t, filepath.Join(bin, name), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	}
	t.Setenv("PATH", bin)

	got := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "list")
	if len(got.Successful) != 2 || got.Successful[0].Resource.Workdir != one || got.Successful[1].Resource.Workdir != two {
		t.Fatalf("runner list = %+v", got)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("runner list called an external process: %v", err)
	}
}

func TestRunnerLaunchAndRestartRejectMissingAndFileWorkdirsBeforeTmuxMutation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		workdir func(*testing.T) string
	}{
		{name: "missing", workdir: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") }},
		{name: "file", workdir: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		for _, command := range []string{"launch", "restart"} {
			t.Run(command+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				valid := t.TempDir()
				writeRunnerRegistry(t, dir, "alpha\t"+valid+"\nbeta\t"+tc.workdir(t)+"\n")
				bin := t.TempDir()
				log := filepath.Join(bin, "tmux.log")
				writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+log+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

				err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", command, "--all")
				if err == nil || result.ExitCode(err) != result.ExitRejected {
					t.Fatalf("bulk %s error = %v, exit=%d", command, err, result.ExitCode(err))
				}
				data, _ := os.ReadFile(log)
				if strings.Contains(string(data), "new-session") || strings.Contains(string(data), "new-window") {
					t.Fatalf("bulk preflight mutated tmux:\n%s", data)
				}
			})
		}
	}
}

func TestRunnerNonGitDirectoryAcceptedAcrossRestartAndDoctor(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	registryBefore, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	runnerCacheDirBefore := runnerCacheDir
	runnerCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { runnerCacheDir = runnerCacheDirBefore })
	sum := sha256.Sum256([]byte(workdir))
	marker := filepath.Join(cache, "amp", "pids", fmt.Sprintf("runner-%x.pid", sum[:8]))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	markerBefore := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(marker, markerBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(start)+` ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	restart := executeRunnerJSON(t, "--json", "--dry-run", "--config-dir", dir, "runner", "restart", "--workdir", workdir)
	if len(restart.Planned) != 1 || len(restart.Failed) != 0 {
		t.Fatalf("non-Git directory restart = %+v", restart)
	}
	doctor := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "doctor", "--workdir", workdir)
	if len(doctor.Successful) != 1 || len(doctor.Failed) != 0 || !strings.Contains(doctor.Successful[0].Message, "workdir=directory") {
		t.Fatalf("non-Git directory doctor = %+v", doctor)
	}
	registryAfter, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
	if err != nil || !bytes.Equal(registryAfter, registryBefore) {
		t.Fatalf("primary worktree lifecycle changed migrated runner row: before=%q after=%q err=%v", registryBefore, registryAfter, err)
	}
	markerAfter, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(markerAfter, markerBefore) {
		t.Fatalf("primary worktree lifecycle changed private Amp PID marker: before=%q after=%q err=%v", markerBefore, markerAfter, err)
	}
}

func TestRunnerRestartAcceptsUnlockedLinkedWorktree(t *testing.T) {
	dir := t.TempDir()
	repo := primaryTestWorktree(t)
	workdir := filepath.Join(t.TempDir(), "linked")
	runGit(t, repo, "worktree", "add", "-q", "--detach", workdir)
	window := config.RunnerWindow(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(workdir))+` ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeRunnerJSON(t, "--json", "--dry-run", "--config-dir", dir, "runner", "restart", "--workdir", workdir)
	if len(got.Planned) != 1 || len(got.Failed) != 0 {
		t.Fatalf("unlocked linked restart = %+v", got)
	}
}

func TestRunnerLaunchVerifiesExactCreatedPaneAndSkipsAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	bin := t.TempDir()
	state := filepath.Join(bin, "running")
	log := filepath.Join(bin, "tmux.log")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "$*" >> "`+log+`"
case "$1" in
  has-session) test -e "`+state+`" ;;
  new-session) touch "`+state+`"; printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes)
    if [ -e "`+state+`" ]; then printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(start)+`; fi ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	first := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if len(first.Successful) != 1 {
		t.Fatalf("launch = %+v", first)
	}
	second := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if len(second.Skipped) != 1 || second.Skipped[0].Message != "already running" {
		t.Fatalf("second launch = %+v", second)
	}
	data, _ := os.ReadFile(log)
	if strings.Count(string(data), "new-session") != 1 || !strings.Contains(string(data), "new-session -d -P -F #{session_name}") {
		t.Fatalf("launch did not capture exact creation identity:\n%s", data)
	}
}

func TestRunnerLaunchVerifiesRetainedShellWithExactAmpChild(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	bin := t.TempDir()
	state := filepath.Join(bin, "running")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) test -e "`+state+`" ;;
  new-session) touch "`+state+`"; printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes)
    if [ -e "`+state+`" ]; then printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(start)+`; fi ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(bin, "ps"), `#!/bin/sh
test "$*" = "-ax -o pid= -o ppid= -o comm=" || exit 2
printf ' 4242     1 /bin/zsh\n'
printf ' 5252  4242 /opt/amp/bin/amp\n'
`)
	oldProcessArgs := runnerProcessArgs
	runnerProcessArgs = func(pid int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	t.Cleanup(func() { runnerProcessArgs = oldProcessArgs })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	first := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if len(first.Successful) != 1 {
		t.Fatalf("retained-shell launch = %+v", first)
	}
	second := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if len(second.Skipped) != 1 || second.Skipped[0].Message != "already running" {
		t.Fatalf("second retained-shell launch = %+v", second)
	}
}

func TestRunnerLaunchAcceptsExactPaneAtEquivalentTmuxWorkdir(t *testing.T) {
	realWorkdir := t.TempDir()
	aliasParent := t.TempDir()
	workdir := filepath.Join(aliasParent, "primary")
	if err := os.Symlink(realWorkdir, workdir); err != nil {
		t.Fatal(err)
	}
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	tmuxStart := `"` + strings.Replace(start, "exit $status", `exit \$status`, 1) + `"`
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+realWorkdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(tmuxStart)+` ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	pane, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err != nil {
		t.Fatalf("launch through equivalent tmux workdir: %v", err)
	}
	if pane.PaneID != "%9" || pane.WindowID != "@7" {
		t.Fatalf("launch returned pane %+v", pane)
	}
}

func TestRunnerStartCommandMatcherAcceptsOnlyMeasuredTmuxEscaping(t *testing.T) {
	expected := runnerStartCommand("/tmp/primary")
	measured := `"` + strings.Replace(expected, "exit $status", `exit \$status`, 1) + `"`
	additionalEscape := `"` + strings.ReplaceAll(expected, "$", `\$`) + `"`

	if !runnerStartCommandMatches(measured, expected) {
		t.Fatalf("measured tmux command did not match: %q", measured)
	}
	if runnerStartCommandMatches(additionalEscape, expected) {
		t.Fatalf("command with unmeasured escaping matched: %q", additionalEscape)
	}
}

func TestRunnerLaunchReportsLastObservedIdentityRejection(t *testing.T) {
	workdir := t.TempDir()
	wrongWorkdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+wrongWorkdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(start)+` ;;
  capture-pane) exit 0 ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	child := tmux.ProcessMetadata{PID: 5252, ParentPID: 4242, Name: "amp", Identity: "native-start-123"}
	oldChildren, oldArgs := runnerChildProcesses, runnerProcessArgs
	runnerChildProcesses = func(int) ([]tmux.ProcessMetadata, error) { return []tmux.ProcessMetadata{child}, nil }
	runnerProcessArgs = func(int) ([]string, error) { return []string{"/opt/amp/bin/amp", "--no-tui"}, nil }
	t.Cleanup(func() { runnerChildProcesses, runnerProcessArgs = oldChildren, oldArgs })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil {
		t.Fatal("identity-rejected launch succeeded")
	}
	message := err.Error()
	for _, want := range []string{
		"last observed at +",
		"pane=%9 window=@7",
		"start-command-match=true",
		"workdir-equivalent=false",
		"retained-shell-pid=4242",
		"pid=5252 ppid=4242 name=\"amp\" incarnation=\"native-start-123\"",
		`argv=["/opt/amp/bin/amp" "--no-tui"]`,
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("identity rejection diagnostic %q does not contain %q", message, want)
		}
	}
}

func TestRunnerLaunchReportsCreatedPaneIdentityDrift(t *testing.T) {
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'other\tdrifted\t@8\t%%9\t`+workdir+`\tzsh\tignored\t0\t4242\t123\n' ;;
  capture-pane) exit 0 ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil {
		t.Fatal("identity-drifted launch succeeded")
	}
	for _, want := range []string{"expected session=\"alpha\"", "window-name=\"" + window + "\"", "window=@7 pane=%9", "observed session=\"other\"", "window-name=\"drifted\"", "window=@8 pane=%9"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("identity drift diagnostic %q does not contain %q", err, want)
		}
	}
}

func TestRunnerLaunchPreservesDetailedObservationBeforePaneLookupFailure(t *testing.T) {
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes)
    n=0; test -e "`+calls+`" && n=$(cat "`+calls+`"); n=$((n+1)); echo "$n" > "`+calls+`"
    if [ "$n" -eq 1 ]; then
      printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(start)+`
    else
      echo 'transient pane lookup failure' >&2
      exit 1
    fi ;;
  capture-pane) exit 0 ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	child := tmux.ProcessMetadata{PID: 5252, ParentPID: 4242, Name: "amp", Identity: "native-start-123"}
	oldChildren, oldArgs := runnerChildProcesses, runnerProcessArgs
	runnerChildProcesses = func(int) ([]tmux.ProcessMetadata, error) { return []tmux.ProcessMetadata{child}, nil }
	runnerProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	t.Cleanup(func() { runnerChildProcesses, runnerProcessArgs = oldChildren, oldArgs })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil {
		t.Fatal("launch with final pane lookup failure succeeded")
	}
	for _, want := range []string{
		"last detailed observation",
		"retained-shell-pid=4242",
		"pid=5252 ppid=4242 name=\"amp\" incarnation=\"native-start-123\"",
		`argv=["amp" "--no-tui"]`,
		"final observation",
		"exact pane %9 unavailable",
		"transient pane lookup failure",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("transient pane lookup diagnostic %q does not contain %q", err, want)
		}
	}
}

func TestRunnerLaunchKeepsIdentityAndProcessEvidenceWithLongValues(t *testing.T) {
	workdir := t.TempDir()
	wrongWorkdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	bin := t.TempDir()
	longStart := strings.Repeat("x", 1600)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+wrongWorkdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(longStart)+` ;;
  capture-pane) exit 0 ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	child := tmux.ProcessMetadata{PID: 5252, ParentPID: 4242, Name: "amp", Identity: "native-start-123"}
	oldChildren, oldArgs := runnerChildProcesses, runnerProcessArgs
	runnerChildProcesses = func(int) ([]tmux.ProcessMetadata, error) { return []tmux.ProcessMetadata{child}, nil }
	runnerProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	t.Cleanup(func() { runnerChildProcesses, runnerProcessArgs = oldChildren, oldArgs })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil {
		t.Fatal("long identity-rejected launch succeeded")
	}
	for _, want := range []string{"start-command-match=false", "workdir-equivalent=false", "retained-shell-pid=4242", "incarnation=\"native-start-123\"", `argv=["amp" "--no-tui"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("long-value diagnostic %q does not contain %q", err, want)
		}
	}
}

func TestRunnerPaneExactProcessRevalidatesChildSnapshot(t *testing.T) {
	oldChildren, oldArgs := runnerChildProcesses, runnerProcessArgs
	calls := 0
	runnerChildProcesses = func(parentPID int) ([]tmux.ProcessMetadata, error) {
		calls++
		identity := "start-one"
		if calls > 1 {
			identity = "start-two"
		}
		return []tmux.ProcessMetadata{{PID: 5252, ParentPID: parentPID, Name: "amp", Identity: identity}}, nil
	}
	runnerProcessArgs = func(pid int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	t.Cleanup(func() { runnerChildProcesses, runnerProcessArgs = oldChildren, oldArgs })

	exact, err := runnerPaneHasExactProcess(tmux.WindowPane{Command: "zsh", PID: 4242}, true)
	if err != nil || exact {
		t.Fatalf("changed child snapshot exact=%t err=%v", exact, err)
	}
}

func TestRunnerPaneRejectsChildNameWhitespaceChange(t *testing.T) {
	oldChildren, oldArgs := runnerChildProcesses, runnerProcessArgs
	calls := 0
	runnerChildProcesses = func(parentPID int) ([]tmux.ProcessMetadata, error) {
		calls++
		name := "amp"
		if calls > 1 {
			name = "amp "
		}
		return []tmux.ProcessMetadata{{PID: 5252, ParentPID: parentPID, Name: name, Identity: "start-one"}}, nil
	}
	runnerProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	t.Cleanup(func() { runnerChildProcesses, runnerProcessArgs = oldChildren, oldArgs })

	exact, err := runnerPaneHasExactProcess(tmux.WindowPane{Command: "zsh", PID: 4242}, true)
	if err != nil || exact {
		t.Fatalf("whitespace-changed child exact=%t err=%v", exact, err)
	}
}

func TestRunnerPaneLegacyDirectAmpRequiresExactArgs(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "exact", args: []string{"/opt/amp/bin/amp", "--no-tui"}, want: true},
		{name: "extra argument", args: []string{"amp", "--no-tui", "unexpected"}},
		{name: "wrong mode", args: []string{"amp", "threads"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			oldArgs := runnerProcessArgs
			runnerProcessArgs = func(pid int) ([]string, error) { return test.args, nil }
			t.Cleanup(func() { runnerProcessArgs = oldArgs })

			exact, err := runnerPaneHasExactProcess(tmux.WindowPane{Command: "amp", PID: 5252}, false)
			if err != nil || exact != test.want {
				t.Fatalf("direct amp args %#v exact=%t err=%v, want %t", test.args, exact, err, test.want)
			}
		})
	}
}

func TestRunnerPaneLegacyDirectAmpRejectsChangedProcessIdentity(t *testing.T) {
	oldArgs, oldIdentity := runnerProcessArgs, runnerProcessIdentity
	runnerProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui"}, nil }
	calls := 0
	runnerProcessIdentity = func(int) (string, error) {
		calls++
		return fmt.Sprintf("start-%d", calls), nil
	}
	t.Cleanup(func() { runnerProcessArgs, runnerProcessIdentity = oldArgs, oldIdentity })

	exact, err := runnerPaneHasExactProcess(tmux.WindowPane{Command: "amp", PID: 5252}, false)
	if err != nil || exact {
		t.Fatalf("legacy changed process identity exact=%t err=%v", exact, err)
	}
}

func TestRunnerLegacyPaneRevalidationRejectsChangedTmuxSnapshot(t *testing.T) {
	oldPaneByID := runnerPaneByID
	before := tmux.WindowPane{Session: "alpha", Window: "legacy", WindowID: "@1", PaneID: "%1", Path: "/tmp/project", Command: "amp", StartCommand: "exec amp --no-tui", PID: 5252, StartTime: 123}
	runnerPaneByID = func(string) (tmux.WindowPane, error) {
		after := before
		after.StartTime++
		return after, nil
	}
	t.Cleanup(func() { runnerPaneByID = oldPaneByID })

	unchanged, err := legacyRunnerPaneUnchanged(before)
	if err != nil || unchanged {
		t.Fatalf("changed legacy pane unchanged=%t err=%v", unchanged, err)
	}
}

func TestRunnerPaneCanonicalCurrentAmpVerifiesRetainedShellChild(t *testing.T) {
	oldChildren, oldArgs := runnerChildProcesses, runnerProcessArgs
	child := tmux.ProcessMetadata{PID: 5252, ParentPID: 4242, Name: "amp", Identity: "start-one"}
	runnerChildProcesses = func(parentPID int) ([]tmux.ProcessMetadata, error) {
		if parentPID != 4242 {
			t.Fatalf("enumerated children of pid %d, want retained shell 4242", parentPID)
		}
		return []tmux.ProcessMetadata{child}, nil
	}
	runnerProcessArgs = func(pid int) ([]string, error) {
		if pid != child.PID {
			t.Fatalf("inspected argv for pid %d, want amp child %d", pid, child.PID)
		}
		return []string{"amp", "--no-tui"}, nil
	}
	t.Cleanup(func() { runnerChildProcesses, runnerProcessArgs = oldChildren, oldArgs })

	exact, err := runnerPaneHasExactProcess(tmux.WindowPane{Command: "amp", PID: 4242}, true)
	if err != nil || !exact {
		t.Fatalf("canonical current amp exact=%t err=%v", exact, err)
	}
}

func TestRunnerLaunchRejectsCanonicalCurrentAmpWithExtraArgv(t *testing.T) {
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	bin := t.TempDir()
	killed := filepath.Join(bin, "killed")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tamp\t%s\t0\t5252\t123\n' `+shellSingleQuote(start)+` ;;
  capture-pane) echo 'direct amp had extra argv' ;;
  kill-window) touch "`+killed+`" ;;
  *) exit 2 ;;
esac
`)
	oldArgs := runnerProcessArgs
	runnerProcessArgs = func(int) ([]string, error) { return []string{"amp", "--no-tui", "unexpected"}, nil }
	t.Cleanup(func() { runnerProcessArgs = oldArgs })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil || !strings.Contains(err.Error(), "direct amp had extra argv") {
		t.Fatalf("direct amp with extra argv error = %v", err)
	}
	if _, statErr := os.Stat(killed); statErr != nil {
		t.Fatalf("rejected direct amp window was not removed: %v", statErr)
	}
}

func TestRunnerLaunchRejectsAmpDescendantOfUnrelatedShellChild(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	bin := t.TempDir()
	state := filepath.Join(bin, "running")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) test -e "`+state+`" ;;
  new-session) touch "`+state+`"; printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes)
    if [ -e "`+state+`" ]; then printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(start)+`; fi ;;
  capture-pane) echo 'unrelated descendant rejected' ;;
  kill-window) rm "`+state+`" ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(bin, "ps"), `#!/bin/sh
test "$*" = "-ax -o pid= -o ppid= -o comm=" || exit 2
printf ' 4242     1 /bin/zsh\n'
printf ' 5151  4242 /usr/bin/node\n'
printf ' 5252  5151 /opt/amp/bin/amp\n'
`)
	oldChildren := runnerChildProcesses
	runnerChildProcesses = func(parentPID int) ([]tmux.ProcessMetadata, error) {
		return []tmux.ProcessMetadata{{PID: 5151, ParentPID: parentPID, Name: "node", Identity: "start-node"}}, nil
	}
	t.Cleanup(func() { runnerChildProcesses = oldChildren })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if err == nil || !strings.Contains(err.Error(), "unrelated descendant rejected") {
		t.Fatalf("unrelated descendant launch error = %v", err)
	}
	if _, statErr := os.Stat(state); !os.IsNotExist(statErr) {
		t.Fatalf("rejected runner window remained: %v", statErr)
	}
}

func TestRunnerLaunchRetainedShellKeepsProcessInspectionFailureDistinct(t *testing.T) {
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(start)+` ;;
  capture-pane) echo 'pane remains alive' ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(bin, "ps"), "#!/bin/sh\nprintf ' 5252 4242 /opt/amp/bin/amp\\n'\n")
	oldProcessArgs := runnerProcessArgs
	calls := 0
	runnerProcessArgs = func(pid int) ([]string, error) {
		calls++
		if calls == 1 {
			return []string{"amp", "--no-tui"}, nil
		}
		return nil, errors.New("process argv inspection unavailable")
	}
	t.Cleanup(func() { runnerProcessArgs = oldProcessArgs })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 2*time.Second, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil || !strings.Contains(err.Error(), "process argv inspection unavailable") || strings.Contains(err.Error(), "runner exited during startup") {
		t.Fatalf("process inspection failure = %v", err)
	}
	if len(err.Error()) > 5000 {
		t.Fatalf("process inspection diagnostic is unbounded: %d", len(err.Error()))
	}
}

func TestRunnerLaunchBoundsCompleteDiagnosticIncludingStalePIDText(t *testing.T) {
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@7\t%%9\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@7\t%%9\t`+workdir+`\tzsh\t%s\t0\t4242\t123\n' `+shellSingleQuote(start)+` ;;
  capture-pane) echo 'bounded pane failure' ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	oldChildren, oldCache := runnerChildProcesses, runnerCacheDir
	runnerChildProcesses = func(int) ([]tmux.ProcessMetadata, error) { return nil, nil }
	runnerCacheDir = func() (string, error) { return "/" + strings.Repeat("very-long-cache-path/", 1000), nil }
	t.Cleanup(func() { runnerChildProcesses, runnerCacheDir = oldChildren, oldCache })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	_, err := launchRunner(config.RunnerRow{Workspace: "alpha", Window: window, Workdir: workdir})
	if err == nil || !strings.Contains(err.Error(), "Amp-owned PID marker") {
		t.Fatalf("complete startup diagnostic = %v", err)
	}
	if len(err.Error()) > runnerStartupErrorLimit {
		t.Fatalf("complete startup diagnostic len=%d, limit=%d", len(err.Error()), runnerStartupErrorLimit)
	}
}

func TestRunnerLaunchEarlyExitReportsBoundedPaneAndStalePIDDiagnostics(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	cache := t.TempDir()
	pids := filepath.Join(cache, "amp", "pids")
	oldCacheDir := runnerCacheDir
	runnerCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { runnerCacheDir = oldCacheDir })
	if err := os.MkdirAll(pids, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(workdir))
	marker := filepath.Join(pids, fmt.Sprintf("runner-%x.pid", sum[:8]))
	if err := os.WriteFile(marker, []byte("99999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@8\t%%10\n' ;;
  list-panes) printf 'alpha\t`+window+`\t@8\t%%10\t`+workdir+`\tsleep\tignored\t0\n' ;;
  capture-pane) python3 -c 'print("fatal stale runner " + "x" * 6000)' ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", cache)
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 20*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("early exit error = %v", err)
	}
	var stdout bytes.Buffer
	err = (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir})
	if err == nil {
		t.Fatal("second early exit succeeded")
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	message := env.Failed[0].Error.Message
	if !strings.Contains(message, "fatal stale runner") || !strings.Contains(message, "Amp-owned PID marker") || !strings.Contains(message, "stale pid") || !strings.Contains(message, "left unchanged") || len(message) > 5000 {
		t.Fatalf("early exit diagnostic len=%d: %s", len(message), message)
	}
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("diagnosis deleted Amp-owned marker: %v", statErr)
	}
}

func TestStaleAmpPIDDiagnosticReportsLiveAmbiguousOwnershipWithoutDeletion(t *testing.T) {
	workdir := filepath.Join(t.TempDir(), "project")
	cache := t.TempDir()
	oldCacheDir := runnerCacheDir
	runnerCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { runnerCacheDir = oldCacheDir })
	sum := sha256.Sum256([]byte(workdir))
	marker := filepath.Join(cache, "amp", "pids", fmt.Sprintf("runner-%x.pid", sum[:8]))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldProbe := runnerProcessAlive
	runnerProcessAlive = func(pid int) bool { return pid == 12345 }
	t.Cleanup(func() { runnerProcessAlive = oldProbe })

	got := staleAmpPIDDiagnostic(workdir)
	if !strings.Contains(got, "live but ownership is ambiguous") || !strings.Contains(got, "left unchanged") {
		t.Fatalf("live PID diagnostic = %q", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("diagnostic deleted marker: %v", err)
	}
}

func TestRunnerLaunchRejectsAmpChildThatDoesNotSurviveVerificationWindow(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	window := config.RunnerWindow(workdir)
	start := runnerStartCommand(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 1 ;;
  new-session) printf 'alpha\t`+window+`\t@8\t%%10\n' ;;
  list-panes)
    n=0; test -e "`+calls+`" && n=$(cat "`+calls+`"); n=$((n+1)); echo "$n" > "`+calls+`"
    if [ "$n" -le 1 ]; then command=amp; else command=sleep; fi
    printf 'alpha\t`+window+`\t@8\t%%10\t`+workdir+`\t%s\t%s\t0\n' "$command" `+shellSingleQuote(start)+` ;;
  capture-pane) echo 'runner exited after briefly starting' ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	oldChildren := runnerChildProcesses
	childCalls := 0
	runnerChildProcesses = func(parentPID int) ([]tmux.ProcessMetadata, error) {
		childCalls++
		if childCalls > 2 {
			return nil, nil
		}
		return []tmux.ProcessMetadata{{PID: 5252, ParentPID: parentPID, Name: "amp", Identity: "start-one"}}, nil
	}
	t.Cleanup(func() { runnerChildProcesses = oldChildren })
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 2*time.Second, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("transient runner error = %v", err)
	}
}

func TestRunnerLegacyWindowIsManagedAndRestartMigratesRuntimeName(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	derived := config.RunnerWindow(workdir)
	writeRunnerRegistry(t, dir, "alpha\tlegacy-runner\t"+workdir+"\n")
	bin := t.TempDir()
	state := filepath.Join(bin, "state")
	if err := os.WriteFile(state, []byte("legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyStart := tmux.RunnerCommand(workdir)
	newStart := runnerStartCommand(workdir)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
mode=$(cat "`+state+`" 2>/dev/null)
case "$1" in
  has-session) exit 0 ;;
  list-panes)
    if [ "$mode" = legacy ]; then printf 'alpha\tlegacy-runner\t@1\t%%1\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(legacyStart)+`
    elif [ "$mode" = canonical ]; then printf 'alpha\t`+derived+`\t@2\t%%2\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(newStart)+`; fi ;;
  kill-window) echo absent > "`+state+`" ;;
  new-window) echo canonical > "`+state+`"; printf 'alpha\t`+derived+`\t@2\t%%2\n' ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 15*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	launch := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if len(launch.Skipped) != 1 || launch.Skipped[0].Message != "already running" {
		t.Fatalf("legacy launch = %+v", launch)
	}
	restarted := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "restart", "--workdir", workdir)
	if len(restarted.Successful) != 1 {
		t.Fatalf("legacy restart = %+v", restarted)
	}
	after := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if len(after.Skipped) != 1 || after.Skipped[0].Message != "already running" {
		t.Fatalf("launch after legacy restart = %+v", after)
	}
	parked := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "park", "--workdir", workdir)
	if len(parked.Successful) != 1 {
		t.Fatalf("park after legacy restart = %+v", parked)
	}
}

func TestProcessSignalMayBeAliveFailsClosedOnPermissionAndUnknownErrors(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{nil, true},
		{syscall.EPERM, true},
		{errors.New("unknown probe failure"), true},
		{os.ErrProcessDone, false},
		{syscall.ESRCH, false},
	} {
		if got := processSignalMayBeAlive(test.err); got != test.want {
			t.Fatalf("processSignalMayBeAlive(%v) = %t, want %t", test.err, got, test.want)
		}
	}
}

func TestRunnerLegacyRestartRejectsOccupiedCanonicalWindowBeforeStopping(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	derived := config.RunnerWindow(workdir)
	writeRunnerRegistry(t, dir, "alpha\tlegacy-runner\t"+workdir+"\n")
	bin := t.TempDir()
	log := filepath.Join(bin, "tmux.log")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "$*" >> "`+log+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then
  printf 'alpha\tlegacy-runner\t@1\t%%1\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(tmux.RunnerCommand(workdir))+`
  printf 'alpha\t`+derived+`\t@2\t%%2\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(workdir))+`
  exit 0
fi
if [ "$1" = kill-window ]; then exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "restart", "--workdir", workdir)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("occupied canonical restart error = %v", err)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "kill-window") {
		t.Fatalf("restart stopped legacy runner before collision rejection:\n%s", data)
	}
}

func TestRunnerReconcileFailsClosedWithLiveRuntimeAndRepeatsAsSkip(t *testing.T) {
	dir := t.TempDir()
	workdir := filepath.Join(t.TempDir(), "missing")
	window := config.RunnerWindow(workdir)
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf 'alpha\t`+window+`\t@1\t%%1\t`+workdir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(workdir))+`; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "reconcile", "--workdir", workdir)
	if err == nil || !strings.Contains(err.Error(), "runtime ownership") {
		t.Fatalf("live reconcile error = %v", err)
	}
	if rows, loadErr := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile)); loadErr != nil || len(rows) != 1 {
		t.Fatalf("live reconcile removed config: %+v err=%v", rows, loadErr)
	}
}

func TestRunnerReconcileRejectsInvalidWorkdirsBeforeConfigOrTmuxMutation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		invalid func(*testing.T) string
	}{
		{name: "file", invalid: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "stat-error", invalid: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "loop")
			if err := os.Symlink(filepath.Base(path), path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		for _, bulk := range []bool{false, true} {
			name := "single"
			if bulk {
				name = "bulk"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				invalid := tc.invalid(t)
				rows := "beta\t" + invalid + "\n"
				if bulk {
					rows = "alpha\t" + filepath.Join(t.TempDir(), "missing") + "\n" + rows
				}
				writeRunnerRegistry(t, dir, rows)
				registryPath := filepath.Join(dir, config.RunnersFile)
				before, err := os.ReadFile(registryPath)
				if err != nil {
					t.Fatal(err)
				}
				bin := t.TempDir()
				log := filepath.Join(bin, "tmux.log")
				writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+log+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

				args := []string{"--json", "--config-dir", dir, "runner", "reconcile", "--workdir", invalid}
				if bulk {
					args = []string{"--json", "--config-dir", dir, "runner", "reconcile", "--all"}
				}
				err = executeRunnerJSONError(t, args...)
				if err == nil || result.ExitCode(err) != result.ExitRejected {
					t.Fatalf("reconcile error = %v, exit=%d", err, result.ExitCode(err))
				}
				after, readErr := os.ReadFile(registryPath)
				if readErr != nil || !bytes.Equal(after, before) {
					t.Fatalf("reconcile changed registry: before=%q after=%q err=%v", before, after, readErr)
				}
				data, _ := os.ReadFile(log)
				if strings.Contains(string(data), "new-session") || strings.Contains(string(data), "new-window") || strings.Contains(string(data), "kill-window") {
					t.Fatalf("reconcile mutated tmux:\n%s", data)
				}
			})
		}
	}
}

func TestRunnerParkRemoveReconcileAndDryRunConverge(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	writeRunnerRegistry(t, dir, "alpha\t"+missing+"\n")
	registryPath := filepath.Join(dir, config.RunnersFile)
	registryBefore, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	oldCacheDir := runnerCacheDir
	runnerCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { runnerCacheDir = oldCacheDir })
	sum := sha256.Sum256([]byte(missing))
	marker := filepath.Join(cache, "amp", "pids", fmt.Sprintf("runner-%x.pid", sum[:8]))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldProbe := runnerProcessAlive
	runnerProcessAlive = func(pid int) bool { return pid == 12345 }
	t.Cleanup(func() { runnerProcessAlive = oldProbe })
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	reconcileErr := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "reconcile", "--workdir", missing)
	if reconcileErr == nil || result.ExitCode(reconcileErr) != result.ExitRejected || !strings.Contains(reconcileErr.Error(), "ownership is ambiguous") {
		t.Fatalf("ambiguous reconcile error = %v, exit=%d", reconcileErr, result.ExitCode(reconcileErr))
	}
	registryAfter, err := os.ReadFile(registryPath)
	if err != nil || !bytes.Equal(registryAfter, registryBefore) {
		t.Fatalf("ambiguous reconcile changed registry: before=%q after=%q err=%v", registryBefore, registryAfter, err)
	}
	runnerProcessAlive = func(int) bool { return false }
	reconciled := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "reconcile", "--workdir", missing)
	if len(reconciled.Successful) != 1 || !strings.Contains(reconciled.Successful[0].Message, "stale pid") {
		t.Fatalf("reconcile = %+v", reconciled)
	}
	repeated := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "reconcile", "--workdir", missing)
	if len(repeated.Skipped) != 1 || !strings.Contains(repeated.Skipped[0].Message, "stale pid") {
		t.Fatalf("repeated reconcile = %+v", repeated)
	}
	removed := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "remove", "--workdir", missing)
	if len(removed.Skipped) != 1 || removed.Skipped[0].Message != "already in desired state" {
		t.Fatalf("idempotent remove = %+v", removed)
	}
}

func TestRunnerBulkReconcileRejectsAmbiguousPIDOwnershipBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first-missing")
	ambiguous := filepath.Join(t.TempDir(), "second-missing")
	writeRunnerRegistry(t, dir, "alpha\t"+first+"\nbeta\t"+ambiguous+"\n")
	registryPath := filepath.Join(dir, config.RunnersFile)
	before, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	oldCacheDir := runnerCacheDir
	runnerCacheDir = func() (string, error) { return cache, nil }
	t.Cleanup(func() { runnerCacheDir = oldCacheDir })
	sum := sha256.Sum256([]byte(ambiguous))
	marker := filepath.Join(cache, "amp", "pids", fmt.Sprintf("runner-%x.pid", sum[:8]))
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldProbe := runnerProcessAlive
	runnerProcessAlive = func(pid int) bool { return pid == 12345 }
	t.Cleanup(func() { runnerProcessAlive = oldProbe })
	bin := t.TempDir()
	log := filepath.Join(bin, "tmux.log")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+log+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	reconcileErr := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "reconcile", "--all")
	if reconcileErr == nil || result.ExitCode(reconcileErr) != result.ExitRejected || !strings.Contains(reconcileErr.Error(), "ownership is ambiguous") {
		t.Fatalf("bulk reconcile error = %v, exit=%d", reconcileErr, result.ExitCode(reconcileErr))
	}
	after, readErr := os.ReadFile(registryPath)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("bulk reconcile changed registry: before=%q after=%q err=%v", before, after, readErr)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "new-session") || strings.Contains(string(data), "new-window") || strings.Contains(string(data), "kill-window") {
		t.Fatalf("bulk reconcile mutated tmux:\n%s", data)
	}
}

func executeRunnerJSON(t *testing.T, args ...string) result.Envelope {
	t.Helper()
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute(args); err != nil {
		t.Fatalf("execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	var envelope result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&envelope); err != nil {
		t.Fatalf("decode execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	return envelope
}

func executeRunnerJSONError(t *testing.T, args ...string) error {
	t.Helper()
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(args)
	if err != nil {
		var envelope result.Envelope
		if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
			t.Fatalf("decode failed execute(%q): %v\nstdout: %s", args, decodeErr, stdout.String())
		}
	}
	return err
}

func writeRunnerRegistry(t *testing.T, dir, rows string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, config.RunnersFile), []byte("# amux-schema: runners/v1\n"+rows), 0o600); err != nil {
		t.Fatal(err)
	}
}

func primaryTestWorktree(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "README")
	runGit(t, repo, "commit", "-qm", "initial")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}
