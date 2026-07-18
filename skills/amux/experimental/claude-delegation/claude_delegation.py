#!/usr/bin/env python3
"""Unstable, skill-owned helper for experimental Claude delegation."""

from __future__ import annotations

import argparse
import ast
import contextlib
import copy
import ctypes
import fcntl
import hashlib
import json
import os
import pathlib
import platform
import shlex
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from typing import Any, Iterator


SCHEMA_VERSION = 1
PROTOCOL_VERSION = 1
MAX_STORE_BYTES = 4 * 1024 * 1024
MAX_PACKET_BYTES = 256 * 1024
INTERNAL_EVENT_PREFIX = "amux:"
REQUIRED_CLAUDE_FLAGS = [
    "--allowed-tools", "--disable-slash-commands", "--disallowed-tools", "--mcp-config",
    "--no-chrome", "--permission-mode", "--prompt-suggestions", "--session-id",
    "--setting-sources", "--settings", "--strict-mcp-config", "--tools",
]


class HelperError(Exception):
    pass


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def read_input() -> dict[str, Any]:
    raw = sys.stdin.buffer.read(MAX_STORE_BYTES + 1)
    if len(raw) > MAX_STORE_BYTES:
        raise HelperError("input exceeds the experimental size limit")
    if not raw:
        return {}
    try:
        value = json.loads(raw)
    except json.JSONDecodeError as error:
        raise HelperError(f"invalid JSON input: {error.msg}") from error
    if not isinstance(value, dict):
        raise HelperError("input must be a JSON object")
    return value


class ReceiptStore:
    def __init__(self, state_dir: pathlib.Path):
        self.state_dir = state_dir
        self.path = state_dir / "receipts.json"
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

    def load_store(self) -> dict[str, Any]:
        try:
            raw = self.path.read_bytes()
        except FileNotFoundError:
            return {"schema_version": SCHEMA_VERSION, "receipts": []}
        if len(raw) > MAX_STORE_BYTES:
            raise HelperError("receipt store exceeds the experimental size limit")
        try:
            store = json.loads(raw)
        except json.JSONDecodeError as error:
            raise HelperError(f"invalid receipt store: {error.msg}") from error
        if not isinstance(store, dict) or store.get("schema_version") != SCHEMA_VERSION:
            raise HelperError("unsupported or invalid receipt store")
        receipts = store.get("receipts")
        if not isinstance(receipts, list):
            raise HelperError("invalid receipt store receipts")
        identities: set[str] = set()
        for receipt in receipts:
            if not isinstance(receipt, dict):
                raise HelperError("invalid receipt record")
            delegation_id = receipt.get("binding", {}).get("delegation_id")
            if not isinstance(delegation_id, str) or delegation_id in identities:
                raise HelperError("invalid or duplicate receipt identity")
            events = receipt.get("events")
            if not isinstance(events, list):
                raise HelperError("invalid receipt event history")
            event_ids: set[str] = set()
            for event in events:
                event_id = event.get("event_id") if isinstance(event, dict) else None
                if not isinstance(event_id, str) or not event_id or event_id in event_ids:
                    raise HelperError("invalid or duplicate receipt event identity")
                event_ids.add(event_id)
            identities.add(delegation_id)
        return store

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
        with self.mutation_lock():
            store = self.load_store()
            for receipt in store["receipts"]:
                if receipt["binding"]["delegation_id"] != binding["delegation_id"]:
                    continue
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

    def submit_message(self, request: dict[str, Any], expected_kind: str) -> str:
        with self.mutation_lock():
            store = self.load_store()
            delegation_id = protocol_id(request, "delegation_id")
            receipt = self.find(store, delegation_id)
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
            replay = find_event(receipt, event_id)
            if replay is None:
                if receipt["state"] != "acknowledged":
                    raise HelperError("verified parking requires acknowledgement")
                if receipt.get("input_state") in {"pending", "seen"}:
                    raise HelperError("verified parking requires no unresolved input request")
            identity = copy.deepcopy(receipt.get("session_identity"))
            if not isinstance(identity, dict):
                raise HelperError("verified parking requires an acquired session identity")
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
            current = inspect_claude_identity(identity["pane_id"], identity["claude_session_id"])
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
            launch_identity = completed_launch_identity(receipt)
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

        identity = inspect_claude_identity(pane_id, claude_session_id)
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, delegation_id)
            launch_identity = completed_launch_identity(receipt)
            for key in ("session", "window", "window_id", "pane_id"):
                if identity[key] != launch_identity[key]:
                    raise HelperError("Claude session does not match the exact pane created by this receipt")
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
    reject_unknown(value, allowed, "binding")
    if value.get("protocol_version") != PROTOCOL_VERSION:
        raise HelperError("unsupported protocol_version")
    for key in allowed - {"protocol_version"}:
        required_string(value, key, 2048 if key == "workdir" else 256)
    protocol_id(value, "delegation_id")
    if value["producer_role"] == "thinker":
        if value["authority"] != "read_only":
            raise HelperError("thinker authority must remain read_only")
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


