package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMutatingCapacityDecisionUsesEveryReserveAndTightestWindow(t *testing.T) {
	t.Parallel()
	request := map[string]any{
		"capacity": map[string]any{
			"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 20, "window_minutes": 300, "resets_at": futureCapacityReset(300)},
				map[string]any{"name": "secondary", "used_percent": 40, "window_minutes": 10080, "resets_at": futureCapacityReset(10080)},
				map[string]any{"name": "sonnet", "used_percent": 65, "window_minutes": 300, "resets_at": futureCapacityReset(300), "model_specific": true},
			},
		},
		"reserve_floors": map[string]any{
			"five_hour": 30, "weekly": 40, "model_specific": map[string]any{"sonnet": 30},
		},
		"acknowledged_unknown_capacity": false,
	}
	stdout, stderr, err := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
	if err != nil {
		t.Fatalf("capacity decision: %v: %s", err, stderr)
	}
	for _, want := range []string{
		`"decision":"autonomous_allowed"`, `"governing_window":"sonnet"`,
		`"remaining_percent":35`, `"reserve_floor_percent":30`, `"margin_percent":5`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("capacity decision missing %s:\n%s", want, stdout)
		}
	}

	belowFloor := cloneJSONMap(t, request)
	belowFloor["capacity"].(map[string]any)["windows"].([]any)[2].(map[string]any)["used_percent"] = float64(80)
	_, stderr, err = runHelper(t, t.TempDir(), belowFloor, "capacity", "decide-mutating")
	if err == nil || !strings.Contains(stderr, "hard reserve floor") {
		t.Fatalf("below-floor decision error = %v, stderr %q", err, stderr)
	}

	missingFloor := cloneJSONMap(t, request)
	delete(missingFloor["reserve_floors"].(map[string]any)["model_specific"].(map[string]any), "sonnet")
	_, stderr, err = runHelper(t, t.TempDir(), missingFloor, "capacity", "decide-mutating")
	if err == nil || !strings.Contains(stderr, "reserve floor is required for every available") {
		t.Fatalf("missing-floor decision error = %v, stderr %q", err, stderr)
	}

	knownLowWithUnknown := cloneJSONMap(t, request)
	knownLowWithUnknown["capacity"].(map[string]any)["windows"].([]any)[0].(map[string]any)["used_percent"] = float64(90)
	knownLowWithUnknown["capacity"].(map[string]any)["windows"].([]any)[2].(map[string]any)["used_percent"] = nil
	knownLowWithUnknown["acknowledged_unknown_capacity"] = true
	knownLowWithUnknown["acknowledgement_of"] = strings.Repeat("a", 64)
	_, stderr, err = runHelper(t, t.TempDir(), knownLowWithUnknown, "capacity", "decide-mutating")
	if err == nil || !strings.Contains(stderr, "hard reserve floor") {
		t.Fatalf("unknown capacity bypassed known hard floor: %v, stderr %q", err, stderr)
	}
}

func TestMutatingCapacityAutonomyRequiresExactSupportedContract(t *testing.T) {
	t.Parallel()
	valid := map[string]any{
		"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
		"windows": []any{
			map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300, "resets_at": futureCapacityReset(300)},
			map[string]any{"name": "secondary", "used_percent": 10, "window_minutes": 10080, "resets_at": futureCapacityReset(10080)},
		},
	}
	mutations := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "empty provider", mutate: func(c map[string]any) { c["provider"] = "" }},
		{name: "wrong provider", mutate: func(c map[string]any) { c["provider"] = "other" }},
		{name: "unknown source", mutate: func(c map[string]any) { c["source"] = "unknown" }},
		{name: "unsupported source", mutate: func(c map[string]any) { c["source"] = "scrape" }},
		{name: "wrong source version", mutate: func(c map[string]any) { c["source_version"] = float64(2) }},
		{name: "missing schema", mutate: func(c map[string]any) { delete(c, "schema_version") }},
		{name: "extra capacity field", mutate: func(c map[string]any) { c["account"] = "private" }},
		{name: "missing window field", mutate: func(c map[string]any) { delete(c["windows"].([]any)[0].(map[string]any), "resets_at") }},
		{name: "extra window field", mutate: func(c map[string]any) { c["windows"].([]any)[0].(map[string]any)["extra"] = true }},
		{name: "malformed reset", mutate: func(c map[string]any) { c["windows"].([]any)[0].(map[string]any)["resets_at"] = "not-a-time" }},
		{name: "duplicate class", mutate: func(c map[string]any) { c["windows"].([]any)[1].(map[string]any)["window_minutes"] = float64(300) }},
		{name: "unbounded window", mutate: func(c map[string]any) { c["windows"].([]any)[0].(map[string]any)["window_minutes"] = nil }},
		{name: "non-finite utilization", mutate: func(c map[string]any) { c["windows"].([]any)[0].(map[string]any)["used_percent"] = "NaN" }},
		{name: "wrong window name", mutate: func(c map[string]any) { c["windows"].([]any)[0].(map[string]any)["name"] = "tertiary" }},
		{name: "wrong class", mutate: func(c map[string]any) { c["windows"].([]any)[0].(map[string]any)["model_specific"] = false }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			capacity := cloneJSONMap(t, valid)
			test.mutate(capacity)
			request := map[string]any{
				"capacity":                      capacity,
				"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
				"acknowledged_unknown_capacity": false,
			}
			stdout, _, _ := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
			if strings.Contains(stdout, `"decision":"autonomous_allowed"`) {
				t.Fatalf("unsupported contract became autonomous: %s", stdout)
			}
		})
	}
}

func TestWrongCapacityClassCannotBypassKnownReserveFloor(t *testing.T) {
	t.Parallel()
	capacities := []map[string]any{
		{
			"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300, "resets_at": "2026-07-19T20:00:00Z"},
				map[string]any{"name": "secondary", "used_percent": 60, "window_minutes": 300, "resets_at": "2026-07-26T00:00:00Z"},
			},
		},
		{
			"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300, "resets_at": "2026-07-19T20:00:00Z"},
				map[string]any{"name": "secondary", "used_percent": 10, "window_minutes": 10080, "resets_at": "2026-07-26T00:00:00Z"},
				map[string]any{"name": "sonnet", "used_percent": 80, "window_minutes": nil, "resets_at": "2026-07-19T20:00:00Z"},
			},
		},
	}
	for _, capacity := range capacities {
		request := map[string]any{
			"capacity":                      capacity,
			"reserve_floors":                map[string]any{"five_hour": 10, "weekly": 50, "model_specific": map[string]any{"sonnet": 30}},
			"acknowledged_unknown_capacity": false,
		}
		_, stderr, err := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
		if err == nil || !strings.Contains(stderr, "conflicts with its declared class") {
			t.Fatalf("wrong-class known floor was acknowledgement-eligible: %v, stderr %q", err, stderr)
		}
	}
}

