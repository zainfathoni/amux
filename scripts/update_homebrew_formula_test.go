package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateHomebrewFormulaUpdatesAllAssets(t *testing.T) {
	repoRoot := repoRoot(t)
	formula := copyFormula(t, repoRoot)
	dist := writeChecksums(t, "v1.2.3")

	cmd := exec.Command(filepath.Join(repoRoot, "scripts", "update-homebrew-formula.sh"), "v1.2.3", dist, formula)
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("update-homebrew-formula.sh failed: %v\n%s", err, output)
	}

	updatedBytes, err := os.ReadFile(formula)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(updatedBytes)
	if !strings.Contains(updated, `version "1.2.3"`) {
		t.Fatalf("updated formula missing version: %s", updated)
	}
	for _, platform := range []string{"darwin-arm64", "darwin-amd64", "linux-arm64", "linux-amd64"} {
		url := "https://github.com/zainfathoni/amux/releases/download/v1.2.3/amux-v1.2.3-" + platform + ".tar.gz"
		if strings.Count(updated, url) != 1 {
			t.Fatalf("updated formula contains %s %d times, want once", url, strings.Count(updated, url))
		}
	}
}

func TestUpdateHomebrewFormulaFailsWhenReplacementIsMissing(t *testing.T) {
	repoRoot := repoRoot(t)
	formula := copyFormula(t, repoRoot)
	dist := writeChecksums(t, "v1.2.3")

	brokenBytes, err := os.ReadFile(formula)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(brokenBytes), "linux-amd64.tar.gz", "linux-x64.tar.gz", 1)
	if err := os.WriteFile(formula, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(repoRoot, "scripts", "update-homebrew-formula.sh"), "v1.2.3", dist, formula)
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("update-homebrew-formula.sh succeeded unexpectedly:\n%s", output)
	}
	if !strings.Contains(string(output), "expected exactly one replacement for linux-amd64, got 0") {
		t.Fatalf("unexpected failure output:\n%s", output)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(file))
}

func copyFormula(t *testing.T, repoRoot string) string {
	t.Helper()
	formulaBytes, err := os.ReadFile(filepath.Join(repoRoot, "Formula", "amux.rb"))
	if err != nil {
		t.Fatal(err)
	}
	formula := filepath.Join(t.TempDir(), "amux.rb")
	if err := os.WriteFile(formula, formulaBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return formula
}

func writeChecksums(t *testing.T, tag string) string {
	t.Helper()
	dist := t.TempDir()
	checksums := map[string]string{
		"darwin-arm64": strings.Repeat("a", 64),
		"darwin-amd64": strings.Repeat("b", 64),
		"linux-arm64":  strings.Repeat("c", 64),
		"linux-amd64":  strings.Repeat("d", 64),
	}
	for platform, checksum := range checksums {
		asset := "amux-" + tag + "-" + platform + ".tar.gz"
		if err := os.WriteFile(filepath.Join(dist, asset+".sha256"), []byte(checksum+"  "+asset+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dist
}
