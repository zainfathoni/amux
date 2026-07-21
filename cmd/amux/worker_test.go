package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

func TestWorkerSpawnDryRunDoesNotResumeStartedOperation(t *testing.T) {
	for _, thread := range []string{"", "T-bound"} {
		t.Run(map[bool]string{true: "unbound", false: "bound"}[thread == ""], func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
			sum := sha256.Sum256([]byte(request))
			now := time.Now().UTC()
			record := config.OperationRecord{Key: "dry-spawn", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseCreatingThread, Resource: config.OperationResource{Kind: "worker", Thread: thread}, CreatedAt: now, UpdatedAt: now}
			if thread != "" {
				record.Phase = config.OperationPhaseThreadBound
			}
			path := filepath.Join(dir, config.OperationsFile)
			if _, err := config.StoreOperation(path, record); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "dry-spawn")
			if len(got.Planned) != 1 {
				t.Fatalf("dry-run result = %+v", got)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("operations changed: err=%v\nbefore=%s\nafter=%s", err, before, after)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("dry-run called amp or tmux: %v", err)
			}
		})
	}
}

func TestWorkerSpawnRejectsDuplicateIssueIdentityBeforeSideEffects(t *testing.T) {
	for _, test := range []struct {
		window     string
		suggestion string
	}{
		{window: "issue-119", suggestion: "<semantic-slug>"},
		{window: "issue-119-install-update-diagnostics", suggestion: "install-update-diagnostics"},
		{window: "#119", suggestion: "<semantic-slug>"},
		{window: "#119 install-update-diagnostics", suggestion: "install-update-diagnostics"},
	} {
		t.Run(test.window, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			var stdout bytes.Buffer
			err := (app{stdout: &stdout}).execute([]string{"--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", test.window, "--workdir", workdir, "--mode", "medium", "--title-prefix", "#119", "--message", "hello", "--idempotency-key", "duplicate-title"})
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "duplicates issue identity #119") || !strings.Contains(err.Error(), test.suggestion) {
				t.Fatalf("duplicate title error = %v, exit = %d", err, result.ExitCode(err))
			}
			var envelope result.Envelope
			if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if len(envelope.Failed) != 1 || envelope.Failed[0].Error == nil || envelope.Failed[0].Error.Kind != result.ErrorPreflight {
				t.Fatalf("duplicate title JSON = %+v", envelope)
			}
			if _, statErr := os.Stat(called); !os.IsNotExist(statErr) {
				t.Fatalf("duplicate title called amp or tmux: %v", statErr)
			}
			for _, name := range []string{config.WorkersFile, config.OperationsFile} {
				if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
					t.Fatalf("duplicate title created %s: %v", name, statErr)
				}
			}
		})
	}
}

func TestWorkerSpawnIssueTitleDryRunUsesSemanticWindow(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "install-update-diagnostics", "--workdir", workdir, "--mode", "medium", "--title-prefix", "#119", "--message", "hello", "--idempotency-key", "issue-title")
	if len(got.Planned) != 1 || !strings.Contains(got.Planned[0].Message, "alpha/#119 install-update-diagnostics") {
		t.Fatalf("issue title dry-run = %+v", got)
	}
}

func TestWorkerSpawnGenericTitlePrefixDoesNotApplyIssueRules(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "issue-119-install", "--workdir", workdir, "--title-prefix", "release", "--message", "hello", "--idempotency-key", "generic-title")
	if len(got.Planned) != 1 || !strings.Contains(got.Planned[0].Message, "alpha/release issue-119-install") {
		t.Fatalf("generic title dry-run = %+v", got)
	}
}

func TestWorkerSpawnGroupsDryRunSortsDeduplicatesWithoutSideEffects(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "zeta", "--group", "alpha", "--group", "zeta", "--message", "hello", "--idempotency-key", "group-dry-run")
	if len(got.Planned) != 3 || got.Planned[1].Resource.Kind != "command" || got.Planned[1].Group.ID != "alpha" || got.Planned[2].Resource.Kind != "command" || got.Planned[2].Group.ID != "zeta" {
		t.Fatalf("grouped dry-run result = %+v", got)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("grouped dry-run called amp or tmux: %v", err)
	}
	for _, name := range []string{config.WorkersFile, config.OperationsFile, config.GroupsFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("grouped dry-run created %s: %v", name, err)
		}
	}
}

func TestWorkerSpawnRejectsAnyInvalidGroupBeforeSideEffects(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "valid", "--group", "Invalid", "--message", "hello", "--idempotency-key", "invalid-group")
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "invalid group ID") {
		t.Fatalf("invalid group error = %v, exit = %d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("invalid group called amp or tmux: %v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("invalid group mutated config: entries=%v err=%v", entries, readErr)
	}
}

func TestWorkerSpawnRejectsOverlongGroupBeforeSideEffects(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", strings.Repeat("a", 33), "--message", "hello", "--idempotency-key", "overlong-group")
	if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "at most 32 characters") {
		t.Fatalf("overlong group error = %v, exit = %d", err, result.ExitCode(err))
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("overlong group called amp or tmux: %v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("overlong group mutated config: entries=%v err=%v", entries, readErr)
	}
}

func TestWorkerSpawnDerivesRepositoryScopedTrackerNeutralNaming(t *testing.T) {
	for _, workItemID := range []string{"123", "975"} {
		t.Run(workItemID, func(t *testing.T) {
			dir := t.TempDir()
			workdir := testGitRepository(t, "https://github.com/owner/project.git")
			writeGroupNamingConfig(t, dir, "github.com/owner/project", "bta")
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "unlisted-addons", "--workdir", workdir, "--work-item-id", workItemID, "--worker-ordinal", "2", "--message", "hello", "--idempotency-key", "derived-"+workItemID)
			if len(got.Planned) != 3 || got.Planned[0].GroupNaming == nil {
				t.Fatalf("derived dry-run = %+v", got)
			}
			naming := got.Planned[0].GroupNaming
			wantGroup := "bta-" + workItemID + "-unlisted-addons"
			if naming.ProjectPrefix != "bta" || naming.WorkItemID != workItemID || naming.Slug != "unlisted-addons" || naming.GroupID != wantGroup || naming.ReportID != wantGroup+"-worker-2" || naming.ConfigSource != filepath.Join(dir, config.GroupNamingFile) {
				t.Fatalf("resolved naming = %+v", naming)
			}
			if got.Planned[2].Group == nil || got.Planned[2].Group.ID != wantGroup {
				t.Fatalf("derived group plan = %+v", got.Planned)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("derived dry-run called amp or tmux: %v", err)
			}
			for _, name := range []string{config.WorkersFile, config.OperationsFile, config.GroupsFile} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("derived dry-run created %s: %v", name, err)
				}
			}
		})
	}
}

func TestWorkerSpawnExplicitGroupOverridesAutomaticNaming(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	got := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "legacy", "--workdir", workdir, "--group", "explicit-group", "--work-item-id", "#invalid", "--worker-ordinal", "invalid", "--message", "hello", "--idempotency-key", "explicit-wins")
	if len(got.Planned) != 2 || got.Planned[1].Group == nil || got.Planned[1].Group.ID != "explicit-group" || got.Planned[0].GroupNaming != nil {
		t.Fatalf("explicit override = %+v", got)
	}
}

func TestWorkerSpawnAutomaticNamingOrdinalBoundaries(t *testing.T) {
	dir := t.TempDir()
	workdir := testGitRepository(t, "https://github.com/owner/project.git")
	writeGroupNamingConfig(t, dir, "github.com/owner/project", "bta")
	args := []string{"--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "addons", "--workdir", workdir, "--work-item-id", "975", "--worker-ordinal", "10000", "--message", "hello", "--idempotency-key", "ordinal-boundary"}
	got := executeWorkerJSON(t, args...)
	if len(got.Planned) != 3 || got.Planned[0].GroupNaming == nil || got.Planned[0].GroupNaming.ReportID != "bta-975-addons-worker-10000" {
		t.Fatalf("ordinal above former ceiling = %+v", got)
	}

	maxInt := int(^uint(0) >> 1)
	overflow := new(big.Int).Add(big.NewInt(int64(maxInt)), big.NewInt(1)).String()
	args[optionValueIndex(t, args, "--worker-ordinal")] = overflow
	if err := executeWorkerJSONError(t, args...); err == nil || !strings.Contains(err.Error(), "canonical positive integer") {
		t.Fatalf("host-int overflow ordinal error = %v", err)
	}
}

func TestWorkerSpawnAutomaticNamingResumesRealGrouping(t *testing.T) {
	dir := t.TempDir()
	workdir := testGitRepository(t, "https://github.com/owner/project.git")
	writeGroupNamingConfig(t, dir, "github.com/owner/project", "bta")
	groupID, reportID, err := config.DeriveGroupNaming("bta", "975", "unlisted-addons", 2)
	if err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "unlisted-addons", Workdir: workdir, Thread: "T-spawned"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "derived-grouping",
		Kind:        "worker-spawn",
		RequestHash: automaticNamingRequestHash("alpha", "unlisted-addons", workdir, "", "hello", groupID, "github.com/owner/project", reportID),
		State:       config.OperationStarted,
		Phase:       config.OperationPhaseConfigured,
		Resource:    config.OperationResource{Kind: "worker", Thread: row.Thread},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	calls := installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "unlisted-addons", "--workdir", workdir, "--work-item-id", "975", "--worker-ordinal", "2", "--message", "hello", "--idempotency-key", "derived-grouping")
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 1 || memberships[0].Group != groupID || memberships[0].Thread != row.Thread {
		t.Fatalf("derived membership = %+v, error = %v", memberships, err)
	}
	completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "derived-grouping")
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseGrouped {
		t.Fatalf("derived operation = %+v, found=%t, error=%v", completed, found, err)
	}
	if targets := groupLabelTargets(*calls); !reflect.DeepEqual(targets, []string{"T-spawned/" + groupID}) {
		t.Fatalf("derived label calls = %+v", *calls)
	}
	if len(got.Successful) != 2 || got.Successful[0].GroupNaming == nil || got.Successful[1].Resource.Group != groupID {
		t.Fatalf("derived grouping outcomes = %+v", got)
	}
}

func TestWorkerSpawnAutomaticNamingBindsOrdinalAndRepositoryToIdempotency(t *testing.T) {
	dir := t.TempDir()
	workdir := testGitRepository(t, "https://github.com/owner/one.git")
	configPath := filepath.Join(dir, config.GroupNamingFile)
	if err := os.WriteFile(configPath, []byte(`{"schema_version":1,"projects":[{"repository":"github.com/owner/one","prefix":"bta"},{"repository":"github.com/owner/two","prefix":"bta"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	groupID, reportID, err := config.DeriveGroupNaming("bta", "975", "addons", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "bound-naming", Kind: "worker-spawn", RequestHash: automaticNamingRequestHash("alpha", "addons", workdir, "", "hello", groupID, "github.com/owner/one", reportID), State: config.OperationStarted, Phase: config.OperationPhaseCreatingThread, Resource: config.OperationResource{Kind: "worker"}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	base := []string{"--json", "--dry-run", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "addons", "--workdir", workdir, "--work-item-id", "975", "--worker-ordinal", "1", "--message", "hello", "--idempotency-key", "bound-naming"}
	if got := executeWorkerJSON(t, base...); len(got.Planned) != 3 {
		t.Fatalf("exact automatic replay = %+v", got)
	}
	ordinalMismatch := append([]string(nil), base...)
	ordinalMismatch[optionValueIndex(t, ordinalMismatch, "--worker-ordinal")] = "2"
	if err := executeWorkerJSONError(t, ordinalMismatch...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("ordinal idempotency mismatch = %v", err)
	}
	if output, err := exec.Command("git", "-C", workdir, "remote", "set-url", "origin", "https://github.com/owner/two.git").CombinedOutput(); err != nil {
		t.Fatalf("change origin: %v: %s", err, output)
	}
	if err := executeWorkerJSONError(t, base...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("repository idempotency mismatch = %v", err)
	}
}

func TestVerifiedRepositoryIdentityRejectsAmbiguousAndOverLimitOrigins(t *testing.T) {
	t.Run("multiple URLs", func(t *testing.T) {
		workdir := testGitRepository(t, "https://github.com/owner/project.git")
		if output, err := exec.Command("git", "-C", workdir, "config", "--add", "remote.origin.url", "https://github.com/owner/other.git").CombinedOutput(); err != nil {
			t.Fatalf("add origin: %v: %s", err, output)
		}
		if _, err := verifiedRepositoryIdentity(workdir); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("ambiguous origin error = %v", err)
		}
	})
	t.Run("over limit output", func(t *testing.T) {
		workdir := testGitRepository(t, "https://github.com/owner/project.git")
		remote := "https://github.com/owner/" + strings.Repeat("a", 5000) + ".git"
		if output, err := exec.Command("git", "-C", workdir, "remote", "set-url", "origin", remote).CombinedOutput(); err != nil {
			t.Fatalf("set large origin: %v: %s", err, output)
		}
		if _, err := verifiedRepositoryIdentity(workdir); err == nil || !strings.Contains(err.Error(), "over-limit") {
			t.Fatalf("over-limit origin error = %v", err)
		}
	})
}

func TestWorkerSpawnNamingRejectionsHaveNoSideEffects(t *testing.T) {
	for _, test := range []struct {
		name       string
		configure  bool
		repository string
		workItem   string
		slug       string
		ordinal    string
		want       string
	}{
		{name: "missing config", workItem: "123", slug: "valid", ordinal: "1", want: "config is missing"},
		{name: "repository scope mismatch", configure: true, repository: "github.com/owner/other", workItem: "123", slug: "valid", ordinal: "1", want: "no project matching verified repository"},
		{name: "invalid slug", configure: true, repository: "github.com/owner/project", workItem: "123", slug: "Invalid-Slug", ordinal: "1", want: "invalid group slug"},
		{name: "over limit slug", configure: true, repository: "github.com/owner/project", workItem: "123", slug: strings.Repeat("a", 29), ordinal: "1", want: "without truncation"},
		{name: "invalid ordinal", configure: true, repository: "github.com/owner/project", workItem: "123", slug: "valid", ordinal: "01", want: "canonical positive integer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			workdir := testGitRepository(t, "git@github.com:owner/project.git")
			if test.configure {
				writeGroupNamingConfig(t, dir, test.repository, "bta")
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", test.slug, "--workdir", workdir, "--work-item-id", test.workItem, "--worker-ordinal", test.ordinal, "--message", "hello", "--idempotency-key", "rejected")
			if err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("naming rejection = %v, want %q", err, test.want)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("naming rejection called amp or tmux: %v", err)
			}
			for _, name := range []string{config.WorkersFile, config.OperationsFile, config.GroupsFile} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("naming rejection created %s: %v", name, err)
				}
			}
		})
	}
}

func testGitRepository(t *testing.T, remote string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "remote", "add", "origin", remote}} {
		command := exec.Command("git", args...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	return dir
}

func writeGroupNamingConfig(t *testing.T, dir, repository, prefix string) {
	t.Helper()
	content := `{"schema_version":1,"projects":[{"repository":` + strconv.Quote(repository) + `,"prefix":` + strconv.Quote(prefix) + `}]}`
	if err := os.WriteFile(filepath.Join(dir, config.GroupNamingFile), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func automaticNamingRequestHash(workspace, window, workdir, mode, message, groupID, repository, reportID string) string {
	fields := []string{workspace, window, workdir, mode, message, "groups", groupID, "group-naming/v1", repository, reportID}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}

func optionValueIndex(t *testing.T, args []string, option string) int {
	t.Helper()
	for index := range args {
		if args[index] == option && index+1 < len(args) {
			return index + 1
		}
	}
	t.Fatalf("missing option %s in %v", option, args)
	return -1
}

func TestWorkerSpawnDeliveryReplayNeverResubmitsOrSearchesOtherThreads(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "delivery-started",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationStarted,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-bound"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-bound"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: "T-bound"}, row)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-bound" ]; then printf '%s\n' '{"id":"T-bound","messages":[]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf '%s\t@1\t%s\n' worker `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "delivery-started")
	if err == nil || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("delivery replay error = %v", err)
	}
	updated, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "delivery-started")
	if loadErr != nil || !found || updated.State != config.OperationIndeterminate || updated.DeliveryStatus != config.OperationDeliveryUnknown {
		t.Fatalf("delivery operation = %+v found=%t err=%v", updated, found, loadErr)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if len(log) != 0 {
		t.Fatalf("delivery replay performed external work:\n%s", log)
	}
}

