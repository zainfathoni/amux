package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestLinuxProcessIdentityRejectsAmbiguousSnapshots(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, pathlib, sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
fields = [b"S"] + [b"0"] * 18 + [b"424242"]
assert module.linux_process_start_time(b"123 (claude ) name) " + b" ".join(fields)) == "424242"
try:
    module.linux_process_start_time(b"123 malformed")
except module.HelperError:
    pass
else:
    raise AssertionError("malformed stat accepted")

def rejected(starts, executables, commands):
    start_values = iter(starts)
    executable_values = iter(executables)
    command_values = iter(commands)
    module.read_linux_process_start = lambda proc: next(start_values)
    module.read_linux_process_executable = lambda proc: next(executable_values)
    module.read_linux_process_command = lambda proc: next(command_values)
    try:
        module.read_linux_process_snapshot(123)
    except module.HelperError:
        return
    raise AssertionError("ambiguous Linux process snapshot accepted")

same_executable = ("/usr/bin/claude", 8, 42)
rejected(["100", "101"], [same_executable, same_executable], [b"claude\0", b"claude\0"])
rejected(["100", "100"], [same_executable, ("/usr/bin/claude", 8, 43)], [b"claude\0", b"claude\0"])
rejected(["100", "100"], [same_executable, same_executable], [b"claude\0", b"other\0"])
rejected(["100", "100"], [same_executable, same_executable], [b"", b""])
rejected(["100", "100"], [same_executable, same_executable], [b"claude", b"claude"])

package = "/tmp/node_modules/@anthropic-ai/claude-code/cli.js"
assert module.is_claude_process("Darwin", "claude", ["/usr/bin/claude", "--session-id", "session"], 1)
assert not module.is_claude_process("Darwin", "node", ["/usr/bin/node", package, "--session-id", "session"], 2)
assert module.is_claude_process("Linux", "node", ["/usr/bin/node", package, "--session-id", "session"], 2)
assert module.is_claude_process("Linux", "bun", ["/usr/bin/bun", package, "--session-id", "session"], 2)
assert not module.is_claude_process("Linux", "node", ["/usr/bin/node", "/tmp/unrelated/cli.js", package, "--session-id", "session"], 3)
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("Linux stat parser fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Linux stat parser output = %q", output)
	}
}

func TestReceiptBindingIsImmutableWhileRoutingCanChange(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-1")
	create := map[string]any{"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"}}
	assertHelperOutcome(t, stateDir, "recorded", create, "receipt", "create")
	assertHelperOutcome(t, stateDir, "duplicate", create, "receipt", "create")

	conflictingBinding := cloneJSONMap(t, binding)
	conflictingBinding["origin_thread"] = "T-crossed"
	conflict := map[string]any{"binding": conflictingBinding, "routing": create["routing"]}
	_, stderr, err := runHelper(t, stateDir, conflict, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "different immutable binding") {
		t.Fatalf("conflicting create error = %v, stderr %q; want immutable-binding rejection", err, stderr)
	}

	route := map[string]any{
		"delegation_id": "delegation-1",
		"event_id":      "route-1",
		"routing":       map[string]any{"target": "T-origin", "recovery": "machine_local_inbox"},
	}
	assertHelperOutcome(t, stateDir, "recorded", route, "receipt", "route")
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", "delegation-1")
	if err != nil {
		t.Fatalf("show receipt: %v: %s", err, stderr)
	}
	var receipt struct {
		Binding map[string]any `json:"binding"`
		Routing map[string]any `json:"routing"`
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("decode receipt: %v\n%s", err, stdout)
	}
	if receipt.Binding["origin_thread"] != "T-origin" {
		t.Fatalf("routing mutation changed immutable origin: %#v", receipt.Binding)
	}
	if receipt.Routing["target"] != "T-origin" || receipt.Routing["recovery"] != "machine_local_inbox" {
		t.Fatalf("routing = %#v, want updated target with inbox recovery", receipt.Routing)
	}
}

func TestReportLifecycleRequiresExplicitOrderedTransitions(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-report")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-1", "thinker_report", map[string]any{
		"accepted_role":       true,
		"accepted_exclusions": true,
		"status":              "complete",
		"verdict":             "The bounded mechanism is coherent.",
		"rationale":           "Evidence and assumptions remain distinct.",
		"evidence":            []any{"public source A"},
		"assumptions":         []any{"runtime enforcement remains vendor-owned"},
		"unsupported_claims":  []any{},
		"blockers":            []any{},
		"verification":        []any{"read public source A"},
		"changed_artifacts":   []any{},
		"references":          []any{"source:A"},
	})
	assertHelperOutcome(t, stateDir, "recorded", report, "report", "submit")
	assertHelperOutcome(t, stateDir, "duplicate", report, "report", "submit")
	lateInput := testMessage(binding, "input-too-late", "input_request", map[string]any{
		"request_type": "clarification", "question": "Too late?", "blocking_reason": "A report already exists.",
	})
	_, stderr, err := runHelper(t, stateDir, lateInput, "input", "submit")
	if err == nil || !strings.Contains(stderr, "closes the input-request stream") {
		t.Fatalf("late input error = %v, stderr %q", err, stderr)
	}

	conflict := cloneJSONMap(t, report)
	conflict["report"].(map[string]any)["verdict"] = "Conflicting payload"
	_, stderr, err = runHelper(t, stateDir, conflict, "report", "submit")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflicting report replay error = %v, stderr %q", err, stderr)
	}

	acknowledge := map[string]any{"delegation_id": binding["delegation_id"], "event_id": "ack-1", "message_id": "report-1"}
	_, stderr, err = runHelper(t, stateDir, acknowledge, "report", "acknowledge")
	if err == nil || !strings.Contains(stderr, "requires delivery") {
		t.Fatalf("early acknowledge error = %v, stderr %q", err, stderr)
	}
	park := map[string]any{"delegation_id": binding["delegation_id"], "event_id": "park-1"}
	_, stderr, err = runHelper(t, stateDir, park, "session", "park")
	if err == nil || !strings.Contains(stderr, "requires acknowledgement") {
		t.Fatalf("early park error = %v, stderr %q", err, stderr)
	}

	consume := map[string]any{"delegation_id": binding["delegation_id"], "event_id": "deliver-1", "message_id": "report-1"}
	assertHelperOutcome(t, stateDir, "recorded", consume, "inbox", "consume")
	assertHelperOutcome(t, stateDir, "recorded", acknowledge, "report", "acknowledge")
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show acknowledged receipt: %v: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"state":"acknowledged"`) {
		t.Fatalf("acknowledged receipt state missing: %s", stdout)
	}
}

func TestInvalidOrPrivateReportDoesNotAdvanceReceipt(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-invalid-report")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-private", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Invalid fixture.", "rationale": "Private content must be rejected.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{}, "changed_artifacts": []any{}, "references": []any{},
		"transcript": "must never be persisted",
	})
	_, stderr, err := runHelper(t, stateDir, report, "report", "submit")
	if err == nil || !strings.Contains(stderr, "unknown fields") {
		t.Fatalf("private report error = %v, stderr %q", err, stderr)
	}
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show unresolved receipt: %v: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"state":"created"`) || strings.Contains(stdout, "must never be persisted") {
		t.Fatalf("invalid report changed or leaked into receipt: %s", stdout)
	}
}

