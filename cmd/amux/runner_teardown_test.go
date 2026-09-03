package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func TestRunnerTeardownDryRunThenApplyRemovesWorktreeAndBindingButPreservesBranch(t *testing.T) {
	repo, worktree, head := newRunnerTeardownRepo(t, false)
	dir := t.TempDir()
	writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
	installAbsentRunnerTmux(t)
	stores := writeRunnerTeardownLegacyStores(t, dir)

	dry := executeRunnerJSON(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)
	if len(dry.Planned) != 1 || dry.Planned[0].Teardown == nil || len(dry.Planned[0].Teardown.PlanDigest) != 64 {
		t.Fatalf("teardown dry-run = %+v", dry)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("dry-run removed worktree: %v", err)
	}
	if rows, err := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile)); err != nil || len(rows) != 1 {
		t.Fatalf("dry-run runner rows = %+v, err=%v", rows, err)
	}
	assertRunnerTeardownStoresUnchanged(t, dir, stores)

	digest := dry.Planned[0].Teardown.PlanDigest
	got := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree, "--confirm-plan", digest)
	if len(got.Successful) != 1 || got.Successful[0].Teardown == nil {
		t.Fatalf("teardown apply = %+v", got)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree remains after teardown: %v", err)
	}
	if rows, err := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile)); err != nil || len(rows) != 0 {
		t.Fatalf("teardown runner rows = %+v, err=%v", rows, err)
	}
	if gotHead := runnerTeardownGitTest(t, repo, "rev-parse", "--verify", "refs/heads/teardown-feature"); gotHead != head+"\n" {
		t.Fatalf("preserved branch head = %q, want %q", gotHead, head)
	}
	assertRunnerTeardownStoresUnchanged(t, dir, stores)
}

func TestRunnerTeardownRecoversMissingWorktreeByUnpinningOnly(t *testing.T) {
	dir := t.TempDir()
	worktree := filepath.Join(t.TempDir(), "missing")
	writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
	installAbsentRunnerTmux(t)

	dry := executeRunnerJSON(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)
	digest := dry.Planned[0].Teardown.PlanDigest
	got := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree, "--confirm-plan", digest)
	if len(got.Successful) != 1 {
		t.Fatalf("missing-worktree recovery = %+v", got)
	}
	if rows, err := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile)); err != nil || len(rows) != 0 {
		t.Fatalf("recovery runner rows = %+v, err=%v", rows, err)
	}

	replay := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)
	if len(replay.Skipped) != 1 || replay.Skipped[0].Message != "already in desired state" {
		t.Fatalf("teardown replay = %+v", replay)
	}
}

func TestRunnerTeardownStopsExactLiveRunnerBeforeRemovingWorktree(t *testing.T) {
	_, worktree, _ := newRunnerTeardownRepo(t, false)
	dir := t.TempDir()
	writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
	bin := t.TempDir()
	state := filepath.Join(bin, "running")
	writeRunnerTeardownFile(t, state, "live")
	window := config.RunnerWindow(worktree)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) test -e "`+state+`" ;;
  list-panes) if [ -e "`+state+`" ]; then printf 'alpha\t`+window+`\t@7\t%%9\t`+worktree+`\tbash\t%s\t0\t7000\t123\n' `+shellSingleQuote(runnerStartCommand(worktree))+`; fi ;;
  kill-window) rm "`+state+`" ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	dry := executeRunnerJSON(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)
	got := executeRunnerJSON(t, "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree, "--confirm-plan", dry.Planned[0].Teardown.PlanDigest)
	if len(got.Successful) != 1 {
		t.Fatalf("live runner teardown = %+v", got)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("exact runner was not stopped: %v", err)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree was not removed after runner stop: %v", err)
	}
}