func TestWorkerSpawnInterruptedAdoptionReplayRequiresExplicitReconcileWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "adoption-started",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationStarted,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		ThreadAdoption: &config.OperationThreadAdoption{
			ProvisionedThread: "T-provisioned",
			ReceivingThread:   "T-receiving",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "adoption-started")
	if err == nil || !strings.Contains(err.Error(), "requires explicit --reconcile") || !strings.Contains(err.Error(), "refusing external work") {
		t.Fatalf("interrupted adoption replay error = %v", err)
	}
	updated, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), record.Key)
	if loadErr != nil || !found || updated.State != config.OperationStarted || updated.ThreadAdoption == nil || updated.ThreadAdoption.ReceivingThread != "T-receiving" {
		t.Fatalf("interrupted adoption operation = %+v found=%t err=%v", updated, found, loadErr)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("interrupted adoption replay called amp or tmux: %v", err)
	}
}

func TestWorkerListIsDeterministicLocalJSONAndFiltersShelfIntent(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir,
		"zeta\tz\t/tmp/z\tT-z\n"+
			"alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, "shelves.tsv"), []byte("# amux-schema: shelves/v1\nT-z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch "+called+"\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch "+called+"\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "list", "--shelf", "unshelved"})
	if err != nil {
		t.Fatal(err)
	}
	var got result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Successful) != 1 || got.Successful[0].Resource.Thread != "T-a" || got.Successful[0].Message != "unshelved" {
		t.Fatalf("worker list result = %+v", got)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("local worker list invoked amp or tmux: %v", err)
	}
}

func TestWorkerShelveWritesIntentBeforeArchiveAndRetriesRemoteRepair(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, "shelves.tsv"), []byte("# amux-schema: shelves/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "amp.log")
	attempt := filepath.Join(bin, "attempt")
	script := "#!/bin/sh\ngrep -q '^T-a$' '" + filepath.Join(dir, "shelves.tsv") + "' || exit 88\necho \"$*\" >> '" + log + "'\nif [ ! -e '" + attempt + "' ]; then touch '" + attempt + "'; exit 42; fi\n"
	writeExecutable(t, filepath.Join(bin, "amp"), script)
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "shelve", "--thread", "T-a"}); err == nil {
		t.Fatal("first archive succeeded, want injected failure")
	}
	stdout.Reset()
	if err := (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "shelve", "--thread", "T-a"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "threads archive T-a"); got != 2 {
		t.Fatalf("archive calls = %d, log=%q", got, data)
	}
}

func TestWorkerPinIsIdempotentAndDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "pin", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--thread", "T-a"}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || len(first.Skipped) != 0 {
		t.Fatalf("first pin = %+v", first)
	}
	second := executeWorkerJSON(t, args...)
	if len(second.Successful) != 0 || len(second.Skipped) != 1 || second.Skipped[0].Message != "already pinned" {
		t.Fatalf("second pin = %+v", second)
	}

	dryDir := t.TempDir()
	dryArgs := append([]string{"--dry-run"}, args...)
	for i, arg := range dryArgs {
		if arg == dir {
			dryArgs[i] = dryDir
		}
	}
	dry := executeWorkerJSON(t, dryArgs...)
	if len(dry.Planned) != 1 {
		t.Fatalf("dry-run pin = %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(dryDir, config.WorkersFile)); !os.IsNotExist(err) {
		t.Fatalf("dry-run pin wrote workers registry: %v", err)
	}
}

func TestWorkerPinTreatsCanonicalWorkdirAsAlreadyPinned(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-a\n")
	t.Setenv("HOME", home)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "pin", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--thread", "T-a")
	if len(result.Skipped) != 1 || result.Skipped[0].Message != "already pinned" || len(result.Successful) != 0 {
		t.Fatalf("canonical pin result = %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil || !strings.Contains(string(data), "~/project") {
		t.Fatalf("canonical pin rewrote row: data=%q err=%v", data, err)
	}
}

func TestWorkerUnshelveRemovesIntentOnlyAfterRemoteSuccess(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	shelfPath := filepath.Join(dir, config.ShelvesFile)
	if err := os.WriteFile(shelfPath, []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	attempt := filepath.Join(bin, "attempt")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ngrep -q '^T-a$' '"+shelfPath+"' || exit 88\nif [ ! -e '"+attempt+"' ]; then touch '"+attempt+"'; exit 42; fi\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "unshelve", "--thread", "T-a"}

	if err := executeWorkerJSONError(t, args...); err == nil {
		t.Fatal("first unshelve succeeded, want injected remote failure")
	}
	if data, err := os.ReadFile(shelfPath); err != nil || !strings.Contains(string(data), "T-a\n") {
		t.Fatalf("failed unshelve removed intent: data=%q err=%v", data, err)
	}
	result := executeWorkerJSON(t, args...)
	if len(result.Successful) != 1 {
		t.Fatalf("retried unshelve = %+v", result)
	}
	if data, err := os.ReadFile(shelfPath); err != nil || strings.Contains(string(data), "T-a\n") {
		t.Fatalf("successful unshelve retained intent: data=%q err=%v", data, err)
	}
}

func TestWorkerSpawnVerifiesDelayedExecutingProvisionedThreadWithoutDuplicatingMultilinePrompt(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	bufferPath := filepath.Join(bin, "buffer")
	pasteCountPath := filepath.Join(bin, "paste-count")
	delivered := filepath.Join(bin, "delivered")
	exportCountPath := filepath.Join(bin, "export-count")
	messagePath := filepath.Join(bin, "assignment.md")
	exactExportPath := filepath.Join(bin, "exact.json")
	duplicatedExportPath := filepath.Join(bin, "duplicated.json")
	transportedMessage := "Paragraph A\n\nParagraph B\n\nParagraph C\n\nParagraph D\n\nParagraph E\n\nParagraph F\n\nParagraph G"
	message := transportedMessage + "\n"
	duplicated := strings.TrimSuffix(transportedMessage, "\n\nParagraph G") + "\n\n" + transportedMessage
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		exactExportPath:      transportedMessage,
		duplicatedExportPath: duplicated,
	} {
		payload, err := json.Marshal(map[string]any{
			"id": "T-created",
			"messages": []map[string]any{
				{"role": "user", "content": content},
				{"role": "assistant", "content": "Starting the assignment", "state": "running_tools"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-created"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-created; exit 0; fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads export T-created" ]; then
  count=0; if [ -f "`+exportCountPath+`" ]; then count=$(cat "`+exportCountPath+`"); fi
  count=$((count + 1)); printf '%s\n' "$count" > "`+exportCountPath+`"
  if [ ! -e "`+delivered+`" ] || [ "$count" -lt 3 ]; then printf '%s\n' '{"id":"T-created","messages":[]}'; exit 0; fi
  pastes=$(cat "`+pasteCountPath+`")
  if [ "$pastes" -gt 1 ]; then cat "`+duplicatedExportPath+`"; else cat "`+exactExportPath+`"; fi
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = load-buffer ]; then cat > "`+bufferPath+`"; exit 0; fi
if [ "$1" = paste-buffer ]; then
  count=0; if [ -f "`+pasteCountPath+`" ]; then count=$(cat "`+pasteCountPath+`"); fi
  count=$((count + 1)); printf '%s\n' "$count" > "`+pasteCountPath+`"; exit 0
fi
if [ "$1" = capture-pane ]; then
  if [ -f "`+delivered+`" ]; then
    printf ' ┃ Paragraph A\n ┃ Paragraph B\n ┃ Paragraph C\n ┃ Paragraph D\n ┃ Paragraph E\n ┃ Paragraph F\n ┃ Paragraph G\n╭ composer ─╮\n│           │\n╰────────────╯\n'
  elif [ -f "`+pasteCountPath+`" ]; then
    printf '╭ composer ─╮\n│ Paragraph A │\n│ │\n│ Paragraph B │\n│ │\n│ Paragraph C │\n│ │\n│ Paragraph D │\n│ │\n│ Paragraph E │\n│ │\n│ Paragraph F │\n│ │\n│ Paragraph G │\n╰────────────╯\n'
  else
    printf '╭ composer ─╮\n│           │\n╰────────────╯\n'
  fi
  exit 0
fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then touch "`+delivered+`"; exit 0; fi
if [ "$1" = send-keys ]; then exit 0; fi
if [ "$1" = kill-window ]; then rm -f "`+running+`"; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "50ms")

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message-file", messagePath, "--idempotency-key", "delayed-executing")
	if len(got.Successful) != 1 || got.Successful[0].Resource.Thread != "T-created" {
		t.Fatalf("spawn result = %+v", got)
	}
	count, err := os.ReadFile(pasteCountPath)
	if err != nil || strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("multiline assignment paste count = %q, err=%v", count, err)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Thread != "T-created" {
		t.Fatalf("worker registry = %+v, err=%v", rows, err)
	}
	operation, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "delayed-executing")
	if err != nil || !found || operation.MessageSource != config.OperationMessageSourceFile || operation.SubmissionStatus != config.OperationSubmissionTransitioned || operation.DeliveryStatus != config.OperationDeliveryPersisted {
		t.Fatalf("spawn operation = %+v, found=%t, err=%v", operation, found, err)
	}
}

func TestWorkerSpawnDoesNotSubmitMultilineAssignmentThatNeverAppearsInComposer(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	running := filepath.Join(bin, "running")
	enterAttempted := filepath.Join(bin, "enter-attempted")
	pasteCountPath := filepath.Join(bin, "paste-count")
	messagePath := filepath.Join(bin, "assignment.md")
	message := "Synthetic first paragraph.\n\nSynthetic second paragraph.\n"
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-synthetic-empty"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2" = "threads new" ]; then echo T-synthetic-empty; exit 0; fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-synthetic-empty"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads export T-synthetic-empty" ]; then printf '%s\n' '{"id":"T-synthetic-empty","messages":[]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = load-buffer ]; then cat >/dev/null; exit 0; fi
if [ "$1" = paste-buffer ]; then
  count=0; if [ -f "`+pasteCountPath+`" ]; then count=$(cat "`+pasteCountPath+`"); fi
  count=$((count + 1)); printf '%s\n' "$count" > "`+pasteCountPath+`"; exit 0
fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ] && [ "$4" = Enter ]; then touch "`+enterAttempted+`"; exit 0; fi
if [ "$1" = send-keys ]; then exit 0; fi
if [ "$1" = kill-window ]; then rm -f "`+running+`"; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message-file", messagePath, "--idempotency-key", "synthetic-silent-multiline-loss")
	if err == nil {
		t.Fatal("spawn succeeded, want silent multiline loss to fail closed")
	}
	if _, statErr := os.Stat(enterAttempted); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("Enter was attempted before the complete multiline assignment was visible: %v", statErr)
	}
	if count, readErr := os.ReadFile(pasteCountPath); readErr != nil || strings.TrimSpace(string(count)) != "2" {
		t.Fatalf("bounded multiline paste attempts = %q, err=%v", count, readErr)
	}
	operation, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "synthetic-silent-multiline-loss")
	if loadErr != nil || !found || operation.State != config.OperationIndeterminate || operation.Phase != config.OperationPhaseDeliveryStarted || operation.SubmissionStatus != config.OperationSubmissionInputNotVisible || operation.DeliveryStatus != config.OperationDeliveryUnknown {
		t.Fatalf("silent-loss operation = %+v, found=%t, err=%v", operation, found, loadErr)
	}
	for _, private := range []string{message, messagePath, workdir, row.Thread} {
		if strings.Contains(operation.Error, private) {
			t.Fatalf("durable diagnostic contains private input %q: %q", private, operation.Error)
		}
	}
}

func TestWorkerSpawnPersistsBoundedOutcomeWhenMultilinePasteCommandFails(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	running := filepath.Join(bin, "running")
	messagePath := filepath.Join(bin, "assignment.md")
	message := "Synthetic first paragraph.\n\nSynthetic second paragraph.\n"
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-synthetic-submit-error"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2" = "threads new" ]; then echo T-synthetic-submit-error; exit 0; fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-synthetic-submit-error"}]'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = load-buffer ]; then cat >/dev/null; exit 42; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message-file", messagePath, "--idempotency-key", "synthetic-submit-error")
	if err == nil || !strings.Contains(err.Error(), "send initial message") {
		t.Fatalf("submit command failure = %v", err)
	}
	operation, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "synthetic-submit-error")
	if loadErr != nil || !found || operation.State != config.OperationIndeterminate || operation.Phase != config.OperationPhaseDeliveryStarted || operation.SubmissionStatus != config.OperationSubmissionError || operation.DeliveryStatus != config.OperationDeliveryUnknown {
		t.Fatalf("submit-error operation = %+v, found=%t, err=%v", operation, found, loadErr)
	}
	for _, private := range []string{message, messagePath, workdir, row.Thread} {
		if strings.Contains(operation.Error, private) {
			t.Fatalf("durable submit-error diagnostic contains private input %q: %q", private, operation.Error)
		}
	}
}

