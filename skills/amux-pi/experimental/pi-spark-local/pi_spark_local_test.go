// Package pisparklocal_test exercises the local Pi executor as a black box.
package pisparklocal_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fixture struct {
	helper, state, worktree, packet, pi, auth, target string
}

func TestBoundedSparkExecutionAppliesOnlyExactAllowedReplacement(t *testing.T) {
	f := newFixture(t, "success")
	authBefore := mustRead(t, f.auth)
	foreignMarker := filepath.Join(f.worktree, ".target.txt.amux-pi-success")
	if err := os.WriteFile(foreignMarker, []byte("foreign\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runOK(t, f, "plan", "--packet", f.packet)
	intent := readObject(t, filepath.Join(f.state, "intents", "success.json"))
	argv := intent["argv"].([]any)
	wantArgv := []string{
		filepath.Base(argv[0].(string)), filepath.Base(argv[1].(string)), "--mode", "json",
		"--model", "openai-codex/gpt-5.3-codex-spark", "--thinking", "high",
		"--no-session", "--no-tools", "--no-extensions", "--no-skills",
		"--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve",
		"--system-prompt", "Return only the requested JSON replacement envelope. Do not use tools, files, external context, network publishing, or delegation.",
	}
	if len(argv) != len(wantArgv) {
		t.Fatalf("argv has %d entries, want exact %d", len(argv), len(wantArgv))
	}
	for index, want := range wantArgv {
		got := argv[index].(string)
		if index < 2 {
			got = filepath.Base(got)
		}
		if got != want {
			t.Fatalf("argv[%d]=%q, want %q", index, got, want)
		}
	}
	output := runOK(t, f, "execute", "--operation-id", "success")
	for _, required := range []string{`"provider":"openai-codex"`, `"model":"gpt-5.3-codex-spark"`, `"status":"awaiting_quota_confirmation"`, `"result_trust":"untrusted_pending_coordinator_review_and_validation"`} {
		if !strings.Contains(output, required) {
			t.Errorf("result missing %s: %s", required, output)
		}
	}
	if got := mustRead(t, f.target); got != "after\n" {
		t.Fatalf("target=%q, want replacement", got)
	}
	if got := mustRead(t, f.auth); got != authBefore {
		t.Fatal("shared OAuth credential bytes changed")
	}
	if got := mustRead(t, foreignMarker); got != "foreign\n" {
		t.Fatalf("foreign temporary-name collision marker changed: %q", got)
	}
	receipt := readObject(t, filepath.Join(f.state, "operations", "success.json"))
	process := receipt["process"].(map[string]any)
	for _, field := range []string{"pid", "start_seconds", "start_microseconds", "executable", "executable_identity"} {
		if _, ok := process[field]; !ok {
			t.Errorf("recorded process incarnation is missing %q", field)
		}
	}
	packet := readObject(t, f.packet)
	reset := packet["quota_evidence"].(map[string]any)["reset_at"]
	quotaAfter := filepath.Join(filepath.Dir(f.packet), "quota-after.json")
	writeObject(t, quotaAfter, map[string]any{"route": "chatgpt-codex-oauth-spark", "source_confidence": "trusted", "observed_at": time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano), "reset_at": reset, "usage_increased": true}, 0o600)
	output = runOK(t, f, "finalize", "--operation-id", "success", "--quota-after", quotaAfter)
	if !strings.Contains(output, `"status":"success"`) || !strings.Contains(output, `"billing_route":"chatgpt-codex-oauth-spark"`) {
		t.Fatalf("final result lacks billing proof: %s", output)
	}
}

func TestPreflightRejectsFilesAliasingSharedAuthentication(t *testing.T) {
	for _, target := range []string{"packet", "settings", "allowed"} {
		t.Run(target, func(t *testing.T) {
			f := newFixture(t, "alias-"+target)
			if target == "packet" {
				runBlockedArgs(t, f, "aliases shared authentication", "preflight", "--packet", f.auth)
				return
			}
			path := f.target
			if target == "settings" {
				path = filepath.Join(filepath.Dir(f.auth), "settings.json")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(f.auth, path); err != nil {
				t.Fatal(err)
			}
			runBlocked(t, f, "preflight", "aliases shared authentication")
		})
	}
}

func TestPreflightRejectsWrongModelCatalog(t *testing.T) {
	f := newFixture(t, "wrong-model")
	t.Setenv("FAKE_PI_MODE", "wrong-model")
	runBlocked(t, f, "preflight", "exact Spark model")
}

func TestPreflightRejectsWrongVersionAndTraversalOperationID(t *testing.T) {
	f := newFixture(t, "wrong-version")
	t.Setenv("FAKE_PI_MODE", "wrong-version")
	runBlocked(t, f, "preflight", "supported version")
	t.Setenv("FAKE_PI_MODE", "")
	runBlockedArgs(t, f, "operation_id is invalid", "inspect", "--operation-id", "../outside")
}

func TestPreflightRejectsUnavailableOrAmbiguousAdmissionEvidence(t *testing.T) {
	tests := []struct {
		name, mutate, want string
	}{
		{"auth", "auth", "auth evidence"},
		{"quota", "quota", "quota evidence"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, test.name)
			packet := readObject(t, f.packet)
			if test.mutate == "auth" {
				packet["auth_evidence"].(map[string]any)["type"] = "api_key"
			} else {
				packet["quota_evidence"].(map[string]any)["available"] = false
			}
			writeObject(t, f.packet, packet, 0o600)
			runBlocked(t, f, "preflight", test.want)
		})
	}
}

