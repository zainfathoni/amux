package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

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
