package main

import (
	"bytes"
	"context"
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
	gitPath        = "/usr/bin/git"
)

var fixedArgs = []string{
	"--model", model,
	"--no-session", "--no-tools", "--no-extensions", "--no-skills",
	"--no-prompt-templates", "--no-themes", "--no-context-files", "--no-approve",
	"-p",
}

var extraProcessEnvironment func() []string

type options struct {
	pi, piSHA256, node, nodeSHA256 string
	workdir, file, task            string
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
	Status           string   `json:"status"`
	Package          string   `json:"package"`
	Version          string   `json:"version"`
	Model            string   `json:"model"`
	Executable       string   `json:"executable"`
	ExecutableSHA256 string   `json:"executable_sha256"`
	Node             string   `json:"node"`
	NodeSHA256       string   `json:"node_sha256"`
	Argv             []string `json:"argv"`
	ChangedPath      string   `json:"changed_path"`
	StdoutBytes      int      `json:"stdout_bytes"`
	StderrBytes      int      `json:"stderr_bytes"`
	Stderr           string   `json:"stderr"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "blocked:", err)
		os.Exit(2)
	}
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
	flags.DurationVar(&o.timeout, "timeout", 2*time.Minute, "wall-clock limit")
	flags.IntVar(&o.stdoutLimit, "stdout-limit", 128<<10, "final response byte limit")
	flags.IntVar(&o.stderrLimit, "stderr-limit", 16<<10, "diagnostic byte limit")
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

	argv := append(append([]string{node, pi}, fixedArgs...), prompt)
	stdout, stderr, err := execute(o, argv, agentDir)
	if err != nil {
		return err
	}
	if currentPi, currentDigest, err := admitPi(o.pi, o.piSHA256); err != nil || currentPi != pi || currentDigest != piDigest {
		return errors.New("Pi executable or package identity changed during execution")
	}
	if currentNode, currentDigest, err := admitExecutable(o.node, o.nodeSHA256, "Node"); err != nil || currentNode != node || currentDigest != nodeDigest {
		return errors.New("Node executable identity changed during execution")
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
	if err := requireUnchanged(workdir, target, before); err != nil {
		return err
	}
	if err := replaceFile(target, []byte(parsed.Replacement)); err != nil {
		return err
	}
	if err := requireExactDiff(workdir, relative); err != nil {
		return err
	}

	stderrSummary := "empty"
	if len(stderr) > 0 {
		stderrSummary = "present_redacted"
	}
	return json.NewEncoder(output).Encode(result{
		Status: "replacement_applied_untrusted", Package: packageName, Version: packageVersion,
		Model: model, Executable: pi, ExecutableSHA256: piDigest, Node: node, NodeSHA256: nodeDigest,
		Argv:        append([]string{node, pi}, fixedArgs...),
		ChangedPath: relative, StdoutBytes: len(stdout), StderrBytes: len(stderr), Stderr: stderrSummary,
	})
}

func admitPi(argument, expectedDigest string) (string, string, error) {
	pi, observedDigest, err := admitExecutable(argument, expectedDigest, "Pi")
	if err != nil {
		return "", "", err
	}
	root := filepath.Dir(filepath.Dir(pi))
	metadataPath := filepath.Join(root, "package.json")
	metadata, err := os.ReadFile(metadataPath)
	if err != nil || len(metadata) > 1<<20 {
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
	if !filepath.IsAbs(argument) || len(expectedDigest) != 64 {
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
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("%s executable cannot be hashed", label)
	}
	observed := digest(contents)
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
	for _, overlay := range []string{"models.json", "package.json"} {
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
	if filepath.IsAbs(relative) || relative == "." || relative != filepath.Clean(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", "", "", nil, errors.New("--file must be one canonical relative path")
	}
	root, err := git(workdir, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(string(root)) != workdir {
		return "", "", "", nil, errors.New("--workdir is not the canonical Git worktree root")
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

func buildPrompt(task, path, originalDigest string, contents []byte) (string, error) {
	packet := struct {
		Task           string `json:"task"`
		Path           string `json:"path"`
		OriginalSHA256 string `json:"original_sha256"`
		Original       string `json:"original"`
	}{task, path, originalDigest, string(contents)}
	encoded, err := json.Marshal(packet)
	if err != nil {
		return "", errors.New("task packet cannot be encoded")
	}
	return "You are a bounded replacement generator. Do not use tools, sessions, files, network publishing, delegation, retries, or external context. Return only one JSON object with exactly these string fields: path, original_sha256, replacement. Preserve path and original_sha256 exactly; replacement is the complete new file. Task packet: " + string(encoded), nil
}

type cappedWriter struct {
	mu       sync.Mutex
	b        bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
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
		w.cancel()
	}
	return len(p), nil
}

func execute(o options, argv []string, agentDir string) ([]byte, []byte, error) {
	ctx, timeoutCancel := context.WithTimeout(context.Background(), o.timeout)
	defer timeoutCancel()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &cappedWriter{limit: o.stdoutLimit, cancel: cancel}
	stderr := &cappedWriter{limit: o.stderrLimit, cancel: cancel}
	privateDir, err := os.MkdirTemp("", "amux-pi-spark-")
	if err != nil {
		return nil, nil, errors.New("private Pi working directory cannot be created")
	}
	defer os.RemoveAll(privateDir)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = privateDir
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, errors.New("owner home is unavailable")
	}
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
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	err = cmd.Run()
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
		_ = cmd.Cancel()
		if err := verifyProcessGroupGone(pid); err != nil {
			return nil, nil, err
		}
	}
	if stdout.overflow || stderr.overflow {
		return nil, nil, errors.New("Pi output exceeded its configured bound")
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, exec.ErrWaitDelay) {
		return nil, nil, fmt.Errorf("Pi timed out after %s", o.timeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, nil, fmt.Errorf("Pi exited with code %d", exitErr.ExitCode())
		}
		return nil, nil, errors.New("Pi failed to start")
	}
	return stdout.b.Bytes(), stderr.b.Bytes(), nil
}

func verifyProcessGroupGone(pid int) error {
	if pid <= 0 {
		return errors.New("Pi process identity was not recorded")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(-pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return errors.New("Pi process-group termination could not be verified")
		}
		if time.Now().After(deadline) {
			return errors.New("Pi process group remained live after termination")
		}
		time.Sleep(10 * time.Millisecond)
	}
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
	return value, nil
}

func requireUnchanged(workdir, target string, before []byte) error {
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
	status, err := git(workdir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	expected := []byte(" M " + relative + "\x00")
	if err != nil || !bytes.Equal(status, expected) {
		return errors.New("resulting Git diff is not exactly the one allowed file")
	}
	changed, err := git(workdir, "diff", "--name-only", "-z", "--no-ext-diff")
	if err != nil || !bytes.Equal(changed, []byte(relative+"\x00")) {
		return errors.New("Git diff scope verification failed")
	}
	staged, err := git(workdir, "diff", "--cached", "--name-only", "-z", "--no-ext-diff")
	if err != nil || len(staged) != 0 {
		return errors.New("unexpected staged diff after replacement")
	}
	return nil
}

func git(workdir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, gitPath, append([]string{"-C", workdir}, args...)...)
	cmd.Env = []string{
		"HOME=/nonexistent", "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_LITERAL_PATHSPECS=1",
	}
	return cmd.Output()
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}
