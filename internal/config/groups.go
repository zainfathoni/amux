package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const groupsSchemaLine = "# amux-schema: groups/v1"

var groupIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type GroupRole string

const (
	GroupMember      GroupRole = "member"
	GroupCoordinator GroupRole = "coordinator"
)

type GroupMembership struct {
	Group  string
	Thread string
	Role   GroupRole
}

func ValidateGroupID(value string) error {
	if !groupIDPattern.MatchString(value) {
		return fmt.Errorf("invalid group ID %q: must match ^[a-z0-9]+(?:-[a-z0-9]+)*$ exactly", value)
	}
	if len(value) > 32 {
		return fmt.Errorf("invalid group ID %q: must be at most 32 characters", value)
	}
	return nil
}

func (m GroupMembership) Validate() error {
	if err := ValidateGroupID(m.Group); err != nil {
		return err
	}
	thread, err := CanonicalThreadID(m.Thread)
	if err != nil {
		return err
	}
	if thread != m.Thread {
		return fmt.Errorf("group member thread must be canonical: %s", thread)
	}
	if m.Role != GroupMember && m.Role != GroupCoordinator {
		return fmt.Errorf("invalid group role %q: expected member or coordinator", m.Role)
	}
	return nil
}

func (m GroupMembership) String() string {
	return strings.Join([]string{m.Group, m.Thread, string(m.Role)}, "\t")
}

func LoadGroupsReadOnly(path string) ([]GroupMembership, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return ParseGroups(file)
}

func ParseGroups(r io.Reader) ([]GroupMembership, error) {
	scanner := bufio.NewScanner(r)
	var memberships []GroupMembership
	seenSchema := false
	seenMembership := make(map[string]bool)
	coordinator := make(map[string]string)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, "# amux-schema:") {
			if seenSchema {
				return nil, fmt.Errorf("duplicate groups schema declaration on line %d", lineNo)
			}
			if line != groupsSchemaLine {
				return nil, fmt.Errorf("unsupported groups schema on line %d: %s", lineNo, line)
			}
			seenSchema = true
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !seenSchema {
			return nil, fmt.Errorf("groups schema must be declared before membership rows (line %d)", lineNo)
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid group membership on line %d: expected 3 tab-separated fields", lineNo)
		}
		thread, err := CanonicalThreadID(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid group membership on line %d: %w", lineNo, err)
		}
		if thread != fields[1] {
			return nil, fmt.Errorf("invalid group membership on line %d: thread must be the canonical Amp thread ID %s", lineNo, thread)
		}
		membership := GroupMembership{Group: fields[0], Thread: thread, Role: GroupRole(fields[2])}
		if err := membership.Validate(); err != nil {
			return nil, fmt.Errorf("invalid group membership on line %d: %w", lineNo, err)
		}
		key := membership.Group + "\x00" + membership.Thread
		if seenMembership[key] {
			return nil, fmt.Errorf("duplicate membership for group %s and thread %s", membership.Group, membership.Thread)
		}
		if membership.Role == GroupCoordinator {
			if existing := coordinator[membership.Group]; existing != "" {
				return nil, fmt.Errorf("group %s has multiple coordinators: %s and %s", membership.Group, existing, membership.Thread)
			}
			coordinator[membership.Group] = membership.Thread
		}
		seenMembership[key] = true
		memberships = append(memberships, membership)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !seenSchema {
		return nil, errors.New("groups registry is missing schema declaration")
	}
	sortGroupMemberships(memberships)
	return memberships, nil
}

func WriteGroups(path string, memberships []GroupMembership) error {
	copyOfMemberships := append([]GroupMembership(nil), memberships...)
	sortGroupMemberships(copyOfMemberships)
	lines := []string{
		groupsSchemaLine,
		"# group-id\tthread-id\trole",
	}
	for _, membership := range copyOfMemberships {
		lines = append(lines, membership.String())
	}
	if _, err := ParseGroups(strings.NewReader(strings.Join(lines, "\n") + "\n")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeLinesAtomic(path, lines)
}

func sortGroupMemberships(memberships []GroupMembership) {
	sort.Slice(memberships, func(i, j int) bool {
		if memberships[i].Group != memberships[j].Group {
			return memberships[i].Group < memberships[j].Group
		}
		if memberships[i].Role != memberships[j].Role {
			return memberships[i].Role == GroupCoordinator
		}
		return memberships[i].Thread < memberships[j].Thread
	})
}
