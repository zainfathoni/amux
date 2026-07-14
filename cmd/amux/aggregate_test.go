package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func TestAggregateListUsesWorkerRunnerWorkspaceUnionAndCanonicalSelectors(t *testing.T) {
	dir := t.TempDir()
	workerDir := filepath.Join(t.TempDir(), "worker")
	runnerDir := filepath.Join(t.TempDir(), "runner")
	otherRunnerDir := filepath.Join(t.TempDir(), "other-runner")
	writeWorkerRegistry(t, dir,
		"mixed\tworker\t"+workerDir+"\tT-mixed\n"+
			"workers\tworker\t"+workerDir+"\tT-worker\n")
	writeRunnerRegistry(t, dir,
		"mixed\t"+runnerDir+"\n"+
			"runners\t"+otherRunnerDir+"\n")

	all := executeAggregateJSON(t, "--json", "--config-dir", dir, "list", "--all")
	if got := aggregateResourceKeys(all.Successful); strings.Join(got, ",") != "runner:"+runnerDir+",runner:"+otherRunnerDir+",worker:T-mixed,worker:T-worker" {
		t.Fatalf("aggregate all ordering = %v", got)
	}
	workspace := executeAggregateJSON(t, "--json", "--config-dir", dir, "list", "--workspace", "mixed")
	if got := aggregateResourceKeys(workspace.Successful); strings.Join(got, ",") != "runner:"+runnerDir+",worker:T-mixed" {
		t.Fatalf("aggregate workspace = %v", got)
	}
	worker := executeAggregateJSON(t, "--json", "--config-dir", dir, "list", "--thread", "T-worker")
	if got := aggregateResourceKeys(worker.Successful); strings.Join(got, ",") != "worker:T-worker" {
		t.Fatalf("aggregate worker identity = %v", got)
	}
	runner := executeAggregateJSON(t, "--json", "--config-dir", dir, "list", "--workdir", runnerDir)
	if got := aggregateResourceKeys(runner.Successful); strings.Join(got, ",") != "runner:"+runnerDir {
		t.Fatalf("aggregate runner identity = %v", got)
	}
}

func TestAggregateCurrentSelectsExactlyOneResourceMode(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t"+workdir+"\tT-current\n")
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	got := executeAggregateJSON(t, "--json", "--config-dir", dir, "list", "--current")
	if keys := aggregateResourceKeys(got.Successful); len(keys) != 1 || keys[0] != "worker:T-current" {
		t.Fatalf("aggregate current = %v", keys)
	}
}

