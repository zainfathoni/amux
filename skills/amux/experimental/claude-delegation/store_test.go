package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestLinuxProcessIdentityRejectsAmbiguousSnapshots(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, pathlib, sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
fields = [b"S"] + [b"0"] * 18 + [b"424242"]
assert module.linux_process_start_time(b"123 (claude ) name) " + b" ".join(fields)) == "424242"
try:
    module.linux_process_start_time(b"123 malformed")
except module.HelperError:
    pass
else:
    raise AssertionError("malformed stat accepted")

def rejected(starts, executables, commands):
    start_values = iter(starts)
    executable_values = iter(executables)
    command_values = iter(commands)
    module.read_linux_process_start = lambda proc: next(start_values)
    module.read_linux_process_executable = lambda proc: next(executable_values)
    module.read_linux_process_command = lambda proc: next(command_values)
    try:
        module.read_linux_process_snapshot(123)
    except module.HelperError:
        return
    raise AssertionError("ambiguous Linux process snapshot accepted")

same_executable = ("/usr/bin/claude", 8, 42)
rejected(["100", "101"], [same_executable, same_executable], [b"claude\0", b"claude\0"])
rejected(["100", "100"], [same_executable, ("/usr/bin/claude", 8, 43)], [b"claude\0", b"claude\0"])
rejected(["100", "100"], [same_executable, same_executable], [b"claude\0", b"other\0"])
rejected(["100", "100"], [same_executable, same_executable], [b"", b""])
rejected(["100", "100"], [same_executable, same_executable], [b"claude", b"claude"])

package = "/tmp/node_modules/@anthropic-ai/claude-code/cli.js"
assert module.is_claude_process("Darwin", "claude", ["/usr/bin/claude", "--session-id", "session"], 1)
assert not module.is_claude_process("Darwin", "node", ["/usr/bin/node", package, "--session-id", "session"], 2)
assert module.is_claude_process("Linux", "node", ["/usr/bin/node", package, "--session-id", "session"], 2)
assert module.is_claude_process("Linux", "bun", ["/usr/bin/bun", package, "--session-id", "session"], 2)
assert not module.is_claude_process("Linux", "node", ["/usr/bin/node", "/tmp/unrelated/cli.js", package, "--session-id", "session"], 3)
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("Linux stat parser fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Linux stat parser output = %q", output)
	}
}

func TestLinuxLifecycleRejectsIndependentObservedIdentityDriftWithoutTargetMutation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process identity is backed by procfs")
	}
	sessionID := "650e8400-e29b-41d4-a716-446655440000"
	_, executable := startProcessFixture(t, "claude", "--session-id", sessionID, "policy")
	_, alternateExecutable := startProcessFixture(t, "claude", "--session-id", sessionID, "policy")
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	script := `import hashlib, importlib.util, os, pathlib, sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
state = pathlib.Path(sys.argv[2])
executable = pathlib.Path(sys.argv[3])
alternate = pathlib.Path(sys.argv[4])
session_id = sys.argv[5]
proc_root = state / "proc"
module.LINUX_PROC_ROOT = proc_root
pane = {"pid": 4242, "window_id": "@20", "pane_id": "%20"}
mutations = []
start_command = "exec claude --session-id " + session_id + " policy"
arguments = ["--session-id", session_id, "policy"]

def write_proc(pid, start, target, argv):
    proc = proc_root / str(pid)
    proc.mkdir(parents=True, exist_ok=True)
    fields = ["S"] + ["0"] * 18 + [str(start)]
    (proc / "stat").write_text(f"{pid} (claude) " + " ".join(fields))
    (proc / "cmdline").write_bytes(b"\0".join(os.fsencode(value) for value in [str(target), *argv]) + b"\0")
    (proc / "exe").unlink(missing_ok=True)
    (proc / "exe").symlink_to(target)

def reset_observation():
    pane.update(pid=4242, window_id="@20", pane_id="%20")
    write_proc(4242, 100, executable, arguments)
    write_proc(4343, 100, executable, arguments)
    mutations.clear()

def run_command(command, environment=None, executable_fd=None):
    if command[:2] == ["tmux", "display-message"]:
        return "\t".join(["Claude", "thinker", pane["window_id"], pane["pane_id"], str(pane["pid"]), str(state), "claude", start_command])
    if command[:2] == ["tmux", "kill-pane"]:
        mutations.append("kill")
        return ""
    if command and command[0] == "tmux":
        mutations.append("unexpected-tmux")
        return ""
    raise AssertionError(f"unexpected command: {command}")

module.run_command = run_command
reset_observation()
descriptor, _, launcher_identity, object_identity = module.open_verified_executable(executable)
os.close(descriptor)
argv_digest = module.normalized_argv_digest(arguments)
baseline_identity = module.inspect_claude_identity("%20", session_id, argv_digest, launcher_identity, object_identity)
binding = {
    "protocol_version": 1, "delegation_id": "linux-observed-drift", "nonce": "a" * 64,
    "task_id": "task", "question_message_id": "question", "origin_thread": "origin",
    "repository": "repository", "base": "b" * 40, "workdir": str(state),
    "producer_role": "thinker", "authority": "read_only", "task_reference": "fixture",
    "packet_digest": "c" * 64, "launch_policy_digest": "d" * 64,
    "launch_command_digest": hashlib.sha256(start_command.encode()).hexdigest(),
}
store = module.ReceiptStore(state)
assert store.create({"binding": binding, "routing": {"target": "machine_local_inbox"}}) == "recorded"
data = store.load_store()
receipt = data["receipts"][0]
receipt["events"].extend([
    {"event_id": "launch", "kind": "launch_intent", "expected_argv_digest": argv_digest,
     "expected_launcher_identity": launcher_identity, "expected_executable_object_identity": object_identity},
    {"event_id": "launch-result", "kind": "launch_completed", "operation_event_id": "launch", "identity": baseline_identity},
])
store.commit(data)
before_acquisition = store.path.read_bytes()

def start_drift(): write_proc(4242, 101, executable, arguments)
def executable_drift(): write_proc(4242, 100, alternate, arguments)
def argv_drift(): write_proc(4242, 100, executable, ["--session-id", session_id, "changed-policy"])
def pid_reuse(): pane.update(pid=4343)
def pane_drift(): pane.update(window_id="@21")
cases = {
    "kernel start identity": start_drift,
    "executable identity": executable_drift,
    "NUL-delimited argv digest": argv_drift,
    "PID reuse state": pid_reuse,
    "tmux pane identity": pane_drift,
}

for name, mutate in cases.items():
    store.path.write_bytes(before_acquisition)
    reset_observation()
    mutate()
    try:
        store.acquire_session({"delegation_id": binding["delegation_id"], "event_id": "acquire-" + name.replace(" ", "-"), "pane_id": "%20", "claude_session_id": session_id})
    except module.HelperError:
        pass
    else:
        raise AssertionError(f"acquisition accepted changed {name}")
    assert store.path.read_bytes() == before_acquisition, f"rejected acquisition mutated receipt for {name}"
    assert not mutations, f"rejected acquisition mutated target for {name}: {mutations}"

store.path.write_bytes(before_acquisition)
reset_observation()
assert store.acquire_session({"delegation_id": binding["delegation_id"], "event_id": "acquire-baseline", "pane_id": "%20", "claude_session_id": session_id}) == "recorded"
data = store.load_store()
data["receipts"][0]["state"] = "acknowledged"
store.commit(data)
before_parking = store.path.read_bytes()
for name, mutate in cases.items():
    store.path.write_bytes(before_parking)
    reset_observation()
    mutate()
    try:
        store.park({"delegation_id": binding["delegation_id"], "event_id": "park-" + name.replace(" ", "-")})
    except module.HelperError:
        pass
    else:
        raise AssertionError(f"parking accepted changed {name}")
    assert "kill" not in mutations, f"rejected parking killed target for {name}"
    assert "unexpected-tmux" not in mutations, f"rejected parking adopted or notified target for {name}"
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper, stateDir, executable, alternateExecutable, sessionID).CombinedOutput()
	if err != nil {
		t.Fatalf("Linux lifecycle identity fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Linux lifecycle identity output = %q", output)
	}
}

func TestLinuxNodeDescriptorIdentityRejectsSameInodeContentSubstitution(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux node and bun launchers use proc descriptor identity")
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import hashlib, importlib.util, os, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
directory = pathlib.Path(tempfile.mkdtemp())
launcher = directory / "cli.js"
launcher.write_bytes(b"original descriptor launcher")
launcher.chmod(0o755)
descriptor, _, expected_launcher, expected_object = module.open_verified_executable(launcher)
session_id = "550e8400-e29b-41d4-a716-446655440000"
arguments = ["/usr/bin/node", f"/proc/self/fd/{descriptor}", "--session-id", session_id, "--strict-mcp-config"]
expected_argv = module.normalized_argv_digest(arguments[2:])
commands = []
module.run_command = lambda command, environment=None: commands.append(command) or f"Claude\tthinker\t@20\t%20\t{os.getpid()}\t{directory}\tnode\tcommand"
module.exact_process_identity = lambda pid: ("node", "linux:stable", arguments, hashlib.sha256(b"node").hexdigest())
module.process_executable_identity = lambda pid: "file:node:stable"
identity = module.inspect_claude_identity("%20", session_id, expected_argv, expected_launcher, expected_object)
write_descriptor = os.open(launcher, os.O_WRONLY)
before = os.fstat(descriptor)
launcher.unlink()
assert not launcher.exists(), "fixture backing pathname still exists"
os.ftruncate(write_descriptor, 0)
os.write(write_descriptor, b"substituted descriptor launcher")
os.fchmod(write_descriptor, 0o755)
after = os.fstat(descriptor)
assert (before.st_dev, before.st_ino) == (after.st_dev, after.st_ino), "fixture did not preserve inode"
try:
    module.inspect_claude_identity("%20", session_id, expected_argv, expected_launcher, expected_object)
except module.HelperError as error:
    assert "launcher content does not match immutable launch intent" in str(error), error
else:
    raise AssertionError("same-inode launcher substitution was accepted")
assert identity["pane_id"] == "%20"
assert all(command[:2] == ["tmux", "display-message"] for command in commands), commands
os.close(write_descriptor)
os.close(descriptor)
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("Linux descriptor content fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Linux descriptor content output = %q", output)
	}
}

func TestDarwinTransportRejectsCopiedExecutableReplacementBeforeExec(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin materializes a verified executable inside the delegation container")
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import hashlib, importlib.util, json, os, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
state = pathlib.Path(tempfile.mkdtemp())
state.chmod(0o700)
store = module.ReceiptStore(state)
delegation_id = "darwin-copy-replacement"
source = state / "source-claude"
source.write_bytes(b"#!/bin/sh\nexit 0\n")
source.chmod(0o755)
source_descriptor, source_path, launcher_identity, object_identity = module.open_verified_executable(source)
os.close(source_descriptor)
packet = state / "packet"
packet.write_bytes(b"packet")
packet.chmod(0o600)
packet_bytes, packet_identity = module.read_owner_private_regular_file(packet, module.MAX_PACKET_BYTES, "launch packet")
workdir = state / "workdir"
workdir.mkdir(mode=0o700)
work_descriptor, resolved_workdir, workdir_identity = module.open_verified_directory(workdir)
os.close(work_descriptor)
argv = [source_path, "--session-id", "550e8400-e29b-41d4-a716-446655440000"]
transport = {
    "argv": argv,
    "environment": {"PATH": "/usr/bin:/bin"},
    "expected_argv_digest": module.normalized_argv_digest(argv[1:]),
    "expected_launcher_argv0_digest": module.launcher_argv0_digest(argv[0]),
    "expected_launcher_identity": launcher_identity,
    "expected_executable_object_identity": object_identity,
    "remove_environment": [],
    "workdir": resolved_workdir,
    "workdir_identity": workdir_identity,
    "packet_identity": packet_identity,
    "packet_path": str(packet),
    "packet_digest": hashlib.sha256(packet_bytes).hexdigest(),
}
transport_path = module.private_launch_transport_path(store, delegation_id)
darwin_executable = transport_path.with_name("verified-claude")
transport["darwin_executable_path"] = str(darwin_executable)
raw = module.encode_private_json(transport)
module.write_private_bytes(transport_path, raw)
source_descriptor, _, _, _ = module.open_verified_executable(source)
module.materialize_executable(source_descriptor, darwin_executable.parent, darwin_executable)
os.close(source_descriptor)
symlink = transport_path.with_name("symlink-launcher")
symlink.symlink_to(source)
try:
    module.open_exact_verified_executable(symlink)
except OSError:
    pass
else:
    raise AssertionError("Darwin exact executable opener followed a symlink")
replacement = transport_path.with_name("replacement-launcher")
replacement.write_bytes(b"#!/bin/sh\nprintf wrong > wrong-process\n")
replacement.chmod(0o500)
os.replace(replacement, darwin_executable)
container_descriptor = os.open(darwin_executable.parent, os.O_RDONLY)
executable_descriptor, _, _, _ = module.open_exact_verified_executable(darwin_executable)
module.seal_darwin_launch_container(container_descriptor, executable_descriptor)
replacement_performed = True
try:
    os.rename(darwin_executable.parent, str(darwin_executable.parent) + ".replacement")
except OSError:
    container_replacement_blocked = True
else:
    container_replacement_blocked = False
executed = False
def reject_exec(path, argv, environment):
    global executed
    executed = True
    raise AssertionError("wrong copied executable reached execve")
module.os.execve = reject_exec
try:
    try:
        module.execute_launch_transport_locked(store, delegation_id, hashlib.sha256(raw).hexdigest(), "0" * 64)
    except module.HelperError as error:
        assert "verified Claude executable copy changed before execution" in str(error), error
    else:
        raise AssertionError("replaced copied executable was accepted")
finally:
    module.restore_darwin_launch_container(container_descriptor, executable_descriptor)
    os.close(executable_descriptor)
    os.close(container_descriptor)
assert replacement_performed
assert container_replacement_blocked
assert not executed
assert transport_path.parent.stat().st_mode & 0o777 == 0o700
assert state.stat().st_mode & 0o777 == 0o700
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("Darwin copied executable fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Darwin copied executable output = %q", output)
	}
}

func TestDarwinProbeRejectsCopiedExecutableReplacementBeforeExec(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin probes execute a private copy of the verified source descriptor")
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, os, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
directory = pathlib.Path(tempfile.mkdtemp())
source = directory / "source-claude"
wrong_marker = directory / "wrong-executed"
wrong_script = f"#!/bin/sh\nprintf wrong > {wrong_marker}\nprintf wrong\n".encode()
source_prefix = b"#!/bin/sh\nprintf source\n#"
assert len(source_prefix) < len(wrong_script)
source_script = source_prefix + b"s" * (len(wrong_script) - len(source_prefix))
assert len(source_script) == len(wrong_script)
source.write_bytes(source_script)
source.chmod(0o755)
source_descriptor, source_path, _, _ = module.open_verified_executable(source)
probe_root = pathlib.Path(tempfile.gettempdir())
before = set(probe_root.glob("amux-claude-probe.*"))
original_materialize = module.materialize_executable
replacement_performed = False
def substitute(descriptor, output_directory, destination=None):
    global replacement_performed
    output = original_materialize(descriptor, output_directory, destination)
    replacement = output_directory / "wrong-probe"
    replacement.write_bytes(wrong_script)
    replacement.chmod(0o500)
    os.replace(replacement, output)
    replacement_performed = True
    return output
module.materialize_executable = substitute
seal_calls = 0
restore_calls = 0
retained_descriptors = []
original_seal = module.seal_darwin_launch_container
original_restore = module.restore_darwin_launch_container
def observe_seal(container_descriptor, executable_descriptor):
    global seal_calls
    seal_calls += 1
    original_seal(container_descriptor, executable_descriptor)
def observe_restore(container_descriptor, executable_descriptor):
    global restore_calls
    restore_calls += 1
    retained_descriptors.extend([container_descriptor, executable_descriptor])
    original_restore(container_descriptor, executable_descriptor)
module.seal_darwin_launch_container = observe_seal
module.restore_darwin_launch_container = observe_restore
try:
    module.run_command([source_path, "--version"], {"PATH": "/usr/bin:/bin"}, source_descriptor)
except module.HelperError as error:
    assert "verified Claude probe executable copy changed" in str(error), error
else:
    raise AssertionError("replaced Darwin probe executable was accepted")
finally:
    os.close(source_descriptor)
assert replacement_performed
assert seal_calls == 0, seal_calls
assert restore_calls == 1, restore_calls
assert not wrong_marker.exists(), "wrong Darwin probe code executed"
for descriptor in retained_descriptors:
    try:
        os.fstat(descriptor)
    except OSError:
        pass
    else:
        raise AssertionError("Darwin probe retained descriptor was not closed")
after = set(probe_root.glob("amux-claude-probe.*"))
assert after == before, (before, after)
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("Darwin probe executable fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Darwin probe executable output = %q", output)
	}
}

func TestDarwinLaunchAcceptsTransportCopyOfVersionedClaudeExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin transport executes a private copy of the versioned Claude executable")
	}
	fixture := newLaunchFixture(t)
	permitSealedRuntimeTempCleanup(t, fixture.stateDir)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
	claudeLink := filepath.Join(fixture.binDir, "claude")
	versionedClaude := filepath.Join(fixture.binDir, "2.1.212")
	if err := os.Rename(claudeLink, versionedClaude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(versionedClaude), claudeLink); err != nil {
		t.Fatal(err)
	}
	receiptPath := createPlannedLaunchReceipt(t, fixture)

	started := time.Now()
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err != nil || !strings.Contains(stdout, `"outcome":"launched"`) {
		t.Fatalf("versioned Darwin transport launch = %v: %s%s", err, stdout, stderr)
	}
	if elapsed := time.Since(started); elapsed < 1400*time.Millisecond {
		t.Fatalf("Darwin launch completed before the stable startup window: %s", elapsed)
	}
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receipt, []byte(`"kind":"launch_completed"`)) {
		t.Fatalf("verified Darwin transport launch did not record completion: %s", receipt)
	}
	var stored map[string]any
	if err := json.Unmarshal(receipt, &stored); err != nil {
		t.Fatal(err)
	}
	receiptValue := stored["receipts"].([]any)[0].(map[string]any)
	events := receiptValue["events"].([]any)
	launchIdentity := events[len(events)-1].(map[string]any)["identity"].(map[string]any)
	if launchIdentity["process_name"] != "verified-claude" || launchIdentity["process_executable_object_identity"] == "" {
		t.Fatalf("Darwin launch did not bind the private executable object: %#v", launchIdentity)
	}
	var launchIntent map[string]any
	for _, value := range events {
		event := value.(map[string]any)
		if event["kind"] == "launch_intent" {
			launchIntent = event
			break
		}
	}
	launcherPathDigest, ok := launchIntent["expected_launcher_argv0_digest"].(string)
	if !ok || len(launcherPathDigest) != 64 {
		t.Fatalf("Darwin launch intent did not bind the launcher path digest: %#v", launchIntent)
	}

	acquire := map[string]any{
		"delegation_id": fixture.request["delegation_id"], "event_id": "acquire-versioned-transport",
		"pane_id": "%20", "claude_session_id": fixture.request["claude_session_id"],
	}
	beforeAcquisition := append([]byte(nil), receipt...)
	wrongSession := cloneJSONMap(t, acquire)
	wrongSession["event_id"] = "acquire-wrong-session"
	wrongSession["claude_session_id"] = "650e8400-e29b-41d4-a716-446655440000"
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, wrongSession, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "expected Claude session") {
		t.Fatalf("Darwin transport wrong-session acquisition = %v: %s", err, stderr)
	}
	wrongWorkdirEnvironment := append(append([]string(nil), fixture.environment...), "REPORTED_WORKDIR="+t.TempDir())
	wrongWorkdir := cloneJSONMap(t, acquire)
	wrongWorkdir["event_id"] = "acquire-wrong-workdir"
	_, stderr, err = runHelperEnv(t, fixture.stateDir, wrongWorkdirEnvironment, wrongWorkdir, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "exact process and pane created by this receipt") {
		t.Fatalf("Darwin transport wrong-workdir acquisition = %v: %s", err, stderr)
	}
	wrongCommandEnvironment := append(append([]string(nil), fixture.environment...), "REPORTED_START_COMMAND=other-command")
	wrongCommand := cloneJSONMap(t, acquire)
	wrongCommand["event_id"] = "acquire-wrong-launch-command"
	_, stderr, err = runHelperEnv(t, fixture.stateDir, wrongCommandEnvironment, wrongCommand, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "exact process and pane created by this receipt") {
		t.Fatalf("Darwin transport wrong-launch-command acquisition = %v: %s", err, stderr)
	}
	afterRejectedAcquisition, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterRejectedAcquisition, beforeAcquisition) {
		t.Fatalf("rejected Darwin acquisition mutated the receipt:\nbefore: %s\nafter: %s", beforeAcquisition, afterRejectedAcquisition)
	}
	if err := json.Unmarshal(beforeAcquisition, &stored); err != nil {
		t.Fatal(err)
	}
	receiptValue = stored["receipts"].([]any)[0].(map[string]any)
	events = receiptValue["events"].([]any)
	events[len(events)-1].(map[string]any)["identity"].(map[string]any)["process_identity"] = "changed-incarnation"
	tamperedCompletion, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, tamperedCompletion, 0o600); err != nil {
		t.Fatal(err)
	}
	incarnationSubstitution := cloneJSONMap(t, acquire)
	incarnationSubstitution["event_id"] = "acquire-process-incarnation-substitution"
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, incarnationSubstitution, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "exact process and pane created by this receipt") {
		t.Fatalf("Darwin process-incarnation substitution acquisition = %v: %s", err, stderr)
	}
	afterIncarnationRejection, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterIncarnationRejection, tamperedCompletion) {
		t.Fatalf("rejected Darwin incarnation acquisition mutated the receipt")
	}
	if err := os.WriteFile(receiptPath, beforeAcquisition, 0o600); err != nil {
		t.Fatal(err)
	}

	originalLauncher := versionedClaude + ".original"
	if err := os.Rename(versionedClaude, originalLauncher); err != nil {
		t.Fatal(err)
	}
	launcherContent, err := os.ReadFile(originalLauncher)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versionedClaude, launcherContent, 0o500); err != nil {
		t.Fatal(err)
	}
	objectSubstitution := cloneJSONMap(t, acquire)
	objectSubstitution["event_id"] = "acquire-launcher-object-substitution"
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, objectSubstitution, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "immutable launch intent") {
		t.Fatalf("Darwin transport launcher-object substitution = %v: %s", err, stderr)
	}
	if err := os.Remove(versionedClaude); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(originalLauncher, versionedClaude); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", acquire, "session", "acquire")

	receipt, err = os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(receipt, &stored); err != nil {
		t.Fatal(err)
	}
	receiptValue = stored["receipts"].([]any)[0].(map[string]any)
	receiptValue["state"] = "acknowledged"
	acknowledged, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, acknowledged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(acknowledged, &stored); err != nil {
		t.Fatal(err)
	}
	receiptValue = stored["receipts"].([]any)[0].(map[string]any)
	receiptValue["session_identity"].(map[string]any)["process_identity"] = "changed-incarnation"
	wrongParkingIdentity, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, wrongParkingIdentity, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, map[string]any{
		"delegation_id": fixture.request["delegation_id"], "event_id": "park-incarnation-substitution",
	}, "session", "park")
	if err == nil || !strings.Contains(stderr, "incarnation identity changed") {
		t.Fatalf("Darwin process-incarnation substitution parking = %v: %s", err, stderr)
	}
	if err := os.WriteFile(receiptPath, acknowledged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versionedClaude, []byte("changed launcher content"), 0o500); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, map[string]any{
		"delegation_id": fixture.request["delegation_id"], "event_id": "park-content-substitution",
	}, "session", "park")
	if err == nil || !strings.Contains(stderr, "launcher content does not match immutable launch intent") {
		t.Fatalf("Darwin transport launcher-content substitution parking = %v: %s", err, stderr)
	}
	log, err := os.ReadFile(fixture.tmuxLog)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(log, []byte("kill-pane")) {
		t.Fatalf("Darwin identity substitution killed the pane: %s", log)
	}
	finalReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(finalReceipt, []byte(`"kind":"verified_parked"`)) {
		t.Fatalf("Darwin content substitution recorded false parking completion: %s", finalReceipt)
	}
}

func TestDarwinTransportVersionedExecutableRejectsArgvSubstitutionWithoutCompletion(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin transport executes a private copy of the versioned Claude executable")
	}
	fixture := newLaunchFixture(t)
	permitSealedRuntimeTempCleanup(t, fixture.stateDir)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
	claudeLink := filepath.Join(fixture.binDir, "claude")
	versionedClaude := filepath.Join(fixture.binDir, "2.1.212")
	if err := os.Rename(claudeLink, versionedClaude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(versionedClaude), claudeLink); err != nil {
		t.Fatal(err)
	}
	fixture.environment = append(fixture.environment, "SUBSTITUTE_ARGV=1")
	receiptPath := createPlannedLaunchReceipt(t, fixture)
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "argv does not match immutable launch intent") {
		t.Fatalf("Darwin transport argv substitution = %v: %s", err, stderr)
	}
	receipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(receipt, []byte(`"kind":"launch_intent"`)) || bytes.Contains(receipt, []byte(`"kind":"launch_completed"`)) {
		t.Fatalf("Darwin argv substitution did not remain indeterminate: %s", receipt)
	}
	log, err := os.ReadFile(fixture.tmuxLog)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(log, []byte("kill-pane")) {
		t.Fatalf("Darwin argv substitution killed the pane: %s", log)
	}
}

func TestDarwinTransportRejectsLauncherPathSubstitutionsWithoutCompletionOrKill(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin transport preserves the planned launcher as argv[0]")
	}
	for _, test := range []struct {
		name        string
		alternate   string
		wantStartup string
	}{
		{name: "verified process cannot fall through to direct Claude form", alternate: "claude", wantStartup: "launcher path does not match immutable launch intent"},
		{name: "same-object hardlink cannot replace planned launcher path", alternate: "2.1.212-hardlink", wantStartup: "launcher path does not match immutable launch intent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLaunchFixture(t)
			permitSealedRuntimeTempCleanup(t, fixture.stateDir)
			if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
			claudeLink := filepath.Join(fixture.binDir, "claude")
			versionedClaude := filepath.Join(fixture.binDir, "2.1.212")
			if err := os.Rename(claudeLink, versionedClaude); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(versionedClaude), claudeLink); err != nil {
				t.Fatal(err)
			}
			alternate := filepath.Join(t.TempDir(), test.alternate)
			if err := os.Link(versionedClaude, alternate); err != nil {
				t.Fatal(err)
			}
			fixture.environment = append(fixture.environment, "SUBSTITUTE_ARGV0="+alternate)
			receiptPath := createPlannedLaunchReceipt(t, fixture)
			_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
			if err == nil || !strings.Contains(stderr, test.wantStartup) {
				t.Fatalf("Darwin launcher path substitution = %v: %s", err, stderr)
			}
			receipt, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(receipt, []byte(`"kind":"launch_intent"`)) || bytes.Contains(receipt, []byte(`"kind":"launch_completed"`)) {
				t.Fatalf("Darwin launcher path substitution did not remain indeterminate: %s", receipt)
			}
			_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, map[string]any{
				"delegation_id": fixture.request["delegation_id"], "event_id": "acquire-substituted-launcher",
				"pane_id": "%20", "claude_session_id": fixture.request["claude_session_id"],
			}, "session", "acquire")
			if err == nil || !strings.Contains(stderr, "requires one completed receipt launch") {
				t.Fatalf("Darwin substituted launcher acquisition = %v: %s", err, stderr)
			}
			afterAcquisition, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(afterAcquisition, receipt) {
				t.Fatalf("rejected substituted-launcher acquisition mutated the receipt")
			}
			log, err := os.ReadFile(fixture.tmuxLog)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(log, []byte("kill-pane")) {
				t.Fatalf("Darwin launcher path substitution killed the pane: %s", log)
			}
		})
	}
}

func TestDarwinTransportFormRequiresExactShapeAndStableProcessSnapshot(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import hashlib, importlib.util, os, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
root = pathlib.Path(tempfile.mkdtemp())
launcher = root / "2.1.212"
private = root / "verified-claude"
launcher.write_bytes(b"synthetic executable")
private.write_bytes(launcher.read_bytes())
launcher.chmod(0o500)
private.chmod(0o500)
session_id = "550e8400-e29b-41d4-a716-446655440000"
arguments = [str(launcher), "--session-id", session_id, "policy"]
descriptor, _, launcher_identity, launcher_object = module.open_exact_verified_executable(launcher)
os.close(descriptor)
descriptor, _, _, private_object = module.open_exact_verified_executable(private)
os.close(descriptor)

assert module.normalized_claude_arguments("Darwin", "verified-claude", arguments, private) == ("darwin_transport", arguments[1:])
alternate_direct = [str(root / "claude"), *arguments[1:]]
assert module.normalized_claude_arguments("Darwin", "verified-claude", alternate_direct, private) == ("darwin_transport", alternate_direct[1:])
for name, argv, executable in (
    ("other", arguments, private),
    ("verified-claude", arguments, root / "other"),
    ("verified-claude", ["relative-launcher", *arguments[1:]], private),
):
    try:
        module.normalized_claude_arguments("Darwin", name, argv, executable)
    except module.HelperError:
        pass
    else:
        raise AssertionError((name, argv, executable))

module.platform.system = lambda: "Darwin"
module.process_executable_path = lambda pid: private
module.run_command = lambda command, environment=None, executable_fd=None: "Claude\tthinker\t@20\t%20\t4242\t" + str(root) + "\tverified-claude\tcommand"

stable_snapshot = ("verified-claude", "100.000001", arguments, hashlib.sha256(b"stable").hexdigest())
module.exact_process_identity = lambda pid: stable_snapshot
try:
    module.inspect_claude_identity(
        "%20", session_id, module.normalized_argv_digest(arguments[1:]),
        launcher_identity, launcher_object, None, None,
        module.launcher_argv0_digest(arguments[0]),
    )
except module.HelperError as error:
    assert "requires the planned private executable object identity" in str(error), error
else:
    raise AssertionError("Darwin transport without private executable identity was accepted")

try:
    module.inspect_claude_identity(
        "%20", session_id, module.normalized_argv_digest(arguments[1:]),
        launcher_identity, launcher_object, None, private_object,
    )
except module.HelperError as error:
    assert "launcher path does not match" in str(error), error
else:
    raise AssertionError("missing launcher argv0 digest was accepted without compatibility")
historical = module.inspect_claude_identity(
    "%20", session_id, module.normalized_argv_digest(arguments[1:]),
    launcher_identity, launcher_object, None, private_object,
    allow_missing_launcher_argv0_digest=True,
)
assert historical["normalized_argv_digest"] == module.normalized_argv_digest(arguments[1:])
assert historical["process_executable_object_identity"] == private_object

snapshots = iter([
    ("verified-claude", "100.000001", arguments, hashlib.sha256(b"before").hexdigest()),
    ("verified-claude", "100.000002", arguments, hashlib.sha256(b"after").hexdigest()),
])
module.exact_process_identity = lambda pid: next(snapshots)
try:
    module.inspect_claude_identity(
        "%20", session_id, module.normalized_argv_digest(arguments[1:]),
        launcher_identity, launcher_object, None, private_object,
        module.launcher_argv0_digest(arguments[0]),
    )
except module.HelperError as error:
    assert "changed during identity inspection" in str(error), error
else:
    raise AssertionError("hybrid Darwin process snapshot was accepted")
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("Darwin transport shape fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("Darwin transport shape output = %q", output)
	}
}

func TestPrivateReaderRejectsWrongEffectiveOwner(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, os, pathlib, stat, sys, tempfile, types
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
descriptor, name = tempfile.mkstemp()
os.write(descriptor, b"packet")
os.close(descriptor)
os.chmod(name, 0o600)
real_fstat = module.os.fstat
info = os.stat(name)
module.os.fstat = lambda descriptor: types.SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=module.os.geteuid() + 1)
try:
    module.read_owner_private_regular_file(pathlib.Path(name), 1024, "launch packet")
except module.HelperError as error:
    assert "owner-only regular file" in str(error)
else:
    raise AssertionError("wrong effective owner was accepted")
finally:
    module.os.fstat = real_fstat
    os.unlink(name)
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil {
		t.Fatalf("wrong-owner private reader fixture: %v\n%s", err, output)
	}
	if string(output) != "ok\n" {
		t.Fatalf("wrong-owner private reader output = %q", output)
	}
}

func TestReceiptBindingIsImmutableWhileRoutingCanChange(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-1")
	create := map[string]any{"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"}}
	assertHelperOutcome(t, stateDir, "recorded", create, "receipt", "create")
	assertHelperOutcome(t, stateDir, "duplicate", create, "receipt", "create")

	conflictingBinding := cloneJSONMap(t, binding)
	conflictingBinding["origin_thread"] = "T-crossed"
	conflict := map[string]any{"binding": conflictingBinding, "routing": create["routing"]}
	_, stderr, err := runHelper(t, stateDir, conflict, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "different immutable binding") {
		t.Fatalf("conflicting create error = %v, stderr %q; want immutable-binding rejection", err, stderr)
	}

	route := map[string]any{
		"delegation_id": "delegation-1",
		"event_id":      "route-1",
		"routing":       map[string]any{"target": "T-origin", "recovery": "machine_local_inbox"},
	}
	assertHelperOutcome(t, stateDir, "recorded", route, "receipt", "route")
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", "delegation-1")
	if err != nil {
		t.Fatalf("show receipt: %v: %s", err, stderr)
	}
	var receipt struct {
		Binding map[string]any `json:"binding"`
		Routing map[string]any `json:"routing"`
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("decode receipt: %v\n%s", err, stdout)
	}
	if receipt.Binding["origin_thread"] != "T-origin" {
		t.Fatalf("routing mutation changed immutable origin: %#v", receipt.Binding)
	}
	if receipt.Routing["target"] != "T-origin" || receipt.Routing["recovery"] != "machine_local_inbox" {
		t.Fatalf("routing = %#v, want updated target with inbox recovery", receipt.Routing)
	}
}

func TestWorkerTeardownLifecycleScopesPairsByImmutableOriginThread(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	unrelated := testBinding("delegation-unrelated")
	unrelated["origin_thread"] = "T-unrelated"
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": unrelated, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{},
		"lifecycle", "worker-teardown", "--origin-thread", "T-origin", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run unrelated worker teardown: %v: %s", err, stderr)
	}
	var result struct {
		Action             string `json:"action"`
		OriginThreadSHA256 string `json:"origin_thread_sha256"`
		Outcome            string `json:"outcome"`
		DryRun             bool   `json:"dry_run"`
		Pairs              []any  `json:"pairs"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode worker teardown dry-run: %v\n%s", err, stdout)
	}
	wantOriginDigest := fmt.Sprintf("%x", sha256.Sum256([]byte("T-origin")))
	if result.Action != "worker_teardown" || result.OriginThreadSHA256 != wantOriginDigest || result.Outcome != "ready" || !result.DryRun || len(result.Pairs) != 0 {
		t.Fatalf("worker teardown dry-run = %#v, want ready no-pair result", result)
	}
	if strings.Contains(stdout+stderr, "T-origin") || strings.Contains(stdout+stderr, "delegation-unrelated") {
		t.Fatalf("worker teardown dry-run leaked raw lifecycle identity: %s%s", stdout, stderr)
	}

	stored, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stored, []byte(`"origin_thread":"T-unrelated"`)) {
		t.Fatalf("unrelated durable pair was changed: %s", stored)
	}
}

