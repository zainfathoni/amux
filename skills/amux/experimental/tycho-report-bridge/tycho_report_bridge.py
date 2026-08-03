#!/usr/bin/env python3
"""Unstable, skill-owned Tycho semantic-report receipt/inbox adapter."""

from __future__ import annotations

import argparse
import contextlib
import errno
import fcntl
import hashlib
import hmac
import json
import os
import pathlib
import re
import stat
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from typing import Any, Iterator


SCHEMA_VERSION = 1
MAX_INPUT_BYTES = 64 * 1024
MAX_STORE_BYTES = 1024 * 1024
MAX_RECEIPTS = 128
MAX_EVENTS = 16
MAX_LIST_ITEMS = 32
TOKEN_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$")
INTERNAL_EVENT_PREFIX = "internal:"
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
NONCE_RE = re.compile(r"^[0-9a-f]{64,128}$")
PANE_RE = re.compile(r"^%[0-9]+$")
PANE_CREATED_RE = re.compile(r"^[0-9]+$")


class BridgeError(Exception):
    pass


class LockBusy(BridgeError):
    pass


def now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def reject_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise BridgeError("JSON contains a duplicated field")
        result[key] = value
    return result


def reject_constant(_value: str) -> None:
    raise BridgeError("JSON contains an unsupported constant")


def decode_json(raw: bytes, label: str) -> Any:
    try:
        return json.loads(
            raw,
            object_pairs_hook=reject_pairs,
            parse_constant=reject_constant,
        )
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        raise BridgeError(f"invalid {label}") from error


def read_request() -> dict[str, Any]:
    raw = sys.stdin.buffer.read(MAX_INPUT_BYTES + 1)
    if len(raw) > MAX_INPUT_BYTES:
        raise BridgeError("input exceeds the experimental size limit")
    value = decode_json(raw or b"{}", "JSON input")
    if not isinstance(value, dict):
        raise BridgeError("input must be a JSON object")
    return value


