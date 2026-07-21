package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/lock"
	"github.com/zainfathoni/amux/internal/result"
)

const supportedGroupHelp = "\x1b[=0u\r\n" + groupLabelUsageLine + "\r\n\r\n" + groupLabelAdditiveLine + "\r\n"

func TestGroupDeclareAddManyToManyAndCoordinatorTransitions(t *testing.T) {
	dir := t.TempDir()
	commands := installSupportedGroupAmp(t, nil)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	for _, args := range [][]string{
		{"group", "declare", "--group", "alpha", "--thread", "T-coordinator"},
		{"group", "add", "--group", "alpha", "--thread", "T-shared"},
		{"group", "declare", "--group", "beta", "--thread", "T-beta"},
		{"group", "add", "--group", "beta", "--thread", "T-shared"},
		{"group", "coordinator", "--group", "alpha", "--thread", "T-shared"},
	} {
		full := append([]string{"--config-dir", dir}, args...)
		if err := (app{}).execute(full); err != nil {
			t.Fatalf("amux %s: %v", strings.Join(args, " "), err)
		}
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil {
		t.Fatal(err)
	}
	want := []config.GroupMembership{
		{Group: "alpha", Thread: "T-shared", Role: config.GroupCoordinator},
		{Group: "alpha", Thread: "T-coordinator", Role: config.GroupMember},
		{Group: "beta", Thread: "T-beta", Role: config.GroupCoordinator},
		{Group: "beta", Thread: "T-shared", Role: config.GroupMember},
	}
	if !reflect.DeepEqual(memberships, want) {
		t.Fatalf("memberships = %+v, want %+v", memberships, want)
	}
	if got := countMutationCommands(*commands); got != 2 {
		t.Fatalf("label mutation commands = %d, commands=%v", got, *commands)
	}
}

func TestGroupAddPersistsBeforeLabelAndRetainsIntentOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	path := filepath.Join(dir, config.GroupsFile)
	installSupportedGroupAmp(t, func(args []string) ([]byte, error) {
		if reflect.DeepEqual(args, []string{"threads", "label", "T-archived", "issue-131"}) {
			memberships, err := config.LoadGroupsReadOnly(path)
			if err != nil || len(memberships) != 1 || memberships[0].Thread != "T-archived" {
				t.Fatalf("intent was not persisted before label mutation: %+v, %v", memberships, err)
			}
			return []byte("archived mutation unavailable"), errors.New("exit status 1")
		}
		return nil, nil
	})

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "group", "add", "--group", "issue-131", "--thread", "T-archived"})
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("add failure = %v, exit=%d", err, result.ExitCode(err))
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(env.Failed) != 1 || env.Failed[0].Group == nil || env.Failed[0].Group.ExternalSync != "failed" || env.Failed[0].Group.Drift != "label_may_be_missing" {
		t.Fatalf("failure envelope = %+v", env)
	}
	memberships, loadErr := config.LoadGroupsReadOnly(path)
	if loadErr != nil || len(memberships) != 1 {
		t.Fatalf("retained memberships = %+v, %v", memberships, loadErr)
	}
}

func TestGroupCapabilityPreflightAcceptsCurrentThreadUsage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	help := strings.ReplaceAll(supportedGroupHelp, groupLabelUsageLine, "Usage: amp threads label [options] <thread> <labels...>")
	calls := installGroupAmp(t, minimumGroupAmpVersion+"\n", help, nil)

	if err := (app{}).execute([]string{"--config-dir", dir, "group", "add", "--group", "group", "--thread", "T-one"}); err != nil {
		t.Fatalf("current Amp label usage rejected: %v", err)
	}
	if countMutationCommands(*calls) != 1 {
		t.Fatalf("label mutation commands = %d, commands=%v", countMutationCommands(*calls), *calls)
	}
}

