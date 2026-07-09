package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRows(t *testing.T) {
	rows, err := Parse(strings.NewReader("# comment\n\nmac\ttycho\t~/Code/tycho\tT-1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Workspace != "mac" || rows[0].Window != "tycho" || rows[0].Workdir != "~/Code/tycho" || rows[0].Thread != "T-1" {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
}

func TestEnsureWritesDefaultConfigWithPinUnpinGuidance(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/workspaces.tsv"

	if err := Ensure(path); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "amux pin/pin-current/unpin/unpin-current/spawn"; !strings.Contains(string(got), want) {
		t.Fatalf("default config does not prefer pin/unpin guidance\ngot: %q\nwant substring: %q", got, want)
	}
	if strings.Contains(string(got), "amux store/store-current/remove/remove-current/spawn") {
		t.Fatalf("default config still prefers legacy store/remove guidance: %q", got)
	}
}

func TestDefaultPathUsesAmuxConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(WorkspacesEnv, "")
	t.Setenv(LegacyWorkspacesEnv, "")

	got := DefaultPath()
	want := filepath.Join(home, ".config", "amux", "workspaces.tsv")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestDefaultPathMigratesLegacyConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(WorkspacesEnv, "")
	t.Setenv(LegacyWorkspacesEnv, "")
	legacyDir := filepath.Join(home, ".config", "amp-tmux")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyWorkspace := filepath.Join(legacyDir, "workspaces.tsv")
	legacyRunner := filepath.Join(legacyDir, "runners.tsv")
	if err := os.WriteFile(legacyWorkspace, []byte("mac\told\t/tmp\tT-old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyRunner, []byte("mac\trunner\t/tmp\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := DefaultPath()
	want := filepath.Join(home, ".config", "amux", "workspaces.tsv")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want migrated path %q", got, want)
	}
	for _, tc := range []struct {
		path string
		want string
	}{
		{filepath.Join(home, ".config", "amux", "workspaces.tsv"), "mac\told\t/tmp\tT-old\n"},
		{filepath.Join(home, ".config", "amux", "runners.tsv"), "mac\trunner\t/tmp\n"},
		{legacyWorkspace, "mac\told\t/tmp\tT-old\n"},
	} {
		gotBytes, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotBytes) != tc.want {
			t.Fatalf("%s = %q, want %q", tc.path, gotBytes, tc.want)
		}
	}
}

func TestDefaultPathDoesNotOverwriteExistingAmuxConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(WorkspacesEnv, "")
	t.Setenv(LegacyWorkspacesEnv, "")
	legacyDir := filepath.Join(home, ".config", "amp-tmux")
	newDir := filepath.Join(home, ".config", "amux")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	newWorkspace := filepath.Join(newDir, "workspaces.tsv")
	if err := os.WriteFile(filepath.Join(legacyDir, "workspaces.tsv"), []byte("mac\told\t/tmp\tT-old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newWorkspace, []byte("mac\tnew\t/tmp\tT-new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := DefaultPath(); got != newWorkspace {
		t.Fatalf("DefaultPath() = %q, want %q", got, newWorkspace)
	}
	gotBytes, err := os.ReadFile(newWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(gotBytes), "mac\tnew\t/tmp\tT-new\n"; got != want {
		t.Fatalf("new config was overwritten: got %q, want %q", got, want)
	}
}

func TestDefaultPathEnvironmentPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(WorkspacesEnv, "/tmp/amux.tsv")
	t.Setenv(LegacyWorkspacesEnv, "/tmp/legacy.tsv")
	if got, want := DefaultPath(), "/tmp/amux.tsv"; got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
	t.Setenv(WorkspacesEnv, "")
	if got, want := DefaultPath(), "/tmp/legacy.tsv"; got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestParseRejectsMalformedRows(t *testing.T) {
	cases := []string{
		"mac\twin\tdir\n",
		"mac\twin\tdir\tthread\textra\n",
		"mac\t\tdir\tthread\n",
	}
	for _, input := range cases {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Fatalf("Parse(%q) succeeded, want error", input)
		}
	}
}

func TestStoreReplacesAndPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/workspaces.tsv"
	input := "# header\nmac\ttycho\t/old\tT-old\nother\twin\t/tmp\tT-other\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	replaced, err := Store(path, Row{Workspace: "mac", Window: "tycho", Workdir: "/new", Thread: "T-new"})
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("got replaced=false, want true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# header\nmac\ttycho\t/new\tT-new\nother\twin\t/tmp\tT-other\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRemoveKeepsOtherRows(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/workspaces.tsv"
	input := "# header\nmac\ttycho\t/old\tT-old\nother\twin\t/tmp\tT-other\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Remove(path, "mac", "tycho")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("got removed=false, want true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# header\nother\twin\t/tmp\tT-other\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