func TestConfiguredModelFloorRequiresExactlyOneMatchingWindow(t *testing.T) {
	t.Parallel()
	base := map[string]any{
		"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
		"windows": []any{
			map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300, "resets_at": futureCapacityReset(300)},
			map[string]any{"name": "secondary", "used_percent": 10, "window_minutes": 10080, "resets_at": futureCapacityReset(10080)},
		},
	}
	for _, capacity := range []map[string]any{base, func() map[string]any {
		duplicate := cloneJSONMap(t, base)
		duplicate["windows"] = append(duplicate["windows"].([]any),
			map[string]any{"name": "sonnet", "used_percent": 10, "window_minutes": 300, "resets_at": futureCapacityReset(300), "model_specific": true},
			map[string]any{"name": "sonnet", "used_percent": 10, "window_minutes": 300, "resets_at": futureCapacityReset(300), "model_specific": true},
		)
		return duplicate
	}()} {
		request := map[string]any{
			"capacity":                      capacity,
			"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{"sonnet": 20}},
			"acknowledged_unknown_capacity": false,
		}
		stdout, _, _ := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
		if strings.Contains(stdout, `"decision":"autonomous_allowed"`) {
			t.Fatalf("missing or duplicate configured model window became autonomous: %s", stdout)
		}
	}
}

func TestCanonicalWindowWithMalformedDurationIsNeverAcknowledgementEligible(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		window map[string]any
	}{
		{name: "primary duration", window: map[string]any{"name": "primary", "used_percent": 99, "window_minutes": 60, "resets_at": futureCapacityReset(60)}},
		{name: "secondary duration", window: map[string]any{"name": "secondary", "used_percent": 99, "window_minutes": 60, "resets_at": futureCapacityReset(60)}},
		{name: "primary class marker", window: map[string]any{"name": "primary", "used_percent": 99, "window_minutes": 300, "resets_at": futureCapacityReset(300), "model_specific": false}},
		{name: "secondary class marker", window: map[string]any{"name": "secondary", "used_percent": 99, "window_minutes": 10080, "resets_at": futureCapacityReset(10080), "model_specific": false}},
	} {
		request := map[string]any{
			"capacity": map[string]any{
				"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
				"windows": []any{test.window},
			},
			"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
			"acknowledged_unknown_capacity": false,
		}
		_, stderr, err := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
		if err == nil || !strings.Contains(stderr, "conflicts with its declared class") {
			t.Fatalf("malformed canonical %s was acknowledgement-eligible: %v, stderr %q", test.name, err, stderr)
		}
	}
}

func TestCanonicalWindowRequiresAnExactIntegerDuration(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		fmt.Sprintf(`{"capacity":{"status":"supported","provider":"claude","source":"web","source_version":1,"schema_version":1,"confidence":"reported","windows":[{"name":"primary","used_percent":99,"window_minutes":300.0,"resets_at":%q}]},"reserve_floors":{"five_hour":20,"weekly":20,"model_specific":{}},"acknowledged_unknown_capacity":false}`, futureCapacityReset(300)),
		fmt.Sprintf(`{"capacity":{"status":"supported","provider":"claude","source":"web","source_version":1,"schema_version":1,"confidence":"reported","windows":[{"name":"secondary","used_percent":99,"window_minutes":10080.0,"resets_at":%q}]},"reserve_floors":{"five_hour":20,"weekly":20,"model_specific":{}},"acknowledged_unknown_capacity":false}`, futureCapacityReset(10080)),
	} {
		command := exec.Command("python3", helper, "--state-dir", t.TempDir(), "capacity", "decide-mutating")
		command.Stdin = strings.NewReader(input)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "conflicts with its declared class") {
			t.Fatalf("non-integer canonical duration was acknowledgement-eligible: %v: %s", err, output)
		}
	}
}

func TestCapacityResetFreshnessUsesOneDecisionClock(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, pathlib, sys
from datetime import datetime, timezone
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
now = datetime(2026, 7, 20, 12, 0, 0, tzinfo=timezone.utc)
module.capacity_now = lambda: now
request = {
    "capacity": {"status":"supported", "provider":"claude", "source":"web", "source_version":1, "schema_version":1, "confidence":"reported", "windows":[
        {"name":"primary", "used_percent":10, "window_minutes":300, "resets_at":"2026-07-20T14:00:00Z"},
        {"name":"secondary", "used_percent":10, "window_minutes":10080, "resets_at":"2026-07-25T12:00:00Z"},
    ]},
    "reserve_floors":{"five_hour":20, "weekly":20, "model_specific":{}},
    "acknowledged_unknown_capacity":False,
}
assert module.decide_mutating_capacity(request)["decision"] == "autonomous_allowed"
module.capacity_now = lambda: datetime(2026, 7, 20, 14, 0, 0, tzinfo=timezone.utc)
assert module.decide_mutating_capacity(request)["decision"] == "acknowledgement_required"
module.capacity_now = lambda: now
request["capacity"]["windows"][0]["resets_at"] = "2026-07-21T12:00:01Z"
assert module.decide_mutating_capacity(request)["decision"] == "acknowledgement_required"
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("capacity reset freshness: %v: %s", err, output)
	}
}

