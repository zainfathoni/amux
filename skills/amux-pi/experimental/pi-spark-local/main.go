package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	packageName    = "@earendil-works/pi-coding-agent"
	packageVersion = "0.80.10"
	model          = "openai-codex/gpt-5.3-codex-spark"
	maxInputBytes  = 64 << 10
	maxTaskBytes   = 16 << 10
	maxPromptBytes = 512 << 10
	maxGitBytes    = 1 << 20
	gitPath        = "/usr/bin/git"
)

var errAppliedStateIndeterminate = errors.New("applied state indeterminate")

var fixedArgs = []string{
	"--model", model,
	"--no-session", "--no-tools", "--no-extensions", "--no-skills",
	"--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve",
	"-p",
}

var (
	extraProcessEnvironment func() []string
	postApplyCheck          = requireExactDiff
)

type options struct {
	pi, piSHA256, node, nodeSHA256 string
	workdir, file, task            string
	expectedReplacementSHA256      string
	timeout                        time.Duration
	stdoutLimit, stderrLimit       int
}

type packageJSON struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Bin     json.RawMessage `json:"bin"`
}

type settingsJSON struct {
	Retry struct {
		Enabled  *bool `json:"enabled"`
		Provider struct {
			MaxRetries *int `json:"maxRetries"`
		} `json:"provider"`
	} `json:"retry"`
	Compaction struct {
		Enabled *bool `json:"enabled"`
	} `json:"compaction"`
}

type replacement struct {
	Path           string `json:"path"`
	OriginalSHA256 string `json:"original_sha256"`
	Replacement    string `json:"replacement"`
}