func TestInputRequestDeliveryDoesNotResolveItsMeaning(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-input")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	request := testMessage(binding, "input-1", "input_request", map[string]any{
		"request_type":    "clarification",
		"question":        "Which public source should govern the comparison?",
		"blocking_reason": "The two public sources use different terms.",
	})
	assertHelperOutcome(t, stateDir, "recorded", request, "input", "submit")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "seen-1", "message_id": "input-1",
	}, "inbox", "consume")

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show input receipt: %v: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"input_state":"seen"`) || strings.Contains(stdout, `"input_state":"resolved"`) {
		t.Fatalf("delivery must mark input seen but unresolved: %s", stdout)
	}
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "accepted-1", "message_id": "input-1",
	}, "input", "accept")
}

func TestConcurrentWritersShareOnePrivateLockDomainWithoutLosingEvents(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-concurrent")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")

	const writers = 12
	start := make(chan struct{})
	errors := make(chan error, writers)
	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			input := map[string]any{
				"delegation_id": binding["delegation_id"],
				"event_id":      fmt.Sprintf("route-%d", index),
				"routing":       map[string]any{"target": fmt.Sprintf("T-%d", index), "recovery": "machine_local_inbox"},
			}
			stdout, stderr, err := runHelper(t, stateDir, input, "receipt", "route")
			if err != nil || !strings.Contains(stdout, `"outcome":"recorded"`) {
				errors <- fmt.Errorf("writer %d: %v: %s%s", index, err, stdout, stderr)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show concurrent receipt: %v: %s", err, stderr)
	}
	var receipt struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Events) != writers+1 {
		t.Fatalf("event count = %d, want %d; concurrent mutation lost an event", len(receipt.Events), writers+1)
	}
	for path, want := range map[string]os.FileMode{
		stateDir:                                     0o700,
		filepath.Join(stateDir, "receipts.json"):     0o600,
		filepath.Join(stateDir, "experimental.lock"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(path), info.Mode().Perm(), want)
		}
	}
}

func TestMCPServerExposesOnlySchemaLimitedSemanticSubmission(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-mcp")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-mcp", "thinker_report", map[string]any{
		"accepted_role":       true,
		"accepted_exclusions": true,
		"status":              "complete",
		"verdict":             "Synthetic schema check complete.",
		"rationale":           "The MCP request contains only bounded fields.",
		"evidence":            []any{"synthetic fixture"},
		"assumptions":         []any{},
		"unsupported_claims":  []any{},
		"blockers":            []any{},
		"verification":        []any{"MCP protocol fixture"},
		"changed_artifacts":   []any{},
		"references":          []any{},
	})
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "submit_report", "arguments": report}},
	}
	var input bytes.Buffer
	for _, message := range messages {
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(data)
		input.WriteByte('\n')
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", helper, "--state-dir", stateDir, "mcp", "serve", "--delegation-id", binding["delegation_id"].(string))
	command.Stdin = &input
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP server failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 3 {
		t.Fatalf("MCP responses = %d, want 3\n%s", len(lines), output)
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Tools) != 2 || listed.Result.Tools[0].Name != "submit_report" || listed.Result.Tools[1].Name != "submit_input_request" {
		t.Fatalf("MCP tools = %#v, want only report and input-request submission", listed.Result.Tools)
	}
	if !strings.Contains(lines[2], `"outcome":"recorded"`) || !strings.Contains(lines[2], `"isError":false`) {
		t.Fatalf("MCP tool result did not record report: %s", lines[2])
	}
}

func TestNotificationFailsClosedAndRunsOnlyAfterDurableReport(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	panePID := startProcessFixture(t, "amp", "threads", "continue", "T-origin")
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  display-message) printf 'Amp\tcoordinator\t@9\t%%9\t%s\t%s\tamp\n' "$PANE_PID" "$TARGET_WORKDIR" ;;
  send-keys)
    grep -q '"state":"valid_report"' "$STATE_DIR/receipts.json"
    printf '%s\n' "$*" >> "$TMUX_LOG"
    ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "ps"), `#!/bin/sh
set -eu
case "$*" in
  *lstart=*) printf '%s\n' 'Fri Jul 17 12:00:00 2026' ;;
  *comm=*) printf '%s\n' '/usr/local/bin/amp' ;;
  *command=*) printf '%s\n' 'amp threads continue T-origin' ;;
  *) exit 2 ;;
