package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zainfathoni/amux/internal/config"
	"github.com/zainfathoni/amux/internal/result"
	"github.com/zainfathoni/amux/internal/tmux"
)

const runnerTeardownPlanVersion = 1

var runnerTeardownGit = func(dir string, args ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, runnerTeardownGitDiagnostic(output))
	}
	return output, nil
}

var runnerTeardownCurrentDir = os.Getwd

type runnerTeardownPlan struct {
	Version               int    `json:"version"`
	Workspace             string `json:"workspace"`
	Workdir               string `json:"workdir"`
	GitWorktree           string `json:"git_worktree,omitempty"`
	Window                string `json:"window"`
	RegistryDigest        string `json:"registry_digest"`
	RunnerState           string `json:"runner_state"`
	WindowID              string `json:"window_id,omitempty"`
	PaneID                string `json:"pane_id,omitempty"`
	ProcessPID            int    `json:"process_pid,omitempty"`
	ProcessParentPID      int    `json:"process_parent_pid,omitempty"`
	ProcessIdentityDigest string `json:"process_identity_digest,omitempty"`
	WorktreeState         string `json:"worktree_state"`
	Repository            string `json:"repository,omitempty"`
	Branch                string `json:"branch,omitempty"`
	Head                  string `json:"head,omitempty"`

	inspection      runnerInspection     `json:"-"`
	expectedProcess tmux.ProcessMetadata `json:"-"`
}

type runnerTeardownWorktree struct {
	State      string
	Path       string
	Repository string
	Branch     string
	Head       string
}

type runnerTeardownWorktreeRecord struct {
	Path     string
	Head     string
	Branch   string
	Locked   bool
	Prunable bool
}

