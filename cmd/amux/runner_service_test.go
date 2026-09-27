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
	"time"

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
		Share:                 true,
		AmpEnv:                true,
	}
	servicePath := "/opt/homebrew/bin:/usr/bin:/bin"
	systemd, err := systemdRunnerServiceArtifact("/opt/amp/bin/amp", servicePath, profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`Environment="PATH=/opt/homebrew/bin:/usr/bin:/bin"`,
		`WorkingDirectory=/Users/me/Code Root%%$`,
		`ExecStart="/opt/amp/bin/amp" "--no-tui" "--runner-id" "laptop-main" "--discover-dirs" "--discover-depth" "3" "--dir" "/Users/me/Obsidian/Vault" "--dir" "/Users/me/.dotfiles" "--remote-control-terminal" "--share" "--amp-env"`,
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
		"<string>--share</string>",
		"<string>--amp-env</string>",
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
	if slices.Contains(args, "--share") {
		t.Fatalf("private runner args = %q", args)
	}
	if slices.Contains(args, "--amp-env") {
		t.Fatalf("default runner args = %q", args)
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
	doctorEnvelope, err := app.executeRunnerService(doctor, dir)
	if err != nil {
		t.Fatalf("doctor healthy services: %v", err)
	}
	if !strings.Contains(doctorEnvelope.Successful[0].Message, "share=false") {
		t.Fatalf("doctor message = %s", doctorEnvelope.Successful[0].Message)
	}
	initialArtifact, err := os.ReadFile(sortedDigestPaths(metadata.Artifacts)[0])
	if err != nil || strings.Contains(string(initialArtifact), `"--amp-env"`) {
		t.Fatalf("default artifact = %s, error = %v", initialArtifact, err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "systemctl --user is-active "+runnerServiceLabelPrefix+"main.service") {
		t.Fatalf("doctor did not check active state: %v", calls)
	}

	sharedDocument := `{"schema_version":1,"runners":[{"name":"main","runner_id":"laptop-main","startup_directory":` + jsonString(code) + `,"discover_dirs":true,"dirs":[` + jsonString(vault) + `],"share":true}]}`
	if err := os.WriteFile(dir.NativeRunnersPath(), []byte(sharedDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.executeRunnerService(doctor, dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("doctor share drift error = %v", err)
	}
	if _, err := app.executeRunnerService(install, dir); err != nil {
		t.Fatalf("install shared service: %v", err)
	}
	doctorEnvelope, err = app.executeRunnerService(doctor, dir)
	if err != nil || len(doctorEnvelope.Successful) != 1 || !strings.Contains(doctorEnvelope.Successful[0].Message, "share=true") {
		t.Fatalf("doctor shared service: envelope=%+v error=%v", doctorEnvelope, err)
	}
	sharedMetadata, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
	if err != nil {
		t.Fatal(err)
	}
	sharedArtifact, err := os.ReadFile(sortedDigestPaths(sharedMetadata.Artifacts)[0])
	if err != nil || !strings.Contains(string(sharedArtifact), `"--share"`) {
		t.Fatalf("shared artifact = %s, error = %v", sharedArtifact, err)
	}
	if strings.Contains(string(sharedArtifact), `"--amp-env"`) {
		t.Fatalf("shared artifact unexpectedly enables amp-env: %s", sharedArtifact)
	}

	ampEnvDocument := `{"schema_version":1,"runners":[{"name":"main","runner_id":"laptop-main","startup_directory":` + jsonString(code) + `,"discover_dirs":true,"dirs":[` + jsonString(vault) + `],"share":true,"amp_env":true}]}`
	if err := os.WriteFile(dir.NativeRunnersPath(), []byte(ampEnvDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.executeRunnerService(doctor, dir); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("doctor amp_env drift error = %v", err)
	}
	updated, err := app.executeRunnerService(install, dir)
	if err != nil || len(updated.Successful) != 1 || updated.Successful[0].Action != "replace-runner-service" {
		t.Fatalf("install amp_env service: envelope=%+v error=%v", updated, err)
	}
	ampEnvArtifact, err := os.ReadFile(sortedDigestPaths(sharedMetadata.Artifacts)[0])
	if err != nil || !strings.Contains(string(ampEnvArtifact), `"--amp-env"`) {
		t.Fatalf("amp_env artifact = %s, error = %v", ampEnvArtifact, err)
	}
	if _, err := app.executeRunnerService(doctor, dir); err != nil {
		t.Fatalf("doctor amp_env service: %v", err)
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
	want := []string{"launchctl print " + target, "launchctl bootout " + target, "launchctl print " + target}
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

func TestRunnerServicePendingRecoveryPreservesHealthyServices(t *testing.T) {
	for _, state := range []string{"absent", "running", "changed-running", "pending-running", "old-artifact", "unrecognized", "removal-timeout", "bootstrap-failure"} {
		t.Run(state, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			dir := config.Directory{Path: root}
			ampPath := filepath.Join(root, "amp")
			writeExecutable(t, ampPath, "#!/bin/sh\nexit 0\n")
			oldGOOS, oldHome, oldLookPath, oldExec := runnerServiceGOOS, runnerServiceHome, runnerServiceLookPath, runnerServiceExec
			oldTimeout := runnerServiceTimeout
			runnerServiceTimeout = 20 * time.Millisecond
			runnerServiceGOOS = "darwin"
			runnerServiceHome = func() (string, error) { return root, nil }
			runnerServiceLookPath = func(string) (string, error) { return ampPath, nil }
			t.Cleanup(func() {
				runnerServiceGOOS, runnerServiceHome, runnerServiceLookPath, runnerServiceExec = oldGOOS, oldHome, oldLookPath, oldExec
				runnerServiceTimeout = oldTimeout
			})
			t.Setenv("PATH", "/usr/bin:/bin")
			profiles := []config.NativeRunnerProfile{
				{Name: "healthy", RunnerID: "healthy", StartupDirectory: filepath.Join(root, "healthy")},
				{Name: "changed", RunnerID: "changed", StartupDirectory: filepath.Join(root, "changed"), AmpEnv: true},
			}
			for _, profile := range profiles {
				if err := os.Mkdir(profile.StartupDirectory, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := atomicJSON(dir.NativeRunnersPath(), config.NativeRunnerConfig{SchemaVersion: 1, Runners: profiles}); err != nil {
				t.Fatal(err)
			}
			artifacts, err := runnerServiceArtifacts(ampPath, "/usr/bin:/bin", profiles)
			if err != nil {
				t.Fatal(err)
			}
			changed := filepath.Join(root, "Library", "LaunchAgents", runnerServiceLabelPrefix+"changed.plist")
			healthy := filepath.Join(filepath.Dir(changed), runnerServiceLabelPrefix+"healthy.plist")
			oldProfile := profiles[1]
			oldProfile.AmpEnv = false
			oldArtifact := []byte(launchdRunnerServiceArtifact(ampPath, "/usr/bin:/bin", oldProfile))
			metadata := runnerServiceMetadata{SchemaVersion: 1, Platform: "darwin", AmpPath: ampPath, Path: "/usr/bin:/bin", Profiles: []string{"changed", "healthy"}, Artifacts: artifactDigests(artifacts), ActivationPending: true,
				PreviousArtifacts: map[string]string{changed: digest(oldArtifact), healthy: digest(artifacts[healthy])}}
			if state == "running" || state == "pending-running" {
				metadata.PreviousArtifacts[changed] = metadata.Artifacts[changed]
			}
			if state == "pending-running" {
				metadata.PendingArtifacts = []string{changed}
			}
			for path, data := range artifacts {
				if path == changed && (state == "old-artifact" || state == "removal-timeout") {
					data = oldArtifact
				} else if path == changed && state == "unrecognized" {
					data = []byte("<plist><array><string>--amp-env</string></array></plist>")
				}
				if err := atomicWrite(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := atomicJSON(dir.RunnerServicesPath(), metadata); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(dir.RunnerServicesPath())
			loaded := state != "absent" && state != "bootstrap-failure"
			failBootstrap := state == "bootstrap-failure"
			var mutations []string
			runnerServiceExec = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if args[0] == "print" {
					if strings.HasSuffix(args[1], ".healthy") || loaded {
						return []byte("state = running"), nil
					}
					return []byte("Could not find service in domain"), errors.New("exit status 113")
				}
				mutations = append(mutations, strings.Join(args, " "))
				if args[0] == "bootout" {
					loaded = state == "removal-timeout"
				} else if args[0] == "bootstrap" {
					if failBootstrap {
						return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
					}
					loaded = true
				}
				return nil, nil
			}
			application := app{stdout: &bytes.Buffer{}}
			install := invocation{Command: &commandSpec{Name: "install"}, Path: []string{"runner", "service", "install"}}
			install.Options.DryRun = true
			envelope, err := application.executeRunnerService(install, dir)
			after, _ := os.ReadFile(dir.RunnerServicesPath())
			if !bytes.Equal(before, after) || len(mutations) != 0 {
				t.Fatal("dry run changed metadata or services")
			}
			if state == "unrecognized" {
				if err == nil || !strings.Contains(err.Error(), "refusing to modify unrecognized runner service artifact") {
					t.Fatalf("ownership refusal = %v", err)
				}
				install.Options.DryRun = false
				if _, err := application.executeRunnerService(install, dir); err == nil || !strings.Contains(err.Error(), "refusing to modify unrecognized runner service artifact") {
					t.Fatalf("apply ownership refusal = %v", err)
				}
				after, _ = os.ReadFile(dir.RunnerServicesPath())
				if !bytes.Equal(before, after) || len(mutations) != 0 {
					t.Fatal("ownership refusal changed metadata or services")
				}
				return
			}
			if err != nil || len(envelope.Planned) != 1 {
				t.Fatalf("dry run = %+v, %v", envelope, err)
			}
			wantAction := "replace-runner-service"
			if state == "running" {
				wantAction = "complete-runner-service-activation"
			} else if !strings.Contains(envelope.Planned[0].Message, changed) {
				t.Fatalf("wrong service selected: %+v", envelope.Planned)
			}
			if envelope.Planned[0].Action != wantAction {
				t.Fatalf("plan = %+v", envelope.Planned)
			}
			install.Options.DryRun = false
			_, err = application.executeRunnerService(install, dir)
			if state == "removal-timeout" || state == "bootstrap-failure" {
				pending, metadataErr := loadRunnerServiceMetadata(dir.RunnerServicesPath())
				if err == nil || metadataErr != nil || !pending.ActivationPending || !slices.Equal(pending.PendingArtifacts, []string{changed}) || len(mutations) != 1 || strings.Contains(mutations[0], ".healthy") {
					t.Fatalf("failed recovery: error=%v metadata=%+v metadata error=%v calls=%v", err, pending, metadataErr, mutations)
				}
				if state == "removal-timeout" {
					data, readErr := os.ReadFile(changed)
					if !errors.Is(err, context.DeadlineExceeded) || readErr != nil || !bytes.Equal(data, oldArtifact) {
						t.Fatalf("removal timeout changed artifact: %v, %v", err, readErr)
					}
					return
				}
				if !strings.Contains(err.Error(), "Bootstrap failed: 5") {
					t.Fatalf("bootstrap error = %v", err)
				}
				failBootstrap = false
				mutations = nil
				_, err = application.executeRunnerService(install, dir)
			}
			if err != nil {
				t.Fatal(err)
			}
			wantMutations := 1
			if state == "running" {
				wantMutations = 0
			} else if state == "old-artifact" || state == "changed-running" || state == "pending-running" {
				wantMutations = 2
			}
			if len(mutations) != wantMutations || strings.Contains(strings.Join(mutations, "\n"), ".healthy") {
				t.Fatalf("unexpected service changes: %v", mutations)
			}
			final, err := loadRunnerServiceMetadata(dir.RunnerServicesPath())
			if err != nil || final.ActivationPending || len(final.PreviousArtifacts) != 0 || len(final.PendingArtifacts) != 0 || !loaded {
				t.Fatalf("recovery metadata = %+v, loaded=%t, error=%v", final, loaded, err)
			}
			if err := verifyInstalledRunnerServiceArtifacts(final); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWaitRunnerServiceAbsentIsBoundedAndRequiresAbsence(t *testing.T) {
	for _, state := range []string{"delayed", "still-loaded", "unknown-error", "blocked-print"} {
		t.Run(state, func(t *testing.T) {
			oldExec, oldTimeout := runnerServiceExec, runnerServiceTimeout
			t.Cleanup(func() { runnerServiceExec, runnerServiceTimeout = oldExec, oldTimeout })
			runnerServiceTimeout = 20 * time.Millisecond
			if state == "delayed" {
				runnerServiceTimeout = time.Second
			}
			calls := 0
			runnerServiceExec = func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if name != "launchctl" || !slices.Equal(args, []string{"print", "gui/501/test"}) {
					t.Fatalf("unexpected command %s %v", name, args)
				}
				switch state {
				case "delayed":
					if calls == 2 {
						return []byte("Could not find service in domain"), errors.New("exit status 113")
					}
				case "unknown-error":
					return []byte("permission denied"), errors.New("exit status 1")
				case "blocked-print":
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return []byte("state = running"), nil
			}
			err := waitRunnerServiceAbsent("gui/501/test")
			switch state {
			case "delayed":
				if err != nil || calls != 2 {
					t.Fatalf("wait result = %v, calls=%d", err, calls)
				}
			case "unknown-error":
				if err == nil || !strings.Contains(err.Error(), "permission denied") || calls != 1 {
					t.Fatalf("unknown error = %v, calls=%d", err, calls)
				}
			default:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline error = %v", err)
				}
			}
		})
	}
}

func TestRunnerServiceMetadataRejectsInvalidPendingArtifacts(t *testing.T) {
	for _, state := range []string{"unknown", "duplicate", "not-pending"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			artifact := filepath.Join(root, runnerServiceLabelPrefix+"main.plist")
			metadata := runnerServiceMetadata{SchemaVersion: 1, Platform: "darwin", AmpPath: "/opt/amp", Profiles: []string{"main"},
				Artifacts: map[string]string{artifact: strings.Repeat("a", 64)}, ActivationPending: true, PendingArtifacts: []string{artifact}}
			switch state {
			case "unknown":
				metadata.PendingArtifacts = []string{filepath.Join(root, "unknown.plist")}
			case "duplicate":
				metadata.PendingArtifacts = append(metadata.PendingArtifacts, artifact)
			case "not-pending":
				metadata.ActivationPending = false
			}
			path := filepath.Join(root, "runner-services.json")
			if err := atomicJSON(path, metadata); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRunnerServiceMetadata(path); err == nil || !strings.Contains(err.Error(), "invalid pending artifacts") {
				t.Fatalf("invalid pending metadata was not rejected: %v", err)
			}
		})
	}
}
