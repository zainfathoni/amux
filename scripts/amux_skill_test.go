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
	filepath.Join("skills", "amux", "reference", "pi-spark-orb-executor.md"),
	filepath.Join("skills", "amux", "reference", "commands.md"),
	filepath.Join("skills", "amux", "reference", "trigger-phrases.md"),
	filepath.Join("skills", "amux", "reference", "workflows.md"),
	filepath.Join("skills", "amux", "reference", "troubleshooting.md"),
	filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"),
	filepath.Join("skills", "amux", "reference", "claude-opus-orb-executor.md"),
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
	if len(matches) != 23 {
		t.Fatalf("trigger checklist has %d routes, want 23", len(matches))
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
	for _, name := range []string{
		"commands.md",
		"workflows.md",
		"troubleshooting.md",
		"trigger-phrases.md",
		"claude-opus-orb-executor.md",
		"pi-spark-orb-executor.md",
		"claude-read-only-delegation.md",
		"claude-mutating-delegation.md",
		"claude-delegation-contract.md",
		"claude-delegation-recovery.md",
		"amp-invocation-policy.md",
	} {
		if !strings.Contains(skill, "reference/"+name) {
			t.Errorf("SKILL.md does not link reference/%s", name)
		}
		if _, err := os.Stat(filepath.Join(root, "skills", "amux", "reference", name)); err != nil {
			t.Errorf("reference/%s is missing: %v", name, err)
		}
	}
}