func (a app) runnerTeardown(in invocation, dir config.Directory, rows []config.RunnerRow) (*result.Envelope, error) {
	env := result.NewEnvelope(strings.Join(in.Path, " "), in.Options.DryRun)
	if in.Selectors.Workdir == "" {
		return &env, result.Request(errors.New("runner teardown requires --workdir"))
	}
	if len(rows) == 0 {
		if _, err := os.Lstat(in.Selectors.Workdir); os.IsNotExist(err) {
			resource, _ := result.RunnerResource(in.Selectors.Workdir)
			env.Skipped = append(env.Skipped, result.Outcome{Resource: resource, Action: "teardown", Message: "already in desired state"})
			return &env, nil
		}
		return &env, result.Preflight(fmt.Errorf("runner teardown requires one exact configured runner for %s", in.Selectors.Workdir))
	}
	if len(rows) != 1 {
		return &env, result.Preflight(fmt.Errorf("runner teardown requires exactly one configured runner; selector matched %d", len(rows)))
	}
	if in.Options.DryRun && in.Selectors.ConfirmPlan != "" {
		return &env, result.Request(errors.New("--confirm-plan is not valid with --dry-run"))
	}

	plan, err := buildRunnerTeardownPlan(dir, rows[0])
	if err != nil {
		return &env, result.Preflight(err)
	}
	digest, err := runnerTeardownPlanDigest(plan)
	if err != nil {
		return &env, result.Preflight(err)
	}
	out := runnerTeardownOutcome(rows[0], plan, digest)
	if in.Options.DryRun {
		out.Message = fmt.Sprintf("%s; apply with --confirm-plan %s", runnerTeardownMessage(out.Teardown), digest)
		env.Planned = append(env.Planned, out)
		if !in.Options.JSON {
			fmt.Fprintln(a.stdout, out.Message)
		}
		return &env, nil
	}
	if err := validateRunnerTeardownConfirmation(in.Selectors.ConfirmPlan); err != nil {
		return &env, result.Request(err)
	}
	if in.Selectors.ConfirmPlan != digest {
		return &env, result.Preflight(fmt.Errorf("runner teardown plan changed: confirmed %s, current %s; rerun --dry-run", in.Selectors.ConfirmPlan, digest))
	}

	if plan.RunnerState == string(runnerPaneExact) {
		if err := stopRunner(rows[0], plan.inspection, plan.expectedProcess); err != nil {
			setRunnerTeardownArtifact(out.Teardown, "local_runner", "failed", err.Error())
			return runnerTeardownRuntimeFailure(&env, out, err)
		}
	}
	after, inspectErr := inspectRunner(rows[0])
	if inspectErr != nil || after.state != runnerPaneAbsent {
		if inspectErr == nil {
			inspectErr = fmt.Errorf("local runner state is %s before worktree removal", after.state)
		}
		setRunnerTeardownArtifact(out.Teardown, "local_runner", "failed", inspectErr.Error())
		return runnerTeardownRuntimeFailure(&env, out, inspectErr)
	}
	if plan.RunnerState == string(runnerPaneExact) {
		setRunnerTeardownArtifact(out.Teardown, "local_runner", "stopped", "")
	}

	if err := requireRunnerRegistryDigest(dir.RunnersPath(), plan.RegistryDigest); err != nil {
		setRunnerTeardownArtifact(out.Teardown, "runner_binding", "failed", err.Error())
		return runnerTeardownRuntimeFailure(&env, out, err)
	}
	if plan.WorktreeState == "present" {
		current, inspectErr := inspectRunnerTeardownWorktree(plan.Workdir)
		if inspectErr != nil || current.State != plan.WorktreeState || current.Path != plan.GitWorktree || current.Repository != plan.Repository || current.Branch != plan.Branch || current.Head != plan.Head {
			if inspectErr == nil {
				inspectErr = errors.New("Git worktree identity changed after teardown planning")
			}
			setRunnerTeardownArtifact(out.Teardown, "git_worktree", "failed", inspectErr.Error())
			return runnerTeardownRuntimeFailure(&env, out, inspectErr)
		}
		if _, removeErr := runnerTeardownGit(plan.Repository, "worktree", "remove", "--", plan.GitWorktree); removeErr != nil {
			setRunnerTeardownArtifact(out.Teardown, "git_worktree", "failed", removeErr.Error())
			return runnerTeardownRuntimeFailure(&env, out, removeErr)
		}
		if verifyErr := verifyRunnerTeardownWorktreeRemoved(plan); verifyErr != nil {
			setRunnerTeardownArtifact(out.Teardown, "git_worktree", "failed", verifyErr.Error())
			return runnerTeardownRuntimeFailure(&env, out, verifyErr)
		}
		setRunnerTeardownArtifact(out.Teardown, "git_worktree", "removed", "")
	}

	if err := requireRunnerRegistryDigest(dir.RunnersPath(), plan.RegistryDigest); err != nil {
		setRunnerTeardownArtifact(out.Teardown, "runner_binding", "failed", err.Error())
		return runnerTeardownRuntimeFailure(&env, out, err)
	}
	removed, err := config.RemoveRunnerWorkdir(dir.RunnersPath(), plan.Workdir)
	if err != nil {
		setRunnerTeardownArtifact(out.Teardown, "runner_binding", "failed", err.Error())
		return runnerTeardownRuntimeFailure(&env, out, err)
	}
	if removed {
		setRunnerTeardownArtifact(out.Teardown, "runner_binding", "removed", "")
	} else {
		setRunnerTeardownArtifact(out.Teardown, "runner_binding", "already_absent", "")
	}
	out.Message = runnerTeardownMessage(out.Teardown)
	env.Successful = append(env.Successful, out)
	if !in.Options.JSON {
		fmt.Fprintln(a.stdout, out.Message)
	}
	return &env, nil
}

