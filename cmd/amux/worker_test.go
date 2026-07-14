package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func TestWorkerSpawnDryRunDoesNotResumeStartedOperation(t *testing.T) {
	for _, thread := range []string{"", "T-bound"} {
		t.Run(map[bool]string{true: "unbound", false: "bound"}[thread == ""], func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
			sum := sha256.Sum256([]byte(request))
			now := time.Now().UTC()
			record := config.OperationRecord{Key: "dry-spawn", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseCreatingThread, Resource: config.OperationResource{Kind: "worker", Thread: thread}, CreatedAt: now, UpdatedAt: now}
			if thread != "" {
				record.Phase = config.OperationPhaseThreadBound
			}
			path := filepath.Join(dir, config.OperationsFile)
			if _, err := config.StoreOperation(path, record); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "dry-spawn")
			if len(got.Planned) != 1 {
				t.Fatalf("dry-run result = %+v", got)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("operations changed: err=%v\nbefore=%s\nafter=%s", err, before, after)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("dry-run called amp or tmux: %v", err)
			}
		})
	}
}

func TestWorkerSpawnDeliveryReplayNeverResubmitsOrSearchesOtherThreads(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "delivery-started",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationStarted,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-bound"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-bound"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-bound"}, row)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-bound" ]; then printf '%s\n' '{"id":"T-bound","messages":[]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf '%s\t@1\t%s\n' worker `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "delivery-started")
	if err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("delivery replay error = %v", err)
	}
	updated, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "delivery-started")
	if loadErr != nil || !found || updated.State != config.OperationIndeterminate {
		t.Fatalf("delivery operation = %+v found=%t err=%v", updated, found, loadErr)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(log), "send-keys") || strings.Contains(string(log), "threads search") {
		t.Fatalf("delivery replay resubmitted or searched alternate threads:\n%s", log)
	}
}

func TestWorkerListIsDeterministicLocalJSONAndFiltersShelfIntent(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir,
		"zeta\tz\t/tmp/z\tT-z\n"+
			"alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, "shelves.tsv"), []byte("# amux-schema: shelves/v1\nT-z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch "+called+"\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch "+called+"\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "list", "--shelf", "unshelved"})
	if err != nil {
		t.Fatal(err)
	}
	var got result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Successful) != 1 || got.Successful[0].Resource.Thread != "T-a" || got.Successful[0].Message != "unshelved" {
		t.Fatalf("worker list result = %+v", got)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("local worker list invoked amp or tmux: %v", err)
	}
}

func TestWorkerShelveWritesIntentBeforeArchiveAndRetriesRemoteRepair(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, "shelves.tsv"), []byte("# amux-schema: shelves/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "amp.log")
	attempt := filepath.Join(bin, "attempt")
	script := "#!/bin/sh\ngrep -q '^T-a$' '" + filepath.Join(dir, "shelves.tsv") + "' || exit 88\necho \"$*\" >> '" + log + "'\nif [ ! -e '" + attempt + "' ]; then touch '" + attempt + "'; exit 42; fi\n"
	writeExecutable(t, filepath.Join(bin, "amp"), script)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "shelve", "--thread", "T-a"}); err == nil {
		t.Fatal("first archive succeeded, want injected failure")
	}
	stdout.Reset()
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "shelve", "--thread", "T-a"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "threads archive T-a"); got != 2 {
		t.Fatalf("archive calls = %d, log=%q", got, data)
	}
}

func TestWorkerPinIsIdempotentAndDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "pin", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--thread", "T-a"}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || len(first.Skipped) != 0 {
		t.Fatalf("first pin = %+v", first)
	}
	second := executeWorkerJSON(t, args...)
	if len(second.Successful) != 0 || len(second.Skipped) != 1 || second.Skipped[0].Message != "already pinned" {
		t.Fatalf("second pin = %+v", second)
	}

	dryDir := t.TempDir()
	dryArgs := append([]string{"--dry-run"}, args...)
	for i, arg := range dryArgs {
		if arg == dir {
			dryArgs[i] = dryDir
		}
	}
	dry := executeWorkerJSON(t, dryArgs...)
	if len(dry.Planned) != 1 {
		t.Fatalf("dry-run pin = %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(dryDir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run pin wrote workers registry: %v", err)
	}
}

