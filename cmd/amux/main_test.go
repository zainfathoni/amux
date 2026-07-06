package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/tmux"
)

func TestPathWritesToInjectedStdout(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	var stdout bytes.Buffer

	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "path"}); err != nil {
		t.Fatal(err)
	}

	if got, want := stdout.String(), configPath+"\n"; got != want {
		t.Fatalf("got stdout %q, want %q", got, want)
	}
}

func TestVersionPrintsDefaultVersion(t *testing.T) {
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"version"}); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "amux dev\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVersionFlagPrintsDefaultVersion(t *testing.T) {
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--version"}); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "amux dev\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestVersionStringIncludesBuildMetadata(t *testing.T) {
	oldVersion, oldCommit, oldBuilt := version, commit, built
	t.Cleanup(func() {
		version, commit, built = oldVersion, oldCommit, oldBuilt
	})
	version = "v0.1.0"
	commit = "abc1234"
	built = "2026-06-13T11:20:06Z"

	if got, want := versionString(), "amux v0.1.0 commit=abc1234 built=2026-06-13T11:20:06Z"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelfUpdateDryRunPlansLatestReleaseAsset(t *testing.T) {
	tmp := t.TempDir()
	exePath := filepath.Join(tmp, "amux")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archiveName := fmt.Sprintf("amux-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":%q},{"name":%q,"browser_download_url":%q}]}`, archiveName, serverURL(r, "/archive"), archiveName+".sha256", serverURL(r, "/checksum"))
	}))
	defer server.Close()
	withSelfUpdateTestState(t, exePath, server.URL+"/latest", server.Client())

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--dry-run", "self-update"}); err != nil {
		t.Fatal(err)
	}

	resolvedExePath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "Would update "+resolvedExePath+" to v9.9.9 using "+archiveName) {
		t.Fatalf("unexpected dry-run output: %q", got)
	}
	contents, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "old binary" {
		t.Fatalf("dry-run changed executable to %q", contents)
	}
}

func TestSelfUpdateDryRunDoesNotRequireWritableInstallDirOrDownloadArchive(t *testing.T) {
	tmp := t.TempDir()
	exePath := filepath.Join(tmp, "amux")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tmp, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(tmp, 0o755) })
	archiveName := fmt.Sprintf("amux-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":%q},{"name":%q,"browser_download_url":%q}]}`, archiveName, serverURL(r, "/archive"), archiveName+".sha256", serverURL(r, "/checksum"))
		case "/archive", "/checksum":
			t.Fatalf("dry-run downloaded %s", r.URL.Path)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	withSelfUpdateTestState(t, exePath, server.URL+"/latest", server.Client())

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--dry-run", "self-update"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "Would update ") {
		t.Fatalf("unexpected dry-run output: %q", stdout.String())
	}
}

func TestSelfUpdateReplacesCurrentBinary(t *testing.T) {
	tmp := t.TempDir()
	exePath := filepath.Join(tmp, "amux")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archiveName := fmt.Sprintf("amux-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archiveBytes := testReleaseArchive(t, "new binary")
	checksum := sha256.Sum256(archiveBytes)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":%q},{"name":%q,"browser_download_url":%q}]}`, archiveName, serverURL(r, "/archive"), archiveName+".sha256", serverURL(r, "/checksum"))
		case "/archive":
			w.Write(archiveBytes)
		case "/checksum":
			fmt.Fprintf(w, "%x  %s\n", checksum, archiveName)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	withSelfUpdateTestState(t, exePath, server.URL+"/latest", server.Client())

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"self-update"}); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new binary" {
		t.Fatalf("got executable contents %q, want updated binary", contents)
	}
	resolvedExePath, err := filepath.EvalSymlinks(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "Updated amux to v9.9.9 at "+resolvedExePath) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestSelfUpdateChecksumMismatchLeavesCurrentBinary(t *testing.T) {
	tmp := t.TempDir()
	exePath := filepath.Join(tmp, "amux")
	if err := os.WriteFile(exePath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archiveName := fmt.Sprintf("amux-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archiveBytes := testReleaseArchive(t, "new binary")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"v9.9.9","assets":[{"name":%q,"browser_download_url":%q},{"name":%q,"browser_download_url":%q}]}`, archiveName, serverURL(r, "/archive"), archiveName+".sha256", serverURL(r, "/checksum"))
		case "/archive":
			w.Write(archiveBytes)
		case "/checksum":
			fmt.Fprintf(w, "%064x  %s\n", 0, archiveName)
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	withSelfUpdateTestState(t, exePath, server.URL+"/latest", server.Client())

	err := (app{}).run([]string{"self-update"})
	if err == nil {
		t.Fatal("self-update succeeded with checksum mismatch, want error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %q", err)
	}
	contents, readErr := os.ReadFile(exePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(contents) != "old binary" {
		t.Fatalf("checksum failure changed executable to %q", contents)
	}
}

func TestSelfUpdateRefusesPackageManagedInstall(t *testing.T) {
	withSelfUpdateTestState(t, "/nix/store/example-amux/bin/amux", "http://127.0.0.1/should-not-fetch", http.DefaultClient)

	err := (app{}).run([]string{"self-update"})
	if err == nil {
		t.Fatal("self-update succeeded for package-managed path, want error")
	}
	if !strings.Contains(err.Error(), "self-update refused for package-managed install") || !strings.Contains(err.Error(), "~/.local/bin/amux") {
		t.Fatalf("unexpected error: %q", err)
	}
}

func TestLaunchDoesNotAttachByDefault(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tone\t"+workdir+"\tT-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 1; fi
if [ "$1" = new-session ]; then exit 0; fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := runWithDiscardedStdout([]string{"--config", configPath, "launch", "mac", "Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "new-session") {
		t.Fatalf("launch did not create session\nlog:\n%s", log)
	}
	if strings.Contains(log, "attach") {
		t.Fatalf("cold launch attached by default\nlog:\n%s", log)
	}
}

func TestLaunchAutoAttachesToExistingMatchingSession(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tone\t"+workdir+"\tT-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-windows ]; then printf 'one\n'; exit 0; fi
if [ "$1" = list-panes ]; then printf 'one\t`+workdir+`\n'; exit 0; fi
if [ "$1" = select-window ] || [ "$1" = attach ]; then exit 0; fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := runWithDiscardedStdout([]string{"--config", configPath, "launch", "mac", "Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "select-window -t Amp:1") || !strings.Contains(log, "attach -t Amp") {
		t.Fatalf("existing matching session was not attached\nlog:\n%s", log)
	}
}

func TestLaunchDoesNotAutoAttachToExistingDriftedSession(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tone\t"+workdir+"\tT-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-windows ]; then printf 'one\nextra\n'; exit 0; fi
if [ "$1" = list-panes ]; then printf 'one\t`+workdir+`\nextra\t/tmp\n'; exit 0; fi
if [ "$1" = new-window ]; then exit 0; fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runWithDiscardedStdout([]string{"--config", configPath, "launch", "mac", "Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "attach") {
		t.Fatalf("existing drifted session was attached\nlog:\n%s", log)
	}
}

func TestLaunchDoesNotAutoAttachAfterRestoringMissingWindow(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tone\t"+workdir+"\tT-one\nmac\ttwo\t"+workdir+"\tT-two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-windows ]; then printf 'one\n'; exit 0; fi
if [ "$1" = list-panes ]; then printf 'one\t`+workdir+`\ntwo\t`+workdir+`\n'; exit 0; fi
if [ "$1" = new-window ]; then exit 0; fi
if [ "$1" = select-window ] || [ "$1" = attach ]; then exit 0; fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runWithDiscardedStdout([]string{"--config", configPath, "launch", "mac", "Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "new-window") {
		t.Fatalf("launch did not restore missing window\nlog:\n%s", log)
	}
	if strings.Contains(log, "attach") {
		t.Fatalf("launch auto-attached after restoring missing window\nlog:\n%s", log)
	}
}

func TestLaunchAttachFlagAttaches(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tone\t"+workdir+"\tT-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tmp, "calls.log")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 1; fi
if [ "$1" = new-session ] || [ "$1" = select-window ] || [ "$1" = attach ]; then exit 0; fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "")

	if err := runWithDiscardedStdout([]string{"--config", configPath, "--attach", "launch", "mac", "Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "select-window -t Amp:1") || !strings.Contains(log, "attach -t Amp") {
		t.Fatalf("launch --attach did not select and attach\nlog:\n%s", log)
	}
}

func TestSpawnCreatesInteractiveAmpWindowAndStoresThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "work dir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-new-thread\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-new-thread ]; then
  printf '{"id":"T-new-thread","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "new win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tnew win\t"+workdir+"\tT-new-thread\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain spawned row\ngot:  %q\nwant: %q", got, want)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"new-session -d -P -F #{window_id} -s Amp -n new win cd '" + workdir + "' && AMUX_WORKSPACE='mac' AMUX_SESSION='Amp' AMUX_WINDOW='new win' AMUX_THREAD_ID='T-new-thread' AMUX_WORKDIR='" + workdir + "' exec amp threads continue 'T-new-thread'",
		"send-keys -t @1 -l hello Amp",
		"send-keys -t @1 Enter",
		"select-window -t @1",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("tmux log missing %q\nlog:\n%s", want, log)
		}
	}
}

