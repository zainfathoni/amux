package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
)

func TestNativeRunnerArtifactsPreserveMixedRootConfiguration(t *testing.T) {
	profile := config.NativeRunnerProfile{
		Name:                  "main",
		RunnerID:              "laptop-main",
		StartupDirectory:      "/Users/me/Code",
		DiscoverDirectories:   true,
		Directories:           []string{"/Users/me/Obsidian/Vault", "/Users/me/.dotfiles"},
		RemoteControlTerminal: true,
	}
	systemd := systemdRunnerServiceArtifact("/opt/amp/bin/amp", profile)
	for _, want := range []string{
		`WorkingDirectory="/Users/me/Code"`,
		`ExecStart="/opt/amp/bin/amp" "--no-tui" "--runner-id" "laptop-main" "--discover-dirs" "--dir" "/Users/me/Obsidian/Vault" "--dir" "/Users/me/.dotfiles" "--remote-control-terminal"`,
		"Restart=always",
	} {
		if !strings.Contains(systemd, want) {
			t.Errorf("systemd artifact missing %q:\n%s", want, systemd)
		}
	}
	launchd := launchdRunnerServiceArtifact("/opt/amp/bin/amp", profile)
	for _, want := range []string{
		"<key>WorkingDirectory</key><string>/Users/me/Code</string>",
		"<string>--discover-dirs</string>",
		"<string>/Users/me/Obsidian/Vault</string>",
		"<string>--remote-control-terminal</string>",
		"<key>RunAtLoad</key><true/>",
		"<key>KeepAlive</key><true/>",
	} {
		if !strings.Contains(launchd, want) {
			t.Errorf("launchd artifact missing %q:\n%s", want, launchd)
		}
	}
}

func TestRunnerServiceLinuxLifecycleAndDoctorDetectsConfigurationDrift(t *testing.T) {
	root := t.TempDir()
	code := filepath.Join(root, "Code")
	vault := filepath.Join(root, "Vault")
	other := filepath.Join(root, "Other")
	for _, directory := range []string{code, vault, other} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
	ampPath := filepath.Join(root, "amp")
	writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")

	oldGOOS, oldConfigDir, oldLookPath, oldExec := runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath, runnerServiceExec
	runnerServiceGOOS = "linux"
	runnerServiceUserConfigDir = func() (string, error) { return filepath.Join(root, "user-config"), nil }
	runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
	var calls []string
	runnerServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil, nil
	}
	t.Cleanup(func() {
		runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath, runnerServiceExec = oldGOOS, oldConfigDir, oldLookPath, oldExec
	})

	app := app{stdout: &bytes.Buffer{}}
	install := invocation{Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	envelope, err := app.executeRunnerService(install, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Successful) != 1 {
		t.Fatalf("install envelope = %+v", envelope)
	}
	metadata, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ActivationPending || !strings.Contains(strings.Join(calls, "\n"), "systemctl --user enable --now "+runnerServiceLabelPrefix+"main.service") {
		t.Fatalf("metadata=%+v calls=%v", metadata, calls)
	}

	doctor := invocation{Command: &commandSpec{Name: "doctor", Usage: "amux runner service doctor"}, Path: []string{"runner", "service", "doctor"}}
	if _, err := app.executeRunnerService(doctor, dir); err != nil {
		t.Fatalf("doctor healthy services: %v", err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "systemctl --user is-active "+runnerServiceLabelPrefix+"main.service") {
		t.Fatalf("doctor did not check active state: %v", calls)
	}

	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, other)
	if _, err := app.executeRunnerService(doctor, dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("doctor configuration drift error = %v", err)
	}

	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
	remove := invocation{Command: &commandSpec{Name: "remove", Usage: "amux runner service remove"}, Path: []string{"runner", "service", "remove"}}
	if _, err := app.executeRunnerService(remove, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir.RunnerServicesPath()); !os.IsNotExist(err) {
		t.Fatalf("metadata remains after remove: %v", err)
	}
}