func TestAggregateCurrentSelectsRunnerOutsideSpawnInjectedWorker(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t"+workdir+"\tT-worker\n")
	writeRunnerRegistry(t, dir, "alpha\t"+workdir+"\n")
	for _, name := range []string{"AMUX_WORKSPACE", "AMUX_SESSION", "AMUX_WINDOW", "AMUX_THREAD_ID", "AMUX_WORKDIR"} {
		t.Setenv(name, "")
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$5" = '#{pane_current_path}' ]; then echo '`+workdir+`'; exit 0; fi
exit 99
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX_PANE", "%16")

	got := executeAggregateJSON(t, "--json", "--config-dir", dir, "list", "--current")
	if keys := aggregateResourceKeys(got.Successful); len(keys) != 1 || keys[0] != "runner:"+workdir {
		t.Fatalf("aggregate runner current = %v", keys)
	}
}

func TestAggregateLaunchJointlyPreflightsBeforeEitherModeMutates(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	missingWorker := filepath.Join(t.TempDir(), "missing")
	writeRunnerRegistry(t, dir, "alpha\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, "alpha\tworker\t"+missingWorker+"\tT-worker\n")
	bin := t.TempDir()
	log := filepath.Join(bin, "tmux.log")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+log+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeAggregateJSONError(t, "--json", "--config-dir", dir, "launch", "--workspace", "alpha")
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "missing workdir") {
		t.Fatalf("aggregate preflight error = %v exit=%d", err, result.ExitCode(err))
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "new-session") || strings.Contains(string(data), "new-window") {
		t.Fatalf("aggregate preflight partially mutated tmux:\n%s", data)
	}
}

func TestAggregateLaunchRunsRunnersFirstAndContinuesWorkersAfterRuntimeFailure(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	workerDir := t.TempDir()
	runnerWindow := config.RunnerWindow(runnerDir)
	workerRow := config.Row{Workspace: "workers", Window: "worker", Workdir: workerDir, Thread: "T-worker"}
	workerStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "workers", Session: "workers", Window: "worker", Thread: "T-worker"}, workerRow)
	writeRunnerRegistry(t, dir, "runners\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, workerRow.String()+"\n")
	bin := t.TempDir()
	log := filepath.Join(bin, "tmux.log")
	workerRunning := filepath.Join(bin, "worker-running")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "$*" >> "`+log+`"
case "$1" in
  has-session) if [ "$3" = =workers ] && [ -e "`+workerRunning+`" ]; then exit 0; fi; exit 1 ;;
  new-session)
    if echo "$*" | grep -q '`+runnerWindow+`'; then printf 'runners\t`+runnerWindow+`\t@1\t%%1\n'; exit 0; fi
    touch "`+workerRunning+`"; exit 0 ;;
  list-panes)
    if [ -e "`+workerRunning+`" ]; then printf 'worker\t@2\t%s\n' `+shellSingleQuote(workerStart)+`; fi ;;
  capture-pane) echo 'runner startup failed' ;;
  kill-window) exit 0 ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "launch", "--all"})
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("aggregate runtime error = %v exit=%d", err, result.ExitCode(err))
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(env.Failed) != 1 || env.Failed[0].Resource.Kind != "runner" || len(env.Successful) != 1 || env.Successful[0].Resource.Thread != "T-worker" {
		t.Fatalf("aggregate continuation result = %+v", env)
	}
	data, _ := os.ReadFile(log)
	runnerCreate := strings.Index(string(data), runnerWindow)
	workerCreate := strings.LastIndex(string(data), "new-session")
	if runnerCreate < 0 || workerCreate < 0 || runnerCreate > workerCreate {
		t.Fatalf("aggregate launch order is not runner then worker:\n%s", data)
	}
}

func TestAggregateLateWorkerPreflightAfterRunnerMutationIsRuntimeFailure(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	workerDir := t.TempDir()
	runnerWindow := config.RunnerWindow(runnerDir)
	writeRunnerRegistry(t, dir, "alpha\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, "beta\tworker\t"+workerDir+"\tT-worker\n")
	bin := t.TempDir()
	running := filepath.Join(bin, "running")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) test -e "`+running+`" ;;
  new-session)
    touch "`+running+`"
    rmdir "`+workerDir+`"
    printf 'alpha\t`+runnerWindow+`\t@1\t%%1\n' ;;
  list-panes)
    if [ -e "`+running+`" ]; then printf 'alpha\t`+runnerWindow+`\t@1\t%%1\t`+runnerDir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(runnerDir))+`; fi ;;
  *) exit 99 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldTimeout, oldPoll := runnerStartupTimeout, runnerPollInterval
	runnerStartupTimeout, runnerPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() { runnerStartupTimeout, runnerPollInterval = oldTimeout, oldPoll })

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "launch", "--all"})
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("late preflight error = %v exit=%d", err, result.ExitCode(err))
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(env.Successful) != 1 || env.Successful[0].Resource.Kind != "runner" || len(env.Failed) != 1 || env.Failed[0].Resource.Thread != "T-worker" || env.Failed[0].Error == nil || env.Failed[0].Error.Kind != result.ErrorRuntime {
		t.Fatalf("late preflight result = %+v", env)
	}
}

