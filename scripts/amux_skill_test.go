package scripts_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerSpawnGuidanceRequiresExplicitMode(t *testing.T) {
	t.Parallel()

	skillDir := filepath.Join(repoRoot(t), "skills", "amux")
	policy := readSkillFile(t, skillDir, "SKILL.md")
	for _, required := range []string{
		"MUST pass `--mode medium`",
		"Do not infer `high` or `ultra` from task complexity, size, urgency, or expected duration.",
		"An explicitly requested mode always wins",
		"amux spawn --mode medium ...",
	} {
		if !strings.Contains(policy, required) {
			t.Errorf("SKILL.md does not contain mandatory spawn guidance %q", required)
		}
	}
}

func TestWorkerSpawnExamplesAlwaysPassMediumMode(t *testing.T) {
	t.Parallel()

	skillDir := filepath.Join(repoRoot(t), "skills", "amux")
	for _, relativePath := range []string{
		filepath.Join("reference", "workflows.md"),
		filepath.Join("reference", "troubleshooting.md"),
	} {
		path := filepath.Join(skillDir, relativePath)
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}

		scanner := bufio.NewScanner(file)
		for lineNumber := 1; scanner.Scan(); lineNumber++ {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "amux spawn ") || strings.HasPrefix(line, "amux --dry-run spawn ") {
				if !strings.Contains(line, "spawn --mode medium ") {
					t.Errorf("%s:%d skill-driven spawn example omits explicit --mode medium: %s", relativePath, lineNumber, line)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			t.Errorf("scan %s: %v", relativePath, err)
		}
		if err := file.Close(); err != nil {
			t.Errorf("close %s: %v", relativePath, err)
		}
	}
}

func TestSprawlAndTriggerRoutesUseExplicitMediumMode(t *testing.T) {
	t.Parallel()

	skillDir := filepath.Join(repoRoot(t), "skills", "amux")
	workflows := readSkillFile(t, skillDir, filepath.Join("reference", "workflows.md"))
	if !strings.Contains(workflows, "amux spawn --mode medium --title-prefix '#<issue>'") {
		t.Error("sprawl does not spawn every issue worker with explicit --mode medium")
	}

	triggers := readSkillFile(t, skillDir, filepath.Join("reference", "trigger-phrases.md"))
	if !strings.Contains(triggers, "use `amux spawn --mode medium ...`") {
		t.Error("natural-language worker spawn route omits explicit --mode medium")
	}
}

func readSkillFile(t *testing.T, skillDir, relativePath string) string {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join(skillDir, relativePath))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