func TestPreflightRejectsAPIKeyEnvironmentWithoutPrintingValue(t *testing.T) {
	f := newFixture(t, "api-key")
	t.Setenv("OPENAI_API_KEY", "do-not-print-this-secret")
	output := runBlocked(t, f, "preflight", "API-key environment")
	if strings.Contains(output, "do-not-print-this-secret") {
		t.Fatal("diagnostic exposed prohibited credential value")
	}
}

func TestExecutionFailsClosedOnBusyConcurrency(t *testing.T) {
	f := newFixture(t, "busy")
	runOK(t, f, "plan", "--packet", f.packet)
	lockPath := filepath.Join(f.state, "execute.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	runBlockedArgs(t, f, "another local Pi operation is active", "execute", "--operation-id", "busy")
}

func TestExecutionBoundsTimeoutAndOutputOverflow(t *testing.T) {
	for _, test := range []struct{ name, mode, want string }{{"timeout", "sleep", "timed out"}, {"overflow", "overflow", "exceeded its bound"}} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, test.name)
			authBefore := mustRead(t, f.auth)
			packet := readObject(t, f.packet)
			if test.name == "timeout" {
				packet["timeout_seconds"] = float64(1)
			} else {
				packet["stdout_limit"] = float64(1024)
			}
			writeObject(t, f.packet, packet, 0o600)
			runOK(t, f, "plan", "--packet", f.packet)
			t.Setenv("FAKE_PI_MODE", test.mode)
			runBlockedArgs(t, f, test.want, "execute", "--operation-id", test.name)
			if got := mustRead(t, f.target); got != "before\n" {
				t.Fatalf("failed run edited target: %q", got)
			}
			if got := mustRead(t, f.auth); got != authBefore {
				t.Fatal("failed run changed shared OAuth credential bytes")
			}
		})
	}
}

func TestExecutionRejectsChangedExecutableAndOutOfScopeResult(t *testing.T) {
	for _, test := range []struct{ name, mode, want string }{{"changed-executable", "", "immutable execution intent no longer matches"}, {"scope", "scope", "outside the allowed scope"}} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t, test.name)
			runOK(t, f, "plan", "--packet", f.packet)
			if test.name == "changed-executable" {
				if err := os.Chtimes(f.pi, time.Now(), time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("FAKE_PI_MODE", test.mode)
			}
			runBlockedArgs(t, f, test.want, "execute", "--operation-id", test.name)
		})
	}
}

