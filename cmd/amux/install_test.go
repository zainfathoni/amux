package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zainfathoni/amux/internal/result"
)

func TestUpdateRejectsManagedAndNoncanonicalTargetsBeforeNetwork(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "mise", path: filepath.Join(t.TempDir(), ".local", "share", "mise", "installs", "go", "1.24", "bin", "amux"), want: "toolchain-managed"},
		{name: "asdf", path: filepath.Join(t.TempDir(), ".asdf", "installs", "golang", "1.24", "packages", "bin", "amux"), want: "toolchain-managed"},
		{name: "other manual path", path: filepath.Join(t.TempDir(), "bin", "amux"), want: "noncanonical install"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(tt.path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tt.path, []byte("old"), 0o755); err != nil {
				t.Fatal(err)
			}
			canonical := filepath.Join(t.TempDir(), ".local", "bin", "amux")
			withInstallTestPaths(t, tt.path, canonical)
			requested := false
			oldClient := selfUpdateHTTPClient
			selfUpdateHTTPClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				requested = true
				return nil, nil
			})}
			t.Cleanup(func() { selfUpdateHTTPClient = oldClient })

			err := (app{stdout: io.Discard}).execute([]string{"update"})
			if err == nil || !strings.Contains(err.Error(), tt.want) || result.ExitCode(err) != result.ExitRejected {
				t.Fatalf("update error = %v, exit = %d, want %q", err, result.ExitCode(err), tt.want)
			}
			if requested {
				t.Fatal("rejected update made a network request")
			}
		})
	}
}

func TestUpdateAndDoctorRejectCanonicalSymlinks(t *testing.T) {
	for _, tt := range []struct {
		name       string
		linkLevels int
	}{
		{name: "executable symlink"},
		{name: "bin directory symlink", linkLevels: 1},
		{name: ".local directory symlink", linkLevels: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			realDir := filepath.Join(tmp, "real", ".local", "bin")
			if err := os.MkdirAll(realDir, 0o755); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(realDir, "amux")
			if err := os.WriteFile(real, []byte("#!/bin/sh\necho 'amux v1.0.0'\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			canonicalDir := filepath.Join(tmp, "home", ".local", "bin")
			canonical := filepath.Join(canonicalDir, "amux")
			link := canonical
			target := real
			for range tt.linkLevels {
				link = filepath.Dir(link)
				target = filepath.Dir(target)
			}
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.linkLevels == 0 {
				if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			withInstallTestPaths(t, canonical, canonical)
			t.Setenv("PATH", canonicalDir)

			err := (app{stdout: io.Discard}).execute([]string{"update"})
			if err == nil || !strings.Contains(err.Error(), "noncanonical install") || result.ExitCode(err) != result.ExitRejected {
				t.Fatalf("symlinked canonical update error = %v", err)
			}
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute([]string{"install", "doctor"}); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), "self-update refused for noncanonical install") {
				t.Fatalf("doctor endorsed symlinked canonical install:\n%s", stdout.String())
			}
		})
	}
}

func TestInstallDoctorReportsEveryPATHCandidateAndDrift(t *testing.T) {
	realRoot := t.TempDir()
	configuredRoot := filepath.Join(t.TempDir(), "configured-root")
	if err := os.Symlink(realRoot, configuredRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	shadowDir := filepath.Join(configuredRoot, "mise", "bin")
	canonical := filepath.Join(configuredRoot, "home", ".local", "bin", "amux")
	canonicalTarget := filepath.Join(resolvedRoot, "home", ".local", "bin", "amux")
	for path, body := range map[string]string{
		filepath.Join(shadowDir, "amux"): "#!/bin/sh\necho 'amux v0.1.33'\n",
		canonical:                        "#!/bin/sh\necho 'amux v0.1.12'\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	running := filepath.Join(configuredRoot, "running", "amux")
	if err := os.MkdirAll(filepath.Dir(running), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(running, []byte("running"), 0o755); err != nil {
		t.Fatal(err)
	}
	withInstallTestPaths(t, running, canonical)
	t.Setenv("PATH", strings.Join([]string{shadowDir, filepath.Dir(canonical)}, string(os.PathListSeparator)))

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"install", "doctor"}); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"Canonical self-update target: " + canonical + " -> " + canonicalTarget + " (amux v0.1.12)",
		"Running executable: " + running,
		filepath.Join(shadowDir, "amux") + " -> " + filepath.Join(resolvedRoot, "mise", "bin", "amux") + " (amux v0.1.33) [selected]",
		canonical + " -> " + canonicalTarget + " (amux v0.1.12)",
		"canonical executable is shadowed",
		"running executable drift",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("diagnostics missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "self-update refused for noncanonical install") {
		t.Fatalf("doctor rejected a canonical configured path whose resolved target differs:\n%s", out)
	}
}

func TestInstallDoctorJSONUsesResultEnvelope(t *testing.T) {
	realRoot := t.TempDir()
	configuredRoot := filepath.Join(t.TempDir(), "configured-root")
	if err := os.Symlink(realRoot, configuredRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(configuredRoot, ".local", "bin", "amux")
	canonicalTarget := filepath.Join(resolvedRoot, ".local", "bin", "amux")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("running"), 0o755); err != nil {
		t.Fatal(err)
	}
	withInstallTestPaths(t, canonical, canonical)
	t.Setenv("PATH", filepath.Dir(canonical))

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--json", "install", "doctor"}); err != nil {
		t.Fatal(err)
	}
	var document struct {
		SchemaVersion int `json:"schema_version"`
		Successful    []struct {
			Resource struct {
				Kind string `json:"kind"`
				Path string `json:"path"`
			} `json:"resource"`
			Message    string `json:"message"`
			Executable struct {
				Roles    []string `json:"roles"`
				Target   string   `json:"target"`
				Version  string   `json:"version"`
				Selected bool     `json:"selected"`
			} `json:"executable"`
		} `json:"successful"`
		Skipped []json.RawMessage `json:"skipped"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != result.SchemaVersion || len(document.Successful) != 3 || len(document.Skipped) != 0 {
		t.Fatalf("unexpected diagnostic envelope: %+v", document)
	}
	for i, roles := range []string{"running", "canonical,scheduled-maintenance", "path"} {
		outcome := document.Successful[i]
		if outcome.Resource.Kind != "executable" || outcome.Resource.Path != canonical || outcome.Executable.Target != canonicalTarget || outcome.Executable.Version != "amux dev" || strings.Join(outcome.Executable.Roles, ",") != roles {
			t.Fatalf("unexpected diagnostic outcome %d: %+v", i, outcome)
		}
	}
	if !document.Successful[2].Executable.Selected {
		t.Fatalf("unexpected diagnostic envelope: %+v", document)
	}
}

func TestUnknownUpdateLikeCommandCannotFallThroughToLaunch(t *testing.T) {
	for _, args := range [][]string{{"udpate"}, {"update", "workspace"}, {"self-update"}} {
		err := (app{}).execute(args)
		if err == nil || result.ExitCode(err) != result.ExitRejected {
			t.Fatalf("execute(%q) error = %v, exit = %d", args, err, result.ExitCode(err))
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func withInstallTestPaths(t *testing.T, running, canonical string) {
	t.Helper()
	oldExecutablePath := executablePath
	oldCanonicalSelfUpdatePath := canonicalSelfUpdatePath
	executablePath = func() (string, error) { return running, nil }
	canonicalSelfUpdatePath = func() (string, error) { return canonical, nil }
	t.Cleanup(func() {
		executablePath = oldExecutablePath
		canonicalSelfUpdatePath = oldCanonicalSelfUpdatePath
	})
}
