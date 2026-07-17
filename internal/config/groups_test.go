package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupsRegistrySupportsManyToManyRolesAndDeterministicPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), GroupsFile)
	memberships := []GroupMembership{
		{Group: "second-group", Thread: "T-shared", Role: GroupMember},
		{Group: "first-group", Thread: "T-worker", Role: GroupMember},
		{Group: "first-group", Thread: "T-coordinator", Role: GroupCoordinator},
		{Group: "first-group", Thread: "T-shared", Role: GroupMember},
	}
	if err := WriteGroups(path, memberships); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "# amux-schema: groups/v1\n# group-id\tthread-id\trole\n" +
		"first-group\tT-coordinator\tcoordinator\n" +
		"first-group\tT-shared\tmember\n" +
		"first-group\tT-worker\tmember\n" +
		"second-group\tT-shared\tmember\n"
	if string(got) != want {
		t.Fatalf("groups registry =\n%s\nwant:\n%s", got, want)
	}
	loaded, err := LoadGroupsReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 4 || loaded[0].Role != GroupCoordinator || loaded[3].Group != "second-group" {
		t.Fatalf("loaded memberships = %+v", loaded)
	}
}

func TestGroupsRegistryMissingIsEmptyAndDoesNotCreateState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", GroupsFile)
	memberships, err := LoadGroupsReadOnly(path)
	if err != nil || len(memberships) != 0 {
		t.Fatalf("missing registry = %+v, %v", memberships, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("read-only load created config directory: %v", err)
	}
}

func TestGroupsRegistryFailsClosedOnMalformedDuplicateAndUnsupportedState(t *testing.T) {
	tests := map[string]string{
		"missing schema":       "group\tT-one\tmember\n",
		"unsupported schema":   "# amux-schema: groups/v2\n",
		"duplicate schema":     "# amux-schema: groups/v1\n# amux-schema: groups/v1\n",
		"malformed fields":     "# amux-schema: groups/v1\ngroup\tT-one\n",
		"invalid group":        "# amux-schema: groups/v1\nBad_Group\tT-one\tmember\n",
		"invalid thread":       "# amux-schema: groups/v1\ngroup\tnot-a-thread\tmember\n",
		"noncanonical thread":  "# amux-schema: groups/v1\ngroup\thttps://ampcode.com/threads/T-one\tmember\n",
		"invalid role":         "# amux-schema: groups/v1\ngroup\tT-one\towner\n",
		"duplicate membership": "# amux-schema: groups/v1\ngroup\tT-one\tmember\ngroup\tT-one\tmember\n",
		"two coordinators":     "# amux-schema: groups/v1\ngroup\tT-one\tcoordinator\ngroup\tT-two\tcoordinator\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseGroups(strings.NewReader(contents)); err == nil {
				t.Fatalf("ParseGroups accepted malformed registry:\n%s", contents)
			}
		})
	}
}

func TestGroupsWriteValidatesBeforeAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), GroupsFile)
	original := []GroupMembership{{Group: "stable", Thread: "T-one", Role: GroupCoordinator}}
	if err := WriteGroups(path, original); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := append(original, GroupMembership{Group: "stable", Thread: "T-two", Role: GroupCoordinator})
	if err := WriteGroups(path, invalid); err == nil {
		t.Fatal("WriteGroups accepted two coordinators")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("failed write changed registry:\nbefore=%s\nafter=%s", before, after)
	}
	matches, err := filepath.Glob(path + ".tmp.*")
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files after atomic write = %v, %v", matches, err)
	}
}

func TestValidateGroupIDIsBytePreservingAndRejectsNormalizationCandidates(t *testing.T) {
	for _, valid := range []string{"a", "007", "amux-agent-first", "a1-b2", strings.Repeat("a", 32)} {
		if err := ValidateGroupID(valid); err != nil {
			t.Errorf("ValidateGroupID(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "Group", " group", "group ", "group_name", "-group", "group-", "group--name", strings.Repeat("a", 33)} {
		if err := ValidateGroupID(invalid); err == nil {
			t.Errorf("ValidateGroupID(%q) succeeded", invalid)
		}
	}
}