func TestExecutionBindsSettingsWorktreeAndConsumesRejectedAttempt(t *testing.T) {
	t.Run("settings", func(t *testing.T) {
		f := newFixture(t, "settings")
		runOK(t, f, "plan", "--packet", f.packet)
		writeObject(t, filepath.Join(filepath.Dir(f.auth), "settings.json"), map[string]any{"compaction": map[string]any{"enabled": false}, "retry": map[string]any{"enabled": true, "provider": map[string]any{"maxRetries": float64(0)}}}, 0o600)
		runBlockedArgs(t, f, "do not disable agent retries", "execute", "--operation-id", "settings")
	})
	t.Run("worktree", func(t *testing.T) {
		f := newFixture(t, "worktree")
		runOK(t, f, "plan", "--packet", f.packet)
		if err := os.WriteFile(filepath.Join(f.worktree, "unplanned.txt"), []byte("drift\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runBlockedArgs(t, f, "immutable execution intent no longer matches", "execute", "--operation-id", "worktree")
	})
	t.Run("one-attempt", func(t *testing.T) {
		f := newFixture(t, "one-attempt")
		runOK(t, f, "plan", "--packet", f.packet)
		t.Setenv("FAKE_PI_MODE", "scope")
		runBlockedArgs(t, f, "outside the allowed scope", "execute", "--operation-id", "one-attempt")
		runBlockedArgs(t, f, "exact planned state", "execute", "--operation-id", "one-attempt")
	})
}

func TestExecutionRejectsRetryEventAndWillRetryCompletion(t *testing.T) {
	for _, mode := range []string{"retry-event", "will-retry"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, mode)
			runOK(t, f, "plan", "--packet", f.packet)
			t.Setenv("FAKE_PI_MODE", mode)
			runBlockedArgs(t, f, "retry", "execute", "--operation-id", mode)
			receipt := readObject(t, filepath.Join(f.state, "operations", mode+".json"))
			if receipt["status"] != "blocked" {
				t.Fatalf("rejected protocol status=%v, want blocked", receipt["status"])
			}
		})
	}
}

func TestExecutionRejectsToolContentAndOutOfOrderLifecycle(t *testing.T) {
	for _, mode := range []string{"tool-content", "out-of-order"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, mode)
			runOK(t, f, "plan", "--packet", f.packet)
			t.Setenv("FAKE_PI_MODE", mode)
			runBlockedArgs(t, f, "Pi", "execute", "--operation-id", mode)
			if got := mustRead(t, f.target); got != "before\n" {
				t.Fatalf("rejected provenance edited target: %q", got)
			}
		})
	}
}

func TestRecoveryRefusesChangedLiveIdentityAndRecordsExactAbsence(t *testing.T) {
	f := newFixture(t, "recover")
	authBefore := mustRead(t, f.auth)
	runOK(t, f, "plan", "--packet", f.packet)
	receiptPath := filepath.Join(f.state, "operations", "recover.json")
	receipt := readObject(t, receiptPath)
	receipt["status"] = "running"
	receipt["process"] = map[string]any{"pid": float64(os.Getpid()), "ps": "not-the-current-process"}
	writeObject(t, receiptPath, receipt, 0o600)
	runBlockedArgs(t, f, "identity changed", "recover", "--operation-id", "recover")
	receipt["process"] = map[string]any{"pid": float64(99999999), "ps": "absent"}
	writeObject(t, receiptPath, receipt, 0o600)
	output := runOK(t, f, "recover", "--operation-id", "recover")
	if !strings.Contains(output, `"status":"indeterminate"`) || !strings.Contains(output, "semantic completion unknown") {
		t.Fatalf("absence recovery=%s", output)
	}
	if got := mustRead(t, f.auth); got != authBefore {
		t.Fatal("recovery changed shared OAuth credential bytes")
	}
}

