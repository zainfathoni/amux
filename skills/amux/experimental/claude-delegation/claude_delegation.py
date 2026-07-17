#!/usr/bin/env python3
"""Unstable, skill-owned helper for experimental read-only Claude delegation."""

from __future__ import annotations

import argparse
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
            created_at = utc_now()
            store["receipts"].append(
                {
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
            )
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
        envelope = validate_envelope(request, expected_kind)
        with self.mutation_lock():
            store = self.load_store()
            receipt = self.find(store, envelope["delegation_id"])
            validate_envelope_binding(envelope, receipt["binding"])
            event_id = envelope["message_id"]
            kind = "valid_report" if expected_kind == "thinker_report" else "input_request"
            event = {"event_id": event_id, "kind": kind, "message": envelope}
            replay = find_event(receipt, event_id)
            if replay is not None:
                if replay.get("kind") == kind and replay.get("message") == envelope:
                    return "duplicate"
                raise HelperError("event ID is already bound to a conflicting event")
            if expected_kind == "thinker_report" and receipt["report_message_id"]:
                raise HelperError("receipt already contains a different valid report")
            if expected_kind == "input_request" and receipt["report_message_id"]:
                raise HelperError("a valid report closes the input-request stream")
            timestamp = utc_now()
            event["at"] = timestamp
            if expected_kind == "thinker_report":
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
    reject_unknown(value, allowed, "binding")
    if value.get("protocol_version") != PROTOCOL_VERSION:
        raise HelperError("unsupported protocol_version")
    for key in allowed - {"protocol_version"}:
        required_string(value, key, 2048 if key == "workdir" else 256)
    protocol_id(value, "delegation_id")
    if value["producer_role"] != "thinker" or value["authority"] != "read_only":
        raise HelperError("#148 permits only thinker/read_only authority")
    for key in ("nonce", "packet_digest", "launch_policy_digest", "launch_command_digest"):
        if len(value[key]) != 64 or any(character not in "0123456789abcdef" for character in value[key]):
            raise HelperError(f"{key} must be a lowercase SHA-256 value")
    return copy.deepcopy(value)


def validate_envelope(value: Any, expected_kind: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise HelperError("message must be an object")
    payload_key = "report" if expected_kind == "thinker_report" else "input_request"
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


def exact_process_identity(pid: int) -> tuple[str, str, list[str], str]:
    if platform.system() != "Darwin":
        name = pathlib.Path(run_command(["ps", "-p", str(pid), "-o", "comm="]).strip()).name
        command = run_command(["ps", "-p", str(pid), "-o", "command="]).strip()
        try:
            arguments = shlex.split(command)
        except ValueError as error:
            raise HelperError("process command could not be parsed") from error
        started = run_command(["ps", "-p", str(pid), "-o", "lstart="]).strip()
        return name, started, arguments, hashlib.sha256(command.encode()).hexdigest()

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
            "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_created}\t#{pane_current_path}\t#{pane_current_command}",
        ]
    ).split("\t")
    if len(fields) != 8 or fields[3] != pane_id or fields[7] != "amp":
        raise HelperError("target is not one exact live Amp pane")
    try:
        pane_pid = int(fields[4])
        pane_created = int(fields[5])
    except ValueError as error:
        raise HelperError("Amp target returned invalid tmux process identity") from error
    workdir = str(pathlib.Path(fields[6]).resolve(strict=True))
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
        "pane_created": pane_created,
        "workdir": workdir,
        "current_command": fields[7],
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
        "pane_created",
        "workdir",
        "current_command",
        "process_name",
        "process_identity",
        "process_command_digest",
    }
    reject_unknown(value, fields, "notification target")
    for key in fields - {"pane_pid", "pane_created"}:
        required_string(value, key, 2048 if key == "workdir" else 256)
    if not isinstance(value.get("pane_pid"), int) or value["pane_pid"] <= 0:
        raise HelperError("notification target pane_pid is invalid")
    if not isinstance(value.get("pane_created"), int) or value["pane_created"] <= 0:
        raise HelperError("notification target pane_created is invalid")
    return copy.deepcopy(value)


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
            "#{session_name}\t#{window_name}\t#{window_id}\t#{pane_id}\t#{pane_pid}\t#{pane_created}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_start_command}",
        ]
    ).split("\t")
    if len(fields) != 9 or fields[3] != pane_id or not fields[7] or not fields[8]:
        raise HelperError("Claude pane is missing exact live tmux identity")
    try:
        pane_pid = int(fields[4])
        pane_created = int(fields[5])
    except ValueError as error:
        raise HelperError("Claude pane returned invalid process identity") from error
    workdir = str(pathlib.Path(fields[6]).resolve(strict=True))
    process_name, process_identity, process_args, process_command_digest = exact_process_identity(pane_pid)
    session_positions = [index for index, argument in enumerate(process_args) if argument == "--session-id"]
    if (
        process_name != "claude"
        or fields[7] != "claude"
        or pathlib.Path(process_args[0]).name != "claude"
        or len(session_positions) != 1
        or session_positions[0] + 1 >= len(process_args)
        or process_args[session_positions[0] + 1] != claude_session_id
    ):
        raise HelperError("pane process is not the expected Claude session")
    return {
        "claude_session_id": claude_session_id,
        "session": fields[0],
        "window": fields[1],
        "window_id": fields[2],
        "pane_id": fields[3],
        "pane_pid": pane_pid,
        "pane_created": pane_created,
        "workdir": workdir,
        "current_command": fields[7],
        "process_name": process_name,
        "process_identity": process_identity,
        "process_command_digest": process_command_digest,
        "launch_command_digest": hashlib.sha256(fields[8].encode()).hexdigest(),
    }


