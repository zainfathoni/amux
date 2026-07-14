package scripts_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These static contract tests run under the repository's existing `go test ./...`
// CI step. They inspect the bundled Markdown skill directly because agents execute
// that guidance; no live Amp thread, tmux window, or amux configuration is needed.
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

func TestSkillDrivenSpawnRoutesUseExplicitMediumMode(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	for _, route := range []struct {
		name     string
		path     string
		required string
	}{
		{
			name:     "top-level direct spawn",
			path:     filepath.Join("skills", "amux", "SKILL.md"),
			required: "**Spawn a worker for ...**: load [`reference/workflows.md`](reference/workflows.md), then use `amux spawn --mode medium ...`",
		},
		{
			name:     "top-level sprawl",
			path:     filepath.Join("skills", "amux", "SKILL.md"),
			required: "**/amux sprawl #12 #34 ...**: skill-only orchestration around `gh`, `git worktree`, and `amux spawn --mode medium`",
		},
		{
			name:     "direct spawn trigger",
			path:     filepath.Join("skills", "amux", "reference", "trigger-phrases.md"),
			required: "use `amux spawn --mode medium ...`",
		},
		{
			name:     "sprawl trigger",
			path:     filepath.Join("skills", "amux", "reference", "trigger-phrases.md"),
			required: "`/amux sprawl #12 #34 ...` | Load [`workflows.md#sprawl-independent-issue-workers`](workflows.md#sprawl-independent-issue-workers), inspect dependencies, then spawn each accepted worker with `amux spawn --mode medium --title-prefix '#<issue>' ...`",
		},
		{
			name:     "sprawl workflow",
			path:     filepath.Join("skills", "amux", "reference", "workflows.md"),
			required: "amux spawn --mode medium --title-prefix '#<issue>'",
		},
		{
			name:     "command reference example",
			path:     filepath.Join("skills", "amux", "reference", "commands.md"),
			required: "amux spawn --mode medium worker ~/Code/repo \"prompt\" amux",
		},
		{
			name:     "README direct spawn",
			path:     "README.md",
			required: "Spawn a worker for ... -> amux spawn --mode medium [--title-prefix <prefix>] ...",
		},
		{
			name:     "README sprawl",
			path:     "README.md",
			required: "uses `amux spawn --mode medium --title-prefix '#<issue>'`",
		},
	} {
		route := route
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()

			contents := readSkillFile(t, root, route.path)
			if !strings.Contains(contents, route.required) {
				t.Errorf("%s does not preserve required skill-driven spawn route %q", route.path, route.required)
			}
		})
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
