#!/usr/bin/env python3
"""Provider-owned state machine for one bounded mutating Claude fresh-Orb run.

This helper never launches Claude, creates an Orb, archives a thread, cleans a
workspace, or integrates a commit.  It makes the authority and handoff gates
around those native Amp operations durable and independently checkable.
"""

from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import pathlib
import re
import stat
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from typing import Any, Callable


MODEL = "claude-opus-4-8"
SCHEMA = 1
MAX_INPUT = 256 * 1024
MAX_ARTIFACT = 64 * 1024 * 1024
MAX_METADATA = 256 * 1024
CAPABILITIES = (
    "cli_support", "authentication", "entitlement", "availability",
    "capacity", "charge_route",
)
FORBIDDEN_AUTHORITY = (
    "push", "pull_request", "merge", "release", "issue_mutation",
    "secret_management", "infrastructure", "archive", "cleanup",
    "recursive_delegation",
)
SENSITIVE_KEY = re.compile(
    r"(^|_)(prompt|transcript|token|secret|credential|provider_output|session_metadata)(_|$)",
    re.IGNORECASE,
)
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
GIT_SHA = re.compile(r"[0-9a-f]{40,64}\Z")


class WorkflowError(Exception):
    pass


def now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def digest(value: Any) -> str:
    raw = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(raw).hexdigest()


