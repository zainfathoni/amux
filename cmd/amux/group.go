package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
)

const (
	minimumGroupAmpVersion     = "0.0.1784084982-g029ec3"
	groupLabelUsageLine        = "Usage: amp threads label [options] <threadIDOrURL> <labels...>"
	groupLabelCurrentUsageLine = "Usage: amp threads label [options] <thread> <labels...>"
	groupLabelAdditiveLine     = "Add one or more labels to an existing thread without removing the labels it already has."
)

var (
	groupLookPath = exec.LookPath
	groupExec     = func(path string, args ...string) ([]byte, error) {
		return exec.Command(path, args...).CombinedOutput()
	}
	groupVersionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)-g[0-9a-fA-F]+$`)
	ansiCSI             = regexp.MustCompile("\\x1b\\[[0-?]*[ -/]*[@-~]")
	ansiOSC             = regexp.MustCompile("\\x1b\\][^\\x07]*(?:\\x07|\\x1b\\\\)")
)

func (a app) executeGroup(in invocation, dir config.Directory) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if err := validateGroupInvocation(in); err != nil {
		return &env, result.Request(err)
	}
	memberships, err := config.LoadGroupsReadOnly(dir.GroupsPath())
	if err != nil {
		return &env, result.Preflight(err)
	}

	switch in.Command.Name {
	case "list", "show":
		selected := selectGroupMemberships(memberships, in.Selectors)
		if in.Command.Name == "show" && len(selected) == 0 {
			return &env, result.Preflight(fmt.Errorf("group %s is not declared", in.Selectors.Group))
		}
		for _, membership := range selected {
			out := groupOutcome(membership, in.Command.Name)
			env.Successful = append(env.Successful, out)
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", membership.Group, membership.Thread, membership.Role)
			}
		}
		if in.Command.Name == "show" {
			pending, err := config.LoadPendingReports(dir.ReportsPath())
			if err != nil {
				return &env, result.Preflight(err)
			}
			for _, report := range pending {
				if report.GroupID != in.Selectors.Group {
					continue
				}
				out := reportOutcome(report, "pending")
				out.Report.Pending = true
				env.Successful = append(env.Successful, out)
				if !in.Options.JSON {
					fmt.Fprintf(a.stdout, "REPORT\t%s\t%s\t%s\t%s\n", report.ReportID, report.MemberThread, report.Status, report.Summary)
				}
			}
		}
		return &env, nil
	case "remove":
		return a.removeGroupMembership(in, dir, memberships, &env)
	}

	ampPath, err := preflightGroupAmp()
	if err != nil {
		return &env, result.Preflight(err)
	}
	if in.Command.Name == "reconcile" {
		return a.reconcileGroupLabels(in, memberships, ampPath, &env)
	}
	return a.addOrCoordinateGroup(in, dir, memberships, ampPath, &env)
}

func validateGroupInvocation(in invocation) error {
	s := in.Selectors
	switch in.Command.Name {
	case "declare", "add", "remove", "coordinator":
		if s.Group == "" || s.Thread == "" || s.All {
			return fmt.Errorf("group %s requires exactly --group <id> and --thread <id>", in.Command.Name)
		}
	case "show":
		if s.Group == "" || s.Thread != "" || s.All {
			return errors.New("group show requires exactly --group <id>")
		}
	case "list":
		// An unscoped list is intentionally the local, machine-wide view.
	case "reconcile":
		scopes := 0
		if s.Group != "" {
			scopes++
		}
		if s.Thread != "" {
			scopes++
		}
		if s.All {
			scopes++
		}
		if scopes != 1 {
			return errors.New("group reconcile requires exactly one of --group <id>, --thread <id>, or --all")
		}
	default:
		return fmt.Errorf("unsupported group command %s", in.Command.Name)
	}
	return nil
}

func selectGroupMemberships(memberships []config.GroupMembership, selectors selectors) []config.GroupMembership {
	selected := make([]config.GroupMembership, 0, len(memberships))
	for _, membership := range memberships {
		if selectors.Group != "" && membership.Group != selectors.Group {
			continue
		}
		if selectors.Thread != "" && membership.Thread != selectors.Thread {
			continue
		}
		selected = append(selected, membership)
	}
	return selected
}

func (a app) removeGroupMembership(in invocation, dir config.Directory, memberships []config.GroupMembership, env *result.Envelope) (*result.Envelope, error) {
	index := membershipIndex(memberships, in.Selectors.Group, in.Selectors.Thread)
	wanted := config.GroupMembership{Group: in.Selectors.Group, Thread: in.Selectors.Thread}
	if index < 0 {
		out := groupOutcome(wanted, "remove")
		out.Group.ExternalSync = "unsupported"
		out.Group.Drift = "may_remain_indefinitely"
		out.Message = "local membership already absent; the external Amp label may remain indefinitely"
		env.Skipped = append(env.Skipped, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stderr, "Warning: local membership %s/%s is already absent, but external Amp label removal is unsupported and the label may remain indefinitely.\n", wanted.Group, wanted.Thread)
		}
		return env, nil
	}
	wanted = memberships[index]
	out := groupOutcome(wanted, "remove")
	out.Group.ExternalSync = "unsupported"
	out.Group.Drift = "may_remain_indefinitely"
	out.Message = "local membership removed; the external Amp label may remain indefinitely"
	if in.Options.DryRun {
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stderr, "Warning: would remove local membership for %s/%s; external Amp label removal is unsupported and may remain indefinitely.\n", wanted.Group, wanted.Thread)
		}
		return env, nil
	}
	updated := append([]config.GroupMembership(nil), memberships[:index]...)
	updated = append(updated, memberships[index+1:]...)
	if err := config.WriteGroups(dir.GroupsPath(), updated); err != nil {
		return env, result.Runtime(err)
	}
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintf(a.stdout, "Removed local membership %s\t%s\n", wanted.Group, wanted.Thread)
		fmt.Fprintf(a.stderr, "Warning: external Amp label removal is unsupported; label %q may remain on %s indefinitely.\n", wanted.Group, wanted.Thread)
	}
	return env, nil
}

func (a app) addOrCoordinateGroup(in invocation, dir config.Directory, memberships []config.GroupMembership, ampPath string, env *result.Envelope) (*result.Envelope, error) {
	group, thread := in.Selectors.Group, in.Selectors.Thread
	index := membershipIndex(memberships, group, thread)
	role := config.GroupMember
	if in.Command.Name == "declare" || in.Command.Name == "coordinator" {
		role = config.GroupCoordinator
	}
	if in.Command.Name == "declare" {
		for _, membership := range memberships {
			if membership.Group == group && (membership.Thread != thread || membership.Role != config.GroupCoordinator) {
				return env, result.Preflight(fmt.Errorf("group %s is already declared; use group add or group coordinator", group))
			}
		}
	}
	updated := append([]config.GroupMembership(nil), memberships...)
	changed := false
	if role == config.GroupCoordinator {
		for i := range updated {
			if updated[i].Group == group && updated[i].Role == config.GroupCoordinator && updated[i].Thread != thread {
				updated[i].Role = config.GroupMember
				changed = true
			}
		}
	}
	if index < 0 {
		updated = append(updated, config.GroupMembership{Group: group, Thread: thread, Role: role})
		changed = true
	} else if role == config.GroupCoordinator && updated[index].Role != role {
		updated[index].Role = role
		changed = true
	} else {
		role = updated[index].Role
	}
	membership := config.GroupMembership{Group: group, Thread: thread, Role: role}
	out := groupOutcome(membership, in.Command.Name)
	out.Group.ExternalSync = "additive_ensure_planned"
	if !changed {
		out.Message = "local intent already in desired state; additive label ensure remains planned"
	}
	if in.Options.DryRun {
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintf(a.stdout, "Would persist %s membership %s\t%s and add-only ensure its Amp label.\n", role, group, thread)
		}
		return env, nil
	}
	if changed {
		if err := config.WriteGroups(dir.GroupsPath(), updated); err != nil {
			return env, result.Runtime(err)
		}
	}
	return a.ensureGroupLabel(env, out, ampPath, membership, in.Options.JSON)
}

func (a app) reconcileGroupLabels(in invocation, memberships []config.GroupMembership, ampPath string, env *result.Envelope) (*result.Envelope, error) {
	selected := selectGroupMemberships(memberships, in.Selectors)
	if len(selected) == 0 {
		return env, result.Preflight(errors.New("no local group membership matches the selector"))
	}
	if in.Options.DryRun {
		for _, membership := range selected {
			out := groupOutcome(membership, "ensure-label")
			out.Group.ExternalSync = "additive_ensure_planned"
			env.Planned = append(env.Planned, out)
			if !in.Options.JSON {
				fmt.Fprintf(a.stdout, "Would add-only ensure label %q on %s.\n", membership.Group, membership.Thread)
			}
		}
		return env, nil
	}
	failed := false
	for _, membership := range selected {
		out := groupOutcome(membership, "ensure-label")
		if _, err := a.ensureGroupLabel(env, out, ampPath, membership, in.Options.JSON); err != nil {
			failed = true
		}
	}
	if failed {
		return env, result.Runtime(errors.New("one or more additive Amp label commands failed; local intent was retained as drift"))
	}
	return env, nil
}

func (a app) ensureGroupLabel(env *result.Envelope, out result.Outcome, ampPath string, membership config.GroupMembership, jsonOutput bool) (*result.Envelope, error) {
	output, err := groupExec(ampPath, "threads", "label", membership.Thread, membership.Group)
	if err != nil {
		message := strings.TrimSpace(normalizeAmpOutput(output))
		if message == "" {
			message = err.Error()
		} else {
			message = err.Error() + ": " + message
		}
		out.Group.ExternalSync = "failed"
		out.Group.Drift = "label_may_be_missing"
		out.Message = "local intent retained; additive Amp label ensure failed"
		out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: message}
		env.Failed = append(env.Failed, out)
		fmt.Fprintf(a.stderr, "Warning: retained local membership %s/%s, but additive Amp label ensure failed; drift remains: %s\n", membership.Group, membership.Thread, message)
		return env, result.Runtime(errors.New(message))
	}
	out.Group.ExternalSync = "additive_command_completed"
	out.Group.Drift = "not_exactly_inspectable"
	out.Message = "local intent persisted; additive Amp label command completed"
	env.Successful = append(env.Successful, out)
	if !jsonOutput {
		fmt.Fprintf(a.stdout, "%s\t%s\t%s\tadditive label command completed\n", membership.Group, membership.Thread, membership.Role)
	}
	return env, nil
}

func groupOutcome(membership config.GroupMembership, action string) result.Outcome {
	resource, _ := result.GroupMembershipResource(membership.Group, membership.Thread)
	return result.Outcome{
		Resource: resource,
		Action:   action,
		Group:    &result.GroupDetails{Role: string(membership.Role)},
	}
}

func membershipIndex(memberships []config.GroupMembership, group, thread string) int {
	for i, membership := range memberships {
		if membership.Group == group && membership.Thread == thread {
			return i
		}
	}
	return -1
}

func preflightGroupAmp() (string, error) {
	path, err := groupLookPath("amp")
	if err != nil {
		return "", fmt.Errorf("add-only Amp label capability unavailable: resolve amp executable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("add-only Amp label capability unavailable: resolve amp executable path: %w", err)
	}
	if target, evalErr := filepath.EvalSymlinks(path); evalErr == nil {
		path = target
	}
	versionOutput, err := groupExec(path, "version")
	if err != nil {
		return "", fmt.Errorf("add-only Amp label capability unavailable: amp version failed: %w", err)
	}
	versionFields := strings.Fields(normalizeAmpOutput(versionOutput))
	if len(versionFields) == 0 {
		return "", errors.New("add-only Amp label capability unavailable: amp version returned no version token")
	}
	if err := requireMinimumGroupAmpVersion(versionFields[0]); err != nil {
		return "", err
	}
	helpOutput, err := groupExec(path, "threads", "label", "--help")
	if err != nil {
		return "", fmt.Errorf("add-only Amp label capability unavailable: amp threads label --help failed: %w", err)
	}
	lines := strings.Split(normalizeAmpOutput(helpOutput), "\n")
	hasSupportedUsage := containsExactLine(lines, groupLabelUsageLine) || containsExactLine(lines, groupLabelCurrentUsageLine)
	if !hasSupportedUsage || !containsExactLine(lines, groupLabelAdditiveLine) {
		return "", errors.New("add-only Amp label capability unavailable: amp threads label help does not contain the exact documented usage and additive-preservation lines")
	}
	return path, nil
}

func requireMinimumGroupAmpVersion(version string) error {
	got, err := numericGroupAmpVersion(version)
	if err != nil {
		return fmt.Errorf("add-only Amp label capability unavailable: %w", err)
	}
	floor, _ := numericGroupAmpVersion(minimumGroupAmpVersion)
	for i := range got {
		if got[i] > floor[i] {
			return nil
		}
		if got[i] < floor[i] {
			return fmt.Errorf("add-only Amp label capability unavailable: amp %s is older than required %s", version, minimumGroupAmpVersion)
		}
	}
	if version != minimumGroupAmpVersion {
		return fmt.Errorf("add-only Amp label capability unavailable: amp build %s is not the tested floor %s", version, minimumGroupAmpVersion)
	}
	return nil
}

func numericGroupAmpVersion(version string) ([3]uint64, error) {
	var parsed [3]uint64
	matches := groupVersionPattern.FindStringSubmatch(version)
	if matches == nil {
		return parsed, fmt.Errorf("invalid amp version token %q", version)
	}
	for i := range parsed {
		value, err := strconv.ParseUint(matches[i+1], 10, 64)
		if err != nil {
			return parsed, fmt.Errorf("invalid amp version token %q", version)
		}
		parsed[i] = value
	}
	return parsed, nil
}

func normalizeAmpOutput(output []byte) string {
	text := strings.ReplaceAll(string(output), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ansiOSC.ReplaceAllString(text, "")
	text = ansiCSI.ReplaceAllString(text, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func containsExactLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
