package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func helperPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestShippedHelperHasNoLegacyMutationDispatcher(t *testing.T) {
	script := `import importlib.util,pathlib,sys
spec=importlib.util.spec_from_file_location("claude_delegation",pathlib.Path(sys.argv[1]))
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
for name in ("dispatch","legacy_test_main","ReceiptStore","execute_launch","quarantine_apply"):
 assert not hasattr(module,name), name
`
	command := exec.Command("python3", "-c", script, helperPath(t))
	command.Env = append(os.Environ(), "AMUX_CLAUDE_DELEGATION_TESTING=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("import shipped helper: %v\n%s", err, output)
	}
}

func TestShippedHelperRejectsWorkerBoundRoutesBeforeMutation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	for _, args := range [][]string{
		{"receipt", "create"},
		{"report", "submit"},
		{"launch", "execute"},
		{"session", "park"},
		{"lifecycle", "worker-teardown"},
		{"quarantine", "apply"},
	} {
		command := exec.Command("python3", append([]string{helperPath(t), "--state-dir", stateDir}, args...)...)
		command.Env = append(os.Environ(), "AMUX_CLAUDE_DELEGATION_TESTING=1")
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "worker-bound Claude delegation is closed") {
			t.Fatalf("%v was not rejected: err=%v output=%s", args, err, output)
		}
		if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
			t.Fatalf("%v touched state: %v", args, err)
		}
	}
}

func TestShippedHelperReadsOneHistoricalReceiptWithoutMutation(t *testing.T) {
	stateDir := t.TempDir()
	store := map[string]any{"schema_version": 1, "receipts": []any{map[string]any{
		"state":   "created",
		"binding": map[string]any{"delegation_id": "historical"},
		"events":  []any{},
	}}}
	payload, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateDir, "receipts.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", helperPath(t), "--state-dir", stateDir, "receipt", "show", "--delegation-id", "historical")
	output, err := command.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(`"state":"created"`)) {
		t.Fatalf("show historical receipt: %v\n%s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("receipt inspection changed the historical store")
	}
}

func TestShippedHelperMissingInspectionFailsClosedWithoutState(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	command := exec.Command("python3", helperPath(t), "--state-dir", stateDir, "quarantine", "inspect")
	command.Stdin = strings.NewReader(`{"operation_sha256":"` + strings.Repeat("0", 64) + `"}`)
	output, err := command.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte(`"outcome":"blocked"`)) {
		t.Fatalf("quarantine inspection did not fail closed: %v\n%s", err, output)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("quarantine inspection touched state: %v", err)
	}
}
