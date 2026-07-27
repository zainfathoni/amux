#!/usr/bin/env python3
"""Provider-specific local Pi/Spark executor for bounded code microtasks."""

from __future__ import annotations

import argparse
import ctypes
import datetime as dt
import fcntl
import hashlib
import json
import os
import pathlib
import shutil
import stat
import subprocess
import sys
import tempfile
import threading
import time
from typing import Any


MODEL = "openai-codex/gpt-5.3-codex-spark"
PROVIDER = "openai-codex"
MODEL_ID = "gpt-5.3-codex-spark"
PI_VERSION = "0.80.10"
PROHIBITED_ENV = ("OPENAI_API_KEY", "CODEX_API_KEY")
PROHIBITED_AUTHORITY = (
    "amp_thread_read",
    "cross_worker_communication",
    "recursive_delegation",
    "network_publish",
    "pr",
    "push",
    "merge",
    "release",
    "install",
    "cleanup",
    "teardown",
)
PI_ARGS = (
    "--mode", "json",
    "--model", MODEL,
    "--thinking", "high",
    "--no-session",
    "--no-tools",
    "--no-extensions",
    "--no-skills",
    "--no-prompt-templates",
    "--no-themes",
    "--no-context-files",
    "--no-approve",
    "--system-prompt",
    "Return only the requested JSON replacement envelope. Do not use tools, files, external context, network publishing, or delegation.",
)


class Blocked(Exception):
    pass


class Indeterminate(Blocked):
    pass


def validate_operation_id(value: Any) -> str:
    if not isinstance(value, str) or not value or len(value) > 128 or any(
        character not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-" for character in value
    ):
        raise Blocked("operation_id is invalid")
    return value


def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def parse_time(value: Any, name: str) -> dt.datetime:
    if not isinstance(value, str):
        raise Blocked(f"{name} must be an RFC3339 timestamp")
    try:
        parsed = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise Blocked(f"{name} must be an RFC3339 timestamp") from error
    if parsed.tzinfo is None:
        raise Blocked(f"{name} must include a timezone")
    return parsed.astimezone(dt.timezone.utc)