func TestSpawnRetriesEnterWhenInitialMessageRemainsInPane(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	readyCountPath := filepath.Join(tmp, "ready-count")
	literalSentPath := filepath.Join(tmp, "literal-sent")
	enterCountPath := filepath.Join(tmp, "enter-count")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-retry-enter\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-retry-enter ]; then
  printf '{"id":"T-retry-enter","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = -l ]; then
  printf 'sent\n' > "`+literalSentPath+`"
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then
  count=0
  if [ -f "`+enterCountPath+`" ]; then count=$(cat "`+enterCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+enterCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  if [ ! -f "`+literalSentPath+`" ]; then
    count=0
    if [ -f "`+readyCountPath+`" ]; then count=$(cat "`+readyCountPath+`"); fi
    count=$((count + 1))
    printf '%s\n' "$count" > "`+readyCountPath+`"
    if [ "$count" -lt 2 ]; then
      printf 'starting Amp\n'
    else
      printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
    fi
    exit 0
  fi
  count=0
  if [ -f "`+enterCountPath+`" ]; then count=$(cat "`+enterCountPath+`"); fi
  if [ "$count" -lt 2 ]; then
    printf '╭ composer ─╮\n│ hello     │\n│ Amp       │\n╰────────────╯\n'
  else
    printf ' ┃ hello\n ┃ Amp\n╭ composer ─╮\n│           │\n╰────────────╯\n'
  fi
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "retry", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "display-message -p -t @1 #{pane_id}") {
		t.Fatalf("spawn did not resolve the spawned window to a pane id before submitting\nlog:\n%s", log)
	}
	readyCheck := "capture-pane -J -p -t %1"
	literalSend := "send-keys -t %1 -l hello Amp"
	if strings.Index(log, readyCheck) == -1 || strings.Index(log, literalSend) == -1 || strings.Index(log, readyCheck) > strings.Index(log, literalSend) {
		t.Fatalf("spawn did not wait for composer readiness before typing\nlog:\n%s", log)
	}
	if got, want := strings.Count(log, "send-keys -t %1 Enter"), 2; got != want {
		t.Fatalf("spawn sent Enter %d times, want %d\nlog:\n%s", got, want, log)
	}
	if !strings.Contains(log, "capture-pane -J -p -t %1") {
		t.Fatalf("spawn did not verify pane contents after submitting\nlog:\n%s", log)
	}
}

func TestSpawnRetypesInitialMessageIfItNeverAppearsAfterFirstSend(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	literalCountPath := filepath.Join(tmp, "literal-count")
	enterCountPath := filepath.Join(tmp, "enter-count")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-retype\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-retype ]; then
  printf '{"id":"T-retype","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = C-u ]; then
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = -l ]; then
  count=0
  if [ -f "`+literalCountPath+`" ]; then count=$(cat "`+literalCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+literalCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then
  count=0
  if [ -f "`+enterCountPath+`" ]; then count=$(cat "`+enterCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+enterCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  count=0
  if [ -f "`+literalCountPath+`" ]; then count=$(cat "`+literalCountPath+`"); fi
  enter_count=0
  if [ -f "`+enterCountPath+`" ]; then enter_count=$(cat "`+enterCountPath+`"); fi
  if [ "$enter_count" -gt 0 ]; then
    printf ' ┃ hello Amp\n╭ composer ─╮\n│           │\n╰────────────╯\n'
  elif [ "$count" -lt 1 ]; then
    printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
  elif [ "$count" -lt 2 ]; then
    printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
  else
    printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  fi
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "retype", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if got, want := strings.Count(log, "send-keys -t %1 -l hello Amp"), 2; got != want {
		t.Fatalf("spawn sent literal %d times, want %d\nlog:\n%s", got, want, log)
	}
	if got, want := strings.Count(log, "send-keys -t %1 C-u"), 1; got != want {
		t.Fatalf("spawn cleared composer %d times, want %d\nlog:\n%s", got, want, log)
	}
	if got, want := strings.Count(log, "send-keys -t %1 Enter"), 1; got != want {
		t.Fatalf("spawn sent Enter %d times, want %d\nlog:\n%s", got, want, log)
	}
}

func TestSpawnSubmitsWhenLongInitialMessageIsNotFullyVisibleInComposer(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	enterCountPath := filepath.Join(tmp, "enter-count")
	longMessage := "Inventory CSB setup paths, compare product variant handling, and report only the smallest spawn reliability fix with test evidence for issue nineteen"
	var stderr bytes.Buffer

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-long-prompt\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-long-prompt ]; then
  if [ ! -f "`+enterCountPath+`" ]; then
    printf '{"id":"T-long-prompt","messages":[]}\n'
    exit 0
  fi
  printf '{"id":"T-long-prompt","messages":[{"role":"user","content":"`+longMessage+`"}]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = rename ] && [ "$3" = T-long-prompt ] && [ "$4" = '#19 long prompt' ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ]; then
  if [ "$4" = Enter ]; then
    count=0
    if [ -f "`+enterCountPath+`" ]; then count=$(cat "`+enterCountPath+`"); fi
    count=$((count + 1))
    printf '%s\n' "$count" > "`+enterCountPath+`"
  fi
  exit 0
fi
if [ "$1" = capture-pane ]; then
  printf '╭ composer ─╮\n│ Inventory CSB setup paths, compare product variant handling │\n╰────────────╯\n'
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := (app{stderr: &stderr}).run([]string{"--config", configPath, "spawn", "--title-prefix", "#19", "long prompt", workdir, longMessage}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "initial message may not have been submitted") {
		t.Fatalf("spawn printed manual-submit warning after pressing Enter and verifying delivery:\n%s", stderr.String())
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if got, want := strings.Count(log, "tmux send-keys -t %1 Enter"), 1; got != want {
		t.Fatalf("spawn sent Enter %d times, want %d\nlog:\n%s", got, want, log)
	}
	for _, want := range []string{
		"tmux send-keys -t %1 -l " + longMessage,
		"amp threads export T-long-prompt",
		"amp threads rename T-long-prompt #19 long prompt",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Contains(log, "amp threads search") {
		t.Fatalf("spawn searched for a different thread despite verified delivery\nlog:\n%s", log)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\t#19 long prompt\t"+workdir+"\tT-long-prompt\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain spawned row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSpawnRefusesToStoreWhenInitialMessageRemainsInComposer(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	var stderr bytes.Buffer

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-still-composer\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-still-composer ]; then
  printf '{"id":"T-still-composer","messages":[]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := (app{stderr: &stderr}).run([]string{"--config", configPath, "spawn", "warn", workdir, "hello Amp"})
	if err == nil {
		t.Fatal("spawn succeeded, want composer verification failure")
	}
	if !strings.Contains(err.Error(), "initial message is still visible in the tmux composer") {
		t.Fatalf("got error %q, want composer diagnostic", err)
	}
}

func TestSpawnRefusesToStoreWhenPaneCannotBeCapturedAfterEnter(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	enterCountPath := filepath.Join(tmp, "enter-count")
	var stderr bytes.Buffer

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-capture-fails-after-enter\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-capture-fails-after-enter ]; then
  printf '{"id":"T-capture-fails-after-enter","messages":[]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = search ]; then
  printf '[]\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then
  printf '1\n' > "`+enterCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  if [ -f "`+enterCountPath+`" ]; then
    printf 'pane disappeared\n' >&2
    exit 1
  fi
  printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := (app{stderr: &stderr}).run([]string{"--config", configPath, "spawn", "capture-fails", workdir, "hello Amp"})
	if err == nil {
		t.Fatal("spawn succeeded, want unverified delivery failure")
	}
	if !strings.Contains(err.Error(), "stored thread is empty or missing the initial message") {
		t.Fatalf("got error %q, want lost/empty diagnostic", err)
	}
}

func TestSpawnRetypesWhenEnterClearsComposerWithoutTranscriptEcho(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	literalCountPath := filepath.Join(tmp, "literal-count")
	enterCountPath := filepath.Join(tmp, "enter-count")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-lost-after-enter\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-lost-after-enter ]; then
  printf '{"id":"T-lost-after-enter","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = -l ]; then
  count=0
  if [ -f "`+literalCountPath+`" ]; then count=$(cat "`+literalCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+literalCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then
  count=0
  if [ -f "`+enterCountPath+`" ]; then count=$(cat "`+enterCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+enterCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  literal_count=0
  if [ -f "`+literalCountPath+`" ]; then literal_count=$(cat "`+literalCountPath+`"); fi
  enter_count=0
  if [ -f "`+enterCountPath+`" ]; then enter_count=$(cat "`+enterCountPath+`"); fi
  if [ "$literal_count" -gt 1 ] && [ "$enter_count" -eq 1 ]; then
    printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  elif [ "$enter_count" -eq 0 ]; then
    printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  elif [ "$enter_count" -eq 1 ]; then
    printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
  elif [ "$literal_count" -gt 1 ]; then
    printf ' ┃ hello Amp\n╭ composer ─╮\n│           │\n╰────────────╯\n'
  else
    printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
  fi
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "lost-after-enter", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if got, want := strings.Count(log, "send-keys -t %1 -l hello Amp"), 2; got != want {
		t.Fatalf("spawn sent literal %d times, want %d\nlog:\n%s", got, want, log)
	}
	if got, want := strings.Count(log, "send-keys -t %1 Enter"), 2; got != want {
		t.Fatalf("spawn sent Enter %d times, want %d\nlog:\n%s", got, want, log)
	}
}

func TestSpawnRefusesToStoreWhenInitialMessageStillInComposer(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	exportCountPath := filepath.Join(tmp, "export-count")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-typed-only\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-typed-only ]; then
  count=0
  if [ -f "`+exportCountPath+`" ]; then count=$(cat "`+exportCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+exportCountPath+`"
  if [ "$count" -eq 1 ]; then
    printf 'temporary export error\n' >&2
    exit 1
  fi
  printf '{"id":"T-typed-only","messages":[]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = archive ] && [ "$3" = T-typed-only ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
if [ "$1" = kill-window ] && [ "$2" = -t ] && [ "$3" = @1 ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := run([]string{"--config", configPath, "spawn", "typed-only", workdir, "hello Amp"})
	if err == nil {
		t.Fatal("spawn succeeded, want typed-only verification failure")
	}
	if !strings.Contains(err.Error(), "initial message is still visible in the tmux composer") {
		t.Fatalf("got error %q, want composer diagnostic", err)
	}
	if configBytes, readErr := os.ReadFile(configPath); readErr == nil && strings.Contains(string(configBytes), "T-typed-only") {
		t.Fatalf("spawn stored unverified thread row: %q", configBytes)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"amp threads archive T-typed-only",
		"tmux kill-window -t @1",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing cleanup call %q\nlog:\n%s", want, log)
		}
	}
}

func TestSpawnRefusesToStoreWhenInitialMessageLandsInDifferentThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	literalSentPath := filepath.Join(tmp, "literal-sent")
	enterCountPath := filepath.Join(tmp, "enter-count")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-stored-empty\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-stored-empty ]; then
  printf '{"id":"T-stored-empty","messages":[]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-actual-recipient ]; then
  printf '{"id":"T-actual-recipient","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = search ]; then
  printf '[{"id":"T-actual-recipient","title":"worker"}]\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = @1 ] && [ "$5" = '#{pane_id}' ]; then
  printf '%%1\n'
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = -l ]; then
  printf 'sent\n' > "`+literalSentPath+`"
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then
  count=0
  if [ -f "`+enterCountPath+`" ]; then count=$(cat "`+enterCountPath+`"); fi
  count=$((count + 1))
  printf '%s\n' "$count" > "`+enterCountPath+`"
  exit 0