esac
`)
	environment := append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TMUX_LOG="+logPath, "STATE_DIR="+stateDir, "TARGET_WORKDIR="+stateDir, fmt.Sprintf("PANE_PID=%d", panePID))
	binding := testBinding("delegation-notify")
	binding["workdir"] = stateDir
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "T-origin", "recovery": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-notify", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Synthetic notification fixture.", "rationale": "No content is sent to the pane.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{"durable-before-send fixture"}, "changed_artifacts": []any{}, "references": []any{},
	})
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", report, "report", "submit")

	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "amp", "inspect", "--pane", "%9", "--origin-thread", "T-origin")
	if err != nil {
		t.Fatalf("inspect Amp target: %v: %s", err, stderr)
	}
	var target map[string]any
	if err := json.Unmarshal([]byte(stdout), &target); err != nil {
		t.Fatal(err)
	}
	notify := map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "notify-1", "message_id": "report-notify", "target": target,
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "notified", notify, "notify", "amp-pane")
	unsafeID := cloneJSONMap(t, notify)
	unsafeID["event_id"] = "notify\nunsafe"
	_, stderr, err = runHelperEnv(t, stateDir, environment, unsafeID, "notify", "amp-pane")
	if err == nil || !strings.Contains(stderr, "control characters") {
		t.Fatalf("unsafe notification ID error = %v, stderr %q", err, stderr)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "AMUX_CLAUDE_REPORT delegation_sha256=") || !strings.Contains(string(log), "message_sha256=") {
		t.Fatalf("wake-up token missing or contains semantic content: %s", log)
	}
	receiptPath := filepath.Join(stateDir, "receipts.json")
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var interruptedStore map[string]any
	if err := json.Unmarshal(receiptBytes, &interruptedStore); err != nil {
		t.Fatal(err)
	}
	receipt := interruptedStore["receipts"].([]any)[0].(map[string]any)
	events := receipt["events"].([]any)
	withoutResult := events[:0]
	for _, raw := range events {
		if raw.(map[string]any)["kind"] != "notification_result" {
			withoutResult = append(withoutResult, raw)
		}
	}
	receipt["events"] = withoutResult
	interruptedBytes, err := json.Marshal(interruptedStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, interruptedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "unavailable", notify, "notify", "amp-pane")
	logAfterRecovery, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(logAfterRecovery) != string(log) {
		t.Fatal("interrupted notification recovery resent a wake-up")
	}
	recovered, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil || !strings.Contains(recovered, "interrupted before a durable result") {
		t.Fatalf("interrupted notification result was not persisted: %v: %s%s", err, recovered, stderr)
	}

	stale := cloneJSONMap(t, notify)
	stale["event_id"] = "notify-stale"
	stale["target"].(map[string]any)["process_identity"] = "changed-process-start"
	assertHelperOutcomeEnv(t, stateDir, environment, "unavailable", stale, "notify", "amp-pane")
	logAfter, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(logAfter) != string(log) {
		t.Fatal("stale target received a wake-up")
	}
	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil || !strings.Contains(stdout, `"state":"valid_report"`) {
		t.Fatalf("notification incorrectly established delivery: %v: %s%s", err, stdout, stderr)
	}
}

func TestParkingReverifiesExactClaudeIncarnationAfterAcknowledgement(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	identityUnavailable := filepath.Join(t.TempDir(), "identity-unavailable")
	sessionID := "550e8400-e29b-41d4-a716-446655440000"
	panePID := startProcessFixture(t, "claude", "--session-id", sessionID, "argument with spaces", "literal'quote")
	startCommand := "exec claude --session-id " + sessionID
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  display-message)
    test ! -e "$IDENTITY_UNAVAILABLE"
    printf 'Claude\tthinker\t%s\t%s\t%s\t%s\t2.1.212\t"%s"\n' "$CLAUDE_WINDOW_ID" "$CLAUDE_PANE_ID" "$PANE_PID" "$TARGET_WORKDIR" "$START_COMMAND"
    ;;
  list-panes) exit 0 ;;
  kill-pane) printf '%s\n' "$*" >> "$TMUX_LOG" ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "ps"), `#!/bin/sh