def validate_launch_request(value: Any) -> dict[str, str]:
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
    }
    reject_unknown(value, fields, "launch request")
    result: dict[str, str] = {}
    for key in fields:
        result[key] = required_string(value, key, 2048 if key in {"workdir", "packet_file"} else 256)
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


def launch_components(store: ReceiptStore, request_value: Any) -> dict[str, Any]:
    request = validate_launch_request(request_value)
    if platform.system() != "Darwin":
        raise HelperError("experimental Claude launch is available only on Darwin")
    run_command(["tmux", "-V"])
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
    policy = {
        "interactive": True,
        "permission_mode": "dontAsk",
        "setting_sources": [],
        "strict_mcp": True,
        "built_in_tools": ["Read", "Grep", "Glob"],
        "allowed_mcp_tools": [mcp_tool_prefix + "submit_report", mcp_tool_prefix + "submit_input_request"],
        "denied_tools": ["Bash", "Edit", "Write", "NotebookEdit", "Agent", "WebFetch", "WebSearch", "Skill"],
        "additional_directories": [],
        "automatic_interactive_input": False,
    }
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
        "Read,Grep,Glob",
        "--allowed-tools",
        ",".join(["Read", "Grep", "Glob"] + policy["allowed_mcp_tools"]),
        "--disallowed-tools",
        ",".join(policy["denied_tools"]),
        "--disable-slash-commands",
        "--no-chrome",
        "--prompt-suggestions",
        "false",
        packet_text,
    ]
    start_command = f"cd {shlex.quote(workdir)} && exec {shlex.join(argv)}"
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
    return {
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


def execute_launch(store: ReceiptStore, request: Any) -> dict[str, Any]:
    request_data = validate_launch_request(request)
    delegation_id = request_data["delegation_id"]
    event_id = request_data["event_id"]
    result_id = internal_event_id("launch-result", event_id)
    with store.mutation_lock():
        receipt = store.find(store.load_store(), delegation_id)
        replay = find_event(receipt, event_id)
        if replay is not None:
            if (
                replay.get("kind") != "launch_intent"
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

    components = launch_components(store, request_data)
    intent = {
        "event_id": event_id,
        "kind": "launch_intent",
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
        replay = find_event(receipt, event_id)
        if replay is not None:
            raise HelperError("launch event appeared concurrently; retry the exact request")

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
    capabilities["platform"] = {"status": "supported" if platform.system() == "Darwin" else "unavailable", "value": platform.system()}
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
                windows.append({"name": f"extra_{index}", "used_percent": window.get("usedPercent"), "window_minutes": window.get("windowMinutes"), "resets_at": window.get("resetsAt")})
        source = provider.get("source", "unknown")
        capacity = {"status": "supported", "source": source, "confidence": "reported" if source in {"web", "api", "oauth"} else "unknown", "windows": windows}
    except (HelperError, json.JSONDecodeError, KeyError, TypeError) as error:
        capacity = {"status": "unavailable", "reason": str(error)[:512], "windows": []}
    return {"experimental": True, "capabilities": capabilities, "capacity": capacity}


def mcp_tools() -> list[dict[str, Any]]:
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
        "producer_role": {"type": "string", "const": "thinker"},
        "authority": {"type": "string", "const": "read_only"},
        "launch_policy_digest": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
        "created_at": {"type": "string", "maxLength": 256},
    }
    common_required = list(common_properties)
    string_list = {"type": "array", "maxItems": 32, "items": {"type": "string", "maxLength": 2048}}
    report_properties = dict(common_properties)
    report_properties.update(
        {
            "kind": {"type": "string", "const": "thinker_report"},
            "report": {
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
                ],
            },
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
            "description": "Submit the complete bounded semantic thinker report. Pane output is not authoritative.",
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
            mcp_response(identifier, result={"tools": mcp_tools()})
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
                    outcome = store.submit_message(arguments, "thinker_report")
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
        if arguments.area == "launch" and arguments.command == "plan":
            output = plan_launch(store, request)
        elif arguments.area == "launch" and arguments.command == "execute":
            output = execute_launch(store, request)
        elif arguments.area == "receipt" and arguments.command == "create":
            output = {"outcome": store.create(request)}
        elif arguments.area == "receipt" and arguments.command == "route":
            output = {"outcome": store.route(request)}
        elif arguments.area == "report" and arguments.command == "submit":
            output = {"outcome": store.submit_message(request, "thinker_report")}
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