func TestGroupCapabilityPreflightUsesOneExecutableAndWritesNothingWhenUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		version string
		help    string
	}{
		{name: "old version", version: "0.0.1784084981-gabcdef\n", help: supportedGroupHelp},
		{name: "different build at floor", version: "0.0.1784084982-gdeadbeef\n", help: supportedGroupHelp},
		{name: "invalid first token", version: "amp 0.0.1784084982-g029ec3\n", help: supportedGroupHelp},
		{name: "changed usage", version: minimumGroupAmpVersion + " released today\n", help: strings.ReplaceAll(supportedGroupHelp, "<labels...>", "<label>")},
		{name: "missing preservation", version: minimumGroupAmpVersion + "\n", help: groupLabelUsageLine + "\nAdds labels.\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			calls := installGroupAmp(t, test.version, test.help, nil)
			err := (app{}).execute([]string{"--config-dir", dir, "group", "add", "--group", "group", "--thread", "T-one"})
			if err == nil || result.ExitCode(err) != result.ExitRejected {
				t.Fatalf("unsupported preflight = %v, exit=%d", err, result.ExitCode(err))
			}
			if _, statErr := os.Stat(filepath.Join(dir, config.GroupsFile)); !os.IsNotExist(statErr) {
				t.Fatalf("unsupported preflight wrote groups registry: %v", statErr)
			}
			if countMutationCommands(*calls) != 0 {
				t.Fatalf("unsupported preflight attempted mutation: %v", *calls)
			}
			for _, call := range *calls {
				if call.path != (*calls)[0].path {
					t.Fatalf("preflight used multiple executables: %v", *calls)
				}
			}
		})
	}
}

func TestGroupCoordinatorMutationAndDryRunDoNotProbeAmp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldLookPath, oldExec := groupLookPath, groupExec
	groupLookPath = func(string) (string, error) { panic("coordinator dry-run probed Amp") }
	groupExec = func(string, ...string) ([]byte, error) { panic("coordinator dry-run invoked Amp") }
	t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--dry-run", "--config-dir", dir, "group", "declare", "--group", "issue-131", "--thread", "T-coordinator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.GroupsFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote registry: %v", err)
	}
	var env result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Planned) != 1 || env.Planned[0].Group.ExternalSync != "not_projected" || len(env.Successful) != 0 {
		t.Fatalf("dry-run envelope = %+v", env)
	}
	stdout.Reset()
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "group", "declare", "--group", "issue-131", "--thread", "T-coordinator"}); err != nil {
		t.Fatal(err)
	}
	env = result.Envelope{}
	if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Successful) != 1 || env.Successful[0].Group.ExternalSync != "not_projected" {
		t.Fatalf("declare envelope = %+v", env)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 1 || memberships[0].Role != config.GroupCoordinator {
		t.Fatalf("coordinator membership = %+v, %v", memberships, err)
	}
}

func TestGroupCoordinatorReassignmentReportsDemotedAndPromotedDriftWithoutProbingAmp(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.GroupsFile)
			memberships := []config.GroupMembership{
				{Group: "alpha", Thread: "T-old", Role: config.GroupCoordinator},
				{Group: "alpha", Thread: "T-new", Role: config.GroupMember},
			}
			if err := config.WriteGroups(path, memberships); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			oldLookPath, oldExec := groupLookPath, groupExec
			groupLookPath = func(string) (string, error) { panic("coordinator reassignment probed Amp") }
			groupExec = func(string, ...string) ([]byte, error) { panic("coordinator reassignment invoked Amp") }
			t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
			args := []string{"--json", "--config-dir", dir, "group", "coordinator", "--group", "alpha", "--thread", "T-new"}
			if dryRun {
				args = append([]string{"--json", "--dry-run"}, args[1:]...)
			}
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute(args); err != nil {
				t.Fatal(err)
			}
			var env result.Envelope
			if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
				t.Fatal(err)
			}
			bucket := env.Successful
			if dryRun {
				bucket = env.Planned
			}
			var promoted, demoted *result.Outcome
			for i := range bucket {
				switch bucket[i].Resource.Thread {
				case "T-new":
					promoted = &bucket[i]
				case "T-old":
					demoted = &bucket[i]
				}
			}
			if promoted == nil || promoted.Group.Role != string(config.GroupCoordinator) || promoted.Group.ExternalSync != "not_projected" || promoted.Group.Drift != "may_remain_indefinitely" {
				t.Fatalf("promoted outcome = %+v (env=%+v)", promoted, env)
			}
			if demoted == nil || demoted.Group.Role != string(config.GroupMember) || demoted.Group.ExternalSync != "additive_ensure_required" || demoted.Group.Drift != "label_may_be_missing" {
				t.Fatalf("demoted outcome = %+v (env=%+v)", demoted, env)
			}
			after, err := config.LoadGroupsReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			want := memberships
			if !dryRun {
				want = []config.GroupMembership{
					{Group: "alpha", Thread: "T-new", Role: config.GroupCoordinator},
					{Group: "alpha", Thread: "T-old", Role: config.GroupMember},
				}
			}
			if !reflect.DeepEqual(after, want) {
				t.Fatalf("memberships after reassignment (dry-run=%t) = %+v, want %+v", dryRun, after, want)
			}
		})
	}
}

