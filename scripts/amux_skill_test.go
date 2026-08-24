package scripts_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
)

var publicSkillFiles = []string{
	"README.md",
	filepath.Join("skills", "amux", "SKILL.md"),
	filepath.Join("skills", "amux", "reference", "commands.md"),
	filepath.Join("skills", "amux", "reference", "trigger-phrases.md"),
	filepath.Join("skills", "amux", "reference", "workflows.md"),
	filepath.Join("skills", "amux", "reference", "troubleshooting.md"),
	filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"),
	filepath.Join("skills", "amux", "reference", "contract-v1.md"),
	filepath.Join("skills", "amux", "reference", "deadline-v1.md"),
	filepath.Join("skills", "amux", "reference", "removal-safety.md"),
	filepath.Join("skills", "amux-tycho", "SKILL.md"),
	filepath.Join("skills", "amux-tycho", "reference", "tycho-report-bridge.md"),
	filepath.Join("skills", "amux-tycho", "reference", "team-review-second-opinion.md"),
	filepath.Join("skills", "amux-tycho", "reference", "trigger-phrases.md"),
	filepath.Join("skills", "amux-claude", "SKILL.md"),
	filepath.Join("skills", "amux-claude", "reference", "claude-local-tmux-adoption.md"),
	filepath.Join("skills", "amux-pi", "SKILL.md"),
	filepath.Join("skills", "amux-pi", "reference", "pi-spark-orb-executor.md"),
	filepath.Join("skills", "amux-claude", "reference", "claude-opus-orb-executor.md"),
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
	if len(matches) != 18 {
		t.Fatalf("trigger checklist has %d routes, want 18", len(matches))
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
	type skillRefs struct {
		skillDir string
		refs     []string
	}
	for _, pkg := range []skillRefs{
		{
			skillDir: filepath.Join("skills", "amux"),
			refs: []string{
				"commands.md",
				"workflows.md",
				"troubleshooting.md",
				"trigger-phrases.md",
				"contract-v1.md",
				"deadline-v1.md",
				"amp-invocation-policy.md",
				"removal-safety.md",
			},
		},
		{
			skillDir: filepath.Join("skills", "amux-tycho"),
			refs: []string{
				"tycho-report-bridge.md",
				"team-review-second-opinion.md",
				"trigger-phrases.md",
			},
		},
		{
			skillDir: filepath.Join("skills", "amux-claude"),
			refs: []string{
				"claude-delegation-contract.md",
				"claude-delegation-recovery.md",
				"claude-read-only-delegation.md",
				"claude-mutating-delegation.md",
				"claude-local-tmux-adoption.md",
				"claude-opus-orb-executor.md",
				"trigger-phrases.md",
			},
		},
		{
			skillDir: filepath.Join("skills", "amux-pi"),
			refs: []string{
				"pi-spark-orb-executor.md",
				"trigger-phrases.md",
			},
		},
	} {
		skill := readSkillFile(t, root, filepath.Join(pkg.skillDir, "SKILL.md"))
		for _, name := range pkg.refs {
			if !strings.Contains(skill, "reference/"+name) {
				t.Errorf("%s/SKILL.md does not link reference/%s", pkg.skillDir, name)
			}
			if _, err := os.Stat(filepath.Join(root, pkg.skillDir, "reference", name)); err != nil {
				t.Errorf("%s/reference/%s is missing: %v", pkg.skillDir, name, err)
			}
		}
	}
}

func TestOrdinaryIssuePRChecklistStaysDocsOnlyAndLean(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	checklist := readSkillFile(t, root, filepath.Join("docs", "ordinary-issue-pr-checklist.md"))

	// ADR 0007: ordinary issue/PR guidance must not activate the retiring /amux drain skill.
	frontmatterEnd := strings.Index(skill, "\n---\n")
	if frontmatterEnd < 0 {
		t.Fatal("SKILL.md missing frontmatter terminator")
	}
	frontmatter := skill[:frontmatterEnd]
	for _, forbidden := range []string{
		"ordinary issue and PR-review",
		"ordinary issue or PR review",
		"ordinary-issue-pr-checklist",
		"Ordinary issue or PR review",
	} {
		if strings.Contains(frontmatter, forbidden) {
			t.Errorf("/amux frontmatter must not claim ordinary issue/PR work (%q)", forbidden)
		}
		if strings.Contains(triggers, forbidden) {
			t.Errorf("trigger-phrases must not route ordinary issue/PR work (%q)", forbidden)
		}
	}
	if strings.Contains(skill, "ordinary-issue-pr-checklist") || strings.Contains(workflow, "ordinary-issue-pr-checklist") {
		t.Error("/amux skill or workflows must not link ordinary-issue-pr-checklist as an activation surface")
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "amux", "reference", "ordinary-issue-pr-checklist.md")); err == nil {
		t.Error("checklist must not live under skills/amux/reference (docs-only under ADR 0007)")
	}

	// Child-creation guidance applies only when a child is needed; direct coordinator remains valid.
	if !strings.Contains(skill, "When delegated work needs a native child") {
		t.Error("SKILL.md must gate create_thread on needing a native child")
	}
	if !strings.Contains(skill, "may run on the direct coordinator without a child") {
		t.Error("SKILL.md must allow direct coordinator execution without a child")
	}
	if !strings.Contains(workflow, "When ordinary work needs a native child thread") {
		t.Error("workflows.md must scope fresh native work to cases that need a child")
	}
	if !strings.Contains(workflow, "may stay on the direct coordinator without creating a child") {
		t.Error("workflows.md must allow direct coordinator execution without a child")
	}

	for _, required := range []string{
		"ADR 0007",
		"not** an `/amux` skill route",
		"direct coordinator",
		"native child",
		"locked dedicated",
		"exact-head",
		"Exclusive write ownership",
		"Thread isolation",
		"Git worktree isolation",
		"runtime isolation",
		"no-change",
		"no-findings",
		"#238",
		"#328",
		"#344",
		"#331",
		"#339",
		"#313",
		"No new Amux lifecycle",
		"generalized `amux spawn`",
		"permanent Lead",
		"docs-only",
	} {
		if !strings.Contains(checklist, required) {
			t.Errorf("ordinary issue/PR checklist is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"amux spawn admission",
		"authorize-finish",
		"/amux finish procedure",
		"multi-phase closeout",
	} {
		if strings.Contains(checklist, forbidden) {
			t.Errorf("ordinary issue/PR checklist contains out-of-scope material %q", forbidden)
		}
	}
	// Keep the checklist roughly one screen (issue #369).
	if lines := strings.Count(checklist, "\n") + 1; lines > 50 {
		t.Fatalf("ordinary issue/PR checklist is %d lines; want ≤50 for one-screen lean form", lines)
	}
}

func TestClaudeLocalTmuxAdoptionRouteStaysOperatorAssistedAndFailClosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "SKILL.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "trigger-phrases.md"))
	reference := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-local-tmux-adoption.md"))

	trigger := "Adopt owner-created Claude Code tmux windows on an explicitly selected physical host"
	for name, contents := range map[string]string{"SKILL.md": skill, "trigger checklist": triggers} {
		if !strings.Contains(contents, trigger) || !strings.Contains(contents, "claude-local-tmux-adoption.md") {
			t.Errorf("%s does not route the explicit local tmux adoption trigger", name)
		}
	}
	for _, required := range []string{
		"not managed local delegation and not fresh-Orb execution",
		"session:window",
		"claude-opus-5",
		"Omission, alias normalization, a default, fallback, substitution",
		"human-readable label such as `Opus 5`",
		"never licenses inferring an alias table",
		"Physical host",
		"Worktree state",
		"read-only",
		"exclusive-writer",
		"Reject a dirty worktree",
		"16 KiB",
		"autocomplete",
		"Distinguish ghost autocomplete from committed composer content",
		"`Tab` accepts the ghost suggestion",
		"pasted-text",
		"one or more `[Pasted text #…]` segments",
		"Placeholder segment count is not paste-action count",
		"never repaste merely to expand collapsed text",
		"Vim `-- INSERT --`",
		"A known, owner-confirmed Vim composer state may proceed",
		"hand exactly that one submission or recovery step to the owner",
		"Never send a mode-switching key",
		"Every UI state this route has not explicitly classified is ambiguous by default",
		"Never send blind `Enter`",
		"result-consumed",
		"window-decommissioned",
		"decommission-indeterminate",
		"final window",
		"never implicitly destroys the final or whole session",
		"owner authentication",
		"Amp independently verifies",
		"exact remote PR head",
		"Do not start an automatic repair loop",
	} {
		if !strings.Contains(reference, required) {
			t.Errorf("local tmux adoption reference is missing %q", required)
		}
	}
	for _, forbidden := range []string{"claude-opus-4-8", "tmux kill-session", "kill-server"} {
		if strings.Contains(reference, forbidden) {
			t.Errorf("local tmux adoption reference contains forbidden marker %q", forbidden)
		}
	}
}

func TestExperimentalPiRoutesStayProviderSpecificAndFailClosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux-pi", "SKILL.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux-pi", "reference", "trigger-phrases.md"))
	recipe := readSkillFile(t, root, filepath.Join("skills", "amux-pi", "reference", "pi-spark-orb-executor.md"))
	core := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	if strings.Contains(core, "Run Pi on Spark in an Amp Orb") {
		t.Error("core /amux skill must not route Pi/Spark triggers")
	}

	if !strings.Contains(skill, "Run Pi on Spark in an Amp Orb") || !strings.Contains(triggers, "Run Pi on Spark in an Amp Orb") {
		t.Error("Pi/Spark trigger is missing from skill routing or trigger checklist")
	}
	if !strings.Contains(skill, "reference/pi-spark-orb-executor.md") || !strings.Contains(triggers, "pi-spark-orb-executor.md") {
		t.Error("Pi/Spark reference is missing from skill routing or trigger checklist")
	}
	if !strings.Contains(skill, "Spike one bounded local file replacement") || !strings.Contains(triggers, "Spike one bounded local file replacement") {
		t.Error("local Pi replacement trigger is missing from skill routing or trigger checklist")
	}
	if !strings.Contains(skill, "experimental/pi-spark-local") || !strings.Contains(triggers, "experimental/pi-spark-local") {
		t.Error("local Pi replacement route is missing from skill routing or trigger checklist")
	}
	localPhysicalTrigger := "Run Pi on Spark on a physical runner in a dedicated local worktree/tmux with no Orb"
	if !strings.Contains(skill, localPhysicalTrigger) || !strings.Contains(triggers, localPhysicalTrigger) {
		t.Error("explicit physical-runner + dedicated worktree/tmux + no-Orb request is not recognized as the local route")
	}
	for _, required := range []string{"physical-host helper", "Pi 0.82.1", "Node `>=22.19.0`", "normal managed provider-catalog cache is allowed", "one tracked file", "not authority for arbitrary local work"} {
		if !strings.Contains(skill, required) {
			t.Errorf("local physical-host route is missing bound %q", required)
		}
	}
	for name, contents := range map[string]string{"SKILL.md": skill, "trigger checklist": triggers} {
		for _, required := range []string{"Pi 0.82.1", "Node `>=22.19.0`"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing local runtime contract %q", name, required)
			}
		}
	}
	for _, required := range []string{"never the Orb recipe", "one attempt with no fallback", "no-recursive-delegation", "no-publication", "independent-review", "cleanup", "rollback"} {
		if !strings.Contains(triggers, required) {
			t.Errorf("local physical-host trigger contract is missing %q", required)
		}
	}
	for _, required := range []string{
		"openai-codex/gpt-5.3-codex-spark",
		"OPENAI_API_KEY",
		"CODEX_API_KEY",
		"credential_environment_preflight",
		"https://registry.npmjs.org/",
		"env -i PATH=",
		"TRUSTED_SYSTEM_PATH=/usr/local/bin:/usr/bin:/bin",
		"TMPDIR=\"$EXPERIMENT_TMP\"",
		"--cache=\"$NPM_CACHE\"",
		"PI_VERSION=0.80.10",
		".version' \"$PACKAGE_METADATA\")\" = \"$PI_VERSION",
		"--ignore-scripts",
		"Runtime acceptance is not established by this recipe or its static repository tests",
		"owner-operated OAuth, fresh trusted before/after quota observations",
		"run_setup()",
		"--connect-timeout 10 --max-time 110 --max-filesize",
		"setup step failed or exceeded bounds",
		"report only the step label, status class, and byte counts, never raw stderr",
		"owner-operated Codex OAuth",
		"auth_type=oauth",
		"auth_mode=0600",
		"--mode json",
		"without `--print` or `-p`",
		"RUN=probe",
		"RUN_DIR=$RUNS/$RUN",
		"SPARK_PROBE_OK",
		"| join(\"\")) == \"SPARK_PROBE_OK\"",
		"--no-session",
		"--no-tools",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
		"--no-themes",
		"--no-context-files",
		"--no-approve",
		"timeout --signal=TERM",
		"agent_settled",
		"jq -Rce 'fromjson",
		"([.[] | select(.type == \"agent_end\")] | length) == 1",
		".stopReason == \"stop\"",
		"mkfifo -m 600",
		"STDOUT_READER_PID=$!",
		"STDERR_READER_PID=$!",
		"wait \"$STDOUT_READER_PID\"",
		"wait \"$STDERR_READER_PID\"",
		"rm -- \"$STDOUT_FIFO\" \"$STDERR_FIFO\"",
		"set(value) != {\"openai-codex\"}",
		"auth state is not exactly empty after logout",
		"if os.path.lexists(path):",
		"type == \"string\" and startswith(\"sha512-\")",
		"type == \"array\" and length == 1",
		"selects an in-memory manager",
		"This proves source behavior, not a successful Orb pilot",
		"65536",
		"16384",
		"Do not send the full event stream or raw stderr",
		"no retry or fallback",
		"native Amp messaging",
		"Local removal never proves provider-side token revocation",
		"stat -c '%d:%i'",
		"useful versus discarded findings",
		"setup/coordination cost",
	} {
		if !strings.Contains(recipe, required) {
			t.Errorf("Pi/Spark recipe is missing %q", required)
		}
	}
	for _, forbidden := range []string{"T-019f", "/Users/", "CLAUDE_CODE_OAUTH_TOKEN", "Gas City adoption"} {
		if strings.Contains(recipe, forbidden) {
			t.Errorf("Pi/Spark recipe contains forbidden unrelated/private marker %q", forbidden)
		}
	}
	for _, forbidden := range []string{"> >(", "2> >(", "SYSTEM_PATH=$PATH"} {
		if strings.Contains(recipe, forbidden) {
			t.Errorf("Pi/Spark recipe contains unsafe executable marker %q", forbidden)
		}
	}
	if count := strings.Count(recipe, `TERM="${TERM:-xterm-256color}"`); count != 2 {
		t.Errorf("Pi/Spark recipe passes TERM %d times, want login and logout only", count)
	}
	if count := strings.Count(recipe, `timeout --signal=TERM --kill-after=5s 600s "$PI"`); count != 2 {
		t.Errorf("Pi/Spark recipe bounds %d interactive auth commands, want login and logout", count)
	}
	for _, step := range []string{
		"npm-metadata", "node-checksums-download", "node-archive-download", "node-checksum",
		"node-extract", "node-version", "npm-pack", "npm-install", "pi-version", "npm-list",
		"model-catalog", "npm-uninstall",
	} {
		pattern := regexp.MustCompile(`run_setup ` + regexp.QuoteMeta(step) + ` [0-9]+ [0-9]+ [0-9]+ `)
		if !pattern.MatchString(recipe) {
			t.Errorf("Pi/Spark setup step %q lacks timeout/output bounds", step)
		}
	}
	if count := strings.Count(recipe, "--connect-timeout 10 --max-time 110 --max-filesize"); count != 2 {
		t.Errorf("Pi/Spark recipe has %d internally bounded downloads, want 2", count)
	}
	runStart := strings.Index(recipe, "RESULT=$RUN_DIR/result.txt")
	if runStart < 0 {
		t.Fatal("Pi/Spark run block is missing its result binding")
	}
	runBlock := recipe[runStart:]
	ordered := []string{
		"credential_environment_preflight",
		"mkfifo -m 600",
		"STDOUT_READER_PID=$!",
		"STDERR_READER_PID=$!",
		"wait \"$STDOUT_READER_PID\"",
		"wait \"$STDERR_READER_PID\"",
		"rm -- \"$STDOUT_FIFO\" \"$STDERR_FIFO\"",
		"STDOUT_BYTES=$(wc -c",
		"jq -Rce 'fromjson",
	}
	last := -1
	for _, marker := range ordered {
		at := strings.Index(runBlock, marker)
		if at <= last {
			t.Errorf("Pi/Spark capture invariant missing or out of order: %q", marker)
		}
		last = at
	}
}

func TestPiSparkResultPipelineDecodesSchemaCorrectTextDelta(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	recipe := readSkillFile(t, root, filepath.Join("skills", "amux-pi", "reference", "pi-spark-orb-executor.md"))
	const pipeline = `jq -s '[.[] | select(.type == "message_update") | .assistantMessageEvent | select(.type == "text_delta") | .delta] | join("")' "$VALIDATED_EVENTS" | jq -r . >"$RESULT"`
	if !strings.Contains(recipe, pipeline) {
		t.Fatal("Pi/Spark recipe is missing the reviewed result-decoding pipeline")
	}

	dir := t.TempDir()
	events := filepath.Join(dir, "events.validated.jsonl")
	result := filepath.Join(dir, "result.txt")
	fixture := "{\"type\":\"message_update\",\"message\":{\"role\":\"assistant\",\"content\":[]},\"assistantMessageEvent\":{\"type\":\"text_delta\",\"delta\":\"SPARK_PROBE_OK\"}}\n"
	if err := os.WriteFile(events, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", pipeline)
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"VALIDATED_EVENTS=" + events,
		"RESULT=" + result,
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("result pipeline failed: %v: %s", err, output)
	}
	output, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "SPARK_PROBE_OK\n" {
		t.Fatalf("result pipeline output=%q, want exact decoded probe", output)
	}
}

func TestInvocationPolicyIsProgressivelyDisclosedWithoutChangingClaudeRoutes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	policy := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	claude := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-read-only-delegation.md"))
	for _, required := range []string{"amp-invocation-policy.md", "Never bypass a binding `ask` or `reject`"} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md is missing invocation-policy routing %q", required)
		}
	}
	for _, required := range []string{"observed", "instruction-only", "Raw delegated arguments are never logged", "Amp-native `runner(id)`", "unknown charge route", "public-safe", "#147", "#176"} {
		if !strings.Contains(policy, required) {
			t.Errorf("invocation policy is missing %q", required)
		}
	}
	for _, required := range []string{"exact Workspace Project and Orb", "known linked ChatGPT subscription", "small mechanical work", "ordinary implementation", "hard architecture", "one exact runner ID", "parent/child route", "automatic adoption"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("native creation workflow is missing %q", required)
		}
	}
	if strings.Contains(claude, "amp-invocation-policy") {
		t.Error("independent Claude route unexpectedly loads invocation policy")
	}
}

func TestNativeCreationDoesNotAdoptOrClaimExecutorMigration(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	adr := readSkillFile(t, root, filepath.Join("docs", "adr", "0003-native-thread-creation-and-explicit-adoption.md"))
	readme := readSkillFile(t, root, "README.md")
	for path, check := range map[string]struct {
		contents string
		required []string
	}{
		"workflow": {workflow, []string{"exact Workspace Project and Orb", "one exact runner ID", "automatic adoption", "Same-directory Amux ownership remains separate and unmanaged", "stop without retry"}},
		"ADR 0003": {adr, []string{"Adoption does not re-home, migrate, or retarget", "does not verify continued affinity", "admission-canonicalized workdir", "authoritative catalog spelling unchanged", "execution affinity as `unknown`"}},
		"README":   {readme, []string{"exact intended Workspace Project and Orb", "one exact live runner", "do not call it automatically", "do not fall back between executors", "stop without retrying"}},
	} {
		for _, required := range check.required {
			if !strings.Contains(check.contents, required) {
				t.Errorf("%s is missing explicit native-affinity boundary %q", path, required)
			}
		}
	}
}