func TestUnknownCapacityAcknowledgementDigestBindsProvenanceSchemaAndWindows(t *testing.T) {
	t.Parallel()
	request := map[string]any{
		"capacity": map[string]any{
			"status": "supported", "provider": "claude", "source": "unknown", "source_version": 1, "schema_version": 1, "confidence": "unknown",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300, "resets_at": futureCapacityReset(300)},
				map[string]any{"name": "secondary", "used_percent": 10, "window_minutes": 10080, "resets_at": futureCapacityReset(10080)},
			},
		},
		"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
		"acknowledged_unknown_capacity": false,
	}
	stdout, stderr, err := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
	if err != nil {
		t.Fatalf("unknown capacity decision: %v: %s", err, stderr)
	}
	required := decodeJSONMap(t, stdout)
	mutations := []func(map[string]any){
		func(c map[string]any) { c["provider"] = "other" },
		func(c map[string]any) { c["source"] = "web" },
		func(c map[string]any) { c["schema_version"] = float64(2) },
		func(c map[string]any) {
			c["windows"].([]any)[0].(map[string]any)["resets_at"] = futureCapacityReset(240)
		},
	}
	for index, mutate := range mutations {
		changed := cloneJSONMap(t, request)
		mutate(changed["capacity"].(map[string]any))
		changed["acknowledged_unknown_capacity"] = true
		changed["acknowledgement_of"] = required["decision_digest"]
		_, stderr, err = runHelper(t, t.TempDir(), changed, "capacity", "decide-mutating")
		if err == nil {
			t.Fatalf("changed contract %d replay error = %v, stderr %q", index, err, stderr)
		}
	}
	request["acknowledged_unknown_capacity"] = true
	request["acknowledgement_of"] = required["decision_digest"]
	stdout, stderr, err = runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
	if err != nil || !strings.Contains(stdout, `"decision":"explicit_acknowledgement"`) {
		t.Fatalf("exact acknowledgement replay: %v: %s%s", err, stdout, stderr)
	}
}

func TestMutatingCapacityRejectsDuplicatedAndNonFiniteJSON(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []string{
		`{"capacity":{"status":"supported","provider":"claude","provider":"claude","source":"web","source_version":1,"schema_version":1,"confidence":"reported","windows":[]},"reserve_floors":{"five_hour":20,"weekly":20,"model_specific":{}},"acknowledged_unknown_capacity":false}`,
		`{"capacity":{"status":"supported","provider":"claude","source":"web","source_version":1,"schema_version":1,"confidence":"reported","windows":[{"name":"primary","used_percent":NaN,"window_minutes":300,"resets_at":"2026-07-19T20:00:00Z"},{"name":"secondary","used_percent":10,"window_minutes":10080,"resets_at":"2026-07-26T00:00:00Z"}]},"reserve_floors":{"five_hour":20,"weekly":20,"model_specific":{}},"acknowledged_unknown_capacity":false}`,
	}
	for _, input := range inputs {
		command := exec.Command("python3", helper, "--state-dir", t.TempDir(), "capacity", "decide-mutating")
		command.Stdin = strings.NewReader(input)
		output, err := command.CombinedOutput()
		if err == nil || strings.Contains(string(output), `"decision":"autonomous_allowed"`) {
			t.Fatalf("malformed JSON became autonomous: %v: %s", err, output)
		}
	}
}

func TestCapacityDecisionDigestPreservesNumericTypes(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, pathlib, sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
assert module.capacity_decision_digest({"schema_version": 1}) != module.capacity_decision_digest({"schema_version": 1.0})
assert module.capacity_decision_digest({"source_version": 1}) != module.capacity_decision_digest({"source_version": 1.0})
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("capacity digest numeric types: %v: %s", err, output)
	}
}

func TestMutatingCapacityMissingOrLowConfidenceRequiresExplicitAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name     string
		capacity map[string]any
	}{
		{name: "missing", capacity: map[string]any{"status": "unavailable", "windows": []any{}}},
		{name: "low confidence", capacity: map[string]any{
			"status": "supported", "source": "unknown", "confidence": "unknown",
			"windows": []any{map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300}},
		}},
		{name: "missing applicable model window", capacity: map[string]any{
			"status": "supported", "source": "web", "confidence": "reported",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 10, "window_minutes": 300},
				map[string]any{"name": "sonnet", "used_percent": nil, "model_specific": true},
			},
		}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			modelFloors := map[string]any{}
			if fixture.name == "missing applicable model window" {
				modelFloors["sonnet"] = 20
			}
			request := map[string]any{
				"capacity":                      fixture.capacity,
				"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": modelFloors},
				"acknowledged_unknown_capacity": false,
			}
			stdout, stderr, err := runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
			if err != nil || !strings.Contains(stdout, `"decision":"acknowledgement_required"`) || !strings.Contains(stdout, `"autonomous_selection":false`) {
				t.Fatalf("unacknowledged decision: %v: %s%s", err, stdout, stderr)
			}
			required := decodeJSONMap(t, stdout)
			expectedSource, _ := fixture.capacity["source"].(string)
			if expectedSource == "" {
				expectedSource = "unknown"
			}
			expectedConfidence, _ := fixture.capacity["confidence"].(string)
			if expectedConfidence == "" {
				expectedConfidence = "unknown"
			}
			if required["capacity_source"] != expectedSource || required["capacity_confidence"] != expectedConfidence {
				t.Fatalf("decision must preserve capacity provenance: %#v", required)
			}
			request["acknowledged_unknown_capacity"] = true
			request["acknowledgement_of"] = required["decision_digest"]
			stdout, stderr, err = runHelper(t, t.TempDir(), request, "capacity", "decide-mutating")
			if err != nil || !strings.Contains(stdout, `"decision":"explicit_acknowledgement"`) || !strings.Contains(stdout, `"may_proceed":true`) {
				t.Fatalf("acknowledged decision: %v: %s%s", err, stdout, stderr)
			}
		})
	}

	lowKnown := map[string]any{
		"capacity": map[string]any{
			"status": "supported", "source": "unknown", "confidence": "unknown",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 90, "window_minutes": 300},
				map[string]any{"name": "secondary", "used_percent": 10, "window_minutes": 10080},
			},
		},
		"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
		"acknowledged_unknown_capacity": true,
		"acknowledgement_of":            strings.Repeat("a", 64),
	}
	_, stderr, err := runHelper(t, t.TempDir(), lowKnown, "capacity", "decide-mutating")
	if err == nil || !strings.Contains(stderr, "hard reserve floor") {
		t.Fatalf("low-confidence known hard floor was bypassed: %v, stderr %q", err, stderr)
	}

	missing := map[string]any{
		"capacity":                      map[string]any{"status": "unavailable", "windows": []any{}},
		"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
		"acknowledged_unknown_capacity": false,
	}
	stdout, stderr, err := runHelper(t, t.TempDir(), missing, "capacity", "decide-mutating")
	if err != nil {
		t.Fatalf("missing capacity decision: %v: %s", err, stderr)
	}
	prior := decodeJSONMap(t, stdout)
	changed := cloneJSONMap(t, missing)
	changed["reserve_floors"].(map[string]any)["weekly"] = float64(30)
	changed["acknowledged_unknown_capacity"] = true
	changed["acknowledgement_of"] = prior["decision_digest"]
	_, stderr, err = runHelper(t, t.TempDir(), changed, "capacity", "decide-mutating")
	if err == nil || !strings.Contains(stderr, "prior acknowledgement-required decision") {
		t.Fatalf("stale capacity acknowledgement error = %v, stderr %q", err, stderr)
	}
}