fi
if [ "$1" = send-keys ]; then
  exit 0
fi
if [ "$1" = capture-pane ]; then
  enter_count=0
  if [ -f "`+enterCountPath+`" ]; then enter_count=$(cat "`+enterCountPath+`"); fi
  if [ ! -f "`+literalSentPath+`" ]; then
    printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
  elif [ "$enter_count" -eq 0 ]; then
    printf '╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n'
  else
    printf ' ┃ hello Amp\n╭ composer ─╮\n│           │\n╰────────────╯\n'
  fi
  exit 0
fi
if [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := run([]string{"--config", configPath, "spawn", "wrong-thread", workdir, "hello Amp"})
	if err == nil {
		t.Fatal("spawn succeeded, want different-thread verification failure")
	}
	if !strings.Contains(err.Error(), "initial message appears in thread T-actual-recipient instead") {
		t.Fatalf("got error %q, want different-thread diagnostic", err)
	}
	if configBytes, readErr := os.ReadFile(configPath); readErr == nil && strings.Contains(string(configBytes), "T-stored-empty") {
		t.Fatalf("spawn stored wrong thread row: %q", configBytes)
	}
}

func TestTextContainsComposerMessage(t *testing.T) {
	tests := []struct {
		name     string
		pane     string
		message  string
		contains bool
	}{
		{
			name:     "message in composer",
			pane:     "╭ composer ─╮\n│ hello Amp │\n╰────────────╯\n",
			message:  "hello Amp",
			contains: true,
		},
		{
			name:     "message only in transcript above empty composer",
			pane:     " ┃ hello Amp\n╭ composer ─╮\n│           │\n╰────────────╯\n",
			message:  "hello Amp",
			contains: false,
		},
		{
			name:     "wrapped message in composer",
			pane:     "╭ composer ─╮\n│ hello     │\n│ Amp       │\n╰────────────╯\n",
			message:  "hello Amp",
			contains: true,
		},
		{
			name:     "fallback without composer frame",
			pane:     "hello Amp\n",
			message:  "hello Amp",
			contains: false,
		},
		{
			name:     "box drawing characters are normalized in message",
			pane:     "╭ composer ─╮\n│ add separator │\n╰────────────╯\n",
			message:  "add ─ separator",
			contains: true,
		},
		{
			name:     "blank message never matches vacuously",
			pane:     "╭ composer ─╮\n│           │\n╰────────────╯\n",
			message:  "   ",
			contains: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := textContainsComposerMessage(tt.pane, tt.message)
			if got != tt.contains {
				t.Fatalf("got %v, want %v", got, tt.contains)
			}
		})
	}
}

func TestSpawnLongModeFlagCreatesThreadWithMode(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-mode-thread\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-mode-thread ]; then
  printf '{"id":"T-mode-thread","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "--mode", "plan", "mode win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(logBytes); !strings.Contains(got, "threads new --mode plan\n") {
		t.Fatalf("got amp calls %q, want mode thread creation", got)
	}
}

func TestSpawnTitlePrefixRenamesNewThreadAfterSubmittingInitialMessage(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-prefixed-thread\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-prefixed-thread ]; then
  printf '{"id":"T-prefixed-thread","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = rename ] && [ "$3" = T-prefixed-thread ] && [ "$4" = '#255 prefixed win' ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "--title-prefix", "#255", "prefixed win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"amp threads new",
		"amp threads rename T-prefixed-thread #255 prefixed win",
		"tmux new-session -d -P -F #{window_id} -s Amp -n #255 prefixed win",
		"AMUX_WINDOW='#255 prefixed win'",
		"tmux send-keys -t @1 -l hello Amp",
		"tmux send-keys -t @1 Enter",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Index(log, "amp threads rename") < strings.Index(log, "tmux send-keys -t @1 Enter") {
		t.Fatalf("rename did not happen after initial message submission\nlog:\n%s", log)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\t#255 prefixed win\t"+workdir+"\tT-prefixed-thread\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain prefixed spawned row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSpawnTitlePrefixEqualsFlagRenamesNewThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-prefixed-thread\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-prefixed-thread ]; then
  printf '{"id":"T-prefixed-thread","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = rename ] && [ "$3" = T-prefixed-thread ] && [ "$4" = '#255 equals win' ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "--title-prefix=#255", "equals win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(logBytes); !strings.Contains(got, "amp threads rename T-prefixed-thread #255 equals win") {
		t.Fatalf("log missing equals-form rename\nlog:\n%s", got)
	}
}

