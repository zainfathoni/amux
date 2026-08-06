// Package tycho_report_bridge verifies the experimental amux-tycho adapter.
package tycho_report_bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	origin           = "T-01234567-89ab-cdef-0123-456789abcdef"
	producerNonce    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	coordinatorToken = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	abandonmentToken = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

func helperPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("tycho_report_bridge.py")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func binding(stateDir string) map[string]any {
	return map[string]any{
		"receipt_id": "receipt-323", "origin_thread": origin,
		"correlation_id": "correlation-323", "producer_nonce": producerNonce,
		"tycho_agent_key": "reviewer", "claude_session_id": "session-323",
		"run_id": "run-323", "task_message_id": "task-message-323",
		"workdir": stateDir, "task_reference": "github.com/zainfathoni/amux/issues/323",
		"task_digest": strings.Repeat("d", 64), "producer_role": "safety_reviewer",
		"authority": "report_only", "group_reference": "issue-323",
	}
}

func createRequest(stateDir string) map[string]any {
	return map[string]any{
		"binding": binding(stateDir), "event_id": "create-323",
		"coordinator_token": coordinatorToken, "abandonment_token": abandonmentToken,
	}
}

func createRequestWithTarget(stateDir string) map[string]any {
	request := createRequest(stateDir)
	request["binding"].(map[string]any)["notification_target"] = map[string]any{"pane_id": "%9", "pane_created": "123"}
	return request
}

func submitRequest(stateDir string) map[string]any {
	request := binding(stateDir)
	delete(request, "group_reference")
	request["event_id"] = "report-323"
	request["report"] = map[string]any{
		"status": "complete", "summary": "Bounded semantic review completed.",
		"findings": []any{"No authority widening observed."}, "blockers": []any{},
		"verification": []any{"Synthetic fixture."},
	}
	return request
}

func runBridgeEnv(t *testing.T, stateDir string, request any, env []string, args ...string) (map[string]any, string, error) {
	t.Helper()
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := []string{helperPath(t), "--state-dir", stateDir}
	switch args[0] {
	case "create", "abandon":
		commandArgs = append(commandArgs, "--custody-dir", stateDir+"-custody", "--abandonment-dir", stateDir+"-abandonment")
	case "consume", "acknowledge":
		commandArgs = append(commandArgs, "--custody-dir", stateDir+"-custody")
	}
	commandArgs = append(commandArgs, args...)
	command := exec.Command("python3", commandArgs...)
	command.Stdin = bytes.NewReader(input)
	command.Env = os.Environ()
	for _, replacement := range env {
		name := strings.SplitN(replacement, "=", 2)[0] + "="
		filtered := command.Env[:0]
		for _, current := range command.Env {
			if !strings.HasPrefix(current, name) {
				filtered = append(filtered, current)
			}
		}
		command.Env = append(filtered, replacement)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	var result map[string]any
	if stdout.Len() > 0 {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			t.Fatalf("decode stdout %q: %v", stdout.String(), decodeErr)
		}
	}
	return result, stderr.String(), err
}

func runBridge(t *testing.T, stateDir string, request any, args ...string) (map[string]any, string, error) {
	t.Helper()
	return runBridgeEnv(t, stateDir, request, nil, args...)
}

func requireOutcome(t *testing.T, stateDir, outcome string, request any, args ...string) map[string]any {
	t.Helper()
	result, stderr, err := runBridge(t, stateDir, request, args...)
	if err != nil || result["outcome"] != outcome {
		t.Fatalf("%v outcome = %#v, err = %v, stderr = %s", args, result, err, stderr)
	}
	return result
}

func show(t *testing.T, stateDir string) map[string]any {
	t.Helper()
	result, stderr, err := runBridge(t, stateDir, map[string]any{}, "show", "--receipt-id", "receipt-323")
	if err != nil {
		t.Fatalf("show: %v: %s", err, stderr)
	}
	return result
}

// readCapability returns the exact owner-private capability record the helper
// wrote. Tests must never recompute the bound custody-directory digest: darwin
// resolves the test temporary root to a different canonical prefix than the one
// the helper sees.
func readCapability(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func writeCapability(t *testing.T, path string, record map[string]any) {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func withField(record map[string]any, key string, value any) map[string]any {
	changed := map[string]any{}
	for name, current := range record {
		changed[name] = current
	}
	changed[key] = value
	return changed
}

// chmodDir restores the original mode so the sibling capability directory does
// not survive the test as an unwritable directory.
func chmodDir(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	original := info.Mode().Perm()
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, original) })
}

func TestImmutableProducerBindingFailsClosedBeforeMutation(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	want, _ := json.Marshal(show(t, stateDir))
	for _, field := range []string{
		"origin_thread", "correlation_id", "producer_nonce", "tycho_agent_key",
		"claude_session_id", "run_id", "task_message_id", "workdir", "task_reference",
		"task_digest", "producer_role", "authority",
	} {
		request := submitRequest(stateDir)
		if field == "origin_thread" {
			request[field] = "T-11111111-1111-1111-1111-111111111111"
		} else if field == "producer_nonce" || field == "task_digest" {
			request[field] = strings.Repeat("b", 64)
		} else if field == "workdir" {
			request[field] = filepath.Dir(stateDir)
		} else if field == "authority" {
			request[field] = "coordinator"
		} else {
			request[field] = "wrong"
		}
		_, stderr, err := runBridge(t, stateDir, request, "submit")
		if err == nil || !strings.Contains(stderr, "immutable") && field != "authority" {
			t.Errorf("wrong %s: err = %v, stderr = %q", field, err, stderr)
		}
		got, _ := json.Marshal(show(t, stateDir))
		if !bytes.Equal(got, want) {
			t.Fatalf("wrong %s mutated receipt", field)
		}
	}
}