func TestWorkerTeardownLifecycleParksOnlyExactAcknowledgedPair(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	identityUnavailable := filepath.Join(t.TempDir(), "identity-unavailable")
	sessionID := "850e8400-e29b-41d4-a716-446655440000"
	arguments := []string{"--session-id", sessionID, "policy"}
	panePID, executable := startProcessFixture(t, "claude", arguments...)
	launcherIdentity := testExecutableIdentity(t, executable)
	objectIdentity := testExecutableObjectIdentity(t, executable)
	startCommand := "exec claude --session-id " + sessionID + " policy"
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  display-message)
    test ! -e "$IDENTITY_UNAVAILABLE"
    printf 'Claude\tthinker\t%s\t%%40\t%s\t%s\tclaude\t%s\n' "$CLAUDE_WINDOW_ID" "$PANE_PID" "$TARGET_WORKDIR" "$START_COMMAND"
    ;;
  list-panes) exit 0 ;;
  kill-pane) printf '%s\n' "$*" >> "$TMUX_LOG" ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "ps"), `#!/bin/sh
set -eu
case "$*" in
  *lstart=*) printf '%s\n' 'Fri Jul 17 12:01:00 2026' ;;
  *comm=*) printf '%s\n' "$PROCESS_EXECUTABLE" ;;
  *command=*) printf 'claude --session-id %s policy\n' "$CLAUDE_SESSION_ID" ;;
  *) exit 2 ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"), "TMUX_LOG="+logPath,
		"TARGET_WORKDIR="+stateDir, "START_COMMAND="+startCommand,
		"CLAUDE_SESSION_ID="+sessionID, "PROCESS_EXECUTABLE="+executable,
		"IDENTITY_UNAVAILABLE="+identityUnavailable, "CLAUDE_WINDOW_ID=@40",
		fmt.Sprintf("PANE_PID=%d", panePID),
	)
	binding := testBinding("private-identity-mismatch-sentinel")
	binding["workdir"] = stateDir
	binding["launch_command_digest"] = fmt.Sprintf("%x", sha256.Sum256([]byte(startCommand)))
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	identity := inspectTestClaudeIdentity(t, environment, "%40", sessionID, nulDigest(arguments), launcherIdentity, objectIdentity)
	if runtime.GOOS == "linux" {
		delete(identity, "process_executable_object_identity")
	}
	recordTestLaunch(t, stateDir, binding["delegation_id"].(string), identity, nulDigest(arguments), launcherIdentity, objectIdentity)
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "acquire-worker-teardown",
		"pane_id": "%40", "claude_session_id": sessionID,
	}, "session", "acquire")
	report := testMessage(binding, "report-worker-teardown", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Synthetic lifecycle fixture.", "rationale": "The full durable chain authorizes exact parking.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{"synthetic exact identity"}, "changed_artifacts": []any{}, "references": []any{},
	})
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", report, "report", "submit")
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "deliver-worker-teardown", "message_id": "report-worker-teardown",
	}, "inbox", "consume")
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "ack-worker-teardown", "message_id": "report-worker-teardown",
	}, "report", "acknowledge")
	acknowledgedReceipt, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}

	for name, unsafeEnvironment := range map[string][]string{
		"mismatched pane": replaceEnvironment(environment, "CLAUDE_WINDOW_ID", "@41"),
		"missing pane":    environment,
	} {
		if name == "missing pane" {
			if err := os.WriteFile(identityUnavailable, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		stdout, stderr, err := runHelperEnv(t, stateDir, unsafeEnvironment, map[string]any{},
			"lifecycle", "worker-teardown", "--origin-thread", "T-origin")
		if err == nil || !strings.Contains(stdout, `"blocker":"identity_mismatch_or_unavailable"`) {
			t.Fatalf("%s worker teardown was not blocked: %v: %s%s", name, err, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "private-identity-mismatch-sentinel") || strings.Contains(stdout+stderr, "T-origin") {
			t.Fatalf("%s worker teardown leaked raw lifecycle identity: %s%s", name, stdout, stderr)
		}
		if _, err := os.Stat(logPath); !os.IsNotExist(err) {
			t.Fatalf("%s worker teardown killed a pane: %v", name, err)
		}
		currentReceipt, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
		if err != nil || !bytes.Equal(currentReceipt, acknowledgedReceipt) {
			t.Fatalf("%s worker teardown changed durable state: %v", name, err)
		}
		if name == "missing pane" {
			if err := os.Remove(identityUnavailable); err != nil {
				t.Fatal(err)
			}
		}
	}

	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{},
		"lifecycle", "worker-teardown", "--origin-thread", "T-origin", "--dry-run")
	if err != nil || !strings.Contains(stdout, `"action":"park"`) || !strings.Contains(stdout, `"outcome":"ready"`) {
		t.Fatalf("worker teardown dry-run did not plan exact pair parking: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "private-identity-mismatch-sentinel") || strings.Contains(stdout+stderr, "T-origin") {
		t.Fatalf("worker teardown dry-run leaked raw lifecycle identity: %s%s", stdout, stderr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("worker teardown dry-run killed a pane: %v", err)
	}

	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{},
		"lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err != nil || !strings.Contains(stdout, `"action":"park"`) || !strings.Contains(stdout, `"outcome":"cleaned"`) {
		t.Fatalf("worker teardown did not clean exact pair: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "private-identity-mismatch-sentinel") || strings.Contains(stdout+stderr, "T-origin") {
		t.Fatalf("worker teardown cleanup leaked raw lifecycle identity: %s%s", stdout, stderr)
	}
	log, err := os.ReadFile(logPath)
	if err != nil || strings.Count(string(log), "kill-pane -t %40") != 1 {
		t.Fatalf("worker teardown exact kill count: %v: %s", err, log)
	}

	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{},
		"lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err != nil || !strings.Contains(stdout, `"action":"none"`) || !strings.Contains(stdout, `"state":"verified_parked"`) {
		t.Fatalf("worker teardown replay was not idempotent: %v: %s%s", err, stdout, stderr)
	}
	log, err = os.ReadFile(logPath)
	if err != nil || strings.Count(string(log), "kill-pane -t %40") != 1 {
		t.Fatalf("worker teardown replay repeated kill: %v: %s", err, log)
	}

	lateBinding := testBinding("late-after-cleanup")
	_, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{
		"binding": lateBinding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "durable teardown fence") {
		t.Fatalf("durable teardown fence permitted a late pair: %v: %s", err, stderr)
	}
	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{},
		"lifecycle", "worker-teardown-release", "--origin-thread", "T-origin")
	if err != nil || !strings.Contains(stdout, `"outcome":"released"`) {
		t.Fatalf("explicit safe fence release failed: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "T-origin") {
		t.Fatalf("worker teardown release leaked raw origin thread: %s%s", stdout, stderr)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": lateBinding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
}

func TestWorkerTeardownLifecycleBlocksUnsafeStatesWithPrivacySafeJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		blocker string
		mutate  func(map[string]any)
	}{
		{
			name: "active without verified launch", blocker: "launch_unverified",
			mutate: func(map[string]any) {},
		},
		{
			name: "indeterminate launch", blocker: "launch_indeterminate",
			mutate: func(receipt map[string]any) {
				receipt["events"] = append(receipt["events"].([]any), testLaunchIntentEvent())
			},
		},
		{
			name: "unacknowledged report", blocker: "unacknowledged_report",
			mutate: func(receipt map[string]any) {
				receipt["state"] = "delivered"
				receipt["events"] = append(receipt["events"].([]any), testLaunchIntentEvent(), testLaunchCompletedEvent())
				receipt["private_test_content"] = "private-report-sentinel"
			},
		},
		{
			name: "unresolved input", blocker: "unresolved_input",
			mutate: func(receipt map[string]any) {
				receipt["input_state"] = "seen"
				receipt["private_test_content"] = "private-input-sentinel"
			},
		},
		{
			name: "indeterminate park", blocker: "park_indeterminate",
			mutate: func(receipt map[string]any) {
				receipt["state"] = "acknowledged"
				receipt["events"] = append(receipt["events"].([]any), testLaunchIntentEvent(), testLaunchCompletedEvent(), map[string]any{
					"event_id": "park-intent", "kind": "park_intent", "identity": map[string]any{"pane_id": "%private"},
				})
			},
		},
		{
			name: "forged acknowledged materialized state", blocker: "receipt_unverified",
			mutate: func(receipt map[string]any) {
				receipt["state"] = "acknowledged"
				receipt["session_identity"] = testLaunchCompletedEvent()["identity"]
				receipt["events"] = append(receipt["events"].([]any), testLaunchIntentEvent(), testLaunchCompletedEvent(), map[string]any{
					"event_id": "acquire-forged", "kind": "session_acquired", "identity": receipt["session_identity"],
				})
			},
		},
		{
			name: "forged parked materialized state", blocker: "receipt_unverified",
			mutate: func(receipt map[string]any) {
				receipt["state"] = "verified_parked"
				receipt["parked_at"] = "2026-07-17T12:00:00Z"
				receipt["cleanup_eligible_at"] = "2026-08-16T12:00:00Z"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateDir := t.TempDir()
			workdir := t.TempDir()
			evidence := filepath.Join(workdir, "lifecycle-evidence")
			if err := os.WriteFile(evidence, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			binding := testBinding("private-delegation-sentinel")
			binding["workdir"] = workdir
			assertHelperOutcome(t, stateDir, "recorded", map[string]any{
				"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
			}, "receipt", "create")
			mutateTestReceipt(t, stateDir, test.mutate)
			before, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
			if err != nil {
				t.Fatal(err)
			}

			stdout, stderr, err := runHelper(t, stateDir, map[string]any{},
				"lifecycle", "worker-teardown", "--origin-thread", "T-origin")
			if err == nil || !strings.Contains(stdout, `"outcome":"blocked"`) || !strings.Contains(stdout, `"blocker":"`+test.blocker+`"`) {
				t.Fatalf("unsafe worker teardown result = %v: %s%s", err, stdout, stderr)
			}
			if strings.Contains(stdout+stderr, "private-") || strings.Contains(stdout+stderr, "T-origin") || strings.Contains(stdout+stderr, "--session-id") {
				t.Fatalf("unsafe worker teardown leaked content or argv: %s%s", stdout, stderr)
			}
			after, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("unsafe worker teardown changed receipt: %v", err)
			}
			if content, err := os.ReadFile(evidence); err != nil || string(content) != "preserve" {
				t.Fatalf("unsafe worker teardown deleted lifecycle evidence: %v: %q", err, content)
			}
		})
	}
}

func TestCanonicalLifecycleRegistryFindsAlternateStoresAndFencesOrigin(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	firstState := t.TempDir()
	secondState := t.TempDir()
	binDir := t.TempDir()
	tmuxLog := filepath.Join(t.TempDir(), "tmux.log")
	writeExecutable(t, filepath.Join(binDir, "tmux"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TMUX_LOG\"\nexit 99\n")
	environment := append(replaceEnvironment(os.Environ(), "HOME", home),
		"PATH="+binDir+":"+os.Getenv("PATH"), "TMUX_LOG="+tmuxLog)
	run := func(stateDir string, input map[string]any, args ...string) (string, string, error) {
		payload, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		commandArgs := []string{helper}
		if stateDir != "" {
			commandArgs = append(commandArgs, "--state-dir", stateDir)
		}
		command := exec.Command("python3", append(commandArgs, args...)...)
		command.Env = environment
		command.Stdin = bytes.NewReader(payload)
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err = command.Run()
		return stdout.String(), stderr.String(), err
	}
	binding := testBinding("alternate-store-pair")
	stdout, stderr, err := run(firstState, map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	if err != nil || !strings.Contains(stdout, `"outcome":"recorded"`) {
		t.Fatalf("create alternate-store pair: %v: %s%s", err, stdout, stderr)
	}

	stdout, stderr, err = run("", map[string]any{}, "lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err == nil || !strings.Contains(stdout, `"outcome":"blocked"`) || !strings.Contains(stdout, `"blocker":"launch_unverified"`) {
		t.Fatalf("canonical teardown missed alternate-store pair: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, firstState) || strings.Contains(stdout+stderr, "alternate-store-pair") || strings.Contains(stdout+stderr, "T-origin") {
		t.Fatalf("canonical teardown leaked store, delegation, or thread identity: %s%s", stdout, stderr)
	}
	if err := os.Remove(filepath.Join(firstState, "receipts.json")); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run("", map[string]any{}, "lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err == nil || !strings.Contains(stdout, `"blocker":"receipt_store_invalid_or_unavailable"`) {
		t.Fatalf("missing registered store did not block teardown: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, firstState) || strings.Contains(stdout+stderr, "alternate-store-pair") || strings.Contains(stdout+stderr, "T-origin") {
		t.Fatalf("missing registered store leaked lifecycle identity: %s%s", stdout, stderr)
	}
	if _, err := os.Stat(tmuxLog); !os.IsNotExist(err) {
		t.Fatalf("missing registered store attempted tmux mutation: %v", err)
	}
	lifecyclePath := filepath.Join(home, "Library", "Application Support", "amux", "experimental", "claude-delegation", "lifecycle.json")
	lifecycleEvidence, err := os.ReadFile(lifecyclePath)
	if err != nil || !bytes.Contains(lifecycleEvidence, []byte(`"T-origin"`)) {
		t.Fatalf("missing registered store did not preserve durable fence evidence: %v: %s", err, lifecycleEvidence)
	}

	secondBinding := testBinding("late-pair")
	stdout, stderr, err = run(secondState, map[string]any{
		"binding": secondBinding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	if err == nil || !strings.Contains(stderr, "durable teardown fence") {
		t.Fatalf("origin fence permitted late pair: %v: %s%s", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(secondState, "receipts.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected late pair wrote a receipt: %v", err)
	}
}

func TestWorkerTeardownMalformedStoreReturnsBoundedJSONBlocker(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("malformed-private-identity")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	mutateTestReceipt(t, stateDir, func(receipt map[string]any) {
		receipt["binding"] = []any{"private-path-sentinel", stateDir}
	})

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{},
		"lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err == nil || !strings.Contains(stdout, `"blocker":"receipt_store_invalid_or_unavailable"`) {
		t.Fatalf("malformed store did not return JSON blocker: %v: %s%s", err, stdout, stderr)
	}
	if stderr != "" || strings.Contains(stdout, stateDir) || strings.Contains(stdout, "private-") || strings.Contains(stdout, "T-origin") || strings.Contains(stdout, "Traceback") {
		t.Fatalf("malformed store leaked private detail or traceback: %s%s", stdout, stderr)
	}
}

func TestExplicitLegacyRegistrationAndIndeterminateDetachPermitOnlyWorkerTeardown(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	legacyState := t.TempDir()
	legacyState, err = filepath.EvalSymlinks(legacyState)
	if err != nil {
		t.Fatal(err)
	}
	worktree := t.TempDir()
	artifact := filepath.Join(worktree, "private-artifact")
	if err := os.WriteFile(artifact, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	tmuxLog := filepath.Join(t.TempDir(), "tmux.log")
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
printf '%s\n' "$*" >> "$TMUX_LOG"
if [ "$1" = list-panes ]; then exit 0; fi
exit 99
`)
	writeExecutable(t, filepath.Join(binDir, "ps"), "#!/bin/sh\nexit 0\n")
	environment := append(replaceEnvironment(os.Environ(), "HOME", home),
		"PATH="+binDir+":"+os.Getenv("PATH"), "TMUX_LOG="+tmuxLog)
	run := func(isolated bool, input map[string]any, args ...string) (string, string, error) {
		payload, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		commandArgs := []string{helper, "--state-dir", legacyState}
		if isolated {
			commandArgs = append(commandArgs, "--isolated-test-state")
		}
		command := exec.Command("python3", append(commandArgs, args...)...)
		command.Env = append(environment, "AMUX_CLAUDE_DELEGATION_TESTING=1")
		command.Stdin = bytes.NewReader(payload)
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		runErr := command.Run()
		return stdout.String(), stderr.String(), runErr
	}

	binding := testBinding("synthetic-indeterminate")
	binding["workdir"] = worktree
	stdout, stderr, err := run(true, map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	if err != nil || !strings.Contains(stdout, `"outcome":"recorded"`) {
		t.Fatalf("create synthetic legacy receipt: %v: %s%s", err, stdout, stderr)
	}
	mutateTestReceipt(t, legacyState, func(receipt map[string]any) {
		receipt["events"] = append(receipt["events"].([]any), map[string]any{
			"event_id": "synthetic-launch", "kind": "launch_intent", "workflow": "read_only",
			"request_digest":    strings.Repeat("e", 64),
			"claude_session_id": "550e8400-e29b-41d4-a716-446655440000",
			"tmux_session":      "Synthetic", "tmux_window": "indeterminate",
			"packet_digest":         strings.Repeat("b", 64),
			"launch_policy_digest":  strings.Repeat("c", 64),
			"launch_command_digest": strings.Repeat("d", 64),
			"at":                    "2026-07-20T12:00:00Z",
		})
	})
	if err := os.Remove(filepath.Join(legacyState, "lifecycle.json")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err = run(false, map[string]any{}, "lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err != nil || !strings.Contains(stdout, `"outcome":"ready"`) || !strings.Contains(stdout, `"pairs":[]`) {
		t.Fatalf("unregistered synthetic store should be undiscoverable: %v: %s%s", err, stdout, stderr)
	}
	stdout, stderr, err = run(false, map[string]any{}, "lifecycle", "register-legacy-store",
		"--origin-thread", "T-origin", "--store-path", legacyState)
	if err != nil || !strings.Contains(stdout, `"outcome":"registered"`) {
		t.Fatalf("register exact synthetic legacy store: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, legacyState) || strings.Contains(stdout+stderr, "T-origin") || strings.Contains(stdout+stderr, "synthetic-indeterminate") {
		t.Fatalf("legacy registration leaked private identity: %s%s", stdout, stderr)
	}
	stdout, stderr, err = run(false, map[string]any{}, "lifecycle", "register-legacy-store",
		"--origin-thread", "T-origin", "--store-path", legacyState)
	if err != nil || !strings.Contains(stdout, `"outcome":"duplicate"`) {
		t.Fatalf("exact legacy registration replay was not idempotent: %v: %s%s", err, stdout, stderr)
	}

	stdout, stderr, err = run(false, map[string]any{}, "lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err == nil || !strings.Contains(stdout, `"blocker":"launch_indeterminate"`) {
		t.Fatalf("ordinary paired teardown did not remain fail-closed: %v: %s%s", err, stdout, stderr)
	}
	detach := map[string]any{
		"delegation_id": "synthetic-indeterminate",
		"event_id":      "detach-operation-1",
		"origin_thread": "T-origin",
		"compatibility": "pre_identity_launch_intent_v1",
		"authorization": map[string]any{
			"terminal_state":                   "merged",
			"report_sha256":                    strings.Repeat("c", 64),
			"coordinator_authorization_sha256": strings.Repeat("d", 64),
		},
	}
	stdout, stderr, err = run(false, detach, "lifecycle", "detach-indeterminate-worker")
	if err != nil || !strings.Contains(stdout, `"outcome":"detached"`) {
		t.Fatalf("detach synthetic indeterminate receipt: %v: %s%s", err, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, legacyState) || strings.Contains(stdout+stderr, "T-origin") || strings.Contains(stdout+stderr, "synthetic-indeterminate") {
		t.Fatalf("detach leaked private identity: %s%s", stdout, stderr)
	}
	stdout, stderr, err = run(false, detach, "lifecycle", "detach-indeterminate-worker")
	if err != nil || !strings.Contains(stdout, `"outcome":"duplicate"`) {
		t.Fatalf("exact detach replay was not idempotent: %v: %s%s", err, stdout, stderr)
	}

	stdout, stderr, err = run(false, map[string]any{}, "lifecycle", "worker-teardown", "--origin-thread", "T-origin")
	if err != nil || !strings.Contains(stdout, `"outcome":"ready"`) || !strings.Contains(stdout, `"state":"worker_detached"`) {
		t.Fatalf("paired teardown did not permit detached Amp worker: %v: %s%s", err, stdout, stderr)
	}
	stored, err := os.ReadFile(filepath.Join(legacyState, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"kind":"launch_completed"`, `"kind":"session_acquired"`, `"kind":"valid_report"`, `"kind":"park_intent"`, `"kind":"verified_parked"`, `"cleanup_eligible_at"`} {
		if bytes.Contains(stored, []byte(forbidden)) {
			t.Fatalf("detach created prohibited transition %s: %s", forbidden, stored)
		}
	}
	if !bytes.Contains(stored, []byte(`"kind":"worker_detached"`)) || !bytes.Contains(stored, []byte(`"origin_thread":"T-origin"`)) {
		t.Fatalf("detach did not append evidence while retaining immutable origin: %s", stored)
	}
	if content, err := os.ReadFile(artifact); err != nil || string(content) != "preserve" {
		t.Fatalf("detach removed private artifact or worktree: %v: %q", err, content)
	}
	log, err := os.ReadFile(tmuxLog)
	if err != nil || strings.Contains(string(log), "kill-pane") || strings.Contains(string(log), "send-keys") {
		t.Fatalf("detach issued a prohibited tmux mutation: %v: %s", err, log)
	}
}

func TestPreIdentityLaunchIntentDetachRequiresExplicitSyntheticCompatibilityProof(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import copy, importlib.util, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

legacy_intent = {
    "event_id":"synthetic-launch", "kind":"launch_intent", "workflow":"read_only",
    "request_digest":"8" * 64,
    "claude_session_id":"550e8400-e29b-41d4-a716-446655440000",
    "tmux_session":"Synthetic", "tmux_window":"indeterminate",
    "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64,
    "launch_command_digest":"5" * 64, "at":"2026-07-20T12:00:00Z",
}

def fixture(intent=legacy_intent, name="synthetic-pre-identity"):
    state = pathlib.Path(tempfile.mkdtemp()).resolve(); state.chmod(0o700)
    store = module.ReceiptStore(state, state)
    binding = {
        "protocol_version":1, "delegation_id":name, "nonce":"1" * 64,
        "task_id":"task", "question_message_id":"question", "origin_thread":"T-synthetic",
        "repository":"repository", "base":"2" * 40, "workdir":str(state),
        "producer_role":"thinker", "authority":"read_only", "task_reference":"fixture",
        "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64,
        "launch_command_digest":"5" * 64,
    }
    assert store.create({"binding":binding, "routing":{"target":"machine_local_inbox"}}) == "recorded"
    data = store.load_store(); data["receipts"][0]["events"].append(copy.deepcopy(intent)); store.commit(data)
    request = {
        "delegation_id":name, "event_id":"detach-pre-identity", "origin_thread":"T-synthetic",
        "compatibility":"pre_identity_launch_intent_v1",
        "authorization":{"terminal_state":"merged", "report_sha256":"6" * 64,
                         "coordinator_authorization_sha256":"7" * 64},
    }
    return store, request

# The compatibility selector is mandatory and is rejected for modern or mixed evidence.
store, request = fixture()
without_compatibility = dict(request); without_compatibility.pop("compatibility")
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(without_compatibility)
except module.HelperError:
    pass
else:
    raise AssertionError("pre-identity launch intent detached without explicit compatibility")
assert store.path.read_bytes() == before

modern = dict(legacy_intent, expected_argv_digest="a" * 64,
              expected_launcher_identity="file:1:2",
              expected_executable_object_identity="object:1:2:3:" + "b" * 64)
store, request = fixture(modern, "synthetic-modern")
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(request)
except module.HelperError:
    pass
else:
    raise AssertionError("modern launch intent accepted legacy compatibility")
assert store.path.read_bytes() == before

mixed = dict(legacy_intent, expected_argv_digest="a" * 64)
store, request = fixture(mixed, "synthetic-mixed")
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(request)
except module.HelperError:
    pass
else:
    raise AssertionError("mixed modern and pre-identity evidence detached")
assert store.path.read_bytes() == before

substituted = dict(legacy_intent, packet_digest="9" * 64)
store, request = fixture(substituted, "synthetic-substituted")
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(request)
except module.HelperError:
    pass
else:
    raise AssertionError("legacy intent substituted immutable evidence")
assert store.path.read_bytes() == before

malformed_timestamp = dict(legacy_intent, at="not-a-timestamp")
store, request = fixture(malformed_timestamp, "synthetic-malformed-timestamp")
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(request)
except module.HelperError:
    pass
else:
    raise AssertionError("legacy intent with malformed timestamp detached")
assert store.path.read_bytes() == before

# Legacy absence never infers argv or executable identity.
def inspect(tmux, ps="", identities=None):
    module.read_bounded_command = lambda command, limit: tmux if command[0] == "tmux" else ps
    identities = identities or {}
    module.exact_process_identity = lambda pid: identities[pid]
    return module.inspect_pre_identity_launch_absence(legacy_intent)

assert inspect("Synthetic\tindeterminate\t%41") == "matching_legacy_target"
assert inspect("", "101", {101:("unrelated", "stable", ["/synthetic/tool", "--session-id", legacy_intent["claude_session_id"]], "digest")}) == "matching_legacy_session"
assert inspect("malformed") == "tmux_inspection_ambiguous"
assert inspect("Synthetic\tother\t%invalid") == "tmux_inspection_ambiguous"
assert inspect("\tother\t%41") == "tmux_inspection_ambiguous"
assert inspect("Synthetic\tother\t%41\nSynthetic\tother\t%41") == "tmux_inspection_ambiguous"
assert inspect("Synthetic\tfirst\t%41\nSynthetic\tsecond\t%41") == "tmux_inspection_ambiguous"
assert inspect("\n".join(f"S\tw{i}\t%{i}" for i in range(module.MAX_ABSENCE_PANES + 1))) == "tmux_inspection_ambiguous"
module.read_bounded_command = lambda command, limit: "" if command[0] == "tmux" else "not-a-pid"
assert module.inspect_pre_identity_launch_absence(legacy_intent) == "process_inspection_ambiguous"
module.read_bounded_command = lambda command, limit: (
    "" if command[0] == "tmux" else "\n".join(str(i + 1) for i in range(module.MAX_ABSENCE_PROCESSES + 1))
)
assert module.inspect_pre_identity_launch_absence(legacy_intent) == "process_inspection_ambiguous"
module.read_bounded_command = lambda command, limit: (
    "" if command[0] == "tmux" else (_ for _ in ()).throw(module.HelperError("synthetic limit"))
)
assert module.inspect_pre_identity_launch_absence(legacy_intent) == "process_inspection_unavailable"
module.read_bounded_command = lambda command, limit: (_ for _ in ()).throw(module.HelperError("synthetic limit"))
assert module.inspect_pre_identity_launch_absence(legacy_intent) == "tmux_inspection_unavailable"
module.read_bounded_command = lambda command, limit: "" if command[0] == "tmux" else "101"
module.exact_process_identity = lambda pid: (_ for _ in ()).throw(module.HelperError("synthetic inaccessible"))
assert module.inspect_pre_identity_launch_absence(legacy_intent) == "process_inspection_unavailable"

for code in ["matching_legacy_target", "matching_legacy_session", "tmux_inspection_ambiguous",
             "tmux_inspection_unavailable", "process_inspection_ambiguous", "process_inspection_unavailable"]:
    store, request = fixture(name="blocked-" + code.replace("_", "-"))
    before = store.path.read_bytes()
    module.inspect_pre_identity_launch_absence = lambda intent, code=code: code
    result = store.detach_indeterminate_worker(request)
    assert result["outcome"] == "blocked" and result["blocker"] == code, result
    assert store.path.read_bytes() == before, code
    assert store.lifecycle.load()["teardown_fences"]["T-synthetic"], code

# Exact synthetic absence appends only the bound terminal detach proof and seals transport.
store, request = fixture()
prior = copy.deepcopy(store.load_store()["receipts"][0])
module.inspect_pre_identity_launch_absence = lambda intent: "legacy_target_and_session_absent"
result = store.detach_indeterminate_worker(request)
assert result["outcome"] == "detached" and result["compatibility"] == "pre_identity_launch_intent_v1", result
receipt = store.load_store()["receipts"][0]
assert receipt["events"][:-1] == prior["events"]
assert receipt["binding"] == prior["binding"] and receipt["state"] == prior["state"]
event = receipt["events"][-1]
assert event["kind"] == "worker_detached" and event["absence_code"] == "legacy_target_and_session_absent"
assert event["compatibility"] == "pre_identity_launch_intent_v1"
for forbidden in ["launch_completed", "session_acquired", "valid_report", "park_intent", "verified_parked"]:
    assert not any(candidate.get("kind") == forbidden for candidate in receipt["events"]), forbidden
assert "cleanup_eligible_at" not in receipt and module.valid_worker_detach_chain(receipt)
assert store.worker_teardown("T-synthetic", True)["pairs"] == [{
    "pair_sha256":module.hashlib.sha256(b"synthetic-pre-identity").hexdigest(),
    "state":"worker_detached", "action":"none",
}]
try:
    module.require_launch_transport_allowed(store, "synthetic-pre-identity")
except module.HelperError:
    pass
else:
    raise AssertionError("direct launch transport remained authorized after legacy detach")
assert store.detach_indeterminate_worker(request)["outcome"] == "duplicate"

def assert_invalid_detach_proof(mutate, name):
    store, request = fixture(name=name)
    module.inspect_pre_identity_launch_absence = lambda intent: "legacy_target_and_session_absent"
    assert store.detach_indeterminate_worker(request)["outcome"] == "detached"
    data = store.load_store(); receipt = data["receipts"][0]; mutate(receipt); store.commit(data)
    assert not module.valid_worker_detach_chain(receipt), name
    before = store.path.read_bytes()
    try:
        store.detach_indeterminate_worker(request)
    except module.HelperError:
        pass
    else:
        raise AssertionError("malformed detach proof accepted as exact replay: " + name)
    assert store.path.read_bytes() == before, name
    teardown = store.worker_teardown("T-synthetic", True)
    assert teardown["outcome"] == "blocked", (name, teardown)

assert_invalid_detach_proof(
    lambda receipt: (receipt["events"][-1].pop("at"), receipt["worker_detached"].pop("at")),
    "proof-missing-timestamp",
)
assert_invalid_detach_proof(
    lambda receipt: receipt["events"][-1].update({"unknown":"synthetic"}),
    "proof-extra-event-field",
)
assert_invalid_detach_proof(
    lambda receipt: receipt["worker_detached"].update({"unknown":"synthetic"}),
    "proof-extra-materialized-field",
)
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("pre-identity detach compatibility fixture: %v: %s", err, output)
	}
}

func TestLegacyRegistrationRejectsUnprovenOwnerPrivateEvidence(t *testing.T) {
	t.Parallel()
	newStore := func(t *testing.T) string {
		t.Helper()
		stateDir := t.TempDir()
		resolved, err := filepath.EvalSymlinks(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		assertHelperOutcome(t, resolved, "recorded", map[string]any{
			"binding": testBinding("legacy-registration-fixture"),
			"routing": map[string]any{"target": "machine_local_inbox"},
		}, "receipt", "create")
		return resolved
	}
	assertBlocked := func(t *testing.T, stateDir, path, origin string) {
		t.Helper()
		stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "lifecycle", "register-legacy-store",
			"--origin-thread", origin, "--store-path", path)
		if err == nil || !strings.Contains(stdout, `"blocker":"registration_evidence_invalid_or_unavailable"`) || stderr != "" {
			t.Fatalf("unsafe registration was not privacy-safe blocked JSON: %v: %s%s", err, stdout, stderr)
		}
		if strings.Contains(stdout, path) || strings.Contains(stdout, origin) || strings.Contains(stdout, "legacy-registration-fixture") {
			t.Fatalf("registration blocker leaked private evidence: %s", stdout)
		}
	}

	t.Run("relative path", func(t *testing.T) {
		stateDir := newStore(t)
		assertBlocked(t, stateDir, "relative/store", "T-origin")
	})
	t.Run("non-canonical dot path", func(t *testing.T) {
		stateDir := newStore(t)
		assertBlocked(t, stateDir, stateDir+string(os.PathSeparator)+".", "T-origin")
	})
	t.Run("symlink path", func(t *testing.T) {
		stateDir := newStore(t)
		alias := filepath.Join(t.TempDir(), "store-alias")
		if err := os.Symlink(stateDir, alias); err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, stateDir, alias, "T-origin")
	})
	t.Run("directory mode", func(t *testing.T) {
		stateDir := newStore(t)
		if err := os.Chmod(stateDir, 0o750); err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, stateDir, stateDir, "T-origin")
	})
	t.Run("receipt mode", func(t *testing.T) {
		stateDir := newStore(t)
		if err := os.Chmod(filepath.Join(stateDir, "receipts.json"), 0o640); err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, stateDir, stateDir, "T-origin")
	})
	t.Run("receipt symlink", func(t *testing.T) {
		stateDir := newStore(t)
		receiptPath := filepath.Join(stateDir, "receipts.json")
		target := filepath.Join(t.TempDir(), "receipts.json")
		contents, err := os.ReadFile(receiptPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, contents, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(receiptPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, receiptPath); err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, stateDir, stateDir, "T-origin")
	})
	t.Run("missing receipt", func(t *testing.T) {
		stateDir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, stateDir, stateDir, "T-origin")
	})
	t.Run("malformed schema", func(t *testing.T) {
		stateDir := newStore(t)
		if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), []byte(`{"schema_version":99,"receipts":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		assertBlocked(t, stateDir, stateDir, "T-origin")
	})
	t.Run("origin mismatch", func(t *testing.T) {
		stateDir := newStore(t)
		assertBlocked(t, stateDir, stateDir, "T-other-origin")
	})
	t.Run("owner mismatch", func(t *testing.T) {
		stateDir := newStore(t)
		helper, err := filepath.Abs("claude_delegation.py")
		if err != nil {
			t.Fatal(err)
		}
		script := `import importlib.util, pathlib, sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
actual = module.os.geteuid()
module.os.geteuid = lambda: actual + 1
try:
    with module.locked_owner_private_store(pathlib.Path(sys.argv[2])):
        pass
except module.HelperError:
    print("blocked")
else:
    raise AssertionError("owner mismatch accepted")
`
		output, err := exec.Command("python3", "-c", script, helper, stateDir).CombinedOutput()
		if err != nil || string(output) != "blocked\n" {
			t.Fatalf("owner mismatch descriptor validation: %v: %s", err, output)
		}
	})
}

func TestIndeterminateDetachFailsClosedOnAuthorizationIdentityAndConflictingReplay(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import hashlib, importlib.util, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

intent = {
    "event_id":"launch", "kind":"launch_intent", "workflow":"read_only",
    "claude_session_id":"550e8400-e29b-41d4-a716-446655440000",
    "tmux_session":"Synthetic", "tmux_window":"indeterminate",
    "expected_argv_digest":"a" * 64, "expected_launcher_identity":"file:1:2",
    "expected_executable_object_identity":"object:1:2:3:" + "b" * 64,
}
module.read_bounded_command = lambda command, limit: "Synthetic\tindeterminate\t%20"
module.inspect_claude_identity = lambda *args, **kwargs: {"live": True}
assert module.inspect_indeterminate_launch_absence(intent) == "matching_live_process"
module.inspect_claude_identity = lambda *args, **kwargs: (_ for _ in ()).throw(module.HelperError("private mismatch"))
assert module.inspect_indeterminate_launch_absence(intent) == "launch_identity_ambiguous_or_mismatched"
module.read_bounded_command = lambda command, limit: "malformed-private-output"
assert module.inspect_indeterminate_launch_absence(intent) == "tmux_inspection_ambiguous"
module.read_bounded_command = lambda command, limit: (_ for _ in ()).throw(module.HelperError("private unavailable"))
assert module.inspect_indeterminate_launch_absence(intent) == "tmux_inspection_unavailable"
module.read_bounded_command = lambda command, limit: ""
assert module.inspect_indeterminate_launch_absence(intent) == "exact_launch_target_absent"
renamed = dict(intent)
renamed["expected_argv_digest"] = module.normalized_argv_digest(["--session-id", intent["claude_session_id"]])
module.read_bounded_command = lambda command, limit: (
    "Synthetic\trenamed\t%20" if command[0] == "tmux" else "4242"
)
module.exact_process_identity = lambda pid: (
    "claude", "stable", ["/synthetic/claude", "--session-id", intent["claude_session_id"]], "digest"
)
module.process_executable_path = lambda pid: pathlib.Path("/synthetic/claude")
module.normalized_claude_arguments = lambda system, name, args, executable: ("direct", args[1:])
renamed_result = module.inspect_indeterminate_launch_absence(renamed)
assert renamed_result == "matching_live_process", renamed_result

def fixture():
    state = pathlib.Path(tempfile.mkdtemp()).resolve()
    state.chmod(0o700)
    store = module.ReceiptStore(state, state)
    binding = {
        "protocol_version":1, "delegation_id":"synthetic-detach", "nonce":"1" * 64,
        "task_id":"task", "question_message_id":"question", "origin_thread":"T-synthetic",
        "repository":"repository", "base":"2" * 40, "workdir":str(state),
        "producer_role":"thinker", "authority":"read_only", "task_reference":"fixture",
        "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64,
        "launch_command_digest":"5" * 64,
    }
    assert store.create({"binding":binding, "routing":{"target":"machine_local_inbox"}}) == "recorded"
    data = store.load_store()
    data["receipts"][0]["events"].append(dict(intent))
    store.commit(data)
    request = {
        "delegation_id":"synthetic-detach", "event_id":"detach-stable", "origin_thread":"T-synthetic",
        "authorization":{"terminal_state":"merged", "report_sha256":"6" * 64,
                         "coordinator_authorization_sha256":"7" * 64},
    }
    return store, request

for code in ["matching_live_process", "launch_identity_ambiguous_or_mismatched", "tmux_inspection_ambiguous", "tmux_inspection_unavailable"]:
    store, request = fixture()
    before = store.path.read_bytes()
    module.inspect_indeterminate_launch_absence = lambda launch, code=code: code
    result = store.detach_indeterminate_worker(request)
    assert result["outcome"] == "blocked" and result["blocker"] == code, result
    assert store.path.read_bytes() == before, code
    assert store.lifecycle.load()["teardown_fences"]["T-synthetic"], code

store, request = fixture()
invalid = dict(request)
invalid["authorization"] = dict(request["authorization"], terminal_state="ready")
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(invalid)
except module.HelperError:
    pass
else:
    raise AssertionError("non-terminal authorization accepted")
assert store.path.read_bytes() == before

store, request = fixture()
data = store.load_store()
data["receipts"][0]["state"] = "valid_report"
data["receipts"][0]["report_message_id"] = "synthetic-report"
data["receipts"][0]["events"].append({"event_id":"synthetic-report", "kind":"valid_report"})
store.commit(data)
before = store.path.read_bytes()
try:
    store.detach_indeterminate_worker(request)
except module.HelperError:
    pass
else:
    raise AssertionError("launch-indeterminate receipt with report state detached")
assert store.path.read_bytes() == before

store, request = fixture()
module.inspect_indeterminate_launch_absence = lambda launch: "exact_launch_target_absent"
assert store.detach_indeterminate_worker(request)["outcome"] == "detached"
detached = store.path.read_bytes()
conflict = dict(request)
conflict["authorization"] = dict(request["authorization"], report_sha256="8" * 64)
try:
    store.detach_indeterminate_worker(conflict)
except module.HelperError:
    pass
else:
    raise AssertionError("conflicting event replay accepted")
assert store.path.read_bytes() == detached
assert store.detach_indeterminate_worker(request)["outcome"] == "duplicate"
before = store.path.read_bytes()
try:
    store.route({"delegation_id":"synthetic-detach", "event_id":"post-detach-route",
                 "routing":{"target":"machine_local_inbox", "recovery":"changed"}})
except module.HelperError as error:
    assert "sealed" in str(error), error
else:
    raise AssertionError("detached receipt accepted a routing mutation")
assert store.path.read_bytes() == before
assert module.valid_worker_detach_chain(store.load_store()["receipts"][0])

oversized = pathlib.Path(tempfile.mkstemp()[1])
oversized.write_bytes(b"x" * (module.MAX_STORE_BYTES + 1))
descriptor = module.os.open(oversized, module.os.O_RDONLY)
try:
    try:
        module.read_stable_descriptor(descriptor, module.MAX_STORE_BYTES, "synthetic oversized store")
    except module.HelperError:
        pass
    else:
        raise AssertionError("oversized descriptor evidence accepted")
finally:
    module.os.close(descriptor)

legacy = pathlib.Path(tempfile.mkdtemp()).resolve()
legacy.chmod(0o700)
isolated = module.ReceiptStore(legacy, legacy)
binding = fixture()[0].load_store()["receipts"][0]["binding"]
binding = dict(binding, delegation_id="legacy-object", workdir=str(legacy))
assert isolated.create({"binding":binding, "routing":{"target":"machine_local_inbox"}}) == "recorded"
(legacy / "lifecycle.json").unlink()
canonical = pathlib.Path(tempfile.mkdtemp()).resolve()
canonical.chmod(0o700)
registrar = module.ReceiptStore(legacy, canonical)
assert registrar.register_legacy_store("T-synthetic", legacy)["outcome"] == "registered"
moved = legacy.with_name(legacy.name + "-moved")
legacy.rename(moved)
legacy.mkdir(mode=0o700)
for name in ("experimental.lock", "receipts.json"):
    (legacy / name).write_bytes((moved / name).read_bytes())
    (legacy / name).chmod(0o600)
result = registrar.worker_teardown("T-synthetic", True)
assert result["outcome"] == "blocked"
assert result["blockers"] == [{"blocker":"receipt_store_invalid_or_unavailable"}]
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("indeterminate detach fail-closed fixture: %v: %s", err, output)
	}
}

func TestExactLivePairRetirementsAreDurableAndRecoverable(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import copy, hashlib, importlib.util, json, os, pathlib, sys, tempfile, time
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
real_confirm_retirement_target_absent = module.confirm_retirement_target_absent
real_inspect_live_indeterminate_target = module.inspect_live_indeterminate_target
real_stop_exact_retirement_target = module.stop_exact_retirement_target

identity = {"session":"Synthetic", "session_id":"$17", "window":"live-indeterminate", "window_id":"@17", "pane_id":"%23",
    "pane_pid":4242, "claude_session_id":"550e8400-e29b-41d4-a716-446655440000",
    "workdir":"", "current_command":"claude", "process_name":"claude", "process_identity":"start:17",
    "process_start_identity":"process-start:start:17",
    "process_executable_identity":"file:7:11", "process_executable_object_identity":"object:7:11:13:" + "b" * 64,
    "normalized_argv_digest":"a" * 64, "process_command_digest":"c" * 64,
    "launch_command_digest":"5" * 64, "expected_launcher_identity":"file:7:11",
    "expected_executable_object_identity":"object:7:11:13:" + "b" * 64,
    "expected_launcher_argv0_digest":"d" * 64}
intent = {"event_id":"launch", "kind":"launch_intent", "workflow":"read_only",
    "claude_session_id":identity["claude_session_id"], "tmux_session":identity["session"],
    "tmux_window":identity["window"], "expected_argv_digest":identity["normalized_argv_digest"],
    "expected_launcher_identity":"file:7:11", "expected_executable_object_identity":"object:7:11:13:" + "b" * 64,
    "expected_launcher_argv0_digest":"d" * 64, "request_digest":"8" * 64,
    "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64, "launch_command_digest":"5" * 64,
    "packet_identity":"file:synthetic-packet", "workdir_identity":"directory:synthetic-workdir",
    "at":"2026-07-20T12:00:00Z"}

def fixture(name="synthetic-live"):
    state = pathlib.Path(tempfile.mkdtemp()).resolve(); state.chmod(0o700)
    store = module.ReceiptStore(state, state)
    binding = {"protocol_version":1, "delegation_id":name, "nonce":"1" * 64, "task_id":"task",
        "question_message_id":"question", "origin_thread":"T-synthetic", "repository":"repository",
        "base":"2" * 40, "workdir":str(state), "producer_role":"thinker", "authority":"read_only",
        "task_reference":"fixture", "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64,
        "launch_command_digest":"5" * 64}
    assert store.create({"binding":binding, "routing":{"target":"machine_local_inbox"}}) == "recorded"
    data = store.load_store(); receipt = data["receipts"][0]; receipt["events"].append(dict(intent))
    report = {"protocol_version":1, "delegation_id":name, "nonce":"1" * 64, "message_id":"report",
        "in_reply_to":"question", "kind":"thinker_report", "task_id":"task", "origin_thread":"T-synthetic",
        "repository":"repository", "base":"2" * 40, "workdir":str(state), "producer_role":"thinker",
        "authority":"read_only", "launch_policy_digest":"4" * 64, "created_at":"2026-07-20T12:00:00Z",
        "report":{"accepted_role":True, "accepted_exclusions":True, "status":"complete", "verdict":"synthetic",
          "rationale":"synthetic", "evidence":[], "assumptions":[], "unsupported_claims":[], "blockers":[],
          "verification":[], "changed_artifacts":[], "references":[]}}
    receipt["state"] = "valid_report"; receipt["report_message_id"] = "report"
    receipt["events"].append({"event_id":"report", "kind":"valid_report", "message":report,
                              "at":"2026-07-20T12:00:01Z"}); store.commit(data)
    bound = dict(identity, workdir=str(state))
    request = {"delegation_id":name, "event_id":"retire-stable", "origin_thread":"T-synthetic",
        "authorization":{"terminal_state":"merged", "report_sha256":module.canonical_sha256(report),
                         "coordinator_authorization_sha256":"7" * 64}}
    return store, bound, request

# Reproduce the exact launch-completed, acquired, no-report state before exercising its separate recovery.
def acquired_fixture(name="synthetic-acquired-no-report", model=None):
    acquired, bound, request = fixture(name)
    data = acquired.load_store(); receipt = data["receipts"][0]
    if model is not None:
        receipt["binding"]["model"] = model
        receipt["events"][1]["model"] = model
    acquired_identity = {key:value for key,value in bound.items() if key not in {
        "session_id", "process_start_identity", "expected_launcher_identity",
        "expected_executable_object_identity", "expected_launcher_argv0_digest"}}
    receipt["state"] = "created"; receipt["report_message_id"] = ""
    receipt["events"] = receipt["events"][:2]
    receipt["events"].extend([
        {"event_id":module.internal_event_id("launch-result", "launch"), "kind":"launch_completed",
         "operation_event_id":"launch",
         "identity":copy.deepcopy(acquired_identity), "at":"2026-07-20T12:00:01Z"},
        {"event_id":"acquired", "kind":"session_acquired", "identity":copy.deepcopy(acquired_identity),
         "at":"2026-07-20T12:00:02Z"},
    ])
    receipt["session_identity"] = copy.deepcopy(acquired_identity)
    receipt["updated_at"] = "2026-07-20T12:00:02Z"; acquired.commit(data)
    request["authorization"]["report_sha256"] = "6" * 64
    return acquired, bound, request

# A successful exec ends launch-gate ownership before either exact live retirement route runs.
def assert_exec_releases_launch_gate(store, current_identity, request, retire):
    completed = store.load_store()
    preexec = copy.deepcopy(completed)
    receipt = preexec["receipts"][0]
    receipt["state"] = "created"; receipt["report_message_id"] = ""
    receipt["events"] = receipt["events"][:2]
    for field in ("session_identity", "acquired_retirement_intent", "acquired_pair_retired",
                  "retirement_intent", "pair_retired"):
        receipt.pop(field, None)
    store.commit(preexec)
    marker = store.state_dir / "transport-exec-complete"
    child = os.fork()
    if child == 0:
        try:
            with store.launch_gate(receipt["binding"]["delegation_id"]):
                module.execute_authorized_launch_transport(
                    store, receipt["binding"]["delegation_id"], sys.executable,
                    [sys.executable, "-c",
                     "import pathlib,sys,time; pathlib.Path(sys.argv[1]).write_text('exec'); time.sleep(30)",
                     str(marker)], dict(os.environ))
        finally: os._exit(1)
    reaped = False
    try:
        deadline = time.monotonic() + 3
        while not marker.exists() and time.monotonic() < deadline: time.sleep(0.01)
        assert marker.exists(), "exec target did not start"
        store.commit(completed)
        stops = []
        module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
        def stop(bound, *args):
            nonlocal reaped
            stops.append(copy.deepcopy(bound)); os.kill(child, 15); os.waitpid(child, 0); reaped = True
        module.stop_exact_retirement_target = stop
        module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
        outcome = retire(request)
        assert outcome["outcome"] == "retired", outcome
        assert stops == [current_identity]
    finally:
        if not reaped:
            try: os.kill(child, 15)
            except ProcessLookupError: pass
            os.waitpid(child, 0)

# A v0.2.25 target inherited the gate across successful exec. Exact post-exec durable evidence must
# reclassify that busy gate without weakening the separate pre-exec launch-indeterminate blocker.
def assert_legacy_exec_holder_does_not_block_retirement(store, current_identity, request, retire):
    marker = store.state_dir / "legacy-transport-exec-complete"
    gate_path = store.state_dir / "launch-gates" / hashlib.sha256(
        request["delegation_id"].encode()).hexdigest()
    gate_path.parent.mkdir(mode=0o700, exist_ok=True)
    child = os.fork()
    if child == 0:
        try:
            descriptor = os.open(gate_path, os.O_CREAT | os.O_RDWR, 0o600)
            module.fcntl.flock(descriptor, module.fcntl.LOCK_EX)
            os.set_inheritable(descriptor, True)
            os.execve(sys.executable, [sys.executable, "-c",
                "import pathlib,sys,time; pathlib.Path(sys.argv[1]).write_text('exec'); time.sleep(30)",
                str(marker)], dict(os.environ))
        finally: os._exit(1)
    reaped = False
    try:
        deadline = time.monotonic() + 3
        while not marker.exists() and time.monotonic() < deadline: time.sleep(0.01)
        assert marker.exists(), "legacy exec target did not start"
        stops = []
        module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
        def stop(bound, *args):
            nonlocal reaped
            stops.append(copy.deepcopy(bound)); os.kill(child, 15); os.waitpid(child, 0); reaped = True
        module.stop_exact_retirement_target = stop
        module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
        outcome = retire(request)
        assert outcome["outcome"] == "retired", outcome
        assert stops == [current_identity]
    finally:
        if not reaped:
            try: os.kill(child, 15)
            except ProcessLookupError: pass
            os.waitpid(child, 0)

launched, current_identity, launched_request = acquired_fixture("acquired-after-transport-exec")
assert_exec_releases_launch_gate(
    launched, current_identity, launched_request, launched.retire_live_acquired_no_report_pair)
launched, current_identity, launched_request = fixture("report-after-transport-exec")
assert_exec_releases_launch_gate(
    launched, current_identity, launched_request, launched.retire_live_indeterminate_pair)

legacy, current_identity, legacy_request = acquired_fixture("acquired-after-legacy-transport-exec")
assert_legacy_exec_holder_does_not_block_retirement(
    legacy, current_identity, legacy_request, legacy.retire_live_acquired_no_report_pair)
legacy, current_identity, legacy_request = fixture("report-after-legacy-transport-exec")
assert_legacy_exec_holder_does_not_block_retirement(
    legacy, current_identity, legacy_request, legacy.retire_live_indeterminate_pair)

# A genuinely pre-exec holder may coexist with report candidate evidence, but cannot pass final
# transport authorization or cause retirement to mutate without one exact inspected live target.
preexec, _, preexec_request = fixture("report-with-preexec-gate-holder")
preexec_before = preexec.path.read_bytes()
ready_read, ready_write = os.pipe(); release_read, release_write = os.pipe()
child = os.fork()
if child == 0:
    try:
        with preexec.launch_gate(preexec_request["delegation_id"]):
            os.write(ready_write, b"1"); os.read(release_read, 1)
    finally: os._exit(0)
os.close(ready_write); os.close(release_read); assert os.read(ready_read, 1) == b"1"
try:
    inspections = []; stops = []
    module.inspect_live_indeterminate_target = lambda *args: (
        inspections.append(args), (_ for _ in ()).throw(module.HelperError("no exact live target")))[1]
    module.stop_exact_retirement_target = lambda *args: stops.append(args)
    try: preexec.retire_live_indeterminate_pair(preexec_request)
    except module.HelperError: pass
    else: raise AssertionError("pre-exec holder without exact live target entered retirement")
    assert len(inspections) == 1 and not stops and preexec.path.read_bytes() == preexec_before
    assert "T-synthetic" in preexec.lifecycle.load()["teardown_fences"]
finally:
    os.write(release_write, b"1"); os.close(release_write)
    _, status = os.waitpid(child, 0)
assert os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0

# Body exceptions release an acquired retirement gate instead of being mistaken for gate contention.
unwound, _, _ = acquired_fixture("retirement-exclusion-unwind")
try:
    with unwound.live_retirement_exclusion("retirement-exclusion-unwind"):
        raise RuntimeError("synthetic body failure")
except RuntimeError: pass
else: raise AssertionError("retirement exclusion swallowed a body failure")
with unwound.launch_gate("retirement-exclusion-unwind", timeout_seconds=0.2): pass

acquired, current_identity, acquired_request = acquired_fixture()
original = copy.deepcopy(acquired.load_store()["receipts"][0]); stops = []
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(copy.deepcopy(bound))
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
result = acquired.retire_live_acquired_no_report_pair(acquired_request)
assert result["outcome"] == "retired" and result["fence"] == "retained", result
receipt = acquired.load_store()["receipts"][0]
assert stops == [current_identity]
assert receipt["events"][:4] == original["events"] and receipt["binding"] == original["binding"]
assert receipt["routing"] == original["routing"] and receipt["session_identity"] == original["session_identity"]
assert [event["kind"] for event in receipt["events"]][-2:] == [
    "acquired_retirement_intent", "acquired_pair_retired"]
assert receipt["state"] == "created" and receipt["report_message_id"] == ""
assert "cleanup_eligible_at" not in receipt
assert module.valid_acquired_no_report_pair_retirement_chain(receipt)
for forbidden in ("valid_report", "input_request", "delivered", "acknowledged", "park_intent",
                  "verified_parked", "worker_detached", "pair_retired"):
    assert forbidden not in [event["kind"] for event in receipt["events"]]
assert acquired.worker_teardown("T-synthetic", True)["pairs"] == [{
    "pair_sha256":hashlib.sha256(b"synthetic-acquired-no-report").hexdigest(),
    "state":"acquired_pair_retired", "action":"none"}]
assert acquired.retire_live_acquired_no_report_pair(acquired_request)["outcome"] == "duplicate"
assert stops == [current_identity]
sealed = acquired.load_store()
try:
    acquired.route({"delegation_id":"synthetic-acquired-no-report", "event_id":"sealed-route",
                    "routing":{"target":"machine_local_inbox"}})
except module.HelperError: pass
else: raise AssertionError("acquired no-report retirement did not seal later mutation")
assert acquired.load_store() == sealed

# Exact Opus selection remains immutable through pre-semantic acquired-session retirement.
opus, current_identity, opus_request = acquired_fixture(
    "synthetic-acquired-opus-entitlement", "claude-opus-4-8")
original = copy.deepcopy(opus.load_store()["receipts"][0]); stops = []
assert module.receipt_launch_intent(original)["model"] == "claude-opus-4-8"
acknowledged = copy.deepcopy(original); acknowledged["report_message_id"] = "report"
acknowledged["state"] = "acknowledged"
acknowledged["events"].extend([
    {"event_id":"report", "kind":"valid_report"},
    {"event_id":"deliver", "kind":"delivered", "message_id":"report"},
    {"event_id":"ack", "kind":"acknowledged", "message_id":"report"},
])
assert module.valid_worker_lifecycle_chain(acknowledged, False)
parked = copy.deepcopy(acknowledged); parked["state"] = "verified_parked"
parked["parked_at"] = "2026-07-20T12:00:03Z"
parked["cleanup_eligible_at"] = "2026-08-19T12:00:03Z"
parked["events"].extend([
    {"event_id":"park", "kind":"park_intent", "identity":copy.deepcopy(parked["session_identity"])},
    {"event_id":"parked", "kind":"verified_parked", "operation_event_id":"park",
     "at":parked["parked_at"]},
])
assert module.valid_worker_lifecycle_chain(parked, True)
for name, mutate in [
    ("omitted", lambda receipt: receipt["events"][1].pop("model")),
    ("changed", lambda receipt: receipt["events"][1].update(model="claude-fable-5")),
]:
    drifted = copy.deepcopy(parked); mutate(drifted)
    try: module.receipt_launch_intent(drifted)
    except module.HelperError: pass
    else: raise AssertionError("model drift entered acquisition or parking: " + name)
    assert not module.valid_worker_lifecycle_chain(drifted, True), name
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(copy.deepcopy(bound))
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
assert opus.retire_live_acquired_no_report_pair(opus_request)["outcome"] == "retired"
receipt = opus.load_store()["receipts"][0]
assert receipt["binding"]["model"] == "claude-opus-4-8"
assert receipt["events"][1]["model"] == "claude-opus-4-8"
assert receipt["events"][:4] == original["events"] and stops == [current_identity]

# A changed model in recovery evidence fails before inspection, stop, or durable-byte mutation.
changed, current_identity, changed_request = acquired_fixture(
    "synthetic-acquired-opus-changed", "claude-opus-4-8")
data = changed.load_store(); data["receipts"][0]["events"][1]["model"] = "claude-fable-5"
changed.commit(data); before = (changed.state_dir / "receipts.json").read_bytes()
inspections = []; stops = []
module.inspect_live_indeterminate_target = lambda *args: inspections.append(args)
module.stop_exact_retirement_target = lambda *args: stops.append(args)
try: changed.retire_live_acquired_no_report_pair(changed_request)
except module.HelperError: pass
else: raise AssertionError("changed acquired-session model entered retirement")
assert (changed.state_dir / "receipts.json").read_bytes() == before
assert not inspections and not stops

# The already-bounded historical-modern selector remains explicit on this separate exact chain.
historical, current_identity, acquired_request = acquired_fixture("synthetic-acquired-historical")
data = historical.load_store(); data["receipts"][0]["events"][1].pop("expected_launcher_argv0_digest")
historical.commit(data); current_identity.pop("expected_launcher_argv0_digest")
acquired_request["compatibility"] = "historical_modern_read_only_launch_intent_v1"
real_system = module.platform.system; module.platform.system = lambda: "Darwin"; stops = []
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(copy.deepcopy(bound))
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
result = historical.retire_live_acquired_no_report_pair(acquired_request)
assert result["outcome"] == "retired" and result["compatibility"] == acquired_request["compatibility"]
assert stops == [current_identity]
assert historical.load_store()["receipts"][0]["acquired_retirement_intent"]["compatibility"] == acquired_request["compatibility"]
module.platform.system = real_system

# Recovery observes absence before acting and exact replay never performs a blind second stop.
interrupted, current_identity, acquired_request = acquired_fixture("synthetic-acquired-interrupted")
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda *args: (_ for _ in ()).throw(RuntimeError("synthetic interruption"))
try: interrupted.retire_live_acquired_no_report_pair(acquired_request)
except RuntimeError: pass
else: raise AssertionError("synthetic acquired retirement interruption was not exposed")
receipt = interrupted.load_store()["receipts"][0]
assert receipt["events"][-1]["kind"] == "acquired_retirement_intent"
assert not module.valid_acquired_no_report_pair_retirement_chain(receipt)
stops = []; module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
module.inspect_live_indeterminate_target = lambda *args: (_ for _ in ()).throw(module.HelperError("absent"))
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
assert interrupted.retire_live_acquired_no_report_pair(
    dict(acquired_request, recover=True))["outcome"] == "retired"
assert not stops

# A durable operation identity cannot be replaced by a fresh event ID after interruption.
conflicted, current_identity, acquired_request = acquired_fixture("synthetic-acquired-conflict")
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda *args: (_ for _ in ()).throw(RuntimeError("synthetic interruption"))
try: conflicted.retire_live_acquired_no_report_pair(acquired_request)
except RuntimeError: pass
before = (conflicted.state_dir / "receipts.json").read_bytes(); inspections = []; stops = []
module.inspect_live_indeterminate_target = lambda *args: inspections.append(args)
module.stop_exact_retirement_target = lambda *args: stops.append(args)
different = copy.deepcopy(acquired_request); different["event_id"] = "retire-different"
try: conflicted.retire_live_acquired_no_report_pair(different)
except module.HelperError: pass
else: raise AssertionError("different acquired retirement event ID replaced durable intent")
assert (conflicted.state_dir / "receipts.json").read_bytes() == before
assert not inspections and not stops

# Completion/acquisition provenance, receipt shape, and platform identity schema are exact.
for name, mutate in [
    ("completion-id", lambda receipt: receipt["events"][2].update(event_id="fabricated")),
    ("acquisition-id", lambda receipt: receipt["events"][3].update(event_id="invalid id")),
    ("created-at", lambda receipt: receipt.update(created_at="2026-07-20T12:00:09Z")),
    ("updated-at", lambda receipt: receipt.update(updated_at="2026-07-20T12:00:09Z")),
    ("null-marker", lambda receipt: receipt.update(acquired_pair_retired=None)),
    ("extra-field", lambda receipt: receipt.update(extra="fabricated")),
]:
    blocked, current_identity, candidate = acquired_fixture("acquired-provenance-" + name)
    data = blocked.load_store(); mutate(data["receipts"][0]); blocked.commit(data)
    inspections = []; stops = []
    module.inspect_live_indeterminate_target = lambda *args: inspections.append(args)
    module.stop_exact_retirement_target = lambda *args: stops.append(args)
    try: blocked.retire_live_acquired_no_report_pair(candidate)
    except module.HelperError: pass
    else: raise AssertionError("malformed acquired provenance retired: " + name)
    assert not inspections and not stops, name

blocked, current_identity, candidate = acquired_fixture("acquired-darwin-object-missing")
data = blocked.load_store(); receipt = data["receipts"][0]
for durable in (receipt["session_identity"], receipt["events"][2]["identity"],
                receipt["events"][3]["identity"]):
    durable.pop("process_executable_object_identity")
blocked.commit(data); real_system = module.platform.system; module.platform.system = lambda: "Darwin"
inspections = []; stops = []
module.inspect_live_indeterminate_target = lambda *args: inspections.append(args)
module.stop_exact_retirement_target = lambda *args: stops.append(args)
try: blocked.retire_live_acquired_no_report_pair(candidate)
except module.HelperError: pass
else: raise AssertionError("incomplete Darwin acquired identity retired")
assert not inspections and not stops
module.platform.system = real_system

# Every live identity component is required before durable intent or process mutation.
for name, mutate in [
    ("pane", lambda live: live.update(pane_id="%99")),
    ("pid", lambda live: live.update(pane_pid=9999)),
    ("executable", lambda live: live.update(process_executable_identity="file:changed")),
    ("object", lambda live: live.update(process_executable_object_identity="object:changed")),
    ("argv", lambda live: live.update(normalized_argv_digest="9" * 64)),
    ("session", lambda live: live.update(claude_session_id="550e8400-e29b-41d4-a716-446655440099")),
    ("workdir", lambda live: live.update(workdir="/synthetic/substitute")),
]:
    blocked, current_identity, candidate = acquired_fixture("acquired-blocked-" + name)
    mutate(current_identity); stops = []
    module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
    module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
    try: outcome = blocked.retire_live_acquired_no_report_pair(candidate)
    except module.HelperError: outcome = {"outcome":"blocked"}
    receipt = blocked.load_store()["receipts"][0]
    assert outcome["outcome"] == "blocked" and not stops, name
    assert "acquired_retirement_intent" not in receipt and "acquired_pair_retired" not in receipt, name

# Pending input and every report/delivery/acknowledgement/park/detach/prior-retirement shape are ineligible.
for name, event, materialized in [
    ("input", {"event_id":"input", "kind":"input_request"}, {"input_state":"pending"}),
    ("report", {"event_id":"report", "kind":"valid_report"}, {"report_message_id":"report"}),
    ("delivery", {"event_id":"delivery", "kind":"delivered"}, {}),
    ("ack", {"event_id":"ack", "kind":"acknowledged"}, {}),
    ("park", {"event_id":"park", "kind":"park_intent"}, {}),
    ("detach", {"event_id":"detach", "kind":"worker_detached"}, {}),
    ("retired", {"event_id":"retired", "kind":"pair_retired"}, {}),
]:
    blocked, current_identity, candidate = acquired_fixture("acquired-forbidden-" + name)
    data = blocked.load_store(); receipt = data["receipts"][0]
    receipt["events"].append(event); receipt.update(materialized); blocked.commit(data)
    inspections = []; stops = []
    module.inspect_live_indeterminate_target = lambda *args: inspections.append(args)
    module.stop_exact_retirement_target = lambda *args: stops.append(args)
    try: blocked.retire_live_acquired_no_report_pair(candidate)
    except module.HelperError: pass
    else: raise AssertionError("forbidden acquired receipt shape retired: " + name)
    assert not inspections and not stops, name

# Pane/PID replacement between the two inspections, unavailable inspection, and ambiguous absence fail closed.
for name, mutate in [
    ("replaced-pane", lambda live: live.update(pane_id="%99")),
    ("pid-reuse", lambda live: (live.update(process_identity="start:reused"),
                                live.update(process_start_identity="process-start:start:reused"))),
]:
    blocked, current_identity, candidate = acquired_fixture("acquired-race-" + name)
    changed = copy.deepcopy(current_identity); mutate(changed); observations = [current_identity, changed]
    stops = []; module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(observations.pop(0))
    module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
    outcome = blocked.retire_live_acquired_no_report_pair(candidate)
    assert outcome["outcome"] == "blocked" and outcome["blocker"] == "retirement_identity_changed"
    assert not stops

blocked, current_identity, candidate = acquired_fixture("acquired-inspection-unavailable")
module.inspect_live_indeterminate_target = lambda *args: (_ for _ in ()).throw(module.HelperError("unavailable"))
stops = []; module.stop_exact_retirement_target = lambda *args: stops.append(args)
try: blocked.retire_live_acquired_no_report_pair(candidate)
except module.HelperError: pass
else: raise AssertionError("unavailable acquired inspection retired")
assert not stops and "acquired_retirement_intent" not in blocked.load_store()["receipts"][0]

blocked, current_identity, candidate = acquired_fixture("acquired-absence-ambiguous")
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
stops = []; module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
module.confirm_retirement_target_absent = lambda bound: "retirement_absence_unconfirmed"
outcome = blocked.retire_live_acquired_no_report_pair(candidate)
assert outcome["outcome"] == "blocked" and outcome["blocker"] == "retirement_absence_unconfirmed"
receipt = blocked.load_store()["receipts"][0]
assert len(stops) == 1 and "acquired_retirement_intent" in receipt and "acquired_pair_retired" not in receipt

# Exact completion/acquisition allows retirement to rely on fence/lock exclusion despite a busy gate.
blocked, current_identity, candidate = acquired_fixture("acquired-legacy-gate-holder")
class BusyGate:
    def __enter__(self): raise module.LaunchGateBusy()
    def __exit__(self, *args): pass
blocked.launch_gate = lambda *args, **kwargs: BusyGate(); stops = []
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(copy.deepcopy(bound))
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
outcome = blocked.retire_live_acquired_no_report_pair(candidate)
assert outcome["outcome"] == "retired" and stops == [current_identity]
assert "acquired_pair_retired" in blocked.load_store()["receipts"][0]

blocked, current_identity, candidate = acquired_fixture("acquired-mutated-intent")
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda *args: (_ for _ in ()).throw(RuntimeError("synthetic interruption"))
try: blocked.retire_live_acquired_no_report_pair(candidate)
except RuntimeError: pass
data = blocked.load_store(); data["receipts"][0]["acquired_retirement_intent"]["identity"]["pane_id"] = "%99"
blocked.commit(data); stops = []; module.stop_exact_retirement_target = lambda *args: stops.append(args)
try: blocked.retire_live_acquired_no_report_pair(dict(candidate, recover=True))
except module.HelperError: pass
else: raise AssertionError("mutated acquired retirement intent recovered")
assert not stops

stops = []
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(copy.deepcopy(bound))
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
store, current_identity, request = fixture()
result = store.retire_live_indeterminate_pair(request)
assert result["outcome"] == "retired" and result["fence"] == "retained", result
assert len(stops) == 1 and stops[0] == current_identity
receipt = store.load_store()["receipts"][0]
assert [event["kind"] for event in receipt["events"]][-2:] == ["retirement_intent", "pair_retired"]
assert receipt["state"] == "valid_report" and receipt["report_message_id"] == "report"
assert "cleanup_eligible_at" not in receipt and module.valid_pair_retirement_chain(receipt)
assert store.worker_teardown("T-synthetic", True)["pairs"] == [{
    "pair_sha256":hashlib.sha256(b"synthetic-live").hexdigest(), "state":"pair_retired", "action":"none"}]
assert store.retire_live_indeterminate_pair(request)["outcome"] == "duplicate" and len(stops) == 1

# The historical-modern authority is Darwin-only and cannot admit Linux executable forms.
compatibility = "historical_modern_read_only_launch_intent_v1"
module.platform.system = lambda: "Linux"
for executable_form in ("direct", "node", "bun"):
    blocked, current_identity, candidate = fixture("blocked-linux-" + executable_form)
    data = blocked.load_store(); data["receipts"][0]["events"][1].pop("expected_launcher_argv0_digest")
    blocked.commit(data); current_identity.pop("expected_launcher_argv0_digest")
    current_identity["process_name"] = executable_form; candidate["compatibility"] = compatibility
    stops.clear(); inspections = []
    module.inspect_live_indeterminate_target = lambda *args: (inspections.append(args), copy.deepcopy(current_identity))[1]
    module.stop_exact_retirement_target = lambda *args, **kwargs: stops.append(args)
    try: outcome = blocked.retire_live_indeterminate_pair(candidate)
    except module.HelperError: outcome = {"outcome":"blocked"}
    receipt = blocked.load_store()["receipts"][0]
    assert outcome["outcome"] == "blocked" and not inspections and not stops, executable_form
    assert "retirement_intent" not in receipt and "pair_retired" not in receipt, executable_form
module.platform.system = lambda: "Darwin"

# The explicit historical-modern selector accepts only the modern read-only shape missing launcher argv0.
historical, current_identity, request = fixture("synthetic-historical-modern")
data = historical.load_store(); historical_intent = data["receipts"][0]["events"][1]
historical_intent.pop("expected_launcher_argv0_digest"); historical.commit(data)
current_identity.pop("expected_launcher_argv0_digest"); request["compatibility"] = compatibility; stops.clear()
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(copy.deepcopy(bound))
result = historical.retire_live_indeterminate_pair(request)
assert result["outcome"] == "retired" and result["compatibility"] == compatibility, result
assert len(stops) == 1 and stops[0] == current_identity
receipt = historical.load_store()["receipts"][0]
assert receipt["retirement_intent"]["compatibility"] == compatibility
assert module.valid_pair_retirement_chain(receipt)
assert historical.worker_teardown("T-synthetic", True)["pairs"] == [{
    "pair_sha256":hashlib.sha256(b"synthetic-historical-modern").hexdigest(),
    "state":"pair_retired", "action":"none"}]
assert historical.retire_live_indeterminate_pair(request)["outcome"] == "duplicate"

# The historical selector still requires the immutable expected argv digest to match live argv.
blocked, current_identity, candidate = fixture("blocked-historical-argv")
data = blocked.load_store(); data["receipts"][0]["events"][1].pop("expected_launcher_argv0_digest")
blocked.commit(data); current_identity.pop("expected_launcher_argv0_digest")
current_identity["normalized_argv_digest"] = "9" * 64; candidate["compatibility"] = compatibility
stops.clear(); module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
try: outcome = blocked.retire_live_indeterminate_pair(candidate)
except module.HelperError: outcome = {"outcome":"blocked"}
assert outcome["outcome"] == "blocked" and not stops

# Selector omission/substitution and every shape other than exact missing-one-field fail before stop.
for name, selector, mutate in [
    ("historical-without-selector", None,
     lambda launch: launch.pop("expected_launcher_argv0_digest")),
    ("selector-on-current", compatibility, lambda launch: None),
    ("historical-extra", compatibility,
     lambda launch: (launch.pop("expected_launcher_argv0_digest"), launch.update(extra="blocked"))),
    ("historical-missing-two", compatibility,
     lambda launch: (launch.pop("expected_launcher_argv0_digest"), launch.pop("packet_identity"))),
]:
    blocked, current_identity, candidate = fixture("blocked-" + name)
    data = blocked.load_store(); mutate(data["receipts"][0]["events"][1]); blocked.commit(data)
    if selector is not None: candidate["compatibility"] = selector
    stops.clear(); module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
    module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
    try: outcome = blocked.retire_live_indeterminate_pair(candidate)
    except module.HelperError: outcome = {"outcome":"blocked"}
    assert outcome["outcome"] == "blocked" and not stops, name

# The historical-modern selector cannot substitute for the distinct pre-identity schema.
blocked, current_identity, candidate = fixture("blocked-pre-identity-substitution")
data = blocked.load_store(); launch = data["receipts"][0]["events"][1]
launch.clear(); launch.update({"event_id":"launch", "kind":"launch_intent", "workflow":"read_only",
    "request_digest":"8" * 64, "claude_session_id":identity["claude_session_id"],
    "tmux_session":identity["session"], "tmux_window":identity["window"],
    "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64,
    "launch_command_digest":"5" * 64, "at":"2026-07-20T12:00:00Z"})
blocked.commit(data); candidate["compatibility"] = compatibility; stops.clear()
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
try: outcome = blocked.retire_live_indeterminate_pair(candidate)
except module.HelperError: outcome = {"outcome":"blocked"}
assert outcome["outcome"] == "blocked" and not stops

# Historical-modern interruption binds the selector durably and requires it for exact recovery.
historical, current_identity, request = fixture("synthetic-historical-interrupted")
data = historical.load_store(); data["receipts"][0]["events"][1].pop("expected_launcher_argv0_digest")
historical.commit(data); current_identity.pop("expected_launcher_argv0_digest")
request["compatibility"] = compatibility
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: (_ for _ in ()).throw(RuntimeError("synthetic interruption"))
try: historical.retire_live_indeterminate_pair(request)
except RuntimeError: pass
else: raise AssertionError("synthetic historical interruption was not exposed")
interrupted = historical.load_store(); durable = interrupted["receipts"][0]["retirement_intent"]
assert durable["compatibility"] == compatibility
assert "expected_launcher_argv0_digest" not in durable["identity"]
without_selector = dict(request, recover=True); without_selector.pop("compatibility"); stops.clear()
module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
try: historical.retire_live_indeterminate_pair(without_selector)
except module.HelperError: pass
else: raise AssertionError("historical recovery omitted its durable compatibility selector")
assert historical.load_store() == interrupted and not stops
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
assert historical.retire_live_indeterminate_pair(dict(request, recover=True))["outcome"] == "retired"
sealed = historical.load_store(); receipt = sealed["receipts"][0]
assert "cleanup_eligible_at" not in receipt and module.valid_pair_retirement_chain(receipt) and not stops
try:
    historical.route({"delegation_id":"synthetic-historical-interrupted", "event_id":"sealed-route",
                      "routing":{"target":"machine_local_inbox"}})
except module.HelperError: pass
else: raise AssertionError("historical pair retirement did not seal later mutation")
assert historical.load_store() == sealed

# Durable intent precedes mutation; recovery observes exact evidence and never blindly repeats a stop after absence.
store, current_identity, request = fixture("synthetic-interrupted")
module.stop_exact_retirement_target = lambda bound, *args: (_ for _ in ()).throw(RuntimeError("synthetic interruption"))
try: store.retire_live_indeterminate_pair(request)
except RuntimeError: pass
else: raise AssertionError("synthetic interruption was not exposed")
receipt = store.load_store()["receipts"][0]
assert receipt["events"][-1]["kind"] == "retirement_intent" and not module.valid_pair_retirement_chain(receipt)
recover = dict(request, recover=True); stops.clear()
module.inspect_live_indeterminate_target = lambda *args: (_ for _ in ()).throw(module.HelperError("absent"))
module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
module.confirm_retirement_target_absent = lambda bound: "exact_retirement_target_absent"
assert store.retire_live_indeterminate_pair(recover)["outcome"] == "retired" and not stops

# Every identity, report, receipt-chain, and inspection mismatch blocks before process mutation.
for name, mutate in [
    ("argv", lambda live, receipt, req: live.update(normalized_argv_digest="9" * 64)),
    ("workdir", lambda live, receipt, req: live.update(workdir="/synthetic/substitute")),
    ("report", lambda live, receipt, req: req["authorization"].update(report_sha256="8" * 64)),
    ("input", lambda live, receipt, req: receipt["events"].append({"event_id":"input", "kind":"input_request"})),
    ("launch-shape", lambda live, receipt, req: receipt["events"][1].pop("packet_identity")),
    ("extra-created", lambda live, receipt, req: receipt["events"].append({"event_id":"extra", "kind":"created"})),
]:
    blocked, current_identity, candidate = fixture("blocked-" + name); data = blocked.load_store(); rec = data["receipts"][0]
    mutate(current_identity, rec, candidate); blocked.commit(data); stops.clear()
    module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
    module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
    try: outcome = blocked.retire_live_indeterminate_pair(candidate)
    except module.HelperError: outcome = {"outcome":"blocked"}
    assert outcome["outcome"] == "blocked" and not stops, name

blocked, current_identity, candidate = fixture("blocked-materialized-intent"); stops.clear()
module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(current_identity)
module.stop_exact_retirement_target = lambda bound, *args: (_ for _ in ()).throw(RuntimeError("synthetic interruption"))
try: blocked.retire_live_indeterminate_pair(candidate)
except RuntimeError: pass
data = blocked.load_store(); data["receipts"][0]["retirement_intent"]["identity"]["pane_id"] = "%99"
blocked.commit(data); module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
try: blocked.retire_live_indeterminate_pair(dict(candidate, recover=True))
except module.HelperError: pass
else: raise AssertionError("mismatched materialized retirement intent recovered")
assert not stops

for name, field, replacement in [("wrong-pane", "pane_id", "%99"),
                                 ("pid-reuse", "process_identity", "start:reused")]:
    blocked, current_identity, candidate = fixture("blocked-" + name); changed = dict(current_identity)
    changed[field] = replacement; observations = [current_identity, changed]; stops.clear()
    module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(observations.pop(0))
    module.stop_exact_retirement_target = lambda bound, *args: stops.append(bound)
    outcome = blocked.retire_live_indeterminate_pair(candidate)
    assert outcome["outcome"] == "blocked" and outcome["blocker"] == "retirement_identity_changed"
    assert not stops, name

# Inaccessible live identity and a surviving changed incarnation never become absence.
module.read_bounded_command = lambda command, limit: ""
module.exact_process_identity = lambda pid: (_ for _ in ()).throw(module.HelperError("inaccessible"))
module.process_pid_is_absent = lambda pid: False
absence = real_confirm_retirement_target_absent(current_identity)
assert absence == "process_inspection_unavailable", absence
module.exact_process_identity = lambda pid: ("changed", current_identity["process_identity"], ["changed"], "changed")
module.process_executable_identity = lambda pid: "file:changed"
absence = real_confirm_retirement_target_absent(current_identity)
assert absence == "retirement_target_identity_changed_after_stop", absence
# Linux execve may change the composite executable identity without changing the process start incarnation.
module.exact_process_identity = lambda pid: ("changed", "linux:777:9:10:" + "f" * 64, ["changed"], "changed")
module.process_start_identity_from_process_identity = lambda value: "linux:777"
linux_identity = dict(current_identity, process_identity="linux:777:1:2:" + "e" * 64,
                      process_start_identity="linux:777")
absence = real_confirm_retirement_target_absent(linux_identity)
assert absence == "retirement_target_identity_changed_after_stop", absence
module.process_start_identity_from_process_identity = lambda value: current_identity["process_start_identity"]
for panes in ("%bad", "%1\n%1", "\n".join("%" + str(index) for index in range(module.MAX_ABSENCE_PANES + 1))):
    module.read_bounded_command = lambda command, limit, panes=panes: panes
    assert real_confirm_retirement_target_absent(current_identity) == "tmux_inspection_ambiguous"
module.read_bounded_command = lambda command, limit: ""
observations = iter([module.HelperError("absent"),
    (current_identity["process_name"], current_identity["process_identity"], ["synthetic"],
     current_identity["process_command_digest"])])
def transient_process(pid):
    observation = next(observations)
    if isinstance(observation, Exception): raise observation
    return observation
module.exact_process_identity = transient_process
module.process_pid_is_absent = lambda pid: True
module.process_executable_identity = lambda pid: current_identity["process_executable_identity"]
assert real_confirm_retirement_target_absent(current_identity) == "retirement_target_still_live"
module.exact_process_identity = lambda pid: (_ for _ in ()).throw(module.HelperError("absent"))
assert real_confirm_retirement_target_absent(current_identity) == "exact_retirement_target_absent"

# Darwin retirement derives and requires this delegation's exact private executable route and object.
darwin_store, darwin_identity, _ = fixture("synthetic-darwin")
private_path = module.private_launch_transport_path(darwin_store, "synthetic-darwin").with_name("verified-claude")
private_path.parent.mkdir(mode=0o700, parents=True); private_path.write_bytes(b"synthetic executable"); private_path.chmod(0o500)
descriptor, _, _, private_object = module.open_exact_verified_executable(private_path); module.os.close(descriptor)
module.platform.system = lambda: "Darwin"
module.read_bounded_command = lambda command, limit: "Synthetic\t$17\tlive-indeterminate\t@17\t%23"
observed_private_objects = []
def inspect_darwin(*args, **kwargs):
    observed_private_objects.append(kwargs.get("expected_process_executable_object_identity"))
    return copy.deepcopy(darwin_identity)
module.inspect_claude_identity = inspect_darwin
module.process_executable_path = lambda pid: private_path
binding = darwin_store.load_store()["receipts"][0]["binding"]
assert real_inspect_live_indeterminate_target(intent, binding, darwin_store, "synthetic-darwin")["pane_id"] == "%23"
assert observed_private_objects == [private_object]
alternate = private_path.with_name("alternate-verified-claude"); alternate.write_bytes(private_path.read_bytes()); alternate.chmod(0o500)
module.process_executable_path = lambda pid: alternate
try: real_inspect_live_indeterminate_target(intent, binding, darwin_store, "synthetic-darwin")
except module.HelperError: pass
else: raise AssertionError("equal-content alternate Darwin executable route was accepted")
module.process_executable_path = lambda pid: private_path

# Final snapshot, external process verification, and kill all use one retained control connection.
control_instances = []
control_values = {"#{session_name}":"Synthetic", "#{session_id}":"$17",
    "#{window_name}":"live-indeterminate", "#{window_id}":"@17", "#{pane_id}":"%23",
    "#{pane_pid}":"4242", "#{pane_current_path}":darwin_identity["workdir"],
    "#{pane_current_command}":"claude", "#{pane_start_command}":"synthetic-command"}
class FakeControl:
    def __init__(self, session): self.commands = []; control_instances.append(self)
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def command(self, args):
        self.commands.append(args)
        formats = {module.tmux_single_line_format(key[2:-1]): value for key,value in control_values.items()}
        token, format_value = args[-1].split(":", 1)
        return [token + ":" + json.dumps(formats[format_value])]
    def command_sequence(self, commands):
        self.commands.extend(commands)
        return [[], [commands[1][-1]]]
module.TmuxControlConnection = FakeControl
def inspect_retained(*args, **kwargs):
    assert kwargs["tmux_fields"] == [control_values[key] for key in
        ("#{session_name}", "#{window_name}", "#{window_id}", "#{pane_id}", "#{pane_pid}",
         "#{pane_current_path}", "#{pane_current_command}", "#{pane_start_command}")]
    return {key:value for key,value in darwin_identity.items() if key not in
        {"session_id", "process_start_identity", "expected_launcher_identity",
         "expected_executable_object_identity", "expected_launcher_argv0_digest"}}
module.inspect_claude_identity = inspect_retained
real_stop_exact_retirement_target(darwin_identity, private_path)
assert len(control_instances) == 1 and control_instances[0].commands[-2] == ["kill-pane", "-t", "%23"]

# Historical-modern final revalidation keeps the same retained connection and all non-argv0 identity.
historical_darwin_identity = dict(darwin_identity)
historical_darwin_identity.pop("expected_launcher_argv0_digest")
def inspect_historical_retained(*args, **kwargs):
    assert kwargs["allow_missing_launcher_argv0_digest"] is True
    return {key:value for key,value in historical_darwin_identity.items() if key not in
        {"session_id", "process_start_identity", "expected_launcher_identity",
         "expected_executable_object_identity"}}
module.inspect_claude_identity = inspect_historical_retained
real_stop_exact_retirement_target(historical_darwin_identity, private_path, compatibility)
assert len(control_instances) == 2 and control_instances[1].commands[-2] == ["kill-pane", "-t", "%23"]
module.inspect_claude_identity = inspect_retained

# A same-object hardlink route change only at final revalidation cannot kill or seal either schema.
for route_name, active_compatibility in (("current", None), ("historical", compatibility)):
    blocked, active_identity, candidate = fixture("blocked-final-route-" + route_name)
    if active_compatibility is not None:
        data = blocked.load_store(); data["receipts"][0]["events"][1].pop("expected_launcher_argv0_digest")
        blocked.commit(data); active_identity.pop("expected_launcher_argv0_digest")
        candidate["compatibility"] = active_compatibility
    expected_route = module.retirement_private_executable_path(
        blocked, candidate["delegation_id"], active_compatibility)
    expected_route.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    expected_route.write_bytes(b"same synthetic executable"); expected_route.chmod(0o500)
    hardlink = expected_route.with_name("verified-claude-hardlink"); module.os.link(expected_route, hardlink)
    module.process_executable_path = lambda pid, hardlink=hardlink: hardlink
    module.inspect_live_indeterminate_target = lambda *args: copy.deepcopy(active_identity)
    control_values["#{pane_current_path}"] = active_identity["workdir"]
    def inspect_final_route_change(*args, **kwargs):
        assert kwargs["allow_missing_launcher_argv0_digest"] is (active_compatibility is not None)
        return {key:value for key,value in active_identity.items() if key not in
            {"session_id", "process_start_identity", "expected_launcher_identity",
             "expected_executable_object_identity", "expected_launcher_argv0_digest"}}
    module.inspect_claude_identity = inspect_final_route_change
    module.stop_exact_retirement_target = real_stop_exact_retirement_target
    before = len(control_instances)
    try: blocked.retire_live_indeterminate_pair(candidate)
    except module.HelperError: pass
    else: raise AssertionError("same-object alternate private route retired the pair")
    receipt = blocked.load_store()["receipts"][0]
    assert "retirement_intent" in receipt and "pair_retired" not in receipt
    assert all(command[0] != "kill-pane" for command in control_instances[before].commands)
module.process_executable_path = lambda pid: private_path
module.inspect_claude_identity = inspect_retained

# Complete queued fake frames cannot satisfy a fresh per-command token and never authorize kill.
class PrequeuedControl(FakeControl):
    def command(self, args):
        self.commands.append(args)
        formats = {module.tmux_single_line_format(key[2:-1]): value for key,value in control_values.items()}
        _, format_value = args[-1].split(":", 1)
        return ["0" * 64 + ":" + json.dumps(formats[format_value])]
    def command_sequence(self, commands):
        raise AssertionError("queued fake snapshot reached kill sequence")
module.TmuxControlConnection = PrequeuedControl
try: real_stop_exact_retirement_target(darwin_identity, private_path)
except module.HelperError: pass
else: raise AssertionError("wrong-token queued frame was accepted")
assert all(command[0] != "kill-pane" for command in control_instances[-1].commands)

# An injected empty or wrong-token second frame cannot acknowledge an exact kill sequence.
for injected in ([], ["retirement-kill:" + "0" * 64]):
    class InjectedKillControl(FakeControl):
        def command_sequence(self, commands):
            self.commands.extend(commands)
            return [[], injected]
    module.TmuxControlConnection = InjectedKillControl
    try: real_stop_exact_retirement_target(darwin_identity, private_path)
    except module.HelperError: pass
    else: raise AssertionError("injected kill response was accepted")
module.TmuxControlConnection = FakeControl

# Encoded literal backslashes and encoded newlines remain exact values and cannot forge a match or kill.
def inspect_encoded_cwd(*args, **kwargs):
    current = {key:value for key,value in darwin_identity.items() if key not in
        {"session_id", "process_start_identity", "expected_launcher_identity",
         "expected_executable_object_identity", "expected_launcher_argv0_digest"}}
    current["workdir"] = kwargs["tmux_fields"][5]
    return current
module.inspect_claude_identity = inspect_encoded_cwd
for encoded_cwd in (r"trusted\057work", "line\n%end 10 7 1", "line\r%error 10 7 1"):
    control_values["#{pane_current_path}"] = encoded_cwd
    before = len(control_instances)
    try: real_stop_exact_retirement_target(darwin_identity, private_path)
    except module.HelperError: pass
    else: raise AssertionError("ambiguous encoded cwd reached exact kill")
    assert all(command[0] != "kill-pane" for command in control_instances[before].commands)

print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("live indeterminate pair retirement fixture: %v\n%s", err, output)
	}

	stateDir := t.TempDir()
	private := map[string]any{
		"delegation_id": "private-delegation", "event_id": "private-event",
		"origin_thread": "private-origin", "authorization": map[string]any{
			"terminal_state": "merged", "report_sha256": strings.Repeat("6", 64),
			"coordinator_authorization_sha256": strings.Repeat("7", 64),
		},
	}
	stdout, stderr, err := runHelper(t, stateDir, private, "lifecycle", "retire-live-indeterminate-pair")
	if err == nil || !strings.Contains(stdout, `"blocker":"retirement_proof_invalid_or_unavailable"`) {
		t.Fatalf("privacy-safe retirement CLI blocker = %v: %s%s", err, stdout, stderr)
	}
	for _, forbidden := range []string{"private-delegation", "private-event", "private-origin"} {
		if strings.Contains(stdout+stderr, forbidden) {
			t.Fatalf("retirement CLI leaked private identity %q: %s%s", forbidden, stdout, stderr)
		}
	}
	stdout, stderr, err = runHelper(t, stateDir, private, "lifecycle", "retire-live-acquired-no-report-pair")
	if err == nil || !strings.Contains(stdout, `"blocker":"retirement_proof_invalid_or_unavailable"`) {
		t.Fatalf("privacy-safe acquired retirement CLI blocker = %v: %s%s", err, stdout, stderr)
	}
	for _, forbidden := range []string{"private-delegation", "private-event", "private-origin"} {
		if strings.Contains(stdout+stderr, forbidden) {
			t.Fatalf("acquired retirement CLI leaked private identity %q: %s%s", forbidden, stdout, stderr)
		}
	}
}

func TestPreIdentityAcquiredNoReportPairHasPermanentTerminalPolicy(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import copy, hashlib, importlib.util, pathlib, shutil, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)

def fixture(name="synthetic-pre-identity-acquired"):
    state = pathlib.Path(tempfile.mkdtemp()).resolve(); state.chmod(0o700)
    workdir = pathlib.Path(tempfile.mkdtemp()).resolve()
    store = module.ReceiptStore(state, state)
    binding = {"protocol_version":1, "delegation_id":name, "nonce":"1" * 64, "task_id":"task",
        "question_message_id":"question", "origin_thread":"T-synthetic", "repository":"repository",
        "base":"2" * 40, "workdir":str(workdir), "producer_role":"thinker", "authority":"read_only",
        "task_reference":"fixture", "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64,
        "launch_command_digest":"5" * 64}
    assert store.create({"binding":binding, "routing":{"target":"machine_local_inbox"}}) == "recorded"
    data = store.load_store(); receipt = data["receipts"][0]
    intent = {"event_id":"launch", "kind":"launch_intent", "workflow":"read_only",
        "request_digest":"8" * 64, "claude_session_id":"550e8400-e29b-41d4-a716-446655440000",
        "tmux_session":"Synthetic", "tmux_window":"pre-identity", "packet_digest":"3" * 64,
        "launch_policy_digest":"4" * 64, "launch_command_digest":"5" * 64,
        "at":"2026-07-20T12:00:00Z"}
    completed_identity = {"session":"Synthetic", "window":"pre-identity", "window_id":"@17", "pane_id":"%23"}
    acquired_identity = {**completed_identity, "pane_pid":4242,
        "claude_session_id":intent["claude_session_id"], "workdir":str(workdir),
        "current_command":"claude", "process_name":"claude", "process_identity":"1750000000.000017",
        "process_command_digest":"9" * 64, "launch_command_digest":"5" * 64}
    receipt["events"].extend([
        intent,
        {"event_id":module.internal_event_id("launch-result", "launch"), "kind":"launch_completed",
         "operation_event_id":"launch", "identity":completed_identity, "at":"2026-07-20T12:00:01Z"},
        {"event_id":"acquired", "kind":"session_acquired", "identity":acquired_identity,
         "at":"2026-07-20T12:00:02Z"},
    ])
    receipt["session_identity"] = copy.deepcopy(acquired_identity)
    receipt["updated_at"] = "2026-07-20T12:00:02Z"; store.commit(data)
    request = {"delegation_id":name, "event_id":"policy-stable", "origin_thread":"T-synthetic",
        "compatibility":"pre_identity_acquired_no_report_v1",
        "authorization":{"terminal_state":"merged", "report_sha256":"6" * 64,
                         "coordinator_authorization_sha256":"7" * 64}}
    store.synthetic_workdir = workdir
    return store, request

store, request = fixture(); before = store.path.read_bytes(); inspections = []; stops = []
module.inspect_live_indeterminate_target = lambda *args: inspections.append(args)
module.stop_exact_retirement_target = lambda *args: stops.append(args)
result = store.retire_live_acquired_no_report_pair(request)
assert result == {
    "action":"live_acquired_no_report_pair_retirement",
    "origin_thread_sha256":hashlib.sha256(b"T-synthetic").hexdigest(),
    "pair_sha256":hashlib.sha256(b"synthetic-pre-identity-acquired").hexdigest(),
    "outcome":"blocked", "blocker":"pre_identity_acquired_pair_permanently_non_retirable",
    "policy":"preserve_receipt_runtime_artifacts_and_origin_fence",
    "remediation":"paired_worker_teardown_prohibited",
    "compatibility":"pre_identity_acquired_no_report_v1", "fence":"retained",
}
assert store.path.read_bytes() == before and not inspections and not stops
assert "T-synthetic" in store.lifecycle.load()["teardown_fences"]
assert store.retire_live_acquired_no_report_pair(request) == result
teardown = store.worker_teardown("T-synthetic", True)
assert teardown["outcome"] == "blocked"
assert teardown["pairs"] == [{
    "pair_sha256":hashlib.sha256(b"synthetic-pre-identity-acquired").hexdigest(),
    "state":"created", "action":"block",
    "blocker":"pre_identity_acquired_pair_permanently_non_retirable"}]

# The selector is mandatory and exact; malformed, report-bearing, and modern shapes retain their old paths.
without = copy.deepcopy(request); without.pop("compatibility")
try: store.retire_live_acquired_no_report_pair(without)
except module.HelperError: pass
else: raise AssertionError("pre-identity acquired shape entered the modern route")
for name, mutate in [
    ("completion-expanded", lambda receipt: receipt["events"][2]["identity"].update(pane_pid=4242)),
    ("acquired-normalized-argv", lambda receipt: receipt["session_identity"].update(normalized_argv_digest="a" * 64)),
    ("acquired-event-drift", lambda receipt: receipt["events"][3]["identity"].update(pane_id="%99")),
    ("report-bearing", lambda receipt: (receipt.update(state="valid_report", report_message_id="report"),
                                         receipt["events"].append({"event_id":"report", "kind":"valid_report"}))),
]:
    blocked, candidate = fixture("synthetic-blocked-" + name)
    data = blocked.load_store(); mutate(data["receipts"][0]); blocked.commit(data)
    before = blocked.path.read_bytes(); inspections.clear(); stops.clear()
    try: blocked.retire_live_acquired_no_report_pair(candidate)
    except module.HelperError: pass
    else: raise AssertionError("malformed pre-identity acquired shape accepted: " + name)
    assert blocked.path.read_bytes() == before and not inspections and not stops, name

for name, value in [("boolean-pid", True), ("oversized-process-name", "x" * 257)]:
    blocked, candidate = fixture("synthetic-malformed-" + name)
    data = blocked.load_store(); receipt = data["receipts"][0]
    field = "pane_pid" if name == "boolean-pid" else "process_name"
    receipt["session_identity"][field] = value
    receipt["events"][3]["identity"][field] = value
    blocked.commit(data); before = blocked.path.read_bytes()
    try: blocked.retire_live_acquired_no_report_pair(candidate)
    except module.HelperError: pass
    else: raise AssertionError("malformed durable identity accepted: " + name)
    assert blocked.path.read_bytes() == before and not inspections and not stops, name

store, request = fixture("synthetic-missing-workdir"); shutil.rmtree(store.synthetic_workdir)
result = store.retire_live_acquired_no_report_pair(request)
assert result["blocker"] == "pre_identity_acquired_pair_permanently_non_retirable"
assert store.worker_teardown("T-synthetic", True)["blockers"] == [{
    "pair_sha256":hashlib.sha256(b"synthetic-missing-workdir").hexdigest(),
    "blocker":"pre_identity_acquired_pair_permanently_non_retirable"}]

store, request = fixture("synthetic-no-recovery")
try: store.retire_live_acquired_no_report_pair(dict(request, recover=True))
except module.HelperError: pass
else: raise AssertionError("permanent-terminal policy accepted recovery mutation")
assert not inspections and not stops
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("pre-identity acquired policy fixture: %v: %s", err, output)
	}
}