func TestRunnerServiceRefusesModifiedOwnedArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), runnerServiceLabelPrefix+"main.service")
	original := []byte("owned")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := runnerServiceMetadata{
		SchemaVersion: 1,
		Platform:      "linux",
		AmpPath:       "/opt/amp/bin/amp",
		Profiles:      []string{"main"},
		Artifacts:     map[string]string{path: digest(original)},
	}
	if err := os.WriteFile(path, []byte("user modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyOwnedRunnerServiceArtifacts(metadata); err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("ownership check error = %v", err)
	}
}

func TestActivateLaunchdRunnerService(t *testing.T) {
	oldExec := runnerServiceExec
	var call string
	runnerServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call = strings.Join(append([]string{name}, args...), " ")
		return nil, nil
	}
	t.Cleanup(func() { runnerServiceExec = oldExec })
	path := "/Users/me/Library/LaunchAgents/" + runnerServiceLabelPrefix + "main.plist"
	metadata := runnerServiceMetadata{Platform: "darwin", Artifacts: map[string]string{path: strings.Repeat("a", 64)}}
	if err := activateRunnerServices(metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(call, "launchctl bootstrap gui/") || !strings.HasSuffix(call, " "+path) {
		t.Fatalf("activation call = %q", call)
	}
}

func TestCheckRunnerServicesActiveReportsInactiveUnit(t *testing.T) {
	oldExec := runnerServiceExec
	runnerServiceExec = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if slicesContain(args, "is-active") {
			return []byte("inactive"), errors.New("exit status 3")
		}
		return []byte("enabled"), nil
	}
	t.Cleanup(func() { runnerServiceExec = oldExec })
	path := "/tmp/" + runnerServiceLabelPrefix + "main.service"
	err := checkRunnerServicesActive(runnerServiceMetadata{Platform: "linux", Artifacts: map[string]string{path: strings.Repeat("a", 64)}})
	if err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("active check error = %v", err)
	}
}

func TestRunnerServiceInstallDryRunPlansWithoutWritingOrActivating(t *testing.T) {
	root := t.TempDir()
	code := filepath.Join(root, "Code")
	vault := filepath.Join(root, "Vault")
	for _, directory := range []string{code, vault} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	document := `{"schema_version":1,"runners":[{"name":"main","runner_id":"laptop-main","startup_directory":` + jsonString(code) + `,"discover_dirs":true,"dirs":[` + jsonString(vault) + `]}]}`
	if err := os.WriteFile(dir.NativeRunnersPath(), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	ampPath := filepath.Join(root, "amp")
	writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")

	oldGOOS, oldConfigDir, oldLookPath, oldExec := runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath, runnerServiceExec
	runnerServiceGOOS = "linux"
	runnerServiceUserConfigDir = func() (string, error) { return filepath.Join(root, "user-config"), nil }
	runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
	called := false
	runnerServiceExec = func(context.Context, string, ...string) ([]byte, error) { called = true; return nil, nil }
	t.Cleanup(func() {
		runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath, runnerServiceExec = oldGOOS, oldConfigDir, oldLookPath, oldExec
	})

	var stdout bytes.Buffer
	in := invocation{Options: cliOptions{DryRun: true}, Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	envelope, err := (app{stdout: &stdout}).executeRunnerService(in, dir)
	if err != nil {
		t.Fatal(err)
	}
	if called || len(envelope.Planned) != 1 || len(envelope.Successful) != 0 {
		t.Fatalf("called=%t envelope=%+v", called, envelope)
	}
	if _, err := os.Stat(dir.RunnerServicesPath()); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote metadata: %v", err)
	}
	if !strings.Contains(stdout.String(), runnerServiceLabelPrefix+"main.service") {
		t.Fatalf("dry-run output = %q", stdout.String())
	}
}

func jsonString(value string) string {
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}

func writeNativeRunnerConfig(t *testing.T, path, startupDirectory, directory string) {
	t.Helper()
	document := `{"schema_version":1,"runners":[{"name":"main","runner_id":"laptop-main","startup_directory":` + jsonString(startupDirectory) + `,"discover_dirs":true,"dirs":[` + jsonString(directory) + `]}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
}

func slicesContain(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