func TestADR0003IsHistoricalAndSupersededByADR0007(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	adr3 := readSkillFile(t, root, filepath.Join("docs", "adr", "0003-native-thread-creation-and-explicit-adoption.md"))
	adr7 := readSkillFile(t, root, filepath.Join("docs", "adr", "0007-retire-amux-through-native-cutover-and-staged-drain.md"))

	if !strings.HasPrefix(adr3, "---\nstatus: superseded\nsuperseded-by: 0007\n---\n") {
		t.Error("ADR 0003 frontmatter must mark it superseded by ADR 0007")
	}
	for _, required := range []string{
		"# Historical: native Amp thread creation followed by explicit Amux adoption",
		"**Historical only — superseded by [ADR 0007]",
		"Every command, workflow step, preflight, ordering or recovery action, failure-matrix behavior, migration gate, and other imperative statement below is superseded and non-current",
		"New native children remain unmanaged by Amux and retain native parent/reply routing only",
		"Only an exact persisted pre-cutover drain-eligible adoption operation may continue its exact allowed next transition under ADR 0007",
		"Do not native-create then adopt, group, report, or otherwise enroll new work into Amux",
		"## Historical decision (superseded; non-current)",
		"## Historical preflight, ordering, and recovery (superseded; non-current)",
		"## Historical failure matrix (superseded; non-current)",
		"## Historical migration and removal gate (superseded; non-current)",
	} {
		if !strings.Contains(adr3, required) {
			t.Errorf("ADR 0003 is missing historical-only boundary %q", required)
		}
	}
	for _, forbidden := range []string{
		"status: accepted",
		"# Use native Amp thread creation followed by explicit amux adoption",
		"Worker assignment becomes a two-owner protocol:",
		"The prototype command is:",
		"For a new adoption path,",
		"The `/amux` skill uses native create → adopt for new workers.",
	} {
		if strings.Contains(adr3, forbidden) {
			t.Errorf("ADR 0003 retains accepted/current native-to-adopt wording %q", forbidden)
		}
	}

	for _, required := range []string{
		"supersedes: 0003, 0005, 0006",
		"supersedes ADR 0003's native-create → `amux worker adopt` new-work workflow",
		"ADR 0003 remains only historical rationale and evidence; none of its operational instructions are current",
		"New native children remain unmanaged by Amux",
		"Only an exact persisted pre-cutover drain-eligible adoption operation may continue its exact allowed next transition",
	} {
		if !strings.Contains(adr7, required) {
			t.Errorf("ADR 0007 is missing ADR 0003 supersession boundary %q", required)
		}
	}
}

func TestDurableTaskGroupLeadTitleGuidanceIsPresent(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{"Every durable task-group Lead title starts with `🎖️ `", "never deliberately apply it to member workers", "presentation only", "neither executor placement nor authoritative group role", "Do not rename a thread merely to drain it", "Existing presentation metadata conveys no new authority"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("durable task-group Lead title guidance is missing %q", required)
		}
	}
}

func TestReadThreadDiscrepancyRecoveryContractStaysAligned(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, relativePath := range []string{
		filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"),
		filepath.Join("skills", "amux", "reference", "workflows.md"),
		filepath.Join("skills", "amux", "reference", "troubleshooting.md"),
	} {
		contents := readSkillFile(t, root, relativePath)
		for _, required := range []string{"authorized `/amux` lifecycle or coordination operation", "concrete local/GitHub discrepancy", "deterministic evidence", "durable/local/GitHub evidence", "one narrow query", "block rather than widening or chaining"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing discrepancy-recovery contract %q", relativePath, required)
			}
		}
	}
}

func TestCoordinatorWorkflowMatchesDurableCLIContract(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	stages := []string{"### 1. Preflight existing authoritative state", "### 2. Revalidate the existing coordinator lease", "### 3. Continue only the existing member", "### 4. Persist ready, wake, acknowledge, and independently verify", "### 5. Merge, verify post-merge CI, then authorize finish", "### 6. Submit merged and run `/amux finish`", "### 7. Coordinator-owned deadline queue"}
	last := -1
	for _, stage := range stages {
		at := strings.Index(workflow, stage)
		if at <= last {
			t.Errorf("coordinator stage missing or out of order: %q", stage)
		}
		last = at
	}
	for _, required := range []string{
		"native parent/child association", "authenticated `create_thread`", "exact executor/workdir", "task-only assignment", "leave it unmanaged by Amux", "compatibility-only", "Do not add a new member", "--group <durable-issue-group>",
		"amux report submit --report-id <stable-report-id>", "amux report pending --group <durable-issue-group>", "amux report acknowledge --report-id <stable-report-id>",
		"PR URL, head branch/SHA", "amux report authorize-finish --report-id <stable-report-id>", "post-merge CI", "--status merged", "amux teardown --thread <member-thread>", "Group membership and report history survive teardown",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("coordinator workflow is missing %q", required)
		}
	}
}

func TestIssueCoordinationUsesNativeIdentityAndDrainsExistingDurableIdentity(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	readme := readSkillFile(t, root, "README.md")
	for _, required := range []string{"issue-bearing branch/worktree", "issue-unprefixed semantic title", "authenticated native-created thread", "native identity and reply routing", "already-recorded member thread", "stable report ID"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("workflow is missing native/drain identity rule %q", required)
		}
	}
	for _, required := range []string{"spawn-native-cutover-v1", "do not call it automatically", "remain drain-writable only", "compatibility/drain surface only for an exact persisted pre-cutover adoption operation whose next transition is proven drain-eligible"} {
		if !strings.Contains(readme, required) {
			t.Errorf("README is missing native/drain cutover rule %q", required)
		}
	}
}

func TestConfigurableGroupNamingSourceReferencesStayConsistent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	current := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "commands.md")) + readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, removed := range []string{"--work-item-id", "--worker-ordinal", "GROUP_NAMING<TAB>"} {
		if strings.Contains(current, removed) {
			t.Errorf("current native workflow still advertises removed automatic spawn naming %q", removed)
		}
	}
	for _, required := range []string{"never automatic after native creation", "projectless physical-host", "stable report ID"} {
		if !strings.Contains(current, required) {
			t.Errorf("current native/drain workflow is missing explicit identity %q", required)
		}
	}
}

func TestWorkGroupCompletionsExposeImplementedCommands(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	checks := map[string][]string{
		"bash": {"declare add remove coordinator list show reconcile", "register clear", "submit pending history acknowledge authorize-finish", "worker", "adopt"},
		"zsh":  {"group_commands=(", "callback_commands=(", "report_commands=(", "--report-id", "--pane", "adopt"},
		"fish": {"__fish_amux_group_leaf", "__fish_amux_callback_leaf", "__fish_amux_report_leaf", "authorize-finish", "report-id", "pane", "adopt"},
	}
	for shell, wants := range checks {
		command := exec.Command("go", "run", "./cmd/amux", "completion", shell)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("completion %s failed: %v\n%s", shell, err, output)
		}
		for _, want := range wants {
			if !strings.Contains(string(output), want) {
				t.Errorf("completion %s is missing %q", shell, want)
			}
		}
		for _, removed := range []string{"--message-file", "--idempotency-key", "--work-item-id", "--worker-ordinal"} {
			if strings.Contains(string(output), removed) {
				t.Errorf("completion %s retains removed spawn flag %q", shell, removed)
			}
		}
	}
}

func TestInvocationProbeEvidenceIsReproducibleAndBounded(t *testing.T) {
	t.Parallel()
	probe := readSkillFile(t, repoRoot(t), filepath.Join("docs", "proposals", "issue-175-invocation-policy-probes.md"))
	for _, required := range []string{
		"https://ampcode.com/notes/permissions",
		"https://ampcode.com/manual#permissions",
		"https://ampcode.com/manual#use-a-built-in-agent",
		"amp --settings-file \"$PROBE/settings.json\" permissions add delegate",
		"permissions test shell_command",
		"helper=0 cli=0",
		"helper=1 cli=1",
		"helper=2 cli=2",
		"name=create_thread exit=1 stdout=No such tool: create_thread",
		"reported/unverified",
		"not publicly reproducible evidence",
	} {
		if !strings.Contains(probe, required) {
			t.Errorf("probe evidence is missing %q", required)
		}
	}
	for _, forbidden := range []string{"/Users/", "used_percent", "resets_at", "T-019f"} {
		if strings.Contains(probe, forbidden) {
			t.Errorf("probe evidence contains private/runtime marker %q", forbidden)
		}
	}
}

func TestExperimentalClaudeDelegationReferencesStayNarrowAndConsistent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-read-only-delegation.md"))
	mutating := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-mutating-delegation.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-delegation-contract.md"))
	recovery := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-delegation-recovery.md"))

	stages := []string{"## 1. Preflight", "## 2. Create the receipt", "## 3. Launch and acquire", "## 4. Recover and deliver", "## 5. Acknowledge", "## 6. Park explicitly"}
	last := -1
	for _, stage := range stages {
		at := strings.Index(workflow, stage)
		if at <= last {
			t.Errorf("experimental Claude stage missing or out of order: %q", stage)
		}
		last = at
	}
	for _, required := range []string{"valid_report → delivered → acknowledged → verified_parked", "machine-local inbox", "notification is not delivery", "no automatic response injection", "cleanup-eligible", "no compatibility guarantee"} {
		if !strings.Contains(contract, required) {
			t.Errorf("experimental Claude contract is missing %q", required)
		}
	}
	for _, required := range []string{"same event ID", "leave the receipt recoverable", "Do not infer", "Do not automatically"} {
		if !strings.Contains(recovery, required) {
			t.Errorf("experimental Claude recovery is missing %q", required)
		}
	}
	for _, required := range []string{
		"exclusive logical write ownership",
		"one clean local commit",
		"zero commits",
		"submission freeze",
		"mutation validate-handoff",
		"never proves correctness, acceptance, merge readiness, or cleanup authority",
		"Never park automatically",
	} {
		if !strings.Contains(mutating, required) {
			t.Errorf("experimental mutating Claude contract is missing %q", required)
		}
	}
}

func TestExperimentalTychoReportBridgeStaysReportOnly(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	core := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	skill := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "SKILL.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "trigger-phrases.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "tycho-report-bridge.md"))
	matrix := readSkillFile(t, root, filepath.Join("docs", "provider-executor-readiness.md"))
	for _, required := range []string{
		"receipt's immutable real Amp origin remains coordinator and consume/acknowledgement authority",
		"typed report-only producer",
		"no group, member, callback, finish, label, provider-identity, or lifecycle authority",
		"owner-authorized external Tycho second opinion",
		"never grant Tycho GitHub review mutation or readiness promotion",
	} {
		if !strings.Contains(core, required) {
			t.Errorf("core /amux Tycho pointer is missing authority boundary %q", required)
		}
	}
	for _, forbidden := range []string{
		"tycho-report-bridge.md",
		"team-review-second-opinion.md",
		"tycho_report_bridge.py",
		"created → valid_report → delivered → acknowledged",
		"one-time Amp schedule",
		"claude-opus-5",
		"PENDING GitHub review",
	} {
		if strings.Contains(core, forbidden) {
			t.Errorf("core /amux duplicates detailed Tycho protocol %q", forbidden)
		}
	}
	for _, required := range []string{
		"Bind the minimum semantic receipt",
		"existing real Amp coordinator",
		"exact owner-selected producer route",
		"task bytes covered by `task_digest`; do not add another field",
		"Submit one typed report",
		"Consume separately",
		"Acknowledge separately",
		"Route selection",
		"Task-specific validation",
		"Exceptional recovery",
		"Optional formal promotion policy",
		"Migrating pre-split receipts",
		"a single one-time Amp schedule",
		"only re-checks the exact bound local Tycho agent's status/result",
		"Clear it as soon as the run reaches a terminal or recovered state",
		"only a wake-up token—never durable truth, delivery, consume, or acknowledgement",
		"Do not turn it into a recurring watcher",
		"Authoritative Amp `/team-review` with one Opus second opinion",
		"reference/team-review-second-opinion.md",
		"temporary compatibility transport",
		"native authenticated structured delivery and separate acknowledgement",
		"Only Amp mutates the PENDING GitHub review",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("amux-tycho workflow is missing %q", required)
		}
	}
	for _, required := range []string{
		"explicit-only",
		"Incidental mentions",
		"Recover this /amux-tycho receipt",
		"report_only",
		"Authoritative Amp /team-review with one Opus second opinion",
		"team-review-second-opinion.md",
		"does not promote readiness",
	} {
		if !strings.Contains(triggers, required) {
			t.Errorf("amux-tycho trigger checklist is missing %q", required)
		}
	}
	for _, required := range []string{
		"created → valid_report → delivered → acknowledged",
		"report_only",
		"Notification is never delivery or acknowledgement",
		"no arbitrary Amp Web-thread delivery claim",
		"There is no resident watcher",
		"no compatibility guarantee",
		"Minimal semantic contract",
		"task/artifact digest",
		"no additional receipt field is required",
		"temporary compatibility transport",
		"native authenticated structured report delivery and separate acknowledgement",
		"process exit, logs, and prose never substitute",
		"team-review-second-opinion.md",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("experimental Tycho contract is missing %q", required)
		}
	}
	if !strings.Contains(matrix, "| `/amux-tycho` semantic-report receipt/inbox | Conditional |") ||
		!strings.Contains(matrix, "Tycho has `report_only` authority") ||
		!strings.Contains(matrix, "exact producer route") ||
		!strings.Contains(matrix, "task/artifact digest") ||
		!strings.Contains(matrix, "Tycho never mutates GitHub reviews") ||
		!strings.Contains(matrix, "#328") ||
		!strings.Contains(matrix, "optional promotion policy") {
		t.Error("readiness matrix overstates or omits the experimental Tycho route")
	}
	for _, path := range []string{
		filepath.Join("skills", "amux-tycho", "experimental", "tycho-report-bridge", "tycho_report_bridge.py"),
		filepath.Join("skills", "amux-tycho", "experimental", "tycho-report-bridge", "tycho_report_bridge_test.go"),
		filepath.Join("skills", "amux-tycho", "reference", "tycho-report-bridge.md"),
		filepath.Join("skills", "amux-tycho", "reference", "team-review-second-opinion.md"),
		filepath.Join("skills", "amux-tycho", "reference", "trigger-phrases.md"),
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("amux-tycho split payload is missing %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join("skills", "amux", "experimental", "tycho-report-bridge", "tycho_report_bridge.py"),
		filepath.Join("skills", "amux", "experimental", "tycho-report-bridge", "tycho_report_bridge_test.go"),
		filepath.Join("skills", "amux", "reference", "tycho-report-bridge.md"),
	} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Errorf("core /amux retains a drift-prone Tycho payload at %s: %v", path, err)
		}
	}
}

func TestTychoPolicyCategoriesStaySeparate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "SKILL.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "team-review-second-opinion.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "tycho-report-bridge.md"))
	matrix := readSkillFile(t, root, filepath.Join("docs", "provider-executor-readiness.md"))

	ordinaryStart := strings.Index(skill, "## Explicit-only workflow")
	ordinaryEnd := strings.Index(skill, "## Route selection")
	if ordinaryStart < 0 || ordinaryEnd <= ordinaryStart {
		t.Fatal("amux-tycho skill does not separate the ordinary receipt contract from route selection")
	}
	ordinary := skill[ordinaryStart:ordinaryEnd]
	for _, required := range []string{"existing real Amp coordinator", "exact owner-selected producer route", "task_digest", "`complete` or `blocked`", "`consume`", "`acknowledge`"} {
		if !strings.Contains(ordinary, required) {
			t.Errorf("ordinary receipt contract is missing %q", required)
		}
	}
	for _, conflated := range []string{"worktree-cleanliness", "entitlement", "natural-failure", "cleanup replay", "#327", "#328"} {
		if strings.Contains(ordinary, conflated) {
			t.Errorf("ordinary receipt contract conflates policy category %q", conflated)
		}
	}

	for name, contents := range map[string]string{"skill": skill, "bridge contract": contract, "#328 workflow": workflow} {
		for _, stale := range []string{"Runtime-unverified", "runtime-unverified", "categorical field-cycle blocker", "#327 categorical"} {
			if strings.Contains(contents, stale) {
				t.Errorf("%s retains stale Tycho policy %q", name, stale)
			}
		}
	}
	for _, required := range []string{"Route selection", "Task-specific validation", "Exceptional recovery", "Optional formal promotion"} {
		if !strings.Contains(skill, "## "+required) || !strings.Contains(workflow, required) {
			t.Errorf("Tycho policy does not consistently label category %q", required)
		}
	}
	for _, required := range []string{"temporary compatibility transport", "native authenticated structured report delivery", "no additional receipt field is required"} {
		if !strings.Contains(contract, required) {
			t.Errorf("bridge removal contract is missing %q", required)
		}
	}

	var tychoRow string
	for _, line := range strings.Split(matrix, "\n") {
		if strings.HasPrefix(line, "| `/amux-tycho` semantic-report receipt/inbox |") {
			tychoRow = line
			break
		}
	}
	if tychoRow == "" {
		t.Fatal("readiness matrix has no /amux-tycho row")
	}
	for _, stale := range []string{"Runtime-unverified", "categorical", "no live Tycho cycle"} {
		if strings.Contains(tychoRow, stale) {
			t.Errorf("readiness row retains stale Tycho policy %q", stale)
		}
	}
}

