package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/config"
)

func TestNativeRunnerArtifactsPreserveMixedRootConfiguration(t *testing.T) {
	discoveryDepth := 3
	profile := config.NativeRunnerProfile{
		Name:                  "main",
		RunnerID:              "laptop-main",
		StartupDirectory:      "/Users/me/Code Root%$",
		DiscoverDirectories:   true,
		DiscoverDepth:         &discoveryDepth,
		Directories:           []string{"/Users/me/Obsidian/Vault", "/Users/me/.dotfiles"},
		RemoteControlTerminal: true,
	}
	servicePath := "/opt/homebrew/bin:/usr/bin:/bin"
	systemd, err := systemdRunnerServiceArtifact("/opt/amp/bin/amp", servicePath, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Environment="PATH=/opt/homebrew/bin:/usr/bin:/bin"`,
		`WorkingDirectory=/Users/me/Code Root%%$`,
		`ExecStart="/opt/amp/bin/amp" "--no-tui" "--runner-id" "laptop-main" "--discover-dirs" "--discover-depth" "3" "--dir" "/Users/me/Obsidian/Vault" "--dir" "/Users/me/.dotfiles" "--remote-control-terminal"`,
		"Restart=always",
	} {
		if !strings.Contains(systemd, want) {
			t.Errorf("systemd artifact missing %q:\n%s", want, systemd)
		}
	}
	launchd := launchdRunnerServiceArtifact("/opt/amp/bin/amp", servicePath, profile)
	for _, want := range []string{
		"<key>EnvironmentVariables</key><dict><key>PATH</key><string>/opt/homebrew/bin:/usr/bin:/bin</string></dict>",
		"<key>WorkingDirectory</key><string>/Users/me/Code Root%$</string>",
		"<string>--discover-dirs</string>",
		"<string>--discover-depth</string><string>3</string>",
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

func TestNativeRunnerArgsOmitUnconfiguredDiscoveryDepth(t *testing.T) {
	args := nativeRunnerArgs(config.NativeRunnerProfile{RunnerID: "runner", DiscoverDirectories: true})
	if slices.Contains(args, "--discover-depth") {
		t.Fatalf("args = %q", args)
	}
}

func TestSanitizedServicePathKeepsUniqueAbsoluteCallerEntries(t *testing.T) {
	got, err := sanitizedServicePath("relative:/opt/homebrew/bin::/usr/bin:/opt/homebrew/bin:/bin/../bin")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/opt/homebrew/bin:/usr/bin:/bin"; got != want {
		t.Fatalf("sanitized PATH = %q, want %q", got, want)
	}
	if _, err := sanitizedServicePath("relative:also-relative"); err == nil {
		t.Fatal("relative-only PATH was accepted")
	}
}

func TestSystemdRunnerServiceArtifactPassesSystemdAnalyze(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd-analyze verification is Linux-only")
	}
	systemdAnalyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze unavailable")
	}
	workdir := filepath.Join(t.TempDir(), "Code Root%$")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact, err := systemdRunnerServiceArtifact("/bin/true", "/usr/local/bin:/usr/bin:/bin", config.NativeRunnerProfile{Name: "main", RunnerID: "test-runner", StartupDirectory: workdir})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "runner.service")
	if err := os.WriteFile(path, []byte(artifact), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(systemdAnalyze, "verify", path).CombinedOutput(); err != nil {
		t.Fatalf("systemd-analyze verify: %v\n%s\n%s", err, output, artifact)
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
	inactive := false
	runnerServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if inactive && slicesContain(args, "is-active") {
			return []byte("inactive"), errors.New("exit status 3")
		}
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
	calls = nil
	envelope, err = app.executeRunnerService(install, dir)
	if err != nil {
		t.Fatal(err)
	}
	wantHealthChecks := []string{
		"systemctl --user is-enabled " + runnerServiceLabelPrefix + "main.service",
		"systemctl --user is-active " + runnerServiceLabelPrefix + "main.service",
	}
	if len(envelope.Skipped) != 1 || !slices.Equal(calls, wantHealthChecks) {
		t.Fatalf("unchanged install disrupted services: envelope=%+v calls=%v", envelope, calls)
	}

	calls = nil
	inactive = true
	envelope, err = app.executeRunnerService(install, dir)
	inactive = false
	if err != nil {
		t.Fatal(err)
	}
	serviceCalls := strings.Join(calls, "\n")
	if len(envelope.Successful) != 1 || !strings.Contains(serviceCalls, "systemctl --user disable --now "+runnerServiceLabelPrefix+"main.service") || !strings.Contains(serviceCalls, "systemctl --user enable --now "+runnerServiceLabelPrefix+"main.service") {
		t.Fatalf("inactive unchanged service was not replaced: envelope=%+v calls=%v", envelope, calls)
	}

	otherConfig := config.Directory{Path: filepath.Join(root, "other-config")}
	if err := os.MkdirAll(otherConfig.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeRunnerConfig(t, otherConfig.NativeRunnersPath(), code, vault)
	if _, err := app.executeRunnerService(install, otherConfig); err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("second config ownership error = %v", err)
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

func TestRunnerServiceInstallReplacesLegacyLaunchAgentWithCallerPath(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	code := filepath.Join(root, "Code")
	vault := filepath.Join(root, "Vault")
	for _, directory := range []string{home, code, vault} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
	configuration, err := config.LoadNativeRunners(dir.NativeRunnersPath())
	if err != nil {
		t.Fatal(err)
	}
	ampPath := filepath.Join(root, "amp")
	writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")
	launchAgent := filepath.Join(home, "Library", "LaunchAgents", runnerServiceLabelPrefix+"main.plist")
	legacy := []byte(launchdRunnerServiceArtifact(ampPath, "", configuration.Runners[0]))
	if err := os.MkdirAll(filepath.Dir(launchAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launchAgent, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(dir.RunnerServicesPath(), runnerServiceMetadata{
		SchemaVersion: 1,
		Platform:      "darwin",
		AmpPath:       ampPath,
		Profiles:      []string{"main"},
		Artifacts:     map[string]string{launchAgent: digest(legacy)},
	}); err != nil {
		t.Fatal(err)
	}

	oldGOOS, oldHome, oldLookPath, oldExec := runnerServiceGOOS, runnerServiceHome, runnerServiceLookPath, runnerServiceExec
	runnerServiceGOOS = "darwin"
	runnerServiceHome = func() (string, error) { return home, nil }
	runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
	loaded := true
	runnerServiceExec = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "print":
			if loaded {
				return []byte("state = running"), nil
			}
			return []byte("Could not find service in domain"), errors.New("exit status 113")
		case "bootout":
			loaded = false
		case "bootstrap":
			loaded = true
		}
		return nil, nil
	}
	t.Cleanup(func() {
		runnerServiceGOOS, runnerServiceHome, runnerServiceLookPath, runnerServiceExec = oldGOOS, oldHome, oldLookPath, oldExec
	})
	t.Setenv("PATH", "relative:/opt/homebrew/bin:/usr/bin:/bin:/opt/homebrew/bin")

	install := invocation{Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	envelope, err := (app{stdout: &bytes.Buffer{}}).executeRunnerService(install, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.Successful) != 1 || envelope.Successful[0].Action != "replace-runner-service" {
		t.Fatalf("install envelope = %+v", envelope)
	}
	updated, err := os.ReadFile(launchAgent)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := "/opt/homebrew/bin:/usr/bin:/bin"
	if !strings.Contains(string(updated), "<key>EnvironmentVariables</key><dict><key>PATH</key><string>"+wantPath+"</string></dict>") {
		t.Fatalf("updated LaunchAgent lacks caller PATH:\n%s", updated)
	}
	metadata, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ActivationPending || metadata.Path != wantPath || !loaded {
		t.Fatalf("metadata=%+v loaded=%t", metadata, loaded)
	}
}

func TestRunnerServiceRemoveRecoversPendingInstallationWithoutDesiredConfigOrAmp(t *testing.T) {
	root := t.TempDir()
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(root, runnerServiceLabelPrefix+"main.service")
	previous := filepath.Join(root, runnerServiceLabelPrefix+"old.service")
	currentData, previousData := []byte("current"), []byte("previous")
	for path, data := range map[string][]byte{current: currentData, previous: previousData} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	metadata := runnerServiceMetadata{
		SchemaVersion:     1,
		Platform:          "linux",
		AmpPath:           "/missing/amp",
		Profiles:          []string{"main"},
		Artifacts:         map[string]string{current: digest(currentData)},
		ActivationPending: true,
		PreviousArtifacts: map[string]string{previous: digest(previousData)},
	}
	if err := atomicJSON(dir.RunnerServicesPath(), metadata); err != nil {
		t.Fatal(err)
	}

	oldGOOS, oldExec := runnerServiceGOOS, runnerServiceExec
	runnerServiceGOOS = "linux"
	runnerServiceExec = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	t.Cleanup(func() { runnerServiceGOOS, runnerServiceExec = oldGOOS, oldExec })
	remove := invocation{Command: &commandSpec{Name: "remove", Usage: "amux runner service remove"}, Path: []string{"runner", "service", "remove"}}
	if _, err := (app{stdout: &bytes.Buffer{}}).executeRunnerService(remove, dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{current, previous, dir.RunnerServicesPath()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("pending artifact remains at %s: %v", path, err)
		}
	}
}

func TestRunnerServiceRetryPreservesInstalledDigestAcrossFailures(t *testing.T) {
	root := t.TempDir()
	directories := []string{filepath.Join(root, "Code"), filepath.Join(root, "Vault A"), filepath.Join(root, "Vault B"), filepath.Join(root, "Vault C")}
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	ampPath := filepath.Join(root, "amp")
	writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")

	oldGOOS, oldConfigDir, oldLookPath, oldExec := runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath, runnerServiceExec
	runnerServiceGOOS = "linux"
	runnerServiceUserConfigDir = func() (string, error) { return filepath.Join(root, "user-config"), nil }
	runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
	failCommand := ""
	runnerServiceExec = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if slicesContain(args, failCommand) {
			return []byte("injected failure"), errors.New("exit status 1")
		}
		return nil, nil
	}
	t.Cleanup(func() {
		runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath, runnerServiceExec = oldGOOS, oldConfigDir, oldLookPath, oldExec
	})

	app := app{stdout: &bytes.Buffer{}}
	install := invocation{Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), directories[0], directories[1])
	if _, err := app.executeRunnerService(install, dir); err != nil {
		t.Fatal(err)
	}

	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), directories[0], directories[2])
	failCommand = "enable"
	if _, err := app.executeRunnerService(install, dir); err == nil {
		t.Fatal("install succeeded despite injected activation failure")
	}
	failedActivation, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if err != nil {
		t.Fatal(err)
	}
	path := sortedDigestPaths(failedActivation.Artifacts)[0]
	installed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	installedDigest := digest(installed)
	if !failedActivation.ActivationPending || failedActivation.Artifacts[path] != installedDigest {
		t.Fatalf("activation failure metadata = %+v, installed digest = %s", failedActivation, installedDigest)
	}

	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), directories[0], directories[3])
	failCommand = "disable"
	if _, err := app.executeRunnerService(install, dir); err == nil {
		t.Fatal("retry succeeded despite injected deactivation failure")
	}
	failedRetry, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if err != nil {
		t.Fatal(err)
	}
	if !failedRetry.ActivationPending || failedRetry.PreviousArtifacts[path] != installedDigest {
		t.Fatalf("retry did not journal installed artifact: metadata=%+v installed digest=%s", failedRetry, installedDigest)
	}

	failCommand = ""
	remove := invocation{Command: &commandSpec{Name: "remove", Usage: "amux runner service remove"}, Path: []string{"runner", "service", "remove"}}
	if _, err := app.executeRunnerService(remove, dir); err != nil {
		t.Fatalf("remove after interrupted retry: %v", err)
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

func TestDeactivateLaunchdRunnerServiceUsesTargetAndHandlesAbsence(t *testing.T) {
	oldExec := runnerServiceExec
	path := "/missing/Library/LaunchAgents/" + runnerServiceLabelPrefix + "main.plist"
	target := "gui/" + fmt.Sprint(os.Getuid()) + "/" + runnerServiceLabelPrefix + "main"
	var calls []string
	loaded := true
	runnerServiceExec = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := strings.Join(append([]string{name}, args...), " ")
		calls = append(calls, call)
		if args[0] == "print" && !loaded {
			return []byte("Could not find service in domain"), errors.New("exit status 113")
		}
		if args[0] == "bootout" {
			loaded = false
		}
		return nil, nil
	}
	t.Cleanup(func() { runnerServiceExec = oldExec })
	metadata := runnerServiceMetadata{Platform: "darwin", Artifacts: map[string]string{path: strings.Repeat("a", 64)}}
	if err := deactivateRunnerServices(metadata); err != nil {
		t.Fatal(err)
	}
	want := []string{"launchctl print " + target, "launchctl bootout " + target}
	if !slices.Equal(calls, want) {
		t.Fatalf("deactivation calls = %v, want %v", calls, want)
	}
	calls = nil
	if err := deactivateRunnerServices(metadata); err != nil {
		t.Fatalf("already absent service: %v", err)
	}
	if !slices.Equal(calls, []string{"launchctl print " + target}) {
		t.Fatalf("absent service calls = %v", calls)
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

func TestCheckRunnerServicesActiveReportsLoadedButStoppedLaunchAgent(t *testing.T) {
	oldExec := runnerServiceExec
	runnerServiceExec = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("state = exited\nlast exit code = 1\n"), nil
	}
	t.Cleanup(func() { runnerServiceExec = oldExec })
	err := checkRunnerServicesActive(runnerServiceMetadata{Platform: "darwin", Profiles: []string{"main"}})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("launchd active check error = %v", err)
	}
}

func TestVerifyInstalledRunnerServiceArtifactsRejectsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), runnerServiceLabelPrefix+"main.service")
	err := verifyInstalledRunnerServiceArtifacts(runnerServiceMetadata{Artifacts: map[string]string{path: strings.Repeat("a", 64)}})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing artifact check error = %v", err)
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

func TestRunnerServiceInstallInterlocksLegacyMaintenanceOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		owner   string
		pending bool
		blocked bool
	}{
		{name: "installed self-owned", owner: "self", blocked: true},
		{name: "pending self-owned", owner: "self", pending: true, blocked: true},
		{name: "pending externally-owned tail", owner: "external", pending: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			code, vault := filepath.Join(root, "Code"), filepath.Join(root, "Vault")
			for _, path := range []string{code, vault} {
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			dir := config.Directory{Path: filepath.Join(root, "config")}
			if err := os.MkdirAll(dir.Path, 0o755); err != nil {
				t.Fatal(err)
			}
			writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
			if err := atomicJSON(dir.MaintenancePath(), maintenanceMetadata{
				SchemaVersion: 1, ActivationPending: test.pending, Owner: test.owner,
				Platform: "linux", Schedule: "6h", Path: "/usr/bin", AmuxPath: "/opt/amux",
				AmpPath: "/opt/amp", AmpTarget: "/opt/amp", Artifacts: map[string]string{"/artifact": strings.Repeat("a", 64)},
			}); err != nil {
				t.Fatal(err)
			}
			ampPath := filepath.Join(root, "amp")
			writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")
			oldGOOS, oldConfigDir, oldLookPath := runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath
			runnerServiceGOOS = "linux"
			runnerServiceUserConfigDir = func() (string, error) { return filepath.Join(root, "user-config"), nil }
			runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
			t.Cleanup(func() {
				runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath = oldGOOS, oldConfigDir, oldLookPath
			})

			in := invocation{Options: cliOptions{DryRun: true}, Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
			envelope, err := (app{stdout: &bytes.Buffer{}}).executeRunnerService(in, dir)
			if test.blocked {
				if err == nil || !strings.Contains(err.Error(), "self-owned legacy maintenance") {
					t.Fatalf("service install error = %v", err)
				}
				return
			}
			if err != nil || len(envelope.Planned) != 1 {
				t.Fatalf("external maintenance tail blocked: envelope=%+v err=%v", envelope, err)
			}
		})
	}
}

func TestRunnerServiceInstallRejectsLegacyRunnerIDCollisionWithoutRewritingRegistry(t *testing.T) {
	root := t.TempDir()
	code, vault, legacyWorkdir := filepath.Join(root, "Code"), filepath.Join(root, "Vault"), filepath.Join(root, "Legacy")
	for _, path := range []string{code, vault, legacyWorkdir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
	registry := []byte("# amux-schema: runners/v2\n# workspace\tworkdir\t[runner-id]\nlegacy\t" + legacyWorkdir + "\tLAPTOP-MAIN\n")
	if err := os.WriteFile(dir.RunnersPath(), registry, 0o600); err != nil {
		t.Fatal(err)
	}

	in := invocation{Options: cliOptions{DryRun: true}, Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	_, err := (app{stdout: &bytes.Buffer{}}).executeRunnerService(in, dir)
	if err == nil || !strings.Contains(err.Error(), "runner ID") || !strings.Contains(err.Error(), "LAPTOP-MAIN") {
		t.Fatalf("runner ID collision error = %v", err)
	}
	got, readErr := os.ReadFile(dir.RunnersPath())
	if readErr != nil || !bytes.Equal(got, registry) {
		t.Fatalf("collision check rewrote runners.tsv: got=%q err=%v", got, readErr)
	}
}

func TestRunnerServiceStartupCollisionWithLegacyWorkdir(t *testing.T) {
	legacy := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(legacy, alias); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, startup, workdir, id string
		wantError                  bool
	}{
		{"distinct ID same startup", legacy, legacy, "legacy", true},
		{"unnamed legacy row", legacy, legacy, "", true},
		{"legacy alias", legacy, alias, "legacy", true},
		{"native alias", alias, legacy, "legacy", true},
		{"dedicated startup serves legacy directory", t.TempDir(), legacy, "legacy", false},
		{"missing legacy directory", t.TempDir(), filepath.Join(t.TempDir(), "missing"), "legacy", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := config.Directory{Path: t.TempDir()}
			registry := []byte("# amux-schema: runners/v2\nlegacy\t" + test.workdir + "\t" + test.id + "\n")
			if err := os.WriteFile(dir.RunnersPath(), registry, 0o600); err != nil {
				t.Fatal(err)
			}
			c := config.NativeRunnerConfig{SchemaVersion: 1, Runners: []config.NativeRunnerProfile{
				{Name: "pilot", RunnerID: "native", StartupDirectory: test.startup},
			}}
			if !test.wantError {
				c.Runners[0].Directories = []string{legacy}
			}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			err := preflightRunnerServiceInstallCoexistence(dir, c.Runners)
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "startup_directory conflicts") || !strings.Contains(err.Error(), "dedicated startup directory") {
					t.Fatalf("error = %v, want startup collision with remediation", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(dir.RunnersPath())
			if err != nil || !bytes.Equal(got, registry) {
				t.Fatalf("registry changed: %q, %v", got, err)
			}
		})
	}
}

func TestRunnerServiceInstallRejectsDanglingLegacyMaintenanceMetadata(t *testing.T) {
	root := t.TempDir()
	code, vault := filepath.Join(root, "Code"), filepath.Join(root, "Vault")
	for _, path := range []string{code, vault} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
	if err := os.Symlink(filepath.Join(root, "missing-maintenance.json"), dir.MaintenancePath()); err != nil {
		t.Fatal(err)
	}

	in := invocation{Options: cliOptions{DryRun: true}, Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	_, err := (app{stdout: &bytes.Buffer{}}).executeRunnerService(in, dir)
	if err == nil || !strings.Contains(err.Error(), "metadata entry") || !strings.Contains(err.Error(), "target is unavailable") {
		t.Fatalf("dangling maintenance metadata error = %v", err)
	}
}

func TestRunnerServiceInstallRejectsDanglingLegacyRegistry(t *testing.T) {
	root := t.TempDir()
	code, vault := filepath.Join(root, "Code"), filepath.Join(root, "Vault")
	for _, path := range []string{code, vault} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dir := config.Directory{Path: filepath.Join(root, "config")}
	if err := os.MkdirAll(dir.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeRunnerConfig(t, dir.NativeRunnersPath(), code, vault)
	if err := os.Symlink(filepath.Join(root, "missing-runners.tsv"), dir.RunnersPath()); err != nil {
		t.Fatal(err)
	}

	in := invocation{Options: cliOptions{DryRun: true}, Command: &commandSpec{Name: "install", Usage: "amux runner service install"}, Path: []string{"runner", "service", "install"}}
	_, err := (app{stdout: &bytes.Buffer{}}).executeRunnerService(in, dir)
	if err == nil || !strings.Contains(err.Error(), "legacy runner IDs") || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("dangling legacy registry error = %v", err)
	}
}

func TestRunnerServiceDispatchIgnoresUnmigratedLegacyRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(config.ConfigDirEnv, "")
	legacyDir := filepath.Join(home, ".config", "amp-tmux")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, config.RunnersFile), []byte("malformed legacy registry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configDir := config.Directory{Path: filepath.Join(home, config.DefaultDirectoryRelativePath)}
	code, vault := filepath.Join(home, "Code"), filepath.Join(home, "Vault")
	for _, directory := range []string{configDir.Path, code, vault} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeNativeRunnerConfig(t, configDir.NativeRunnersPath(), code, vault)
	ampPath := filepath.Join(home, "amp")
	writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")

	oldGOOS, oldConfigDir, oldLookPath := runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath
	runnerServiceGOOS = "linux"
	runnerServiceUserConfigDir = func() (string, error) { return filepath.Join(home, ".config"), nil }
	runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
	t.Cleanup(func() {
		runnerServiceGOOS, runnerServiceUserConfigDir, runnerServiceLookPath = oldGOOS, oldConfigDir, oldLookPath
	})

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout, stderr: &bytes.Buffer{}}).execute([]string{"--dry-run", "runner", "service", "install"}); err != nil {
		t.Fatalf("native service dispatch was blocked by legacy migration: %v", err)
	}
	if !strings.Contains(stdout.String(), "install and start native Amp runner service") {
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

func writeRunnerServiceMetadata(t *testing.T, dir config.Directory, pending bool) {
	t.Helper()
	artifact := filepath.Join(t.TempDir(), runnerServiceLabelPrefix+"main.service")
	if err := atomicJSON(dir.RunnerServicesPath(), runnerServiceMetadata{
		SchemaVersion: 1, Platform: "linux", AmpPath: "/opt/amp", Profiles: []string{"main"},
		Artifacts: map[string]string{artifact: strings.Repeat("a", 64)}, ActivationPending: pending,
	}); err != nil {
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
