package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRows(t *testing.T) {
	rows, err := Parse(strings.NewReader("# comment\n\nmac\ttycho\t~/Code/tycho\tT-1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Workspace != "mac" || rows[0].Window != "tycho" || rows[0].Workdir != "~/Code/tycho" || rows[0].Thread != "T-1" {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
}

func TestEnsureWritesVersionedWorkersRegistryWithoutRemovedAliases(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/" + WorkersFile

	if err := Ensure(path); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# amux-schema: workers/v1"; !strings.Contains(string(got), want) {
		t.Fatalf("default workers registry is not versioned\ngot: %q\nwant substring: %q", got, want)
	}
	for _, removed := range []string{"store", "store-current", "remove-current"} {
		if strings.Contains(string(got), removed) {
			t.Fatalf("default workers registry mentions removed command %q: %q", removed, got)
		}
	}
}

func TestResolveDirectoryUsesExplicitFlagBeforeEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ConfigDirEnv, filepath.Join(home, "from-env"))

	dir, err := ResolveDirectory(filepath.Join(home, "from-flag"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := dir.Path, filepath.Join(home, "from-flag"); got != want {
		t.Fatalf("ResolveDirectory() path = %q, want %q", got, want)
	}
	if got, want := dir.WorkersPath(), filepath.Join(home, "from-flag", WorkersFile); got != want {
		t.Fatalf("WorkersPath() = %q, want %q", got, want)
	}
}

func TestDefaultPathDoesNotMigrateLegacyConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(ConfigDirEnv, "")
	legacyDir := filepath.Join(home, ".config", "amp-tmux")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyWorkspace := filepath.Join(legacyDir, "workspaces.tsv")
	legacyRunner := filepath.Join(legacyDir, "runners.tsv")
	if err := os.WriteFile(legacyWorkspace, []byte("mac\told\t/tmp\tT-old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyRunner, []byte("mac\trunner\t/tmp\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := DefaultPath()
	want := filepath.Join(home, ".config", "amux", WorkersFile)
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Dir(want)); !os.IsNotExist(err) {
		t.Fatalf("DefaultPath created or migrated config: %v", err)
	}
	gotBytes, err := os.ReadFile(legacyWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(gotBytes), "mac\told\t/tmp\tT-old\n"; got != want {
		t.Fatalf("legacy config changed: got %q, want %q", got, want)
	}
}

func TestDefaultPathUsesExistingWorkersConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(ConfigDirEnv, "")
	newDir := filepath.Join(home, ".config", "amux")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workers := filepath.Join(newDir, WorkersFile)
	if err := os.WriteFile(workers, []byte("mac\tnew\t/tmp\tT-new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := DefaultPath(); got != workers {
		t.Fatalf("DefaultPath() = %q, want %q", got, workers)
	}
	gotBytes, err := os.ReadFile(workers)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(gotBytes), "mac\tnew\t/tmp\tT-new\n"; got != want {
		t.Fatalf("new config was overwritten: got %q, want %q", got, want)
	}
}

func TestDefaultPathUsesConfigDirectoryEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(ConfigDirEnv, "/tmp/amux-config")
	if got, want := DefaultPath(), "/tmp/amux-config/"+WorkersFile; got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestParseRejectsMalformedRows(t *testing.T) {
	cases := []string{
		"mac\twin\tdir\n",
		"mac\twin\tdir\tthread\textra\n",
		"mac\t\tdir\tthread\n",
	}
	for _, input := range cases {
		if _, err := Parse(strings.NewReader(input)); err == nil {
			t.Fatalf("Parse(%q) succeeded, want error", input)
		}
	}
}

func TestStoreReplacesAndPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/workspaces.tsv"
	input := "# header\nmac\ttycho\t/old\tT-old\nother\twin\t/tmp\tT-other\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	replaced, err := Store(path, Row{Workspace: "mac", Window: "tycho", Workdir: "/new", Thread: "T-new"})
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("got replaced=false, want true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# header\nmac\ttycho\t/new\tT-new\nother\twin\t/tmp\tT-other\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRemoveKeepsOtherRows(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/workspaces.tsv"
	input := "# header\nmac\ttycho\t/old\tT-old\nother\twin\t/tmp\tT-other\n"
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Remove(path, "mac", "tycho")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("got removed=false, want true")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# header\nother\twin\t/tmp\tT-other\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMigrationIsExplicitIdempotentAndPreservesLegacyFiles(t *testing.T) {
	path := t.TempDir()
	dir := Directory{Path: path}
	legacyPath := filepath.Join(path, "workspaces.tsv")
	legacy := "mac\tworker\t/tmp/project\thttps://ampcode.com/threads/T-worker\n"
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanMigration(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.Actions), 3; got != want {
		t.Fatalf("migration actions = %d, want %d", got, want)
	}
	for _, target := range []string{dir.WorkersPath(), dir.RunnersPath(), dir.ShelvesPath()} {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("planning migration wrote %s: %v", target, err)
		}
	}

	results, err := plan.Apply()
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Status != MigrationSuccessful {
			t.Fatalf("migration result for %s = %s, want %s", result.Registry, result.Status, MigrationSuccessful)
		}
	}
	workers, err := os.ReadFile(dir.WorkersPath())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(workers), "# amux-schema: workers/v1\nmac\tworker\t/tmp/project\tT-worker\n"; got != want {
		t.Fatalf("workers migration = %q, want %q", got, want)
	}
	legacyAfter, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(legacyAfter); got != legacy {
		t.Fatalf("legacy config changed: got %q, want %q", got, legacy)
	}

	second, err := PlanMigration(dir)
	if err != nil {
		t.Fatal(err)
	}
	results, err = second.Apply()
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Status != MigrationSkipped {
			t.Fatalf("second migration result for %s = %s, want %s", result.Registry, result.Status, MigrationSkipped)
		}
	}
}