func TestTeamReviewSecondOpinionWorkflowStaysReportOnlyAndProgressivelyDisclosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	core := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	skill := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "SKILL.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "team-review-second-opinion.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "tycho-report-bridge.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "trigger-phrases.md"))
	matrix := readSkillFile(t, root, filepath.Join("docs", "provider-executor-readiness.md"))
	helper, err := os.ReadFile(filepath.Join(root, "skills", "amux-tycho", "experimental", "tycho-report-bridge", "tycho_report_bridge.py"))
	if err != nil {
		t.Fatal(err)
	}
	helperText := string(helper)

	for _, required := range []string{
		"Settled design decisions",
		"Bounded `complete` / `blocked` finding schema",
		"Producer-only submit capability delivery",
		"Truthful blocked-report behavior near provider stop",
		"Task-specific validation: reviewed-artifact identity",
		"Stale / concurrent PENDING-review generation protection",
		"Exact evidence required for the #328 workflow",
		"Authoritative Amp first pass",
		"Create the receipt **before** Tycho execution",
		"exactly one durable `submit`",
		"Independently reproduce or reject **every** candidate",
		"Tycho must never call GitHub review or comment mutation APIs",
		"Path or directory-name equality is not identity",
		"full head SHA",
		"HEAD^{tree}",
		"Both Amp and Tycho review worktrees must be clean",
		"Stop rather than overwrite",
		"Exit codes (including `143`)",
		"no Tycho finding",
		"normally exact `claude-opus-5`",
		"Six PR #11886 gaps",
		"promote `/amux-tycho`",
		"alter closed [#323](https://github.com/zainfathoni/amux/issues/323)",
		"does not widen stable Amux core",
		"tycho-report-bridge.md",
		"authority: \"report_only\"",
		"Publication",
		"Wake-ups and schedules never imply them",
		"Desire to formally promote the transport",
		"refused here",
		// Application report invariants beyond generic bridge schema.
		"`complete` must use `blockers: []`",
		"non-empty for both statuses",
		"application-invalid",
		// Producer-only GitHub boundary.
		"GitHub credentials intended for review mutation",
		"no new GitHub write credentials",
		// Artifact task freeze includes route identity.
		"Tycho agent key, project, harness, model",
		"task_digest` is SHA-256 of those exact task bytes",
		// PENDING ownership + snapshot contract.
		"owns for this assignment",
		"unowned pre-existing current-user PENDING review is always a conflict",
		"Canonical PENDING snapshot",
		"Comment canonicalization",
		"Comment `updated_at`",
		"Do **not** use review `submitted_at` as a freshness signal",
		"does not provide atomic compare-and-swap",
		"Residual TOCTOU",
		"Pinned PR head revalidation (every write)",
		"baseline-none/create path and the existing-owned-review reconciliation path",
		"PR head SHA ≠ pinned reviewed SHA, or PR head read failed",
		// Artifact timing + helper non-attestation.
		"Post-Tycho / pre-consume",
		"bridge helper does **not** attest Git state",
		"reject the application payload",
		// #328 evidence completeness without reopening #323 policy or its historical #327 gate.
		"Historical #327 gate",
		"completed by merged PR #361",
		"native `create_thread`",
		"Pre-Tycho",
		"Post-Tycho / pre-consume** reviewed-artifact proof",
		"per-write PR head equality checks",
		"Pre-Tycho and post-Tycho GitHub snapshots",
		"Cleanup evidence from acknowledge output, not `show`",
		"show` cannot supply it",
		"final `show` only to inspect terminal `acknowledged`",
		"no Tycho-phase review/comment mutation",
		"Committed tree object identity",
		"does **not** detect dirty index or worktree content by itself",
		"An ambiguous, partial, or failed read is a deny",
		"bridge helper never attests Git or GitHub head state",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("team-review second-opinion workflow is missing %q", required)
		}
	}
	if !strings.Contains(workflow, "Out of scope") || !strings.Contains(workflow, "Stable `cmd/` or `internal/` changes") {
		t.Error("team-review workflow must keep stable cmd/internal changes out of scope")
	}
	if strings.Contains(workflow, "| `/amux-tycho` semantic-report receipt/inbox | Proven") ||
		strings.Contains(workflow, "marks `/amux-tycho` field-proven") {
		t.Error("team-review workflow must not promote readiness")
	}
	if strings.Contains(core, "team-review-second-opinion.md") || strings.Contains(core, "Six PR #11886") {
		t.Error("core /amux must not embed the detailed team-review second-opinion protocol")
	}
	if !strings.Contains(skill, "reference/team-review-second-opinion.md") {
		t.Error("amux-tycho SKILL.md must progressively disclose the team-review workflow")
	}
	if !strings.Contains(skill, "#327") || !strings.Contains(skill, "completed by merged PR #361") || !strings.Contains(skill, "native Amp coordinator") {
		t.Error("amux-tycho SKILL.md must retire #327 as a current #328 blocker")
	}
	if !strings.Contains(triggers, "Authoritative Amp /team-review with one Opus second opinion") {
		t.Error("amux-tycho triggers must route the team-review second-opinion phrase")
	}
	if !strings.Contains(contract, "team-review-second-opinion.md") {
		t.Error("canonical bridge protocol must point at the application workflow without duplicating a second helper")
	}
	if !strings.Contains(matrix, "| `/amux-tycho` semantic-report receipt/inbox | Conditional |") || strings.Contains(matrix, "| `/amux-tycho` semantic-report receipt/inbox | Proven") {
		t.Error("#328 must not formally promote the conditional /amux-tycho readiness row")
	}
	if !strings.Contains(matrix, "completed by merged PR #361") ||
		!strings.Contains(matrix, "historical local-worker workflow") ||
		!strings.Contains(matrix, "native Amp coordinator") {
		t.Error("readiness matrix must retire #327 as a current #328 blocker")
	}
	// Reuse one canonical helper/schema rather than a duplicate implementation.
	if strings.Contains(workflow, "tycho_report_bridge_v2") || strings.Contains(workflow, "second_opinion_bridge.py") {
		t.Error("team-review workflow must not introduce a second bridge helper")
	}
	for _, required := range []string{
		`"complete"`,
		`"blocked"`,
		"findings",
		"blockers",
		"verification",
		"report_only",
		"MAX_LIST_ITEMS = 32",
		"MAX_INPUT_BYTES = 64 * 1024",
	} {
		if !strings.Contains(helperText, required) {
			t.Errorf("canonical helper is missing settled report contract piece %q", required)
		}
	}

	// Documentation-consistency fixtures only: encode the settled workflow rules as
	// pure examples. They are not runtime enforcement and do not call the bridge helper
	// for Git/GitHub attestation (the helper does not attest Git state).

	// Task digest changes when route/head/workdir fields change.
	type taskFields struct {
		repo, pr, head, tree, ampWT, tychoWT, agent, project, harness, model string
	}
	base := taskFields{
		repo: "acme/widgets", pr: "11886",
		head:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		tree:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ampWT: "/tmp/amp-review", tychoWT: "/tmp/tycho-review",
		agent: "reviewer-1", project: "bta", harness: "claude", model: "claude-opus-5",
	}
	taskDigest := func(f taskFields) string {
		payload := strings.Join([]string{
			"repo=" + f.repo, "pr=" + f.pr, "head=" + f.head, "tree=" + f.tree,
			"amp_workdir=" + f.ampWT, "tycho_workdir=" + f.tychoWT,
			"agent=" + f.agent, "project=" + f.project, "harness=" + f.harness, "model=" + f.model,
			"producer_role=team_review_second_opinion", "authority=report_only",
		}, "\n")
		sum := sha256.Sum256([]byte(payload))
		return hex.EncodeToString(sum[:])
	}
	baseDigest := taskDigest(base)
	if len(baseDigest) != 64 {
		t.Fatalf("task digest length = %d", len(baseDigest))
	}
	for _, variant := range []struct {
		name string
		mut  func(*taskFields)
	}{
		{"model", func(f *taskFields) { f.model = "claude-opus-4-8" }},
		{"head", func(f *taskFields) { f.head = "cccccccccccccccccccccccccccccccccccccccc" }},
		{"tree", func(f *taskFields) { f.tree = "dddddddddddddddddddddddddddddddddddddddd" }},
		{"tycho workdir", func(f *taskFields) { f.tychoWT = "/tmp/other-tycho" }},
		{"amp workdir", func(f *taskFields) { f.ampWT = "/tmp/other-amp" }},
		{"project", func(f *taskFields) { f.project = "other-project" }},
		{"harness", func(f *taskFields) { f.harness = "other-harness" }},
		{"agent", func(f *taskFields) { f.agent = "other-agent" }},
	} {
		changed := base
		variant.mut(&changed)
		if taskDigest(changed) == baseDigest {
			t.Errorf("doc fixture task freeze %s did not change SHA-256 digest", variant.name)
		}
	}

	// Application report validity examples (independent of bridge envelope).
	type appReport struct {
		name                             string
		status                           string
		findings, blockers, verification []string
		valid                            bool
	}
	for _, report := range []appReport{
		{name: "clean complete", status: "complete", findings: nil, blockers: nil, verification: []string{"git rev-parse HEAD"}, valid: true},
		{name: "blocked partial", status: "blocked", findings: []string{"candidate"}, blockers: []string{"provider_stop"}, verification: []string{"checked head"}, valid: true},
		{name: "complete with blockers", status: "complete", findings: []string{"x"}, blockers: []string{"leftover"}, verification: []string{"ok"}, valid: false},
		{name: "empty verification", status: "complete", findings: nil, blockers: nil, verification: nil, valid: false},
	} {
		appValid := report.status == "complete" && len(report.blockers) == 0 && len(report.verification) > 0 ||
			report.status == "blocked" && len(report.blockers) > 0 && len(report.verification) > 0
		if appValid != report.valid {
			t.Fatalf("doc fixture application report %s: valid=%v want %v", report.name, appValid, report.valid)
		}
	}

	// Canonical PENDING snapshot: sorted-key JSON + comments canonicalized by numeric id.
	type pendingSnapshot map[string]any
	var marshalSorted func(any) []byte
	marshalSorted = func(v any) []byte {
		t.Helper()
		switch typed := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			for i := 1; i < len(keys); i++ {
				j := i
				for j > 0 && keys[j-1] > keys[j] {
					keys[j-1], keys[j] = keys[j], keys[j-1]
					j--
				}
			}
			var b strings.Builder
			b.WriteByte('{')
			for i, key := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				keyJSON, err := json.Marshal(key)
				if err != nil {
					t.Fatal(err)
				}
				b.Write(keyJSON)
				b.WriteByte(':')
				b.Write(marshalSorted(typed[key]))
			}
			b.WriteByte('}')
			return []byte(b.String())
		case []any:
			var b strings.Builder
			b.WriteByte('[')
			for i, item := range typed {
				if i > 0 {
					b.WriteByte(',')
				}
				b.Write(marshalSorted(item))
			}
			b.WriteByte(']')
			return []byte(b.String())
		default:
			raw, err := json.Marshal(typed)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
	}
	commentIDLess := func(a, b string) bool {
		ai, okA := new(big.Int).SetString(a, 10)
		bi, okB := new(big.Int).SetString(b, 10)
		if okA && okB {
			return ai.Cmp(bi) < 0
		}
		return a < b
	}
	canonicalizeComments := func(comments []any) []any {
		out := append([]any(nil), comments...)
		for i := 1; i < len(out); i++ {
			j := i
			for j > 0 {
				prevID, _ := out[j-1].(map[string]any)["id"].(string)
				curID, _ := out[j].(map[string]any)["id"].(string)
				if !commentIDLess(curID, prevID) {
					break
				}
				out[j-1], out[j] = out[j], out[j-1]
				j--
			}
		}
		return out
	}
	snapshotDigest := func(s pendingSnapshot) string {
		cloned := map[string]any{}
		for k, v := range s {
			cloned[k] = v
		}
		if comments, ok := cloned["comments"].([]any); ok {
			cloned["comments"] = canonicalizeComments(comments)
		}
		sum := sha256.Sum256(marshalSorted(cloned))
		return hex.EncodeToString(sum[:])
	}
	cloneSnap := func(s pendingSnapshot) pendingSnapshot {
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var out pendingSnapshot
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	commentA := map[string]any{
		"id": "10", "path": "pkg/a.ts", "line": 10, "original_line": nil,
		"side": "RIGHT", "start_side": nil,
		"commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"body":      "comment A", "updated_at": "2026-08-04T12:00:00Z",
	}
	commentB := map[string]any{
		"id": "2", "path": "pkg/b.ts", "line": 2, "original_line": nil,
		"side": "RIGHT", "start_side": nil,
		"commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"body":      "comment B", "updated_at": "2026-08-04T12:01:00Z",
	}
	baseSnap := pendingSnapshot{
		"review_id": "111",
		"commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"body":      "baseline body",
		"state":     "PENDING",
		"comments":  []any{commentA, commentB}, // API order: 10 then 2
	}
	// Reversed API order must yield the same digest after id canonicalization.
	reversedSnap := cloneSnap(baseSnap)
	reversedSnap["comments"] = []any{cloneMap(t, commentB), cloneMap(t, commentA)}
	baseSnapDigest := snapshotDigest(baseSnap)
	if got := snapshotDigest(reversedSnap); got != baseSnapDigest {
		t.Fatalf("doc fixture comment order invariance: got %s want %s", got, baseSnapDigest)
	}
	// Ensure canonical order is numeric (2 before 10), not lexicographic string order.
	sorted := canonicalizeComments([]any{cloneMap(t, commentA), cloneMap(t, commentB)})
	if sorted[0].(map[string]any)["id"] != "2" || sorted[1].(map[string]any)["id"] != "10" {
		t.Fatalf("doc fixture comment id sort order = %#v", sorted)
	}
	for _, variant := range []struct {
		name string
		mut  func(pendingSnapshot)
	}{
		{"review body", func(s pendingSnapshot) { s["body"] = "edited body" }},
		{"review id", func(s pendingSnapshot) { s["review_id"] = "222" }},
		{"review state", func(s pendingSnapshot) { s["state"] = "COMMENTED" }},
		{"comment body", func(s pendingSnapshot) {
			s["comments"].([]any)[0].(map[string]any)["body"] = "edited comment"
		}},
		{"comment updated_at", func(s pendingSnapshot) {
			s["comments"].([]any)[0].(map[string]any)["updated_at"] = "2026-08-04T13:00:00Z"
		}},
		{"comment path", func(s pendingSnapshot) {
			s["comments"].([]any)[0].(map[string]any)["path"] = "pkg/z.ts"
		}},
		{"comment line", func(s pendingSnapshot) {
			s["comments"].([]any)[0].(map[string]any)["line"] = 99
		}},
		{"add comment", func(s pendingSnapshot) {
			s["comments"] = append(s["comments"].([]any), map[string]any{
				"id": "3", "path": "pkg/c.ts", "line": 1, "original_line": nil,
				"side": "RIGHT", "start_side": nil, "commit_id": s["commit_id"],
				"body": "new", "updated_at": "2026-08-04T12:02:00Z",
			})
		}},
		{"delete comment", func(s pendingSnapshot) { s["comments"] = []any{} }},
	} {
		changed := cloneSnap(baseSnap)
		variant.mut(changed)
		if snapshotDigest(changed) == baseSnapDigest {
			t.Errorf("doc fixture pending snapshot %s did not change SHA-256 digest", variant.name)
		}
	}

	// Mutation gate examples: ownership + snapshot + PR head equality + read health.
	// Covers baseline-none/create and existing-owned-review paths.
	pinnedHead := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	type mutationCase struct {
		name           string
		ownedID        string
		pendingIDs     []string
		baselineDigest string
		current        pendingSnapshot
		prHead         string
		prHeadReadOK   bool
		snapshotReadOK bool
		allowMutation  bool
	}
	for _, pc := range []mutationCase{
		{name: "create when none head stable", ownedID: "", pendingIDs: nil, prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: true},
		{name: "create when none head advanced", ownedID: "", pendingIDs: nil, prHead: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "create when none head read failed", ownedID: "", pendingIDs: nil, prHead: "", prHeadReadOK: false, snapshotReadOK: true, allowMutation: false},
		{name: "create blocked by unexpected pending", ownedID: "", pendingIDs: []string{"999"}, prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "owned stable head stable", ownedID: "111", pendingIDs: []string{"111"}, baselineDigest: baseSnapDigest, current: baseSnap, prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: true},
		{name: "owned stable head advanced", ownedID: "111", pendingIDs: []string{"111"}, baselineDigest: baseSnapDigest, current: baseSnap, prHead: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "owned stable head read failed", ownedID: "111", pendingIDs: []string{"111"}, baselineDigest: baseSnapDigest, current: baseSnap, prHead: "", prHeadReadOK: false, snapshotReadOK: true, allowMutation: false},
		{name: "unowned sole", ownedID: "", pendingIDs: []string{"999"}, prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "body drift", ownedID: "111", pendingIDs: []string{"111"}, baselineDigest: baseSnapDigest, current: func() pendingSnapshot {
			s := cloneSnap(baseSnap)
			s["body"] = "drift"
			return s
		}(), prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "id changed", ownedID: "111", pendingIDs: []string{"222"}, baselineDigest: baseSnapDigest, current: func() pendingSnapshot {
			s := cloneSnap(baseSnap)
			s["review_id"] = "222"
			return s
		}(), prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "ambiguous two", ownedID: "111", pendingIDs: []string{"111", "222"}, baselineDigest: baseSnapDigest, current: baseSnap, prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: true, allowMutation: false},
		{name: "snapshot read failed", ownedID: "111", pendingIDs: []string{"111"}, baselineDigest: baseSnapDigest, current: baseSnap, prHead: pinnedHead, prHeadReadOK: true, snapshotReadOK: false, allowMutation: false},
	} {
		allow := false
		if pc.prHeadReadOK && pc.prHead == pinnedHead && pc.snapshotReadOK {
			switch {
			case len(pc.pendingIDs) == 0 && pc.ownedID == "":
				allow = true
			case len(pc.pendingIDs) == 1 && pc.pendingIDs[0] == pc.ownedID && pc.ownedID != "" && pc.current != nil &&
				snapshotDigest(pc.current) == pc.baselineDigest:
				allow = true
			}
		}
		if allow != pc.allowMutation {
			t.Fatalf("doc fixture mutation gate %s: allow=%v want %v", pc.name, allow, pc.allowMutation)
		}
	}

	// Post-Tycho / pre-consume attachment proof examples (helper does not attest these).
	type attachment struct {
		repo, workdir, head, tree string
		clean, readOK             bool
	}
	type attachCase struct {
		name                                                           string
		pinnedRepo, pinnedAmpWT, pinnedTychoWT, pinnedHead, pinnedTree string
		amp, tycho                                                     attachment
		acceptApplication                                              bool
	}
	for _, ac := range []attachCase{
		{
			name:       "both stable",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/widgets", workdir: "/tmp/tycho-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			acceptApplication: true,
		},
		{
			name:       "tycho head drifted",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/widgets", workdir: "/tmp/tycho-review", head: "ffffffffffffffffffffffffffffffffffffffff", tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			acceptApplication: false,
		},
		{
			name:       "amp tree drifted",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "cccccccccccccccccccccccccccccccccccccccc", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/widgets", workdir: "/tmp/tycho-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			acceptApplication: false,
		},
		{
			name:       "tycho dirty",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/widgets", workdir: "/tmp/tycho-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: false, readOK: true},
			acceptApplication: false,
		},
		{
			name:       "workdir substituted",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/widgets", workdir: "/tmp/other-tycho", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			acceptApplication: false,
		},
		{
			name:       "repo mismatch",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/other", workdir: "/tmp/tycho-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			acceptApplication: false,
		},
		{
			name:       "read failure",
			pinnedRepo: "acme/widgets", pinnedAmpWT: "/tmp/amp-review", pinnedTychoWT: "/tmp/tycho-review",
			pinnedHead: pinnedHead, pinnedTree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			amp:               attachment{repo: "acme/widgets", workdir: "/tmp/amp-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: true},
			tycho:             attachment{repo: "acme/widgets", workdir: "/tmp/tycho-review", head: pinnedHead, tree: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", clean: true, readOK: false},
			acceptApplication: false,
		},
	} {
		match := func(a attachment, wantRepo, wantWT string) bool {
			return a.readOK && a.clean &&
				a.repo == wantRepo && a.workdir == wantWT &&
				a.head == ac.pinnedHead && a.tree == ac.pinnedTree
		}
		accept := match(ac.amp, ac.pinnedRepo, ac.pinnedAmpWT) && match(ac.tycho, ac.pinnedRepo, ac.pinnedTychoWT)
		if accept != ac.acceptApplication {
			t.Fatalf("doc fixture post-tycho attachment %s: accept=%v want %v", ac.name, accept, ac.acceptApplication)
		}
	}
}

func TestTeamReviewSecondOpinionAllowsPreparedRouteAndImmutableRemoteArtifact(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "SKILL.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "team-review-second-opinion.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux-tycho", "reference", "trigger-phrases.md"))
	matrix := readSkillFile(t, root, filepath.Join("docs", "provider-executor-readiness.md"))

	for name, contents := range map[string]string{
		"skill":             skill,
		"workflow":          workflow,
		"trigger checklist": triggers,
		"readiness matrix":  matrix,
	} {
		for _, required := range []string{
			"owner-authorized prepared route",
			"without provider execution",
			"no fallback",
			"immutable remote artifact",
			"full head SHA",
			"full tree SHA",
		} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing prepared-route/remote-artifact policy %q", name, required)
			}
		}
	}

	for _, required := range []string{
		"create exactly one dormant project/agent",
		"must not start the provider",
		"create the immutable receipt before the first provider run",
		"route creation is indeterminate",
		"repository `owner/repo`, PR number, full head SHA, and full tree SHA",
		"coordinator comparison HEAD",
		"must inspect the pinned commit/diff explicitly rather than treating worktree `HEAD` as the reviewed artifact",
		"dual-local-attachment",
		"immutable-remote",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("team-review second-opinion workflow is missing %q", required)
		}
	}

	for _, stale := range []string{
		"remains blocked on #327",
		"#327-specific #327 prerequisite",
		"Do not create a Tycho agent/project",
		"both worktrees report the same full 40-character commit SHA",
	} {
		if strings.Contains(workflow, stale) {
			t.Errorf("team-review second-opinion workflow retains stale gate %q", stale)
		}
	}
}

