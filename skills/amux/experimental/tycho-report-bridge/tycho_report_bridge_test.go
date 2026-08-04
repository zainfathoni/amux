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
	abandonmentPath := filepath.Join(stateDir+"-abandonment", "receipt-323.json")
	wrong := map[string]any{"receipt_id": "receipt-323", "origin_thread": origin, "abandonment_token": producerNonce}
	raw, _ := json.Marshal(wrong)
	if err := os.WriteFile(abandonmentPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runBridge(t, stateDir, abandon, "abandon")
	if err == nil || !strings.Contains(stderr, "invalid abandonment capability") {
		t.Fatalf("wrong capability abandoned receipt: %v: %s", err, stderr)
	}
	correct := map[string]any{"receipt_id": "receipt-323", "origin_thread": origin, "abandonment_token": abandonmentToken}
	raw, _ = json.Marshal(correct)
	if err := os.WriteFile(abandonmentPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
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
