package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fixture struct {
	root, workdir, pi, node, target string
}

func TestPinnedPackageContract(t *testing.T) {
	if packageVersion != "0.82.1" {
		t.Fatalf("packageVersion=%q, want 0.82.1", packageVersion)
	}
	if packageEngine != ">=22.19.0" {
		t.Fatalf("packageEngine=%q, want >=22.19.0", packageEngine)
	}
}

func TestAppliesOneExactReplacementInPrintMode(t *testing.T) {
	f := newFixture(t)
	leaderPIDFile := filepath.Join(f.root, "leader.pid")
	t.Setenv("FAKE_PI_LEADER_PID_FILE", leaderPIDFile)
	before := mustRead(t, f.target)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply)+"\n")
	var output bytes.Buffer
	if err := run(f.args("--task", "make the requested tiny edit"), &output); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, f.target)); got != "after\n" {
		t.Fatalf("replacement=%q", got)
	}
	var got result
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "replacement_applied_untrusted" || got.RequestedModel != model || got.Version != packageVersion || got.Stderr != "empty" {
		t.Fatalf("result=%+v", got)
	}
	wantArgs := append([]string{mustCanonical(t, f.node), "--import", nodeEngineGuard, mustCanonical(t, f.pi)}, fixedArgs...)
	if strings.Join(got.Argv, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("argv=%q, want %q", got.Argv, wantArgs)
	}
	leaderPID, err := strconv.Atoi(strings.TrimSpace(string(mustRead(t, leaderPIDFile))))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(leaderPID, 0); err == nil {
		t.Fatalf("ordinary-completion leader %d remains live after Wait", leaderPID)
	}
}

func TestPromptUsesBoundedStdinAndFixedArgv(t *testing.T) {
	f := newFixture(t)
	stdinPath := filepath.Join(f.root, "pi.stdin")
	t.Setenv("FAKE_PI_STDIN_FILE", stdinPath)
	before := mustRead(t, f.target)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	var output bytes.Buffer
	if err := run(f.args("--task", "stdin-only task"), &output); err != nil {
		t.Fatal(err)
	}
	wantPrompt, err := buildPrompt("stdin-only task", "target.txt", digest(before), before)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, stdinPath); !bytes.Equal(got, wantPrompt) {
		t.Fatalf("stdin packet mismatch: got %d bytes, want %d", len(got), len(wantPrompt))
	}
	var got result
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Argv) != 4+len(fixedArgs) || got.Argv[len(got.Argv)-1] != "-p" {
		t.Fatalf("argv contains prompt or missing fixed switches: %q", got.Argv)
	}
}