func TestAggregateLaunchAttachIsGatedOnCompleteSuccess(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	workerDir := t.TempDir()
	runnerWindow := config.RunnerWindow(runnerDir)
	workerRow := config.Row{Workspace: "alpha", Window: "worker", Workdir: workerDir, Thread: "T-worker"}
	workerStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-worker"}, workerRow)
	writeRunnerRegistry(t, dir, "alpha\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, workerRow.String()+"\n")
	bin := t.TempDir()
	log := filepath.Join(bin, "tmux.log")
	attachEntered := filepath.Join(bin, "attach-entered")
	releaseAttach := filepath.Join(bin, "release-attach")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "$*" >> "`+log+`"
case "$1" in
  has-session) exit 0 ;;
  list-panes)
	if echo "$*" | grep -q pane_current_path; then
	  printf 'alpha\t`+runnerWindow+`\t@1\t%%1\t`+runnerDir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(runnerDir))+`
	else
	  printf 'worker\t@2\t%s\n' `+shellSingleQuote(workerStart)+`
	fi ;;
  select-window) exit 0 ;;
  switch-client|attach)
	  touch "`+attachEntered+`"
	  while [ ! -e "`+releaseAttach+`" ]; do sleep 0.01; done
	  ;;
	*) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	errCh := make(chan error, 1)
	go func() {
		errCh <- (app{stdout: &bytes.Buffer{}}).execute([]string{"--attach", "--config-dir", dir, "launch", "--workspace", "alpha"})
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(attachEntered); err == nil {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(log)
			t.Fatalf("attach was not entered:\n%s", data)
		}
		time.Sleep(time.Millisecond)
	}
	held, lockErr := acquireMutationLock([]string{"aggregate-attach-test"})
	if lockErr != nil {
		_ = os.WriteFile(releaseAttach, nil, 0o600)
		t.Fatalf("attach retained the mutation lock: %v", lockErr)
	}
	held.Release()
	if err := os.WriteFile(releaseAttach, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		data, _ := os.ReadFile(log)
		t.Fatalf("%v\n%s", err, data)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "select-window -t alpha:1") || !strings.Contains(string(data), "switch-client -t alpha") && !strings.Contains(string(data), "attach -t alpha") {
		t.Fatalf("successful aggregate launch did not attach:\n%s", data)
	}
}

func TestAggregateSharedMutationsPlanEveryMixedWorkspaceResource(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	workerDir := t.TempDir()
	runnerWindow := config.RunnerWindow(runnerDir)
	workerRow := config.Row{Workspace: "alpha", Window: "worker", Workdir: workerDir, Thread: "T-worker"}
	workerStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-worker"}, workerRow)
	writeRunnerRegistry(t, dir, "alpha\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, workerRow.String()+"\n")
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-panes)
    if echo "$*" | grep -q pane_current_path; then
      printf 'alpha\t`+runnerWindow+`\t@1\t%%1\t`+runnerDir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(runnerDir))+`
    else
      printf 'worker\t@2\t%s\n' `+shellSingleQuote(workerStart)+`
    fi ;;
  *) exit 99 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	for _, command := range []string{"park", "restart", "remove"} {
		t.Run(command, func(t *testing.T) {
			got := executeAggregateJSON(t, "--json", "--dry-run", "--config-dir", dir, command, "--workspace", "alpha")
			if keys := aggregateResourceKeys(got.Planned); strings.Join(keys, ",") != "runner:"+runnerDir+",worker:T-worker" || len(got.Successful) != 0 {
				t.Fatalf("aggregate %s dry-run = %+v", command, got)
			}
		})
	}
}

func TestAggregateReconcilePlansWorkerAndMissingRunnerWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	workerDir := t.TempDir()
	missingRunner := filepath.Join(t.TempDir(), "missing")
	writeWorkerRegistry(t, dir, "alpha\tworker\t"+workerDir+"\tT-worker\n")
	writeRunnerRegistry(t, dir, "alpha\t"+missingRunner+"\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeAggregateJSON(t, "--json", "--dry-run", "--config-dir", dir, "reconcile", "--workspace", "alpha")
	if keys := aggregateResourceKeys(got.Planned); strings.Join(keys, ",") != "runner:"+missingRunner+",worker:T-worker" {
		t.Fatalf("aggregate reconcile dry-run = %+v", got)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("dry-run reconcile invoked mutation: %v", err)
	}
}

