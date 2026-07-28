#!/usr/bin/env python3
"""Fail-closed diagnostic for the unavailable fresh-Orb mutation route."""

from __future__ import annotations

import argparse
import json


MODEL = "claude-opus-4-8"
BLOCKER = "native_fresh_orb_mutation_adapter_unavailable"
REQUIRED_PRIMITIVES = [
    "credentialless_networkless_mutation_and_commit_executor",
    "native_operation_bound_single_use_owner_challenge",
    "native_orb_launch_intent_and_completion_receipt",
    "native_artifact_and_report_import_receipt",
    "native_process_absence_receipt",
    "native_archive_intent_and_result_transaction",
    "native_workspace_cleanup_intent_and_result_transaction",
]


def diagnostic() -> dict[str, object]:
    return {
        "schema_version": 1,
        "outcome": "blocked",
        "blocker": BLOCKER,
        "model": MODEL,
        "authorizing": False,
        "mutation_available": False,
        "real_pilot_authorized": False,
        "required_native_primitives": REQUIRED_PRIMITIVES,
        "remediation": "keep_issue_254_open_and_preserve_read_only_orb_route",
    }


def parser() -> argparse.ArgumentParser:
    root = argparse.ArgumentParser(add_help=False)
    root.add_argument("command", choices=("diagnose",))
    return root


def main() -> int:
    parser().parse_args()
    print(json.dumps(diagnostic(), sort_keys=True, separators=(",", ":")))
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