func TestGroupRepeatedCoordinatorAndAddOnCoordinatorAreSkippedNoOps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.GroupsFile)
	if err := config.WriteGroups(path, []config.GroupMembership{{Group: "alpha", Thread: "T-coordinator", Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	oldLookPath, oldExec := groupLookPath, groupExec
	groupLookPath = func(string) (string, error) { panic("no-op probed Amp") }
	groupExec = func(string, ...string) ([]byte, error) { panic("no-op invoked Amp") }
	t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"declare", "coordinator", "add"} {
		var stdout bytes.Buffer
		if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "group", command, "--group", "alpha", "--thread", "T-coordinator"}); err != nil {
			t.Fatalf("group %s: %v", command, err)
		}
		var env result.Envelope
		if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
			t.Fatal(err)
		}
		if len(env.Skipped) != 1 || len(env.Planned) != 0 || len(env.Successful) != 0 || len(env.Failed) != 0 {
			t.Fatalf("group %s no-op envelope = %+v", command, env)
		}
		if env.Skipped[0].Group.Role != string(config.GroupCoordinator) || env.Skipped[0].Group.ExternalSync != "not_projected" {
			t.Fatalf("group %s skipped outcome = %+v", command, env.Skipped[0])
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("no-op mutated registry: err=%v\nbefore=%s\nafter=%s", err, before, after)
	}
}

func TestGroupDeclareRepeatWithMembersIsNoOpAndRejectsConflicts(t *testing.T) {
	// The group already has its coordinator plus an ordinary member row; a repeat
	// declare on the coordinator must be a skipped no-op that neither writes nor
	// probes Amp, while a declare naming a different thread stays a conflict.
	base := []config.GroupMembership{
		{Group: "alpha", Thread: "T-coordinator", Role: config.GroupCoordinator},
		{Group: "alpha", Thread: "T-member", Role: config.GroupMember},
	}
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.GroupsFile)
			if err := config.WriteGroups(path, base); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			oldLookPath, oldExec := groupLookPath, groupExec
			groupLookPath = func(string) (string, error) { panic("repeat declare probed Amp") }
			groupExec = func(string, ...string) ([]byte, error) { panic("repeat declare invoked Amp") }
			t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--json", "--config-dir", dir, "group", "declare", "--group", "alpha", "--thread", "T-coordinator"}
			if dryRun {
				args = append([]string{"--json", "--dry-run"}, args[1:]...)
			}
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute(args); err != nil {
				t.Fatalf("repeat declare: %v", err)
			}
			var env result.Envelope
			if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
				t.Fatal(err)
			}
			if len(env.Skipped) != 1 || len(env.Planned) != 0 || len(env.Successful) != 0 || len(env.Failed) != 0 {
				t.Fatalf("repeat declare envelope = %+v", env)
			}
			if env.Skipped[0].Group.Role != string(config.GroupCoordinator) || env.Skipped[0].Group.ExternalSync != "not_projected" {
				t.Fatalf("repeat declare skipped outcome = %+v", env.Skipped[0])
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("repeat declare mutated registry: err=%v\nbefore=%s\nafter=%s", err, before, after)
			}
		})
	}

	// A declare naming a thread that is not the existing coordinator is rejected as
	// a conflict, whether another coordinator holds the group or none does.
	for name, memberships := range map[string][]config.GroupMembership{
		"other-coordinator": {{Group: "alpha", Thread: "T-other", Role: config.GroupCoordinator}, {Group: "alpha", Thread: "T-member", Role: config.GroupMember}},
		"no-coordinator":    {{Group: "alpha", Thread: "T-member", Role: config.GroupMember}},
	} {
		t.Run("reject/"+name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.GroupsFile)
			if err := config.WriteGroups(path, memberships); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			oldLookPath, oldExec := groupLookPath, groupExec
			groupLookPath = func(string) (string, error) { panic("conflicting declare probed Amp") }
			groupExec = func(string, ...string) ([]byte, error) { panic("conflicting declare invoked Amp") }
			t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
			err := (app{}).execute([]string{"--config-dir", dir, "group", "declare", "--group", "alpha", "--thread", "T-coordinator"})
			if err == nil || result.ExitCode(err) != result.ExitRejected {
				t.Fatalf("conflicting declare = %v, exit=%d", err, result.ExitCode(err))
			}
		})
	}
}

