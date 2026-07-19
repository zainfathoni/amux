package scripts_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type policyResult struct {
	Action      string   `json:"action"`
	Decision    string   `json:"decision"`
	Result      string   `json:"result"`
	Enforcement string   `json:"enforcement"`
	Reason      string   `json:"reason"`
	Capability  string   `json:"capability"`
	Sources     []string `json:"sources"`
}

func TestInvocationPolicyResolver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		exit     int
		decision string
		result   string
		enforce  string
		reason   string
	}{
		{name: "automatic medium spawn", input: `{"version":1,"action":"amux_spawn","mode":"medium","automatic":true}`, decision: "allow", result: "allow", enforce: "deterministic", reason: "automatic_medium"},
		{name: "automatic low spawn is blocked without rewrite", input: `{"version":1,"action":"amux_spawn","mode":"low","automatic":true}`, exit: 2, decision: "reject", result: "reject", enforce: "deterministic", reason: "automatic_mode_not_medium"},
		{name: "exact approved task read remains advisory", input: `{"version":1,"action":"read_thread","purpose":"task_context","exact_approval":true,"target_count":1}`, decision: "allow", result: "allow", enforce: "observed", reason: "exact_task_read_approved"},
		{name: "thread URL is not approval", input: `{"version":1,"action":"read_thread","purpose":"task_context","exact_approval":false,"target_count":1,"url_provenance":true}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "exact_approval_required"},
		{name: "first discrepancy recovery query", input: `{"version":1,"action":"read_thread","purpose":"discrepancy_recovery","target_count":1,"concrete_discrepancy":true,"deterministic_evidence_exhausted":true,"relationship_evidenced":true,"prior_queries":0}`, decision: "allow", result: "allow", enforce: "observed", reason: "bounded_discrepancy_query"},
		{name: "discrepancy recovery cannot chain", input: `{"version":1,"action":"read_thread","purpose":"discrepancy_recovery","target_count":1,"concrete_discrepancy":true,"deterministic_evidence_exhausted":true,"relationship_evidenced":true,"prior_queries":1}`, decision: "reject", result: "would_reject", enforce: "observed", reason: "discrepancy_query_exhausted"},
		{name: "native creation requires executor but remains advisory", input: `{"version":1,"action":"native_create","mode":"medium"}`, decision: "reject", result: "would_reject", enforce: "observed", reason: "explicit_executor_required"},
		{name: "capacity schema drift asks in observation", input: `{"version":1,"action":"oracle","capacity":{"schema_version":2}}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "capacity_schema_unsupported"},
		{name: "unknown charge route asks despite known pool", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":43,"freshness":"fresh","confidence":"reported","charge_route":"unknown","reservation":"none","reserve_status":"above"}}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "capacity_charge_route_unknown"},
		{name: "unknown reserve status asks with otherwise complete capacity", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":43,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"unknown"}}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "capacity_reserve_unknown"},
		{name: "complete capacity stays advisory until promoted", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"unpromoted-provider","provider_version":"unpromoted-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":43,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"above"}}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "capacity_unproven"},
		{name: "negative capacity amount fails safely", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":-1,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"above"}}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "capacity_fields_unknown"},
		{name: "non-finite capacity amount fails safely", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":NaN,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"above"}}`, decision: "ask", result: "would_ask", enforce: "observed", reason: "capacity_fields_unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, exit, stderr := runPolicy(t, nil, test.input)
			if exit != test.exit {
				t.Fatalf("exit=%d, want %d; stderr=%q", exit, test.exit, stderr)
			}
			if got.Decision != test.decision || got.Result != test.result || got.Enforcement != test.enforce || got.Reason != test.reason {
				t.Fatalf("result=%+v", got)
			}
			if len(got.Sources) != 1 || got.Sources[0] != "public" {
				t.Fatalf("sources=%v, want redacted public source class", got.Sources)
			}
		})
	}
}

