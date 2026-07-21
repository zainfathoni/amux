#!/usr/bin/env python3
"""Unstable, skill-owned helper for experimental Claude delegation."""

from __future__ import annotations

import argparse
import ast
import contextlib
import copy
import ctypes
import errno
import fcntl
import hashlib
import json
import math
import os
import pathlib
import platform
import re
import secrets
import selectors
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timedelta, timezone
from typing import Any, Iterator


SCHEMA_VERSION = 1
PROTOCOL_VERSION = 1
LIFECYCLE_SCHEMA_VERSION = 1
MAX_STORE_BYTES = 4 * 1024 * 1024
MAX_PACKET_BYTES = 256 * 1024
MAX_LAUNCH_TRANSPORT_BYTES = 2 * 1024 * 1024
MAX_CAPACITY_SOURCE_BYTES = 256 * 1024
MAX_CAPACITY_EXTRA_WINDOWS = 32
MAX_TMUX_COMMAND_BYTES = 8 * 1024
MAX_ABSENCE_PANES = 256
MAX_ABSENCE_PROCESSES = 2048
MAX_ABSENCE_OUTPUT_BYTES = 64 * 1024
PRE_IDENTITY_LAUNCH_INTENT_COMPATIBILITY = "pre_identity_launch_intent_v1"
HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY = (
    "historical_modern_read_only_launch_intent_v1"
)
PRE_IDENTITY_ACQUIRED_NO_REPORT_COMPATIBILITY = "pre_identity_acquired_no_report_v1"
EXEC_BUDGET_MARGIN_BYTES = 32 * 1024
STARTUP_TIMEOUT_SECONDS = 4.0
STARTUP_STABILITY_SECONDS = 1.5
STARTUP_POLL_SECONDS = 0.05
INTERNAL_EVENT_PREFIX = "amux:"
LINUX_PROC_ROOT = pathlib.Path("/proc")
REQUIRED_CLAUDE_FLAGS = [
    "--allowed-tools", "--disable-slash-commands", "--disallowed-tools", "--mcp-config",
    "--no-chrome", "--permission-mode", "--prompt-suggestions", "--session-id",
    "--setting-sources", "--settings", "--strict-mcp-config", "--tools",
]
APPROVED_READ_ONLY_MODELS = {"claude-fable-5"}


class HelperError(Exception):
    pass


class LaunchGateBusy(HelperError):
    pass


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def canonical_sha256(value: Any) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def reject_duplicate_json_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, nested in pairs:
        if key in value:
            raise HelperError("JSON input contains a duplicated field")
        value[key] = nested
    return value


def read_input() -> dict[str, Any]:
    raw = sys.stdin.buffer.read(MAX_STORE_BYTES + 1)
    if len(raw) > MAX_STORE_BYTES:
        raise HelperError("input exceeds the experimental size limit")
    if not raw:
        return {}
    try:
        value = json.loads(raw, object_pairs_hook=reject_duplicate_json_pairs)
    except json.JSONDecodeError as error:
        raise HelperError(f"invalid JSON input: {error.msg}") from error
    if not isinstance(value, dict):
        raise HelperError("input must be a JSON object")
    return value


def decode_receipt_store(raw: bytes) -> dict[str, Any]:
    try:
        store = json.loads(raw, object_pairs_hook=reject_duplicate_json_pairs)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        raise HelperError("invalid receipt store") from error
    if not isinstance(store, dict) or store.get("schema_version") != SCHEMA_VERSION:
        raise HelperError("unsupported or invalid receipt store")
    receipts = store.get("receipts")
    if not isinstance(receipts, list):
        raise HelperError("invalid receipt store receipts")
    identities: set[str] = set()
    for receipt in receipts:
        if not isinstance(receipt, dict):
            raise HelperError("invalid receipt record")
        binding = receipt.get("binding")
        if not isinstance(binding, dict):
            raise HelperError("invalid receipt binding")
        try:
            binding = validate_binding(binding)
        except HelperError as error:
            raise HelperError("invalid receipt binding") from error
        delegation_id = binding["delegation_id"]
        if delegation_id in identities:
            raise HelperError("invalid or duplicate receipt identity")
        events = receipt.get("events")
        if not isinstance(events, list):
            raise HelperError("invalid receipt event history")
        event_ids: set[str] = set()
        for event in events:
            if not isinstance(event, dict):
                raise HelperError("invalid receipt event history")
            event_id = event.get("event_id")
            if not isinstance(event_id, str) or not event_id or event_id in event_ids:
                raise HelperError("invalid or duplicate receipt event identity")
            event_ids.add(event_id)
        identities.add(delegation_id)
    return store