def run_command(arguments: list[str]) -> str:
    try:
        completed = subprocess.run(arguments, capture_output=True, text=True, timeout=5, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise HelperError(f"run {arguments[0]}: {error}") from error
    if completed.returncode != 0:
        detail = completed.stderr.strip() or f"exit {completed.returncode}"
        raise HelperError(f"run {arguments[0]}: {detail}")
    return completed.stdout.rstrip("\r\n")


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
    proc = pathlib.Path("/proc") / str(pid)
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
    raw_name = bytes(info.name).split(b"\0", 1)[0] or bytes(info.comm).split(b"\0", 1)[0]
    name = os.fsdecode(raw_name)
    started = f"{info.start_seconds}.{info.start_microseconds:06d}"
    digest = hashlib.sha256(b"\0".join(os.fsencode(argument) for argument in arguments)).hexdigest()
    return name, started, arguments, digest


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


def is_claude_process(system: str, process_name: str, process_args: list[str], session_position: int) -> bool:
    if pathlib.Path(process_args[0]).name == "claude":
        return True
    if system != "Linux" or process_name not in {"node", "bun"} or session_position <= 1:
        return False
    script = pathlib.PurePath(process_args[1])
    return script.name == "cli.js" and "@anthropic-ai" in script.parts and "claude-code" in script.parts


def inspect_claude_identity(pane_id: str, claude_session_id: str) -> dict[str, Any]:
    if not pane_id.startswith("%") or len(pane_id) < 2:
        raise HelperError("Claude identity requires an exact tmux pane ID")
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
    session_positions = [index for index, argument in enumerate(process_args) if argument == "--session-id"]
    if (
        len(session_positions) != 1
        or session_positions[0] + 1 >= len(process_args)
        or process_args[session_positions[0] + 1] != claude_session_id
        or not is_claude_process(platform.system(), process_name, process_args, session_positions[0])
    ):
        raise HelperError("pane process is not the expected Claude session")
    return {
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
        "process_command_digest": process_command_digest,
        "launch_command_digest": hashlib.sha256(start_command.encode()).hexdigest(),
    }


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
    if value.get("workflow") == "mutating":
        fields |= mutating_fields
    reject_unknown(value, fields, "launch request")
    result: dict[str, Any] = {}
    for key in fields - mutating_fields - {"workflow"}:
        result[key] = required_string(value, key, 2048 if key in {"workdir", "packet_file"} else 256)
    if value.get("workflow", "read_only") not in {"read_only", "mutating"}:
        raise HelperError("launch workflow must be read_only or mutating")
    result["workflow"] = value.get("workflow", "read_only")
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


def preflight_worktree(request: dict[str, str]) -> str:
    workdir = str(pathlib.Path(request["workdir"]).resolve(strict=True))
    git = ["git", "--no-optional-locks", "-C", workdir]
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
    return workdir


def private_runtime_paths(store: ReceiptStore, delegation_id: str) -> tuple[pathlib.Path, pathlib.Path]:
    # Delegation IDs are opaque protocol values, not trusted path components.
    runtime_key = hashlib.sha256(delegation_id.encode()).hexdigest()
    runtime = store.state_dir / "runtime" / runtime_key
    return runtime / "mcp.json", runtime / "settings.json"


def require_tmux_session(session: str) -> None:
    try:
        run_command(["tmux", "has-session", "-t", "=" + session])
    except HelperError as error:
        raise HelperError("target tmux session does not exist or cannot be verified") from error


def launch_components(store: ReceiptStore, request_value: Any) -> dict[str, Any]:
    request = validate_launch_request(request_value)
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
    else:
        workdir = preflight_worktree(request)
    packet_path = pathlib.Path(request["packet_file"]).resolve(strict=True)
    mode = packet_path.stat().st_mode & 0o777
    if mode & 0o077:
        raise HelperError("launch packet file must not be group- or world-accessible")
    packet = packet_path.read_bytes()
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
    claude = shutil.which("claude")
    if not claude:
        raise HelperError("Claude Code is unavailable")
    run_command([claude, "--version"])
    help_text = run_command([claude, "--help"])
    missing_flags = [flag for flag in REQUIRED_CLAUDE_FLAGS if flag not in help_text]
    if missing_flags:
        raise HelperError("Claude Code is missing required flags: " + ", ".join(missing_flags))
    mcp_path, settings_path = private_runtime_paths(store, request["delegation_id"])
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
        packet_text,
    ]
    process_argv = ["env", "-u", "GH_TOKEN", "-u", "GITHUB_TOKEN", "-u", "GITLAB_TOKEN", *argv] if mutating else argv
    start_command = f"cd {shlex.quote(workdir)} && exec {shlex.join(process_argv)}"
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
        "launch_policy_digest": hashlib.sha256(json.dumps(policy, sort_keys=True, separators=(",", ":")).encode()).hexdigest(),
        "launch_command_digest": hashlib.sha256(start_command.encode()).hexdigest(),
        "start_command": start_command,
        "mcp_path": mcp_path,
        "settings_path": settings_path,
        "mcp_config": mcp_config,
        "settings": settings,
        "workflow": workflow,
        "capacity_decision_digest": decision_digest_value,
        "capacity_decision": capacity_decision if mutating else None,
    }