func TestMutatingPreparationBindsCleanBaselineAndExclusiveWriter(t *testing.T) {
	fixture := newMutatingGitFixture(t)
	request := mutatingPrepareRequest(fixture)
	stdout, stderr, err := runHelper(t, t.TempDir(), request, "mutation", "prepare")
	if err != nil {
		t.Fatalf("prepare mutation: %v: %s", err, stderr)
	}
	for _, want := range []string{
		`"baseline_commit":"` + fixture.baseline + `"`,
		`"baseline_branch":"delegate"`,
		`"writer_owner":"claude_mutating_delegate"`,
		`"integration_owner":"amp_coordinator"`,
		`"handoff":"one_clean_local_commit"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("prepared mutation missing %s:\n%s", want, stdout)
		}
	}

	shared := cloneJSONMap(t, request)
	shared["shared_writable"] = true
	_, stderr, err = runHelper(t, t.TempDir(), shared, "mutation", "prepare")
	if err == nil || !strings.Contains(stderr, "shared writable workdirs are prohibited") {
		t.Fatalf("shared writable error = %v, stderr %q", err, stderr)
	}

	ambiguous := cloneJSONMap(t, request)
	ambiguous["coordinator_write_frozen"] = false
	_, stderr, err = runHelper(t, t.TempDir(), ambiguous, "mutation", "prepare")
	if err == nil || !strings.Contains(stderr, "exclusive writer ownership") {
		t.Fatalf("ambiguous ownership error = %v, stderr %q", err, stderr)
	}

	if err := os.WriteFile(filepath.Join(fixture.worktree, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelper(t, t.TempDir(), request, "mutation", "prepare")
	if err == nil || !strings.Contains(stderr, "clean immutable baseline") {
		t.Fatalf("dirty baseline error = %v, stderr %q", err, stderr)
	}
}

func TestMutatingBindingBaselineIsImmutable(t *testing.T) {
	stateDir := t.TempDir()
	binding := mutatingBinding("mutation-immutable", "/tmp/mutation", strings.Repeat("1", 40), "delegate")
	create := map[string]any{"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"}}
	assertHelperOutcome(t, stateDir, "recorded", create, "receipt", "create")
	conflict := cloneJSONMap(t, create)
	conflict["binding"].(map[string]any)["base"] = strings.Repeat("2", 40)
	_, stderr, err := runHelper(t, stateDir, conflict, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "different immutable binding") {
		t.Fatalf("baseline conflict error = %v, stderr %q", err, stderr)
	}
}

func TestMutatingReceiptRejectsASecondUnresolvedWorktreeLeaseWithoutChangingStore(t *testing.T) {
	for _, test := range []struct {
		name  string
		alias bool
	}{
		{name: "canonical path"},
		{name: "symlink alias", alias: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newMutatingGitFixture(t)
			stateDir := t.TempDir()
			first := mutatingBinding("delegation-lease-first", fixture.worktree, fixture.baseline, "delegate")
			assertHelperOutcome(t, stateDir, "recorded", map[string]any{
				"binding": first, "routing": map[string]any{"target": "machine_local_inbox"},
			}, "receipt", "create")
			before, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
			if err != nil {
				t.Fatal(err)
			}

			workdir := fixture.worktree
			if test.alias {
				workdir = filepath.Join(t.TempDir(), "delegate-alias")
				if err := os.Symlink(fixture.worktree, workdir); err != nil {
					t.Fatal(err)
				}
			}
			second := mutatingBinding("delegation-lease-second", workdir, fixture.baseline, "delegate")
			_, stderr, err := runHelper(t, stateDir, map[string]any{
				"binding": second, "routing": map[string]any{"target": "machine_local_inbox"},
			}, "receipt", "create")
			if err == nil || !strings.Contains(stderr, "exclusive logical writer lease") {
				t.Fatalf("second lease error = %v, stderr %q", err, stderr)
			}
			after, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("rejected lease changed store bytes:\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}

func TestMutatingReceiptRejectsCaseVariantWorktreeLeaseWithoutChangingStore(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("case-variant lease identity is a Darwin filesystem regression")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	fixtureRoot, err := os.MkdirTemp(home, ".amux-lease-case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureRoot) })
	fixture := newMutatingGitFixtureUnder(t, fixtureRoot)
	caseVariant := caseVariantPath(fixture.worktree)
	originalInfo, err := os.Stat(fixture.worktree)
	if err != nil {
		t.Fatal(err)
	}
	variantInfo, err := os.Stat(caseVariant)
	if err != nil || !os.SameFile(originalInfo, variantInfo) {
		t.Skip("test filesystem is case-sensitive")
	}
	stateDir := t.TempDir()
	first := mutatingBinding("delegation-case-first", fixture.worktree, fixture.baseline, "delegate")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": first, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	before, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}

	second := mutatingBinding("delegation-case-second", caseVariant, fixture.baseline, "delegate")
	_, stderr, err := runHelper(t, stateDir, map[string]any{
		"binding": second, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "exclusive logical writer lease") {
		t.Fatalf("case-variant lease error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("rejected case-variant lease changed store bytes:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestMissingUnresolvedWorktreeRemainsInspectableButBlocksNewLease(t *testing.T) {
	missing := newMutatingGitFixture(t)
	stateDir := t.TempDir()
	first := mutatingBinding("delegation-missing-first", missing.worktree, missing.baseline, "delegate")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": first, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	before, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	moved := missing.worktree + "-moved"
	if err := os.Rename(missing.worktree, moved); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", "delegation-missing-first")
	if err != nil || !strings.Contains(stdout, `"delegation_id":"delegation-missing-first"`) {
		t.Fatalf("missing unresolved receipt was not inspectable: %v: %s%s", err, stdout, stderr)
	}

	available := newMutatingGitFixture(t)
	second := mutatingBinding("delegation-missing-second", available.worktree, available.baseline, "delegate")
	_, stderr, err = runHelper(t, stateDir, map[string]any{
		"binding": second, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "cannot safely compare mutating writer lease identities") {
		t.Fatalf("missing unresolved lease comparison error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("rejected missing-path lease changed store bytes:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestMutatingSubmissionAcceptsOnlyOneCleanCommitAndFreezesWriter(t *testing.T) {
	fixture := newMutatingGitFixture(t)
	stateDir := t.TempDir()
	binding := mutatingBinding("mutation-success", fixture.worktree, fixture.baseline, "delegate")
	createMutatingReceipt(t, stateDir, binding)
	handoff := commitFixtureChange(t, fixture.worktree, "result.txt", "result", "delegated result")
	report := mutatingReport(binding, "handoff-1", "complete", handoff)
	assertHelperOutcome(t, stateDir, "recorded", report, "report", "submit")
	assertHelperOutcome(t, stateDir, "duplicate", report, "report", "submit")

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", "mutation-success")
	if err != nil {
		t.Fatalf("show handoff: %v: %s", err, stderr)
	}
	for _, want := range []string{
		`"state":"valid_report"`, `"submission_frozen":true`, `"writer_authority":"frozen"`,
		`"commit_count":1`, `"handoff_commit":"` + handoff + `"`,
		`"validation_scope":"objective_handoff_only"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("valid handoff missing %s:\n%s", want, stdout)
		}
	}
	for _, forbidden := range []string{`"correct":true`, `"accepted":true`, `"merge_ready":true`, `"cleanup_authorized":true`} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("valid report made prohibited claim %s:\n%s", forbidden, stdout)
		}
	}
	validationRequest := map[string]any{"delegation_id": "mutation-success"}
	validation, stderr, err := runHelper(t, stateDir, validationRequest, "mutation", "validate-handoff")
	if err != nil || !strings.Contains(validation, `"validation_scope":"objective_handoff_only"`) {
		t.Fatalf("independent handoff validation: %v: %s%s", err, validation, stderr)
	}
	if err := os.WriteFile(filepath.Join(fixture.worktree, "post-freeze.txt"), []byte("unauthorized"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelper(t, stateDir, validationRequest, "mutation", "validate-handoff")
	if err == nil || !strings.Contains(stderr, "clean worktree") {
		t.Fatalf("post-freeze mutation validation error = %v, stderr %q", err, stderr)
	}
	consume := map[string]any{"delegation_id": "mutation-success", "event_id": "deliver-after-write", "message_id": "handoff-1"}
	_, stderr, err = runHelper(t, stateDir, consume, "inbox", "consume")
	if err == nil || !strings.Contains(stderr, "clean worktree") {
		t.Fatalf("post-freeze mutation delivery error = %v, stderr %q", err, stderr)
	}

	second := mutatingReport(binding, "handoff-2", "complete", handoff)
	_, stderr, err = runHelper(t, stateDir, second, "report", "submit")
	if err == nil || !strings.Contains(stderr, "submission freeze") {
		t.Fatalf("post-freeze report error = %v, stderr %q", err, stderr)
	}
}

func TestMutatingSubmissionAcceptsCleanZeroCommitBlockedReport(t *testing.T) {
	fixture := newMutatingGitFixture(t)
	stateDir := t.TempDir()
	binding := mutatingBinding("mutation-blocked", fixture.worktree, fixture.baseline, "delegate")
	createMutatingReceipt(t, stateDir, binding)
	report := mutatingReport(binding, "blocked-1", "blocked", "")
	assertHelperOutcome(t, stateDir, "recorded", report, "report", "submit")
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", "mutation-blocked")
	if err != nil || !strings.Contains(stdout, `"outcome":"blocked"`) || !strings.Contains(stdout, `"commit_count":0`) || !strings.Contains(stdout, `"submission_frozen":true`) {
		t.Fatalf("zero-commit blocked receipt: %v: %s%s", err, stdout, stderr)
	}
}

func TestMutatingSubmissionRejectsDirtyMissingAndUnexpectedHandoffs(t *testing.T) {
	t.Run("dirty", func(t *testing.T) {
		fixture := newMutatingGitFixture(t)
		stateDir := t.TempDir()
		binding := mutatingBinding("mutation-dirty", fixture.worktree, fixture.baseline, "delegate")
		createMutatingReceipt(t, stateDir, binding)
		commit := commitFixtureChange(t, fixture.worktree, "result.txt", "result", "result")
		if err := os.WriteFile(filepath.Join(fixture.worktree, "leftover.txt"), []byte("dirty"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertMutatingSubmitError(t, stateDir, mutatingReport(binding, "dirty-1", "complete", commit), "clean worktree")
	})
	t.Run("missing commit", func(t *testing.T) {
		fixture := newMutatingGitFixture(t)
		stateDir := t.TempDir()
		binding := mutatingBinding("mutation-missing", fixture.worktree, fixture.baseline, "delegate")
		createMutatingReceipt(t, stateDir, binding)
		assertMutatingSubmitError(t, stateDir, mutatingReport(binding, "missing-1", "complete", fixture.baseline), "exactly one commit")
	})
	t.Run("unexpected second commit", func(t *testing.T) {
		fixture := newMutatingGitFixture(t)
		stateDir := t.TempDir()
		binding := mutatingBinding("mutation-extra", fixture.worktree, fixture.baseline, "delegate")
		createMutatingReceipt(t, stateDir, binding)
		commitFixtureChange(t, fixture.worktree, "one.txt", "one", "one")
		second := commitFixtureChange(t, fixture.worktree, "two.txt", "two", "two")
		assertMutatingSubmitError(t, stateDir, mutatingReport(binding, "extra-1", "complete", second), "exactly one commit")
	})
	t.Run("reported commit mismatch", func(t *testing.T) {
		fixture := newMutatingGitFixture(t)
		stateDir := t.TempDir()
		binding := mutatingBinding("mutation-mismatch", fixture.worktree, fixture.baseline, "delegate")
		createMutatingReceipt(t, stateDir, binding)
		commitFixtureChange(t, fixture.worktree, "result.txt", "result", "result")
		assertMutatingSubmitError(t, stateDir, mutatingReport(binding, "mismatch-1", "complete", strings.Repeat("f", 40)), "reported handoff commit")
	})
}

func TestMutatingReportReplayConflictAndReceiptOrderingRemainFailClosed(t *testing.T) {
	fixture := newMutatingGitFixture(t)
	stateDir := t.TempDir()
	binding := mutatingBinding("mutation-ordering", fixture.worktree, fixture.baseline, "delegate")
	createMutatingReceipt(t, stateDir, binding)
	commit := commitFixtureChange(t, fixture.worktree, "result.txt", "result", "result")
	report := mutatingReport(binding, "handoff-order", "complete", commit)
	assertHelperOutcome(t, stateDir, "recorded", report, "report", "submit")
	before, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	conflict := cloneJSONMap(t, report)
	conflict["report"].(map[string]any)["summary"] = "conflicting reuse"
	_, stderr, err := runHelper(t, stateDir, conflict, "report", "submit")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflicting replay error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("conflicting report replay mutated durable receipt")
	}

	ack := map[string]any{"delegation_id": "mutation-ordering", "event_id": "ack-early", "message_id": "handoff-order"}
	_, stderr, err = runHelper(t, stateDir, ack, "report", "acknowledge")
	if err == nil || !strings.Contains(stderr, "requires delivery") {
		t.Fatalf("early acknowledgement error = %v, stderr %q", err, stderr)
	}
	consume := map[string]any{"delegation_id": "mutation-ordering", "event_id": "deliver-1", "message_id": "handoff-order"}
	assertHelperOutcome(t, stateDir, "recorded", consume, "inbox", "consume")
	ack["event_id"] = "ack-1"
	assertHelperOutcome(t, stateDir, "recorded", ack, "report", "acknowledge")
}

func TestMutatingReceiptRejectsThinkerReportWithoutFreezingOrDelivery(t *testing.T) {
	fixture := newMutatingGitFixture(t)
	stateDir := t.TempDir()
	binding := mutatingBinding("mutation-kind", fixture.worktree, fixture.baseline, "delegate")
	createMutatingReceipt(t, stateDir, binding)
	commitFixtureChange(t, fixture.worktree, "result.txt", "result", "result")
	thinker := testMessage(binding, "wrong-kind", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "wrong route", "rationale": "must not bypass mutating validation",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{}, "changed_artifacts": []any{}, "references": []any{},
	})
	thinker["producer_role"] = "mutating_delegate"
	thinker["authority"] = "exclusive_writer"
	_, stderr, err := runHelper(t, stateDir, thinker, "report", "submit")
	if err == nil || !strings.Contains(stderr, "kind is invalid") {
		t.Fatalf("wrong-kind mutating report error = %v, stderr %q", err, stderr)
	}
	stdout, showStderr, showErr := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", "mutation-kind")
	if showErr != nil || !strings.Contains(stdout, `"state":"created"`) || strings.Contains(stdout, `"submission_frozen":true`) {
		t.Fatalf("wrong-kind report changed receipt: %v: %s%s", showErr, stdout, showStderr)
	}

	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18"}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "submit_report", "arguments": thinker}},
	}
	var input bytes.Buffer
	for _, message := range messages {
		data, marshalErr := json.Marshal(message)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		input.Write(data)
		input.WriteByte('\n')
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", helper, "--state-dir", stateDir, "mcp", "serve", "--delegation-id", "mutation-kind")
	command.Stdin = &input
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), `"isError":true`) || !strings.Contains(string(output), "kind is invalid") {
		t.Fatalf("MCP wrong-kind report was not rejected: %v\n%s", err, output)
	}
}