func TestExperimentalPiSparkOrbRecipeStaysProviderSpecificAndFailClosed(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	triggers := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md"))
	recipe := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "pi-spark-orb-executor.md"))

	if !strings.Contains(skill, "Run Pi on Spark in an Amp Orb") || !strings.Contains(triggers, "Run Pi on Spark in an Amp Orb") {
		t.Error("Pi/Spark trigger is missing from skill routing or trigger checklist")
	}
	if !strings.Contains(skill, "reference/pi-spark-orb-executor.md") || !strings.Contains(triggers, "pi-spark-orb-executor.md") {
		t.Error("Pi/Spark reference is missing from skill routing or trigger checklist")
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
		".version' \"$EXPERIMENT/package-metadata.json\")\" = \"$PI_VERSION",
		"--ignore-scripts",
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

func TestInvocationPolicyIsProgressivelyDisclosedWithoutChangingClaudeRoutes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	policy := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "amp-invocation-policy.md"))
	claude := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-read-only-delegation.md"))

	for _, required := range []string{
		"load [`reference/amp-invocation-policy.md`]",
		"Never bypass a binding `ask` or `reject`",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("SKILL.md is missing invocation-policy routing %q", required)
		}
	}
	for _, required := range []string{
		"observed",
		"instruction-only",
		"one narrow query",
		"Raw delegated arguments are never logged",
		"Amp-native `runner(id)`",
		"unknown charge route",
		"public-safe",
		"#147",
		"#176",
	} {
		if !strings.Contains(policy, required) {
			t.Errorf("invocation policy is missing %q", required)
		}
	}
	if strings.Contains(claude, "amp-invocation-policy") {
		t.Error("independent Claude route unexpectedly loads invocation policy")
	}
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	for _, required := range []string{
		"resolve-amp-invocation-policy",
		"MODE=medium",
		`"mode":"%s"`,
		"exact deterministic `allow` document",
		"Every automatic `amux spawn` command in this reference",
		"Exit nonzero stops before `amux spawn`",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("automatic spawn workflow is missing resolver preflight %q", required)
		}
	}
	if count := strings.Count(workflow, "Run the shared automatic-spawn preflight above before this block."); count != 2 {
		t.Errorf("automatic spawn route markers=%d, want 2 for sprawl and durable coordination", count)
	}
	spawnCommands := 0
	scanLines(t, filepath.Join(root, "skills", "amux", "reference", "workflows.md"), func(lineNumber int, line string) {
		command := commandText(line)
		if !strings.HasPrefix(command, "amux ") || !strings.Contains(command, " spawn ") {
			return
		}
		spawnCommands++
		if !strings.Contains(command, `--mode "$MODE"`) {
			t.Errorf("workflows.md:%d automatic spawn does not bind shared MODE: %s", lineNumber, strings.TrimSpace(line))
		}
	})
	if spawnCommands != 6 {
		t.Errorf("automatic spawn command coverage=%d, want 6", spawnCommands)
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
		for _, required := range []string{
			"authorized `/amux` lifecycle or coordination operation",
			"concrete local/GitHub discrepancy",
			"deterministic evidence",
			"durable/local/GitHub evidence",
			"one narrow query",
			"block rather than widening or chaining",
		} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing discrepancy-recovery contract %q", relativePath, required)
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
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-read-only-delegation.md"))
	mutating := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-mutating-delegation.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-delegation-contract.md"))
	recovery := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-delegation-recovery.md"))

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

func TestClaudeOpusOrbExecutorRecipeStaysProviderSpecificAndBounded(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	recipe := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-opus-orb-executor.md"))

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

func TestCoordinatorWorkflowMatchesDurableCLIContract(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	commands := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "commands.md"))
	troubleshooting := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "troubleshooting.md"))

	stages := []string{
		"### 1. Preflight authoritative state and bootstrap the CLI",
		"### 2. Declare the group and register the verified coordinator lease",
		"### 3. Spawn and attach the authoritative receiving thread",
		"### 4. Persist ready, wake, acknowledge, and independently verify",
		"### 5. Merge, verify post-merge CI, then authorize finish",
		"### 6. Submit merged and run `/amux finish`",
		"### 7. Coordinator-owned deadline queue",
	}
	last := -1
	for _, stage := range stages {
		at := strings.Index(workflow, stage)
		if at <= last {
			t.Errorf("coordinator stage missing or out of order: %q", stage)
		}
		last = at
	}

	for _, required := range []string{
		"native parent/sub-issue/blocked-by/blocking relationships",
		"fresh `origin/main`",
		"issue-unprefixed semantic window",
		"--mode medium",
		"--group <durable-issue-group>",
		"authoritative receiving thread",
		"amux --json callback register --group <durable-issue-group> --thread <coordinator-thread> --pane <coordinator-pane>",
		"amux report submit --report-id <stable-report-id> --group <durable-issue-group>",
		"amux report pending --group <durable-issue-group>",
		"amux report acknowledge --report-id <stable-report-id>",
		"PR URL, head branch/SHA, issue scope and diff, mergeability, closing-issue metadata",
		"amux report authorize-finish --report-id <stable-report-id>",
		"verify post-merge CI",
		"--status merged",
		"invokes `amux teardown --thread <member-thread>` last",
		"Group membership and report history survive teardown",
		"<stable-report-id><TAB>ready<TAB>recorded<TAB><member-thread>",
		"CALLBACK<TAB><durable-issue-group><TAB><stable-report-id><TAB>notified",
		"AMUX_REPORT group=<durable-issue-group> report=<stable-report-id>",
		"Do not edit `reports.json` directly",
		"current CLI exposes no command to create or update deadline records",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("coordinator workflow is missing %q", required)
		}
	}
	for _, inconsistent := range []string{"--group amux-135", "--group <group>", "CALLBACK<TAB><group><TAB><stable-report-id>", "AMUX_REPORT group=<group> report=<stable-report-id>"} {
		if strings.Contains(workflow, inconsistent) {
			t.Errorf("coordinator workflow contains inconsistent durable issue placeholder %q", inconsistent)
		}
	}

	for _, required := range []string{
		"<report><TAB><status><TAB>recorded<TAB><thread>",
		"CALLBACK<TAB><group><TAB><report><TAB>notified",
		"AMUX_REPORT group=<group> report=<id>",
		"external_sync: unsupported",
		"drift: may_remain_indefinitely",
		"Lock contention is exit `2`",
	} {
		if !strings.Contains(commands, required) {
			t.Errorf("command contract is missing %q", required)
		}
	}

	for _, required := range []string{
		"Missing, stale, or recycled callback",
		"Busy composer",
		"Failed send with a verified safe pane",
		"Duplicate or reordered wake-up",
		"Coordinator restart",
		"Add-only label drift",
		"Bootstrap mismatch",
		"retry the identical desired-state operation with the same report ID or spawn key",
		"do not fall back to stale bare `amux`",
	} {
		if !strings.Contains(troubleshooting, required) {
			t.Errorf("coordinator recovery is missing %q", required)
		}
	}
}