func TestReportReplayConflictAndTransitions(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	requireOutcome(t, stateDir, "duplicate", createRequest(stateDir), "create")
	submit := submitRequest(stateDir)
	requireOutcome(t, stateDir, "recorded", submit, "submit")
	requireOutcome(t, stateDir, "duplicate", submit, "submit")
	metadata, _ := json.Marshal(show(t, stateDir))
	if bytes.Contains(metadata, []byte("Bounded semantic review completed.")) ||
		bytes.Contains(metadata, []byte("producer_nonce_hash")) ||
		bytes.Contains(metadata, []byte("coordinator_token_hash")) ||
		!bytes.Contains(metadata, []byte(`"report_present":true`)) {
		t.Fatalf("show bypassed consume delivery boundary: %s", metadata)
	}
	conflict := submitRequest(stateDir)
	conflict["report"].(map[string]any)["summary"] = "Conflicting replay."
	_, stderr, err := runBridge(t, stateDir, conflict, "submit")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflict: %v: %s", err, stderr)
	}

	ack := map[string]any{"receipt_id": "receipt-323", "event_id": "ack-323", "report_event_id": "report-323", "origin_thread": origin}
	_, stderr, err = runBridge(t, stateDir, ack, "acknowledge")
	if err == nil || !strings.Contains(stderr, "requires delivery") {
		t.Fatalf("early acknowledgement: %v: %s", err, stderr)
	}
	consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-323", "origin_thread": origin}
	result := requireOutcome(t, stateDir, "recorded", consume, "consume")
	if result["state"] != "delivered" || result["report"] == nil {
		t.Fatalf("consume result = %#v", result)
	}
	result = requireOutcome(t, stateDir, "duplicate", consume, "consume")
	if result["report"] == nil {
		t.Fatal("consume replay did not rematerialize the report")
	}
	requireOutcome(t, stateDir, "recorded", ack, "acknowledge")
	requireOutcome(t, stateDir, "duplicate", ack, "acknowledge")
}

func TestCoordinatorAuthorityIsSeparate(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	requireOutcome(t, stateDir, "recorded", submitRequest(stateDir), "submit")
	consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-323", "origin_thread": "T-11111111-1111-1111-1111-111111111111"}
	_, stderr, err := runBridge(t, stateDir, consume, "consume")
	if err == nil || !strings.Contains(stderr, "origin") {
		t.Fatalf("wrong origin consumed report: %v: %s", err, stderr)
	}
	consume["origin_thread"] = origin
	custodyPath := filepath.Join(stateDir+"-custody", "receipt-323.json")
	custody := map[string]any{"receipt_id": "receipt-323", "origin_thread": origin, "coordinator_token": producerNonce}
	raw, _ := json.Marshal(custody)
	if err := os.WriteFile(custodyPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runBridge(t, stateDir, consume, "consume")
	if err == nil || !strings.Contains(stderr, "coordinator token") {
		t.Fatalf("wrong custody token consumed report: %v: %s", err, stderr)
	}
}

func TestNotificationRouteIsCoordinatorBound(t *testing.T) {
	stateDir := t.TempDir()
	create := createRequestWithTarget(stateDir)
	requireOutcome(t, stateDir, "recorded", create, "create")
	changed := createRequestWithTarget(stateDir)
	changed["binding"].(map[string]any)["notification_target"] = map[string]any{"pane_id": "%8", "pane_created": "122"}
	_, stderr, err := runBridge(t, stateDir, changed, "create")
	if err == nil || !strings.Contains(stderr, "conflicts") {
		t.Fatalf("changed create target: %v: %s", err, stderr)
	}
	producerSelected := submitRequest(stateDir)
	producerSelected["notification"] = map[string]any{"pane_id": "%8", "pane_created": "122"}
	_, stderr, err = runBridge(t, stateDir, producerSelected, "submit")
	if err == nil || !strings.Contains(stderr, "unknown fields") {
		t.Fatalf("producer-selected target: %v: %s", err, stderr)
	}
	if show(t, stateDir)["state"] != "created" {
		t.Fatal("route mismatch mutated receipt")
	}
}

func TestConcurrentSubmitIsDuplicateSafe(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	const writers = 12
	start := make(chan struct{})
	results := make(chan string, writers)
	var wait sync.WaitGroup
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for attempt := 0; attempt < 100; attempt++ {
				result, stderr, err := runBridge(t, stateDir, submitRequest(stateDir), "submit")
				if err == nil {
					results <- result["outcome"].(string)
					return
				}
				if !strings.Contains(stderr, "lock is busy") {
					results <- "error:" + stderr
					return
				}
				time.Sleep(time.Millisecond)
			}
			results <- "error:lock remained busy"
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	recorded, duplicate := 0, 0
	for result := range results {
		switch result {
		case "recorded":
			recorded++
		case "duplicate":
			duplicate++
		default:
			t.Errorf("submit result = %s", result)
		}
	}
	if recorded != 1 || duplicate != writers-1 {
		t.Fatalf("recorded = %d, duplicate = %d", recorded, duplicate)
	}
}

func TestMalformedOversizedAndLockContentionReject(t *testing.T) {
	stateDir := t.TempDir()
	request := createRequest(stateDir)
	request["unexpected"] = true
	_, stderr, err := runBridge(t, stateDir, request, "create")
	if err == nil || !strings.Contains(stderr, "unknown fields") {
		t.Fatalf("unknown field: %v: %s", err, stderr)
	}

	command := exec.Command("python3", helperPath(t), "--state-dir", stateDir, "--custody-dir", stateDir+"-custody", "--abandonment-dir", stateDir+"-abandonment", "create")
	command.Stdin = strings.NewReader(`{"padding":"` + strings.Repeat("x", 70*1024) + `"}`)
	var oversizedErr bytes.Buffer
	command.Stderr = &oversizedErr
	if err := command.Run(); err == nil || !strings.Contains(oversizedErr.String(), "size limit") {
		t.Fatalf("oversized input: %v: %s", err, oversizedErr.String())
	}

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, "experimental.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runBridge(t, stateDir, createRequest(stateDir), "create")
	if err == nil || !strings.Contains(stderr, "lock is busy") {
		t.Fatalf("lock contention: %v: %s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "receipts.json")); !os.IsNotExist(err) {
		t.Fatalf("busy lock mutated store: %v", err)
	}
}

func writeFakeTmux(t *testing.T, mode, stateDir string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "tmux.log")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %q
case "$1" in
  display-message)
    case %q in
      stale) printf '%%%%9\t999\tbash\n' ;;
      failure) exit 1 ;;
      timeout) sleep 5 ;;
      *) printf '%%%%9\t123\tamp\n' ;;
    esac ;;
  send-keys)
	if [ %q = result-lock ] && [ "$4" = Enter ]; then
      python3 -c 'import fcntl,sys,time; f=open(sys.argv[1], "a"); fcntl.flock(f, fcntl.LOCK_EX); open(sys.argv[2], "w").close(); time.sleep(0.5)' %q %q &
      while [ ! -e %q ]; do sleep 0.01; done
    fi ;;