func TestResolveSpawnReceivingThreadRechecksProvisionedThreadAfterSlowFreshThreadDiscovery(t *testing.T) {
	bin := t.TempDir()
	messagePath := filepath.Join(bin, "assignment")
	visible := filepath.Join(bin, "visible")
	exportCount := filepath.Join(bin, "export-count")
	message := strings.Repeat("a", 2819)
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then
  count=0; if [ -f "`+exportCount+`" ]; then count=$(cat "`+exportCount+`"); fi
  count=$((count + 1)); printf '%s\n' "$count" > "`+exportCount+`"
  if [ -e "`+visible+`" ]; then printf '{"id":"T-provisioned","messages":[{"role":"user","content":"'; cat "`+messagePath+`"; printf '"},{"role":"assistant","content":"already executing"}]}\n'; else printf '%s\n' '{"id":"T-provisioned","messages":[]}'; fi
  exit 0
fi
if [ "$1 $2" = "threads list" ]; then
  touch "`+visible+`"
  sleep 0.2
  printf '%s\n' '[{"id":"T-provisioned"}]'
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	got, err := resolveSpawnReceivingThread("T-provisioned", message, t.TempDir(), map[string]bool{"T-provisioned": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "T-provisioned" {
		t.Fatalf("receiving thread = %q, want T-provisioned", got)
	}
	count, err := os.ReadFile(exportCount)
	if err != nil || strings.TrimSpace(string(count)) != "2" {
		t.Fatalf("provisioned export count = %q, err=%v", count, err)
	}
}

func TestVerifyAlternateReceiverBeforeCleanupRejectsProvisionedContent(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then printf '%s\n' '{"messages":[{"role":"user","content":"different"}]}'; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := verifyAlternateReceiverBeforeCleanup("T-provisioned", "T-receiver", "assignment", t.TempDir(), map[string]bool{"T-provisioned": true}, false)
	if err == nil || !strings.Contains(err.Error(), "provisioned residue T-provisioned is not empty") {
		t.Fatalf("cleanup evidence error = %v", err)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil || strings.Contains(string(log), "threads archive") {
		t.Fatalf("conflicting cleanup evidence mutated threads: err=%v\n%s", readErr, log)
	}
}

func TestResolveSpawnReceivingThreadRescansFreshCandidatesAfterProvisionedAssignmentAppears(t *testing.T) {
	bin := t.TempDir()
	workdir := t.TempDir()
	firstListComplete := filepath.Join(bin, "first-list-complete")
	provisionedProven := filepath.Join(bin, "provisioned-proven")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then
  if [ -e "`+firstListComplete+`" ]; then
    touch "`+provisionedProven+`"
    printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"assignment"}]}'
  else
    printf '%s\n' '{"id":"T-provisioned","messages":[]}'
  fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-receiver" ]; then printf '%s\n' '{"id":"T-receiver","env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}' ; exit 0; fi
if [ "$1 $2" = "threads list" ]; then
  if [ -e "`+provisionedProven+`" ]; then printf '%s\n' '[{"id":"T-provisioned"},{"id":"T-receiver"}]' ; else touch "`+firstListComplete+`"; printf '%s\n' '[{"id":"T-provisioned"}]' ; fi
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")

	_, err := resolveSpawnReceivingThread("T-provisioned", "assignment", workdir, map[string]bool{"T-provisioned": true}, false)
	if err == nil || !strings.Contains(err.Error(), "identity conflict between provisioned thread T-provisioned and fresh receiving thread(s) T-receiver") {
		t.Fatalf("final provisioned visibility ambiguity error = %v", err)
	}
}

func TestResolveSpawnReceivingThreadWaitsForExactProvisionedAssignmentBeyondSubmitWindow(t *testing.T) {
	bin := t.TempDir()
	exportCount := filepath.Join(bin, "export-count")
	visible := filepath.Join(bin, "visible")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then
  count=0; if [ -f "`+exportCount+`" ]; then count=$(cat "`+exportCount+`"); fi
  count=$((count + 1)); printf '%s\n' "$count" > "`+exportCount+`"
  if [ -e "`+visible+`" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"assignment"},{"role":"assistant","content":"already executing"}]}' ; else printf '%s\n' '{"id":"T-provisioned","messages":[]}' ; fi
  exit 0
fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-provisioned"}]'; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "50ms")
	workdir := t.TempDir()
	visibleWritten := make(chan struct{})
	go func() {
		defer close(visibleWritten)
		time.Sleep(750 * time.Millisecond)
		_ = os.WriteFile(visible, nil, 0o600)
	}()
	defer func() { <-visibleWritten }()

	got, err := resolveSpawnReceivingThread("T-provisioned", "assignment", workdir, map[string]bool{"T-provisioned": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "T-provisioned" {
		t.Fatalf("receiving thread = %q, want T-provisioned", got)
	}
	count, err := os.ReadFile(exportCount)
	parsedCount, parseErr := strconv.Atoi(strings.TrimSpace(string(count)))
	if err != nil || parseErr != nil || parsedCount < 3 {
		t.Fatalf("provisioned export count = %q, err=%v", count, err)
	}
}

func TestResolveSpawnReceivingThreadKeepsAlternateAmbiguityFailClosedDuringExtendedVisibilityWait(t *testing.T) {
	bin := t.TempDir()
	visible := filepath.Join(bin, "visible")
	workdir := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then
  if [ -e "`+visible+`" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"assignment"}]}' ; else printf '%s\n' '{"id":"T-provisioned","messages":[]}' ; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-receiver" ]; then printf '%s\n' '{"id":"T-receiver","env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}' ; exit 0; fi
if [ "$1 $2" = "threads list" ]; then
  if [ -e "`+visible+`" ]; then printf '%s\n' '[{"id":"T-provisioned"},{"id":"T-receiver"}]' ; else printf '%s\n' '[{"id":"T-provisioned"}]' ; fi
  exit 0
fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "50ms")
	visibleWritten := make(chan struct{})
	go func() {
		defer close(visibleWritten)
		time.Sleep(750 * time.Millisecond)
		_ = os.WriteFile(visible, nil, 0o600)
	}()
	defer func() { <-visibleWritten }()

	_, err := resolveSpawnReceivingThread("T-provisioned", "assignment", workdir, map[string]bool{"T-provisioned": true}, false)
	if err == nil || !strings.Contains(err.Error(), "identity conflict between provisioned thread T-provisioned and fresh receiving thread(s) T-receiver") {
		t.Fatalf("extended visibility ambiguity error = %v", err)
	}
}

func TestWorkerSpawnRecoversVerifiedProvisionedThreadFromIndeterminateWithoutResubmitting(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	messagePath := filepath.Join(bin, "assignment.md")
	exportPath := filepath.Join(bin, "export.json")
	transportedMessage := "Paragraph A\n\nParagraph B\n\nParagraph C"
	message := transportedMessage + "\n"
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"id": "T-provisioned",
		"messages": []map[string]any{
			{"role": "user", "content": transportedMessage},
			{"role": "assistant", "content": "Continuing work", "state": "running_tools"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exportPath, append(payload, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	request := strings.Join([]string{"alpha", "worker", workdir, "", message}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:              "recover-indeterminate",
		Kind:             "worker-spawn",
		RequestHash:      hex.EncodeToString(sum[:]),
		SubmissionStatus: config.OperationSubmissionTransitioned,
		DeliveryStatus:   config.OperationDeliveryMissing,
		State:            config.OperationIndeterminate,
		Phase:            config.OperationPhaseDeliveryStarted,
		Resource:         config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		Error:            "initial assignment delivery could not be verified; do not resubmit",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-provisioned"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then cat "`+exportPath+`"; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message-file", messagePath, "--idempotency-key", record.Key, "--reconcile")
	if len(got.Successful) != 1 || got.Successful[0].Resource.Thread != "T-provisioned" {
		t.Fatalf("recovered spawn result = %+v", got)
	}
	completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), record.Key)
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Resource.Thread != "T-provisioned" || completed.SubmissionStatus != config.OperationSubmissionTransitioned || completed.DeliveryStatus != config.OperationDeliveryPersisted {
		t.Fatalf("recovered operation = %+v, found=%t, err=%v", completed, found, err)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Thread != "T-provisioned" {
		t.Fatalf("recovered worker registry = %+v, err=%v", rows, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"threads new", "threads list", "threads search", "send-keys", "paste-buffer"} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("indeterminate recovery performed forbidden %q work:\n%s", forbidden, log)
		}
	}
}

func TestWorkerSpawnReconcileCompletesIndeterminateOperationAfterExactManualAdoption(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	message := "assignment"
	request := strings.Join([]string{"alpha", "worker", workdir, "", message}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:           "manual-adoption",
		Kind:          "worker-spawn",
		RequestHash:   hex.EncodeToString(sum[:]),
		MessageSource: config.OperationMessageSourceStdin,
		State:         config.OperationIndeterminate,
		Phase:         config.OperationPhaseDeliveryStarted,
		Resource:      config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		Error:         "initial assignment was not found in provisioned thread T-provisioned or one unambiguous fresh receiving thread; recovery: inspect thread T-provisioned and do not resubmit",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-provisioned"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"assignment"},{"role":"assistant","content":"already executing"}]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message-stdin", "--idempotency-key", record.Key, "--reconcile"}
	if err := (app{stdin: strings.NewReader(message), stdout: &stdout}).execute(args); err != nil {
		t.Fatalf("reconcile manually adopted worker: %v\nstdout: %s", err, stdout.String())
	}
	var got result.Envelope
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode reconciliation result: %v\nstdout: %s", err, stdout.String())
	}
	if len(got.Skipped) != 1 || got.Skipped[0].Resource.Thread != "T-provisioned" {
		t.Fatalf("manual-adoption reconciliation result = %+v", got)
	}
	completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), record.Key)
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseConfigured {
		t.Fatalf("reconciled operation = %+v, found=%t, err=%v", completed, found, err)
	}
	replayArgs := append([]string(nil), args[:len(args)-1]...)
	var replayStdout bytes.Buffer
	if err := (app{stdin: strings.NewReader(message), stdout: &replayStdout}).execute(replayArgs); err != nil {
		t.Fatalf("replay manually adopted reconciliation: %v\nstdout: %s", err, replayStdout.String())
	}
	var replayed result.Envelope
	if err := json.NewDecoder(&replayStdout).Decode(&replayed); err != nil {
		t.Fatalf("decode manual-adoption replay: %v\nstdout: %s", err, replayStdout.String())
	}
	if len(replayed.Skipped) != 1 || replayed.Skipped[0].Resource.Thread != "T-provisioned" {
		t.Fatalf("manual-adoption reconciliation replay = %+v", replayed)
	}
	var conflictingStdout bytes.Buffer
	conflictingErr := (app{stdin: strings.NewReader("different"), stdout: &conflictingStdout}).execute(args)
	if conflictingErr == nil || !strings.Contains(conflictingErr.Error(), "already bound to a different request") {
		t.Fatalf("conflicting manual-adoption reconciliation error = %v", conflictingErr)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"threads new", "threads list", "threads search", "send-keys", "paste-buffer", "new-session", "new-window", "respawn-pane"} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("manual-adoption reconciliation performed forbidden %q work:\n%s", forbidden, log)
		}
	}
}

func TestWorkerSpawnReconcileAdoptsOneFreshAlternateWithoutResubmitting(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	message := "assignment\n"
	messagePath := filepath.Join(dir, "assignment.md")
	if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	provisioned := "T-019f7519-01e8-7000-8000-000000000001"
	receiver := "T-019f7519-05d0-7000-8000-000000000001"
	window := "#104 worker"
	request := strings.Join([]string{"alpha", window, workdir, "", message, "#104", "groups", "issue-104"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	record := config.OperationRecord{
		Key:              "reconcile-alternate",
		Kind:             "worker-spawn",
		RequestHash:      hex.EncodeToString(sum[:]),
		MessageSource:    config.OperationMessageSourceFile,
		SubmissionStatus: config.OperationSubmissionEnterAttempted,
		DeliveryStatus:   config.OperationDeliveryMissing,
		State:            config.OperationIndeterminate,
		Phase:            config.OperationPhaseDeliveryStarted,
		Resource:         config.OperationResource{Kind: "worker", Thread: provisioned},
		Error:            "initial assignment delivery could not be verified; do not resubmit",
		CreatedAt:        now,
		UpdatedAt:        now.Add(3 * time.Second),
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	row := config.Row{Workspace: "alpha", Window: window, Workdir: workdir, Thread: receiver}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"`+provisioned+`"},{"id":"`+receiver+`"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+provisioned+`" ]; then printf '%s\n' '{"id":"`+provisioned+`","messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+receiver+`" ]; then printf '%s\n' '{"id":"`+receiver+`","env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
if [ "$1 $2 $3" = "threads archive `+provisioned+`" ]; then exit 0; fi
if [ "$1 $2 $3" = "threads rename `+receiver+`" ] && [ "$4" = "#104 worker" ]; then exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1 $2" = "list-panes -a" ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf '#104 worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	groupCalls := installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--title-prefix", "#104", "--group", "issue-104", "--message-file", messagePath, "--idempotency-key", record.Key, "--reconcile"}

	got := executeWorkerJSON(t, args...)
	if len(got.Successful) != 2 || got.Successful[0].Resource.Thread != receiver || got.Successful[1].Resource.Thread != receiver {
		t.Fatalf("alternate reconciliation result = %+v", got)
	}
	completed, found, err := config.LoadOperation(operationPath, record.Key)
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Resource.Thread != receiver || completed.ThreadAdoption == nil || completed.ThreadAdoption.ProvisionedThread != provisioned || completed.SubmissionStatus != config.OperationSubmissionEnterAttempted || completed.DeliveryStatus != config.OperationDeliveryAlternateReceiver {
		t.Fatalf("alternate reconciled operation = %+v found=%t err=%v", completed, found, err)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0] != row {
		t.Fatalf("alternate reconciled workers = %+v err=%v", rows, err)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 1 || memberships[0].Thread != receiver {
		t.Fatalf("alternate reconciled groups = %+v err=%v", memberships, err)
	}
	for _, call := range *groupCalls {
		if len(call.args) == 4 && call.args[0] == "threads" && call.args[1] == "label" && call.args[2] != receiver {
			t.Fatalf("alternate reconciliation grouped wrong thread: %+v", call.args)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"threads new", "threads search", "send-keys", "paste-buffer", "respawn-pane"} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("alternate reconciliation performed forbidden %q work:\n%s", forbidden, log)
		}
	}
	archiveAt := strings.Index(string(log), "amp threads archive "+provisioned)
	launchAt := strings.Index(string(log), "tmux new-session")
	if archiveAt < 0 || launchAt <= archiveAt {
		t.Fatalf("alternate reconciliation archive/launch order = archive:%d launch:%d\n%s", archiveAt, launchAt, log)
	}
	beforeReplay := string(log)
	replayArgs := append([]string(nil), args[:len(args)-1]...)
	replayed := executeWorkerJSON(t, replayArgs...)
	if len(replayed.Skipped) != 1 || replayed.Skipped[0].Resource.Thread != receiver {
		t.Fatalf("alternate reconciliation replay = %+v", replayed)
	}
	afterReplay, err := os.ReadFile(logPath)
	if err != nil || string(afterReplay) != beforeReplay {
		t.Fatalf("alternate reconciliation replay performed external work: err=%v\nbefore=%s\nafter=%s", err, beforeReplay, afterReplay)
	}
}

func TestWorkerSpawnReconcileRejectsAmbiguousAlternateWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	const provisioned = "T-019f7519-01e8-7000-8000-000000000001"
	const receiverOne = "T-019f7519-05d0-7000-8000-000000000001"
	const receiverTwo = "T-019f7519-09b8-7000-8000-000000000001"
	request := strings.Join([]string{"alpha", "worker", workdir, "", "assignment"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	record := config.OperationRecord{
		Key: "reconcile-ambiguous", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]),
		MessageSource: config.OperationMessageSourceMessage, State: config.OperationIndeterminate, Phase: config.OperationPhaseDeliveryStarted,
		Resource:  config.OperationResource{Kind: "worker", Thread: provisioned},
		Error:     "initial assignment was not found in provisioned thread " + provisioned + " or one unambiguous fresh receiving thread; recovery: inspect thread " + provisioned + " and do not resubmit",
		CreatedAt: now, UpdatedAt: now.Add(3 * time.Second),
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(operationPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"`+provisioned+`"},{"id":"`+receiverOne+`"},{"id":"`+receiverTwo+`"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+provisioned+`" ]; then printf '%s\n' '{"messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+receiverOne+`" ] || [ "$1 $2 $3" = "threads export `+receiverTwo+`" ]; then
  printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}'; exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
exit 99
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err = executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "assignment", "--idempotency-key", record.Key, "--reconcile")
	if err == nil || !strings.Contains(err.Error(), "multiple exact fresh alternate receivers") {
		t.Fatalf("ambiguous reconciliation error = %v", err)
	}
	after, readErr := os.ReadFile(operationPath)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("ambiguous reconciliation mutated operation: err=%v\nbefore=%s\nafter=%s", readErr, before, after)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, forbidden := range []string{"threads new", "threads archive", "threads rename", "threads search", "send-keys", "paste-buffer", "tmux "} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("ambiguous reconciliation performed forbidden %q work:\n%s", forbidden, log)
		}
	}
}