func TestInvocationPermissionAdapterAllowsUnsupportedToolsWithBoundedDiagnostic(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"threadID":"T-12345678-secret-target","prompt":"private"}`,
		`not-json-private`,
		``,
	} {
		got, exit, stderr := runPolicy(t, []string{"permission"}, input)
		if exit != 0 {
			t.Fatalf("input=%q exit=%d; stderr=%q", input, exit, stderr)
		}
		if got.Action != "unsupported" || got.Result != "allow" || got.Enforcement != "observed" || got.Capability != "permission_tool_unproven" {
			t.Fatalf("input=%q result=%+v", input, got)
		}
		if strings.Contains(stderr, "secret") || strings.Contains(stderr, "private") {
			t.Fatalf("diagnostic leaked raw arguments: %q", stderr)
		}
		for _, want := range []string{"action=unsupported", "result=allow", "reason=permission_tool_unproven"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("diagnostic %q missing %q", stderr, want)
			}
		}
	}
}

func TestInvocationPolicyRejectsUnknownFieldsAndMalformedInput(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"version":1,"action":"amux_spawn","mode":"medium","automatic":true,"raw_target":"secret"}`,
		`{"version":1,"action":"amux_spawn"}`,
		`{"version":true,"action":"amux_spawn","mode":"medium","automatic":true}`,
		`not-json`,
	} {
		got, exit, stderr := runPolicy(t, nil, input)
		if exit != 2 || got.Reason != "invalid_input" {
			t.Fatalf("input=%q exit=%d result=%+v stderr=%q", input, exit, got, stderr)
		}
		if strings.Contains(stderr, input) || strings.Contains(stderr, "secret") {
			t.Fatalf("invalid-input diagnostic leaked input: %q", stderr)
		}
	}
}

func TestInvocationPolicyMalformedObservedActionsRemainNonBinding(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		`{"version":1,"action":"native_create","mode":"medium","executor":"bogus"}`,
		`{"version":1,"action":"native_create"}`,
		`{"version":1,"action":"native_create","mode":true}`,
		`{"version":1,"action":"native_create","mode":""}`,
		`{"version":1,"action":"read_thread","purpose":"task_context","exact_approval":true,"target_count":true}`,
	} {
		got, exit, stderr := runPolicy(t, nil, input)
		if exit != 0 || got.Decision != "reject" || got.Result != "would_reject" || got.Enforcement != "observed" || got.Reason != "invalid_input" {
			t.Fatalf("input=%q exit=%d result=%+v stderr=%q", input, exit, got, stderr)
		}
	}

	got, exit, stderr := runPolicy(t, nil, `{"version":1,"action":"oracle","capacity":{"schema_version":true}}`)
	if exit != 0 || got.Decision != "ask" || got.Result != "would_ask" || got.Enforcement != "observed" || got.Reason != "capacity_schema_unsupported" {
		t.Fatalf("capacity bool schema exit=%d result=%+v stderr=%q", exit, got, stderr)
	}
}

func TestInvocationPolicyInvalidActionIsRedacted(t *testing.T) {
	t.Parallel()
	secret := "/private/account/T-secret-correlator"
	got, exit, stderr := runPolicy(t, nil, `{"version":true,"action":"`+secret+`"}`)
	if exit != 2 || got.Action != "invalid" || got.Reason != "invalid_input" {
		t.Fatalf("exit=%d result=%+v stderr=%q", exit, got, stderr)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(stderr, secret) {
		t.Fatalf("invalid action leaked: stdout=%s stderr=%q", encoded, stderr)
	}
}

func runPolicy(t *testing.T, args []string, input string) (policyResult, int, string) {
	t.Helper()
	path := filepath.Join(repoRoot(t), "skills", "amux", "scripts", "resolve-amp-invocation-policy")
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), "AGENT_TOOL_NAME=unproven_tool")
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		exit = exitErr.ExitCode()
	}
	var got policyResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout %q: %v", stdout.String(), err)
	}
	return got, exit, stderr.String()
}
