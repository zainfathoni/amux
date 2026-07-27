package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fixture struct {
	root, workdir, pi, node, target string
}

func TestAppliesOneExactReplacementInPrintMode(t *testing.T) {
	f := newFixture(t)
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
	if got.Status != "replacement_applied_untrusted" || got.Model != model || got.Version != packageVersion || got.Stderr != "empty" {
		t.Fatalf("result=%+v", got)
	}
	wantArgs := append([]string{f.node, f.pi}, fixedArgs...)
	if strings.Join(got.Argv, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("argv=%q, want %q", got.Argv, wantArgs)
	}
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
}

func TestRejectsWrongPackageIdentityBeforeLaunch(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(p map[string]any) { p["name"] = "other" },
		func(p map[string]any) { p["version"] = "0.80.11" },
		func(p map[string]any) { p["bin"] = map[string]string{"pi": "dist/other.js"} },
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

func TestRejectsMalformedOrUnboundFinalOutput(t *testing.T) {
	for _, tc := range []struct{ name, output, want string }{
		{"not-json", "hello\n", "envelope"},
		{"extra-field", `{"path":"target.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x","summary":"no"}`, "unknown"},
		{"missing-replacement", `{"path":"target.txt","original_sha256":"` + strings.Repeat("a", 64) + `"}`, "malformed"},
		{"duplicate-path", `{"path":"target.txt","path":"other.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x"}`, "duplicate"},
		{"invalid-utf8", "{\xff}", "UTF-8"},
		{"wrong-path", `{"path":"other.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x"}`, "bind"},
		{"wrong-digest", `{"path":"target.txt","original_sha256":"` + strings.Repeat("a", 64) + `","replacement":"x"}`, "bind"},
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
	t.Run("overflow", func(t *testing.T) {
		f := newFixture(t)
		t.Setenv("FAKE_PI_MODE", "overflow")
		err := run(f.args("--task", "edit", "--stdout-limit", "128"), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "exceeded") {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
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
		deadline := time.Now().Add(time.Second)
		for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if err := syscall.Kill(pid, 0); err == nil {
			t.Fatalf("descendant %d survived verified group termination", pid)
		}
	})
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

func TestAuthIsMetadataOnlyAndPreserved(t *testing.T) {
	f := newFixture(t)
	auth := filepath.Join(f.root, "agent", "auth.json")
	secretFixture := []byte(`{"openai-codex":{"type":"oauth","secret":"fixture-only"}}`)
	mustWrite(t, auth, secretFixture, 0o600)
	reply := replacement{Path: "target.txt", OriginalSHA256: digest(mustRead(t, f.target)), Replacement: "after\n"}
	t.Setenv("FAKE_PI_OUTPUT", marshal(t, reply))
	if err := run(f.args("--task", "edit"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mustRead(t, auth), secretFixture) {
		t.Fatal("auth bytes changed")
	}
}

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
		for _, name := range []string{"FAKE_PI_MODE", "FAKE_PI_OUTPUT", "FAKE_PI_PID_FILE", "FAKE_PI_MUTATE", "FAKE_PI_SELF"} {
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
test -n "${12}"
case "${FAKE_PI_MODE:-success}" in
  overflow) while :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done ;;
  sleep) sleep 60 & child=$!; printf '%s\n' "$child" >"$FAKE_PI_PID_FILE"; wait "$child" ;;
  mutate) printf 'bad\n' >"$FAKE_PI_MUTATE"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  mutate-pi) printf '# changed\n' >>"$FAKE_PI_SELF"; printf '%s' "$FAKE_PI_OUTPUT" ;;
  *) printf '%s' "$FAKE_PI_OUTPUT" ;;
esac
`), 0o755)
	pkg := map[string]any{"name": packageName, "version": packageVersion, "bin": map[string]string{"pi": "dist/cli.js"}}
	mustWrite(t, filepath.Join(packageRoot, "package.json"), []byte(marshal(t, pkg)), 0o600)
	node := filepath.Join(root, "node")
	mustWrite(t, node, []byte("#!/bin/sh\nexec \"$@\"\n"), 0o755)

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