func buildRunnerTeardownPlan(dir config.Directory, row config.RunnerRow) (runnerTeardownPlan, error) {
	registryBytes, err := os.ReadFile(dir.RunnersPath())
	if err != nil {
		return runnerTeardownPlan{}, fmt.Errorf("read runner registry for teardown: %w", err)
	}
	inspection, err := inspectRunner(row)
	if err != nil {
		return runnerTeardownPlan{}, fmt.Errorf("runner teardown blocked for %s: local runner state is unreadable: %w", row.Workdir, err)
	}
	if inspection.state != runnerPaneAbsent && inspection.state != runnerPaneExact {
		return runnerTeardownPlan{}, fmt.Errorf("runner teardown blocked for %s: local runner state is %s", row.Workdir, inspection.state)
	}
	worktree, err := inspectRunnerTeardownWorktree(row.Workdir)
	if err != nil {
		return runnerTeardownPlan{}, err
	}
	registrySum := sha256.Sum256(registryBytes)
	plan := runnerTeardownPlan{
		Version:        runnerTeardownPlanVersion,
		Workspace:      row.Workspace,
		Workdir:        row.Workdir,
		GitWorktree:    worktree.Path,
		Window:         row.Window,
		RegistryDigest: hex.EncodeToString(registrySum[:]),
		RunnerState:    string(inspection.state),
		WorktreeState:  worktree.State,
		Repository:     worktree.Repository,
		Branch:         worktree.Branch,
		Head:           worktree.Head,
		inspection:     inspection,
	}
	if inspection.state == runnerPaneExact {
		processes, evidenceErr := preflightLifecycleExecutorEvidence("runner teardown", []tmux.WindowPane{inspection.pane})
		if evidenceErr != nil {
			return runnerTeardownPlan{}, evidenceErr
		}
		process, ok := processes[lifecyclePaneProcessKey(inspection.pane)]
		if !ok {
			return runnerTeardownPlan{}, errors.New("runner teardown process evidence is unavailable")
		}
		identitySum := sha256.Sum256([]byte(process.Identity))
		plan.WindowID = inspection.pane.WindowID
		plan.PaneID = inspection.pane.PaneID
		plan.ProcessPID = process.PID
		plan.ProcessParentPID = process.ParentPID
		plan.ProcessIdentityDigest = hex.EncodeToString(identitySum[:])
		plan.expectedProcess = process
	}
	return plan, nil
}

func inspectRunnerTeardownWorktree(workdir string) (runnerTeardownWorktree, error) {
	info, err := os.Lstat(workdir)
	if os.IsNotExist(err) {
		return runnerTeardownWorktree{State: "absent"}, nil
	}
	if err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("inspect teardown workdir %s: %w", workdir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return runnerTeardownWorktree{}, fmt.Errorf("runner teardown requires a non-symlink Git worktree directory: %s", workdir)
	}
	resolvedWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("resolve runner teardown workdir identity %s: %w", workdir, err)
	}
	resolvedWorkdir = filepath.Clean(resolvedWorkdir)
	currentDir, err := runnerTeardownCurrentDir()
	if err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("inspect current directory before runner teardown: %w", err)
	}
	resolvedCurrentDir, err := filepath.EvalSymlinks(currentDir)
	if err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("resolve current directory before runner teardown: %w", err)
	}
	if withinRunnerTeardownWorktree(resolvedWorkdir, filepath.Clean(resolvedCurrentDir)) {
		return runnerTeardownWorktree{}, fmt.Errorf("runner teardown refuses the current process directory %s; run it from outside the selected worktree", currentDir)
	}
	top, err := runnerTeardownGitLine(workdir, "rev-parse", "--show-toplevel")
	resolvedTop := ""
	if err == nil {
		resolvedTop, err = filepath.EvalSymlinks(top)
	}
	if err != nil || filepath.Clean(resolvedTop) != resolvedWorkdir {
		if err == nil {
			err = fmt.Errorf("Git top-level is %s", top)
		}
		return runnerTeardownWorktree{}, fmt.Errorf("runner teardown requires the exact Git worktree root %s: %w", workdir, err)
	}
	data, err := runnerTeardownGit(workdir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return runnerTeardownWorktree{}, err
	}
	records, err := parseRunnerTeardownWorktrees(data)
	if err != nil {
		return runnerTeardownWorktree{}, err
	}
	if len(records) < 2 {
		return runnerTeardownWorktree{}, errors.New("runner teardown refuses the repository's primary worktree")
	}
	var selected *runnerTeardownWorktreeRecord
	for i := range records {
		recordInfo, resolveErr := os.Stat(records[i].Path)
		if resolveErr != nil {
			if os.IsNotExist(resolveErr) {
				continue
			}
			return runnerTeardownWorktree{}, fmt.Errorf("inspect registered Git worktree identity %s: %w", records[i].Path, resolveErr)
		}
		if os.SameFile(info, recordInfo) {
			selected = &records[i]
			break
		}
	}
	if selected == nil {
		return runnerTeardownWorktree{}, fmt.Errorf("runner teardown workdir is not registered with Git: %s", workdir)
	}
	primaryInfo, err := os.Stat(records[0].Path)
	if err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("inspect primary Git worktree identity %s: %w", records[0].Path, err)
	}
	if os.SameFile(info, primaryInfo) {
		return runnerTeardownWorktree{}, errors.New("runner teardown refuses the repository's primary worktree")
	}
	if selected.Locked || selected.Prunable {
		return runnerTeardownWorktree{}, errors.New("runner teardown refuses a locked or prunable worktree")
	}
	if selected.Branch == "" {
		return runnerTeardownWorktree{}, errors.New("runner teardown refuses a detached worktree because no branch preserves its exact HEAD")
	}
	branchHead, err := runnerTeardownGitLine(workdir, "rev-parse", "--verify", selected.Branch+"^{commit}")
	if err != nil || branchHead != selected.Head {
		if err == nil {
			err = errors.New("worktree branch does not preserve its exact HEAD")
		}
		return runnerTeardownWorktree{}, err
	}
	status, err := runnerTeardownGit(workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return runnerTeardownWorktree{}, err
	}
	if len(status) != 0 {
		return runnerTeardownWorktree{}, errors.New("runner teardown refuses a worktree with staged, unstaged, or untracked changes")
	}
	if err := inspectRunnerTeardownIndex(workdir); err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("runner teardown refuses unsafe index or worktree content: %w", err)
	}
	if err := inspectRunnerTeardownFilesystem(workdir); err != nil {
		return runnerTeardownWorktree{}, fmt.Errorf("runner teardown refuses undeclared worktree content: %w", err)
	}
	return runnerTeardownWorktree{State: "present", Path: selected.Path, Repository: records[0].Path, Branch: selected.Branch, Head: selected.Head}, nil
}