func TestWorkerAndRunnerRegistriesEnforceCanonicalMachineIdentities(t *testing.T) {
	_, err := Parse(strings.NewReader(
		"one\tfirst\t/tmp/one\tT-same\n" +
			"two\tsecond\t/tmp/two\thttps://ampcode.com/threads/T-same\n",
	))
	if err == nil || !strings.Contains(err.Error(), "worker thread T-same is already configured") {
		t.Fatalf("Parse duplicate worker identity error = %v", err)
	}

	workdir := t.TempDir()
	_, err = ParseRunners(strings.NewReader(
		"one\tfirst\t" + workdir + "\n" +
			"two\tsecond\t" + filepath.Join(workdir, ".") + "\n",
	))
	if err == nil || !strings.Contains(err.Error(), "runner workdir "+workdir+" is already configured") {
		t.Fatalf("ParseRunners duplicate runner identity error = %v", err)
	}

	path := filepath.Join(t.TempDir(), WorkersFile)
	if _, err := Store(path, Row{
		Workspace: "one",
		Window:    "worker",
		Workdir:   "/tmp/project",
		Thread:    "https://ampcode.com/threads/T-worker",
	}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "\thttps://") || !strings.Contains(string(contents), "\tT-worker\n") {
		t.Fatalf("stored worker identity was not canonicalized: %q", contents)
	}
}

func TestMigrationRejectsDuplicateCanonicalIdentityBeforeWriting(t *testing.T) {
	path := t.TempDir()
	dir := Directory{Path: path}
	legacy := "one\tfirst\t/tmp/one\tT-same\ntwo\tsecond\t/tmp/two\thttps://ampcode.com/threads/T-same\n"
	if err := os.WriteFile(filepath.Join(path, "workspaces.tsv"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := PlanMigration(dir)
	if err == nil || !strings.Contains(err.Error(), "worker thread T-same is already configured") {
		t.Fatalf("PlanMigration duplicate identity error = %v", err)
	}
	for _, target := range []string{dir.WorkersPath(), dir.RunnersPath(), dir.ShelvesPath()} {
		if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
			t.Fatalf("failed migration planning wrote %s: %v", target, statErr)
		}
	}
}

func TestShelfRegistryPersistsCanonicalWorkerIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), ShelvesFile)
	added, err := StoreShelf(path, "https://ampcode.com/threads/T-worker")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("first StoreShelf reported an existing shelf")
	}
	added, err = StoreShelf(path, "T-worker")
	if err != nil {
		t.Fatal(err)
	}
	if added {
		t.Fatal("second StoreShelf duplicated canonical worker identity")
	}
	threads, err := LoadShelvesReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(threads, ","), "T-worker"; got != want {
		t.Fatalf("shelves = %q, want %q", got, want)
	}
	removed, err := RemoveShelf(path, "T-worker")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("RemoveShelf did not remove canonical worker identity")
	}
}

func TestOperationRecordsPersistIdempotencyStateAndRejectKeyReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), OperationsFile)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	record := OperationRecord{
		Key:         "request-123",
		Kind:        "worker.spawn",
		RequestHash: "sha256:abc",
		State:       OperationStarted,
		Resource:    OperationResource{Kind: "worker", Thread: "https://ampcode.com/threads/T-worker"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	created, err := StoreOperation(path, record)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first StoreOperation reported an existing operation")
	}

	record.State = OperationSucceeded
	record.UpdatedAt = now.Add(time.Minute)
	created, err = StoreOperation(path, record)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("operation update reported a new operation")
	}
	got, found, err := LoadOperation(path, record.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.State != OperationSucceeded || got.Resource.Thread != "T-worker" {
		t.Fatalf("loaded operation = %+v, found=%v", got, found)
	}

	conflict := record
	conflict.RequestHash = "sha256:different"
	if _, err := StoreOperation(path, conflict); err == nil || !strings.Contains(err.Error(), "idempotency key") {
		t.Fatalf("StoreOperation conflicting key error = %v", err)
	}
	got, found, err = LoadOperation(path, record.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.RequestHash != record.RequestHash {
		t.Fatalf("conflicting write changed operation: %+v", got)
	}
}