func TestSpawnTitlePrefixRetriesRenameWhileThreadIsEmpty(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	renameCountPath := filepath.Join(tmp, "rename-count")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-eventually-non-empty\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-eventually-non-empty ]; then
  printf '{"id":"T-eventually-non-empty","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = rename ]; then
  count=0
  if [ -f "`+renameCountPath+`" ]; then
    count=$(cat "`+renameCountPath+`")
  fi
  count=$((count + 1))
  printf '%s' "$count" > "`+renameCountPath+`"
  if [ "$count" -lt 3 ]; then
    printf 'Error: Cannot rename an empty thread.\n' >&2
    exit 1
  fi
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "--title-prefix", "#255", "retry win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if got := strings.Count(log, "amp threads rename T-eventually-non-empty #255 retry win"); got != 3 {
		t.Fatalf("got %d rename attempts, want 3\nlog:\n%s", got, log)
	}
	if strings.Index(log, "amp threads rename") < strings.Index(log, "tmux send-keys -t @1 Enter") {
		t.Fatalf("rename retry happened before initial message submission\nlog:\n%s", log)
	}
}

func TestSpawnTitlePrefixRenameFailureKeepsSpawnedWorkerAndReportsRecovery(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-rename-fails\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-rename-fails ]; then
  printf '{"id":"T-rename-fails","messages":[{"role":"user","content":"hello Amp"}]}\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = rename ]; then
  printf 'rename unavailable\n' >&2
  exit 3
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 1
fi
if [ "$1" = new-session ]; then
  printf '@1\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	var stderr bytes.Buffer
	if err := (app{stderr: &stderr}).run([]string{"--config", configPath, "spawn", "--title-prefix", "#255", "prefixed win", workdir, "hello Amp"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"warning: rename Amp thread T-rename-fails failed",
		"rename unavailable",
		"spawned worker was created and stored as mac/#255 prefixed win",
		"retry with `amp threads rename T-rename-fails \"#255 prefixed win\"`",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q\nstderr:\n%s", want, stderr.String())
		}
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\t#255 prefixed win\t"+workdir+"\tT-rename-fails\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain spawned row after rename failure\ngot:  %q\nwant: %q", got, want)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux new-session -d -P -F #{window_id} -s Amp -n #255 prefixed win",
		"tmux send-keys -t @1 -l hello Amp",
		"tmux send-keys -t @1 Enter",
		"amp threads rename T-rename-fails #255 prefixed win",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log missing %q\nlog:\n%s", want, log)
		}
	}
	if strings.Index(log, "amp threads rename") < strings.Index(log, "tmux send-keys -t @1 Enter") {
		t.Fatalf("rename failure happened before initial message submission\nlog:\n%s", log)
	}
}

func TestSpawnRejectsBlankTitlePrefixBeforeCreatingThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "spawn", "--title-prefix", "   ", "fresh", workdir, "hello"})
	if err == nil {
		t.Fatal("spawn succeeded, want blank title-prefix error")
	}
	if !strings.Contains(err.Error(), "title-prefix must not be blank") {
		t.Fatalf("got error %q, want blank title-prefix error", err)
	}
	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("amp threads new was called before title-prefix validation")
	}
}

func TestSpawnDryRunDoesNotCreateThreadOrWriteConfig(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "--dry-run", "spawn", "dry", workdir, "hello"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn called amp threads new")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn wrote config file")
	}
	for _, want := range []string{
		"Would create Amp thread for mac/dry",
		"Would create tmux session \"Amp\" with window \"dry\"",
		"Would start Amp in " + workdir + " and submit initial message",
		"Would store mac/dry in " + configPath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("dry-run output missing %q\nstdout:\n%s", want, stdout.String())
		}
	}
}

func TestSpawnDryRunShortModeFlagPrintsMode(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "--dry-run", "spawn", "-m", "accept-edits", "dry", workdir, "hello"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn called amp threads new")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn wrote config file")
	}
	for _, want := range []string{
		"Would create Amp thread for mac/dry with mode \"accept-edits\"",
		"Would create tmux session \"Amp\" with window \"dry\"",
		"Would start Amp in " + workdir + " and submit initial message",
		"Would store mac/dry in " + configPath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("dry-run output missing %q\nstdout:\n%s", want, stdout.String())
		}
	}
}

func TestSpawnDryRunTitlePrefixPrintsPlannedRenameWithoutCallingAmp(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 1
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "--dry-run", "spawn", "--title-prefix", "#255", "dry", workdir, "hello"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn called amp")
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn wrote config file")
	}
	for _, want := range []string{
		"Would create Amp thread for mac/#255 dry",
		"Would rename new Amp thread to \"#255 dry\"",
		"Would create tmux session \"Amp\" with window \"#255 dry\"",
		"Would start Amp in " + workdir + " and submit initial message",
		"Would store mac/#255 dry in " + configPath,
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("dry-run output missing %q\nstdout:\n%s", want, stdout.String())
		}
	}
	if strings.Index(stdout.String(), "Would rename new Amp thread") < strings.Index(stdout.String(), "Would start Amp") {
		t.Fatalf("dry-run rename appeared before initial message submission\nstdout:\n%s", stdout.String())
	}
}

func TestSpawnDryRunRefusesExistingWindowBeforeCreatingThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 0
fi
if [ "$1" = list-windows ]; then
  printf 'existing\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", configPath, "--dry-run", "spawn", "existing", workdir, "hello"})
	if err == nil {
		t.Fatal("dry-run spawn succeeded, want existing-window error")
	}
	if !strings.Contains(err.Error(), `window "existing" already exists in tmux session "Amp"`) {
		t.Fatalf("got error %q, want existing-window error", err)
	}
	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run spawn called amp threads new")
	}
}

func TestSpawnRefusesExistingWindowBeforeCreatingThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then
  exit 0
fi
if [ "$1" = list-windows ]; then
  printf 'existing\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := run([]string{"--config", configPath, "spawn", "existing", workdir, "hello"})
	if err == nil {
		t.Fatal("spawn succeeded, want existing-window error")
	}
	if !strings.Contains(err.Error(), `window "existing" already exists in tmux session "Amp"`) {
		t.Fatalf("got error %q, want existing-window error", err)
	}
	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("amp threads new was called before existing-window check")
	}
}

func TestSpawnAddsWindowToExistingSession(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
if [ "$1" = threads ] && [ "$2" = new ]; then
  printf 'T-existing-session\n'
  exit 0
fi
if [ "$1" = threads ] && [ "$2" = export ] && [ "$3" = T-existing-session ]; then
  printf '{"id":"T-existing-session","messages":[{"role":"user","content":"hello"}]}\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = has-session ]; then
  exit 0
fi
if [ "$1" = list-windows ]; then
  printf 'already-there\n'
  exit 0
fi
if [ "$1" = new-window ]; then
  printf '@7\n'
  exit 0
fi
if [ "$1" = send-keys ] || [ "$1" = select-window ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	if err := run([]string{"--config", configPath, "spawn", "fresh", workdir, "hello"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "new-session") {
		t.Fatalf("spawn created a new session despite existing session\nlog:\n%s", log)
	}
	for _, want := range []string{
		"new-window -P -F #{window_id} -t Amp -n fresh cd '" + workdir + "' && AMUX_WORKSPACE='mac' AMUX_SESSION='Amp' AMUX_WINDOW='fresh' AMUX_THREAD_ID='T-existing-session' AMUX_WORKDIR='" + workdir + "' exec amp threads continue 'T-existing-session'",
		"send-keys -t @7 -l hello",
		"send-keys -t @7 Enter",
		"select-window -t @7",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("tmux log missing %q\nlog:\n%s", want, log)
		}
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tfresh\t"+workdir+"\tT-existing-session\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain spawned row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestSpawnRejectsInvalidInitialMessageBeforeCreatingThread(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "workdir")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	ampCalledPath := filepath.Join(tmp, "amp-called")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
touch "`+ampCalledPath+`"
printf 'T-should-not-exist\n'
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "spawn", "fresh", workdir, "hello\nAmp"})
	if err == nil {
		t.Fatal("spawn succeeded, want invalid initial-message error")
	}
	if !strings.Contains(err.Error(), "initial-message must not contain tabs or newlines") {
		t.Fatalf("got error %q, want invalid initial-message error", err)
	}
	if _, err := os.Stat(ampCalledPath); !os.IsNotExist(err) {
		t.Fatalf("amp threads new was called before initial-message validation")
	}
}

func TestStoreCurrentInfersWindowAndWorkdirFromTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ]; then
  case "$5" in
    '#W') printf 'current-window\n'; exit 0 ;;
    '#{pane_current_path}') printf '/tmp/current workdir\n'; exit 0 ;;
  esac
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	if err := run([]string{"--config", configPath, "store-current", "T-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tcurrent-window\t/tmp/current workdir\tT-current\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain inferred current row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestStoreCurrentRequiresInvokingPaneWhenInsideTmux(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "store-current", "T-current"})
	if err == nil {
		t.Fatal("store-current succeeded without TMUX_PANE, want error")
	}
	if !strings.Contains(err.Error(), "TMUX_PANE is unavailable") {
		t.Fatalf("got error %q, want missing TMUX_PANE guidance", err)
	}
	if !strings.Contains(err.Error(), "run amux from the pane you want to target") {
		t.Fatalf("got error %q, want actionable target guidance", err)
	}
}

func TestStoreCurrentTargetsInvokingPaneInsteadOfFocusedClient(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ]; then
  case "$5" in
    '#W') printf 'pane-window\n'; exit 0 ;;
    '#{pane_current_path}') printf '/tmp/pane workdir\n'; exit 0 ;;
  esac