set -eu
case "$*" in
  *lstart=*) printf '%s\n' 'Fri Jul 17 12:01:00 2026' ;;
  *comm=*) printf '%s\n' '/usr/local/bin/claude' ;;
  *command=*) printf 'claude --session-id %s\n' "$CLAUDE_SESSION_ID" ;;
  *) exit 2 ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TMUX_LOG="+logPath,
		"TARGET_WORKDIR="+stateDir,
		"START_COMMAND="+startCommand,
		"CLAUDE_SESSION_ID="+sessionID,
		"IDENTITY_UNAVAILABLE="+identityUnavailable,
		fmt.Sprintf("PANE_PID=%d", panePID),
		"CLAUDE_WINDOW_ID=@10",
		"CLAUDE_PANE_ID=%10",
	)
	binding := testBinding("delegation-park")
	binding["workdir"] = stateDir
	digest := sha256.Sum256([]byte(startCommand))
	binding["launch_command_digest"] = fmt.Sprintf("%x", digest)
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	recordTestLaunch(t, stateDir, binding["delegation_id"].(string), map[string]any{
		"session": "Claude", "window": "thinker", "window_id": "@10", "pane_id": "%10",
	})
	wrongEnvironment := replaceEnvironment(environment, "CLAUDE_PANE_ID", "%11")
	wrongAcquire := map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "acquire-wrong-pane", "pane_id": "%11", "claude_session_id": sessionID,
	}
	_, stderr, err := runHelperEnv(t, stateDir, wrongEnvironment, wrongAcquire, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "exact pane created by this receipt") {
		t.Fatalf("wrong launch pane acquisition error = %v, stderr %q", err, stderr)
	}
	acquire := map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "acquire-1", "pane_id": "%10", "claude_session_id": sessionID,
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", acquire, "session", "acquire")
	if runtime.GOOS == "linux" {
		stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
		if err != nil {
			t.Fatalf("show Linux acquisition: %v: %s", err, stderr)
		}
		var acquired map[string]any
		if err := json.Unmarshal([]byte(stdout), &acquired); err != nil {
			t.Fatal(err)
		}
		identity := acquired["session_identity"].(map[string]any)
		if !strings.HasPrefix(identity["process_identity"].(string), "linux:") {
			t.Fatalf("Linux process identity = %q, want kernel start/executable identity", identity["process_identity"])
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(panePID), "cmdline"))
		if err != nil {
			t.Fatal(err)
		}
		wantDigest := fmt.Sprintf("%x", sha256.Sum256(cmdline[:len(cmdline)-1]))
		if identity["process_command_digest"] != wantDigest {
			t.Fatalf("Linux argv digest = %q, want NUL-delimited /proc digest %q", identity["process_command_digest"], wantDigest)
		}
	}
	if err := os.WriteFile(identityUnavailable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "duplicate", acquire, "session", "acquire")
	conflictingAcquire := cloneJSONMap(t, acquire)
	conflictingAcquire["pane_id"] = "%11"
	_, stderr, err = runHelperEnv(t, stateDir, environment, conflictingAcquire, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflicting acquisition replay error = %v, stderr %q", err, stderr)
	}
	if err := os.Remove(identityUnavailable); err != nil {
		t.Fatal(err)
	}
	report := testMessage(binding, "report-park", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Synthetic parking fixture.", "rationale": "Identity is checked before the tmux mutation.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{"synthetic exact identity"}, "changed_artifacts": []any{}, "references": []any{},
	})
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", report, "report", "submit")
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "deliver-park", "message_id": "report-park",
	}, "inbox", "consume")
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "ack-park", "message_id": "report-park",
	}, "report", "acknowledge")
	acknowledgedStore, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	recycledEnvironment := replaceEnvironment(environment, "CLAUDE_WINDOW_ID", "@99")
	_, stderr, err = runHelperEnv(t, stateDir, recycledEnvironment, map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-recycled-pane",
	}, "session", "park")
	if err == nil || !strings.Contains(stderr, "identity changed") {
		t.Fatalf("recycled pane parking error = %v, stderr %q", err, stderr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("recycled pane was killed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), acknowledgedStore, 0o600); err != nil {
		t.Fatal(err)
	}
	var changedStart map[string]any
	if err := json.Unmarshal(acknowledgedStore, &changedStart); err != nil {
		t.Fatal(err)
	}
	changedReceipt := changedStart["receipts"].([]any)[0].(map[string]any)
	changedReceipt["session_identity"].(map[string]any)["process_identity"] = "linux:start-time-reused"
	changedStartBytes, err := json.Marshal(changedStart)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), changedStartBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-reused-pid",
	}, "session", "park")
	if err == nil || !strings.Contains(stderr, "identity changed") {
		t.Fatalf("reused PID parking error = %v, stderr %q", err, stderr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("changed process incarnation was killed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), acknowledgedStore, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-verified",
	}, "session", "park")
	log, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(log), "kill-pane -t %10") {
		t.Fatalf("verified park did not target exact window: %v: %s", err, log)
	}
	var interrupted map[string]any
	if err := json.Unmarshal(acknowledgedStore, &interrupted); err != nil {
		t.Fatal(err)
	}
	receipt := interrupted["receipts"].([]any)[0].(map[string]any)
	receipt["events"] = append(receipt["events"].([]any), map[string]any{
		"event_id": "park-recovery", "kind": "park_intent", "identity": receipt["session_identity"], "at": "2026-07-17T12:02:00Z",
	})
	interruptedBytes, err := json.Marshal(interrupted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), interruptedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identityUnavailable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-recovery", "recover": true,
	}, "session", "park")
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil || !strings.Contains(stdout, `"state":"verified_parked"`) || !strings.Contains(stdout, sessionID) || !strings.Contains(stdout, `"cleanup_eligible_at"`) || !strings.Contains(stdout, `"recovered_absence":true`) {
		t.Fatalf("parked receipt does not preserve session/eligibility: %v: %s%s", err, stdout, stderr)
	}
}

func TestLaunchPlanRejectsMissingTargetSessionWithoutMutation(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "target tmux session does not exist") {
		t.Fatalf("missing-session plan error = %v, stderr %q", err, stderr)
	}
	entries, err := os.ReadDir(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("missing-session plan mutated private state: %#v", entries)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("missing-session plan created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLinuxLaunchDoesNotClaimMutatingDelegation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific capability boundary")
	}
	fixture := newLaunchFixture(t)
	request := cloneJSONMap(t, fixture.request)
	request["workflow"] = "mutating"
	delete(request, "expected_launch_policy_digest")
	request["baseline_branch"] = "delegate"
	request["writer_owner"] = "claude"
	request["integration_owner"] = "amp"
	request["coordinator_write_frozen"] = true
	request["shared_writable"] = false
	request["handoff"] = "one_clean_local_commit"
	request["capacity_request"] = map[string]any{}
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, request, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "mutating Claude launch remains available only on Darwin") {
		t.Fatalf("Linux mutating launch error = %v, stderr %q", err, stderr)
	}
}