def write_private_json(path: pathlib.Path, value: Any) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(path.parent, 0o700)
    descriptor, temporary = tempfile.mkstemp(prefix=path.name + ".tmp.", dir=path.parent)
    try:
        os.fchmod(descriptor, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as output:
            json.dump(value, output, sort_keys=True, separators=(",", ":"))
            output.write("\n")
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


def plan_launch(store: ReceiptStore, request: Any) -> dict[str, Any]:
    components = launch_components(store, request)
    result = {
        "packet_digest": components["packet_digest"],
        "launch_policy_digest": components["launch_policy_digest"],
        "launch_command_digest": components["launch_command_digest"],
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
    return result


def execute_launch(store: ReceiptStore, request: Any) -> dict[str, Any]:
    request_data = validate_launch_request(request)
    request_digest = hashlib.sha256(json.dumps(request_data, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    delegation_id = request_data["delegation_id"]
    event_id = request_data["event_id"]
    result_id = internal_event_id("launch-result", event_id)
    with store.mutation_lock():
        receipt = store.find(store.load_store(), delegation_id)
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
            return {"outcome": "duplicate", **result["identity"]}
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
    }
    with store.mutation_lock():
        receipt_store = store.load_store()
        receipt = store.find(receipt_store, delegation_id)
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

        require_tmux_session(request_data["tmux_session"])
        if components["workflow"] == "mutating":
            revalidate_mutating_launch_lease(receipt_store, receipt, request_data)
        intent["at"] = utc_now()
        receipt["events"].append(intent)
        receipt["updated_at"] = intent["at"]
        store.commit(receipt_store)

        runtime_root = components["mcp_path"].parent.parent
        runtime_root.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(runtime_root, 0o700)
        write_private_json(components["mcp_path"], components["mcp_config"])
        write_private_json(components["settings_path"], components["settings"])
        output = run_command(
            [
                "tmux", "new-window", "-d", "-P", "-F",
                "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}",
                "-t", "=" + request_data["tmux_session"] + ":",
                "-n", request_data["tmux_window"], components["start_command"],
            ]
        )
        fields = output.split("\t")
        if len(fields) != 4 or fields[0] != request_data["tmux_session"] or fields[1] != request_data["tmux_window"] or not fields[2].startswith("@") or not fields[3].startswith("%"):
            raise HelperError("tmux launch did not return one exact session/window/pane identity")
        identity = {"session": fields[0], "window": fields[1], "window_id": fields[2], "pane_id": fields[3]}
        result = {
            "event_id": result_id,
            "kind": "launch_completed",
            "operation_event_id": event_id,
            "identity": identity,
            "at": utc_now(),
        }
        receipt["events"].append(result)
        receipt["updated_at"] = result["at"]
        store.commit(receipt_store)
        return {"outcome": "launched", **identity}


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
        version = run_command(["claude", "--version"])
        help_text = run_command(["claude", "--help"])
        missing = [flag for flag in REQUIRED_CLAUDE_FLAGS if flag not in help_text]
        capabilities["claude_code"] = {"status": "supported" if not missing else "unavailable", "version": version[:128], "missing_flags": missing}
    except HelperError as error:
        capabilities["claude_code"] = {"status": "unavailable", "reason": str(error)[:512]}
    try:
        capabilities["tmux"] = {"status": "supported", "version": run_command(["tmux", "-V"])[:128]}
    except HelperError as error:
        capabilities["tmux"] = {"status": "unavailable", "reason": str(error)[:512]}
    capacity: dict[str, Any] = {"status": "unavailable", "windows": []}
    try:
        raw = json.loads(run_command(["codexbar", "usage", "--provider", "claude", "--format", "json"]))
        provider = raw[0] if isinstance(raw, list) and raw and isinstance(raw[0], dict) else {}
        usage = provider.get("usage", {}) if isinstance(provider.get("usage"), dict) else {}
        windows = []
        for name in ("primary", "secondary", "tertiary"):
            window = usage.get(name)
            if isinstance(window, dict):
                windows.append({"name": name, "used_percent": window.get("usedPercent"), "window_minutes": window.get("windowMinutes"), "resets_at": window.get("resetsAt")})
        for index, window in enumerate(usage.get("extraRateWindows", [])):
            if isinstance(window, dict):
                windows.append({"name": f"extra_{index}", "used_percent": window.get("usedPercent"), "window_minutes": window.get("windowMinutes"), "resets_at": window.get("resetsAt"), "model_specific": True})
        source = provider.get("source", "unknown")
        capacity = {"status": "supported", "source": source, "confidence": "reported" if source in {"web", "api", "oauth"} else "unknown", "windows": windows}
    except (HelperError, json.JSONDecodeError, KeyError, TypeError) as error:
        capacity = {"status": "unavailable", "reason": str(error)[:512], "windows": []}
    return {"experimental": True, "capabilities": capabilities, "capacity": capacity}


def percentage(value: Any, label: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value < 0 or value > 100:
        raise HelperError(f"{label} must be a number from 0 to 100")
    return float(value)


def capacity_decision_digest(value: Any) -> str:
    def normalize(item: Any) -> Any:
        if isinstance(item, dict):
            return {key: normalize(nested) for key, nested in item.items()}
        if isinstance(item, list):
            return [normalize(nested) for nested in item]
        if isinstance(item, (int, float)) and not isinstance(item, bool):
            return float(item)
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

    reliable = capacity.get("status") == "supported" and capacity.get("confidence") == "reported"
    windows = capacity.get("windows")
    if not isinstance(windows, list) or not windows:
        return unknown_capacity_decision(acknowledged, acknowledgement_of, "capacity has no available windows; reserve impact is unknown", request_digest, capacity_source, capacity_confidence)
    evaluated = []
    present_window_classes = {"five_hour": False, "weekly": False}
    missing_capacity = not reliable
    for raw in windows:
        if not isinstance(raw, dict):
            missing_capacity = True
            continue
        name = required_string(raw, "name", 256)
        if raw.get("model_specific") is True:
            if name not in model_floors:
                raise HelperError("reserve floor is required for every available model-specific window")
            floor = percentage(model_floors[name], f"{name} reserve floor")
            window_class = "model_specific"
        elif raw.get("window_minutes") == 300:
            floor = five_hour_floor
            window_class = "five_hour"
            present_window_classes[window_class] = True
        elif raw.get("window_minutes") == 10080:
            floor = weekly_floor
            window_class = "weekly"
            present_window_classes[window_class] = True
        else:
            raise HelperError("reserve floor is required for every available capacity window")
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


def reject_unknown(value: dict[str, Any], allowed: set[str], label: str) -> None:
    unknown = sorted(set(value) - allowed)
    if unknown:
        raise HelperError(f"{label} contains unknown fields: {', '.join(unknown)}")


def find_event(receipt: dict[str, Any], event_id: str) -> dict[str, Any] | None:
    for event in receipt["events"]:
        if event.get("event_id") == event_id:
            return event
    return None


def completed_launch_identity(receipt: dict[str, Any]) -> dict[str, str]:
    completed = [event for event in receipt["events"] if event.get("kind") == "launch_completed"]
    if len(completed) != 1 or not isinstance(completed[0].get("identity"), dict):
        raise HelperError("session acquisition requires one completed receipt launch")
    identity = completed[0]["identity"]
    for key in ("session", "window", "window_id", "pane_id"):
        if not isinstance(identity.get(key), str) or not identity[key]:
            raise HelperError("completed receipt launch has incomplete tmux identity")
    return identity


def event_without_time(event: dict[str, Any]) -> dict[str, Any]:
    return {key: value for key, value in event.items() if key != "at"}


def default_state_dir() -> pathlib.Path:
    return pathlib.Path.home() / "Library" / "Application Support" / "amux" / "experimental" / "claude-delegation"


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(description=__doc__)
    root.add_argument("--state-dir", type=pathlib.Path, default=default_state_dir())
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
    launch_commands.add_parser("plan")
    launch_commands.add_parser("execute")
    capacity = commands.add_parser("capacity")
    capacity_commands = capacity.add_subparsers(dest="command", required=True)
    capacity_commands.add_parser("decide-mutating")
    mutation = commands.add_parser("mutation")
    mutation_commands = mutation.add_subparsers(dest="command", required=True)
    mutation_commands.add_parser("prepare")
    mutation_commands.add_parser("validate-handoff")
    commands.add_parser("diagnose")
    return root


def main() -> int:
    arguments = parser().parse_args()
    store = ReceiptStore(arguments.state_dir.expanduser().resolve())
    if arguments.area == "mcp" and arguments.command == "serve":
        return serve_mcp(store, arguments.delegation_id)
    if arguments.area == "amp" and arguments.command == "inspect":
        print(json.dumps(inspect_amp_target(arguments.pane, arguments.origin_thread), sort_keys=True, separators=(",", ":")))
        return 0
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
