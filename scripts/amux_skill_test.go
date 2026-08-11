package scripts_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
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
	filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"),
	filepath.Join("skills", "amux", "reference", "contract-v1.md"),
	filepath.Join("skills", "amux", "reference", "deadline-v1.md"),
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
	for _, required := range []string{"explicit executor", "known linked ChatGPT subscription", "small mechanical work", "ordinary implementation", "hard architecture", "Amp-native `runner(id)`", "does not return a prompt digest", "do not claim exactly-once delivery"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("native creation workflow is missing %q", required)
		}
	}
	if strings.Contains(claude, "amp-invocation-policy") {
		t.Error("independent Claude route unexpectedly loads invocation policy")
	}
}

func TestNativeAdoptionDoesNotClaimExecutorMigration(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	adr := readSkillFile(t, root, filepath.Join("docs", "adr", "0003-native-thread-creation-and-explicit-adoption.md"))
	readme := readSkillFile(t, root, "README.md")
	for path, check := range map[string]struct {
		contents string
		required []string
	}{
		"workflow": {workflow, []string{"Orb creation followed by physical adoption as migration", "Adoption neither changes nor verifies continued affinity", "owner-supplied workdir", "preserves legacy catalog spelling", "execution affinity as `unknown`"}},
		"ADR 0003": {adr, []string{"Adoption does not re-home, migrate, or retarget", "does not verify continued affinity", "admission-canonicalized workdir", "authoritative catalog spelling unchanged", "execution affinity as `unknown`"}},
		"README":   {readme, []string{"Adoption never re-homes the thread or proves where future turns run", "legacy relative value is not a canonical or physical-location claim", "owner-supplied canonical workdir", "execution affinity as `unknown`"}},
	} {
		for _, required := range check.required {
			if !strings.Contains(check.contents, required) {
				t.Errorf("%s is missing explicit native-affinity boundary %q", path, required)
			}
		}
	}
}

func TestDurableTaskGroupLeadTitleGuidanceIsPresent(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{"Every durable task-group Lead title starts with `🎖️ `", "never deliberately apply it to member workers", "presentation only", "neither executor placement nor authoritative group role", "amp threads rename", "create no replacement", "stop before group or adoption mutations"} {
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
	stages := []string{"### 1. Preflight authoritative state and bootstrap the CLI", "### 2. Declare the group and register the verified coordinator lease", "### 3. Native-create and adopt the authoritative thread", "### 4. Persist ready, wake, acknowledge, and independently verify", "### 5. Merge, verify post-merge CI, then authorize finish", "### 6. Submit merged and run `/amux finish`", "### 7. Coordinator-owned deadline queue"}
	last := -1
	for _, stage := range stages {
		at := strings.Index(workflow, stage)
		if at <= last {
			t.Errorf("coordinator stage missing or out of order: %q", stage)
		}
		last = at
	}
	for _, required := range []string{
		"native parent/sub-issue/blocked-by/blocking relationships", "fresh `origin/main`", "issue-unprefixed semantic window", "worker adopt", "--group <durable-issue-group>",
		"amux --json callback register", "amux report submit --report-id <stable-report-id>", "amux report pending --group <durable-issue-group>", "amux report acknowledge --report-id <stable-report-id>",
		"PR URL, head branch/SHA", "amux report authorize-finish --report-id <stable-report-id>", "post-merge CI", "--status merged", "amux teardown --thread <member-thread>", "Group membership and report history survive teardown",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("coordinator workflow is missing %q", required)
		}
	}
}

