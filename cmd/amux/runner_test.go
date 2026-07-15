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

func TestRunnerPinRequiresLockedWorktreeAndIsCanonicalIdempotent(t *testing.T) {
	dir := t.TempDir()
	locked := lockedTestWorktree(t)
	unlockedRepo := primaryTestWorktree(t)
	unlocked := filepath.Join(t.TempDir(), "unlocked")
	runGit(t, unlockedRepo, "worktree", "add", "-q", "--detach", unlocked)
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\ntouch '"+called+"'\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "pin", "--workspace", "alpha", "--workdir", unlocked)
	if err == nil || !strings.Contains(err.Error(), "not locked") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("unlocked pin error = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, statErr := os.Stat(filepath.Join(dir, config.RunnersFile)); !os.IsNotExist(statErr) {
		t.Fatalf("failed preflight wrote runner config: %v", statErr)
	}

	args := []string{"--json", "--config-dir", dir, "runner", "pin", "--workspace", "alpha", "--workdir", filepath.Join(locked, ".")}
	first := executeRunnerJSON(t, args...)
	if len(first.Successful) != 1 || first.Successful[0].Resource.Workdir != locked {
		t.Fatalf("first pin = %+v", first)
	}
	second := executeRunnerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Message != "already pinned" {
		t.Fatalf("second pin = %+v", second)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
	if err != nil || !strings.Contains(string(data), "alpha\t"+locked+"\n") || strings.Contains(string(data), config.RunnerWindow(locked)+"\t") {
		t.Fatalf("runner registry = %q, err=%v", data, err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("runner pin used unexpected tmux mutation: %v", err)
	}
}

func TestRunnerPinCurrentDerivesWorkspaceAndWorkdirFromInvokingPane(t *testing.T) {
	dir := t.TempDir()
	workdir := lockedTestWorktree(t)
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

func TestRunnerLaunchPreflightsEveryLockedWorktreeBeforeTmuxMutation(t *testing.T) {
	dir := t.TempDir()
	locked := lockedTestWorktree(t)
	unlockedRepo := primaryTestWorktree(t)
	unlocked := filepath.Join(t.TempDir(), "unlocked")
	runGit(t, unlockedRepo, "worktree", "add", "-q", "--detach", unlocked)
	writeRunnerRegistry(t, dir, "alpha\t"+locked+"\nbeta\t"+unlocked+"\n")
	bin := t.TempDir()
	log := filepath.Join(bin, "tmux.log")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+log+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "launch", "--all")
	if err == nil || !strings.Contains(err.Error(), "not locked") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("bulk launch error = %v", err)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "new-session") || strings.Contains(string(data), "new-window") {
		t.Fatalf("bulk preflight mutated tmux:\n%s", data)
	}
}

func TestRunnerPrimaryWorktreeOwnershipAgreesAcrossRestartAndDoctor(t *testing.T) {
	dir := t.TempDir()
	workdir := primaryTestWorktree(t)
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
		t.Fatalf("primary worktree restart = %+v", restart)
	}
	doctor := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "doctor", "--workdir", workdir)
	if len(doctor.Successful) != 1 || len(doctor.Failed) != 0 || !strings.Contains(doctor.Successful[0].Message, "worktree=stable primary") {
		t.Fatalf("primary worktree doctor = %+v", doctor)
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

func TestRunnerRestartRejectsUnlockedLinkedWorktree(t *testing.T) {
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

	err := executeRunnerJSONError(t, "--json", "--dry-run", "--config-dir", dir, "runner", "restart", "--workdir", workdir)
	if err == nil || !strings.Contains(err.Error(), "not locked") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("unlocked linked restart error = %v, exit=%d", err, result.ExitCode(err))
	}
}

func TestRunnerLaunchVerifiesExactCreatedPaneAndSkipsAlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	workdir := lockedTestWorktree(t)
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

func TestRunnerLaunchEarlyExitReportsBoundedPaneAndStalePIDDiagnostics(t *testing.T) {
	dir := t.TempDir()
	workdir := lockedTestWorktree(t)
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

func TestRunnerLaunchRejectsAmpThatDoesNotSurviveVerificationWindow(t *testing.T) {
	dir := t.TempDir()
	workdir := lockedTestWorktree(t)
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
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 30*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "launch", "--workdir", workdir)
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("transient runner error = %v", err)
	}
}

func TestRunnerLegacyWindowIsManagedAndRestartMigratesRuntimeName(t *testing.T) {
	dir := t.TempDir()
	workdir := lockedTestWorktree(t)
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
	workdir := lockedTestWorktree(t)
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

func TestRunnerParkRemoveReconcileAndDryRunConverge(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(t.TempDir(), "gone")
	writeRunnerRegistry(t, dir, "alpha\t"+missing+"\n")
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

	dry := executeRunnerJSON(t, "--json", "--dry-run", "--config-dir", dir, "runner", "reconcile", "--workdir", missing)
	if len(dry.Planned) != 1 || !strings.Contains(dry.Planned[0].Message, "ownership is ambiguous") {
		t.Fatalf("dry reconcile = %+v", dry)
	}
	if rows, err := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile)); err != nil || len(rows) != 1 {
		t.Fatalf("dry reconcile mutated rows: %+v err=%v", rows, err)
	}
	reconciled := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "reconcile", "--workdir", missing)
	if len(reconciled.Successful) != 1 || !strings.Contains(reconciled.Successful[0].Message, "ownership is ambiguous") {
		t.Fatalf("reconcile = %+v", reconciled)
	}
	repeated := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "reconcile", "--workdir", missing)
	if len(repeated.Skipped) != 1 || !strings.Contains(repeated.Skipped[0].Message, "ownership is ambiguous") {
		t.Fatalf("repeated reconcile = %+v", repeated)
	}
	removed := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "remove", "--workdir", missing)
	if len(removed.Skipped) != 1 || removed.Skipped[0].Message != "already in desired state" {
		t.Fatalf("idempotent remove = %+v", removed)
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

func lockedTestWorktree(t *testing.T) string {
	t.Helper()
	repo := primaryTestWorktree(t)
	worktree := filepath.Join(t.TempDir(), "project")
	runGit(t, repo, "worktree", "add", "-q", "--detach", worktree)
	runGit(t, repo, "worktree", "lock", "--reason", "amux test", worktree)
	return worktree
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
