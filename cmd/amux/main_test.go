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
		"new-session -d -P -F #{window_id} -s Amp -n new win cd '" + workdir + "' && exec amp threads continue 'T-new-thread'",
		"send-keys -t @1 -l hello Amp",
		"send-keys -t @1 Enter",
		"select-window -t @1",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("tmux log missing %q\nlog:\n%s", want, log)
		}
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
	if got, want := string(logBytes), "threads new --mode plan\n"; got != want {
		t.Fatalf("got amp call %q, want %q", got, want)
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
		"new-window -P -F #{window_id} -t Amp -n fresh cd '" + workdir + "' && exec amp threads continue 'T-existing-session'",
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
if [ "$1" = display-message ] && [ "$2" = -p ]; then
  case "$3" in
    '#W') printf 'current-window\n'; exit 0 ;;
    '#{pane_current_path}') printf '/tmp/current workdir\n'; exit 0 ;;
  esac
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "")

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

func TestRemoveCurrentInfersWindowFromTmux(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent-window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = '#W' ]; then
  printf 'current-window\n'
  exit 0
fi
exit 2
`)

	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")
	t.Setenv("TMUX_PANE", "")

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

func TestParkCurrentRemovesRestoreRowAndKillsCapturedWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = display-message ] && [ "$2" = -p ]; then
  case "$3" in
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
	t.Setenv("TMUX_PANE", "")
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

func TestParkCurrentGracefullyStopsPaneBeforeKillingWindow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	logPath := filepath.Join(tmp, "calls.log")
	if err := os.WriteFile(configPath, []byte("mac\tcurrent window\t/tmp\tT-current\nmac\tother\t/tmp\tT-other\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "`+logPath+`"
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = '#S:#I' ]; then
  printf 'Amp:7\n'
  exit 0
fi
if [ "$1" = display-message ] && [ "$2" = -p ] && [ "$3" = '#W' ]; then
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
	t.Setenv("TMUX_PANE", "")
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

func TestDoctorFailsWhenInsideTmuxButTmuxCannotBeQueried(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "workspaces.tsv")
	if err := os.WriteFile(configPath, []byte("mac\twin\t/tmp\tT-thread\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(tmp, "amp"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(tmp, "tmux"), `#!/bin/sh
if [ "$1" = display-message ]; then
  exit 2
fi
exit 0
`)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "fake-tmux-socket")

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
if [ "$1" = display-message ] && [ "$2" = -p ]; then
  case "$3" in
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