func TestRetirementControlConnectionNeverReconnectsToReplacementTmuxServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for the retained control-connection boundary")
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(
		"/tmp", fmt.Sprintf("amux203-%d-%d.sock", os.Getpid(), time.Now().UnixNano()),
	)
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-f", "/dev/null", "-S", socket, "kill-server").Run()
		_ = os.Remove(socket)
	})
	script := `import importlib.util, pathlib, subprocess, sys, time
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
socket = sys.argv[2]
prefix = ["tmux", "-f", "/dev/null", "-S", socket]
subprocess.run(prefix + ["new-session", "-d", "-s", "Synthetic"], check=True)
subprocess.run(prefix + ["new-window", "-d", "-t", "Synthetic"], check=True)
connection = module.TmuxControlConnection("Synthetic", prefix)
connection.__enter__()
original_pane = connection.command([
    "display-message", "-p", "-t", "%0", module.tmux_single_line_format("pane_id")])
assert len(original_pane) == 1 and module.decode_tmux_command_argument(original_pane[0]) == "%0", original_pane
kill_token = "synthetic-kill-token"
kill_response = connection.command_sequence([
    ["kill-pane", "-t", "%1"], ["display-message", "-p", kill_token]])
assert kill_response == [[], [kill_token]], kill_response
assert "%1" not in subprocess.check_output(
    prefix + ["list-panes", "-a", "-F", "#{pane_id}"], text=True).splitlines()
subprocess.run(prefix + ["kill-server"], check=True)
deadline = time.monotonic() + 2
while True:
    replacement = subprocess.run(prefix + ["new-session", "-d", "-s", "Synthetic"],
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if replacement.returncode == 0: break
    if time.monotonic() >= deadline: raise RuntimeError(replacement.stderr.decode())
    time.sleep(0.01)
replacement_before = subprocess.check_output(prefix + ["list-panes", "-a", "-F", "#{pane_id}"], text=True).strip()
assert replacement_before == "%0", replacement_before
try:
    connection.command(["kill-pane", "-t", "%0"])
except module.HelperError:
    pass
else:
    raise AssertionError("dead retained connection reconnected to replacement server")
replacement_after = subprocess.check_output(prefix + ["list-panes", "-a", "-F", "#{pane_id}"], text=True).strip()
assert replacement_after == "%0", replacement_after
connection.close()
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper, socket).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("retained tmux control replacement boundary: %v\n%s", err, output)
	}
}

func TestRetirementControlParserPreservesLiteralOutputAndMatchesFrames(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, os, pathlib, selectors, sys, types
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)

def read_frames(payload, count=1):
    reader, writer = os.pipe()
    os.write(writer, payload); os.close(writer)
    connection = module.TmuxControlConnection("Synthetic")
    connection.process = types.SimpleNamespace(stdout=os.fdopen(reader, "rb", buffering=0))
    connection.selector = selectors.DefaultSelector()
    connection.selector.register(connection.process.stdout, selectors.EVENT_READ)
    try:
        return [connection._read_response() for _ in range(count)]
    finally:
        connection.selector.close(); connection.process.stdout.close()

# Notifications outside a complete response frame are ignored. Ordinary command output stays literal.
literal = read_frames(
    b"%sessions-changed\n%begin 10 7 1\ntrusted\\057work\n%end 10 7 1\n"
    b"%window-add @1\n%begin 10 8 1\ncurrent-token:value\n"
    b"%end 10 8 1\n%session-window-changed $1 @1\n",
    2,
)
assert literal == [[r"trusted\057work"], ["current-token:value"]], literal

for payload in (
    b"%begin 10 7 1\nsafe\n%end 10 8 1\n",
    b"%begin 10 7 1\nencoded-cwd\n%end 90 90 1\n%begin 90 90 1\nfabricated\n%end 90 90 1\n",
    b"%begin 10 7 1\nsafe\n%error 10 8 1\n",
    "%begin ١٠ 7 1\nsafe\n%end ١٠ 7 1\n".encode(),
    "%begin 10 7 1\nsafe\n%end 10 ٧ 1\n".encode(),
    "%begin 10 7 1\nsafe\n%error 10 7 ١\n".encode(),
    b"%begin 10 7 1\n%output %0 current-token:value\ncurrent-token:value\n%end 10 7 1\n",
):
    try:
        read_frames(payload)
    except module.HelperError:
        pass
    else:
        raise AssertionError("mismatched tmux control frame was accepted")

# tmux's documented q format plus LF/CR substitution is reversible and single-line.
assert module.decode_tmux_command_argument(r'"trusted\\057work"') == r"trusted\057work"
assert module.decode_tmux_command_argument(r'"line\n%end 10 7 1"') == "line\n%end 10 7 1"
assert module.decode_tmux_command_argument(r'"line\r%error 10 7 1"') == "line\r%error 10 7 1"
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("retirement tmux control parser fixture: %v\n%s", err, output)
	}
}

func TestDetachedReceiptIsSealedAndLaunchGateSerializesPreExecRace(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, os, pathlib, select, sys, tempfile, time
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
intent = {"event_id":"launch", "kind":"launch_intent", "workflow":"read_only",
    "claude_session_id":"550e8400-e29b-41d4-a716-446655440000",
    "tmux_session":"Synthetic", "tmux_window":"indeterminate",
    "expected_argv_digest":"a" * 64, "expected_launcher_identity":"file:1:2",
    "expected_executable_object_identity":"object:1:2:3:" + "b" * 64}
def fixture(name):
    state = pathlib.Path(tempfile.mkdtemp()).resolve(); state.chmod(0o700)
    store = module.ReceiptStore(state, state)
    binding = {"protocol_version":1, "delegation_id":name, "nonce":"1" * 64,
        "task_id":"task", "question_message_id":"question", "origin_thread":"T-synthetic",
        "repository":"repository", "base":"2" * 40, "workdir":str(state),
        "producer_role":"thinker", "authority":"read_only", "task_reference":"fixture",
        "packet_digest":"3" * 64, "launch_policy_digest":"4" * 64, "launch_command_digest":"5" * 64}
    create = {"binding":binding, "routing":{"target":"machine_local_inbox"}}
    assert store.create(create) == "recorded"
    data = store.load_store(); data["receipts"][0]["events"].append(dict(intent)); store.commit(data)
    request = {"delegation_id":name, "event_id":"detach-stable", "origin_thread":"T-synthetic",
        "authorization":{"terminal_state":"merged", "report_sha256":"6" * 64,
                         "coordinator_authorization_sha256":"7" * 64}}
    return store, create, request
module.inspect_indeterminate_launch_absence = lambda launch: "exact_launch_target_absent"
store, create, detach = fixture("sealed")
assert store.detach_indeterminate_worker(detach)["outcome"] == "detached"
before = store.path.read_bytes()
target = {key:"value" for key in ("origin_thread", "session", "window", "window_id", "pane_id", "workdir",
    "current_command", "process_name", "process_identity", "process_command_digest")}; target["pane_pid"] = 1
module.expected_launch_policy = lambda request: None
launch = {"delegation_id":"sealed", "event_id":"second-launch", "workdir":str(store.state_dir),
    "packet_file":str(store.state_dir / "packet"), "tmux_session":"Synthetic", "tmux_window":"second",
    "claude_session_id":intent["claude_session_id"], "repository":"repository", "base":"2" * 40,
    "expected_launch_policy_digest":"4" * 64}
operations = [lambda: store.route({"delegation_id":"sealed", "event_id":"route", "routing":{"target":"machine_local_inbox"}}),
    lambda: module.execute_launch(store, launch),
    lambda: store.submit_message({"delegation_id":"sealed"}, "report"),
    lambda: store.consume({"delegation_id":"sealed", "event_id":"consume", "message_id":"message"}),
    lambda: store.acknowledge({"delegation_id":"sealed", "event_id":"ack", "message_id":"message"}),
    lambda: store.accept_input({"delegation_id":"sealed", "event_id":"input", "message_id":"message"}),
    lambda: store.park({"delegation_id":"sealed", "event_id":"park"}),
    lambda: store.record_park_failure("sealed", "park", "failure"),
    lambda: store.acquire_session({"delegation_id":"sealed", "event_id":"acquire", "pane_id":"%1",
                                   "claude_session_id":intent["claude_session_id"]}),
    lambda: store.notify_amp({"delegation_id":"sealed", "event_id":"notify", "message_id":"message", "target":target}),
    lambda: module.execute_launch_transport(store, "sealed", "0" * 64, "0" * 64)]
for operation in operations:
    try: operation()
    except module.HelperError as error: assert "sealed" in str(error), error
    else: raise AssertionError("detached receipt mutation was accepted")
    assert store.path.read_bytes() == before

for terminal_state in ("launch_completed", "verified_parked"):
    retained, _, _ = fixture("retained-" + terminal_state)
    data = retained.load_store(); receipt = data["receipts"][0]
    receipt["events"].append({"event_id":"completed", "kind":"launch_completed",
                              "operation_event_id":"launch", "identity":{"synthetic":True}})
    if terminal_state == "verified_parked":
        receipt["state"] = "verified_parked"
        receipt["events"].append({"event_id":"parked", "kind":"verified_parked",
                                  "operation_event_id":"park"})
    retained.commit(data); retained_before = retained.path.read_bytes(); executed = []
    module.os.execve = lambda *arguments: executed.append(arguments)
    try: module.execute_launch_transport(retained, "retained-" + terminal_state, "0" * 64, "0" * 64)
    except module.HelperError as error: assert "not authorized" in str(error), error
    else: raise AssertionError("retained transport executed after " + terminal_state)
    assert not executed and retained.path.read_bytes() == retained_before

for name, model in (("fable", "claude-fable-5"), ("omitted", None)):
    drifted, _, _ = fixture("model-drift-" + name)
    data = drifted.load_store(); receipt = data["receipts"][0]
    receipt["binding"]["model"] = "claude-opus-4-8"
    if model is not None: receipt["events"][1]["model"] = model
    drifted.commit(data); drifted_before = drifted.path.read_bytes(); executed = []
    module.os.execve = lambda *arguments: executed.append(arguments)
    try:
        module.execute_authorized_launch_transport(
            drifted, "model-drift-" + name, "/synthetic", ["synthetic"], {})
    except module.HelperError as error:
        assert "model differs" in str(error), error
    else:
        raise AssertionError("model-drifted receipt reached transport execve: " + name)
    assert not executed and drifted.path.read_bytes() == drifted_before, name

fenced, _, _ = fixture("pre-fenced")
with fenced.lifecycle.mutation_lock():
    lifecycle = fenced.lifecycle.load()
    lifecycle["teardown_fences"]["T-synthetic"] = {"operation_id":"fence", "created_at":module.utc_now()}
    fenced.lifecycle.commit(lifecycle)
fenced_before = fenced.path.read_bytes(); executed = []
module.os.execve = lambda *arguments: executed.append(arguments)
try:
    with fenced.launch_gate("pre-fenced"):
        module.execute_authorized_launch_transport(fenced, "pre-fenced", "/synthetic", ["synthetic"], {})
except module.HelperError as error: assert "revoked" in str(error), error
else: raise AssertionError("pre-existing origin fence permitted transport execution")
assert not executed and fenced.path.read_bytes() == fenced_before

writer_store, _, _ = fixture("fence-writer-race")
writer_store.lifecycle = module.LifecycleRegistry(pathlib.Path(tempfile.mkdtemp()).resolve())
preexec_read, preexec_write = os.pipe(); release_read, release_write = os.pipe()
committed_read, committed_write = os.pipe(); transport_pid = os.fork()
if transport_pid == 0:
    try:
        with writer_store.launch_gate("fence-writer-race"):
            def pause_at_exec(*arguments):
                os.write(preexec_write, b"1"); assert os.read(release_read, 1) == b"1"
            module.os.execve = pause_at_exec
            module.execute_authorized_launch_transport(
                writer_store, "fence-writer-race", "/synthetic", ["synthetic"], {})
    finally: os._exit(0)
assert os.read(preexec_read, 1) == b"1"; writer_pid = os.fork()
if writer_pid == 0:
    try:
        with writer_store.lifecycle.mutation_lock():
            lifecycle = writer_store.lifecycle.load()
            lifecycle["teardown_fences"]["T-synthetic"] = {"operation_id":"writer", "created_at":module.utc_now()}
            writer_store.lifecycle.commit(lifecycle); os.write(committed_write, b"1")
    finally: os._exit(0)
assert select.select([committed_read], [], [], 0.3)[0] == []
os.write(release_write, b"1")
_, transport_status = os.waitpid(transport_pid, 0); assert os.WEXITSTATUS(transport_status) == 0
assert os.read(committed_read, 1) == b"1"
_, writer_status = os.waitpid(writer_pid, 0); assert os.WEXITSTATUS(writer_status) == 0

race_store, _, race_detach = fixture("race")
ready_read, ready_write = os.pipe(); continue_read, continue_write = os.pipe()
marker = race_store.state_dir / "receipt-lock-acquired"; pid = os.fork()
if pid == 0:
    os.close(ready_read); os.close(continue_write)
    try:
        with race_store.launch_gate("race"):
            os.write(ready_write, b"1"); assert os.read(continue_read, 1) == b"1"
            with race_store.mutation_lock(): marker.write_text("acquired")
    finally: os._exit(0)
os.close(ready_write); os.close(continue_read); assert os.read(ready_read, 1) == b"1"
race_before = race_store.path.read_bytes(); started = time.monotonic()
blocked = race_store.detach_indeterminate_worker(race_detach); elapsed = time.monotonic() - started
assert blocked["outcome"] == "blocked" and blocked["blocker"] == "launch_transport_active_or_indeterminate", blocked
assert elapsed < 1.0 and race_store.path.read_bytes() == race_before, elapsed
os.write(continue_write, b"1"); os.close(continue_write); _, status = os.waitpid(pid, 0)
assert os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0
assert marker.read_text() == "acquired"
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("detached sealing and launch gate fixture: %v\n%s", err, output)
	}
}

func TestIndeterminateAbsenceInventoryRejectsRealOverLimitSubprocessOutput(t *testing.T) {
	t.Parallel()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, os, pathlib, sys, tempfile
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
directory = pathlib.Path(tempfile.mkdtemp())
for name in ("tmux", "ps"):
    path = directory / name; path.write_text("#!/bin/sh\nhead -c 65537 /dev/zero | tr '\\000' x\n"); path.chmod(0o755)
os.environ["PATH"] = str(directory) + os.pathsep + os.environ["PATH"]
intent = {"claude_session_id":"550e8400-e29b-41d4-a716-446655440000",
          "tmux_session":"Synthetic", "tmux_window":"indeterminate"}
assert module.inspect_indeterminate_launch_absence(intent) == "tmux_inspection_unavailable"
(directory / "tmux").write_text("#!/bin/sh\nexit 0\n"); (directory / "tmux").chmod(0o755)
assert module.inspect_indeterminate_launch_absence(intent) == "process_inspection_unavailable"
print("ok")
`
	output, err := exec.Command("python3", "-c", script, helper).CombinedOutput()
	if err != nil || string(output) != "ok\n" {
		t.Fatalf("bounded absence subprocess fixture: %v\n%s", err, output)
	}
}