func TestMutatingReportRequiresCompletedAcquiredMutatingSession(t *testing.T) {
	fixture := newMutatingGitFixture(t)
	stateDir := t.TempDir()
	binding := mutatingBinding("mutation-no-session", fixture.worktree, fixture.baseline, "delegate")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	commit := commitFixtureChange(t, fixture.worktree, "result.txt", "result", "result")
	_, stderr, err := runHelper(t, stateDir, mutatingReport(binding, "no-session", "complete", commit), "report", "submit")
	if err == nil || !strings.Contains(stderr, "completed and acquired mutating Claude session") {
		t.Fatalf("missing mutating session error = %v, stderr %q", err, stderr)
	}
}

func TestMutatingLaunchIsAnExplicitSeparateWriterPolicy(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("experimental Claude launch is Darwin-only")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	environmentLog := filepath.Join(t.TempDir(), "claude.env")
	probeEnvironmentLog := filepath.Join(t.TempDir(), "claude-probes.env")
	fixture.environment = append(fixture.environment,
		"GH_TOKEN=must-be-removed", "GITHUB_TOKEN=must-be-removed", "GITLAB_TOKEN=must-be-removed",
		"BENIGN_SENTINEL=must-survive", "ENV_LOG="+environmentLog, "PROBE_ENV_LOG="+probeEnvironmentLog,
	)
	enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
	capacityRequest := map[string]any{
		"capacity": map[string]any{
			"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 20, "window_minutes": 300, "resets_at": futureCapacityReset(300)},
				map[string]any{"name": "secondary", "used_percent": 30, "window_minutes": 10080, "resets_at": futureCapacityReset(10080)},
			},
		},
		"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
		"acknowledged_unknown_capacity": false,
	}
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, capacityRequest, "capacity", "decide-mutating")
	if err != nil {
		t.Fatalf("capacity decision: %v: %s", err, stderr)
	}
	fixture.request["workflow"] = "mutating"
	delete(fixture.request, "expected_launch_policy_digest")
	fixture.request["baseline_branch"] = "delegate"
	fixture.request["writer_owner"] = "claude_mutating_delegate"
	fixture.request["integration_owner"] = "amp_coordinator"
	fixture.request["coordinator_write_frozen"] = true
	fixture.request["shared_writable"] = false
	fixture.request["handoff"] = "one_clean_local_commit"
	fixture.request["capacity_request"] = capacityRequest

	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("mutating launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	if plan["workflow"] != "mutating" || !strings.Contains(stdout, `"writer_authority":"exclusive"`) {
		t.Fatalf("mutating launch plan did not expose separate writer policy: %s", stdout)
	}
	tampered := cloneJSONMap(t, fixture.request)
	tampered["capacity_request"].(map[string]any)["capacity"].(map[string]any)["windows"].([]any)[0].(map[string]any)["used_percent"] = float64(99)
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, tampered, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "hard reserve floor") {
		t.Fatalf("tampered capacity decision error = %v, stderr %q", err, stderr)
	}
	binding := mutatingBinding("delegation-session-preflight", fixture.request["workdir"].(string), fixture.request["base"].(string), "delegate")
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	binding["capacity_decision_digest"] = plan["capacity_decision"].(map[string]any)["decision_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "launched", fixture.request, "launch", "execute")
	var environmentBytes []byte
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		environmentBytes, err = os.ReadFile(environmentLog)
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	environmentResult := string(environmentBytes)
	for _, removed := range []string{"GH_TOKEN=false:", "GITHUB_TOKEN=false:", "GITLAB_TOKEN=false:"} {
		if !strings.Contains(environmentResult, removed) {
			t.Errorf("mutating Claude environment did not remove credential: %s", environmentResult)
		}
	}
	if !strings.Contains(environmentResult, "BENIGN_SENTINEL=true:must-survive") {
		t.Errorf("mutating Claude environment dropped benign sentinel: %s", environmentResult)
	}
	probeEnvironmentBytes, err := os.ReadFile(probeEnvironmentLog)
	if err != nil {
		t.Fatal(err)
	}
	probeEnvironmentResult := string(probeEnvironmentBytes)
	if strings.Count(probeEnvironmentResult, "probe=--version") < 2 || strings.Count(probeEnvironmentResult, "probe=--help") < 2 {
		t.Errorf("mutating launch did not inspect both sanitized probe environments: %s", probeEnvironmentResult)
	}
	for _, removed := range []string{"GH_TOKEN=false:", "GITHUB_TOKEN=false:", "GITLAB_TOKEN=false:"} {
		if strings.Count(probeEnvironmentResult, removed) < 4 {
			t.Errorf("mutating Claude probe environment exposed credential: %s", probeEnvironmentResult)
		}
	}
	if strings.Count(probeEnvironmentResult, "BENIGN_SENTINEL=true:must-survive") < 4 {
		t.Errorf("mutating Claude probe environment dropped benign sentinel: %s", probeEnvironmentResult)
	}
	conflictingReplay := cloneJSONMap(t, fixture.request)
	conflictingReplay["capacity_request"].(map[string]any)["capacity"].(map[string]any)["windows"].([]any)[0].(map[string]any)["used_percent"] = float64(21)
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, conflictingReplay, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflicting launch replay error = %v, stderr %q", err, stderr)
	}
	log, err := os.ReadFile(fixture.tmuxLog)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "--tools") || strings.Contains(string(log), "Bash(git push:*)") {
		t.Fatalf("tmux command metadata exposed transported mutating arguments:\n%s", log)
	}
	runtimeKey := fmt.Sprintf("%x", sha256.Sum256([]byte(fixture.request["delegation_id"].(string))))
	transportBytes, err := os.ReadFile(filepath.Join(fixture.stateDir, "runtime", runtimeKey, "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var transport struct {
		Argv              []string `json:"argv"`
		RemoveEnvironment []string `json:"remove_environment"`
	}
	if err := json.Unmarshal(transportBytes, &transport); err != nil {
		t.Fatal(err)
	}
	if strings.Join(transport.RemoveEnvironment, ",") != "GH_TOKEN,GITHUB_TOKEN,GITLAB_TOKEN" {
		t.Fatalf("mutating launch environment removal = %#v", transport.RemoveEnvironment)
	}
	transportedArgv := strings.Join(transport.Argv, " ")
	for _, want := range []string{"--tools Read,Grep,Glob,Bash,Edit,Write", "Bash(git push:*)", "Bash(gh:*)", "Bash(git worktree:*)"} {
		if !strings.Contains(transportedArgv, want) {
			t.Errorf("mutating launch transport missing %q", want)
		}
	}
}

