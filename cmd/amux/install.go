package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zainfathoni/amux/internal/result"
)

const installVersionTimeout = 2 * time.Second

type executableDiagnostic struct {
	path    string
	target  string
	version string
	err     error
}

func (a app) installDoctor(in invocation) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	canonical, err := canonicalSelfUpdatePath()
	if err != nil {
		return &env, result.Preflight(err)
	}
	canonical = filepath.Clean(canonical)
	running, err := executablePath()
	if err != nil {
		return &env, result.Preflight(fmt.Errorf("find running executable: %w", err))
	}
	running, err = filepath.Abs(running)
	if err != nil {
		return &env, result.Preflight(fmt.Errorf("resolve running executable: %w", err))
	}
	running = filepath.Clean(running)
	runningTarget := resolvePathForComparison(running)

	candidates := pathExecutableDiagnostics(runningTarget)
	canonicalTarget := resolvePathForComparison(canonical)
	canonicalVersion, canonicalVersionErr := diagnosticVersion(canonical, canonicalTarget, runningTarget)
	selectedTarget := ""
	if len(candidates) > 0 {
		selectedTarget = candidates[0].target
	}

	if !in.Options.JSON {
		fmt.Fprintln(a.stdout, "amux installation diagnostics")
		canonicalVersionText := canonicalVersion
		if canonicalVersionErr != nil {
			canonicalVersionText = "ERROR: " + canonicalVersionErr.Error()
		}
		fmt.Fprintf(a.stdout, "Canonical self-update target: %s -> %s (%s)\n", canonical, canonicalTarget, canonicalVersionText)
		fmt.Fprintf(a.stdout, "Running executable: %s -> %s (%s)\n", running, runningTarget, versionString())
		fmt.Fprintf(a.stdout, "Expected scheduled maintenance target: %s -> %s (%s)\n", canonical, canonicalTarget, canonicalVersionText)
		fmt.Fprintln(a.stdout, "PATH candidates:")
		if len(candidates) == 0 {
			fmt.Fprintln(a.stdout, "  (none)")
		}
	}
	env.Successful = append(env.Successful, result.Outcome{
		Resource:   result.ExecutableResource(running),
		Action:     "diagnose",
		Message:    fmt.Sprintf("running executable resolves to %s at %s", runningTarget, versionString()),
		Executable: &result.ExecutableDetails{Roles: []string{"running"}, Target: runningTarget, Version: versionString()},
	})
	canonicalDetails := &result.ExecutableDetails{Roles: []string{"canonical", "scheduled-maintenance"}, Target: canonicalTarget, Version: canonicalVersion}
	canonicalMessage := fmt.Sprintf("canonical and expected scheduled-maintenance target resolves to %s at %s", canonicalTarget, canonicalVersion)
	if canonicalVersionErr != nil {
		canonicalMessage = fmt.Sprintf("canonical target resolves to %s; version unavailable: %v", canonicalTarget, canonicalVersionErr)
		canonicalDetails.Version = ""
		canonicalDetails.VersionError = canonicalVersionErr.Error()
	}
	env.Successful = append(env.Successful, result.Outcome{
		Resource:   result.ExecutableResource(canonical),
		Action:     "diagnose",
		Message:    canonicalMessage,
		Executable: canonicalDetails,
	})

	for i, candidate := range candidates {
		details := &result.ExecutableDetails{Roles: []string{"path"}, Target: candidate.target, Version: candidate.version, Selected: i == 0}
		message := fmt.Sprintf("PATH candidate resolves to %s at %s", candidate.target, candidate.version)
		if candidate.err != nil {
			message = fmt.Sprintf("PATH candidate resolves to %s; version unavailable: %v", candidate.target, candidate.err)
			details.Version = ""
			details.VersionError = candidate.err.Error()
		}
		if i == 0 {
			message += "; selected by PATH"
		}
		env.Successful = append(env.Successful, result.Outcome{
			Resource:   result.ExecutableResource(candidate.path),
			Action:     "diagnose",
			Message:    message,
			Executable: details,
		})
		if !in.Options.JSON {
			selected := ""
			if i == 0 {
				selected = " [selected]"
			}
			value := candidate.version
			if candidate.err != nil {
				value = "ERROR: " + candidate.err.Error()
			}
			fmt.Fprintf(a.stdout, "  %d. %s -> %s (%s)%s\n", i+1, candidate.path, candidate.target, value, selected)
		}
	}

	warnings := make([]string, 0, 3)
	if _, err := validateCanonicalSelfUpdateTarget(canonical); err != nil {
		warnings = append(warnings, err.Error())
	}
	if _, err := os.Stat(canonical); err != nil {
		if os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("canonical executable is missing; install the release binary at %s", canonical))
		} else {
			warnings = append(warnings, fmt.Sprintf("canonical executable cannot be inspected: %v", err))
		}
	}
	if selectedTarget != "" && selectedTarget != canonicalTarget {
		warnings = append(warnings, fmt.Sprintf("canonical executable is shadowed: PATH selects %s; put %s before the shadowing directory or remove the duplicate", candidates[0].path, filepath.Dir(canonical)))
	}
	if runningTarget != canonicalTarget {
		warnings = append(warnings, fmt.Sprintf("running executable drift: this process runs %s; shells and scheduled maintenance must invoke %s", running, canonical))
	}
	for _, warning := range warnings {
		env.Skipped = append(env.Skipped, result.Outcome{
			Resource: result.ExecutableResource(canonical),
			Action:   "diagnose",
			Message:  warning,
		})
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, "WARNING:", warning)
		}
	}
	return &env, nil
}

func pathExecutableDiagnostics(runningTarget string) []executableDiagnostic {
	seen := make(map[string]bool)
	candidates := make([]executableDiagnostic, 0)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		path, err := filepath.Abs(filepath.Join(dir, "amux"))
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		target := resolvePathForComparison(path)
		candidate := executableDiagnostic{path: filepath.Clean(path), target: target}
		if target == runningTarget {
			candidate.version = versionString()
		} else {
			candidate.version, candidate.err = executableVersion(path)
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func executableVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), installVersionTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("version command timed out")
	}
	if err != nil {
		return "", fmt.Errorf("version command failed: %w", err)
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("version command returned empty output")
	}
	return value, nil
}

func diagnosticVersion(path, target, runningTarget string) (string, error) {
	if target == runningTarget {
		return versionString(), nil
	}
	return executableVersion(path)
}
