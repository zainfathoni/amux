package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLoadGroupNamingSelectsRepositoryScopedPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), GroupNamingFile)
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"projects":[{"repository":"github.com/owner/trello-project","prefix":"board"},{"repository":"github.com/owner/github-project","prefix":"gh"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadGroupNaming(path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := loaded.Project("github.com/owner/trello-project")
	if err != nil || project.Prefix != "board" {
		t.Fatalf("project = %+v, error = %v", project, err)
	}
	if _, err := loaded.Project("github.com/owner/other"); err == nil || !strings.Contains(err.Error(), "verified repository") {
		t.Fatalf("scope mismatch error = %v", err)
	}
}

func TestLoadGroupNamingRejectsAmbiguousRepository(t *testing.T) {
	path := filepath.Join(t.TempDir(), GroupNamingFile)
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"projects":[{"repository":"github.com/owner/project","prefix":"one"},{"repository":"github.com/owner/project","prefix":"two"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGroupNaming(path); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous config error = %v", err)
	}
}

func TestLoadGroupNamingRejectsEmptyRepositoryComponents(t *testing.T) {
	for _, repository := range []string{"github.com//project", "github.com/owner/", "/owner/project"} {
		t.Run(repository, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), GroupNamingFile)
			content := `{"schema_version":1,"projects":[{"repository":` + strconv.Quote(repository) + `,"prefix":"one"}]}`
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadGroupNaming(path); err == nil || !strings.Contains(err.Error(), "non-empty components") {
				t.Fatalf("empty repository component error = %v", err)
			}
		})
	}
}

func TestDeriveGroupNamingTrackerNeutralAndFailClosed(t *testing.T) {
	for _, workItem := range []string{"123", "975", "abc123"} {
		group, report, err := DeriveGroupNaming("bta", workItem, "unlisted-addons", 2)
		if err != nil || group != "bta-"+workItem+"-unlisted-addons" || report != group+"-worker-2" {
			t.Fatalf("derive %q = %q, %q, %v", workItem, group, report, err)
		}
	}
	for _, test := range []struct {
		name, workItem, slug string
	}{
		{"invalid work item", "#123", "valid"},
		{"invalid slug", "123", "Needs-Normalizing"},
		{"over limit", "123", strings.Repeat("a", 29)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := DeriveGroupNaming("bta", test.workItem, test.slug, 1); err == nil {
				t.Fatal("expected derivation rejection")
			}
		})
	}
}