esac
`, log, mode, mode, filepath.Join(stateDir, "experimental.lock"), filepath.Join(dir, "locked"), filepath.Join(dir, "locked"))
	path := filepath.Join(dir, "tmux")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, log
}

func TestNotificationIsNeverDeliveryAndIsNotRetried(t *testing.T) {
	for _, mode := range []string{"success", "stale", "failure", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			stateDir := t.TempDir()
			requireOutcome(t, stateDir, "recorded", createRequestWithTarget(stateDir), "create")
			fakeDir, log := writeFakeTmux(t, mode, stateDir)
			request := submitRequest(stateDir)
			result, stderr, err := runBridgeEnv(t, stateDir, request, []string{"PATH=" + fakeDir + ":" + os.Getenv("PATH")}, "submit")
			if err != nil {
				t.Fatalf("submit: %v: %s", err, stderr)
			}
			want := map[string]string{"success": "succeeded", "stale": "failed", "failure": "failed", "timeout": "indeterminate"}[mode]
			if result["notification"] != want || result["state"] != "valid_report" {
				calls, _ := os.ReadFile(log)
				t.Fatalf("result = %#v, want notification %q and valid_report; tmux calls: %s", result, want, calls)
			}
			receipt := show(t, stateDir)
			if receipt["state"] != "valid_report" {
				t.Fatalf("notification changed delivery: %#v", receipt)
			}
			before, _ := os.ReadFile(log)
			requireOutcome(t, stateDir, "duplicate", request, "submit")
			after, _ := os.ReadFile(log)
			if !bytes.Equal(before, after) {
				t.Fatal("duplicate submission retried notification")
			}
		})
	}
}

func TestOwnerPrivateCrashDurableStore(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	requireOutcome(t, stateDir, "recorded", submitRequest(stateDir), "submit")
	for path, want := range map[string]os.FileMode{
		stateDir: 0o700, filepath.Join(stateDir, "receipts.json"): 0o600,
		filepath.Join(stateDir, "experimental.lock"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(stateDir, "receipts.json.tmp.*")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v, err = %v", matches, err)
	}
	if receipt := show(t, stateDir); receipt["state"] != "valid_report" {
		t.Fatalf("recovered state = %#v", receipt)
	}
}

func TestCreateReportsStateAndRestartRecoversCustody(t *testing.T) {
	stateDir := t.TempDir()
	result := requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	if result["state"] != "created" {
		t.Fatalf("create state = %#v", result)
	}
	custodyPath := filepath.Join(stateDir+"-custody", "receipt-323.json")
	info, err := os.Stat(custodyPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("custody file = %#v, err = %v", info, err)
	}
	receipts, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil || bytes.Contains(receipts, []byte(coordinatorToken)) ||
		bytes.Contains(receipts, []byte(abandonmentToken)) || bytes.Contains(receipts, []byte(producerNonce)) {
		t.Fatalf("receipt store exposed a plaintext capability: err = %v", err)
	}
	requireOutcome(t, stateDir, "recorded", submitRequest(stateDir), "submit")

	// Every helper call is a fresh process, proving restart-safe token recovery.
	consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-restart", "origin_thread": origin}
	requireOutcome(t, stateDir, "recorded", consume, "consume")
	if _, err := os.Stat(custodyPath); err != nil {
		t.Fatalf("custody removed before acknowledgement: %v", err)
	}
	ack := map[string]any{"receipt_id": "receipt-323", "event_id": "ack-restart", "report_event_id": "report-323", "origin_thread": origin}
	requireOutcome(t, stateDir, "recorded", ack, "acknowledge")
	if _, err := os.Stat(custodyPath); !os.IsNotExist(err) {
		t.Fatalf("custody retained after acknowledgement: %v", err)
	}
	requireOutcome(t, stateDir, "duplicate", ack, "acknowledge")
}

func TestLostTokenCreatedReceiptIsAppendOnlyAbandoned(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	abandon := map[string]any{
		"receipt_id": "receipt-323", "event_id": "abandon-323", "origin_thread": origin,
		"reason": "coordinator_token_lost",
	}
	_, stderr, err := runBridge(t, stateDir, abandon, "abandon")
	if err == nil || !strings.Contains(stderr, "recoverable coordinator custody") {
		t.Fatalf("recoverable receipt was abandoned: %v: %s", err, stderr)
	}
	custodyPath := filepath.Join(stateDir+"-custody", "receipt-323.json")
	if err := os.Remove(custodyPath); err != nil {
		t.Fatal(err)
	}
	// Rewrite only the token field of the record create wrote, so the
	// coordinator-custody identity binding stays exactly as bound.
	abandonmentPath := filepath.Join(stateDir+"-abandonment", "receipt-323.json")
	genuine := readCapability(t, abandonmentPath)
	writeCapability(t, abandonmentPath, withField(genuine, "abandonment_token", producerNonce))
	_, stderr, err = runBridge(t, stateDir, abandon, "abandon")
	if err == nil || !strings.Contains(stderr, "invalid abandonment capability") {
		t.Fatalf("wrong capability abandoned receipt: %v: %s", err, stderr)
	}
	writeCapability(t, abandonmentPath, genuine)
	result := requireOutcome(t, stateDir, "recorded", abandon, "abandon")
	if result["state"] != "abandoned" {
		t.Fatalf("abandon result = %#v", result)
	}
	receipt := show(t, stateDir)
	events := receipt["events"].([]any)
	if receipt["state"] != "abandoned" || len(events) != 2 || events[1].(map[string]any)["kind"] != "abandoned" {
		t.Fatalf("abandoned receipt = %#v", receipt)
	}
	requireOutcome(t, stateDir, "duplicate", abandon, "abandon")
	_, stderr, err = runBridge(t, stateDir, submitRequest(stateDir), "submit")
	if err == nil || !strings.Contains(stderr, "different valid report") {
		t.Fatalf("abandoned receipt accepted report: %v: %s", err, stderr)
	}
	_, stderr, err = runBridge(t, stateDir, createRequest(stateDir), "create")
	if err == nil || !strings.Contains(stderr, "conflicts") {
		t.Fatalf("abandoned identity rebound: %v: %s", err, stderr)
	}
}

// runDirs invokes the helper with exactly the given owner directories so a test
// can supply a custody directory other than the one bound at create.
func runDirs(t *testing.T, stateDir, custodyDir, abandonmentDir string, request any, args ...string) (map[string]any, string, error) {
	t.Helper()
	input, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := []string{helperPath(t), "--state-dir", stateDir}
	if custodyDir != "" {
		commandArgs = append(commandArgs, "--custody-dir", custodyDir)
	}
	if abandonmentDir != "" {
		commandArgs = append(commandArgs, "--abandonment-dir", abandonmentDir)
	}
	command := exec.Command("python3", append(commandArgs, args...)...)
	command.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	var result map[string]any
	if stdout.Len() > 0 {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			t.Fatalf("decode stdout %q: %v", stdout.String(), decodeErr)
		}
	}
	return result, stderr.String(), err
}

func abandonRequest() map[string]any {
	return map[string]any{
		"receipt_id": "receipt-323", "event_id": "abandon-323", "origin_thread": origin,
		"reason": "coordinator_token_lost",
	}
}

// A caller must not bypass the missing-custody gate by naming a different
// custody directory than the one bound into the abandonment capability.
func TestAbandonRequiresTheExactBoundCustodyDirectory(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	custodyDir := stateDir + "-custody"
	abandonmentDir := stateDir + "-abandonment"
	realCustody := filepath.Join(custodyDir, "receipt-323.json")

	empty := filepath.Join(t.TempDir(), "empty-custody")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(t.TempDir(), "never-created-custody")
	alias := filepath.Join(t.TempDir(), "alias-custody")
	if err := os.Symlink(empty, alias); err != nil {
		t.Fatal(err)
	}
	for name, custody := range map[string]string{"empty": empty, "absent": absent, "aliased": alias} {
		result, stderr, err := runDirs(t, stateDir, custody, abandonmentDir, abandonRequest(), "abandon")
		if err == nil || !strings.Contains(stderr, "bound to a different coordinator custody directory") {
			t.Fatalf("%s custody directory abandoned receipt: %#v: %v: %s", name, result, err, stderr)
		}
		if show(t, stateDir)["state"] != "created" {
			t.Fatalf("%s custody directory mutated the receipt", name)
		}
		if _, err := os.Stat(realCustody); err != nil {
			t.Fatalf("%s custody directory disturbed real custody: %v", name, err)
		}
	}

	// The bound directory still gates on real recoverable custody, and an
	// unresolvable entry there is treated as present rather than as loss.
	if _, stderr, err := runDirs(t, stateDir, custodyDir, abandonmentDir, abandonRequest(), "abandon"); err == nil ||
		!strings.Contains(stderr, "recoverable coordinator custody") {
		t.Fatalf("bound directory with live custody abandoned receipt: %v: %s", err, stderr)
	}
	if err := os.Remove(realCustody); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(custodyDir, "missing-target.json"), realCustody); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := runDirs(t, stateDir, custodyDir, abandonmentDir, abandonRequest(), "abandon"); err == nil ||
		!strings.Contains(stderr, "recoverable coordinator custody") {
		t.Fatalf("dangling custody entry abandoned receipt: %v: %s", err, stderr)
	}
	if err := os.Remove(realCustody); err != nil {
		t.Fatal(err)
	}

	// A custody directory replaced by a regular file, or made unsearchable, is
	// indeterminate rather than absent, and must not read as token loss.
	replaced := stateDir + "-custody-replaced"
	for _, indeterminate := range []struct {
		name    string
		prepare func()
	}{
		{"replaced_by_file", func() {
			if err := os.Rename(custodyDir, replaced); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(custodyDir, []byte("not a directory\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsearchable", func() {
			if err := os.Remove(custodyDir); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replaced, custodyDir); err != nil {
				t.Fatal(err)
			}
			chmodDir(t, custodyDir, 0o000)
		}},
	} {
		name := indeterminate.name
		indeterminate.prepare()
		_, stderr, err := runDirs(t, stateDir, custodyDir, abandonmentDir, abandonRequest(), "abandon")
		if err == nil || !strings.Contains(stderr, "presence cannot be determined") {
			t.Fatalf("%s custody directory abandoned receipt: %v: %s", name, err, stderr)
		}
		if strings.Contains(stderr, "Traceback") || strings.Contains(stderr, custodyDir) {
			t.Fatalf("%s custody directory leaked a traceback or owner path: %s", name, stderr)
		}
	}
	if err := os.Chmod(custodyDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if result := requireOutcome(t, stateDir, "recorded", abandonRequest(), "abandon"); result["state"] != "abandoned" ||
		result["capability_cleanup"] != "removed" {
		t.Fatalf("bound abandonment result = %#v", result)
	}
}

// A created-only receipt with no bound abandonment capability, the legacy field
// shape, must stay preserved rather than become retro-abandonable.
func TestLegacyCreatedOnlyReceiptWithoutBoundCapabilityCannotBeAbandoned(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	storePath := filepath.Join(stateDir, "receipts.json")
	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	var store struct {
		SchemaVersion int              `json:"schema_version"`
		Receipts      []map[string]any `json:"receipts"`
	}
	if err := json.Unmarshal(raw, &store); err != nil {
		t.Fatal(err)
	}
	delete(store.Receipts[0], "abandonment_token_hash")
	legacy, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storePath, append(legacy, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stateDir+"-custody", "receipt-323.json")); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runBridge(t, stateDir, abandonRequest(), "abandon")
	if err == nil || !strings.Contains(stderr, "invalid abandonment capability") {
		t.Fatalf("legacy receipt was abandoned: %v: %s", err, stderr)
	}
	if show(t, stateDir)["state"] != "created" {
		t.Fatal("legacy receipt was mutated")
	}
}

// The bound custody-directory identity must not leak an owner path anywhere a
// producer, log, diagnostic, or the receipt store could observe it.
func TestBoundCustodyIdentityNeverLeavesTheOwnerPrivateRecord(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	custodyDir := stateDir + "-custody"
	record := readCapability(t, filepath.Join(stateDir+"-abandonment", "receipt-323.json"))
	digest, ok := record["custody_dir_hash"].(string)
	if !ok || len(digest) != 64 {
		t.Fatalf("capability record = %#v", record)
	}
	if err := os.Remove(filepath.Join(custodyDir, "receipt-323.json")); err != nil {
		t.Fatal(err)
	}
	result, stderr, err := runDirs(t, stateDir, custodyDir, stateDir+"-abandonment", abandonRequest(), "abandon")
	if err != nil || result["outcome"] != "recorded" {
		t.Fatalf("abandon: %#v: %v: %s", result, err, stderr)
	}
	emitted, _ := json.Marshal(result)
	receipts, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	metadata, _ := json.Marshal(show(t, stateDir))
	for label, observed := range map[string][]byte{
		"stdout": emitted, "stderr": []byte(stderr), "receipts.json": receipts, "show": metadata,
	} {
		if bytes.Contains(observed, []byte(custodyDir)) || bytes.Contains(observed, []byte(digest)) {
			t.Errorf("%s exposed the bound custody directory: %s", label, observed)
		}
	}
}

// Every post-commit cleanup failure must leave a truthful terminal result, and
// the identical replay must finish cleanup without appending a second event.
func TestTerminalCleanupFailuresStayTruthfulAndReplayCompletes(t *testing.T) {
	// 0o500 denies the unlink; 0o300 allows the unlink but denies the
	// directory open the fsync needs, isolating the after-unlink failure.
	for _, failure := range []struct {
		name        string
		mode        os.FileMode
		stillExists bool
	}{
		{"unlink_denied", 0o500, true},
		{"directory_flush_denied", 0o300, false},
	} {
		t.Run("acknowledge/"+failure.name, func(t *testing.T) {
			stateDir := t.TempDir()
			requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
			requireOutcome(t, stateDir, "recorded", submitRequest(stateDir), "submit")
			consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-323", "origin_thread": origin}
			requireOutcome(t, stateDir, "recorded", consume, "consume")
			custodyDir := stateDir + "-custody"
			custodyPath := filepath.Join(custodyDir, "receipt-323.json")
			ack := map[string]any{
				"receipt_id": "receipt-323", "event_id": "ack-323",
				"report_event_id": "report-323", "origin_thread": origin,
			}

			chmodDir(t, custodyDir, failure.mode)
			result := requireOutcome(t, stateDir, "recorded", ack, "acknowledge")
			if result["state"] != "acknowledged" || result["custody_cleanup"] != "pending" {
				t.Fatalf("cleanup failure result = %#v", result)
			}
			if _, err := os.Stat(custodyPath); (err == nil) != failure.stillExists {
				t.Fatalf("custody presence = %v, want exists %v", err, failure.stillExists)
			}
			receipt := show(t, stateDir)
			events := len(receipt["events"].([]any))
			if receipt["state"] != "acknowledged" {
				t.Fatalf("durable state = %#v", receipt)
			}

			if !failure.stillExists {
				// The record is already gone, so only a retried directory
				// flush can still observe the induced failure.
				stillPending := requireOutcome(t, stateDir, "duplicate", ack, "acknowledge")
				if stillPending["custody_cleanup"] != "pending" {
					t.Fatalf("replay skipped the directory flush retry: %#v", stillPending)
				}
			}
			if err := os.Chmod(custodyDir, 0o700); err != nil {
				t.Fatal(err)
			}
			replayed := requireOutcome(t, stateDir, "duplicate", ack, "acknowledge")
			if replayed["state"] != "acknowledged" || replayed["custody_cleanup"] != "removed" {
				t.Fatalf("replay result = %#v", replayed)
			}
			if _, err := os.Stat(custodyPath); !os.IsNotExist(err) {
				t.Fatalf("replay left custody behind: %v", err)
			}
			if replayedReceipt := show(t, stateDir); len(replayedReceipt["events"].([]any)) != events {
				t.Fatalf("replay appended an event: %#v", replayedReceipt)
			}
		})

		t.Run("abandon/"+failure.name, func(t *testing.T) {
			stateDir := t.TempDir()
			requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
			if err := os.Remove(filepath.Join(stateDir+"-custody", "receipt-323.json")); err != nil {
				t.Fatal(err)
			}
			abandonmentDir := stateDir + "-abandonment"
			capabilityPath := filepath.Join(abandonmentDir, "receipt-323.json")

			chmodDir(t, abandonmentDir, failure.mode)
			result := requireOutcome(t, stateDir, "recorded", abandonRequest(), "abandon")
			if result["state"] != "abandoned" || result["capability_cleanup"] != "pending" {
				t.Fatalf("cleanup failure result = %#v", result)
			}
			if _, err := os.Stat(capabilityPath); (err == nil) != failure.stillExists {
				t.Fatalf("capability presence = %v, want exists %v", err, failure.stillExists)
			}
			receipt := show(t, stateDir)
			events := len(receipt["events"].([]any))
			if receipt["state"] != "abandoned" || events != 2 {
				t.Fatalf("durable state = %#v", receipt)
			}

			if !failure.stillExists {
				stillPending := requireOutcome(t, stateDir, "duplicate", abandonRequest(), "abandon")
				if stillPending["capability_cleanup"] != "pending" {
					t.Fatalf("replay skipped the directory flush retry: %#v", stillPending)
				}
			}
			if err := os.Chmod(abandonmentDir, 0o700); err != nil {
				t.Fatal(err)
			}
			replayed := requireOutcome(t, stateDir, "duplicate", abandonRequest(), "abandon")
			if replayed["state"] != "abandoned" || replayed["capability_cleanup"] != "removed" {
				t.Fatalf("replay result = %#v", replayed)
			}
			if _, err := os.Stat(capabilityPath); !os.IsNotExist(err) {
				t.Fatalf("replay left the capability behind: %v", err)
			}
			if replayedReceipt := show(t, stateDir); len(replayedReceipt["events"].([]any)) != events {
				t.Fatalf("replay appended an event: %#v", replayedReceipt)
			}
		})
	}
}

// Cleanup status is scoped to the caller-supplied directory. A terminal replay
// against another directory cannot prove global absence or authorize removal
// of a separately discovered owner-private record.
func TestTerminalReplayUsesTheSameCapabilityDirectory(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	requireOutcome(t, stateDir, "recorded", submitRequest(stateDir), "submit")
	consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-323", "origin_thread": origin}
	requireOutcome(t, stateDir, "recorded", consume, "consume")
	ack := map[string]any{
		"receipt_id": "receipt-323", "event_id": "ack-323",
		"report_event_id": "report-323", "origin_thread": origin,
	}
	realDir := stateDir + "-custody"
	realRecord := filepath.Join(realDir, "receipt-323.json")
	chmodDir(t, realDir, 0o500)
	if result := requireOutcome(t, stateDir, "recorded", ack, "acknowledge"); result["custody_cleanup"] != "pending" {
		t.Fatalf("initial cleanup = %#v", result)
	}

	alternateDir := t.TempDir()
	result, stderr, err := runDirs(t, stateDir, alternateDir, "", ack, "acknowledge")
	if err != nil || result["outcome"] != "duplicate" || result["custody_cleanup"] != "removed" {
		t.Fatalf("alternate replay = %#v: %v: %s", result, err, stderr)
	}
	if _, err := os.Stat(realRecord); err != nil {
		t.Fatalf("alternate replay proved false global absence: %v", err)
	}

	// Manual removal is safe only after the exact receipt is independently
	// verified terminal; the alternate cleanup result is insufficient.
	receipt := show(t, stateDir)
	if receipt["state"] != "acknowledged" {
		t.Fatalf("leftover record receipt is not terminal: %#v", receipt)
	}
	if err := os.Chmod(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(realRecord); err != nil {
		t.Fatal(err)
	}
}

func TestPrivatePathsAreSeparatedAndNeverGivenToProducer(t *testing.T) {
	stateDir := t.TempDir()
	request, _ := json.Marshal(createRequest(stateDir))
	nested := filepath.Join(stateDir, "private")
	command := exec.Command("python3", helperPath(t), "--state-dir", stateDir, "--custody-dir", nested, "--abandonment-dir", stateDir+"-abandonment", "create")
	command.Stdin = bytes.NewReader(request)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err == nil || !strings.Contains(stderr.String(), "non-nested") {
		t.Fatalf("nested custody accepted: %v: %s", err, stderr.String())
	}
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatalf("nested preflight mutated custody: %v", err)
	}

	command = exec.Command("python3", helperPath(t), "--state-dir", stateDir, "--custody-dir", stateDir+"-custody", "submit")
	submit, _ := json.Marshal(submitRequest(stateDir))
	command.Stdin = bytes.NewReader(submit)
	stderr.Reset()
	command.Stderr = &stderr
	if err := command.Run(); err == nil || !strings.Contains(stderr.String(), "must not receive private capability paths") {
		t.Fatalf("producer received custody path: %v: %s", err, stderr.String())
	}
}

func TestBlockedReportAndProviderStopWithoutSubmit(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")

	// A provider stop or exit without submit leaves created-only state: no finding.
	if show(t, stateDir)["state"] != "created" {
		t.Fatal("pre-submit receipt is not created-only")
	}
	metadata, _ := json.Marshal(show(t, stateDir))
	if bytes.Contains(metadata, []byte(`"report_present":true`)) {
		t.Fatalf("created-only show leaked a report: %s", metadata)
	}

	emptyBlocked := submitRequest(stateDir)
	emptyBlocked["event_id"] = "report-blocked-empty"
	emptyBlocked["report"] = map[string]any{
		"status": "blocked", "summary": "Stopped before finishing.",
		"findings": []any{}, "blockers": []any{}, "verification": []any{"checked pinned head only"},
	}
	_, stderr, err := runBridge(t, stateDir, emptyBlocked, "submit")
	if err == nil || !strings.Contains(stderr, "blocker") {
		t.Fatalf("blocked without blockers: %v: %s", err, stderr)
	}
	if show(t, stateDir)["state"] != "created" {
		t.Fatal("invalid blocked report mutated receipt")
	}

	blocked := submitRequest(stateDir)
	blocked["event_id"] = "report-blocked-323"
	blocked["report"] = map[string]any{
		"status":  "blocked",
		"summary": "Provider nearing stop; partial second opinion only.",
		"findings": []any{
			"[medium] pkg/date.ts:selectSlot — initial partial month; missing fetchDates; evidence at deadbeef; test gap; eager-complete boundary month",
		},
		"blockers":     []any{"provider_stop_requested: remaining files unscanned"},
		"verification": []any{"git rev-parse HEAD matched binding task text"},
	}
	requireOutcome(t, stateDir, "recorded", blocked, "submit")
	requireOutcome(t, stateDir, "duplicate", blocked, "submit")

	conflict := submitRequest(stateDir)
	conflict["event_id"] = "report-blocked-323"
	conflict["report"] = map[string]any{
		"status": "complete", "summary": "Conflicting completion after blocked.",
		"findings": []any{"should not replace blocked"}, "blockers": []any{}, "verification": []any{"x"},
	}
	_, stderr, err = runBridge(t, stateDir, conflict, "submit")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("blocked conflict: %v: %s", err, stderr)
	}

	consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-blocked", "origin_thread": origin}
	result := requireOutcome(t, stateDir, "recorded", consume, "consume")
	report, _ := result["report"].(map[string]any)
	if result["state"] != "delivered" || report == nil || report["status"] != "blocked" {
		t.Fatalf("blocked consume = %#v", result)
	}
	findings, _ := report["findings"].([]any)
	blockers, _ := report["blockers"].([]any)
	if len(findings) != 1 || len(blockers) != 1 {
		t.Fatalf("blocked payload = %#v", report)
	}
	ack := map[string]any{"receipt_id": "receipt-323", "event_id": "ack-blocked", "report_event_id": "report-blocked-323", "origin_thread": origin}
	requireOutcome(t, stateDir, "recorded", ack, "acknowledge")
}

func TestSemanticReportRejectsTranscriptAndOversizedFindings(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")

	withTranscript := submitRequest(stateDir)
	withTranscript["event_id"] = "report-transcript"
	withTranscript["report"] = map[string]any{
		"status": "complete", "summary": "ok", "findings": []any{"one"}, "blockers": []any{},
		"verification": []any{"ok"}, "transcript": "raw provider dump",
	}
	_, stderr, err := runBridge(t, stateDir, withTranscript, "submit")
	if err == nil || !strings.Contains(stderr, "unknown fields") {
		t.Fatalf("transcript field: %v: %s", err, stderr)
	}

	oversizedItem := submitRequest(stateDir)
	oversizedItem["event_id"] = "report-oversized-item"
	oversizedItem["report"] = map[string]any{
		"status": "complete", "summary": "ok",
		"findings": []any{strings.Repeat("f", 2049)}, "blockers": []any{}, "verification": []any{"ok"},
	}
	_, stderr, err = runBridge(t, stateDir, oversizedItem, "submit")
	if err == nil || !strings.Contains(stderr, "invalid item") {
		t.Fatalf("oversized finding item: %v: %s", err, stderr)
	}

	tooMany := make([]any, 33)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("finding-%d", i)
	}
	oversizedList := submitRequest(stateDir)
	oversizedList["event_id"] = "report-oversized-list"
	oversizedList["report"] = map[string]any{
		"status": "complete", "summary": "ok", "findings": tooMany, "blockers": []any{}, "verification": []any{"ok"},
	}
	_, stderr, err = runBridge(t, stateDir, oversizedList, "submit")
	if err == nil || !strings.Contains(stderr, "bounded array") {
		t.Fatalf("oversized findings list: %v: %s", err, stderr)
	}

	oversizedSummary := submitRequest(stateDir)
	oversizedSummary["event_id"] = "report-oversized-summary"
	oversizedSummary["report"] = map[string]any{
		"status": "complete", "summary": strings.Repeat("s", 8193),
		"findings": []any{}, "blockers": []any{}, "verification": []any{"ok"},
	}
	_, stderr, err = runBridge(t, stateDir, oversizedSummary, "submit")
	if err == nil || !strings.Contains(stderr, "summary") {
		t.Fatalf("oversized summary: %v: %s", err, stderr)
	}

	oversizedBlocker := submitRequest(stateDir)
	oversizedBlocker["event_id"] = "report-oversized-blocker"
	oversizedBlocker["report"] = map[string]any{
		"status": "blocked", "summary": "partial",
		"findings": []any{}, "blockers": []any{strings.Repeat("b", 2049)}, "verification": []any{"ok"},
	}
	_, stderr, err = runBridge(t, stateDir, oversizedBlocker, "submit")
	if err == nil || !strings.Contains(stderr, "invalid item") {
		t.Fatalf("oversized blocker item: %v: %s", err, stderr)
	}

	oversizedVerification := submitRequest(stateDir)
	oversizedVerification["event_id"] = "report-oversized-verification"
	oversizedVerification["report"] = map[string]any{
		"status": "complete", "summary": "ok",
		"findings": []any{}, "blockers": []any{}, "verification": []any{strings.Repeat("v", 2049)},
	}
	_, stderr, err = runBridge(t, stateDir, oversizedVerification, "submit")
	if err == nil || !strings.Contains(stderr, "invalid item") {
		t.Fatalf("oversized verification item: %v: %s", err, stderr)
	}

	if show(t, stateDir)["state"] != "created" {
		t.Fatal("invalid semantic report mutated receipt")
	}
}

func TestProducerCannotEscalateBeyondReportOnlyAuthority(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	// Producer-only surface already rejects custody/abandonment args in
	// TestPrivatePathsAreSeparatedAndNeverGivenToProducer. Escalate authority must fail closed.
	coord := submitRequest(stateDir)
	coord["authority"] = "coordinator"
	_, stderr, err := runBridge(t, stateDir, coord, "submit")
	if err == nil || (!strings.Contains(stderr, "immutable") && !strings.Contains(stderr, "authority") && !strings.Contains(stderr, "report_only")) {
		t.Fatalf("coordinator authority submit: %v: %s", err, stderr)
	}
	if show(t, stateDir)["state"] != "created" {
		t.Fatal("authority escalation mutated receipt")
	}
}

func TestWrongWorkdirBindingFailsClosedForSameHeadWorkflow(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequest(stateDir), "create")
	// Distinct path is not identity: submit must repeat the exact bound Tycho workdir.
	request := submitRequest(stateDir)
	request["workdir"] = stateDir + "-other-worktree"
	_, stderr, err := runBridge(t, stateDir, request, "submit")
	if err == nil || !strings.Contains(stderr, "immutable workdir") {
		t.Fatalf("wrong workdir: %v: %s", err, stderr)
	}
	digest := submitRequest(stateDir)
	digest["task_digest"] = strings.Repeat("e", 64)
	_, stderr, err = runBridge(t, stateDir, digest, "submit")
	if err == nil || !strings.Contains(stderr, "immutable task_digest") {
		t.Fatalf("wrong task digest: %v: %s", err, stderr)
	}
	if show(t, stateDir)["state"] != "created" {
		t.Fatal("wrong head/workdir binding mutated receipt")
	}
}

func TestNotificationTimeoutIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("exercises the notification timeout")
	}
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequestWithTarget(stateDir), "create")
	fakeDir, _ := writeFakeTmux(t, "timeout", stateDir)
	request := submitRequest(stateDir)
	start := time.Now()
	result, stderr, err := runBridgeEnv(t, stateDir, request, []string{"PATH=" + fakeDir + ":" + os.Getenv("PATH")}, "submit")
	if err != nil || result["notification"] != "indeterminate" {
		t.Fatalf("timeout result = %#v, err = %v, stderr = %s", result, err, stderr)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("notification timeout took %v", elapsed)
	}
}

func TestMaximumEventIDAndReservedNamespaceRemainReadable(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequestWithTarget(stateDir), "create")
	fakeDir, _ := writeFakeTmux(t, "success", stateDir)
	request := submitRequest(stateDir)
	request["event_id"] = strings.Repeat("r", 128)
	result, stderr, err := runBridgeEnv(t, stateDir, request, []string{"PATH=" + fakeDir + ":" + os.Getenv("PATH")}, "submit")
	if err != nil || result["notification"] != "succeeded" {
		t.Fatalf("maximum event ID: %#v: %v: %s", result, err, stderr)
	}
	consume := map[string]any{"receipt_id": "receipt-323", "event_id": "consume-max", "origin_thread": origin}
	requireOutcome(t, stateDir, "recorded", consume, "consume")
	ack := map[string]any{"receipt_id": "receipt-323", "event_id": "ack-max", "report_event_id": strings.Repeat("r", 128), "origin_thread": origin}
	requireOutcome(t, stateDir, "recorded", ack, "acknowledge")
	if show(t, stateDir)["state"] != "acknowledged" {
		t.Fatal("maximum event ID made store unreadable")
	}

	other := t.TempDir()
	reserved := createRequest(other)
	reserved["event_id"] = "internal:notify:intent:forbidden"
	_, stderr, err = runBridge(t, other, reserved, "create")
	if err == nil || !strings.Contains(stderr, "reserved internal namespace") {
		t.Fatalf("reserved ID: %v: %s", err, stderr)
	}
}

func TestNotificationResultLockContentionLeavesDurableIntent(t *testing.T) {
	stateDir := t.TempDir()
	requireOutcome(t, stateDir, "recorded", createRequestWithTarget(stateDir), "create")
	fakeDir, log := writeFakeTmux(t, "result-lock", stateDir)
	request := submitRequest(stateDir)
	result, stderr, err := runBridgeEnv(t, stateDir, request, []string{"PATH=" + fakeDir + ":" + os.Getenv("PATH")}, "submit")
	if err != nil || result["notification"] != "indeterminate" || result["state"] != "valid_report" {
		t.Fatalf("result lock contention: %#v: %v: %s", result, err, stderr)
	}
	time.Sleep(600 * time.Millisecond)
	receipt := show(t, stateDir)
	events := receipt["events"].([]any)
	if events[len(events)-1].(map[string]any)["kind"] != "notification_intent" {
		t.Fatalf("durable resting event = %#v", events[len(events)-1])
	}
	before, _ := os.ReadFile(log)
	requireOutcome(t, stateDir, "duplicate", request, "submit")
	after, _ := os.ReadFile(log)
	if !bytes.Equal(before, after) {
		t.Fatal("replay resent notification after indeterminate result persistence")
	}
}
