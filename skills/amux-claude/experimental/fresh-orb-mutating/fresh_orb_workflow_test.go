package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFreshOrbMutationDiagnosticIsNonAuthorizing(t *testing.T) {
	helper, err := filepath.Abs("fresh_orb_workflow.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", helper, "diagnose")
	output, err := command.Output()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("diagnose exit = %v, want blocked exit 2", err)
	}
	var result struct {
		Outcome                  string   `json:"outcome"`
		Blocker                  string   `json:"blocker"`
		Model                    string   `json:"model"`
		Authorizing              bool     `json:"authorizing"`
		MutationAvailable        bool     `json:"mutation_available"`
		RealPilotAuthorized      bool     `json:"real_pilot_authorized"`
		RequiredNativePrimitives []string `json:"required_native_primitives"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "blocked" || result.Blocker != "native_fresh_orb_mutation_adapter_unavailable" ||
		result.Model != "claude-opus-4-8" || result.Authorizing || result.MutationAvailable ||
		result.RealPilotAuthorized || len(result.RequiredNativePrimitives) != 7 {
		t.Fatalf("unexpected diagnostic: %+v", result)
	}
}

func TestFreshOrbMutationScaffoldRejectsAuthorizingCommandsWithoutState(t *testing.T) {
	helper, err := filepath.Abs("fresh_orb_workflow.py")
	if err != nil {
		t.Fatal(err)
	}
	workingDirectory := t.TempDir()
	for _, arguments := range [][]string{
		{"-h"}, {"--help"}, {"diagnose", "-h"}, {"diagnose", "--help"},
		{"intent"}, {"authorize"}, {"launch"}, {"export"}, {"transfer"}, {"verify"}, {"checks"},
		{"process-absence"}, {"deliver"}, {"acknowledge"}, {"authorize-archive"},
		{"archive-result"}, {"authorize-cleanup"}, {"cleanup-result"},
	} {
		command := exec.Command("python3", append([]string{helper}, arguments...)...)
		command.Dir = workingDirectory
		output, err := command.CombinedOutput()
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
			t.Fatalf("arguments %q were not rejected: %v: %s", arguments, err, output)
		}
	}
	entries, err := os.ReadDir(workingDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected operations created state: %v", entries)
	}
}