func TestIssueCoordinationPreservesAndConfiguresDurableIdentity(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	readme := readSkillFile(t, root, "README.md")
	for _, required := range []string{"issue-bearing branch/worktree", "issue-unprefixed semantic window", "exact durable group", "stable report ID", "immutable coordination input"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("workflow is missing durable identity rule %q", required)
		}
	}
	for _, required := range []string{"amux group declare --group amux-131", "--report-id amux-133-worker-1 --group amux-133", "explicit local adoption"} {
		if !strings.Contains(readme, required) {
			t.Errorf("README is missing durable identity example %q", required)
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
	for _, required := range []string{"worker adopt", "exact group", "stable report ID"} {
		if !strings.Contains(current, required) {
			t.Errorf("current native workflow is missing explicit identity %q", required)
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
		"existing Tycho agent, project, harness, and model/provider route",
		"Freeze the task and binding",
		"restart-safe custody",
		"Run through Tycho, not as Tycho",
		"Submit one semantic report",
		"Recover explicitly",
		"Consume, assess, then acknowledge",
		"notification as wake-up only",
		"created-only receipt",
		"cleanup `pending`",
		"Evidence and promotion limits",
		"Tycho may route Claude or Pi",
		"does not grant Tycho Claude/Pi provider identity",
		"not bridge attestation of project, harness, provider, or model identity",
		"Migrating pre-split receipts",
		"custody possession never transfer coordinator, consume, or acknowledgement authority",
		"a single one-time Amp schedule",
		"only re-checks the exact bound local Tycho agent's status/result",
		"Clear it as soon as the run reaches a terminal or recovered state",
		"only a wake-up token—never durable truth, delivery, consume, or acknowledgement",
		"Do not turn it into a recurring watcher",
		"Authoritative Amp `/team-review` with one Opus second opinion",
		"reference/team-review-second-opinion.md",
		"Practical usability and #323 closure are separate claims",
		"Provider stop or exit without `submit`",
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
		"two useful real cycles",
		"process exit, logs, and prose never substitute",
		"team-review-second-opinion.md",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("experimental Tycho contract is missing %q", required)
		}
	}
	if !strings.Contains(matrix, "| `/amux-tycho` semantic-report receipt/inbox | Conditional |") ||
		!strings.Contains(matrix, "Tycho has `report_only` authority") ||
		!strings.Contains(matrix, "existing Tycho agent/project/harness/model route") ||
		!strings.Contains(matrix, "practically established") ||
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
		"Same-head proof across Amp and Tycho worktrees",
		"Stale / concurrent PENDING-review generation protection",
		"Exact evidence required for #323 to count a cycle",
		"Authoritative Amp first pass",
		"Create the receipt **before** Tycho execution",
		"exactly one durable `submit`",
		"Independently reproduce or reject **every** candidate",
		"Tycho must never call GitHub review or comment mutation APIs",
		"Path or directory-name equality is not identity",
		"full 40-character commit SHA",
		"HEAD^{tree}",
		"both Amp and Tycho review worktrees must be clean",
		"Stop rather than overwrite",
		"Exit codes (including `143`)",
		"no Tycho finding",
		"normally exact `claude-opus-5`",
		"Six PR #11886 gaps",
		"promote `/amux-tycho`",
		"close [#323](https://github.com/zainfathoni/amux/issues/323)",
		"does not widen stable Amux core",
		"tycho-report-bridge.md",
		"authority: \"report_only\"",
		"Publication",
		"Wake-ups and schedules never imply them",
		"Desire to mark field-proven",
		"refused here",
		// Application report invariants beyond generic bridge schema.
		"`complete` must use `blockers: []`",
		"non-empty for both statuses",
		"application-invalid",
		// Producer-only GitHub boundary.
		"GitHub credentials intended for review mutation",
		"no new GitHub write credentials",
		// Same-head task freeze includes route identity.
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
		// Same-head timing + helper non-attestation.
		"Post-Tycho / pre-consume",
		"bridge helper does **not** attest Git state",
		"reject the application payload",
		// #323 evidence completeness.
		"#328-specific #327 prerequisite",
		"accepted and merged",
		"does not block generic `/amux-tycho` use or #323 credit",
		"Pre-Tycho",
		"Post-Tycho / pre-consume** same-head proof",
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
	if !strings.Contains(skill, "#327") || !strings.Contains(skill, "does not block generic `/amux-tycho` use or #323 field credit") {
		t.Error("amux-tycho SKILL.md must scope #327 to the #328 local-worker workflow")
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
	if !strings.Contains(matrix, "blocks only") ||
		!strings.Contains(matrix, "#327") ||
		!strings.Contains(matrix, "#328") ||
		!strings.Contains(matrix, "not generic `/amux-tycho` use or") {
		t.Error("readiness matrix must scope #327 to #328 rather than generic use or #323 credit")
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

func TestSkillDrivenSpawnCommandsUseExplicitMode(t *testing.T) {
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
	for _, required := range []string{"MUST pass an explicit mode", "linked ChatGPT subscription", "target-mode availability", "small mechanical work", "ordinary implementation", "hard architecture", "An explicitly requested mode always wins", "special modes remain explicit-only"} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md is missing spawn policy %q", required)
		}
	}
}

func TestSprawlContractUsesDedicatedSemanticWorkers(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{
		"native dependency",
		"one dedicated branch/worktree",
		"issue-unprefixed semantic window",
		"task-only assignment",
		"native-created thread",
		"worker adopt",
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
	worktreeRemoval := strings.Index(workflow, "Remove the clean worker worktree")
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

func TestContractV1IsProgressivelyDisclosedForWorkers(t *testing.T) {
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
		"read this file once",
		"absolute path",
		"never a bare relative path",
		"ready",
		"blocked",
		"merged",
		"never authorize finish",
		"/amux-claude",
		"known linked ChatGPT subscription",
		"small mechanical tasks",
		"premium or special modes require an exact owner request",
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
	if !strings.Contains(workflow, "contract-v1.md") || !strings.Contains(workflow, "task-only") {
		t.Error("workflows must require task-only prompts and contract-v1 read-once")
	}
	for _, required := range []string{"absolute path", "one-time read"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("spawn message template must resolve the contract path for the worker: missing %q", required)
		}
	}
	for _, required := range []string{
		"path to the loaded skill",
		"never a bare relative path",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md spawn routing must require absolute contract path: missing %q", required)
		}
	}
	// The coordinator work-group route is linked directly from SKILL.md, so it must
	// carry the mandatory contract read line itself rather than relying on a reader
	// having scrolled through the sprawl section's message requirements.
	coordinateAt := strings.Index(workflow, "## Coordinate a durable issue work group")
	if coordinateAt < 0 {
		t.Fatal("coordinator work-group workflow is missing")
	}
	coordinate := workflow[coordinateAt:]
	if healthAt := strings.Index(coordinate, "## Health workers and runners"); healthAt > 0 {
		coordinate = coordinate[:healthAt]
	}
	for _, required := range []string{"contract-v1.md", "task-only assignment"} {
		if !strings.Contains(coordinate, required) {
			t.Errorf("coordinator work-group spawn must carry the contract read requirement: missing %q", required)
		}
	}
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	if !strings.Contains(triggers, "absolute path to contract-v1") {
		t.Error("trigger checklist must require absolute contract-v1 path on spawn")
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

func TestPublicDocsDescribeNarrowProjectlessSpawnException(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, path := range []string{"README.md", filepath.Join("skills", "amux", "SKILL.md"), filepath.Join("skills", "amux", "reference", "commands.md"), filepath.Join("skills", "amux", "reference", "workflows.md")} {
		contents := readSkillFile(t, root, path)
		for _, required := range []string{"projectless", "physical", "runner"} {
			if !strings.Contains(strings.ToLower(contents), required) {
				t.Errorf("%s does not describe narrow spawn term %q", path, required)
			}
		}
	}
}
