package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const cliRetirementRecordID = "ret_ffeeddccbbaa99887766554433221100"

func TestRetirementInspectJSONAndHumanAreReadOnlyAndPrivacySafe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	createCLIRetirementRecord(t, dir)

	var jsonOutput bytes.Buffer
	a := app{stdout: &jsonOutput, stderr: &bytes.Buffer{}}
	if err := a.execute([]string{"--json", "--config-dir", dir, "retirement", "inspect", "--record", cliRetirementRecordID}); err != nil {
		t.Fatal(err)
	}
	var envelope result.Envelope
	if err := json.Unmarshal(jsonOutput.Bytes(), &envelope); err != nil {
		t.Fatalf("decode JSON output: %v\n%s", err, jsonOutput.String())
	}
	if len(envelope.Successful) != 1 || envelope.Successful[0].Retirement == nil || envelope.Successful[0].Retirement.RecordID != cliRetirementRecordID || envelope.Successful[0].Retirement.IntegrityStatus != "verified" {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	if strings.Contains(jsonOutput.String(), "/synthetic/private") || strings.Contains(jsonOutput.String(), "prompt-bytes") {
		t.Fatalf("JSON output leaked private input: %s", jsonOutput.String())
	}

	var human bytes.Buffer
	a.stdout = &human
	if err := a.execute([]string{"--config-dir", dir, "retirement", "inspect", "--record", cliRetirementRecordID}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{cliRetirementRecordID, "integrity=verified", "events=1", "Subject commitments:"} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("human output missing %q: %s", want, human.String())
		}
	}
}

func TestRetirementInspectAbsentUsesStableCodeExitTwoAndDoesNotCreate(t *testing.T) {
	dir := t.TempDir()
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	a := app{stdout: &output, stderr: &bytes.Buffer{}}
	err = a.execute([]string{"--json", "--config-dir", dir, "retirement", "inspect", "--record", cliRetirementRecordID})
	if result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("exit=%d err=%v", result.ExitCode(err), err)
	}
	var envelope result.Envelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(envelope.Failed) != 1 || envelope.Failed[0].Error == nil || envelope.Failed[0].Error.Code != config.RetirementRecordNotFound {
		t.Fatalf("unexpected absent envelope: %+v", envelope)
	}
	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatalf("inspect created config files: before=%v after=%v", before, after)
	}
}

func TestRetirementInspectIsIndependentOfLegacyRegistryMigration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "workspaces.tsv"), []byte("legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	a := app{stdout: &output, stderr: &bytes.Buffer{}}
	err := a.execute([]string{"--json", "--config-dir", dir, "retirement", "inspect", "--record", cliRetirementRecordID})
	if result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("exit=%d err=%v output=%s", result.ExitCode(err), err, output.String())
	}
	var envelope result.Envelope
	if decodeErr := json.Unmarshal(output.Bytes(), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(envelope.Failed) != 1 || envelope.Failed[0].Error == nil || envelope.Failed[0].Error.Code != config.RetirementRecordNotFound {
		t.Fatalf("retirement inspect was blocked by migration: %+v", envelope)
	}
}

func TestRetirementInspectRejectsLatestAndMutationCommands(t *testing.T) {
	for _, args := range [][]string{
		{"retirement", "inspect"},
		{"retirement", "inspect", "--record", "latest"},
		{"retirement", "repair", "--record", cliRetirementRecordID},
		{"retirement", "truncate", "--record", cliRetirementRecordID},
		{"retirement", "prepare", "--record", cliRetirementRecordID},
		{"retirement", "finalize", "--record", cliRetirementRecordID},
	} {
		a := app{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := a.execute(args); result.ExitCode(err) != result.ExitRejected {
			t.Fatalf("args=%v exit=%d err=%v", args, result.ExitCode(err), err)
		}
	}
}

func TestRetirementInspectDoesNotEchoInvalidRecordInput(t *testing.T) {
	privateInput := "../../private-token"
	var output bytes.Buffer
	a := app{stdout: &output, stderr: &bytes.Buffer{}}
	err := a.execute([]string{"--json", "--config-dir", t.TempDir(), "retirement", "inspect", "--record", privateInput})
	if result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("exit=%d err=%v", result.ExitCode(err), err)
	}
	if strings.Contains(output.String(), privateInput) || strings.Contains(output.String(), "private-token") {
		t.Fatalf("invalid private input leaked in output: %s", output.String())
	}
}

func createCLIRetirementRecord(t *testing.T, path string) {
	t.Helper()
	commitment := func(kind, value string) string {
		digest, err := config.IdentityCommitment(kind, value)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}
	classes := []string{"amp_thread", "tmux_client_process", "git_worktree", "catalog_recovery_pointer", "provider_evidence", "descendant"}
	dispositions := map[string]string{"amp_thread": "retain", "tmux_client_process": "retain", "git_worktree": "retain_preserved", "catalog_recovery_pointer": "retain", "provider_evidence": "retain", "descendant": "retain"}
	intent := make([]config.RetirementClassIntent, len(classes))
	for index, class := range classes {
		intent[index] = config.RetirementClassIntent{Class: class, Items: []config.RetirementIntentItem{{ResourceCommitment: commitment(class, "synthetic"), ExpectedDisposition: dispositions[class], DecisionOwnerCommitment: commitment("owner", class)}}}
	}
	payload := config.RetirementRecordCreatedPayload{
		CanonicalizationVersion: config.RetirementCanonicalVersion,
		Subject: config.RetirementSubject{
			ThreadCommitment:          commitment("thread", "synthetic-thread"),
			WorkerBindingCommitment:   commitment("binding", "synthetic-binding"),
			CreatedBy:                 "adopt",
			WorkspaceCommitment:       commitment("workspace", "synthetic-workspace"),
			InitialWorktreeCommitment: commitment("worktree", "/synthetic/private"),
			PhysicalOwnerCommitment:   commitment("owner", "synthetic-owner"),
		},
		InitialIntent: intent, InitialDescendantState: "explicit_none", InitialDescendantCommitment: commitment("descendant", "none"),
	}
	request := config.RetirementEventRequest{RecordID: cliRetirementRecordID, OperationID: "create-operation", EventType: config.RetirementRecordCreated, Payload: payload, WrittenAt: time.Date(2026, 8, 12, 10, 0, 0, 123456000, time.UTC)}
	if _, _, err := config.DefaultRetirementStore().Append(context.Background(), config.Directory{Path: filepath.Clean(path)}, request); err != nil {
		t.Fatal(err)
	}
}
