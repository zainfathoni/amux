package scripts_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
				"amp-invocation-policy.md",
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

func legacyTestOrdinaryIssuePRChecklistStaysDocsOnlyAndLean(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	checklist := readSkillFile(t, root, filepath.Join("docs", "ordinary-issue-pr-checklist.md"))

	// ADR 0007/0008: ordinary issue/PR guidance must not activate /amux coordination state.
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
		"#249",
		"#328",
		"#344",
		"#331",
		"#339",
		"staged-drain-disposition",
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

func legacyTestInvocationPolicyIsProgressivelyDisclosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	policy := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
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
	for _, required := range []string{"exact Workspace Project and Orb", "known linked ChatGPT subscription", "small mechanical work", "ordinary implementation", "hard architecture", "one exact runner ID", "`runner_id` argument", "not an Amux CLI flag", "parent/child route", "automatic adoption"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("native creation workflow is missing %q", required)
		}
	}
}

func legacyTestNativeCreationDoesNotAdoptOrClaimExecutorMigration(t *testing.T) {
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

func TestRunnerIDDesignDoesNotDisplaceRetainedAmuxLaunch(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	design := readSkillFile(t, root, filepath.Join("docs", "proposals", "runner-id-design-grill.md"))
	if !strings.HasPrefix(design, "---\nstatus: superseded\nsuperseded-by: ../adr/0008-retain-machine-local-runner-host-and-drain-coordination.md\n---\n") {
		t.Error("runner-ID design must be marked superseded by ADR 0008")
	}
	for _, required := range []string{
		"Do not implement issues #212–#216 as an Amux feature graph",
		"do not add",
		"Amux `--runner-id` selector",
		"Canonical workdir remains the identity of a retained Amux runner",
		"amp --no-tui --runner-id <stable-owner-selected-id>",
		"amux runner pin --runner-id <id>",
		"Launch and exact-process validation require that exact argument",
		"native `create_thread` `runner_id` argument",
		"systemd/launchd activates `amux launch --all`",
		"No separate runner-ID store, generated-ID migration framework, lifecycle classifier, or runner-ID selector is required",
	} {
		if !strings.Contains(design, required) {
			t.Errorf("retained-launch runner-ID disposition is missing %q", required)
		}
	}
}

func TestNativeRunnerConfigurationDirectionIsUnambiguous(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	adr7 := readSkillFile(t, root, filepath.Join("docs", "adr", "0007-retire-amux-through-native-cutover-and-staged-drain.md"))
	adr8 := readSkillFile(t, root, filepath.Join("docs", "adr", "0008-retain-machine-local-runner-host-and-drain-coordination.md"))
	adr11 := readSkillFile(t, root, filepath.Join("docs", "adr", "0011-consolidate-on-native-multi-directory-runners.md"))
	adr5 := readSkillFile(t, root, filepath.Join("docs", "adr", "0005-maintain-amux-as-a-local-worker-lifecycle-and-recovery-tool.md"))
	adr6 := readSkillFile(t, root, filepath.Join("docs", "adr", "0006-bound-thread-delegation-and-require-preservation-before-retirement.md"))
	readme := readSkillFile(t, root, "README.md")
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	ledger := readSkillFile(t, root, filepath.Join("docs", "staged-drain-disposition.md"))
	homepage := readSkillFile(t, root, filepath.Join("docs", "index.html"))
	skillGuide := readSkillFile(t, root, filepath.Join("docs", "skill", "index.html"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	active := adr11 + readme + skill + ledger

	for name, check := range map[string]struct {
		contents string
		markers  []string
	}{
		"ADR 0011": {adr11, []string{
			"native multi-directory runners",
			"`native-runners.json`",
			"`amux runner service install`",
			"systemd user services or launchd agents",
			"invokes Amp directly",
			"not a resident supervisor",
		}},
		"README": {readme, []string{
			"declarative machine-local configuration",
			"`native-runners.json`",
			"amux runner service install",
			"Obsidian vault",
			"executes the resolved Amp binary directly",
		}},
		"skill": {skill, []string{
			"native Amp multi-directory runner profiles",
			"`native-runners.json`",
			"`amux runner service install|remove|doctor`",
			"Historical worker/coordination files are inert",
		}},
		"ledger": {ledger, []string{
			"Native Amp owns task coordination, multi-directory runner execution, and native update behavior",
			"`native-runners.json`",
			"`amux runner service install|remove|doctor`",
			"executes the resolved Amp binary directly",
			"preflight-only",
			"not being fully deprecated or archived",
		}},
	} {
		for _, marker := range check.markers {
			if !strings.Contains(check.contents, marker) {
				t.Errorf("%s thin-host direction is missing %q", name, marker)
			}
		}
	}
	for name, check := range map[string]struct {
		contents string
		markers  []string
	}{
		"ADR 0008": {adr8, []string{
			"separate explicit owner request",
			"After that run",
			"accepts one complete inventory",
			"explicitly dispositions an incomplete/error result",
			"confirms no repeat is needed",
			"only then",
			"deleted before the next release",
		}},
		"ledger": {ledger, []string{
			"separate explicit owner request",
			"After that separately authorized run",
			"accepts one complete inventory",
			"explicitly dispositions an incomplete/error result",
			"confirms no repeat is needed",
			"only then",
			"before the next release",
		}},
	} {
		previous := -1
		for _, marker := range check.markers {
			index := strings.Index(check.contents, marker)
			if index < 0 {
				t.Errorf("%s is missing #360 gate %q", name, marker)
				continue
			}
			if index <= previous {
				t.Errorf("%s has out-of-order #360 gate %q", name, marker)
			}
			previous = index
		}
	}
	for _, forbidden := range []string{
		"2026-09-01 cutover date",
		"2026-11-30 reader window begins",
		"Runner admission closes",
	} {
		if strings.Contains(active, forbidden) {
			t.Errorf("active thin-host guidance retains obsolete direction %q", forbidden)
		}
	}
	if !strings.HasPrefix(adr7, "---\nstatus: partially-superseded\n") || !strings.Contains(adr7, "partially-superseded-by: 0008, 0009") {
		t.Error("ADR 0007 must be explicitly partially superseded by ADRs 0008 and 0009")
	}
	for name, contents := range map[string]string{"ADR 0005": adr5, "ADR 0006": adr6} {
		for _, required := range []string{"ADR 0009", "stores inert", "No imperative statement"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s current callout is missing %q", name, required)
			}
		}
	}
	for name, contents := range map[string]string{
		"README":        readme,
		"skill":         skill,
		"trigger table": triggers,
		"homepage":      homepage,
		"skill guide":   skillGuide,
	} {
		if strings.Contains(contents, "amux worker pin") {
			t.Errorf("%s must not actively route new worker pin admission", name)
		}
	}
	for name, contents := range map[string]string{"skill": skill, "trigger table": triggers, "skill guide": skillGuide} {
		if !strings.Contains(contents, "Pin this runner") {
			t.Errorf("%s must document transitional runner pin admission", name)
		}
	}
	for name, contents := range map[string]string{
		"skill description": skill[:strings.Index(skill, "\n---\n")],
		"skill routing":     skill,
		"trigger table":     triggers,
	} {
		if !strings.Contains(contents, "runner service") && !strings.Contains(contents, "Runner service") {
			t.Errorf("%s must route native runner service management", name)
		}
	}
	for _, forbidden := range []string{"amux worker adopt", "amux group declare", "amux report submit"} {
		frontmatterEnd := strings.Index(skill, "\n---\n")
		if frontmatterEnd < 0 {
			t.Fatal("SKILL.md missing frontmatter terminator")
		}
		if strings.Contains(skill[:frontmatterEnd], forbidden) {
			t.Errorf("/amux routing frontmatter revives legacy coordination %q", forbidden)
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
		"ADR 0009](0009-remove-active-legacy-coordination-surfaces.md) removes active adoption and core coordination transitions",
		"historical operation and worker-store bytes are inert evidence",
		"Do not use an older binary, edit stores, or native-create then adopt, group, report, or otherwise enroll work into Amux",
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

func legacyTestDurableTaskGroupLeadTitleGuidanceIsPresent(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{"Every durable task-group Lead title starts with `🎖️ `", "never deliberately apply it to member workers", "presentation only", "neither executor placement nor authoritative group role", "Do not rename a thread merely to drain it", "Existing presentation metadata conveys no new authority"} {
		if !strings.Contains(workflow, required) {
			t.Errorf("durable task-group Lead title guidance is missing %q", required)
		}
	}
}

func legacyTestReadThreadDiscrepancyRecoveryContractStaysAligned(t *testing.T) {
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

func legacyTestCoordinatorWorkflowMatchesDurableCLIContract(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	stages := []string{"### 1. Preflight existing authoritative state", "### 2. Revalidate the existing coordinator lease", "### 3. Continue only the existing member", "### 4. Persist ready, wake, acknowledge, and independently verify", "### 5. Merge, verify post-merge CI, then authorize finish", "### 6. Run `/amux finish`; submit merged inside the approved wind-down", "### 7. Coordinator-owned deadline queue"}
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
		"PR URL, head branch/SHA", "amux report authorize-finish --report-id <stable-report-id>", "post-merge CI", "--status merged", "the child remains alive", "does not wait for a separate acknowledgement round trip", "amux teardown --thread <member-thread>", "Group membership and report history survive teardown",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("coordinator workflow is missing %q", required)
		}
	}
}

func legacyTestIssueCoordinationUsesNativeIdentityAndDrainsExistingDurableIdentity(t *testing.T) {
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

func legacyTestConfigurableGroupNamingSourceReferencesStayConsistent(t *testing.T) {
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

func legacyTestWorkGroupCompletionsExposeImplementedCommands(t *testing.T) {
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

func TestDocumentedCommandTreeMatchesCLIHelp(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	checks := []struct {
		args []string
		want []string
	}{
		{args: []string{"help"}, want: []string{"launch", "list", "park", "restart", "remove", "doctor", "reconcile", "runner", "workspace", "workspaces"}},
		{args: []string{"help", "runner", "pin"}, want: []string{"--workspace, -w", "--workdir, -d", "--current"}},
		{args: []string{"help", "workspace", "list"}, want: []string{"List runner workspaces"}},
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

func legacyTestCoordinatorDeadlinePolicyIsConsistent(t *testing.T) {
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

func legacyTestCoordinatorDeadlineScheduleIsBoundedAndGenerationSafe(t *testing.T) {
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

func legacyTestCoordinatorSafetyAppearsInPublicReferences(t *testing.T) {
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

func legacyTestBoundedSpawnExceptionCommandsUseExplicitMode(t *testing.T) {
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
	coordinateAt := strings.Index(workflow, "## Health runners")
	if sprawlAt < 0 || coordinateAt <= sprawlAt {
		t.Fatal("sprawl workflow section is missing")
	}
	sprawl := workflow[sprawlAt:coordinateAt]
	for _, required := range []string{
		"explicitly asks",
		"independent issues",
		"native children only",
		"preserve native replies",
		"Do not create Amux coordination state",
	} {
		if !strings.Contains(sprawl, required) {
			t.Errorf("sprawl workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--window issue-<issue>", "--window #<issue>", "amux spawn", "worker adopt"} {
		if strings.Contains(sprawl, forbidden) {
			t.Errorf("sprawl workflow contains forbidden guidance %q", forbidden)
		}
	}
}

func legacyTestHealthAndFinishPreserveModeSafety(t *testing.T) {
	t.Parallel()
	workflow := readSkillFile(t, repoRoot(t), filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{
		"Health is aggregate by default",
		"mode=<worker|runner>",
		"Never send text to a runner pane",
		"no-response` means candidate stale, not safe to replace",
		"Any configured runner, unexpected Amp client",
		"Unavailable, incomplete, or ambiguous runner-list or host process evidence also blocks",
		"never escalates to `-D`",
		"run worker teardown as the final action",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("workflow safety contract is missing %q", required)
		}
	}
}

func legacyTestFinishRemovalGateDocumentsEveryFailClosedInvariant(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	reference := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "removal-safety.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "contract-v1.md"))
	combined := workflow + reference
	finishStart := strings.Index(workflow, "## Finish a completed worker")
	generalStart := strings.Index(workflow, "### Removal preflight for finish, remove-on-missing, and prune")
	if finishStart < 0 || generalStart <= finishStart {
		t.Fatal("completed-worker finish section is missing")
	}
	finish := workflow[finishStart:generalStart]
	for _, required := range []string{
		"Run every currently knowable read-only check",
		"**Merged PR:**",
		"**Review-only:**",
		"Do not fabricate merge, PR, CI, or report evidence, submit `merged`, or add a terminal report status",
		"ordinary completed-worker fast path",
		"present worktree with an index-only filesystem",
		"unique or unpushed commits remain reachable",
		"normal removal has no non-index content to discard",
		"current working directory or open file is under the worktree blocks",
		"amux --dry-run --json worker park --thread <thread-id>",
		"FINISH PREFLIGHT: PROCEED | BLOCKED",
		"one concise privacy-safe summary",
		"Ask once for approval",
		"not a durable finish plan",
		"declined/canceled approval before mutation produces no mutation",
		"existing Git, report, provider, and Amux outcomes are the replay evidence",
		"skip unlock for `unlocked`",
		"git worktree remove <exact-path>",
		"Never pass `--force`",
		"Preserve the local branch by default",
		"remote branch deletion is never implicit",
		"archives the exact completed thread",
		"review-only case retains its report unchanged",
		"require durable authorization from the exact current coordinator before entering the approved finish sequence",
		"Include only the already-authorized terminal `merged` transition",
		"exact terminal history when only the separate best-effort callback failed",
		"expected progress, not drift",
		"unrelated lifecycle change",
		"If the owner cancels or any evidence becomes ambiguous after the first mutation",
		"fresh complete preflight and approval",
		"no new runtime or filesystem lock is created",
		"Report actual outcomes rather than the intended plan",
	} {
		if !strings.Contains(finish, required) {
			t.Errorf("ordinary finish workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"git pull --ff-only", "git branch -D", "versioned finish-plan", "persist finish progress"} {
		if strings.Contains(finish, forbidden) {
			t.Errorf("ordinary finish workflow retains superseded ceremony %q", forbidden)
		}
	}
	preflight := strings.Index(finish, "### One read-only preflight and one approval")
	summary := strings.Index(finish, "FINISH PREFLIGHT: PROCEED | BLOCKED")
	approval := strings.Index(finish, "Ask once for approval")
	mutation := strings.Index(finish, "### Mutate narrowly, with adjacent revalidation")
	if mutation < 0 {
		t.Fatal("ordinary finish mutation section is missing")
	}
	mutationBody := finish[mutation:]
	park := strings.Index(mutationBody, "amux --json worker park --thread <thread-id>") + mutation
	transition := strings.Index(mutationBody, "Perform only the listed already-authorized terminal report transition and exact bound-provider dispositions") + mutation
	revalidation := strings.Index(mutationBody, "Re-run the complete ordinary preflight") + mutation
	remove := strings.Index(mutationBody, "git worktree remove <exact-path>") + mutation
	teardown := strings.LastIndex(finish, "amux teardown --thread <thread-id>")
	if preflight < 0 || summary <= preflight || approval <= summary || mutation <= approval || park <= mutation || transition <= park || revalidation <= transition || remove <= revalidation || teardown <= remove {
		t.Errorf("ordinary finish ordering must be preflight → summary → approval → park → exact transitions → revalidation → remove → teardown: preflight=%d summary=%d approval=%d mutation=%d park=%d transition=%d revalidation=%d remove=%d teardown=%d", preflight, summary, approval, mutation, park, transition, revalidation, remove, teardown)
	}
	authorize := strings.Index(workflow, "amux report authorize-finish --report-id <stable-report-id>")
	if authorize < 0 || authorize >= finishStart {
		t.Errorf("durable report authorization must occur before finish preflight: authorize=%d finish=%d", authorize, finishStart)
	}
	if strings.Contains(mutationBody, "amux report authorize-finish") {
		t.Error("finish mutation section must not create durable report authorization")
	}
	for _, required := range []string{
		"existing merged-PR report binding",
		"review-only assignment may instead finish from explicit owner completion and cleanup approval",
		"retain any review-only report unchanged",
		"parks a verified live worker before filesystem mutation",
		"does not manufacture a report or terminal transition for review-only work",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("finish contract is missing %q", required)
		}
	}
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
		"gh pr view <pr> --json state,mergedAt,headRefName,headRefOid",
		"review-only work records `PR: not applicable`",
		"unfiltered count and paths of `??` untracked entries",
		"symlink-to-external",
		"duplicate-of-canonical",
		"Generated-artifact exclusions are configurable presentation filters",
		"never count `refs/stash` as commit coverage",
		"Any scan or resolution error blocks ordinary removal",
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
	for _, required := range []string{
		"exact surviving local branch itself preserves every commit at `HEAD`",
		"GIT_OPTIONAL_LOCKS=0",
		"git ls-remote --heads <remote> <ref>",
		"without fetching or changing local refs",
		"status --porcelain=v1 --untracked-files=all",
		"ls-files --cached -v -z",
		"assume-unchanged",
		"skip-worktree",
		"ls-files --stage -z",
		"require every entry to be stage zero",
		"rather than trusting the stat cache",
		"Git owner-executable bit (`0100`)",
		"hash-object --no-filters -- <git-path>",
		"blocks a filtered, line-ending, encoding, or ident-transformed checkout",
		"hash-object --stdin",
		"unsupported modes (including gitlinks)",
		"Normal removal can also silently delete ignored content",
		"strictly index-only filesystem",
		"ls-files --cached -z",
		"walk the entire worktree with `lstat` without following symlinks",
		"exact linked-worktree `.git` administrative file",
		"ignored file or directory",
		"nested-repository or present submodule content",
		"a count or ignored-file listing is not a complete deletion inventory",
		"Independently remove or preserve generated ignored output",
		"`unlocked` skips unlock",
		"with no `--force`",
		"Branch deletion, remote mutation, prune, and backup-ref creation are outside this fast path",
	} {
		if !strings.Contains(reference, required) {
			t.Errorf("ordinary removal fast path is missing %q", required)
		}
	}
}

func TestOrdinaryFinishGitSemantics(t *testing.T) {
	t.Run("attached branch preserves unique commit after normal removal", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "attached-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-feature", worktree, "HEAD")
		registered := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "--show-toplevel"))
		commitFile(t, worktree, "unique.txt", "unique and unpushed\n", "unique finish commit")
		tip := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD"))
		if got := gitTestOptionalRef(t, repo, "refs/remotes/origin/finish-feature"); got != "" {
			t.Fatalf("synthetic finish commit unexpectedly has remote coverage: %s", got)
		}
		if err := syntheticOrdinaryFinishState(worktree); err != nil {
			t.Fatalf("ordinary finish state = %v, want clear", err)
		}

		gitTest(t, repo, "worktree", "remove", worktree)

		if got := gitTestOptionalRef(t, repo, "refs/heads/finish-feature"); got != tip {
			t.Fatalf("retained branch tip = %q, want unique commit %q", got, tip)
		}
		if _, err := os.Stat(worktree); !os.IsNotExist(err) {
			t.Fatalf("removed worktree stat = %v, want absent", err)
		}
		if block := syntheticWorktreeBlock(gitTest(t, repo, "worktree", "list", "--porcelain"), registered); block != "" {
			t.Fatalf("removed worktree remains registered: %q", block)
		}
	})

	t.Run("untracked content makes normal removal refuse", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "untracked-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-untracked", worktree, "HEAD")
		registered := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "--show-toplevel"))
		if err := os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("keep me\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		gitTestFailure(t, repo, "worktree", "remove", worktree)

		if _, err := os.Stat(filepath.Join(worktree, "untracked.txt")); err != nil {
			t.Fatalf("refused removal lost untracked content: %v", err)
		}
		if block := syntheticWorktreeBlock(gitTest(t, repo, "worktree", "list", "--porcelain"), registered); block == "" {
			t.Fatal("refused removal dropped the worktree registration")
		}
	})

	t.Run("ignored content is silently deleted by normal removal", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("generated.log\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitTest(t, repo, "add", ".gitignore")
		gitTest(t, repo, "commit", "-m", "ignore generated output")
		worktree := filepath.Join(t.TempDir(), "ignored-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-ignored", worktree, "HEAD")
		if err := os.WriteFile(filepath.Join(worktree, "generated.log"), []byte("silently lost\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if status := gitTest(t, worktree, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
			t.Fatalf("ignored fixture unexpectedly appears in ordinary status: %q", status)
		}
		if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "non-index filesystem object") {
			t.Fatalf("ordinary finish state = %v, want ignored-content blocker", err)
		}

		gitTest(t, repo, "worktree", "remove", worktree)

		if _, err := os.Stat(worktree); !os.IsNotExist(err) {
			t.Fatalf("normal removal with ignored content stat = %v, want absent", err)
		}
	})

	t.Run("assume unchanged can hide a modification and therefore blocks", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "assume-unchanged-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-assume-unchanged", worktree, "HEAD")
		gitTest(t, worktree, "update-index", "--assume-unchanged", "tracked.txt")
		if err := os.WriteFile(filepath.Join(worktree, "tracked.txt"), []byte("hidden modification\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if status := gitTest(t, worktree, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
			t.Fatalf("assume-unchanged fixture unexpectedly appears in status: %q", status)
		}
		if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "assume-unchanged") {
			t.Fatalf("index flag check = %v, want assume-unchanged blocker", err)
		}

		gitTest(t, repo, "worktree", "remove", worktree)
		if _, err := os.Stat(worktree); !os.IsNotExist(err) {
			t.Fatalf("normal removal with hidden tracked modification stat = %v, want absent", err)
		}
	})

	t.Run("skip worktree and non-stage-zero entries block", func(t *testing.T) {
		t.Run("skip-worktree", func(t *testing.T) {
			repo, _, _ := newRemovalSafetyRepo(t)
			worktree := filepath.Join(t.TempDir(), "skip-worktree-finish")
			gitTest(t, repo, "worktree", "add", "-b", "finish-skip-worktree", worktree, "HEAD")
			gitTest(t, worktree, "update-index", "--skip-worktree", "tracked.txt")
			if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "skip-worktree") {
				t.Fatalf("index flag check = %v, want skip-worktree blocker", err)
			}
		})

		t.Run("non-stage-zero", func(t *testing.T) {
			repo, _, _ := newRemovalSafetyRepo(t)
			worktree := filepath.Join(t.TempDir(), "unmerged-index-finish")
			gitTest(t, repo, "worktree", "add", "-b", "finish-unmerged-index", worktree, "HEAD")
			oid := strings.TrimSpace(gitTest(t, worktree, "rev-parse", "HEAD:tracked.txt"))
			indexInfo := fmt.Sprintf("0 %s\ttracked.txt\n100644 %s 1\ttracked.txt\n100644 %s 2\ttracked.txt\n", strings.Repeat("0", 40), oid, oid)
			gitTestInput(t, worktree, []byte(indexInfo), "update-index", "--index-info")
			if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "non-stage-zero") {
				t.Fatalf("index stage check = %v, want non-stage-zero blocker", err)
			}
		})
	})

	t.Run("stat cache false-clean tracked modification blocks", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		worktree := filepath.Join(t.TempDir(), "false-clean-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-false-clean", worktree, "HEAD")
		gitTest(t, worktree, "config", "core.trustctime", "false")
		gitTest(t, worktree, "config", "core.checkStat", "minimal")

		tracked := filepath.Join(worktree, "tracked.txt")
		fixedTime := time.Unix(1_600_000_000, 123_456_789)
		if err := os.Chtimes(tracked, fixedTime, fixedTime); err != nil {
			t.Fatal(err)
		}
		gitTest(t, worktree, "update-index", "--refresh")
		if err := os.WriteFile(tracked, []byte("evil\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(tracked, fixedTime, fixedTime); err != nil {
			t.Fatal(err)
		}
		if status := gitTest(t, worktree, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
			t.Fatalf("stat-cache fixture unexpectedly appears in status: %q", status)
		}
		if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "content differs from index") {
			t.Fatalf("index content check = %v, want false-clean content blocker", err)
		}
	})

	t.Run("lossy clean filter cannot hide raw worktree bytes", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		gitTest(t, repo, "config", "filter.lossy.clean", "sed 's/^LOCAL:/INDEX:/'")
		gitTest(t, repo, "config", "filter.lossy.smudge", "cat")
		gitTest(t, repo, "config", "filter.lossy.required", "true")
		if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("filtered.txt filter=lossy\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "filtered.txt"), []byte("INDEX: unique worktree bytes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitTest(t, repo, "add", ".gitattributes", "filtered.txt")
		gitTest(t, repo, "commit", "-m", "add lossy filtered file")

		worktree := filepath.Join(t.TempDir(), "filtered-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-filtered", worktree, "HEAD")
		if err := os.WriteFile(filepath.Join(worktree, "filtered.txt"), []byte("LOCAL: unique worktree bytes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if status := gitTest(t, worktree, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
			t.Fatalf("lossy-filter fixture unexpectedly appears in status: %q", status)
		}
		indexOID := strings.TrimSpace(gitTest(t, worktree, "rev-parse", ":filtered.txt"))
		filteredOID := strings.TrimSpace(gitTest(t, worktree, "hash-object", "--path=filtered.txt", "--", "filtered.txt"))
		if filteredOID != indexOID {
			t.Fatalf("filtered worktree OID = %q, want deceptive index match %q", filteredOID, indexOID)
		}
		if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "content differs from index") {
			t.Fatalf("raw index content check = %v, want lossy-filter byte blocker", err)
		}
	})

	t.Run("filemode false owner executable mismatch blocks", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		gitTest(t, repo, "config", "core.fileMode", "true")
		executable := filepath.Join(repo, "executable.sh")
		if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(executable, 0o755); err != nil {
			t.Fatal(err)
		}
		gitTest(t, repo, "add", "executable.sh")
		gitTest(t, repo, "commit", "-m", "add executable")

		worktree := filepath.Join(t.TempDir(), "filemode-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-filemode", worktree, "HEAD")
		gitTest(t, worktree, "config", "core.fileMode", "false")
		executable = filepath.Join(worktree, "executable.sh")
		if err := os.Chmod(executable, 0o450); err != nil {
			t.Fatal(err)
		}
		if status := gitTest(t, worktree, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
			t.Fatalf("fileMode=false fixture unexpectedly appears in status: %q", status)
		}
		if err := syntheticOrdinaryFinishState(worktree); err == nil || !strings.Contains(err.Error(), "executable mode differs") {
			t.Fatalf("index mode check = %v, want owner-executable mismatch blocker", err)
		}
	})

	t.Run("locked requires unlock and unlocked removes directly", func(t *testing.T) {
		repo, _, _ := newRemovalSafetyRepo(t)
		locked := filepath.Join(t.TempDir(), "locked-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-locked", locked, "HEAD")
		lockedRegistered := strings.TrimSpace(gitTest(t, locked, "rev-parse", "--show-toplevel"))
		gitTest(t, repo, "worktree", "lock", locked)
		gitTestFailure(t, repo, "worktree", "remove", locked)
		if block := syntheticWorktreeBlock(gitTest(t, repo, "worktree", "list", "--porcelain"), lockedRegistered); !strings.Contains(block, "\nlocked") {
			t.Fatalf("locked worktree block = %q", block)
		}
		gitTest(t, repo, "worktree", "unlock", locked)
		gitTest(t, repo, "worktree", "remove", locked)

		unlocked := filepath.Join(t.TempDir(), "unlocked-finish")
		gitTest(t, repo, "worktree", "add", "-b", "finish-unlocked", unlocked, "HEAD")
		unlockedRegistered := strings.TrimSpace(gitTest(t, unlocked, "rev-parse", "--show-toplevel"))
		if block := syntheticWorktreeBlock(gitTest(t, repo, "worktree", "list", "--porcelain"), unlockedRegistered); block == "" || strings.Contains(block, "\nlocked") {
			t.Fatalf("ordinary worktree unexpectedly locked: %q", block)
		}
		gitTest(t, repo, "worktree", "remove", unlocked)
		if _, err := os.Stat(unlocked); !os.IsNotExist(err) {
			t.Fatalf("direct unlocked removal stat = %v, want absent", err)
		}
	})
}

func legacyTestBackupRemovalRefsContractIsNarrowAndFailClosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
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
		"rerun the complete preflight",
		"ignore only that expected exact ref",
		"including rows that need no backup",
		"never removes files or worktrees, unlocks, prunes, deletes branches",
	} {
		if !strings.Contains(reference, required) {
			t.Errorf("removal-safety reference is missing %q", required)
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

	for _, test := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "authoritative report validation rejects invalid report only",
			mutate: func(document map[string]any) {
				document["reports"].([]any)[0].(map[string]any)["schema_version"] = float64(2)
			},
		},
		{
			name: "authoritative report validation rejects invalid deadline only",
			mutate: func(document map[string]any) {
				document["deadlines"] = []any{map[string]any{"group_id": "bad"}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, repo, configDir := newSweepInventoryFixture(t)
			path := filepath.Join(configDir, "reports.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			test.mutate(document)
			data, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", buildSweepAmux(t), "--filesystem-root", root, "--json")
			if exit != 2 || !strings.Contains(output, "authoritative validation failed") || !strings.Contains(output, `"complete":false`) {
				t.Fatalf("invalid authoritative store did not fail closed: exit=%d\n%s", exit, output)
			}
		})
	}

	t.Run("report replacement after snapshot fails closed", func(t *testing.T) {
		root, repo, configDir := newSweepInventoryFixture(t)
		realAmux := buildSweepAmux(t)
		wrapper := filepath.Join(t.TempDir(), "amux-swap-wrapper")
		wrapperBody := "#!/bin/sh\nif [ \"$3\" = \"__sweep-validate-reports\" ]; then printf '%s\\n' '{\"schema_version\":1,\"reports\":[],\"deadlines\":[]}' > \"$2/reports.json\"; fi\nexec " + strconv.Quote(realAmux) + " \"$@\"\n"
		if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o755); err != nil {
			t.Fatal(err)
		}
		output, exit := runSweepInventory(t, filepath.Join(repoRoot(t), "skills", "amux", "scripts", "sweep-inventory"), "--repo", repo, "--config-dir", configDir, "--amux", wrapper, "--filesystem-root", root, "--json")
		if exit != 2 || !strings.Contains(output, "reports.json changed after sweep snapshot") || !strings.Contains(output, `"complete":false`) {
			t.Fatalf("post-snapshot replacement did not fail closed: exit=%d\n%s", exit, output)
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
		"delete `scripts/sweep-inventory`, its sweep-only tests, and every `/amux sweep` route/reference before the next Amux release",
		"Do not promote the helper into the CLI, retain it as a standing diagnostic, schedule it, or extend it for post-drain monitoring",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("sweep contract lacks %q", required)
		}
	}
	previous := -1
	for _, marker := range []string{
		"This documentation does not authorize a run",
		"a future run requires a separate explicit owner request",
		"After that authorization",
		"after the owner records acceptance of one complete staged-drain inventory",
		"explicit incomplete/error disposition",
		"confirms that no repeat inventory is required",
		"delete `scripts/sweep-inventory`, its sweep-only tests, and every `/amux sweep` route/reference before the next Amux release",
	} {
		index := strings.Index(workflow, marker)
		if index < 0 {
			t.Errorf("sweep workflow is missing ordered #360 gate %q", marker)
			continue
		}
		if index <= previous {
			t.Errorf("sweep workflow has out-of-order #360 gate %q", marker)
		}
		previous = index
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

func legacyTestActivePublicSurfacesLabelLegacyLifecycleAndNativeChildren(t *testing.T) {
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
			"Current new task coordination uses native child threads. Machine-local runner registry, launch, maintenance, and diagnostics remain active Amux features; worker/group/report/callback coordination is compatibility/drain-only.",
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
			"retained machine-local Amux runner registry",
			"Routes all new task coordination to native Amp child threads",
			"legacy coordination state",
			"pre-cutover completed-worker compatibility/drain only",
		},
		"trigger checklist": {
			"`Coordinate child threads`",
			"Skill-only pre-cutover completed-worker drain",
		},
		"workflow source": {
			"Coordinate native child threads and drain a durable work group",
			"compatibility-only for a durable group, member worker, callback, and report identity that already exist",
			"Finish is compatibility/drain-only wind-down for an existing pre-cutover worker",
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

func legacyTestNativeCreatePromptExamplesExcludeAmuxLifecycle(t *testing.T) {
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

func legacyTestContractV1IsLimitedToProvenLegacyDrainWorkers(t *testing.T) {
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

func legacyTestGlobalAgentsSnippetSeparatesNativeWorkFromLegacyDrain(t *testing.T) {
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
		"Forgex is experimental and requires my explicit request",
	} {
		if !strings.Contains(snippet, required) {
			t.Errorf("global AGENTS replacement snippet is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"Use Amux for local Amp/tmux lifecycle, exact adoption/recovery, and when native creation is unavailable",
		"task, acceptance criteria, and the absolute path to the loaded `/amux` skill's `reference/contract-v1.md`",
	} {
		if strings.Contains(snippet, forbidden) {
			t.Errorf("global AGENTS replacement snippet retains generic injection policy %q", forbidden)
		}
	}
}

func legacyTestDescriptionCarriesNaturalLanguageSynonymTriggers(t *testing.T) {
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

func TestCoreAmuxIsTheOnlyShippedSkill(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	// A skill bundle has a SKILL.md entry point; leftover caches are not shipped skills.
	skills, err := filepath.Glob(filepath.Join(root, "skills", "*", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "skills", "amux", "SKILL.md")
	if len(skills) != 1 || skills[0] != want {
		t.Fatalf("shipped skill entry points = %v, want only %s", skills, want)
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

func gitTestFailure(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git -C %s %s unexpectedly succeeded\n%s", dir, strings.Join(args, " "), out)
	}
	return string(out)
}

func syntheticOrdinaryFinishState(worktree string) error {
	if err := syntheticOrdinaryFinishIndexState(worktree); err != nil {
		return err
	}
	return syntheticOrdinaryFinishFilesystemState(worktree)
}

func syntheticOrdinaryFinishIndexState(worktree string) error {
	flags := exec.Command("git", "-C", worktree, "ls-files", "--cached", "-v", "-z")
	flags.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	flagOutput, err := flags.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read index flags: %w: %s", err, flagOutput)
	}
	for _, record := range bytes.Split(flagOutput, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		if len(record) < 3 || record[1] != ' ' {
			return fmt.Errorf("malformed index flag record %q", record)
		}
		tag := record[0]
		if tag == 'S' || tag == 's' {
			return fmt.Errorf("skip-worktree index entry %q", record[2:])
		}
		if tag >= 'a' && tag <= 'z' {
			return fmt.Errorf("assume-unchanged index entry %q", record[2:])
		}
	}

	stages := exec.Command("git", "-C", worktree, "ls-files", "--stage", "-z")
	stages.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	stageOutput, err := stages.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read index stages: %w: %s", err, stageOutput)
	}
	for _, record := range bytes.Split(stageOutput, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		headerEnd := bytes.IndexByte(record, '\t')
		if headerEnd < 0 {
			return fmt.Errorf("malformed index stage record %q", record)
		}
		fields := bytes.Fields(record[:headerEnd])
		if len(fields) != 3 {
			return fmt.Errorf("malformed index stage header %q", record[:headerEnd])
		}
		if !bytes.Equal(fields[2], []byte("0")) {
			return fmt.Errorf("non-stage-zero index entry %q at stage %q", record[headerEnd+1:], fields[2])
		}

		mode := string(fields[0])
		oid := string(fields[1])
		path := string(record[headerEnd+1:])
		cleanPath := filepath.Clean(filepath.FromSlash(path))
		if path == "" || filepath.IsAbs(cleanPath) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe index path %q", path)
		}
		if len(oid) != 40 && len(oid) != 64 {
			return fmt.Errorf("malformed index object ID %q for %q", oid, path)
		}
		for _, digit := range oid {
			if !((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f')) {
				return fmt.Errorf("malformed index object ID %q for %q", oid, path)
			}
		}

		absolutePath := filepath.Join(worktree, cleanPath)
		info, err := os.Lstat(absolutePath)
		if err != nil {
			return fmt.Errorf("lstat index path %q: %w", path, err)
		}

		var hash *exec.Cmd
		switch mode {
		case "100644", "100755":
			if !info.Mode().IsRegular() {
				return fmt.Errorf("index path %q has mode %s but worktree type %s", path, mode, info.Mode().Type())
			}
			executable := info.Mode().Perm()&0o100 != 0
			if (mode == "100755") != executable {
				return fmt.Errorf("index path %q executable mode differs from index mode %s", path, mode)
			}
			hash = exec.Command("git", "-C", worktree, "hash-object", "--no-filters", "--", path)
		case "120000":
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("index path %q has symlink mode but worktree type %s", path, info.Mode().Type())
			}
			target, err := os.Readlink(absolutePath)
			if err != nil {
				return fmt.Errorf("read index symlink %q: %w", path, err)
			}
			hash = exec.Command("git", "-C", worktree, "hash-object", "--stdin")
			hash.Stdin = strings.NewReader(target)
		default:
			return fmt.Errorf("unsupported index mode %q for %q", mode, path)
		}

		hash.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
		hashOutput, err := hash.CombinedOutput()
		if err != nil {
			return fmt.Errorf("hash index path %q: %w: %s", path, err, hashOutput)
		}
		hashOutput = bytes.TrimSuffix(hashOutput, []byte{'\n'})
		if len(hashOutput) != len(oid) {
			return fmt.Errorf("malformed worktree object ID %q for %q", hashOutput, path)
		}
		for _, digit := range hashOutput {
			if !((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f')) {
				return fmt.Errorf("malformed worktree object ID %q for %q", hashOutput, path)
			}
		}
		if !bytes.Equal(hashOutput, fields[1]) {
			return fmt.Errorf("worktree content differs from index for %q", path)
		}
	}
	return nil
}

func syntheticOrdinaryFinishFilesystemState(worktree string) error {
	rootCommand := exec.Command("git", "-C", worktree, "rev-parse", "--show-toplevel")
	rootCommand.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	rootOutput, err := rootCommand.CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolve worktree root: %w: %s", err, rootOutput)
	}
	root := strings.TrimSpace(string(rootOutput))
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("malformed worktree root %q", root)
	}

	trackedCommand := exec.Command("git", "-C", root, "ls-files", "--cached", "-z")
	trackedCommand.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	trackedOutput, err := trackedCommand.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read tracked filesystem set: %w: %s", err, trackedOutput)
	}
	tracked := make(map[string]struct{})
	ancestors := make(map[string]struct{})
	for _, record := range bytes.Split(trackedOutput, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		path := filepath.Clean(filepath.FromSlash(string(record)))
		if filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe tracked filesystem path %q", record)
		}
		tracked[path] = struct{}{}
		for ancestor := filepath.Dir(path); ancestor != "."; ancestor = filepath.Dir(ancestor) {
			ancestors[ancestor] = struct{}{}
		}
	}

	return filepath.WalkDir(root, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk worktree filesystem: %w", walkErr)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("lstat worktree filesystem path %q: %w", path, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize worktree filesystem path %q: %w", path, err)
		}
		relative = filepath.Clean(relative)
		if relative == "." {
			if !info.IsDir() {
				return fmt.Errorf("worktree root is not a directory: %s", info.Mode().Type())
			}
			return nil
		}
		if relative == ".git" {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("linked-worktree .git is not a regular administrative file: %s", info.Mode().Type())
			}
			return nil
		}
		if _, ok := tracked[relative]; ok {
			return nil
		}
		if _, ok := ancestors[relative]; ok {
			if !info.IsDir() {
				return fmt.Errorf("tracked ancestor %q is not a directory: %s", relative, info.Mode().Type())
			}
			return nil
		}
		return fmt.Errorf("non-index filesystem object %q", relative)
	})
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

func legacyTestPublicDocsDescribeNarrowProjectlessSpawnException(t *testing.T) {
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