// cloneMap is a tiny test helper for documentation-consistency fixtures.
func cloneMap(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProviderExecutorReadinessMatrixIsLinkedAndKeepsAuthorityBoundaries(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	matrix := readSkillFile(t, root, filepath.Join("docs", "provider-executor-readiness.md"))
	promotion := readSkillFile(t, root, filepath.Join("docs", "proposals", "issue-309-read-only-claude-cli-promotion-gate.md"))
	matrixURL := "https://github.com/zainfathoni/amux/blob/main/docs/provider-executor-readiness.md"
	for _, skillPath := range []string{
		filepath.Join("skills", "amux-tycho", "SKILL.md"),
		filepath.Join("skills", "amux-claude", "SKILL.md"),
		filepath.Join("skills", "amux-pi", "SKILL.md"),
	} {
		if skill := readSkillFile(t, root, skillPath); !strings.Contains(skill, matrixURL) {
			t.Errorf("%s does not link the installed-skill-safe readiness matrix URL", skillPath)
		}
	}
	for _, required := range []string{
		"| Claude local Darwin read-only thinker | Proven experimental |",
		"`claude-fable-5`, `claude-opus-5`, or `claude-opus-4-8`",
		"| Claude local Darwin mutating Stage A | Conditional |",
		"| Claude fresh-Orb mutation | Blocked |",
		"| Claude operator-assisted local tmux adoption | Proven experimental |",
		"| Pi physical-host bounded replacement | Proven experimental |",
		"| Pi fresh-Orb Spark | Runtime-unverified |",
		"| Pi general repository mutation | Unsupported |",
	} {
		if !strings.Contains(matrix, required) {
			t.Errorf("provider executor readiness matrix is missing %q", required)
		}
	}
	for _, required := range []string{
		"decision: repeat-keep-experimental",
		"Do not add a stable Go `amux` command",
		"one accepted real delivery or lifecycle failure recovered",
		"existing-receipt coexistence or migration semantics",
		"one demonstrated reliability problem or trusted additional consumer",
		"It is not proof of OS-level filesystem confinement",
	} {
		if !strings.Contains(promotion, required) {
			t.Errorf("read-only Claude CLI promotion decision is missing %q", required)
		}
	}
	if !strings.Contains(matrix, "[#309](https://github.com/zainfathoni/amux/issues/309) keeps the Go CLI promotion gate at `repeat`") {
		t.Error("Darwin read-only readiness row does not link the repeat promotion decision")
	}
	for _, line := range strings.Split(matrix, "\n") {
		if strings.HasPrefix(line, "| Claude local Darwin mutating Stage A |") &&
			(!strings.Contains(line, "Exact `claude-opus-4-8` only") || strings.Contains(line, "claude-opus-5")) {
			t.Errorf("mutating Claude readiness row broadened beyond exact Opus 4.8: %s", line)
		}
		if strings.HasPrefix(line, "| Claude operator-assisted local tmux adoption |") &&
			(!strings.Contains(line, "Exact owner-confirmed `claude-opus-5` only") || strings.Contains(line, "claude-opus-4-8")) {
			t.Errorf("operator-assisted Claude adoption row does not retain exact Opus 5: %s", line)
		}
	}
}

func TestClaudeOpusOrbExecutorRecipeStaysProviderSpecificAndBounded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	recipe := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-opus-orb-executor.md"))

	for _, required := range []string{
		"CLAUDE_CODE_OAUTH_TOKEN",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_API_KEY",
		"CLAUDE_CODE_USE_ANTHROPIC_AWS",
		"CLAUDE_CODE_USE_MANTLE",
		"CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST",
		"loggedIn: true",
		"authMethod: oauth_token",
		"apiProvider: firstParty",
		"claude-opus-4-8",
		"CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS=64",
		"CLAUDE_CODE_SKIP_PROMPT_HISTORY=1",
		"CLAUDE_CODE_SAFE_MODE=1",
		"ulimit -f 64",
		"cd -- \"$WORK_DIR\"",
		`--tools ""`,
		"--safe-mode",
		"--disable-slash-commands",
		"--strict-mcp-config",
		`--mcp-config '{"mcpServers":{}}'`,
		"--permission-mode dontAsk",
		"--no-session-persistence",
		"--fallback-model",
		"modelUsage",
		"send_message_to_thread",
		"upload_thread_file",
		"nominal pricing telemetry",
		"do **not** prove API-key billing",
		"provider-neutral task state",
		"does not remove or revoke the Amp project secret",
		`if models != {"claude-opus-4-8"}`,
		"if safe != expected",
		"READ_ONLY_ARGS=(",
		`--allowedTools "Read,Grep,Glob"`,
		"permission_denials",
		"is_error",
		"config_session_persistence",
		"repository_changed",
	} {
		if !strings.Contains(recipe, required) {
			t.Errorf("Claude Opus Orb executor recipe is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/Users/",
		"/home/",
		"sk-ant-",
		"T-019f",
		"amux claude",
		"claude-fable-5",
		"--prompt-suggestions",
	} {
		if strings.Contains(recipe, forbidden) {
			t.Errorf("Claude Opus Orb executor recipe contains forbidden marker %q", forbidden)
		}
	}
	invocationStart := strings.Index(recipe, "exec timeout --signal=TERM --kill-after=5s 120s")
	if invocationStart < 0 {
		t.Fatal("Claude Opus Orb executor invocation fence is missing")
	}
	invocationEnd := strings.Index(recipe[invocationStart:], ") >\"$STDOUT_FILE\"")
	if invocationEnd < 0 {
		t.Fatal("Claude Opus Orb executor invocation fence is unterminated")
	}
	invocation := recipe[invocationStart : invocationStart+invocationEnd]
	if strings.Contains(invocation, "--fallback-model") {
		t.Error("Claude Opus Orb executor invocation enables a fallback model")
	}
	if !strings.Contains(invocation, `"${TOOL_ARGS[@]}"`) {
		t.Error("Claude Opus Orb executor invocation does not select one complete tool profile")
	}

	authStart := strings.Index(recipe, `AUTH_STDOUT="$RUN_ROOT/auth.json"`)
	authEnd := strings.Index(recipe, `) >"$AUTH_STDOUT" 2>"$AUTH_STDERR"`)
	if authStart < 0 || authEnd <= authStart {
		t.Fatal("Claude Opus Orb executor auth command is missing")
	}
	authCommand := recipe[authStart:authEnd]
	for _, required := range []string{`cd -- "$WORK_DIR"`, "CLAUDE_CODE_SAFE_MODE=1", `"$CLAUDE" auth status`} {
		if !strings.Contains(authCommand, required) {
			t.Errorf("Claude Opus Orb executor auth command is missing %q", required)
		}
	}

	profileStart := strings.Index(recipe, `NO_TOOL_ARGS=(--tools "" --disallowedTools "*")`)
	profileEnd := strings.Index(recipe, `TOOL_ARGS=("${NO_TOOL_ARGS[@]}")`)
	if profileStart < 0 || profileEnd <= profileStart {
		t.Fatal("Claude Opus Orb executor tool profiles are missing")
	}
	profiles := recipe[profileStart:profileEnd]
	readStart := strings.Index(profiles, "READ_ONLY_ARGS=(")
	if readStart < 0 {
		t.Fatal("Claude Opus Orb executor read-only profile is missing")
	}
	readProfile := profiles[readStart:]
	if strings.Contains(readProfile, `--disallowedTools "*"`) {
		t.Error("Claude Opus Orb executor read-only profile is overridden by deny-all")
	}
	for _, denied := range []string{"Bash", "Edit", "Write", "NotebookEdit", "Agent", "WebFetch", "WebSearch", "mcp__*"} {
		if !strings.Contains(readProfile, denied) {
			t.Errorf("Claude Opus Orb executor read-only profile does not deny %q", denied)
		}
	}

	for index, match := range regexp.MustCompile("(?s)```sh\\n(.*?)\\n```").FindAllStringSubmatch(recipe, -1) {
		command := exec.Command("bash", "-n", "-c", match[1])
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("Claude Opus Orb executor shell fence %d is invalid: %v\n%s", index+1, err, output)
		}
	}

	validatorRegionStart := strings.Index(recipe, "<!-- claude-opus-result-validator:start -->")
	validatorRegionEnd := strings.Index(recipe, "<!-- claude-opus-result-validator:end -->")
	if validatorRegionStart < 0 || validatorRegionEnd <= validatorRegionStart {
		t.Fatal("Claude Opus Orb executor result validator markers are missing")
	}
	validatorRegion := recipe[validatorRegionStart:validatorRegionEnd]
	pythonStartMarker := `<<'PY'` + "\n"
	pythonStart := strings.Index(validatorRegion, pythonStartMarker)
	if pythonStart < 0 {
		t.Fatal("Claude Opus Orb executor result validator Python start is missing")
	}
	pythonStart += len(pythonStartMarker)
	pythonEnd := strings.Index(validatorRegion[pythonStart:], "\nPY\n")
	if pythonEnd < 0 {
		t.Fatal("Claude Opus Orb executor result validator Python end is missing")
	}
	validator := validatorRegion[pythonStart : pythonStart+pythonEnd]
	validModelMetadata := `{"inputTokens":2,"outputTokens":3,"cacheReadInputTokens":0,"cacheCreationInputTokens":0,"webSearchRequests":0,"costUSD":0.1,"contextWindow":1000,"maxOutputTokens":64}`
	validResult := `{"type":"result","subtype":"success","is_error":false,"result":"MARKER","num_turns":1,"permission_denials":[],"usage":{"input_tokens":2,"output_tokens":3,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"modelUsage":{"claude-opus-4-8":` + validModelMetadata + `},"total_cost_usd":0.1,"duration_ms":10,"duration_api_ms":9}`
	tests := []struct {
		name        string
		result      string
		status      string
		stdoutBytes string
		stderrBytes string
		wantSuccess bool
	}{
		{name: "valid", result: validResult, status: "0", stdoutBytes: "1024", stderrBytes: "0", wantSuccess: true},
		{name: "nonzero status", result: validResult, status: "1", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "stdout overflow", result: validResult, status: "0", stdoutBytes: "65537", stderrBytes: "0"},
		{name: "stderr", result: validResult, status: "0", stdoutBytes: "1024", stderrBytes: "1"},
		{name: "error result", result: strings.Replace(validResult, `"is_error":false`, `"is_error":true`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "turn type", result: strings.Replace(validResult, `"num_turns":1`, `"num_turns":"1"`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "permission denial", result: strings.Replace(validResult, `"permission_denials":[]`, `"permission_denials":["Read"]`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "auxiliary model", result: strings.Replace(validResult, `"claude-opus-4-8":{`, `"claude-haiku-4-5":{},"claude-opus-4-8":{`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "empty model metadata", result: strings.Replace(validResult, validModelMetadata, `{}`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "missing model metadata", result: strings.Replace(validResult, `,"maxOutputTokens":64`, ``, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "usage type", result: strings.Replace(validResult, `"input_tokens":2`, `"input_tokens":"2"`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
		{name: "cost type", result: strings.Replace(validResult, `"total_cost_usd":0.1`, `"total_cost_usd":"0.1"`, 1), status: "0", stdoutBytes: "1024", stderrBytes: "0"},
	}
	for _, test := range tests {
		t.Run("validator "+test.name, func(t *testing.T) {
			fixture := filepath.Join(t.TempDir(), "result.json")
			if err := os.WriteFile(fixture, []byte(test.result), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("python3", "-c", validator, fixture, test.status, test.stdoutBytes, test.stderrBytes, "1", "MARKER")
			err := command.Run()
			if test.wantSuccess && err != nil {
				t.Errorf("valid result rejected: %v", err)
			}
			if !test.wantSuccess && err == nil {
				t.Error("invalid result accepted")
			}
		})
	}

	persistenceRegionStart := strings.Index(recipe, "<!-- claude-opus-persistence-validator:start -->")
	persistenceRegionEnd := strings.Index(recipe, "<!-- claude-opus-persistence-validator:end -->")
	if persistenceRegionStart < 0 || persistenceRegionEnd <= persistenceRegionStart {
		t.Fatal("Claude Opus Orb executor persistence validator markers are missing")
	}
	persistenceRegion := recipe[persistenceRegionStart:persistenceRegionEnd]
	persistenceMatch := regexp.MustCompile("(?s)```sh\\n(.*?)\\n```").FindStringSubmatch(persistenceRegion)
	if len(persistenceMatch) != 2 {
		t.Fatal("Claude Opus Orb executor persistence validator shell is missing")
	}
	persistenceValidator := persistenceMatch[1]
	persistenceTests := []struct {
		name         string
		createConfig bool
		setup        func(t *testing.T, workdir, configdir string)
		wantSuccess  bool
	}{
		{name: "empty", createConfig: true, wantSuccess: true},
		{name: "missing config"},
		{name: "work entry", createConfig: true, setup: func(t *testing.T, workdir, _ string) {
			if err := os.WriteFile(filepath.Join(workdir, "unexpected"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "forbidden config", createConfig: true, setup: func(t *testing.T, _, configdir string) {
			directory := filepath.Join(configdir, "sessions")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "state.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "config overflow", createConfig: true, setup: func(t *testing.T, _, configdir string) {
			if err := os.WriteFile(filepath.Join(configdir, "oversized.json"), make([]byte, 262145), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range persistenceTests {
		t.Run("persistence "+test.name, func(t *testing.T) {
			root := t.TempDir()
			workdir := filepath.Join(root, "work")
			configdir := filepath.Join(root, "config")
			if err := os.Mkdir(workdir, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.createConfig {
				if err := os.Mkdir(configdir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if test.setup != nil {
				test.setup(t, workdir, configdir)
			}
			command := exec.Command("bash", "-c", persistenceValidator)
			command.Env = append(os.Environ(),
				"TOOL_PROFILE=no-tool",
				"RUN_ROOT="+root,
				"WORK_DIR="+workdir,
				"CONFIG_DIR="+configdir,
			)
			err := command.Run()
			if test.wantSuccess && err != nil {
				t.Errorf("valid persistence state rejected: %v", err)
			}
			if !test.wantSuccess && err == nil {
				t.Error("invalid persistence state accepted")
			}
		})
	}

	reportAt := strings.Index(recipe, "send_message_to_thread")
	cleanupAt := strings.Index(recipe, `rm -f -- "$AUTH_STDOUT"`)
	if reportAt < 0 || cleanupAt < 0 || reportAt >= cleanupAt {
		t.Error("Claude Opus Orb executor does not order native report before cleanup")
	}
}

func TestDocumentedCommandTreeMatchesCLIHelp(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	checks := []struct {
		args []string
		want []string
	}{
		{args: []string{"help"}, want: []string{"launch", "list", "park", "restart", "remove", "doctor", "reconcile", "worker", "runner", "workspace", "workspaces", "group", "callback", "report", "spawn", "shelve", "unshelve", "teardown"}},
		{args: []string{"help", "worker", "pin"}, want: []string{"--workspace, -w", "--window, -W", "--workdir, -d", "--thread, -t", "--current"}},
		{args: []string{"help", "runner", "pin"}, want: []string{"--workspace, -w", "--workdir, -d", "--current"}},
		{args: []string{"help", "workspace", "list"}, want: []string{"--mode, -m <worker|runner>"}},
		{args: []string{"help", "group", "reconcile"}, want: []string{"--group <id>", "--thread, -t <id>", "--all"}},
		{args: []string{"help", "callback", "register"}, want: []string{"--group <id>", "--thread, -t <id>", "--pane <id>"}},
		{args: []string{"help", "report", "submit"}, want: []string{"--report-id <id>", "--group <id>", "--thread, -t <id>", "--status <ready|blocked|merged>", "--issue <value>", "--reference <value>", "--pr <url>", "--summary <text>"}},
		{args: []string{"help", "report", "authorize-finish"}, want: []string{"--report-id <id>", "--thread, -t <coordinator-id>", "--reference <value>"}},
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

func TestCoordinatorDeadlinePolicyIsConsistent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, relativePath := range []string{
		"README.md",
		filepath.Join("skills", "amux", "reference", "deadline-v1.md"),
		filepath.Join("docs", "skill", "index.html"),
	} {
		contents := readSkillFile(t, root, relativePath)
		for _, required := range []string{"Small 30m", "Medium 1h", "Large 2h", "XL", "15m", "review", "10m", "external CI", "20m", "finish", "half the original budget", "new generation", "diagnostic", "nearest-deadline queue", "timer process per child"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing deadline policy %q", relativePath, required)
			}
		}
	}
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{"Small 30m", "Medium 1h", "Large 2h", "XL", "15m", "10m", "20m", "nearest-deadline queue", "deadline-v1.md"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("workflows.md is missing deadline summary %q", required)
		}
	}
}

func TestCoordinatorDeadlineScheduleIsBoundedAndGenerationSafe(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	deadline := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "deadline-v1.md"))

	if !strings.Contains(skill, "deadline-v1") {
		t.Error("SKILL.md is missing deadline progressive disclosure pointer")
	}
	if !strings.Contains(workflow, "deadline-v1.md") {
		t.Error("workflows.md does not point at deadline-v1.md")
	}
	if !strings.Contains(deadline, "Do **not** load the full `/amux` skill on schedule fire") {
		t.Error("deadline-v1 must forbid full /amux reload on schedule fire")
	}
	if strings.Contains(deadline, "Load `/amux` first") {
		t.Error("deadline-v1 must not require loading full /amux")
	}

	for _, required := range []string{
		"building-automations",
		"get_schedule",
		"set_schedule",
		"update_schedule",
		"clear_schedule",
		"call no schedule tool",
		"at most one schedule or trigger",
		"AMUX_DEADLINE_QUEUE_V1",
		"owner=AMUX_DEADLINE_QUEUE_V1",
		"DTSTART:<utc-basic-deadline>Z",
		"RRULE:FREQ=DAILY;COUNT=1",
		"2030-01-02T03:04:00+02:00",
		"DTSTART:20300102T010400Z",
		"exactly one next/only occurrence equal to the authoritative RFC3339 instant",
		"Repeat this inspection after every re-arm",
		"ambiguous, conflicting, or unverifiable",
		"amux report pending --group <durable-issue-group>",
		"amux report history --report-id <stable-report-id>",
		"order-independent authoritative re-reads",
		"correctness must not depend on their arrival order",
		"stale/late and duplicate",
		"it was superseded",
		"a current `blocked`, `ready`, or `merged` report",
		"acknowledgement before expiry",
		"AMUX_DEADLINE_STOP_ATTEMPT_V1",
		"before native member messaging",
		"consumes that generation even if the send is indeterminate",
		"excluding satisfied, superseded, acknowledged-before-expiry, and stop-attempt-recorded generations",
		"do not retry it blindly",
		"freshly verified UTC `DTSTART`/`COUNT=1` schedule",
		"unrelated schedule",
		"manual wake-up",
		"never create sleeping shell processes, recurring polling schedules, or per-worker supervisors",
		"never authorize acknowledgement, push, PR creation or mutation, merge, release, finish, cleanup",
	} {
		if !strings.Contains(deadline, required) {
			t.Errorf("deadline-v1 is missing schedule safety contract %q", required)
		}
	}
	skillLoadAt := strings.Index(deadline, "load the available `building-automations` skill")
	if skillLoadAt < 0 {
		t.Fatal("deadline-v1 does not load building-automations")
	}
	for _, toolName := range []string{"`get_schedule`", "`set_schedule`", "`update_schedule`", "`clear_schedule`"} {
		toolAt := strings.Index(deadline, toolName)
		if toolAt < 0 || toolAt <= skillLoadAt {
			t.Errorf("deadline-v1 does not order building-automations load before %s management: load=%d tool=%d", toolName, skillLoadAt, toolAt)
		}
	}

	fixtureStart := strings.Index(deadline, "Follow deadline-v1 (do not load full /amux)")
	if fixtureStart < 0 {
		t.Fatal("synthetic scheduled wake-up fixture is missing")
	}
	fixtureEnd := strings.Index(deadline[fixtureStart:], "\n```")
	if fixtureEnd < 0 {
		t.Fatal("synthetic scheduled wake-up fixture is unterminated")
	}
	fixture := deadline[fixtureStart : fixtureStart+fixtureEnd]
	for _, required := range []string{
		"load `building-automations`",
		"owner=AMUX_DEADLINE_QUEUE_V1",
		"group=<durable-issue-group>",
		"report=<stable-report-id>",
		"member=<member-thread>",
		"generation=<deadline-generation>",
		"deadline=<rfc3339-deadline>",
		"AMUX_DEADLINE_STOP_ATTEMPT_V1 group=<durable-issue-group> report=<stable-report-id> member=<member-thread> generation=<deadline-generation> deadline=<rfc3339-deadline>",
		"consumes this generation even if native messaging is indeterminate",
		"preserve work and evidence",
		"stop implementation and review loops",
		"submit this stable report as `blocked` with the exact remaining blocker and `--pr none` when no PR exists",
		"remain alive",
	} {
		if !strings.Contains(fixture, required) {
			t.Errorf("synthetic scheduled wake-up fixture is missing %q", required)
		}
	}
	for name, forbidden := range map[string]*regexp.Regexp{
		"real thread ID":  regexp.MustCompile(`\bT-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
		"pane identifier": regexp.MustCompile(`(?:^|[[:space:]=])%[0-9]+\b`),
		"private path":    regexp.MustCompile(`(?:/Users/|/home/|[A-Za-z]:\\Users\\)`),
		"private field":   regexp.MustCompile(`(?i)\b(?:pane|pid|process|prompt|packet|transcript|receipt|account|secret)[[:space:]]*=`),
	} {
		if forbidden.MatchString(fixture) {
			t.Errorf("synthetic scheduled wake-up fixture contains %s", name)
		}
	}
	recordAt := strings.Index(fixture, "AMUX_DEADLINE_STOP_ATTEMPT_V1")
	sendAt := strings.Index(fixture, "preserve work and evidence")
	nextAt := strings.Index(fixture, "nearest unhandled active generation")
	if recordAt < 0 || sendAt < 0 || nextAt < 0 || recordAt >= sendAt || sendAt >= nextAt {
		t.Errorf("synthetic scheduled wake-up fixture does not order record, bounded send, and next-unhandled reconciliation: record=%d send=%d next=%d", recordAt, sendAt, nextAt)
	}
}

func TestCoordinatorSafetyAppearsInPublicReferences(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, relativePath := range []string{
		"README.md",
		filepath.Join("skills", "amux", "SKILL.md"),
		filepath.Join("skills", "amux", "reference", "workflows.md"),
		filepath.Join("skills", "amux", "reference", "troubleshooting.md"),
		filepath.Join("docs", "skill", "index.html"),
	} {
		contents := readSkillFile(t, root, relativePath)
		for _, required := range []string{"force-delete", "auto-release", "history"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing coordinator safety term %q", relativePath, required)
			}
		}
	}
}

func TestBoundedSpawnExceptionCommandsUseExplicitMode(t *testing.T) {
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
			if !strings.Contains(command, "--mode ") && !strings.Contains(command, "--mode=") {
				t.Errorf("%s:%d spawn example omits explicit --mode: %s", relativePath, lineNumber, strings.TrimSpace(line))
			}
		})
	}

	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	for _, required := range []string{"Every native creation and bounded exception uses an explicit mode", "linked ChatGPT subscription", "target-mode availability", "small mechanical work", "ordinary implementation", "hard architecture", "An explicitly requested mode always wins", "special modes remain explicit-only"} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md is missing native creation mode policy %q", required)
		}
	}
}

func TestSprawlUsesDedicatedNativeIssueThreads(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	sprawlAt := strings.Index(workflow, "## Sprawl independent issue threads")
	coordinateAt := strings.Index(workflow, "## Coordinate native child threads and drain a durable work group")
	if sprawlAt < 0 || coordinateAt <= sprawlAt {
		t.Fatal("sprawl workflow section is missing")
	}
	sprawl := workflow[sprawlAt:coordinateAt]
	for _, required := range []string{
		"native dependency",
		"one dedicated branch/worktree",
		"issue-unprefixed semantic title",
		"task-only assignment",
		"authenticated native-created thread",
		"exact selected live runner",
		"create no Amux lifecycle representation",
	} {
		if !strings.Contains(sprawl, required) {
			t.Errorf("sprawl workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--window issue-<issue>", "--window #<issue>", "runner spawn", "worker adopt"} {
		if strings.Contains(sprawl, forbidden) {
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

func TestFinishRemovalGateDocumentsEveryFailClosedInvariant(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	reference := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "removal-safety.md"))
	combined := workflow + reference
	for _, required := range []string{
		"override`, `origin/HEAD`, or `GitHub default branch`",
		"refs/remotes/origin/<name>",
		"Run `git fetch --prune origin` before classification",
		"Plain `git fetch origin` is insufficient",
		"refs/heads refs/remotes refs/tags",
		"locked` with `stat` proving the path absent",
		"dirty: unknowable",
		"status --porcelain --untracked-files=no",
		"SAFE_KEEP_BRANCH",
		"git cherry <resolved-remote-baseline> <C>",
		"Rule 5 is always `NEEDS_BACKUP`",
		"create refs/heads/backup/<worktree-name>-before-remove-<date> at <C>",
		"classify → backup → unlock → prune",
		"without force only when that immediately preceding revalidation",
		"Keep branch deletion separate from worktree removal",
		"gh pr view <pr> --json state,mergedAt,headRefName,headRefOid",
		"unfiltered count and paths of `??` untracked entries",
		"symlink-to-external",
		"duplicate-of-canonical",
		"Generated-artifact exclusions are configurable presentation filters",
		"never count `refs/stash` as commit coverage",
		"Any scan or resolution error blocks ordinary removal",
		"no earlier verdict survives this step",
		"Revalidate adjacent to targeted removal",
		"Never reuse rule-2a evidence",
		"A single-target verdict never authorizes `git worktree prune`",
		"exact set of prunable paths",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("finish removal gate is missing %q", required)
		}
	}
	for _, required := range []string{
		"`git fetch --prune origin`",
		"Plain `git fetch origin` is insufficient",
		"A deleted upstream ref must not satisfy rule 2a",
		"workflows.md#removal-preflight-for-finish-remove-on-missing-and-prune",
		"refs/remotes/origin/<name>",
	} {
		if !strings.Contains(reference, required) {
			t.Errorf("direct classifier entry path is missing %q", required)
		}
	}
	for _, required := range []string{
		"freshly re-derive rules 2a–5 for every authorized row",
		"never compare or reuse a stale verdict",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("repository-wide prune revalidation is missing %q", required)
		}
	}
	mutation := strings.Index(workflow, "[Remove the worker worktree without force")
	link := strings.Index(workflow, "](removal-safety.md#removal-ordering-context)")
	if mutation < 0 || link < mutation {
		t.Error("actual finish mutation sentence lacks inline progressive-disclosure link")
	}
	pull := strings.Index(workflow, "git pull --ff-only` before classification")
	preflight := strings.Index(workflow, "Run the removal preflight below against the exact worker worktree")
	adjacent := strings.Index(workflow, "Then perform the adjacent revalidation below")
	if pull < 0 || preflight <= pull || adjacent <= preflight || mutation <= adjacent {
		t.Errorf("finish ordering must be pull → classify → adjacent revalidation → remove: pull=%d preflight=%d adjacent=%d remove=%d", pull, preflight, adjacent, mutation)
	}
}

func TestBackupRemovalRefsContractIsNarrowAndFailClosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	reference := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "removal-safety.md"))
	for _, required := range []string{
		"classify → backup → unlock → prune",
		"one `git update-ref --stdin` transaction",
		"absent direct ref is created at the exact tip",
		"already at that tip is verified as an idempotent no-op",
		"abort",
		"--no-backup",
		"removal_authorized` is always false",
		"Human output ends with the exact JSON facts envelope",
		"Restart the complete preflight",
		"ignore only that expected exact ref",
		"including rows that need no backup",
		"never removes files or worktrees, unlocks, prunes, deletes branches",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("backup workflow is missing %q", required)
		}
	}
	for _, required := range []string{"verified durable branch", "complete-set backup procedure", "never authorizes or performs removal"} {
		if !strings.Contains(reference, required) {
			t.Errorf("removal-safety reference is missing backup contract %q", required)
		}
	}
}

func TestBackupRemovalRefsDryRunApplyIdempotencyAndParity(t *testing.T) {
	repo, _, baseline := newRemovalSafetyRepo(t)
	worktree := filepath.Join(t.TempDir(), "unique-detached")
	gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
	commitFile(t, worktree, "unique.txt", "unique\n", "unique detached")
	manifest, backupRef, tip := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})

	dry := runBackupHelper(t, manifest, "--dry-run", "--json")
	if dry["dry_run"] != true || dry["backup_satisfied"] != false || dry["removal_authorized"] != false {
		t.Fatalf("dry-run envelope = %#v", dry)
	}
	rows := dry["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["backup_state"] != "planned" || rows[0].(map[string]any)["backup_ref"] != backupRef {
		t.Fatalf("dry-run rows = %#v", rows)
	}
	if got := gitTestOptionalRef(t, repo, backupRef); got != "" {
		t.Fatalf("dry-run created backup ref %s at %s", backupRef, got)
	}

	humanCommand := exec.Command("python3", backupHelperPath(t), "--manifest", manifest, "--dry-run")
	humanOutput, err := humanCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("human dry-run: %v\n%s", err, humanOutput)
	}
	var humanFacts map[string]any
	for _, line := range strings.Split(string(humanOutput), "\n") {
		if strings.HasPrefix(line, "facts ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "facts ")), &humanFacts); err != nil {
				t.Fatal(err)
			}
		}
	}
	if humanFacts == nil || fmt.Sprint(humanFacts) != fmt.Sprint(dry) {
		t.Fatalf("human/JSON facts differ\nhuman=%#v\njson=%#v", humanFacts, dry)
	}

	created := runBackupHelper(t, manifest, "--json")
	if created["removal_authorized"] != false || created["rows"].([]any)[0].(map[string]any)["backup_state"] != "created" {
		t.Fatalf("created envelope = %#v", created)
	}
	if got := gitTestOptionalRef(t, repo, backupRef); got != tip {
		t.Fatalf("backup ref = %q, want %q", got, tip)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("helper deleted or changed worktree directory: %v", err)
	}

	satisfied := runBackupHelper(t, manifest, "--json")
	if satisfied["rows"].([]any)[0].(map[string]any)["backup_state"] != "already_satisfied" {
		t.Fatalf("idempotent envelope = %#v", satisfied)
	}
}

func TestBackupRemovalRefsDeclineConflictAndDriftFailClosed(t *testing.T) {
	t.Run("explicit no-backup blocks without mutation", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "declined")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "declined.txt", "declined\n", "declined")
		manifest, backupRef, _ := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		document := runBackupHelperExit(t, 2, manifest, "--no-backup", "--json")
		if document["backup_satisfied"] != false || document["removal_authorized"] != false {
			t.Fatalf("declined envelope = %#v", document)
		}
		if got := gitTestOptionalRef(t, repo, backupRef); got != "" {
			t.Fatalf("--no-backup created %s at %s", backupRef, got)
		}
	})

	t.Run("conflicting ref aborts complete set", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		first := filepath.Join(t.TempDir(), "first")
		second := filepath.Join(t.TempDir(), "second")
		gitTest(t, repo, "worktree", "add", "--detach", first, baseline)
		gitTest(t, repo, "worktree", "add", "--detach", second, baseline)
		commitFile(t, first, "first.txt", "first\n", "first")
		commitFile(t, second, "second.txt", "second\n", "second")
		first = strings.TrimSpace(gitTest(t, first, "rev-parse", "--show-toplevel"))
		second = strings.TrimSpace(gitTest(t, second, "rev-parse", "--show-toplevel"))
		firstTip := strings.TrimSpace(gitTest(t, first, "rev-parse", "HEAD"))
		secondTip := strings.TrimSpace(gitTest(t, second, "rev-parse", "HEAD"))
		if err := os.RemoveAll(first); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(second); err != nil {
			t.Fatal(err)
		}
		manifest, _, _ := writeBackupManifest(t, repo, baseline, "prune", []backupManifestTarget{
			{path: first, tip: firstTip, pathState: "absent", prunable: true},
			{path: second, tip: secondTip, pathState: "absent", prunable: true},
		})
		var parsed map[string]any
		contents, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(contents, &parsed); err != nil {
			t.Fatal(err)
		}
		rows := parsed["rows"].([]any)
		firstRef := rows[0].(map[string]any)["backup_ref"].(string)
		secondRef := rows[1].(map[string]any)["backup_ref"].(string)
		gitTest(t, repo, "branch", strings.TrimPrefix(secondRef, "refs/heads/"), baseline)
		document := runBackupHelperExit(t, 2, manifest, "--json")
		if !strings.Contains(document["error"].(string), "different commit") {
			t.Fatalf("conflict error = %#v", document)
		}
		if got := gitTestOptionalRef(t, repo, firstRef); got != "" {
			t.Fatalf("complete-set failure partially created %s at %s", firstRef, got)
		}
	})

	t.Run("target drift rejects stale evidence", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "drift")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "before.txt", "before\n", "before")
		manifest, backupRef, _ := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		commitFile(t, worktree, "after.txt", "after\n", "after")
		document := runBackupHelperExit(t, 2, manifest, "--json")
		if !strings.Contains(document["error"].(string), "identity changed") {
			t.Fatalf("drift error = %#v", document)
		}
		if got := gitTestOptionalRef(t, repo, backupRef); got != "" {
			t.Fatalf("drifted target created stale backup %s at %s", backupRef, got)
		}
	})

	t.Run("symbolic backup ref is never durable coverage", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "symbolic")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "symbolic.txt", "symbolic\n", "symbolic")
		manifest, backupRef, tip := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		gitTest(t, repo, "update-ref", "refs/meta/ephemeral", tip)
		gitTest(t, repo, "symbolic-ref", backupRef, "refs/meta/ephemeral")
		document := runBackupHelperExit(t, 2, manifest, "--dry-run", "--json")
		if !strings.Contains(document["error"].(string), "symbolic") || document["removal_authorized"] != false {
			t.Fatalf("symbolic ref result = %#v", document)
		}
	})
}