func TestAggregateDoctorDiagnosesBothModes(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	workerDir := t.TempDir()
	runnerWindow := config.RunnerWindow(runnerDir)
	workerRow := config.Row{Workspace: "alpha", Window: "worker", Workdir: workerDir, Thread: "T-worker"}
	workerStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-worker"}, workerRow)
	writeRunnerRegistry(t, dir, "alpha\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, workerRow.String()+"\n")
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\nprintf '%s\\n' '[{\"id\":\"T-worker\"}]'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-panes)
    if echo "$*" | grep -q pane_current_path; then
      printf 'alpha\t`+runnerWindow+`\t@1\t%%1\t`+runnerDir+`\tamp\t%s\t0\n' `+shellSingleQuote(runnerStartCommand(runnerDir))+`
    else
      printf 'worker\t@2\t%s\n' `+shellSingleQuote(workerStart)+`
    fi ;;
  *) exit 99 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := executeAggregateJSON(t, "--json", "--config-dir", dir, "doctor", "--workspace", "alpha")
	if keys := aggregateResourceKeys(got.Successful); strings.Join(keys, ",") != "runner:"+runnerDir+",worker:T-worker" {
		t.Fatalf("aggregate doctor = %+v", got)
	}
}

func TestAggregateReadOnlyLateWorkerRejectionRemainsPreflight(t *testing.T) {
	dir := t.TempDir()
	runnerDir := lockedTestWorktree(t)
	workerDir := t.TempDir()
	writeRunnerRegistry(t, dir, "alpha\t"+runnerDir+"\n")
	writeWorkerRegistry(t, dir, "beta\tworker\t"+workerDir+"\tT-worker\n")
	workersPath := filepath.Join(dir, config.WorkersFile)
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then rm -f '"+workersPath+"'; exit 1; fi\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "doctor", "--all"})
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("read-only late rejection = %v exit=%d", err, result.ExitCode(err))
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(env.Successful) != 1 || env.Successful[0].Resource.Kind != "runner" || len(env.Failed) != 1 || env.Failed[0].Error == nil || env.Failed[0].Error.Kind != result.ErrorPreflight {
		t.Fatalf("read-only late rejection result = %+v", env)
	}
}

func TestWorkspaceListAndWorkspacesAreExactReadOnlyUnionAliases(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "zeta\tworker\t/tmp/worker\tT-worker\n")
	writeRunnerRegistry(t, dir, "alpha\t/tmp/runner\nzeta\t/tmp/mixed\n")

	canonical := executeAggregateJSON(t, "--json", "--config-dir", dir, "workspace", "list")
	alias := executeAggregateJSON(t, "--json", "--config-dir", dir, "workspaces")
	if got := aggregateWorkspaceNames(canonical.Successful); strings.Join(got, ",") != "alpha,zeta" {
		t.Fatalf("workspace union = %v", got)
	}
	if got, want := aggregateWorkspaceNames(alias.Successful), aggregateWorkspaceNames(canonical.Successful); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("workspaces alias = %v, want %v", got, want)
	}
	workers := executeAggregateJSON(t, "--json", "--config-dir", dir, "workspace", "list", "--mode", "worker")
	if got := aggregateWorkspaceNames(workers.Successful); strings.Join(got, ",") != "zeta" {
		t.Fatalf("worker workspace filter = %v", got)
	}
	runners := executeAggregateJSON(t, "--json", "--config-dir", dir, "workspaces", "--mode", "runner")
	if got := aggregateWorkspaceNames(runners.Successful); strings.Join(got, ",") != "alpha,zeta" {
		t.Fatalf("runner workspace filter = %v", got)
	}
	for _, args := range [][]string{{"workspace", "list"}, {"workspaces"}} {
		dry := executeAggregateJSON(t, append([]string{"--json", "--dry-run", "--config-dir", dir}, args...)...)
		if !dry.DryRun {
			t.Fatalf("%v did not preserve dry_run metadata: %+v", args, dry)
		}
	}
}

func TestBareAmuxRemainsWorkerOnlyLaunchConvenience(t *testing.T) {
	parsed, err := parseInvocation(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(parsed.Path, " "); got != "worker launch" {
		t.Fatalf("bare amux path = %q", got)
	}
}

func executeAggregateJSON(t *testing.T, args ...string) result.Envelope {
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

func executeAggregateJSONError(t *testing.T, args ...string) error {
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

func aggregateResourceKeys(outcomes []result.Outcome) []string {
	keys := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		if outcome.Resource.Kind == "worker" {
			keys = append(keys, "worker:"+outcome.Resource.Thread)
		} else {
			keys = append(keys, "runner:"+outcome.Resource.Workdir)
		}
	}
	return keys
}

func aggregateWorkspaceNames(outcomes []result.Outcome) []string {
	names := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		names = append(names, outcome.Resource.Workspace)
	}
	return names
}