func TestGroupRemovalIsLocalOnlyAndReportsPermanentDriftHumanAndJSON(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%t", jsonOutput), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, config.GroupsFile)
			if err := config.WriteGroups(path, []config.GroupMembership{{Group: "group", Thread: "T-one", Role: config.GroupCoordinator}}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			oldLookPath, oldExec := groupLookPath, groupExec
			groupLookPath = func(string) (string, error) { panic("removal probed Amp") }
			groupExec = func(string, ...string) ([]byte, error) { panic("removal invoked Amp") }
			t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
			var stdout, stderr bytes.Buffer
			args := []string{"--config-dir", dir, "group", "remove", "--group", "group", "--thread", "T-one"}
			if jsonOutput {
				args = append([]string{"--json"}, args...)
			}
			if err := (app{stdout: &stdout, stderr: &stderr}).execute(args); err != nil {
				t.Fatal(err)
			}
			if jsonOutput {
				var env result.Envelope
				if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
					t.Fatal(err)
				}
				if len(env.Successful) != 1 || env.Successful[0].Group.ExternalSync != "unsupported" || env.Successful[0].Group.Drift != "may_remain_indefinitely" {
					t.Fatalf("remove envelope = %+v", env)
				}
			} else if !strings.Contains(stderr.String(), "may remain on T-one indefinitely") {
				t.Fatalf("human warning = %q", stderr.String())
			}
			memberships, err := config.LoadGroupsReadOnly(path)
			if err != nil || len(memberships) != 0 {
				t.Fatalf("memberships after removal = %+v, %v", memberships, err)
			}
		})
	}
}

func TestGroupDryRunRemovalPlansSameWarningWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.GroupsFile)
	want := []config.GroupMembership{{Group: "group", Thread: "T-one", Role: config.GroupMember}}
	if err := config.WriteGroups(path, want); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--dry-run", "--config-dir", dir, "group", "remove", "--group", "group", "--thread", "T-one"}); err != nil {
		t.Fatal(err)
	}
	var env result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Planned) != 1 || env.Planned[0].Group.ExternalSync != "unsupported" || env.Planned[0].Group.Drift != "may_remain_indefinitely" {
		t.Fatalf("dry-run removal envelope = %+v", env)
	}
	got, err := config.LoadGroupsReadOnly(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("dry-run changed memberships = %+v, %v", got, err)
	}
}

func TestGroupRemovalAlreadyAbsentStillReportsPotentialPermanentDrift(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	if err := (app{stdout: &stdout, stderr: &stderr}).execute([]string{"--config-dir", dir, "group", "remove", "--group", "group", "--thread", "T-one"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "may remain indefinitely") {
		t.Fatalf("absent removal warning = %q", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := (app{stdout: &stdout, stderr: &stderr}).execute([]string{"--json", "--dry-run", "--config-dir", dir, "group", "remove", "--group", "group", "--thread", "T-one"}); err != nil {
		t.Fatal(err)
	}
	var env result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Skipped) != 1 || env.Skipped[0].Group.Role != "" || env.Skipped[0].Group.ExternalSync != "unsupported" || env.Skipped[0].Group.Drift != "may_remain_indefinitely" {
		t.Fatalf("absent removal envelope = %+v", env)
	}
}

func TestGroupListAndShowAreDeterministicLocalOnlyAcrossThreadLifecycles(t *testing.T) {
	dir := t.TempDir()
	memberships := []config.GroupMembership{
		{Group: "group", Thread: "T-worker", Role: config.GroupCoordinator},
		{Group: "group", Thread: "T-archived", Role: config.GroupMember},
		{Group: "group", Thread: "T-recovered", Role: config.GroupMember},
		{Group: "group", Thread: "T-duplicate", Role: config.GroupMember},
		{Group: "group", Thread: "T-evidence", Role: config.GroupMember},
		{Group: "group", Thread: "T-runner-managed", Role: config.GroupMember},
	}
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), memberships); err != nil {
		t.Fatal(err)
	}
	oldLookPath, oldExec := groupLookPath, groupExec
	groupLookPath = func(string) (string, error) { panic("local read probed Amp") }
	groupExec = func(string, ...string) ([]byte, error) { panic("local read invoked Amp or tmux") }
	t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
	for _, command := range []string{"list", "show"} {
		var stdout bytes.Buffer
		args := []string{"--config-dir", dir, "group", command}
		if command == "show" {
			args = append(args, "--group", "group")
		}
		if err := (app{stdout: &stdout}).execute(args); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != len(memberships) || !strings.Contains(lines[0], "T-worker\tcoordinator") {
			t.Fatalf("%s output = %q", command, stdout.String())
		}
	}
}