class LifecycleRegistry:
    def __init__(self, state_dir: pathlib.Path):
        self.state_dir = state_dir
        self.path = state_dir / "lifecycle.json"
        self.lock_path = state_dir / "experimental.lock"

    def prepare(self) -> None:
        self.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(self.state_dir, 0o700)

    @contextlib.contextmanager
    def mutation_lock(self) -> Iterator[None]:
        self.prepare()
        descriptor = os.open(self.lock_path, os.O_CREAT | os.O_RDWR, 0o600)
        os.chmod(self.lock_path, 0o600)
        with os.fdopen(descriptor, "r+") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            yield

    def load(self) -> dict[str, Any]:
        try:
            raw = self.path.read_bytes()
        except FileNotFoundError:
            return {"schema_version": LIFECYCLE_SCHEMA_VERSION, "stores": [], "legacy_store_objects": {}, "teardown_fences": {}}
        except OSError as error:
            raise HelperError("lifecycle registry is unavailable") from error
        if len(raw) > MAX_STORE_BYTES:
            raise HelperError("lifecycle registry exceeds the experimental size limit")
        try:
            registry = json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError) as error:
            raise HelperError("invalid lifecycle registry") from error
        if not isinstance(registry, dict) or registry.get("schema_version") != LIFECYCLE_SCHEMA_VERSION:
            raise HelperError("unsupported or invalid lifecycle registry")
        stores = registry.get("stores")
        legacy_objects = registry.get("legacy_store_objects", {})
        fences = registry.get("teardown_fences")
        if (
            not isinstance(stores, list)
            or any(not isinstance(path, str) or not path for path in stores)
            or len(stores) != len(set(stores))
            or not isinstance(legacy_objects, dict)
            or any(
                path not in stores or not isinstance(identity, str) or not identity.startswith("directory:")
                for path, identity in legacy_objects.items()
            )
            or not isinstance(fences, dict)
            or any(not isinstance(thread, str) or not isinstance(value, dict) for thread, value in fences.items())
        ):
            raise HelperError("invalid lifecycle registry")
        registry["legacy_store_objects"] = legacy_objects
        return registry

    def commit(self, registry: dict[str, Any]) -> None:
        payload = (json.dumps(registry, sort_keys=True, separators=(",", ":")) + "\n").encode()
        if len(payload) > MAX_STORE_BYTES:
            raise HelperError("lifecycle registry exceeds the experimental size limit")
        self.prepare()
        descriptor, temporary = tempfile.mkstemp(prefix="lifecycle.json.tmp.", dir=self.state_dir)
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "wb") as output:
                output.write(payload)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, self.path)
            directory = os.open(self.state_dir, os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
        finally:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass


class ReceiptStore:
    def __init__(self, state_dir: pathlib.Path, lifecycle_state_dir: pathlib.Path | None = None):
        self.state_dir = state_dir
        self.path = state_dir / "receipts.json"
        self.lock_path = state_dir / "experimental.lock"
        self.lifecycle = LifecycleRegistry(lifecycle_state_dir or state_dir)

    def prepare(self) -> None:
        self.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(self.state_dir, 0o700)

    @contextlib.contextmanager
    def mutation_lock(self) -> Iterator[None]:
        self.prepare()
        descriptor = os.open(self.lock_path, os.O_CREAT | os.O_RDWR, 0o600)
        os.chmod(self.lock_path, 0o600)
        with os.fdopen(descriptor, "r+") as lock_file:
            fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX)
            yield

    @contextlib.contextmanager
    def launch_gate(self, delegation_id: str, timeout_seconds: float | None = None) -> Iterator[None]:
        protocol_id({"delegation_id": delegation_id}, "delegation_id")
        self.prepare()
        gate_dir = self.state_dir / "launch-gates"
        gate_path = gate_dir / hashlib.sha256(delegation_id.encode()).hexdigest()
        try:
            gate_dir.mkdir(mode=0o700, exist_ok=True)
            os.chmod(gate_dir, 0o700)
            descriptor = os.open(
                gate_path, os.O_CREAT | os.O_RDWR | getattr(os, "O_NOFOLLOW", 0), 0o600
            )
        except OSError as error:
            raise HelperError("launch gate is unavailable") from error
        with os.fdopen(descriptor, "r+") as gate_file:
            try:
                gate_stat = os.fstat(gate_file.fileno())
                if (
                    not stat.S_ISREG(gate_stat.st_mode)
                    or gate_stat.st_uid != os.geteuid()
                    or stat.S_IMODE(gate_stat.st_mode) != 0o600
                ):
                    raise HelperError("launch gate is unavailable")
                if timeout_seconds is None:
                    fcntl.flock(gate_file.fileno(), fcntl.LOCK_EX)
                else:
                    deadline = time.monotonic() + timeout_seconds
                    while True:
                        try:
                            fcntl.flock(gate_file.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                            break
                        except OSError as error:
                            if error.errno not in {errno.EACCES, errno.EAGAIN, errno.EWOULDBLOCK}:
                                raise
                            if time.monotonic() >= deadline:
                                raise LaunchGateBusy("launch gate is busy") from error
                            time.sleep(0.01)
                os.set_inheritable(gate_file.fileno(), True)
            except OSError as error:
                raise HelperError("launch gate is unavailable") from error
            yield

    def load_store(self, require_exists: bool = False) -> dict[str, Any]:
        try:
            raw = self.path.read_bytes()
        except FileNotFoundError:
            if require_exists:
                raise HelperError("registered receipt store is unavailable")
            return {"schema_version": SCHEMA_VERSION, "receipts": []}
        except OSError as error:
            raise HelperError("receipt store is unavailable") from error
        if len(raw) > MAX_STORE_BYTES:
            raise HelperError("receipt store exceeds the experimental size limit")
        return decode_receipt_store(raw)

    def commit(self, store: dict[str, Any]) -> None:
        payload = (json.dumps(store, sort_keys=True, separators=(",", ":")) + "\n").encode()
        if len(payload) > MAX_STORE_BYTES:
            raise HelperError("receipt store exceeds the experimental size limit")
        self.prepare()
        descriptor, temporary = tempfile.mkstemp(prefix="receipts.json.tmp.", dir=self.state_dir)
        try:
            os.fchmod(descriptor, 0o600)
            with os.fdopen(descriptor, "wb") as output:
                output.write(payload)
                output.flush()
                os.fsync(output.fileno())
            os.replace(temporary, self.path)
            directory = os.open(self.state_dir, os.O_RDONLY)
            try:
                os.fsync(directory)
            finally:
                os.close(directory)
        finally:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass

    @staticmethod
    def find(store: dict[str, Any], delegation_id: str) -> dict[str, Any]:
        for receipt in store["receipts"]:
            if receipt["binding"]["delegation_id"] == delegation_id:
                return receipt
        raise HelperError(f"receipt {delegation_id!r} was not found")

    def create(self, request: dict[str, Any]) -> str:
        binding = validate_binding(request.get("binding"))
        routing = validate_routing(request.get("routing"))
        with self.lifecycle.mutation_lock():
            lifecycle = self.lifecycle.load()
            if binding["origin_thread"] in lifecycle["teardown_fences"]:
                raise HelperError("origin Amp worker has a durable teardown fence")
            state_path = str(self.state_dir.resolve())
            if state_path not in lifecycle["stores"]:
                lifecycle["stores"].append(state_path)
                lifecycle["stores"].sort()
                self.lifecycle.commit(lifecycle)
            store_lock = contextlib.nullcontext() if self.lifecycle.state_dir == self.state_dir else self.mutation_lock()
            with store_lock:
                return self.create_locked(binding, routing)

    def create_locked(self, binding: dict[str, Any], routing: dict[str, Any]) -> str:
        store = self.load_store()
        for receipt in store["receipts"]:
            if receipt["binding"]["delegation_id"] != binding["delegation_id"]:
                continue
            require_receipt_mutable(receipt)
            if receipt["binding"] != binding:
                raise HelperError("delegation ID is already bound to a different immutable binding")
            if receipt["events"][0]["routing"] != routing:
                raise HelperError("delegation ID create event conflicts with its original routing")
            return "duplicate"
        lease = mutating_writer_lease(binding)
        if lease is not None:
            for receipt in store["receipts"]:
                existing_lease = receipt_writer_lease(receipt)
                if (
                    receipt.get("state") != "verified_parked"
                    and existing_lease is not None
                    and writer_leases_match(existing_lease, lease)
                ):
                    raise HelperError("worktree already has an unresolved exclusive logical writer lease")
        created_at = utc_now()
        receipt = {
            "binding": binding,
            "routing": routing,
            "state": "created",
            "report_message_id": "",
            "created_at": created_at,
            "updated_at": created_at,
            "events": [
                {
                    "event_id": f"create:{binding['delegation_id']}",
                    "kind": "created",
                    "at": created_at,
                    "routing": routing,
                }
            ],
        }
        if lease is not None:
            receipt["writer_lease"] = lease
        store["receipts"].append(receipt)
        self.commit(store)
        return "recorded"

    def route(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        routing = validate_routing(request.get("routing"))
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            event = {"event_id": event_id, "kind": "routing_changed", "routing": routing}
            replay = find_event(receipt, event_id)
            if replay is not None:
                if event_without_time(replay) == event:
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            event["at"] = utc_now()
            receipt["routing"] = routing
            receipt["updated_at"] = event["at"]
            receipt["events"].append(event)
            self.commit(store)
            return "recorded"

    def register_legacy_store(self, origin_thread: str, store_path: pathlib.Path) -> dict[str, Any]:
        required_string({"origin_thread": origin_thread}, "origin_thread", 256)
        exact_path = exact_canonical_store_path(store_path)
        with self.lifecycle.mutation_lock():
            with locked_owner_private_store(
                pathlib.Path(exact_path), acquire_lock=pathlib.Path(exact_path) != self.lifecycle.state_dir
            ) as (object_identity, raw):
                store = decode_receipt_store(raw)
                matching = [
                    receipt for receipt in store["receipts"]
                    if receipt["binding"].get("origin_thread") == origin_thread
                ]
                if not matching:
                    raise HelperError("legacy receipt store has no exact immutable origin match")
                if any(receipt["binding"].get("origin_thread") != origin_thread for receipt in store["receipts"]):
                    raise HelperError("legacy receipt store contains a different immutable origin")
                lifecycle = self.lifecycle.load()
                existing_object = lifecycle["legacy_store_objects"].get(exact_path)
                if exact_path in lifecycle["stores"] and existing_object != object_identity:
                    raise HelperError("legacy receipt store path is already bound to a different object")
                outcome = "duplicate" if existing_object == object_identity else "registered"
                if outcome == "registered":
                    if exact_path not in lifecycle["stores"]:
                        lifecycle["stores"].append(exact_path)
                    lifecycle["stores"].sort()
                    lifecycle["legacy_store_objects"][exact_path] = object_identity
                    self.lifecycle.commit(lifecycle)
        return {
            "action": "legacy_store_registration",
            "origin_thread_sha256": hashlib.sha256(origin_thread.encode()).hexdigest(),
            "store_object_sha256": hashlib.sha256(object_identity.encode()).hexdigest(),
            "outcome": outcome,
        }

    def detach_indeterminate_worker(self, request: dict[str, Any]) -> dict[str, Any]:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        origin_thread = required_string(request, "origin_thread", 256)
        compatibility = request.get("compatibility")
        if compatibility is not None and compatibility != PRE_IDENTITY_LAUNCH_INTENT_COMPATIBILITY:
            raise HelperError("indeterminate worker detach compatibility is unsupported")
        authorization = validate_terminal_amp_authorization(request.get("authorization"))
        reject_unknown(
            request,
            {"delegation_id", "event_id", "origin_thread", "authorization", "compatibility"},
            "indeterminate worker detach",
        )
        pair_sha256 = hashlib.sha256(delegation_id.encode()).hexdigest()
        origin_sha256 = hashlib.sha256(origin_thread.encode()).hexdigest()
        absence_code = (
            "legacy_target_and_session_absent"
            if compatibility == PRE_IDENTITY_LAUNCH_INTENT_COMPATIBILITY
            else "exact_launch_target_absent"
        )
        event = {
            "event_id": event_id,
            "kind": "worker_detached",
            "terminal_state": authorization["terminal_state"],
            "report_sha256": authorization["report_sha256"],
            "coordinator_authorization_sha256": authorization["coordinator_authorization_sha256"],
            "absence_code": absence_code,
        }
        if compatibility is not None:
            event["compatibility"] = compatibility
        with self.lifecycle.mutation_lock():
            lifecycle = self.lifecycle.load()
            state_path = str(self.state_dir.resolve())
            if state_path not in lifecycle["stores"]:
                raise HelperError("receipt store is not registered in the canonical lifecycle registry")
            verify_registered_store_object(state_path, lifecycle["legacy_store_objects"].get(state_path))
            if origin_thread not in lifecycle["teardown_fences"]:
                lifecycle["teardown_fences"][origin_thread] = {
                    "operation_id": hashlib.sha256(f"worker-teardown\0{origin_thread}".encode()).hexdigest(),
                    "created_at": utc_now(),
                }
                self.lifecycle.commit(lifecycle)
            store_lock = contextlib.nullcontext() if self.lifecycle.state_dir == self.state_dir else self.mutation_lock()
            with store_lock:
                store = load_registered_receipt_store(
                    self, state_path, lifecycle["legacy_store_objects"].get(state_path), acquire_lock=False
                )
                receipt = self.find(store, delegation_id)
                if receipt["binding"].get("origin_thread") != origin_thread:
                    raise HelperError("receipt immutable origin does not match detach authority")
                replay = find_event(receipt, event_id)
                if replay is not None:
                    if event_without_time(replay) == event and valid_worker_detach_chain(receipt):
                        return worker_detach_result(
                            origin_sha256, pair_sha256, "duplicate", absence_code, compatibility
                        )
                    raise HelperError("event ID is already bound to a conflicting event")
                if any(existing.get("kind") == "worker_detached" for existing in receipt["events"]):
                    raise HelperError("receipt already has a different worker detach operation")
                if not valid_indeterminate_detach_candidate(receipt):
                    raise HelperError("worker detach requires one unresolved launch-indeterminate receipt")
                if compatibility == PRE_IDENTITY_LAUNCH_INTENT_COMPATIBILITY:
                    launch_intent = pre_identity_launch_intent(receipt)
                    inspect_absence = inspect_pre_identity_launch_absence
                else:
                    launch_intent = receipt_launch_intent(receipt)
                    inspect_absence = inspect_indeterminate_launch_absence
                try:
                    with self.launch_gate(delegation_id, timeout_seconds=0.2):
                        absence = inspect_absence(launch_intent)
                        if absence != absence_code:
                            return {
                                "action": "indeterminate_worker_detach",
                                "origin_thread_sha256": origin_sha256,
                                "pair_sha256": pair_sha256,
                                "outcome": "blocked",
                                "blocker": absence,
                                "fence": "retained",
                            }
                        timestamp = utc_now()
                        event["at"] = timestamp
                        receipt["worker_detached"] = {
                            "event_id": event_id,
                            "at": timestamp,
                            "absence_code": event["absence_code"],
                        }
                        if compatibility is not None:
                            receipt["worker_detached"]["compatibility"] = compatibility
                        receipt["events"].append(event)
                        receipt["updated_at"] = timestamp
                        self.commit(store)
                except LaunchGateBusy:
                    return {
                        "action": "indeterminate_worker_detach",
                        "origin_thread_sha256": origin_sha256,
                        "pair_sha256": pair_sha256,
                        "outcome": "blocked",
                        "blocker": "launch_transport_active_or_indeterminate",
                        "fence": "retained",
                    }
        return worker_detach_result(origin_sha256, pair_sha256, "detached", absence_code, compatibility)

    def retire_live_indeterminate_pair(self, request: dict[str, Any]) -> dict[str, Any]:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        origin_thread = required_string(request, "origin_thread", 256)
        authorization = validate_terminal_amp_authorization(request.get("authorization"))
        compatibility = request.get("compatibility")
        if (
            compatibility is not None
            and compatibility != HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY
        ):
            raise HelperError("live pair retirement compatibility is unsupported")
        recover = request.get("recover", False)
        if not isinstance(recover, bool):
            raise HelperError("live pair retirement recover must be boolean")
        reject_unknown(
            request,
            {
                "delegation_id", "event_id", "origin_thread", "authorization", "compatibility",
                "recover",
            },
            "live indeterminate pair retirement",
        )
        origin_sha256 = hashlib.sha256(origin_thread.encode()).hexdigest()
        pair_sha256 = hashlib.sha256(delegation_id.encode()).hexdigest()
        result_id = internal_event_id("retirement-result", event_id)
        with self.lifecycle.mutation_lock():
            lifecycle = self.lifecycle.load()
            state_path = str(self.state_dir.resolve())
            if state_path not in lifecycle["stores"]:
                raise HelperError("receipt store is not registered in the canonical lifecycle registry")
            verify_registered_store_object(state_path, lifecycle["legacy_store_objects"].get(state_path))
            if origin_thread not in lifecycle["teardown_fences"]:
                lifecycle["teardown_fences"][origin_thread] = {
                    "operation_id": hashlib.sha256(f"worker-teardown\0{origin_thread}".encode()).hexdigest(),
                    "created_at": utc_now(),
                }
                self.lifecycle.commit(lifecycle)
            store_lock = contextlib.nullcontext() if self.lifecycle.state_dir == self.state_dir else self.mutation_lock()
            with store_lock:
                store = load_registered_receipt_store(
                    self, state_path, lifecycle["legacy_store_objects"].get(state_path), acquire_lock=False
                )
                receipt = self.find(store, delegation_id)
                if receipt["binding"].get("origin_thread") != origin_thread:
                    raise HelperError("receipt immutable origin does not match retirement authority")
                terminal = find_event(receipt, result_id)
                existing = find_event(receipt, event_id)
                if terminal is not None:
                    if existing is None or not valid_pair_retirement_chain(receipt):
                        raise HelperError("terminal pair retirement proof is invalid")
                    validate_retirement_operation(existing, authorization, compatibility)
                    return pair_retirement_result(
                        origin_sha256, pair_sha256, "duplicate", compatibility
                    )
                if not valid_live_retirement_candidate(receipt, authorization, compatibility):
                    raise HelperError("retirement requires the exact live report-bearing launch-indeterminate chain")
                launch_intent = modern_read_only_retirement_launch_intent(receipt, compatibility)
                expected_private_executable_path = retirement_private_executable_path(
                    self, delegation_id, compatibility
                )
                try:
                    with self.launch_gate(delegation_id, timeout_seconds=0.2):
                        if existing is None:
                            if recover:
                                raise HelperError("retirement recovery requires a durable retirement intent")
                            identity = inspect_live_indeterminate_target(
                                launch_intent, receipt["binding"], self, delegation_id,
                                compatibility, expected_private_executable_path,
                            )
                            if not valid_retirement_identity(identity, launch_intent, receipt["binding"]):
                                raise HelperError("retirement target does not match complete launch identity")
                            retirement_intent = {
                                "event_id": event_id,
                                "kind": "retirement_intent",
                                "terminal_state": authorization["terminal_state"],
                                "report_sha256": authorization["report_sha256"],
                                "coordinator_authorization_sha256": authorization["coordinator_authorization_sha256"],
                                "identity": identity,
                                "at": utc_now(),
                            }
                            if compatibility is not None:
                                retirement_intent["compatibility"] = compatibility
                            receipt["events"].append(retirement_intent)
                            receipt["retirement_intent"] = copy.deepcopy(retirement_intent)
                            receipt["updated_at"] = retirement_intent["at"]
                            self.commit(store)
                            current = inspect_live_indeterminate_target(
                                launch_intent, receipt["binding"], self, delegation_id,
                                compatibility, expected_private_executable_path,
                            )
                            if current != identity:
                                return pair_retirement_blocked(
                                    origin_sha256, pair_sha256, "retirement_identity_changed"
                                )
                            stop_exact_retirement_target(
                                identity, expected_private_executable_path, compatibility
                            )
                        else:
                            if not recover:
                                raise HelperError("retirement outcome is indeterminate; explicit recovery is required")
                            validate_retirement_operation(existing, authorization, compatibility)
                            identity = copy.deepcopy(existing.get("identity"))
                            if not valid_retirement_identity(identity, launch_intent, receipt["binding"]):
                                raise HelperError("durable retirement identity is invalid")
                            absence = confirm_retirement_target_absent(identity)
                            if absence != "exact_retirement_target_absent":
                                try:
                                    current = inspect_live_indeterminate_target(
                                        launch_intent, receipt["binding"], self, delegation_id,
                                        compatibility, expected_private_executable_path,
                                    )
                                except HelperError:
                                    return pair_retirement_blocked(origin_sha256, pair_sha256, absence)
                                if current != identity:
                                    return pair_retirement_blocked(
                                        origin_sha256, pair_sha256, "retirement_identity_changed"
                                    )
                                stop_exact_retirement_target(
                                    identity, expected_private_executable_path, compatibility
                                )
                        absence = confirm_retirement_target_absent(identity)
                        if absence != "exact_retirement_target_absent":
                            return pair_retirement_blocked(origin_sha256, pair_sha256, absence)
                        result = {
                            "event_id": result_id,
                            "kind": "pair_retired",
                            "operation_event_id": event_id,
                            "absence_code": absence,
                            "at": utc_now(),
                        }
                        receipt["events"].append(result)
                        receipt["pair_retired"] = copy.deepcopy(result)
                        receipt["updated_at"] = result["at"]
                        if not valid_pair_retirement_chain(receipt):
                            raise HelperError("terminal pair retirement proof did not seal exactly")
                        self.commit(store)
                except LaunchGateBusy:
                    return pair_retirement_blocked(
                        origin_sha256, pair_sha256, "launch_transport_active_or_indeterminate"
                    )
        return pair_retirement_result(origin_sha256, pair_sha256, "retired", compatibility)

    def retire_live_acquired_no_report_pair(self, request: dict[str, Any]) -> dict[str, Any]:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        origin_thread = required_string(request, "origin_thread", 256)
        authorization = validate_terminal_amp_authorization(request.get("authorization"))
        compatibility = request.get("compatibility")
        if (
            compatibility is not None
            and compatibility not in {
                HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY,
                PRE_IDENTITY_ACQUIRED_NO_REPORT_COMPATIBILITY,
            }
        ):
            raise HelperError("acquired no-report pair retirement compatibility is unsupported")
        recover = request.get("recover", False)
        if not isinstance(recover, bool):
            raise HelperError("acquired no-report pair retirement recover must be boolean")
        reject_unknown(
            request,
            {
                "delegation_id", "event_id", "origin_thread", "authorization", "compatibility",
                "recover",
            },
            "live acquired no-report pair retirement",
        )
        origin_sha256 = hashlib.sha256(origin_thread.encode()).hexdigest()
        pair_sha256 = hashlib.sha256(delegation_id.encode()).hexdigest()
        result_id = internal_event_id("acquired-retirement-result", event_id)
        with self.lifecycle.mutation_lock():
            lifecycle = self.lifecycle.load()
            state_path = str(self.state_dir.resolve())
            if state_path not in lifecycle["stores"]:
                raise HelperError("receipt store is not registered in the canonical lifecycle registry")
            verify_registered_store_object(state_path, lifecycle["legacy_store_objects"].get(state_path))
            if origin_thread not in lifecycle["teardown_fences"]:
                lifecycle["teardown_fences"][origin_thread] = {
                    "operation_id": hashlib.sha256(f"worker-teardown\0{origin_thread}".encode()).hexdigest(),
                    "created_at": utc_now(),
                }
                self.lifecycle.commit(lifecycle)
            store_lock = contextlib.nullcontext() if self.lifecycle.state_dir == self.state_dir else self.mutation_lock()
            with store_lock:
                store = load_registered_receipt_store(
                    self, state_path, lifecycle["legacy_store_objects"].get(state_path), acquire_lock=False
                )
                receipt = self.find(store, delegation_id)
                if receipt["binding"].get("origin_thread") != origin_thread:
                    raise HelperError("receipt immutable origin does not match acquired retirement authority")
                if compatibility == PRE_IDENTITY_ACQUIRED_NO_REPORT_COMPATIBILITY:
                    if recover:
                        raise HelperError("pre-identity acquired policy has no mutable outcome to recover")
                    if not valid_pre_identity_acquired_no_report_candidate(receipt):
                        raise HelperError(
                            "permanent-terminal policy requires the exact pre-identity acquired no-report chain"
                        )
                    return pre_identity_acquired_permanent_terminal_result(
                        origin_sha256, pair_sha256
                    )
                terminal = find_event(receipt, result_id)
                existing = find_event(receipt, event_id)
                acquired_intents = [
                    event for event in receipt.get("events", [])
                    if isinstance(event, dict) and event.get("kind") == "acquired_retirement_intent"
                ]
                if len(acquired_intents) > 1 or (
                    acquired_intents and acquired_intents[0].get("event_id") != event_id
                ):
                    raise HelperError("receipt already has a different acquired retirement operation")
                if terminal is not None:
                    if existing is None or not valid_acquired_no_report_pair_retirement_chain(receipt):
                        raise HelperError("terminal acquired no-report pair retirement proof is invalid")
                    validate_acquired_retirement_operation(
                        existing, authorization, event_id, compatibility
                    )
                    return acquired_pair_retirement_result(
                        origin_sha256, pair_sha256, "duplicate", compatibility
                    )
                if not valid_live_acquired_no_report_retirement_candidate(
                    receipt, authorization, event_id, compatibility
                ):
                    raise HelperError(
                        "retirement requires the exact live launch-completed acquired no-report chain"
                    )
                launch_intent = modern_read_only_retirement_launch_intent(receipt, compatibility)
                expected_private_executable_path = retirement_private_executable_path(
                    self, delegation_id, compatibility
                )
                try:
                    with self.launch_gate(delegation_id, timeout_seconds=0.2):
                        if existing is None:
                            if recover:
                                raise HelperError("acquired retirement recovery requires a durable intent")
                            identity = inspect_live_indeterminate_target(
                                launch_intent, receipt["binding"], self, delegation_id,
                                compatibility, expected_private_executable_path,
                            )
                            if not retirement_identity_matches_acquired_identity(
                                identity, receipt["session_identity"], launch_intent, receipt["binding"]
                            ):
                                raise HelperError("acquired retirement target does not match complete launch identity")
                            retirement_intent = {
                                "event_id": event_id,
                                "kind": "acquired_retirement_intent",
                                "terminal_state": authorization["terminal_state"],
                                "report_sha256": authorization["report_sha256"],
                                "coordinator_authorization_sha256": authorization["coordinator_authorization_sha256"],
                                "identity": identity,
                                "at": utc_now(),
                            }
                            if compatibility is not None:
                                retirement_intent["compatibility"] = compatibility
                            receipt["events"].append(retirement_intent)
                            receipt["acquired_retirement_intent"] = copy.deepcopy(retirement_intent)
                            receipt["updated_at"] = retirement_intent["at"]
                            self.commit(store)
                            current = inspect_live_indeterminate_target(
                                launch_intent, receipt["binding"], self, delegation_id,
                                compatibility, expected_private_executable_path,
                            )
                            if (
                                current != identity
                                or not retirement_identity_matches_acquired_identity(
                                    current, receipt["session_identity"], launch_intent, receipt["binding"]
                                )
                            ):
                                return acquired_pair_retirement_blocked(
                                    origin_sha256, pair_sha256, "retirement_identity_changed"
                                )
                            stop_exact_retirement_target(
                                identity, expected_private_executable_path, compatibility
                            )
                        else:
                            if not recover:
                                raise HelperError(
                                    "acquired retirement outcome is indeterminate; explicit recovery is required"
                                )
                            validate_acquired_retirement_operation(
                                existing, authorization, event_id, compatibility
                            )
                            identity = copy.deepcopy(existing.get("identity"))
                            if not retirement_identity_matches_acquired_identity(
                                identity, receipt["session_identity"], launch_intent, receipt["binding"]
                            ):
                                raise HelperError("durable acquired retirement identity is invalid")
                            absence = confirm_retirement_target_absent(identity)
                            if absence != "exact_retirement_target_absent":
                                try:
                                    current = inspect_live_indeterminate_target(
                                        launch_intent, receipt["binding"], self, delegation_id,
                                        compatibility, expected_private_executable_path,
                                    )
                                except HelperError:
                                    return acquired_pair_retirement_blocked(
                                        origin_sha256, pair_sha256, absence
                                    )
                                if (
                                    current != identity
                                    or not retirement_identity_matches_acquired_identity(
                                        current, receipt["session_identity"], launch_intent,
                                        receipt["binding"],
                                    )
                                ):
                                    return acquired_pair_retirement_blocked(
                                        origin_sha256, pair_sha256, "retirement_identity_changed"
                                    )
                                stop_exact_retirement_target(
                                    identity, expected_private_executable_path, compatibility
                                )
                        absence = confirm_retirement_target_absent(identity)
                        if absence != "exact_retirement_target_absent":
                            return acquired_pair_retirement_blocked(origin_sha256, pair_sha256, absence)
                        result = {
                            "event_id": result_id,
                            "kind": "acquired_pair_retired",
                            "operation_event_id": event_id,
                            "absence_code": absence,
                            "at": utc_now(),
                        }
                        receipt["events"].append(result)
                        receipt["acquired_pair_retired"] = copy.deepcopy(result)
                        receipt["updated_at"] = result["at"]
                        if not valid_acquired_no_report_pair_retirement_chain(receipt):
                            raise HelperError("terminal acquired no-report retirement proof did not seal exactly")
                        self.commit(store)
                except LaunchGateBusy:
                    return acquired_pair_retirement_blocked(
                        origin_sha256, pair_sha256, "launch_transport_active_or_indeterminate"
                    )
        return acquired_pair_retirement_result(
            origin_sha256, pair_sha256, "retired", compatibility
        )

    def worker_teardown(self, origin_thread: str, dry_run: bool) -> dict[str, Any]:
        required_string({"origin_thread": origin_thread}, "origin_thread", 256)
        origin_thread_sha256 = hashlib.sha256(origin_thread.encode()).hexdigest()
        try:
            if dry_run:
                lifecycle = self.lifecycle.load()
            else:
                with self.lifecycle.mutation_lock():
                    lifecycle = self.lifecycle.load()
                    if origin_thread not in lifecycle["teardown_fences"]:
                        lifecycle["teardown_fences"][origin_thread] = {
                            "operation_id": hashlib.sha256(f"worker-teardown\0{origin_thread}".encode()).hexdigest(),
                            "created_at": utc_now(),
                        }
                        self.lifecycle.commit(lifecycle)
            registered_state_paths = set(lifecycle["stores"])
            state_paths = set(registered_state_paths)
            state_paths.add(str(self.lifecycle.state_dir.resolve()))
            receipts: list[tuple[ReceiptStore, dict[str, Any]]] = []
            for state_path in sorted(state_paths):
                owner = ReceiptStore(pathlib.Path(state_path), self.lifecycle.state_dir)
                owner_store = load_registered_receipt_store(
                    owner, state_path, lifecycle["legacy_store_objects"].get(state_path),
                    require_exists=state_path in registered_state_paths,
                )
                receipts.extend(
                    (owner, copy.deepcopy(receipt)) for receipt in owner_store["receipts"]
                    if receipt["binding"].get("origin_thread") == origin_thread
                )
        except HelperError:
            return worker_teardown_store_blocked(origin_thread, dry_run)
        pairs: list[dict[str, str]] = []
        blockers: list[dict[str, str]] = []
        park_requests: list[tuple[ReceiptStore, dict[str, str]]] = []
        for owner, receipt in receipts:
            delegation_id = receipt["binding"]["delegation_id"]
            pair_id = hashlib.sha256(delegation_id.encode()).hexdigest()
            state = receipt.get("state")
            public_state = (
                state if state in {"created", "valid_report", "delivered", "acknowledged", "verified_parked"}
                else "unknown"
            )
            if valid_worker_detach_chain(receipt):
                public_state = "worker_detached"
            elif valid_pair_retirement_chain(receipt):
                public_state = "pair_retired"
            elif valid_acquired_no_report_pair_retirement_chain(receipt):
                public_state = "acquired_pair_retired"
            pair = {"pair_sha256": pair_id, "state": public_state}
            blocker = worker_teardown_receipt_blocker(receipt)
            if blocker is not None:
                pair["action"] = "block"
                pair["blocker"] = blocker
                pairs.append(pair)
                blockers.append({"pair_sha256": pair_id, "blocker": blocker})
                continue
            if state == "verified_parked":
                pair["action"] = "none"
                pairs.append(pair)
                continue
            if valid_worker_detach_chain(receipt):
                pair["action"] = "none"
                pairs.append(pair)
                continue
            if valid_pair_retirement_chain(receipt):
                pair["action"] = "none"
                pairs.append(pair)
                continue
            if valid_acquired_no_report_pair_retirement_chain(receipt):
                pair["action"] = "none"
                pairs.append(pair)
                continue
            identity = receipt["session_identity"]
            launch_intent = receipt_launch_intent(receipt)
            try:
                current = inspect_claude_identity(
                    identity["pane_id"],
                    identity["claude_session_id"],
                    launch_intent["expected_argv_digest"],
                    launch_intent["expected_launcher_identity"],
                    launch_intent["expected_executable_object_identity"],
                    identity["process_executable_identity"],
                    identity.get("process_executable_object_identity"),
                    launch_intent.get("expected_launcher_argv0_digest"),
                )
            except HelperError:
                blocker = "identity_mismatch_or_unavailable"
                pair["action"] = "block"
                pair["blocker"] = blocker
                pairs.append(pair)
                blockers.append({"pair_sha256": pair_id, "blocker": blocker})
                continue
            if current != identity:
                blocker = "identity_mismatch_or_unavailable"
                pair["action"] = "block"
                pair["blocker"] = blocker
                pairs.append(pair)
                blockers.append({"pair_sha256": pair_id, "blocker": blocker})
                continue
            pair["action"] = "park"
            pairs.append(pair)
            event_digest = hashlib.sha256(f"{origin_thread}\0{delegation_id}".encode()).hexdigest()
            park_requests.append((owner, {
                "delegation_id": delegation_id,
                "event_id": f"worker-teardown-park-{event_digest}",
            }))
        if blockers:
            return {
                "action": "worker_teardown",
                "origin_thread_sha256": origin_thread_sha256,
                "outcome": "blocked",
                "dry_run": dry_run,
                "pairs": pairs,
                "blockers": blockers,
                "recovery": "resolve the reported paired lifecycle blocker before retrying worker teardown",
            }
        if not dry_run:
            for owner, request in park_requests:
                try:
                    owner.park(request)
                except HelperError:
                    return {
                        "action": "worker_teardown",
                        "origin_thread_sha256": origin_thread_sha256,
                        "outcome": "blocked",
                        "dry_run": False,
                        "pairs": pairs,
                        "blockers": [{
                            "pair_sha256": hashlib.sha256(request["delegation_id"].encode()).hexdigest(),
                            "blocker": "park_indeterminate",
                        }],
                        "recovery": "inspect the durable park intent and use explicit session park recovery before retrying worker teardown",
                    }
        return {
            "action": "worker_teardown",
            "origin_thread_sha256": origin_thread_sha256,
            "outcome": "ready" if dry_run or not park_requests else "cleaned",
            "dry_run": dry_run,
            "pairs": pairs,
        }

    def release_worker_teardown(self, origin_thread: str) -> dict[str, Any]:
        required_string({"origin_thread": origin_thread}, "origin_thread", 256)
        origin_thread_sha256 = hashlib.sha256(origin_thread.encode()).hexdigest()
        try:
            with self.lifecycle.mutation_lock():
                lifecycle = self.lifecycle.load()
                registered_state_paths = set(lifecycle["stores"])
                state_paths = set(registered_state_paths)
                state_paths.add(str(self.lifecycle.state_dir.resolve()))
                for state_path in sorted(state_paths):
                    owner = ReceiptStore(pathlib.Path(state_path), self.lifecycle.state_dir)
                    owner_store = load_registered_receipt_store(
                        owner, state_path, lifecycle["legacy_store_objects"].get(state_path),
                        require_exists=state_path in registered_state_paths,
                        acquire_lock=pathlib.Path(state_path) != self.lifecycle.state_dir,
                    )
                    for receipt in owner_store["receipts"]:
                        if receipt["binding"].get("origin_thread") != origin_thread:
                            continue
                        if worker_teardown_receipt_blocker(receipt) is not None or receipt.get("state") != "verified_parked":
                            return {
                                "action": "worker_teardown_release",
                                "origin_thread_sha256": origin_thread_sha256,
                                "outcome": "blocked",
                                "blockers": [{"blocker": "paired_state_not_safely_parked"}],
                            }
                outcome = "released" if lifecycle["teardown_fences"].pop(origin_thread, None) is not None else "absent"
                if outcome == "released":
                    self.lifecycle.commit(lifecycle)
        except HelperError:
            return worker_teardown_store_blocked(origin_thread, False)
        return {
            "action": "worker_teardown_release",
            "origin_thread_sha256": origin_thread_sha256,
            "outcome": outcome,
        }

    def submit_message(self, request: dict[str, Any], expected_kind: str) -> str:
        with self.mutation_lock():
            store = self.load_store()
            delegation_id = protocol_id(request, "delegation_id")
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            if expected_kind == "report":
                expected_kind = "mutating_report" if receipt["binding"]["producer_role"] == "mutating_delegate" else "thinker_report"
            envelope = validate_envelope(request, expected_kind)
            validate_envelope_binding(envelope, receipt["binding"])
            event_id = envelope["message_id"]
            kind = "valid_report" if expected_kind in {"thinker_report", "mutating_report"} else "input_request"
            event = {"event_id": event_id, "kind": kind, "message": envelope}
            replay = find_event(receipt, event_id)
            if replay is not None:
                if replay.get("kind") == kind and replay.get("message") == envelope:
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            if expected_kind == "mutating_report" and receipt.get("submission_frozen"):
                raise HelperError("submission freeze prohibits another mutating report")
            if expected_kind in {"thinker_report", "mutating_report"} and receipt["report_message_id"]:
                raise HelperError("receipt already contains a different valid report")
            if expected_kind == "input_request" and receipt["report_message_id"]:
                raise HelperError("a valid report closes the input-request stream")
            timestamp = utc_now()
            event["at"] = timestamp
            if expected_kind in {"thinker_report", "mutating_report"}:
                if expected_kind == "mutating_report":
                    require_mutating_session(receipt)
                    validation = validate_mutating_handoff(envelope, receipt["binding"])
                    event["handoff_validation"] = validation
                    receipt["submission_frozen"] = True
                    receipt["writer_authority"] = "frozen"
                    receipt["handoff_validation"] = validation
                receipt["state"] = "valid_report"
                receipt["report_message_id"] = event_id
                if receipt.get("input_state") in {"pending", "seen"}:
                    event["supersedes_message_id"] = receipt["input_message_id"]
                    receipt["input_state"] = "resolved"
                    receipt["input_resolution"] = "superseded_by_report"
            else:
                if receipt.get("input_state") in {"pending", "seen"}:
                    event["supersedes_message_id"] = receipt["input_message_id"]
                    receipt["input_state"] = "resolved"
                    receipt["input_resolution"] = "superseded_by_input_request"
                receipt["input_message_id"] = event_id
                receipt["input_state"] = "pending"
                receipt.pop("input_resolution", None)
            receipt["updated_at"] = timestamp
            receipt["events"].append(event)
            self.commit(store)
            return "recorded"

    def consume(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        message_id = protocol_id(request, "message_id")
        reject_unknown(request, {"delegation_id", "event_id", "message_id"}, "consume event")
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            source = find_event(receipt, message_id)
            if source is None or source.get("kind") not in {"valid_report", "input_request"}:
                raise HelperError("inbox message was not found")
            kind = "delivered" if source["kind"] == "valid_report" else "input_seen"
            event = {"event_id": event_id, "kind": kind, "message_id": message_id}
            replay = find_event(receipt, event_id)
            if replay is not None:
                if event_without_time(replay) == event:
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            if source["kind"] == "valid_report":
                if receipt["state"] != "valid_report" or receipt["report_message_id"] != message_id:
                    raise HelperError("report delivery requires the current valid report")
                if source.get("message", {}).get("kind") == "mutating_report":
                    validation = validate_mutating_handoff(source["message"], receipt["binding"])
                    if validation != receipt.get("handoff_validation"):
                        raise HelperError("current handoff differs from the frozen objective validation")
                receipt["state"] = "delivered"
            else:
                if receipt.get("input_message_id") != message_id or receipt.get("input_state") not in {"pending", "seen"}:
                    raise HelperError("input request is no longer pending")
                receipt["input_state"] = "seen"
            event["at"] = utc_now()
            receipt["updated_at"] = event["at"]
            receipt["events"].append(event)
            self.commit(store)
            return "recorded"

    def acknowledge(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        message_id = protocol_id(request, "message_id")
        reject_unknown(request, {"delegation_id", "event_id", "message_id"}, "acknowledgement")
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            event = {"event_id": event_id, "kind": "acknowledged", "message_id": message_id}
            replay = find_event(receipt, event_id)
            if replay is not None:
                if event_without_time(replay) == event:
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            if receipt["state"] != "delivered" or receipt["report_message_id"] != message_id:
                raise HelperError("report acknowledgement requires delivery of the same valid report")
            event["at"] = utc_now()
            receipt["state"] = "acknowledged"
            receipt["updated_at"] = event["at"]
            receipt["events"].append(event)
            self.commit(store)
            return "recorded"

    def accept_input(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        message_id = protocol_id(request, "message_id")
        reject_unknown(request, {"delegation_id", "event_id", "message_id"}, "input acceptance")
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            event = {"event_id": event_id, "kind": "input_accepted", "message_id": message_id}
            replay = find_event(receipt, event_id)
            if replay is not None:
                if event_without_time(replay) == event:
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            if receipt.get("input_message_id") != message_id or receipt.get("input_state") not in {"pending", "seen"}:
                raise HelperError("input acceptance requires the current unresolved input request")
            event["at"] = utc_now()
            receipt["input_state"] = "resolved"
            receipt["input_resolution"] = "claude_accepted"
            receipt["updated_at"] = event["at"]
            receipt["events"].append(event)
            self.commit(store)
            return "recorded"

    def park(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        recover = request.get("recover", False)
        if not isinstance(recover, bool):
            raise HelperError("park recover must be boolean")
        reject_unknown(request, {"delegation_id", "event_id", "recover"}, "park event")
        result_id = internal_event_id("park-result", event_id)
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            replay = find_event(receipt, event_id)
            if replay is None:
                if receipt["state"] != "acknowledged":
                    raise HelperError("verified parking requires acknowledgement")
                if receipt.get("input_state") in {"pending", "seen"}:
                    raise HelperError("verified parking requires no unresolved input request")
            identity = copy.deepcopy(receipt.get("session_identity"))
            if not isinstance(identity, dict):
                raise HelperError("verified parking requires an acquired session identity")
            launch_intent = receipt_launch_intent(receipt)
            event = {"event_id": event_id, "kind": "park_intent", "identity": identity}
            if replay is not None:
                if event_without_time(replay) != event:
                    raise HelperError("event ID is already bound to a conflicting event")
                result = find_event(receipt, result_id)
                if result is not None:
                    if result.get("kind") != "verified_parked" or result.get("operation_event_id") != event_id:
                        raise HelperError("park result event conflicts with an existing event")
                    return "duplicate"
                if not recover:
                    raise HelperError("park outcome is indeterminate; explicit recovery is required")
            else:
                if recover:
                    raise HelperError("park recovery requires an existing indeterminate intent")
                event["at"] = utc_now()
                receipt["events"].append(event)
                receipt["updated_at"] = event["at"]
                self.commit(store)

        recovered_absence = False
        try:
            current = inspect_claude_identity(
                identity["pane_id"],
                identity["claude_session_id"],
                launch_intent["expected_argv_digest"],
                launch_intent["expected_launcher_identity"],
                launch_intent["expected_executable_object_identity"],
                identity["process_executable_identity"],
                identity.get("process_executable_object_identity"),
                launch_intent.get("expected_launcher_argv0_digest"),
            )
            if current != identity:
                raise HelperError("Claude pane, process, workdir, or incarnation identity changed")
            run_command(["tmux", "kill-pane", "-t", identity["pane_id"]])
        except HelperError as error:
            if recover and exact_pane_absent(identity["pane_id"]):
                recovered_absence = True
            else:
                self.record_park_failure(delegation_id, event_id, str(error))
                raise

        result_event = {
            "event_id": result_id,
            "kind": "verified_parked",
            "operation_event_id": event_id,
            "recovered_absence": recovered_absence,
        }
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            replay = find_event(receipt, result_id)
            if replay is not None:
                if event_without_time(replay) == result_event:
                    return "duplicate"
                raise HelperError("park result event conflicts with an existing event")
            if receipt["state"] != "acknowledged" or receipt.get("session_identity") != identity:
                raise HelperError("receipt identity changed while parking")
            timestamp = utc_now()
            result_event["at"] = timestamp
            receipt["state"] = "verified_parked"
            receipt["parked_at"] = timestamp
            receipt["cleanup_eligible_at"] = (datetime.now(timezone.utc) + timedelta(days=30)).isoformat().replace("+00:00", "Z")
            receipt["updated_at"] = timestamp
            receipt["events"].append(result_event)
            self.commit(store)
            return "recorded"

    def record_park_failure(self, delegation_id: str, event_id: str, reason: str) -> None:
        failure_id = internal_event_id("park-failure", event_id)
        failure = {"event_id": failure_id, "kind": "park_failed", "operation_event_id": event_id, "reason": reason[:2048]}
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            replay = find_event(receipt, failure_id)
            if replay is not None:
                if replay.get("kind") != "park_failed" or replay.get("operation_event_id") != event_id:
                    raise HelperError("park failure event conflicts with an existing event")
                return
            failure["at"] = utc_now()
            receipt["events"].append(failure)
            receipt["updated_at"] = failure["at"]
            self.commit(store)

    def acquire_session(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        pane_id = required_string(request, "pane_id", 64)
        claude_session_id = protocol_id(request, "claude_session_id")
        reject_unknown(request, {"delegation_id", "event_id", "pane_id", "claude_session_id"}, "session acquisition")

        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            launch_identity = completed_launch_identity(receipt)
            launch_intent = receipt_launch_intent(receipt)
            replay = find_event(receipt, event_id)
            if replay is not None:
                replay_identity = replay.get("identity", {})
                if (
                    replay.get("kind") == "session_acquired"
                    and replay_identity.get("pane_id") == pane_id
                    and replay_identity.get("claude_session_id") == claude_session_id
                ):
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")

        identity = inspect_claude_identity(
            pane_id,
            claude_session_id,
            launch_intent["expected_argv_digest"],
            launch_intent["expected_launcher_identity"],
            launch_intent["expected_executable_object_identity"],
            launch_identity["process_executable_identity"],
            launch_identity.get("process_executable_object_identity"),
            launch_intent.get("expected_launcher_argv0_digest"),
        )
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            launch_identity = completed_launch_identity(receipt)
            current_intent = receipt_launch_intent(receipt)
            if current_intent != launch_intent:
                raise HelperError("immutable launch intent changed during session acquisition")
            if identity != launch_identity:
                raise HelperError("Claude session does not match the exact process and pane created by this receipt")
            if identity["workdir"] != str(pathlib.Path(receipt["binding"]["workdir"]).resolve(strict=True)):
                raise HelperError("Claude session workdir does not match immutable receipt binding")
            if identity["launch_command_digest"] != receipt["binding"]["launch_command_digest"]:
                raise HelperError("Claude session launch command does not match immutable receipt binding")
            event = {"event_id": event_id, "kind": "session_acquired", "identity": identity}
            replay = find_event(receipt, event_id)
            if replay is not None:
                replay_identity = replay.get("identity", {})
                if (
                    replay.get("kind") == "session_acquired"
                    and replay_identity.get("pane_id") == pane_id
                    and replay_identity.get("claude_session_id") == claude_session_id
                ):
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            if receipt.get("session_identity") and receipt["session_identity"] != identity:
                raise HelperError("receipt is already bound to a different Claude incarnation")
            event["at"] = utc_now()
            receipt["session_identity"] = identity
            receipt["updated_at"] = event["at"]
            receipt["events"].append(event)
            self.commit(store)
            return "recorded"

    def notify_amp(self, request: dict[str, Any]) -> str:
        delegation_id = protocol_id(request, "delegation_id")
        event_id = protocol_id(request, "event_id")
        message_id = protocol_id(request, "message_id")
        target = validate_amp_target(request.get("target"))
        reject_unknown(request, {"delegation_id", "event_id", "message_id", "target"}, "notification")
        attempt = {"event_id": event_id, "kind": "notification_attempted", "message_id": message_id, "target": target}
        result_id = internal_event_id("notification-result", event_id)
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            source = find_event(receipt, message_id)
            if source is None or source.get("kind") != "valid_report":
                raise HelperError("notification requires a durably persisted valid report")
            replay = find_event(receipt, event_id)
            result = find_event(receipt, result_id)
            if result is not None and (result.get("kind") != "notification_result" or result.get("message_id") != message_id or result.get("operation_event_id") != event_id):
                raise HelperError("notification result event conflicts with an existing event")
            if replay is not None:
                if event_without_time(replay) != attempt:
                    raise HelperError("event ID is already bound to a conflicting event")
                if result is not None:
                    return "notified" if result.get("result") == "notified" else "unavailable"
                interrupted = {
                    "event_id": result_id,
                    "kind": "notification_result",
                    "operation_event_id": event_id,
                    "message_id": message_id,
                    "result": "unavailable",
                    "reason": "notification attempt was interrupted before a durable result; wake-up was not resent",
                    "at": utc_now(),
                }
                receipt["events"].append(interrupted)
                receipt["updated_at"] = interrupted["at"]
                self.commit(store)
                return "unavailable"
            else:
                attempt["at"] = utc_now()
                receipt["events"].append(attempt)
                receipt["updated_at"] = attempt["at"]
                self.commit(store)

        result_value = "unavailable"
        reason = "target verification failed"
        try:
            current = inspect_amp_target(target["pane_id"], target["origin_thread"])
            if current != target:
                reason = "target identity changed"
            else:
                delegation_digest = hashlib.sha256(delegation_id.encode()).hexdigest()
                message_digest = hashlib.sha256(message_id.encode()).hexdigest()
                token = f"AMUX_CLAUDE_REPORT delegation_sha256={delegation_digest} message_sha256={message_digest}"
                run_command(["tmux", "send-keys", "-t", target["pane_id"], token, "Enter"])
                result_value = "notified"
                reason = "verified Amp pane received a wake-up token; this is not delivery"
        except HelperError as error:
            reason = str(error)

        result_event = {
            "event_id": result_id,
            "kind": "notification_result",
            "operation_event_id": event_id,
            "message_id": message_id,
            "result": result_value,
            "reason": reason,
        }
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            require_receipt_mutable(receipt)
            replay = find_event(receipt, result_id)
            if replay is not None:
                if event_without_time(replay) != result_event:
                    raise HelperError("notification result event conflicts with an existing result")
            else:
                result_event["at"] = utc_now()
                receipt["events"].append(result_event)
                receipt["updated_at"] = result_event["at"]
                self.commit(store)
        return result_value

    def show(self, delegation_id: str) -> dict[str, Any]:
        store = self.load_store()
        return copy.deepcopy(self.find(store, delegation_id))


def required_string(value: dict[str, Any], key: str, limit: int) -> str:
    candidate = value.get(key)
    if not isinstance(candidate, str) or not candidate or len(candidate.encode()) > limit:
        raise HelperError(f"{key} must be a non-empty string of at most {limit} bytes")
    return candidate


def protocol_id(value: dict[str, Any], key: str, limit: int = 256) -> str:
    candidate = required_string(value, key, limit)
    if candidate.startswith(INTERNAL_EVENT_PREFIX) or any(character.isspace() or ord(character) < 0x20 or ord(character) == 0x7F for character in candidate):
        raise HelperError(f"{key} contains reserved, whitespace, or control characters")
    return candidate


def optional_read_only_model(value: dict[str, Any]) -> str | None:
    if "model" not in value:
        return None
    model = value["model"]
    if not isinstance(model, str) or model not in APPROVED_READ_ONLY_MODELS:
        raise HelperError("model must be an exact approved read-only model identifier")
    return model


def help_exposes_option(help_text: str, option: str) -> bool:
    for line in help_text.splitlines():
        stripped = line.lstrip()
        if not stripped.startswith("-"):
            continue
        for token in stripped.split():
            candidate = token.rstrip(",")
            if candidate == option or candidate.startswith(option + "="):
                return True
            if not token.startswith("-"):
                break
    return False


def internal_event_id(kind: str, event_id: str) -> str:
    return f"{INTERNAL_EVENT_PREFIX}{kind}:{hashlib.sha256(event_id.encode()).hexdigest()}"


def validate_binding(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("binding must be an object")
    allowed = {
        "protocol_version",
        "delegation_id",
        "nonce",
        "task_id",
        "question_message_id",
        "origin_thread",
        "repository",
        "base",
        "workdir",
        "producer_role",
        "authority",
        "task_reference",
        "packet_digest",
        "launch_policy_digest",
        "launch_command_digest",
    }
    mutating_fields = {
        "baseline_branch",
        "writer_owner",
        "integration_owner",
        "handoff",
        "capacity_decision_digest",
    }
    if value.get("producer_role") == "mutating_delegate":
        allowed |= mutating_fields
    elif value.get("producer_role") == "thinker":
        allowed.add("model")
    reject_unknown(value, allowed, "binding")
    if value.get("protocol_version") != PROTOCOL_VERSION:
        raise HelperError("unsupported protocol_version")
    for key in allowed - {"protocol_version", "model"}:
        required_string(value, key, 2048 if key == "workdir" else 256)
    protocol_id(value, "delegation_id")
    if value["producer_role"] == "thinker":
        if value["authority"] != "read_only":
            raise HelperError("thinker authority must remain read_only")
        optional_read_only_model(value)
    elif value["producer_role"] == "mutating_delegate":
        if value["authority"] != "exclusive_writer":
            raise HelperError("mutating delegate authority must be exclusive_writer")
        for key in mutating_fields:
            required_string(value, key, 256)
        if value["writer_owner"] != "claude_mutating_delegate" or value["integration_owner"] != "amp_coordinator":
            raise HelperError("mutating binding has invalid exclusive writer or integration ownership")
        if value["handoff"] != "one_clean_local_commit":
            raise HelperError("mutating binding permits only one_clean_local_commit handoff")
    else:
        raise HelperError("producer_role must be thinker or mutating_delegate")
    for key in ("nonce", "packet_digest", "launch_policy_digest", "launch_command_digest"):
        if len(value[key]) != 64 or any(character not in "0123456789abcdef" for character in value[key]):
            raise HelperError(f"{key} must be a lowercase SHA-256 value")
    if value["producer_role"] == "mutating_delegate":
        digest = value["capacity_decision_digest"]
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
            raise HelperError("capacity_decision_digest must be a lowercase SHA-256 value")
    return copy.deepcopy(value)


def mutating_writer_lease(binding: dict[str, Any]) -> str | None:
    if binding.get("producer_role") != "mutating_delegate":
        return None
    return str(pathlib.Path(binding["workdir"]).resolve())


def receipt_writer_lease(receipt: dict[str, Any]) -> str | None:
    lease = receipt.get("writer_lease")
    if isinstance(lease, str):
        return lease
    return mutating_writer_lease(receipt["binding"])


def writer_leases_match(left: str, right: str) -> bool:
    if left == right:
        return True
    try:
        return os.path.samefile(left, right)
    except OSError as error:
        raise HelperError("cannot safely compare mutating writer lease identities") from error


def validate_envelope(value: Any, expected_kind: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("message must be an object")
    payload_key = "report" if expected_kind in {"thinker_report", "mutating_report"} else "input_request"
    common = {
        "protocol_version",
        "delegation_id",
        "nonce",
        "message_id",
        "in_reply_to",
        "kind",
        "task_id",
        "origin_thread",
        "repository",
        "base",
        "workdir",
        "producer_role",
        "authority",
        "launch_policy_digest",
        "created_at",
        payload_key,
    }
    reject_unknown(value, common, "message")
    if value.get("protocol_version") != PROTOCOL_VERSION or value.get("kind") != expected_kind:
        raise HelperError("message protocol_version or kind is invalid")
    for key in common - {"protocol_version", payload_key}:
        required_string(value, key, 2048 if key == "workdir" else 256)
    for key in ("delegation_id", "message_id", "in_reply_to"):
        protocol_id(value, key)
    try:
        datetime.fromisoformat(value["created_at"].replace("Z", "+00:00"))
    except (ValueError, TypeError) as error:
        raise HelperError("created_at must be an ISO-8601 timestamp") from error
    result = copy.deepcopy(value)
    if expected_kind == "thinker_report":
        result[payload_key] = validate_report(value.get(payload_key))
    elif expected_kind == "mutating_report":
        result[payload_key] = validate_mutating_report(value.get(payload_key))
    else:
        result[payload_key] = validate_input_request(value.get(payload_key))
    return result


def validate_envelope_binding(envelope: dict[str, Any], binding: dict[str, Any]) -> None:
    matches = {
        "protocol_version": "protocol_version",
        "delegation_id": "delegation_id",
        "nonce": "nonce",
        "task_id": "task_id",
        "origin_thread": "origin_thread",
        "repository": "repository",
        "base": "base",
        "workdir": "workdir",
        "producer_role": "producer_role",
        "authority": "authority",
        "launch_policy_digest": "launch_policy_digest",
    }
    for message_key, binding_key in matches.items():
        if envelope[message_key] != binding[binding_key]:
            raise HelperError(f"message {message_key} does not match immutable receipt binding")
    if envelope["in_reply_to"] != binding["question_message_id"]:
        raise HelperError("message in_reply_to does not match the immutable question")


def validate_report(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("report must be an object")
    fields = {
        "accepted_role",
        "accepted_exclusions",
        "status",
        "verdict",
        "rationale",
        "evidence",
        "assumptions",
        "unsupported_claims",
        "blockers",
        "verification",
        "changed_artifacts",
        "references",
    }
    reject_unknown(value, fields, "report")
    if value.get("accepted_role") is not True or value.get("accepted_exclusions") is not True:
        raise HelperError("report must accept the thinker role and exclusions")
    if value.get("status") not in {"complete", "blocked"}:
        raise HelperError("report status must be complete or blocked")
    required_string(value, "verdict", 4096)
    required_string(value, "rationale", 8192)
    for key in fields - {"accepted_role", "accepted_exclusions", "status", "verdict", "rationale"}:
        validate_string_list(value.get(key), key)
    if value["changed_artifacts"]:
        raise HelperError("read-only thinker reports cannot contain changed artifacts")
    reject_private_fields(value)
    return copy.deepcopy(value)


def validate_mutating_report(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("report must be an object")
    fields = {
        "accepted_role",
        "accepted_exclusions",
        "status",
        "summary",
        "blockers",
        "changed_artifacts",
        "verification",
        "references",
        "handoff_commit",
        "authorship",
        "non_claims",
    }
    reject_unknown(value, fields, "mutating report")
    if value.get("accepted_role") is not True or value.get("accepted_exclusions") is not True:
        raise HelperError("report must accept the mutating role and exclusions")
    status = value.get("status")
    if status not in {"complete", "blocked"}:
        raise HelperError("mutating report status must be complete or blocked")
    required_string(value, "summary", 8192)
    for key in ("blockers", "changed_artifacts", "verification", "references"):
        validate_string_list(value.get(key), key)
    handoff_commit = value.get("handoff_commit")
    if not isinstance(handoff_commit, str) or len(handoff_commit.encode()) > 256:
        raise HelperError("handoff_commit must be a string of at most 256 bytes")
    if status == "complete" and not handoff_commit:
        raise HelperError("complete mutating report requires a handoff commit")
    if status == "blocked" and (handoff_commit or value["changed_artifacts"]):
        raise HelperError("blocked mutating report requires zero commit and no changed artifacts")
    if value.get("authorship") != "claude_mutating_delegate":
        raise HelperError("mutating report must declare Claude delegate authorship")
    non_claims = value.get("non_claims")
    required_non_claims = {"correct": False, "accepted": False, "merge_ready": False, "cleanup_authorized": False}
    if non_claims != required_non_claims:
        raise HelperError("report validity must disclaim correctness, acceptance, merge readiness, and cleanup authority")
    reject_private_fields(value)
    return copy.deepcopy(value)


def validate_mutating_handoff(envelope: dict[str, Any], binding: dict[str, Any]) -> dict[str, Any]:
    if binding.get("producer_role") != "mutating_delegate" or binding.get("authority") != "exclusive_writer":
        raise HelperError("mutating report requires an immutable mutating delegation binding")
    workdir = str(pathlib.Path(binding["workdir"]).resolve(strict=True))
    git = ["git", "--no-optional-locks", "-C", workdir]
    if run_command(git + ["rev-parse", "--show-toplevel"]) != workdir:
        raise HelperError("handoff workdir is not the bound Git worktree")
    branch = run_command(git + ["symbolic-ref", "--short", "HEAD"])
    if branch != binding["baseline_branch"]:
        raise HelperError("handoff branch differs from the immutable baseline branch")
    if run_command(git + ["status", "--porcelain"]):
        raise HelperError("handoff requires a clean worktree")
    head = run_command(git + ["rev-parse", "HEAD"])
    count_text = run_command(git + ["rev-list", "--count", f"{binding['base']}..{head}"])
    try:
        commit_count = int(count_text)
    except ValueError as error:
        raise HelperError("handoff commit count is invalid") from error
    report = envelope["report"]
    if report["status"] == "complete":
        if commit_count != 1:
            raise HelperError("successful handoff requires exactly one commit beyond the immutable baseline")
        parents = run_command(git + ["rev-list", "--parents", "-n", "1", head]).split()
        if parents != [head, binding["base"]]:
            raise HelperError("handoff commit must be one direct child of the immutable baseline")
        if report["handoff_commit"] != head:
            raise HelperError("reported handoff commit does not match worktree HEAD")
        outcome = "complete"
    else:
        if commit_count != 0 or head != binding["base"]:
            raise HelperError("blocked handoff requires zero commits beyond the immutable baseline")
        outcome = "blocked"
    return {
        "outcome": outcome,
        "baseline_commit": binding["base"],
        "baseline_branch": binding["baseline_branch"],
        "handoff_commit": head if outcome == "complete" else "",
        "commit_count": commit_count,
        "clean": True,
        "validation_scope": "objective_handoff_only",
        "correct": False,
        "accepted": False,
        "merge_ready": False,
        "cleanup_authorized": False,
    }


def require_mutating_session(receipt: dict[str, Any]) -> None:
    intents = [event for event in receipt["events"] if event.get("kind") == "launch_intent"]
    completed = [event for event in receipt["events"] if event.get("kind") == "launch_completed"]
    acquired = [event for event in receipt["events"] if event.get("kind") == "session_acquired"]
    identity = receipt.get("session_identity")
    if (
        len(intents) != 1
        or intents[0].get("workflow") != "mutating"
        or len(completed) != 1
        or completed[0].get("operation_event_id") != intents[0].get("event_id")
        or len(acquired) != 1
        or not isinstance(identity, dict)
        or acquired[0].get("identity") != identity
    ):
        raise HelperError("mutating report requires one completed and acquired mutating Claude session")


def validate_input_request(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("input_request must be an object")
    reject_unknown(value, {"request_type", "question", "blocking_reason"}, "input_request")
    if value.get("request_type") not in {"clarification", "decision", "missing_evidence"}:
        raise HelperError("input_request request_type is invalid")
    required_string(value, "question", 2048)
    required_string(value, "blocking_reason", 2048)
    reject_private_fields(value)
    return copy.deepcopy(value)


def validate_string_list(value: Any, key: str) -> None:
    if not isinstance(value, list) or len(value) > 32:
        raise HelperError(f"report {key} must be a list of at most 32 strings")
    for item in value:
        if not isinstance(item, str) or not item or len(item.encode()) > 2048:
            raise HelperError(f"report {key} entries must be non-empty strings of at most 2048 bytes")


def reject_private_fields(value: Any) -> None:
    forbidden = {"prompt", "transcript", "pane_capture", "tool_stream", "secret", "artifact_content", "complete_artifact"}
    if isinstance(value, dict):
        for key, nested in value.items():
            if key.lower() in forbidden:
                raise HelperError(f"private content field {key!r} is forbidden")
            reject_private_fields(nested)
    elif isinstance(value, list):
        for nested in value:
            reject_private_fields(nested)


def run_command(
    arguments: list[str], environment: dict[str, str] | None = None, executable_fd: int | None = None
) -> str:
    options: dict[str, Any] = {}
    temporary_executable: pathlib.Path | None = None
    temporary_directory: pathlib.Path | None = None
    temporary_executable_descriptor: int | None = None
    temporary_directory_descriptor: int | None = None
    if environment is not None:
        options["env"] = environment
    try:
        if executable_fd is not None:
            if platform.system() == "Darwin":
                source_descriptor = os.dup(executable_fd)
                _, _, _, source_object_identity = verified_executable_descriptor(
                    source_descriptor, "verified Claude probe source"
                )
                os.close(source_descriptor)
                temporary_directory = pathlib.Path(tempfile.mkdtemp(prefix="amux-claude-probe."))
                os.chmod(temporary_directory, 0o700)
                temporary_executable = materialize_executable(executable_fd, temporary_directory)
                temporary_executable_descriptor, _, _, copied_object_identity = (
                    open_exact_verified_executable(temporary_executable)
                )
                temporary_directory_descriptor = os.open(
                    temporary_directory,
                    os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0),
                )
                if (
                    executable_content_identity(copied_object_identity)
                    != executable_content_identity(source_object_identity)
                ):
                    raise HelperError("verified Claude probe executable copy changed")
                seal_darwin_launch_container(
                    temporary_directory_descriptor, temporary_executable_descriptor
                )
                options["executable"] = str(temporary_executable)
            else:
                options["executable"] = descriptor_path(executable_fd)
                options["pass_fds"] = (executable_fd,)
        completed = subprocess.run(
            arguments, capture_output=True, text=True, timeout=5, check=False, **options
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        raise HelperError(f"run {arguments[0]}: {error}") from error
    finally:
        try:
            if temporary_directory_descriptor is not None and temporary_executable_descriptor is not None:
                restore_darwin_launch_container(
                    temporary_directory_descriptor, temporary_executable_descriptor
                )
        finally:
            if temporary_executable_descriptor is not None:
                os.close(temporary_executable_descriptor)
            if temporary_directory_descriptor is not None:
                os.close(temporary_directory_descriptor)
            if temporary_executable is not None:
                temporary_executable.unlink(missing_ok=True)
            if temporary_directory is not None:
                temporary_directory.rmdir()
    if completed.returncode != 0:
        detail = completed.stderr.strip() or f"exit {completed.returncode}"
        raise HelperError(f"run {arguments[0]}: {detail}")
    return completed.stdout.rstrip("\r\n")


def decode_tmux_command_argument(value: str) -> str:
    if "\n" in value or "\r" in value:
        raise HelperError("tmux command argument encoding is not single-line")
    output: list[str] = []
    index = 0
    quote: str | None = None
    while index < len(value):
        character = value[index]
        if quote == "'":
            if character == "'":
                quote = None
            else:
                output.append(character)
            index += 1
            continue
        if character in "'\"":
            if quote is None:
                quote = character
            elif quote == character:
                quote = None
            else:
                output.append(character)
            index += 1
            continue
        if quote is None and character.isspace():
            raise HelperError("tmux command argument encoding contains multiple arguments")
        if character != "\\":
            output.append(character)
            index += 1
            continue
        if index + 1 >= len(value):
            raise HelperError("tmux command argument encoding is truncated")
        escaped = value[index + 1]
        replacements = {"e": "\x1b", "r": "\r", "n": "\n", "t": "\t"}
        if escaped in replacements:
            output.append(replacements[escaped])
            index += 2
            continue
        if escaped in "01234567":
            octal = value[index + 1 : index + 4]
            if len(octal) != 3 or any(digit not in "01234567" for digit in octal):
                raise HelperError("tmux command argument encoding contains invalid octal")
            output.append(chr(int(octal, 8)))
            index += 4
            continue
        output.append(escaped)
        index += 2
    if quote is not None:
        raise HelperError("tmux command argument encoding has unterminated quoting")
    return "".join(output)


def encode_tmux_command_argument(value: str) -> str:
    output = ['"']
    replacements = {
        "\\": "\\\\", '"': '\\"', "$": "\\$", "\x1b": "\\e", "\r": "\\r",
        "\n": "\\n", "\t": "\\t",
    }
    for character in value:
        if character in replacements:
            output.append(replacements[character])
        elif ord(character) < 32 or ord(character) == 127:
            output.append(f"\\{ord(character):03o}")
        else:
            output.append(character)
    output.append('"')
    return "".join(output)


def tmux_single_line_format(variable: str) -> str:
    if not variable or any(character not in "abcdefghijklmnopqrstuvwxyz_" for character in variable):
        raise HelperError("retirement tmux format variable is invalid")
    return f"#{{s/\r/\\\\r/:#{{s/\n/\\\\n/:#{{q:{variable}}}}}}}"


class TmuxControlConnection:
    def __init__(
        self, session: str, command_prefix: list[str] | None = None
    ):
        self.session = required_string({"session": session}, "session", 256)
        self.command_prefix = list(command_prefix or ["tmux"])
        self.process: subprocess.Popen[bytes] | None = None
        self.selector: selectors.BaseSelector | None = None
        self.buffer = b""

    def __enter__(self) -> TmuxControlConnection:
        try:
            self.process = subprocess.Popen(
                self.command_prefix + ["-C", "attach-session", "-t", self.session],
                stdin=subprocess.PIPE,
                stdout=subprocess.PIPE,
                stderr=subprocess.DEVNULL,
                bufsize=0,
            )
        except OSError as error:
            raise HelperError("retirement tmux control connection is unavailable") from error
        assert self.process.stdout is not None
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)
        self._read_response()
        return self

    def _read_response(self) -> list[str]:
        if self.process is None or self.process.stdout is None or self.selector is None:
            raise HelperError("retirement tmux control connection is unavailable")
        deadline = time.monotonic() + 2.0
        response_frame: tuple[str, str, str] | None = None
        output: list[str] = []
        total = 0
        while time.monotonic() < deadline:
            while b"\n" not in self.buffer:
                if not self.selector.select(deadline - time.monotonic()):
                    raise HelperError("retirement tmux control response timed out")
                chunk = os.read(self.process.stdout.fileno(), 4096)
                if not chunk:
                    raise HelperError("retirement tmux control connection disappeared")
                self.buffer += chunk
                if len(self.buffer) > MAX_ABSENCE_OUTPUT_BYTES:
                    raise HelperError("retirement tmux control response exceeds the size limit")
            raw_line, self.buffer = self.buffer.split(b"\n", 1)
            total += len(raw_line) + 1
            if total > MAX_ABSENCE_OUTPUT_BYTES:
                raise HelperError("retirement tmux control response exceeds the size limit")
            try:
                line = raw_line.decode("utf-8")
            except UnicodeDecodeError as error:
                raise HelperError("retirement tmux control response is not UTF-8") from error

            marker: str | None = None
            frame: tuple[str, str, str] | None = None
            for candidate in ("begin", "end", "error"):
                prefix = f"%{candidate} "
                if line.startswith(prefix):
                    fields = line.split(" ")
                    if len(fields) != 4 or any(
                        not field
                        or any(character < "0" or character > "9" for character in field)
                        for field in fields[1:]
                    ):
                        raise HelperError("retirement tmux control frame is invalid")
                    marker = candidate
                    frame = (fields[1], fields[2], fields[3])
                    break
                if line == f"%{candidate}" or line.startswith(f"%{candidate}"):
                    raise HelperError("retirement tmux control frame is invalid")

            if marker == "begin":
                if response_frame is not None:
                    raise HelperError("retirement tmux control response is ambiguous")
                response_frame = frame
                output = []
            elif marker in {"end", "error"}:
                if response_frame is None or frame != response_frame:
                    raise HelperError("retirement tmux control frame does not match")
                if marker == "error":
                    raise HelperError("retirement tmux control command failed")
                return output
            elif line == "%exit" or line.startswith("%exit "):
                raise HelperError("retirement tmux control connection disappeared")
            elif response_frame is not None:
                if line.startswith("%"):
                    raise HelperError("retirement tmux control response is ambiguous")
                output.append(line)
        raise HelperError("retirement tmux control response timed out")

    def command_sequence(self, commands: list[list[str]]) -> list[list[str]]:
        if self.process is None or self.process.stdin is None or self.process.poll() is not None:
            raise HelperError("retirement tmux control connection disappeared")
        if not commands or any(not command for command in commands):
            raise HelperError("retirement tmux control command sequence is invalid")
        try:
            encoded = [
                " ".join(encode_tmux_command_argument(argument) for argument in command)
                for command in commands
            ]
            self.process.stdin.write((" ; ".join(encoded) + "\n").encode())
            self.process.stdin.flush()
        except (BrokenPipeError, OSError) as error:
            raise HelperError("retirement tmux control connection disappeared") from error
        return [self._read_response() for _ in commands]

    def command(self, arguments: list[str]) -> list[str]:
        return self.command_sequence([arguments])[0]

    def close(self) -> None:
        process = self.process
        selector = self.selector
        self.process = None
        self.selector = None
        if selector is not None:
            selector.close()
        if process is None:
            return
        if process.stdin is not None:
            process.stdin.close()
        try:
            process.wait(timeout=0.2)
        except subprocess.TimeoutExpired:
            process.terminate()
            try:
                process.wait(timeout=0.2)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()

    def __exit__(self, *_: Any) -> None:
        self.close()


def exact_pane_absent(pane_id: str) -> bool:
    try:
        panes = run_command(["tmux", "list-panes", "-a", "-F", "#{pane_id}"]).splitlines()
    except HelperError:
        return False
    return pane_id not in panes


class DarwinBSDInfo(ctypes.Structure):
    _fields_ = [
        ("flags", ctypes.c_uint32), ("status", ctypes.c_uint32), ("xstatus", ctypes.c_uint32),
        ("pid", ctypes.c_uint32), ("ppid", ctypes.c_uint32), ("uid", ctypes.c_uint32),
        ("gid", ctypes.c_uint32), ("ruid", ctypes.c_uint32), ("rgid", ctypes.c_uint32),
        ("svuid", ctypes.c_uint32), ("svgid", ctypes.c_uint32), ("reserved", ctypes.c_uint32),
        ("comm", ctypes.c_char * 16), ("name", ctypes.c_char * 32), ("nfiles", ctypes.c_uint32),
        ("pgid", ctypes.c_uint32), ("jobc", ctypes.c_uint32), ("tdev", ctypes.c_uint32),
        ("tpgid", ctypes.c_uint32), ("nice", ctypes.c_int32), ("start_seconds", ctypes.c_uint64),
        ("start_microseconds", ctypes.c_uint64),
    ]


def linux_process_start_time(raw: bytes) -> str:
    open_paren = raw.find(b"(")
    close = raw.rfind(b")")
    if open_paren <= 0 or close <= open_paren or not raw[:open_paren].strip().isdigit():
        raise HelperError("read exact Linux process identity: invalid stat")
    fields = raw[close + 1 :].split()
    if len(fields) < 20 or not fields[19].isdigit() or int(fields[19]) <= 0:
        raise HelperError("read exact Linux process identity: invalid start time")
    if fields[0] == b"Z":
        raise HelperError("read exact Linux process identity: process is a zombie")
    return os.fsdecode(fields[19])


def read_linux_process_start(proc: pathlib.Path) -> str:
    return linux_process_start_time((proc / "stat").read_bytes())


def read_linux_process_executable(proc: pathlib.Path) -> tuple[str, int, int]:
    target = os.readlink(proc / "exe")
    info = (proc / "exe").stat()
    return target, info.st_dev, info.st_ino


def read_linux_process_command(proc: pathlib.Path) -> bytes:
    return (proc / "cmdline").read_bytes()


def read_linux_process_snapshot(pid: int) -> tuple[str, str, bytes, str]:
    proc = LINUX_PROC_ROOT / str(pid)
    try:
        start_before = read_linux_process_start(proc)
        executable_before = read_linux_process_executable(proc)
        command_before = read_linux_process_command(proc)
        executable_after = read_linux_process_executable(proc)
        command_after = read_linux_process_command(proc)
        start_after = read_linux_process_start(proc)
    except (OSError, ValueError) as error:
        raise HelperError(f"read exact Linux process identity: {error}") from error
    if start_before != start_after or executable_before != executable_after or command_before != command_after:
        raise HelperError("read exact Linux process identity: process changed during inspection")
    if not command_before or not command_before.endswith(b"\0"):
        raise HelperError("read exact Linux process arguments: cmdline is empty or truncated")
    executable_target, executable_device, executable_inode = executable_before
    identity = f"linux:{start_before}:{executable_device}:{executable_inode}:{hashlib.sha256(os.fsencode(executable_target)).hexdigest()}"
    return pathlib.Path(executable_target).name, identity, command_before, executable_target


def exact_process_identity(pid: int) -> tuple[str, str, list[str], str]:
    system = platform.system()
    if system == "Linux":
        name, identity, command, _ = read_linux_process_snapshot(pid)
        arguments = [os.fsdecode(argument) for argument in command[:-1].split(b"\0")]
        if not arguments or not arguments[0]:
            raise HelperError("read exact Linux process arguments: argv[0] is empty")
        return name, identity, arguments, hashlib.sha256(command[:-1]).hexdigest()
    if system != "Darwin":
        raise HelperError(f"exact process identity is unavailable on {system}")

    libc = ctypes.CDLL("/usr/lib/libSystem.B.dylib", use_errno=True)
    mib = (ctypes.c_int * 3)(1, 49, pid)  # CTL_KERN, KERN_PROCARGS2, pid
    size = ctypes.c_size_t()
    if libc.sysctl(mib, 3, None, ctypes.byref(size), None, 0) != 0 or size.value < 5:
        raise HelperError("read exact Darwin process arguments: sysctl size failed")
    buffer = ctypes.create_string_buffer(size.value)
    if libc.sysctl(mib, 3, buffer, ctypes.byref(size), None, 0) != 0:
        raise HelperError("read exact Darwin process arguments: sysctl failed")
    raw = buffer.raw[: size.value]
    argument_count = int.from_bytes(raw[:4], sys.byteorder, signed=True)
    if argument_count <= 0 or argument_count > 4096:
        raise HelperError("read exact Darwin process arguments: invalid argc")
    offset = raw.find(b"\0", 4)
    if offset < 0:
        raise HelperError("read exact Darwin process arguments: executable path is unterminated")
    offset += 1
    while offset < len(raw) and raw[offset] == 0:
        offset += 1
    arguments: list[str] = []
    for _ in range(argument_count):
        end = raw.find(b"\0", offset)
        if end < 0:
            raise HelperError("read exact Darwin process arguments: argv is truncated")
        arguments.append(os.fsdecode(raw[offset:end]))
        offset = end + 1

    libproc = ctypes.CDLL("/usr/lib/libproc.dylib", use_errno=True)
    info = DarwinBSDInfo()
    written = libproc.proc_pidinfo(pid, 3, 0, ctypes.byref(info), ctypes.sizeof(info))
    if written != ctypes.sizeof(info):
        raise HelperError("read exact Darwin process identity: proc_pidinfo failed")
    if info.status == 5:
        raise HelperError("read exact Darwin process identity: process is a zombie")
    raw_name = bytes(info.name).split(b"\0", 1)[0] or bytes(info.comm).split(b"\0", 1)[0]
    name = os.fsdecode(raw_name)
    started = f"{info.start_seconds}.{info.start_microseconds:06d}"
    digest = hashlib.sha256(b"\0".join(os.fsencode(argument) for argument in arguments)).hexdigest()
    return name, started, arguments, digest


def process_start_identity_from_process_identity(process_identity: str) -> str:
    required_string({"process_identity": process_identity}, "process_identity", 512)
    fields = process_identity.split(":")
    if len(fields) == 5 and fields[0] == "linux" and fields[1]:
        return f"linux:{fields[1]}"
    return f"process-start:{process_identity}"


def executable_file_identity(path: str | pathlib.Path) -> str:
    info = os.stat(path)
    if not stat.S_ISREG(info.st_mode):
        raise HelperError("Claude executable is not a regular file")
    return f"file:{info.st_dev}:{info.st_ino}"


def executable_content_identity(object_identity: str) -> str:
    fields = object_identity.split(":")
    if len(fields) != 5 or fields[0] != "object":
        raise HelperError("Claude executable object identity is invalid")
    return f"content:{fields[3]}:{fields[4]}"


def descriptor_path(descriptor: int) -> str:
    if platform.system() == "Linux":
        return f"/proc/self/fd/{descriptor}"
    if platform.system() == "Darwin":
        return f"/dev/fd/{descriptor}"
    raise HelperError("descriptor execution is unavailable on this platform")


def materialize_executable(
    descriptor: int, directory: pathlib.Path, destination: pathlib.Path | None = None
) -> pathlib.Path:
    if destination is None:
        output_descriptor, output_path = tempfile.mkstemp(prefix="launcher.", dir=directory)
    else:
        output_path = str(destination)
        output_descriptor = os.open(
            output_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0), 0o600
        )
    try:
        os.lseek(descriptor, 0, os.SEEK_SET)
        while True:
            chunk = os.read(descriptor, 1024 * 1024)
            if not chunk:
                break
            remaining = memoryview(chunk)
            while remaining:
                written = os.write(output_descriptor, remaining)
                if written <= 0:
                    raise HelperError("write verified Claude executable copy failed")
                remaining = remaining[written:]
        os.fsync(output_descriptor)
        os.fchmod(output_descriptor, 0o500)
    finally:
        os.close(output_descriptor)
        os.lseek(descriptor, 0, os.SEEK_SET)
    return pathlib.Path(output_path)


def verified_executable_descriptor(descriptor: int, display_path: str) -> tuple[int, str, str, str]:
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o111 == 0:
            raise HelperError("Claude executable is not one executable regular file")
        digest = hashlib.sha256()
        total = 0
        while True:
            chunk = os.read(descriptor, 1024 * 1024)
            if not chunk:
                break
            total += len(chunk)
            if total > 256 * 1024 * 1024:
                raise HelperError("Claude executable exceeds the verification size limit")
            digest.update(chunk)
        final_info = os.fstat(descriptor)
        if (
            final_info.st_dev, final_info.st_ino, final_info.st_size,
            final_info.st_mtime_ns, final_info.st_ctime_ns,
        ) != (
            info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns,
        ):
            raise HelperError("Claude executable changed during verification")
        os.lseek(descriptor, 0, os.SEEK_SET)
        os.set_inheritable(descriptor, True)
        practical_identity = f"file:{info.st_dev}:{info.st_ino}"
        object_identity = f"object:{info.st_dev}:{info.st_ino}:{info.st_size}:{digest.hexdigest()}"
        return descriptor, display_path, practical_identity, object_identity
    except BaseException:
        os.close(descriptor)
        raise


def open_verified_executable(path: str | pathlib.Path) -> tuple[int, str, str, str]:
    resolved = str(pathlib.Path(path).resolve(strict=True))
    descriptor = os.open(resolved, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
    return verified_executable_descriptor(descriptor, resolved)


def open_exact_verified_executable(
    path: str | pathlib.Path, *, follow_kernel_link: bool = False
) -> tuple[int, str, str, str]:
    exact = os.fspath(path)
    flags = os.O_RDONLY
    if not follow_kernel_link:
        flags |= getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(exact, flags)
    return verified_executable_descriptor(descriptor, exact)


def darwin_fchflags(descriptor: int, flags: int) -> None:
    libc = ctypes.CDLL("/usr/lib/libSystem.B.dylib", use_errno=True)
    if libc.fchflags(descriptor, flags) != 0:
        error = ctypes.get_errno()
        raise OSError(error, os.strerror(error))


def seal_darwin_launch_container(container_descriptor: int, executable_descriptor: int) -> None:
    immutable = getattr(stat, "UF_IMMUTABLE", 0x00000002)
    os.fchmod(container_descriptor, 0o500)
    try:
        darwin_fchflags(executable_descriptor, immutable)
        darwin_fchflags(container_descriptor, immutable)
    except BaseException:
        with contextlib.suppress(OSError):
            darwin_fchflags(container_descriptor, 0)
        with contextlib.suppress(OSError):
            darwin_fchflags(executable_descriptor, 0)
        with contextlib.suppress(OSError):
            os.fchmod(container_descriptor, 0o700)
        raise


def restore_darwin_launch_container(container_descriptor: int, executable_descriptor: int) -> None:
    errors: list[OSError] = []
    for descriptor in (container_descriptor, executable_descriptor):
        try:
            darwin_fchflags(descriptor, 0)
        except OSError as error:
            errors.append(error)
    try:
        os.fchmod(container_descriptor, 0o700)
    except OSError as error:
        errors.append(error)
    if errors:
        raise errors[0]


def directory_identity(info: os.stat_result) -> str:
    return f"directory:{info.st_dev}:{info.st_ino}"


def open_verified_directory(path: str | pathlib.Path, expected_identity: str | None = None) -> tuple[int, str, str]:
    resolved = str(pathlib.Path(path).resolve(strict=True))
    flags = os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(resolved, flags)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISDIR(info.st_mode):
            raise HelperError("launch workdir is not one directory object")
        identity = directory_identity(info)
        if expected_identity is not None and identity != expected_identity:
            raise HelperError("launch workdir object changed before transport")
        return descriptor, resolved, identity
    except BaseException:
        os.close(descriptor)
        raise


def exact_canonical_store_path(path: pathlib.Path) -> str:
    supplied = os.fspath(path)
    if not path.is_absolute():
        raise HelperError("legacy receipt store path must be absolute")
    try:
        resolved = os.fspath(path.resolve(strict=True))
    except OSError as error:
        raise HelperError("legacy receipt store is unavailable") from error
    if supplied != resolved:
        raise HelperError("legacy receipt store path must be exact, canonical, and free of symlinks")
    return resolved


def verify_registered_store_object(path: str, expected_identity: str | None) -> None:
    if expected_identity is None:
        return
    try:
        info = os.stat(path, follow_symlinks=False)
    except OSError as error:
        raise HelperError("registered legacy store object is unavailable") from error
    if (
        not stat.S_ISDIR(info.st_mode)
        or stat.S_IMODE(info.st_mode) != 0o700
        or info.st_uid != os.geteuid()
        or directory_identity(info) != expected_identity
    ):
        raise HelperError("registered legacy store object changed")


def load_registered_receipt_store(
    owner: ReceiptStore,
    path: str,
    expected_identity: str | None,
    require_exists: bool = True,
    acquire_lock: bool = True,
) -> dict[str, Any]:
    if expected_identity is None:
        return owner.load_store(require_exists=require_exists)
    verify_registered_store_object(path, expected_identity)
    with locked_owner_private_store(pathlib.Path(path), acquire_lock=acquire_lock) as (identity, raw):
        if identity != expected_identity:
            raise HelperError("registered legacy store object changed")
        return decode_receipt_store(raw)


@contextlib.contextmanager
def locked_owner_private_store(path: pathlib.Path, acquire_lock: bool = True) -> Iterator[tuple[str, bytes]]:
    try:
        directory_descriptor = os.open(
            path, os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0)
        )
    except OSError as error:
        raise HelperError("legacy receipt store is unavailable") from error
    try:
        info = os.fstat(directory_descriptor)
        if (
            not stat.S_ISDIR(info.st_mode)
            or stat.S_IMODE(info.st_mode) != 0o700
            or info.st_uid != os.geteuid()
        ):
            raise HelperError("legacy receipt store must be one owner-private directory")
        lock_descriptor = open_owner_private_file_at(directory_descriptor, "experimental.lock", "legacy receipt lock")
        try:
            if acquire_lock:
                fcntl.flock(lock_descriptor, fcntl.LOCK_EX)
            receipt_descriptor = open_owner_private_file_at(directory_descriptor, "receipts.json", "legacy receipt store")
            try:
                raw = read_stable_descriptor(receipt_descriptor, MAX_STORE_BYTES, "legacy receipt store")
                if not raw:
                    raise HelperError("legacy receipt store is empty")
                yield directory_identity(info), raw
                current_directory = os.stat(path, follow_symlinks=False)
                current_receipt = os.stat(path / "receipts.json", follow_symlinks=False)
                if not os.path.samestat(info, current_directory) or not os.path.samestat(os.fstat(receipt_descriptor), current_receipt):
                    raise HelperError("legacy receipt store object changed before registration")
            finally:
                os.close(receipt_descriptor)
        finally:
            os.close(lock_descriptor)
    except BaseException:
        raise
    finally:
        os.close(directory_descriptor)


def open_owner_private_file_at(directory_descriptor: int, name: str, label: str) -> int:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    try:
        descriptor = os.open(name, flags, dir_fd=directory_descriptor)
    except OSError as error:
        raise HelperError(f"{label} is unavailable or unsafe") from error
    info = os.fstat(descriptor)
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_uid != os.geteuid():
        os.close(descriptor)
        raise HelperError(f"{label} must be one owner-only regular file")
    return descriptor


def read_stable_descriptor(descriptor: int, limit: int, label: str) -> bytes:
    before = os.fstat(descriptor)
    chunks: list[bytes] = []
    remaining = limit + 1
    while remaining:
        chunk = os.read(descriptor, remaining)
        if not chunk:
            break
        chunks.append(chunk)
        remaining -= len(chunk)
    raw = b"".join(chunks)
    after = os.fstat(descriptor)
    if len(raw) > limit:
        raise HelperError(f"{label} exceeds the experimental size limit")
    if (
        before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns
    ) != (
        after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns
    ):
        raise HelperError(f"{label} changed during descriptor read")
    return raw


def process_executable_identity(pid: int) -> str:
    try:
        return executable_file_identity(process_executable_path(pid))
    except (OSError, RuntimeError) as error:
        raise HelperError("read exact process executable identity") from error


def process_executable_path(pid: int) -> pathlib.Path:
    system = platform.system()
    if system == "Linux":
        return LINUX_PROC_ROOT / str(pid) / "exe"
    if system != "Darwin":
        raise HelperError(f"process executable identity is unavailable on {system}")
    libproc = ctypes.CDLL("/usr/lib/libproc.dylib", use_errno=True)
    buffer = ctypes.create_string_buffer(4096)
    written = libproc.proc_pidpath(pid, buffer, len(buffer))
    if written <= 0:
        raise HelperError("read exact Darwin process executable identity: proc_pidpath failed")
    return pathlib.Path(os.fsdecode(buffer.raw[:written]))


def normalized_argv_digest(arguments: list[str]) -> str:
    return hashlib.sha256(b"\0".join(os.fsencode(argument) for argument in arguments)).hexdigest()


def launcher_argv0_digest(argument: str) -> str:
    return hashlib.sha256(os.fsencode(argument)).hexdigest()


def normalized_claude_arguments(
    system: str,
    process_name: str,
    process_args: list[str],
    process_executable: pathlib.Path | None = None,
) -> tuple[str, list[str]]:
    private_process = process_name == "verified-claude"
    private_executable = process_executable is not None and process_executable.name == "verified-claude"
    if private_process or private_executable:
        if (
            system == "Darwin"
            and private_process
            and private_executable
            and process_args
            and pathlib.Path(process_args[0]).is_absolute()
        ):
            return "darwin_transport", process_args[1:]
        raise HelperError("pane process is not a supported Claude executable form")
    if process_args and pathlib.Path(process_args[0]).name == "claude":
        return "direct", process_args[1:]
    if system == "Linux" and process_name in {"node", "bun"} and len(process_args) > 2:
        script = pathlib.PurePath(process_args[1])
        descriptor_script = len(script.parts) >= 3 and script.parts[-2] == "fd" and script.name.isdigit()
        if descriptor_script or (
            script.name == "cli.js" and "@anthropic-ai" in script.parts and "claude-code" in script.parts
        ):
            return "node", process_args[2:]
    raise HelperError("pane process is not a supported Claude executable form")


def is_claude_process(system: str, process_name: str, process_args: list[str], session_position: int) -> bool:
    try:
        executable_form, _ = normalized_claude_arguments(system, process_name, process_args)
    except HelperError:
        return False
    return executable_form == "direct" or session_position > 1


def inspect_amp_target(pane_id: str, origin_thread: str) -> dict[str, Any]:
    if not pane_id.startswith("%") or len(pane_id) < 2:
        raise HelperError("Amp target requires an exact tmux pane ID")
    required_string({"origin_thread": origin_thread}, "origin_thread", 256)
    fields = run_command(
        [
            "tmux",
            "display-message",
            "-p",
            "-t",
            pane_id,
            "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_current_path}\t#{pane_current_command}",
        ]
    ).split("\t")
    if len(fields) != 7 or fields[3] != pane_id or fields[6] != "amp":
        raise HelperError("target is not one exact live Amp pane")
    try:
        pane_pid = int(fields[4])
    except ValueError as error:
        raise HelperError("Amp target returned invalid tmux process identity") from error
    workdir = str(pathlib.Path(fields[5]).resolve(strict=True))
    process_name, process_identity, process_args, process_command_digest = exact_process_identity(pane_pid)
    if (
        process_name != "amp"
        or len(process_args) != 4
        or pathlib.Path(process_args[0]).name != "amp"
        or process_args[1:3] != ["threads", "continue"]
        or process_args[3] != origin_thread
    ):
        raise HelperError("target process is not the expected origin Amp thread")
    return {
        "origin_thread": origin_thread,
        "session": fields[0],
        "window": fields[1],
        "window_id": fields[2],
        "pane_id": fields[3],
        "pane_pid": pane_pid,
        "workdir": workdir,
        "current_command": fields[6],
        "process_name": process_name,
        "process_identity": process_identity,
        "process_command_digest": process_command_digest,
    }


def validate_amp_target(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("notification target must be an object")
    fields = {
        "origin_thread",
        "session",
        "window",
        "window_id",
        "pane_id",
        "pane_pid",
        "workdir",
        "current_command",
        "process_name",
        "process_identity",
        "process_command_digest",
    }
    reject_unknown(value, fields, "notification target")
    for key in fields - {"pane_pid"}:
        required_string(value, key, 2048 if key == "workdir" else 256)
    if not isinstance(value.get("pane_pid"), int) or value["pane_pid"] <= 0:
        raise HelperError("notification target pane_pid is invalid")
    return copy.deepcopy(value)


def inspect_claude_identity(
    pane_id: str,
    claude_session_id: str,
    expected_argv_digest: str,
    expected_launcher_identity: str,
    expected_executable_object_identity: str,
    expected_process_executable_identity: str | None = None,
    expected_process_executable_object_identity: str | None = None,
    expected_launcher_argv0_digest: str | None = None,
    tmux_fields: list[str] | None = None,
    include_process_executable_object_identity: bool = False,
    allow_missing_launcher_argv0_digest: bool = False,
) -> dict[str, Any]:
    if not pane_id.startswith("%") or len(pane_id) < 2:
        raise HelperError("Claude identity requires an exact tmux pane ID")
    fields = tmux_fields
    if fields is None:
        fields = run_command(
            [
                "tmux",
                "display-message",
                "-p",
                "-t",
                pane_id,
                "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_start_command}",
            ]
        ).split("\t")
    if len(fields) != 8 or fields[3] != pane_id or not fields[6] or not fields[7]:
        raise HelperError("Claude pane is missing exact live tmux identity")
    try:
        pane_pid = int(fields[4])
    except ValueError as error:
        raise HelperError("Claude pane returned invalid process identity") from error
    workdir = str(pathlib.Path(fields[5]).resolve(strict=True))
    start_command = fields[7]
    if start_command.startswith('"') and start_command.endswith('"'):
        try:
            decoded = ast.literal_eval(start_command)
        except (SyntaxError, ValueError) as error:
            raise HelperError("Claude pane launch command has invalid tmux quoting") from error
        if not isinstance(decoded, str):
            raise HelperError("Claude pane launch command has invalid tmux quoting")
        start_command = decoded
    process_name, process_identity, process_args, process_command_digest = exact_process_identity(pane_pid)
    process_executable = process_executable_path(pane_pid)
    if platform.system() == "Darwin":
        try:
            executable_identity = executable_file_identity(process_executable)
        except OSError as error:
            raise HelperError("read exact process executable identity") from error
    else:
        executable_identity = process_executable_identity(pane_pid)
    executable_form, normalized_arguments = normalized_claude_arguments(
        platform.system(), process_name, process_args, process_executable
    )
    session_positions = [index for index, argument in enumerate(process_args) if argument == "--session-id"]
    if (
        len(session_positions) != 1
        or session_positions[0] + 1 >= len(process_args)
        or process_args[session_positions[0] + 1] != claude_session_id
    ):
        raise HelperError("pane process is not the expected Claude session")
    if normalized_argv_digest(normalized_arguments) != expected_argv_digest:
        raise HelperError("Claude process argv does not match immutable launch intent")
    process_executable_object_identity: str | None = None
    if executable_form == "direct":
        try:
            live_descriptor, _, _, live_object_identity = open_exact_verified_executable(
                process_executable, follow_kernel_link=platform.system() == "Linux"
            )
        except OSError as error:
            raise HelperError("Claude process executable content is unavailable") from error
        os.close(live_descriptor)
        if (
            platform.system() == "Linux"
            and live_object_identity != expected_executable_object_identity
        ) or (
            platform.system() == "Darwin"
            and executable_content_identity(live_object_identity)
            != executable_content_identity(expected_executable_object_identity)
        ):
            raise HelperError("Claude process executable content does not match immutable launch intent")
        process_executable_object_identity = live_object_identity
        launcher_identity = expected_launcher_identity
    elif executable_form == "darwin_transport":
        if expected_process_executable_object_identity is None:
            raise HelperError("Claude transport requires the planned private executable object identity")
        if (
            expected_launcher_argv0_digest is None and not allow_missing_launcher_argv0_digest
        ) or (
            expected_launcher_argv0_digest is not None
            and launcher_argv0_digest(process_args[0]) != expected_launcher_argv0_digest
        ):
            raise HelperError("Claude transport launcher path does not match immutable launch intent")
        try:
            process_descriptor, _, process_launcher_identity, process_executable_object_identity = (
                open_exact_verified_executable(process_executable)
            )
            os.close(process_descriptor)
            launcher_descriptor, _, launcher_identity, launcher_object_identity = (
                open_exact_verified_executable(process_args[0])
            )
            os.close(launcher_descriptor)
        except (OSError, RuntimeError) as error:
            raise HelperError("Claude transport executable identity is unavailable") from error
        if (
            process_launcher_identity != executable_identity
            or process_executable_object_identity != expected_process_executable_object_identity
            or executable_content_identity(process_executable_object_identity)
            != executable_content_identity(expected_executable_object_identity)
        ):
            raise HelperError("Claude transport executable does not match the planned private copy")
        if launcher_object_identity != expected_executable_object_identity:
            raise HelperError("Claude transport launcher content does not match immutable launch intent")
    else:
        try:
            script = pathlib.PurePath(process_args[1])
            if platform.system() == "Linux" and len(script.parts) >= 3 and script.parts[-2] == "fd" and script.name.isdigit():
                launcher_path: str | pathlib.Path = LINUX_PROC_ROOT / str(pane_pid) / "fd" / script.name
            else:
                launcher_path = process_args[1]
            live_descriptor, _, launcher_identity, live_object_identity = open_exact_verified_executable(
                launcher_path, follow_kernel_link=platform.system() == "Linux"
            )
            os.close(live_descriptor)
        except (OSError, RuntimeError) as error:
            raise HelperError("Claude launcher identity is unavailable") from error
        if live_object_identity != expected_executable_object_identity:
            raise HelperError("Claude launcher content does not match immutable launch intent")
    if process_executable_object_identity is None and (
        include_process_executable_object_identity
        or expected_process_executable_object_identity is not None
    ):
        try:
            process_descriptor, _, _, process_executable_object_identity = open_exact_verified_executable(
                process_executable, follow_kernel_link=platform.system() == "Linux"
            )
            os.close(process_descriptor)
        except (OSError, RuntimeError) as error:
            raise HelperError("Claude process executable object identity is unavailable") from error
    if launcher_identity != expected_launcher_identity:
        raise HelperError("Claude process launcher does not match immutable launch intent")
    if expected_process_executable_identity is not None and executable_identity != expected_process_executable_identity:
        raise HelperError("Claude process executable changed after verified startup")
    if platform.system() == "Darwin":
        final_name, final_process_identity, final_args, final_command_digest = exact_process_identity(pane_pid)
        final_process_executable = process_executable_path(pane_pid)
        try:
            final_executable_identity = executable_file_identity(final_process_executable)
        except OSError as error:
            raise HelperError("read exact process executable identity") from error
        if (
            final_name != process_name
            or final_process_identity != process_identity
            or final_args != process_args
            or final_command_digest != process_command_digest
            or final_process_executable != process_executable
            or final_executable_identity != executable_identity
        ):
            raise HelperError("Darwin Claude process changed during identity inspection")
    identity = {
        "claude_session_id": claude_session_id,
        "session": fields[0],
        "window": fields[1],
        "window_id": fields[2],
        "pane_id": fields[3],
        "pane_pid": pane_pid,
        "workdir": workdir,
        "current_command": fields[6],
        "process_name": process_name,
        "process_identity": process_identity,
        "process_executable_identity": executable_identity,
        "normalized_argv_digest": expected_argv_digest,
        "process_command_digest": process_command_digest,
        "launch_command_digest": hashlib.sha256(start_command.encode()).hexdigest(),
    }
    if (
        include_process_executable_object_identity
        or expected_process_executable_object_identity is not None
    ):
        identity["process_executable_object_identity"] = process_executable_object_identity
    return identity


def wait_for_claude_startup(
    pane_id: str,
    claude_session_id: str,
    expected_argv_digest: str,
    expected_launcher_identity: str,
    expected_executable_object_identity: str,
    expected_process_executable_object_identity: str | None = None,
    expected_launcher_argv0_digest: str | None = None,
) -> dict[str, Any]:
    deadline = time.monotonic() + STARTUP_TIMEOUT_SECONDS
    last_error = "startup identity unavailable"
    candidate: dict[str, Any] | None = None
    candidate_since = 0.0
    while time.monotonic() < deadline:
        try:
            current = inspect_claude_identity(
                pane_id,
                claude_session_id,
                expected_argv_digest,
                expected_launcher_identity,
                expected_executable_object_identity,
                expected_process_executable_object_identity=expected_process_executable_object_identity,
                expected_launcher_argv0_digest=expected_launcher_argv0_digest,
            )
            if current == candidate:
                if time.monotonic() - candidate_since >= STARTUP_STABILITY_SECONDS:
                    return current
            else:
                candidate = current
                candidate_since = time.monotonic()
        except HelperError as error:
            last_error = str(error)
            candidate = None
            candidate_since = 0.0
        time.sleep(STARTUP_POLL_SECONDS)
    raise HelperError(f"Claude startup was not verified before timeout: {last_error}")


def validate_launch_request(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("launch request must be an object")
    fields = {
        "delegation_id",
        "event_id",
        "workdir",
        "packet_file",
        "tmux_session",
        "tmux_window",
        "claude_session_id",
        "repository",
        "base",
        "workflow",
    }
    mutating_fields = {
        "baseline_branch",
        "writer_owner",
        "integration_owner",
        "coordinator_write_frozen",
        "shared_writable",
        "handoff",
        "capacity_request",
    }
    workflow = value.get("workflow", "read_only")
    if workflow == "mutating":
        fields |= mutating_fields
    else:
        fields |= {"expected_launch_policy_digest", "model"}
    reject_unknown(value, fields, "launch request")
    result: dict[str, Any] = {}
    for key in fields - mutating_fields - {"workflow", "expected_launch_policy_digest", "model"}:
        result[key] = required_string(value, key, 2048 if key in {"workdir", "packet_file"} else 256)
    if workflow not in {"read_only", "mutating"}:
        raise HelperError("launch workflow must be read_only or mutating")
    result["workflow"] = workflow
    if workflow == "read_only":
        model = optional_read_only_model(value)
        if model is not None:
            result["model"] = model
        expected_policy_digest = value.get("expected_launch_policy_digest")
        if (
            not isinstance(expected_policy_digest, str)
            or len(expected_policy_digest) != 64
            or any(character not in "0123456789abcdef" for character in expected_policy_digest)
        ):
            raise HelperError("expected_launch_policy_digest must be a lowercase SHA-256 value")
        result["expected_launch_policy_digest"] = expected_policy_digest
    if result["workflow"] == "mutating":
        for key in ("baseline_branch", "writer_owner", "integration_owner", "handoff"):
            result[key] = required_string(value, key, 256)
        for key in ("coordinator_write_frozen", "shared_writable"):
            if not isinstance(value.get(key), bool):
                raise HelperError(f"{key} must be boolean")
            result[key] = value[key]
        if not isinstance(value.get("capacity_request"), dict):
            raise HelperError("capacity_request must be an object")
        result["capacity_request"] = copy.deepcopy(value["capacity_request"])
    protocol_id(result, "delegation_id")
    protocol_id(result, "event_id")
    protocol_id(result, "claude_session_id")
    return result


def repository_from_remote(remote: str) -> str:
    value = remote.strip()
    if value.startswith("git@github.com:"):
        value = value[len("git@github.com:") :]
    elif "github.com/" in value:
        value = value.split("github.com/", 1)[1]
    if value.endswith(".git"):
        value = value[:-4]
    return value.strip("/")


def preflight_worktree(request: dict[str, str], retain_descriptor: bool = False) -> tuple[str, str, int | None]:
    descriptor, workdir, identity = open_verified_directory(request["workdir"])
    previous = os.open(".", os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fchdir(descriptor)
        git = ["git", "--no-optional-locks", "-C", "."]
        if run_command(git + ["rev-parse", "--show-toplevel"]) != workdir:
            raise HelperError("launch workdir is not the canonical Git worktree root")
        if run_command(git + ["rev-parse", "HEAD"]) != request["base"]:
            raise HelperError("launch worktree HEAD does not match the immutable base")
        git_dir = run_command(git + ["rev-parse", "--git-dir"])
        common_dir = run_command(git + ["rev-parse", "--git-common-dir"])
        if git_dir == common_dir:
            raise HelperError("read-only thinker requires a fresh dedicated linked worktree")
        if run_command(git + ["status", "--porcelain"]):
            raise HelperError("read-only thinker worktree must be clean before launch")
        remote = run_command(git + ["remote", "get-url", "origin"])
        if repository_from_remote(remote) != request["repository"]:
            raise HelperError("launch repository does not match the immutable repository")
        if directory_identity(os.fstat(descriptor)) != identity:
            raise HelperError("launch workdir object changed during validation")
        return workdir, identity, descriptor if retain_descriptor else None
    finally:
        os.fchdir(previous)
        os.close(previous)
        if not retain_descriptor:
            os.close(descriptor)


def private_runtime_paths(store: ReceiptStore, delegation_id: str) -> tuple[pathlib.Path, pathlib.Path]:
    # Delegation IDs are opaque protocol values, not trusted path components.
    runtime_key = hashlib.sha256(delegation_id.encode()).hexdigest()
    runtime = store.state_dir / "runtime" / runtime_key
    return runtime / "mcp.json", runtime / "settings.json"


def private_launch_transport_path(store: ReceiptStore, delegation_id: str) -> pathlib.Path:
    mcp_path, _ = private_runtime_paths(store, delegation_id)
    return mcp_path.with_name("launch.json")


def require_tmux_session(session: str) -> None:
    try:
        run_command(["tmux", "has-session", "-t", "=" + session])
    except HelperError as error:
        raise HelperError("target tmux session does not exist or cannot be verified") from error


def launch_policy(workflow: str, model: str | None = None) -> dict[str, Any]:
    if workflow not in {"read_only", "mutating"}:
        raise HelperError("launch workflow must be read_only or mutating")
    mcp_tool_prefix = "mcp__amux-claude-delegation__"
    mutating = workflow == "mutating"
    built_in_tools = ["Read", "Grep", "Glob", "Bash", "Edit", "Write"] if mutating else ["Read", "Grep", "Glob"]
    mutating_denied = [
        "Agent", "WebFetch", "WebSearch", "Skill",
        "Bash(git push:*)", "Bash(gh:*)", "Bash(git stash:*)", "Bash(git reset:*)",
        "Bash(git clean:*)", "Bash(git worktree:*)", "Bash(git merge:*)", "Bash(git tag:*)",
        "Bash(git branch -d:*)", "Bash(git branch -D:*)",
    ]
    denied_tools = mutating_denied if mutating else ["Bash", "Edit", "Write", "NotebookEdit", "Agent", "WebFetch", "WebSearch", "Skill"]
    policy = {
        "interactive": True,
        "permission_mode": "dontAsk",
        "setting_sources": [],
        "strict_mcp": True,
        "built_in_tools": built_in_tools,
        "allowed_mcp_tools": [mcp_tool_prefix + "submit_report", mcp_tool_prefix + "submit_input_request"],
        "denied_tools": denied_tools,
        "additional_directories": [],
        "automatic_interactive_input": False,
    }
    if mutating:
        policy["workflow"] = "mutating"
        policy["removed_credential_environment"] = ["GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"]
    elif model is not None:
        policy["model"] = model
    return policy


def plan_launch_policy_digest(request: Any) -> dict[str, str]:
    if not isinstance(request, dict):
        raise HelperError("launch policy digest request must be an object")
    reject_unknown(request, {"workflow", "model"}, "launch policy digest request")
    workflow = required_string(request, "workflow", 32)
    if workflow != "read_only":
        raise HelperError("launch policy digest preflight supports only read_only")
    model = optional_read_only_model(request)
    policy = launch_policy(workflow, model)
    result = {
        "workflow": workflow,
        "launch_policy_digest": hashlib.sha256(json.dumps(policy, sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
    }
    if model is not None:
        result["model"] = model
    return result


def expected_launch_policy(request: dict[str, Any]) -> dict[str, Any]:
    policy = launch_policy(request["workflow"], request.get("model"))
    digest = hashlib.sha256(json.dumps(policy, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    if request["workflow"] == "read_only" and request["expected_launch_policy_digest"] != digest:
        raise HelperError("expected launch policy digest does not match selected workflow")
    return policy


def open_owner_private_regular_file(path: pathlib.Path, limit: int, label: str) -> tuple[int, bytes, str]:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    try:
        descriptor = os.open(path, flags)
    except OSError as error:
        raise HelperError(f"{label} is unavailable or unsafe") from error
    try:
        info = os.fstat(descriptor)
        if (
            not stat.S_ISREG(info.st_mode)
            or stat.S_IMODE(info.st_mode) != 0o600
            or info.st_uid != os.geteuid()
        ):
            raise HelperError(f"{label} must be one owner-only regular file")
        chunks = []
        remaining = limit + 1
        while remaining:
            chunk = os.read(descriptor, remaining)
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        raw = b"".join(chunks)
        final_info = os.fstat(descriptor)
        if (
            final_info.st_dev, final_info.st_ino, final_info.st_size,
            final_info.st_mtime_ns, final_info.st_ctime_ns,
        ) != (
            info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns,
        ):
            raise HelperError(f"{label} changed during descriptor read")
        return descriptor, raw, f"file:{info.st_dev}:{info.st_ino}"
    except BaseException:
        os.close(descriptor)
        raise


def read_owner_private_regular_file(path: pathlib.Path, limit: int, label: str) -> tuple[bytes, str]:
    descriptor, raw, identity = open_owner_private_regular_file(path, limit, label)
    os.close(descriptor)
    return raw, identity


def exact_launch_environment(removed_environment: list[str]) -> dict[str, str]:
    environment = dict(os.environ)
    for name in removed_environment:
        environment.pop(name, None)
    for key, value in environment.items():
        if not key or "=" in key or "\x00" in key or "\x00" in value:
            raise HelperError("launch environment contains an unsupported entry")
    return environment


def validate_exec_budget(argv: list[str], environment: dict[str, str], system: str) -> None:
    argument_bytes = [os.fsencode(argument) for argument in argv]
    environment_bytes = [os.fsencode(f"{key}={value}") for key, value in environment.items()]
    try:
        argument_max = os.sysconf("SC_ARG_MAX")
    except (OSError, ValueError) as error:
        raise HelperError("platform process argument budget is unavailable") from error
    if not isinstance(argument_max, int) or argument_max <= EXEC_BUDGET_MARGIN_BYTES:
        raise HelperError("platform process argument budget is unavailable")
    if system == "Linux":
        try:
            string_max = os.sysconf("SC_PAGE_SIZE") * 32
        except (OSError, ValueError) as error:
            raise HelperError("Linux process string budget is unavailable") from error
        if any(len(value) + 1 > string_max for value in argument_bytes + environment_bytes):
            raise HelperError("launch argv or environment exceeds the platform process string limit")
    strings_size = sum(len(value) + 1 for value in argument_bytes + environment_bytes)
    pointers_size = (len(argument_bytes) + len(environment_bytes) + 2) * ctypes.sizeof(ctypes.c_void_p)
    if strings_size + pointers_size + EXEC_BUDGET_MARGIN_BYTES > argument_max:
        raise HelperError("launch argv and environment exceed the conservative platform process budget")


def tmux_environment_snapshot(session: str) -> tuple[list[bytes], str]:
    outputs = []
    for arguments in (
        ["tmux", "show-environment", "-gs"],
        ["tmux", "show-environment", "-s", "-t", "=" + session],
    ):
        try:
            completed = subprocess.run(arguments, capture_output=True, timeout=5, check=False)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise HelperError("target tmux environment is unavailable") from error
        if completed.returncode != 0:
            raise HelperError("target tmux environment is unavailable")
        outputs.append(completed.stdout)
    encoded = [line for output in outputs for line in output.splitlines()]
    digest = hashlib.sha256(outputs[0] + b"\x00" + outputs[1]).hexdigest()
    return encoded, digest


def validate_tmux_exec_budget(session: str, start_command: str, system: str, expected_digest: str | None = None) -> str:
    environment_bytes, digest = tmux_environment_snapshot(session)
    if expected_digest is not None and digest != expected_digest:
        raise HelperError("target tmux environment changed before launch intent")
    shell = run_command(["tmux", "show-options", "-gv", "default-shell"])
    if not shell or "\x00" in shell:
        raise HelperError("target tmux shell is unavailable")
    argument_bytes = [os.fsencode(value) for value in (shell, "-c", start_command)]
    try:
        argument_max = os.sysconf("SC_ARG_MAX")
    except (OSError, ValueError) as error:
        raise HelperError("platform process argument budget is unavailable") from error
    if not isinstance(argument_max, int) or argument_max <= EXEC_BUDGET_MARGIN_BYTES:
        raise HelperError("platform process argument budget is unavailable")
    if system == "Linux":
        try:
            string_max = os.sysconf("SC_PAGE_SIZE") * 32
        except (OSError, ValueError) as error:
            raise HelperError("Linux process string budget is unavailable") from error
        if (
            any(len(value) + 1 > string_max for value in argument_bytes + environment_bytes)
            or sum(len(value) + 1 for value in environment_bytes) > string_max
        ):
            raise HelperError("target tmux environment exceeds the platform process string limit")
    strings_size = sum(len(value) + 1 for value in argument_bytes + environment_bytes)
    pointers_size = (len(argument_bytes) + len(environment_bytes) + 2) * ctypes.sizeof(ctypes.c_void_p)
    if strings_size + pointers_size + EXEC_BUDGET_MARGIN_BYTES > argument_max:
        raise HelperError("target tmux environment exceeds the conservative platform process budget")
    return digest


def launch_components(store: ReceiptStore, request_value: Any) -> dict[str, Any]:
    request = validate_launch_request(request_value)
    policy = expected_launch_policy(request)
    system = platform.system()
    if system not in {"Darwin", "Linux"}:
        raise HelperError(f"experimental Claude launch is unavailable on {system}")
    if request["workflow"] == "mutating" and system != "Darwin":
        raise HelperError("experimental mutating Claude launch remains available only on Darwin")
    if system == "Linux":
        exact_process_identity(os.getpid())
    run_command(["tmux", "-V"])
    require_tmux_session(request["tmux_session"])
    workflow = request["workflow"]
    mutating = workflow == "mutating"
    removed_environment = ["GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"] if mutating else []
    environment = exact_launch_environment(removed_environment)
    decision_digest_value = ""
    if workflow == "mutating":
        prepared = prepare_mutation(
            {
                "workdir": request["workdir"],
                "repository": request["repository"],
                "writer_owner": request["writer_owner"],
                "integration_owner": request["integration_owner"],
                "coordinator_write_frozen": request["coordinator_write_frozen"],
                "shared_writable": request["shared_writable"],
                "handoff": request["handoff"],
            }
        )
        if prepared["baseline_commit"] != request["base"] or prepared["baseline_branch"] != request["baseline_branch"]:
            raise HelperError("mutating launch differs from the prepared immutable baseline")
        capacity_decision = decide_mutating_capacity(request["capacity_request"])
        decision_digest_value = capacity_decision["decision_digest"]
        if capacity_decision.get("may_proceed") is not True or capacity_decision.get("decision") not in {"autonomous_allowed", "explicit_acknowledgement"}:
            raise HelperError("mutating launch capacity decision does not permit proceeding")
        workdir = prepared["workdir"]
        workdir_descriptor, workdir, workdir_identity = open_verified_directory(workdir)
        os.close(workdir_descriptor)
    else:
        workdir, workdir_identity, _ = preflight_worktree(request)
    packet, packet_identity = read_owner_private_regular_file(
        pathlib.Path(request["packet_file"]), MAX_PACKET_BYTES, "launch packet"
    )
    if not packet or len(packet) > MAX_PACKET_BYTES:
        raise HelperError("launch packet must contain 1 to 262144 bytes")
    try:
        packet_text = packet.decode("utf-8")
    except UnicodeDecodeError as error:
        raise HelperError("launch packet must be UTF-8") from error
    if "\x00" in packet_text:
        raise HelperError("launch packet must not contain NUL bytes")
    helper = pathlib.Path(__file__).resolve()
    python = pathlib.Path(sys.executable).resolve()
    claude_candidate = shutil.which("claude", path=environment.get("PATH"))
    if not claude_candidate:
        raise HelperError("Claude Code is unavailable")
    try:
        claude_descriptor, claude, launcher_identity, executable_object_identity = open_verified_executable(
            claude_candidate
        )
    except OSError as error:
        raise HelperError(f"resolve Claude Code executable: {error}") from error
    try:
        run_command([claude, "--version"], environment, claude_descriptor)
        help_text = run_command([claude, "--help"], environment, claude_descriptor)
    finally:
        os.close(claude_descriptor)
    missing_flags = [flag for flag in REQUIRED_CLAUDE_FLAGS if flag not in help_text]
    if missing_flags:
        raise HelperError("Claude Code is missing required flags: " + ", ".join(missing_flags))
    if request.get("model") is not None and not help_exposes_option(help_text, "--model"):
        raise HelperError("Claude Code does not expose explicit model selection")
    mcp_path, settings_path = private_runtime_paths(store, request["delegation_id"])
    built_in_tools = policy["built_in_tools"]
    argv = [
        claude,
        "--session-id",
        request["claude_session_id"],
        "--permission-mode",
        "dontAsk",
        "--setting-sources",
        "",
        "--settings",
        str(settings_path),
        "--strict-mcp-config",
        "--mcp-config",
        str(mcp_path),
        "--tools",
        ",".join(built_in_tools),
        "--allowed-tools",
        ",".join(built_in_tools + policy["allowed_mcp_tools"]),
        "--disallowed-tools",
        ",".join(policy["denied_tools"]),
        "--disable-slash-commands",
        "--no-chrome",
        "--prompt-suggestions",
        "false",
    ]
    if request.get("model") is not None:
        argv.extend(["--model", request["model"]])
    argv.append(packet_text)
    validate_exec_budget(argv, environment, system)
    expected_argv_digest = normalized_argv_digest(argv[1:])
    expected_launcher_argv0_digest = launcher_argv0_digest(argv[0])
    transport_path = private_launch_transport_path(store, request["delegation_id"])
    transport = {
        "argv": argv,
        "environment": environment,
        "expected_argv_digest": expected_argv_digest,
        "expected_launcher_argv0_digest": expected_launcher_argv0_digest,
        "expected_launcher_identity": launcher_identity,
        "expected_executable_object_identity": executable_object_identity,
        "remove_environment": removed_environment,
        "workdir": workdir,
        "workdir_identity": workdir_identity,
        "packet_identity": packet_identity,
        "packet_path": request["packet_file"],
        "packet_digest": hashlib.sha256(packet).hexdigest(),
    }
    if system == "Darwin":
        transport["darwin_executable_path"] = str(transport_path.with_name("verified-claude"))
    transport_bytes = encode_private_json(transport)
    if len(transport_bytes) > MAX_LAUNCH_TRANSPORT_BYTES:
        raise HelperError("launch packet cannot be encoded within the deterministic transport limit")
    transport_digest = hashlib.sha256(transport_bytes).hexdigest()
    _, tmux_environment_digest = tmux_environment_snapshot(request["tmux_session"])
    start_command = "exec " + shlex.join(
        [
            str(python),
            str(helper),
            "--state-dir",
            str(store.state_dir),
            "launch",
            "transport",
            "--delegation-id",
            request["delegation_id"],
            "--transport-sha256",
            transport_digest,
            "--tmux-environment-sha256",
            tmux_environment_digest,
        ]
    )
    if len(start_command.encode()) > MAX_TMUX_COMMAND_BYTES:
        raise HelperError("launch command exceeds the deterministic tmux transport limit")
    validate_tmux_exec_budget(request["tmux_session"], start_command, system, tmux_environment_digest)
    mcp_config = {
        "mcpServers": {
            "amux-claude-delegation": {
                "type": "stdio",
                "command": str(python),
                "args": [str(helper), "--state-dir", str(store.state_dir), "mcp", "serve", "--delegation-id", request["delegation_id"]],
            }
        }
    }
    settings = {"permissions": {"defaultMode": "dontAsk", "additionalDirectories": []}, "disableAllHooks": True}
    return {
        "request": request,
        "workdir": workdir,
        "packet_digest": hashlib.sha256(packet).hexdigest(),
        "packet_identity": packet_identity,
        "launch_policy_digest": hashlib.sha256(json.dumps(policy, sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
        "launch_command_digest": hashlib.sha256(start_command.encode()).hexdigest(),
        "start_command": start_command,
        "mcp_path": mcp_path,
        "settings_path": settings_path,
        "transport_path": transport_path,
        "mcp_config": mcp_config,
        "settings": settings,
        "transport": transport,
        "transport_bytes": transport_bytes,
        "expected_argv_digest": expected_argv_digest,
        "expected_launcher_argv0_digest": expected_launcher_argv0_digest,
        "expected_launcher_identity": launcher_identity,
        "expected_executable_object_identity": executable_object_identity,
        "workdir_identity": workdir_identity,
        "tmux_environment_digest": tmux_environment_digest,
        "workflow": workflow,
        "capacity_decision_digest": decision_digest_value,
        "capacity_decision": capacity_decision if mutating else None,
    }


def encode_private_json(value: Any) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def write_private_bytes(path: pathlib.Path, payload: bytes) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(path.parent, 0o700)
    descriptor, temporary = tempfile.mkstemp(prefix=path.name + ".tmp.", dir=path.parent)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "wb") as output:
            output.write(payload)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
        directory = os.open(path.parent, os.O_RDONLY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        try:
            os.unlink(temporary)
        except FileNotFoundError:
            pass


def write_private_json(path: pathlib.Path, value: Any) -> None:
    write_private_bytes(path, encode_private_json(value))


def execute_launch_transport(
    store: ReceiptStore, delegation_id: str, expected_digest: str, tmux_environment_digest: str
) -> None:
    with store.launch_gate(delegation_id):
        require_launch_transport_allowed(store, delegation_id)
        execute_launch_transport_locked(store, delegation_id, expected_digest, tmux_environment_digest)


def execute_launch_transport_locked(
    store: ReceiptStore, delegation_id: str, expected_digest: str, tmux_environment_digest: str
) -> None:
    protocol_id({"delegation_id": delegation_id}, "delegation_id")
    for value, label in (
        (expected_digest, "transport_sha256"),
        (tmux_environment_digest, "tmux_environment_sha256"),
    ):
        if len(value) != 64 or any(character not in "0123456789abcdef" for character in value):
            raise HelperError(f"{label} must be a lowercase SHA-256 value")
    path = private_launch_transport_path(store, delegation_id)
    raw, _ = read_owner_private_regular_file(path, MAX_LAUNCH_TRANSPORT_BYTES, "private launch transport")
    if not raw or len(raw) > MAX_LAUNCH_TRANSPORT_BYTES:
        raise HelperError("private launch transport has an invalid size")
    if hashlib.sha256(raw).hexdigest() != expected_digest:
        raise HelperError("private launch transport digest does not match launch command")
    try:
        transport = json.loads(raw)
    except json.JSONDecodeError as error:
        raise HelperError("private launch transport is invalid JSON") from error
    if not isinstance(transport, dict):
        raise HelperError("private launch transport must be an object")
    reject_unknown(
        transport,
        {
            "argv", "environment", "expected_argv_digest", "expected_launcher_argv0_digest",
            "expected_launcher_identity",
            "expected_executable_object_identity", "remove_environment", "workdir", "workdir_identity",
            "packet_identity", "packet_path", "packet_digest", "darwin_executable_path",
        },
        "private launch transport",
    )
    argv = transport.get("argv")
    if (
        not isinstance(argv, list)
        or not argv
        or len(argv) > 64
        or any(not isinstance(argument, str) or "\x00" in argument for argument in argv)
        or not pathlib.Path(argv[0]).is_absolute()
    ):
        raise HelperError("private launch transport argv is invalid")
    removed_environment = transport.get("remove_environment")
    if removed_environment not in ([], ["GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"]):
        raise HelperError("private launch transport environment policy is invalid")
    environment = transport.get("environment")
    if (
        not isinstance(environment, dict)
        or any(not isinstance(key, str) or not isinstance(value, str) for key, value in environment.items())
        or any(name in environment for name in removed_environment)
    ):
        raise HelperError("private launch transport environment is invalid")
    expected_argv_digest = transport.get("expected_argv_digest")
    if expected_argv_digest != normalized_argv_digest(argv[1:]):
        raise HelperError("private launch transport argv digest is invalid")
    if transport.get("expected_launcher_argv0_digest") != launcher_argv0_digest(argv[0]):
        raise HelperError("private launch transport launcher path digest is invalid")
    packet_path = transport.get("packet_path")
    packet_identity = transport.get("packet_identity")
    packet_digest = transport.get("packet_digest")
    if not isinstance(packet_path, str) or not isinstance(packet_identity, str) or not isinstance(packet_digest, str):
        raise HelperError("private launch transport packet identity is invalid")
    transported_packet, transported_packet_identity = read_owner_private_regular_file(
        pathlib.Path(packet_path), MAX_PACKET_BYTES, "launch packet"
    )
    if (
        transported_packet_identity != packet_identity
        or hashlib.sha256(transported_packet).hexdigest() != packet_digest
    ):
        raise HelperError("launch packet object changed before transport")
    try:
        executable_descriptor, _, launcher_identity, executable_object_identity = (
            open_verified_executable(argv[0])
        )
    except OSError as error:
        raise HelperError("private launch transport executable identity is unavailable") from error
    if (
        transport.get("expected_launcher_identity") != launcher_identity
        or transport.get("expected_executable_object_identity") != executable_object_identity
    ):
        os.close(executable_descriptor)
        raise HelperError("private launch transport executable object changed")
    validate_exec_budget(argv, environment, platform.system())
    workdir_value = transport.get("workdir")
    workdir_identity = transport.get("workdir_identity")
    if not isinstance(workdir_value, str) or not workdir_value or not isinstance(workdir_identity, str):
        os.close(executable_descriptor)
        raise HelperError("private launch transport workdir is invalid")
    try:
        workdir_descriptor, _, _ = open_verified_directory(workdir_value, workdir_identity)
        os.fchdir(workdir_descriptor)
        os.close(workdir_descriptor)
    except OSError as error:
        os.close(executable_descriptor)
        raise HelperError("private launch transport workdir is unavailable") from error
    sealed_executable_descriptor: int | None = None
    try:
        if platform.system() == "Darwin":
            sealed_executable_value = transport.get("darwin_executable_path")
            sealed_executable = path.with_name("verified-claude")
            if sealed_executable_value != str(sealed_executable):
                raise HelperError("private launch transport Darwin executable route is invalid")
            sealed_executable_descriptor, _, _, sealed_object_identity = open_exact_verified_executable(
                sealed_executable
            )
            if (
                executable_content_identity(sealed_object_identity)
                != executable_content_identity(executable_object_identity)
            ):
                raise HelperError("verified Claude executable copy changed before execution")
            immutable = getattr(stat, "UF_IMMUTABLE", 0x00000002)
            container_descriptor = os.open(
                path.parent,
                os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0),
            )
            try:
                if (
                    os.fstat(sealed_executable_descriptor).st_flags & immutable == 0
                    or os.fstat(container_descriptor).st_flags & immutable == 0
                ):
                    raise HelperError("verified Claude executable container is not immutable")
            finally:
                os.close(container_descriptor)
            os.set_inheritable(sealed_executable_descriptor, False)
            os.close(executable_descriptor)
            executable_descriptor = -1
            execution_path = str(sealed_executable)
        else:
            if transport.get("darwin_executable_path") is not None:
                raise HelperError("private launch transport Darwin executable route is invalid")
            execution_path = descriptor_path(executable_descriptor)
        execute_authorized_launch_transport(
            store, delegation_id, execution_path, argv, environment
        )
    except OSError as error:
        raise HelperError("private launch transport could not execute Claude") from error
    finally:
        if sealed_executable_descriptor is not None:
            os.close(sealed_executable_descriptor)
        if executable_descriptor >= 0:
            os.close(executable_descriptor)


def plan_launch(store: ReceiptStore, request: Any) -> dict[str, Any]:
    components = launch_components(store, request)
    result = {
        "packet_digest": components["packet_digest"],
        "launch_policy_digest": components["launch_policy_digest"],
        "launch_command_digest": components["launch_command_digest"],
        "expected_argv_digest": components["expected_argv_digest"],
        "expected_launcher_argv0_digest": components["expected_launcher_argv0_digest"],
        "expected_launcher_identity": components["expected_launcher_identity"],
        "capabilities": {
            "initial_interactive_input": "supported",
            "semantic_submission": "supported",
            "automatic_interactive_input": "unavailable",
            "managed_policy_runtime": "untested",
            "strict_mcp_runtime": "untested",
            "read_confinement_runtime": "untested",
        },
    }
    if components["workflow"] == "mutating":
        result["workflow"] = "mutating"
        result["capacity_decision"] = components["capacity_decision"]
        result["capabilities"].update({"writer_authority": "exclusive", "handoff": "one_clean_local_commit"})
    elif components["request"].get("model") is not None:
        result["model"] = components["request"]["model"]
    return result


def execute_launch(store: ReceiptStore, request: Any) -> dict[str, Any]:
    request_data = validate_launch_request(request)
    expected_launch_policy(request_data)
    request_digest = hashlib.sha256(json.dumps(request_data, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    delegation_id = request_data["delegation_id"]
    event_id = request_data["event_id"]
    result_id = internal_event_id("launch-result", event_id)
    receipt = store.find(store.load_store(), delegation_id)
    if receipt["binding"].get("model") != request_data.get("model"):
        raise HelperError("model selection does not match immutable receipt binding")
    with store.mutation_lock():
        receipt = store.find(store.load_store(), delegation_id)
        require_receipt_mutable(receipt)
        if receipt["binding"].get("model") != request_data.get("model"):
            raise HelperError("model selection does not match immutable receipt binding")
        replay = find_event(receipt, event_id)
        if replay is not None:
            if (
                replay.get("kind") != "launch_intent"
                or replay.get("workflow", "read_only") != request_data["workflow"]
                or (replay.get("request_digest") is not None and replay.get("request_digest") != request_digest)
                or (request_data["workflow"] == "mutating" and replay.get("request_digest") is None)
                or replay.get("claude_session_id") != request_data["claude_session_id"]
                or replay.get("tmux_session") != request_data["tmux_session"]
                or replay.get("tmux_window") != request_data["tmux_window"]
            ):
                raise HelperError("event ID is already bound to a conflicting event")
            result = find_event(receipt, result_id)
            if result is None:
                raise HelperError("launch outcome is indeterminate; recover the exact tmux identity without relaunching")
            if result.get("kind") != "launch_completed" or result.get("operation_event_id") != event_id:
                raise HelperError("launch result event conflicts with an existing event")
            return {"outcome": "duplicate", **tmux_launch_identity(result["identity"])}
        if any(event.get("kind") == "launch_intent" for event in receipt["events"]):
            raise HelperError("receipt already has a different launch operation")

    components = launch_components(store, request_data)
    intent = {
        "event_id": event_id,
        "kind": "launch_intent",
        "workflow": request_data["workflow"],
        "request_digest": request_digest,
        "claude_session_id": request_data["claude_session_id"],
        "tmux_session": request_data["tmux_session"],
        "tmux_window": request_data["tmux_window"],
        "packet_digest": components["packet_digest"],
        "launch_policy_digest": components["launch_policy_digest"],
        "launch_command_digest": components["launch_command_digest"],
        "expected_argv_digest": components["expected_argv_digest"],
        "expected_launcher_argv0_digest": components["expected_launcher_argv0_digest"],
        "expected_launcher_identity": components["expected_launcher_identity"],
        "expected_executable_object_identity": components["expected_executable_object_identity"],
        "packet_identity": components["packet_identity"],
        "workdir_identity": components["workdir_identity"],
    }
    if request_data.get("model") is not None:
        intent["model"] = request_data["model"]
    with store.mutation_lock():
        receipt_store = store.load_store()
        receipt = store.find(receipt_store, delegation_id)
        require_receipt_mutable(receipt)
        if receipt["binding"].get("model") != request_data.get("model"):
            raise HelperError("model selection does not match immutable receipt binding")
        for key in ("packet_digest", "launch_policy_digest", "launch_command_digest"):
            if receipt["binding"][key] != components[key]:
                raise HelperError(f"launch {key} does not match immutable receipt binding")
        expected_role = "mutating_delegate" if components["workflow"] == "mutating" else "thinker"
        if receipt["binding"]["producer_role"] != expected_role:
            raise HelperError("launch workflow does not match immutable receipt authority")
        if components["workflow"] == "mutating" and receipt["binding"].get("capacity_decision_digest") != components["capacity_decision_digest"]:
            raise HelperError("launch capacity decision does not match immutable receipt binding")
        replay = find_event(receipt, event_id)
        if replay is not None:
            raise HelperError("launch event appeared concurrently; retry the exact request")
        if any(event.get("kind") == "launch_intent" for event in receipt["events"]):
            raise HelperError("receipt acquired a different launch operation concurrently")

        held_descriptors: list[int] = []
        require_tmux_session(request_data["tmux_session"])
        if components["workflow"] == "mutating":
            revalidate_mutating_launch_lease(receipt_store, receipt, request_data)
            held_workdir_descriptor, final_workdir, final_workdir_identity = open_verified_directory(
                request_data["workdir"]
            )
            held_descriptors.append(held_workdir_descriptor)
            if final_workdir != components["workdir"] or final_workdir_identity != components["workdir_identity"]:
                raise HelperError("mutating launch workdir object changed before intent")
        else:
            final_workdir, final_workdir_identity, held_workdir_descriptor = preflight_worktree(
                request_data, retain_descriptor=True
            )
            assert held_workdir_descriptor is not None
            held_descriptors.append(held_workdir_descriptor)
            if final_workdir != components["workdir"] or final_workdir_identity != components["workdir_identity"]:
                raise HelperError("read-only launch worktree changed before intent")
        held_packet_descriptor, final_packet, final_packet_identity = open_owner_private_regular_file(
            pathlib.Path(request_data["packet_file"]), MAX_PACKET_BYTES, "launch packet"
        )
        held_descriptors.append(held_packet_descriptor)
        if (
            hashlib.sha256(final_packet).hexdigest() != components["packet_digest"]
            or final_packet_identity != components["packet_identity"]
        ):
            raise HelperError("launch packet object changed before intent")
        held_executable_descriptor, _, final_launcher_identity, final_executable_object_identity = (
            open_verified_executable(components["transport"]["argv"][0])
        )
        held_descriptors.append(held_executable_descriptor)
        if (
            final_launcher_identity != components["expected_launcher_identity"]
            or final_executable_object_identity != components["expected_executable_object_identity"]
        ):
            raise HelperError("Claude executable object changed before intent")
        validate_exec_budget(
            components["transport"]["argv"], components["transport"]["environment"], platform.system()
        )
        validate_tmux_exec_budget(
            request_data["tmux_session"],
            components["start_command"],
            platform.system(),
            components["tmux_environment_digest"],
        )
        if platform.system() == "Darwin" and os.path.lexists(components["transport_path"].parent):
            raise HelperError("private delegation runtime already exists before launch intent")
        intent["at"] = utc_now()
        receipt["events"].append(intent)
        receipt["updated_at"] = intent["at"]
        store.commit(receipt_store)

        runtime_root = components["mcp_path"].parent.parent
        runtime_root.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(runtime_root, 0o700)
        write_private_json(components["mcp_path"], components["mcp_config"])
        write_private_json(components["settings_path"], components["settings"])
        write_private_bytes(components["transport_path"], components["transport_bytes"])
        darwin_container_descriptor: int | None = None
        darwin_executable_descriptor: int | None = None
        darwin_executable_object_identity: str | None = None
        if platform.system() == "Darwin":
            darwin_executable = pathlib.Path(components["transport"]["darwin_executable_path"])
            materialize_executable(
                held_executable_descriptor, darwin_executable.parent, darwin_executable
            )
            (
                darwin_executable_descriptor,
                _,
                _,
                darwin_executable_object_identity,
            ) = open_exact_verified_executable(darwin_executable)
            if (
                executable_content_identity(darwin_executable_object_identity)
                != executable_content_identity(components["expected_executable_object_identity"])
            ):
                os.close(darwin_executable_descriptor)
                raise HelperError("verified Claude executable copy changed before launch")
            darwin_container_descriptor = os.open(
                darwin_executable.parent,
                os.O_RDONLY | getattr(os, "O_DIRECTORY", 0) | getattr(os, "O_NOFOLLOW", 0),
            )
            try:
                seal_darwin_launch_container(
                    darwin_container_descriptor, darwin_executable_descriptor
                )
            except BaseException:
                os.close(darwin_executable_descriptor)
                os.close(darwin_container_descriptor)
                raise
    try:
        output = run_command(
            [
                "tmux", "new-window", "-d", "-P", "-F",
                "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}",
                "-t", "=" + request_data["tmux_session"] + ":",
                "-n", request_data["tmux_window"], "-c", components["workdir"], components["start_command"],
            ]
        )
        fields = output.split("\t")
        if len(fields) != 4 or fields[0] != request_data["tmux_session"] or fields[1] != request_data["tmux_window"] or not fields[2].startswith("@") or not fields[3].startswith("%"):
            raise HelperError("tmux launch did not return one exact session/window/pane identity")
        tmux_identity = {"session": fields[0], "window": fields[1], "window_id": fields[2], "pane_id": fields[3]}
        identity = wait_for_claude_startup(
            fields[3],
            request_data["claude_session_id"],
            components["expected_argv_digest"],
            components["expected_launcher_identity"],
            components["expected_executable_object_identity"],
            darwin_executable_object_identity,
            components["expected_launcher_argv0_digest"],
        )
    finally:
        if darwin_container_descriptor is not None and darwin_executable_descriptor is not None:
            try:
                restore_darwin_launch_container(
                    darwin_container_descriptor, darwin_executable_descriptor
                )
            finally:
                os.close(darwin_executable_descriptor)
                os.close(darwin_container_descriptor)
    for key, value in tmux_identity.items():
        if identity[key] != value:
            raise HelperError("verified Claude startup does not match created tmux identity")
    if identity["workdir"] != components["workdir"]:
        raise HelperError("verified Claude startup workdir does not match launch plan")
    if identity["launch_command_digest"] != components["launch_command_digest"]:
        raise HelperError("verified Claude startup command does not match launch plan")
    result = {
        "event_id": result_id,
        "kind": "launch_completed",
        "operation_event_id": event_id,
        "identity": identity,
        "at": utc_now(),
    }
    with store.mutation_lock():
        receipt_store = store.load_store()
        receipt = store.find(receipt_store, delegation_id)
        if not valid_indeterminate_detach_candidate(receipt) or find_event(receipt, event_id) != intent:
            raise HelperError("receipt changed while launch completion was being verified")
        receipt["events"].append(result)
        receipt["updated_at"] = result["at"]
        store.commit(receipt_store)
    for descriptor in held_descriptors:
        os.close(descriptor)
    return {"outcome": "launched", **tmux_launch_identity(identity)}


def diagnostics() -> dict[str, Any]:
    capabilities: dict[str, Any] = {
        "automatic_interactive_input": {"status": "unavailable", "reason": "no supported empty focused atomically reserved composer capability"},
        "strict_mcp_runtime": {"status": "untested", "reason": "requires a controlled interactive launch"},
        "read_confinement_runtime": {"status": "untested", "reason": "requires controlled representative allowed and denied reads"},
        "notification_target": {"status": "untested", "reason": "requires a separately supplied verified Amp pane"},
        "managed_policy_runtime": {"status": "untested", "reason": "managed settings and hooks have higher precedence than session settings"},
        "session_hook": {"status": "unavailable", "reason": "helper uses explicit session ID and process identity; no hook contract is assumed"},
    }
    system = platform.system()
    if system == "Darwin":
        capabilities["platform"] = {"status": "supported", "value": system, "identity_source": "Darwin kernel process APIs"}
    elif system == "Linux":
        try:
            exact_process_identity(os.getpid())
            capabilities["platform"] = {"status": "supported", "value": system, "identity_source": "/proc stat, cmdline, and exe"}
        except HelperError as error:
            capabilities["platform"] = {"status": "unavailable", "value": system, "reason": str(error)[:512]}
    else:
        capabilities["platform"] = {"status": "unavailable", "value": system, "reason": "no exact process identity implementation"}
    capabilities["mutating_delegation"] = {"status": "supported" if system == "Darwin" else "unavailable"}
    if system == "Linux":
        capabilities["mutating_delegation"]["reason"] = "Linux support is limited to read-only delegation"
    elif system != "Darwin":
        capabilities["mutating_delegation"]["reason"] = "not proven on this platform"
    try:
        probe_environment = exact_launch_environment(["GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"])
        version = run_command(["claude", "--version"], probe_environment)
        help_text = run_command(["claude", "--help"], probe_environment)
        missing = [flag for flag in REQUIRED_CLAUDE_FLAGS if flag not in help_text]
        capabilities["claude_code"] = {"status": "supported" if not missing else "unavailable", "version": version[:128], "missing_flags": missing}
        exposes_model = help_exposes_option(help_text, "--model")
        capabilities["model_selection"] = {
            "status": "supported" if exposes_model else "unavailable",
            "reason": "installed Claude CLI exposes --model; model provisioning and availability are not observed"
            if exposes_model else "installed Claude CLI does not expose --model",
        }
    except HelperError as error:
        capabilities["claude_code"] = {"status": "unavailable", "reason": str(error)[:512]}
        capabilities["model_selection"] = {"status": "unavailable", "reason": "Claude CLI capability is unavailable"}
    try:
        capabilities["tmux"] = {"status": "supported", "version": run_command(["tmux", "-V"])[:128]}
    except HelperError as error:
        capabilities["tmux"] = {"status": "unavailable", "reason": str(error)[:512]}
    capacity: dict[str, Any] = {"status": "unavailable", "windows": []}
    try:
        diagnostic_environment = exact_launch_environment(["GH_TOKEN", "GITHUB_TOKEN", "GITLAB_TOKEN"])
        raw = json.loads(
            read_capacity_source(
                ["codexbar", "usage", "--provider", "claude", "--format", "json"],
                diagnostic_environment,
            ),
            object_pairs_hook=reject_duplicate_json_pairs,
        )
        validate_codexbar_capacity_payload(raw)
        capacity = {"status": "unavailable", "reason": "capacity source payload has no supported versioned contract", "windows": []}
    except (HelperError, json.JSONDecodeError, KeyError, RecursionError, TypeError):
        capacity = {"status": "unavailable", "reason": "capacity source is unavailable", "windows": []}
    return {"experimental": True, "capabilities": capabilities, "capacity": capacity}


def read_capacity_source(arguments: list[str], environment: dict[str, str]) -> str:
    try:
        process = subprocess.Popen(
            arguments,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
        )
    except OSError as error:
        raise HelperError("capacity source is unavailable") from error
    output = bytearray()
    deadline = time.monotonic() + 5
    selector = selectors.DefaultSelector()
    try:
        if process.stdout is None:
            raise HelperError("capacity source is unavailable")
        selector.register(process.stdout, selectors.EVENT_READ)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise HelperError("capacity source is unavailable")
            if not selector.select(remaining):
                raise HelperError("capacity source is unavailable")
            chunk = os.read(process.stdout.fileno(), min(64 * 1024, MAX_CAPACITY_SOURCE_BYTES + 1 - len(output)))
            if not chunk:
                break
            output.extend(chunk)
            if len(output) > MAX_CAPACITY_SOURCE_BYTES:
                raise HelperError("capacity source is unavailable")
        process.wait(timeout=max(0.001, deadline - time.monotonic()))
        if process.returncode != 0:
            raise HelperError("capacity source is unavailable")
        return output.decode("utf-8")
    except (OSError, subprocess.TimeoutExpired, UnicodeDecodeError) as error:
        raise HelperError("capacity source is unavailable") from error
    finally:
        selector.close()
        if process.poll() is None:
            process.kill()
        process.wait()
        if process.stdout is not None:
            process.stdout.close()


def read_bounded_command(arguments: list[str], limit: int) -> str:
    try:
        process = subprocess.Popen(arguments, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    except OSError as error:
        raise HelperError("bounded inspection is unavailable") from error
    output = bytearray()
    deadline = time.monotonic() + 5
    selector = selectors.DefaultSelector()
    try:
        if process.stdout is None:
            raise HelperError("bounded inspection is unavailable")
        selector.register(process.stdout, selectors.EVENT_READ)
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not selector.select(remaining):
                raise HelperError("bounded inspection is unavailable")
            chunk = os.read(process.stdout.fileno(), min(64 * 1024, limit + 1 - len(output)))
            if not chunk:
                break
            output.extend(chunk)
            if len(output) > limit:
                raise HelperError("bounded inspection is unavailable")
        process.wait(timeout=max(0.001, deadline - time.monotonic()))
        if process.returncode != 0:
            raise HelperError("bounded inspection is unavailable")
        return output.decode("utf-8").rstrip("\r\n")
    except (OSError, subprocess.TimeoutExpired, UnicodeDecodeError) as error:
        raise HelperError("bounded inspection is unavailable") from error
    finally:
        selector.close()
        if process.poll() is None:
            process.kill()
        process.wait()
        if process.stdout is not None:
            process.stdout.close()


def validate_codexbar_capacity_payload(value: Any) -> None:
    if not isinstance(value, list) or len(value) != 1 or not isinstance(value[0], dict):
        raise HelperError("capacity source payload contract is unsupported")
    provider = value[0]
    if set(provider) != {"provider", "source", "usage"}:
        raise HelperError("capacity source payload contract is unsupported")
    if provider.get("provider") != "claude" or provider.get("source") not in {"web", "oauth"}:
        raise HelperError("capacity source payload contract is unsupported")
    usage = provider.get("usage")
    if not isinstance(usage, dict) or set(usage) != {"primary", "secondary", "tertiary", "extraRateWindows", "updatedAt"}:
        raise HelperError("capacity source payload contract is unsupported")
    if not isinstance(usage.get("updatedAt"), str) or not bounded_utc_timestamp(usage["updatedAt"]):
        raise HelperError("capacity source payload contract is unsupported")
    for name in ("primary", "secondary", "tertiary"):
        window = usage.get(name)
        if window is not None:
            validate_codexbar_window(window)
    extra = usage.get("extraRateWindows")
    if not isinstance(extra, list) or len(extra) > MAX_CAPACITY_EXTRA_WINDOWS:
        raise HelperError("capacity source payload contract is unsupported")
    for named in extra:
        if not isinstance(named, dict) or set(named) != {"id", "title", "window"}:
            raise HelperError("capacity source payload contract is unsupported")
        required_string(named, "id", 256)
        required_string(named, "title", 256)
        validate_codexbar_window(named.get("window"))


def validate_codexbar_window(value: Any) -> None:
    if not isinstance(value, dict) or set(value) != {"usedPercent", "windowMinutes", "resetsAt"}:
        raise HelperError("capacity source payload contract is unsupported")
    percentage(value.get("usedPercent"), "capacity source usedPercent")
    if type(value.get("windowMinutes")) is not int or value["windowMinutes"] <= 0:
        raise HelperError("capacity source payload contract is unsupported")
    if not bounded_utc_timestamp(value.get("resetsAt")):
        raise HelperError("capacity source payload contract is unsupported")


def percentage(value: Any, label: str) -> float:
    if (
        isinstance(value, bool)
        or not isinstance(value, (int, float))
        or not math.isfinite(value)
        or value < 0
        or value > 100
    ):
        raise HelperError(f"{label} must be a number from 0 to 100")
    return float(value)


def bounded_utc_timestamp(value: Any) -> bool:
    if not isinstance(value, str) or not value or len(value.encode()) > 64 or not value.endswith("Z"):
        return False
    try:
        parsed = datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError:
        return False
    return parsed.utcoffset() == timedelta(0)


def capacity_now() -> datetime:
    return datetime.now(timezone.utc)


def bounded_capacity_reset(value: Any, window_minutes: int, observed_at: datetime) -> bool:
    if not bounded_utc_timestamp(value):
        return False
    parsed = datetime.fromisoformat(value[:-1] + "+00:00")
    return observed_at < parsed <= observed_at + timedelta(minutes=window_minutes)


def capacity_decision_digest(value: Any) -> str:
    def normalize(item: Any) -> Any:
        if isinstance(item, dict):
            return {key: normalize(nested) for key, nested in item.items()}
        if isinstance(item, list):
            return [normalize(nested) for nested in item]
        return item

    canonical = json.dumps(normalize(value), sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(canonical.encode()).hexdigest()


def unknown_capacity_decision(
    acknowledged: bool,
    acknowledgement_of: str,
    reason: str,
    request_digest: str,
    source: str,
    confidence: str,
) -> dict[str, Any]:
    required = {
        "decision": "acknowledgement_required",
        "autonomous_selection": False,
        "may_proceed": False,
        "governing_window": "unknown",
        "reason": reason,
        "capacity_request_digest": request_digest,
        "capacity_source": source,
        "capacity_confidence": confidence,
    }
    required["decision_digest"] = capacity_decision_digest(required)
    if not acknowledged:
        if acknowledgement_of:
            raise HelperError("acknowledgement_of requires explicit acknowledgement")
        return required
    if acknowledgement_of != required["decision_digest"]:
        raise HelperError("explicit acknowledgement must reference the prior acknowledgement-required decision")
    decision = {
        "decision": "explicit_acknowledgement",
        "autonomous_selection": False,
        "may_proceed": True,
        "governing_window": "unknown",
        "reason": reason,
        "acknowledgement_of": acknowledgement_of,
    }
    decision["decision_digest"] = capacity_decision_digest(decision)
    return decision


def decide_mutating_capacity(request: Any) -> dict[str, Any]:
    if not isinstance(request, dict):
        raise HelperError("capacity decision request must be an object")
    reject_unknown(request, {"capacity", "reserve_floors", "acknowledged_unknown_capacity", "acknowledgement_of"}, "capacity decision")
    acknowledged = request.get("acknowledged_unknown_capacity")
    if not isinstance(acknowledged, bool):
        raise HelperError("acknowledged_unknown_capacity must be boolean")
    acknowledgement_of = request.get("acknowledgement_of", "")
    if not isinstance(acknowledgement_of, str) or len(acknowledgement_of.encode()) > 256:
        raise HelperError("acknowledgement_of must be a string of at most 256 bytes")
    capacity = request.get("capacity")
    floors = request.get("reserve_floors")
    if not isinstance(capacity, dict) or not isinstance(floors, dict):
        raise HelperError("capacity and reserve_floors must be objects")
    capacity_source = capacity.get("source", "unknown")
    capacity_confidence = capacity.get("confidence", "unknown")
    if not isinstance(capacity_source, str) or not isinstance(capacity_confidence, str):
        raise HelperError("capacity source and confidence must be strings")
    digest_request = copy.deepcopy(request)
    digest_request["acknowledged_unknown_capacity"] = False
    digest_request.pop("acknowledgement_of", None)
    request_digest = capacity_decision_digest(digest_request)
    reject_unknown(floors, {"five_hour", "weekly", "model_specific"}, "reserve floors")
    five_hour_floor = percentage(floors.get("five_hour"), "five_hour reserve floor")
    weekly_floor = percentage(floors.get("weekly"), "weekly reserve floor")
    model_floors = floors.get("model_specific")
    if not isinstance(model_floors, dict):
        raise HelperError("model_specific reserve floors must be an object")
    for name, floor in model_floors.items():
        if not isinstance(name, str) or not name:
            raise HelperError("model-specific reserve floor names must be non-empty strings")
        percentage(floor, f"{name} reserve floor")

    capacity_fields = {"status", "provider", "source", "source_version", "schema_version", "confidence", "windows"}
    reliable = (
        set(capacity) == capacity_fields
        and capacity.get("status") == "supported"
        and capacity.get("provider") == "claude"
        and capacity.get("source") in {"web", "api", "oauth"}
        and type(capacity.get("source_version")) is int
        and capacity.get("source_version") == 1
        and type(capacity.get("schema_version")) is int
        and capacity.get("schema_version") == 1
        and capacity.get("confidence") == "reported"
    )
    windows = capacity.get("windows")
    if not isinstance(windows, list) or not windows:
        return unknown_capacity_decision(acknowledged, acknowledgement_of, "capacity has no available windows; reserve impact is unknown", request_digest, capacity_source, capacity_confidence)
    evaluated = []
    present_window_classes = {"five_hour": False, "weekly": False}
    present_model_windows = {name: 0 for name in model_floors}
    seen_window_names: set[str] = set()
    missing_capacity = not reliable
    observed_at = capacity_now()
    for raw in windows:
        if not isinstance(raw, dict):
            missing_capacity = True
            continue
        name = required_string(raw, "name", 256)
        duration = raw.get("window_minutes")
        if name == "primary" and ("model_specific" in raw or type(duration) is not int or duration != 300):
            raise HelperError("capacity window name conflicts with its declared class")
        if name == "secondary" and ("model_specific" in raw or type(duration) is not int or duration != 10080):
            raise HelperError("capacity window name conflicts with its declared class")
        if name in model_floors and raw.get("model_specific") is not True:
            raise HelperError("capacity window name conflicts with its declared class")
        if raw.get("model_specific") is True:
            if name in {"primary", "secondary"}:
                raise HelperError("capacity window name conflicts with its declared class")
            if set(raw) != {"name", "used_percent", "window_minutes", "resets_at", "model_specific"}:
                missing_capacity = True
            if raw.get("window_minutes") not in {300, 10080}:
                missing_capacity = True
            if name not in model_floors:
                raise HelperError("reserve floor is required for every available model-specific window")
            floor = percentage(model_floors[name], f"{name} reserve floor")
            window_class = "model_specific"
            present_model_windows[name] += 1
        elif raw.get("window_minutes") == 300:
            if name != "primary" or name in model_floors:
                raise HelperError("capacity window name conflicts with its declared class")
            if set(raw) != {"name", "used_percent", "window_minutes", "resets_at"}:
                missing_capacity = True
            floor = five_hour_floor
            window_class = "five_hour"
            if present_window_classes[window_class]:
                missing_capacity = True
            present_window_classes[window_class] = True
        elif raw.get("window_minutes") == 10080:
            if name != "secondary" or name in model_floors:
                raise HelperError("capacity window name conflicts with its declared class")
            if set(raw) != {"name", "used_percent", "window_minutes", "resets_at"}:
                missing_capacity = True
            floor = weekly_floor
            window_class = "weekly"
            if present_window_classes[window_class]:
                missing_capacity = True
            present_window_classes[window_class] = True
        else:
            missing_capacity = True
            continue
        if name in seen_window_names:
            missing_capacity = True
        seen_window_names.add(name)
        window_minutes = raw.get("window_minutes")
        if type(window_minutes) is not int or not bounded_capacity_reset(raw.get("resets_at"), window_minutes, observed_at):
            missing_capacity = True
        if isinstance(raw.get("used_percent"), bool) or not isinstance(raw.get("used_percent"), (int, float)):
            missing_capacity = True
            continue
        used = percentage(raw["used_percent"], f"{name} used_percent")
        remaining = 100.0 - used
        evaluated.append(
            {
                "name": name,
                "class": window_class,
                "remaining_percent": remaining,
                "reserve_floor_percent": floor,
                "margin_percent": remaining - floor,
            }
        )
    governing = min(evaluated, key=lambda window: (window["margin_percent"], window["name"])) if evaluated else None
    if governing is not None and governing["margin_percent"] < 0:
        raise HelperError(f"capacity is below the hard reserve floor for governing window {governing['name']}")
    if any(count != 1 for count in present_model_windows.values()):
        missing_capacity = True
    if missing_capacity or not all(present_window_classes.values()):
        reason = "capacity is low-confidence; reserve impact is unknown" if not reliable else "one or more required capacity windows are missing; reserve impact is unknown"
        return unknown_capacity_decision(acknowledged, acknowledgement_of, reason, request_digest, capacity_source, capacity_confidence)
    if governing is None:
        return unknown_capacity_decision(acknowledged, acknowledgement_of, "capacity has no evaluable windows; reserve impact is unknown", request_digest, capacity_source, capacity_confidence)
    if acknowledged or acknowledgement_of:
        raise HelperError("reliable capacity does not accept unknown-capacity acknowledgement fields")
    decision = {
        "decision": "autonomous_allowed",
        "autonomous_selection": True,
        "may_proceed": True,
        "capacity_source": capacity_source,
        "capacity_confidence": capacity_confidence,
        "governing_window": governing["name"],
        "remaining_percent": governing["remaining_percent"],
        "reserve_floor_percent": governing["reserve_floor_percent"],
        "margin_percent": governing["margin_percent"],
        "windows": evaluated,
    }
    decision["decision_digest"] = capacity_decision_digest(decision)
    return decision


def prepare_mutation(request: Any) -> dict[str, Any]:
    if not isinstance(request, dict):
        raise HelperError("mutation preparation request must be an object")
    fields = {
        "workdir",
        "repository",
        "writer_owner",
        "integration_owner",
        "coordinator_write_frozen",
        "shared_writable",
        "handoff",
    }
    reject_unknown(request, fields, "mutation preparation")
    for key in ("workdir", "repository", "writer_owner", "integration_owner", "handoff"):
        required_string(request, key, 2048 if key == "workdir" else 256)
    if request.get("shared_writable") is not False:
        raise HelperError("shared writable workdirs are prohibited")
    if (
        request.get("coordinator_write_frozen") is not True
        or request["writer_owner"] != "claude_mutating_delegate"
        or request["integration_owner"] != "amp_coordinator"
    ):
        raise HelperError("mutation requires unambiguous exclusive writer ownership")
    if request["handoff"] != "one_clean_local_commit":
        raise HelperError("mutation permits only one_clean_local_commit handoff")
    workdir = str(pathlib.Path(request["workdir"]).resolve(strict=True))
    git = ["git", "--no-optional-locks", "-C", workdir]
    if run_command(git + ["rev-parse", "--show-toplevel"]) != workdir:
        raise HelperError("mutation workdir is not the canonical Git worktree root")
    if run_command(git + ["rev-parse", "--git-dir"]) == run_command(git + ["rev-parse", "--git-common-dir"]):
        raise HelperError("mutation requires a dedicated linked worktree")
    if run_command(git + ["status", "--porcelain"]):
        raise HelperError("mutation requires a clean immutable baseline")
    branch = run_command(git + ["symbolic-ref", "--short", "HEAD"])
    if not branch:
        raise HelperError("mutation requires an unambiguous checked-out branch")
    remote = run_command(git + ["remote", "get-url", "origin"])
    if repository_from_remote(remote) != request["repository"]:
        raise HelperError("mutation repository does not match the declared repository")
    return {
        "workdir": workdir,
        "repository": request["repository"],
        "baseline_commit": run_command(git + ["rev-parse", "HEAD"]),
        "baseline_branch": branch,
        "writer_owner": request["writer_owner"],
        "integration_owner": request["integration_owner"],
        "coordinator_write_frozen": True,
        "shared_writable": False,
        "handoff": request["handoff"],
    }


def revalidate_mutating_launch_lease(
    receipt_store: dict[str, Any], receipt: dict[str, Any], request: dict[str, Any]
) -> None:
    binding = receipt["binding"]
    prepared = prepare_mutation(
        {
            "workdir": request["workdir"],
            "repository": request["repository"],
            "writer_owner": request["writer_owner"],
            "integration_owner": request["integration_owner"],
            "coordinator_write_frozen": request["coordinator_write_frozen"],
            "shared_writable": request["shared_writable"],
            "handoff": request["handoff"],
        }
    )
    if (
        prepared["workdir"] != receipt_writer_lease(receipt)
        or prepared["repository"] != binding["repository"]
        or prepared["baseline_branch"] != binding["baseline_branch"]
        or prepared["baseline_commit"] != binding["base"]
    ):
        raise HelperError("mutating launch no longer matches the immutable leased baseline")
    owners = [
        candidate
        for candidate in receipt_store["receipts"]
        if candidate.get("state") != "verified_parked"
        and receipt_writer_lease(candidate) is not None
        and writer_leases_match(receipt_writer_lease(candidate), prepared["workdir"])
    ]
    if len(owners) != 1 or owners[0]["binding"]["delegation_id"] != binding["delegation_id"]:
        raise HelperError("mutating launch receipt does not exclusively own the logical writer lease")


def validate_frozen_handoff(store: ReceiptStore, request: Any) -> dict[str, Any]:
    if not isinstance(request, dict):
        raise HelperError("handoff validation request must be an object")
    reject_unknown(request, {"delegation_id"}, "handoff validation")
    delegation_id = protocol_id(request, "delegation_id")
    with store.mutation_lock():
        receipt = store.find(store.load_store(), delegation_id)
        if not receipt.get("submission_frozen") or receipt.get("writer_authority") != "frozen":
            raise HelperError("handoff validation requires a frozen mutating submission")
        source = find_event(receipt, receipt.get("report_message_id", ""))
        if source is None or source.get("kind") != "valid_report" or source.get("message", {}).get("kind") != "mutating_report":
            raise HelperError("handoff validation requires the frozen mutating report")
        validation = validate_mutating_handoff(source["message"], receipt["binding"])
        if validation != receipt.get("handoff_validation"):
            raise HelperError("current handoff differs from the frozen objective validation")
        return validation


def mcp_tools(mutating: bool = False) -> list[dict[str, Any]]:
    common_properties = {
        "protocol_version": {"type": "integer", "const": PROTOCOL_VERSION},
        "delegation_id": {"type": "string", "maxLength": 256},
        "nonce": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
        "message_id": {"type": "string", "maxLength": 256},
        "in_reply_to": {"type": "string", "maxLength": 256},
        "task_id": {"type": "string", "maxLength": 256},
        "origin_thread": {"type": "string", "maxLength": 256},
        "repository": {"type": "string", "maxLength": 256},
        "base": {"type": "string", "maxLength": 256},
        "workdir": {"type": "string", "maxLength": 2048},
        "producer_role": {"type": "string", "const": "mutating_delegate" if mutating else "thinker"},
        "authority": {"type": "string", "const": "exclusive_writer" if mutating else "read_only"},
        "launch_policy_digest": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
        "created_at": {"type": "string", "maxLength": 256},
    }
    common_required = list(common_properties)
    string_list = {"type": "array", "maxItems": 32, "items": {"type": "string", "maxLength": 2048}}
    report_properties = dict(common_properties)
    if mutating:
        report_payload = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "accepted_role": {"type": "boolean", "const": True},
                "accepted_exclusions": {"type": "boolean", "const": True},
                "status": {"type": "string", "enum": ["complete", "blocked"]},
                "summary": {"type": "string", "maxLength": 8192},
                "blockers": string_list,
                "changed_artifacts": string_list,
                "verification": string_list,
                "references": string_list,
                "handoff_commit": {"type": "string", "maxLength": 256},
                "authorship": {"type": "string", "const": "claude_mutating_delegate"},
                "non_claims": {
                    "type": "object",
                    "additionalProperties": False,
                    "properties": {name: {"type": "boolean", "const": False} for name in ("correct", "accepted", "merge_ready", "cleanup_authorized")},
                    "required": ["correct", "accepted", "merge_ready", "cleanup_authorized"],
                },
            },
            "required": [
                "accepted_role", "accepted_exclusions", "status", "summary", "blockers", "changed_artifacts",
                "verification", "references", "handoff_commit", "authorship", "non_claims",
            ],
        }
    else:
        report_payload = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "accepted_role": {"type": "boolean", "const": True},
                "accepted_exclusions": {"type": "boolean", "const": True},
                "status": {"type": "string", "enum": ["complete", "blocked"]},
                "verdict": {"type": "string", "maxLength": 4096},
                "rationale": {"type": "string", "maxLength": 8192},
                "evidence": string_list,
                "assumptions": string_list,
                "unsupported_claims": string_list,
                "blockers": string_list,
                "verification": string_list,
                "changed_artifacts": {"type": "array", "maxItems": 0},
                "references": string_list,
            },
            "required": [
                "accepted_role", "accepted_exclusions", "status", "verdict", "rationale", "evidence", "assumptions",
                "unsupported_claims", "blockers", "verification", "changed_artifacts", "references",
            ],
        }
    report_properties.update(
        {
            "kind": {"type": "string", "const": "mutating_report" if mutating else "thinker_report"},
            "report": report_payload,
        }
    )
    input_properties = dict(common_properties)
    input_properties.update(
        {
            "kind": {"type": "string", "const": "input_request"},
            "input_request": {
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "request_type": {"type": "string", "enum": ["clarification", "decision", "missing_evidence"]},
                    "question": {"type": "string", "maxLength": 2048},
                    "blocking_reason": {"type": "string", "maxLength": 2048},
                },
                "required": ["request_type", "question", "blocking_reason"],
            },
        }
    )
    return [
        {
            "name": "submit_report",
            "description": "Submit the bounded semantic report and freeze mutating authority." if mutating else "Submit the complete bounded semantic thinker report. Pane output is not authoritative.",
            "inputSchema": {
                "type": "object",
                "additionalProperties": False,
                "properties": report_properties,
                "required": common_required + ["kind", "report"],
            },
        },
        {
            "name": "submit_input_request",
            "description": "Persist one typed request for manual coordinator input; this never injects a response.",
            "inputSchema": {
                "type": "object",
                "additionalProperties": False,
                "properties": input_properties,
                "required": common_required + ["kind", "input_request"],
            },
        },
    ]


def mcp_response(identifier: Any, *, result: Any = None, error: dict[str, Any] | None = None) -> None:
    response: dict[str, Any] = {"jsonrpc": "2.0", "id": identifier}
    if error is not None:
        response["error"] = error
    else:
        response["result"] = result
    print(json.dumps(response, sort_keys=True, separators=(",", ":")), flush=True)


def serve_mcp(store: ReceiptStore, delegation_id: str) -> int:
    initialized = False
    for raw in sys.stdin.buffer:
        if len(raw) > MAX_STORE_BYTES:
            raise HelperError("MCP message exceeds the experimental size limit")
        try:
            request = json.loads(raw)
        except json.JSONDecodeError:
            mcp_response(None, error={"code": -32700, "message": "Parse error"})
            continue
        if not isinstance(request, dict) or request.get("jsonrpc") != "2.0" or not isinstance(request.get("method"), str):
            mcp_response(request.get("id") if isinstance(request, dict) else None, error={"code": -32600, "message": "Invalid Request"})
            continue
        method = request["method"]
        identifier = request.get("id")
        if identifier is None:
            if method == "notifications/initialized":
                initialized = True
            continue
        if method == "initialize":
            params = request.get("params", {})
            requested = params.get("protocolVersion") if isinstance(params, dict) else ""
            protocol = requested if requested in {"2025-03-26", "2025-06-18", "2025-11-25"} else "2025-06-18"
            mcp_response(
                identifier,
                result={
                    "protocolVersion": protocol,
                    "capabilities": {"tools": {"listChanged": False}},
                    "serverInfo": {"name": "amux-experimental-claude-delegation", "version": "1"},
                    "instructions": "Only explicit bounded semantic submission is authoritative.",
                },
            )
            continue
        if not initialized:
            mcp_response(identifier, error={"code": -32002, "message": "Server not initialized"})
            continue
        if method == "ping":
            mcp_response(identifier, result={})
        elif method == "tools/list":
            receipt = store.show(delegation_id)
            mutating = receipt["binding"].get("producer_role") == "mutating_delegate"
            mcp_response(identifier, result={"tools": mcp_tools(mutating)})
        elif method == "tools/call":
            params = request.get("params")
            if not isinstance(params, dict) or params.get("name") not in {"submit_report", "submit_input_request"} or not isinstance(params.get("arguments"), dict):
                mcp_response(identifier, error={"code": -32602, "message": "Invalid tool call"})
                continue
            arguments = params["arguments"]
            if arguments.get("delegation_id") != delegation_id:
                mcp_response(identifier, result={"content": [{"type": "text", "text": "delegation identity mismatch"}], "isError": True})
                continue
            try:
                if params["name"] == "submit_report":
                    outcome = store.submit_message(arguments, "report")
                else:
                    outcome = store.submit_message(arguments, "input_request")
            except HelperError as error:
                mcp_response(identifier, result={"content": [{"type": "text", "text": str(error)}], "isError": True})
                continue
            mcp_response(identifier, result={"content": [{"type": "text", "text": f"outcome:{outcome}"}], "structuredContent": {"outcome": outcome}, "isError": False})
        else:
            mcp_response(identifier, error={"code": -32601, "message": "Method not found"})
    return 0


def validate_routing(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("routing must be an object")
    reject_unknown(value, {"target", "recovery"}, "routing")
    target = required_string(value, "target", 256)
    recovery = value.get("recovery", "")
    if not isinstance(recovery, str) or len(recovery.encode()) > 256:
        raise HelperError("routing recovery must be a string of at most 256 bytes")
    result = {"target": target}
    if recovery:
        result["recovery"] = recovery
    return result


def validate_terminal_amp_authorization(value: Any) -> dict[str, str]:
    if not isinstance(value, dict):
        raise HelperError("terminal Amp work authorization must be an object")
    allowed = {"terminal_state", "report_sha256", "coordinator_authorization_sha256"}
    reject_unknown(value, allowed, "terminal Amp work authorization")
    terminal_state = required_string(value, "terminal_state", 32)
    if terminal_state not in {"merged", "closed_terminal"}:
        raise HelperError("terminal Amp work authorization has an unsupported state")
    for key in ("report_sha256", "coordinator_authorization_sha256"):
        digest = required_string(value, key, 64)
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
            raise HelperError(f"terminal Amp work authorization {key} must be a lowercase SHA-256 value")
    return copy.deepcopy(value)


def reject_unknown(value: dict[str, Any], allowed: set[str], label: str) -> None:
    unknown = sorted(set(value) - allowed)
    if unknown:
        raise HelperError(f"{label} contains unknown fields: {', '.join(unknown)}")


def find_event(receipt: dict[str, Any], event_id: str) -> dict[str, Any] | None:
    for event in receipt["events"]:
        if event.get("event_id") == event_id:
            return event
    return None


def require_receipt_mutable(receipt: dict[str, Any]) -> None:
    events = receipt.get("events")
    if (
        receipt.get("worker_detached") is not None
        or receipt.get("retirement_intent") is not None
        or receipt.get("pair_retired") is not None
        or receipt.get("acquired_retirement_intent") is not None
        or receipt.get("acquired_pair_retired") is not None
        or (
        isinstance(events, list)
        and any(
            isinstance(event, dict)
            and event.get("kind") in {
                "worker_detached", "retirement_intent", "pair_retired",
                "acquired_retirement_intent", "acquired_pair_retired",
            }
            for event in events
        )
        )
    ):
        raise HelperError("terminal receipt is sealed against further mutation")


def require_launch_transport_allowed(store: ReceiptStore, delegation_id: str) -> None:
    receipt = store.find(store.load_store(require_exists=True), delegation_id)
    require_receipt_mutable(receipt)
    if not valid_indeterminate_detach_candidate(receipt):
        raise HelperError("launch transport is not authorized for the current receipt state")
    origin_thread = receipt["binding"]["origin_thread"]
    if origin_thread in store.lifecycle.load()["teardown_fences"]:
        raise HelperError("launch transport is revoked by the durable origin fence")


def execute_authorized_launch_transport(
    store: ReceiptStore,
    delegation_id: str,
    execution_path: str,
    argv: list[str],
    environment: dict[str, str],
) -> None:
    with store.lifecycle.mutation_lock():
        store_lock = (
            contextlib.nullcontext()
            if store.lifecycle.state_dir == store.state_dir
            else store.mutation_lock()
        )
        with store_lock:
            require_launch_transport_allowed(store, delegation_id)
            os.execve(execution_path, argv, environment)


def completed_launch_identity(receipt: dict[str, Any]) -> dict[str, str]:
    completed = [event for event in receipt["events"] if event.get("kind") == "launch_completed"]
    if len(completed) != 1 or not isinstance(completed[0].get("identity"), dict):
        raise HelperError("session acquisition requires one completed receipt launch")
    identity = completed[0]["identity"]
    string_fields = {"session", "window", "window_id", "pane_id"}
    if platform.system() == "Linux":
        string_fields |= {
            "claude_session_id", "workdir", "current_command", "process_name", "process_identity",
            "process_executable_identity", "normalized_argv_digest", "process_command_digest",
            "launch_command_digest",
        }
    if any(not isinstance(identity.get(key), str) or not identity[key] for key in string_fields):
        raise HelperError("completed receipt launch has incomplete process or tmux identity")
    if platform.system() == "Linux" and (not isinstance(identity.get("pane_pid"), int) or identity["pane_pid"] <= 0):
        raise HelperError("completed receipt launch has incomplete process or tmux identity")
    return identity


def tmux_launch_identity(identity: dict[str, Any]) -> dict[str, str]:
    return {key: identity[key] for key in ("session", "window", "window_id", "pane_id")}


def receipt_launch_intent(receipt: dict[str, Any]) -> dict[str, Any]:
    intents = [event for event in receipt["events"] if event.get("kind") == "launch_intent"]
    if len(intents) != 1:
        raise HelperError("session identity requires one immutable launch intent")
    intent = intents[0]
    for key in (
        "expected_argv_digest", "expected_launcher_identity", "expected_executable_object_identity"
    ):
        value = intent.get(key)
        if not isinstance(value, str) or not value:
            raise HelperError("launch intent lacks expected process identity")
    return copy.deepcopy(intent)


def pre_identity_launch_intent(receipt: dict[str, Any]) -> dict[str, Any]:
    intents = [event for event in receipt["events"] if event.get("kind") == "launch_intent"]
    if len(intents) != 1:
        raise HelperError("legacy detach requires one immutable launch intent")
    intent = intents[0]
    fields = {
        "event_id", "kind", "workflow", "request_digest", "claude_session_id", "tmux_session",
        "tmux_window", "packet_digest", "launch_policy_digest", "launch_command_digest", "at",
    }
    if set(intent) != fields:
        raise HelperError("launch intent is not the recognized pre-identity shape")
    protocol_id(intent, "event_id")
    protocol_id(intent, "claude_session_id")
    if intent.get("workflow") not in {"read_only", "mutating"}:
        raise HelperError("legacy launch intent workflow is invalid")
    for key in ("tmux_session", "tmux_window"):
        required_string(intent, key, 256)
    if not bounded_utc_timestamp(intent.get("at")):
        raise HelperError("legacy launch intent timestamp is invalid")
    for key in ("request_digest", "packet_digest", "launch_policy_digest", "launch_command_digest"):
        digest = required_string(intent, key, 64)
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
            raise HelperError(f"legacy launch intent {key} must be a lowercase SHA-256 value")
    binding = receipt.get("binding")
    if not isinstance(binding, dict) or any(
        binding.get(key) != intent[key]
        for key in ("packet_digest", "launch_policy_digest", "launch_command_digest")
    ):
        raise HelperError("legacy launch intent substituted immutable receipt evidence")
    expected_role = "mutating_delegate" if intent["workflow"] == "mutating" else "thinker"
    if binding.get("producer_role") != expected_role:
        raise HelperError("legacy launch intent workflow does not match immutable receipt authority")
    return copy.deepcopy(intent)


def worker_teardown_receipt_blocker(receipt: dict[str, Any]) -> str | None:
    if (
        valid_worker_detach_chain(receipt)
        or valid_pair_retirement_chain(receipt)
        or valid_acquired_no_report_pair_retirement_chain(receipt)
    ):
        return None
    if valid_pre_identity_acquired_no_report_candidate(receipt):
        return "pre_identity_acquired_pair_permanently_non_retirable"
    if receipt.get("input_state") in {"pending", "seen"}:
        return "unresolved_input"
    events = receipt.get("events", [])
    park_intents = [event for event in events if event.get("kind") == "park_intent"]
    if park_intents and receipt.get("state") != "verified_parked":
        return "park_indeterminate"
    state = receipt.get("state")
    if state == "verified_parked":
        return None if valid_worker_lifecycle_chain(receipt, parked=True) else "receipt_unverified"
    launch_intents = [event for event in events if event.get("kind") == "launch_intent"]
    launch_results = [event for event in events if event.get("kind") == "launch_completed"]
    if len(launch_intents) == 1 and not launch_results:
        return "launch_indeterminate"
    if len(launch_intents) != 1 or len(launch_results) != 1:
        return "launch_unverified"
    if state in {"valid_report", "delivered"}:
        return "unacknowledged_report"
    if state != "acknowledged":
        return "active_or_unresolved"
    if not valid_worker_lifecycle_chain(receipt, parked=False):
        return "receipt_unverified"
    identity = receipt.get("session_identity")
    required_identity = {
        "pane_id", "claude_session_id", "process_executable_identity",
    }
    if not isinstance(identity, dict) or any(not isinstance(identity.get(key), str) or not identity[key] for key in required_identity):
        return "identity_unresolved"
    try:
        receipt_launch_intent(receipt)
    except HelperError:
        return "launch_unverified"
    return None


def valid_worker_detach_chain(receipt: dict[str, Any]) -> bool:
    events = receipt.get("events")
    materialized = receipt.get("worker_detached")
    if not isinstance(events, list) or not isinstance(materialized, dict):
        return False
    detached = [event for event in events if isinstance(event, dict) and event.get("kind") == "worker_detached"]
    intents = [event for event in events if isinstance(event, dict) and event.get("kind") == "launch_intent"]
    completions = [event for event in events if isinstance(event, dict) and event.get("kind") == "launch_completed"]
    if len(detached) != 1 or len(intents) != 1 or completions:
        return False
    event = detached[0]
    if events[-1] is not event or events.index(event) <= events.index(intents[0]):
        return False
    compatibility = event.get("compatibility")
    if compatibility is None:
        event_fields = {
            "event_id", "kind", "terminal_state", "report_sha256",
            "coordinator_authorization_sha256", "absence_code", "at",
        }
        materialized_fields = {"event_id", "at", "absence_code"}
        if event.get("absence_code") != "exact_launch_target_absent":
            return False
    else:
        event_fields = {
            "event_id", "kind", "terminal_state", "report_sha256",
            "coordinator_authorization_sha256", "absence_code", "at", "compatibility",
        }
        materialized_fields = {"event_id", "at", "absence_code", "compatibility"}
        if (
            compatibility != PRE_IDENTITY_LAUNCH_INTENT_COMPATIBILITY
            or event.get("absence_code") != "legacy_target_and_session_absent"
        ):
            return False
    if set(event) != event_fields or set(materialized) != materialized_fields:
        return False
    if not bounded_utc_timestamp(event.get("at")):
        return False
    if materialized != {key: event[key] for key in materialized_fields}:
        return False
    if event.get("terminal_state") not in {"merged", "closed_terminal"}:
        return False
    for key in ("report_sha256", "coordinator_authorization_sha256"):
        value = event.get(key)
        if not isinstance(value, str) or len(value) != 64 or any(character not in "0123456789abcdef" for character in value):
            return False
    prior = copy.deepcopy(receipt)
    prior.pop("worker_detached", None)
    prior["events"] = prior["events"][:-1]
    if not valid_indeterminate_detach_candidate(prior):
        return False
    try:
        if compatibility == PRE_IDENTITY_LAUNCH_INTENT_COMPATIBILITY:
            pre_identity_launch_intent(prior)
        else:
            receipt_launch_intent(prior)
    except HelperError:
        return False
    return True


def valid_indeterminate_detach_candidate(receipt: dict[str, Any]) -> bool:
    events = receipt.get("events")
    if not isinstance(events, list) or receipt.get("state") != "created" or receipt.get("report_message_id") != "":
        return False
    if any(key in receipt for key in (
        "session_identity", "input_state", "input_message_id", "worker_detached",
        "parked_at", "cleanup_eligible_at", "submission_frozen", "handoff_validation",
    )):
        return False
    forbidden = {
        "launch_completed", "session_acquired", "valid_report", "input_request", "input_seen",
        "input_accepted", "delivered", "acknowledged", "park_intent", "park_failed",
        "verified_parked", "worker_detached",
    }
    if any(not isinstance(event, dict) or event.get("kind") in forbidden for event in events):
        return False
    intents = [event for event in events if event.get("kind") == "launch_intent"]
    return len(intents) == 1 and worker_teardown_receipt_blocker(receipt) == "launch_indeterminate"


def modern_read_only_retirement_launch_intent(
    receipt: dict[str, Any], compatibility: str | None = None
) -> dict[str, Any]:
    if compatibility not in (None, HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY):
        raise HelperError("retirement launch intent compatibility is unsupported")
    intents = [event for event in receipt.get("events", []) if event.get("kind") == "launch_intent"]
    if len(intents) != 1:
        raise HelperError("retirement requires one modern launch intent")
    intent = intents[0]
    fields = {
        "event_id", "kind", "workflow", "request_digest", "claude_session_id", "tmux_session",
        "tmux_window", "packet_digest", "launch_policy_digest", "launch_command_digest",
        "expected_argv_digest", "expected_launcher_argv0_digest", "expected_launcher_identity",
        "expected_executable_object_identity", "packet_identity", "workdir_identity", "at",
    }
    if compatibility == HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY:
        fields.remove("expected_launcher_argv0_digest")
    binding = receipt.get("binding")
    if not isinstance(binding, dict) or binding.get("producer_role") != "thinker":
        raise HelperError("retirement launch intent authority is not read-only thinker")
    if binding.get("model") is not None:
        fields.add("model")
    if set(intent) != fields or intent.get("workflow") != "read_only":
        raise HelperError("retirement launch intent is not the exact modern read-only shape")
    protocol_id(intent, "event_id")
    protocol_id(intent, "claude_session_id")
    for key in ("tmux_session", "tmux_window", "packet_identity", "workdir_identity"):
        required_string(intent, key, 2048)
    if not bounded_utc_timestamp(intent.get("at")):
        raise HelperError("retirement launch intent timestamp is invalid")
    for key in (
        "request_digest", "packet_digest", "launch_policy_digest", "launch_command_digest",
        "expected_argv_digest",
    ):
        digest = required_string(intent, key, 64)
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
            raise HelperError(f"retirement launch intent {key} is not a lowercase SHA-256")
    if compatibility is None:
        digest = required_string(intent, "expected_launcher_argv0_digest", 64)
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest):
            raise HelperError(
                "retirement launch intent expected_launcher_argv0_digest is not a lowercase SHA-256"
            )
    for key in ("expected_launcher_identity", "expected_executable_object_identity"):
        required_string(intent, key, 512)
    for key in ("packet_digest", "launch_policy_digest", "launch_command_digest"):
        if intent[key] != binding.get(key):
            raise HelperError("retirement launch intent substituted immutable binding evidence")
    if intent.get("model") != binding.get("model"):
        raise HelperError("retirement launch intent model differs from immutable binding")
    return copy.deepcopy(intent)


def valid_live_retirement_candidate(
    receipt: dict[str, Any], authorization: dict[str, str], compatibility: str | None = None
) -> bool:
    events = receipt.get("events")
    if (
        not isinstance(events, list)
        or any(not isinstance(event, dict) for event in events)
        or receipt.get("state") != "valid_report"
        or not isinstance(receipt.get("report_message_id"), str)
        or not receipt["report_message_id"]
        or any(key in receipt for key in (
            "session_identity", "input_state", "input_message_id", "worker_detached", "pair_retired",
            "parked_at", "cleanup_eligible_at", "submission_frozen", "handoff_validation",
        ))
    ):
        return False
    kinds = [event.get("kind") for event in events]
    if kinds not in (
        ["created", "launch_intent", "valid_report"],
        ["created", "launch_intent", "valid_report", "retirement_intent"],
    ):
        return False
    created = events[0]
    binding = receipt.get("binding", {})
    if (
        set(created) != {"event_id", "kind", "at", "routing"}
        or created.get("event_id") != f"create:{binding.get('delegation_id', '')}"
        or not bounded_utc_timestamp(created.get("at"))
        or created.get("routing") != receipt.get("routing")
    ):
        return False
    reports = [event for event in events if event.get("kind") == "valid_report"]
    intents = [event for event in events if event.get("kind") == "retirement_intent"]
    if (
        len(reports) != 1
        or len(intents) > 1
        or reports[0].get("event_id") != receipt["report_message_id"]
        or set(reports[0]) != {"event_id", "kind", "message", "at"}
        or not bounded_utc_timestamp(reports[0].get("at"))
    ):
        return False
    message = reports[0].get("message")
    try:
        envelope = validate_envelope(message, "thinker_report")
        validate_envelope_binding(envelope, receipt["binding"])
        modern_read_only_retirement_launch_intent(receipt, compatibility)
    except HelperError:
        return False
    if envelope != message or canonical_sha256(message) != authorization["report_sha256"]:
        return False
    if intents:
        try:
            validate_retirement_operation(intents[0], authorization, compatibility)
        except HelperError:
            return False
        if receipt.get("retirement_intent") != intents[0]:
            return False
    elif receipt.get("retirement_intent") is not None:
        return False
    return True


def validate_retirement_operation(
    event: dict[str, Any], authorization: dict[str, str], compatibility: str | None = None
) -> None:
    fields = {
        "event_id", "kind", "terminal_state", "report_sha256",
        "coordinator_authorization_sha256", "identity", "at",
    }
    if compatibility is not None:
        fields.add("compatibility")
    if (
        set(event) != fields
        or event.get("kind") != "retirement_intent"
        or not bounded_utc_timestamp(event.get("at"))
        or any(event.get(key) != authorization[key] for key in authorization)
        or event.get("compatibility") != compatibility
    ):
        raise HelperError("retirement operation conflicts with durable intent")


def valid_pair_retirement_chain(receipt: dict[str, Any]) -> bool:
    events = receipt.get("events")
    intent = receipt.get("retirement_intent")
    result = receipt.get("pair_retired")
    if not isinstance(events, list) or not isinstance(intent, dict) or not isinstance(result, dict):
        return False
    intents = [event for event in events if isinstance(event, dict) and event.get("kind") == "retirement_intent"]
    results = [event for event in events if isinstance(event, dict) and event.get("kind") == "pair_retired"]
    if len(intents) != 1 or len(results) != 1 or intents[0] != intent or results[0] != result:
        return False
    if events[-1] != result or events[-2] != intent:
        return False
    result_fields = {"event_id", "kind", "operation_event_id", "absence_code", "at"}
    if (
        set(result) != result_fields
        or result.get("event_id") != internal_event_id("retirement-result", intent.get("event_id", ""))
        or result.get("operation_event_id") != intent.get("event_id")
        or result.get("absence_code") != "exact_retirement_target_absent"
        or not bounded_utc_timestamp(result.get("at"))
    ):
        return False
    authorization = {
        key: intent.get(key)
        for key in ("terminal_state", "report_sha256", "coordinator_authorization_sha256")
    }
    compatibility = intent.get("compatibility")
    try:
        validate_terminal_amp_authorization(authorization)
        validate_retirement_operation(intent, authorization, compatibility)
        launch_intent = modern_read_only_retirement_launch_intent(receipt, compatibility)
    except HelperError:
        return False
    if not valid_retirement_identity(intent.get("identity"), launch_intent, receipt.get("binding", {})):
        return False
    prior = copy.deepcopy(receipt)
    prior.pop("pair_retired", None)
    prior["events"] = prior["events"][:-1]
    return valid_live_retirement_candidate(prior, authorization, compatibility)


def valid_live_acquired_no_report_retirement_candidate(
    receipt: dict[str, Any],
    authorization: dict[str, str],
    operation_event_id: str,
    compatibility: str | None = None,
) -> bool:
    events = receipt.get("events")
    if (
        not isinstance(events, list)
        or any(not isinstance(event, dict) for event in events)
        or receipt.get("state") != "created"
        or receipt.get("report_message_id") != ""
        or any(key in receipt for key in (
            "input_state", "input_message_id", "worker_detached", "retirement_intent", "pair_retired",
            "acquired_pair_retired", "parked_at", "cleanup_eligible_at", "submission_frozen",
            "handoff_validation",
        ))
    ):
        return False
    kinds = [event.get("kind") for event in events]
    if kinds not in (
        ["created", "launch_intent", "launch_completed", "session_acquired"],
        [
            "created", "launch_intent", "launch_completed", "session_acquired",
            "acquired_retirement_intent",
        ],
    ):
        return False
    created, launch_intent, launch_completed, acquired = events[:4]
    binding = receipt.get("binding", {})
    identity = receipt.get("session_identity")
    common_acquired_identity_fields = {
        "session", "window", "window_id", "pane_id", "pane_pid", "claude_session_id", "workdir",
        "current_command", "process_name", "process_identity", "process_executable_identity",
        "normalized_argv_digest", "process_command_digest", "launch_command_digest",
    }
    acquired_identity_fields = set(identity) if isinstance(identity, dict) else set()
    system = platform.system()
    valid_identity_schema = (
        acquired_identity_fields == common_acquired_identity_fields | {
            "process_executable_object_identity"
        }
        if system == "Darwin"
        else system == "Linux" and acquired_identity_fields in (
            common_acquired_identity_fields,
            common_acquired_identity_fields | {"process_executable_object_identity"},
        )
    )
    intents = [event for event in events if event.get("kind") == "acquired_retirement_intent"]
    receipt_fields = {
        "binding", "routing", "state", "report_message_id", "created_at", "updated_at", "events",
        "session_identity",
    }
    if intents:
        receipt_fields.add("acquired_retirement_intent")
    if (
        set(receipt) != receipt_fields
        or set(created) != {"event_id", "kind", "at", "routing"}
        or created.get("event_id") != f"create:{binding.get('delegation_id', '')}"
        or not bounded_utc_timestamp(created.get("at"))
        or receipt.get("created_at") != created.get("at")
        or receipt.get("updated_at") != events[-1].get("at")
        or set(launch_completed) != {
            "event_id", "kind", "operation_event_id", "identity", "at",
        }
        or launch_completed.get("event_id")
        != internal_event_id("launch-result", launch_intent.get("event_id", ""))
        or launch_completed.get("operation_event_id") != launch_intent.get("event_id")
        or not bounded_utc_timestamp(launch_completed.get("at"))
        or set(acquired) != {"event_id", "kind", "identity", "at"}
        or not bounded_utc_timestamp(acquired.get("at"))
        or not isinstance(identity, dict)
        or not valid_identity_schema
        or launch_completed.get("identity") != identity
        or acquired.get("identity") != identity
    ):
        return False
    try:
        protocol_id(acquired, "event_id")
        routing = validate_routing(receipt.get("routing"))
        if routing != receipt.get("routing") or created.get("routing") != routing:
            return False
        modern_read_only_retirement_launch_intent(receipt, compatibility)
    except HelperError:
        return False
    if intents:
        try:
            validate_acquired_retirement_operation(
                intents[0], authorization, operation_event_id, compatibility
            )
        except HelperError:
            return False
        if receipt.get("acquired_retirement_intent") != intents[0]:
            return False
    elif receipt.get("acquired_retirement_intent") is not None:
        return False
    return True


def valid_pre_identity_acquired_no_report_candidate(receipt: dict[str, Any]) -> bool:
    events = receipt.get("events")
    if (
        not isinstance(events, list)
        or any(not isinstance(event, dict) for event in events)
        or [event.get("kind") for event in events]
        != ["created", "launch_intent", "launch_completed", "session_acquired"]
        or receipt.get("state") != "created"
        or receipt.get("report_message_id") != ""
        or set(receipt) != {
            "binding", "routing", "state", "report_message_id", "created_at", "updated_at", "events",
            "session_identity",
        }
    ):
        return False
    created, launch_intent, launch_completed, acquired = events
    binding = receipt.get("binding", {})
    identity = receipt.get("session_identity")
    historical_identity_fields = {
        "claude_session_id", "session", "window", "window_id", "pane_id", "pane_pid", "workdir",
        "current_command", "process_name", "process_identity", "process_command_digest",
        "launch_command_digest",
    }
    if (
        set(created) != {"event_id", "kind", "at", "routing"}
        or created.get("event_id") != f"create:{binding.get('delegation_id', '')}"
        or not bounded_utc_timestamp(created.get("at"))
        or receipt.get("created_at") != created.get("at")
        or receipt.get("updated_at") != acquired.get("at")
        or set(launch_completed)
        != {"event_id", "kind", "operation_event_id", "identity", "at"}
        or launch_completed.get("event_id")
        != internal_event_id("launch-result", launch_intent.get("event_id", ""))
        or launch_completed.get("operation_event_id") != launch_intent.get("event_id")
        or not bounded_utc_timestamp(launch_completed.get("at"))
        or not isinstance(launch_completed.get("identity"), dict)
        or set(launch_completed["identity"]) != {"session", "window", "window_id", "pane_id"}
        or set(acquired) != {"event_id", "kind", "identity", "at"}
        or not bounded_utc_timestamp(acquired.get("at"))
        or not isinstance(identity, dict)
        or set(identity) != historical_identity_fields
        or acquired.get("identity") != identity
        or any(
            not isinstance(identity.get(key), str) or not identity[key]
            for key in historical_identity_fields - {"pane_pid"}
        )
        or type(identity.get("pane_pid")) is not int
        or identity["pane_pid"] <= 0
        or any(
            launch_completed["identity"].get(key) != identity.get(key)
            for key in ("session", "window", "window_id", "pane_id")
        )
        or not re.fullmatch(r"@[0-9]+", identity["window_id"])
        or not re.fullmatch(r"%[0-9]+", identity["pane_id"])
        or any(
            len(identity[key]) != 64
            or any(character not in "0123456789abcdef" for character in identity[key])
            for key in ("process_command_digest", "launch_command_digest")
        )
    ):
        return False
    try:
        protocol_id(acquired, "event_id")
        protocol_id(identity, "claude_session_id")
        for key in ("session", "window", "window_id", "pane_id", "current_command", "process_name", "process_identity"):
            required_string(identity, key, 256)
        required_string(identity, "workdir", 2048)
        intent = pre_identity_launch_intent(receipt)
        if validate_binding(binding) != binding:
            return False
        routing = validate_routing(receipt.get("routing"))
    except (HelperError, KeyError, OSError):
        return False
    return (
        routing == receipt.get("routing")
        and created.get("routing") == routing
        and identity["session"] == intent["tmux_session"]
        and identity["window"] == intent["tmux_window"]
        and identity["claude_session_id"] == intent["claude_session_id"]
        and identity["workdir"] == binding.get("workdir")
        and identity["launch_command_digest"] == binding.get("launch_command_digest")
    )


def validate_acquired_retirement_operation(
    event: dict[str, Any],
    authorization: dict[str, str],
    operation_event_id: str,
    compatibility: str | None = None,
) -> None:
    fields = {
        "event_id", "kind", "terminal_state", "report_sha256",
        "coordinator_authorization_sha256", "identity", "at",
    }
    if compatibility is not None:
        fields.add("compatibility")
    if (
        set(event) != fields
        or event.get("kind") != "acquired_retirement_intent"
        or event.get("event_id") != operation_event_id
        or not bounded_utc_timestamp(event.get("at"))
        or any(event.get(key) != authorization[key] for key in authorization)
        or event.get("compatibility") != compatibility
    ):
        raise HelperError("acquired retirement operation conflicts with durable intent")


def valid_acquired_no_report_pair_retirement_chain(receipt: dict[str, Any]) -> bool:
    events = receipt.get("events")
    intent = receipt.get("acquired_retirement_intent")
    result = receipt.get("acquired_pair_retired")
    if not isinstance(events, list) or not isinstance(intent, dict) or not isinstance(result, dict):
        return False
    intents = [
        event for event in events
        if isinstance(event, dict) and event.get("kind") == "acquired_retirement_intent"
    ]
    results = [
        event for event in events
        if isinstance(event, dict) and event.get("kind") == "acquired_pair_retired"
    ]
    if len(intents) != 1 or len(results) != 1 or intents[0] != intent or results[0] != result:
        return False
    if events[-1] != result or events[-2] != intent:
        return False
    if set(receipt) != {
        "binding", "routing", "state", "report_message_id", "created_at", "updated_at", "events",
        "session_identity", "acquired_retirement_intent", "acquired_pair_retired",
    }:
        return False
    result_fields = {"event_id", "kind", "operation_event_id", "absence_code", "at"}
    if (
        set(result) != result_fields
        or result.get("event_id")
        != internal_event_id("acquired-retirement-result", intent.get("event_id", ""))
        or result.get("operation_event_id") != intent.get("event_id")
        or result.get("absence_code") != "exact_retirement_target_absent"
        or not bounded_utc_timestamp(result.get("at"))
        or receipt.get("updated_at") != result.get("at")
    ):
        return False
    authorization = {
        key: intent.get(key)
        for key in ("terminal_state", "report_sha256", "coordinator_authorization_sha256")
    }
    compatibility = intent.get("compatibility")
    try:
        validate_terminal_amp_authorization(authorization)
        validate_acquired_retirement_operation(
            intent, authorization, intent.get("event_id", ""), compatibility
        )
        launch_intent = modern_read_only_retirement_launch_intent(receipt, compatibility)
    except HelperError:
        return False
    if not retirement_identity_matches_acquired_identity(
        intent.get("identity"), receipt.get("session_identity"), launch_intent,
        receipt.get("binding", {}),
    ):
        return False
    prior = copy.deepcopy(receipt)
    prior.pop("acquired_pair_retired", None)
    prior["events"] = prior["events"][:-1]
    prior["updated_at"] = intent["at"]
    return valid_live_acquired_no_report_retirement_candidate(
        prior, authorization, intent["event_id"], compatibility
    )


def pair_retirement_result(
    origin_sha256: str, pair_sha256: str, outcome: str, compatibility: str | None = None
) -> dict[str, str]:
    result = {
        "action": "live_indeterminate_pair_retirement",
        "origin_thread_sha256": origin_sha256,
        "pair_sha256": pair_sha256,
        "outcome": outcome,
        "absence_code": "exact_retirement_target_absent",
        "fence": "retained",
    }
    if compatibility is not None:
        result["compatibility"] = compatibility
    return result


def pair_retirement_blocked(origin_sha256: str, pair_sha256: str, blocker: str) -> dict[str, str]:
    return {
        "action": "live_indeterminate_pair_retirement",
        "origin_thread_sha256": origin_sha256,
        "pair_sha256": pair_sha256,
        "outcome": "blocked",
        "blocker": blocker,
        "fence": "retained",
    }


def acquired_pair_retirement_result(
    origin_sha256: str, pair_sha256: str, outcome: str, compatibility: str | None = None
) -> dict[str, str]:
    result = {
        "action": "live_acquired_no_report_pair_retirement",
        "origin_thread_sha256": origin_sha256,
        "pair_sha256": pair_sha256,
        "outcome": outcome,
        "absence_code": "exact_retirement_target_absent",
        "fence": "retained",
    }
    if compatibility is not None:
        result["compatibility"] = compatibility
    return result


def acquired_pair_retirement_blocked(
    origin_sha256: str, pair_sha256: str, blocker: str
) -> dict[str, str]:
    return {
        "action": "live_acquired_no_report_pair_retirement",
        "origin_thread_sha256": origin_sha256,
        "pair_sha256": pair_sha256,
        "outcome": "blocked",
        "blocker": blocker,
        "fence": "retained",
    }


def pre_identity_acquired_permanent_terminal_result(
    origin_sha256: str, pair_sha256: str
) -> dict[str, str]:
    return {
        "action": "live_acquired_no_report_pair_retirement",
        "origin_thread_sha256": origin_sha256,
        "pair_sha256": pair_sha256,
        "outcome": "blocked",
        "blocker": "pre_identity_acquired_pair_permanently_non_retirable",
        "policy": "preserve_receipt_runtime_artifacts_and_origin_fence",
        "remediation": "paired_worker_teardown_prohibited",
        "compatibility": PRE_IDENTITY_ACQUIRED_NO_REPORT_COMPATIBILITY,
        "fence": "retained",
    }


def worker_detach_result(
    origin_sha256: str,
    pair_sha256: str,
    outcome: str,
    absence_code: str = "exact_launch_target_absent",
    compatibility: str | None = None,
) -> dict[str, Any]:
    result = {
        "action": "indeterminate_worker_detach",
        "origin_thread_sha256": origin_sha256,
        "pair_sha256": pair_sha256,
        "outcome": outcome,
        "absence_code": absence_code,
        "fence": "retained",
    }
    if compatibility is not None:
        result["compatibility"] = compatibility
    return result


def valid_retirement_identity(
    identity: Any, launch_intent: dict[str, Any], binding: dict[str, Any]
) -> bool:
    required_strings = {
        "session", "session_id", "window", "window_id", "pane_id", "claude_session_id", "workdir",
        "current_command", "process_name", "process_identity", "process_start_identity",
        "process_executable_identity",
        "process_executable_object_identity", "normalized_argv_digest", "process_command_digest",
        "launch_command_digest", "expected_launcher_identity", "expected_executable_object_identity",
    }
    expected_fields = required_strings | {"pane_pid"}
    if launch_intent.get("expected_launcher_argv0_digest") is not None:
        expected_fields.add("expected_launcher_argv0_digest")
    if (
        not isinstance(identity, dict)
        or set(identity) != expected_fields
        or any(not isinstance(identity.get(key), str) or not identity[key] for key in required_strings)
        or not isinstance(identity.get("pane_pid"), int)
        or identity["pane_pid"] <= 0
    ):
        return False
    try:
        workdir = str(pathlib.Path(binding["workdir"]).resolve(strict=True))
    except (KeyError, OSError):
        return False
    return (
        identity["session"] == launch_intent.get("tmux_session")
        and identity["window"] == launch_intent.get("tmux_window")
        and identity["claude_session_id"] == launch_intent.get("claude_session_id")
        and identity["normalized_argv_digest"] == launch_intent.get("expected_argv_digest")
        and identity["process_start_identity"]
        == process_start_identity_from_process_identity(identity["process_identity"])
        and identity["expected_launcher_identity"] == launch_intent.get("expected_launcher_identity")
        and identity["expected_executable_object_identity"]
        == launch_intent.get("expected_executable_object_identity")
        and identity.get("expected_launcher_argv0_digest")
        == launch_intent.get("expected_launcher_argv0_digest")
        and identity["workdir"] == workdir
        and identity["launch_command_digest"] == binding.get("launch_command_digest")
    )


def retirement_identity_matches_acquired_identity(
    retirement_identity: Any,
    acquired_identity: Any,
    launch_intent: dict[str, Any],
    binding: dict[str, Any],
) -> bool:
    if (
        not valid_retirement_identity(retirement_identity, launch_intent, binding)
        or not isinstance(acquired_identity, dict)
    ):
        return False
    retirement_only_fields = {
        "session_id", "process_start_identity", "expected_launcher_identity",
        "expected_executable_object_identity", "expected_launcher_argv0_digest",
    }
    return (
        set(acquired_identity).issubset(retirement_identity)
        and not set(acquired_identity).intersection(retirement_only_fields)
        and all(retirement_identity.get(key) == value for key, value in acquired_identity.items())
    )


def retirement_private_executable_path(
    store: ReceiptStore, delegation_id: str, compatibility: str | None
) -> pathlib.Path | None:
    system = platform.system()
    if compatibility == HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY and system != "Darwin":
        raise HelperError("historical modern retirement compatibility requires Darwin")
    if system != "Darwin":
        return None
    return private_launch_transport_path(store, delegation_id).with_name("verified-claude")


def inspect_live_indeterminate_target(
    launch_intent: dict[str, Any],
    binding: dict[str, Any],
    store: ReceiptStore,
    delegation_id: str,
    compatibility: str | None = None,
    expected_private_executable_path: pathlib.Path | None = None,
) -> dict[str, Any]:
    if compatibility not in (None, HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY):
        raise HelperError("retirement inspection compatibility is unsupported")
    if compatibility == HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY:
        if platform.system() != "Darwin" or expected_private_executable_path is None:
            raise HelperError("historical modern retirement requires the exact Darwin private route")
    try:
        output = read_bounded_command([
            "tmux", "list-panes", "-a", "-F",
            "#{session_name}\t#{session_id}\t#{window_name}\t#{window_id}\t#{pane_id}",
        ], MAX_ABSENCE_OUTPUT_BYTES)
    except HelperError as error:
        raise HelperError("retirement tmux inspection is unavailable") from error
    rows = output.splitlines() if output else []
    if len(rows) > MAX_ABSENCE_PANES:
        raise HelperError("retirement tmux inspection is ambiguous")
    candidates: list[tuple[str, str, str]] = []
    pane_ids: set[str] = set()
    for row in rows:
        fields = row.split("\t")
        if (
            len(fields) != 5
            or not fields[0]
            or not fields[1].startswith("$")
            or not fields[2]
            or not fields[3].startswith("@")
            or not fields[4].startswith("%")
            or fields[4] in pane_ids
        ):
            raise HelperError("retirement tmux inspection is ambiguous")
        pane_ids.add(fields[4])
        if fields[0] == launch_intent.get("tmux_session") and fields[2] == launch_intent.get("tmux_window"):
            candidates.append((fields[1], fields[3], fields[4]))
    if len(candidates) != 1:
        raise HelperError("retirement requires one exact live launch target")
    session_id, window_id, pane_id = candidates[0]
    expected_process_object: str | None = None
    expected_process_path = expected_private_executable_path
    if platform.system() == "Darwin":
        if expected_process_path is None:
            expected_process_path = retirement_private_executable_path(store, delegation_id, compatibility)
        try:
            descriptor, _, _, expected_process_object = open_exact_verified_executable(
                expected_process_path
            )
            os.close(descriptor)
        except (OSError, RuntimeError) as error:
            raise HelperError("retirement private executable identity is unavailable") from error
    identity = inspect_claude_identity(
        pane_id,
        launch_intent["claude_session_id"],
        launch_intent["expected_argv_digest"],
        launch_intent["expected_launcher_identity"],
        launch_intent["expected_executable_object_identity"],
        expected_process_executable_object_identity=expected_process_object,
        expected_launcher_argv0_digest=launch_intent.get("expected_launcher_argv0_digest"),
        include_process_executable_object_identity=True,
        allow_missing_launcher_argv0_digest=(
            compatibility == HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY
        ),
    )
    if identity.get("window_id") != window_id:
        raise HelperError("retirement tmux target changed during inspection")
    if expected_process_path is not None and process_executable_path(identity["pane_pid"]) != expected_process_path:
        raise HelperError("retirement process is not the exact private executable route")
    identity["expected_launcher_identity"] = launch_intent["expected_launcher_identity"]
    identity["expected_executable_object_identity"] = launch_intent["expected_executable_object_identity"]
    identity["session_id"] = session_id
    identity["process_start_identity"] = process_start_identity_from_process_identity(
        identity["process_identity"]
    )
    if launch_intent.get("expected_launcher_argv0_digest") is not None:
        identity["expected_launcher_argv0_digest"] = launch_intent["expected_launcher_argv0_digest"]
    if not valid_retirement_identity(identity, launch_intent, binding):
        raise HelperError("retirement target does not match complete launch identity")
    return identity


def stop_exact_retirement_target(
    identity: dict[str, Any],
    expected_private_executable_path: pathlib.Path | None = None,
    compatibility: str | None = None,
) -> None:
    if compatibility not in (None, HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY):
        raise HelperError("retirement stop compatibility is unsupported")
    if platform.system() == "Darwin" and expected_private_executable_path is None:
        raise HelperError("Darwin retirement stop requires the exact private executable route")
    if compatibility == HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY:
        if platform.system() != "Darwin" or expected_private_executable_path is None:
            raise HelperError("historical modern retirement stop requires Darwin private authority")
    formats = [
        "session_name", "session_id", "window_name", "window_id", "pane_id", "pane_pid",
        "pane_current_path", "pane_current_command", "pane_start_command",
    ]
    with TmuxControlConnection(identity["session"]) as control:
        values: list[str] = []
        for format_value in formats:
            token = secrets.token_hex(32)
            response = control.command([
                "display-message", "-p", "-t", identity["pane_id"],
                f"{token}:{tmux_single_line_format(format_value)}",
            ])
            prefix = f"{token}:"
            if len(response) != 1 or not response[0].startswith(prefix):
                raise HelperError("retirement tmux control snapshot is ambiguous")
            values.append(decode_tmux_command_argument(response[0][len(prefix) :]))
        tmux_fields = [values[index] for index in (0, 2, 3, 4, 5, 6, 7, 8)]
        current = inspect_claude_identity(
            identity["pane_id"],
            identity["claude_session_id"],
            identity["normalized_argv_digest"],
            identity["expected_launcher_identity"],
            identity["expected_executable_object_identity"],
            identity["process_executable_identity"],
            identity["process_executable_object_identity"],
            identity.get("expected_launcher_argv0_digest"),
            tmux_fields=tmux_fields,
            include_process_executable_object_identity=True,
            allow_missing_launcher_argv0_digest=(
                compatibility == HISTORICAL_MODERN_READ_ONLY_LAUNCH_INTENT_COMPATIBILITY
            ),
        )
        if (
            expected_private_executable_path is not None
            and process_executable_path(current["pane_pid"])
            != expected_private_executable_path
        ):
            raise HelperError("retirement process changed its exact private executable route")
        current["session_id"] = values[1]
        current["process_start_identity"] = process_start_identity_from_process_identity(
            current["process_identity"]
        )
        current["expected_launcher_identity"] = identity["expected_launcher_identity"]
        current["expected_executable_object_identity"] = identity["expected_executable_object_identity"]
        if identity.get("expected_launcher_argv0_digest") is not None:
            current["expected_launcher_argv0_digest"] = identity["expected_launcher_argv0_digest"]
        if current != identity:
            raise HelperError("retirement target changed immediately before exact stop")
        kill_token = secrets.token_hex(32)
        kill_responses = control.command_sequence([
            ["kill-pane", "-t", identity["pane_id"]],
            ["display-message", "-p", f"retirement-kill:{kill_token}"],
        ])
        if kill_responses != [[], [f"retirement-kill:{kill_token}"]]:
            raise HelperError("retirement tmux exact stop returned ambiguous output")


def process_pid_is_absent(pid: int) -> bool:
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return True
    except (PermissionError, OSError):
        return False
    return False


def confirm_retirement_target_absent(identity: dict[str, Any]) -> str:
    deadline = time.monotonic() + 2.0
    confirmed = 0
    while time.monotonic() < deadline:
        try:
            panes = read_bounded_command(
                ["tmux", "list-panes", "-a", "-F", "#{pane_id}"], MAX_ABSENCE_OUTPUT_BYTES
            ).splitlines()
        except HelperError:
            return "tmux_inspection_unavailable"
        if (
            len(panes) > MAX_ABSENCE_PANES
            or any(len(pane) < 2 or pane[0] != "%" or not pane[1:].isdigit() for pane in panes)
            or len(panes) != len(set(panes))
        ):
            return "tmux_inspection_ambiguous"
        if identity["pane_id"] in panes:
            return "retirement_target_still_live"
        try:
            name, process_identity, _, command_digest = exact_process_identity(identity["pane_pid"])
            executable_identity = process_executable_identity(identity["pane_pid"])
        except HelperError:
            if not process_pid_is_absent(identity["pane_pid"]):
                return "process_inspection_unavailable"
            confirmed += 1
            if confirmed >= 2:
                return "exact_retirement_target_absent"
            time.sleep(STARTUP_POLL_SECONDS)
            continue
        current_start_identity = process_start_identity_from_process_identity(process_identity)
        if current_start_identity == identity["process_start_identity"]:
            if (
                process_identity == identity["process_identity"]
                and name == identity["process_name"]
                and command_digest == identity["process_command_digest"]
                and executable_identity == identity["process_executable_identity"]
            ):
                return "retirement_target_still_live"
            return "retirement_target_identity_changed_after_stop"
        confirmed += 1
        if confirmed >= 2:
            return "exact_retirement_target_absent"
        time.sleep(STARTUP_POLL_SECONDS)
    return "retirement_absence_unconfirmed"


def inspect_pre_identity_launch_absence(intent: dict[str, Any]) -> str:
    session = intent["tmux_session"]
    window = intent["tmux_window"]
    claude_session_id = intent["claude_session_id"]
    try:
        output = read_bounded_command([
            "tmux", "list-panes", "-a", "-F",
            "#{session_name}\t#{window_name}\t#{pane_id}",
        ], MAX_ABSENCE_OUTPUT_BYTES)
    except HelperError:
        return "tmux_inspection_unavailable"
    rows = output.splitlines() if output else []
    if len(rows) > MAX_ABSENCE_PANES:
        return "tmux_inspection_ambiguous"
    pane_ids: set[str] = set()
    for row in rows:
        fields = row.split("\t")
        if (
            len(fields) != 3
            or not fields[0]
            or not fields[1]
            or len(fields[2]) < 2
            or fields[2][0] != "%"
            or not fields[2][1:].isdigit()
            or fields[2] in pane_ids
        ):
            return "tmux_inspection_ambiguous"
        pane_ids.add(fields[2])
        if fields[0] == session and fields[1] == window:
            return "matching_legacy_target"

    process_ids, blocker = owner_process_ids()
    if blocker is not None:
        return blocker
    inaccessible: list[int] = []
    for pid in process_ids:
        try:
            _, _, process_args, _ = exact_process_identity(pid)
        except HelperError:
            inaccessible.append(pid)
            continue
        if any(
            index + 1 < len(process_args)
            and value == "--session-id"
            and process_args[index + 1] == claude_session_id
            for index, value in enumerate(process_args)
        ):
            return "matching_legacy_session"
    if inaccessible:
        current, current_blocker = owner_process_ids()
        if current_blocker is not None:
            return current_blocker
        current_processes = set(current)
        if any(pid in current_processes for pid in inaccessible):
            return "process_inspection_unavailable"
    return "legacy_target_and_session_absent"


def inspect_indeterminate_launch_absence(intent: dict[str, Any]) -> str:
    session = intent.get("tmux_session")
    window = intent.get("tmux_window")
    claude_session_id = intent.get("claude_session_id")
    if any(not isinstance(value, str) or not value for value in (session, window, claude_session_id)):
        return "launch_identity_unavailable"
    try:
        output = read_bounded_command([
            "tmux", "list-panes", "-a", "-F",
            "#{session_name}\t#{window_name}\t#{pane_id}",
        ], MAX_ABSENCE_OUTPUT_BYTES)
    except HelperError:
        return "tmux_inspection_unavailable"
    rows = output.splitlines() if output else []
    if len(rows) > MAX_ABSENCE_PANES:
        return "tmux_inspection_ambiguous"
    candidates: list[str] = []
    for row in rows:
        fields = row.split("\t")
        if len(fields) != 3 or not fields[2].startswith("%"):
            return "tmux_inspection_ambiguous"
        if fields[0] == session and fields[1] == window:
            candidates.append(fields[2])
    if not candidates:
        return inspect_indeterminate_process_absence(intent)
    for pane_id in candidates:
        try:
            inspect_claude_identity(
                pane_id,
                claude_session_id,
                intent["expected_argv_digest"],
                intent["expected_launcher_identity"],
                intent["expected_executable_object_identity"],
                expected_launcher_argv0_digest=intent.get("expected_launcher_argv0_digest"),
            )
        except (HelperError, KeyError):
            return "launch_identity_ambiguous_or_mismatched"
        return "matching_live_process"
    return inspect_indeterminate_process_absence(intent)


def owner_process_ids() -> tuple[list[int], str | None]:
    try:
        output = read_bounded_command(
            ["ps", "-U", str(os.geteuid()), "-o", "pid="], MAX_ABSENCE_OUTPUT_BYTES
        )
    except HelperError:
        return [], "process_inspection_unavailable"
    rows = output.splitlines() if output else []
    if len(rows) > MAX_ABSENCE_PROCESSES:
        return [], "process_inspection_ambiguous"
    try:
        process_ids = [int(row.strip()) for row in rows]
    except ValueError:
        return [], "process_inspection_ambiguous"
    if any(pid <= 0 for pid in process_ids) or len(process_ids) != len(set(process_ids)):
        return [], "process_inspection_ambiguous"
    return process_ids, None


def inspect_indeterminate_process_absence(intent: dict[str, Any]) -> str:
    process_ids, blocker = owner_process_ids()
    if blocker is not None:
        return blocker
    inaccessible: list[int] = []
    for pid in process_ids:
        try:
            process_name, _, process_args, _ = exact_process_identity(pid)
        except HelperError:
            inaccessible.append(pid)
            continue
        session_positions = [index for index, value in enumerate(process_args) if value == "--session-id"]
        matching_session = any(
            index + 1 < len(process_args) and process_args[index + 1] == intent["claude_session_id"]
            for index in session_positions
        )
        if not matching_session:
            continue
        try:
            _, normalized_arguments = normalized_claude_arguments(
                platform.system(), process_name, process_args, process_executable_path(pid)
            )
        except HelperError:
            return "launch_identity_ambiguous_or_mismatched"
        if normalized_argv_digest(normalized_arguments) != intent["expected_argv_digest"]:
            return "launch_identity_ambiguous_or_mismatched"
        return "matching_live_process"
    if inaccessible:
        current, current_blocker = owner_process_ids()
        if current_blocker is not None:
            return current_blocker
        if any(pid in set(current) for pid in inaccessible):
            return "process_inspection_unavailable"
    return "exact_launch_target_absent"


def worker_teardown_store_blocked(origin_thread: str, dry_run: bool) -> dict[str, Any]:
    return {
        "action": "worker_teardown",
        "origin_thread_sha256": hashlib.sha256(origin_thread.encode()).hexdigest(),
        "outcome": "blocked",
        "dry_run": dry_run,
        "pairs": [],
        "blockers": [{"blocker": "receipt_store_invalid_or_unavailable"}],
        "recovery": "repair the owner-private lifecycle registry or receipt store before retrying worker teardown",
    }


def valid_worker_lifecycle_chain(receipt: dict[str, Any], parked: bool) -> bool:
    events = receipt.get("events")
    if not isinstance(events, list) or any(not isinstance(event, dict) for event in events):
        return False

    def exactly_one(kind: str) -> tuple[int, dict[str, Any]] | None:
        matches = [(index, event) for index, event in enumerate(events) if event.get("kind") == kind]
        return matches[0] if len(matches) == 1 else None

    launch_intent = exactly_one("launch_intent")
    launch_completed = exactly_one("launch_completed")
    acquired = exactly_one("session_acquired")
    if launch_intent is None or launch_completed is None or acquired is None:
        return False
    if not (launch_intent[0] < launch_completed[0] < acquired[0]):
        return False
    if launch_completed[1].get("operation_event_id") != launch_intent[1].get("event_id"):
        return False
    identity = receipt.get("session_identity")
    if (
        not isinstance(identity, dict)
        or launch_completed[1].get("identity") != identity
        or acquired[1].get("identity") != identity
    ):
        return False

    message_id = receipt.get("report_message_id")
    reports = [
        (index, event) for index, event in enumerate(events)
        if event.get("kind") == "valid_report" and event.get("event_id") == message_id
    ]
    deliveries = [
        (index, event) for index, event in enumerate(events)
        if event.get("kind") == "delivered" and event.get("message_id") == message_id
    ]
    acknowledgements = [
        (index, event) for index, event in enumerate(events)
        if event.get("kind") == "acknowledged" and event.get("message_id") == message_id
    ]
    if len(reports) != 1 or len(deliveries) != 1 or len(acknowledgements) != 1:
        return False
    if not (acquired[0] < reports[0][0] < deliveries[0][0] < acknowledgements[0][0]):
        return False
    if not parked:
        return receipt.get("state") == "acknowledged"

    parked_results = [
        (index, event) for index, event in enumerate(events)
        if event.get("kind") == "verified_parked"
    ]
    if len(parked_results) != 1 or parked_results[0][0] <= acknowledgements[0][0]:
        return False
    operation_event_id = parked_results[0][1].get("operation_event_id")
    matching_intents = [
        (index, event) for index, event in enumerate(events)
        if event.get("kind") == "park_intent" and event.get("event_id") == operation_event_id
    ]
    if (
        len(matching_intents) != 1
        or not (acknowledgements[0][0] < matching_intents[0][0] < parked_results[0][0])
        or matching_intents[0][1].get("identity") != identity
        or receipt.get("parked_at") != parked_results[0][1].get("at")
    ):
        return False
    try:
        parked_at = datetime.fromisoformat(receipt["parked_at"].replace("Z", "+00:00"))
        cleanup_at = datetime.fromisoformat(receipt["cleanup_eligible_at"].replace("Z", "+00:00"))
    except (KeyError, TypeError, ValueError):
        return False
    return receipt.get("state") == "verified_parked" and cleanup_at - parked_at >= timedelta(days=30)


def event_without_time(event: dict[str, Any]) -> dict[str, Any]:
    return {key: value for key, value in event.items() if key != "at"}


def default_state_dir() -> pathlib.Path:
    return pathlib.Path.home() / "Library" / "Application Support" / "amux" / "experimental" / "claude-delegation"


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description=__doc__)
    root.add_argument("--state-dir", type=pathlib.Path, default=default_state_dir())
    root.add_argument("--isolated-test-state", action="store_true", help=argparse.SUPPRESS)
    commands = root.add_subparsers(dest="area", required=True)
    receipt = commands.add_parser("receipt")
    receipt_commands = receipt.add_subparsers(dest="command", required=True)
    receipt_commands.add_parser("create")
    receipt_commands.add_parser("route")
    show = receipt_commands.add_parser("show")
    show.add_argument("--delegation-id", required=True)
    report = commands.add_parser("report")
    report_commands = report.add_subparsers(dest="command", required=True)
    report_commands.add_parser("submit")
    report_commands.add_parser("acknowledge")
    inbox = commands.add_parser("inbox")
    inbox_commands = inbox.add_subparsers(dest="command", required=True)
    inbox_commands.add_parser("consume")
    input_parser = commands.add_parser("input")
    input_commands = input_parser.add_subparsers(dest="command", required=True)
    input_commands.add_parser("submit")
    input_commands.add_parser("accept")
    session = commands.add_parser("session")
    session_commands = session.add_subparsers(dest="command", required=True)
    session_commands.add_parser("acquire")
    session_commands.add_parser("park")
    mcp = commands.add_parser("mcp")
    mcp_commands = mcp.add_subparsers(dest="command", required=True)
    serve = mcp_commands.add_parser("serve")
    serve.add_argument("--delegation-id", required=True)
    amp = commands.add_parser("amp")
    amp_commands = amp.add_subparsers(dest="command", required=True)
    inspect = amp_commands.add_parser("inspect")
    inspect.add_argument("--pane", required=True)
    inspect.add_argument("--origin-thread", required=True)
    notify = commands.add_parser("notify")
    notify_commands = notify.add_subparsers(dest="command", required=True)
    notify_commands.add_parser("amp-pane")
    launch = commands.add_parser("launch")
    launch_commands = launch.add_subparsers(dest="command", required=True)
    launch_commands.add_parser("policy-digest")
    launch_commands.add_parser("plan")
    launch_commands.add_parser("execute")
    transport = launch_commands.add_parser("transport")
    transport.add_argument("--delegation-id", required=True)
    transport.add_argument("--transport-sha256", required=True)
    transport.add_argument("--tmux-environment-sha256", required=True)
    capacity = commands.add_parser("capacity")
    capacity_commands = capacity.add_subparsers(dest="command", required=True)
    capacity_commands.add_parser("decide-mutating")
    mutation = commands.add_parser("mutation")
    mutation_commands = mutation.add_subparsers(dest="command", required=True)
    mutation_commands.add_parser("prepare")
    mutation_commands.add_parser("validate-handoff")
    lifecycle = commands.add_parser("lifecycle")
    lifecycle_commands = lifecycle.add_subparsers(dest="command", required=True)
    worker_teardown = lifecycle_commands.add_parser("worker-teardown")
    worker_teardown.add_argument("--origin-thread", required=True)
    worker_teardown.add_argument("--dry-run", action="store_true")
    worker_teardown_release = lifecycle_commands.add_parser("worker-teardown-release")
    worker_teardown_release.add_argument("--origin-thread", required=True)
    register_legacy = lifecycle_commands.add_parser("register-legacy-store")
    register_legacy.add_argument("--origin-thread", required=True)
    register_legacy.add_argument("--store-path", type=pathlib.Path, required=True)
    lifecycle_commands.add_parser("detach-indeterminate-worker")
    lifecycle_commands.add_parser("retire-live-indeterminate-pair")
    lifecycle_commands.add_parser("retire-live-acquired-no-report-pair")
    commands.add_parser("diagnose")
    return root


def main() -> int:
    arguments = parser().parse_args()
    state_dir = arguments.state_dir.expanduser().resolve()
    if arguments.isolated_test_state:
        if os.environ.get("AMUX_CLAUDE_DELEGATION_TESTING") != "1":
            raise HelperError("isolated test state is unavailable outside the test harness")
        lifecycle_state_dir = state_dir
    else:
        lifecycle_state_dir = default_state_dir().expanduser().resolve()
    store = ReceiptStore(state_dir, lifecycle_state_dir)
    if arguments.area == "mcp" and arguments.command == "serve":
        return serve_mcp(store, arguments.delegation_id)
    if arguments.area == "launch" and arguments.command == "transport":
        execute_launch_transport(
            store,
            arguments.delegation_id,
            arguments.transport_sha256,
            arguments.tmux_environment_sha256,
        )
        raise HelperError("private launch transport returned without executing Claude")
    if arguments.area == "amp" and arguments.command == "inspect":
        print(json.dumps(inspect_amp_target(arguments.pane, arguments.origin_thread), sort_keys=True, separators=(",", ":")))
        return 0
    if arguments.area == "lifecycle" and arguments.command == "worker-teardown":
        result = store.worker_teardown(arguments.origin_thread, arguments.dry_run)
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2 if result["outcome"] == "blocked" else 0
    if arguments.area == "lifecycle" and arguments.command == "worker-teardown-release":
        result = store.release_worker_teardown(arguments.origin_thread)
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2 if result["outcome"] == "blocked" else 0
    if arguments.area == "lifecycle" and arguments.command == "register-legacy-store":
        try:
            result = store.register_legacy_store(arguments.origin_thread, arguments.store_path)
        except HelperError:
            result = {
                "action": "legacy_store_registration",
                "origin_thread_sha256": hashlib.sha256(arguments.origin_thread.encode()).hexdigest(),
                "outcome": "blocked",
                "blocker": "registration_evidence_invalid_or_unavailable",
            }
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2 if result["outcome"] == "blocked" else 0
    if arguments.area == "lifecycle" and arguments.command == "detach-indeterminate-worker":
        try:
            result = store.detach_indeterminate_worker(read_input())
        except HelperError:
            result = {
                "action": "indeterminate_worker_detach",
                "outcome": "blocked",
                "blocker": "detach_proof_invalid_or_unavailable",
                "fence": "unchanged_or_retained",
            }
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2 if result["outcome"] == "blocked" else 0
    if arguments.area == "lifecycle" and arguments.command == "retire-live-indeterminate-pair":
        try:
            result = store.retire_live_indeterminate_pair(read_input())
        except HelperError:
            result = {
                "action": "live_indeterminate_pair_retirement",
                "outcome": "blocked",
                "blocker": "retirement_proof_invalid_or_unavailable",
                "fence": "unchanged_or_retained",
            }
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2 if result["outcome"] == "blocked" else 0
    if arguments.area == "lifecycle" and arguments.command == "retire-live-acquired-no-report-pair":
        try:
            result = store.retire_live_acquired_no_report_pair(read_input())
        except HelperError:
            result = {
                "action": "live_acquired_no_report_pair_retirement",
                "outcome": "blocked",
                "blocker": "retirement_proof_invalid_or_unavailable",
                "fence": "unchanged_or_retained",
            }
        print(json.dumps(result, sort_keys=True, separators=(",", ":")))
        return 2 if result["outcome"] == "blocked" else 0
    if arguments.area == "diagnose":
        output: Any = diagnostics()
    elif arguments.area == "receipt" and arguments.command == "show":
        output = store.show(arguments.delegation_id)
    else:
        request = read_input()
        if arguments.area == "capacity" and arguments.command == "decide-mutating":
            output = decide_mutating_capacity(request)
        elif arguments.area == "mutation" and arguments.command == "prepare":
            output = prepare_mutation(request)
        elif arguments.area == "mutation" and arguments.command == "validate-handoff":
            output = validate_frozen_handoff(store, request)
        elif arguments.area == "launch" and arguments.command == "policy-digest":
            output = plan_launch_policy_digest(request)
        elif arguments.area == "launch" and arguments.command == "plan":
            output = plan_launch(store, request)
        elif arguments.area == "launch" and arguments.command == "execute":
            output = execute_launch(store, request)
        elif arguments.area == "receipt" and arguments.command == "create":
            output = {"outcome": store.create(request)}
        elif arguments.area == "receipt" and arguments.command == "route":
            output = {"outcome": store.route(request)}
        elif arguments.area == "report" and arguments.command == "submit":
            output = {"outcome": store.submit_message(request, "report")}
        elif arguments.area == "report" and arguments.command == "acknowledge":
            output = {"outcome": store.acknowledge(request)}
        elif arguments.area == "inbox" and arguments.command == "consume":
            output = {"outcome": store.consume(request)}
        elif arguments.area == "input" and arguments.command == "submit":
            output = {"outcome": store.submit_message(request, "input_request")}
        elif arguments.area == "input" and arguments.command == "accept":
            output = {"outcome": store.accept_input(request)}
        elif arguments.area == "session" and arguments.command == "acquire":
            output = {"outcome": store.acquire_session(request)}
        elif arguments.area == "session" and arguments.command == "park":
            output = {"outcome": store.park(request)}
        elif arguments.area == "notify" and arguments.command == "amp-pane":
            output = {"outcome": store.notify_amp(request)}
        else:
            raise HelperError("unsupported command")
    print(json.dumps(output, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except HelperError as error:
        print(f"claude-delegation: {error}", file=sys.stderr)
        raise SystemExit(2) from error