func TestRunnerTeardownPartialFailureAfterStopRetainsWorktreeAndBinding(t *testing.T) {
	_, worktree, _ := newRunnerTeardownRepo(t, false)
	dir := t.TempDir()
	writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
	bin := t.TempDir()
	state := filepath.Join(bin, "running")
	writeRunnerTeardownFile(t, state, "live")
	window := config.RunnerWindow(worktree)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
case "$1" in
  has-session) test -e "`+state+`" ;;
  list-panes) if [ -e "`+state+`" ]; then printf 'alpha\t`+window+`\t@7\t%%9\t`+worktree+`\tbash\t%s\t0\t7000\t123\n' `+shellSingleQuote(runnerStartCommand(worktree))+`; fi ;;
  kill-window) rm "`+state+`"; printf keep > "`+filepath.Join(worktree, "arrived-after-stop")+`" ;;
  *) exit 2 ;;
esac
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	dry := executeRunnerJSON(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree, "--confirm-plan", dry.Planned[0].Teardown.PlanDigest)
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || !strings.Contains(err.Error(), "changes") {
		t.Fatalf("post-stop failure = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("runner was not stopped before partial failure: %v", err)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("partial failure removed worktree: %v", err)
	}
	if rows, err := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile)); err != nil || len(rows) != 1 {
		t.Fatalf("partial failure runner rows = %+v, err=%v", rows, err)
	}
}

func TestRunnerTeardownPartialFailureAfterWorktreeRemovalRetainsRecoverableBinding(t *testing.T) {
	_, worktree, _ := newRunnerTeardownRepo(t, false)
	dir := t.TempDir()
	writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
	installAbsentRunnerTmux(t)
	dry := executeRunnerJSON(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)

	oldGit := runnerTeardownGit
	runnerTeardownGit = func(gitDir string, args ...string) ([]byte, error) {
		output, err := oldGit(gitDir, args...)
		if err == nil && len(args) >= 2 && args[0] == "worktree" && args[1] == "remove" {
			writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\nbeta\t"+t.TempDir()+"\n")
		}
		return output, err
	}
	t.Cleanup(func() { runnerTeardownGit = oldGit })

	err := executeRunnerJSONError(t, "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree, "--confirm-plan", dry.Planned[0].Teardown.PlanDigest)
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure || !strings.Contains(err.Error(), "registry changed") {
		t.Fatalf("post-worktree failure = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree should already be removed: %v", err)
	}
	rows, loadErr := config.LoadRunnersReadOnly(filepath.Join(dir, config.RunnersFile))
	if loadErr != nil || len(rows) != 2 {
		t.Fatalf("recoverable rows = %+v, err=%v", rows, loadErr)
	}
}

func TestRunnerTeardownRequiresFreshExactConfirmationBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name       string
		confirm    string
		mutatePlan bool
		wantExit   int
		want       string
	}{
		{name: "missing", wantExit: result.ExitRejected, want: "requires --confirm-plan"},
		{name: "malformed", confirm: "not-a-digest", wantExit: result.ExitRejected, want: "lowercase SHA-256"},
		{name: "stale", mutatePlan: true, wantExit: result.ExitRejected, want: "plan changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, worktree, _ := newRunnerTeardownRepo(t, false)
			dir := t.TempDir()
			writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
			installAbsentRunnerTmux(t)
			dry := executeRunnerJSON(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)
			confirm := test.confirm
			if confirm == "" && test.mutatePlan {
				confirm = dry.Planned[0].Teardown.PlanDigest
				writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\nbeta\t"+t.TempDir()+"\n")
			}
			args := []string{"--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree}
			if confirm != "" {
				args = append(args, "--confirm-plan", confirm)
			}
			err := executeRunnerJSONError(t, args...)
			if err == nil || result.ExitCode(err) != test.wantExit || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("confirmation error = %v, exit=%d", err, result.ExitCode(err))
			}
			if _, statErr := os.Stat(worktree); statErr != nil {
				t.Fatalf("rejected teardown changed worktree: %v", statErr)
			}
		})
	}
}