def load_json(path: pathlib.Path, label: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise Blocked(f"{label} is unreadable") from error
    if not isinstance(value, dict):
        raise Blocked(f"{label} must be a JSON object")
    return value


def canonical_existing_file(path: str, label: str) -> pathlib.Path:
    candidate = pathlib.Path(path)
    if not candidate.is_absolute() or candidate.is_symlink() or not candidate.is_file():
        raise Blocked(f"{label} must be an absolute, existing, non-symlink file")
    return candidate.resolve(strict=True)


def file_identity(path: pathlib.Path) -> dict[str, Any]:
    info = path.stat()
    return {
        "path": str(path),
        "device": str(info.st_dev),
        "inode": str(info.st_ino),
        "mode": stat.S_IMODE(info.st_mode),
        "size": info.st_size,
        "mtime_ns": str(info.st_mtime_ns),
    }


def digest_file(path: pathlib.Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require_exact_identity(path: pathlib.Path, expected: dict[str, Any], label: str) -> None:
    if file_identity(path) != expected:
        raise Blocked(f"{label} identity changed")


def validate_packet(path: pathlib.Path) -> tuple[dict[str, Any], pathlib.Path, list[pathlib.Path]]:
    packet = load_json(path, "task packet")
    required = {
        "schema", "operation_id", "owner_authorized", "goal", "workdir",
        "allowed_paths", "expected_output", "validation_commands", "exclusions",
        "timeout_seconds", "stdout_limit", "stderr_limit", "event_limit",
        "auth_evidence", "quota_evidence",
    }
    if set(packet) != required or packet["schema"] != 1:
        raise Blocked("task packet schema or fields are not exact")
    validate_operation_id(packet["operation_id"])
    if packet["owner_authorized"] is not True:
        raise Blocked("explicit owner authorization is required")
    for name in ("goal", "expected_output"):
        if not isinstance(packet[name], str) or not packet[name].strip() or len(packet[name].encode()) > 8192:
            raise Blocked(f"{name} must be a non-empty bounded string")
    if packet["exclusions"] != list(PROHIBITED_AUTHORITY):
        raise Blocked("task packet exclusions are not exact")
    commands = packet["validation_commands"]
    if not isinstance(commands, list) or not commands or len(commands) > 8 or not all(
        isinstance(command, str) and command.strip() and len(command) <= 512 for command in commands
    ):
        raise Blocked("validation_commands must contain 1-8 bounded commands")
    bounds = (("timeout_seconds", 1, 300), ("stdout_limit", 1024, 65536),
              ("stderr_limit", 0, 16384), ("event_limit", 1, 1000))
    for name, minimum, maximum in bounds:
        if not isinstance(packet[name], int) or not minimum <= packet[name] <= maximum:
            raise Blocked(f"{name} is outside the supported bound")

    workdir_value = packet["workdir"]
    if not isinstance(workdir_value, str) or not pathlib.Path(workdir_value).is_absolute():
        raise Blocked("workdir must be absolute")
    workdir = pathlib.Path(workdir_value).resolve(strict=True)
    if not workdir.is_dir() or pathlib.Path(workdir_value).is_symlink():
        raise Blocked("workdir must be a canonical non-symlink directory")
    git_root = subprocess.run(
        ["git", "-C", str(workdir), "rev-parse", "--show-toplevel"],
        capture_output=True, text=True, timeout=10, check=False,
    )
    if git_root.returncode != 0 or pathlib.Path(git_root.stdout.strip()).resolve() != workdir:
        raise Blocked("workdir must be the exact root of a Git worktree")
    branch = subprocess.run(
        ["git", "-C", str(workdir), "symbolic-ref", "--short", "HEAD"],
        capture_output=True, text=True, timeout=10, check=False,
    )
    if branch.returncode != 0 or branch.stdout.strip() in {"main", "master"}:
        raise Blocked("workdir must use a dedicated non-default branch")

    requested = packet["allowed_paths"]
    if not isinstance(requested, list) or len(requested) != 1:
        raise Blocked("allowed_paths must contain exactly one existing file")
    allowed: list[pathlib.Path] = []
    for relative in requested:
        if not isinstance(relative, str) or not relative or pathlib.PurePosixPath(relative).is_absolute() or ".." in pathlib.PurePosixPath(relative).parts:
            raise Blocked("allowed_paths must be canonical relative paths")
        candidate = workdir.joinpath(*pathlib.PurePosixPath(relative).parts)
        resolved = canonical_existing_file(str(candidate), "allowed path")
        if workdir not in resolved.parents:
            raise Blocked("allowed path escapes the worktree")
        allowed.append(resolved)
    return packet, workdir, allowed


def validate_admission(packet: dict[str, Any], now: dt.datetime) -> pathlib.Path:
    present = [name for name in PROHIBITED_ENV if name in os.environ]
    if present:
        raise Blocked("prohibited API-key environment is present")
    auth = packet["auth_evidence"]
    if not isinstance(auth, dict) or set(auth) != {"provider", "type", "source", "observed_at", "path", "identity"}:
        raise Blocked("auth evidence is unavailable or ambiguous")
    if (auth["provider"], auth["type"], auth["source"]) != (PROVIDER, "oauth", "owner-confirmed-metadata"):
        raise Blocked("auth evidence does not prove owner-confirmed Codex OAuth metadata")
    observed = parse_time(auth["observed_at"], "auth observed_at")
    if abs((now - observed).total_seconds()) > 300:
        raise Blocked("auth evidence is stale")
    auth_path = canonical_existing_file(auth["path"], "auth evidence path")
    agent_dir = pathlib.Path(os.environ.get("PI_CODING_AGENT_DIR", pathlib.Path.home() / ".pi" / "agent")).expanduser()
    try:
        expected_auth_path = (agent_dir / "auth.json").resolve(strict=True)
    except OSError as error:
        raise Blocked("selected Pi agent directory has no canonical auth file") from error
    if auth_path != expected_auth_path:
        raise Blocked("auth evidence does not bind the selected Pi agent directory")
    if stat.S_IMODE(auth_path.stat().st_mode) != 0o600:
        raise Blocked("auth evidence path mode is not 0600")
    identity = auth["identity"]
    observed_identity = file_identity(auth_path)
    if not isinstance(identity, dict) or identity != observed_identity:
        mismatched = sorted(set(observed_identity) | (set(identity) if isinstance(identity, dict) else set()))
        if isinstance(identity, dict):
            mismatched = [key for key in mismatched if identity.get(key) != observed_identity.get(key)]
        raise Blocked("auth evidence file identity is stale or ambiguous (mismatched metadata: " + ",".join(mismatched) + ")")

    quota = packet["quota_evidence"]
    if not isinstance(quota, dict) or set(quota) != {"route", "source_confidence", "observed_at", "reset_at", "available"}:
        raise Blocked("quota evidence is unavailable or ambiguous")
    if quota["route"] != "chatgpt-codex-oauth-spark" or quota["source_confidence"] != "trusted" or quota["available"] is not True:
        raise Blocked("quota evidence does not prove available OAuth Spark capacity")
    quota_time = parse_time(quota["observed_at"], "quota observed_at")
    reset = parse_time(quota["reset_at"], "quota reset_at")
    if (now - quota_time).total_seconds() < -30 or (now - quota_time).total_seconds() > 300 or reset <= now:
        raise Blocked("quota evidence is stale or has an invalid reset window")

    settings_path = auth_path.parent / "settings.json"
    settings = load_json(settings_path, "shared Pi settings")
    retry = settings.get("retry")
    if not isinstance(retry, dict) or retry.get("enabled") is not False or retry.get("maxRetries", 0) != 0:
        raise Blocked("shared Pi settings do not disable agent retries")
    provider_retry = retry.get("provider")
    if not isinstance(provider_retry, dict) or provider_retry.get("maxRetries") != 0:
        raise Blocked("shared Pi settings do not disable provider retries")
    compaction = settings.get("compaction")
    if not isinstance(compaction, dict) or compaction.get("enabled") is not False:
        raise Blocked("shared Pi settings do not disable compaction")
    return auth_path


def run_probe(argv: list[str], timeout: int, limit: int) -> subprocess.CompletedProcess[bytes]:
    environment = os.environ.copy()
    environment.pop("NODE_OPTIONS", None)
    environment.pop("NODE_PATH", None)
    environment.update({"PI_SKIP_VERSION_CHECK": "1", "PI_OFFLINE": "1"})
    process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment)
    identity = process_identity(process.pid)
    stdout, stderr, timed_out, overflowed = finish_bounded_process(
        process, b"", identity, time.monotonic() + timeout, limit, limit,
    )
    if timed_out or overflowed or len(stdout) > limit or len(stderr) > limit:
        raise Blocked("Pi probe exceeded its output bound")
    return subprocess.CompletedProcess(argv, process.returncode, stdout, stderr)


def resolve_pi(pi_argument: str) -> tuple[pathlib.Path, pathlib.Path, str]:
    located = shutil.which(pi_argument) if os.sep not in pi_argument else pi_argument
    if not located:
        raise Blocked("Pi executable is unavailable")
    candidate = pathlib.Path(located).absolute()
    try:
        pi = candidate.resolve(strict=True)
    except OSError as error:
        raise Blocked("Pi executable cannot be resolved") from error
    if not pi.is_file() or pi.is_symlink():
        raise Blocked("resolved Pi executable object is not a regular file")
    if not os.access(pi, os.R_OK):
        raise Blocked("Pi executable is not executable")
    node_location = shutil.which("node", path=os.environ.get("PATH", ""))
    if not node_location:
        raise Blocked("Node interpreter for Pi is unavailable")
    node = pathlib.Path(node_location).resolve(strict=True)
    if not node.is_file() or not os.access(node, os.X_OK):
        raise Blocked("resolved Node interpreter is not executable")
    version = run_probe([str(node), str(pi), "--version"], 15, 4096)
    if version.returncode != 0 or version.stdout.decode().strip() != PI_VERSION:
        raise Blocked("Pi version is not exactly the supported version")
    catalog = run_probe([str(node), str(pi), "--list-models", MODEL], 30, 65536)
    matches = [line.split()[:2] for line in catalog.stdout.decode(errors="replace").splitlines()]
    if catalog.returncode != 0 or matches.count([PROVIDER, MODEL_ID]) != 1:
        raise Blocked("Pi model catalog does not resolve the exact Spark model once")
    return pi, node, PI_VERSION


def state_paths(state_dir: pathlib.Path, operation_id: str) -> tuple[pathlib.Path, pathlib.Path]:
    identifier = validate_operation_id(operation_id)
    return state_dir / "operations" / f"{identifier}.json", state_dir / "execute.lock"


def intent_path(state_dir: pathlib.Path, operation_id: str) -> pathlib.Path:
    identifier = validate_operation_id(operation_id)
    return state_dir / "intents" / f"{identifier}.json"


def validate_state_dir(path: pathlib.Path) -> pathlib.Path:
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    canonical = path.resolve(strict=True)
    info = canonical.stat()
    if path.is_symlink() or not canonical.is_dir() or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise Blocked("state directory must be an owner-controlled non-symlink 0700 directory")
    return canonical


def git_value(workdir: pathlib.Path, *arguments: str) -> str:
    result = subprocess.run(["git", "-C", str(workdir), *arguments], capture_output=True, timeout=10, check=False)
    if result.returncode != 0:
        raise Blocked("Git worktree identity is unavailable")
    return result.stdout.decode().rstrip("\n")


def worktree_identity(workdir: pathlib.Path) -> dict[str, Any]:
    info = workdir.stat()
    common = pathlib.Path(git_value(workdir, "rev-parse", "--git-common-dir"))
    if not common.is_absolute():
        common = workdir / common
    status = subprocess.run(
        ["git", "-C", str(workdir), "status", "--porcelain=v2", "-z", "--untracked-files=all"],
        capture_output=True, timeout=15, check=False,
    )
    if status.returncode != 0:
        raise Blocked("Git worktree status is unavailable")
    return {
        "device": str(info.st_dev), "inode": str(info.st_ino),
        "branch": git_value(workdir, "symbolic-ref", "--short", "HEAD"),
        "head": git_value(workdir, "rev-parse", "HEAD"),
        "common_dir": str(common.resolve(strict=True)),
        "status_sha256": hashlib.sha256(status.stdout).hexdigest(),
    }


def build_intent(packet_path: pathlib.Path, pi_argument: str) -> tuple[dict[str, Any], list[pathlib.Path]]:
    packet, workdir, allowed = validate_packet(packet_path)
    auth_path = validate_admission(packet, utcnow())
    pi, node, version = resolve_pi(pi_argument)
    parent_identities = {}
    for path in allowed:
        info = path.parent.stat()
        parent_identities[str(path.relative_to(workdir))] = {"path": str(path.parent), "device": str(info.st_dev), "inode": str(info.st_ino)}
    intent = {
        "schema": 1,
        "provider": PROVIDER,
        "model": MODEL_ID,
        "operation_id": packet["operation_id"],
        "packet_path": str(packet_path.resolve(strict=True)),
        "packet_sha256": digest_file(packet_path),
        "workdir": str(workdir),
        "allowed_paths": [str(path.relative_to(workdir)) for path in allowed],
        "allowed_before": {str(path.relative_to(workdir)): digest_file(path) for path in allowed},
        "allowed_identity": {str(path.relative_to(workdir)): file_identity(path) for path in allowed},
        "parent_identity": parent_identities,
        "worktree_identity": worktree_identity(workdir),
        "pi": file_identity(pi),
        "pi_sha256": digest_file(pi),
        "node": file_identity(node),
        "node_sha256": digest_file(node),
        "pi_version": version,
        "argv": [str(node), str(pi), *PI_ARGS],
        "auth": file_identity(auth_path),
        "settings_sha256": digest_file(auth_path.parent / "settings.json"),
        "quota_before": packet["quota_evidence"],
        "timeout_seconds": packet["timeout_seconds"],
        "stdout_limit": packet["stdout_limit"],
        "stderr_limit": packet["stderr_limit"],
        "event_limit": packet["event_limit"],
        "status": "planned",
    }
    return intent, allowed


def atomic_json(path: pathlib.Path, value: dict[str, Any], mode: int = 0o600) -> None:
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    descriptor, temporary = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as output:
            json.dump(value, output, sort_keys=True, separators=(",", ":"))
            output.write("\n")
            output.flush()
            os.fsync(output.fileno())
        os.chmod(temporary, mode)
        os.replace(temporary, path)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def create_plan(state_dir: pathlib.Path, packet_path: pathlib.Path, pi_argument: str) -> dict[str, Any]:
    intent, _ = build_intent(packet_path, pi_argument)
    receipt, _ = state_paths(state_dir, intent["operation_id"])
    immutable = intent_path(state_dir, intent["operation_id"])
    receipt.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    immutable.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    if receipt.exists() or immutable.exists():
        raise Blocked("operation identity already exists")
    atomic_json(immutable, intent, 0o400)
    operation = {
        "schema": 1, "operation_id": intent["operation_id"],
        "intent_sha256": digest_file(immutable), "status": "planned",
    }
    try:
        descriptor = os.open(receipt, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    except FileExistsError as error:
        raise Blocked("operation identity already exists") from error
    os.close(descriptor)
    atomic_json(receipt, operation)
    return {**intent, **operation}


def load_operation(state_dir: pathlib.Path, requested_id: str) -> tuple[pathlib.Path, dict[str, Any], dict[str, Any]]:
    receipt_path, _ = state_paths(state_dir, requested_id)
    operation = load_json(receipt_path, "operation state")
    if operation.get("operation_id") != requested_id:
        raise Blocked("operation state identity does not match the request")
    immutable = intent_path(state_dir, requested_id)
    try:
        immutable_info = immutable.stat()
    except OSError as error:
        raise Blocked("immutable execution intent is unavailable") from error
    if immutable.is_symlink() or stat.S_IMODE(immutable_info.st_mode) != 0o400:
        raise Blocked("immutable execution intent type or mode changed")
    intent = load_json(immutable, "immutable execution intent")
    if digest_file(immutable) != operation.get("intent_sha256") or intent.get("operation_id") != requested_id:
        raise Blocked("immutable execution intent digest or identity changed")
    intent_fields = {
        "schema", "provider", "model", "operation_id", "packet_path", "packet_sha256", "workdir",
        "allowed_paths", "allowed_before", "allowed_identity", "parent_identity", "worktree_identity",
        "pi", "pi_sha256", "node", "node_sha256", "pi_version", "argv", "auth", "settings_sha256",
        "quota_before", "timeout_seconds", "stdout_limit", "stderr_limit", "event_limit", "status",
    }
    operation_fields = {
        "schema", "operation_id", "intent_sha256", "status", "started_at", "process", "ended_at",
        "exit_status", "stdout_bytes", "stderr_bytes", "stderr_summary", "reason", "event_count",
        "summary", "changed_paths", "result_trust", "billing_route", "quota_confirmed_at", "quota_after",
        "quota_after_sha256", "quota_after_identity",
    }
    operation_base = {"schema", "operation_id", "intent_sha256", "status"}
    if (
        set(intent) != intent_fields or intent.get("status") != "planned"
        or not operation_base.issubset(operation) or not set(operation).issubset(operation_fields)
        or operation.get("schema") != 1
    ):
        raise Blocked("immutable intent or operation state schema is not exact")
    return receipt_path, operation, intent


class ProcBSDInfo(ctypes.Structure):
    _fields_ = [
        ("flags", ctypes.c_uint32), ("status", ctypes.c_uint32), ("xstatus", ctypes.c_uint32),
        ("pid", ctypes.c_uint32), ("ppid", ctypes.c_uint32), ("uid", ctypes.c_uint32),
        ("gid", ctypes.c_uint32), ("ruid", ctypes.c_uint32), ("rgid", ctypes.c_uint32),
        ("svuid", ctypes.c_uint32), ("svgid", ctypes.c_uint32), ("rfu", ctypes.c_uint32),
        ("comm", ctypes.c_char * 16), ("name", ctypes.c_char * 32),
        ("nfiles", ctypes.c_uint32), ("pgid", ctypes.c_uint32), ("pjobc", ctypes.c_uint32),
        ("e_tdev", ctypes.c_uint32), ("e_tpgid", ctypes.c_uint32), ("nice", ctypes.c_int32),
        ("start_tvsec", ctypes.c_uint64), ("start_tvusec", ctypes.c_uint64),
    ]


def process_identity(pid: int) -> dict[str, Any] | None:
    if sys.platform != "darwin":
        raise Blocked("local Pi process incarnation verification is currently supported only on Darwin")
    library = ctypes.CDLL("/usr/lib/libproc.dylib", use_errno=True)
    info = ProcBSDInfo()
    size = library.proc_pidinfo(pid, 3, 0, ctypes.byref(info), ctypes.sizeof(info))
    if size <= 0:
        return None
    if size != ctypes.sizeof(info) or info.pid != pid:
        raise Blocked("Darwin returned an incomplete process identity")
    path_buffer = ctypes.create_string_buffer(4096)
    path_size = library.proc_pidpath(pid, path_buffer, len(path_buffer))
    if path_size <= 0:
        raise Blocked("Darwin process executable path is unavailable")
    executable = pathlib.Path(os.fsdecode(path_buffer.value)).resolve(strict=True)
    return {
        "pid": pid,
        "start_seconds": str(info.start_tvsec),
        "start_microseconds": str(info.start_tvusec),
        "executable": str(executable),
        "executable_identity": file_identity(executable),
    }


def stable_process_identity(pid: int, deadline: float) -> dict[str, Any] | None:
    previous = None
    while time.monotonic() < deadline:
        current = process_identity(pid)
        if current is None:
            return None
        if current == previous:
            return current
        previous = current
        time.sleep(0.05)
    raise Blocked("Pi process identity did not stabilize before the operation deadline")


def extract_result(events: bytes, event_limit: int, workdir: pathlib.Path) -> tuple[dict[str, Any], int]:
    lines = events.splitlines()
    if not lines or len(lines) > event_limit:
        raise Blocked("Pi event count is empty or overflowed")
    parsed: list[dict[str, Any]] = []
    for line in lines:
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise Blocked("Pi emitted an invalid JSON event") from error
        if not isinstance(event, dict):
            raise Blocked("Pi emitted a non-object event")
        parsed.append(event)
    allowed_types = {
        "session", "agent_start", "turn_start", "message_start", "message_update",
        "message_end", "turn_end", "agent_end", "agent_settled",
    }
    if any(event.get("type") not in allowed_types for event in parsed):
        raise Blocked("Pi emitted an unexpected, retry, tool, or compaction event")
    sessions = [event for event in parsed if event.get("type") == "session"]
    starts = [event for event in parsed if event.get("type") == "agent_start"]
    ends = [event for event in parsed if event.get("type") == "agent_end"]
    if (
        len(sessions) != 1 or parsed[0] is not sessions[0] or sessions[0].get("version") != 3
        or sessions[0].get("cwd") != str(workdir) or len(starts) != 1 or len(ends) != 1
        or ends[0].get("willRetry") is not False
    ):
        raise Blocked("Pi session, attempt, retry, or workdir provenance is invalid")
    assistant = [event.get("message") for event in parsed if event.get("type") == "message_end" and isinstance(event.get("message"), dict) and event["message"].get("role") == "assistant"]
    valid = [message for message in assistant if message.get("provider") == PROVIDER and message.get("model") == MODEL_ID and message.get("stopReason") == "stop"]
    settled = [event for event in parsed if event.get("type") == "agent_settled"]
    if len(assistant) != 1 or len(valid) != 1 or len(settled) != 1 or parsed[-1].get("type") != "agent_settled":
        raise Blocked("Pi completion or exact inference provenance is unavailable")
    content = valid[0].get("content")
    if not isinstance(content, list):
        raise Blocked("Pi assistant content is unavailable")
    text = "".join(block.get("text", "") for block in content if isinstance(block, dict) and block.get("type") == "text")
    try:
        replacement = json.loads(text)
    except json.JSONDecodeError as error:
        raise Blocked("Pi result is not the strict JSON replacement envelope") from error
    if not isinstance(replacement, dict) or set(replacement) != {"summary", "files"} or not isinstance(replacement["summary"], str) or not isinstance(replacement["files"], list):
        raise Blocked("Pi result replacement envelope has an invalid shape")
    return replacement, len(lines)


def task_prompt(packet: dict[str, Any], workdir: pathlib.Path, allowed: list[pathlib.Path]) -> bytes:
    files = []
    total = 0
    for path in allowed:
        content = path.read_text()
        total += len(content.encode())
        if total > 131072:
            raise Blocked("allowed file context exceeds 128 KiB")
        files.append({"path": str(path.relative_to(workdir)), "content": content})
    request = {
        "goal": packet["goal"],
        "expected_output": packet["expected_output"],
        "allowed_files": files,
        "response_schema": {"summary": "string", "files": [{"path": "allowed relative path", "content": "complete replacement text"}]},
        "rules": ["Return JSON only", "Replace only listed files", "Do not request or claim validation, publishing, delegation, cleanup, or repository authority"],
    }
    encoded = (json.dumps(request, separators=(",", ":")) + "\n").encode()
    if len(encoded) > 153600:
        raise Blocked("Pi input exceeds 150 KiB")
    return encoded


def bounded_read(stream: Any, limit: int, destination: bytearray, overflow: threading.Event) -> None:
    while True:
        chunk = stream.read(65536)
        if not chunk:
            return
        remaining = limit + 1 - len(destination)
        if remaining > 0:
            destination.extend(chunk[:remaining])
        if len(destination) > limit:
            overflow.set()


def bounded_write(stream: Any, prompt: bytes) -> None:
    try:
        stream.write(prompt)
        stream.close()
    except (BrokenPipeError, OSError):
        pass


def stop_exact_process(process: subprocess.Popen[bytes], identity: dict[str, Any]) -> None:
    if identity is None:
        raise Blocked("Pi process identity is unavailable; exact stop refused")
    if process_identity(process.pid) != identity:
        raise Blocked("Pi process identity changed; exact stop refused")
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        if process_identity(process.pid) != identity:
            raise Blocked("Pi process identity changed; forced stop refused")
        process.kill()
        process.wait(timeout=5)


def finish_bounded_process(
    process: subprocess.Popen[bytes], prompt: bytes, identity: dict[str, Any] | None, deadline: float,
    stdout_limit: int, stderr_limit: int,
) -> tuple[bytes, bytes, bool, bool]:
    stdout_buffer = bytearray()
    stderr_buffer = bytearray()
    overflow = threading.Event()
    readers = [
        threading.Thread(target=bounded_read, args=(process.stdout, stdout_limit, stdout_buffer, overflow), daemon=True),
        threading.Thread(target=bounded_read, args=(process.stderr, stderr_limit, stderr_buffer, overflow), daemon=True),
    ]
    for reader in readers:
        reader.start()
    assert process.stdin is not None
    writer = threading.Thread(target=bounded_write, args=(process.stdin, prompt), daemon=True)
    writer.start()
    timed_out = False
    while process.poll() is None:
        if overflow.is_set():
            stop_exact_process(process, identity)
            break
        if time.monotonic() >= deadline:
            timed_out = True
            stop_exact_process(process, identity)
            break
        time.sleep(0.01)
    for reader in [writer, *readers]:
        reader.join(timeout=5)
        if reader.is_alive():
            raise Blocked("Pi output stream did not close after exact process exit")
    return bytes(stdout_buffer), bytes(stderr_buffer), timed_out, overflow.is_set()


def replace_allowed_file(
    operation: str, target: pathlib.Path, content: str,
    file_expected: dict[str, Any], parent_expected: dict[str, Any], expected_digest: str,
) -> None:
    try:
        directory_fd = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    except OSError as error:
        raise Blocked("allowed parent directory could not be opened without following links") from error
    temporary_name = f".{target.name}.amux-pi-{operation}"
    applied = False
    try:
        parent_info = os.fstat(directory_fd)
        if {"path": str(target.parent), "device": str(parent_info.st_dev), "inode": str(parent_info.st_ino)} != parent_expected:
            raise Blocked("allowed parent directory identity changed")
        target_fd = os.open(target.name, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory_fd)
        try:
            target_info = os.fstat(target_fd)
            observed = {
                "path": str(target), "device": str(target_info.st_dev), "inode": str(target_info.st_ino),
                "mode": stat.S_IMODE(target_info.st_mode), "size": target_info.st_size,
                "mtime_ns": str(target_info.st_mtime_ns),
            }
            digest = hashlib.sha256()
            while True:
                chunk = os.read(target_fd, 65536)
                if not chunk:
                    break
                digest.update(chunk)
            if observed != file_expected or digest.hexdigest() != expected_digest:
                raise Blocked("allowed file identity changed before replacement")
        finally:
            os.close(target_fd)
        descriptor = os.open(
            temporary_name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
            file_expected["mode"], dir_fd=directory_fd,
        )
        try:
            encoded = content.encode()
            offset = 0
            while offset < len(encoded):
                offset += os.write(descriptor, encoded[offset:])
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        os.rename(temporary_name, target.name, src_dir_fd=directory_fd, dst_dir_fd=directory_fd)
        applied = True
        os.fsync(directory_fd)
    except OSError as error:
        if applied:
            raise Indeterminate("replacement was applied but durable completion is uncertain") from error
        raise Blocked("race-safe allowed-file replacement failed") from error
    finally:
        try:
            os.unlink(temporary_name, dir_fd=directory_fd)
        except FileNotFoundError:
            pass
        os.close(directory_fd)


def execute(state_dir: pathlib.Path, operation_id: str) -> dict[str, Any]:
    receipt_path, receipt, intent = load_operation(state_dir, operation_id)
    _, lock_path = state_paths(state_dir, operation_id)
    if set(receipt) != {"schema", "operation_id", "intent_sha256", "status"} or receipt.get("status") != "planned":
        raise Blocked("operation is not in exact planned state")
    lock_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock = lock_path.open("a+")
    try:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise Blocked("another local Pi operation is active") from error
        packet_path = pathlib.Path(intent["packet_path"])
        if digest_file(packet_path) != intent["packet_sha256"]:
            raise Blocked("task packet changed after immutable intent")
        packet, workdir, allowed = validate_packet(packet_path)
        validate_admission(packet, utcnow())
        fresh_intent, _ = build_intent(packet_path, intent["pi"]["path"])
        if fresh_intent != intent:
            raise Blocked("immutable execution intent no longer matches current admission identity")
        pi_path = pathlib.Path(intent["pi"]["path"])
        node_path = pathlib.Path(intent["node"]["path"])
        argv = [str(node_path), str(pi_path), *PI_ARGS]
        if intent["argv"] != argv:
            raise Blocked("immutable normalized Pi argv changed")
        prompt = task_prompt(packet, workdir, allowed)
        environment = os.environ.copy()
        environment.pop("NODE_OPTIONS", None)
        environment.pop("NODE_PATH", None)
        environment["PATH"] = os.pathsep.join(dict.fromkeys([str(node_path.parent), "/usr/local/bin", "/usr/bin", "/bin"]))
        environment.update({"PI_SKIP_VERSION_CHECK": "1", "PI_OFFLINE": "1"})
        started = utcnow()
        deadline = time.monotonic() + intent["timeout_seconds"]
        receipt.update({"status": "attempt_started", "started_at": started.isoformat()})
        atomic_json(receipt_path, receipt)
        try:
            process = subprocess.Popen(argv, cwd=workdir, env=environment, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        except OSError as error:
            receipt.update({"status": "blocked", "reason": "Pi launch failed before process identity"})
            atomic_json(receipt_path, receipt)
            raise Blocked("Pi launch failed") from error
        identity = stable_process_identity(process.pid, deadline)
        if identity is None:
            receipt.update({"status": "indeterminate", "reason": "Pi exited before process identity was recorded"})
            atomic_json(receipt_path, receipt)
            raise Blocked("Pi process identity was unavailable after launch")
        if identity["executable_identity"] != intent["node"]:
            receipt.update({"status": "indeterminate", "reason": "launched process executable did not match planned Node"})
            atomic_json(receipt_path, receipt)
            stop_exact_process(process, identity)
            raise Blocked("Pi process executable does not match immutable Node identity")
        receipt.update({"status": "running", "started_at": started.isoformat(), "process": identity})
        atomic_json(receipt_path, receipt)
        try:
            stdout, stderr, timed_out, overflowed = finish_bounded_process(
                process, prompt, identity, deadline, intent["stdout_limit"], intent["stderr_limit"]
            )
        except Blocked as error:
            receipt.update({"status": "indeterminate", "reason": str(error)})
            atomic_json(receipt_path, receipt)
            raise
        ended = utcnow()
        receipt.update({
            "ended_at": ended.isoformat(), "exit_status": process.returncode,
            "stdout_bytes": len(stdout), "stderr_bytes": len(stderr),
            "stderr_summary": "empty" if not stderr else "present_redacted",
        })
        if timed_out:
            receipt.update({"status": "timeout", "reason": "wall-clock timeout"})
            atomic_json(receipt_path, receipt)
            raise Blocked("Pi operation timed out")
        if overflowed or len(stdout) > intent["stdout_limit"] or len(stderr) > intent["stderr_limit"]:
            receipt.update({"status": "blocked", "reason": "output overflow"})
            atomic_json(receipt_path, receipt)
            raise Blocked("Pi output exceeded its bound")
        if process.returncode != 0:
            receipt.update({"status": "blocked", "reason": "nonzero exit"})
            atomic_json(receipt_path, receipt)
            raise Blocked("Pi exited nonzero")
        try:
            replacement, count = extract_result(stdout, intent["event_limit"], workdir)
        except Blocked as error:
            receipt.update({"status": "blocked", "reason": str(error)})
            atomic_json(receipt_path, receipt)
            raise
        requested = {str(path.relative_to(workdir)): path for path in allowed}
        returned = replacement["files"]
        if len(returned) != 1:
            receipt.update({"status": "blocked", "reason": "Pi returned a non-single replacement set"})
            atomic_json(receipt_path, receipt)
            raise Blocked("Pi returned a non-single replacement set")
        seen: set[str] = set()
        for item in returned:
            if not isinstance(item, dict) or set(item) != {"path", "content"} or item.get("path") not in requested or item["path"] in seen or not isinstance(item.get("content"), str):
                receipt.update({"status": "blocked", "reason": "Pi returned a replacement outside the allowed scope"})
                atomic_json(receipt_path, receipt)
                raise Blocked("Pi returned a replacement outside the allowed scope")
            if len(item["content"].encode()) > 131072:
                receipt.update({"status": "blocked", "reason": "Pi returned an oversized file replacement"})
                atomic_json(receipt_path, receipt)
                raise Blocked("Pi returned an oversized file replacement")
            seen.add(item["path"])
        if worktree_identity(workdir) != intent["worktree_identity"]:
            receipt.update({"status": "blocked", "reason": "worktree identity changed during Pi execution"})
            atomic_json(receipt_path, receipt)
            raise Blocked("worktree identity changed during Pi execution")
        item = returned[0]
        target = requested[item["path"]]
        try:
            replace_allowed_file(
                operation_id, target, item["content"], intent["allowed_identity"][item["path"]],
                intent["parent_identity"][item["path"]], intent["allowed_before"][item["path"]],
            )
        except Indeterminate as error:
            receipt.update({"status": "indeterminate", "reason": str(error)})
            atomic_json(receipt_path, receipt)
            raise
        except Blocked as error:
            receipt.update({"status": "blocked", "reason": str(error)})
            atomic_json(receipt_path, receipt)
            raise
        receipt.update({
            "status": "awaiting_quota_confirmation", "event_count": count, "summary": replacement["summary"],
            "changed_paths": sorted(seen), "result_trust": "untrusted_pending_coordinator_review_and_validation",
        })
        atomic_json(receipt_path, receipt)
        return {**intent, **receipt}
    finally:
        lock.close()


def inspect(state_dir: pathlib.Path, operation_id: str, recover: bool) -> dict[str, Any]:
    receipt_path, receipt, intent = load_operation(state_dir, operation_id)
    if not recover:
        return {**intent, **receipt}
    _, lock_path = state_paths(state_dir, operation_id)
    with lock_path.open("a+") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise Blocked("another local Pi operation is active") from error
        receipt_path, receipt, intent = load_operation(state_dir, operation_id)
        if receipt.get("status") in {"attempt_started", "running"}:
            expected = receipt.get("process")
            if not isinstance(expected, dict) or not isinstance(expected.get("pid"), int):
                receipt.update({"status": "indeterminate", "reason": "launch was interrupted before exact process identity was recorded"})
                atomic_json(receipt_path, receipt)
            else:
                current = process_identity(expected["pid"])
                if current is None:
                    receipt.update({"status": "indeterminate", "reason": "interrupted process is exactly absent; semantic completion unknown"})
                    atomic_json(receipt_path, receipt)
                elif current != expected:
                    raise Blocked("live process identity changed; recovery stop refused")
                else:
                    raise Blocked("exact Pi process is still live; automatic recovery stop is not authorized")
        return {**intent, **receipt}


def finalize(state_dir: pathlib.Path, operation_id: str, quota_after_path: pathlib.Path) -> dict[str, Any]:
    _, lock_path = state_paths(state_dir, operation_id)
    with lock_path.open("a+") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise Blocked("another local Pi operation is active") from error
        receipt_path, receipt, intent = load_operation(state_dir, operation_id)
        if receipt.get("status") != "awaiting_quota_confirmation":
            raise Blocked("operation is not awaiting quota confirmation")
        evidence_path = canonical_existing_file(str(quota_after_path.absolute()), "post-run quota evidence")
        evidence = load_json(evidence_path, "post-run quota evidence")
        required = {"route", "source_confidence", "observed_at", "reset_at", "usage_increased"}
        if set(evidence) != required or evidence.get("route") != "chatgpt-codex-oauth-spark" or evidence.get("source_confidence") != "trusted" or evidence.get("usage_increased") is not True:
            raise Blocked("post-run quota evidence does not prove the OAuth Spark billing route")
        now = utcnow()
        observed = parse_time(evidence["observed_at"], "post-run quota observed_at")
        ended = parse_time(receipt["ended_at"], "execution ended_at")
        reset = parse_time(evidence["reset_at"], "post-run quota reset_at")
        baseline_reset = parse_time(intent["quota_before"]["reset_at"], "baseline quota reset_at")
        age = (now - observed).total_seconds()
        if observed < ended or age < -30 or age > 300 or observed >= reset:
            raise Blocked("post-run quota evidence is stale, future-dated, predates execution, or follows reset")
        if reset != baseline_reset:
            raise Blocked("post-run quota reset window differs from the admitted baseline")
        receipt.update({
            "status": "success", "billing_route": "chatgpt-codex-oauth-spark",
            "quota_confirmed_at": evidence["observed_at"], "quota_after": evidence,
            "quota_after_sha256": digest_file(evidence_path), "quota_after_identity": file_identity(evidence_path),
        })
        atomic_json(receipt_path, receipt)
        return {**intent, **receipt}


def public_result(value: dict[str, Any]) -> dict[str, Any]:
    allowed = {
        "schema", "provider", "model", "operation_id", "pi_version", "argv", "status",
        "started_at", "ended_at", "exit_status", "stdout_bytes", "stderr_bytes",
        "stderr_summary", "reason", "event_count", "summary", "changed_paths", "result_trust",
        "billing_route", "quota_confirmed_at",
    }
    sanitized = {key: value[key] for key in allowed if key in value}
    if "argv" in sanitized:
        sanitized["argv"] = [pathlib.Path(value).name if index < 2 else value for index, value in enumerate(sanitized["argv"])]
    return sanitized


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--state-dir", type=pathlib.Path, required=True)
    parser.add_argument("--pi", default="pi")
    commands = parser.add_subparsers(dest="command", required=True)
    preflight = commands.add_parser("preflight")
    preflight.add_argument("--packet", type=pathlib.Path, required=True)
    plan = commands.add_parser("plan")
    plan.add_argument("--packet", type=pathlib.Path, required=True)
    execute_parser = commands.add_parser("execute")
    execute_parser.add_argument("--operation-id", required=True)
    finalize_parser = commands.add_parser("finalize")
    finalize_parser.add_argument("--operation-id", required=True)
    finalize_parser.add_argument("--quota-after", type=pathlib.Path, required=True)
    for name in ("inspect", "recover"):
        command = commands.add_parser(name)
        command.add_argument("--operation-id", required=True)
    arguments = parser.parse_args()
    try:
        arguments.state_dir = validate_state_dir(arguments.state_dir)
        if arguments.command == "preflight":
            result, _ = build_intent(arguments.packet, arguments.pi)
        elif arguments.command == "plan":
            result = create_plan(arguments.state_dir, arguments.packet, arguments.pi)
        elif arguments.command == "execute":
            result = execute(arguments.state_dir, arguments.operation_id)
        elif arguments.command == "finalize":
            result = finalize(arguments.state_dir, arguments.operation_id, arguments.quota_after)
        else:
            result = inspect(arguments.state_dir, arguments.operation_id, arguments.command == "recover")
        print(json.dumps(public_result(result), sort_keys=True, separators=(",", ":")))
        return 0
    except Blocked as error:
        print(f"pi-spark-local: blocked: {error}", file=sys.stderr)
        return 2
    except Exception as error:
        print(f"pi-spark-local: runtime failure: {type(error).__name__}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