func TestPromptBounds(t *testing.T) {
	t.Run("task", func(t *testing.T) {
		f := newFixture(t)
		err := run(f.args("--task", strings.Repeat("x", maxTaskBytes+1)), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "task exceeds") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("generated packet", func(t *testing.T) {
		contents := bytes.Repeat([]byte{0}, maxInputBytes)
		prompt, err := buildPrompt(strings.Repeat("\x00", maxTaskBytes), "target.txt", strings.Repeat("a", 64), contents)
		if err != nil || len(prompt) > maxPromptBytes {
			t.Fatalf("worst-case admitted packet: bytes=%d err=%v", len(prompt), err)
		}
		encodedReply := marshal(t, replacement{Path: "target.txt", OriginalSHA256: strings.Repeat("a", 64), Replacement: string(contents)})
		if len(encodedReply) > 512<<10 {
			t.Fatalf("worst-case admitted response exceeds default stdout bound: %d", len(encodedReply))
		}
	})
}

func TestExpectedReplacementSHA256Gate(t *testing.T) {
	t.Run("matching bytes apply as expected", func(t *testing.T) {
		f := newFixture(t)
		before := mustRead(t, f.target)
		replacementBytes := []byte("after\n")
		reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: string(replacementBytes)}
		t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
		var output bytes.Buffer
		if err := run(f.args("--task", "edit", "--expected-replacement-sha256", digest(replacementBytes)), &output); err != nil {
			t.Fatal(err)
		}
		var got result
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != "replacement_applied_expected" || got.ExpectedReplacementSHA256 != digest(replacementBytes) {
			t.Fatalf("result=%+v", got)
		}
		if !bytes.Equal(mustRead(t, f.target), replacementBytes) {
			t.Fatal("matching replacement was not applied byte-for-byte")
		}
	})

	t.Run("trailing newline mismatch does not apply", func(t *testing.T) {
		f := newFixture(t)
		before := mustRead(t, f.target)
		reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after"}
		t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
		err := run(f.args("--task", "edit", "--expected-replacement-sha256", digest([]byte("after\n"))), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "does not match expected SHA-256") {
			t.Fatalf("err=%v", err)
		}
		if !bytes.Equal(mustRead(t, f.target), before) {
			t.Fatal("mismatched replacement changed target")
		}
		if status, statusErr := git(f.workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all"); statusErr != nil || len(status) != 0 {
			t.Fatalf("mismatched replacement left worktree changes: %q, %v", status, statusErr)
		}
	})

	for _, malformed := range []string{strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		t.Run("malformed expected hash", func(t *testing.T) {
			f := newFixture(t)
			err := run(f.args("--task", "edit", "--expected-replacement-sha256", malformed), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "exactly 64 hexadecimal") {
				t.Fatalf("hash=%q err=%v", malformed, err)
			}
		})
	}
}

func TestRejectsAPIKeyRoutesBeforeLaunch(t *testing.T) {
	for _, name := range []string{"OPENAI_API_KEY", "CODEX_API_KEY"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			t.Setenv(name, "fixture-not-a-secret")
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), name+" is present") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestRejectsExactParentPath(t *testing.T) {
	f := newFixture(t)
	err := run(f.args("--file", "..", "--task", "edit"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "canonical relative path") {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectsWrongPackageIdentityBeforeLaunch(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(p map[string]any) { p["name"] = "other" },
		func(p map[string]any) { p["version"] = "0.82.0" },
		func(p map[string]any) { p["bin"] = map[string]string{"pi": "dist/other.js"} },
		func(p map[string]any) { p["engines"] = map[string]string{"node": ">=22.18.0"} },
	} {
		f := newFixture(t)
		path := filepath.Join(filepath.Dir(filepath.Dir(f.pi)), "package.json")
		var pkg map[string]any
		if err := json.Unmarshal(mustRead(t, path), &pkg); err != nil {
			t.Fatal(err)
		}
		mutate(pkg)
		mustWrite(t, path, []byte(marshal(t, pkg)), 0o600)
		if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "package") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestNodeEngineGuardRunsBeforePiEntrypoint(t *testing.T) {
	for _, version := range []string{"22.18.9", "22.19", "22.19.0-pre", "malformed"} {
		t.Run("reject "+version, func(t *testing.T) {
			f := newFixture(t)
			marker := filepath.Join(f.root, "pi-reached")
			t.Setenv("FAKE_NODE_VERSION", version)
			t.Setenv("FAKE_PI_REACHED_FILE", marker)
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "Pi exited with code") {
				t.Fatalf("version=%q err=%v", version, err)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("version=%q reached Pi entrypoint: %v", version, statErr)
			}
		})
	}
	for _, version := range []string{"22.19.0", "22.20.1", "23.0.0"} {
		t.Run("accept "+version, func(t *testing.T) {
			f := newFixture(t)
			t.Setenv("FAKE_NODE_VERSION", version)
			before := mustRead(t, f.target)
			t.Setenv("FAKE_PI_OUTPUT", marshal(t, replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}))
			if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err != nil {
				t.Fatalf("version=%q err=%v", version, err)
			}
		})
	}
}

func TestRejectsOversizedPackageMetadataBeforeRead(t *testing.T) {
	f := newFixture(t)
	metadata := filepath.Join(filepath.Dir(filepath.Dir(f.pi)), "package.json")
	mustWrite(t, metadata, bytes.Repeat([]byte(" "), (1<<20)+1), 0o600)
	err := run(f.args("--task", "edit"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "oversized") {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectsChangedExecutableIdentityAfterAttempt(t *testing.T) {
	f := newFixture(t)
	t.Setenv("FAKE_PI_MODE", "mutate-pi")
	t.Setenv("FAKE_PI_SELF", f.pi)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(mustRead(t, f.target)), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	err := run(f.args("--task", "edit"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("err=%v", err)
	}
	if got := string(mustRead(t, f.target)); got != "before\n" {
		t.Fatalf("target=%q", got)
	}
}

func TestRejectsRetryOrCompactionDefaults(t *testing.T) {
	for _, settings := range []string{
		`{}`,
		`{"retry":{"enabled":true,"provider":{"maxRetries":0}},"compaction":{"enabled":false}}`,
		`{"retry":{"enabled":false,"provider":{"maxRetries":1}},"compaction":{"enabled":false}}`,
		`{"retry":{"enabled":false,"provider":{"maxRetries":0}},"compaction":{"enabled":true}}`,
	} {
		f := newFixture(t)
		mustWrite(t, filepath.Join(f.root, "agent", "settings.json"), []byte(settings), 0o600)
		err := run(f.args("--task", "edit"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "settings do not disable") {
			t.Fatalf("settings=%s err=%v", settings, err)
		}
	}
}

func TestRejectsAgentOverlaysBeforeLaunch(t *testing.T) {
	for _, overlay := range []string{"models.json", "package.json", "SYSTEM.md", "APPEND_SYSTEM.md"} {
		t.Run(overlay, func(t *testing.T) {
			f := newFixture(t)
			mustWrite(t, filepath.Join(f.root, "agent", overlay), []byte("fixture"), 0o600)
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), overlay) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestRejectsAgentAdmissionDriftAfterAttempt(t *testing.T) {
	for _, tc := range []struct{ name, mode string }{
		{"overlay", "agent-overlay"},
		{"settings", "agent-settings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			t.Setenv("FAKE_PI_MODE", tc.mode)
			t.Setenv("FAKE_PI_AGENT", filepath.Join(f.root, "agent"))
			reply := replacement{Path: "target.txt", OriginalSHA256: digest(mustRead(t, f.target)), Replacement: "after\n"}
			t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "agent admission changed") {
				t.Fatalf("err=%v", err)
			}
			if got := string(mustRead(t, f.target)); got != "before\n" {
				t.Fatalf("post-run drift changed target: %q", got)
			}
		})
	}
}

func TestSettingsPermissionsAcceptOwnerManagedDefaults(t *testing.T) {
	t.Run("readable 0644", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Chmod(filepath.Join(f.root, "agent", "settings.json"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(f.root, "agent", "models-store.json"), []byte(`{}`), 0o644)
		before := mustRead(t, f.target)
		t.Setenv("FAKE_PI_OUTPUT", marshal(t, replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}))
		if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("group writable", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Chmod(filepath.Join(f.root, "agent", "settings.json"), 0o660); err != nil {
			t.Fatal(err)
		}
		if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "group/world-writable") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestRejectsUnsafeManagedModelCacheObjects(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T, string)
	}{
		{
			name: "symlink",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(path), "cache-target")
				mustWrite(t, target, []byte(`{}`), 0o600)
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "directory",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "oversized",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				mustWrite(t, path, bytes.Repeat([]byte(" "), (1<<20)+1), 0o600)
			},
		},
		{
			name: "group writable",
			prepare: func(t *testing.T, path string) {
				t.Helper()
				mustWrite(t, path, []byte(`{}`), 0o660)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.prepare(t, filepath.Join(f.root, "agent", "models-store.json"))
			if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "model catalog cache") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestRejectsMalformedOrUnboundFinalOutput(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"not-json", "hello\n", "envelope"},
		{"extra-field", `{"path":"target.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x","summary":"no"}`, "unknown"},
		{"missing-replacement", `{"path":"target.txt","original_sha256":"` + strings.Repeat("a", 64) + `"}`, "malformed"},
		{"duplicate-path", `{"path":"target.txt","path":"other.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x"}`, "duplicate"},
		{"invalid-utf8", "{\xff}", "UTF-8"},
		{"wrong-path", `{"path":"other.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x"}`, "bind"},
		{"wrong-digest", `{"path":"target.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x"}`, "bind"},
		{"uppercase-digest", `{"path":"target.txt","original_sha256":"` + strings.Repeat("A", 64) + `","replacement":"x"}`, "canonical lowercase"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			t.Setenv("FAKE_PI_OUTPUT", tc.output)
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v", err)
			}
			if got := string(mustRead(t, f.target)); got != "before\n" {
				t.Fatalf("rejected output changed target: %q", got)
			}
		})
	}
}

func TestBoundsOutputAndTimeoutAndTerminatesProcessGroup(t *testing.T) {
	t.Run("overflow with active pipes", func(t *testing.T) {
		f := newFixture(t)
		t.Setenv("FAKE_PI_MODE", "overflow")
		err := run(f.args("--task", "edit", "--stdout-limit", "128"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("timeout with active pipes", func(t *testing.T) {
		f := newFixture(t)
		pidFile := filepath.Join(f.root, "child.pid")
		t.Setenv("FAKE_PI_MODE", "sleep")
		t.Setenv("FAKE_PI_PID_FILE", pidFile)
		err := run(f.args("--task", "edit", "--timeout", "100ms"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("err=%v", err)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(mustRead(t, pidFile))))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		requirePIDGone(t, pid)
	})
	t.Run("closed pipes then hang", func(t *testing.T) {
		f := newFixture(t)
		started := time.Now()
		t.Setenv("FAKE_PI_MODE", "close-hang")
		err := run(f.args("--task", "edit", "--timeout", "100ms"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("err=%v", err)
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("closed-pipe hang exceeded bounded cleanup: %s", elapsed)
		}
	})
	t.Run("overflow then close and hang", func(t *testing.T) {
		f := newFixture(t)
		t.Setenv("FAKE_PI_MODE", "overflow-close-hang")
		err := run(f.args("--task", "edit", "--stdout-limit", "128"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("zero exit removes redirected descendant", func(t *testing.T) {
		f := newFixture(t)
		pidFile := filepath.Join(f.root, "child.pid")
		t.Setenv("FAKE_PI_MODE", "normal-descendant")
		t.Setenv("FAKE_PI_PID_FILE", pidFile)
		before := mustRead(t, f.target)
		reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
		t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
		if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(mustRead(t, pidFile))))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		requirePIDGone(t, pid)
	})
	t.Run("unexpected signal", func(t *testing.T) {
		f := newFixture(t)
		before := mustRead(t, f.target)
		reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
		t.Setenv("FAKE_PI_MODE", "signal")
		t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
		err := run(f.args("--task", "edit"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "terminated unexpectedly") {
			t.Fatalf("err=%v", err)
		}
		if !bytes.Equal(mustRead(t, f.target), before) {
			t.Fatal("unexpectedly signaled Pi changed target")
		}
	})
	t.Run("independent SIGKILL", func(t *testing.T) {
		f := newFixture(t)
		before := mustRead(t, f.target)
		reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
		t.Setenv("FAKE_PI_MODE", "sigkill")
		t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
		err := run(f.args("--task", "edit"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "terminated unexpectedly") {
			t.Fatalf("err=%v", err)
		}
		if !bytes.Equal(mustRead(t, f.target), before) {
			t.Fatal("independently SIGKILLed Pi changed target")
		}
	})
}

func requirePIDGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for processIsLive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if processIsLive(pid) {
		t.Fatalf("descendant %d survived anchored process-group termination", pid)
	}
}

func processIsLive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	if runtime.GOOS != "linux" {
		return true
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return !errors.Is(err, os.ErrNotExist)
	}
	closingParen := bytes.LastIndexByte(stat, ')')
	return closingParen < 0 || len(stat) <= closingParen+2 || stat[closingParen+2] != 'Z'
}

func TestRequirePIDGoneAcceptsLinuxZombie(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc process-state regression")
	}
	cmd := exec.Command("/bin/true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	deadline := time.Now().Add(time.Second)
	for processIsLive(cmd.Process.Pid) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if processIsLive(cmd.Process.Pid) {
		t.Fatal("fixture process did not reach zombie state")
	}
	requirePIDGone(t, cmd.Process.Pid)
}

func TestGroupKillFailureUsesBoundedExactProcessFallback(t *testing.T) {
	t.Run("first group kill fails", func(t *testing.T) {
		f := newFixture(t)
		childPIDFile := filepath.Join(f.root, "child.pid")
		t.Setenv("FAKE_PI_MODE", "sleep")
		t.Setenv("FAKE_PI_PID_FILE", childPIDFile)
		originalSignal := signalProcessGroup
		calls := 0
		guardianPID := 0
		signalProcessGroup = func(pgid int) error {
			calls++
			guardianPID = pgid
			if calls == 1 {
				return syscall.EPERM
			}
			return originalSignal(pgid)
		}
		t.Cleanup(func() { signalProcessGroup = originalSignal })
		err := run(f.args("--task", "edit", "--timeout", "100ms"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "timed out") || calls != 2 {
			t.Fatalf("err=%v group-kill calls=%d", err, calls)
		}
		childPID, parseErr := strconv.Atoi(strings.TrimSpace(string(mustRead(t, childPIDFile))))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		requirePIDGone(t, childPID)
		requirePIDGone(t, guardianPID)
	})

	t.Run("both group kills fail", func(t *testing.T) {
		f := newFixture(t)
		leaderPIDFile := filepath.Join(f.root, "leader.pid")
		t.Setenv("FAKE_PI_MODE", "hang")
		t.Setenv("FAKE_PI_LEADER_PID_FILE", leaderPIDFile)
		originalSignal := signalProcessGroup
		calls := 0
		guardianPID := 0
		signalProcessGroup = func(pgid int) error {
			calls++
			guardianPID = pgid
			return syscall.EPERM
		}
		t.Cleanup(func() { signalProcessGroup = originalSignal })
		started := time.Now()
		err := run(f.args("--task", "edit", "--timeout", "100ms"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "process-group termination could not be verified") || calls != 2 {
			t.Fatalf("err=%v group-kill calls=%d", err, calls)
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("failed group-kill cleanup was not bounded: %s", elapsed)
		}
		leaderPID, parseErr := strconv.Atoi(strings.TrimSpace(string(mustRead(t, leaderPIDFile))))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		requirePIDGone(t, leaderPID)
		requirePIDGone(t, guardianPID)
	})
}

func TestGuardianOnlyCleanupIsBoundedWhenGroupKillsFail(t *testing.T) {
	input, hold, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	guardian := exec.Command("/bin/cat")
	guardian.Stdin = input
	guardian.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := guardian.Start(); err != nil {
		hold.Close()
		t.Fatal(err)
	}
	guardianPID := guardian.Process.Pid
	originalSignal := signalProcessGroup
	calls := 0
	signalProcessGroup = func(int) error {
		calls++
		return syscall.EPERM
	}
	t.Cleanup(func() { signalProcessGroup = originalSignal })
	started := time.Now()
	err = terminateGuardian(guardian, hold)
	if err == nil || !strings.Contains(err.Error(), "process-group termination could not be verified") || calls != 2 {
		t.Fatalf("err=%v group-kill calls=%d", err, calls)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("guardian-only cleanup was not bounded: %s", elapsed)
	}
	requirePIDGone(t, guardianPID)
}

func TestRejectsPiWorktreeMutationAndOutOfScopeDiff(t *testing.T) {
	f := newFixture(t)
	t.Setenv("FAKE_PI_MODE", "mutate")
	t.Setenv("FAKE_PI_MUTATE", filepath.Join(f.workdir, "outside.txt"))
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(mustRead(t, f.target)), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	err := run(f.args("--task", "edit"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "changed the worktree") {
		t.Fatalf("err=%v", err)
	}
	if got := string(mustRead(t, f.target)); got != "before\n" {
		t.Fatalf("target=%q", got)
	}
}

func TestGitHardeningDisablesRepositoryFSMonitor(t *testing.T) {
	f := newFixture(t)
	marker := filepath.Join(f.root, "fsmonitor-invoked")
	hook := filepath.Join(f.root, "fsmonitor")
	mustWrite(t, hook, []byte(fmt.Sprintf("#!/bin/sh\nprintf invoked >%q\nprintf '{}'\n", marker)), 0o755)
	gitRun(t, f.workdir, "config", "core.fsmonitor", hook)
	before := mustRead(t, f.target)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("repository fsmonitor executed: %v", err)
	}
}

func TestGitHardeningRejectsExternalCleanFiltersBeforeExecution(t *testing.T) {
	f := newFixture(t)
	mustWrite(t, filepath.Join(f.workdir, ".gitattributes"), []byte("target.txt filter=evil\n"), 0o644)
	gitRun(t, f.workdir, "add", ".gitattributes")
	gitRun(t, f.workdir, "commit", "-qm", "filter attributes fixture")
	marker := filepath.Join(f.root, "clean-filter-invoked")
	gitRun(t, f.workdir, "config", "filter.evil.clean", fmt.Sprintf("sh -c 'printf invoked >%q; cat'", marker))
	if err := os.Chtimes(f.target, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	err := run(f.args("--task", "edit"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "external Git filter configuration") {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("repository clean filter executed: %v", statErr)
	}
}

func TestGitHardeningRejectsIncludedExternalFiltersBeforeExecution(t *testing.T) {
	for _, key := range []string{"clean", "process"} {
		t.Run(key, func(t *testing.T) {
			f := newFixture(t)
			mustWrite(t, filepath.Join(f.workdir, ".gitattributes"), []byte("target.txt filter=evil\n"), 0o644)
			gitRun(t, f.workdir, "add", ".gitattributes")
			gitRun(t, f.workdir, "commit", "-qm", "filter attributes fixture")
			marker := filepath.Join(f.root, "included-filter-invoked")
			included := filepath.Join(f.root, "included.gitconfig")
			mustWrite(t, included, []byte(fmt.Sprintf("[filter \"evil\"]\n\t%s = sh -c 'printf invoked >%s; cat'\n", key, marker)), 0o600)
			gitRun(t, f.workdir, "config", "include.path", included)
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "external Git filter configuration") {
				t.Fatalf("err=%v", err)
			}
			if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("included repository filter executed: %v", statErr)
			}
		})
	}
}

func TestGitHardeningRejectsEveryFilterAttributeState(t *testing.T) {
	for _, attribute := range []string{"filter=unconfigured", "filter=unspecified", "-filter", "!filter"} {
		t.Run(attribute, func(t *testing.T) {
			f := newFixture(t)
			mustWrite(t, filepath.Join(f.workdir, ".gitattributes"), []byte("target.txt "+attribute+"\n"), 0o644)
			gitRun(t, f.workdir, "add", ".gitattributes")
			gitRun(t, f.workdir, "commit", "-qm", "filter attributes fixture")
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "filter attribute binding") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestGitHardeningRejectsCoreAttributesFileIncludingFromInclude(t *testing.T) {
	for _, included := range []bool{false, true} {
		t.Run(fmt.Sprintf("included=%t", included), func(t *testing.T) {
			f := newFixture(t)
			attributes := filepath.Join(f.root, "external-attributes")
			mustWrite(t, attributes, []byte("* text\n"), 0o600)
			if included {
				config := filepath.Join(f.root, "included.gitconfig")
				mustWrite(t, config, []byte("[core]\n\tattributesFile = "+attributes+"\n"), 0o600)
				gitRun(t, f.workdir, "config", "include.path", config)
			} else {
				gitRun(t, f.workdir, "config", "core.attributesFile", attributes)
			}
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "core.attributesFile") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestGitHardeningRejectsWorktreeScopedConfigurationAndIncludes(t *testing.T) {
	for _, tc := range []struct {
		key      string
		included bool
	}{
		{"core.attributesFile", false},
		{"core.attributesFile", true},
		{"filter.evil.clean", false},
		{"filter.evil.clean", true},
		{"filter.evil.process", false},
		{"filter.evil.process", true},
	} {
		t.Run(fmt.Sprintf("%s/included=%t", tc.key, tc.included), func(t *testing.T) {
			f := newFixture(t)
			gitRun(t, f.workdir, "config", "extensions.worktreeConfig", "true")
			value := filepath.Join(f.root, "external-attributes")
			want := "core.attributesFile"
			if strings.HasPrefix(tc.key, "filter.") {
				mustWrite(t, filepath.Join(f.workdir, ".gitattributes"), []byte("target.txt filter=evil\n"), 0o644)
				gitRun(t, f.workdir, "add", ".gitattributes")
				gitRun(t, f.workdir, "commit", "-qm", "worktree filter fixture")
				marker := filepath.Join(f.root, "worktree-filter-invoked")
				value = fmt.Sprintf("sh -c 'printf invoked >%s; cat'", marker)
				want = "external Git filter configuration"
				if err := os.Chtimes(f.target, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
						t.Errorf("worktree-scoped filter executed: %v", statErr)
					}
				})
			} else {
				mustWrite(t, value, []byte("* text\n"), 0o600)
			}
			if tc.included {
				included := filepath.Join(f.root, "worktree-included.gitconfig")
				var contents string
				if strings.HasPrefix(tc.key, "filter.") {
					driver := strings.TrimPrefix(tc.key, "filter.evil.")
					contents = fmt.Sprintf("[filter \"evil\"]\n\t%s = %s\n", driver, value)
				} else {
					contents = fmt.Sprintf("[core]\n\tattributesFile = %s\n", value)
				}
				mustWrite(t, included, []byte(contents), 0o600)
				gitRun(t, f.workdir, "config", "--worktree", "include.path", included)
			} else {
				gitRun(t, f.workdir, "config", "--worktree", tc.key, value)
			}
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestGitCommandOutputIsBounded(t *testing.T) {
	f := newFixture(t)
	large := filepath.Join(f.workdir, "large.txt")
	mustWrite(t, large, bytes.Repeat([]byte("x"), maxGitBytes+1), 0o644)
	gitRun(t, f.workdir, "add", "large.txt")
	gitRun(t, f.workdir, "commit", "-qm", "large output fixture")
	if _, err := git(f.workdir, "show", "HEAD:large.txt"); !errors.Is(err, errGitOutputBound) {
		t.Fatalf("err=%v", err)
	}
}

func TestRejectsHiddenIndexEntries(t *testing.T) {
	for _, tc := range []struct{ name, flag string }{
		{"assume-unchanged", "--assume-unchanged"},
		{"skip-worktree", "--skip-worktree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			hidden := filepath.Join(f.workdir, "hidden.txt")
			mustWrite(t, hidden, []byte("before\n"), 0o644)
			gitRun(t, f.workdir, "add", "hidden.txt")
			gitRun(t, f.workdir, "commit", "-qm", "hidden fixture")
			gitRun(t, f.workdir, "update-index", tc.flag, "hidden.txt")
			mustWrite(t, hidden, []byte("hidden mutation\n"), 0o644)
			err := run(f.args("--task", "edit"), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "skip-worktree or assume-unchanged") {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestTransactionalApplyRollsBackFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*testing.T) io.Writer
		want    string
	}{
		{
			name: "post-apply validation",
			prepare: func(t *testing.T) io.Writer {
				postApplyCheck = func(string, string) error { return errors.New("forced postcondition") }
				t.Cleanup(func() { postApplyCheck = requireExactDiff })
				return &bytes.Buffer{}
			},
			want: "post-apply validation failed",
		},
		{
			name:    "receipt output",
			prepare: func(*testing.T) io.Writer { return failingWriter{} },
			want:    "receipt output failed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			before := mustRead(t, f.target)
			reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: "after\n"}
			t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
			err := run(f.args("--task", "edit"), tc.prepare(t))
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "replacement rolled back") {
				t.Fatalf("err=%v", err)
			}
			if !bytes.Equal(mustRead(t, f.target), before) {
				t.Fatal("failed transaction did not restore original bytes")
			}
			if status, statusErr := git(f.workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all"); statusErr != nil || len(status) != 0 {
				t.Fatalf("failed transaction did not restore clean state: %q, %v", status, statusErr)
			}
		})
	}
}

func TestEveryIndeterminateRollbackBranchUsesSentinel(t *testing.T) {
	before := []byte("before\n")
	cause := errors.New("forced post-apply failure")
	okReplace := func(string, []byte) error { return nil }
	okRead := func(string) ([]byte, error) { return before, nil }
	okCheck := func(string) error { return nil }
	okGit := func(string, ...string) ([]byte, error) { return nil, nil }
	tests := []struct {
		name string
		deps rollbackDependencies
	}{
		{
			name: "restore failure",
			deps: rollbackDependencies{
				replace: func(string, []byte) error { return errors.New("restore failed") },
				read:    okRead, gitSafe: okCheck, visible: okCheck, git: okGit,
			},
		},
		{
			name: "read-back failure",
			deps: rollbackDependencies{
				replace: okReplace, read: func(string) ([]byte, error) { return nil, errors.New("read failed") },
				gitSafe: okCheck, visible: okCheck, git: okGit,
			},
		},
		{
			name: "Git safety failure",
			deps: rollbackDependencies{
				replace: okReplace, read: okRead,
				gitSafe: func(string) error { return errors.New("unsafe Git") }, visible: okCheck, git: okGit,
			},
		},
		{
			name: "index visibility failure",
			deps: rollbackDependencies{
				replace: okReplace, read: okRead, gitSafe: okCheck,
				visible: func(string) error { return errors.New("hidden index") }, git: okGit,
			},
		},
		{
			name: "dirty status",
			deps: rollbackDependencies{
				replace: okReplace, read: okRead, gitSafe: okCheck, visible: okCheck,
				git: func(string, ...string) ([]byte, error) { return []byte("dirty"), nil },
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := rollbackAfterApplyWith("worktree", "target", before, cause, tc.deps)
			if !errors.Is(err, errAppliedStateIndeterminate) || failureExitCode(err) != 3 {
				t.Fatalf("err=%v exit=%d", err, failureExitCode(err))
			}
		})
	}
	if failureExitCode(errors.New("blocked")) != 2 {
		t.Fatal("ordinary refusal must retain exit status 2")
	}
}

func TestIndeterminateFailureEmitsSentinelAndExitsThree(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestIndeterminateExitHelper")
	cmd.Env = append(os.Environ(), "AMUX_TEST_INDETERMINATE_EXIT=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("err=%v", err)
	}
	if got := stderr.String(); !strings.Contains(got, "indeterminate_applied_state: applied state indeterminate") {
		t.Fatalf("stderr=%q", got)
	}
}

func TestIndeterminateExitHelper(t *testing.T) {
	if os.Getenv("AMUX_TEST_INDETERMINATE_EXIT") != "1" {
		return
	}
	os.Exit(reportFailure(fmt.Errorf("%w: forced fixture", errAppliedStateIndeterminate), os.Stderr))
}

func TestRejectsUnchangedReplacement(t *testing.T) {
	f := newFixture(t)
	before := mustRead(t, f.target)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(before), Replacement: string(before)}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	err := run(f.args("--task", "edit"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "replacement is unchanged") {
		t.Fatalf("err=%v", err)
	}
	if status, statusErr := git(f.workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all"); statusErr != nil || len(status) != 0 {
		t.Fatalf("unchanged replacement dirtied worktree: %q, %v", status, statusErr)
	}
}

func TestAuthMetadataAllowsContentRefresh(t *testing.T) {
	f := newFixture(t)
	auth := filepath.Join(f.root, "agent", "auth.json")
	t.Setenv("FAKE_PI_MODE", "auth-refresh")
	t.Setenv("FAKE_PI_AUTH", auth)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(mustRead(t, f.target)), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, auth)); got != "refreshed-fixture" {
		t.Fatalf("auth refresh was not preserved: %q", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("forced output failure") }

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	agent := filepath.Join(root, "agent")
	if err := os.Mkdir(agent, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(agent, "auth.json"), []byte("fixture-auth"), 0o600)
	mustWrite(t, filepath.Join(agent, "settings.json"), []byte(`{"retry":{"enabled":false,"provider":{"maxRetries":0}},"compaction":{"enabled":false}}`), 0o600)
	t.Setenv("PI_CODING_AGENT_DIR", agent)
	extraProcessEnvironment = func() []string {
		var environment []string
		for _, name := range []string{"FAKE_NODE_VERSION", "FAKE_PI_MODE", "FAKE_PI_OUTPUT", "FAKE_PI_PID_FILE", "FAKE_PI_LEADER_PID_FILE", "FAKE_PI_MUTATE", "FAKE_PI_SELF", "FAKE_PI_STDIN_FILE", "FAKE_PI_AGENT", "FAKE_PI_AUTH", "FAKE_PI_REACHED_FILE"} {
			if value, present := os.LookupEnv(name); present {
				environment = append(environment, name+"="+value)
			}
		}
		return environment
	}
	t.Cleanup(func() { extraProcessEnvironment = nil })

	packageRoot := filepath.Join(root, "package")
	dist := filepath.Join(packageRoot, "dist")
	if err := os.MkdirAll(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	pi := filepath.Join(dist, "cli.js")
	mustWrite(t, pi, []byte(`#!/bin/sh
set -eu
test "$1" = "--model"
test "$2" = "openai-codex/gpt-5.3-codex-spark"
test "$3" = "--no-session"
test "$4" = "--no-tools"
test "$5" = "--no-extensions"
test "$6" = "--no-skills"
test "$7" = "--no-prompt-templates"
test "$8" = "--no-themes"
test "$9" = "--no-context-files"
test "${10}" = "--no-approve"
test "${11}" = "-p"
test "$#" = 11
if [ -n "${FAKE_PI_REACHED_FILE:-}" ]; then printf reached >"$FAKE_PI_REACHED_FILE"; fi
if [ -n "${FAKE_PI_LEADER_PID_FILE:-}" ]; then printf '%s\n' "$$" >"$FAKE_PI_LEADER_PID_FILE"; fi
if [ -n "${FAKE_PI_STDIN_FILE:-}" ]; then
  cat >"$FAKE_PI_STDIN_FILE"
  test -s "$FAKE_PI_STDIN_FILE"
else
  prompt=$(cat)
  test -n "$prompt"
fi
case "${FAKE_PI_MODE:-success}" in
  overflow) while :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done ;;
  sleep) sleep 60 & child=$!; printf '%s\n' "$child" >"$FAKE_PI_PID_FILE"; wait "$child" ;;
  close-hang) exec 1>&- 2>&-; sleep 60 ;;
  overflow-close-hang) printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; exec 1>&- 2>&-; sleep 60 ;;
  hang) exec sleep 60 ;;
  normal-descendant) sleep 60 >/dev/null 2>&1 & child=$!; printf '%s\n' "$child" >"$FAKE_PI_PID_FILE"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  signal) printf '%s' "$FAKE_PI_OUTPUT"; kill -TERM $$ ;;
  sigkill) printf '%s' "$FAKE_PI_OUTPUT"; kill -KILL $$ ;;
  mutate) printf 'bad\n' >"$FAKE_PI_MUTATE"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  mutate-pi) printf '# changed\n' >>"$FAKE_PI_SELF"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  agent-overlay) printf fixture >"$FAKE_PI_AGENT/SYSTEM.md"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  agent-settings) printf '{}' >"$FAKE_PI_AGENT/settings.json"; chmod 600 "$FAKE_PI_AGENT/settings.json"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  auth-refresh) printf refreshed-fixture >"$FAKE_PI_AUTH"; chmod 600 "$FAKE_PI_AUTH"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  *) printf '%s' "$FAKE_PI_OUTPUT" ;;
esac
`), 0o755)
	pkg := map[string]any{
		"name": packageName, "version": packageVersion,
		"bin": map[string]string{"pi": "dist/cli.js"}, "engines": map[string]string{"node": packageEngine},
	}
	mustWrite(t, filepath.Join(packageRoot, "package.json"), []byte(marshal(t, pkg)), 0o600)
	node := filepath.Join(root, "node")
	mustWrite(t, node, []byte(`#!/bin/sh
test "$1" = --import
case "$2" in data:text/javascript,*) ;; *) exit 63 ;; esac
version=${FAKE_NODE_VERSION:-22.19.0}
case "$version" in ''|*[!0-9.]*|.*|*.|*.*.*.*) exit 64 ;; esac
case "$version" in *.*.*) ;; *) exit 64 ;; esac
major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
patch=${rest#*.}
test -n "$major" && test -n "$minor" && test -n "$patch" || exit 64
if [ "$major" -lt 22 ] || { [ "$major" -eq 22 ] && [ "$minor" -lt 19 ]; }; then exit 64; fi
shift 2
exec "$@"
`), 0o755)

	workdir := filepath.Join(root, "worktree")
	if err := os.Mkdir(workdir, 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, workdir, "init", "-q")
	gitRun(t, workdir, "config", "user.email", "fixture@example.invalid")
	gitRun(t, workdir, "config", "user.name", "Fixture")
	gitRun(t, workdir, "config", "commit.gpgsign", "false")
	target := filepath.Join(workdir, "target.txt")
	mustWrite(t, target, []byte("before\n"), 0o644)
	gitRun(t, workdir, "add", "target.txt")
	gitRun(t, workdir, "commit", "-qm", "fixture")
	return fixture{root: root, workdir: workdir, pi: pi, node: node, target: target}
}

func (f fixture) args(extra ...string) []string {
	return append([]string{
		"--pi", f.pi, "--pi-sha256", digest(mustReadFile(f.pi)),
		"--node", f.node, "--node-sha256", digest(mustReadFile(f.node)),
		"--workdir", f.workdir, "--file", "target.txt",
	}, extra...)
}

func mustReadFile(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return data
}

func mustCanonical(t *testing.T, path string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func marshal(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, workdir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", workdir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(fmt.Errorf("git %v: %w: %s", args, err, output))
	}
}