func TestIssueCoordinationUsesRepositoryQualifiedDurableIdentity(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	checks := map[string][]string{
		filepath.Join("skills", "amux", "reference", "workflows.md"): {
			"`amux-<issue-number>`",
			"`amux-<issue-number>-worker-<ordinal>`",
			"`<repository-slug>-<issue-number>`",
			"`<repository-slug>-<issue-number>-worker-<ordinal>`",
			"not a generic `amux group` validation rule",
			"`amux-135-worker-1`",
			"purpose-specific groups such as `pr-181-review`",
		},
		"README.md": {
			"--group amux-110",
			"--report-id amux-133-worker-1 --group amux-133",
			"another repository uses the equivalent `<repository-slug>-131` and `<repository-slug>-131-worker-1`",
			"Legacy `issue-*` identities and purpose-specific groups such as `pr-181-review` remain valid",
		},
		filepath.Join("docs", "skill", "index.html"): {
			"amux-&lt;issue-number&gt;",
			"amux-&lt;issue-number&gt;-worker-&lt;ordinal&gt;",
			"--group amux-135",
			"--report-id amux-135-worker-1 --group amux-135",
		},
	}
	for relativePath, required := range checks {
		contents := readSkillFile(t, root, relativePath)
		for _, want := range required {
			if !strings.Contains(contents, want) {
				t.Errorf("%s is missing issue identity convention %q", relativePath, want)
			}
		}
		for _, obsolete := range []string{"--group issue-110", "--group issue-131", "--group issue-133", "`issue-135-worker-1`"} {
			if strings.Contains(contents, obsolete) {
				t.Errorf("%s still teaches obsolete issue identity %q", relativePath, obsolete)
			}
		}
	}
}

func TestWorkGroupCompletionsExposeImplementedCommands(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	checks := map[string][]string{
		"bash": {"declare add remove coordinator list show reconcile", "register clear", "submit pending history acknowledge authorize-finish"},
		"zsh":  {"group_commands=(", "callback_commands=(", "report_commands=(", "--report-id", "--pane"},
		"fish": {"__fish_amux_group_leaf", "__fish_amux_callback_leaf", "__fish_amux_report_leaf", "authorize-finish", "-l 'report-id'", "-l 'pane'"},
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
	}
}

func TestCoordinatorDeadlinePolicyIsConsistent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, relativePath := range []string{
		"README.md",
		filepath.Join("skills", "amux", "reference", "workflows.md"),
		filepath.Join("docs", "skill", "index.html"),
	} {
		contents := readSkillFile(t, root, relativePath)
		for _, required := range []string{"Small 30m", "Medium 1h", "Large 2h", "XL", "15m", "review", "10m", "external CI", "20m", "finish", "half the original budget", "new generation", "diagnostic", "nearest-deadline queue", "timer process per child"} {
			if !strings.Contains(contents, required) {
				t.Errorf("%s is missing deadline policy %q", relativePath, required)
			}
		}
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
			if !strings.Contains(command, "--mode medium") && !(relativePath == filepath.Join("skills", "amux", "reference", "workflows.md") && strings.Contains(command, `--mode "$MODE"`)) {
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
		"--group <durable-issue-group>",
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

func TestClaudePairTeardownIsFailClosedAndRunsBeforeWorkerTeardown(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	skill := readSkillFile(t, root, filepath.Join("skills", "amux", "SKILL.md"))
	contract := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-delegation-contract.md"))
	workflow := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "workflows.md"))
	recovery := readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "claude-delegation-recovery.md"))

	for _, required := range []string{
		"lifecycle worker-teardown --origin-thread <thread-id> --dry-run",
		"lifecycle worker-teardown --origin-thread <thread-id>",
		"active, unacknowledged, unresolved, mismatched, missing, or indeterminate",
		"before `amux teardown`",
		"worker teardown remains the final action",
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
	if !strings.Contains(skill, "paired Claude lifecycle preflight") {
		t.Error("SKILL.md does not route teardown through paired Claude lifecycle preflight")
	}
	for _, required := range []string{
		"register-legacy-store --origin-thread <thread-id> --store-path <exact-private-store>",
		"detach-indeterminate-worker",
		"retire-live-indeterminate-pair",
		"historical_modern_read_only_launch_intent_v1",
		"state:pair_retired",
		"terminal Amp work authorization",
		"durable origin fence",
		"must not continue to worktree removal",
	} {
		if !strings.Contains(recovery+workflow+contract, required) {
			t.Errorf("indeterminate detach progressive disclosure is missing %q", required)
		}
	}
	if !strings.Contains(skill, "Recover indeterminate Claude worker evidence") || !strings.Contains(readSkillFile(t, root, filepath.Join("skills", "amux", "reference", "trigger-phrases.md")), "Recover indeterminate Claude worker evidence") {
		t.Error("indeterminate recovery trigger is not routed at both skill tiers")
	}
	pairAdmission := strings.Index(workflow, "Run the paired Claude lifecycle dry-run and execution")
	worktreeRemoval := strings.Index(workflow, "Remove the clean worker worktree")
	finalRevalidation := strings.Index(workflow, "rerun `lifecycle worker-teardown")
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