fi
if [ "$1" = display-message ] && [ "$2" = -p ]; then
  case "$3" in
    '#W') printf 'focused-window\n'; exit 0 ;;
    '#{pane_current_path}') printf '/tmp/focused workdir\n'; exit 0 ;;
  esac
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	if err := run([]string{"--config", configPath, "store-current", "T-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tpane-window\t/tmp/pane workdir\tT-current\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain invoking pane row\ngot:  %q\nwant: %q", got, want)
	}
	if strings.Contains(string(configBytes), "focused-window") {
		t.Fatalf("config used focused-client window instead of invoking pane: %q", configBytes)
	}
}

func TestStoreCurrentWithExplicitWindowAndWorkdirDoesNotRequireTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	t.Setenv("TMUX", "")

	if err := run([]string{"--config", configPath, "store-current", "mac", "T-current", "explicit-window", "/tmp/explicit-workdir"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\texplicit-window\t/tmp/explicit-workdir\tT-current\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain explicit current row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestPinAndUnpinAreConfigOnlyAliases(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	var stdout bytes.Buffer

	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "pin", "mac", "pinned", "/tmp/pinned", "T-pinned"}); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Pinned mac/pinned"; !strings.Contains(got, want) {
		t.Fatalf("pin output missing %q\nstdout: %s", want, got)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tpinned\t/tmp/pinned\tT-pinned\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain pinned row\ngot:  %q\nwant: %q", got, want)
	}

	stdout.Reset()
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "unpin", "mac", "pinned"}); err != nil {
		t.Fatal(err)
	}
	if got, want := stdout.String(), "Unpinned mac/pinned"; !strings.Contains(got, want) {
		t.Fatalf("unpin output missing %q\nstdout: %s", want, got)
	}

	configBytes, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configBytes), "pinned") {
		t.Fatalf("config still contains unpinned row: %q", configBytes)
	}
}

func TestPinCurrentAndUnpinCurrentAreConfigOnlyAliases(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	t.Setenv("TMUX", "")

	if err := run([]string{"--config", configPath, "pin-current", "mac", "T-current", "pinned-current", "/tmp/pinned-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(configBytes), "mac\tpinned-current\t/tmp/pinned-current\tT-current\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not contain pinned current row\ngot:  %q\nwant: %q", got, want)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ] && [ "$5" = '#W' ]; then
  printf 'pinned-current\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	if err := run([]string{"--config", configPath, "unpin-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(configBytes), "pinned-current") {
		t.Fatalf("config still contains unpinned current row: %q", configBytes)
	}
}

func TestRemoveCurrentInfersWindowFromTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent-window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ] && [ "$5" = '#W' ]; then
  printf 'current-window\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	if err := run([]string{"--config", configPath, "remove-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(configBytes)
	if strings.Contains(got, "current-window") {
		t.Fatalf("config still contains removed current window: %q", got)
	}
	if want := "mac\tother\t/tmp\tT-other\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not preserve other row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRemoveCurrentTargetsInvokingPaneInsteadOfFocusedClient(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tpane-window\t/tmp/pane\tT-pane\nmac\tfocused-window\t/tmp/focused\tT-focused\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ] && [ "$5" = '#W' ]; then
  printf 'pane-window\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = '#W' ]; then
  printf 'focused-window\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	if err := run([]string{"--config", configPath, "remove-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(configBytes)
	if strings.Contains(got, "pane-window") {
		t.Fatalf("config still contains invoking pane window: %q", got)
	}
	if want := "mac\tfocused-window\t/tmp/focused\tT-focused\n"; !strings.Contains(got, want) {
		t.Fatalf("config did not preserve focused-client row\ngot:  %q\nwant: %q", got, want)
	}
}

func TestRemoveCurrentRequiresTmux(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMUX", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "remove-current"})
	if err == nil {
		t.Fatal("remove-current succeeded outside tmux, want error")
	}
	if !strings.Contains(err.Error(), "current tmux window is unavailable: run inside tmux") {
		t.Fatalf("got error %q, want outside-tmux error", err)
	}
}

func TestRemoveCurrentRequiresInvokingPaneWhenInsideTmux(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "remove-current"})
	if err == nil {
		t.Fatal("remove-current succeeded without TMUX_PANE, want error")
	}
	if !strings.Contains(err.Error(), "TMUX_PANE is unavailable") {
		t.Fatalf("got error %q, want missing TMUX_PANE guidance", err)
	}
	if !strings.Contains(err.Error(), "run amux from the pane you want to target") {
		t.Fatalf("got error %q, want actionable target guidance", err)
	}
}

func TestParkCurrentRemovesRestoreRowAndKillsCapturedWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ]; then
  case "$5" in
    '#S:#I') printf 'Amp:7\n'; exit 0 ;;
    '#W') printf 'current window\n'; exit 0 ;;
  esac
fi
if [ "$1" = run-shell ] && [ "$2" = -b ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")
	t.Setenv("AMUX_PARK_GRACE_PERIOD", "0")
	t.Setenv("AMUX_PARK_SHUTDOWN_DELAY", "0")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "park-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gotConfig := string(configBytes)
	if strings.Contains(gotConfig, "current window") {
		t.Fatalf("config still contains parked window: %q", gotConfig)
	}
	if want := "mac\tother\t/tmp\tT-other\n"; !strings.Contains(gotConfig, want) {
		t.Fatalf("config did not preserve other row\ngot:  %q\nwant: %q", gotConfig, want)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "run-shell -b") {
		t.Fatalf("tmux log did not schedule captured target shutdown\nlog:\n%s", log)
	}
	if !strings.Contains(stdout.String(), "Amp thread history is not deleted") {
		t.Fatalf("stdout did not explain Amp history semantics: %q", stdout.String())
	}
}

func TestParkCurrentRequiresInvokingPaneWhenInsideTmux(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "park-current"})
	if err == nil {
		t.Fatal("park-current succeeded without TMUX_PANE, want error")
	}
	if !strings.Contains(err.Error(), "TMUX_PANE is unavailable") {
		t.Fatalf("got error %q, want missing TMUX_PANE guidance", err)
	}
	if !strings.Contains(err.Error(), "run amux from the pane you want to target") {
		t.Fatalf("got error %q, want actionable target guidance", err)
	}
}

func TestParkCurrentGracefullyStopsPaneBeforeKillingWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ] && [ "$5" = '#S:#I' ]; then
  printf 'Amp:7\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ] && [ "$5" = '#W' ]; then
  printf 'current window\n'
  exit 0
fi
if [ "$1" = run-shell ] && [ "$2" = -b ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")
	t.Setenv("AMUX_PARK_SHUTDOWN_DELAY", "0")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "park-current"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"run-shell -b",
		"target='Amp:7'",
		"tmux send-keys -t \"$target\" C-c",
		"tmux send-keys -t \"$target\" C-d",
		"tmux kill-window -t \"$target\"",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("tmux log missing deferred shutdown command %q\nlog:\n%s", want, log)
		}
	}
	if strings.Contains(log, "\nsend-keys") || strings.Contains(log, "\nkill-window") {
		t.Fatalf("park-current stopped pane synchronously instead of scheduling shutdown\nlog:\n%s", log)
	}
}

func TestParkCurrentTargetsInvokingPaneInsteadOfFocusedClient(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tpane-window\t/tmp/pane\tT-pane\nmac\tfocused-window\t/tmp/focused\tT-focused\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ]; then
  case "$5" in
    '#S:#I') printf 'Amp:3\n'; exit 0 ;;
    '#W') printf 'pane-window\n'; exit 0 ;;
  esac
fi
if [ "$1" = display-message ] && [ "$2" = -p ]; then
  case "$3" in
    '#S:#I') printf 'Amp:5\n'; exit 0 ;;
    '#W') printf 'focused-window\n'; exit 0 ;;
  esac