func TestRunnerTeardownRetainsEverythingOnUnownedOrUnreadableRunnerState(t *testing.T) {
	for _, test := range []struct {
		name   string
		script func(string, string) string
		want   string
	}{
		{name: "unreadable", script: func(_, _ string) string { return "#!/bin/sh\nexit 2\n" }, want: "unreadable"},
		{name: "conflict", script: func(window, worktree string) string {
			return `#!/bin/sh
case "$1" in
  has-session) exit 0 ;;
  list-panes) printf 'alpha\t` + window + `\t@7\t%%9\t` + worktree + `\tbash\tnot-amux\t0\t7000\t123\n' ;;
  kill-window) exit 99 ;;
  *) exit 2 ;;
esac
`
		}, want: "conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, worktree, _ := newRunnerTeardownRepo(t, false)
			dir := t.TempDir()
			writeRunnerRegistry(t, dir, "alpha\t"+worktree+"\n")
			before, err := os.ReadFile(filepath.Join(dir, config.RunnersFile))
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "tmux"), test.script(config.RunnerWindow(worktree), worktree))
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err = executeRunnerJSONError(t, "--dry-run", "--json", "--config-dir", dir, "runner", "teardown", "--workdir", worktree)
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("%s runner error = %v, exit=%d", test.name, err, result.ExitCode(err))
			}
			after, readErr := os.ReadFile(filepath.Join(dir, config.RunnersFile))
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("%s changed runner registry: %v", test.name, readErr)
			}
			if _, statErr := os.Stat(worktree); statErr != nil {
				t.Fatalf("%s changed worktree: %v", test.name, statErr)
			}
		})
	}
}