func TestGroupAddOnlyReconcileByMemberGroupAndAll(t *testing.T) {
	dir := t.TempDir()
	memberships := []config.GroupMembership{
		{Group: "alpha", Thread: "T-one", Role: config.GroupCoordinator},
		{Group: "alpha", Thread: "T-two", Role: config.GroupMember},
		{Group: "beta", Thread: "T-two", Role: config.GroupCoordinator},
	}
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), memberships); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	tests := []struct {
		selector    []string
		wantTargets []string
	}{
		{selector: []string{"--thread", "T-two"}, wantTargets: []string{"T-two/alpha"}},
		{selector: []string{"--thread", "T-one"}, wantTargets: nil},
		{selector: []string{"--group", "alpha"}, wantTargets: []string{"T-two/alpha"}},
		{selector: []string{"--all"}, wantTargets: []string{"T-two/alpha"}},
	}
	for _, test := range tests {
		commands := installSupportedGroupAmp(t, nil)
		args := append([]string{"--config-dir", dir, "group", "reconcile"}, test.selector...)
		if err := (app{}).execute(args); err != nil {
			t.Fatal(err)
		}
		if got := groupLabelTargets(*commands); !reflect.DeepEqual(got, test.wantTargets) {
			t.Fatalf("reconcile %v label targets = %v, want %v (%v)", test.selector, got, test.wantTargets, *commands)
		}
	}
}

func TestGroupCoordinatorOnlyReconcileNeverProbesAmp(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry-run=%t", dryRun), func(t *testing.T) {
			dir := t.TempDir()
			memberships := []config.GroupMembership{
				{Group: "alpha", Thread: "T-one", Role: config.GroupCoordinator},
				{Group: "beta", Thread: "T-one", Role: config.GroupCoordinator},
			}
			if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), memberships); err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			oldLookPath, oldExec := groupLookPath, groupExec
			groupLookPath = func(string) (string, error) { panic("coordinator reconcile probed Amp") }
			groupExec = func(string, ...string) ([]byte, error) { panic("coordinator reconcile invoked Amp") }
			t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
			args := []string{"--json", "--config-dir", dir, "group", "reconcile", "--thread", "T-one"}
			if dryRun {
				args = append([]string{"--json", "--dry-run"}, args[1:]...)
			}
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute(args); err != nil {
				t.Fatal(err)
			}
			var env result.Envelope
			if err := json.NewDecoder(&stdout).Decode(&env); err != nil {
				t.Fatal(err)
			}
			if len(env.Planned) != 0 || len(env.Successful) != 0 || len(env.Failed) != 0 || len(env.Skipped) != 2 {
				t.Fatalf("coordinator-only reconcile envelope = %+v", env)
			}
			for _, out := range env.Skipped {
				if out.Group.ExternalSync != "not_projected" {
					t.Fatalf("coordinator skip = %+v", out)
				}
			}
		})
	}
}