def file_digest(path: pathlib.Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            value.update(chunk)
    return value.hexdigest()


def read_json(stream: Any = sys.stdin.buffer) -> dict[str, Any]:
    raw = stream.read(MAX_INPUT + 1)
    if not raw or len(raw) > MAX_INPUT:
        raise WorkflowError("input must be one bounded JSON object")

    def pairs(items: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in items:
            if key in result:
                raise WorkflowError("duplicate JSON field")
            result[key] = value
        return result

    try:
        value = json.loads(raw, object_pairs_hook=pairs)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        raise WorkflowError("invalid JSON input") from error
    if not isinstance(value, dict):
        raise WorkflowError("input must be a JSON object")
    reject_sensitive(value)
    return value


def load_json(path: pathlib.Path) -> dict[str, Any]:
    if not path.is_file() or path.stat().st_size > MAX_INPUT:
        raise WorkflowError("bounded JSON file is unavailable")
    with path.open("rb") as source:
        return read_json(source)


def reject_sensitive(value: Any) -> None:
    if isinstance(value, dict):
        for key, nested in value.items():
            if SENSITIVE_KEY.search(key):
                raise WorkflowError(f"privacy-sensitive field is forbidden: {key}")
            reject_sensitive(nested)
    elif isinstance(value, list):
        for nested in value:
            reject_sensitive(nested)


def string(request: dict[str, Any], key: str, limit: int = 512) -> str:
    value = request.get(key)
    if not isinstance(value, str) or not value or len(value.encode()) > limit or "\n" in value:
        raise WorkflowError(f"{key} must be a bounded non-empty string")
    return value


def exact_keys(value: dict[str, Any], keys: set[str], label: str) -> None:
    if set(value) != keys:
        raise WorkflowError(f"{label} fields do not match the exact contract")


def valid_sha(value: Any, label: str, git: bool = False) -> str:
    if not isinstance(value, str) or (GIT_SHA if git else SHA256).fullmatch(value) is None:
        raise WorkflowError(f"{label} is not a valid digest")
    return value


def git(repo: pathlib.Path, *args: str, timeout: int = 30) -> str:
    try:
        process = subprocess.run(
            ["git", "-C", str(repo), *args], text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise WorkflowError("bounded Git inspection failed") from error
    if process.returncode != 0 or len(process.stdout.encode()) > MAX_INPUT:
        raise WorkflowError("bounded Git inspection failed")
    return process.stdout.strip()


def validate_binding(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise WorkflowError("binding must be an object")
    keys = {
        "operation_id", "origin_thread", "orb_thread", "repository", "base_sha",
        "branch", "worktree_id", "task_sha256", "executable_path",
        "executable_version", "argv", "model", "auth_route", "execution_id",
        "child_limit", "depth", "attempt_limit", "check_argv", "allowed_paths",
        "authority",
    }
    exact_keys(value, keys, "binding")
    operation = string(value, "operation_id", 128)
    if re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", operation) is None:
        raise WorkflowError("operation_id has an invalid shape")
    origin = string(value, "origin_thread", 128)
    orb = string(value, "orb_thread", 128)
    if not origin.startswith("T-") or not orb.startswith("T-") or origin == orb:
        raise WorkflowError("origin and fresh Orb identities must be distinct canonical threads")
    valid_sha(string(value, "base_sha", 64), "base_sha", git=True)
    valid_sha(string(value, "task_sha256", 64), "task_sha256")
    valid_sha(string(value, "worktree_id", 64), "worktree_id")
    valid_sha(string(value, "execution_id", 64), "execution_id")
    string(value, "repository", 1024)
    branch = string(value, "branch", 256)
    if branch.startswith(("-", "/")) or ".." in branch or re.search(r"[~^:?*\\\[\s]", branch):
        raise WorkflowError("branch has an invalid shape")
    string(value, "executable_version", 128)
    if value.get("model") != MODEL:
        raise WorkflowError(f"model must be the exact literal {MODEL}")
    if value.get("child_limit") != 1 or value.get("depth") != 0 or value.get("attempt_limit") != 1:
        raise WorkflowError("operation must bind one child, depth zero, and one attempt")
    if value.get("auth_route") != "project_secret_oauth_first_party":
        raise WorkflowError("authentication route must be exact first-party project-secret OAuth")
    executable = pathlib.PurePath(string(value, "executable_path", 2048))
    if not executable.is_absolute():
        raise WorkflowError("executable_path must be absolute")
    argv = value.get("argv")
    if not isinstance(argv, list) or not argv or any(not isinstance(x, str) or not x for x in argv):
        raise WorkflowError("argv must be a bounded string array")
    if len(argv) > 64 or sum(len(x.encode()) for x in argv) > 16 * 1024:
        raise WorkflowError("argv exceeds bounds")
    forbidden = {
        "--fallback-model", "--dangerously-skip-permissions", "--continue", "--resume", "--session-id",
    }
    if any(item in forbidden or any(item.startswith(flag + "=") for flag in forbidden) for item in argv):
        raise WorkflowError("normalized argv contains a forbidden control")
    require_flag(argv, "--model", MODEL)
    require_flag(argv, "--output-format", "json")
    require_flag(argv, "--permission-mode", "dontAsk")
    require_flag(argv, "--mcp-config", '{"mcpServers":{}}')
    turns = require_flag(argv, "--max-turns")
    if re.fullmatch(r"[1-9][0-9]?", turns) is None or int(turns) > 32:
        raise WorkflowError("max turns must be in the bounded range 1..32")
    for flag in (
        "--print", "--no-session-persistence", "--safe-mode", "--disable-slash-commands",
        "--strict-mcp-config",
    ):
        if argv.count(flag) != 1:
            raise WorkflowError(f"normalized argv requires one exact {flag}")
    tools = require_flag(argv, "--tools").split(",")
    allowed_tools = require_flag(argv, "--allowedTools").split(",")
    if tools != allowed_tools or not {"Edit", "Write"}.intersection(tools):
        raise WorkflowError("mutation tool profile is not exact")
    if not set(tools).issubset({"Read", "Grep", "Glob", "Edit", "Write", "Bash"}):
        raise WorkflowError("mutation tool profile contains undeclared authority")
    denied = set(require_flag(argv, "--disallowedTools").split(","))
    if denied != {"Agent", "WebFetch", "WebSearch", "mcp__*"}:
        raise WorkflowError("mutation deny profile is not exact")
    checks = value.get("check_argv")
    if not isinstance(checks, list) or len(checks) > 8:
        raise WorkflowError("check_argv must be a bounded list")
    for check in checks:
        if not isinstance(check, list) or not check or len(check) > 16 or any(not isinstance(x, str) or not x for x in check):
            raise WorkflowError("each check must be a bounded argv array")
    paths = value.get("allowed_paths")
    if not isinstance(paths, list) or not paths or len(paths) > 64:
        raise WorkflowError("allowed_paths must be a non-empty bounded list")
    for path in paths:
        if not isinstance(path, str) or not path or path.startswith(("/", "../")) or "//" in path:
            raise WorkflowError("allowed path must be repository-relative")
    authority = value.get("authority")
    if not isinstance(authority, dict):
        raise WorkflowError("authority must be an object")
    exact_keys(authority, {"mutation", "integration", "forbidden"}, "authority")
    if authority.get("mutation") != "one_bounded_commit_in_dedicated_worktree" or authority.get("integration") != "amp_coordinator_only":
        raise WorkflowError("mutation and integration authority are not exact")
    if authority.get("forbidden") != list(FORBIDDEN_AUTHORITY):
        raise WorkflowError("forbidden authority list is not exact")
    return value


def require_flag(argv: list[str], flag: str, expected: str | None = None) -> str:
    if argv.count(flag) != 1:
        raise WorkflowError(f"normalized argv requires one exact {flag}")
    index = argv.index(flag)
    if index + 1 >= len(argv) or argv[index + 1].startswith("--"):
        raise WorkflowError(f"normalized argv lacks the {flag} value")
    value = argv[index + 1]
    if expected is not None and value != expected:
        raise WorkflowError(f"normalized argv has the wrong {flag} value")
    return value


def event(receipt: dict[str, Any], kind: str) -> dict[str, Any] | None:
    return next((item for item in receipt["events"] if item["kind"] == kind), None)


def append_event(
    receipt: dict[str, Any], request: dict[str, Any], kind: str, data: dict[str, Any], *, singleton: bool = True,
) -> str:
    event_id = string(request, "event_id", 128)
    candidate = {"event_id": event_id, "kind": kind, "data": data, "request_sha256": digest(request)}
    existing = next((item for item in receipt["events"] if item["event_id"] == event_id), None)
    if existing:
        if {key: existing[key] for key in ("event_id", "kind", "data", "request_sha256")} != candidate:
            raise WorkflowError("event ID was already used for different content")
        return "duplicate"
    if singleton and event(receipt, kind):
        raise WorkflowError(f"{kind} already has a different event")
    candidate["at"] = now()
    receipt["events"].append(candidate)
    receipt["updated_at"] = candidate["at"]
    return "recorded"


class Store:
    def __init__(self, root: pathlib.Path):
        self.root = root.expanduser().resolve()
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(self.root, 0o700)
        self.lock_path = self.root / ".lock"

    def path(self, operation: str) -> pathlib.Path:
        if re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", operation) is None:
            raise WorkflowError("invalid operation selector")
        return self.root / f"{operation}.json"

    def locked(self, operation: str, callback: Callable[[dict[str, Any]], Any]) -> Any:
        with self.lock_path.open("a+b") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            path = self.path(operation)
            if not path.is_file() or path.stat().st_size > MAX_INPUT:
                raise WorkflowError("durable receipt is unavailable")
            receipt = json.loads(path.read_text())
            result = callback(receipt)
            write_atomic(path, receipt)
            return result

    def create(self, binding: dict[str, Any], event_id: str) -> str:
        binding = validate_binding(binding)
        path = self.path(binding["operation_id"])
        packet = self.root / f"{binding['operation_id']}.packet.json"
        packet_value = {"schema": SCHEMA, "binding": binding, "binding_sha256": digest(binding)}
        with self.lock_path.open("a+b") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            receipt = {
                "schema": SCHEMA, "binding": binding, "binding_sha256": digest(binding),
                "created_at": now(), "updated_at": now(), "events": [{
                    "event_id": event_id, "kind": "launch_intent_persisted", "at": now(),
                    "data": {"authority": "not_granted", "attempts": 0},
                }],
            }
            if path.exists():
                existing = json.loads(path.read_text())
                if existing["binding_sha256"] == receipt["binding_sha256"] and existing["events"][0]["event_id"] == event_id:
                    if not packet.exists() or load_json(packet) != packet_value:
                        write_atomic(packet, packet_value)
                    return "duplicate"
                raise WorkflowError("operation identity is already bound")
            write_atomic(packet, packet_value)
            write_atomic(path, receipt)
            return "recorded"


def write_atomic(path: pathlib.Path, value: dict[str, Any]) -> None:
    descriptor, temporary = tempfile.mkstemp(prefix=path.name + ".", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w") as target:
            json.dump(value, target, sort_keys=True, separators=(",", ":"))
            target.write("\n")
            target.flush()
            os.fsync(target.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def require_binding(receipt: dict[str, Any], request: dict[str, Any]) -> None:
    supplied = request.get("binding_sha256")
    if supplied != receipt["binding_sha256"]:
        raise WorkflowError("immutable binding digest changed")
    validate_binding(receipt["binding"])


def authorize(receipt: dict[str, Any], request: dict[str, Any]) -> str:
    require_binding(receipt, request)
    for capability in CAPABILITIES:
        item = event(receipt, "capability_" + capability)
        if not item or item["data"].get("outcome") != "pass":
            raise WorkflowError(f"{capability} has not durably passed")
        if capability not in {"capacity", "charge_route"} and item["data"].get("evidence") != "known":
            raise WorkflowError(f"{capability} requires known evidence")
    capacity = event(receipt, "capability_capacity")["data"]
    charge = event(receipt, "capability_charge_route")["data"]
    acknowledgement = request.get("capacity_ack_sha256")
    if capacity.get("evidence") in {"unknown", "unsupported"} or charge.get("evidence") in {"unknown", "unsupported"}:
        valid_sha(acknowledgement, "capacity_ack_sha256")
    elif acknowledgement is not None:
        raise WorkflowError("capacity acknowledgement is not applicable")
    if capacity.get("known_floor_failure") is True:
        raise WorkflowError("known capacity floor failure is non-overridable")
    return append_event(receipt, request, "mutation_authorized", {
        "binding_sha256": receipt["binding_sha256"], "capacity_ack_sha256": acknowledgement,
    })


def record_capability(receipt: dict[str, Any], request: dict[str, Any]) -> str:
    require_binding(receipt, request)
    name = string(request, "name", 64)
    if name not in CAPABILITIES:
        raise WorkflowError("unknown capability stage")
    outcome = request.get("outcome")
    evidence = request.get("evidence")
    if outcome not in {"pass", "blocked"} or evidence not in {"known", "unknown", "unsupported"}:
        raise WorkflowError("capability outcome/evidence is invalid")
    data = {"outcome": outcome, "evidence": evidence}
    if name == "capacity":
        data["known_floor_failure"] = request.get("known_floor_failure") is True
        if data["known_floor_failure"]:
            data["outcome"] = "blocked"
    return append_event(receipt, request, "capability_" + name, data)


def record_launch(receipt: dict[str, Any], request: dict[str, Any]) -> str:
    require_binding(receipt, request)
    if not event(receipt, "mutation_authorized"):
        raise WorkflowError("launch intent is not authorized")
    if request.get("attempt") != 1:
        raise WorkflowError("only attempt one is permitted")
    binding = receipt["binding"]
    expected_identity = {
        "orb_thread": binding["orb_thread"], "execution_id": binding["execution_id"],
        "executable_path": binding["executable_path"], "executable_version": binding["executable_version"],
        "argv_sha256": digest(binding["argv"]), "model": MODEL,
    }
    if request.get("launch_identity") != expected_identity:
        raise WorkflowError("launch identity does not match the immutable manifest")
    return append_event(receipt, request, "launch_recorded", {
        "attempt": 1, "launch_sha256": valid_sha(string(request, "launch_sha256", 64), "launch_sha256"),
        "model": MODEL, "launch_identity_sha256": digest(expected_identity),
    })


def repository_state(repo: pathlib.Path, binding: dict[str, Any]) -> tuple[str, str | None, list[str]]:
    if git(repo, "rev-parse", "--is-inside-work-tree") != "true":
        raise WorkflowError("handoff is not a Git worktree")
    if git(repo, "symbolic-ref", "--short", "HEAD") != binding["branch"]:
        raise WorkflowError("handoff branch changed")
    if git(repo, "status", "--porcelain=v1", "--untracked-files=all"):
        raise WorkflowError("dirty handoff remains unresolved")
    head = git(repo, "rev-parse", "HEAD")
    base = binding["base_sha"]
    count = git(repo, "rev-list", "--count", f"{base}..{head}")
    if head == base and count == "0":
        return "blocked", None, []
    parents = git(repo, "rev-list", "--parents", "-n", "1", head).split()
    if count != "1" or parents != [head, base]:
        raise WorkflowError("divergent or multi-commit handoff remains unresolved")
    changed = git(repo, "diff-tree", "--no-commit-id", "--name-only", "-r", head).splitlines()
    return "complete", head, changed


def export_handoff(request: dict[str, Any]) -> dict[str, Any]:
    packet = load_json(pathlib.Path(string(request, "packet", 4096)))
    binding = validate_binding(packet.get("binding"))
    if packet.get("binding_sha256") != digest(binding):
        raise WorkflowError("packet binding is corrupt")
    if request.get("binding_sha256") != packet["binding_sha256"]:
        raise WorkflowError("Orb binding identity changed")
    usage = request.get("model_usage")
    if not isinstance(usage, dict) or list(usage) != [MODEL]:
        raise WorkflowError("validated result must contain one exact modelUsage key")
    repo = pathlib.Path(string(request, "worktree", 4096)).resolve()
    outcome, commit, changed = repository_state(repo, binding)
    declared = request.get("outcome")
    if declared != outcome:
        raise WorkflowError("declared outcome does not match repository evidence")
    output = pathlib.Path(string(request, "output", 4096)).resolve()
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    report_sha256 = valid_sha(string(request, "report_sha256", 64), "report_sha256")
    blocker_sha256 = request.get("blocker_sha256")
    if outcome == "blocked":
        valid_sha(blocker_sha256, "blocker_sha256")
    elif blocker_sha256 is not None:
        raise WorkflowError("complete handoff cannot contain a blocker digest")
    metadata: dict[str, Any] = {
        "schema": SCHEMA, "binding": binding, "binding_sha256": packet["binding_sha256"],
        "outcome": outcome, "commit": commit, "changed_paths": changed,
        "model_usage": {MODEL: {}}, "bundle_sha256": None, "report_sha256": report_sha256,
        "blocker_sha256": blocker_sha256,
    }
    if outcome == "complete":
        bundle = output / "result.bundle"
        process = subprocess.run(
            [
                "git", "-C", str(repo), "bundle", "create", str(bundle),
                "refs/heads/" + binding["branch"], f"^{binding['base_sha']}",
            ],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30, check=False,
        )
        if process.returncode != 0 or not bundle.is_file() or bundle.stat().st_size > MAX_ARTIFACT:
            raise WorkflowError("bounded commit-bearing bundle export failed")
        metadata["bundle_sha256"] = file_digest(bundle)
    write_atomic(output / "handoff.json", metadata)
    return {"outcome": outcome, "commit": commit, "artifact_sha256": artifact_digest(output)}


def admit_artifact(directory: pathlib.Path) -> tuple[dict[str, Any], str]:
    try:
        entries = {item.name for item in directory.iterdir()}
    except OSError as error:
        raise WorkflowError("artifact directory is unavailable") from error
    if entries not in ({"handoff.json"}, {"handoff.json", "result.bundle"}):
        raise WorkflowError("artifact directory has unexpected entries")
    metadata_path = directory / "handoff.json"
    paths = [metadata_path] + ([directory / "result.bundle"] if "result.bundle" in entries else [])
    total = 0
    for path in paths:
        try:
            info = path.lstat()
        except OSError as error:
            raise WorkflowError("artifact entry is unavailable") from error
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise WorkflowError("artifact entries must be unlinked regular files")
        if path == metadata_path and info.st_size > MAX_METADATA:
            raise WorkflowError("artifact metadata exceeds its bound")
        total += info.st_size
    if total > MAX_ARTIFACT:
        raise WorkflowError("artifact exceeds its total bound")
    metadata = load_json(metadata_path)
    expected = {"handoff.json"} if metadata.get("outcome") == "blocked" else {"handoff.json", "result.bundle"}
    if entries != expected:
        raise WorkflowError("artifact entries do not match the declared outcome")
    value = hashlib.sha256()
    for path in paths:
        with path.open("rb") as source:
            while chunk := source.read(1024 * 1024):
                value.update(chunk)
    return metadata, value.hexdigest()


def artifact_digest(directory: pathlib.Path) -> str:
    return admit_artifact(directory)[1]


def verify_artifact(receipt: dict[str, Any], request: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    require_binding(receipt, request)
    directory = pathlib.Path(string(request, "artifact", 4096)).resolve()
    transferred = event(receipt, "artifact_transferred")
    if not transferred or transferred["data"]["artifact_sha256"] != artifact_digest(directory):
        raise WorkflowError("artifact has not been durably transferred or changed after transfer")
    metadata, admitted_digest = admit_artifact(directory)
    binding = receipt["binding"]
    if metadata.get("binding_sha256") != receipt["binding_sha256"] or metadata.get("binding") != binding:
        raise WorkflowError("artifact provenance does not match durable receipt")
    if metadata.get("model_usage") != {MODEL: {}}:
        raise WorkflowError("artifact model provenance is not exact")
    outcome = metadata.get("outcome")
    commit = metadata.get("commit")
    semantic = event(receipt, "semantic_completion")
    if not semantic or any(
        metadata.get(key) != semantic["data"].get(key)
        for key in ("outcome", "commit", "report_sha256", "blocker_sha256")
    ):
        raise WorkflowError("artifact contradicts durable semantic completion")
    if outcome == "blocked":
        if commit is not None or metadata.get("changed_paths") != [] or metadata.get("bundle_sha256") is not None:
            raise WorkflowError("blocked artifact is not a clean zero-commit result")
    elif outcome == "complete":
        valid_sha(commit, "artifact commit", git=True)
        bundle = directory / "result.bundle"
        if not bundle.is_file() or bundle.stat().st_size > MAX_ARTIFACT:
            raise WorkflowError("commit-bearing artifact is unavailable")
        if metadata.get("bundle_sha256") != file_digest(bundle):
            raise WorkflowError("commit-bearing artifact is corrupt")
        base_repository = pathlib.Path(string(request, "base_repository", 4096)).resolve()
        if git(base_repository, "rev-parse", binding["base_sha"]) != binding["base_sha"]:
            raise WorkflowError("declared base is unavailable from the coordinator repository")
        with tempfile.TemporaryDirectory(prefix="amux-claude-orb-verify-") as temporary:
            repo = pathlib.Path(temporary)
            subprocess.run(["git", "init", "-q", str(repo)], check=True, timeout=15)
            git(repo, "fetch", str(base_repository), binding["base_sha"])
            git(repo, "update-ref", "refs/heads/declared-base", "FETCH_HEAD")
            process = subprocess.run(
                ["git", "-C", str(repo), "bundle", "verify", str(bundle)],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30, check=False,
            )
            if process.returncode != 0:
                raise WorkflowError("Git bundle verification failed")
            heads = git(repo, "bundle", "list-heads", str(bundle)).splitlines()
            if heads != [f"{commit} refs/heads/{binding['branch']}"]:
                raise WorkflowError("Git bundle advertises unexpected refs")
            packs_before = set((repo / ".git" / "objects" / "pack").glob("*.idx"))
            unbundled = git(repo, "bundle", "unbundle", str(bundle)).splitlines()
            if unbundled != [f"{commit} refs/heads/{binding['branch']}"]:
                raise WorkflowError("Git bundle unbundle identity changed")
            fetched = git(repo, "rev-parse", commit)
            parents = git(repo, "rev-list", "--parents", "-n", "1", commit).split()
            if fetched != commit or parents != [commit, binding["base_sha"]]:
                raise WorkflowError("artifact is not exactly one direct child of the base")
            if git(repo, "rev-list", "--count", f"{binding['base_sha']}..{commit}") != "1":
                raise WorkflowError("artifact contains a non-singular handoff")
            changed = git(repo, "diff-tree", "--no-commit-id", "--name-only", "-r", commit).splitlines()
            if changed != metadata.get("changed_paths") or not all(path_allowed(path, binding["allowed_paths"]) for path in changed):
                raise WorkflowError("artifact diff exceeds the declared scope")
            pack_indexes = list(set((repo / ".git" / "objects" / "pack").glob("*.idx")) - packs_before)
            if len(pack_indexes) != 1:
                raise WorkflowError("bundle object inventory is ambiguous")
            inventory = git(repo, "verify-pack", "-v", str(pack_indexes[0])).splitlines()
            commits = [line for line in inventory if len(line.split()) >= 2 and line.split()[1] == "commit"]
            if len(commits) != 1 or commits[0].split()[0] != commit:
                raise WorkflowError("bundle does not contain exactly one commit object")
    else:
        raise WorkflowError("artifact outcome is invalid")
    data = {
        "outcome": outcome, "commit": commit, "artifact_sha256": admitted_digest,
        "authority": "coordinator_verified_no_integration",
    }
    return outcome, data


def path_allowed(path: str, allowed: list[str]) -> bool:
    return any(path == item.rstrip("/") or (item.endswith("/") and path.startswith(item)) for item in allowed)


def lifecycle_authorize(receipt: dict[str, Any], request: dict[str, Any], kind: str) -> str:
    require_binding(receipt, request)
    if not event(receipt, "owner_acknowledged"):
        raise WorkflowError(f"{kind} before owner acknowledgement is forbidden")
    identity = request.get("fresh_identity")
    expected = {
        "orb_thread": receipt["binding"]["orb_thread"],
        "repository": receipt["binding"]["repository"],
        "branch": receipt["binding"]["branch"],
        "worktree_id": receipt["binding"]["worktree_id"],
    }
    if identity != expected:
        raise WorkflowError(f"{kind} requires fresh exact identity validation")
    observation = request.get("fresh_observation")
    if not isinstance(observation, dict) or set(observation) != {"evidence_sha256", "head_sha", "clean"}:
        raise WorkflowError(f"{kind} requires a bounded fresh state observation")
    valid_sha(observation.get("evidence_sha256"), "fresh observation evidence")
    expected_head = event(receipt, "handoff_verified")["data"].get("commit") or receipt["binding"]["base_sha"]
    if observation.get("head_sha") != expected_head or observation.get("clean") is not True:
        raise WorkflowError(f"{kind} fresh workspace state is unsafe")
    if kind == "cleanup" and not event(receipt, "process_absence"):
        raise WorkflowError("cleanup requires durable headless process absence")
    return append_event(receipt, request, kind + "_authorized", {
        "fresh_identity_sha256": digest(identity), "fresh_observation": observation, "automatic": False,
    }, singleton=False)


def lifecycle_result(receipt: dict[str, Any], request: dict[str, Any], kind: str) -> str:
    require_binding(receipt, request)
    authorization_id = string(request, "authorization_event_id", 128)
    authorization = next((
        item for item in receipt["events"]
        if item["event_id"] == authorization_id and item["kind"] == kind + "_authorized"
    ), None)
    if authorization is None:
        raise WorkflowError(f"{kind} is not separately authorized")
    if any(
        item["kind"] == kind + "_result" and item["data"].get("authorization_event_id") == authorization_id
        for item in receipt["events"]
    ):
        raise WorkflowError(f"{kind} authorization was already consumed")
    status = request.get("status")
    if status not in {"success", "failure"}:
        raise WorkflowError("lifecycle result must be success or failure")
    result_sha256 = valid_sha(string(request, "result_sha256", 64), "result_sha256")
    failure_sha256 = request.get("failure_sha256")
    if status == "failure":
        valid_sha(failure_sha256, "failure_sha256")
    elif failure_sha256 is not None:
        raise WorkflowError("successful lifecycle result cannot contain a failure digest")
    return append_event(receipt, request, kind + "_result", {
        "authorization_event_id": authorization_id, "status": status, "result_sha256": result_sha256,
        "failure_sha256": failure_sha256, "durable_success": status == "success",
    }, singleton=False)


def mutate(store: Store, command: str, request: dict[str, Any]) -> Any:
    operation = string(request, "operation_id", 128)

    def apply(receipt: dict[str, Any]) -> Any:
        prior = next((item for item in receipt["events"] if item["event_id"] == request.get("event_id")), None)
        if prior:
            if prior.get("kind") != expected_event_kind(command, request) or prior.get("request_sha256") != digest(request):
                raise WorkflowError("event ID was already used for different content")
            result: dict[str, Any] = {"outcome": "duplicate"}
            if command == "verify":
                result["handoff"] = prior["data"]["outcome"]
            return result
        if command == "capability":
            return {"outcome": record_capability(receipt, request)}
        if command == "authorize":
            return {"outcome": authorize(receipt, request)}
        if command == "launch":
            return {"outcome": record_launch(receipt, request)}
        if command == "process-absence":
            require_binding(receipt, request)
            if not event(receipt, "launch_recorded"):
                raise WorkflowError("process absence requires a recorded launch")
            return {"outcome": append_event(receipt, request, "process_absence", {
                "evidence_sha256": valid_sha(string(request, "evidence_sha256", 64), "evidence_sha256"),
                "state": "terminated_or_absent", "parking": False,
            })}
        if command == "semantic":
            require_binding(receipt, request)
            if not event(receipt, "launch_recorded"):
                raise WorkflowError("semantic completion requires a recorded launch")
            outcome = request.get("handoff")
            commit = request.get("commit")
            if outcome == "complete":
                valid_sha(commit, "semantic commit", git=True)
            elif outcome == "blocked":
                if commit is not None:
                    raise WorkflowError("blocked semantic result cannot name a commit")
            else:
                raise WorkflowError("semantic handoff is invalid")
            blocker_sha256 = request.get("blocker_sha256")
            if outcome == "blocked":
                valid_sha(blocker_sha256, "blocker_sha256")
            elif blocker_sha256 is not None:
                raise WorkflowError("complete semantic result cannot contain a blocker digest")
            return {"outcome": append_event(receipt, request, "semantic_completion", {
                "outcome": outcome, "commit": commit,
                "artifact_sha256": valid_sha(string(request, "artifact_sha256", 64), "artifact_sha256"),
                "report_sha256": valid_sha(string(request, "report_sha256", 64), "report_sha256"),
                "blocker_sha256": request.get("blocker_sha256"), "model": MODEL,
            })}
        if command == "transfer":
            require_binding(receipt, request)
            semantic = event(receipt, "semantic_completion")
            if not semantic:
                raise WorkflowError("artifact transfer requires semantic completion")
            directory = pathlib.Path(string(request, "artifact", 4096)).resolve()
            artifact_sha256 = artifact_digest(directory)
            if artifact_sha256 != semantic["data"]["artifact_sha256"]:
                raise WorkflowError("transferred artifact does not match semantic handoff")
            return {"outcome": append_event(receipt, request, "artifact_transferred", {
                "artifact_sha256": artifact_sha256,
                "transfer_sha256": valid_sha(string(request, "transfer_sha256", 64), "transfer_sha256"),
            })}
        if command == "verify":
            outcome, data = verify_artifact(receipt, request)
            return {"outcome": append_event(receipt, request, "handoff_verified", data), "handoff": outcome}
        if command == "checks":
            require_binding(receipt, request)
            if not event(receipt, "handoff_verified"):
                raise WorkflowError("check attestation requires verified commit evidence")
            results = request.get("results")
            expected = [digest(argv) for argv in receipt["binding"]["check_argv"]]
            if (
                not isinstance(results, list) or not all(isinstance(item, dict) for item in results)
                or [item.get("argv_sha256") for item in results] != expected
            ):
                raise WorkflowError("check attestation does not match declared checks")
            if any(set(item) != {"argv_sha256", "status"} or item["status"] != 0 for item in results):
                raise WorkflowError("check attestation contains a failed or malformed result")
            return {"outcome": append_event(receipt, request, "checks_verified", {
                "results": results,
                "verifier_sha256": valid_sha(string(request, "verifier_sha256", 64), "verifier_sha256"),
                "evidence_sha256": valid_sha(string(request, "evidence_sha256", 64), "evidence_sha256"),
                "execution": "external_isolated_coordinator_verifier",
            })}
        if command == "deliver":
            require_binding(receipt, request)
            verified = event(receipt, "handoff_verified")
            if not verified:
                raise WorkflowError("durable delivery requires verified handoff evidence")
            if not event(receipt, "checks_verified"):
                raise WorkflowError("durable delivery requires independent check attestation")
            result = append_event(receipt, request, "durably_delivered", {
                "artifact_sha256": verified["data"]["artifact_sha256"],
                "delivery_sha256": valid_sha(string(request, "delivery_sha256", 64), "delivery_sha256"),
            })
            return {"outcome": result}
        if command == "acknowledge":
            require_binding(receipt, request)
            delivered = event(receipt, "durably_delivered")
            if not delivered:
                raise WorkflowError("owner acknowledgement requires durable delivery")
            result = append_event(receipt, request, "owner_acknowledged", {
                "delivery_sha256": delivered["data"]["delivery_sha256"],
                "ack_sha256": valid_sha(string(request, "ack_sha256", 64), "ack_sha256"),
            })
            return {"outcome": result}
        if command == "notify":
            require_binding(receipt, request)
            if not event(receipt, "durably_delivered"):
                raise WorkflowError("notification cannot precede durable delivery")
            status = request.get("status")
            if status not in {"success", "failure"}:
                raise WorkflowError("notification status must be success or failure")
            return {"outcome": append_event(receipt, request, "notification_result", {
                "status": status, "notification_sha256": valid_sha(
                    string(request, "notification_sha256", 64), "notification_sha256"
                ),
            })}
        if command in {"authorize-archive", "authorize-cleanup"}:
            kind = command.removeprefix("authorize-")
            return {"outcome": lifecycle_authorize(receipt, request, kind)}
        if command in {"archive-result", "cleanup-result"}:
            kind = command.removesuffix("-result")
            return {"outcome": lifecycle_result(receipt, request, kind)}
        raise WorkflowError("unsupported workflow command")

    return store.locked(operation, apply)


def expected_event_kind(command: str, request: dict[str, Any]) -> str:
    return {
        "capability": "capability_" + str(request.get("name")),
        "authorize": "mutation_authorized", "launch": "launch_recorded",
        "process-absence": "process_absence", "semantic": "semantic_completion",
        "transfer": "artifact_transferred", "verify": "handoff_verified", "checks": "checks_verified",
        "deliver": "durably_delivered", "notify": "notification_result",
        "acknowledge": "owner_acknowledged", "authorize-archive": "archive_authorized",
        "archive-result": "archive_result", "authorize-cleanup": "cleanup_authorized",
        "cleanup-result": "cleanup_result",
    }[command]


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser()
    root.add_argument("--state-dir", type=pathlib.Path)
    root.add_argument("command", choices=(
        "intent", "capability", "authorize", "launch", "export", "process-absence", "semantic",
        "transfer", "verify", "checks", "deliver", "notify", "acknowledge", "authorize-archive", "archive-result",
        "authorize-cleanup", "cleanup-result", "show",
    ))
    root.add_argument("--operation-id")
    return root


def main() -> int:
    arguments = parser().parse_args()
    if arguments.command == "export":
        print(json.dumps(export_handoff(read_json()), sort_keys=True, separators=(",", ":")))
        return 0
    if arguments.state_dir is None:
        raise WorkflowError("--state-dir is required for coordinator state")
    store = Store(arguments.state_dir)
    if arguments.command == "show":
        if not arguments.operation_id:
            raise WorkflowError("show requires --operation-id")
        print(store.path(arguments.operation_id).read_text(), end="")
        return 0
    request = read_json()
    if arguments.command == "intent":
        result = {"outcome": store.create(request.get("binding"), string(request, "event_id", 128))}
        result["binding_sha256"] = digest(validate_binding(request.get("binding")))
        result["packet"] = str(store.root / f"{request['binding']['operation_id']}.packet.json")
    else:
        result = mutate(store, arguments.command, request)
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except WorkflowError as error:
        print(f"fresh-orb-workflow: {error}", file=sys.stderr)
        raise SystemExit(2) from error