type result struct {
	Status                    string   `json:"status"`
	Package                   string   `json:"package"`
	Version                   string   `json:"version"`
	RequestedModel            string   `json:"requested_model"`
	Executable                string   `json:"executable"`
	ExecutableSHA256          string   `json:"executable_sha256"`
	Node                      string   `json:"node"`
	NodeSHA256                string   `json:"node_sha256"`
	Argv                      []string `json:"argv"`
	ChangedPath               string   `json:"changed_path"`
	StdoutBytes               int      `json:"stdout_bytes"`
	StderrBytes               int      `json:"stderr_bytes"`
	Stderr                    string   `json:"stderr"`
	ExpectedReplacementSHA256 string   `json:"expected_replacement_sha256,omitempty"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		os.Exit(reportFailure(err, os.Stderr))
	}
}

func reportFailure(err error, output io.Writer) int {
	if errors.Is(err, errAppliedStateIndeterminate) {
		fmt.Fprintln(output, "indeterminate_applied_state:", err)
	} else {
		fmt.Fprintln(output, "blocked:", err)
	}
	return failureExitCode(err)
}

func failureExitCode(err error) int {
	if errors.Is(err, errAppliedStateIndeterminate) {
		return 3
	}
	return 2
}

func run(args []string, output io.Writer) error {
	var o options
	flags := flag.NewFlagSet("pi-spark-local", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&o.pi, "pi", "", "absolute path to Pi 0.80.10")
	flags.StringVar(&o.piSHA256, "pi-sha256", "", "expected SHA-256 of Pi's dist/cli.js")
	flags.StringVar(&o.node, "node", "", "absolute path to the selected Node executable")
	flags.StringVar(&o.nodeSHA256, "node-sha256", "", "expected SHA-256 of the Node executable")
	flags.StringVar(&o.workdir, "workdir", "", "absolute clean Git worktree")
	flags.StringVar(&o.file, "file", "", "one tracked file relative to worktree")
	flags.StringVar(&o.task, "task", "", "self-contained microtask")
	flags.StringVar(&o.expectedReplacementSHA256, "expected-replacement-sha256", "", "optional exact SHA-256 required before replacement")
	flags.DurationVar(&o.timeout, "timeout", 2*time.Minute, "wall-clock limit")
	flags.IntVar(&o.stdoutLimit, "stdout-limit", 512<<10, "final response byte limit")
	flags.IntVar(&o.stderrLimit, "stderr-limit", 16<<10, "diagnostic byte limit; 0 rejects any stderr byte")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: pi-spark-local --pi ABS --pi-sha256 HEX --node ABS --node-sha256 HEX --workdir ABS --file REL --task TEXT [--timeout 2m]")
	}
	if o.pi == "" || o.piSHA256 == "" || o.node == "" || o.nodeSHA256 == "" || o.workdir == "" || o.file == "" || strings.TrimSpace(o.task) == "" {
		return errors.New("Pi, Node, worktree, file, task, and expected hashes are required")
	}
	if o.timeout <= 0 || o.timeout > 5*time.Minute || o.stdoutLimit < 1 || o.stdoutLimit > 1<<20 || o.stderrLimit < 0 || o.stderrLimit > 64<<10 {
		return errors.New("invalid bounds (timeout: 1ns..5m, stdout: 1..1048576, stderr: 0..65536)")
	}
	if !utf8.ValidString(o.task) {
		return errors.New("task must be valid UTF-8")
	}
	if len([]byte(o.task)) > maxTaskBytes {
		return fmt.Errorf("task exceeds %d-byte limit", maxTaskBytes)
	}
	if o.expectedReplacementSHA256 != "" {
		decoded, err := hex.DecodeString(o.expectedReplacementSHA256)
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("--expected-replacement-sha256 must be exactly 64 hexadecimal characters")
		}
		o.expectedReplacementSHA256 = strings.ToLower(o.expectedReplacementSHA256)
	}
	if _, present := os.LookupEnv("OPENAI_API_KEY"); present {
		return errors.New("OPENAI_API_KEY is present; exact OAuth billing is not established")
	}
	if _, present := os.LookupEnv("CODEX_API_KEY"); present {
		return errors.New("CODEX_API_KEY is present; exact OAuth billing is not established")
	}

	pi, piDigest, err := admitPi(o.pi, o.piSHA256)
	if err != nil {
		return err
	}
	node, nodeDigest, err := admitExecutable(o.node, o.nodeSHA256, "Node")
	if err != nil {
		return err
	}
	workdir, relative, target, before, err := admitWorktree(o.workdir, o.file)
	if err != nil {
		return err
	}
	agentDir, err := admitAgentMetadata()
	if err != nil {
		return err
	}
	originalDigest := digest(before)
	prompt, err := buildPrompt(o.task, relative, originalDigest, before)
	if err != nil {
		return err
	}

	argv := append([]string{node, pi}, fixedArgs...)
	stdout, stderr, err := execute(o, argv, prompt, agentDir)
	if err != nil {
		return err
	}
	if currentPi, currentDigest, err := admitPi(o.pi, o.piSHA256); err != nil || currentPi != pi || currentDigest != piDigest {
		return errors.New("Pi executable or package identity changed during execution")
	}
	if currentNode, currentDigest, err := admitExecutable(o.node, o.nodeSHA256, "Node"); err != nil || currentNode != node || currentDigest != nodeDigest {
		return errors.New("Node executable identity changed during execution")
	}
	if currentAgentDir, err := admitAgentMetadata(); err != nil || currentAgentDir != agentDir {
		return errors.New("Pi agent admission changed during execution")
	}
	parsed, err := parseReplacement(stdout)
	if err != nil {
		return err
	}
	if parsed.Path != relative || parsed.OriginalSHA256 != originalDigest {
		return errors.New("final response does not bind the exact allowed path and original bytes")
	}
	if len([]byte(parsed.Replacement)) > maxInputBytes {
		return fmt.Errorf("replacement exceeds %d-byte limit", maxInputBytes)
	}
	if o.expectedReplacementSHA256 != "" && digest([]byte(parsed.Replacement)) != o.expectedReplacementSHA256 {
		return errors.New("replacement does not match expected SHA-256")
	}
	replacementBytes := []byte(parsed.Replacement)
	if bytes.Equal(replacementBytes, before) {
		return errors.New("replacement is unchanged")
	}
	if err := requireUnchanged(workdir, target, before); err != nil {
		return err
	}
	if err := replaceFile(target, replacementBytes); err != nil {
		return err
	}
	if current, err := os.ReadFile(target); err != nil || !bytes.Equal(current, replacementBytes) {
		return rollbackAfterApply(workdir, target, before, errors.New("replacement read-back verification failed"))
	}
	if err := postApplyCheck(workdir, relative); err != nil {
		return rollbackAfterApply(workdir, target, before, fmt.Errorf("post-apply validation failed: %w", err))
	}

	stderrSummary := "empty"
	if len(stderr) > 0 {
		stderrSummary = "present_redacted"
	}
	status := "replacement_applied_untrusted"
	if o.expectedReplacementSHA256 != "" {
		status = "replacement_applied_expected"
	}
	receipt := result{
		Status: status, Package: packageName, Version: packageVersion,
		RequestedModel: model, Executable: pi, ExecutableSHA256: piDigest, Node: node, NodeSHA256: nodeDigest,
		Argv:        argv,
		ChangedPath: relative, StdoutBytes: len(stdout), StderrBytes: len(stderr), Stderr: stderrSummary,
		ExpectedReplacementSHA256: o.expectedReplacementSHA256,
	}
	encodedReceipt, err := json.Marshal(receipt)
	if err != nil {
		return rollbackAfterApply(workdir, target, before, errors.New("receipt serialization failed"))
	}
	encodedReceipt = append(encodedReceipt, '\n')
	if written, err := output.Write(encodedReceipt); err != nil || written != len(encodedReceipt) {
		return rollbackAfterApply(workdir, target, before, errors.New("receipt output failed"))
	}
	return nil
}

func admitPi(argument, expectedDigest string) (string, string, error) {
	pi, observedDigest, err := admitExecutable(argument, expectedDigest, "Pi")
	if err != nil {
		return "", "", err
	}
	root := filepath.Dir(filepath.Dir(pi))
	metadataPath := filepath.Join(root, "package.json")
	metadataInfo, err := os.Lstat(metadataPath)
	if err != nil || !metadataInfo.Mode().IsRegular() || metadataInfo.Size() > 1<<20 {
		return "", "", errors.New("Pi package metadata is unavailable, linked, or oversized")
	}
	metadata, err := os.ReadFile(metadataPath)
	if err != nil {
		return "", "", errors.New("Pi package metadata is unavailable or oversized")
	}
	var pkg packageJSON
	if err := json.Unmarshal(metadata, &pkg); err != nil || pkg.Name != packageName || pkg.Version != packageVersion {
		return "", "", errors.New("Pi package name or version is not exactly admitted")
	}
	var bins map[string]string
	if err := json.Unmarshal(pkg.Bin, &bins); err != nil || len(bins) != 1 || filepath.Clean(bins["pi"]) != filepath.Join("dist", "cli.js") {
		return "", "", errors.New("Pi package bin metadata is not exactly admitted")
	}
	declared, err := filepath.EvalSymlinks(filepath.Join(root, bins["pi"]))
	if err != nil || declared != pi {
		return "", "", errors.New("resolved executable is not the admitted package's exact Pi bin")
	}
	return pi, observedDigest, nil
}

func admitExecutable(argument, expectedDigest, label string) (string, string, error) {
	decodedDigest, digestErr := hex.DecodeString(expectedDigest)
	if !filepath.IsAbs(argument) || digestErr != nil || len(decodedDigest) != sha256.Size {
		return "", "", fmt.Errorf("%s path or expected SHA-256 is malformed", label)
	}
	path, err := filepath.EvalSymlinks(argument)
	if err != nil {
		return "", "", fmt.Errorf("%s executable is unavailable", label)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", "", fmt.Errorf("resolved %s object is not an executable regular file", label)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("%s executable cannot be hashed", label)
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return "", "", fmt.Errorf("%s executable cannot be hashed", label)
	}
	observed := hex.EncodeToString(hasher.Sum(nil))
	if observed != strings.ToLower(expectedDigest) {
		return "", "", fmt.Errorf("%s executable does not match its admitted SHA-256", label)
	}
	return path, observed, nil
}

func admitAgentMetadata() (string, error) {
	agentDir := os.Getenv("PI_CODING_AGENT_DIR")
	if agentDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("Pi agent directory is unavailable")
		}
		agentDir = filepath.Join(home, ".pi", "agent")
	}
	if !filepath.IsAbs(agentDir) {
		return "", errors.New("Pi agent directory must be absolute")
	}
	agentDir, err := filepath.EvalSymlinks(agentDir)
	if err != nil {
		return "", errors.New("Pi agent directory cannot be canonicalized")
	}
	auth := filepath.Join(agentDir, "auth.json")
	info, err := os.Lstat(auth)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return "", errors.New("owner-managed Pi auth metadata is absent, linked, or not mode 0600")
	}
	settingsPath := filepath.Join(agentDir, "settings.json")
	settingsInfo, err := os.Lstat(settingsPath)
	if err != nil || !settingsInfo.Mode().IsRegular() || settingsInfo.Mode().Perm() != 0o600 || settingsInfo.Size() > 1<<20 {
		return "", errors.New("owner-managed Pi settings are absent, linked, oversized, or not mode 0600")
	}
	settingsBytes, err := os.ReadFile(settingsPath)
	if err != nil {
		return "", errors.New("owner-managed Pi settings are unreadable")
	}
	var settings settingsJSON
	if err := json.Unmarshal(settingsBytes, &settings); err != nil ||
		settings.Retry.Enabled == nil || *settings.Retry.Enabled ||
		settings.Retry.Provider.MaxRetries == nil || *settings.Retry.Provider.MaxRetries != 0 ||
		settings.Compaction.Enabled == nil || *settings.Compaction.Enabled {
		return "", errors.New("Pi settings do not disable agent retry, provider retry, and compaction")
	}
	for _, overlay := range []string{"models.json", "package.json", "models-store.json", "SYSTEM.md", "APPEND_SYSTEM.md"} {
		if _, err := os.Lstat(filepath.Join(agentDir, overlay)); err == nil || !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("Pi agent %s is present or ambiguous", overlay)
		}
	}
	return agentDir, nil
}

func admitWorktree(argument, relative string) (string, string, string, []byte, error) {
	if !filepath.IsAbs(argument) {
		return "", "", "", nil, errors.New("--workdir must be absolute")
	}
	workdir, err := filepath.EvalSymlinks(argument)
	if err != nil {
		return "", "", "", nil, errors.New("worktree is unavailable")
	}
	if filepath.IsAbs(relative) || relative == "." || relative == ".." || relative != filepath.Clean(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", "", nil, errors.New("--file must be one canonical relative path")
	}
	if err := requireSafeGitConfiguration(workdir); err != nil {
		return "", "", "", nil, err
	}
	root, err := git(workdir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(string(root)) != workdir {
		return "", "", "", nil, errors.New("--workdir is not the canonical Git worktree root")
	}
	if err := requireVisibleIndex(workdir); err != nil {
		return "", "", "", nil, err
	}
	status, err := git(workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status) != 0 {
		return "", "", "", nil, errors.New("worktree must be clean before execution")
	}
	if _, err := git(workdir, "ls-files", "--error-unmatch", "--", relative); err != nil {
		return "", "", "", nil, errors.New("allowed file must already exist and be tracked")
	}
	target := filepath.Join(workdir, relative)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil || resolved != target {
		return "", "", "", nil, errors.New("allowed file must be an existing non-symlink path")
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", "", nil, errors.New("allowed file must be regular")
	}
	before, err := os.ReadFile(target)
	if err != nil || len(before) > maxInputBytes || !utf8.Valid(before) {
		return "", "", "", nil, fmt.Errorf("allowed file is unreadable, non-UTF-8, or exceeds %d bytes", maxInputBytes)
	}
	return workdir, filepath.ToSlash(relative), target, before, nil
}

func buildPrompt(task, path, originalDigest string, contents []byte) ([]byte, error) {
	packet := struct {
		Task           string `json:"task"`
		Path           string `json:"path"`
		OriginalSHA256 string `json:"original_sha256"`
		Original       string `json:"original"`
	}{task, path, originalDigest, string(contents)}
	encoded, err := json.Marshal(packet)
	if err != nil {
		return nil, errors.New("task packet cannot be encoded")
	}
	prompt := append([]byte("You are a bounded replacement generator. Do not use tools, sessions, files, network publishing, delegation, retries, or external context. Return only one JSON object with exactly these string fields: path, original_sha256, replacement. Preserve path and original_sha256 exactly; replacement is the complete new file. Task packet: "), encoded...)
	if len(prompt) > maxPromptBytes {
		return nil, fmt.Errorf("generated prompt exceeds %d-byte limit", maxPromptBytes)
	}
	return prompt, nil
}

type cappedWriter struct {
	mu             sync.Mutex
	b              bytes.Buffer
	limit          int
	overflow       bool
	overflowSignal chan struct{}
	overflowOnce   *sync.Once
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	remaining := w.limit - w.b.Len()
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		_, _ = w.b.Write(p[:remaining])
	}
	first := len(p) > remaining && !w.overflow
	if len(p) > remaining {
		w.overflow = true
	}
	w.mu.Unlock()
	if first {
		w.overflowOnce.Do(func() { close(w.overflowSignal) })
	}
	return len(p), nil
}

func execute(o options, argv []string, prompt []byte, agentDir string) ([]byte, []byte, error) {
	ctx, timeoutCancel := context.WithTimeout(context.Background(), o.timeout)
	defer timeoutCancel()
	overflowSignal := make(chan struct{})
	overflowOnce := &sync.Once{}
	stdout := &cappedWriter{limit: o.stdoutLimit, overflowSignal: overflowSignal, overflowOnce: overflowOnce}
	stderr := &cappedWriter{limit: o.stderrLimit, overflowSignal: overflowSignal, overflowOnce: overflowOnce}
	privateDir, err := os.MkdirTemp("", "amux-pi-spark-")
	if err != nil {
		return nil, nil, errors.New("private Pi working directory cannot be created")
	}
	defer os.RemoveAll(privateDir)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, errors.New("owner home is unavailable")
	}

	guardianInput, guardianHold, err := os.Pipe()
	if err != nil {
		return nil, nil, errors.New("process-group guardian pipe cannot be created")
	}
	guardian := exec.Command("/bin/cat")
	guardian.Dir = privateDir
	guardian.Stdin = guardianInput
	guardian.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := guardian.Start(); err != nil {
		guardianInput.Close()
		guardianHold.Close()
		return nil, nil, errors.New("process-group guardian cannot be started")
	}
	guardianInput.Close()
	guardianPID := guardian.Process.Pid

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = privateDir
	cmd.Stdin = bytes.NewReader(prompt)
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(argv[0]) + ":/usr/bin:/bin",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"PI_CODING_AGENT_DIR=" + agentDir,
		"PI_OFFLINE=1", "PI_SKIP_VERSION_CHECK=1",
	}
	for _, name := range []string{"TMPDIR", "SSL_CERT_FILE", "SSL_CERT_DIR", "HTTPS_PROXY", "NO_PROXY"} {
		if value, present := os.LookupEnv(name); present {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	if extraProcessEnvironment != nil {
		cmd.Env = append(cmd.Env, extraProcessEnvironment()...)
	}
	stdoutPipe, stdoutChild, err := os.Pipe()
	if err != nil {
		terminateGuardian(guardian, guardianHold)
		return nil, nil, errors.New("Pi stdout cannot be captured")
	}
	stderrPipe, stderrChild, err := os.Pipe()
	if err != nil {
		stdoutPipe.Close()
		stdoutChild.Close()
		terminateGuardian(guardian, guardianHold)
		return nil, nil, errors.New("Pi stderr cannot be captured")
	}
	cmd.Stdout = stdoutChild
	cmd.Stderr = stderrChild
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: guardianPID}
	if err := cmd.Start(); err != nil {
		stdoutPipe.Close()
		stdoutChild.Close()
		stderrPipe.Close()
		stderrChild.Close()
		terminateGuardian(guardian, guardianHold)
		return nil, nil, errors.New("Pi failed to start")
	}
	stdoutChild.Close()
	stderrChild.Close()

	streamDone := make(chan error, 2)
	go func() {
		defer stdoutPipe.Close()
		_, err := io.Copy(stdout, stdoutPipe)
		streamDone <- err
	}()
	go func() {
		defer stderrPipe.Close()
		_, err := io.Copy(stderr, stderrPipe)
		streamDone <- err
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var waitErr error
	waitCollected := false
	timedOut := false
	streamErrors := make([]error, 0, 2)
	for !waitCollected {
		select {
		case streamErr := <-streamDone:
			streamErrors = append(streamErrors, streamErr)
		case waitErr = <-waitDone:
			waitCollected = true
		case <-overflowSignal:
			goto cleanup
		case <-ctx.Done():
			timedOut = true
			goto cleanup
		}
	}

cleanup:
	// The guardian remains live and unreaped, so it owns and reserves the PGID
	// while every terminal path removes Pi and any descendants from that group.
	killErr := syscall.Kill(-guardianPID, syscall.SIGKILL)
	guardianHold.Close()
	var cleanupStreamErr error
	if len(streamErrors) < 2 {
		cleanupTimer := time.NewTimer(2 * time.Second)
		cleanupTimerC := cleanupTimer.C
		for len(streamErrors) < 2 {
			select {
			case streamErr := <-streamDone:
				streamErrors = append(streamErrors, streamErr)
			case <-cleanupTimerC:
				_ = stdoutPipe.Close()
				_ = stderrPipe.Close()
				cleanupStreamErr = errors.New("Pi process streams did not close after group termination")
				cleanupTimerC = nil
			}
		}
		if !cleanupTimer.Stop() {
			select {
			case <-cleanupTimer.C:
			default:
			}
		}
	}
	if !waitCollected {
		waitErr = <-waitDone
		waitCollected = true
	}
	guardianErr := guardian.Wait()
	if killErr != nil {
		return nil, nil, errors.New("Pi process-group termination could not be verified")
	}
	if !killedBySignal(guardianErr, syscall.SIGKILL) {
		return nil, nil, errors.New("process-group guardian termination could not be verified")
	}
	if stdout.overflow || stderr.overflow {
		return nil, nil, errors.New("Pi output exceeded its configured bound")
	}
	if timedOut {
		return nil, nil, fmt.Errorf("Pi timed out after %s", o.timeout)
	}
	if cleanupStreamErr != nil {
		return nil, nil, cleanupStreamErr
	}
	for _, streamErr := range streamErrors {
		if streamErr != nil {
			return nil, nil, errors.New("Pi process streams did not close after group termination")
		}
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return nil, nil, errors.New("Pi terminated unexpectedly")
		}
		if exitErr.ProcessState.ExitCode() >= 0 {
			return nil, nil, fmt.Errorf("Pi exited with code %d", exitErr.ExitCode())
		}
		return nil, nil, errors.New("Pi terminated unexpectedly")
	}
	return stdout.b.Bytes(), stderr.b.Bytes(), nil
}

func terminateGuardian(guardian *exec.Cmd, hold *os.File) {
	_ = syscall.Kill(-guardian.Process.Pid, syscall.SIGKILL)
	_ = hold.Close()
	_ = guardian.Wait()
}

func killedBySignal(err error, signal syscall.Signal) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == signal
}

func parseReplacement(data []byte) (replacement, error) {
	var value replacement
	if !utf8.Valid(data) {
		return value, errors.New("final response is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return value, errors.New("final response is not the exact replacement envelope")
	}
	seen := make(map[string]bool, 3)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || (key != "path" && key != "original_sha256" && key != "replacement") {
			return value, errors.New("final response has unknown, duplicate, or malformed fields")
		}
		seen[key] = true
		var field string
		if err := decoder.Decode(&field); err != nil {
			return value, errors.New("final response replacement fields must be strings")
		}
		switch key {
		case "path":
			value.Path = field
		case "original_sha256":
			value.OriginalSHA256 = field
		case "replacement":
			value.Replacement = field
		}
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return value, errors.New("final response is not the exact replacement envelope")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return value, errors.New("final response contains data after the replacement envelope")
	}
	decodedDigest, digestErr := hex.DecodeString(value.OriginalSHA256)
	if len(seen) != 3 || !seen["path"] || !seen["original_sha256"] || !seen["replacement"] || value.Path == "" || digestErr != nil || len(decodedDigest) != sha256.Size {
		return value, errors.New("final response has empty or malformed replacement fields")
	}
	if value.OriginalSHA256 != strings.ToLower(value.OriginalSHA256) {
		return value, errors.New("final response original_sha256 must use canonical lowercase hexadecimal")
	}
	return value, nil
}

func requireUnchanged(workdir, target string, before []byte) error {
	if err := requireSafeGitConfiguration(workdir); err != nil {
		return err
	}
	if err := requireVisibleIndex(workdir); err != nil {
		return err
	}
	status, err := git(workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status) != 0 {
		return errors.New("Pi changed the worktree; refusing to apply its replacement")
	}
	current, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(current, before) {
		return errors.New("allowed file changed during execution")
	}
	return nil
}

func replaceFile(target string, contents []byte) error {
	info, err := os.Stat(target)
	if err != nil {
		return errors.New("allowed file disappeared before replacement")
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".amux-pi-replacement-*")
	if err != nil {
		return errors.New("cannot create replacement beside allowed file")
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return errors.New("cannot preserve allowed file mode")
	}
	if _, err := temporary.Write(contents); err != nil || temporary.Sync() != nil || temporary.Close() != nil {
		return errors.New("cannot durably write replacement")
	}
	if err := os.Rename(name, target); err != nil {
		return errors.New("cannot atomically replace allowed file")
	}
	return nil
}

func requireExactDiff(workdir, relative string) error {
	if err := requireSafeGitConfiguration(workdir); err != nil {
		return err
	}
	if err := requireVisibleIndex(workdir); err != nil {
		return err
	}
	status, err := git(workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	expected := []byte(" M " + relative + "\x00")
	if err != nil || !bytes.Equal(status, expected) {
		return errors.New("resulting Git diff is not exactly the one allowed file")
	}
	changed, err := git(workdir, "diff", "--name-only", "-z", "--no-ext-diff", "--no-textconv")
	if err != nil || !bytes.Equal(changed, []byte(relative+"\x00")) {
		return errors.New("Git diff scope verification failed")
	}
	staged, err := git(workdir, "diff", "--cached", "--name-only", "-z", "--no-ext-diff", "--no-textconv")
	if err != nil || len(staged) != 0 {
		return errors.New("unexpected staged diff after replacement")
	}
	return nil
}

func requireVisibleIndex(workdir string) error {
	entries, err := git(workdir, "ls-files", "-v", "-z")
	if err != nil {
		return errors.New("Git index visibility could not be verified")
	}
	for _, entry := range bytes.Split(entries, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		if len(entry) < 3 || entry[1] != ' ' {
			return errors.New("Git index visibility output is malformed")
		}
		tag := entry[0]
		if tag == 'S' || (tag >= 'a' && tag <= 'z') {
			return errors.New("Git index contains skip-worktree or assume-unchanged entries")
		}
	}
	return nil
}

type rollbackDependencies struct {
	replace func(string, []byte) error
	read    func(string) ([]byte, error)
	gitSafe func(string) error
	visible func(string) error
	git     func(string, ...string) ([]byte, error)
}

func rollbackAfterApply(workdir, target string, before []byte, cause error) error {
	return rollbackAfterApplyWith(workdir, target, before, cause, rollbackDependencies{
		replace: replaceFile, read: os.ReadFile, gitSafe: requireSafeGitConfiguration,
		visible: requireVisibleIndex, git: git,
	})
}

func rollbackAfterApplyWith(workdir, target string, before []byte, cause error, deps rollbackDependencies) error {
	if err := deps.replace(target, before); err != nil {
		return fmt.Errorf("%w: replacement may remain applied; rollback failed after %v: %v", errAppliedStateIndeterminate, cause, err)
	}
	current, readErr := deps.read(target)
	if readErr != nil || !bytes.Equal(current, before) {
		return fmt.Errorf("%w: replacement may remain applied; rollback read-back failed after %v", errAppliedStateIndeterminate, cause)
	}
	if err := deps.gitSafe(workdir); err != nil {
		return fmt.Errorf("%w: original bytes restored but Git safety is unverified after %v: %v", errAppliedStateIndeterminate, cause, err)
	}
	if err := deps.visible(workdir); err != nil {
		return fmt.Errorf("%w: original bytes restored but clean state is unverified after %v: %v", errAppliedStateIndeterminate, cause, err)
	}
	status, err := deps.git(workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status) != 0 {
		return fmt.Errorf("%w: original bytes restored but worktree is not clean after %v", errAppliedStateIndeterminate, cause)
	}
	return fmt.Errorf("%w; replacement rolled back", cause)
}

func requireSafeGitConfiguration(workdir string) error {
	configNames, err := git(workdir, "config", "--includes", "--name-only", "--null", "--list")
	if err != nil {
		return errors.New("effective repository Git configuration could not be inspected safely")
	}
	for _, name := range bytes.Split(configNames, []byte{0}) {
		lower := strings.ToLower(string(name))
		if lower == "core.attributesfile" {
			return errors.New("repository-local core.attributesFile is not admitted")
		}
		if strings.HasPrefix(lower, "filter.") && (strings.HasSuffix(lower, ".clean") || strings.HasSuffix(lower, ".process")) {
			return errors.New("repository-local external Git filter configuration is not admitted")
		}
	}
	paths, err := git(workdir, "ls-files", "-z")
	if err != nil {
		return errors.New("tracked paths could not be inspected for Git filter attributes")
	}
	fallback, sentinel, cleanup, err := createAttributeFallback()
	if err != nil {
		return err
	}
	defer cleanup()
	attributes, err := gitInputWithConfig(workdir, paths, "core.attributesFile="+fallback, "check-attr", "-z", "--stdin", "filter")
	if err != nil {
		return errors.New("Git attributes could not be inspected safely")
	}
	tracked := bytes.Split(paths, []byte{0})
	fields := bytes.Split(attributes, []byte{0})
	if len(tracked) == 0 || len(fields) != 3*(len(tracked)-1)+1 {
		return errors.New("Git attribute inspection output is malformed")
	}
	for i, path := range tracked[:len(tracked)-1] {
		field := fields[3*i : 3*i+3]
		if !bytes.Equal(field[0], path) || string(field[1]) != "filter" {
			return errors.New("Git attribute inspection output is malformed")
		}
		if string(field[2]) != sentinel {
			return errors.New("repository-local Git filter attribute binding is not admitted")
		}
	}
	return nil
}

func createAttributeFallback() (string, string, func(), error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", "", nil, errors.New("private Git attribute sentinel cannot be generated")
	}
	sentinel := "amux_safe_" + hex.EncodeToString(random)
	directory, err := os.MkdirTemp("", "amux-git-attributes-")
	if err != nil {
		return "", "", nil, errors.New("private Git attribute directory cannot be created")
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path := filepath.Join(directory, "attributes")
	if err := os.WriteFile(path, []byte("* filter="+sentinel+"\n"), 0o600); err != nil {
		cleanup()
		return "", "", nil, errors.New("private Git attribute fallback cannot be written")
	}
	return path, sentinel, cleanup, nil
}

var errGitOutputBound = errors.New("Git command output exceeded its bound")

type boundedBuffer struct {
	b     bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.b.Len()
	if remaining >= len(p) {
		return b.b.Write(p)
	}
	if remaining > 0 {
		_, _ = b.b.Write(p[:remaining])
	}
	return max(remaining, 0), errGitOutputBound
}

func (b *boundedBuffer) Bytes() []byte { return b.b.Bytes() }

func git(workdir string, args ...string) ([]byte, error) {
	return gitInput(workdir, nil, args...)
}

func gitInput(workdir string, input []byte, args ...string) ([]byte, error) {
	return gitInputWithConfig(workdir, input, "", args...)
}

func gitInputWithConfig(workdir string, input []byte, config string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gitArgs := []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}
	if config != "" {
		gitArgs = append(gitArgs, "-c", config)
	}
	gitArgs = append(gitArgs, "-C", workdir)
	cmd := exec.CommandContext(ctx, gitPath, append(gitArgs, args...)...)
	cmd.Env = []string{
		"HOME=/nonexistent", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LITERAL_PATHSPECS=1",
		"GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0",
	}
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{limit: maxGitBytes}
	stderr := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}