func TestWorkerSpawnReconcileRejectsOnlyMatchingReceiverAfterOriginalCutoffWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	const provisioned = "T-019f7519-01e8-7000-8000-000000000001"
	const lateReceiver = "T-019f7519-1190-7000-8000-000000000001"
	request := strings.Join([]string{"alpha", "#104 worker", workdir, "", "assignment", "#104", "groups", "issue-104"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	record := config.OperationRecord{
		Key: "reconcile-late-only", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]),
		MessageSource: config.OperationMessageSourceMessage, State: config.OperationIndeterminate, Phase: config.OperationPhaseDeliveryStarted,
		Resource:  config.OperationResource{Kind: "worker", Thread: provisioned},
		Error:     "initial assignment was not found in provisioned thread " + provisioned + " or one unambiguous fresh receiving thread; recovery: inspect thread " + provisioned + " and do not resubmit",
		CreatedAt: now, UpdatedAt: now.Add(3 * time.Second),
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(operationPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"`+provisioned+`"},{"id":"`+lateReceiver+`"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+provisioned+`" ]; then printf '%s\n' '{"messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+lateReceiver+`" ]; then printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
exit 99
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	groupCalls := installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })

	err = executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--title-prefix", "#104", "--group", "issue-104", "--message", "assignment", "--idempotency-key", record.Key, "--reconcile")
	if err == nil || !strings.Contains(err.Error(), "no exact fresh alternate receiver") {
		t.Fatalf("late-only reconciliation error = %v", err)
	}
	after, readErr := os.ReadFile(operationPath)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("late-only reconciliation mutated operation: err=%v\nbefore=%s\nafter=%s", readErr, before, after)
	}
	if rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile)); loadErr != nil || len(rows) != 0 {
		t.Fatalf("late-only reconciliation workers = %+v err=%v", rows, loadErr)
	}
	if memberships, loadErr := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile)); loadErr != nil || len(memberships) != 0 {
		t.Fatalf("late-only reconciliation groups = %+v err=%v", memberships, loadErr)
	}
	if len(*groupCalls) != 0 {
		t.Fatalf("late-only reconciliation called group label API: %+v", *groupCalls)
	}
	log, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, forbidden := range []string{"threads new", "threads archive", "threads rename", "threads label", "threads search", "send-keys", "paste-buffer", "tmux "} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("late-only reconciliation performed forbidden %q work:\n%s", forbidden, log)
		}
	}
}

func TestWorkerSpawnReconcileResumesPendingAlternateAdoptionWithoutResubmitting(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	const provisioned = "T-019f7519-01e8-7000-8000-000000000001"
	const receiver = "T-019f7519-05d0-7000-8000-000000000001"
	const laterMatch = "T-019f7519-1190-7000-8000-000000000001"
	request := strings.Join([]string{"alpha", "worker", workdir, "", "assignment"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	record := config.OperationRecord{
		Key: "reconcile-pending-adoption", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]),
		MessageSource: config.OperationMessageSourceMessage, State: config.OperationIndeterminate, Phase: config.OperationPhaseDeliveryStarted,
		Resource:  config.OperationResource{Kind: "worker", Thread: provisioned},
		Error:     "initial assignment was not found in provisioned thread " + provisioned + " or one unambiguous fresh receiving thread; recovery: inspect thread " + provisioned + " and do not resubmit",
		CreatedAt: now, UpdatedAt: now,
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	if _, err := config.BeginIndeterminateWorkerSpawnThreadAdoption(operationPath, record.Key, provisioned, receiver); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: receiver}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads list" ]; then
  if echo "$*" | grep -q -- --include-archived; then printf '%s\n' '[{"id":"`+provisioned+`"},{"id":"`+receiver+`"},{"id":"`+laterMatch+`"}]'; else printf '%s\n' '[{"id":"`+receiver+`"},{"id":"`+laterMatch+`"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export `+provisioned+`" ]; then printf '%s\n' '{"messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+receiver+`" ]; then printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+laterMatch+`" ]; then printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1 $2" = "list-panes -a" ]; then exit 0; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "assignment", "--idempotency-key", record.Key, "--reconcile")
	if len(got.Successful) != 1 || got.Successful[0].Resource.Thread != receiver {
		t.Fatalf("pending adoption reconciliation result = %+v", got)
	}
	completed, found, err := config.LoadOperation(operationPath, record.Key)
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Resource.Thread != receiver || completed.Phase != config.OperationPhaseConfigured {
		t.Fatalf("pending adoption operation = %+v found=%t err=%v", completed, found, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"threads new", "threads archive", "threads search", "send-keys", "paste-buffer", "respawn-pane"} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("pending adoption reconciliation performed forbidden %q work:\n%s", forbidden, log)
		}
	}
	if strings.Contains(string(log), "threads export "+laterMatch) {
		t.Fatalf("pending adoption widened its receiver cutoff to inspect later match:\n%s", log)
	}
}

func TestWorkerSpawnRejectsIndeterminateRecoveryWithoutExplicitReconcile(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	message := "assignment"
	request := strings.Join([]string{"alpha", "worker", workdir, "", message}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "implicit-recovery-rejected",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationIndeterminate,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		Error:       "initial assignment was not found in provisioned thread T-provisioned or one unambiguous fresh receiving thread; recovery: inspect thread T-provisioned and do not resubmit",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(operationPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "external-call")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err = executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", message, "--idempotency-key", record.Key)
	if err == nil || !strings.Contains(err.Error(), "requires explicit --reconcile") {
		t.Fatalf("implicit reconciliation error = %v", err)
	}
	after, err := os.ReadFile(operationPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("implicit reconciliation mutated operation: err=%v\nbefore=%s\nafter=%s", err, before, after)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("implicit reconciliation performed external work: %v", err)
	}
}

func TestWorkerSpawnReconcileRejectsOrdinaryPartialPhasesWithoutExternalWork(t *testing.T) {
	for _, phase := range []config.OperationPhase{
		config.OperationPhaseMessageVerified,
		config.OperationPhaseConfigured,
		config.OperationPhaseGroupIntent,
		config.OperationPhaseGrouped,
	} {
		t.Run(string(phase), func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			message := "assignment"
			request := strings.Join([]string{"alpha", "worker", workdir, "", message}, "\x00")
			sum := sha256.Sum256([]byte(request))
			now := time.Now().UTC()
			record := config.OperationRecord{
				Key:         "partial-phase-reconciliation",
				Kind:        "worker-spawn",
				RequestHash: hex.EncodeToString(sum[:]),
				State:       config.OperationStarted,
				Phase:       phase,
				Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			operationPath := filepath.Join(dir, config.OperationsFile)
			if _, err := config.StoreOperation(operationPath, record); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(operationPath)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			called := filepath.Join(bin, "external-call")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err = executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", message, "--idempotency-key", record.Key, "--reconcile")
			if err == nil || !strings.Contains(err.Error(), "recoverable exact provisioned-thread timeout state") {
				t.Fatalf("partial-phase reconciliation error = %v", err)
			}
			after, err := os.ReadFile(operationPath)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("partial-phase reconciliation mutated operation: err=%v\nbefore=%s\nafter=%s", err, before, after)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("partial-phase reconciliation performed external work: %v", err)
			}
		})
	}
}

func TestWorkerSpawnReconcileRequiresExistingIdenticalOperation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "assignment", "--idempotency-key", "missing-reconciliation", "--reconcile")
	if err == nil || !strings.Contains(err.Error(), "requires an existing identical operation in the recoverable exact provisioned-thread timeout state") {
		t.Fatalf("missing reconciliation operation error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, config.OperationsFile)); !os.IsNotExist(err) {
		t.Fatalf("missing reconciliation created operation state: %v", err)
	}
}

func TestWorkerSpawnReconcileLaunchesMissingClientForExactManualPin(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "assignment"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "manual-adoption-needs-launch",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationIndeterminate,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		Error:       "initial assignment was not found in provisioned thread T-provisioned or one unambiguous fresh receiving thread; recovery: inspect thread T-provisioned and do not resubmit",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	writeWorkerRegistry(t, dir, config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-provisioned"}.String()+"\n")
	before, err := os.ReadFile(operationPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	running := filepath.Join(bin, "running")
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-provisioned"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "assignment", "--idempotency-key", record.Key, "--reconcile")
	if len(got.Skipped) != 1 || got.Skipped[0].Resource.Thread != "T-provisioned" {
		t.Fatalf("missing-client reconciliation result = %+v", got)
	}
	completed, found, err := config.LoadOperation(operationPath, record.Key)
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseConfigured {
		t.Fatalf("missing-client reconciled operation = %+v, found=%t, err=%v", completed, found, err)
	}
	if _, err := os.Stat(running); err != nil {
		t.Fatalf("missing-client reconciliation did not launch local client: %v", err)
	}
	after, err := os.ReadFile(operationPath)
	if err != nil || bytes.Equal(after, before) {
		t.Fatalf("missing-client reconciliation did not transition operation: err=%v\nbefore=%s\nafter=%s", err, before, after)
	}
}

func TestWorkerSpawnIndeterminateRecoveryUsesOriginalMessageSource(t *testing.T) {
	for _, test := range []struct {
		name           string
		originalSource config.OperationMessageSource
		replayWithFile bool
		wantVerified   bool
	}{
		{name: "message replayed with file remains exact", originalSource: config.OperationMessageSourceMessage, replayWithFile: true},
		{name: "file replayed with message retains normalization", originalSource: config.OperationMessageSourceFile, wantVerified: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			message := "assignment\n"
			request := strings.Join([]string{"alpha", "worker", workdir, "", message}, "\x00")
			sum := sha256.Sum256([]byte(request))
			now := time.Now().UTC()
			record := config.OperationRecord{
				Key:           "source-bound-recovery",
				Kind:          "worker-spawn",
				RequestHash:   hex.EncodeToString(sum[:]),
				MessageSource: test.originalSource,
				State:         config.OperationIndeterminate,
				Phase:         config.OperationPhaseDeliveryStarted,
				Resource:      config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
				Error:         "initial assignment was not found in provisioned thread T-provisioned or one unambiguous fresh receiving thread; recovery: inspect thread T-provisioned and do not resubmit",
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			tmuxCalled := filepath.Join(bin, "tmux-called")
			writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
exit 2
`)
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+tmuxCalled+"'\nexit 99\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", message, "--idempotency-key", record.Key}
			if test.replayWithFile {
				messagePath := filepath.Join(dir, "assignment.md")
				if err := os.WriteFile(messagePath, []byte(message), 0o600); err != nil {
					t.Fatal(err)
				}
				args = []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message-file", messagePath, "--idempotency-key", record.Key}
			}
			args = append(args, "--reconcile")

			err := executeWorkerJSONError(t, args...)
			if test.wantVerified {
				if err == nil || strings.Contains(err.Error(), "not verified in exact provisioned thread") {
					t.Fatalf("source-bound recovery did not retain file normalization: %v", err)
				}
				if _, statErr := os.Stat(tmuxCalled); statErr != nil {
					t.Fatalf("verified recovery did not reach tmux identity preflight: %v", statErr)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), "not verified in exact provisioned thread") {
					t.Fatalf("source-bound recovery accepted replay-source normalization: %v", err)
				}
				if _, statErr := os.Stat(tmuxCalled); !os.IsNotExist(statErr) {
					t.Fatalf("unverified recovery reached tmux: %v", statErr)
				}
			}
		})
	}
}

func TestWorkerSpawnIndeterminateRecoveryPreflightsConflictsBeforeOperationMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "conflicting-recovery",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationIndeterminate,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		Error:       "initial assignment was not found in provisioned thread T-provisioned or one unambiguous fresh receiving thread; recovery: inspect thread T-provisioned and do not resubmit",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	writeWorkerRegistry(t, dir, config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-other"}.String()+"\n")
	before, err := os.ReadFile(operationPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	tmuxCalled := filepath.Join(bin, "tmux-called")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"hello"}]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+tmuxCalled+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err = executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", record.Key, "--reconcile")
	if err == nil || !strings.Contains(err.Error(), "configured for thread T-other") {
		t.Fatalf("conflicting recovery error = %v", err)
	}
	after, err := os.ReadFile(operationPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("conflicting recovery mutated operation: err=%v\nbefore=%s\nafter=%s", err, before, after)
	}
	if _, err := os.Stat(tmuxCalled); !os.IsNotExist(err) {
		t.Fatalf("conflicting recovery inspected or mutated tmux: %v", err)
	}
}

