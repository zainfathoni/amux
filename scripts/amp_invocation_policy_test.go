package scripts_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type policyResult struct {
	Action     string   `json:"action"`
	Result     string   `json:"result"`
	Reason     string   `json:"reason"`
	Capability string   `json:"capability"`
	Sources    []string `json:"sources"`
}

func TestInvocationPolicyResolver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		exit   int
		result string
		reason string
	}{
		{name: "automatic medium spawn", input: `{"version":1,"action":"amux_spawn","mode":"medium","automatic":true}`, result: "allow", reason: "automatic_medium"},
		{name: "automatic low spawn is blocked without rewrite", input: `{"version":1,"action":"amux_spawn","mode":"low","automatic":true}`, exit: 2, result: "reject", reason: "automatic_mode_not_medium"},
		{name: "exact approved task read remains advisory", input: `{"version":1,"action":"read_thread","purpose":"task_context","exact_approval":true,"target_count":1}`, result: "would_allow", reason: "exact_task_read_approved"},
		{name: "thread URL is not approval", input: `{"version":1,"action":"read_thread","purpose":"task_context","exact_approval":false,"target_count":1,"url_provenance":true}`, result: "would_ask", reason: "exact_approval_required"},
		{name: "first local GitHub discrepancy recovery query", input: `{"version":1,"action":"read_thread","purpose":"discrepancy_recovery","target_count":1,"concrete_local_github_discrepancy":true,"deterministic_evidence_exhausted":true,"relationship_evidenced":true,"prior_queries":0}`, result: "would_allow", reason: "bounded_discrepancy_query"},
		{name: "discrepancy recovery cannot chain", input: `{"version":1,"action":"read_thread","purpose":"discrepancy_recovery","target_count":1,"concrete_local_github_discrepancy":true,"deterministic_evidence_exhausted":true,"relationship_evidenced":true,"prior_queries":1}`, result: "would_reject", reason: "discrepancy_query_exhausted"},
		{name: "native creation requires executor but remains advisory", input: `{"version":1,"action":"native_create","mode":"medium"}`, result: "would_reject", reason: "explicit_executor_required"},
		{name: "capacity schema drift asks in observation", input: `{"version":1,"action":"oracle","capacity":{"schema_version":2}}`, result: "would_ask", reason: "capacity_schema_unsupported"},
		{name: "unknown charge route asks despite known pool", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":43,"freshness":"fresh","confidence":"reported","charge_route":"unknown","reservation":"none","reserve_status":"above"}}`, result: "would_ask", reason: "capacity_charge_route_unknown"},
		{name: "unknown reserve status asks with otherwise complete capacity", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":43,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"unknown"}}`, result: "would_ask", reason: "capacity_reserve_unknown"},
		{name: "complete capacity stays advisory until promoted", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"unpromoted-provider","provider_version":"unpromoted-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":43,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"above"}}`, result: "would_ask", reason: "capacity_unproven"},
		{name: "negative capacity amount fails safely", input: `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":-1,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"above"}}`, result: "would_ask", reason: "capacity_fields_unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, exit, stderr := runPolicy(t, nil, test.input)
			if exit != test.exit {
				t.Fatalf("exit=%d, want %d; stderr=%q", exit, test.exit, stderr)
			}
			if got.Result != test.result || got.Reason != test.reason {
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
		if got.Action != "unsupported" || got.Result != "would_allow" || got.Capability != "permission_tool_unproven" {
			t.Fatalf("input=%q result=%+v", input, got)
		}
		if strings.Contains(stderr, "secret") || strings.Contains(stderr, "private") {
			t.Fatalf("diagnostic leaked raw arguments: %q", stderr)
		}
		for _, want := range []string{"action=unsupported", "result=would_allow", "reason=permission_tool_unproven"} {
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
		if exit != 0 || got.Result != "would_reject" || got.Reason != "invalid_input" {
			t.Fatalf("input=%q exit=%d result=%+v stderr=%q", input, exit, got, stderr)
		}
	}

	got, exit, stderr := runPolicy(t, nil, `{"version":1,"action":"oracle","capacity":{"schema_version":true}}`)
	if exit != 0 || got.Result != "would_ask" || got.Reason != "capacity_schema_unsupported" {
		t.Fatalf("capacity bool schema exit=%d result=%+v stderr=%q", exit, got, stderr)
	}
}

func TestInvocationPolicyHugeIntegerIsTotalAndNaNIsInvalidJSON(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("9", 401)
	input := `{"version":1,"action":"oracle","capacity":{"schema_version":1,"provider":"codexbar","provider_version":"documented-v1","pool":"primary","window_minutes":300,"unit":"percent_used","observed_amount":` + huge + `,"freshness":"fresh","confidence":"reported","charge_route":"primary","reservation":"held","reserve_status":"above"}}`
	got, exit, stderr := runPolicy(t, nil, input)
	if exit != 0 || got.Result != "would_ask" || got.Reason != "capacity_fields_unknown" {
		t.Fatalf("huge integer exit=%d result=%+v stderr=%q", exit, got, stderr)
	}
	if strings.Contains(stderr, "Traceback") || strings.Contains(stderr, "resolve-amp-invocation-policy") {
		t.Fatalf("huge integer leaked traceback or path: %q", stderr)
	}

	got, exit, stderr = runPolicy(t, nil, `{"version":1,"action":"oracle","capacity":{"observed_amount":NaN}}`)
	if exit != 2 || got.Action != "invalid" || got.Result != "reject" || got.Reason != "invalid_input" {
		t.Fatalf("NaN exit=%d result=%+v stderr=%q", exit, got, stderr)
	}
	if strings.Contains(stderr, "Traceback") || strings.Contains(stderr, "resolve-amp-invocation-policy") {
		t.Fatalf("NaN leaked traceback or path: %q", stderr)
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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode stdout %q: %v", stdout.String(), err)
	}
	wantKeys := []string{"action", "capability", "reason", "result", "sources"}
	if len(raw) != len(wantKeys) {
		t.Fatalf("public keys=%v, want exactly %v", mapKeys(raw), wantKeys)
	}
	for _, key := range wantKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("public keys=%v, missing %q", mapKeys(raw), key)
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode projected stdout %q: %v", stdout.String(), err)
	}
	return got, exit, stderr.String()
}

func mapKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