func TestMutatingLaunchRevalidatesTheLeasedBaselineBeforeRecordingIntent(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("experimental Claude launch is Darwin-only")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	capacityRequest := map[string]any{
		"capacity": map[string]any{
			"status": "supported", "provider": "claude", "source": "web", "source_version": 1, "schema_version": 1, "confidence": "reported",
			"windows": []any{
				map[string]any{"name": "primary", "used_percent": 20, "window_minutes": 300, "resets_at": futureCapacityReset(300)},
				map[string]any{"name": "secondary", "used_percent": 30, "window_minutes": 10080, "resets_at": futureCapacityReset(10080)},
			},
		},
		"reserve_floors":                map[string]any{"five_hour": 20, "weekly": 20, "model_specific": map[string]any{}},
		"acknowledged_unknown_capacity": false,
	}
	fixture.request["workflow"] = "mutating"
	delete(fixture.request, "expected_launch_policy_digest")
	fixture.request["baseline_branch"] = "delegate"
	fixture.request["writer_owner"] = "claude_mutating_delegate"
	fixture.request["integration_owner"] = "amp_coordinator"
	fixture.request["coordinator_write_frozen"] = true
	fixture.request["shared_writable"] = false
	fixture.request["handoff"] = "one_clean_local_commit"
	fixture.request["capacity_request"] = capacityRequest

	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("mutating launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := mutatingBinding("delegation-session-preflight", fixture.request["workdir"].(string), fixture.request["base"].(string), "delegate")
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	binding["capacity_decision_digest"] = plan["capacity_decision"].(map[string]any)["decision_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.dirtyAfterPreflight, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "clean immutable baseline") {
		t.Fatalf("dirty final baseline error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || bytes.Contains(after, []byte(`"kind":"launch_intent"`)) {
		t.Fatalf("dirty final baseline changed receipt before launch:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("dirty final baseline created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("dirty final baseline created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

type mutatingGitFixture struct {
	worktree string
	baseline string
}

func newMutatingGitFixture(t *testing.T) mutatingGitFixture {
	t.Helper()
	return newMutatingGitFixtureUnder(t, "")
}

func newMutatingGitFixtureUnder(t *testing.T, parent string) mutatingGitFixture {
	t.Helper()
	rootParent := parent
	worktreeParent := parent
	if parent == "" {
		rootParent = t.TempDir()
		worktreeParent = t.TempDir()
	}
	root := filepath.Join(rootParent, "repository")
	worktree := filepath.Join(worktreeParent, "delegate")
	runGit(t, "init", root)
	runGit(t, "-C", root, "config", "user.name", "Test")
	runGit(t, "-C", root, "config", "user.email", "test@example.com")
	runGit(t, "-C", root, "remote", "add", "origin", "https://github.com/zainfathoni/amux.git")
	if err := os.WriteFile(filepath.Join(root, "baseline.txt"), []byte("baseline"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", root, "add", "baseline.txt")
	runGit(t, "-C", root, "commit", "-m", "baseline")
	baseline := strings.TrimSpace(runGit(t, "-C", root, "rev-parse", "HEAD"))
	runGit(t, "-C", root, "worktree", "add", "-b", "delegate", worktree, baseline)
	return mutatingGitFixture{worktree: worktree, baseline: baseline}
}

func runGit(t *testing.T, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
	return string(output)
}

func caseVariantPath(path string) string {
	for index, character := range path {
		if character >= 'a' && character <= 'z' {
			return path[:index] + string(character-'a'+'A') + path[index+1:]
		}
		if character >= 'A' && character <= 'Z' {
			return path[:index] + string(character-'A'+'a') + path[index+1:]
		}
	}
	return path
}

func mutatingPrepareRequest(fixture mutatingGitFixture) map[string]any {
	return map[string]any{
		"workdir": fixture.worktree, "repository": "zainfathoni/amux",
		"writer_owner": "claude_mutating_delegate", "integration_owner": "amp_coordinator",
		"coordinator_write_frozen": true, "shared_writable": false, "handoff": "one_clean_local_commit",
	}
}

func mutatingBinding(delegationID, workdir, baseline, branch string) map[string]any {
	binding := testBinding(delegationID)
	binding["workdir"] = workdir
	binding["base"] = baseline
	binding["producer_role"] = "mutating_delegate"
	binding["authority"] = "exclusive_writer"
	binding["baseline_branch"] = branch
	binding["writer_owner"] = "claude_mutating_delegate"
	binding["integration_owner"] = "amp_coordinator"
	binding["handoff"] = "one_clean_local_commit"
	binding["capacity_decision_digest"] = strings.Repeat("e", 64)
	return binding
}

func createMutatingReceipt(t *testing.T, stateDir string, binding map[string]any) {
	t.Helper()
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	recordMutatingSessionFixture(t, stateDir, binding["delegation_id"].(string))
}

func recordMutatingSessionFixture(t *testing.T, stateDir, delegationID string) {
	t.Helper()
	path := filepath.Join(stateDir, "receipts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store map[string]any
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	receipt := store["receipts"].([]any)[0].(map[string]any)
	if receipt["binding"].(map[string]any)["delegation_id"] != delegationID {
		t.Fatal("test mutating receipt identity mismatch")
	}
	launchIdentity := map[string]any{"session": "Claude", "window": "delegate", "window_id": "@30", "pane_id": "%30"}
	sessionIdentity := map[string]any{"claude_session_id": "session-fixture", "pane_id": "%30"}
	receipt["events"] = append(receipt["events"].([]any),
		map[string]any{"event_id": "launch-fixture", "kind": "launch_intent", "workflow": "mutating", "at": "2026-07-18T12:00:00Z"},
		map[string]any{"event_id": "amux:launch-fixture", "kind": "launch_completed", "operation_event_id": "launch-fixture", "identity": launchIdentity, "at": "2026-07-18T12:00:01Z"},
		map[string]any{"event_id": "acquire-fixture", "kind": "session_acquired", "identity": sessionIdentity, "at": "2026-07-18T12:00:02Z"},
	)
	receipt["session_identity"] = sessionIdentity
	updated, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitFixtureChange(t *testing.T, worktree, name, contents, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(worktree, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", worktree, "add", name)
	runGit(t, "-C", worktree, "commit", "-m", message)
	return strings.TrimSpace(runGit(t, "-C", worktree, "rev-parse", "HEAD"))
}

func mutatingReport(binding map[string]any, messageID, status, handoffCommit string) map[string]any {
	changedArtifacts := []any{"result.txt"}
	if status == "blocked" {
		changedArtifacts = []any{}
	}
	message := testMessage(binding, messageID, "mutating_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": status,
		"summary": "Bounded mutating delegation result.", "blockers": []any{},
		"changed_artifacts": changedArtifacts, "verification": []any{"focused check"}, "references": []any{},
		"handoff_commit": handoffCommit, "authorship": "claude_mutating_delegate",
		"non_claims": map[string]any{"correct": false, "accepted": false, "merge_ready": false, "cleanup_authorized": false},
	})
	message["report"] = message[""]
	delete(message, "")
	message["producer_role"] = "mutating_delegate"
	message["authority"] = "exclusive_writer"
	return message
}

func assertMutatingSubmitError(t *testing.T, stateDir string, report map[string]any, want string) {
	t.Helper()
	_, stderr, err := runHelper(t, stateDir, report, "report", "submit")
	if err == nil || !strings.Contains(stderr, want) {
		t.Fatalf("mutating submit error = %v, stderr %q; want %q", err, stderr, want)
	}
	stdout, showStderr, showErr := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", report["delegation_id"].(string))
	if showErr != nil || !strings.Contains(stdout, `"state":"created"`) || strings.Contains(stdout, `"submission_frozen":true`) {
		t.Fatalf("invalid handoff changed receipt: %v: %s%s", showErr, stdout, showStderr)
	}
}

func decodeJSONMap(t *testing.T, value string) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(value), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func futureCapacityReset(windowMinutes int) string {
	return time.Now().UTC().Add(time.Duration(windowMinutes/2) * time.Minute).Format(time.RFC3339)
}