func TestWorkerSpawnIndeterminateRecoveryRejectsTmuxConflictBeforeOperationMutation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{
		Key:         "tmux-conflicting-recovery",
		Kind:        "worker-spawn",
		RequestHash: hex.EncodeToString(sum[:]),
		State:       config.OperationIndeterminate,
		Phase:       config.OperationPhaseDeliveryStarted,
		Resource:    config.OperationResource{Kind: "worker", Thread: "T-provisioned"},
		Error:       "initial assignment was not found in provisioned thread T-provisioned or one unambiguous fresh receiving thread; recovery: inspect thread T-provisioned and do not resubmit",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	operationPath := filepath.Join(dir, config.OperationsFile)
	if _, err := config.StoreOperation(operationPath, record); err != nil {
		t.Fatal(err)
	}
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-provisioned"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	operationBefore, err := os.ReadFile(operationPath)
	if err != nil {
		t.Fatal(err)
	}
	workersBefore, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2 $3" = "threads export T-provisioned" ]; then printf '%s\n' '{"id":"T-provisioned","messages":[{"role":"user","content":"hello"}]}'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\tcd /tmp && exec amp threads continue T-other\n'; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err = executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", record.Key, "--reconcile")
	if err == nil || !strings.Contains(err.Error(), "worker tmux identity is conflict") {
		t.Fatalf("tmux-conflicting recovery error = %v", err)
	}
	operationAfter, err := os.ReadFile(operationPath)
	if err != nil || !bytes.Equal(operationAfter, operationBefore) {
		t.Fatalf("tmux-conflicting recovery mutated operation: err=%v\nbefore=%s\nafter=%s", err, operationBefore, operationAfter)
	}
	workersAfter, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil || !bytes.Equal(workersAfter, workersBefore) {
		t.Fatalf("tmux-conflicting recovery mutated workers: err=%v\nbefore=%s\nafter=%s", err, workersBefore, workersAfter)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"threads new", "threads list", "threads search", "send-keys", "paste-buffer", "new-session", "new-window"} {
		if strings.Contains(string(log), forbidden) {
			t.Fatalf("tmux-conflicting recovery performed forbidden %q work:\n%s", forbidden, log)
		}
	}
}

func TestDuplicatedPrefixAssignmentMatchesOnlyKnownMultilineCorruptionShape(t *testing.T) {
	want := "Paragraph A\n\nParagraph B\n\nParagraph C"
	knownDuplicate := strings.TrimSuffix(want, "Paragraph C") + want
	for _, test := range []struct {
		name        string
		got         string
		wantMessage string
		want        bool
	}{
		{name: "exact", got: want, wantMessage: want, want: true},
		{name: "known multiline prefix", got: knownDuplicate, wantMessage: want, want: true},
		{name: "known multiline prefix with LF terminator", got: knownDuplicate + "\n", wantMessage: want + "\n", want: true},
		{name: "known multiline prefix with CRLF terminator", got: strings.ReplaceAll(knownDuplicate, "\n", "\r\n") + "\r\n", wantMessage: strings.ReplaceAll(want, "\n", "\r\n") + "\r\n", want: true},
		{name: "one byte prefix", got: want[:1] + want, wantMessage: want},
		{name: "earlier line prefix", got: "Paragraph A\n" + want, wantMessage: want},
		{name: "whole multiline message duplicated", got: want + want, wantMessage: want},
		{name: "single line duplicated", got: "hellohello", wantMessage: "hello"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := duplicatedPrefixAssignment(test.got, test.wantMessage); got != test.want {
				t.Fatalf("duplicatedPrefixAssignment() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestProvisionedAssignmentMatchesOnlyOneRemovedTerminalLineEnding(t *testing.T) {
	for _, test := range []struct {
		name string
		got  string
		want string
		ok   bool
	}{
		{name: "no newline exact", got: "assignment", want: "assignment", ok: true},
		{name: "LF removed", got: "assignment", want: "assignment\n", ok: true},
		{name: "CRLF removed", got: "assignment", want: "assignment\r\n", ok: true},
		{name: "one of two LFs removed", got: "assignment\n", want: "assignment\n\n", ok: true},
		{name: "one of two CRLFs removed", got: "assignment\r\n", want: "assignment\r\n\r\n", ok: true},
		{name: "trailing spaces preserved", got: "assignment  ", want: "assignment  \n", ok: true},
		{name: "leading spaces preserved", got: "  assignment", want: "  assignment\n", ok: true},
		{name: "empty remains rejected", got: "", want: ""},
		{name: "LF-only must not become empty evidence", got: "", want: "\n"},
		{name: "CRLF-only must not become empty evidence", got: "", want: "\r\n"},
		{name: "two LFs retain one", got: "\n", want: "\n\n", ok: true},
		{name: "two CRLFs retain one", got: "\r\n", want: "\r\n\r\n", ok: true},
		{name: "unrelated content", got: "different", want: "assignment\n"},
		{name: "trailing space not trimmed", got: "assignment", want: "assignment \n"},
		{name: "additional newline not trimmed", got: "assignment", want: "assignment\n\n"},
		{name: "bare CR is not a transport line ending", got: "assignment", want: "assignment\r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			messages := []any{map[string]any{"role": "user", "content": test.got}}
			if got := messagesContainProvisionedAssignment(messages, test.want, true); got != test.ok {
				t.Fatalf("messagesContainProvisionedAssignment() = %t, want %t", got, test.ok)
			}
		})
	}
}

func TestAssignmentWithAdditionalContentIsExternalWork(t *testing.T) {
	messages := []any{map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": "assignment"},
			map[string]any{"type": "text", "text": "conflicting extra content"},
		},
	}}
	if !messagesContainWorkBeyondAssignment(messages, "assignment", false) {
		t.Fatal("exact assignment block with conflicting sibling content was treated as unstarted")
	}
}

func TestAmpUUIDv7ThreadTimeProvidesStrictFreshnessBoundary(t *testing.T) {
	provisioned, ok := ampUUIDv7ThreadTime("T-019f7519-01e8-7000-8000-000000000001")
	if !ok || !provisioned.Equal(time.Date(2026, 7, 18, 12, 0, 1, 0, time.UTC)) {
		t.Fatalf("provisioned UUIDv7 time = %s ok=%t", provisioned, ok)
	}
	stale, staleOK := ampUUIDv7ThreadTime("T-019f7518-fe00-7000-8000-000000000001")
	fresh, freshOK := ampUUIDv7ThreadTime("T-019f7519-05d0-7000-8000-000000000001")
	if !staleOK || !freshOK || !stale.Before(provisioned) || !fresh.After(provisioned) {
		t.Fatalf("UUIDv7 ordering stale=%s provisioned=%s fresh=%s", stale, provisioned, fresh)
	}
	for _, invalid := range []string{"T-provisioned", "T-019f7519-01e8-6000-8000-000000000001", "T-019f7519-01e8-7000-0000-000000000001"} {
		if _, ok := ampUUIDv7ThreadTime(invalid); ok {
			t.Fatalf("accepted non-UUIDv7 freshness identity %q", invalid)
		}
	}
}

func TestFindIndeterminateSpawnReceiverRejectsCandidateAfterOriginalCutoff(t *testing.T) {
	workdir := t.TempDir()
	const provisioned = "T-019f7519-01e8-7000-8000-000000000001"
	const lateReceiver = "T-019f7519-1190-7000-8000-000000000001"
	operation := config.OperationRecord{
		State: config.OperationIndeterminate, Phase: config.OperationPhaseDeliveryStarted,
		Resource:  config.OperationResource{Kind: "worker", Thread: provisioned},
		CreatedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 7, 18, 12, 0, 3, 0, time.UTC),
	}
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"`+provisioned+`"},{"id":"`+lateReceiver+`"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+provisioned+`" ]; then printf '%s\n' '{"messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export `+lateReceiver+`" ]; then printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"assignment"}]}'; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := findIndeterminateSpawnReceiver(operation, "assignment", workdir, false)
	if err == nil || !strings.Contains(err.Error(), "no exact fresh alternate receiver") {
		t.Fatalf("late alternate receiver error = %v", err)
	}
}

func TestFindIndeterminateSpawnReceiverRejectsInvalidDurableCutoffs(t *testing.T) {
	const provisioned = "T-019f7519-01e8-7000-8000-000000000001"
	for _, test := range []struct {
		name     string
		updated  time.Time
		adoption *config.OperationThreadAdoption
		want     string
	}{
		{
			name:    "original cutoff before provisioned thread",
			updated: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC),
			want:    "precedes provisioned thread",
		},
		{
			name:     "pending receiver has no UUIDv7 boundary",
			updated:  time.Date(2026, 7, 18, 12, 0, 3, 0, time.UTC),
			adoption: &config.OperationThreadAdoption{ProvisionedThread: provisioned, ReceivingThread: "T-receiving"},
			want:     "invalid or inconsistent UUIDv7 freshness evidence",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation := config.OperationRecord{
				State: config.OperationIndeterminate, Phase: config.OperationPhaseDeliveryStarted,
				Resource: config.OperationResource{Kind: "worker", Thread: provisioned}, ThreadAdoption: test.adoption,
				CreatedAt: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC), UpdatedAt: test.updated,
			}
			bin := t.TempDir()
			writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
if [ "$1 $2 $3" = "threads export `+provisioned+`" ]; then printf '%s\n' '{"messages":[]}'; exit 0; fi
exit 2
`)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			_, err := findIndeterminateSpawnReceiver(operation, "assignment", t.TempDir(), false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid cutoff error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestExactAssignmentMatcherDoesNotNormalizeAlternateThreadContent(t *testing.T) {
	messages := []any{map[string]any{"role": "user", "content": "assignment"}}
	if messagesContainExactUserAssignment(messages, "assignment\n") {
		t.Fatal("alternate-thread matcher accepted a removed terminal line ending")
	}
	if messagesContainProvisionedAssignment(messages, "assignment\n", false) {
		t.Fatal("non-file provisioned assignment accepted a removed terminal line ending")
	}
}

func TestWorkerSpawnPersistsOperationAndReplaysWithoutDuplicateCreation(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	delivered := filepath.Join(bin, "delivered")
	renameAttempted := filepath.Join(bin, "rename-attempted")
	row := config.Row{Workspace: "alpha", Window: "#119 worker", Workdir: workdir, Thread: "T-spawned"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "#119 worker", Thread: "T-spawned"}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-spawned; exit 0; fi
if [ "$1 $2 $3" = "threads export T-spawned" ]; then
  if [ -e "`+delivered+`" ]; then printf '%s\n' '{"id":"T-spawned","messages":[{"role":"user","content":"hello"}]}'; else printf '%s\n' '{"id":"T-spawned","messages":[]}'; fi
  exit 0
fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-spawned"}]'; exit 0; fi
if [ "$1 $2" = "threads search" ]; then printf '%s\n' '[{"id":"T-spawned"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads rename T-spawned" ] && [ "$4" = "#119 worker" ]; then
  if [ ! -e "`+renameAttempted+`" ]; then touch "`+renameAttempted+`"; exit 42; fi
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf '%s\t@1\t%s\n' '#119 worker' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--title-prefix", "#119", "--message", "hello", "--idempotency-key", "spawn-1"}

	if err := executeWorkerJSONError(t, args...); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("first spawn rename error = %v", err)
	}
	failedRecord, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "spawn-1")
	if err != nil || !found || failedRecord.State != config.OperationStarted || failedRecord.Phase != config.OperationPhaseMessageVerified {
		t.Fatalf("rename-failed operation = %+v found=%t err=%v", failedRecord, found, err)
	}
	if rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile)); loadErr != nil || len(rows) != 0 {
		t.Fatalf("rename failure stored worker rows = %+v err=%v", rows, loadErr)
	}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 1 || first.Successful[0].Resource.Thread != "T-spawned" {
		t.Fatalf("spawn result = %+v", first)
	}
	record, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "spawn-1")
	if err != nil || !found || record.State != config.OperationSucceeded || record.Resource.Thread != "T-spawned" {
		t.Fatalf("spawn operation = %+v found=%t err=%v", record, found, err)
	}
	second := executeWorkerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Resource.Thread != "T-spawned" {
		t.Fatalf("spawn replay = %+v", second)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(log), "amp threads new"); got != 1 {
		t.Fatalf("spawn creation calls = %d\n%s", got, log)
	}
	if got := strings.Count(string(log), "amp threads rename T-spawned #119 worker"); got != 2 {
		t.Fatalf("spawn rename calls = %d\n%s", got, log)
	}
	withoutPrefix := append([]string(nil), args...)
	for i := 0; i < len(withoutPrefix); i++ {
		if withoutPrefix[i] == "--window" {
			withoutPrefix[i+1] = "#119 worker"
		}
		if withoutPrefix[i] == "--title-prefix" {
			withoutPrefix = append(withoutPrefix[:i], withoutPrefix[i+2:]...)
			break
		}
	}
	if err := executeWorkerJSONError(t, withoutPrefix...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("prefix rename intent key mismatch error = %v", err)
	}

	mismatch := append([]string(nil), args...)
	for i, arg := range mismatch {
		if arg == "hello" {
			mismatch[i] = "different"
		}
	}
	if err := executeWorkerJSONError(t, mismatch...); err == nil || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("spawn key mismatch error = %v", err)
	}
}