func TestReportLifecycleRequiresExplicitOrderedTransitions(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-report")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-1", "thinker_report", map[string]any{
		"accepted_role":       true,
		"accepted_exclusions": true,
		"status":              "complete",
		"verdict":             "The bounded mechanism is coherent.",
		"rationale":           "Evidence and assumptions remain distinct.",
		"evidence":            []any{"public source A"},
		"assumptions":         []any{"runtime enforcement remains vendor-owned"},
		"unsupported_claims":  []any{},
		"blockers":            []any{},
		"verification":        []any{"read public source A"},
		"changed_artifacts":   []any{},
		"references":          []any{"source:A"},
	})
	assertHelperOutcome(t, stateDir, "recorded", report, "report", "submit")
	assertHelperOutcome(t, stateDir, "duplicate", report, "report", "submit")
	lateInput := testMessage(binding, "input-too-late", "input_request", map[string]any{
		"request_type": "clarification", "question": "Too late?", "blocking_reason": "A report already exists.",
	})
	_, stderr, err := runHelper(t, stateDir, lateInput, "input", "submit")
	if err == nil || !strings.Contains(stderr, "closes the input-request stream") {
		t.Fatalf("late input error = %v, stderr %q", err, stderr)
	}

	conflict := cloneJSONMap(t, report)
	conflict["report"].(map[string]any)["verdict"] = "Conflicting payload"
	_, stderr, err = runHelper(t, stateDir, conflict, "report", "submit")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflicting report replay error = %v, stderr %q", err, stderr)
	}

	acknowledge := map[string]any{"delegation_id": binding["delegation_id"], "event_id": "ack-1", "message_id": "report-1"}
	_, stderr, err = runHelper(t, stateDir, acknowledge, "report", "acknowledge")
	if err == nil || !strings.Contains(stderr, "requires delivery") {
		t.Fatalf("early acknowledge error = %v, stderr %q", err, stderr)
	}
	park := map[string]any{"delegation_id": binding["delegation_id"], "event_id": "park-1"}
	_, stderr, err = runHelper(t, stateDir, park, "session", "park")
	if err == nil || !strings.Contains(stderr, "requires acknowledgement") {
		t.Fatalf("early park error = %v, stderr %q", err, stderr)
	}

	consume := map[string]any{"delegation_id": binding["delegation_id"], "event_id": "deliver-1", "message_id": "report-1"}
	assertHelperOutcome(t, stateDir, "recorded", consume, "inbox", "consume")
	assertHelperOutcome(t, stateDir, "recorded", acknowledge, "report", "acknowledge")
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show acknowledged receipt: %v: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"state":"acknowledged"`) {
		t.Fatalf("acknowledged receipt state missing: %s", stdout)
	}
}

func TestInvalidOrPrivateReportDoesNotAdvanceReceipt(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-invalid-report")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-private", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Invalid fixture.", "rationale": "Private content must be rejected.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{}, "changed_artifacts": []any{}, "references": []any{},
		"transcript": "must never be persisted",
	})
	_, stderr, err := runHelper(t, stateDir, report, "report", "submit")
	if err == nil || !strings.Contains(stderr, "unknown fields") {
		t.Fatalf("private report error = %v, stderr %q", err, stderr)
	}
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show unresolved receipt: %v: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"state":"created"`) || strings.Contains(stdout, "must never be persisted") {
		t.Fatalf("invalid report changed or leaked into receipt: %s", stdout)
	}
}