func withinRunnerTeardownWorktree(worktree, candidate string) bool {
	relative, err := filepath.Rel(worktree, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func inspectRunnerTeardownIndex(worktree string) error {
	flags, err := runnerTeardownGit(worktree, "ls-files", "--cached", "-v", "-z")
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(flags, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		if len(record) < 3 || record[1] != ' ' {
			return fmt.Errorf("malformed index flag record %q", record)
		}
		if record[0] == 'S' || record[0] == 's' {
			return fmt.Errorf("skip-worktree index entry %q", record[2:])
		}
		if record[0] >= 'a' && record[0] <= 'z' {
			return fmt.Errorf("assume-unchanged index entry %q", record[2:])
		}
	}

	stages, err := runnerTeardownGit(worktree, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(stages, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		headerEnd := bytes.IndexByte(record, '\t')
		if headerEnd < 0 {
			return fmt.Errorf("malformed index stage record %q", record)
		}
		fields := bytes.Fields(record[:headerEnd])
		if len(fields) != 3 || !bytes.Equal(fields[2], []byte("0")) {
			return fmt.Errorf("non-stage-zero or malformed index entry %q", record)
		}
		mode, objectID, gitPath := string(fields[0]), string(fields[1]), string(record[headerEnd+1:])
		path := filepath.Clean(filepath.FromSlash(gitPath))
		if gitPath == "" || filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe index path %q", gitPath)
		}
		if !validRunnerTeardownObjectID(objectID) {
			return fmt.Errorf("malformed index object ID %q for %q", objectID, gitPath)
		}

		info, statErr := os.Lstat(filepath.Join(worktree, path))
		if statErr != nil {
			return fmt.Errorf("inspect index path %q: %w", gitPath, statErr)
		}
		var actual []byte
		switch mode {
		case "100644", "100755":
			if !info.Mode().IsRegular() {
				return fmt.Errorf("index path %q has mode %s but worktree type %s", gitPath, mode, info.Mode().Type())
			}
			if (mode == "100755") != (info.Mode().Perm()&0o100 != 0) {
				return fmt.Errorf("index path %q executable mode differs from index mode %s", gitPath, mode)
			}
			actual, err = runnerTeardownGit(worktree, "hash-object", "--no-filters", "--", gitPath)
		case "120000":
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("index path %q has symlink mode but worktree type %s", gitPath, info.Mode().Type())
			}
			target, readErr := os.Readlink(filepath.Join(worktree, path))
			if readErr != nil {
				return fmt.Errorf("read index symlink %q: %w", gitPath, readErr)
			}
			actual, err = runnerTeardownGitInput(worktree, []byte(target), "hash-object", "--stdin")
		default:
			return fmt.Errorf("unsupported index mode %q for %q", mode, gitPath)
		}
		if err != nil {
			return err
		}
		actualID := strings.TrimSpace(string(actual))
		if !validRunnerTeardownObjectID(actualID) || actualID != objectID {
			return fmt.Errorf("worktree content differs from index for %q", gitPath)
		}
	}
	return nil
}

func validRunnerTeardownObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if !((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f')) {
			return false
		}
	}
	return true
}