func TestLaunchExecutionRejectsDisappearedTargetSessionBeforeIntent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	var plan struct {
		PacketDigest        string `json:"packet_digest"`
		LaunchPolicyDigest  string `json:"launch_policy_digest"`
		LaunchCommandDigest string `json:"launch_command_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.LaunchPolicyDigest != "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445" {
		t.Fatalf("read-only launch policy digest changed: %s", plan.LaunchPolicyDigest)
	}
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.disappearAfterCheck, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "target tmux session does not exist") {
		t.Fatalf("disappeared-session execute error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || bytes.Contains(after, []byte(`"kind":"launch_intent"`)) {
		t.Fatalf("disappeared-session execution changed the pre-launch receipt:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("disappeared-session execution created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("disappeared-session execution created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLaunchRequestsRejectMissingOrWrongExpectedPolicyDigestBeforeProbes(t *testing.T) {
	fixture := newLaunchFixture(t)
	for _, test := range []struct {
		name    string
		request map[string]any
		want    string
	}{
		{name: "missing", request: cloneJSONMap(t, fixture.request), want: "expected_launch_policy_digest must be a lowercase SHA-256 value"},
		{name: "wrong", request: cloneJSONMap(t, fixture.request), want: "expected launch policy digest does not match selected workflow"},
	} {
		for _, command := range []string{"plan", "execute"} {
			t.Run(test.name+"_"+command, func(t *testing.T) {
				if test.name == "missing" {
					delete(test.request, "expected_launch_policy_digest")
				} else {
					test.request["expected_launch_policy_digest"] = strings.Repeat("f", 64)
				}
				_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, test.request, "launch", command)
				if err == nil || !strings.Contains(stderr, test.want) {
					t.Fatalf("launch %s error = %v, stderr %q; want %q", command, err, stderr, test.want)
				}
				entries, readErr := os.ReadDir(fixture.stateDir)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if len(entries) != 0 {
					t.Fatalf("rejected launch %s mutated private state: %#v", command, entries)
				}
				if log, readErr := os.ReadFile(fixture.tmuxLog); readErr == nil && len(log) != 0 {
					t.Fatalf("rejected launch %s invoked tmux: %s", command, log)
				} else if readErr != nil && !os.IsNotExist(readErr) {
					t.Fatal(readErr)
				}
			})
		}
	}
}

func TestLaunchPolicyDigestCanFinalizeSelfContainedPacketBeforePlan(t *testing.T) {
	fixture := newLaunchFixture(t)
	stdout, stderr, err := runHelper(t, fixture.stateDir, map[string]any{"workflow": "read_only"}, "launch", "policy-digest")
	if err != nil {
		t.Fatalf("launch policy digest preflight: %v: %s", err, stderr)
	}
	var preflight struct {
		Workflow           string `json:"workflow"`
		LaunchPolicyDigest string `json:"launch_policy_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &preflight); err != nil {
		t.Fatal(err)
	}
	if preflight.Workflow != "read_only" || len(preflight.LaunchPolicyDigest) != 64 {
		t.Fatalf("launch policy digest preflight = %#v", preflight)
	}
	entries, err := os.ReadDir(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("launch policy digest preflight mutated private state: %#v", entries)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && len(log) != 0 {
		t.Fatalf("launch policy digest preflight invoked tmux: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, stderr, err := runHelper(t, fixture.stateDir, map[string]any{"workflow": "mutating"}, "launch", "policy-digest"); err == nil || !strings.Contains(stderr, "supports only read_only") {
		t.Fatalf("mutating policy digest preflight error = %v, stderr %q", err, stderr)
	}
	mutatingLaunch := cloneJSONMap(t, fixture.request)
	mutatingLaunch["workflow"] = "mutating"
	if _, stderr, err := runHelper(t, fixture.stateDir, mutatingLaunch, "launch", "plan"); err == nil || !strings.Contains(stderr, "unknown fields: expected_launch_policy_digest") {
		t.Fatalf("mutating launch expected policy field error = %v, stderr %q", err, stderr)
	}
	if runtime.GOOS != "darwin" {
		t.Skip("experimental Claude launch is macOS-first")
	}

	packet := fmt.Sprintf(`{"launch_policy_digest":%q,"task":"submit one correlated thinker report"}`, preflight.LaunchPolicyDigest)
	if err := os.WriteFile(fixture.request["packet_file"].(string), []byte(packet), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.request["expected_launch_policy_digest"] = preflight.LaunchPolicyDigest
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan for finalized packet: %v: %s", err, stderr)
	}
	var plan struct {
		PacketDigest        string `json:"packet_digest"`
		LaunchPolicyDigest  string `json:"launch_policy_digest"`
		LaunchCommandDigest string `json:"launch_command_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.LaunchPolicyDigest != preflight.LaunchPolicyDigest {
		t.Fatalf("final launch policy digest = %q, preflight = %q", plan.LaunchPolicyDigest, preflight.LaunchPolicyDigest)
	}

	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")

	report := testMessage(binding, "self-contained-report", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "The packet supplied every correlation value.", "rationale": "No post-launch input was required.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{}, "changed_artifacts": []any{}, "references": []any{},
	})
	wrong := cloneJSONMap(t, report)
	wrong["message_id"] = "wrong-policy-report"
	wrong["launch_policy_digest"] = strings.Repeat("f", 64)
	if _, stderr, err := runHelper(t, fixture.stateDir, wrong, "report", "submit"); err == nil || !strings.Contains(stderr, "does not match immutable receipt binding") {
		t.Fatalf("wrong policy digest error = %v, stderr %q", err, stderr)
	}
	omitted := cloneJSONMap(t, report)
	omitted["message_id"] = "omitted-policy-report"
	delete(omitted, "launch_policy_digest")
	if _, stderr, err := runHelper(t, fixture.stateDir, omitted, "report", "submit"); err == nil || !strings.Contains(stderr, "launch_policy_digest must be a non-empty string") {
		t.Fatalf("omitted policy digest error = %v, stderr %q", err, stderr)
	}
	assertHelperOutcome(t, fixture.stateDir, "recorded", report, "report", "submit")
}

type launchFixture struct {
	stateDir            string
	environment         []string
	request             map[string]any
	tmuxLog             string
	session             string
	disappearAfterCheck string
	dirtyAfterPreflight string
	dirtyState          string
}

func newLaunchFixture(t *testing.T) launchFixture {
	t.Helper()
	stateDir := t.TempDir()
	binDir := t.TempDir()
	workdir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(packetPath, []byte("bounded launch packet"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmuxLog := filepath.Join(t.TempDir(), "tmux.log")
	session := filepath.Join(t.TempDir(), "session-exists")
	disappearAfterCheck := filepath.Join(t.TempDir(), "disappear-after-check")
	dirtyAfterPreflight := filepath.Join(t.TempDir(), "dirty-after-preflight")
	dirtyState := filepath.Join(t.TempDir(), "dirty-state")
	base := "0123456789abcdef0123456789abcdef01234567"
	writeExecutable(t, filepath.Join(binDir, "git"), `#!/bin/sh
set -eu
case "$*" in
  *'rev-parse --show-toplevel'*) printf '%s\n' "$WORKDIR" ;;
  *'rev-parse HEAD'*) printf '%s\n' "$BASE" ;;
  *'rev-parse --git-dir'*) printf '%s\n' "$WORKDIR/.git/worktrees/fixture" ;;
  *'rev-parse --git-common-dir'*) printf '%s\n' '/tmp/source/.git' ;;
  *'symbolic-ref --short HEAD'*) printf '%s\n' 'delegate' ;;
  *'status --porcelain'*) if [ -e "$DIRTY_STATE" ]; then printf '%s\n' '?? changed'; fi ;;
  *'remote get-url origin'*) printf '%s\n' 'git@github.com:zainfathoni/amux.git' ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