fi
if [ "$1" = run-shell ] && [ "$2" = -b ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")
	t.Setenv("AMUX_PARK_GRACE_PERIOD", "0")
	t.Setenv("AMUX_PARK_SHUTDOWN_DELAY", "0")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "park-current"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gotConfig := string(configBytes)
	if strings.Contains(gotConfig, "pane-window") {
		t.Fatalf("config still contains invoking pane window: %q", gotConfig)
	}
	if !strings.Contains(gotConfig, "mac\tfocused-window\t/tmp/focused\tT-focused\n") {
		t.Fatalf("config did not preserve focused-client window\ngot: %q", gotConfig)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "display-message -p -t %42 #W") {
		t.Fatalf("tmux log did not target invoking pane for window lookup\nlog:\n%s", log)
	}
	if !strings.Contains(log, "target='Amp:3'") {
		t.Fatalf("tmux log did not schedule invoking pane window shutdown\nlog:\n%s", log)
	}
}

func TestParkCurrentRequiresTmux(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMUX", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "park-current"})
	if err == nil {
		t.Fatal("park-current succeeded outside tmux, want error")
	}
	if !strings.Contains(err.Error(), "current tmux window is unavailable: run inside tmux") {
		t.Fatalf("got error %q, want outside-tmux error", err)
	}
}

func TestTeardownFromSpawnIdentityArchivesRemovesAndStopsWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tworker\t/tmp/project\tT-worker\nmac\tother\t/tmp/other\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := teardownExpectedStartCommand(
		teardownIdentity{Workspace: "mac", Session: "Amp", Window: "worker", Thread: "T-worker"},
		config.Row{Workspace: "mac", Window: "worker", Workdir: "/tmp/project", Thread: "T-worker"},
	)

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = archive ] && [ "$3" = T-worker ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'worker\t@7\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", startCommand))+`
  printf 'other\t@8\tcd /tmp/other && AMUX_THREAD_ID=T-other exec amp threads continue T-other\n'
  exit 0
fi
if [ "$1" = kill-window ] && [ "$2" = -t ] && [ "$3" = @7 ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "mac")
	t.Setenv("AMUX_SESSION", "Amp")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-worker")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "teardown"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gotConfig := string(configBytes)
	if strings.Contains(gotConfig, "worker") {
		t.Fatalf("config still contains torn-down row: %q", gotConfig)
	}
	if !strings.Contains(gotConfig, "mac\tother\t/tmp/other\tT-other\n") {
		t.Fatalf("config did not preserve other row: %q", gotConfig)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux list-panes -s -t Amp -F #{window_name}\t#{window_id}\t#{pane_start_command}",
		"amp threads archive T-worker",
		"tmux kill-window -t @7",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("teardown log missing %q\nlog:\n%s", want, log)
		}
	}
	for _, want := range []string{
		"Unpinned mac/worker",
		"Archived Amp thread T-worker",
		"Stopped tmux window Amp/worker (@7)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("teardown output missing %q\nstdout:\n%s", want, stdout.String())
		}
	}
}

func TestTeardownExplicitWorkspaceWindowArchivesRemovesAndStopsWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("bta\tpr-11840\t/tmp/project\tT-explicit\nbta\tother\t/tmp/other\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := tmux.ContinueCommand("/tmp/project", "T-explicit")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = archive ] && [ "$3" = T-explicit ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'pr-11840\t@11\t%s\n' `+shellSingleQuote(startCommand)+`
  printf 'other\t@12\tcd /tmp/other && exec amp threads continue T-other\n'
  exit 0
fi
if [ "$1" = kill-window ] && [ "$2" = -t ] && [ "$3" = @11 ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "")
	t.Setenv("AMUX_SESSION", "")
	t.Setenv("AMUX_WINDOW", "")
	t.Setenv("AMUX_THREAD_ID", "")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "teardown", "bta", "pr-11840", "BTA"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gotConfig := string(configBytes)
	if strings.Contains(gotConfig, "pr-11840") {
		t.Fatalf("config still contains torn-down row: %q", gotConfig)
	}
	if !strings.Contains(gotConfig, "bta\tother\t/tmp/other\tT-other\n") {
		t.Fatalf("config did not preserve other row: %q", gotConfig)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux list-panes -s -t BTA -F #{window_name}\t#{window_id}\t#{pane_start_command}",
		"amp threads archive T-explicit",
		"tmux kill-window -t @11",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("explicit teardown log missing %q\nlog:\n%s", want, log)
		}
	}
	for _, want := range []string{
		"Unpinned bta/pr-11840",
		"Archived Amp thread T-explicit",
		"Stopped tmux window BTA/pr-11840 (@11)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("explicit teardown output missing %q\nstdout:\n%s", want, stdout.String())
		}
	}
}

func TestTeardownExplicitWorkspaceWindowAcceptsQuotedSpawnCommand(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("amux\tworker\t/tmp/project\tT-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := teardownExpectedStartCommand(
		teardownIdentity{Workspace: "amux", Session: "Amux", Window: "worker", Thread: "T-worker"},
		config.Row{Workspace: "amux", Window: "worker", Workdir: "/tmp/project", Thread: "T-worker"},
	)

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = archive ] && [ "$3" = T-worker ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'worker\t@14\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", startCommand))+`
  exit 0
fi
if [ "$1" = kill-window ] && [ "$2" = -t ] && [ "$3" = @14 ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "")
	t.Setenv("AMUX_SESSION", "")
	t.Setenv("AMUX_WINDOW", "")
	t.Setenv("AMUX_THREAD_ID", "")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "teardown", "amux", "worker", "Amux"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"amp threads archive T-worker",
		"tmux kill-window -t @14",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("explicit teardown log missing %q\nlog:\n%s", want, log)
		}
	}
	if !strings.Contains(stdout.String(), "Stopped tmux window Amux/worker (@14)") {
		t.Fatalf("explicit teardown output missing stopped window\nstdout:\n%s", stdout.String())
	}
}

func TestTeardownByThreadArchivesRemovesAndStopsVerifiedWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	threadURL := "https://ampcode.com/threads/T-worker"
	if err := os.WriteFile(configPath, []byte("kelas\tmailgun-258-failures\t/tmp/project\t"+threadURL+"\nkelas\tother\t/tmp/other\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := teardownExpectedStartCommand(
		teardownIdentity{Workspace: "kelas", Session: "Kelas", Window: "mailgun-258-failures", Thread: threadURL},
		config.Row{Workspace: "kelas", Window: "mailgun-258-failures", Workdir: "/tmp/project", Thread: threadURL},
	)

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
if [ "$1" = threads ] && [ "$2" = archive ] && [ "$3" = T-worker ]; then
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'mailgun-258-failures\t@21\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", startCommand))+`
  printf 'other\t@22\tcd /tmp/other && exec amp threads continue T-other\n'
  exit 0
fi
if [ "$1" = kill-window ] && [ "$2" = -t ] && [ "$3" = @21 ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "")
	t.Setenv("AMUX_SESSION", "")
	t.Setenv("AMUX_WINDOW", "")
	t.Setenv("AMUX_THREAD_ID", "")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "teardown", "--thread", "T-worker", "--session", "Kelas"}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	gotConfig := string(configBytes)
	if strings.Contains(gotConfig, "mailgun-258-failures") {
		t.Fatalf("config still contains torn-down row: %q", gotConfig)
	}
	if !strings.Contains(gotConfig, "kelas\tother\t/tmp/other\tT-other\n") {
		t.Fatalf("config did not preserve other row: %q", gotConfig)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux list-panes -s -t Kelas -F #{window_name}\t#{window_id}\t#{pane_start_command}",
		"amp threads archive T-worker",
		"tmux kill-window -t @21",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("thread teardown log missing %q\nlog:\n%s", want, log)
		}
	}
	for _, want := range []string{
		"Unpinned kelas/mailgun-258-failures",
		"Archived Amp thread T-worker",
		"Stopped tmux window Kelas/mailgun-258-failures (@21)",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("thread teardown output missing %q\nstdout:\n%s", want, stdout.String())
		}
	}
}

func TestTeardownByThreadWithoutSessionFindsUniqueVerifiedWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("kelas\tmailgun-258-failures\t/tmp/project\tT-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := teardownExpectedStartCommand(
		teardownIdentity{Workspace: "kelas", Session: "Kelas", Window: "mailgun-258-failures", Thread: "T-worker"},
		config.Row{Workspace: "kelas", Window: "mailgun-258-failures", Workdir: "/tmp/project", Thread: "T-worker"},
	)

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ] && [ "$2" = -a ]; then
  printf 'Kelas\tmailgun-258-failures\t@21\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", startCommand))+`
  printf 'Other\tmailgun-258-failures\t@22\tcd /tmp/other && exec amp threads continue T-other\n'
  exit 0
fi
if [ "$1" = list-panes ] && [ "$2" = -s ]; then
  printf 'mailgun-258-failures\t@21\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", startCommand))+`
  exit 0
