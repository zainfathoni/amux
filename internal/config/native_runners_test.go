package config

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestLoadNativeRunnersCanonicalizesMixedRootsDeterministically(t *testing.T) {
	root := t.TempDir()
	code := filepath.Join(root, "Code")
	vault := filepath.Join(root, "Obsidian", "Vault")
	dotfiles := filepath.Join(root, "dotfiles")
	for _, path := range []string{code, vault, dotfiles} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "native-runners.json")
	document := `{"schema_version":1,"runners":[{"name":"main","runner_id":"Laptop.Main","startup_directory":` + quoteJSON(code) + `,"discover_dirs":true,"discover_depth":3,"dirs":[` + quoteJSON(vault) + `,` + quoteJSON(dotfiles) + `],"remote_control_terminal":true}]}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadNativeRunners(path)
	if err != nil {
		t.Fatal(err)
	}
	profile := got.Runners[0]
	wantCode, err := filepath.EvalSymlinks(code)
	if err != nil {
		t.Fatal(err)
	}
	wantVault, err := filepath.EvalSymlinks(vault)
	if err != nil {
		t.Fatal(err)
	}
	wantDotfiles, err := filepath.EvalSymlinks(dotfiles)
	if err != nil {
		t.Fatal(err)
	}
	wantDirectories := []string{wantVault, wantDotfiles}
	sort.Strings(wantDirectories)
	if profile.StartupDirectory != wantCode || profile.DiscoverDepth == nil || *profile.DiscoverDepth != 3 || !slices.Equal(profile.Directories, wantDirectories) {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestLoadNativeRunnersValidatesOptionalDiscoveryDepth(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, fields, want string
	}{
		{"omitted", `"discover_dirs":true`, ""},
		{"minimum", `"discover_dirs":true,"discover_depth":1`, ""},
		{"maximum", `"discover_dirs":true,"discover_depth":10`, ""},
		{"zero", `"discover_dirs":true,"discover_depth":0`, "between 1 and 10"},
		{"above maximum", `"discover_dirs":true,"discover_depth":11`, "between 1 and 10"},
		{"discovery disabled", `"discover_dirs":false,"discover_depth":3`, "requires discover_dirs"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "native-runners.json")
			document := `{"schema_version":1,"runners":[{"name":"main","runner_id":"runner","startup_directory":` + quoteJSON(root) + `,` + test.fields + `}]}`
			if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadNativeRunners(path)
			if test.want != "" {
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error = %v, want %q", err, test.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if (test.name == "omitted") != (got.Runners[0].DiscoverDepth == nil) {
				t.Fatalf("discovery depth = %v", got.Runners[0].DiscoverDepth)
			}
		})
	}
}

func TestLoadNativeRunnersRejectsAmbiguousOrUnsafeProfiles(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, document, want string
	}{
		{"unknown field", `{"schema_version":1,"unknown":true,"runners":[]}`, "unknown field"},
		{"bad schema", `{"schema_version":2,"runners":[]}`, "schema_version"},
		{"duplicate names", `{"schema_version":1,"runners":[{"name":"main","runner_id":"one","startup_directory":` + quoteJSON(root) + `},{"name":"main","runner_id":"two","startup_directory":` + quoteJSON(root) + `}]}`, "duplicate runner profile"},
		{"case insensitive IDs", `{"schema_version":1,"runners":[{"name":"one","runner_id":"Runner","startup_directory":` + quoteJSON(root) + `},{"name":"two","runner_id":"runner","startup_directory":` + quoteJSON(root) + `}]}`, "duplicate runner ID"},
		{"startup repeated", `{"schema_version":1,"runners":[{"name":"main","runner_id":"runner","startup_directory":` + quoteJSON(root) + `,"dirs":[` + quoteJSON(root) + `]}]}`, "repeats served directory"},
		{"missing directory", `{"schema_version":1,"runners":[{"name":"main","runner_id":"runner","startup_directory":` + quoteJSON(filepath.Join(root, "missing")) + `}]}`, "no such file"},
		{"invalid name", `{"schema_version":1,"runners":[{"name":"Main Profile","runner_id":"runner","startup_directory":` + quoteJSON(root) + `}]}`, "name must match"},
		{"invalid runner ID", `{"schema_version":1,"runners":[{"name":"main","runner_id":"bad_id","startup_directory":` + quoteJSON(root) + `}]}`, "valid hostname"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "native-runners.json")
			if err := os.WriteFile(path, []byte(test.document), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadNativeRunners(path)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNativeRunnerStartupDirectoriesMustBeDistinct(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, second string
		wantError    bool
	}{
		{"same path", root, true},
		{"symlink alias", alias, true},
		{"distinct startups sharing served directory", other, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			shared := t.TempDir()
			c := NativeRunnerConfig{SchemaVersion: 1, Runners: []NativeRunnerProfile{
				{Name: "one", RunnerID: "one", StartupDirectory: root, Directories: []string{shared}},
				{Name: "two", RunnerID: "two", StartupDirectory: test.second, Directories: []string{shared}},
			}}
			err := c.Validate()
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "share startup_directory") {
					t.Fatalf("error = %v, want shared startup rejection", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func quoteJSON(value string) string {
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}
