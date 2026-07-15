package scripts_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var publicSkillFiles = []string{
	"README.md",
	filepath.Join("skills", "amux", "SKILL.md"),
	filepath.Join("skills", "amux", "reference", "commands.md"),
	filepath.Join("skills", "amux", "reference", "trigger-phrases.md"),
	filepath.Join("skills", "amux", "reference", "workflows.md"),
	filepath.Join("skills", "amux", "reference", "troubleshooting.md"),
	filepath.Join("docs", "index.html"),
	filepath.Join("docs", "skill", "index.html"),
	filepath.Join("docs", "og-image.svg"),
}

func TestTriggerChecklistMatchesSkillActivationAndRouting(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	checklist := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))

	triggerPattern := regexp.MustCompile(`(?m)^\| \x60([^\x60]+)\x60 \|`)
	matches := triggerPattern.FindAllStringSubmatch(checklist, -1)
	if len(matches) != 17 {
		t.Fatalf("trigger checklist has %d routes, want 17", len(matches))
	}
	for _, match := range matches {
		trigger := match[1]
		if !strings.Contains(skill, trigger) {
			t.Errorf("SKILL.md is missing checklist trigger %q", trigger)
		}
	}
}

func TestSkillReferencesExistAndAreLinked(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	for _, name := range []string{"commands.md", "workflows.md", "troubleshooting.md", "trigger-phrases.md"} {
		if !strings.Contains(skill, "reference/"+name) {
			t.Errorf("SKILL.md does not link reference/%s", name)
		}
		if _, err := os.Stat(filepath.Join(root, "skills", "amux", "reference", name)); err != nil {
			t.Errorf("reference/%s is missing: %v", name, err)
		}
	}
}

func TestDocumentedCommandTreeMatchesCLIHelp(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	checks := []struct {
		args []string
		want []string
	}{
		{args: []string{"help"}, want: []string{"launch", "list", "park", "restart", "remove", "doctor", "reconcile", "worker", "runner", "workspace", "workspaces", "group", "spawn", "shelve", "unshelve", "teardown"}},
		{args: []string{"help", "worker", "pin"}, want: []string{"--workspace, -w", "--window, -W", "--workdir, -d", "--thread, -t", "--current"}},
		{args: []string{"help", "runner", "pin"}, want: []string{"--workspace, -w", "--workdir, -d", "--current"}},
		{args: []string{"help", "workspace", "list"}, want: []string{"--mode, -m <worker|runner>"}},
		{args: []string{"help", "group", "reconcile"}, want: []string{"--group <id>", "--thread, -t <id>", "--all"}},
	}
	for _, check := range checks {
		command := exec.Command("go", append([]string{"run", "./cmd/amux"}, check.args...)...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("amux %s failed: %v\n%s", strings.Join(check.args, " "), err, output)
		}
		for _, want := range check.want {
			if !strings.Contains(string(output), want) {
				t.Errorf("amux %s help is missing %q", strings.Join(check.args, " "), want)
			}
		}
		for _, fake := range []string{"  health ", "  sprawl ", "  finish "} {
			if strings.Contains(string(output), fake) {
				t.Errorf("amux %s help exposes fake skill-only command %q", strings.Join(check.args, " "), strings.TrimSpace(fake))
			}
		}
	}
}

func TestSkillDrivenSpawnCommandsUseExplicitMedium(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, relativePath := range publicSkillFiles {
		scanLines(t, filepath.Join(root, relativePath), func(lineNumber int, line string) {
			command := commandText(line)
			if !strings.Contains(command, "spawn") || !strings.HasPrefix(command, "amux ") {
				return
			}
			if strings.Contains(command, "[selectors]") || strings.Contains(command, "|") {
				return
			}
			if !strings.Contains(command, "--mode medium") {
				t.Errorf("%s:%d spawn example omits explicit --mode medium: %s", relativePath, lineNumber, strings.TrimSpace(line))
			}
		})
	}

	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	for _, required := range []string{"MUST pass `--mode medium`", "An explicitly requested mode always wins", "Do not infer `high` or `ultra`"} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md is missing spawn policy %q", required)
		}
	}
}

func TestSprawlContractUsesDedicatedSemanticWorkers(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{
		"worker-only orchestration",
		"native `blockedBy`, `blocking`, parent, and sub-issue relationships",
		"`amux-agent-first` label",
		"one narrow issue, one dedicated worktree, and one branch",
		"--window <semantic-window>",
		"--mode medium",
		"--title-prefix '#<issue>'",
		"focused Oracle review",
		"callback destination metadata",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("sprawl workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--window issue-<issue>", "--window #<issue>", "runner spawn"} {
		if strings.Contains(workflow, forbidden) {
			t.Errorf("sprawl workflow contains forbidden guidance %q", forbidden)
		}
	}
}

func TestHealthAndFinishPreserveModeSafety(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{
		"Health is aggregate by default",
		"mode=<worker|runner>",
		"Never send text to a runner pane",
		"no-response` means candidate stale, not safe to replace",
		"Fail closed on unexpected runner ownership",
		"do not use `-D` automatically",
		"run worker teardown as the final action",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("workflow safety contract is missing %q", required)
		}
	}
}

func TestPublicSkillDocsDoNotExposeFakeOrRemovedCommands(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fake := regexp.MustCompile(`^amux\s+(health|sprawl|finish)(\s|$)`)
	removed := regexp.MustCompile(`^amux\s+(store|store-current|pin-current|unpin-current|park-current|shelve-current|shelved|prune-archived|self-update)(\s|$)`)
	positional := regexp.MustCompile(`^amux\s+(launch|list|park|restart|remove|doctor|reconcile)\s+[A-Za-z0-9]`)
	incompatibleCurrent := regexp.MustCompile(`^amux\s+worker\s+pin\b.*--current\b.*--thread\b|^amux\s+worker\s+pin\b.*--thread\b.*--current\b`)
	for _, relativePath := range publicSkillFiles {
		scanLines(t, filepath.Join(root, relativePath), func(lineNumber int, line string) {
			command := commandText(line)
			for label, pattern := range map[string]*regexp.Regexp{"fake skill-only command": fake, "removed command": removed, "removed positional syntax": positional, "incompatible current selector": incompatibleCurrent} {
				if pattern.MatchString(command) {
					t.Errorf("%s:%d exposes %s: %s", relativePath, lineNumber, label, strings.TrimSpace(line))
				}
			}
		})
	}
}

func TestPublicInstallationUsesSkillsCLI(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, relativePath := range []string{"README.md", filepath.Join("docs", "index.html"), filepath.Join("docs", "skill", "index.html")} {
		contents := readSkillFile(t, root, relativePath)
		if !strings.Contains(contents, "npx skills add zainfathoni/amux") {
			t.Errorf("%s does not document the primary skills CLI installation", relativePath)
		}
		if strings.Contains(contents, `ln -sfn "$PWD/skills/amux"`) {
			t.Errorf("%s exposes contributor symlinking as public installation", relativePath)
		}
	}
	contributing := readSkillFile(t, root, "CONTRIBUTING.md")
	if !strings.Contains(contributing, `ln -sfn "$PWD/skills/amux"`) {
		t.Error("CONTRIBUTING.md does not document local skill development symlinking")
	}
}

func commandText(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "$ ")
	line = strings.TrimPrefix(line, "<span class=\"prompt\">$</span> ")
	line = strings.TrimPrefix(line, "+")
	return strings.TrimSpace(line)
}

func scanLines(t *testing.T, path string, check func(lineNumber int, line string)) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		check(lineNumber, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func readSkillFile(t *testing.T, root, relativePath string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(root, relativePath))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