fi
if [ "$1" = kill-window ] && [ "$2" = -t ] && [ "$3" = @21 ]; then
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "")
	t.Setenv("AMUX_SESSION", "")
	t.Setenv("AMUX_WINDOW", "")
	t.Setenv("AMUX_THREAD_ID", "")

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).run([]string{"--config", configPath, "teardown", "--thread", "T-worker"}); err != nil {
		t.Fatal(err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	for _, want := range []string{
		"tmux list-panes -a -F #{session_name}\t#{window_name}\t#{window_id}\t#{pane_start_command}",
		"amp threads archive T-worker",
		"tmux kill-window -t @21",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("thread teardown log missing %q\nlog:\n%s", want, log)
		}
	}
	if !strings.Contains(stdout.String(), "Stopped tmux window Kelas/mailgun-258-failures (@21)") {
		t.Fatalf("thread teardown output missing resolved session\nstdout:\n%s", stdout.String())
	}
}

func TestTeardownByThreadFailsClosedOnStartCommandMismatch(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("kelas\tmailgun-258-failures\t/tmp/project\tT-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'mailgun-258-failures\t@21\tcd /tmp/other && exec amp threads continue T-other\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", configPath, "teardown", "--thread", "T-worker", "--session", "Kelas"})
	if err == nil {
		t.Fatal("thread teardown succeeded, want start-command mismatch")
	}
	if !strings.Contains(err.Error(), "does not match restore row thread T-worker") {
		t.Fatalf("got error %q, want start-command mismatch", err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "amp threads archive") || strings.Contains(log, "tmux kill-window") {
		t.Fatalf("thread teardown archived or killed despite mismatch\nlog:\n%s", log)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), "kelas\tmailgun-258-failures\t/tmp/project\tT-worker\n") {
		t.Fatalf("thread teardown removed row despite mismatch: %q", configBytes)
	}
}

func TestTeardownByThreadFailsClosedWhenNoRestoreRowMatches(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("kelas\tmailgun-258-failures\t/tmp/project\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", configPath, "teardown", "--thread", "T-worker", "--session", "Kelas"})
	if err == nil {
		t.Fatal("thread teardown succeeded, want missing row")
	}
	if !strings.Contains(err.Error(), "no restore row for thread T-worker") {
		t.Fatalf("got error %q, want missing row", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		logBytes, _ := os.ReadFile(logPath)
		t.Fatalf("thread teardown called amp or tmux despite missing row\nlog:\n%s", logBytes)
	}
}

func TestTeardownByThreadFailsClosedWhenRestoreRowsAreAmbiguous(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("kelas\tone\t/tmp/one\tT-worker\nother\ttwo\t/tmp/two\thttps://ampcode.com/threads/T-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", configPath, "teardown", "--thread", "T-worker"})
	if err == nil {
		t.Fatal("thread teardown succeeded, want ambiguous rows")
	}
	if !strings.Contains(err.Error(), "ambiguous restore rows for thread T-worker") {
		t.Fatalf("got error %q, want ambiguous rows", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		logBytes, _ := os.ReadFile(logPath)
		t.Fatalf("thread teardown called amp or tmux despite ambiguous rows\nlog:\n%s", logBytes)
	}
}

func TestTeardownByThreadWithoutSessionFailsClosedWhenNoLiveWindowVerifies(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("kelas\tmailgun-258-failures\t/tmp/project\tT-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ] && [ "$2" = -a ]; then
  printf 'Kelas\tmailgun-258-failures\t@21\tcd /tmp/other && exec amp threads continue T-other\n'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", configPath, "teardown", "--thread", "T-worker"})
	if err == nil {
		t.Fatal("thread teardown succeeded, want no verified live window")
	}
	if !strings.Contains(err.Error(), "no live tmux window for thread T-worker matches restore row kelas/mailgun-258-failures") {
		t.Fatalf("got error %q, want no verified live window", err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "amp threads archive") || strings.Contains(log, "tmux kill-window") {
		t.Fatalf("thread teardown archived or killed despite no verified live window\nlog:\n%s", log)
	}
}

func TestTeardownByThreadWithoutSessionFailsClosedWhenLiveWindowsAreAmbiguous(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("kelas\tmailgun-258-failures\t/tmp/project\tT-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := teardownExpectedStartCommand(
		teardownIdentity{Workspace: "kelas", Session: "Kelas", Window: "mailgun-258-failures", Thread: "T-worker"},
		config.Row{Workspace: "kelas", Window: "mailgun-258-failures", Workdir: "/tmp/project", Thread: "T-worker"},
	)
	otherStartCommand := teardownExpectedStartCommand(
		teardownIdentity{Workspace: "kelas", Session: "Other", Window: "mailgun-258-failures", Thread: "T-worker"},
		config.Row{Workspace: "kelas", Window: "mailgun-258-failures", Workdir: "/tmp/project", Thread: "T-worker"},
	)

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ] && [ "$2" = -a ]; then
  printf 'Kelas\tmailgun-258-failures\t@21\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", startCommand))+`
  printf 'Other\tmailgun-258-failures\t@22\t%s\n' `+shellSingleQuote(fmt.Sprintf("%q", otherStartCommand))+`
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := run([]string{"--config", configPath, "teardown", "--thread", "T-worker"})
	if err == nil {
		t.Fatal("thread teardown succeeded, want ambiguous live windows")
	}
	for _, want := range []string{
		"ambiguous live tmux windows for thread T-worker",
		"Kelas/@21",
		"Other/@22",
		"pass --session",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("got error %q, want %q", err, want)
		}
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "amp threads archive") || strings.Contains(log, "tmux kill-window") {
		t.Fatalf("thread teardown archived or killed despite ambiguous live windows\nlog:\n%s", log)
	}
}

func TestTeardownRejectsInvalidThreadOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "session-without-thread", args: []string{"teardown", "--session", "Kelas"}, want: "--session requires --thread"},
		{name: "unknown-option", args: []string{"teardown", "--Thread", "T-worker"}, want: "unknown teardown option --Thread"},
		{name: "thread-with-extra-positional", args: []string{"teardown", "--thread", "T-worker", "extra"}, want: "usage: amux teardown --thread <thread-id-or-url> [--session <session>]"},
		{name: "thread-empty-equals", args: []string{"teardown", "--thread="}, want: "--thread requires a thread id or URL"},
		{name: "session-empty-equals", args: []string{"teardown", "--thread", "T-worker", "--session="}, want: "--session requires a tmux session name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--config", filepath.Join(t.TempDir(), "workspaces.tsv")}, tc.args...)
			err := run(args)
			if err == nil {
				t.Fatal("teardown succeeded, want option error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got error %q, want %q", err, tc.want)
			}
		})
	}
}

func TestTeardownExplicitWorkspaceWindowFailsClosedOnThreadMismatch(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("bta\tpr-11840\t/tmp/project\tT-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	startCommand := tmux.ContinueCommand("/tmp/project", "T-other")

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'pr-11840\t@11\t%s\n' `+shellSingleQuote(startCommand)+`
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "")
	t.Setenv("AMUX_SESSION", "")
	t.Setenv("AMUX_WINDOW", "")
	t.Setenv("AMUX_THREAD_ID", "")

	err := run([]string{"--config", configPath, "teardown", "bta", "pr-11840", "BTA"})
	if err == nil {
		t.Fatal("explicit teardown succeeded, want thread mismatch")
	}
	if !strings.Contains(err.Error(), "does not match restore row thread T-config") {
		t.Fatalf("got error %q, want thread mismatch", err)
	}

	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "amp threads archive") || strings.Contains(log, "tmux kill-window") {
		t.Fatalf("explicit teardown archived or killed despite mismatch\nlog:\n%s", log)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), "bta\tpr-11840\t/tmp/project\tT-config\n") {
		t.Fatalf("explicit teardown removed row despite mismatch: %q", configBytes)
	}
}