def exact_fields(value: Any, allowed: set[str], label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise BridgeError(f"{label} must be an object")
    unknown = set(value) - allowed
    if unknown:
        raise BridgeError(f"{label} contains unknown fields")
    return value


def text(value: dict[str, Any], key: str, limit: int, pattern: re.Pattern[str] | None = None) -> str:
    result = value.get(key)
    if not isinstance(result, str) or not result or len(result.encode()) > limit:
        raise BridgeError(f"{key} must be a non-empty bounded string")
    if pattern is not None and pattern.fullmatch(result) is None:
        raise BridgeError(f"{key} has an invalid format")
    return result


def optional_text(value: dict[str, Any], key: str, limit: int) -> str | None:
    result = value.get(key)
    if result is None:
        return None
    if not isinstance(result, str) or not result or len(result.encode()) > limit:
        raise BridgeError(f"{key} must be null or a non-empty bounded string")
    return result


def user_event_id(value: dict[str, Any]) -> str:
    event_id = text(value, "event_id", 128, TOKEN_RE)
    if event_id.startswith(INTERNAL_EVENT_PREFIX):
        raise BridgeError("event_id uses the reserved internal namespace")
    return event_id


def canonical_origin(value: dict[str, Any]) -> str:
    origin = text(value, "origin_thread", 128)
    if not origin.startswith("T-") or len(origin) == 2 or re.search(r"[\t\n\r/ ?#]", origin):
        raise BridgeError("origin_thread must be an exact canonical Amp thread ID")
    return origin


def canonical_workdir(value: dict[str, Any]) -> str:
    workdir = text(value, "workdir", 4096)
    path = pathlib.Path(workdir)
    if not path.is_absolute() or os.path.abspath(workdir) != workdir:
        raise BridgeError("workdir must be an absolute canonical path")
    return workdir


def validate_binding(value: Any) -> dict[str, Any]:
    binding = exact_fields(value, {
        "receipt_id", "origin_thread", "correlation_id", "producer_nonce",
        "tycho_agent_key", "claude_session_id", "run_id", "task_message_id",
        "workdir", "task_reference", "task_digest", "producer_role", "authority",
        "group_reference", "notification_target",
    }, "binding")
    result = {
        "receipt_id": text(binding, "receipt_id", 128, TOKEN_RE),
        "origin_thread": canonical_origin(binding),
        "correlation_id": text(binding, "correlation_id", 128, TOKEN_RE),
        "producer_nonce_hash": hashlib.sha256(text(binding, "producer_nonce", 128, NONCE_RE).encode()).hexdigest(),
        "tycho_agent_key": text(binding, "tycho_agent_key", 128, TOKEN_RE),
        "claude_session_id": optional_text(binding, "claude_session_id", 128),
        "run_id": text(binding, "run_id", 128, TOKEN_RE),
        "task_message_id": text(binding, "task_message_id", 128, TOKEN_RE),
        "workdir": canonical_workdir(binding),
        "task_reference": text(binding, "task_reference", 256),
        "task_digest": text(binding, "task_digest", 64, SHA256_RE),
        "producer_role": text(binding, "producer_role", 64, TOKEN_RE),
        "authority": binding.get("authority"),
        "group_reference": optional_text(binding, "group_reference", 128),
        "notification_target": validate_notification(binding.get("notification_target")),
    }
    if result["authority"] != "report_only":
        raise BridgeError("authority must be exactly report_only")
    return result


PROOF_FIELDS = {
    "receipt_id", "origin_thread", "correlation_id", "producer_nonce",
    "tycho_agent_key", "claude_session_id", "run_id", "task_message_id",
    "workdir", "task_reference", "task_digest", "producer_role", "authority",
}


def validate_proof(request: dict[str, Any], binding: dict[str, Any]) -> None:
    exact_fields(request, PROOF_FIELDS | {"event_id", "report"}, "report submission")
    supplied = {
        "receipt_id": text(request, "receipt_id", 128, TOKEN_RE),
        "origin_thread": canonical_origin(request),
        "correlation_id": text(request, "correlation_id", 128, TOKEN_RE),
        "producer_nonce_hash": hashlib.sha256(text(request, "producer_nonce", 128, NONCE_RE).encode()).hexdigest(),
        "tycho_agent_key": text(request, "tycho_agent_key", 128, TOKEN_RE),
        "claude_session_id": optional_text(request, "claude_session_id", 128),
        "run_id": text(request, "run_id", 128, TOKEN_RE),
        "task_message_id": text(request, "task_message_id", 128, TOKEN_RE),
        "workdir": canonical_workdir(request),
        "task_reference": text(request, "task_reference", 256),
        "task_digest": text(request, "task_digest", 64, SHA256_RE),
        "producer_role": text(request, "producer_role", 64, TOKEN_RE),
        "authority": request.get("authority"),
    }
    for key, value in supplied.items():
        if key not in binding or not hmac.compare_digest(str(value), str(binding[key])):
            raise BridgeError(f"report submission does not match immutable {key}")


def string_list(value: dict[str, Any], key: str) -> list[str]:
    items = value.get(key)
    if not isinstance(items, list) or len(items) > MAX_LIST_ITEMS:
        raise BridgeError(f"{key} must be a bounded array")
    for item in items:
        if not isinstance(item, str) or not item or len(item.encode()) > 2048:
            raise BridgeError(f"{key} contains an invalid item")
    return items


def validate_report(value: Any) -> dict[str, Any]:
    report = exact_fields(value, {"status", "summary", "findings", "blockers", "verification"}, "semantic report")
    status = report.get("status")
    if status not in {"complete", "blocked"}:
        raise BridgeError("semantic report status must be complete or blocked")
    result = {
        "status": status,
        "summary": text(report, "summary", 8192),
        "findings": string_list(report, "findings"),
        "blockers": string_list(report, "blockers"),
        "verification": string_list(report, "verification"),
    }
    if status == "blocked" and not result["blockers"]:
        raise BridgeError("a blocked semantic report requires a blocker")
    return result


def validate_notification(value: Any) -> dict[str, str] | None:
    if value is None:
        return None
    target = exact_fields(value, {"pane_id", "pane_created"}, "notification target")
    return {
        "pane_id": text(target, "pane_id", 32, PANE_RE),
        "pane_created": text(target, "pane_created", 32, PANE_CREATED_RE),
    }


def event_body(event: dict[str, Any]) -> dict[str, Any]:
    return {key: value for key, value in event.items() if key != "at"}


def find_event(receipt: dict[str, Any], event_id: str) -> dict[str, Any] | None:
    return next((event for event in receipt["events"] if event["event_id"] == event_id), None)


def replay(receipt: dict[str, Any], event: dict[str, Any]) -> bool:
    existing = find_event(receipt, event["event_id"])
    if existing is None:
        return False
    if event_body(existing) == event:
        return True
    raise BridgeError("event_id is already bound to a conflicting event")


def validate_store(raw: bytes) -> dict[str, Any]:
    value = decode_json(raw, "receipt store")
    if not isinstance(value, dict) or set(value) != {"schema_version", "receipts"} or value.get("schema_version") != SCHEMA_VERSION:
        raise BridgeError("unsupported or invalid receipt store")
    receipts = value.get("receipts")
    if not isinstance(receipts, list) or len(receipts) > MAX_RECEIPTS:
        raise BridgeError("invalid receipt store")
    seen_receipts: set[str] = set()
    for receipt in receipts:
        if not isinstance(receipt, dict) or set(receipt) != {
            "binding", "coordinator_token_hash", "state", "report_event_id",
            "events", "created_at", "updated_at",
        }:
            raise BridgeError("invalid receipt record")
        binding = receipt.get("binding")
        if not isinstance(binding, dict) or set(binding) != {
            "receipt_id", "origin_thread", "correlation_id", "producer_nonce_hash",
            "tycho_agent_key", "claude_session_id", "run_id", "task_message_id",
            "workdir", "task_reference", "task_digest", "producer_role", "authority",
            "group_reference",
            "notification_target",
        }:
            raise BridgeError("invalid receipt binding")
        receipt_id = binding.get("receipt_id")
        if (
            not isinstance(receipt_id, str)
            or TOKEN_RE.fullmatch(receipt_id) is None
            or receipt_id in seen_receipts
            or not isinstance(receipt.get("coordinator_token_hash"), str)
            or SHA256_RE.fullmatch(receipt["coordinator_token_hash"]) is None
            or not isinstance(binding.get("producer_nonce_hash"), str)
            or SHA256_RE.fullmatch(binding["producer_nonce_hash"]) is None
        ):
            raise BridgeError("invalid or duplicate receipt")
        canonical_origin(binding)
        canonical_workdir(binding)
        for key, limit, pattern in (
            ("correlation_id", 128, TOKEN_RE), ("tycho_agent_key", 128, TOKEN_RE),
            ("run_id", 128, TOKEN_RE), ("task_message_id", 128, TOKEN_RE),
            ("task_reference", 256, None), ("task_digest", 64, SHA256_RE),
            ("producer_role", 64, TOKEN_RE),
        ):
            text(binding, key, limit, pattern)
        optional_text(binding, "claude_session_id", 128)
        optional_text(binding, "group_reference", 128)
        validate_notification(binding.get("notification_target"))
        if binding.get("authority") != "report_only":
            raise BridgeError("invalid receipt authority")
        events = receipt.get("events")
        if not isinstance(events, list) or not events or len(events) > MAX_EVENTS:
            raise BridgeError("invalid receipt event history")
        ids = [event.get("event_id") for event in events if isinstance(event, dict)]
        if len(ids) != len(events) or len(ids) != len(set(ids)):
            raise BridgeError("invalid receipt event history")
        kinds = []
        report_ids = []
        history_state = "none"
        for event in events:
            kind = event.get("kind")
            allowed = {
                "created": {"event_id", "kind", "binding", "at"},
                "valid_report": {"event_id", "kind", "report", "at"},
                "notification_intent": {"event_id", "kind", "report_event_id", "target", "detail", "at"},
                "notification_succeeded": {"event_id", "kind", "report_event_id", "target", "detail", "at"},
                "notification_failed": {"event_id", "kind", "report_event_id", "target", "detail", "at"},
                "notification_indeterminate": {"event_id", "kind", "report_event_id", "target", "detail", "at"},
                "delivered": {"event_id", "kind", "report_event_id", "at"},
                "acknowledged": {"event_id", "kind", "report_event_id", "at"},
            }.get(kind)
            if allowed is None or set(event) != allowed:
                raise BridgeError("invalid or unknown receipt event")
            text(event, "event_id", 128, TOKEN_RE)
            text(event, "at", 64)
            if kind == "created" and event.get("binding") != binding:
                raise BridgeError("created event binding differs from receipt binding")
            if kind == "created":
                if history_state != "none":
                    raise BridgeError("invalid created event order")
                history_state = "created"
            if kind == "valid_report":
                if history_state != "created":
                    raise BridgeError("invalid valid_report event order")
                validate_report(event.get("report"))
                report_ids.append(event.get("event_id"))
                history_state = "valid_report"
            if "report_event_id" in event and event.get("report_event_id") != receipt.get("report_event_id"):
                raise BridgeError("receipt event references the wrong report")
            if kind.startswith("notification_"):
                if history_state not in {"valid_report", "delivered", "acknowledged"}:
                    raise BridgeError("notification precedes valid_report")
                validate_notification(event.get("target"))
                text(event, "detail", 64, TOKEN_RE)
            if kind == "delivered":
                if history_state != "valid_report":
                    raise BridgeError("invalid delivered event order")
                history_state = "delivered"
            if kind == "acknowledged":
                if history_state != "delivered":
                    raise BridgeError("invalid acknowledged event order")
                history_state = "acknowledged"
            kinds.append(kind)
        if kinds[0] != "created" or kinds.count("created") != 1 or len(report_ids) > 1:
            raise BridgeError("invalid receipt event transition history")
        expected_state = history_state
        if (
            receipt.get("state") != expected_state
            or receipt.get("report_event_id") != (report_ids[0] if report_ids else None)
            or ("acknowledged" in kinds and "delivered" not in kinds)
            or kinds.count("delivered") > 1
            or kinds.count("acknowledged") > 1
        ):
            raise BridgeError("receipt state differs from append-only event history")
        seen_receipts.add(receipt_id)
    return value


class Store:
    def __init__(self, state_dir: pathlib.Path):
        self.state_dir = state_dir
        self.path = state_dir / "receipts.json"
        self.lock_path = state_dir / "experimental.lock"

    def prepare(self) -> None:
        self.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        info = self.state_dir.stat(follow_symlinks=False)
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid():
            raise BridgeError("state directory must be an owner-controlled directory")
        if stat.S_IMODE(info.st_mode) != 0o700:
            os.chmod(self.state_dir, 0o700)

    @contextlib.contextmanager
    def lock(self) -> Iterator[None]:
        self.prepare()
        try:
            descriptor = os.open(self.lock_path, os.O_CREAT | os.O_RDWR | getattr(os, "O_NOFOLLOW", 0), 0o600)
        except OSError as error:
            raise BridgeError("receipt lock is unavailable") from error
        with os.fdopen(descriptor, "r+") as lock_file:
            info = os.fstat(lock_file.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid():
                raise BridgeError("receipt lock must be an owner-controlled regular file")
            os.fchmod(lock_file.fileno(), 0o600)
            try:
                fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError as error:
                if error.errno in {errno.EACCES, errno.EAGAIN, errno.EWOULDBLOCK}:
                    raise LockBusy("receipt lock is busy; retry the identical operation") from error
                raise BridgeError("receipt lock is unavailable") from error
            yield

    def load(self) -> dict[str, Any]:
        try:
            descriptor = os.open(self.path, os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0))
        except FileNotFoundError:
            return {"schema_version": SCHEMA_VERSION, "receipts": []}
        except OSError as error:
            raise BridgeError("receipt store is unavailable") from error
        with os.fdopen(descriptor, "rb") as source:
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o600:
                raise BridgeError("receipt store must be an owner-private regular file")
            raw = source.read(MAX_STORE_BYTES + 1)
        if len(raw) > MAX_STORE_BYTES:
            raise BridgeError("receipt store exceeds the experimental size limit")
        return validate_store(raw)

    def commit(self, store: dict[str, Any]) -> None:
        payload = (json.dumps(store, sort_keys=True, separators=(",", ":")) + "\n").encode()
        if len(payload) > MAX_STORE_BYTES:
            raise BridgeError("receipt store exceeds the experimental size limit")
        validate_store(payload)
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
    def find(store: dict[str, Any], receipt_id: str) -> dict[str, Any]:
        for receipt in store["receipts"]:
            if receipt["binding"]["receipt_id"] == receipt_id:
                return receipt
        raise BridgeError("receipt was not found")

    @staticmethod
    def authorize_coordinator(receipt: dict[str, Any], request: dict[str, Any]) -> None:
        origin = canonical_origin(request)
        token = text(request, "coordinator_token", 128, NONCE_RE)
        if origin != receipt["binding"]["origin_thread"]:
            raise BridgeError("coordinator origin does not match the receipt")
        digest = hashlib.sha256(token.encode()).hexdigest()
        if not hmac.compare_digest(digest, receipt["coordinator_token_hash"]):
            raise BridgeError("invalid coordinator token")

    def create(self, request: dict[str, Any]) -> dict[str, Any]:
        exact_fields(request, {"binding", "event_id", "coordinator_token"}, "receipt creation")
        binding = validate_binding(request.get("binding"))
        event_id = user_event_id(request)
        coordinator_token = text(request, "coordinator_token", 128, NONCE_RE)
        timestamp = now()
        event = {"event_id": event_id, "kind": "created", "binding": binding}
        with self.lock():
            store = self.load()
            try:
                receipt = self.find(store, binding["receipt_id"])
            except BridgeError:
                receipt = None
            if receipt is not None:
                existing = find_event(receipt, event_id)
                token_hash = hashlib.sha256(coordinator_token.encode()).hexdigest()
                if (
                    existing is not None
                    and event_body(existing) == event
                    and hmac.compare_digest(token_hash, receipt["coordinator_token_hash"])
                ):
                    return {"outcome": "duplicate", "receipt_id": binding["receipt_id"]}
                raise BridgeError("receipt identity or event_id conflicts with existing state")
            if len(store["receipts"]) >= MAX_RECEIPTS:
                raise BridgeError("receipt store reached its experimental receipt limit")
            receipt = {
                "binding": binding,
                "coordinator_token_hash": hashlib.sha256(coordinator_token.encode()).hexdigest(),
                "state": "created",
                "report_event_id": None,
                "events": [{**event, "at": timestamp}],
                "created_at": timestamp,
                "updated_at": timestamp,
            }
            store["receipts"].append(receipt)
            self.commit(store)
        return {"outcome": "recorded", "receipt_id": binding["receipt_id"]}

    def submit(self, request: dict[str, Any]) -> tuple[dict[str, Any], dict[str, str] | None]:
        receipt_id = text(request, "receipt_id", 128, TOKEN_RE)
        event_id = user_event_id(request)
        report = validate_report(request.get("report"))
        event = {"event_id": event_id, "kind": "valid_report", "report": report}
        with self.lock():
            store = self.load()
            receipt = self.find(store, receipt_id)
            validate_proof(request, receipt["binding"])
            if replay(receipt, event):
                return {"outcome": "duplicate", "receipt_id": receipt_id, "state": receipt["state"]}, None
            if receipt["state"] != "created":
                raise BridgeError("receipt already contains a different valid report")
            timestamp = now()
            receipt["state"] = "valid_report"
            receipt["report_event_id"] = event_id
            receipt["updated_at"] = timestamp
            receipt["events"].append({**event, "at": timestamp})
            notification = receipt["binding"]["notification_target"]
            if notification is not None:
                intent = notification_event(receipt_id, event_id, notification, "intent", "notification_intent", "attempt_once")
                receipt["events"].append({**intent, "at": timestamp})
            self.commit(store)
        return {"outcome": "recorded", "receipt_id": receipt_id, "state": "valid_report"}, notification

    def record_notification(self, receipt_id: str, report_id: str, target: dict[str, str], kind: str, detail: str) -> None:
        event = notification_event(receipt_id, report_id, target, "result", kind, detail)
        with self.lock():
            store = self.load()
            receipt = self.find(store, receipt_id)
            if find_event(receipt, event["event_id"]) is not None:
                raise BridgeError("notification outcome is already recorded")
            timestamp = now()
            receipt["events"].append({**event, "at": timestamp})
            receipt["updated_at"] = timestamp
            self.commit(store)

    def consume(self, request: dict[str, Any]) -> dict[str, Any]:
        exact_fields(request, {"receipt_id", "event_id", "origin_thread", "coordinator_token"}, "inbox consumption")
        receipt_id = text(request, "receipt_id", 128, TOKEN_RE)
        event_id = user_event_id(request)
        with self.lock():
            store = self.load()
            receipt = self.find(store, receipt_id)
            self.authorize_coordinator(receipt, request)
            report_id = receipt["report_event_id"]
            event = {"event_id": event_id, "kind": "delivered", "report_event_id": report_id}
            if replay(receipt, event):
                source = find_event(receipt, report_id)
                return {"outcome": "duplicate", "receipt_id": receipt_id, "state": receipt["state"], "report": source["report"]}
            if receipt["state"] != "valid_report" or not isinstance(report_id, str):
                raise BridgeError("delivery requires the current valid_report")
            source = find_event(receipt, report_id)
            timestamp = now()
            receipt["state"] = "delivered"
            receipt["updated_at"] = timestamp
            receipt["events"].append({**event, "at": timestamp})
            self.commit(store)
        return {"outcome": "recorded", "receipt_id": receipt_id, "state": "delivered", "report": source["report"]}

    def acknowledge(self, request: dict[str, Any]) -> dict[str, Any]:
        exact_fields(request, {"receipt_id", "event_id", "report_event_id", "origin_thread", "coordinator_token"}, "acknowledgement")
        receipt_id = text(request, "receipt_id", 128, TOKEN_RE)
        event_id = user_event_id(request)
        report_id = text(request, "report_event_id", 128, TOKEN_RE)
        with self.lock():
            store = self.load()
            receipt = self.find(store, receipt_id)
            self.authorize_coordinator(receipt, request)
            event = {"event_id": event_id, "kind": "acknowledged", "report_event_id": report_id}
            if replay(receipt, event):
                return {"outcome": "duplicate", "receipt_id": receipt_id, "state": receipt["state"]}
            if receipt["state"] != "delivered" or receipt["report_event_id"] != report_id:
                raise BridgeError("acknowledgement requires delivery of the same valid_report")
            timestamp = now()
            receipt["state"] = "acknowledged"
            receipt["updated_at"] = timestamp
            receipt["events"].append({**event, "at": timestamp})
            self.commit(store)
        return {"outcome": "recorded", "receipt_id": receipt_id, "state": "acknowledged"}

    def show(self, receipt_id: str) -> dict[str, Any]:
        with self.lock():
            receipt = self.find(self.load(), receipt_id)
            public = json.loads(json.dumps(receipt))
        public.pop("coordinator_token_hash")
        public["binding"].pop("producer_nonce_hash")
        for event in public["events"]:
            if event.get("kind") == "created":
                event.pop("binding")
                event["binding_present"] = True
            if event.get("kind") == "valid_report":
                event.pop("report")
                event["report_present"] = True
        return public


def notification_event(
    receipt_id: str,
    report_id: str,
    target: dict[str, str],
    phase: str,
    kind: str,
    detail: str,
) -> dict[str, Any]:
    digest = hashlib.sha256(f"{receipt_id}\0{report_id}".encode()).hexdigest()
    return {
        "event_id": f"{INTERNAL_EVENT_PREFIX}notify:{phase}:{digest}",
        "kind": kind,
        "report_event_id": report_id,
        "target": target,
        "detail": detail,
    }


def notification_result(
    store: Store,
    receipt_id: str,
    report_id: str,
    target: dict[str, str],
    kind: str,
    detail: str,
    outcome: str,
) -> dict[str, Any]:
    try:
        store.record_notification(receipt_id, report_id, target, kind, detail)
    except BridgeError:
        return {"notification": "indeterminate"}
    return {"notification": outcome}


def notify(store: Store, receipt_id: str, report_id: str, target: dict[str, str]) -> dict[str, Any]:
    token = f"AMUX_TYCHO_REPORT receipt={receipt_id} correlation={report_id}"
    try:
        observed = subprocess.run(
            ["tmux", "display-message", "-p", "-t", target["pane_id"], "#{pane_id}\t#{pane_created}\t#{pane_current_command}"],
            check=True, capture_output=True, text=True, timeout=2,
        ).stdout.rstrip("\n").split("\t")
        if observed != [target["pane_id"], target["pane_created"], "amp"]:
            return notification_result(store, receipt_id, report_id, target, "notification_failed", "stale_or_non_amp_pane", "failed")
        subprocess.run(["tmux", "send-keys", "-t", target["pane_id"], "-l", token], check=True, timeout=2)
        subprocess.run(["tmux", "send-keys", "-t", target["pane_id"], "Enter"], check=True, timeout=2)
    except subprocess.TimeoutExpired:
        return notification_result(store, receipt_id, report_id, target, "notification_indeterminate", "tmux_timeout_no_retry", "indeterminate")
    except (OSError, subprocess.CalledProcessError):
        return notification_result(store, receipt_id, report_id, target, "notification_failed", "tmux_failure", "failed")
    return notification_result(store, receipt_id, report_id, target, "notification_succeeded", "wake_up_only", "succeeded")


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--state-dir", required=True)
    sub = result.add_subparsers(dest="command", required=True)
    sub.add_parser("create")
    sub.add_parser("submit")
    sub.add_parser("consume")
    sub.add_parser("acknowledge")
    show = sub.add_parser("show")
    show.add_argument("--receipt-id", required=True)
    return result


def main() -> int:
    args = parser().parse_args()
    store = Store(pathlib.Path(args.state_dir))
    try:
        if args.command == "create":
            result = store.create(read_request())
        elif args.command == "submit":
            request = read_request()
            result, target = store.submit(request)
            if target is not None:
                result.update(notify(store, result["receipt_id"], user_event_id(request), target))
        elif args.command == "consume":
            result = store.consume(read_request())
        elif args.command == "acknowledge":
            result = store.acknowledge(read_request())
        else:
            result = store.show(args.receipt_id)
    except LockBusy as error:
        print(json.dumps({"error": str(error), "outcome": "rejected"}, sort_keys=True), file=sys.stderr)
        return 2
    except BridgeError as error:
        print(json.dumps({"error": str(error), "outcome": "rejected"}, sort_keys=True), file=sys.stderr)
        return 2
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