func TestBackupRemovalRefsPruneRequiresCompleteSetAndRejectsUnsafePaths(t *testing.T) {
	t.Run("malformed duplicate manifest key", func(t *testing.T) {
		manifest := filepath.Join(t.TempDir(), "duplicate.json")
		if err := os.WriteFile(manifest, []byte(`{"schema_version":1,"schema_version":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
		document := runBackupHelperExit(t, 2, manifest, "--json")
		if !strings.Contains(document["error"].(string), "duplicate JSON key") || document["removal_authorized"] != false {
			t.Fatalf("malformed manifest result = %#v", document)
		}
	})

	t.Run("malformed types and ownership stay structured", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "malformed")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "malformed.txt", "malformed\n", "malformed")
		manifest, _, _ := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		mutateBackupManifest(t, manifest, func(document map[string]any) {
			document["baseline"].(map[string]any)["source"] = []any{"origin/HEAD"}
		})
		document := runBackupHelperExit(t, 2, manifest, "--dry-run", "--json")
		if !strings.Contains(document["error"].(string), "baseline source") || document["removal_authorized"] != false {
			t.Fatalf("malformed type result = %#v", document)
		}

		manifest, _, _ = writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		mutateBackupManifest(t, manifest, func(document map[string]any) {
			document["rows"].([]any)[0].(map[string]any)["ownership_evidence"] = map[string]any{"garbage": true}
		})
		document = runBackupHelperExit(t, 2, manifest, "--dry-run", "--json")
		if !strings.Contains(document["error"].(string), "ownership_evidence") || document["removal_authorized"] != false {
			t.Fatalf("malformed ownership result = %#v", document)
		}
	})

	t.Run("duplicate backup names reject truthful dry-run", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		first := filepath.Join(t.TempDir(), "same")
		second := filepath.Join(t.TempDir(), "same")
		gitTest(t, repo, "worktree", "add", "--detach", first, baseline)
		gitTest(t, repo, "worktree", "add", "--detach", second, baseline)
		commitFile(t, first, "first.txt", "first\n", "first")
		commitFile(t, second, "second.txt", "second\n", "second")
		first = strings.TrimSpace(gitTest(t, first, "rev-parse", "--show-toplevel"))
		second = strings.TrimSpace(gitTest(t, second, "rev-parse", "--show-toplevel"))
		firstTip := strings.TrimSpace(gitTest(t, first, "rev-parse", "HEAD"))
		secondTip := strings.TrimSpace(gitTest(t, second, "rev-parse", "HEAD"))
		if err := os.RemoveAll(first); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(second); err != nil {
			t.Fatal(err)
		}
		manifest, backupRef, _ := writeBackupManifest(t, repo, baseline, "prune", []backupManifestTarget{
			{path: first, tip: firstTip, pathState: "absent", prunable: true},
			{path: second, tip: secondTip, pathState: "absent", prunable: true},
		})
		document := runBackupHelperExit(t, 2, manifest, "--dry-run", "--json")
		if !strings.Contains(document["error"].(string), "duplicate backup ref") || document["removal_authorized"] != false {
			t.Fatalf("duplicate backup result = %#v", document)
		}
		if got := gitTestOptionalRef(t, repo, backupRef); got != "" {
			t.Fatalf("duplicate dry-run created %s at %s", backupRef, got)
		}
	})

	t.Run("omitted prune row", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		firstRoot := t.TempDir()
		secondRoot := t.TempDir()
		first := filepath.Join(firstRoot, "first-missing")
		second := filepath.Join(secondRoot, "second-missing")
		gitTest(t, repo, "worktree", "add", "--detach", first, baseline)
		gitTest(t, repo, "worktree", "add", "--detach", second, baseline)
		commitFile(t, first, "first.txt", "first\n", "first")
		commitFile(t, second, "second.txt", "second\n", "second")
		first = strings.TrimSpace(gitTest(t, first, "rev-parse", "--show-toplevel"))
		second = strings.TrimSpace(gitTest(t, second, "rev-parse", "--show-toplevel"))
		firstTip := strings.TrimSpace(gitTest(t, first, "rev-parse", "HEAD"))
		if err := os.RemoveAll(first); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(second); err != nil {
			t.Fatal(err)
		}
		manifest, backupRef, _ := writeBackupManifest(t, repo, baseline, "prune", []backupManifestTarget{{path: first, tip: firstTip, pathState: "absent", prunable: true}})
		document := runBackupHelperExit(t, 2, manifest, "--json")
		if !strings.Contains(document["error"].(string), "complete current") {
			t.Fatalf("incomplete prune error = %#v", document)
		}
		if got := gitTestOptionalRef(t, repo, backupRef); got != "" {
			t.Fatalf("incomplete prune created %s at %s", backupRef, got)
		}
	})

	t.Run("repository symlink escape", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "escaped")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "escape.txt", "escape\n", "escape")
		manifest, backupRef, _ := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		contents, err := os.ReadFile(manifest)
		if err != nil {
			t.Fatal(err)
		}
		var document map[string]any
		if err := json.Unmarshal(contents, &document); err != nil {
			t.Fatal(err)
		}
		repositoryLink := filepath.Join(t.TempDir(), "repo-link")
		if err := os.Symlink(document["repository"].(string), repositoryLink); err != nil {
			t.Fatal(err)
		}
		document["repository"] = repositoryLink
		contents, err = json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		result := runBackupHelperExit(t, 2, manifest, "--json")
		if !strings.Contains(result["error"].(string), "symlink") {
			t.Fatalf("path escape error = %#v", result)
		}
		if got := gitTestOptionalRef(t, repo, backupRef); got != "" {
			t.Fatalf("unsafe path created %s at %s", backupRef, got)
		}
	})

	t.Run("GitHub fallback is bound to target origin", func(t *testing.T) {
		repo, remote, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "github-fallback")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "github.txt", "github\n", "github fallback")
		manifest, _, _ := writeBackupManifest(t, repo, baseline, "targeted_remove", []backupManifestTarget{{path: worktree}})
		mutateBackupManifest(t, manifest, func(document map[string]any) {
			document["baseline"].(map[string]any)["source"] = "GitHub default branch"
		})
		githubURL := "https://github.com/acme/example.git"
		gitTest(t, repo, "config", "url."+remote+".insteadOf", githubURL)
		gitTest(t, repo, "remote", "set-url", "origin", githubURL)
		fakeBin := t.TempDir()
		argsPath := filepath.Join(t.TempDir(), "gh-args")
		fakeGH := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\nprintf '%s\\n' '{\"defaultBranchRef\":{\"name\":\"main\"}}'\n"
		if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(fakeGH), 0o700); err != nil {
			t.Fatal(err)
		}
		document := runBackupHelperExitEnv(t, 0, append(os.Environ(), "PATH="+fakeBin+":"+os.Getenv("PATH")), manifest, "--dry-run", "--json")
		if document["removal_authorized"] != false {
			t.Fatalf("GitHub fallback envelope = %#v", document)
		}
		args, err := os.ReadFile(argsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(args), "--repo\nacme/example\n") {
			t.Fatalf("gh was not bound to target origin: %q", args)
		}
	})
}

func TestBackupRemovalRefsMixedSetAndMultiRefAtomicSuccess(t *testing.T) {
	repo, _, baseline := newRemovalSafetyRepo(t)
	first := filepath.Join(t.TempDir(), "first-unique")
	second := filepath.Join(t.TempDir(), "second-unique")
	safe := filepath.Join(t.TempDir(), "safe-baseline")
	gitTest(t, repo, "worktree", "add", "--detach", first, baseline)
	gitTest(t, repo, "worktree", "add", "--detach", second, baseline)
	gitTest(t, repo, "worktree", "add", "--detach", safe, baseline)
	commitFile(t, first, "first.txt", "first\n", "first")
	commitFile(t, second, "second.txt", "second\n", "second")
	first = strings.TrimSpace(gitTest(t, first, "rev-parse", "--show-toplevel"))
	second = strings.TrimSpace(gitTest(t, second, "rev-parse", "--show-toplevel"))
	safe = strings.TrimSpace(gitTest(t, safe, "rev-parse", "--show-toplevel"))
	firstTip := strings.TrimSpace(gitTest(t, first, "rev-parse", "HEAD"))
	secondTip := strings.TrimSpace(gitTest(t, second, "rev-parse", "HEAD"))
	safeTip := strings.TrimSpace(gitTest(t, safe, "rev-parse", "HEAD"))
	for _, path := range []string{first, second, safe} {
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	manifest, firstRef, _ := writeBackupManifest(t, repo, baseline, "prune", []backupManifestTarget{
		{path: first, tip: firstTip, pathState: "absent", prunable: true},
		{path: second, tip: secondTip, pathState: "absent", prunable: true},
		{path: safe, tip: safeTip, pathState: "absent", prunable: true},
	})
	var secondRef string
	safeRefs := strings.Fields(gitTest(t, repo, "for-each-ref", "--contains", safeTip, "--format=%(refname)", "refs/heads", "refs/remotes", "refs/tags"))
	sort.Strings(safeRefs)
	remoteSafeRefs := make([]string, 0, len(safeRefs))
	for _, ref := range safeRefs {
		if strings.HasPrefix(ref, "refs/remotes/") {
			remoteSafeRefs = append(remoteSafeRefs, ref)
		}
	}
	mutateBackupManifest(t, manifest, func(document map[string]any) {
		rows := document["rows"].([]any)
		secondRef = rows[1].(map[string]any)["backup_ref"].(string)
		safeRow := rows[2].(map[string]any)
		safeRow["verdict"] = "SAFE"
		safeRow["covering_refs"] = safeRefs
		safeRow["rule_evidence"] = map[string]any{"rule": "2a", "refs": remoteSafeRefs}
		safeRow["backup_ref"] = nil
		safeRow["date"] = nil
	})
	document := runBackupHelper(t, manifest, "--json")
	if document["backup_satisfied"] != true || document["removal_authorized"] != false {
		t.Fatalf("mixed complete-set envelope = %#v", document)
	}
	if got := gitTestOptionalRef(t, repo, firstRef); got != firstTip {
		t.Fatalf("first atomic ref = %q, want %q", got, firstTip)
	}
	if got := gitTestOptionalRef(t, repo, secondRef); got != secondTip {
		t.Fatalf("second atomic ref = %q, want %q", got, secondTip)
	}
}

func TestRemovalSafetySyntheticRefCoverageAndPatchEquivalence(t *testing.T) {
	t.Run("attached local branch", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "attached")
		gitTest(t, repo, "worktree", "add", "-b", "feature", worktree, baseline)
		commitFile(t, worktree, "feature.txt", "attached\n", "attached")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		verdict, evidence, err := syntheticRemovalVerdict(repo, baseline, tip)
		if err != nil || verdict != "SAFE_KEEP_BRANCH" || !strings.Contains(evidence, "refs/heads/feature") {
			t.Fatalf("attached verdict=(%q, %q, %v), want local branch coverage", verdict, evidence, err)
		}
	})

	t.Run("remote branch", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "remote")
		gitTest(t, repo, "worktree", "add", "-b", "feature", worktree, baseline)
		commitFile(t, worktree, "remote.txt", "remote\n", "remote")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		gitTest(t, worktree, "push", "-u", "origin", "feature")
		verdict, evidence, err := syntheticRemovalVerdict(repo, baseline, tip)
		if err != nil || verdict != "SAFE" || !strings.Contains(evidence, "refs/remotes/origin/feature") {
			t.Fatalf("remote verdict=(%q, %q, %v), want remote coverage", verdict, evidence, err)
		}
	})

	t.Run("stale remote coverage disappears after fetch", func(t *testing.T) {
		repo, remote, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "remote-pruned")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "remote-only.txt", "remote only\n", "remote only")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		gitTest(t, worktree, "push", "origin", "HEAD:refs/heads/transient")
		gitTest(t, repo, "fetch", "origin", "refs/heads/transient:refs/remotes/origin/transient")
		if verdict, evidence, err := syntheticRemovalVerdict(repo, baseline, tip); err != nil || verdict != "SAFE" || evidence != "refs/remotes/origin/transient" {
			t.Fatalf("pre-fetch verdict=(%q, %q, %v), want remote SAFE", verdict, evidence, err)
		}
		cmd := exec.Command("git", "--git-dir", remote, "update-ref", "-d", "refs/heads/transient")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("delete synthetic remote ref: %v\n%s", err, out)
		}
		gitTest(t, repo, "fetch", "origin")
		if verdict, evidence, err := syntheticRemovalVerdict(repo, baseline, tip); err != nil || verdict != "SAFE" || evidence != "refs/remotes/origin/transient" {
			t.Fatalf("plain-fetch verdict=(%q, %q, %v), want phantom remote SAFE evidence", verdict, evidence, err)
		}
		gitTest(t, repo, "fetch", "--prune", "origin")
		if verdict, _, err := syntheticRemovalVerdict(repo, baseline, tip); err != nil || verdict != "NEEDS_BACKUP" {
			t.Fatalf("post-fetch verdict=(%q, %v), want NEEDS_BACKUP", verdict, err)
		}
	})

	t.Run("tag and detached unique", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "detached")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "detached.txt", "unique\n", "detached")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		verdict, _, err := syntheticRemovalVerdict(repo, baseline, tip)
		if err != nil || verdict != "NEEDS_BACKUP" {
			t.Fatalf("detached verdict=(%q, %v), want NEEDS_BACKUP", verdict, err)
		}
		gitTest(t, repo, "tag", "rescue-detached", tip)
		verdict, evidence, err := syntheticRemovalVerdict(repo, baseline, tip)
		if err != nil || verdict != "SAFE_KEEP_BRANCH" || !strings.Contains(evidence, "refs/tags/rescue-detached") {
			t.Fatalf("tag verdict=(%q, %q, %v), want local tag coverage", verdict, evidence, err)
		}
	})

	t.Run("stash is not coverage", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "stash")
		gitTest(t, repo, "worktree", "add", "--detach", worktree, baseline)
		commitFile(t, worktree, "detached.txt", "unique\n", "detached")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		if err := os.WriteFile(filepath.Join(worktree, "detached.txt"), []byte("stashed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitTest(t, worktree, "stash", "push", "-m", "detached-wip")
		allRefs := gitTest(t, repo, "for-each-ref", "--contains", tip, "--format=%(refname)")
		allowedRefs := gitTest(t, repo, "for-each-ref", "--contains", tip, "--format=%(refname)", "refs/heads", "refs/remotes", "refs/tags")
		if !strings.Contains(allRefs, "refs/stash") || strings.Contains(allowedRefs, "refs/stash") {
			t.Fatalf("stash include-list evidence all=%q allowed=%q", allRefs, allowedRefs)
		}
		verdict, _, err := syntheticRemovalVerdict(repo, baseline, tip)
		if err != nil || verdict != "NEEDS_BACKUP" {
			t.Fatalf("stash verdict=(%q, %v), want NEEDS_BACKUP", verdict, err)
		}
	})

	t.Run("attached stash attribution", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "stash-attribution")
		gitTest(t, repo, "worktree", "add", "-b", "stash-feature", worktree, baseline)
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("stash me\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitTest(t, worktree, "stash", "push", "-m", "attributed-wip")
		stash := gitTest(t, repo, "stash", "list", "--format=%gd%x09%H%x09%P%x09%gs")
		for _, evidence := range []string{"stash@{0}", tip, "stash-feature", "attributed-wip"} {
			if !strings.Contains(stash, evidence) {
				t.Errorf("stash row lacks %q: %q", evidence, stash)
			}
		}
	})

	t.Run("one directional patch equivalence", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "patch")
		gitTest(t, repo, "worktree", "add", "-b", "patch-source", worktree, baseline)
		commitFile(t, worktree, "dist/generated.js", "same generated patch\n", "source change")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		patch := gitTestBytes(t, worktree, "show", "--format=", "--binary", tip)
		gitTestInput(t, repo, patch, "apply")
		gitTest(t, repo, "add", "dist/generated.js")
		gitTest(t, repo, "commit", "-m", "squash destination")
		baseline = "HEAD"
		gitTest(t, repo, "update-ref", "-d", "refs/heads/patch-source")
		cherry := gitTest(t, repo, "cherry", baseline, tip)
		if !strings.HasPrefix(strings.TrimSpace(cherry), "-") {
			t.Fatalf("git cherry = %q, want one-directional '-' evidence", cherry)
		}
		verdict, evidence, err := syntheticRemovalVerdict(repo, baseline, tip)
		if err != nil || verdict != "SAFE" || evidence != "patch-equivalent" {
			t.Fatalf("patch verdict=(%q, %q, %v), want SAFE", verdict, evidence, err)
		}
	})
}

func TestRemovalSafetySyntheticMissingDirtyAndPreciousFiles(t *testing.T) {
	t.Run("tracked only blocks", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "dirty")
		gitTest(t, repo, "worktree", "add", "-b", "dirty", worktree, baseline)
		if err := os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := syntheticTrackedState(worktree); err != nil || got != "clean" {
			t.Fatalf("untracked-only tracked state=(%q, %v), want clean", got, err)
		}
		if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := syntheticTrackedState(worktree); err != nil || got != "BLOCKED" {
			t.Fatalf("tracked state=(%q, %v), want BLOCKED", got, err)
		}
	})

	t.Run("locked and unlocked absent", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		locked := filepath.Join(t.TempDir(), "locked")
		plain := filepath.Join(t.TempDir(), "plain")
		gitTest(t, repo, "worktree", "add", "--detach", locked, baseline)
		gitTest(t, repo, "worktree", "lock", locked)
		gitTest(t, repo, "worktree", "add", "--detach", plain, baseline)
		lockedRegistered := strings.TrimSpace(gitTest(t, locked, "rev-parse", "--show-toplevel"))
		plainRegistered := strings.TrimSpace(gitTest(t, plain, "rev-parse", "--show-toplevel"))
		if err := os.RemoveAll(locked); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(plain); err != nil {
			t.Fatal(err)
		}
		porcelain := gitTest(t, repo, "worktree", "list", "--porcelain")
		lockedBlock := syntheticWorktreeBlock(porcelain, lockedRegistered)
		plainBlock := syntheticWorktreeBlock(porcelain, plainRegistered)
		if !strings.Contains(lockedBlock, "locked") || strings.Contains(lockedBlock, "prunable") {
			t.Fatalf("locked absent block = %q", lockedBlock)
		}
		if !strings.Contains(plainBlock, "prunable") {
			t.Fatalf("unlocked absent block = %q", plainBlock)
		}
		if _, err := os.Stat(locked); !os.IsNotExist(err) {
			t.Fatalf("locked path stat = %v, want absent", err)
		}
	})

	t.Run("ignored precious resolution and untracked prediction", func(t *testing.T) {
		repo, _, baseline := newRemovalSafetyRepo(t)
		canonical := repo
		worktree := filepath.Join(t.TempDir(), "precious")
		gitTest(t, repo, "worktree", "add", "-b", "precious", worktree, baseline)
		ignore := ".env.local\n*.local.json\n.envrc\n"
		if err := os.WriteFile(filepath.Join(canonical, ".gitignore"), []byte(ignore), 0o600); err != nil {
			t.Fatal(err)
		}
		gitTest(t, canonical, "add", ".gitignore")
		gitTest(t, canonical, "commit", "-m", "ignore precious fixtures")
		gitTest(t, worktree, "merge", "main")
		for _, root := range []string{canonical, worktree} {
			if err := os.WriteFile(filepath.Join(root, ".env.local"), []byte("duplicate\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(worktree, "auth.local.json"), []byte("unique\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		external := filepath.Join(t.TempDir(), "external-envrc")
		if err := os.WriteFile(external, []byte("external\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(worktree, ".envrc")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(worktree, "notes.md"), []byte("untracked\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		status := gitTest(t, worktree, "status", "--porcelain", "--untracked-files=all", "--ignored")
		for _, expected := range []string{"?? notes.md", "!! .env.local", "!! .envrc", "!! auth.local.json"} {
			if !strings.Contains(status, expected) {
				t.Errorf("status missing %q: %q", expected, status)
			}
		}
		if got, err := syntheticPreciousResolution(worktree, canonical, ".env.local"); err != nil || got != "duplicate-of-canonical" {
			t.Errorf("duplicate resolution = (%q, %v)", got, err)
		}
		if got, err := syntheticPreciousResolution(worktree, canonical, "auth.local.json"); err != nil || got != "unique" {
			t.Errorf("unique resolution = (%q, %v)", got, err)
		}
		if got, err := syntheticPreciousResolution(worktree, canonical, ".envrc"); err != nil || got != "symlink-to-external" {
			t.Errorf("symlink resolution = (%q, %v)", got, err)
		}
		containedTarget := filepath.Join(worktree, "contained-target")
		if err := os.WriteFile(containedTarget, []byte("contained\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("contained-target", filepath.Join(worktree, "contained-auth")); err != nil {
			t.Fatal(err)
		}
		if got, err := syntheticPreciousResolution(worktree, canonical, "contained-auth"); err != nil || got != "unique" {
			t.Errorf("contained symlink resolution = (%q, %v)", got, err)
		}
		if err := os.Symlink("missing-target", filepath.Join(worktree, "broken-auth")); err != nil {
			t.Fatal(err)
		}
		if got, err := syntheticPreciousResolution(worktree, canonical, "broken-auth"); err == nil || got != "unique" {
			t.Errorf("broken symlink resolution = (%q, %v), want blocked unique", got, err)
		}
	})
}

func TestRemovalSafetySyntheticFailuresStayErrors(t *testing.T) {
	repo, _, _ := newRemovalSafetyRepo(t)
	if _, _, err := syntheticRemovalVerdict(repo, "refs/remotes/origin/missing", "HEAD"); err == nil {
		t.Fatal("missing baseline was converted into a verdict")
	}
	missing := filepath.Join(t.TempDir(), "absent")
	cmd := exec.Command("git", "-C", missing, "status", "--porcelain", "--untracked-files=no")
	if err := cmd.Run(); err == nil {
		t.Fatal("missing-worktree status unexpectedly succeeded; errors must not mean clean")
	}
	if state, err := syntheticTrackedState(missing); err == nil || state != "" {
		t.Fatalf("missing-worktree tracked state=(%q, %v), want error", state, err)
	}
	blobPath := filepath.Join(repo, "blob")
	if err := os.WriteFile(blobPath, []byte("not a commit\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	blob := strings.TrimSpace(gitTest(t, repo, "hash-object", "-w", blobPath))
	if _, _, err := syntheticRemovalVerdict(repo, blob, "HEAD"); err == nil {
		t.Fatal("non-commit baseline was converted into a verdict")
	}
	gitTest(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	if out, err := exec.Command("git", "-C", repo, "fetch", "origin").CombinedOutput(); err == nil {
		t.Fatalf("synthetic fetch failure unexpectedly succeeded: %s", out)
	}
}

type sweepDocument struct {
	SchemaVersion int `json:"schema_version"`
	Complete      bool
	Rows          []map[string]any
}

func TestSweepInventoryFullOuterJoinStableOrderingAndParity(t *testing.T) {
	root, repo, configDir := newSweepInventoryFixture(t)
	script := filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory")
	args := []string{script, "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root}

	jsonOutput, exit := runSweepInventory(t, append(args, "--json")...)
	if exit != 0 {
		t.Fatalf("sweep JSON exit=%d\n%s", exit, jsonOutput)
	}
	var document sweepDocument
	if err := json.Unmarshal([]byte(jsonOutput), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != 1 || !document.Complete {
		t.Fatalf("sweep metadata = version %d complete %t", document.SchemaVersion, document.Complete)
	}
	wantClasses := map[string]bool{
		"directory_without_worker_record":    false,
		"git_registration_without_directory": false,
		"worker_record_without_directory":    false,
		"directory_without_git_registration": false,
		"open_lifecycle_obligation":          false,
		"lifecycle_record_without_worker":    false,
	}
	previous := ""
	lockedMissingFound := false
	literalZeroObligationFound := false
	for _, row := range document.Rows {
		classification := row["classification"].(string)
		if _, exists := wantClasses[classification]; exists {
			wantClasses[classification] = true
		}
		if row["removal_verdict"] != "NOT_EVALUATED" {
			t.Errorf("row %q manufactured removal verdict %v", classification, row["removal_verdict"])
		}
		if classification == "git_registration_without_directory" && row["git_locked"] == true && row["git_prunable"] == false {
			lockedMissingFound = true
		}
		if classification == "worker_record_without_directory" && row["thread"] == "T-missing" && row["open_obligation"] == true {
			literalZeroObligationFound = true
		}
		key := fmt.Sprintf("%03.0f\x00%s\x00%s\x00%s", row["rank"].(float64), stringValue(row["path"]), stringValue(row["thread"]), classification)
		if previous > key {
			t.Errorf("unstable row order: %q before %q", previous, key)
		}
		previous = key
	}
	for classification, found := range wantClasses {
		if !found {
			t.Errorf("full outer join lacks %q: %#v", classification, document.Rows)
		}
	}
	if !lockedMissingFound {
		t.Error("locked-and-missing Git registration was not retained and classified from lstat evidence")
	}
	if !literalZeroObligationFound {
		t.Error("literal-zero blocked lifecycle obligation was not carried through the worker binding")
	}

	repeated, repeatedExit := runSweepInventory(t, append(args, "--json")...)
	if repeatedExit != 0 || repeated != jsonOutput {
		t.Fatalf("repeated output was not byte-stable: exit=%d\nfirst=%s\nsecond=%s", repeatedExit, jsonOutput, repeated)
	}
	humanOutput, humanExit := runSweepInventory(t, args...)
	if humanExit != 0 {
		t.Fatalf("sweep human exit=%d\n%s", humanExit, humanOutput)
	}
	humanRows := parseSweepHumanRows(t, humanOutput)
	if len(humanRows) != len(document.Rows) {
		t.Fatalf("human rows=%d JSON rows=%d", len(humanRows), len(document.Rows))
	}
	for index, jsonRow := range document.Rows {
		if len(humanRows[index]) != len(jsonRow) {
			t.Errorf("row %d human fields=%d JSON fields=%d", index, len(humanRows[index]), len(jsonRow))
		}
		for field := range jsonRow {
			if !equalJSONValue(humanRows[index][field], jsonRow[field]) {
				t.Errorf("row %d field %s differs: human=%#v JSON=%#v", index, field, humanRows[index][field], jsonRow[field])
			}
		}
	}
}

func TestSweepInventoryMalformedInputsAndPathsFailClosed(t *testing.T) {
	t.Run("partial malformed stores remain visible", func(t *testing.T) {
		root, repo, configDir := newSweepInventoryFixture(t)
		workers := filepath.Join(configDir, "workers.tsv")
		if err := os.WriteFile(workers, []byte("# amux-schema: workers/v1\nrelative\tbad\trelative/path\tT-relative\nmalformed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "reports.json"), []byte(`{"schema_version":1,"reports":[`), 0o600); err != nil {
			t.Fatal(err)
		}
		output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root, "--json")
		if exit != 2 {
			t.Fatalf("malformed sweep exit=%d, want 2\n%s", exit, output)
		}
		var document sweepDocument
		if err := json.Unmarshal([]byte(output), &document); err != nil {
			t.Fatal(err)
		}
		if document.Complete {
			t.Fatal("malformed inventory was reported complete")
		}
		sources := map[string]bool{}
		for _, row := range document.Rows {
			if failure, ok := row["error"].(map[string]any); ok {
				sources[failure["source"].(string)] = true
			}
		}
		if !sources["workers.tsv"] || !sources["reports.json"] {
			t.Fatalf("malformed source rows were dropped: %#v", sources)
		}
	})

	t.Run("symlink boundaries and worker paths are errors", func(t *testing.T) {
		root, repo, configDir := newSweepInventoryFixture(t)
		symlinkRoot := filepath.Join(t.TempDir(), "checkout-root-link")
		if err := os.Symlink(root, symlinkRoot); err != nil {
			t.Fatal(err)
		}
		workerTarget := filepath.Join(root, "worker-target")
		if err := os.Mkdir(workerTarget, 0o700); err != nil {
			t.Fatal(err)
		}
		workerLink := filepath.Join(root, "worker-link")
		if err := os.Symlink(workerTarget, workerLink); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(configDir, "workers.tsv"), []byte("ws\tlink\t"+workerLink+"\tT-link\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", symlinkRoot, "--json")
		if exit != 2 {
			t.Fatalf("unsafe-path sweep exit=%d, want 2\n%s", exit, output)
		}
		for _, required := range []string{"filesystem root is a symlink", "path is a symlink", `"complete":false`} {
			if !strings.Contains(output, required) {
				t.Errorf("unsafe-path output lacks %q: %s", required, output)
			}
		}
	})

	t.Run("ancestor symlink aliases canonicalize to one identity", func(t *testing.T) {
		root, repo, configDir := newSweepInventoryFixture(t)
		aliasParent := filepath.Join(t.TempDir(), "alias-parent")
		if err := os.Symlink(filepath.Dir(root), aliasParent); err != nil {
			t.Fatal(err)
		}
		aliasRoot := filepath.Join(aliasParent, filepath.Base(root))
		workers, err := os.ReadFile(filepath.Join(configDir, "workers.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		workers = []byte(strings.ReplaceAll(string(workers), root, aliasRoot))
		if err := os.WriteFile(filepath.Join(configDir, "workers.tsv"), workers, 0o600); err != nil {
			t.Fatal(err)
		}
		output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", aliasRoot, "--json")
		var document sweepDocument
		if err := json.Unmarshal([]byte(output), &document); err != nil {
			t.Fatal(err)
		}
		joined := 0
		for _, row := range document.Rows {
			if row["thread"] == "T-open" && row["git_registration"] == true && row["worker_record"] == true {
				joined++
			}
		}
		if exit != 0 || joined != 1 {
			t.Fatalf("ancestor alias split one physical identity: exit=%d\n%s", exit, output)
		}
	})

	t.Run("authoritative report validation rejects impossible stores", func(t *testing.T) {
		root, repo, configDir := newSweepInventoryFixture(t)
		invalid := `{"schema_version":1,"reports":[{"schema_version":1,"report_id":"bad","member_thread":"T-open","status":"blocked","authorized_at":"garbage","unknown":true}],"deadlines":[{"group_id":"bad"}]}`
		if err := os.WriteFile(filepath.Join(configDir, "reports.json"), []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root, "--json")
		if exit != 2 || !strings.Contains(output, "authoritative amux report validation failed") || !strings.Contains(output, `"complete":false`) {
			t.Fatalf("invalid authoritative store did not fail closed: exit=%d\n%s", exit, output)
		}
	})

	t.Run("ambiguous workers are quarantined independent of order", func(t *testing.T) {
		for _, reverse := range []bool{false, true} {
			root, repo, configDir := newSweepInventoryFixture(t)
			rows := []string{
				"ws\tshared-a\t" + filepath.Join(root, "a") + "\tT-shared",
				"ws\tshared-b\t" + filepath.Join(root, "b") + "\tT-shared",
				"ws\tduplicate-window\t" + filepath.Join(root, "c") + "\tT-c",
				"ws\tduplicate-window\t" + filepath.Join(root, "d") + "\tT-d",
			}
			if reverse {
				for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
					rows[left], rows[right] = rows[right], rows[left]
				}
			}
			if err := os.WriteFile(filepath.Join(configDir, "workers.tsv"), []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root, "--json")
			if exit != 2 || strings.Contains(output, `"worker_record":true`) || strings.Contains(output, `"classification":"open_lifecycle_obligation"`) {
				t.Fatalf("ambiguous worker order reverse=%t selected authority: exit=%d\n%s", reverse, exit, output)
			}
		}
	})

	t.Run("all current worker assignment states are accepted", func(t *testing.T) {
		root, repo, configDir := newSweepInventoryFixture(t)
		states := []string{"", "retained_indeterminate", "native_not_attempted", "native_rejected", "native_indeterminate", "authenticated_accepted"}
		var rows []string
		for index, state := range states {
			row := fmt.Sprintf("ws\tstate-%d\t%s\tT-state-%d", index, filepath.Join(root, fmt.Sprintf("state-%d", index)), index)
			if state != "" {
				row += "\t" + state
			}
			rows = append(rows, row)
		}
		if err := os.WriteFile(filepath.Join(configDir, "workers.tsv"), []byte(strings.Join(rows, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root, "--json")
		if exit != 0 || strings.Contains(output, "unsupported assignment state") {
			t.Fatalf("current assignment states rejected: exit=%d\n%s", exit, output)
		}
	})

	t.Run("non-directory config and missing git are structured errors", func(t *testing.T) {
		root, repo, _ := newSweepInventoryFixture(t)
		configFile := filepath.Join(root, "config-file")
		if err := os.WriteFile(configFile, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory")
		output, exit := runSweepInventory(t, script, "--repo", repo, "--config-dir", configFile, "--amux", buildSweepAmux(t), "--filesystem-root", root, "--json")
		if exit != 2 || !strings.Contains(output, "config directory is not_a_directory") {
			t.Fatalf("non-directory config did not fail closed: exit=%d\n%s", exit, output)
		}
		python, err := exec.LookPath("python3")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(python, script, "--repo", repo, "--config-dir", filepath.Join(root, "absent-config"), "--filesystem-root", root, "--json")
		cmd.Env = append(os.Environ(), "PATH=/nonexistent")
		gitOutput, gitErr := cmd.CombinedOutput()
		gitExit, ok := gitErr.(*exec.ExitError)
		if !ok || gitExit.ExitCode() != 2 || !strings.Contains(string(gitOutput), "cannot execute git") || !strings.Contains(string(gitOutput), `"complete":false`) {
			t.Fatalf("missing git was not a structured exit 2: err=%v\n%s", gitErr, gitOutput)
		}
	})
}

func TestSweepPresentationFiltersNeverChangeSafety(t *testing.T) {
	root, repo, configDir := newSweepInventoryFixture(t)
	worktree := filepath.Join(root, "open-worker")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".env.local\nauth.local.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", ".gitignore")
	gitTest(t, repo, "commit", "-m", "ignore precious files")
	gitTest(t, worktree, "merge", "main")
	for _, target := range []string{filepath.Join(repo, ".env.local"), filepath.Join(worktree, ".env.local")} {
		if err := os.WriteFile(target, []byte("duplicate\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, "auth.local.json"), []byte("unique\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitFile(t, worktree, "dist/generated.js", "generated\n", "generated")
	commitFile(t, worktree, "src/real.go", "package real\n", "real")
	if err := os.WriteFile(filepath.Join(worktree, "dist/generated.js"), []byte("generated stash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, worktree, "stash", "push", "-m", "generated-only")
	if err := os.WriteFile(filepath.Join(worktree, "dist/untracked.js"), []byte("untracked stash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, worktree, "stash", "push", "-u", "-m", "untracked-generated")
	unmanaged := filepath.Join(root, "unmanaged")
	if err := os.WriteFile(filepath.Join(unmanaged, "tracked.txt"), []byte("other branch stash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, unmanaged, "stash", "push", "-m", "mentions open-worker in prose")
	indexPath := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "--git-path", "index"))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(worktree, indexPath)
	}
	staleTime := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(worktree, "tracked.txt"), staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	indexBefore, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{
		filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"),
		"--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root,
		"--presentation-baseline", "refs/heads/main", "--generated-exclude", "dist/*", "--canonical-worktree", repo, "--json",
	}
	output, exit := runSweepInventory(t, args...)
	if exit != 0 {
		t.Fatalf("presentation sweep exit=%d\n%s", exit, output)
	}
	indexAfter, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Error("read-only sweep refreshed or rewrote the external worktree index")
	}
	var document sweepDocument
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatal(err)
	}
	for _, row := range document.Rows {
		if row["thread"] != "T-open" {
			continue
		}
		presentation := row["presentation"].(map[string]any)
		if !equalJSONValue(presentation["divergence_raw"], []any{"dist/generated.js", "src/real.go"}) || !equalJSONValue(presentation["divergence_filtered"], []any{"src/real.go"}) {
			t.Errorf("unexpected divergence presentation: %#v", presentation)
		}
		precious := presentation["precious_paths"].([]any)
		if len(precious) != 2 || !strings.Contains(fmt.Sprint(precious), "duplicate-of-canonical") || !strings.Contains(fmt.Sprint(precious), "unique") {
			t.Errorf("unexpected precious presentation: %#v", precious)
		}
		stashes := presentation["stashes"].([]any)
		stashText := fmt.Sprint(stashes)
		if len(stashes) != 3 || !strings.Contains(stashText, "dist/generated.js") || !strings.Contains(stashText, "dist/untracked.js") || !strings.Contains(stashText, "association:unassigned") || !strings.Contains(stashText, "paths_filtered:[]") {
			t.Errorf("unexpected stash presentation: %#v", stashes)
		}
		if row["removal_verdict"] != "NOT_EVALUATED" {
			t.Errorf("presentation filter changed safety: %#v", row)
		}
		return
	}
	t.Fatal("T-open presentation row not found")
}

func TestSweepPresentationInputsFailClosedWithoutMutation(t *testing.T) {
	root, repo, configDir := newSweepInventoryFixture(t)
	script := filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory")
	amux := buildSweepAmux(t)
	probeDir := t.TempDir()
	probe := filepath.Join(probeDir, "git-output")
	output, exit := runSweepInventory(t, script, "--repo", repo, "--config-dir", configDir, "--amux", amux, "--filesystem-root", root, "--presentation-baseline=--output="+probe, "--json")
	if exit != 2 || !strings.Contains(output, `"complete":false`) || !strings.Contains(output, "presentation divergence") {
		t.Fatalf("option-like baseline did not fail closed: exit=%d\n%s", exit, output)
	}
	entries, err := os.ReadDir(probeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("option-like baseline created files: %#v", entries)
	}

	canonical := filepath.Join(t.TempDir(), "canonical")
	if err := os.Symlink(repo, canonical); err != nil {
		t.Fatal(err)
	}
	output, exit = runSweepInventory(t, script, "--repo", repo, "--config-dir", configDir, "--amux", amux, "--filesystem-root", root, "--canonical-worktree", canonical, "--json")
	if exit != 2 || !strings.Contains(output, "canonical worktree is ambiguous_symlink") || !strings.Contains(output, `"complete":false`) {
		t.Fatalf("canonical final symlink did not fail closed: exit=%d\n%s", exit, output)
	}
}

func TestSweepWorkflowDocumentsReadOnlyAuthorityBoundary(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{
		"/amux sweep",
		"read-only skill workflow",
		"full outer join over four independent authorities",
		"reports.json member_thread joined through workers.tsv",
		"report_id` and `groups.tsv` never establish a workdir",
		"explicit and uncapped",
		"removal_verdict=NOT_EVALUATED",
		"no fetch, cleanup, reconciliation, removal, unlock, prune, backup-ref mutation",
		"Preservation-locked historical resources",
		"one-time read-only migration inventory for the staged Amux drain",
		"after the owner records acceptance of one complete staged-drain inventory",
		"delete `scripts/sweep-inventory`, its sweep-only tests, and every `/amux sweep` route/reference before the next Amux release",
		"Do not promote the helper into the CLI, retain it as a standing diagnostic, schedule it, or extend it for post-drain monitoring",
	} {
		if !strings.Contains(skill+workflow, required) {
			t.Errorf("sweep contract lacks %q", required)
		}
	}
}

func TestClaudePairTeardownIsFailClosedAndRunsBeforeWorkerTeardown(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-delegation-contract.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	recovery := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "claude-delegation-recovery.md"))

	for _, required := range []string{
		"lifecycle worker-teardown --origin-thread <thread-id> --dry-run",
		"lifecycle worker-teardown --origin-thread <thread-id>",
		"stop without Amp teardown",
		"Worker teardown remains the final action",
		"/amux-claude",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("worker lifecycle workflow is missing %q", required)
		}
	}
	for _, required := range []string{
		"exact immutable origin-thread binding",
		"never becomes an Amp worker, runner, group member, or generic CLI resource",
		"non-content action and blocker codes",
		"origin-thread SHA-256",
		"missing or unreadable registered directory or `receipts.json` blocks",
		"30-day cleanup eligibility",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("Claude lifecycle contract is missing %q", required)
		}
	}
	if !strings.Contains(recovery, "paired worker teardown") || !strings.Contains(recovery, "preserve the Amp worker") {
		t.Error("Claude recovery does not preserve worker and evidence on paired teardown blockers")
	}
	if !strings.Contains(skill, "/amux-claude") {
		t.Error("SKILL.md does not gate teardown through /amux-claude when pairs may exist")
	}
	for _, required := range []string{
		"register-legacy-store --origin-thread <thread-id> --store-path <exact-private-store>",
		"detach-indeterminate-worker",
		"retire-live-indeterminate-pair",
		"retire-live-acquired-no-report-pair",
		"dispose-exact-pre-identity-acquired-pair",
		"historical_modern_read_only_launch_intent_v1",
		"pre_identity_acquired_no_report_v1",
		"pre_identity_acquired_pair_permanently_non_retirable",
		"paired_worker_teardown_prohibited",
		"state:pair_retired",
		"state:acquired_pair_retired",
		"state:exact_pane_disposed",
		"dispose_exact_pane_process_incarnation",
		"executable_identity_acknowledgement",
		"terminal Amp work authorization",
		"durable origin fence",
		"must not continue to worktree removal",
	} {
		if !strings.Contains(recovery+workflow+contract, required) {
			t.Errorf("indeterminate detach progressive disclosure is missing %q", required)
		}
	}
	claudeSkill := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "SKILL.md"))
	claudeTriggers := readSkillFile(t, root, filepath.Join("skills", "amux-claude", "reference", "trigger-phrases.md"))
	if !strings.Contains(claudeSkill, "Recover indeterminate Claude worker evidence") || !strings.Contains(claudeTriggers, "Recover indeterminate Claude worker evidence") {
		t.Error("indeterminate recovery trigger is not routed in amux-claude")
	}
	if strings.Contains(skill, "Recover indeterminate Claude worker evidence") {
		t.Error("core /amux skill must not own Claude recovery triggers")
	}
	pairAdmission := strings.Index(workflow, "If `/amux-claude` pairs may exist, run the paired Claude lifecycle dry-run")
	worktreeRemoval := strings.Index(workflow, "[Remove the worker worktree without force")
	finalRevalidation := strings.Index(workflow, "rerun paired Claude lifecycle revalidation")
	finalTeardown := strings.LastIndex(workflow, "amux teardown --thread <thread-id>")
	if pairAdmission < 0 || worktreeRemoval < 0 || pairAdmission > worktreeRemoval {
		t.Error("finish does not admit paired Claude lifecycle before worktree removal")
	}
	if finalRevalidation < 0 || finalTeardown < 0 || finalRevalidation > finalTeardown {
		t.Error("finish does not revalidate the durable pair fence before final worker teardown")
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
	for _, relativePath := range []string{"README.md", filepath.Join("docs", "skill", "index.html")} {
		contents := readSkillFile(t, root, relativePath)
		if !strings.Contains(contents, "AMUX_SKILLS_SOURCE") {
			t.Errorf("%s does not document the opt-in local-checkout skill links", relativePath)
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

func textFenceAfter(t *testing.T, contents, marker string) string {
	t.Helper()
	markerAt := strings.Index(contents, marker)
	if markerAt < 0 {
		t.Fatalf("missing prompt marker %q", marker)
	}
	remainder := contents[markerAt+len(marker):]
	start := strings.Index(remainder, "```text\n")
	if start < 0 {
		t.Fatalf("missing text fence after %q", marker)
	}
	remainder = remainder[start+len("```text\n"):]
	end := strings.Index(remainder, "\n```")
	if end < 0 {
		t.Fatalf("unterminated text fence after %q", marker)
	}
	return remainder[:end]
}

func TestActivePublicSurfacesLabelLegacyLifecycleAndNativeChildren(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	surfaces := map[string]string{
		"homepage":           readSkillFile(t, root, filepath.Join("docs", "index.html")),
		"public skill guide": readSkillFile(t, root, filepath.Join("docs", "skill", "index.html")),
		"README":             readSkillFile(t, root, "README.md"),
		"skill metadata":     readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md")),
		"trigger checklist":  readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md")),
		"workflow source":    readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md")),
	}
	for name, contents := range surfaces {
		lower := strings.ToLower(contents)
		for _, forbidden := range []string{
			"coordinate issue workers",
			"coordinated issue workers",
			"native issue-worker coordination",
			"issue-worker coordination",
			"issue worker coordination",
			"adopt a native-created thread",
			"adopt an exact native-created thread",
		} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s retains active native/Amux terminology %q", name, forbidden)
			}
		}
	}

	requiredBySurface := map[string][]string{
		"homepage": {
			"Current new work uses native child threads. The pre-cutover lifecycle cards below describe compatibility/drain support retained by this release, not new-work features.",
			"<code>pre-cutover group drain</code>",
			"<code>pre-cutover report drain</code>",
			"<code>pre-cutover callback drain</code>",
			"Coordinate child threads",
			"coordinated native child threads",
		},
		"public skill guide": {
			"Amux work groups, reports, callback leases, deadlines, and finish authorization are pre-cutover compatibility/drain state only",
			"Native child-thread coordination",
			"exact allowed next transition",
		},
		"README": {
			"Pre-cutover durable work-group compatibility",
			"Pre-cutover reports, callback leases, and finish authorization",
			"`amux worker adopt` is a compatibility/drain surface only for an exact persisted pre-cutover adoption operation whose next transition is proven drain-eligible",
		},
		"skill metadata": {
			"proven pre-cutover compatibility/drain state for work groups, reports, callback leases, deadlines, and finish authorization",
			"routing new Amp work to native child threads",
			"pre-cutover existing-worker compatibility/drain only",
		},
		"trigger checklist": {
			"`Coordinate child threads`",
			"Skill-only pre-cutover compatibility/drain",
		},
		"workflow source": {
			"Coordinate native child threads and drain a durable work group",
			"compatibility-only for a durable group, member worker, callback, and report identity that already exist",
			"Finish is compatibility/drain-only post-merge orchestration for an existing pre-cutover worker",
		},
	}
	for name, required := range requiredBySurface {
		for _, want := range required {
			if !strings.Contains(surfaces[name], want) {
				t.Errorf("%s is missing active boundary %q", name, want)
			}
		}
	}
}

func TestNativeCreatePromptExamplesExcludeAmuxLifecycle(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	type promptExample struct {
		name   string
		prompt string
	}
	examples := []promptExample{
		{name: "workflow", prompt: textFenceAfter(t, workflow, "### Lean native `create_thread` prompt example")},
	}
	publicPage := readSkillFile(t, root, filepath.Join("docs", "skill", "index.html"))
	htmlExamples := regexp.MustCompile(`(?s)<pre data-native-create-prompt><code>(.*?)</code></pre>`).FindAllStringSubmatch(publicPage, -1)
	if len(htmlExamples) != 2 {
		t.Fatalf("public skill page has %d native create prompt examples, want 2", len(htmlExamples))
	}
	for index, match := range htmlExamples {
		examples = append(examples, promptExample{name: fmt.Sprintf("public-page-%d", index+1), prompt: match[1]})
	}

	for _, example := range examples {
		lower := strings.ToLower(example.prompt)
		for _, forbidden := range []string{"amux", "contract-v1", "receipt", "report", "callback", "adopt", "group", "deadline", "finish authoriz", "spawned worker"} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("native create prompt example %s contains forbidden lifecycle marker %q:\n%s", example.name, forbidden, example.prompt)
			}
		}
		for _, required := range []string{"task", "acceptance criteria", "relevant context and constraints", "validation", "expected result", "reply to the parent"} {
			if !strings.Contains(lower, required) {
				t.Errorf("native create prompt example %s is missing lean task field %q:\n%s", example.name, required, example.prompt)
			}
		}
	}
}