func TestWorkerSpawnLabelFailureRetainsIntentAndRetryResumesGroupingOnly(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	delivered := filepath.Join(bin, "delivered")
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-spawned; exit 0; fi
if [ "$1 $2 $3" = "threads export T-spawned" ]; then
  if [ -e "`+delivered+`" ]; then printf '%s\n' '{"id":"T-spawned","messages":[{"role":"user","content":"hello"}]}'; else printf '%s\n' '{"id":"T-spawned","messages":[]}'; fi
  exit 0
fi
if [ "$1 $2" = "threads list" ]; then printf '%s\n' '[{"id":"T-spawned"}]'; exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
	failedOnce := false
	calls := installSupportedGroupAmp(t, func(args []string) ([]byte, error) {
		if args[3] == "zeta" && !failedOnce {
			failedOnce = true
			return []byte("label unavailable"), errors.New("exit status 1")
		}
		return nil, nil
	})
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "zeta", "--group", "alpha", "--group", "zeta", "--message", "hello", "--idempotency-key", "group-retry"}

	first, firstErr := executeWorkerJSONResult(t, args...)
	if firstErr == nil || result.ExitCode(firstErr) != result.ExitRuntimeFailure {
		t.Fatalf("first grouped spawn error = %v", firstErr)
	}
	if len(first.Successful) != 2 || first.Successful[0].Resource.Kind != "worker" || first.Successful[1].Resource.Group != "alpha" || len(first.Failed) != 1 || first.Failed[0].Resource.Group != "zeta" {
		t.Fatalf("first grouped spawn outcomes = %+v", first)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 2 || memberships[0].Group != "alpha" || memberships[0].Thread != "T-spawned" || memberships[1].Group != "zeta" || memberships[1].Thread != "T-spawned" {
		t.Fatalf("retained spawn group intent = %+v err=%v", memberships, err)
	}
	record, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "group-retry")
	if err != nil || !found || record.State != config.OperationStarted || record.Phase != config.OperationPhaseGroupIntent {
		t.Fatalf("partial grouped operation = %+v found=%t err=%v", record, found, err)
	}
	beforeRetry, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	retryArgs := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "alpha", "--group", "zeta", "--message", "hello", "--idempotency-key", "group-retry"}
	retry := executeWorkerJSON(t, retryArgs...)
	if len(retry.Skipped) != 1 || !strings.Contains(retry.Skipped[0].Message, "grouping only") || len(retry.Successful) != 2 || len(retry.Failed) != 0 {
		t.Fatalf("grouping retry outcomes = %+v", retry)
	}
	record, found, err = config.LoadOperation(filepath.Join(dir, config.OperationsFile), "group-retry")
	if err != nil || !found || record.State != config.OperationSucceeded || record.Phase != config.OperationPhaseGrouped {
		t.Fatalf("completed grouped operation = %+v found=%t err=%v", record, found, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil || !bytes.Equal(log, beforeRetry) || strings.Count(string(log), "amp threads new") != 1 {
		t.Fatalf("grouping retry recreated or resubmitted: err=%v\n%s", err, log)
	}
	var labels []string
	for _, call := range *calls {
		if len(call.args) == 4 && call.args[0] == "threads" && call.args[1] == "label" {
			labels = append(labels, call.args[2]+"/"+call.args[3])
		}
	}
	if got, want := strings.Join(labels, ","), "T-spawned/alpha,T-spawned/zeta,T-spawned/alpha,T-spawned/zeta"; got != want {
		t.Fatalf("label attempts = %q, want %q", got, want)
	}
	conflict := append([]string(nil), retryArgs...)
	for i, arg := range conflict {
		if arg == "zeta" {
			conflict[i] = "beta"
		}
	}
	if err := executeWorkerJSONError(t, conflict...); err == nil || result.ExitCode(err) != result.ExitRejected || !strings.Contains(err.Error(), "different request") {
		t.Fatalf("changed group set idempotency conflict = %v", err)
	}
}

func TestWorkerSpawnGroupingReplayHumanFailureShowsWorkerIntentAndDrift(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "issue-132", Thread: row.Thread, Role: config.GroupMember}}); err != nil {
		t.Fatal(err)
	}
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello", "groups", "issue-132"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "human-group-replay", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseConfigured, Resource: config.OperationResource{Kind: "worker", Thread: row.Thread}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	installSupportedGroupAmp(t, func([]string) ([]byte, error) { return []byte("label unavailable"), errors.New("exit status 1") })
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	err := (app{stdout: &stdout, stderr: &stderr}).execute([]string{"--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "issue-132", "--message", "hello", "--idempotency-key", "human-group-replay"})
	if err == nil || result.ExitCode(err) != result.ExitRuntimeFailure {
		t.Fatalf("human grouping replay error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Worker T-spawned already exists; resuming durable grouping only") || !strings.Contains(stderr.String(), "retained local membership issue-132/T-spawned") || !strings.Contains(stderr.String(), "drift remains") {
		t.Fatalf("human grouping output:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("human grouping replay called creation tools: %v", err)
	}
	if strings.Contains(stdout.String(), "Spawned worker") {
		t.Fatalf("human grouping replay falsely claimed worker creation: %s", stdout.String())
	}
}

func TestWorkerSpawnResumeGroupingSkipsLabelWhenMembershipBecameCoordinator(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	// The spawned thread was later promoted to coordinator of its group; coordinator
	// identity is durable local metadata and must never be projected to an Amp label.
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "issue-140", Thread: row.Thread, Role: config.GroupCoordinator}}); err != nil {
		t.Fatal(err)
	}
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello", "groups", "issue-140"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "coordinator-resume", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseConfigured, Resource: config.OperationResource{Kind: "worker", Thread: row.Thread}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	calls := installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "issue-140", "--message", "hello", "--idempotency-key", "coordinator-resume")
	if len(got.Failed) != 0 {
		t.Fatalf("coordinator grouping resume failed: %+v", got)
	}
	var attach *result.Outcome
	for i := range got.Skipped {
		if got.Skipped[i].Action == "attach-group" {
			attach = &got.Skipped[i]
		}
	}
	if attach == nil || attach.Group.Role != string(config.GroupCoordinator) || attach.Group.ExternalSync != "not_projected" {
		t.Fatalf("coordinator attach outcome = %+v (env=%+v)", attach, got)
	}
	if got := countMutationCommands(*calls); got != 0 {
		t.Fatalf("coordinator resume projected a label: %v", *calls)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 1 || memberships[0].Role != config.GroupCoordinator {
		t.Fatalf("coordinator role not preserved: %+v, %v", memberships, err)
	}
	completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "coordinator-resume")
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseGrouped {
		t.Fatalf("coordinator grouping did not complete = %+v found=%t err=%v", completed, found, err)
	}
}

func TestWorkerSpawnAllCoordinatorGroupingReplayNeverProbesAmp(t *testing.T) {
	base := func(t *testing.T) (dir, workdir string) {
		t.Helper()
		dir = t.TempDir()
		workdir = t.TempDir()
		row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
		writeWorkerRegistry(t, dir, row.String()+"\n")
		// Both requested groups already have this thread as their coordinator; coordinator
		// identity is durable local-only metadata that is never projected to an Amp label,
		// so the replay must not resolve or invoke Amp at all.
		if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{
			{Group: "issue-140", Thread: row.Thread, Role: config.GroupCoordinator},
			{Group: "issue-141", Thread: row.Thread, Role: config.GroupCoordinator},
		}); err != nil {
			t.Fatal(err)
		}
		request := strings.Join([]string{"alpha", "worker", workdir, "", "hello", "groups", "issue-140", "issue-141"}, "\x00")
		sum := sha256.Sum256([]byte(request))
		now := time.Now().UTC()
		record := config.OperationRecord{Key: "coordinator-only", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseConfigured, Resource: config.OperationResource{Kind: "worker", Thread: row.Thread}, CreatedAt: now, UpdatedAt: now}
		if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
			t.Fatal(err)
		}
		return dir, workdir
	}
	spawnArgs := func(dir, workdir string, prefix ...string) []string {
		return append(prefix, "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "issue-140", "--group", "issue-141", "--message", "hello", "--idempotency-key", "coordinator-only")
	}

	oldLookPath, oldExec := groupLookPath, groupExec
	groupLookPath = func(string) (string, error) { panic("coordinator-only spawn probed Amp") }
	groupExec = func(string, ...string) ([]byte, error) { panic("coordinator-only spawn invoked Amp") }
	t.Cleanup(func() { groupLookPath, groupExec = oldLookPath, oldExec })

	t.Run("dry-run", func(t *testing.T) {
		dir, workdir := base(t)
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		env := executeWorkerJSON(t, spawnArgs(dir, workdir, "--json", "--dry-run")...)
		for _, out := range env.Planned {
			if out.Action == "attach-group" {
				t.Fatalf("coordinator dry-run falsely planned a member label: %+v", env)
			}
		}
		attachSkips := 0
		for _, out := range env.Skipped {
			if out.Action == "attach-group" {
				attachSkips++
				if out.Group.Role != string(config.GroupCoordinator) || out.Group.ExternalSync != "not_projected" {
					t.Fatalf("coordinator dry-run skip = %+v", out)
				}
			}
		}
		if attachSkips != 2 {
			t.Fatalf("coordinator dry-run attach skips = %d (env=%+v)", attachSkips, env)
		}
	})

	t.Run("execution", func(t *testing.T) {
		dir, workdir := base(t)
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		env := executeWorkerJSON(t, spawnArgs(dir, workdir, "--json")...)
		if len(env.Failed) != 0 {
			t.Fatalf("coordinator-only spawn failed: %+v", env)
		}
		attachSkips := 0
		for _, out := range env.Skipped {
			if out.Action == "attach-group" {
				attachSkips++
				if out.Group.Role != string(config.GroupCoordinator) || out.Group.ExternalSync != "not_projected" {
					t.Fatalf("coordinator execution skip = %+v", out)
				}
			}
		}
		if attachSkips != 2 {
			t.Fatalf("coordinator execution attach skips = %d (env=%+v)", attachSkips, env)
		}
		completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "coordinator-only")
		if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseGrouped {
			t.Fatalf("coordinator-only grouping did not complete = %+v found=%t err=%v", completed, found, err)
		}
	})
}

func TestWorkerSpawnMixedGroupingReplayPreflightsAndLabelsMembersOnly(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	// One group is a coordinator (never projected); the other is an ordinary member
	// whose label must still be add-only ensured, so Amp preflight is still required.
	if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{
		{Group: "issue-140", Thread: row.Thread, Role: config.GroupCoordinator},
		{Group: "issue-141", Thread: row.Thread, Role: config.GroupMember},
	}); err != nil {
		t.Fatal(err)
	}
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello", "groups", "issue-140", "issue-141"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "mixed-resume", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseGroupIntent, Resource: config.OperationResource{Kind: "worker", Thread: row.Thread}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	calls := installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })

	env := executeWorkerJSON(t, "--json", "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "issue-140", "--group", "issue-141", "--message", "hello", "--idempotency-key", "mixed-resume")
	if len(env.Failed) != 0 {
		t.Fatalf("mixed grouping resume failed: %+v", env)
	}
	// Preflight ran and exactly the member group was projected.
	if got := groupLabelTargets(*calls); !reflect.DeepEqual(got, []string{row.Thread + "/issue-141"}) {
		t.Fatalf("mixed replay label targets = %v (calls=%v)", got, *calls)
	}
	coordSkips := 0
	for _, out := range env.Skipped {
		if out.Action == "attach-group" {
			coordSkips++
			if out.Group.Role != string(config.GroupCoordinator) || out.Group.ExternalSync != "not_projected" {
				t.Fatalf("mixed replay coordinator skip = %+v", out)
			}
		}
	}
	memberSuccess := 0
	for _, out := range env.Successful {
		if out.Action == "attach-group" && out.Group.Role == string(config.GroupMember) {
			memberSuccess++
		}
	}
	if coordSkips != 1 || memberSuccess != 1 {
		t.Fatalf("mixed replay outcomes: coordSkips=%d memberSuccess=%d (env=%+v)", coordSkips, memberSuccess, env)
	}
	completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "mixed-resume")
	if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseGrouped {
		t.Fatalf("mixed grouping did not complete = %+v found=%t err=%v", completed, found, err)
	}
}

func TestWorkerSpawnExactRowPartialPhasesResumeGroupingWithoutWorkerSuccess(t *testing.T) {
	for _, phase := range []config.OperationPhase{config.OperationPhaseMessageVerified, config.OperationPhaseConfigured} {
		t.Run(string(phase), func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-spawned"}
			writeWorkerRegistry(t, dir, row.String()+"\n")
			if err := config.WriteGroups(filepath.Join(dir, config.GroupsFile), []config.GroupMembership{{Group: "issue-132", Thread: row.Thread, Role: config.GroupMember}}); err != nil {
				t.Fatal(err)
			}
			request := strings.Join([]string{"alpha", "worker", workdir, "", "hello", "groups", "issue-132"}, "\x00")
			sum := sha256.Sum256([]byte(request))
			now := time.Now().UTC()
			record := config.OperationRecord{Key: "phase-replay", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: phase, Resource: config.OperationResource{Kind: "worker", Thread: row.Thread}, CreatedAt: now, UpdatedAt: now}
			if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
				t.Fatal(err)
			}
			installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })
			bin := t.TempDir()
			called := filepath.Join(bin, "called")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			if phase == config.OperationPhaseMessageVerified {
				start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
				writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
touch "`+called+`"
exit 99
`)
			} else {
				writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			got := executeWorkerJSON(t, "--json", "--config-dir", dir, "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--group", "issue-132", "--message", "hello", "--idempotency-key", "phase-replay")
			if len(got.Skipped) != 1 || got.Skipped[0].Resource.Kind != "worker" || len(got.Successful) != 1 || got.Successful[0].Resource.Kind != "group_membership" {
				t.Fatalf("%s replay outcomes = %+v", phase, got)
			}
			completed, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "phase-replay")
			if err != nil || !found || completed.State != config.OperationSucceeded || completed.Phase != config.OperationPhaseGrouped {
				t.Fatalf("%s completed operation = %+v found=%t err=%v", phase, completed, found, err)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("%s replay called creation tools: %v", phase, err)
			}
		})
	}
}

func TestWorkerSpawnAdoptsSoleFreshActiveReceivingThread(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	running := filepath.Join(bin, "running")
	delivered := filepath.Join(bin, "delivered")
	pasted := filepath.Join(bin, "pasted")
	identity := filepath.Join(bin, "identity")
	messagePath := filepath.Join(dir, "assignment.md")
	if err := os.WriteFile(messagePath, []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRow := config.Row{Workspace: "alpha", Window: "#104 worker", Workdir: workdir, Thread: "T-created"}
	newRow := config.Row{Workspace: "alpha", Window: "#104 worker", Workdir: workdir, Thread: "T-receiver"}
	oldStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "#104 worker", Thread: oldRow.Thread}, oldRow)
	newStart := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "#104 worker", Thread: newRow.Thread}, newRow)
	writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-created; exit 0; fi
if [ "$1 $2" = "threads list" ]; then
  if [ -e "`+delivered+`" ]; then printf '%s\n' '[{"id":"T-created"},{"id":"T-receiver"}]'; else printf '%s\n' '[{"id":"T-created"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-created" ]; then printf '%s\n' '{"id":"T-created","messages":[]}'; exit 0; fi
if [ "$1 $2 $3" = "threads export T-receiver" ]; then printf '%s\n' '{"id":"T-receiver","env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"hello"}]}'; exit 0; fi
if [ "$1 $2" = "threads search" ]; then printf '%s\n' '[{"id":"T-receiver"}]'; exit 0; fi
if [ "$1 $2 $3" = "threads archive T-created" ]; then exit 0; fi
if [ "$1 $2 $3" = "threads rename T-receiver" ] && [ "$4" = "#104 worker" ]; then exit 0; fi
exit 2
`)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; echo T-created > "`+identity+`"; exit 0; fi
if [ "$1" = list-panes ]; then
  if grep -q T-receiver "`+identity+`"; then printf '#104 worker\t@1\t%s\n' `+shellSingleQuote(newStart)+`; else printf '#104 worker\t@1\t%s\n' `+shellSingleQuote(oldStart)+`; fi
  exit 0
fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then
  if [ -e "`+delivered+`" ]; then printf ' ┃ hello\n╭ composer ─╮\n│           │\n╰────────────╯\n';
  elif [ -e "`+pasted+`" ]; then printf '╭ composer ─╮\n│ hello │\n╰────────────╯\n';
  else printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; fi
  exit 0
fi
if [ "$1" = load-buffer ]; then cat >/dev/null; exit 0; fi
if [ "$1" = paste-buffer ]; then touch "`+pasted+`"; exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
if [ "$1" = respawn-pane ]; then echo T-receiver > "`+identity+`"; exit 0; fi
if [ "$1" = kill-window ]; then rm -f "`+running+`"; exit 0; fi
exit 2
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
	groupCalls := installSupportedGroupAmp(t, func([]string) ([]byte, error) { return nil, nil })
	args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--title-prefix", "#104", "--group", "issue-132", "--message-file", messagePath, "--idempotency-key", "adopt-1"}

	first := executeWorkerJSON(t, args...)
	if len(first.Successful) != 2 || first.Successful[0].Resource.Thread != "T-receiver" || first.Successful[1].Resource.Group != "issue-132" {
		t.Fatalf("adopted spawn result = %+v", first)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Thread != "T-receiver" {
		t.Fatalf("adopted worker registry = %+v err=%v", rows, err)
	}
	record, found, err := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "adopt-1")
	if err != nil || !found || record.State != config.OperationSucceeded || record.Resource.Thread != "T-receiver" || record.SubmissionStatus != config.OperationSubmissionTransitioned || record.DeliveryStatus != config.OperationDeliveryAlternateReceiver {
		t.Fatalf("adopted operation = %+v found=%t err=%v", record, found, err)
	}
	memberships, err := config.LoadGroupsReadOnly(filepath.Join(dir, config.GroupsFile))
	if err != nil || len(memberships) != 1 || memberships[0].Group != "issue-132" || memberships[0].Thread != "T-receiver" {
		t.Fatalf("adopted group membership = %+v err=%v", memberships, err)
	}
	for _, call := range *groupCalls {
		if len(call.args) == 4 && call.args[0] == "threads" && call.args[1] == "label" && (call.args[2] != "T-receiver" || call.args[3] != "issue-132") {
			t.Fatalf("group label targeted abandoned identity: %+v", call.args)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tmux respawn-pane -k -t @1", "amp threads archive T-created", "amp threads rename T-receiver #104 worker"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("adoption log missing %q:\n%s", want, log)
		}
	}
	respawnAt := strings.Index(string(log), "tmux respawn-pane -k -t @1")
	archiveAt := strings.Index(string(log), "amp threads archive T-created")
	renameAt := strings.Index(string(log), "amp threads rename T-receiver #104 worker")
	if respawnAt < 0 || archiveAt <= respawnAt || renameAt <= archiveAt {
		t.Fatalf("adoption cleanup order = respawn:%d archive:%d rename:%d\n%s", respawnAt, archiveAt, renameAt, log)
	}
	beforeReplay := string(log)
	second := executeWorkerJSON(t, args...)
	if len(second.Skipped) != 1 || second.Skipped[0].Resource.Thread != "T-receiver" {
		t.Fatalf("adopted spawn replay = %+v", second)
	}
	afterReplay, err := os.ReadFile(logPath)
	if err != nil || string(afterReplay) != beforeReplay {
		t.Fatalf("adopted replay performed external work: err=%v\nbefore=%s\nafter=%s", err, beforeReplay, afterReplay)
	}
	if got := strings.Count(beforeReplay, "amp threads new"); got != 1 {
		t.Fatalf("alternate adoption created %d threads:\n%s", got, beforeReplay)
	}
}

func TestWorkerSpawnAlternateDeliveryFailsClosedWhenOwnershipIsAmbiguous(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
	}{
		{name: "archived", wantErr: "is archived"},
		{name: "duplicate", wantErr: "multiple fresh receiving threads T-receiver, T-second"},
		{name: "bound-duplicate", wantErr: "identity conflict between provisioned thread T-created and fresh receiving thread(s) T-receiver"},
		{name: "delayed-bound-duplicate", wantErr: "identity conflict between provisioned thread T-created and fresh receiving thread(s) T-receiver"},
		{name: "slow-discovery-bound-duplicate", wantErr: "identity conflict between provisioned thread T-created and fresh receiving thread(s) T-receiver"},
		{name: "started-receiver", wantErr: "fresh receiving thread T-receiver already started external work"},
		{name: "timeout", wantErr: "list fresh receiving threads after delivery"},
		{name: "identity-conflict", wantErr: "receiving thread T-receiver is already configured as beta/existing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			workdir := t.TempDir()
			bin := t.TempDir()
			logPath := filepath.Join(bin, "calls.log")
			running := filepath.Join(bin, "running")
			delivered := filepath.Join(bin, "delivered")
			listCount := filepath.Join(bin, "list-count")
			provisionedVisible := filepath.Join(bin, "provisioned-visible")
			row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-created"}
			start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "worker", Thread: row.Thread}, row)
			if tt.name == "identity-conflict" {
				writeWorkerRegistry(t, dir, config.Row{Workspace: "beta", Window: "existing", Workdir: workdir, Thread: "T-receiver"}.String()+"\n")
			}
			writeExecutable(t, filepath.Join(bin, "amp"), `#!/bin/sh
echo "amp $*" >> "`+logPath+`"
if [ "$1 $2" = "threads new" ]; then echo T-created; exit 0; fi
if [ "$1 $2" = "threads list" ]; then
  count=0; if [ -f "`+listCount+`" ]; then count=$(cat "`+listCount+`"); fi; count=$((count + 1)); printf '%s\n' "$count" > "`+listCount+`"
  if [ ! -e "`+delivered+`" ]; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
  if [ "`+tt.name+`" = timeout ]; then echo 'thread list timed out' >&2; exit 1; fi
  if [ "`+tt.name+`" = delayed-bound-duplicate ] && [ "$count" -lt 4 ]; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
  if [ "`+tt.name+`" = slow-discovery-bound-duplicate ]; then touch "`+provisionedVisible+`"; sleep 0.2; fi
  if [ "`+tt.name+`" = archived ] && ! echo "$*" | grep -q -- --include-archived; then printf '%s\n' '[{"id":"T-created"}]'; exit 0; fi
  if [ "`+tt.name+`" = duplicate ]; then printf '%s\n' '[{"id":"T-created"},{"id":"T-receiver"},{"id":"T-second"}]'; else printf '%s\n' '[{"id":"T-created"},{"id":"T-receiver"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-created" ]; then
  if { [ "`+tt.name+`" = bound-duplicate ] || [ "`+tt.name+`" = delayed-bound-duplicate ]; } && [ -e "`+delivered+`" ] || [ -e "`+provisionedVisible+`" ]; then printf '%s\n' '{"id":"T-created","messages":[{"role":"user","content":"hello"}]}'; else printf '%s\n' '{"id":"T-created","messages":[]}'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads export T-receiver" ] || [ "$1 $2 $3" = "threads export T-second" ]; then
  if [ "`+tt.name+`" = started-receiver ]; then printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"working"}]}' ; exit 0; fi
  printf '%s\n' '{"env":{"initial":{"trees":[{"uri":"file://`+workdir+`"}]}},"messages":[{"role":"user","content":"hello"}]}'
  exit 0
fi
if [ "$1 $2" = "threads search" ]; then
  if [ "`+tt.name+`" = timeout ]; then echo 'search timed out' >&2; exit 1; fi
  if [ "`+tt.name+`" = duplicate ]; then printf '%s\n' '[{"id":"T-receiver"},{"id":"T-second"}]'; else printf '%s\n' '[{"id":"T-receiver"}]'; fi
  exit 0
fi
if [ "$1 $2 $3" = "threads archive T-created" ]; then exit 0; fi
exit 2
`)
			writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
echo "tmux $*" >> "`+logPath+`"
if [ "$1" = has-session ]; then test -e "`+running+`"; exit $?; fi
if [ "$1" = new-session ]; then touch "`+running+`"; exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
if [ "$1" = display-message ]; then echo %1; exit 0; fi
if [ "$1" = capture-pane ]; then printf '╭ composer ─╮\n│           │\n╰────────────╯\n'; exit 0; fi
if [ "$1" = load-buffer ]; then cat >/dev/null; exit 0; fi
if [ "$1" = paste-buffer ]; then exit 0; fi
if [ "$1" = send-keys ]; then if [ "$4" = Enter ]; then touch "`+delivered+`"; fi; exit 0; fi
if [ "$1" = kill-window ]; then rm -f "`+running+`"; exit 0; fi
exit 2
`)
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
			t.Setenv("AMP_TMUX_SPAWN_DELAY", "0")
			args := []string{"--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "ambiguous-1"}

			err := executeWorkerJSONError(t, args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "recovery:") {
				t.Fatalf("ambiguous spawn error = %v, want %q with recovery", err, tt.wantErr)
			}
			rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
			wantRows := 0
			if tt.name == "identity-conflict" {
				wantRows = 1
			}
			if loadErr != nil || len(rows) != wantRows {
				t.Fatalf("ambiguous spawn registry = %+v err=%v", rows, loadErr)
			}
			record, found, loadErr := config.LoadOperation(filepath.Join(dir, config.OperationsFile), "ambiguous-1")
			if loadErr != nil || !found || record.State != config.OperationIndeterminate {
				t.Fatalf("ambiguous operation = %+v found=%t err=%v", record, found, loadErr)
			}
			log, readErr := os.ReadFile(logPath)
			if readErr != nil || !strings.Contains(string(log), "tmux kill-window -t @1") || strings.Contains(string(log), "amp threads archive") {
				t.Fatalf("ambiguous spawn did not stop unconfigured window: err=%v\n%s", readErr, log)
			}
			beforeReplay := string(log)
			if replayErr := executeWorkerJSONError(t, args...); replayErr == nil || !strings.Contains(replayErr.Error(), "terminal in state indeterminate") {
				t.Fatalf("ambiguous replay error = %v", replayErr)
			}
			afterReplay, readErr := os.ReadFile(logPath)
			if readErr != nil || string(afterReplay) != beforeReplay {
				t.Fatalf("ambiguous replay performed external work: err=%v\nbefore=%s\nafter=%s", readErr, beforeReplay, afterReplay)
			}
		})
	}
}

func TestWorkerSpawnRecoversCanonicalExactRow(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-bound\n")
	t.Setenv("HOME", home)
	request := strings.Join([]string{"alpha", "worker", workdir, "", "hello"}, "\x00")
	sum := sha256.Sum256([]byte(request))
	now := time.Now().UTC()
	record := config.OperationRecord{Key: "canonical-recovery", Kind: "worker-spawn", RequestHash: hex.EncodeToString(sum[:]), State: config.OperationStarted, Phase: config.OperationPhaseMessageVerified, Resource: config.OperationResource{Kind: "worker", Thread: "T-bound"}, CreatedAt: now, UpdatedAt: now}
	if _, err := config.StoreOperation(filepath.Join(dir, config.OperationsFile), record); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	row := config.Row{Workspace: "alpha", Window: "worker", Workdir: workdir, Thread: "T-bound"}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: row.Workspace, Session: row.Workspace, Window: row.Window, Thread: row.Thread}, row)
	writeExecutable(t, filepath.Join(bin, "tmux"), `#!/bin/sh
if [ "$1" = has-session ]; then exit 0; fi
if [ "$1" = list-panes ]; then printf 'worker\t@1\t%s\n' `+shellSingleQuote(start)+`; exit 0; fi
touch "`+called+`"
exit 99
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "spawn", "--workspace", "alpha", "--window", "worker", "--workdir", workdir, "--message", "hello", "--idempotency-key", "canonical-recovery")
	if len(result.Skipped) != 1 || result.Skipped[0].Message != "worker already created; resuming verified spawn" {
		t.Fatalf("canonical recovery result = %+v", result)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("canonical recovery performed unexpected external work: %v", err)
	}
}

func TestWorkerRemoveDoesNotArchive(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "amp-called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	removed := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "remove", "--thread", "T-a")
	if len(removed.Successful) != 1 {
		t.Fatalf("remove result = %+v", removed)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("worker remove changed remote archive state: %v", err)
	}
}

func TestWorkerTeardownRequiresExactlyOneConfiguredWorker(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\nalpha\tb\t/tmp/b\tT-b\n")
	shelvesPath := filepath.Join(dir, config.ShelvesFile)
	if err := os.WriteFile(shelvesPath, []byte("# amux-schema: shelves/v1\nT-a\nT-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workersBefore, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil {
		t.Fatal(err)
	}
	shelvesBefore, err := os.ReadFile(shelvesPath)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	var stdout bytes.Buffer
	err = (app{stdout: &stdout}).execute([]string{"--json", "--config-dir", dir, "worker", "teardown", "--workspace", "alpha"})
	if err == nil || !strings.Contains(err.Error(), "exactly one configured worker") || result.ExitCode(err) != result.ExitRejected {
		t.Fatalf("multi-worker teardown error = %v, want rejected exact-one preflight", err)
	}
	var got result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&got); decodeErr != nil {
		t.Fatalf("decode multi-worker teardown: %v\nstdout: %s", decodeErr, stdout.String())
	}
	if len(got.Failed) != 1 || got.Failed[0].Error == nil || got.Failed[0].Error.Kind != result.ErrorPreflight {
		t.Fatalf("multi-worker teardown result = %+v", got)
	}
	if _, statErr := os.Stat(called); !os.IsNotExist(statErr) {
		t.Fatalf("multi-worker teardown called amp or tmux: %v", statErr)
	}
	workersAfter, err := os.ReadFile(filepath.Join(dir, config.WorkersFile))
	if err != nil || !bytes.Equal(workersBefore, workersAfter) {
		t.Fatalf("multi-worker teardown changed workers: err=%v\nbefore=%s\nafter=%s", err, workersBefore, workersAfter)
	}
	shelvesAfter, err := os.ReadFile(shelvesPath)
	if err != nil || !bytes.Equal(shelvesBefore, shelvesAfter) {
		t.Fatalf("multi-worker teardown changed shelves: err=%v\nbefore=%s\nafter=%s", err, shelvesBefore, shelvesAfter)
	}
}