func TestInputRequestDeliveryDoesNotResolveItsMeaning(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-input")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	request := testMessage(binding, "input-1", "input_request", map[string]any{
		"request_type":    "clarification",
		"question":        "Which public source should govern the comparison?",
		"blocking_reason": "The two public sources use different terms.",
	})
	assertHelperOutcome(t, stateDir, "recorded", request, "input", "submit")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "seen-1", "message_id": "input-1",
	}, "inbox", "consume")

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show input receipt: %v: %s", err, stderr)
	}
	if !strings.Contains(stdout, `"input_state":"seen"`) || strings.Contains(stdout, `"input_state":"resolved"`) {
		t.Fatalf("delivery must mark input seen but unresolved: %s", stdout)
	}
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "accepted-1", "message_id": "input-1",
	}, "input", "accept")
}

func TestConcurrentWritersShareOnePrivateLockDomainWithoutLosingEvents(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-concurrent")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")

	const writers = 12
	start := make(chan struct{})
	errors := make(chan error, writers)
	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			input := map[string]any{
				"delegation_id": binding["delegation_id"],
				"event_id":      fmt.Sprintf("route-%d", index),
				"routing":       map[string]any{"target": fmt.Sprintf("T-%d", index), "recovery": "machine_local_inbox"},
			}
			stdout, stderr, err := runHelper(t, stateDir, input, "receipt", "route")
			if err != nil || !strings.Contains(stdout, `"outcome":"recorded"`) {
				errors <- fmt.Errorf("writer %d: %v: %s%s", index, err, stdout, stderr)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}

	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil {
		t.Fatalf("show concurrent receipt: %v: %s", err, stderr)
	}
	var receipt struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	if len(receipt.Events) != writers+1 {
		t.Fatalf("event count = %d, want %d; concurrent mutation lost an event", len(receipt.Events), writers+1)
	}
	for path, want := range map[string]os.FileMode{
		stateDir:                                     0o700,
		filepath.Join(stateDir, "receipts.json"):     0o600,
		filepath.Join(stateDir, "lifecycle.json"):    0o600,
		filepath.Join(stateDir, "experimental.lock"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(path), info.Mode().Perm(), want)
		}
	}
}

func TestMCPServerExposesOnlySchemaLimitedSemanticSubmission(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	binding := testBinding("delegation-mcp")
	assertHelperOutcome(t, stateDir, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-mcp", "thinker_report", map[string]any{
		"accepted_role":       true,
		"accepted_exclusions": true,
		"status":              "complete",
		"verdict":             "Synthetic schema check complete.",
		"rationale":           "The MCP request contains only bounded fields.",
		"evidence":            []any{"synthetic fixture"},
		"assumptions":         []any{},
		"unsupported_claims":  []any{},
		"blockers":            []any{},
		"verification":        []any{"MCP protocol fixture"},
		"changed_artifacts":   []any{},
		"references":          []any{},
	})
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
		{"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": map[string]any{"name": "submit_report", "arguments": report}},
	}
	var input bytes.Buffer
	for _, message := range messages {
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(data)
		input.WriteByte('\n')
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", helper, "--state-dir", stateDir, "--isolated-test-state", "mcp", "serve", "--delegation-id", binding["delegation_id"].(string))
	command.Env = append(os.Environ(), "AMUX_CLAUDE_DELEGATION_TESTING=1")
	command.Stdin = &input
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP server failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 3 {
		t.Fatalf("MCP responses = %d, want 3\n%s", len(lines), output)
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Result.Tools) != 2 || listed.Result.Tools[0].Name != "submit_report" || listed.Result.Tools[1].Name != "submit_input_request" {
		t.Fatalf("MCP tools = %#v, want only report and input-request submission", listed.Result.Tools)
	}
	if !strings.Contains(lines[2], `"outcome":"recorded"`) || !strings.Contains(lines[2], `"isError":false`) {
		t.Fatalf("MCP tool result did not record report: %s", lines[2])
	}
}

func TestNotificationFailsClosedAndRunsOnlyAfterDurableReport(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	panePID, _ := startProcessFixture(t, "amp", "threads", "continue", "T-origin")
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  display-message) printf 'Amp\tcoordinator\t@9\t%%9\t%s\t%s\tamp\n' "$PANE_PID" "$TARGET_WORKDIR" ;;
  send-keys)
    grep -q '"state":"valid_report"' "$STATE_DIR/receipts.json"
    printf '%s\n' "$*" >> "$TMUX_LOG"
    ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "ps"), `#!/bin/sh
set -eu
case "$*" in
  *lstart=*) printf '%s\n' 'Fri Jul 17 12:00:00 2026' ;;
  *comm=*) printf '%s\n' '/usr/local/bin/amp' ;;
  *command=*) printf '%s\n' 'amp threads continue T-origin' ;;
  *) exit 2 ;;
esac
`)
	environment := append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"), "TMUX_LOG="+logPath, "STATE_DIR="+stateDir, "TARGET_WORKDIR="+stateDir, fmt.Sprintf("PANE_PID=%d", panePID))
	binding := testBinding("delegation-notify")
	binding["workdir"] = stateDir
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "T-origin", "recovery": "machine_local_inbox"},
	}, "receipt", "create")
	report := testMessage(binding, "report-notify", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Synthetic notification fixture.", "rationale": "No content is sent to the pane.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{"durable-before-send fixture"}, "changed_artifacts": []any{}, "references": []any{},
	})
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", report, "report", "submit")

	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "amp", "inspect", "--pane", "%9", "--origin-thread", "T-origin")
	if err != nil {
		t.Fatalf("inspect Amp target: %v: %s", err, stderr)
	}
	var target map[string]any
	if err := json.Unmarshal([]byte(stdout), &target); err != nil {
		t.Fatal(err)
	}
	notify := map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "notify-1", "message_id": "report-notify", "target": target,
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "notified", notify, "notify", "amp-pane")
	unsafeID := cloneJSONMap(t, notify)
	unsafeID["event_id"] = "notify\nunsafe"
	_, stderr, err = runHelperEnv(t, stateDir, environment, unsafeID, "notify", "amp-pane")
	if err == nil || !strings.Contains(stderr, "control characters") {
		t.Fatalf("unsafe notification ID error = %v, stderr %q", err, stderr)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "AMUX_CLAUDE_REPORT delegation_sha256=") || !strings.Contains(string(log), "message_sha256=") {
		t.Fatalf("wake-up token missing or contains semantic content: %s", log)
	}
	receiptPath := filepath.Join(stateDir, "receipts.json")
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var interruptedStore map[string]any
	if err := json.Unmarshal(receiptBytes, &interruptedStore); err != nil {
		t.Fatal(err)
	}
	receipt := interruptedStore["receipts"].([]any)[0].(map[string]any)
	events := receipt["events"].([]any)
	withoutResult := events[:0]
	for _, raw := range events {
		if raw.(map[string]any)["kind"] != "notification_result" {
			withoutResult = append(withoutResult, raw)
		}
	}
	receipt["events"] = withoutResult
	interruptedBytes, err := json.Marshal(interruptedStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, interruptedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "unavailable", notify, "notify", "amp-pane")
	logAfterRecovery, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(logAfterRecovery) != string(log) {
		t.Fatal("interrupted notification recovery resent a wake-up")
	}
	recovered, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil || !strings.Contains(recovered, "interrupted before a durable result") {
		t.Fatalf("interrupted notification result was not persisted: %v: %s%s", err, recovered, stderr)
	}

	stale := cloneJSONMap(t, notify)
	stale["event_id"] = "notify-stale"
	stale["target"].(map[string]any)["process_identity"] = "changed-process-start"
	assertHelperOutcomeEnv(t, stateDir, environment, "unavailable", stale, "notify", "amp-pane")
	logAfter, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(logAfter) != string(log) {
		t.Fatal("stale target received a wake-up")
	}
	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil || !strings.Contains(stdout, `"state":"valid_report"`) {
		t.Fatalf("notification incorrectly established delivery: %v: %s%s", err, stdout, stderr)
	}
}

