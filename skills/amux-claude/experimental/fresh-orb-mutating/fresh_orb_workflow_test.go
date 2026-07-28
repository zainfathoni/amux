package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type orbFixture struct {
	root, worktree, state, packet, artifact, base, bindingDigest string
	binding                                                      map[string]any
}

func command(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func helper(t *testing.T, state string, request map[string]any, args ...string) (string, string, error) {
	t.Helper()
	path, err := filepath.Abs("fresh_orb_workflow.py")
	if err != nil {
		t.Fatal(err)
	}
	argv := []string{path}
	if state != "" {
		argv = append(argv, "--state-dir", state)
	}
	argv = append(argv, args...)
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python3", argv...)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return stdout.String(), stderr.String(), err
}

func mustHelper(t *testing.T, state string, request map[string]any, args ...string) map[string]any {
	t.Helper()
	stdout, stderr, err := helper(t, state, request, args...)
	if err != nil {
		t.Fatalf("helper %v: %v: %s%s", args, err, stdout, stderr)
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func hash(value string) string {
	result := sha256.Sum256([]byte(value))
	return hex.EncodeToString(result[:])
}

func newOrbFixture(t *testing.T) *orbFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "source")
	worktree := filepath.Join(t.TempDir(), "orb-worktree")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	command(t, root, "git", "init", "-q", "-b", "main")
	command(t, root, "git", "config", "user.name", "Synthetic Test")
	command(t, root, "git", "config", "user.email", "synthetic@example.invalid")
	command(t, root, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command(t, root, "git", "add", "base.txt")
	command(t, root, "git", "commit", "-q", "-m", "base")
	base := command(t, root, "git", "rev-parse", "HEAD")
	command(t, root, "git", "worktree", "add", "-q", "-b", "delegate", worktree, base)
	binding := map[string]any{
		"operation_id":       "synthetic-op",
		"origin_thread":      "T-origin-synthetic",
		"orb_thread":         "T-orb-synthetic",
		"repository":         "example.invalid/owner/repository",
		"base_sha":           base,
		"branch":             "delegate",
		"worktree_id":        hash("dedicated-worktree"),
		"task_sha256":        hash("bounded-task"),
		"executable_path":    "/opt/provider/bin/claude",
		"executable_version": "1.2.3",
		"argv": []any{
			"claude", "--print", "--model", "claude-opus-4-8", "--output-format", "json",
			"--permission-mode", "dontAsk", "--max-turns", "8", "--no-session-persistence", "--safe-mode",
			"--disable-slash-commands", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
			"--tools", "Read,Grep,Glob,Edit,Write,Bash", "--allowedTools", "Read,Grep,Glob,Edit,Write,Bash",
			"--disallowedTools", "Agent,WebFetch,WebSearch,mcp__*",
		},
		"model":         "claude-opus-4-8",
		"auth_route":    "project_secret_oauth_first_party",
		"execution_id":  hash("execution"),
		"child_limit":   1,
		"depth":         0,
		"attempt_limit": 1,
		"check_argv":    []any{[]any{"git", "diff", "--check", "HEAD^", "HEAD"}},
		"allowed_paths": []any{"result.txt"},
		"authority": map[string]any{
			"mutation":    "one_bounded_commit_in_dedicated_worktree",
			"integration": "amp_coordinator_only",
			"forbidden": []any{
				"push", "pull_request", "merge", "release", "issue_mutation", "secret_management",
				"infrastructure", "archive", "cleanup", "recursive_delegation",
			},
		},
	}
	state := filepath.Join(t.TempDir(), "coordinator-state")
	created := mustHelper(t, state, map[string]any{"event_id": "intent-1", "binding": binding}, "intent")
	return &orbFixture{
		root: root, worktree: worktree, state: state, base: base, binding: binding,
		packet: created["packet"].(string), bindingDigest: created["binding_sha256"].(string),
	}
}

func (fixture *orbFixture) event(eventID string) map[string]any {
	return map[string]any{
		"operation_id": fixture.binding["operation_id"], "binding_sha256": fixture.bindingDigest,
		"event_id": eventID,
	}
}

func admitAndLaunch(t *testing.T, fixture *orbFixture) {
	t.Helper()
	for _, name := range []string{"cli_support", "authentication", "entitlement", "availability", "capacity", "charge_route"} {
		request := fixture.event("capability-" + name)
		request["name"], request["outcome"], request["evidence"] = name, "pass", "known"
		request["known_floor_failure"] = false
		mustHelper(t, fixture.state, request, "capability")
	}
	mustHelper(t, fixture.state, fixture.event("authorize-1"), "authorize")
	launch := fixture.event("launch-1")
	launch["attempt"], launch["launch_sha256"] = 1, hash("launch")
	launch["launch_identity"] = map[string]any{
		"orb_thread": fixture.binding["orb_thread"], "execution_id": fixture.binding["execution_id"],
		"executable_path":    fixture.binding["executable_path"],
		"executable_version": fixture.binding["executable_version"],
		"argv_sha256":        hashJSON(t, fixture.binding["argv"]), "model": "claude-opus-4-8",
	}
	mustHelper(t, fixture.state, launch, "launch")
}

func hashJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	// Python canonical JSON has no whitespace and preserves list order.
	result := sha256.Sum256(encoded)
	return hex.EncodeToString(result[:])
}

func commitResult(t *testing.T, fixture *orbFixture) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(fixture.worktree, "result.txt"), []byte("bounded result\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command(t, fixture.worktree, "git", "add", "result.txt")
	command(t, fixture.worktree, "git", "commit", "-q", "-m", "bounded result")
	return command(t, fixture.worktree, "git", "rev-parse", "HEAD")
}

func exportResult(t *testing.T, fixture *orbFixture, outcome string) map[string]any {
	t.Helper()
	fixture.artifact = filepath.Join(t.TempDir(), "handoff")
	exported := mustHelper(t, "", map[string]any{
		"packet": fixture.packet, "binding_sha256": fixture.bindingDigest,
		"model_usage": map[string]any{"claude-opus-4-8": map[string]any{}},
		"worktree":    fixture.worktree, "outcome": outcome, "output": fixture.artifact,
		"report_sha256": hash("bounded semantic report"),
		"blocker_sha256": func() any {
			if outcome == "blocked" {
				return hash("bounded blocker")
			}
			return nil
		}(),
	}, "export")
	semantic := fixture.event("semantic-1")
	semantic["handoff"], semantic["commit"] = outcome, exported["commit"]
	semantic["artifact_sha256"] = exported["artifact_sha256"]
	semantic["report_sha256"] = hash("bounded semantic report")
	if outcome == "blocked" {
		semantic["blocker_sha256"] = hash("bounded blocker")
	}
	mustHelper(t, fixture.state, semantic, "semantic")
	transfer := fixture.event("transfer-1")
	transfer["artifact"], transfer["transfer_sha256"] = fixture.artifact, hash("native Amp file transfer")
	mustHelper(t, fixture.state, transfer, "transfer")
	return exported
}

func verifyResult(t *testing.T, fixture *orbFixture, eventID string) map[string]any {
	t.Helper()
	request := fixture.event(eventID)
	request["artifact"], request["base_repository"] = fixture.artifact, fixture.root
	return mustHelper(t, fixture.state, request, "verify")
}

func TestFreshOrbExactModelIdentityAndPrivacyGates(t *testing.T) {
	for _, model := range []any{nil, "opus", "claude_opus_4_8", "claude-opus-4-7"} {
		fixture := newOrbFixture(t)
		if model == nil {
			delete(fixture.binding, "model")
		} else {
			fixture.binding["model"] = model
		}
		_, stderr, err := helper(t, t.TempDir(), map[string]any{"event_id": "wrong", "binding": fixture.binding}, "intent")
		if err == nil || (model != nil && !strings.Contains(stderr, "claude-opus-4-8")) {
			t.Fatalf("model %v accepted: %v %s", model, err, stderr)
		}
	}
	fixture := newOrbFixture(t)
	for _, field := range []string{"base_sha", "worktree_id", "orb_thread"} {
		request := fixture.event("identity-" + field)
		request["binding_sha256"] = hash("changed-" + field)
		request["name"], request["outcome"], request["evidence"] = "cli_support", "pass", "known"
		_, _, err := helper(t, fixture.state, request, "capability")
		if err == nil {
			t.Fatalf("changed %s identity accepted", field)
		}
	}
	private := map[string]any{"event_id": "private", "binding": fixture.binding, "prompt": "must not persist"}
	if _, _, err := helper(t, t.TempDir(), private, "intent"); err == nil {
		t.Fatal("privacy-sensitive launch input accepted")
	}
}

func TestFreshOrbAdmissionReplayAndInterruptedLaunch(t *testing.T) {
	fixture := newOrbFixture(t)
	request := fixture.event("capability-cli")
	request["name"], request["outcome"], request["evidence"] = "cli_support", "pass", "known"
	if got := mustHelper(t, fixture.state, request, "capability")["outcome"]; got != "recorded" {
		t.Fatalf("first capability = %v", got)
	}
	if got := mustHelper(t, fixture.state, request, "capability")["outcome"]; got != "duplicate" {
		t.Fatalf("replay capability = %v", got)
	}
	request["outcome"] = "blocked"
	if _, _, err := helper(t, fixture.state, request, "capability"); err == nil {
		t.Fatal("conflicting replay accepted")
	}
	if _, _, err := helper(t, fixture.state, fixture.event("early-authorize"), "authorize"); err == nil {
		t.Fatal("incomplete preflight authorized")
	}
	// The coordinator receipt exists durably while no launch event exists: this is
	// the recoverable interrupted-launch state and cannot create a second intent.
	if _, _, err := helper(t, fixture.state, map[string]any{"event_id": "intent-2", "binding": fixture.binding}, "intent"); err == nil {
		t.Fatal("second attempt intent accepted")
	}
}

func TestFreshOrbCompleteTransferVerificationAndLifecycle(t *testing.T) {
	fixture := newOrbFixture(t)
	admitAndLaunch(t, fixture)
	commit := commitResult(t, fixture)
	exported := exportResult(t, fixture, "complete")
	if exported["commit"] != commit {
		t.Fatalf("exported commit %v, want %s", exported["commit"], commit)
	}
	verified := verifyResult(t, fixture, "verify-1")
	if verified["handoff"] != "complete" {
		t.Fatalf("verification = %#v", verified)
	}
	if got := verifyResult(t, fixture, "verify-1")["outcome"]; got != "duplicate" {
		t.Fatalf("verification replay = %v", got)
	}
	checks := fixture.event("checks-1")
	checks["results"] = []any{map[string]any{
		"argv_sha256": hashJSON(t, []any{"git", "diff", "--check", "HEAD^", "HEAD"}), "status": 0,
	}}
	checks["verifier_sha256"], checks["evidence_sha256"] = hash("isolated verifier"), hash("check evidence")
	mustHelper(t, fixture.state, checks, "checks")
	absence := fixture.event("absence-1")
	absence["evidence_sha256"] = hash("headless process terminated after bounded timeout")
	mustHelper(t, fixture.state, absence, "process-absence")
	delivery := fixture.event("delivery-1")
	delivery["delivery_sha256"] = hash("native file transfer receipt")
	mustHelper(t, fixture.state, delivery, "deliver")
	if got := mustHelper(t, fixture.state, delivery, "deliver")["outcome"]; got != "duplicate" {
		t.Fatalf("delivery replay = %v", got)
	}
	notification := fixture.event("notification-1")
	notification["status"], notification["notification_sha256"] = "failure", hash("notification failed")
	mustHelper(t, fixture.state, notification, "notify")

	identity := map[string]any{
		"orb_thread": fixture.binding["orb_thread"], "repository": fixture.binding["repository"],
		"branch": fixture.binding["branch"], "worktree_id": fixture.binding["worktree_id"],
	}
	earlyArchive := fixture.event("archive-too-early")
	earlyArchive["fresh_identity"] = identity
	if _, _, err := helper(t, fixture.state, earlyArchive, "authorize-archive"); err == nil {
		t.Fatal("archive before acknowledgement accepted")
	}
	earlyCleanup := fixture.event("cleanup-too-early")
	earlyCleanup["fresh_identity"] = identity
	if _, _, err := helper(t, fixture.state, earlyCleanup, "authorize-cleanup"); err == nil {
		t.Fatal("cleanup before acknowledgement accepted")
	}
	ack := fixture.event("ack-1")
	ack["ack_sha256"] = hash("owner acknowledgement")
	mustHelper(t, fixture.state, ack, "acknowledge")
	if got := mustHelper(t, fixture.state, ack, "acknowledge")["outcome"]; got != "duplicate" {
		t.Fatalf("ack replay = %v", got)
	}
	observation := map[string]any{"evidence_sha256": hash("fresh state"), "head_sha": commit, "clean": true}
	archive := fixture.event("archive-authority")
	archive["fresh_identity"], archive["fresh_observation"] = identity, observation
	mustHelper(t, fixture.state, archive, "authorize-archive")
	archiveResult := fixture.event("archive-result")
	archiveResult["status"], archiveResult["authorization_event_id"] = "success", "archive-authority"
	archiveResult["result_sha256"] = hash("archive result")
	mustHelper(t, fixture.state, archiveResult, "archive-result")
	cleanup := fixture.event("cleanup-authority")
	cleanup["fresh_identity"], cleanup["fresh_observation"] = identity, observation
	mustHelper(t, fixture.state, cleanup, "authorize-cleanup")
	cleanupResult := fixture.event("cleanup-result")
	cleanupResult["status"], cleanupResult["failure_sha256"] = "failure", hash("bounded cleanup failure")
	cleanupResult["authorization_event_id"], cleanupResult["result_sha256"] = "cleanup-authority", hash("cleanup result")
	mustHelper(t, fixture.state, cleanupResult, "cleanup-result")
	retryCleanup := fixture.event("cleanup-authority-2")
	retryCleanup["fresh_identity"], retryCleanup["fresh_observation"] = identity, observation
	mustHelper(t, fixture.state, retryCleanup, "authorize-cleanup")
	retryResult := fixture.event("cleanup-result-2")
	retryResult["status"], retryResult["authorization_event_id"] = "success", "cleanup-authority-2"
	retryResult["result_sha256"] = hash("cleanup retry result")
	mustHelper(t, fixture.state, retryResult, "cleanup-result")
	stdout, stderr, err := helper(t, fixture.state, map[string]any{}, "show", "--operation-id", "synthetic-op")
	if err != nil || !strings.Contains(stdout, `"durable_success":false`) ||
		!strings.Contains(stdout, `"durable_success":true`) || strings.Contains(stdout, `"parking":true`) {
		t.Fatalf("durable lifecycle receipt: %v: %s%s", err, stdout, stderr)
	}
}

func TestFreshOrbBlockedAndReceiptSurviveOrbAbsence(t *testing.T) {
	fixture := newOrbFixture(t)
	admitAndLaunch(t, fixture)
	exportResult(t, fixture, "blocked")
	verified := verifyResult(t, fixture, "verify-blocked")
	if verified["handoff"] != "blocked" {
		t.Fatalf("blocked verification = %#v", verified)
	}
	// Rename, rather than clean, the synthetic Orb workspace. Coordinator state
	// and the transferred zero-commit evidence remain independently available.
	absent := fixture.worktree + "-orb-absent"
	if err := os.Rename(fixture.worktree, absent); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := helper(t, fixture.state, map[string]any{}, "show", "--operation-id", "synthetic-op")
	if err != nil || !strings.Contains(stdout, `"handoff_verified"`) {
		t.Fatalf("receipt did not survive Orb absence: %v: %s", err, stdout)
	}
}

func TestFreshOrbRejectsDirtyDivergentAndCorruptTransfer(t *testing.T) {
	t.Run("dirty", func(t *testing.T) {
		fixture := newOrbFixture(t)
		if err := os.WriteFile(filepath.Join(fixture.worktree, "result.txt"), []byte("dirty\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		fixture.artifact = filepath.Join(t.TempDir(), "dirty-artifact")
		_, stderr, err := helper(t, "", map[string]any{
			"packet": fixture.packet, "binding_sha256": fixture.bindingDigest,
			"model_usage": map[string]any{"claude-opus-4-8": map[string]any{}},
			"worktree":    fixture.worktree, "outcome": "complete", "output": fixture.artifact,
		}, "export")
		if err == nil || !strings.Contains(stderr, "dirty") {
			t.Fatalf("dirty handoff accepted: %v %s", err, stderr)
		}
	})
	t.Run("multiple commits", func(t *testing.T) {
		fixture := newOrbFixture(t)
		commitResult(t, fixture)
		if err := os.WriteFile(filepath.Join(fixture.worktree, "result.txt"), []byte("second\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		command(t, fixture.worktree, "git", "commit", "-qam", "second")
		_, _, err := helper(t, "", map[string]any{
			"packet": fixture.packet, "binding_sha256": fixture.bindingDigest,
			"model_usage": map[string]any{"claude-opus-4-8": map[string]any{}},
			"worktree":    fixture.worktree, "outcome": "complete", "output": filepath.Join(t.TempDir(), "artifact"),
		}, "export")
		if err == nil {
			t.Fatal("multi-commit handoff accepted")
		}
	})
	t.Run("corrupt bundle", func(t *testing.T) {
		fixture := newOrbFixture(t)
		admitAndLaunch(t, fixture)
		commitResult(t, fixture)
		exportResult(t, fixture, "complete")
		bundle := filepath.Join(fixture.artifact, "result.bundle")
		file, err := os.OpenFile(bundle, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString("corruption"); err != nil {
			t.Fatal(err)
		}
		file.Close()
		request := fixture.event("verify-corrupt")
		request["artifact"], request["base_repository"] = fixture.artifact, fixture.root
		if _, _, err := helper(t, fixture.state, request, "verify"); err == nil {
			t.Fatal("corrupt transfer accepted")
		}
	})
}