func TestTeardownFailsClosedOnUnexpectedStartCommand(t *testing.T) {
	for _, tc := range []struct {
		name         string
		startCommand string
	}{
		{name: "blank", startCommand: ""},
		{name: "substring-thread", startCommand: "cd /tmp/project && AMUX_THREAD_ID=T-worker2 exec amp threads continue T-worker2"},
		{name: "not-amux-spawn", startCommand: "echo about T-worker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			configPath := filepath.Join(tmp, "workspaces.tsv")
			logPath := filepath.Join(tmp, "calls.log")
			if err := os.WriteFile(configPath, []byte("mac\tworker\t/tmp/project\tT-worker\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
			writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  printf 'worker\t@7\t%s\n' `+shellSingleQuote(tc.startCommand)+`
  exit 0
fi
if [ "$1" = kill-window ]; then
  exit 0
fi
exit 2
`)

			t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("AMUX_WORKSPACE", "mac")
			t.Setenv("AMUX_SESSION", "Amp")
			t.Setenv("AMUX_WINDOW", "worker")
			t.Setenv("AMUX_THREAD_ID", "T-worker")

			err := run([]string{"--config", configPath, "teardown"})
			if err == nil {
				t.Fatal("teardown succeeded, want start-command mismatch")
			}
			if !strings.Contains(err.Error(), "not the expected amux-spawned command") {
				t.Fatalf("got error %q, want start-command mismatch", err)
			}

			logBytes, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			log := string(logBytes)
			if strings.Contains(log, "amp threads archive") || strings.Contains(log, "tmux kill-window") {
				t.Fatalf("teardown archived or killed despite start-command mismatch\nlog:\n%s", log)
			}
			configBytes, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(configBytes), "mac\tworker\t/tmp/project\tT-worker\n") {
				t.Fatalf("teardown removed row despite start-command mismatch: %q", configBytes)
			}
		})
	}
}

func TestTeardownFailsClosedOnThreadMismatchBeforeArchiveOrKill(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tworker\t/tmp/project\tT-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "amp"), `#!/bin/sh
printf 'amp %s\n' "$*" >> "`+logPath+`"
exit 0
`)
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
exit 0
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMUX_WORKSPACE", "mac")
	t.Setenv("AMUX_SESSION", "Amp")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-env")

	err := run([]string{"--config", configPath, "teardown"})
	if err == nil {
		t.Fatal("teardown succeeded, want thread mismatch")
	}
	if !strings.Contains(err.Error(), "restore row thread mismatch") {
		t.Fatalf("got error %q, want thread mismatch", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		logBytes, _ := os.ReadFile(logPath)
		t.Fatalf("teardown called amp or tmux before failing closed\nlog:\n%s", logBytes)
	}
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configBytes), "mac\tworker\t/tmp/project\tT-config\n") {
		t.Fatalf("teardown removed row despite mismatch: %q", configBytes)
	}
}

func TestTeardownRequiresSpawnIdentity(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AMUX_WORKSPACE", "")
	t.Setenv("AMUX_SESSION", "")
	t.Setenv("AMUX_WINDOW", "")
	t.Setenv("AMUX_THREAD_ID", "")

	err := run([]string{"--config", filepath.Join(tmp, "workspaces.tsv"), "teardown"})
	if err == nil {
		t.Fatal("teardown succeeded without AMUX identity")
	}
	if !strings.Contains(err.Error(), "teardown requires spawn-injected identity") {
		t.Fatalf("got error %q, want missing identity guidance", err)
	}
	if !strings.Contains(err.Error(), "amux teardown --thread <thread-id-or-url> [--session <session>]") {
		t.Fatalf("got error %q, want --thread guidance", err)
	}
}

func TestDoctorFailsWhenWorkspaceHasNoRows(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\twin\t/tmp\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "tmux"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runWithDiscardedStdout([]string{"--config", configPath, "doctor", "missing"})
	if err == nil {
		t.Fatal("doctor succeeded for missing workspace, want error")
	}
	if !strings.Contains(err.Error(), "doctor found problems") {
		t.Fatalf("got error %q, want doctor failure", err)
	}
}

func TestDoctorDoesNotCreateMissingConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "missing", "workspaces.tsv")
	writeExecutable(t, filepath.Join(tmp, "tmux"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := runWithDiscardedStdout([]string{"--config", configPath, "doctor", "mac", "Amp"})
	if err == nil {
		t.Fatal("doctor succeeded with missing config, want error")
	}
	if !strings.Contains(err.Error(), "doctor found problems") {
		t.Fatalf("got error %q, want doctor failure", err)
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("doctor created or touched missing config path: stat err = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(statErr) {
		t.Fatalf("doctor created missing config directory: stat err = %v", statErr)
	}
}

func TestDoctorFailsWhenInsideTmuxButTmuxCannotBeQueried(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\twin\t/tmp\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ]; then
  exit 2
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	err := runWithDiscardedStdout([]string{"--config", configPath, "doctor", "mac"})
	if err == nil {
		t.Fatal("doctor succeeded despite broken tmux query, want error")
	}
	if !strings.Contains(err.Error(), "doctor found problems") {
		t.Fatalf("got error %q, want doctor failure", err)
	}
}

func TestDoctorPassesWhenInsideTmuxCanBeQueried(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\twin\t/tmp\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = -t ] && [ "$4" = "%42" ]; then
  case "$5" in
    '#W') printf 'win\n'; exit 0 ;;
    '#{pane_current_path}') printf '/tmp\n'; exit 0 ;;
  esac
fi
if [ "$1" = list-panes ]; then
  printf 'win\t/tmp\n'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "%42")

	if err := runWithDiscardedStdout([]string{"--config", configPath, "doctor", "mac"}); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorReportsLiveWindowNotStoredInWorkspace(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tamux\t/tmp\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = list-panes ]; then
  printf 'amux\t/tmp\nextra\t/tmp\n'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, err := runCapturingStdout(t, []string{"--config", configPath, "doctor", "mac"})
	if err == nil {
		t.Fatal("doctor succeeded despite unstored live window, want error")
	}
	if !strings.Contains(output, "FAIL stored window extra") {
		t.Fatalf("doctor output did not report unstored live window\n%s", output)
	}
}

func TestDoctorReportsConfiguredWindowNotRunning(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tmissing\t/tmp\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = list-panes ]; then
  printf 'other\t/tmp\n'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, err := runCapturingStdout(t, []string{"--config", configPath, "doctor", "mac"})
	if err == nil {
		t.Fatal("doctor succeeded despite missing configured window, want error")
	}
	if !strings.Contains(output, "FAIL live window missing") {
		t.Fatalf("doctor output did not report missing configured window\n%s", output)
	}
}

func TestDoctorReportsPanePathMismatch(t *testing.T) {
	tmp := t.TempDir()
	configuredWorkdir := filepath.Join(tmp, "configured")
	liveWorkdir := filepath.Join(tmp, "live")
	if err := os.Mkdir(configuredWorkdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(liveWorkdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\ttycho\t"+configuredWorkdir+"\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = list-panes ]; then
  printf 'tycho\t`+liveWorkdir+`\n'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, err := runCapturingStdout(t, []string{"--config", configPath, "doctor", "mac"})
	if err == nil {
		t.Fatal("doctor succeeded despite pane path mismatch, want error")
	}
	if !strings.Contains(output, "FAIL pane path tycho") || !strings.Contains(output, liveWorkdir) {
		t.Fatalf("doctor output did not report pane path mismatch\n%s", output)
	}
}

func TestDoctorComparesWorkspaceAgainstExplicitSession(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("bta\tworker\t"+workdir+"\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(tmp, "tmux.log")
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf 'tmux %s\n' "$*" >> "`+logPath+`"
if [ "$1" = list-panes ]; then
  if [ "$4" = BTA ]; then
    printf 'worker\t`+workdir+`\n'
    exit 0
  fi
  printf 'other\t/tmp\n'
  exit 0
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := runWithDiscardedStdout([]string{"--config", configPath, "doctor", "bta", "BTA"}); err != nil {
		t.Fatal(err)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if !strings.Contains(log, "tmux list-panes -s -t BTA") {
		t.Fatalf("doctor did not inspect explicit session BTA\nlog:\n%s", log)
	}
	if strings.Contains(log, "tmux list-panes -s -t Amp") {
		t.Fatalf("doctor inspected default session despite explicit session\nlog:\n%s", log)
	}
}

func runWithDiscardedStdout(args []string) error {
	return (app{}).run(args)
}

func runCapturingStdout(t *testing.T, args []string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	runErr := (app{stdout: &stdout}).run(args)
	return stdout.String(), runErr
}

func withSelfUpdateTestState(t *testing.T, exePath, releaseURL string, client *http.Client) {
	t.Helper()
	oldExecutablePath := executablePath
	oldReleaseURL := selfUpdateReleaseURL
	oldHTTPClient := selfUpdateHTTPClient
	executablePath = func() (string, error) { return exePath, nil }
	selfUpdateReleaseURL = releaseURL
	selfUpdateHTTPClient = client
	t.Cleanup(func() {
		executablePath = oldExecutablePath
		selfUpdateReleaseURL = oldReleaseURL
		selfUpdateHTTPClient = oldHTTPClient
	})
}

func serverURL(r *http.Request, path string) string {
	return "http://" + r.Host + path
}

func testReleaseArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	contents := []byte(binary)
	if err := tw.WriteHeader(&tar.Header{Name: "amux-test/amux", Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}