func newFixture(t *testing.T, operation string) fixture {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("exact process incarnation implementation is Darwin-only")
	}
	root := t.TempDir()
	worktree := filepath.Join(root, "worktree")
	mustRun(t, root, "git", "init", "-b", "feature-test", worktree)
	mustRun(t, worktree, "git", "config", "user.email", "test@example.invalid")
	mustRun(t, worktree, "git", "config", "user.name", "Test")
	target := filepath.Join(worktree, "target.txt")
	if err := os.WriteFile(target, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, worktree, "git", "add", "target.txt")
	mustRun(t, worktree, "git", "commit", "-m", "fixture")
	agent := filepath.Join(root, "agent")
	if err := os.Mkdir(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	auth := filepath.Join(agent, "auth.json")
	if err := os.WriteFile(auth, []byte(`{"opaque":"credential-bytes-must-remain-untouched"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := filepath.EvalSymlinks(auth)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Dir(auth))
	settings := map[string]any{"compaction": map[string]any{"enabled": false}, "retry": map[string]any{"enabled": false, "maxRetries": float64(0), "provider": map[string]any{"maxRetries": float64(0)}}}
	writeObject(t, filepath.Join(agent, "settings.json"), settings, 0o600)
	pi := filepath.Join(root, "pi")
	if err := os.WriteFile(pi, []byte("// synthetic Pi CLI fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(root, "node")
	source := filepath.Join(root, "fake-node.c")
	program := `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
int has(int argc, char **argv, const char *value) { for (int i=1;i<argc;i++) if (!strcmp(argv[i], value)) return 1; return 0; }
int main(int argc, char **argv) {
  const char *mode = getenv("FAKE_PI_MODE"); if (!mode) mode = "";
  if (argc == 3 && !strcmp(argv[2], "--version")) { usleep(150000); puts(!strcmp(mode,"wrong-version") ? "0.81.0" : "0.80.10"); return 0; }
  if (argc >= 3 && !strcmp(argv[2], "--list-models")) { usleep(150000); puts(!strcmp(mode,"wrong-model") ? "openai gpt-5.3-codex-spark" : "openai-codex gpt-5.3-codex-spark text 1 1"); return 0; }
  const char *expected[] = {"--mode","json","--model","openai-codex/gpt-5.3-codex-spark","--thinking","high","--no-session","--no-tools","--no-extensions","--no-skills","--no-prompt-templates","--no-themes","--no-context-files","--no-approve","--system-prompt","Return only the requested JSON replacement envelope. Do not use tools, files, external context, network publishing, or delegation."};
  if (argc != 18) return 42; for (int i=2;i<18;i++) if (strcmp(argv[i],expected[i-2])) return 42;
  if (!strcmp(mode,"sleep")) { sleep(10); return 0; }
  char input[4096]; while (fread(input,1,sizeof(input),stdin) > 0) {}
  if (!strcmp(mode,"overflow")) { while (1) { for (int i=0;i<1024;i++) putchar('x'); fflush(stdout); } }
  char cwd[4096]; if (!getcwd(cwd,sizeof(cwd))) return 43;
  printf("{\"type\":\"session\",\"version\":3,\"cwd\":\"%s\",\"id\":\"fixture\",\"timestamp\":\"fixture\"}\n", cwd);
  puts("{\"type\":\"agent_start\"}");
  if (!strcmp(mode,"retry-event")) puts("{\"type\":\"auto_retry_start\"}");
  if (!strcmp(mode,"out-of-order")) puts("{\"type\":\"agent_end\",\"willRetry\":false}");
  if (!strcmp(mode,"scope")) puts("{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"provider\":\"openai-codex\",\"model\":\"gpt-5.3-codex-spark\",\"stopReason\":\"stop\",\"content\":[{\"type\":\"text\",\"text\":\"{\\\"summary\\\":\\\"bad\\\",\\\"files\\\":[{\\\"path\\\":\\\"outside.txt\\\",\\\"content\\\":\\\"bad\\\"}]}\"}]}}");
  else if (!strcmp(mode,"tool-content")) puts("{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"provider\":\"openai-codex\",\"model\":\"gpt-5.3-codex-spark\",\"stopReason\":\"stop\",\"content\":[{\"type\":\"text\",\"text\":\"{}\"},{\"type\":\"toolCall\",\"name\":\"forbidden\"}]}}");
  else puts("{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"provider\":\"openai-codex\",\"model\":\"gpt-5.3-codex-spark\",\"stopReason\":\"stop\",\"content\":[{\"type\":\"text\",\"text\":\"{\\\"summary\\\":\\\"tiny useful edit\\\",\\\"files\\\":[{\\\"path\\\":\\\"target.txt\\\",\\\"content\\\":\\\"after\\\\n\\\"}]}\"}]}}");
  if (strcmp(mode,"out-of-order")) puts(!strcmp(mode,"will-retry") ? "{\"type\":\"agent_end\",\"willRetry\":true}" : "{\"type\":\"agent_end\",\"willRetry\":false}"); puts("{\"type\":\"agent_settled\"}"); return 0;
}`
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, root, "cc", "-O2", "-o", node, source)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	now := time.Now().UTC()
	info, err := os.Stat(auth)
	if err != nil {
		t.Fatal(err)
	}
	statInfo := info.Sys().(*syscall.Stat_t)
	packetObject := map[string]any{
		"schema": float64(1), "operation_id": operation, "owner_authorized": true,
		"goal": "Change the fixture text from before to after.", "workdir": worktree,
		"allowed_paths": []any{"target.txt"}, "expected_output": "One exact replacement.",
		"validation_commands": []any{"git diff --check", "git diff -- target.txt"},
		"exclusions":          []any{"amp_thread_read", "cross_worker_communication", "recursive_delegation", "network_publish", "pr", "push", "merge", "release", "install", "cleanup", "teardown"},
		"timeout_seconds":     float64(5), "stdout_limit": float64(65536), "stderr_limit": float64(16384), "event_limit": float64(20),
		"auth_evidence":  map[string]any{"provider": "openai-codex", "type": "oauth", "source": "owner-confirmed-metadata", "observed_at": now.Format(time.RFC3339), "path": auth, "identity": map[string]any{"path": auth, "device": fmt.Sprint(statInfo.Dev), "inode": fmt.Sprint(statInfo.Ino), "mode": float64(0o600), "size": float64(info.Size()), "mtime_ns": fmt.Sprint(info.ModTime().UnixNano())}},
		"quota_evidence": map[string]any{"route": "chatgpt-codex-oauth-spark", "source_confidence": "trusted", "observed_at": now.Format(time.RFC3339), "reset_at": now.Add(time.Hour).Format(time.RFC3339), "available": true},
	}
	packet := filepath.Join(root, "packet.json")
	writeObject(t, packet, packetObject, 0o600)
	helper, err := filepath.Abs("pi_spark_local.py")
	if err != nil {
		t.Fatal(err)
	}
	return fixture{helper: helper, state: filepath.Join(root, "state"), worktree: worktree, packet: packet, pi: pi, auth: auth, target: target}
}

func runOK(t *testing.T, f fixture, args ...string) string {
	t.Helper()
	output, code := run(t, f, args...)
	if code != 0 {
		t.Fatalf("command failed (%d): %s", code, output)
	}
	return output
}
func runBlocked(t *testing.T, f fixture, command, want string) string {
	t.Helper()
	return runBlockedArgs(t, f, want, command, "--packet", f.packet)
}
func runBlockedArgs(t *testing.T, f fixture, want string, args ...string) string {
	t.Helper()
	output, code := run(t, f, args...)
	if code != 2 || !strings.Contains(output, want) {
		t.Fatalf("blocked command code=%d output=%q want=%q", code, output, want)
	}
	return output
}
func run(t *testing.T, f fixture, args ...string) (string, int) {
	t.Helper()
	base := []string{f.helper, "--state-dir", f.state, "--pi", f.pi}
	cmd := exec.Command("python3", append(base, args...)...)
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err == nil {
		return string(output), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return string(output), exit.ExitCode()
	}
	t.Fatal(err)
	return "", -1
}
func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", name, err, output)
	}
}
func mustRead(t *testing.T, path string) string {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(value)
}
func readObject(t *testing.T, path string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(mustRead(t, path)), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func writeObject(t *testing.T, path string, value map[string]any, mode os.FileMode) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), mode); err != nil {
		t.Fatal(err)
	}
}