if [ "$1" = "-V" ]; then
  printf '%s\n' 'tmux 3.7b'
  exit 0
fi
if [ "$1" = "has-session" ]; then
  test "$2" = "-t"
  test "$3" = "=Claude"
  test -e "$TMUX_SESSION"
  exit 0
fi
printf '%s\n' "$*" >> "$TMUX_LOG"
if [ "$1" = "new-window" ]; then
  test -e "$TMUX_SESSION"
  printf 'Claude\tthinker\t@20\t%%20\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
case "$1" in
  --version) printf '%s\n' '2.1.212 (Claude Code)' ;;
  --help)
    printf '%s\n' '--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools'
    if [ -e "$DISAPPEAR_AFTER_CHECK" ]; then
      rm "$DISAPPEAR_AFTER_CHECK" "$TMUX_SESSION"
    fi
    if [ -e "$DIRTY_AFTER_PREFLIGHT" ]; then
      rm "$DIRTY_AFTER_PREFLIGHT"
      : > "$DIRTY_STATE"
    fi
    ;;
  *) exit 0 ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"), "WORKDIR="+workdir, "BASE="+base,
		"TMUX_LOG="+tmuxLog, "TMUX_SESSION="+session, "DISAPPEAR_AFTER_CHECK="+disappearAfterCheck,
		"DIRTY_AFTER_PREFLIGHT="+dirtyAfterPreflight, "DIRTY_STATE="+dirtyState,
	)
	request := map[string]any{
		"delegation_id": "delegation-session-preflight", "event_id": "launch-session-preflight", "workdir": workdir,
		"packet_file": packetPath, "tmux_session": "Claude", "tmux_window": "thinker",
		"claude_session_id": "550e8400-e29b-41d4-a716-446655440000", "repository": "zainfathoni/amux", "base": base,
		"expected_launch_policy_digest": "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445",
	}
	return launchFixture{
		stateDir: stateDir, environment: environment, request: request, tmuxLog: tmuxLog,
		session: session, disappearAfterCheck: disappearAfterCheck,
		dirtyAfterPreflight: dirtyAfterPreflight, dirtyState: dirtyState,
	}
}

