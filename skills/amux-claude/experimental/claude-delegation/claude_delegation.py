#!/usr/bin/env python3
"""Read-only inspection for historical Amux-bound Claude evidence."""

from __future__ import annotations

import argparse
import json
import pathlib
import stat
import sys
from typing import Any


MAX_STORE_BYTES = 4 * 1024 * 1024
CLOSED_MESSAGE = (
    "worker-bound Claude delegation is closed; preserve historical evidence "
    "without provider mutation"
)


class HelperError(Exception):
    """A fail-closed command rejection."""


def default_state_dir() -> pathlib.Path:
    return (
        pathlib.Path.home()
        / "Library"
        / "Application Support"
        / "amux"
        / "experimental"
        / "claude-delegation"
    )


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--state-dir", type=pathlib.Path, default=default_state_dir())
    parser.add_argument("command", nargs=argparse.REMAINDER)
    arguments = parser.parse_args()
    if not arguments.command:
        raise HelperError("a read-only inspection command is required")
    return arguments


def read_request() -> dict[str, Any]:
    try:
        value = json.load(sys.stdin)
    except (json.JSONDecodeError, UnicodeDecodeError) as error:
        raise HelperError("stdin must contain one JSON object") from error
    if not isinstance(value, dict):
        raise HelperError("stdin must contain one JSON object")
    return value


def read_store(state_dir: pathlib.Path) -> dict[str, Any]:
    path = state_dir.expanduser().resolve() / "receipts.json"
    try:
        metadata = path.lstat()
    except FileNotFoundError as error:
        raise HelperError("historical Claude receipt store is unavailable") from error
    if not stat.S_ISREG(metadata.st_mode) or path.is_symlink():
        raise HelperError("historical Claude receipt store is not a regular file")
    if metadata.st_size > MAX_STORE_BYTES:
        raise HelperError("historical Claude receipt store exceeds the inspection limit")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise HelperError("historical Claude receipt store is unreadable") from error
    if (
        not isinstance(value, dict)
        or value.get("schema_version") != 1
        or not isinstance(value.get("receipts"), list)
    ):
        raise HelperError("historical Claude receipt store has an unsupported schema")
    identities: set[str] = set()
    for receipt in value["receipts"]:
        if not isinstance(receipt, dict):
            raise HelperError("historical Claude receipt store is malformed")
        binding = receipt.get("binding")
        events = receipt.get("events")
        if (
            not isinstance(binding, dict)
            or not isinstance(binding.get("delegation_id"), str)
            or not binding["delegation_id"]
            or binding["delegation_id"] in identities
            or not isinstance(events, list)
            or any(not isinstance(event, dict) for event in events)
        ):
            raise HelperError("historical Claude receipt store is malformed")
        identities.add(binding["delegation_id"])
    return value


def receipt_show(state_dir: pathlib.Path, command: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="claude_delegation.py receipt show")
    parser.add_argument("--delegation-id", required=True)
    arguments = parser.parse_args(command)
    store = read_store(state_dir)
    matches = [
        receipt
        for receipt in store["receipts"]
        if receipt["binding"]["delegation_id"] == arguments.delegation_id
    ]
    if len(matches) != 1:
        raise HelperError("historical Claude receipt is unavailable or ambiguous")
    print(json.dumps(matches[0], sort_keys=True, separators=(",", ":")))
    return 0


def diagnose() -> int:
    output = {
        "capabilities": {
            "read_only_delegation": {
                "status": "unavailable",
                "reason": "core Amux worker lifecycle was removed; historical evidence is read-only",
            },
            "mutating_delegation": {
                "status": "unavailable",
                "reason": "core Amux worker lifecycle was removed; historical evidence is read-only",
            },
        },
        "mode": "historical_evidence_inspection_only",
    }
    print(json.dumps(output, sort_keys=True, separators=(",", ":")))
    return 0


def blocked_inspection(action: str) -> int:
    try:
        read_request()
    except HelperError:
        pass
    print(
        json.dumps(
            {
                "action": action,
                "outcome": "blocked",
                "blocker": "historical_evidence_unavailable_or_ambiguous",
            },
            sort_keys=True,
            separators=(",", ":"),
        )
    )
    return 2


def main() -> int:
    arguments = parse_arguments()
    command = arguments.command
    if command == ["diagnose"]:
        return diagnose()
    if len(command) >= 2 and command[:2] == ["receipt", "show"]:
        return receipt_show(arguments.state_dir, command[2:])
    if command == ["quarantine", "inspect"]:
        return blocked_inspection("quarantine_inspect")
    if len(command) >= 2 and command[:2] == ["amp", "inspect"]:
        return blocked_inspection("amp_inspect")
    raise HelperError(CLOSED_MESSAGE)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except HelperError as error:
        print(f"claude-delegation: {error}", file=sys.stderr)
        raise SystemExit(2) from error