func TestWorkerTeardownCompletesWhenLocalWorkerIsAlreadyStopped(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> '"+logPath+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
	if len(got.Successful) != 0 || len(got.Skipped) != 1 || got.Skipped[0].Message != "already_stopped" {
		t.Fatalf("missing-window teardown result = %+v", got)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 0 {
		t.Fatalf("missing-window teardown workers = %+v err=%v", rows, err)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("missing-window teardown shelves = %+v err=%v", shelves, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "amp threads archive T-a") || strings.Contains(string(log), "tmux kill-window") {
		t.Fatalf("missing-window teardown calls:\n%s", log)
	}
}

func TestWorkerTeardownFailsClosedForUnverifiedLiveWorker(t *testing.T) {
	row := config.Row{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"}
	exact := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "a", Thread: "T-a"}, row)
	for _, tt := range []struct {
		name  string
		panes string
		want  string
	}{
		{name: "mismatched", panes: "a\t@1\tamp threads continue T-other\n", want: "conflict tmux identity"},
		{name: "ambiguous", panes: "a\t@1\t" + exact + "\na\t@1\t" + exact + "\n", want: "ambiguous tmux identity"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWorkerRegistry(t, dir, row.String()+"\n")
			if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			logPath := filepath.Join(bin, "calls.log")
			writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> '"+logPath+"'\n")
			writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 0; fi\nif [ \"$1\" = list-panes ]; then printf %s "+shellSingleQuote(tt.panes)+"; exit 0; fi\nexit 2\n")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

			err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("teardown error = %v, want %q", err, tt.want)
			}
			log, readErr := os.ReadFile(logPath)
			if readErr != nil || strings.Contains(string(log), "amp threads archive") || strings.Contains(string(log), "tmux kill-window") {
				t.Fatalf("unverified teardown performed mutation: err=%v\n%s", readErr, log)
			}
			rows, loadErr := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
			if loadErr != nil || len(rows) != 1 {
				t.Fatalf("unverified teardown workers = %+v err=%v", rows, loadErr)
			}
			shelves, loadErr := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
			if loadErr != nil || len(shelves) != 1 {
				t.Fatalf("unverified teardown shelves = %+v err=%v", shelves, loadErr)
			}
		})
	}
}

func TestWorkerTeardownArchivesRemovesAndStopsLiveWorker(t *testing.T) {
	dir := t.TempDir()
	row := config.Row{Workspace: "alpha", Window: "a", Workdir: "/tmp/a", Thread: "T-a"}
	writeWorkerRegistry(t, dir, row.String()+"\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := teardownExpectedStartCommand(teardownIdentity{Workspace: "alpha", Session: "alpha", Window: "a", Thread: "T-a"}, row)
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\necho \"amp $*\" >> '"+logPath+"'\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"tmux $*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 0; fi\nif [ \"$1\" = list-panes ]; then printf '%s\\n' "+shellSingleQuote("a\t@1\t"+start)+"; exit 0; fi\nif [ \"$1\" = kill-window ]; then exit 0; fi\nexit 2\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	got := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "teardown", "--thread", "T-a")
	if len(got.Successful) != 1 || len(got.Skipped) != 0 {
		t.Fatalf("live-window teardown result = %+v", got)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 0 {
		t.Fatalf("live-window teardown workers = %+v err=%v", rows, err)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("live-window teardown shelves = %+v err=%v", shelves, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"amp threads archive T-a", "tmux kill-window -t @1"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("live-window teardown missing %q:\n%s", want, log)
		}
	}
}

func TestWorkerPinCurrentUsesCompleteInjectedIdentity(t *testing.T) {
	dir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "pin", "--current")
	if len(result.Successful) != 1 || result.Successful[0].Resource.Thread != "T-current" {
		t.Fatalf("pin current = %+v", result)
	}
	rows, err := config.LoadReadOnly(filepath.Join(dir, config.WorkersFile))
	if err != nil || len(rows) != 1 || rows[0].Workdir != workdir {
		t.Fatalf("current row = %+v err=%v", rows, err)
	}
}

func TestWorkerCurrentMatchesMigratedHomeRelativeWorkdirCanonically(t *testing.T) {
	home := t.TempDir()
	workdir := filepath.Join(home, "project")
	if err := os.Mkdir(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t~/project\tT-current\n")
	t.Setenv("HOME", home)
	t.Setenv("AMUX_WORKSPACE", "alpha")
	t.Setenv("AMUX_SESSION", "alpha")
	t.Setenv("AMUX_WINDOW", "worker")
	t.Setenv("AMUX_THREAD_ID", "T-current")
	t.Setenv("AMUX_WORKDIR", workdir)

	result := executeWorkerJSON(t, "--json", "--config-dir", dir, "worker", "list", "--current")
	if len(result.Successful) != 1 || result.Successful[0].Resource.Thread != "T-current" {
		t.Fatalf("current migrated worker = %+v", result)
	}
}

func TestWorkerDryRunKeepsKnownNoOpsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\tworker\t/tmp/project\tT-current\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-current\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	launch := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "launch", "--thread", "T-current")
	if len(launch.Skipped) != 1 || len(launch.Planned) != 0 {
		t.Fatalf("dry-run shelved launch = %+v", launch)
	}

	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unshelve := executeWorkerJSON(t, "--json", "--dry-run", "--config-dir", dir, "worker", "unshelve", "--thread", "T-current")
	if len(unshelve.Skipped) != 1 || len(unshelve.Planned) != 0 {
		t.Fatalf("dry-run unshelved worker = %+v", unshelve)
	}
}

func TestWorkerRestartSkipsAbsentAndShelvedWorkers(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, state := range []string{"absent", "shelved"} {
			t.Run(state+map[bool]string{true: "-dry-run", false: ""}[dryRun], func(t *testing.T) {
				dir := t.TempDir()
				workdir := t.TempDir()
				writeWorkerRegistry(t, dir, "alpha\tworker\t"+workdir+"\tT-a\n")
				if state == "shelved" {
					if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				bin := t.TempDir()
				logPath := filepath.Join(bin, "tmux.log")
				writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\necho \"$*\" >> '"+logPath+"'\nif [ \"$1\" = has-session ]; then exit 1; fi\nexit 2\n")
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				args := []string{"--json", "--config-dir", dir, "worker", "restart", "--thread", "T-a"}
				if dryRun {
					args = append([]string{"--dry-run"}, args...)
				}

				result := executeWorkerJSON(t, args...)
				if len(result.Skipped) != 1 || len(result.Planned) != 0 || len(result.Successful) != 0 {
					t.Fatalf("restart result = %+v", result)
				}
				log, err := os.ReadFile(logPath)
				if state == "shelved" {
					if !os.IsNotExist(err) {
						t.Fatalf("shelved restart touched tmux: %q err=%v", log, err)
					}
				} else if err != nil || strings.Contains(string(log), "new-session") || strings.Contains(string(log), "new-window") || strings.Contains(string(log), "kill-window") {
					t.Fatalf("absent restart mutated tmux: %q err=%v", log, err)
				}
			})
		}
	}
}

func TestWorkerRemovePlansAndReportsStaleShelfCleanup(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	args := []string{"--json", "--config-dir", dir, "worker", "remove", "--thread", "T-stale"}

	dry := executeWorkerJSON(t, append([]string{"--dry-run"}, args...)...)
	if len(dry.Planned) != 1 || len(dry.Skipped) != 0 {
		t.Fatalf("dry-run stale shelf removal = %+v", dry)
	}
	actual := executeWorkerJSON(t, args...)
	if len(actual.Successful) != 1 || len(actual.Skipped) != 0 {
		t.Fatalf("stale shelf removal = %+v", actual)
	}
	shelves, err := config.LoadShelvesReadOnly(filepath.Join(dir, config.ShelvesFile))
	if err != nil || len(shelves) != 0 {
		t.Fatalf("remaining shelves = %v err=%v", shelves, err)
	}
}

func TestWorkerRemoveAllCleansShelfOnlyIntentAndEmptyInventoryIsNoOp(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(map[bool]string{true: "dry-run", false: "apply"}[dryRun], func(t *testing.T) {
			dir := t.TempDir()
			shelfPath := filepath.Join(dir, config.ShelvesFile)
			if err := os.WriteFile(shelfPath, []byte("# amux-schema: shelves/v1\nT-stale-b\nT-stale-a\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--json", "--config-dir", dir, "worker", "remove", "--all"}
			if dryRun {
				args = append([]string{"--dry-run"}, args...)
			}
			result := executeWorkerJSON(t, args...)
			outcomes := result.Successful
			if dryRun {
				outcomes = result.Planned
			}
			if len(outcomes) != 2 || outcomes[0].Resource.Thread != "T-stale-a" || outcomes[1].Resource.Thread != "T-stale-b" {
				t.Fatalf("remove --all result = %+v", result)
			}
			shelves, err := config.LoadShelvesReadOnly(shelfPath)
			if err != nil || dryRun && len(shelves) != 2 || !dryRun && len(shelves) != 0 {
				t.Fatalf("remaining shelves = %v err=%v", shelves, err)
			}
		})
	}

	empty := executeWorkerJSON(t, "--json", "--config-dir", t.TempDir(), "worker", "remove", "--all")
	if len(empty.Skipped) != 1 || empty.Skipped[0].Message != "already in desired state" {
		t.Fatalf("empty remove --all = %+v", empty)
	}
}

func TestBareAmuxAndExplicitAggregateLaunchPreserveWorkerOnlyWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeWorkerRegistry(t, dir, "alpha\ta\t/tmp/a\tT-a\n")
	if err := os.WriteFile(filepath.Join(dir, config.ShelvesFile), []byte("# amux-schema: shelves/v1\nT-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AMUX_CONFIG_DIR", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	bin := t.TempDir()
	called := filepath.Join(bin, "called")
	writeExecutable(t, filepath.Join(bin, "amp"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := (app{}).execute(nil); err != nil {
		t.Fatalf("bare amux worker launch: %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("shelved bare launch invoked amp or tmux: %v", err)
	}
	if err := (app{}).execute([]string{"launch"}); err != nil {
		t.Fatalf("explicit aggregate launch: %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("shelved aggregate launch invoked amp or tmux: %v", err)
	}
}

func TestWorkerLaunchPreflightsEveryWorkdirBeforeTmuxMutation(t *testing.T) {
	dir := t.TempDir()
	valid := t.TempDir()
	missing := filepath.Join(t.TempDir(), "missing")
	writeWorkerRegistry(t, dir, "alpha\tone\t"+valid+"\tT-one\nalpha\ttwo\t"+missing+"\tT-two\n")
	bin := t.TempDir()
	called := filepath.Join(bin, "tmux-called")
	writeExecutable(t, filepath.Join(bin, "tmux"), "#!/bin/sh\ntouch '"+called+"'\nexit 99\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	err := executeWorkerJSONError(t, "--json", "--config-dir", dir, "worker", "launch", "--all")
	if err == nil || !strings.Contains(err.Error(), "missing workdir") {
		t.Fatalf("bulk launch preflight error = %v", err)
	}
	if _, err := os.Stat(called); !os.IsNotExist(err) {
		t.Fatalf("bulk launch mutated before complete workdir preflight: %v", err)
	}
}

func TestCanonicalWorkerCompletionsAreLeafSpecific(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := (app{stdout: &stdout}).execute([]string{"completion", shell}); err != nil {
				t.Fatal(err)
			}
			output := stdout.String()
			for _, want := range []string{"unpin", "spawn"} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing %q\n%s", shell, want, output)
				}
			}
			idempotencyFlag := "--idempotency-key"
			messageFileFlag := "--message-file"
			titlePrefixFlag := "--title-prefix"
			if shell == "fish" {
				idempotencyFlag = "-l 'idempotency-key'"
				messageFileFlag = "-l 'message-file'"
				titlePrefixFlag = "-r -l 'title-prefix'"
			}
			for _, want := range []string{idempotencyFlag, messageFileFlag, titlePrefixFlag} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing %q\n%s", shell, want, output)
				}
			}
			aliases := []string{"-w", "-W", "-d", "-t"}
			if shell == "fish" {
				aliases = []string{"-s 'w'", "-s 'W'", "-s 'd'", "-s 't'"}
			}
			for _, want := range aliases {
				if !strings.Contains(output, want) {
					t.Fatalf("%s completion missing selector alias %q\n%s", shell, want, output)
				}
			}
			if shell == "bash" {
				if !strings.Contains(output, `unpin) COMPREPLY=( $(compgen -W "--thread --current -t"`) {
					t.Fatalf("bash unpin completion is not leaf-specific:\n%s", output)
				}
				if !strings.Contains(output, `if [[ "$word" == --config-dir || "$word" == -c ]]; then ((i++)); continue; fi`) {
					t.Fatalf("bash completion does not skip global config value:\n%s", output)
				}
			}
			if shell == "zsh" && !strings.Contains(output, `unpin) _arguments '--thread[thread id or URL]:thread:' '--current[current worker]'`) {
				t.Fatalf("zsh unpin completion is not leaf-specific:\n%s", output)
			}
			if shell == "zsh" && strings.Count(output, `'--title-prefix[window and thread title prefix]:prefix:'`) != 2 {
				t.Fatalf("zsh worker and top-level spawn completions do not both require --title-prefix values:\n%s", output)
			}
			if shell == "zsh" && (!strings.Contains(output, `'-c[path to config directory]:directory:_directories'`) || !strings.Contains(output, `--config-dir|-c) (( i += 2 )); continue`)) {
				t.Fatalf("zsh completion does not resolve short global prefixes:\n%s", output)
			}
		})
	}
}

func executeWorkerJSON(t *testing.T, args ...string) result.Envelope {
	t.Helper()
	var stdout bytes.Buffer
	if err := (app{stdout: &stdout}).execute(args); err != nil {
		t.Fatalf("execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	var envelope result.Envelope
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode execute(%q): %v\nstdout: %s", args, err, stdout.String())
	}
	return envelope
}

func executeWorkerJSONResult(t *testing.T, args ...string) (result.Envelope, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(args)
	var envelope result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode execute(%q): %v\nstdout: %s", args, decodeErr, stdout.String())
	}
	return envelope, err
}

func executeWorkerJSONError(t *testing.T, args ...string) error {
	t.Helper()
	var stdout bytes.Buffer
	err := (app{stdout: &stdout}).execute(args)
	if err == nil {
		return nil
	}
	var envelope result.Envelope
	if decodeErr := json.NewDecoder(&stdout).Decode(&envelope); decodeErr != nil {
		t.Fatalf("decode failed execute(%q): %v\nstdout: %s", args, decodeErr, stdout.String())
	}
	return err
}

func writeWorkerRegistry(t *testing.T, dir, rows string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "workers.tsv"), []byte("# amux-schema: workers/v1\n"+rows), 0o600); err != nil {
		t.Fatal(err)
	}
}