func TestGroupReconcileContinuesAfterFailureAndReportsMixedJSONOutcomes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.GroupsFile)
	memberships := []config.GroupMembership{
		{Group: "group", Thread: "T-one", Role: config.GroupCoordinator},
		{Group: "group", Thread: "T-three", Role: config.GroupMember},
		{Group: "group", Thread: "T-two", Role: config.GroupMember},
	}
	if err := config.WriteGroups(path, memberships); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	calls := installSupportedGroupAmp(t, func(args []string) ([]byte, error) {
		if len(args) == 4 && args[2] == "T-three" {
			return []byte("temporary failure"), errors.New("exit status 1")
		}
		return nil, nil
	})
	var stdout bytes.Buffer
	err = (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "group", "reconcile", "--all"})
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("mixed reconcile error = %v, exit=%d", err, result.ExitCode(err))
	}
	var env result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&env); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if countMutationCommands(*calls) != 2 || len(env.Successful) != 1 || len(env.Skipped) != 1 || env.Skipped[0].Group.ExternalSync != "not_projected" || len(env.Failed) != 1 || env.Failed[0].Group.Drift != "label_may_be_missing" {
		t.Fatalf("mixed reconcile calls=%v envelope=%+v", *calls, env)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("reconcile changed local intent: %v\nbefore=%s\nafter=%s", readErr, before, after)
	}
}

func TestGroupMutationUsesMachineLock(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	dir := t.TempDir()
	path, err := lock.MachinePath()
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), path, lock.Owner{PID: 131, Command: "coordinator"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release() })
	oldWait := mutationLockWait
	mutationLockWait = 20 * time.Millisecond
	t.Cleanup(func() { mutationLockWait = oldWait })
	err = (app{}).execute([]string{"--config-dir", dir, "group", "remove", "--group", "group", "--thread", "T-one"})
	if err == nil || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("lock contention = %v, exit=%d", err, result.ExitCode(err))
	}
	if _, statErr := os.Stat(filepath.Join(dir, config.GroupsFile)); !os.IsNotExist(statErr) {
		t.Fatalf("contending mutation wrote registry: %v", statErr)
	}
}

func TestWorkerConfigurationRemovalDoesNotEraseGroupHistory(t *testing.T) {
	dir := config.Directory{Path: t.TempDir()}
	if _, err := config.Store(dir.WorkersPath(), config.Row{Workspace: "work", Window: "worker", Workdir: t.TempDir(), Thread: "T-worker"}); err != nil {
		t.Fatal(err)
	}
	want := []config.GroupMembership{{Group: "history", Thread: "T-worker", Role: config.GroupCoordinator}}
	if err := config.WriteGroups(dir.GroupsPath(), want); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Remove(dir.WorkersPath(), "work", "worker"); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("group history after worker lifecycle removal = %+v, %v", got, err)
	}
}

type groupAmpCall struct {
	path string
	args []string
}

func installSupportedGroupAmp(t *testing.T, mutation func([]string) ([]byte, error)) *[]groupAmpCall {
	return installGroupAmp(t, minimumGroupAmpVersion+" (released now)\n", supportedGroupHelp, mutation)
}

func installGroupAmp(t *testing.T, version, help string, mutation func([]string) ([]byte, error)) *[]groupAmpCall {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "amp")
	if err := os.WriteFile(executable, []byte("fake"), 0o700); err != nil {
		t.Fatal(err)
	}
	calls := &[]groupAmpCall{}
	oldLookPath, oldExec := groupLookPath, groupExec
	groupLookPath = func(name string) (string, error) {
		if name != "amp" {
			t.Fatalf("LookPath(%q)", name)
		}
		return executable, nil
	}
	groupExec = func(path string, args ...string) ([]byte, error) {
		*calls = append(*calls, groupAmpCall{path: path, args: append([]string(nil), args...)})
		switch {
		case reflect.DeepEqual(args, []string{"version"}):
			return []byte(version), nil
		case reflect.DeepEqual(args, []string{"threads", "label", "--help"}):
			return []byte(help), nil
		default:
			if mutation != nil {
				return mutation(args)
			}
			return nil, nil
		}
	}
	t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })
	return calls
}

func countMutationCommands(calls []groupAmpCall) int {
	count := 0
	for _, call := range calls {
		if len(call.args) >= 2 && call.args[0] == "threads" && call.args[1] == "label" && (len(call.args) < 3 || call.args[2] != "--help") {
			count++
		}
	}
	return count
}

// groupLabelTargets returns the exact "<thread>/<label>" additive-label targets, in order.
func groupLabelTargets(calls []groupAmpCall) []string {
	var targets []string
	for _, call := range calls {
		if len(call.args) == 4 && call.args[0] == "threads" && call.args[1] == "label" {
			targets = append(targets, call.args[2]+"/"+call.args[3])
		}
	}
	return targets
}