func TestRunnerTeardownRejectsUnsafeWorktrees(t *testing.T) {
	t.Run("primary", func(t *testing.T) {
		repo, _, _ := newRunnerTeardownRepo(t, false)
		if _, err := inspectRunnerTeardownWorktree(repo); err == nil || !strings.Contains(err.Error(), "primary") {
			t.Fatalf("primary worktree error = %v", err)
		}
	})

	for _, test := range []struct {
		name   string
		setup  func(*testing.T, string, string)
		needle string
	}{
		{name: "untracked", setup: func(t *testing.T, _, worktree string) {
			writeRunnerTeardownFile(t, filepath.Join(worktree, "untracked"), "keep")
		}, needle: "changes"},
		{name: "ignored", setup: func(t *testing.T, _, worktree string) {
			runnerTeardownGitTest(t, worktree, "config", "core.excludesFile", filepath.Join(worktree, ".git", "nonexistent"))
			writeRunnerTeardownFile(t, filepath.Join(worktree, ".gitignore"), "generated\n")
			runnerTeardownGitTest(t, worktree, "add", ".gitignore")
			runnerTeardownGitTest(t, worktree, "commit", "-m", "ignore generated")
			writeRunnerTeardownFile(t, filepath.Join(worktree, "generated"), "keep")
		}, needle: "non-index filesystem object"},
		{name: "assume-unchanged", setup: func(t *testing.T, _, worktree string) {
			runnerTeardownGitTest(t, worktree, "update-index", "--assume-unchanged", "tracked.txt")
		}, needle: "assume-unchanged"},
		{name: "skip-worktree", setup: func(t *testing.T, _, worktree string) {
			runnerTeardownGitTest(t, worktree, "update-index", "--skip-worktree", "tracked.txt")
		}, needle: "skip-worktree"},
		{name: "locked", setup: func(t *testing.T, repo, worktree string) {
			runnerTeardownGitTest(t, repo, "worktree", "lock", worktree)
		}, needle: "locked or prunable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, worktree, _ := newRunnerTeardownRepo(t, false)
			test.setup(t, repo, worktree)
			if _, err := inspectRunnerTeardownWorktree(worktree); err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("unsafe worktree error = %v, want %q", err, test.needle)
			}
			if _, err := os.Stat(worktree); err != nil {
				t.Fatalf("unsafe worktree changed: %v", err)
			}
		})
	}

	t.Run("detached", func(t *testing.T) {
		_, worktree, _ := newRunnerTeardownRepo(t, true)
		if _, err := inspectRunnerTeardownWorktree(worktree); err == nil || !strings.Contains(err.Error(), "detached") {
			t.Fatalf("detached worktree error = %v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		_, worktree, _ := newRunnerTeardownRepo(t, false)
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(worktree, alias); err != nil {
			t.Fatal(err)
		}
		if _, err := inspectRunnerTeardownWorktree(alias); err == nil || !strings.Contains(err.Error(), "non-symlink") {
			t.Fatalf("symlink worktree error = %v", err)
		}
	})

	t.Run("non-root", func(t *testing.T) {
		_, worktree, _ := newRunnerTeardownRepo(t, false)
		nested := filepath.Join(worktree, "nested")
		if err := os.Mkdir(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := inspectRunnerTeardownWorktree(nested); err == nil || !strings.Contains(err.Error(), "exact Git worktree root") {
			t.Fatalf("non-root worktree error = %v", err)
		}
	})

	t.Run("current directory", func(t *testing.T) {
		_, worktree, _ := newRunnerTeardownRepo(t, false)
		old := runnerTeardownCurrentDir
		runnerTeardownCurrentDir = func() (string, error) { return filepath.Join(worktree, "nested"), nil }
		t.Cleanup(func() { runnerTeardownCurrentDir = old })
		if _, err := inspectRunnerTeardownWorktree(worktree); err == nil || !strings.Contains(err.Error(), "current process directory") {
			t.Fatalf("current-directory error = %v", err)
		}
	})
}

func newRunnerTeardownRepo(t *testing.T, detached bool) (string, string, string) {
	t.Helper()
	repo := t.TempDir()
	runnerTeardownGitTest(t, repo, "init", "--initial-branch=main")
	runnerTeardownGitTest(t, repo, "config", "user.email", "runner-teardown@example.com")
	runnerTeardownGitTest(t, repo, "config", "user.name", "Runner Teardown Test")
	writeRunnerTeardownFile(t, filepath.Join(repo, "tracked.txt"), "preserve\n")
	runnerTeardownGitTest(t, repo, "add", "tracked.txt")
	runnerTeardownGitTest(t, repo, "commit", "-m", "initial")
	worktree := filepath.Join(t.TempDir(), "worktree")
	if detached {
		runnerTeardownGitTest(t, repo, "worktree", "add", "--detach", worktree, "HEAD")
	} else {
		runnerTeardownGitTest(t, repo, "worktree", "add", "-b", "teardown-feature", worktree, "HEAD")
	}
	head := strings.TrimSpace(runnerTeardownGitTest(t, worktree, "rev-parse", "HEAD"))
	return repo, worktree, head
}

func runnerTeardownGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func installAbsentRunnerTmux(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

func writeRunnerTeardownFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRunnerTeardownLegacyStores(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	stores := map[string][]byte{
		"workers.tsv":     []byte("# inert worker evidence\n"),
		"groups.tsv":      []byte("# inert group evidence\n"),
		"reports.json":    []byte("{\"inert\":true}\n"),
		"operations.json": []byte("{\"inert\":true}\n"),
		"shelves.tsv":     []byte("# inert shelf evidence\n"),
	}
	for name, contents := range stores {
		writeRunnerTeardownFile(t, filepath.Join(dir, name), string(contents))
	}
	return stores
}

func assertRunnerTeardownStoresUnchanged(t *testing.T, dir string, stores map[string][]byte) {
	t.Helper()
	for name, before := range stores {
		after, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("runner teardown changed %s: err=%v before=%q after=%q", name, err, before, after)
		}
	}
}