func inspectRunnerTeardownFilesystem(worktree string) error {
	output, err := runnerTeardownGit(worktree, "ls-files", "--cached", "-z")
	if err != nil {
		return err
	}
	tracked := make(map[string]struct{})
	ancestors := make(map[string]struct{})
	for _, record := range bytes.Split(output, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		path := filepath.Clean(filepath.FromSlash(string(record)))
		if filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe tracked filesystem path %q", record)
		}
		tracked[path] = struct{}{}
		for ancestor := filepath.Dir(path); ancestor != "."; ancestor = filepath.Dir(ancestor) {
			ancestors[ancestor] = struct{}{}
		}
	}

	return filepath.WalkDir(worktree, func(path string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk worktree filesystem: %w", walkErr)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect worktree filesystem path %q: %w", path, err)
		}
		relative, err := filepath.Rel(worktree, path)
		if err != nil {
			return fmt.Errorf("relativize worktree filesystem path %q: %w", path, err)
		}
		relative = filepath.Clean(relative)
		switch {
		case relative == ".":
			if !info.IsDir() {
				return errors.New("worktree root is not a directory")
			}
			return nil
		case relative == ".git":
			if !info.Mode().IsRegular() {
				return errors.New("linked-worktree .git is not a regular administrative file")
			}
			return nil
		}
		if _, ok := tracked[relative]; ok {
			return nil
		}
		if _, ok := ancestors[relative]; ok {
			if !info.IsDir() {
				return fmt.Errorf("tracked ancestor %q is not a directory", relative)
			}
			return nil
		}
		return fmt.Errorf("non-index filesystem object %q", relative)
	})
}

func runnerTeardownGitInput(dir string, input []byte, args ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, runnerTeardownGitDiagnostic(output))
	}
	return output, nil
}

func runnerTeardownGitDiagnostic(output []byte) string {
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "no diagnostic output"
	}
	if len(value) > 1024 {
		return value[:1024] + "…"
	}
	return value
}

func parseRunnerTeardownWorktrees(data []byte) ([]runnerTeardownWorktreeRecord, error) {
	blocks := strings.Split(strings.TrimSuffix(string(data), "\x00\x00"), "\x00\x00")
	records := make([]runnerTeardownWorktreeRecord, 0, len(blocks))
	for _, block := range blocks {
		if block == "" {
			continue
		}
		var record runnerTeardownWorktreeRecord
		for _, line := range strings.Split(block, "\x00") {
			key, value, found := strings.Cut(line, " ")
			switch {
			case found && key == "worktree":
				record.Path = filepath.Clean(value)
			case found && key == "HEAD":
				record.Head = value
			case found && key == "branch":
				record.Branch = value
			case key == "detached" && !found:
			case key == "bare" && !found:
			case key == "locked":
				record.Locked = true
			case key == "prunable":
				record.Prunable = true
			default:
				return nil, fmt.Errorf("unsupported git worktree metadata %q", line)
			}
		}
		if !filepath.IsAbs(record.Path) || record.Head == "" {
			return nil, errors.New("incomplete Git worktree metadata")
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, errors.New("Git returned no worktree metadata")
	}
	return records, nil
}

func runnerTeardownGitLine(dir string, args ...string) (string, error) {
	output, err := runnerTeardownGit(dir, args...)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(output))
	if line == "" || strings.ContainsAny(line, "\x00\r\n") {
		return "", fmt.Errorf("git %s returned invalid single-line output", strings.Join(args, " "))
	}
	return line, nil
}