func TestParkingReverifiesExactClaudeIncarnationAfterAcknowledgement(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	identityUnavailable := filepath.Join(t.TempDir(), "identity-unavailable")
	sessionID := "550e8400-e29b-41d4-a716-446655440000"
	expectedArguments := []string{"--session-id", sessionID, "argument with spaces", "literal'quote"}
	panePID, paneExecutable := startProcessFixture(t, "claude", expectedArguments...)
	expectedArgvDigest := nulDigest(expectedArguments)
	expectedLauncherIdentity := testExecutableIdentity(t, paneExecutable)
	startCommand := "exec claude --session-id " + sessionID
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  display-message)
    test ! -e "$IDENTITY_UNAVAILABLE"
    printf 'Claude\tthinker\t%s\t%s\t%s\t%s\t2.1.212\t"%s"\n' "$CLAUDE_WINDOW_ID" "$CLAUDE_PANE_ID" "$PANE_PID" "$TARGET_WORKDIR" "$START_COMMAND"
    ;;
  list-panes) exit 0 ;;
  kill-pane) printf '%s\n' "$*" >> "$TMUX_LOG" ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "ps"), `#!/bin/sh
set -eu
case "$*" in
  *lstart=*) printf '%s\n' 'Fri Jul 17 12:01:00 2026' ;;
  *comm=*) printf '%s\n' '/usr/local/bin/claude' ;;
  *command=*) printf 'claude --session-id %s\n' "$CLAUDE_SESSION_ID" ;;
  *) exit 2 ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TMUX_LOG="+logPath,
		"TARGET_WORKDIR="+stateDir,
		"START_COMMAND="+startCommand,
		"CLAUDE_SESSION_ID="+sessionID,
		"IDENTITY_UNAVAILABLE="+identityUnavailable,
		fmt.Sprintf("PANE_PID=%d", panePID),
		"CLAUDE_WINDOW_ID=@10",
		"CLAUDE_PANE_ID=%10",
	)
	binding := testBinding("delegation-park")
	binding["workdir"] = stateDir
	digest := sha256.Sum256([]byte(startCommand))
	binding["launch_command_digest"] = fmt.Sprintf("%x", digest)
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	launchIdentity := inspectTestClaudeIdentity(t, environment, "%10", sessionID, expectedArgvDigest, expectedLauncherIdentity, testExecutableObjectIdentity(t, paneExecutable))
	if runtime.GOOS == "linux" {
		delete(launchIdentity, "process_executable_object_identity")
	}
	recordTestLaunch(t, stateDir, binding["delegation_id"].(string), launchIdentity, expectedArgvDigest, expectedLauncherIdentity, testExecutableObjectIdentity(t, paneExecutable))
	wrongEnvironment := replaceEnvironment(environment, "CLAUDE_PANE_ID", "%11")
	wrongAcquire := map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "acquire-wrong-pane", "pane_id": "%11", "claude_session_id": sessionID,
	}
	_, stderr, err := runHelperEnv(t, stateDir, wrongEnvironment, wrongAcquire, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "pane created by this receipt") {
		t.Fatalf("wrong launch pane acquisition error = %v, stderr %q", err, stderr)
	}
	substitutedPID, _ := startProcessFixture(t, "claude", "--session-id", sessionID, "dropped policy")
	substitutedEnvironment := replaceEnvironment(environment, "PANE_PID", fmt.Sprint(substitutedPID))
	_, stderr, err = runHelperEnv(t, stateDir, substitutedEnvironment, map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "acquire-substituted-argv", "pane_id": "%10", "claude_session_id": sessionID,
	}, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "argv does not match immutable launch intent") {
		t.Fatalf("substituted argv acquisition error = %v, stderr %q", err, stderr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("substituted process was killed during rejected acquisition: %v", err)
	}
	acquire := map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "acquire-1", "pane_id": "%10", "claude_session_id": sessionID,
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", acquire, "session", "acquire")
	if runtime.GOOS == "linux" {
		stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
		if err != nil {
			t.Fatalf("show Linux acquisition: %v: %s", err, stderr)
		}
		var acquired map[string]any
		if err := json.Unmarshal([]byte(stdout), &acquired); err != nil {
			t.Fatal(err)
		}
		identity := acquired["session_identity"].(map[string]any)
		if _, present := identity["process_executable_object_identity"]; present {
			t.Fatalf("v0.2.17 Linux session identity gained a retirement-only executable object: %#v", identity)
		}
		if !strings.HasPrefix(identity["process_identity"].(string), "linux:") {
			t.Fatalf("Linux process identity = %q, want kernel start/executable identity", identity["process_identity"])
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(panePID), "cmdline"))
		if err != nil {
			t.Fatal(err)
		}
		wantDigest := fmt.Sprintf("%x", sha256.Sum256(cmdline[:len(cmdline)-1]))
		if identity["process_command_digest"] != wantDigest {
			t.Fatalf("Linux argv digest = %q, want NUL-delimited /proc digest %q", identity["process_command_digest"], wantDigest)
		}
	}
	if err := os.WriteFile(identityUnavailable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "duplicate", acquire, "session", "acquire")
	conflictingAcquire := cloneJSONMap(t, acquire)
	conflictingAcquire["pane_id"] = "%11"
	_, stderr, err = runHelperEnv(t, stateDir, environment, conflictingAcquire, "session", "acquire")
	if err == nil || !strings.Contains(stderr, "conflicting event") {
		t.Fatalf("conflicting acquisition replay error = %v, stderr %q", err, stderr)
	}
	if err := os.Remove(identityUnavailable); err != nil {
		t.Fatal(err)
	}
	report := testMessage(binding, "report-park", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "Synthetic parking fixture.", "rationale": "Identity is checked before the tmux mutation.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{"synthetic exact identity"}, "changed_artifacts": []any{}, "references": []any{},
	})
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", report, "report", "submit")
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "deliver-park", "message_id": "report-park",
	}, "inbox", "consume")
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "ack-park", "message_id": "report-park",
	}, "report", "acknowledge")
	acknowledgedStore, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	recycledEnvironment := replaceEnvironment(environment, "CLAUDE_WINDOW_ID", "@99")
	_, stderr, err = runHelperEnv(t, stateDir, recycledEnvironment, map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-recycled-pane",
	}, "session", "park")
	if err == nil || !strings.Contains(stderr, "identity changed") {
		t.Fatalf("recycled pane parking error = %v, stderr %q", err, stderr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("recycled pane was killed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), acknowledgedStore, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, stateDir, substitutedEnvironment, map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-reused-pid",
	}, "session", "park")
	if err == nil || !strings.Contains(stderr, "argv does not match immutable launch intent") {
		t.Fatalf("reused PID parking error = %v, stderr %q", err, stderr)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("changed process incarnation was killed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), acknowledgedStore, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-verified",
	}, "session", "park")
	log, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(log), "kill-pane -t %10") {
		t.Fatalf("verified park did not target exact window: %v: %s", err, log)
	}
	var interrupted map[string]any
	if err := json.Unmarshal(acknowledgedStore, &interrupted); err != nil {
		t.Fatal(err)
	}
	receipt := interrupted["receipts"].([]any)[0].(map[string]any)
	receipt["events"] = append(receipt["events"].([]any), map[string]any{
		"event_id": "park-recovery", "kind": "park_intent", "identity": receipt["session_identity"], "at": "2026-07-17T12:02:00Z",
	})
	interruptedBytes, err := json.Marshal(interrupted)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), interruptedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identityUnavailable, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"delegation_id": binding["delegation_id"], "event_id": "park-recovery", "recover": true,
	}, "session", "park")
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
	if err != nil || !strings.Contains(stdout, `"state":"verified_parked"`) || !strings.Contains(stdout, sessionID) || !strings.Contains(stdout, `"cleanup_eligible_at"`) || !strings.Contains(stdout, `"recovered_absence":true`) {
		t.Fatalf("parked receipt does not preserve session/eligibility: %v: %s%s", err, stdout, stderr)
	}
}

func TestLinuxNodeDescriptorSubstitutionBlocksAcquisitionAndParkingKill(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux node and bun process forms use proc descriptor identity")
	}
	for _, processForm := range []string{"node", "bun"} {
		for _, lifecycle := range []string{"acquisition", "parking"} {
			t.Run(processForm+"-"+lifecycle, func(t *testing.T) {
				stateDir := t.TempDir()
				binDir := t.TempDir()
				logPath := filepath.Join(t.TempDir(), "tmux.log")
				sessionID := "750e8400-e29b-41d4-a716-446655440000"
				expectedArguments := []string{"--session-id", sessionID, "--strict-mcp-config"}
				panePID, nodeExecutable, launcherPath, launcherFile := startNodeDescriptorProcessFixture(t, processForm, expectedArguments...)
				expectedLauncherIdentity := testExecutableIdentity(t, launcherPath)
				expectedLauncherObjectIdentity := testExecutableObjectIdentity(t, launcherPath)
				startCommand := "exec " + processForm + " /proc/self/fd/3 --session-id " + sessionID + " --strict-mcp-config"
				writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  display-message) printf 'Claude\tthinker\t@30\t%%30\t%s\t%s\t%s\t%s\n' "$PANE_PID" "$TARGET_WORKDIR" "$PROCESS_FORM" "$START_COMMAND" ;;
  kill-pane) printf '%s\n' "$*" >> "$TMUX_LOG" ;;
  list-panes) exit 0 ;;
  *) exit 2 ;;
esac
`)
				environment := append(os.Environ(),
					"PATH="+binDir+":"+os.Getenv("PATH"), "TMUX_LOG="+logPath,
					"TARGET_WORKDIR="+stateDir, "START_COMMAND="+startCommand,
					"PROCESS_FORM="+processForm,
					fmt.Sprintf("PANE_PID=%d", panePID),
				)
				binding := testBinding("delegation-" + processForm + "-descriptor-" + lifecycle)
				binding["workdir"] = stateDir
				binding["launch_command_digest"] = fmt.Sprintf("%x", sha256.Sum256([]byte(startCommand)))
				assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
					"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
				}, "receipt", "create")
				launchIdentity := inspectTestClaudeIdentity(t, environment, "%30", sessionID, nulDigest(expectedArguments), expectedLauncherIdentity, expectedLauncherObjectIdentity)
				if launchIdentity["process_executable_identity"] != testExecutableIdentity(t, nodeExecutable) {
					t.Fatalf("%s fixture executable identity mismatch", processForm)
				}
				delete(launchIdentity, "process_executable_object_identity")
				recordTestLaunch(t, stateDir, binding["delegation_id"].(string), launchIdentity, nulDigest(expectedArguments), expectedLauncherIdentity, expectedLauncherObjectIdentity)
				acquire := map[string]any{
					"delegation_id": binding["delegation_id"], "event_id": "acquire-node-descriptor",
					"pane_id": "%30", "claude_session_id": sessionID,
				}
				if lifecycle == "parking" {
					assertHelperOutcomeEnv(t, stateDir, environment, "recorded", acquire, "session", "acquire")
					report := testMessage(binding, "report-node-descriptor", "thinker_report", map[string]any{
						"accepted_role": true, "accepted_exclusions": true, "status": "complete",
						"verdict": "Synthetic node descriptor fixture.", "rationale": "Full live launcher object identity is required.",
						"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
						"verification": []any{"synthetic exact identity"}, "changed_artifacts": []any{}, "references": []any{},
					})
					assertHelperOutcomeEnv(t, stateDir, environment, "recorded", report, "report", "submit")
					assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
						"delegation_id": binding["delegation_id"], "event_id": "deliver-node-descriptor", "message_id": "report-node-descriptor",
					}, "inbox", "consume")
					assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
						"delegation_id": binding["delegation_id"], "event_id": "ack-node-descriptor", "message_id": "report-node-descriptor",
					}, "report", "acknowledge")
				}
				before, err := launcherFile.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(launcherPath); err != nil {
					t.Fatal(err)
				}
				if err := launcherFile.Truncate(0); err != nil {
					t.Fatal(err)
				}
				if _, err := launcherFile.Seek(0, 0); err != nil {
					t.Fatal(err)
				}
				if _, err := launcherFile.WriteString("substituted same-inode launcher"); err != nil {
					t.Fatal(err)
				}
				after, err := launcherFile.Stat()
				if err != nil {
					t.Fatal(err)
				}
				if before.Sys().(*syscall.Stat_t).Ino != after.Sys().(*syscall.Stat_t).Ino {
					t.Fatal("same-inode lifecycle fixture changed inode")
				}
				if lifecycle == "acquisition" {
					_, stderr, err := runHelperEnv(t, stateDir, environment, acquire, "session", "acquire")
					if err == nil || !strings.Contains(stderr, "launcher content does not match immutable launch intent") {
						t.Fatalf("same-inode acquisition error = %v, stderr %q", err, stderr)
					}
					stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "receipt", "show", "--delegation-id", binding["delegation_id"].(string))
					if err != nil || strings.Contains(stdout, `"session_identity"`) {
						t.Fatalf("rejected acquisition changed durable identity: %v: %s%s", err, stdout, stderr)
					}
				} else {
					_, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{
						"delegation_id": binding["delegation_id"], "event_id": "park-node-descriptor",
					}, "session", "park")
					if err == nil || !strings.Contains(stderr, "launcher content does not match immutable launch intent") {
						t.Fatalf("same-inode parking error = %v, stderr %q", err, stderr)
					}
				}
				if _, err := os.Stat(logPath); !os.IsNotExist(err) {
					t.Fatalf("substituted node process was killed: %v", err)
				}
			})
		}
	}
}

func TestLaunchPlanRejectsMissingTargetSessionWithoutMutation(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "target tmux session does not exist") {
		t.Fatalf("missing-session plan error = %v, stderr %q", err, stderr)
	}
	entries, err := os.ReadDir(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("missing-session plan mutated private state: %#v", entries)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("missing-session plan created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLinuxLaunchDoesNotClaimMutatingDelegation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific capability boundary")
	}
	fixture := newLaunchFixture(t)
	request := cloneJSONMap(t, fixture.request)
	request["workflow"] = "mutating"
	delete(request, "expected_launch_policy_digest")
	request["baseline_branch"] = "delegate"
	request["writer_owner"] = "claude"
	request["integration_owner"] = "amp"
	request["coordinator_write_frozen"] = true
	request["shared_writable"] = false
	request["handoff"] = "one_clean_local_commit"
	request["capacity_request"] = map[string]any{}
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, request, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "mutating Claude launch remains available only on Darwin") {
		t.Fatalf("Linux mutating launch error = %v, stderr %q", err, stderr)
	}
}

func TestLaunchExecutionRejectsDisappearedTargetSessionBeforeIntent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	var plan struct {
		PacketDigest        string `json:"packet_digest"`
		LaunchPolicyDigest  string `json:"launch_policy_digest"`
		LaunchCommandDigest string `json:"launch_command_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.LaunchPolicyDigest != "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445" {
		t.Fatalf("read-only launch policy digest changed: %s", plan.LaunchPolicyDigest)
	}
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.disappearAfterCheck, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "target tmux session does not exist") {
		t.Fatalf("disappeared-session execute error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) || bytes.Contains(after, []byte(`"kind":"launch_intent"`)) {
		t.Fatalf("disappeared-session execution changed the pre-launch receipt:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("disappeared-session execution created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("disappeared-session execution created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestReadOnlyLaunchRevalidatesWorktreeImmediatelyBeforeIntent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.dirtyAfterPreflight, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "read-only thinker worktree must be clean before launch") {
		t.Fatalf("dirty final read-only worktree error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("dirty read-only worktree changed receipt before intent:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("dirty read-only worktree created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("dirty read-only worktree created tmux window: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLaunchExecutionRejectsUntransportablePacketWithoutChangingDurableState(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	var plan struct {
		PacketDigest        string `json:"packet_digest"`
		LaunchPolicyDigest  string `json:"launch_policy_digest"`
		LaunchCommandDigest string `json:"launch_command_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.request["packet_file"].(string), bytes.Repeat([]byte("x"), 262145), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "launch packet must contain 1 to 262144 bytes") {
		t.Fatalf("oversized packet execute error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("deterministic packet rejection changed durable receipt bytes:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("deterministic packet rejection created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("deterministic packet rejection created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLinuxPlanRejectsPacketBeyondProcessStringLimitWithoutMutation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux has a kernel per-string exec limit")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	packetSize := os.Getpagesize() * 32
	if packetSize > 262144 {
		t.Skip("platform process string limit exceeds the packet protocol maximum")
	}
	if err := os.WriteFile(fixture.request["packet_file"].(string), bytes.Repeat([]byte("x"), packetSize), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "platform process string limit") {
		t.Fatalf("Linux process string limit error = %v, stderr %q", err, stderr)
	}
	entries, err := os.ReadDir(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("Linux process string rejection mutated private state: %#v", entries)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("Linux process string rejection created tmux window: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLinuxExecutionRejectsAggregateExecBudgetWithoutChangingDurableState(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("test lowers Linux stack-backed ARG_MAX")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.request["packet_file"].(string), bytes.Repeat([]byte("x"), 100*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runHelperEnvWithStackLimit(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err == nil || !strings.Contains(stderr, "conservative platform process budget") {
		t.Fatalf("aggregate plan exec budget error = %v, stderr %q", err, stderr)
	}
	entries, err := os.ReadDir(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("aggregate plan rejection mutated private state: %#v", entries)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan at normal exec budget: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnvWithStackLimit(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "conservative platform process budget") {
		t.Fatalf("aggregate exec budget error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("aggregate exec budget rejection changed durable receipt bytes:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("aggregate exec budget rejection created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("aggregate exec budget rejection created tmux window: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLaunchExecutionRejectsOverBudgetTmuxEnvironmentWithoutMutation(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("tmux launch budget requires a supported launch platform")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.oversizedTmuxEnv, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "target tmux environment exceeds") {
		t.Fatalf("tmux environment budget error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("tmux environment rejection changed durable receipt bytes:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("tmux environment rejection created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("tmux environment rejection created tmux window: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLaunchExecutionRejectsSameContentPacketReplacementBeforeIntent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("descriptor identity requires a supported launch platform")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	receiptPath := createPlannedLaunchReceipt(t, fixture)
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	packetPath := fixture.request["packet_file"].(string)
	packet, err := os.ReadFile(packetPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(t.TempDir(), "replacement-packet")
	if err := os.WriteFile(replacement, packet, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, packetPath); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "launch_command_digest does not match immutable receipt binding") {
		t.Fatalf("same-content packet replacement error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("same-content packet replacement changed durable state:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("same-content packet replacement created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("same-content packet replacement created a tmux window: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestDiagnosticsSanitizeClaudeProbeEnvironment(t *testing.T) {
	fixture := newLaunchFixture(t)
	probeLog := filepath.Join(t.TempDir(), "diagnostic-probes.env")
	fixture.environment = append(fixture.environment,
		"GH_TOKEN=must-be-removed", "GITHUB_TOKEN=must-be-removed", "GITLAB_TOKEN=must-be-removed",
		"BENIGN_SENTINEL=must-survive", "PROBE_ENV_LOG="+probeLog,
	)
	enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
	_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, map[string]any{}, "diagnose")
	if err != nil {
		t.Fatalf("diagnose: %v: %s", err, stderr)
	}
	probes, err := os.ReadFile(probeLog)
	if err != nil {
		t.Fatal(err)
	}
	result := string(probes)
	probeCount := strings.Count(result, "probe=")
	if probeCount < 2 {
		t.Fatalf("diagnostics did not run expected Claude probes: %s", result)
	}
	for _, removed := range []string{"GH_TOKEN=false:", "GITHUB_TOKEN=false:", "GITLAB_TOKEN=false:"} {
		if strings.Count(result, removed) != probeCount {
			t.Errorf("diagnostic Claude probes exposed credential: %s", result)
		}
	}
	if strings.Count(result, "BENIGN_SENTINEL=true:must-survive") != probeCount {
		t.Errorf("diagnostic Claude probes dropped benign sentinel: %s", result)
	}
}

func TestLaunchExecutionRejectsObjectReplacementDuringExecutePreflight(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("descriptor identity requires a supported launch platform")
	}
	for _, kind := range []string{"packet", "workdir", "executable"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newLaunchFixture(t)
			if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
			marker := filepath.Join(t.TempDir(), "replace-after-preflight")
			var target, replacement string
			switch kind {
			case "packet":
				target = fixture.request["packet_file"].(string)
				data, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				replacement = filepath.Join(t.TempDir(), "packet")
				if err := os.WriteFile(replacement, data, 0o600); err != nil {
					t.Fatal(err)
				}
			case "workdir":
				target = fixture.request["workdir"].(string)
				replacement = t.TempDir()
			case "executable":
				target = filepath.Join(fixture.binDir, "claude")
				replacement = filepath.Join(t.TempDir(), "claude")
				writeExecutable(t, replacement, "#!/bin/sh\nexit 99\n")
			}
			fixture.environment = append(fixture.environment,
				"REPLACE_AFTER_PREFLIGHT="+marker,
				"REPLACE_KIND="+kind,
				"REPLACE_TARGET="+target,
				"REPLACE_WITH="+replacement,
			)
			receiptPath := createPlannedLaunchReceipt(t, fixture)
			before, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(marker, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
			if err == nil || !strings.Contains(stderr, "changed before intent") {
				t.Fatalf("execute-preflight %s replacement error = %v, stderr %q", kind, err, stderr)
			}
			after, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("execute-preflight %s replacement changed durable state:\nbefore: %s\nafter:  %s", kind, before, after)
			}
			if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
				t.Fatalf("execute-preflight %s replacement created runtime state: %v", kind, err)
			}
			if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
				t.Fatalf("execute-preflight %s replacement created a tmux window: %s", kind, log)
			} else if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		})
	}
}

func TestLaunchTransportRejectsExecutableAndWorkdirReplacementAfterIntent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("descriptor transport requires a supported launch platform")
	}
	for _, test := range []struct {
		name    string
		prepare func(t *testing.T, fixture launchFixture) []string
	}{
		{name: "executable", prepare: func(t *testing.T, fixture launchFixture) []string {
			wrongProcessLog := filepath.Join(t.TempDir(), "wrong-process")
			replacement := filepath.Join(t.TempDir(), "replacement-claude")
			writeExecutable(t, replacement, "#!/bin/sh\nprintf wrong >\"$WRONG_PROCESS_LOG\"\nsleep 10\n")
			return []string{
				"REPLACE_EXECUTABLE_WITH=" + replacement,
				"CLAUDE_PATH=" + filepath.Join(fixture.binDir, "claude"),
				"WRONG_PROCESS_LOG=" + wrongProcessLog,
				"NO_PROCESS_LOG=" + wrongProcessLog,
			}
		}},
		{name: "workdir", prepare: func(t *testing.T, fixture launchFixture) []string {
			replacement := t.TempDir()
			argvLog := filepath.Join(t.TempDir(), "wrong-workdir-process")
			return []string{"REPLACE_WORKDIR_WITH=" + replacement, "ARGV_LOG=" + argvLog, "NO_PROCESS_LOG=" + argvLog}
		}},
		{name: "packet", prepare: func(t *testing.T, fixture launchFixture) []string {
			packetPath := fixture.request["packet_file"].(string)
			data, err := os.ReadFile(packetPath)
			if err != nil {
				t.Fatal(err)
			}
			replacement := filepath.Join(t.TempDir(), "replacement-packet")
			if err := os.WriteFile(replacement, data, 0o600); err != nil {
				t.Fatal(err)
			}
			argvLog := filepath.Join(t.TempDir(), "wrong-packet-process")
			return []string{"REPLACE_PACKET_WITH=" + replacement, "PACKET_PATH=" + packetPath, "ARGV_LOG=" + argvLog, "NO_PROCESS_LOG=" + argvLog}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLaunchFixture(t)
			permitSealedRuntimeTempCleanup(t, fixture.stateDir)
			if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
			fixture.environment = append(fixture.environment, test.prepare(t, fixture)...)
			replacementMarker := filepath.Join(t.TempDir(), "replacement-succeeded")
			fixture.environment = append(fixture.environment, "REPLACEMENT_SUCCEEDED="+replacementMarker)
			receiptPath := createPlannedLaunchReceipt(t, fixture)
			_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
			if err == nil || !strings.Contains(stderr, "startup was not verified") {
				t.Fatalf("post-intent %s replacement error = %v, stderr %q", test.name, err, stderr)
			}
			if _, err := os.Stat(replacementMarker); err != nil {
				t.Fatalf("post-intent %s replacement was not performed: %v", test.name, err)
			}
			paneOutput, err := os.ReadFile(environmentValue(t, fixture.environment, "PANE_OUTPUT"))
			if err != nil {
				t.Fatal(err)
			}
			expectedTransportError := map[string]string{
				"executable": "executable object changed",
				"workdir":    "workdir object changed",
				"packet":     "packet object changed",
			}[test.name]
			if !bytes.Contains(paneOutput, []byte(expectedTransportError)) {
				t.Fatalf("post-intent %s replacement did not reach transport identity rejection: %s", test.name, paneOutput)
			}
			receipt, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(receipt, []byte(`"kind":"launch_intent"`)) || bytes.Contains(receipt, []byte(`"kind":"launch_completed"`)) {
				t.Fatalf("post-intent replacement did not remain indeterminate: %s", receipt)
			}
			noProcessLog := environmentValue(t, fixture.environment, "NO_PROCESS_LOG")
			if _, err := os.Stat(noProcessLog); !os.IsNotExist(err) {
				t.Fatalf("post-intent replacement launched a wrong process: %v", err)
			}
			if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "kill-pane") {
				t.Fatalf("post-intent replacement killed a process: %s", log)
			}
		})
	}
}

func TestLaunchExecutionRejectsUnsafePacketFilesWithoutMutationOrPathLeakage(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("private descriptor validation requires a supported launch platform")
	}
	for _, test := range []struct {
		name   string
		create func(t *testing.T) string
	}{
		{name: "symlink", create: func(t *testing.T) string {
			target := filepath.Join(t.TempDir(), "target-packet")
			if err := os.WriteFile(target, []byte("packet"), 0o600); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "private-packet-link")
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "fifo", create: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "private-packet-fifo")
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "device", create: func(t *testing.T) string { return "/dev/null" }},
		{name: "wrong mode", create: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "private-packet-mode")
			if err := os.WriteFile(path, []byte("packet"), 0o640); err != nil {
				t.Fatal(err)
			}
			return path
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLaunchFixture(t)
			permitSealedRuntimeTempCleanup(t, fixture.stateDir)
			if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
			if err != nil {
				t.Fatalf("launch plan: %v: %s", err, stderr)
			}
			plan := decodeJSONMap(t, stdout)
			binding := testBinding(fixture.request["delegation_id"].(string))
			binding["workdir"] = fixture.request["workdir"]
			binding["base"] = fixture.request["base"]
			binding["packet_digest"] = plan["packet_digest"]
			binding["launch_policy_digest"] = plan["launch_policy_digest"]
			binding["launch_command_digest"] = plan["launch_command_digest"]
			assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
				"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
			}, "receipt", "create")
			receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
			before, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			unsafePath := test.create(t)
			request := cloneJSONMap(t, fixture.request)
			request["packet_file"] = unsafePath
			_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, request, "launch", "execute")
			if err == nil || (!strings.Contains(stderr, "launch packet is unavailable or unsafe") && !strings.Contains(stderr, "launch packet must be one owner-only regular file")) {
				t.Fatalf("unsafe packet error = %v, stderr %q", err, stderr)
			}
			if strings.Contains(stderr, unsafePath) {
				t.Fatalf("unsafe packet error leaked private path: %s", stderr)
			}
			after, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("unsafe packet changed durable receipt bytes:\nbefore: %s\nafter:  %s", before, after)
			}
			if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
				t.Fatalf("unsafe packet created runtime state: %v", err)
			}
			if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
				t.Fatalf("unsafe packet created tmux window: %s", log)
			} else if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		})
	}
}

func TestLaunchExecutionRejectsPlanIdentityMismatchBeforeIntent(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	mismatched := cloneJSONMap(t, fixture.request)
	mismatched["claude_session_id"] = "b65f3784-f8e7-4634-b1cb-32ce61dd3555"
	_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, mismatched, "launch", "execute")
	if err == nil || !strings.Contains(stderr, "launch launch_command_digest does not match immutable receipt binding") {
		t.Fatalf("plan identity mismatch error = %v, stderr %q", err, stderr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("plan identity mismatch changed durable receipt bytes:\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(filepath.Join(fixture.stateDir, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("plan identity mismatch created runtime state: %v", err)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && strings.Contains(string(log), "new-window") {
		t.Fatalf("plan identity mismatch created a tmux window:\n%s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestLaunchStartupFailureRemainsIndeterminateWithoutFalseCompletion(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	for _, test := range []struct {
		name        string
		environment string
		expected    string
	}{
		{name: "transport exits before Claude startup", environment: "STARTUP_EXIT=1", expected: "Claude startup was not verified before timeout"},
		{name: "same session drops policy argv", environment: "SUBSTITUTE_ARGV=1", expected: "Claude startup was not verified before timeout"},
		{name: "tmux returns malformed identity after starting transport", environment: "MALFORMED_NEW_WINDOW=1", expected: "tmux launch did not return one exact session/window/pane identity"},
		{name: "tmux returns failure after starting transport", environment: "FAILED_NEW_WINDOW=1", expected: "run tmux"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLaunchFixture(t)
			permitSealedRuntimeTempCleanup(t, fixture.stateDir)
			if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			environmentLog := filepath.Join(t.TempDir(), "startup.env")
			fixture.environment = append(fixture.environment, test.environment, "ENV_LOG="+environmentLog)
			enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
			stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
			if err != nil {
				t.Fatalf("launch plan: %v: %s", err, stderr)
			}
			plan := decodeJSONMap(t, stdout)
			binding := testBinding(fixture.request["delegation_id"].(string))
			binding["workdir"] = fixture.request["workdir"]
			binding["base"] = fixture.request["base"]
			binding["packet_digest"] = plan["packet_digest"]
			binding["launch_policy_digest"] = plan["launch_policy_digest"]
			binding["launch_command_digest"] = plan["launch_command_digest"]
			assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
				"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
			}, "receipt", "create")
			_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
			if err == nil || !strings.Contains(stderr, test.expected) {
				environment, _ := os.ReadFile(environmentLog)
				t.Fatalf("startup failure error = %v, stderr %q, environment %q", err, stderr, environment)
			}
			if runtime.GOOS == "darwin" {
				runtimeRoot := filepath.Join(fixture.stateDir, "runtime")
				runtimeEntries, err := os.ReadDir(runtimeRoot)
				if err != nil || len(runtimeEntries) != 1 {
					t.Fatalf("inspect isolated runtime: entries=%v error=%v", runtimeEntries, err)
				}
				for _, path := range []string{
					fixture.stateDir,
					runtimeRoot,
					filepath.Join(runtimeRoot, runtimeEntries[0].Name()),
				} {
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if info.Mode().Perm() != 0o700 {
						t.Fatalf("failed Darwin startup left %s mode %o, want 700", filepath.Base(path), info.Mode().Perm())
					}
				}
				if firstPIDBytes, readErr := os.ReadFile(environmentValue(t, fixture.environment, "PANE_PID_FILE")); readErr == nil {
					if firstPID, parseErr := strconv.Atoi(string(firstPIDBytes)); parseErr == nil {
						t.Cleanup(func() { _ = syscall.Kill(firstPID, syscall.SIGKILL) })
					}
				}

				second := fixture
				second.request = cloneJSONMap(t, fixture.request)
				second.request["delegation_id"] = "delegation-after-indeterminate-startup"
				second.request["event_id"] = "launch-after-indeterminate-startup"
				second.request["claude_session_id"] = "650e8400-e29b-41d4-a716-446655440001"
				second.environment = nil
				for _, value := range fixture.environment {
					if value != "STARTUP_EXIT=1" && value != "SUBSTITUTE_ARGV=1" && value != "MALFORMED_NEW_WINDOW=1" && value != "FAILED_NEW_WINDOW=1" {
						second.environment = append(second.environment, value)
					}
				}
				createPlannedLaunchReceipt(t, second)
				stdout, stderr, err := runHelperEnv(t, second.stateDir, second.environment, second.request, "launch", "execute")
				if err != nil || !strings.Contains(stdout, `"outcome":"launched"`) {
					t.Fatalf("distinct delegation after indeterminate Darwin startup = %v: %s%s", err, stdout, stderr)
				}
			}
			receiptBytes, err := os.ReadFile(filepath.Join(fixture.stateDir, "receipts.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(receiptBytes, []byte(`"event_id":"launch-session-preflight"`)) || bytes.Contains(receiptBytes, []byte(`"operation_event_id":"launch-session-preflight"`)) {
				t.Fatalf("startup failure did not remain indeterminate: %s", receiptBytes)
			}
			_, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute")
			if err == nil || !strings.Contains(stderr, "launch outcome is indeterminate") {
				t.Fatalf("startup failure replay error = %v, stderr %q", err, stderr)
			}
			log, err := os.ReadFile(fixture.tmuxLog)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(log, []byte("new-window")) || bytes.Contains(log, []byte("kill-pane")) {
				t.Fatalf("startup failure tmux mutations = %s", log)
			}
		})
	}
}