func TestContractV1IsLimitedToProvenLegacyDrainWorkers(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "contract-v1.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	if !strings.Contains(skill, "contract-v1.md") {
		t.Error("SKILL.md must link contract-v1")
	}
	for _, required := range []string{
		"amux-contract: v1",
		"Compatibility protocol for a pre-cutover Amux-managed spawn, adoption, or durable group flow",
		"read this file once",
		"absolute path",
		"never a bare relative path",
		"persisted provenance proves that flow is drain-eligible",
		"Native Amp `create_thread` work never reads this file",
		"lean task prompt and native parent/reply routing only",
		"no Amux receipt, report, callback, adoption, group, deadline, or finish-authorization requirement",
		"ready",
		"blocked",
		"merged",
		"never authorize finish",
		"/amux-claude",
		"Do not Read Thread",
		"Oracle must not Read Thread",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("contract-v1 missing %q", required)
		}
	}
	for _, required := range []string{"Invocation defaults", "do not Read Thread for task context", "give Oracle supplied diff/context only"} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md must surface credit default %q", required)
		}
	}
	for _, required := range []string{"compatibility-only", "pre-cutover Amux-managed spawn, adopt, or group flow", "proves it is drain-eligible", "Never put its path or lifecycle instructions in a native `create_thread` prompt"} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md is missing contract admission fence %q", required)
		}
	}
	if strings.Contains(contract, "Spawned workers must") || strings.Contains(contract, "Every native child creation") {
		t.Error("contract-v1 still admits native-created work as an Amux contract worker")
	}
	legacyExample := textFenceAfter(t, workflow, "### Proven legacy drain-only prompt example")
	for _, required := range []string{"Legacy Amux drain only", "pre-cutover", "verified drain-eligible", "/absolute/path/to/installed/amux/reference/contract-v1.md", "exact existing group/report/callback identities", "Create or rebind nothing"} {
		if !strings.Contains(legacyExample, required) {
			t.Errorf("legacy drain prompt example is missing %q:\n%s", required, legacyExample)
		}
	}
	if !strings.Contains(workflow, "contract-v1.md` and Amux lifecycle instructions are permitted only inside that proven legacy boundary") {
		t.Error("workflow does not fence contract-v1 to the proven legacy drain boundary")
	}
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	if strings.Contains(triggers, "absolute contract-v1 path") || !strings.Contains(triggers, "lean task prompt and native parent/reply routing only") {
		t.Error("trigger checklist still injects the legacy contract into native work")
	}
}

