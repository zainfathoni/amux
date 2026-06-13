package config

import (
	"os"
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