func TestLaunchCanonicalizesRelativeClaudePathBeforeAcceptingPlan(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	fixture := newLaunchFixture(t)
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for index := len(fixture.environment) - 1; index >= 0; index-- {
		entry := fixture.environment[index]
		if !strings.HasPrefix(entry, "PATH=") {
			continue
		}
		pathEntries := strings.Split(strings.TrimPrefix(entry, "PATH="), string(os.PathListSeparator))
		relativeBin, err := filepath.Rel(currentDir, pathEntries[0])
		if err != nil {
			t.Fatal(err)
		}
		pathEntries[0] = relativeBin
		fixture.environment[index] = "PATH=" + strings.Join(pathEntries, string(os.PathListSeparator))
		break
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan with relative PATH: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "launched", fixture.request, "launch", "execute")
	runtimeKey := fmt.Sprintf("%x", sha256.Sum256([]byte(fixture.request["delegation_id"].(string))))
	transportBytes, err := os.ReadFile(filepath.Join(fixture.stateDir, "runtime", runtimeKey, "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var transport struct {
		Argv []string `json:"argv"`
	}
	if err := json.Unmarshal(transportBytes, &transport); err != nil {
		t.Fatal(err)
	}
	if len(transport.Argv) == 0 || !filepath.IsAbs(transport.Argv[0]) {
		t.Fatalf("transported Claude executable is not absolute: %#v", transport.Argv)
	}
}

func TestLaunchRequestsRejectMissingOrWrongExpectedPolicyDigestBeforeProbes(t *testing.T) {
	fixture := newLaunchFixture(t)
	for _, test := range []struct {
		name    string
		request map[string]any
		want    string
	}{
		{name: "missing", request: cloneJSONMap(t, fixture.request), want: "expected_launch_policy_digest must be a lowercase SHA-256 value"},
		{name: "wrong", request: cloneJSONMap(t, fixture.request), want: "expected launch policy digest does not match selected workflow"},
	} {
		for _, command := range []string{"plan", "execute"} {
			t.Run(test.name+"_"+command, func(t *testing.T) {
				if test.name == "missing" {
					delete(test.request, "expected_launch_policy_digest")
				} else {
					test.request["expected_launch_policy_digest"] = strings.Repeat("f", 64)
				}
				_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, test.request, "launch", command)
				if err == nil || !strings.Contains(stderr, test.want) {
					t.Fatalf("launch %s error = %v, stderr %q; want %q", command, err, stderr, test.want)
				}
				entries, readErr := os.ReadDir(fixture.stateDir)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if len(entries) != 0 {
					t.Fatalf("rejected launch %s mutated private state: %#v", command, entries)
				}
				if log, readErr := os.ReadFile(fixture.tmuxLog); readErr == nil && len(log) != 0 {
					t.Fatalf("rejected launch %s invoked tmux: %s", command, log)
				} else if readErr != nil && !os.IsNotExist(readErr) {
					t.Fatal(readErr)
				}
			})
		}
	}
}

func TestLaunchPolicyDigestCanFinalizeSelfContainedPacketBeforePlan(t *testing.T) {
	fixture := newLaunchFixture(t)
	stdout, stderr, err := runHelper(t, fixture.stateDir, map[string]any{"workflow": "read_only"}, "launch", "policy-digest")
	if err != nil {
		t.Fatalf("launch policy digest preflight: %v: %s", err, stderr)
	}
	var preflight struct {
		Workflow           string `json:"workflow"`
		LaunchPolicyDigest string `json:"launch_policy_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &preflight); err != nil {
		t.Fatal(err)
	}
	if preflight.Workflow != "read_only" || len(preflight.LaunchPolicyDigest) != 64 {
		t.Fatalf("launch policy digest preflight = %#v", preflight)
	}
	entries, err := os.ReadDir(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("launch policy digest preflight mutated private state: %#v", entries)
	}
	if log, err := os.ReadFile(fixture.tmuxLog); err == nil && len(log) != 0 {
		t.Fatalf("launch policy digest preflight invoked tmux: %s", log)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, stderr, err := runHelper(t, fixture.stateDir, map[string]any{"workflow": "mutating"}, "launch", "policy-digest"); err == nil || !strings.Contains(stderr, "supports only read_only") {
		t.Fatalf("mutating policy digest preflight error = %v, stderr %q", err, stderr)
	}
	mutatingLaunch := cloneJSONMap(t, fixture.request)
	mutatingLaunch["workflow"] = "mutating"
	if _, stderr, err := runHelper(t, fixture.stateDir, mutatingLaunch, "launch", "plan"); err == nil || !strings.Contains(stderr, "unknown fields: expected_launch_policy_digest") {
		t.Fatalf("mutating launch expected policy field error = %v, stderr %q", err, stderr)
	}
	if runtime.GOOS != "darwin" {
		t.Skip("experimental Claude launch is macOS-first")
	}

	packet := fmt.Sprintf(`{"launch_policy_digest":%q,"task":"submit one correlated thinker report"}`, preflight.LaunchPolicyDigest)
	if err := os.WriteFile(fixture.request["packet_file"].(string), []byte(packet), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.request["expected_launch_policy_digest"] = preflight.LaunchPolicyDigest
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan for finalized packet: %v: %s", err, stderr)
	}
	var plan struct {
		PacketDigest        string `json:"packet_digest"`
		LaunchPolicyDigest  string `json:"launch_policy_digest"`
		LaunchCommandDigest string `json:"launch_command_digest"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.LaunchPolicyDigest != preflight.LaunchPolicyDigest {
		t.Fatalf("final launch policy digest = %q, preflight = %q", plan.LaunchPolicyDigest, preflight.LaunchPolicyDigest)
	}

	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")

	report := testMessage(binding, "self-contained-report", "thinker_report", map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict": "The packet supplied every correlation value.", "rationale": "No post-launch input was required.",
		"evidence": []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification": []any{}, "changed_artifacts": []any{}, "references": []any{},
	})
	wrong := cloneJSONMap(t, report)
	wrong["message_id"] = "wrong-policy-report"
	wrong["launch_policy_digest"] = strings.Repeat("f", 64)
	if _, stderr, err := runHelper(t, fixture.stateDir, wrong, "report", "submit"); err == nil || !strings.Contains(stderr, "does not match immutable receipt binding") {
		t.Fatalf("wrong policy digest error = %v, stderr %q", err, stderr)
	}
	omitted := cloneJSONMap(t, report)
	omitted["message_id"] = "omitted-policy-report"
	delete(omitted, "launch_policy_digest")
	if _, stderr, err := runHelper(t, fixture.stateDir, omitted, "report", "submit"); err == nil || !strings.Contains(stderr, "launch_policy_digest must be a non-empty string") {
		t.Fatalf("omitted policy digest error = %v, stderr %q", err, stderr)
	}
	assertHelperOutcome(t, fixture.stateDir, "recorded", report, "report", "submit")
}

func TestReadOnlyLaunchModelSelectionIsCanonicalAndReceiptBound(t *testing.T) {
	fixture := newLaunchFixture(t)
	enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)

	defaultStdout, defaultStderr, err := runHelper(t, fixture.stateDir, map[string]any{"workflow": "read_only"}, "launch", "policy-digest")
	if err != nil {
		t.Fatalf("default policy digest: %v: %s", err, defaultStderr)
	}
	defaultPolicy := decodeJSONMap(t, defaultStdout)
	explicitStdout, explicitStderr, err := runHelper(t, fixture.stateDir, map[string]any{
		"workflow": "read_only", "model": "claude-opus-4-8",
	}, "launch", "policy-digest")
	if err != nil {
		t.Fatalf("explicit policy digest: %v: %s", err, explicitStderr)
	}
	explicitPolicy := decodeJSONMap(t, explicitStdout)
	if explicitPolicy["model"] != "claude-opus-4-8" {
		t.Fatalf("explicit policy model = %#v", explicitPolicy["model"])
	}
	if explicitPolicy["launch_policy_digest"] == defaultPolicy["launch_policy_digest"] {
		t.Fatal("explicit model did not change launch policy digest")
	}
	fableStdout, fableStderr, err := runHelper(t, fixture.stateDir, map[string]any{
		"workflow": "read_only", "model": "claude-fable-5",
	}, "launch", "policy-digest")
	if err != nil {
		t.Fatalf("fable policy digest: %v: %s", err, fableStderr)
	}
	fablePolicy := decodeJSONMap(t, fableStdout)
	if fablePolicy["model"] != "claude-fable-5" ||
		fablePolicy["launch_policy_digest"] == explicitPolicy["launch_policy_digest"] {
		t.Fatalf("fable policy = %#v", fablePolicy)
	}

	for _, test := range []struct {
		name  string
		model any
	}{
		{name: "wrong type", model: 5},
		{name: "malformed", model: "claude-fable-5 --danger"},
		{name: "unknown", model: "claude-unknown-5"},
		{name: "empty", model: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := cloneJSONMap(t, fixture.request)
			request["model"] = test.model
			before, readErr := os.ReadDir(fixture.stateDir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, command := range []string{"plan", "execute"} {
				_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, request, "launch", command)
				if err == nil || !strings.Contains(stderr, "model") || len(stderr) > 1024 {
					t.Fatalf("%s error = %v, stderr %q", command, err, stderr)
				}
			}
			after, readErr := os.ReadDir(fixture.stateDir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("rejected model mutated private state: before %#v after %#v", before, after)
			}
			if log, readErr := os.ReadFile(fixture.tmuxLog); readErr == nil && len(log) != 0 {
				t.Fatalf("rejected model invoked tmux: %s", log)
			} else if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatal(readErr)
			}
		})
	}
	if _, stderr, err := runHelper(t, fixture.stateDir, map[string]any{
		"workflow": "read_only", "model": "claude-unknown-5",
	}, "launch", "policy-digest"); err == nil || !strings.Contains(stderr, "exact approved read-only model") {
		t.Fatalf("invalid policy-digest model error = %v, stderr %q", err, stderr)
	}
	mutatingRequest := cloneJSONMap(t, fixture.request)
	mutatingRequest["workflow"] = "mutating"
	mutatingRequest["model"] = "claude-fable-5"
	if _, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, mutatingRequest, "launch", "plan"); err == nil || !strings.Contains(stderr, "unknown fields") || !strings.Contains(stderr, "model") {
		t.Fatalf("mutating launch model error = %v, stderr %q", err, stderr)
	}
	mutatingBinding := testBinding("mutating-model-rejected")
	mutatingBinding["producer_role"] = "mutating_delegate"
	mutatingBinding["model"] = "claude-fable-5"
	if _, stderr, err := runHelper(t, t.TempDir(), map[string]any{
		"binding": mutatingBinding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create"); err == nil || !strings.Contains(stderr, "unknown fields: model") {
		t.Fatalf("mutating binding model error = %v, stderr %q", err, stderr)
	}

	request := cloneJSONMap(t, fixture.request)
	request["model"] = "claude-opus-4-8"
	request["expected_launch_policy_digest"] = explicitPolicy["launch_policy_digest"]
	if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, request, "launch", "plan")
	if err != nil {
		t.Fatalf("explicit model launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	if plan["model"] != "claude-opus-4-8" || plan["launch_policy_digest"] != explicitPolicy["launch_policy_digest"] {
		t.Fatalf("explicit model plan = %#v", plan)
	}
	binding := testBinding(request["delegation_id"].(string))
	binding["workdir"] = request["workdir"]
	binding["base"] = request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	binding["model"] = "claude-opus-4-8"
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
	beforeReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.Stat(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(fixture.stateDir, "experimental.lock")
	beforeLock, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	omitted := cloneJSONMap(t, request)
	delete(omitted, "model")
	omitted["expected_launch_policy_digest"] = defaultPolicy["launch_policy_digest"]
	if _, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, omitted, "launch", "execute"); err == nil || !strings.Contains(stderr, "model selection does not match immutable receipt binding") {
		t.Fatalf("omitted-after-plan error = %v, stderr %q", err, stderr)
	}
	afterReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterReceipt, beforeReceipt) {
		t.Fatal("omitted-after-plan changed durable receipt bytes")
	}
	if log, readErr := os.ReadFile(fixture.tmuxLog); readErr == nil && len(log) != 0 {
		t.Fatalf("omitted-after-plan invoked tmux: %s", log)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	changed := cloneJSONMap(t, request)
	changed["model"] = "claude-fable-5"
	changed["expected_launch_policy_digest"] = fablePolicy["launch_policy_digest"]
	if _, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, changed, "launch", "execute"); err == nil || !strings.Contains(stderr, "model selection does not match immutable receipt binding") {
		t.Fatalf("changed-after-plan error = %v, stderr %q", err, stderr)
	}
	afterChanged, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterChanged, beforeReceipt) {
		t.Fatal("changed-after-plan changed durable receipt bytes")
	}
	afterState, err := os.Stat(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	afterLock, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if beforeState.Mode() != afterState.Mode() || !beforeState.ModTime().Equal(afterState.ModTime()) ||
		beforeLock.Mode() != afterLock.Mode() || beforeLock.Size() != afterLock.Size() || !beforeLock.ModTime().Equal(afterLock.ModTime()) {
		t.Fatalf("model mismatch changed state or lock metadata: state %v -> %v, lock %v -> %v", beforeState, afterState, beforeLock, afterLock)
	}
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "launched", request, "launch", "execute")
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "duplicate", request, "launch", "execute")
	runtimeKey := fmt.Sprintf("%x", sha256.Sum256([]byte(request["delegation_id"].(string))))
	transportBytes, err := os.ReadFile(filepath.Join(fixture.stateDir, "runtime", runtimeKey, "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var transport struct {
		Argv []string `json:"argv"`
	}
	if err := json.Unmarshal(transportBytes, &transport); err != nil {
		t.Fatal(err)
	}
	arguments := make([][]byte, len(transport.Argv))
	for index, argument := range transport.Argv {
		arguments[index] = []byte(argument)
	}
	assertExactArgValue(t, arguments, "--model", "claude-opus-4-8")
	finalReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(finalReceipt, []byte(`"model":"claude-opus-4-8"`)) || !bytes.Contains(finalReceipt, []byte(plan["expected_argv_digest"].(string))) {
		t.Fatalf("receipt does not bind model command identity: %s", finalReceipt)
	}
	launchLog, err := os.ReadFile(fixture.tmuxLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		model any
	}{
		{name: "fable", model: "claude-fable-5"},
		{name: "omitted", model: nil},
	} {
		var stored map[string]any
		if err := json.Unmarshal(finalReceipt, &stored); err != nil {
			t.Fatal(err)
		}
		receipt := stored["receipts"].([]any)[0].(map[string]any)
		for _, candidate := range receipt["events"].([]any) {
			event := candidate.(map[string]any)
			if event["kind"] != "launch_intent" {
				continue
			}
			if test.model == nil {
				delete(event, "model")
			} else {
				event["model"] = test.model
			}
		}
		drifted, err := json.Marshal(stored)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(receiptPath, drifted, 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, request, "launch", "execute")
		if err == nil || !strings.Contains(stderr, "launch intent model differs from immutable binding") {
			t.Fatalf("%s model-drifted replay error = %v, stderr %q", test.name, err, stderr)
		}
		preserved, readErr := os.ReadFile(receiptPath)
		if readErr != nil || !bytes.Equal(preserved, drifted) {
			t.Fatalf("%s model-drifted replay changed receipt bytes: %v", test.name, readErr)
		}
		currentLog, readErr := os.ReadFile(fixture.tmuxLog)
		if readErr != nil || !bytes.Equal(currentLog, launchLog) {
			t.Fatalf("%s model-drifted replay mutated tmux: %v\n%s", test.name, readErr, currentLog)
		}
	}
}

func TestLaunchCompletionRejectsDurableModelDrift(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	for _, test := range []struct {
		name       string
		driftModel string
	}{
		{name: "fable", driftModel: "claude-fable-5"},
		{name: "omitted", driftModel: "__omit__"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newLaunchFixture(t)
			enableAsyncClaudeLaunch(t, fixture.binDir, &fixture.environment)
			receiptPath := filepath.Join(fixture.stateDir, "receipts.json")
			snapshotPath := filepath.Join(t.TempDir(), "drifted-receipt")
			fixture.environment = append(fixture.environment,
				"DRIFT_RECEIPT_PATH="+receiptPath,
				"DRIFT_RECEIPT_MODEL="+test.driftModel,
				"DRIFT_RECEIPT_SNAPSHOT="+snapshotPath,
			)
			if err := os.WriteFile(fixture.session, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			policyStdout, policyStderr, err := runHelper(t, fixture.stateDir, map[string]any{
				"workflow": "read_only", "model": "claude-opus-4-8",
			}, "launch", "policy-digest")
			if err != nil {
				t.Fatalf("Opus policy digest: %v: %s", err, policyStderr)
			}
			policy := decodeJSONMap(t, policyStdout)
			fixture.request["model"] = "claude-opus-4-8"
			fixture.request["expected_launch_policy_digest"] = policy["launch_policy_digest"]
			planStdout, planStderr, err := runHelperEnv(
				t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan",
			)
			if err != nil {
				t.Fatalf("Opus launch plan: %v: %s", err, planStderr)
			}
			plan := decodeJSONMap(t, planStdout)
			binding := testBinding(fixture.request["delegation_id"].(string))
			binding["workdir"] = fixture.request["workdir"]
			binding["base"] = fixture.request["base"]
			binding["packet_digest"] = plan["packet_digest"]
			binding["launch_policy_digest"] = plan["launch_policy_digest"]
			binding["launch_command_digest"] = plan["launch_command_digest"]
			binding["model"] = "claude-opus-4-8"
			assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
				"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
			}, "receipt", "create")
			_, stderr, err := runHelperEnv(
				t, fixture.stateDir, fixture.environment, fixture.request, "launch", "execute",
			)
			if err == nil || !strings.Contains(stderr, "launch intent model differs from immutable binding") {
				t.Fatalf("%s completion drift error = %v, stderr %q", test.name, err, stderr)
			}
			drifted, err := os.ReadFile(snapshotPath)
			if err != nil {
				t.Fatal(err)
			}
			preserved, err := os.ReadFile(receiptPath)
			if err != nil || !bytes.Equal(preserved, drifted) {
				t.Fatalf("%s completion drift changed receipt after rejection: %v", test.name, err)
			}
			if bytes.Contains(preserved, []byte(`"kind":"launch_completed"`)) {
				t.Fatalf("%s completion drift appended launch_completed: %s", test.name, preserved)
			}
			log, err := os.ReadFile(fixture.tmuxLog)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(log), "new-window") != 1 || strings.Contains(string(log), "kill-pane") {
				t.Fatalf("%s completion drift performed unexpected tmux mutation: %s", test.name, log)
			}
		})
	}
}

type launchFixture struct {
	stateDir            string
	binDir              string
	environment         []string
	request             map[string]any
	tmuxLog             string
	session             string
	disappearAfterCheck string
	dirtyAfterPreflight string
	dirtyState          string
	oversizedTmuxEnv    string
}

func createPlannedLaunchReceipt(t *testing.T, fixture launchFixture) string {
	t.Helper()
	stdout, stderr, err := runHelperEnv(t, fixture.stateDir, fixture.environment, fixture.request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	plan := decodeJSONMap(t, stdout)
	binding := testBinding(fixture.request["delegation_id"].(string))
	binding["workdir"] = fixture.request["workdir"]
	binding["base"] = fixture.request["base"]
	binding["packet_digest"] = plan["packet_digest"]
	binding["launch_policy_digest"] = plan["launch_policy_digest"]
	binding["launch_command_digest"] = plan["launch_command_digest"]
	assertHelperOutcomeEnv(t, fixture.stateDir, fixture.environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	return filepath.Join(fixture.stateDir, "receipts.json")
}

func environmentValue(t *testing.T, environment []string, name string) string {
	t.Helper()
	prefix := name + "="
	for index := len(environment) - 1; index >= 0; index-- {
		if strings.HasPrefix(environment[index], prefix) {
			return strings.TrimPrefix(environment[index], prefix)
		}
	}
	t.Fatalf("environment does not contain %s", name)
	return ""
}

func permitSealedRuntimeTempCleanup(t *testing.T, stateDir string) {
	t.Helper()
	t.Cleanup(func() {
		_ = os.Chmod(stateDir, 0o700)
		_ = filepath.WalkDir(filepath.Join(stateDir, "runtime"), func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				_ = os.Chmod(path, 0o700)
			}
			return nil
		})
	})
}

func newLaunchFixture(t *testing.T) launchFixture {
	t.Helper()
	stateDir := t.TempDir()
	binDir := t.TempDir()
	workdir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(packetPath, []byte("bounded launch packet"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmuxLog := filepath.Join(t.TempDir(), "tmux.log")
	session := filepath.Join(t.TempDir(), "session-exists")
	disappearAfterCheck := filepath.Join(t.TempDir(), "disappear-after-check")
	dirtyAfterPreflight := filepath.Join(t.TempDir(), "dirty-after-preflight")
	dirtyState := filepath.Join(t.TempDir(), "dirty-state")
	oversizedTmuxEnv := filepath.Join(t.TempDir(), "oversized-tmux-environment")
	base := "0123456789abcdef0123456789abcdef01234567"
	writeExecutable(t, filepath.Join(binDir, "git"), `#!/bin/sh
set -eu
case "$*" in
  *'rev-parse --show-toplevel'*) printf '%s\n' "$WORKDIR" ;;
  *'rev-parse HEAD'*) printf '%s\n' "$BASE" ;;
  *'rev-parse --git-dir'*) printf '%s\n' "$WORKDIR/.git/worktrees/fixture" ;;
  *'rev-parse --git-common-dir'*) printf '%s\n' '/tmp/source/.git' ;;
  *'symbolic-ref --short HEAD'*) printf '%s\n' 'delegate' ;;
  *'status --porcelain'*) if [ -e "$DIRTY_STATE" ]; then printf '%s\n' '?? changed'; fi ;;
  *'remote get-url origin'*) printf '%s\n' 'git@github.com:zainfathoni/amux.git' ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
if [ "$1" = "-V" ]; then
  printf '%s\n' 'tmux 3.7b'
  exit 0
fi
if [ "$1" = "has-session" ]; then
  test "$2" = "-t"
  test "$3" = "=Claude"
  test -e "$TMUX_SESSION"
  exit 0
fi
if [ "$1" = "show-environment" ]; then
  if [ -e "$OVERSIZED_TMUX_ENV" ]; then python3 -c 'print("OVERSIZED=" + "x" * (3 * 1024 * 1024))'; fi
  exit 0
fi
if [ "$1" = "show-options" ]; then printf '%s\n' '/bin/sh'; exit 0; fi
printf '%s\n' "$*" >> "$TMUX_LOG"
if [ "$1" = "new-window" ]; then
  test -e "$TMUX_SESSION"
  printf 'Claude\tthinker\t@20\t%%20\n'
  exit 0
fi
exit 2
`)
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
case "$1" in
  --version) printf '%s\n' '2.1.212 (Claude Code)' ;;
  --help)
    printf '%s\n' '--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools'
    if [ -e "$DISAPPEAR_AFTER_CHECK" ]; then
      rm "$DISAPPEAR_AFTER_CHECK" "$TMUX_SESSION"
    fi
    if [ -e "$DIRTY_AFTER_PREFLIGHT" ]; then
      rm "$DIRTY_AFTER_PREFLIGHT"
      : > "$DIRTY_STATE"
    fi
    ;;
  *) exit 0 ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"), "WORKDIR="+workdir, "BASE="+base,
		"TMUX_LOG="+tmuxLog, "TMUX_SESSION="+session, "DISAPPEAR_AFTER_CHECK="+disappearAfterCheck,
		"DIRTY_AFTER_PREFLIGHT="+dirtyAfterPreflight, "DIRTY_STATE="+dirtyState, "OVERSIZED_TMUX_ENV="+oversizedTmuxEnv,
	)
	request := map[string]any{
		"delegation_id": "delegation-session-preflight", "event_id": "launch-session-preflight", "workdir": workdir,
		"packet_file": packetPath, "tmux_session": "Claude", "tmux_window": "thinker",
		"claude_session_id": "550e8400-e29b-41d4-a716-446655440000", "repository": "zainfathoni/amux", "base": base,
		"expected_launch_policy_digest": "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445",
	}
	return launchFixture{
		stateDir: stateDir, binDir: binDir, environment: environment, request: request, tmuxLog: tmuxLog,
		session: session, disappearAfterCheck: disappearAfterCheck,
		dirtyAfterPreflight: dirtyAfterPreflight, dirtyState: dirtyState, oversizedTmuxEnv: oversizedTmuxEnv,
	}
}