func TestGlobalAgentsSnippetSeparatesNativeWorkFromLegacyDrain(t *testing.T) {
	t.Parallel()
	snippet := readSkillFile(t, repoRoot(t), filepath.Join("docs", "snippets", "global-agents-amux-prefs.md"))
	for _, required := range []string{
		"Use native Amp `create_thread` for ordinary delegated work",
		"exact Workspace Project for Orb execution, or the exact live runner/workdir",
		"retain the native parent/reply route",
		"If native creation is unavailable, rejected, or indeterminate, stop",
		"Do not retry, use an executor fallback, or create any Amux lifecycle state",
		"Keep native child prompts lean",
		"Never include an Amux `reference/contract-v1.md` path",
		"exact persisted records prove both an existing Amux-managed spawn, adoption, or group flow's pre-cutover admission and its exact allowed next drain transition",
		"Generalized Amux spawn admission is closed",
		"never automatically adopt it",
		"Only an explicitly requested exact existing Tycho route or one owner-authorized prepared route created without provider execution and bound before its first run may own internal machine/provider routing",
		"the real Amp thread remains coordinator and consume/ack authority, while Tycho remains report-only",
		"`/amux-tycho` is the experimental explicit-only report bridge",
		"receipt admission remains open only until the authenticated direct structured-return gate passes",
		"Forgex is experimental and requires my explicit request",
		"`/amux-claude` and `/amux-pi` remain experimental fallback/reference paths and require my explicit request",
	} {
		if !strings.Contains(snippet, required) {
			t.Errorf("global AGENTS replacement snippet is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Use Amux for local Amp/tmux lifecycle, exact adoption/recovery, and when native creation is unavailable",
		"task, acceptance criteria, and the absolute path to the loaded `/amux` skill's `reference/contract-v1.md`",
		"Tycho may own machine/provider routing for Claude Code and Pi/Codex Spark",
	} {
		if strings.Contains(snippet, forbidden) {
			t.Errorf("global AGENTS replacement snippet retains generic injection policy %q", forbidden)
		}
	}
}

func TestDescriptionCarriesNaturalLanguageSynonymTriggers(t *testing.T) {
	t.Parallel()
	skill := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "SKILL.md"))
	descStart := strings.Index(skill, "description:")
	if descStart < 0 {
		t.Fatal("SKILL.md missing description frontmatter")
	}
	descEnd := strings.Index(skill[descStart:], "\n---")
	if descEnd < 0 {
		t.Fatal("SKILL.md description frontmatter is unterminated")
	}
	description := skill[descStart : descStart+descEnd]
	// Phrases that are not substrings of the CLI verb list must remain matchable
	// before skill activation. Do not require every table row in the description.
	for _, phrase := range []string{
		"forget this on restore",
		"hide it for now",
		"defer this workspace",
		"Show shelved work",
		"Restore my workspace",
	} {
		if !strings.Contains(description, phrase) {
			t.Errorf("frontmatter description missing pre-activation synonym %q", phrase)
		}
	}
}