func TestLaunchPlanAndExecutionKeepPacketOutOfReceiptAndDenyMutationTools(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	stateDir := t.TempDir()
	binDir := t.TempDir()
	workdir := t.TempDir()
	workdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(t.TempDir(), "packet.json")
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	argvPath := filepath.Join(t.TempDir(), "claude.argv")
	base := "0123456789abcdef0123456789abcdef01234567"
	packetBinding := testBinding("../delegation-launch")
	packetBinding["workdir"] = workdir
	packetBinding["base"] = base
	packetBinding["launch_policy_digest"] = "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445"
	packetEnvelope := testMessage(packetBinding, "self-contained-mcp-report", "thinker_report", map[string]any{})
	delete(packetEnvelope, "created_at")
	delete(packetEnvelope, "report")
	packetBytes, err := json.Marshal(map[string]any{
		"task":            "submit one correlated thinker report without follow-up input",
		"report_envelope": packetEnvelope,
	})
	if err != nil {
		t.Fatal(err)
	}
	packet := string(packetBytes)
	if err := os.WriteFile(packetPath, packetBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "git"), `#!/bin/sh
set -eu
case "$*" in
  *'rev-parse --show-toplevel'*) printf '%s\n' "$WORKDIR" ;;
  *'rev-parse HEAD'*) printf '%s\n' "$BASE" ;;
  *'rev-parse --git-dir'*) printf '%s\n' "$WORKDIR/.git/worktrees/fixture" ;;
  *'rev-parse --git-common-dir'*) printf '%s\n' '/tmp/source/.git' ;;
  *'status --porcelain'*) exit 0 ;;
  *'remote get-url origin'*) printf '%s\n' 'git@github.com:zainfathoni/amux.git' ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
if [ "$1" = "-V" ]; then
  printf '%s\n' 'tmux 3.7b'
  exit 0
fi
if [ "$1" = "has-session" ]; then
  test "$2" = "-t"
  test "$3" = "=Claude"
  exit 0
fi
printf '%s\n' "$*" >> "$TMUX_LOG"
for argument do start_command=$argument; done
/bin/sh -c "$start_command"
printf '%s\n' 'Claude	thinker	@20	%20'
`)
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
case "$1" in
  --version) printf '%s\n' '2.1.212 (Claude Code)' ;;
  --help) printf '%s\n' '--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools' ;;
  *)
    : > "$ARGV_LOG"
    for argument do printf '%s\0' "$argument" >> "$ARGV_LOG"; done
    ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"), "WORKDIR="+workdir, "BASE="+base, "TMUX_LOG="+logPath, "ARGV_LOG="+argvPath,
	)
	request := map[string]any{
		"delegation_id": "../delegation-launch", "event_id": "launch-1", "workdir": workdir, "packet_file": packetPath,
		"tmux_session": "Claude", "tmux_window": "thinker", "claude_session_id": "550e8400-e29b-41d4-a716-446655440000",
		"repository": "zainfathoni/amux", "base": base,
		"expected_launch_policy_digest": "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445",
	}
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	if strings.Contains(stdout, packet) {
		t.Fatalf("launch plan exposed packet content: %s", stdout)
	}
	var plan struct {
		PacketDigest        string `json:"packet_digest"`
		LaunchPolicyDigest  string `json:"launch_policy_digest"`
		LaunchCommandDigest string `json:"launch_command_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	binding := testBinding("../delegation-launch")
	binding["workdir"] = workdir
	binding["base"] = base
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	assertHelperOutcomeEnv(t, stateDir, environment, "launched", request, "launch", "execute")
	assertHelperOutcomeEnv(t, stateDir, environment, "duplicate", request, "launch", "execute")
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"new-window", "--permission-mode dontAsk", "--strict-mcp-config", "--tools Read,Grep,Glob", "--disallowed-tools Bash,Edit,Write,NotebookEdit,Agent,WebFetch,WebSearch,Skill", "--setting-sources ''"} {
		if !strings.Contains(string(log), required) {
			t.Errorf("launch command missing %q:\n%s", required, log)
		}
	}
	if strings.Count(string(log), "new-window") != 1 {
		t.Fatalf("exact launch replay created another window:\n%s", log)
	}
	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	encodedArgs := bytes.Split(argvBytes, []byte{0})
	if len(encodedArgs) < 2 || len(encodedArgs[len(encodedArgs)-1]) != 0 {
		t.Fatalf("fake Claude argv is not NUL-delimited: %q", argvBytes)
	}
	encodedArgs = encodedArgs[:len(encodedArgs)-1]
	if got := string(encodedArgs[len(encodedArgs)-1]); got != packet {
		t.Fatalf("multiline packet argv changed:\ngot:  %q\nwant: %q", got, packet)
	}
	var deliveredPacket struct {
		ReportEnvelope map[string]any `json:"report_envelope"`
	}
	if err := json.Unmarshal(encodedArgs[len(encodedArgs)-1], &deliveredPacket); err != nil {
		t.Fatalf("decode packet delivered to fake Claude: %v", err)
	}
	report := deliveredPacket.ReportEnvelope
	report["created_at"] = "2026-07-18T23:30:00Z"
	report["report"] = map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict":   "The self-contained packet supplied immutable report correlation.",
		"rationale": "The strict MCP route accepted the packet-derived envelope without follow-up input.",
		"evidence":  []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification":      []any{"recovered the exact packet argument delivered to fake Claude"},
		"changed_artifacts": []any{}, "references": []any{},
	}
	mcpConfigPath := exactArgValue(t, encodedArgs, "--mcp-config")
	mcpConfigBytes, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		t.Fatalf("read generated MCP config: %v", err)
	}
	var mcpConfig struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpConfigBytes, &mcpConfig); err != nil {
		t.Fatalf("decode generated MCP config: %v", err)
	}
	mcpRoute, ok := mcpConfig.Servers["amux-claude-delegation"]
	if !ok || mcpRoute.Command == "" || len(mcpRoute.Args) == 0 {
		t.Fatalf("generated MCP config has no delegation route: %#v", mcpConfig.Servers)
	}
	wrong := cloneJSONMap(t, report)
	wrong["message_id"] = "wrong-packet-policy-report"
	wrong["launch_policy_digest"] = strings.Repeat("f", 64)
	if response := callMCPReport(t, mcpRoute.Command, mcpRoute.Args, wrong); !strings.Contains(response, `"isError":true`) {
		t.Fatalf("wrong packet policy digest MCP response = %s", response)
	}
	assertReceiptHasNoValidReport(t, stateDir, binding["delegation_id"].(string))
	omitted := cloneJSONMap(t, report)
	omitted["message_id"] = "omitted-packet-policy-report"
	delete(omitted, "launch_policy_digest")
	if response := callMCPReport(t, mcpRoute.Command, mcpRoute.Args, omitted); !strings.Contains(response, `"isError":true`) {
		t.Fatalf("omitted packet policy digest MCP response = %s", response)
	}
	assertReceiptHasNoValidReport(t, stateDir, binding["delegation_id"].(string))
	if response := callMCPReport(t, mcpRoute.Command, mcpRoute.Args, report); !strings.Contains(response, `"outcome":"recorded"`) || strings.Contains(response, `"isError":true`) {
		t.Fatalf("correct packet-derived MCP report response = %s", response)
	}
	assertExactArgValue(t, encodedArgs, "--tools", "Read,Grep,Glob")
	assertExactArgValue(t, encodedArgs, "--allowed-tools", "Read,Grep,Glob,mcp__amux-claude-delegation__submit_report,mcp__amux-claude-delegation__submit_input_request")
	assertExactArgValue(t, encodedArgs, "--disallowed-tools", "Bash,Edit,Write,NotebookEdit,Agent,WebFetch,WebSearch,Skill")
	assertExactArgValue(t, encodedArgs, "--setting-sources", "")
	stored, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(packet)) {
		t.Fatal("receipt persisted complete launch packet content")
	}
	runtimeKey := fmt.Sprintf("%x", sha256.Sum256([]byte("../delegation-launch")))
	runtimeRoot := filepath.Join(stateDir, "runtime")
	runtimeInfo, err := os.Stat(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeInfo.Mode().Perm() != 0o700 {
		t.Errorf("runtime parent mode = %o, want 700", runtimeInfo.Mode().Perm())
	}
	for _, name := range []string{"mcp.json", "settings.json"} {
		path := filepath.Join(stateDir, "runtime", runtimeKey, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("private runtime file %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("private runtime file %s mode = %o, want 600", name, info.Mode().Perm())
		}
	}
}

func callMCPReport(t *testing.T, commandPath string, commandArgs []string, report map[string]any) string {
	t.Helper()
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "submit_report", "arguments": report}},
	}
	var input bytes.Buffer
	for _, message := range messages {
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(data)
		input.WriteByte('\n')
	}
	command := exec.Command(commandPath, commandArgs...)
	command.Stdin = &input
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP server failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 {
		t.Fatalf("MCP responses = %d, want 2\n%s", len(lines), output)
	}
	return lines[1]
}

func assertReceiptHasNoValidReport(t *testing.T, stateDir, delegationID string) {
	t.Helper()
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", delegationID)
	if err != nil {
		t.Fatalf("show receipt after invalid MCP report: %v: %s", err, stderr)
	}
	var receipt struct {
		Events []struct {
			Kind string `json:"kind"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	for _, event := range receipt.Events {
		if event.Kind == "valid_report" {
			t.Fatal("invalid MCP report appended a valid_report event")
		}
	}
}

func assertExactArgValue(t *testing.T, arguments [][]byte, flag, want string) {
	t.Helper()
	if got := exactArgValue(t, arguments, flag); got != want {
		t.Fatalf("%s value = %q, want %q", flag, got, want)
	}
}