func TestLaunchPlanAndExecutionKeepPacketOutOfReceiptAndDenyMutationTools(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("experimental Claude launch requires an exact supported process identity")
	}
	stateDir := t.TempDir()
	binDir := t.TempDir()
	workdir := t.TempDir()
	workdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(t.TempDir(), "packet.json")
	logPath := filepath.Join(t.TempDir(), "tmux.log")
	argvPath := filepath.Join(t.TempDir(), "claude.argv")
	base := "0123456789abcdef0123456789abcdef01234567"
	packetBinding := testBinding("../delegation-launch")
	packetBinding["workdir"] = workdir
	packetBinding["base"] = base
	packetBinding["launch_policy_digest"] = "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445"
	packetEnvelope := testMessage(packetBinding, "self-contained-mcp-report", "thinker_report", map[string]any{})
	delete(packetEnvelope, "created_at")
	delete(packetEnvelope, "report")
	packetValue := map[string]any{
		"task":            "submit one correlated thinker report without follow-up input",
		"report_envelope": packetEnvelope,
		"padding":         "",
	}
	packetBytes, err := json.Marshal(packetValue)
	if err != nil {
		t.Fatal(err)
	}
	packetSize := 80 * 1024
	if runtime.GOOS == "darwin" {
		packetSize = 262144
	}
	packetValue["padding"] = strings.Repeat("x", packetSize-len(packetBytes))
	packetBytes, err = json.Marshal(packetValue)
	if err != nil {
		t.Fatal(err)
	}
	if len(packetBytes) != packetSize {
		t.Fatalf("large packet size = %d, want %d", len(packetBytes), packetSize)
	}
	packet := string(packetBytes)
	if err := os.WriteFile(packetPath, packetBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(binDir, "git"), `#!/bin/sh
set -eu
case "$*" in
  *'rev-parse --show-toplevel'*) printf '%s\n' "$WORKDIR" ;;
  *'rev-parse HEAD'*) printf '%s\n' "$BASE" ;;
  *'rev-parse --git-dir'*) printf '%s\n' "$WORKDIR/.git/worktrees/fixture" ;;
  *'rev-parse --git-common-dir'*) printf '%s\n' '/tmp/source/.git' ;;
  *'status --porcelain'*) exit 0 ;;
  *'remote get-url origin'*) printf '%s\n' 'git@github.com:zainfathoni/amux.git' ;;
  *) exit 2 ;;
esac
`)
	environment := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"), "WORKDIR="+workdir, "BASE="+base, "TMUX_LOG="+logPath, "ARGV_LOG="+argvPath,
	)
	enableAsyncClaudeLaunch(t, binDir, &environment)
	request := map[string]any{
		"delegation_id": "../delegation-launch", "event_id": "launch-1", "workdir": workdir, "packet_file": packetPath,
		"tmux_session": "Claude", "tmux_window": "thinker", "claude_session_id": "550e8400-e29b-41d4-a716-446655440000",
		"repository": "zainfathoni/amux", "base": base,
		"expected_launch_policy_digest": "bf1c109e7270e8d6a37a3a1a30198172bc23472be0cc29ca84cf6a3fef927445",
	}
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, request, "launch", "plan")
	if err != nil {
		t.Fatalf("launch plan: %v: %s", err, stderr)
	}
	if strings.Contains(stdout, packet) {
		t.Fatalf("launch plan exposed packet content: %s", stdout)
	}
	var plan struct {
		PacketDigest             string `json:"packet_digest"`
		LaunchPolicyDigest       string `json:"launch_policy_digest"`
		LaunchCommandDigest      string `json:"launch_command_digest"`
		ExpectedArgvDigest       string `json:"expected_argv_digest"`
		ExpectedLauncherIdentity string `json:"expected_launcher_identity"`
	}
	if err := json.Unmarshal([]byte(stdout), &plan); err != nil {
		t.Fatal(err)
	}
	binding := testBinding("../delegation-launch")
	binding["workdir"] = workdir
	binding["base"] = base
	binding["packet_digest"] = plan.PacketDigest
	binding["launch_policy_digest"] = plan.LaunchPolicyDigest
	binding["launch_command_digest"] = plan.LaunchCommandDigest
	assertHelperOutcomeEnv(t, stateDir, environment, "recorded", map[string]any{
		"binding": binding, "routing": map[string]any{"target": "machine_local_inbox"},
	}, "receipt", "create")
	assertHelperOutcomeEnv(t, stateDir, environment, "launched", request, "launch", "execute")
	assertHelperOutcomeEnv(t, stateDir, environment, "duplicate", request, "launch", "execute")
	receiptBytes, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{plan.ExpectedArgvDigest, plan.ExpectedLauncherIdentity} {
		if expected == "" || !bytes.Contains(receiptBytes, []byte(expected)) {
			t.Fatalf("launch intent did not preserve expected process identity %q: %s", expected, receiptBytes)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"new-window", "launch transport", "--delegation-id ../delegation-launch"} {
		if !strings.Contains(string(log), required) {
			t.Errorf("launch command missing %q:\n%s", required, log)
		}
	}
	if bytes.Contains(log, packetBytes) || strings.Contains(string(log), packetPath) {
		t.Fatalf("tmux command metadata leaked packet content or source path")
	}
	if len(log) > 16*1024 {
		t.Fatalf("tmux command metadata is not bounded: %d bytes", len(log))
	}
	if strings.Count(string(log), "new-window") != 1 {
		t.Fatalf("exact launch replay created another window:\n%s", log)
	}
	argvBytes, err := os.ReadFile(argvPath)
	if err != nil {
		t.Fatal(err)
	}
	encodedArgs := bytes.Split(argvBytes, []byte{0})
	if len(encodedArgs) < 2 || len(encodedArgs[len(encodedArgs)-1]) != 0 {
		t.Fatalf("fake Claude argv is not NUL-delimited: %q", argvBytes)
	}
	encodedArgs = encodedArgs[:len(encodedArgs)-1]
	for _, argument := range encodedArgs {
		if string(argument) == "--model" {
			t.Fatalf("omitted model changed default Claude argv: %q", encodedArgs)
		}
	}
	if got := string(encodedArgs[len(encodedArgs)-1]); got != packet {
		t.Fatalf("multiline packet argv changed:\ngot:  %q\nwant: %q", got, packet)
	}
	runtimeKey := fmt.Sprintf("%x", sha256.Sum256([]byte("../delegation-launch")))
	transportPath := filepath.Join(stateDir, "runtime", runtimeKey, "launch.json")
	transportBytes, err := os.ReadFile(transportPath)
	if err != nil {
		t.Fatal(err)
	}
	transportDigest := fmt.Sprintf("%x", sha256.Sum256(transportBytes))
	var tamperedTransport map[string]any
	if err := json.Unmarshal(transportBytes, &tamperedTransport); err != nil {
		t.Fatal(err)
	}
	tamperedArgv := tamperedTransport["argv"].([]any)
	tamperedArgv[len(tamperedArgv)-1] = "substituted packet"
	tamperedBytes, err := json.Marshal(tamperedTransport)
	if err != nil {
		t.Fatal(err)
	}
	var panePIDPath string
	for _, entry := range environment {
		if strings.HasPrefix(entry, "PANE_PID_FILE=") {
			panePIDPath = strings.TrimPrefix(entry, "PANE_PID_FILE=")
		}
	}
	panePIDBytes, err := os.ReadFile(panePIDPath)
	if err != nil {
		t.Fatal(err)
	}
	panePID, err := strconv.Atoi(string(panePIDBytes))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(panePID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	mutateTestReceipt(t, stateDir, func(receipt map[string]any) {
		events := receipt["events"].([]any)
		receipt["events"] = events[:len(events)-1]
	})
	if err := os.WriteFile(transportPath, tamperedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(argvPath); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "launch", "transport", "--delegation-id", "../delegation-launch", "--transport-sha256", transportDigest, "--tmux-environment-sha256", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(stderr, "private launch transport digest does not match launch command") {
		t.Fatalf("tampered transport error = %v, stderr %q", err, stderr)
	}
	if _, err := os.Stat(argvPath); !os.IsNotExist(err) {
		t.Fatalf("tampered transport executed fake Claude: %v", err)
	}
	if err := os.WriteFile(transportPath, transportBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(transportPath, 0o640); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "launch", "transport", "--delegation-id", "../delegation-launch", "--transport-sha256", transportDigest, "--tmux-environment-sha256", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(stderr, "private launch transport must be one owner-only regular file") {
		t.Fatalf("wrong-mode transport error = %v, stderr %q", err, stderr)
	}
	if strings.Contains(stderr, transportPath) {
		t.Fatalf("wrong-mode transport error leaked private path: %s", stderr)
	}
	var invalidSchema map[string]any
	if err := json.Unmarshal(transportBytes, &invalidSchema); err != nil {
		t.Fatal(err)
	}
	invalidSchema["unexpected_private_field"] = true
	invalidSchemaBytes, err := json.Marshal(invalidSchema)
	if err != nil {
		t.Fatal(err)
	}
	invalidSchemaBytes = append(invalidSchemaBytes, '\n')
	invalidSchemaDigest := fmt.Sprintf("%x", sha256.Sum256(invalidSchemaBytes))
	if err := os.WriteFile(transportPath, invalidSchemaBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(transportPath, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "launch", "transport", "--delegation-id", "../delegation-launch", "--transport-sha256", invalidSchemaDigest, "--tmux-environment-sha256", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(stderr, "private launch transport contains unknown fields") {
		t.Fatalf("invalid-schema transport error = %v, stderr %q", err, stderr)
	}
	if err := os.WriteFile(transportPath, transportBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "receipts.json"), receiptBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	var deliveredPacket struct {
		ReportEnvelope map[string]any `json:"report_envelope"`
	}
	if err := json.Unmarshal(encodedArgs[len(encodedArgs)-1], &deliveredPacket); err != nil {
		t.Fatalf("decode packet delivered to fake Claude: %v", err)
	}
	report := deliveredPacket.ReportEnvelope
	report["created_at"] = "2026-07-18T23:30:00Z"
	report["report"] = map[string]any{
		"accepted_role": true, "accepted_exclusions": true, "status": "complete",
		"verdict":   "The self-contained packet supplied immutable report correlation.",
		"rationale": "The strict MCP route accepted the packet-derived envelope without follow-up input.",
		"evidence":  []any{}, "assumptions": []any{}, "unsupported_claims": []any{}, "blockers": []any{},
		"verification":      []any{"recovered the exact packet argument delivered to fake Claude"},
		"changed_artifacts": []any{}, "references": []any{},
	}
	mcpConfigPath := exactArgValue(t, encodedArgs, "--mcp-config")
	mcpConfigBytes, err := os.ReadFile(mcpConfigPath)
	if err != nil {
		t.Fatalf("read generated MCP config: %v", err)
	}
	var mcpConfig struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpConfigBytes, &mcpConfig); err != nil {
		t.Fatalf("decode generated MCP config: %v", err)
	}
	mcpRoute, ok := mcpConfig.Servers["amux-claude-delegation"]
	if !ok || mcpRoute.Command == "" || len(mcpRoute.Args) == 0 {
		t.Fatalf("generated MCP config has no delegation route: %#v", mcpConfig.Servers)
	}
	wrong := cloneJSONMap(t, report)
	wrong["message_id"] = "wrong-packet-policy-report"
	wrong["launch_policy_digest"] = strings.Repeat("f", 64)
	if response := callMCPReport(t, mcpRoute.Command, mcpRoute.Args, wrong); !strings.Contains(response, `"isError":true`) {
		t.Fatalf("wrong packet policy digest MCP response = %s", response)
	}
	assertReceiptHasNoValidReport(t, stateDir, binding["delegation_id"].(string))
	omitted := cloneJSONMap(t, report)
	omitted["message_id"] = "omitted-packet-policy-report"
	delete(omitted, "launch_policy_digest")
	if response := callMCPReport(t, mcpRoute.Command, mcpRoute.Args, omitted); !strings.Contains(response, `"isError":true`) {
		t.Fatalf("omitted packet policy digest MCP response = %s", response)
	}
	assertReceiptHasNoValidReport(t, stateDir, binding["delegation_id"].(string))
	if response := callMCPReport(t, mcpRoute.Command, mcpRoute.Args, report); !strings.Contains(response, `"outcome":"recorded"`) || strings.Contains(response, `"isError":true`) {
		t.Fatalf("correct packet-derived MCP report response = %s", response)
	}
	assertExactArgValue(t, encodedArgs, "--tools", "Read,Grep,Glob")
	assertExactArgValue(t, encodedArgs, "--allowed-tools", "Read,Grep,Glob,mcp__amux-claude-delegation__submit_report,mcp__amux-claude-delegation__submit_input_request")
	assertExactArgValue(t, encodedArgs, "--disallowed-tools", "Bash,Edit,Write,NotebookEdit,Agent,WebFetch,WebSearch,Skill")
	assertExactArgValue(t, encodedArgs, "--setting-sources", "")
	stored, err := os.ReadFile(filepath.Join(stateDir, "receipts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(packet)) {
		t.Fatal("receipt persisted complete launch packet content")
	}
	runtimeRoot := filepath.Join(stateDir, "runtime")
	runtimeInfo, err := os.Stat(runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if runtimeInfo.Mode().Perm() != 0o700 {
		t.Errorf("runtime parent mode = %o, want 700", runtimeInfo.Mode().Perm())
	}
	for _, name := range []string{"mcp.json", "settings.json", "launch.json"} {
		path := filepath.Join(stateDir, "runtime", runtimeKey, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("private runtime file %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("private runtime file %s mode = %o, want 600", name, info.Mode().Perm())
		}
	}
}

func callMCPReport(t *testing.T, commandPath string, commandArgs []string, report map[string]any) string {
	t.Helper()
	messages := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "submit_report", "arguments": report}},
	}
	var input bytes.Buffer
	for _, message := range messages {
		data, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(data)
		input.WriteByte('\n')
	}
	command := exec.Command(commandPath, commandArgs...)
	command.Stdin = &input
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("MCP server failed: %v\n%s", err, output)
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) != 2 {
		t.Fatalf("MCP responses = %d, want 2\n%s", len(lines), output)
	}
	return lines[1]
}

func assertReceiptHasNoValidReport(t *testing.T, stateDir, delegationID string) {
	t.Helper()
	stdout, stderr, err := runHelper(t, stateDir, map[string]any{}, "receipt", "show", "--delegation-id", delegationID)
	if err != nil {
		t.Fatalf("show receipt after invalid MCP report: %v: %s", err, stderr)
	}
	var receipt struct {
		Events []struct {
			Kind string `json:"kind"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatal(err)
	}
	for _, event := range receipt.Events {
		if event.Kind == "valid_report" {
			t.Fatal("invalid MCP report appended a valid_report event")
		}
	}
}

func assertExactArgValue(t *testing.T, arguments [][]byte, flag, want string) {
	t.Helper()
	if got := exactArgValue(t, arguments, flag); got != want {
		t.Fatalf("%s value = %q, want %q", flag, got, want)
	}
}

func exactArgValue(t *testing.T, arguments [][]byte, flag string) string {
	t.Helper()
	for index, argument := range arguments {
		if string(argument) == flag && index+1 < len(arguments) {
			return string(arguments[index+1])
		}
	}
	t.Fatalf("exact argv is missing %s", flag)
	return ""
}

func TestDiagnosticsClassifySupportedUnavailableAndUntestedCapabilities(t *testing.T) {
	stateDir := t.TempDir()
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
case "$1" in
  --version) printf '%s\n' '2.1.212 (Claude Code)' ;;
  --help) printf '%s\n' '--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --model --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools' ;;
  *) exit 2 ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "tmux"), "#!/bin/sh\nprintf '%s\\n' 'tmux 3.7b'\n")
	writeExecutable(t, filepath.Join(binDir, "codexbar"), `#!/bin/sh
printf '%s\n' '[{"provider":"claude","source":"web","usage":{"primary":{"usedPercent":12,"windowMinutes":300,"resetsAt":"2026-07-20T15:00:00Z"},"secondary":{"usedPercent":34,"windowMinutes":10080,"resetsAt":"2026-07-24T00:00:00Z"},"tertiary":null,"extraRateWindows":[],"updatedAt":"2026-07-20T12:00:00Z"}}]'
`)
	environment := append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, map[string]any{}, "diagnose")
	if err != nil {
		t.Fatalf("diagnose: %v: %s", err, stderr)
	}
	for _, required := range []string{`"status":"supported"`, `"status":"unavailable"`, `"status":"untested"`, `"automatic_interactive_input"`, `"strict_mcp_runtime"`, `"model_selection"`, `installed Claude CLI exposes --model; model provisioning and availability are not observed`, `"capacity source payload has no supported versioned contract"`} {
		if !strings.Contains(stdout, required) {
			t.Errorf("diagnostics missing %q:\n%s", required, stdout)
		}
	}
	for _, forbidden := range []string{"accountEmail", "accountOrganization", "transcript", "prompt", `"source":"web"`, `"used_percent"`, `"window_minutes"`, `"source_version"`, `"schema_version"`} {
		if strings.Contains(stdout, forbidden) {
			t.Errorf("diagnostics leaked forbidden field %q", forbidden)
		}
	}
	initialDiagnostic := decodeJSONMap(t, stdout)
	initialCapacity := initialDiagnostic["capacity"].(map[string]any)
	if initialCapacity["reason"] != "capacity source payload has no supported versioned contract" || len(initialCapacity["windows"].([]any)) != 0 {
		t.Fatalf("unversioned capacity diagnostic = %#v", initialCapacity)
	}
	hugePercentage := strings.Repeat("9", 400)
	capacityCases := []struct {
		name    string
		script  string
		reason  string
		private string
	}{
		{
			name:   "command failure",
			script: "#!/bin/sh\nexit 2\n",
			reason: "capacity source command or execution failed",
		},
		{
			name:    "malformed JSON",
			script:  "#!/bin/sh\nprintf '%s\\n' '[{private-sentinel}]'\n",
			reason:  "capacity source returned malformed JSON",
			private: "private-sentinel",
		},
		{
			name:   "nonstandard numeric constants",
			script: "#!/bin/sh\nprintf '%s\\n' '[{\"provider\":\"claude\",\"source\":\"web\",\"usage\":{\"primary\":{\"usedPercent\":NaN},\"secondary\":{\"usedPercent\":Infinity},\"tertiary\":{\"usedPercent\":-Infinity},\"extraRateWindows\":[],\"updatedAt\":\"2026-07-20T12:00:00Z\"}}]'\n",
			reason: "capacity source returned malformed JSON",
		},
		{
			name:   "valid unrecognized payload",
			script: "#!/bin/sh\nprintf '%s\\n' '{}'\n",
			reason: "capacity source payload is unrecognized",
		},
		{
			name: "recognized current CodexBar shape",
			script: `#!/bin/sh
printf '%s\n' '[{"provider":"claude","version":"synthetic-version","source":"web","status":{"indicator":"synthetic"},"usage":{"primary":{"usedPercent":12,"windowMinutes":300},"secondary":{"usedPercent":34,"windowMinutes":10080,"resetsAt":"2026-07-24T00:00:00Z"},"tertiary":null,"extraRateWindows":[],"updatedAt":"2026-07-20T12:00:00Z","dataConfidence":"estimated"}}]'
`,
			reason: "recognized CodexBar capacity payload has unsupported schema or version",
		},
		{
			name:    "duplicate key",
			script:  "#!/bin/sh\nprintf '%s\\n' '[{\"provider\":\"claude\",\"source\":\"private-sentinel\",\"source\":\"web\",\"usage\":{\"primary\":null,\"secondary\":null,\"tertiary\":null,\"extraRateWindows\":[],\"updatedAt\":\"2026-07-20T12:00:00Z\"}}]'\n",
			reason:  "capacity source returned malformed JSON",
			private: "private-sentinel",
		},
		{
			name:   "unsupported surrogate timestamp",
			script: "#!/bin/sh\nprintf '%s\\n' '[{\"provider\":\"claude\",\"source\":\"web\",\"usage\":{\"primary\":null,\"secondary\":null,\"tertiary\":null,\"extraRateWindows\":[],\"updatedAt\":\"\\ud800\"}}]'\n",
			reason: "recognized CodexBar capacity payload has unsupported schema or version",
		},
		{
			name:   "unsupported oversized number",
			script: "#!/bin/sh\nprintf '%s\\n' '[{\"provider\":\"claude\",\"source\":\"web\",\"usage\":{\"primary\":{\"usedPercent\":" + hugePercentage + ",\"windowMinutes\":300,\"resetsAt\":\"2026-07-20T15:00:00Z\"},\"secondary\":null,\"tertiary\":null,\"extraRateWindows\":[],\"updatedAt\":\"2026-07-20T12:00:00Z\"}}]'\n",
			reason: "recognized CodexBar capacity payload has unsupported schema or version",
		},
	}
	for _, test := range capacityCases {
		writeExecutable(t, filepath.Join(binDir, "codexbar"), test.script)
		stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "diagnose")
		if err != nil {
			t.Fatalf("diagnose %s: %v: %s", test.name, err, stderr)
		}
		diagnostic := decodeJSONMap(t, stdout)
		capacity, ok := diagnostic["capacity"].(map[string]any)
		windows, windowsOK := capacity["windows"].([]any)
		if !ok || capacity["status"] != "unavailable" || capacity["reason"] != test.reason || !windowsOK || len(windows) != 0 {
			t.Fatalf("%s diagnostic = %s", test.name, stdout)
		}
		if test.private != "" && strings.Contains(stdout, test.private) {
			t.Fatalf("%s diagnostic leaked private input: %s", test.name, stdout)
		}
	}
	writeExecutable(t, filepath.Join(binDir, "claude"), `#!/bin/sh
case "$1" in
  --version) printf '%s\n' '2.1.212 (Claude Code)' ;;
  --help) printf '%s\n' '--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --model-context --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools' ;;
  *) exit 2 ;;
esac
`)
	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "diagnose")
	if err != nil {
		t.Fatalf("diagnose with model prefix option: %v: %s", err, stderr)
	}
	diagnostic := decodeJSONMap(t, stdout)
	capabilities := diagnostic["capabilities"].(map[string]any)
	modelSelection := capabilities["model_selection"].(map[string]any)
	if modelSelection["status"] != "unavailable" || modelSelection["reason"] != "installed Claude CLI does not expose --model" {
		t.Fatalf("prefix option model selection diagnostic = %#v", modelSelection)
	}

	writeExecutable(t, filepath.Join(binDir, "codexbar"), "#!/bin/sh\nprintf '%s\\n' 'private-stderr-sentinel' >&2\nexit 1\n")
	stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "diagnose")
	if err != nil || strings.Contains(stdout+stderr, "private-stderr-sentinel") || !strings.Contains(stdout, `"status":"unavailable"`) {
		t.Fatalf("diagnostic command failure leaked stderr: %v: %s%s", err, stdout, stderr)
	}

	tooManyExtraWindows := strings.Repeat(`{"id":"id","title":"title","window":{"usedPercent":1,"windowMinutes":1,"resetsAt":"2026-07-20T12:01:00Z"}},`, 33)
	tooManyExtraWindows = strings.TrimSuffix(tooManyExtraWindows, ",")
	boundedMalformed := []struct {
		command string
		reason  string
	}{
		{"printf '\\377'", "capacity source returned malformed JSON"},
		{"python3 -c 'print(\"x\" * 262145)'", "capacity source command or execution failed"},
		{"printf '%s\\n' '" + `[{"provider":"claude","source":"web","usage":{"primary":null,"secondary":null,"tertiary":null,"extraRateWindows":[` + tooManyExtraWindows + `],"updatedAt":"2026-07-20T12:00:00Z"}}]` + "'", "recognized CodexBar capacity payload has unsupported schema or version"},
	}
	for index, test := range boundedMalformed {
		writeExecutable(t, filepath.Join(binDir, "codexbar"), "#!/bin/sh\n"+test.command+"\n")
		stdout, stderr, err = runHelperEnv(t, stateDir, environment, map[string]any{}, "diagnose")
		diagnostic := decodeJSONMap(t, stdout)
		capacity := diagnostic["capacity"].(map[string]any)
		if err != nil || len(stdout) > 4096 || capacity["status"] != "unavailable" || capacity["reason"] != test.reason || len(capacity["windows"].([]any)) != 0 {
			t.Fatalf("unbounded malformed diagnostic %d: %v: %s%s", index, err, stdout, stderr)
		}
	}
}

func testBinding(delegationID string) map[string]any {
	return map[string]any{
		"protocol_version":      1,
		"delegation_id":         delegationID,
		"nonce":                 strings.Repeat("a", 64),
		"task_id":               "task-1",
		"question_message_id":   "question-1",
		"origin_thread":         "T-origin",
		"repository":            "zainfathoni/amux",
		"base":                  "0123456789abcdef0123456789abcdef01234567",
		"workdir":               "/tmp/amux-read-only",
		"producer_role":         "thinker",
		"authority":             "read_only",
		"task_reference":        "issue-148-design-review",
		"packet_digest":         strings.Repeat("b", 64),
		"launch_policy_digest":  strings.Repeat("c", 64),
		"launch_command_digest": strings.Repeat("d", 64),
	}
}

func testMessage(binding map[string]any, messageID, kind string, payload map[string]any) map[string]any {
	return map[string]any{
		"protocol_version":     binding["protocol_version"],
		"delegation_id":        binding["delegation_id"],
		"nonce":                binding["nonce"],
		"message_id":           messageID,
		"in_reply_to":          binding["question_message_id"],
		"kind":                 kind,
		"task_id":              binding["task_id"],
		"origin_thread":        binding["origin_thread"],
		"repository":           binding["repository"],
		"base":                 binding["base"],
		"workdir":              binding["workdir"],
		"producer_role":        binding["producer_role"],
		"authority":            binding["authority"],
		"launch_policy_digest": binding["launch_policy_digest"],
		"created_at":           "2026-07-17T12:00:00Z",
		map[string]string{"thinker_report": "report", "input_request": "input_request"}[kind]: payload,
	}
}

func startProcessFixture(t *testing.T, name string, arguments ...string) (int, string) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return 5252, "/usr/local/bin/claude"
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte("package main\nimport \"time\"\nfunc main() { time.Sleep(time.Minute) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Claude process fixture: %v\n%s", err, output)
	}
	process := exec.Command(binary, arguments...)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})
	return process.Process.Pid, binary
}

func startNodeDescriptorProcessFixture(t *testing.T, processName string, arguments ...string) (int, string, string, *os.File) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	if err := os.WriteFile(source, []byte("package main\nimport \"time\"\nfunc main() { time.Sleep(time.Minute) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, processName)
	build := exec.Command("go", "build", "-o", binary, source)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build node process fixture: %v\n%s", err, output)
	}
	launcherPath := filepath.Join(dir, "cli.js")
	if err := os.WriteFile(launcherPath, []byte("original descriptor launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	launcherFile, err := os.OpenFile(launcherPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	processArguments := append([]string{"/proc/self/fd/3"}, arguments...)
	process := exec.Command(binary, processArguments...)
	process.ExtraFiles = []*os.File{launcherFile}
	if err := process.Start(); err != nil {
		_ = launcherFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
		_ = launcherFile.Close()
	})
	return process.Process.Pid, binary, launcherPath, launcherFile
}

func recordTestLaunch(t *testing.T, stateDir, delegationID string, identity map[string]any, expectedArgvDigest, expectedLauncherIdentity, expectedExecutableObjectIdentity string) {
	t.Helper()
	path := filepath.Join(stateDir, "receipts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store map[string]any
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	receipt := store["receipts"].([]any)[0].(map[string]any)
	if receipt["binding"].(map[string]any)["delegation_id"] != delegationID {
		t.Fatal("test receipt identity mismatch")
	}
	receipt["events"] = append(receipt["events"].([]any),
		map[string]any{
			"event_id": "launch-fixture", "kind": "launch_intent", "at": "2026-07-17T12:00:00Z",
			"expected_argv_digest": expectedArgvDigest, "expected_launcher_identity": expectedLauncherIdentity,
			"expected_executable_object_identity": expectedExecutableObjectIdentity,
		},
		map[string]any{"event_id": "amux:test-launch-result", "kind": "launch_completed", "operation_event_id": "launch-fixture", "identity": identity, "at": "2026-07-17T12:00:01Z"},
	)
	updated, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mutateTestReceipt(t *testing.T, stateDir string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(stateDir, "receipts.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var store map[string]any
	if err := json.Unmarshal(data, &store); err != nil {
		t.Fatal(err)
	}
	mutate(store["receipts"].([]any)[0].(map[string]any))
	updated, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
}

func testLaunchIntentEvent() map[string]any {
	return map[string]any{
		"event_id": "launch-fixture", "kind": "launch_intent",
		"expected_argv_digest":                strings.Repeat("e", 64),
		"expected_launcher_identity":          "file:1:2",
		"expected_executable_object_identity": "object:1:2:3:digest",
	}
}

func testLaunchCompletedEvent() map[string]any {
	return map[string]any{
		"event_id": "launch-completed-fixture", "kind": "launch_completed",
		"operation_event_id": "launch-fixture", "identity": map[string]any{
			"session": "Claude", "window": "thinker", "window_id": "@private", "pane_id": "%private",
		},
	}
}

func inspectTestClaudeIdentity(t *testing.T, environment []string, paneID, sessionID, argvDigest, launcherIdentity, objectIdentity string) map[string]any {
	t.Helper()
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	script := `import importlib.util, json, pathlib, sys
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("claude_delegation", pathlib.Path(sys.argv[1]))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
print(json.dumps(module.inspect_claude_identity(*sys.argv[2:])))
`
	command := exec.Command("python3", "-c", script, helper, paneID, sessionID, argvDigest, launcherIdentity, objectIdentity)
	command.Env = environment
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("inspect Claude fixture identity: %v\n%s", err, output)
	}
	var identity map[string]any
	if err := json.Unmarshal(output, &identity); err != nil {
		t.Fatal(err)
	}
	return identity
}

func nulDigest(arguments []string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(arguments, "\x00"))))
}

func testExecutableIdentity(t *testing.T, path string) string {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("file:%d:%d", info.Sys().(*syscall.Stat_t).Dev, info.Sys().(*syscall.Stat_t).Ino)
}

func testExecutableObjectIdentity(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("object:%d:%d:%d:%x", info.Sys().(*syscall.Stat_t).Dev, info.Sys().(*syscall.Stat_t).Ino, info.Size(), sha256.Sum256(data))
}

func replaceEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func environmentOrCurrent(environment []string) []string {
	if environment == nil {
		return os.Environ()
	}
	return environment
}

func assertHelperOutcome(t *testing.T, stateDir, want string, input map[string]any, args ...string) {
	t.Helper()
	stdout, stderr, err := runHelper(t, stateDir, input, args...)
	if err != nil {
		t.Fatalf("helper %s: %v: %s", strings.Join(args, " "), err, stderr)
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode helper result: %v\n%s", err, stdout)
	}
	if result.Outcome != want {
		t.Fatalf("helper %s outcome = %q, want %q", strings.Join(args, " "), result.Outcome, want)
	}
}

func assertHelperOutcomeEnv(t *testing.T, stateDir string, environment []string, want string, input map[string]any, args ...string) {
	t.Helper()
	stdout, stderr, err := runHelperEnv(t, stateDir, environment, input, args...)
	if err != nil {
		t.Fatalf("helper %s: %v: %s", strings.Join(args, " "), err, stderr)
	}
	if !strings.Contains(stdout, `"outcome":"`+want+`"`) {
		t.Fatalf("helper %s output = %s, want outcome %s", strings.Join(args, " "), stdout, want)
	}
}

func runHelper(t *testing.T, stateDir string, input map[string]any, args ...string) (string, string, error) {
	return runHelperEnv(t, stateDir, nil, input, args...)
}

func runHelperEnv(t *testing.T, stateDir string, environment []string, input map[string]any, args ...string) (string, string, error) {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", append([]string{helper, "--state-dir", stateDir, "--isolated-test-state"}, args...)...)
	command.Env = append(environmentOrCurrent(environment), "AMUX_CLAUDE_DELEGATION_TESTING=1")
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	return stdout.String(), stderr.String(), err
}

func runHelperEnvWithStackLimit(t *testing.T, stateDir string, environment []string, input map[string]any, args ...string) (string, string, error) {
	t.Helper()
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs("claude_delegation.py")
	if err != nil {
		t.Fatal(err)
	}
	arguments := append([]string{"python3", helper, "--state-dir", stateDir, "--isolated-test-state"}, args...)
	command := exec.Command("/bin/sh", append([]string{"-c", `ulimit -s 256; exec "$@"`, "sh"}, arguments...)...)
	command.Env = append(environmentOrCurrent(environment), "AMUX_CLAUDE_DELEGATION_TESTING=1")
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	return stdout.String(), stderr.String(), err
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func enableAsyncClaudeLaunch(t *testing.T, binDir string, environment *[]string) {
	t.Helper()
	claudePath := filepath.Join(binDir, "claude")
	source := filepath.Join(t.TempDir(), "fake_claude.go")
	program := `package main
import (
  "fmt"
  "os"
  "runtime"
  "syscall"
  "time"
)
func main() {
  if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "--help") {
    if path := os.Getenv("PROBE_ENV_LOG"); path != "" {
      output, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
      _, _ = fmt.Fprintf(output, "probe=%s\n", os.Args[1])
      for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "BENIGN_SENTINEL"} {
        value, present := os.LookupEnv(name)
        _, _ = fmt.Fprintf(output, "%s=%t:%s\n", name, present, value)
      }
      _ = output.Close()
    }
  }
  if len(os.Args) > 1 && os.Args[1] == "--version" { fmt.Println("2.1.212 (Claude Code)"); return }
  if len(os.Args) > 1 && os.Args[1] == "--help" {
    fmt.Println("--allowed-tools --disable-slash-commands --disallowed-tools --mcp-config --model --no-chrome --permission-mode --prompt-suggestions --session-id --setting-sources --settings --strict-mcp-config --tools")
    if path := os.Getenv("DISAPPEAR_AFTER_CHECK"); path != "" { if _, err := os.Stat(path); err == nil { _ = os.Remove(path); _ = os.Remove(os.Getenv("TMUX_SESSION")) } }
    if path := os.Getenv("DIRTY_AFTER_PREFLIGHT"); path != "" { if _, err := os.Stat(path); err == nil { _ = os.Remove(path); _ = os.WriteFile(os.Getenv("DIRTY_STATE"), nil, 0600) } }
    if path := os.Getenv("REPLACE_AFTER_PREFLIGHT"); path != "" { if _, err := os.Stat(path); err == nil {
      _ = os.Remove(path)
      kind := os.Getenv("REPLACE_KIND")
      target := os.Getenv("REPLACE_TARGET")
      replacement := os.Getenv("REPLACE_WITH")
      if kind == "workdir" { _ = os.Rename(target, target + ".replaced") }
      _ = os.Rename(replacement, target)
    } }
    return
  }
  if path := os.Getenv("ENV_LOG"); path != "" {
    output, _ := os.Create(path)
    for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN", "BENIGN_SENTINEL", "STARTUP_EXIT", "SUBSTITUTE_ARGV"} {
      value, present := os.LookupEnv(name)
      _, _ = fmt.Fprintf(output, "%s=%t:%s\n", name, present, value)
    }
    _ = output.Close()
  }
  if os.Getenv("STARTUP_EXIT") == "1" { return }
  if (os.Getenv("SUBSTITUTE_ARGV") == "1" || os.Getenv("SUBSTITUTE_ARGV0") != "") && os.Getenv("SUBSTITUTE_REEXECED") == "" {
    filtered := append([]string(nil), os.Args...)
    if os.Getenv("SUBSTITUTE_ARGV") == "1" {
      filtered = []string{os.Args[0]}
      for index := 1; index < len(os.Args); index++ {
        if os.Args[index] == "--disallowed-tools" && index + 1 < len(os.Args) { index++; continue }
        filtered = append(filtered, os.Args[index])
      }
    }
    if value := os.Getenv("SUBSTITUTE_ARGV0"); value != "" { filtered[0] = value }
    _ = os.Setenv("SUBSTITUTE_REEXECED", "1")
    executable := os.Args[0]
    if runtime.GOOS == "darwin" { executable, _ = os.Executable() }
    _ = syscall.Exec(executable, filtered, os.Environ())
    return
  }
  if path := os.Getenv("ARGV_LOG"); path != "" {
    output, _ := os.Create(path)
    for _, argument := range os.Args[1:] { _, _ = output.Write(append([]byte(argument), 0)) }
    _ = output.Close()
  }
  for { time.Sleep(time.Second) }
}
`
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "build", "-o", claudePath, source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build async Claude fixture: %v\n%s", err, output)
	}
	panePID := filepath.Join(t.TempDir(), "pane.pid")
	paneOutput := filepath.Join(t.TempDir(), "pane.output")
	writeExecutable(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
set -eu
case "$1" in
  -V) printf '%s\n' 'tmux 3.7b' ;;
  has-session) test "$2" = "-t"; test "$3" = "=Claude"; test ! -n "${TMUX_SESSION:-}" || test -e "$TMUX_SESSION" ;;
  show-environment) exit 0 ;;
  show-options) printf '%s\n' '/bin/sh' ;;
  new-window)
    printf '%s\n' "$*" >> "$TMUX_LOG"
    for argument do start_command=$argument; done
    replaced=0
    if [ -n "${REPLACE_EXECUTABLE_WITH:-}" ]; then mv "$REPLACE_EXECUTABLE_WITH" "$CLAUDE_PATH"; replaced=1; fi
    if [ -n "${REPLACE_PACKET_WITH:-}" ]; then mv "$REPLACE_PACKET_WITH" "$PACKET_PATH"; replaced=1; fi
    if [ -n "${REPLACE_WORKDIR_WITH:-}" ]; then
      mv "$WORKDIR" "$WORKDIR.replaced"
      mv "$REPLACE_WORKDIR_WITH" "$WORKDIR"
      replaced=1
    fi
    if [ "$replaced" = 1 ]; then printf succeeded >"$REPLACEMENT_SUCCEEDED"; fi
    /bin/sh -c "$start_command" </dev/null >"$PANE_OUTPUT" 2>&1 &
    printf '%s' "$!" > "$PANE_PID_FILE"
    if [ -n "${MALFORMED_NEW_WINDOW:-}" ]; then printf 'malformed\n'; exit 0; fi
    if [ -n "${FAILED_NEW_WINDOW:-}" ]; then exit 2; fi
    printf 'Claude\tthinker\t@20\t%%20\n'
    ;;
  display-message)
    test -s "$PANE_PID_FILE"
    pane_pid=$(cat "$PANE_PID_FILE")
    kill -0 "$pane_pid"
    if [ -n "${DRIFT_RECEIPT_MODEL:-}" ] && [ ! -e "$DRIFT_RECEIPT_SNAPSHOT" ]; then
      counter="$DRIFT_RECEIPT_SNAPSHOT.counter"
      count=0
      if [ -e "$counter" ]; then count=$(cat "$counter"); fi
      count=$((count + 1)); printf '%s' "$count" > "$counter"
      if [ "$count" -ge 10 ]; then
        python3 -c 'import json,os,pathlib; p=pathlib.Path(os.environ["DRIFT_RECEIPT_PATH"]); d=json.loads(p.read_text()); e=next(x for x in d["receipts"][0]["events"] if x.get("kind")=="launch_intent"); m=os.environ["DRIFT_RECEIPT_MODEL"]; e.pop("model",None) if m=="__omit__" else e.__setitem__("model",m); b=json.dumps(d,separators=(",",":")).encode(); p.write_bytes(b); pathlib.Path(os.environ["DRIFT_RECEIPT_SNAPSHOT"]).write_bytes(b)'
      fi
    fi
    start_command=$(tail -n 1 "$TMUX_LOG" | sed -n 's/^.* -c [^ ]* //p')
    if [ -n "${REPORTED_START_COMMAND:-}" ]; then start_command=$REPORTED_START_COMMAND; fi
    printf 'Claude\tthinker\t@20\t%%20\t%s\t%s\tclaude\t%s\n' "$pane_pid" "${REPORTED_WORKDIR:-$WORKDIR}" "$start_command"
    ;;
  kill-pane) pane_pid=$(cat "$PANE_PID_FILE"); kill "$pane_pid"; printf '%s\n' "$*" >> "$TMUX_LOG" ;;
  list-panes) test -s "$PANE_PID_FILE" && printf '%%20\n' ;;
  *) exit 2 ;;
esac
`)
	*environment = append(*environment, "PANE_PID_FILE="+panePID, "PANE_OUTPUT="+paneOutput)
	t.Cleanup(func() {
		data, err := os.ReadFile(panePID)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(string(data))
		if err == nil {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

func cloneJSONMap(t *testing.T, input map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	return output
}