func TestExperimentalSkillsAreSeparatedFromCoreAmux(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	core := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	coreTriggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	for _, forbidden := range []string{
		"Delegate read-only analysis to Claude",
		"Delegate isolated mutating work to Claude",
		"Delegate bounded work to Claude Opus in a fresh Amp Orb",
		"Run Pi on Spark in an Amp Orb",
		"Recover indeterminate Claude worker evidence",
	} {
		if strings.Contains(coreTriggers, forbidden) {
			t.Errorf("core trigger checklist still routes experimental %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "amux-claude", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "amux-pi", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "amux-tycho", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(core, "reference/claude-") || strings.Contains(core, "reference/pi-spark") {
		t.Error("core SKILL.md must not link experimental claude/pi reference paths")
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

func newRemovalSafetyRepo(t *testing.T) (repo, remote, baseline string) {
	t.Helper()
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	repo = filepath.Join(root, "repo")
	gitTest(t, root, "init", "--bare", remote)
	gitTest(t, root, "init", "-b", "main", repo)
	gitTest(t, repo, "config", "user.name", "Synthetic Removal Safety")
	gitTest(t, repo, "config", "user.email", "synthetic@example.invalid")
	gitTest(t, repo, "config", "commit.gpgSign", "false")
	gitTest(t, repo, "config", "tag.gpgSign", "false")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "tracked.txt")
	gitTest(t, repo, "commit", "-m", "base")
	gitTest(t, repo, "remote", "add", "origin", remote)
	gitTest(t, repo, "push", "-u", "origin", "main")
	gitTest(t, repo, "remote", "set-head", "origin", "main")
	return repo, remote, "refs/remotes/origin/main"
}

func commitFile(t *testing.T, repo, name, contents, message string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", name)
	gitTest(t, repo, "commit", "-m", message)
}

type backupManifestTarget struct {
	path      string
	tip       string
	pathState string
	locked    bool
	prunable  bool
}

func backupHelperPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "skills", "amux", "scripts", "backup-removal-refs")
}

func writeBackupManifest(t *testing.T, repo, baseline, operation string, targets []backupManifestTarget) (manifestPath, firstRef, firstTip string) {
	t.Helper()
	repository := strings.TrimSpace(gitTest(t, repo, "rev-parse", "--show-toplevel"))
	baselineTip := strings.TrimSpace(gitTest(t, repo, "rev-parse", baseline+"^{commit}"))
	rows := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		path := target.path
		if target.pathState == "" || target.pathState == "present" {
			path = strings.TrimSpace(gitTest(t, target.path, "rev-parse", "--show-toplevel"))
		}
		tip := target.tip
		if tip == "" {
			tip = strings.TrimSpace(gitTest(t, target.path, "rev-parse", "HEAD"))
		}
		pathState := target.pathState
		if pathState == "" {
			pathState = "present"
		}
		backupRef := "refs/heads/backup/" + filepath.Base(path) + "-before-remove-2026-08-12"
		rows = append(rows, map[string]any{
			"path":          path,
			"tip":           tip,
			"branch":        nil,
			"locked":        target.locked,
			"prunable":      target.prunable,
			"path_state":    pathState,
			"verdict":       "NEEDS_BACKUP",
			"covering_refs": []string{},
			"rule_evidence": map[string]any{"rule": "5"},
			"backup_ref":    backupRef,
			"date":          "2026-08-12",
			"ownership_evidence": map[string]any{
				"worker":  map[string]any{"status": "absent", "evidence": "no workers.tsv row for exact path"},
				"runner":  map[string]any{"status": "absent", "evidence": "no runner row for exact path"},
				"process": map[string]any{"status": "clear", "evidence": "no matching process identity"},
			},
		})
		if firstRef == "" {
			firstRef, firstTip = backupRef, tip
		}
	}
	document := map[string]any{
		"schema_version": 1,
		"operation":      operation,
		"repository":     repository,
		"fetch":          map[string]any{"command": []string{"git", "fetch", "--prune", "origin"}, "result": "success"},
		"baseline":       map[string]any{"ref": baseline, "tip": baselineTip, "source": "origin/HEAD"},
		"rows":           rows,
	}
	contents, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath = filepath.Join(t.TempDir(), "backup-manifest.json")
	if err := os.WriteFile(manifestPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath, firstRef, firstTip
}

func mutateBackupManifest(t *testing.T, manifestPath string, mutate func(map[string]any)) {
	t.Helper()
	contents, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	mutate(document)
	contents, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runBackupHelper(t *testing.T, manifest string, args ...string) map[string]any {
	t.Helper()
	return runBackupHelperExit(t, 0, manifest, args...)
}

func runBackupHelperExit(t *testing.T, wantExit int, manifest string, args ...string) map[string]any {
	t.Helper()
	return runBackupHelperExitEnv(t, wantExit, nil, manifest, args...)
}

func runBackupHelperExitEnv(t *testing.T, wantExit int, environment []string, manifest string, args ...string) map[string]any {
	t.Helper()
	commandArgs := []string{backupHelperPath(t), "--manifest", manifest}
	commandArgs = append(commandArgs, args...)
	command := exec.Command("python3", commandArgs...)
	if environment != nil {
		command.Env = environment
	}
	output, err := command.CombinedOutput()
	gotExit := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			gotExit = exit.ExitCode()
		} else {
			t.Fatalf("run backup helper: %v", err)
		}
	}
	if gotExit != wantExit {
		t.Fatalf("backup helper exit=%d want=%d\n%s", gotExit, wantExit, output)
	}
	var document map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output), &document); err != nil {
		t.Fatalf("decode backup helper output: %v\n%s", err, output)
	}
	return document
}

func gitTestOptionalRef(t *testing.T, repo, ref string) string {
	t.Helper()
	command := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", ref)
	output, err := command.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output))
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
		return ""
	}
	t.Fatalf("inspect ref %s: %v\n%s", ref, err, output)
	return ""
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return string(gitTestBytes(t, dir, args...))
}

func gitTestBytes(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return out
}

func gitTestInput(t *testing.T, dir string, input []byte, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin = bytes.NewReader(input)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

func syntheticRemovalVerdict(repo, baseline, tip string) (string, string, error) {
	for label, rev := range map[string]string{"baseline": baseline, "tip": tip} {
		cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", rev+"^{commit}")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", "", fmt.Errorf("resolve %s: %w: %s", label, err, out)
		}
	}
	cmd := exec.Command("git", "-C", repo, "for-each-ref", "--contains", tip, "--format=%(refname)", "refs/heads", "refs/remotes", "refs/tags")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("ref coverage: %w: %s", err, out)
	}
	refs := strings.Fields(string(out))
	for _, ref := range refs {
		if strings.HasPrefix(ref, "refs/remotes/") {
			return "SAFE", ref, nil
		}
	}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "refs/heads/") || strings.HasPrefix(ref, "refs/tags/") {
			return "SAFE_KEEP_BRANCH", strings.Join(refs, ","), nil
		}
	}
	ancestor := exec.Command("git", "-C", repo, "merge-base", "--is-ancestor", tip, baseline)
	if out, err := ancestor.CombinedOutput(); err == nil {
		return "SAFE", baseline, nil
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		return "", "", fmt.Errorf("ancestry: %w: %s", err, out)
	}
	cherry := exec.Command("git", "-C", repo, "cherry", baseline, tip)
	out, err = cherry.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("patch equivalence: %w: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	allEquivalent := len(lines) > 0 && lines[0] != ""
	for _, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			allEquivalent = false
		}
	}
	if allEquivalent {
		return "SAFE", "patch-equivalent", nil
	}
	return "NEEDS_BACKUP", "create backup ref at " + tip, nil
}

func syntheticWorktreeBlock(porcelain, path string) string {
	for _, block := range strings.Split(strings.TrimSpace(porcelain), "\n\n") {
		if strings.HasPrefix(block, "worktree "+path+"\n") {
			return block
		}
	}
	return ""
}

func syntheticTrackedState(worktree string) (string, error) {
	cmd := exec.Command("git", "-C", worktree, "status", "--porcelain", "--untracked-files=no")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tracked status: %w: %s", err, out)
	}
	if len(out) != 0 {
		return "BLOCKED", nil
	}
	return "clean", nil
}

func syntheticPreciousResolution(worktree, canonical, relative string) (string, error) {
	worktreeRoot, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", fmt.Errorf("resolve worktree: %w", err)
	}
	path := filepath.Join(worktreeRoot, relative)
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("lstat precious path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "unique", fmt.Errorf("resolve precious symlink: %w", err)
		}
		rel, err := filepath.Rel(worktreeRoot, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "unique", nil
		}
		return "symlink-to-external", nil
	}
	if !info.Mode().IsRegular() {
		return "unique", nil
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "unique", fmt.Errorf("resolve precious parent: %w", err)
	}
	if rel, err := filepath.Rel(worktreeRoot, resolvedParent); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "unique", nil
	}
	canonicalRoot, err := filepath.EvalSymlinks(canonical)
	if err != nil {
		return "unique", fmt.Errorf("resolve canonical worktree: %w", err)
	}
	canonicalPath := filepath.Join(canonicalRoot, relative)
	canonicalInfo, err := os.Lstat(canonicalPath)
	if os.IsNotExist(err) {
		return "unique", nil
	}
	if err != nil {
		return "unique", fmt.Errorf("lstat canonical copy: %w", err)
	}
	if !canonicalInfo.Mode().IsRegular() {
		return "unique", nil
	}
	want, err := os.ReadFile(canonicalPath)
	if err != nil {
		return "unique", fmt.Errorf("read canonical copy: %w", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return "unique", fmt.Errorf("read precious path: %w", err)
	}
	if bytes.Equal(got, want) {
		return "duplicate-of-canonical", nil
	}
	return "unique", nil
}

func newSweepInventoryFixture(t *testing.T) (root, repo, configDir string) {
	t.Helper()
	root = t.TempDir()
	repo = filepath.Join(root, "main")
	gitTest(t, root, "init", "-b", "main", repo)
	gitTest(t, repo, "config", "user.name", "Synthetic Sweep")
	gitTest(t, repo, "config", "user.email", "sweep@example.invalid")
	gitTest(t, repo, "config", "commit.gpgSign", "false")
	commitFile(t, repo, "tracked.txt", "base\n", "base")

	unmanaged := filepath.Join(root, "unmanaged")
	gitTest(t, repo, "worktree", "add", "-b", "unmanaged", unmanaged, "HEAD")
	vanished := filepath.Join(root, "vanished")
	gitTest(t, repo, "worktree", "add", "--detach", vanished, "HEAD")
	gitTest(t, repo, "worktree", "lock", vanished)
	if err := os.RemoveAll(vanished); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(root, "stray")
	gitTest(t, root, "init", "-b", "main", stray)

	missingWorker := filepath.Join(root, "missing-worker")
	openWorker := filepath.Join(root, "open-worker")
	gitTest(t, repo, "worktree", "add", "-b", "open-worker", openWorker, "HEAD")
	configDir = filepath.Join(root, "config")
	if err := os.Mkdir(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	workerRows := strings.Join([]string{
		"ws\tmissing\t" + missingWorker + "\tT-missing",
		"ws\topen\t" + openWorker + "\tT-open",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(configDir, "workers.tsv"), []byte(workerRows), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	for _, report := range []config.ReportRecord{
		{ReportID: "report-literal-zero", GroupID: "sweep-test", MemberThread: "T-missing", Status: config.ReportBlocked},
		{ReportID: "report-open", GroupID: "sweep-test", MemberThread: "T-open", Status: config.ReportBlocked},
		{ReportID: "report-unbound", GroupID: "sweep-test", MemberThread: "T-unbound", Status: config.ReportReady},
	} {
		report.SchemaVersion = config.ReportsSchemaVersion
		report.RequestHash = config.ReportRequestHash(report.GroupID, report.MemberThread, "", "")
		report.CreatedAt, report.UpdatedAt = now, now
		if _, err := config.SubmitReport(filepath.Join(configDir, "reports.json"), report); err != nil {
			t.Fatal(err)
		}
	}
	return root, repo, configDir
}

func buildSweepAmux(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "amux")
	cmd := exec.Command("go", "build", "-o", path, "./cmd/amux")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build sweep amux validator: %v\n%s", err, out)
	}
	return path
}

func runSweepInventory(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("python3", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return string(out), exit.ExitCode()
	}
	t.Fatalf("run sweep inventory: %v", err)
	return "", -1
}

func parseSweepHumanRows(t *testing.T, output string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if !strings.HasPrefix(line, "ROW\t") {
			continue
		}
		row := map[string]any{}
		for _, field := range strings.Split(strings.TrimPrefix(line, "ROW\t"), "\t") {
			key, raw, ok := strings.Cut(field, "=")
			if !ok {
				t.Fatalf("malformed human field %q", field)
			}
			var value any
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatalf("decode human field %q: %v", field, err)
			}
			row[key] = value
		}
		rows = append(rows, row)
	}
	return rows
}

func equalJSONValue(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return value.(string)
}

func TestPublicDocsDescribeNarrowProjectlessSpawnException(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, path := range []string{"README.md", filepath.Join("skills", "amux", "SKILL.md"), filepath.Join("skills", "amux", "reference", "commands.md"), filepath.Join("skills", "amux", "reference", "workflows.md")} {
		contents := strings.ToLower(readSkillFile(t, root, path))
		for _, required := range []string{"projectless", "physical", "runner", "amux lifecycle"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s does not describe narrow spawn term %q", path, required)
			}
		}
		if !strings.Contains(contents, "owner-authoriz") && !strings.Contains(contents, "owner authoriz") {
			t.Errorf("%s does not require owner authorization", path)
		}
	}
}