func exactArgValue(t *testing.T, arguments [][]byte, flag string) string {
	t.Helper()
	for index, argument := range arguments {
		if string(argument) == flag && index+1 < len(arguments) {
			return string(arguments[index+1])
		}
	}
	t.Fatalf("exact argv is missing %s", flag)
	return ""
}

func TestDiagnosticsClassifySupportedUnavailableAndUntestedCapabilities(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
case "$1" in
  --version) printf '%s\n' '2.1.212 (Claude Code)' ;;
  --help) printf '%s\n' '--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools' ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "tmux"), "#!/bin/sh\nprintf '%s\\n' 'tmux 3.7b'\n")
	writeExecutable(t, filepath.Join(binDir, "codexbar"), `#!/bin/sh
printf '%s\n' '[{"provider":"claude","source":"web","usage":{"primary":{"usedPercent":12,"windowMinutes":300,"resetsAt":"2026-07-17T15:00:00Z"},"secondary":{"usedPercent":34,"windowMinutes":10080,"resetsAt":"2026-07-24T00:00:00Z"},"extraRateWindows":[]}}]'
`)
	environment := append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "diagnose")
	if err != nil {
		t.Fatalf("diagnose: %v: %s", err, stderr)
	}
	for _, required := range []string{`"status":"supported"`, `"status":"unavailable"`, `"status":"untested"`, `"automatic_interactive_input"`, `"strict_mcp_runtime"`, `"window_minutes":300`, `"window_minutes":10080`} {
		if !strings.Contains(stdout, required) {
			t.Errorf("diagnostics missing %q:\n%s", required, stdout)
		}
	}
	for _, forbidden := range []string{"accountEmail", "accountOrganization", "transcript", "prompt"} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("diagnostics leaked forbidden field %q", forbidden)
		}
	}
}

func testBinding(delegationID string) map[string]any {
	return map[string]any{
		"protocol_version":      1,
		"delegation_id":         delegationID,
		"nonce":                 strings.Repeat("a", 64),
		"task_id":               "task-1",
		"question_message_id":   "question-1",
		"origin_thread":         "T-origin",
		"repository":            "zainfathoni/amux",
		"base":                  "0123456789abcdef0123456789abcdef01234567",
		"workdir":               "/tmp/amux-read-only",
		"producer_role":         "thinker",
		"authority":             "read_only",
		"task_reference":        "issue-148-design-review",
		"packet_digest":         strings.Repeat("b", 64),
		"launch_policy_digest":  strings.Repeat("c", 64),
		"launch_command_digest": strings.Repeat("d", 64),
	}
}

func testMessage(binding map[string]any, messageID, kind string, payload map[string]any) map[string]any {
	return map[string]any{
		"protocol_version":     binding["protocol_version"],
		"delegation_id":        binding["delegation_id"],
		"nonce":                binding["nonce"],
		"message_id":           messageID,
		"in_reply_to":          binding["question_message_id"],
		"kind":                 kind,
		"task_id":              binding["task_id"],
		"origin_thread":        binding["origin_thread"],
		"repository":           binding["repository"],
		"base":                 binding["base"],
		"workdir":              binding["workdir"],
		"producer_role":        binding["producer_role"],
		"authority":            binding["authority"],
		"launch_policy_digest": binding["launch_policy_digest"],
		"created_at":           "2026-07-17T12:00:00Z",
		map[string]string{"thinker_report": "report", "input_request": "input_request"}[kind]: payload,
	}
}

func startProcessFixture(t *testing.T, name string, arguments ...string) int {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return 5252
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte("package main\nimport \"time\"\nfunc main() { time.Sleep(time.Minute) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Claude process fixture: %v\n%s", err, output)
	}
	process := exec.Command(binary, arguments...)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})
	return process.Process.Pid
}

func recordTestLaunch(t *testing.T, stateDir, delegationID string, identity map[string]any) {
	t.Helper()
	path := filepath.Join(stateDir, "receipts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store map[string]any
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	receipt := store["receipts"].([]any)[0].(map[string]any)
	if receipt["binding"].(map[string]any)["delegation_id"] != delegationID {
		t.Fatal("test receipt identity mismatch")
	}
	receipt["events"] = append(receipt["events"].([]any),
		map[string]any{"event_id": "launch-fixture", "kind": "launch_intent", "at": "2026-07-17T12:00:00Z"},
		map[string]any{"event_id": "amux:test-launch-result", "kind": "launch_completed", "operation_event_id": "launch-fixture", "identity": identity, "at": "2026-07-17T12:00:01Z"},
	)
	updated, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func assertHelperOutcome(t *testing.T, stateDir, want string, input map[string]any, args ...string) {
	t.Helper()
	stdout, stderr, err := runHelper(t, stateDir, input, args...)
	if err != nil {
		t.Fatalf("helper %s: %v: %s", strings.Join(args, " "), err, stderr)
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode helper result: %v\n%s", err, stdout)
	}
	if result.Outcome != want {
		t.Fatalf("helper %s outcome = %q, want %q", strings.Join(args, " "), result.Outcome, want)
	}
}

func assertHelperOutcomeEnv(t *testing.T, stateDir string, environment []string, want string, input map[string]any, args ...string) {
	t.Helper()
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, input, args...)
	if err != nil {
		t.Fatalf("helper %s: %v: %s", strings.Join(args, " "), err, stderr)
	}
	if !strings.Contains(stdout, `"outcome":"`+want+`"`) {
		t.Fatalf("helper %s output = %s, want outcome %s", strings.Join(args, " "), stdout, want)
	}
}

func runHelper(t *testing.T, stateDir string, input map[string]any, args ...string) (string, string, error) {
	return runHelperEnv(t, stateDir, nil, input, args...)
}

func runHelperEnv(t *testing.T, stateDir string, environment []string, input map[string]any, args ...string) (string, string, error) {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", append([]string{helper, "--state-dir", stateDir}, args...)...)
	if environment != nil {
		command.Env = environment
	}
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	return stdout.String(), stderr.String(), err
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func cloneJSONMap(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	return output
}