func TestWorkerUnshelveRemovesIntentOnlyAfterRemoteSuccess(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	shelfPath := filepath.Join(dir, config.ShelvesFile)
	if err := os.WriteFile(shelfPath, []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	attempt := filepath.Join(bin, "attempt")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ngrep -q '^T-a$' '"+shelfPath+"' || exit 88\nif [ ! -e '"+attempt+"' ]; then touch '"+attempt+"'; exit 42; fi\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "unshelve", "--thread", "T-a"}

	if err := executeWorkerJSONError(t, args...); err == nil {
		t.Fatal("first unshelve succeeded, want injected remote failure")
	}
	if data, err := os.ReadFile(shelfPath); err != nil || !strings.Contains(string(data), "T-a\n") {
		t.Fatalf("failed unshelve removed intent: data=%q err=%v", data, err)
	}
	result := executeWorkerJSON(t, args...)
	if len(result.Successful) != 1 {
		t.Fatalf("retried unshelve = %+v", result)
	}
	if data, err := os.ReadFile(shelfPath); err != nil || strings.Contains(string(data), "T-a\n") {
		t.Fatalf("successful unshelve retained intent: data=%q err=%v", data, err)
	}
}

func TestWorkerSpawnPersistsOperationAndReplaysWithoutDuplicateCreation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	delivered := filepath.Join(bin, "delivered")
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-spawned"}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-spawned; exit 0; fi
if [ "$1 $2 $3" = "threads export T-spawned" ]; then
  if [ -e "`+delivered+`" ]; then printf '%s\n' '{"id":"T-spawned","messages":[{"role":"user","content":"hello"}]}'; else printf '%s\n' '{"id":"T-spawned","messages":[]}'; fi
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf '%s\t@1\t%s\n' worker `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "spawn-1"}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || first.Successful[0].Resource.Thread != "T-spawned" {
		t.Fatalf("spawn result = %+v", first)
	}
	record, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "spawn-1")
	if err != nil || !found || record.State != config.OperationSucceeded || record.Resource.Thread != "T-spawned" {
		t.Fatalf("spawn operation = %+v found=%t err=%v", record, found, err)
	}
	second := executeWorkerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Resource.Thread != "T-spawned" {
		t.Fatalf("spawn replay = %+v", second)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(log), "amp threads new"); got != 1 {
		t.Fatalf("spawn creation calls = %d\n%s", got, log)
	}

	mismatch := append([]string(nil), args...)
	for i, arg := range mismatch {
		if arg == "hello" {
			mismatch[i] = "different"
		}
	}
	if err := executeWorkerJSONError(t, mismatch...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("spawn key mismatch error = %v", err)
	}
}

func TestWorkerRemoveDoesNotArchiveAndTeardownRequiresLiveWorker(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "amp-called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	removed := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "remove", "--thread", "T-a")
	if len(removed.Successful) != 1 {
		t.Fatalf("remove result = %+v", removed)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("worker remove changed remote archive state: %v", err)
	}

	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
	if err == nil || !strings.Contains(err.Error(), "no live tmux window") {
		t.Fatalf("missing-window teardown error = %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("missing-window teardown archived before preflight: %v", err)
	}
}

func TestWorkerPinCurrentUsesCompleteInjectedIdentity(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "pin", "--current")
	if len(result.Successful) != 1 || result.Successful[0].Resource.Thread != "T-current" {
		t.Fatalf("pin current = %+v", result)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Workdir != workdir {
		t.Fatalf("current row = %+v err=%v", rows, err)
	}
}

func TestWorkerCurrentMatchesMigratedHomeRelativeWorkdirCanonically(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-current\n")
	t.Setenv("HOME", home)
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "list", "--current")
	if len(result.Successful) != 1 || result.Successful[0].Resource.Thread != "T-current" {
		t.Fatalf("current migrated worker = %+v", result)
	}
}

func TestWorkerDryRunKeepsKnownNoOpsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t/tmp/project\tT-current\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	launch := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "launch", "--thread", "T-current")
	if len(launch.Skipped) != 1 || len(launch.Planned) != 0 {
		t.Fatalf("dry-run shelved launch = %+v", launch)
	}

	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unshelve := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "unshelve", "--thread", "T-current")
	if len(unshelve.Skipped) != 1 || len(unshelve.Planned) != 0 {
		t.Fatalf("dry-run unshelved worker = %+v", unshelve)
	}
}

func TestWorkerRemovePlansAndReportsStaleShelfCleanup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "remove", "--thread", "T-stale"}

	dry := executeWorkerJSON(t, append([]string{"--dry-run"}, args...)...)
	if len(dry.Planned) != 1 || len(dry.Skipped) != 0 {
		t.Fatalf("dry-run stale shelf removal = %+v", dry)
	}
	actual := executeWorkerJSON(t, args...)
	if len(actual.Successful) != 1 || len(actual.Skipped) != 0 {
		t.Fatalf("stale shelf removal = %+v", actual)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("remaining shelves = %v err=%v", shelves, err)
	}
}

func TestBareAmuxLaunchesWorkersButExplicitAggregateLaunchStaysReserved(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMUX_CONFIG_DIR", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (app{}).execute(nil); err != nil {
		t.Fatalf("bare amux worker launch: %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("shelved bare launch invoked amp or tmux: %v", err)
	}
	if err := (app{}).execute([]string{"launch"}); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("explicit aggregate launch error = %v", err)
	}
}

func TestWorkerLaunchPreflightsEveryWorkdirBeforeTmuxMutation(t *testing.T) {
	dir := t.TempDir()
	valid := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	writeWorkerRegistry(t, dir, "alpha\tone\t"+valid+"\tT-one\nalpha\ttwo\t"+missing+"\tT-two\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "tmux-called")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "launch", "--all")
	if err == nil || !strings.Contains(err.Error(), "missing workdir") {
		t.Fatalf("bulk launch preflight error = %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("bulk launch mutated before complete workdir preflight: %v", err)
	}
}

func TestCanonicalWorkerCompletionsAreLeafSpecific(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			output := stdout.String()
			for _, want := range []string{"unpin", "spawn"} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing %q\n%s", shell, want, output)
				}
			}
			idempotencyFlag := "--idempotency-key"
			messageFileFlag := "--message-file"
			if shell == "fish" {
				idempotencyFlag = "-l 'idempotency-key'"
				messageFileFlag = "-l 'message-file'"
			}
			for _, want := range []string{idempotencyFlag, messageFileFlag} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing %q\n%s", shell, want, output)
				}
			}
			if shell == "bash" {
				if !strings.Contains(output, `unpin) COMPREPLY=( $(compgen -W "--thread --current"`) {
					t.Fatalf("bash unpin completion is not leaf-specific:\n%s", output)
				}
				if !strings.Contains(output, `if [[ "$word" == --config-dir || "$word" == -c ]]; then ((i++)); continue; fi`) {
					t.Fatalf("bash completion does not skip global config value:\n%s", output)
				}
			}
			if shell == "zsh" && !strings.Contains(output, `unpin) _arguments '--thread[thread id or URL]:thread:' '--current[current worker]'`) {
				t.Fatalf("zsh unpin completion is not leaf-specific:\n%s", output)
			}
			if shell == "zsh" && (!strings.Contains(output, `'-c[path to config directory]:directory:_directories'`) || !strings.Contains(output, `--config-dir|-c) (( i += 2 )); continue`)) {
				t.Fatalf("zsh completion does not resolve short global prefixes:\n%s", output)
			}
		})
	}
}

func executeWorkerJSON(t *testing.T, args ...string) result.Envelope {
	t.Helper()
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute(args); err != nil {
		t.Fatalf("execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	var envelope result.Envelope
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	return envelope
}

func executeWorkerJSONError(t *testing.T, args ...string) error {
	t.Helper()
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(args)
	if err == nil {
		return nil
	}
	var envelope result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode failed execute(%q): %v\nstdout: %s", args, decodeErr, stdout.String())
	}
	return err
}

func writeWorkerRegistry(t *testing.T, dir, rows string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "workers.tsv"), []byte("# amux-schema: workers/v1\n"+rows), 0o600); err != nil {
		t.Fatal(err)
	}
}