func verifyRunnerTeardownWorktreeRemoved(plan runnerTeardownPlan) error {
	if _, err := os.Lstat(plan.Workdir); !os.IsNotExist(err) {
		if err == nil {
			err = errors.New("directory still exists")
		}
		return fmt.Errorf("Git worktree removal did not remove %s: %w", plan.Workdir, err)
	}
	data, err := runnerTeardownGit(plan.Repository, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return err
	}
	records, err := parseRunnerTeardownWorktrees(data)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Path == plan.GitWorktree {
			return errors.New("Git worktree registration remains after removal")
		}
	}
	return nil
}

func runnerTeardownPlanDigest(plan runnerTeardownPlan) (string, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode runner teardown plan: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateRunnerTeardownConfirmation(value string) error {
	if value == "" {
		return errors.New("runner teardown requires --confirm-plan from a fresh --dry-run")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
		return errors.New("--confirm-plan must be a lowercase SHA-256 digest from a fresh runner teardown --dry-run")
	}
	return nil
}

func requireRunnerRegistryDigest(path, expected string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read runner registry: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != expected {
		return errors.New("runner registry changed after teardown planning; retained the selected binding")
	}
	return nil
}

func runnerTeardownOutcome(row config.RunnerRow, plan runnerTeardownPlan, digest string) result.Outcome {
	out := runnerOutcome(row, "teardown", "")
	out.Teardown = &result.TeardownDetails{
		PlanDigest: digest,
		Repository: plan.Repository,
		Branch:     plan.Branch,
		Head:       plan.Head,
		Artifacts: []result.TeardownArtifactDetails{
			{Artifact: "local_runner", Outcome: map[bool]string{true: "planned_stop", false: "already_absent"}[plan.RunnerState == string(runnerPaneExact)]},
			{Artifact: "git_worktree", Outcome: map[bool]string{true: "planned_remove", false: "already_absent"}[plan.WorktreeState == "present"]},
			{Artifact: "runner_binding", Outcome: "planned_remove"},
			{Artifact: "branch_ref", Outcome: map[bool]string{true: "preserved", false: "not_applicable"}[plan.Branch != ""], Reason: "runner teardown never deletes branch refs"},
			{Artifact: "remote_threads", Outcome: "not_owned", Reason: "archive native Amp threads separately after local teardown succeeds"},
		},
	}
	return out
}

func setRunnerTeardownArtifact(details *result.TeardownDetails, name, outcome, reason string) {
	for index := range details.Artifacts {
		if details.Artifacts[index].Artifact == name {
			details.Artifacts[index].Outcome = outcome
			details.Artifacts[index].Reason = reason
			return
		}
	}
}

func runnerTeardownMessage(details *result.TeardownDetails) string {
	parts := make([]string, 0, len(details.Artifacts)+1)
	parts = append(parts, "plan="+details.PlanDigest)
	for _, artifact := range details.Artifacts {
		parts = append(parts, artifact.Artifact+"="+artifact.Outcome)
	}
	return strings.Join(parts, "; ")
}

func runnerTeardownRuntimeFailure(env *result.Envelope, out result.Outcome, err error) (*result.Envelope, error) {
	out.Message = runnerTeardownMessage(out.Teardown)
	out.Error = &result.Failure{Kind: result.ErrorRuntime, Message: err.Error()}
	env.Failed = append(env.Failed, out)
	return env, result.Runtime(err)
}
