#!/usr/bin/env python3
"""
check-openapi-compat.py — API compatibility lint for openapi.yaml.

Usage:
    # Check new/changed operations in a PR (compare against base branch):
    python3 hack/scripts/check-openapi-compat.py --diff-base origin/main

    # Check all operations have x-since (warning only, not an error):
    python3 hack/scripts/check-openapi-compat.py --check-all

Exit codes:
    0  All checks passed (warnings are printed but do not fail)
    1  One or more errors found

Rules enforced:
  - New operations (paths/methods added since base) MUST have x-since set.
  - Modified operations that introduce a breaking change MUST have
    x-breaking-change set to a non-empty version string.
  - Operations missing x-since emit a WARNING (not an error) so existing
    endpoints do not block CI until they are annotated.

Breaking change definition (detected automatically when --diff-base is used):
  - A response field listed in the old spec is absent from the new spec.
  - A previously optional request body field is now required.
  - An operation is removed (path + method no longer exists).
  - A success response status code changes.
"""

from __future__ import annotations

import argparse
import subprocess
import sys
import tempfile
from pathlib import Path

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required: pip install pyyaml")

OPENAPI_PATH = Path("pkg/openapi/native/openapi.yaml")
HTTP_METHODS = {"get", "put", "post", "delete", "options", "head", "patch", "trace"}


def load_spec(path: Path) -> dict:
    with path.open() as f:
        return yaml.safe_load(f)


def load_spec_at_ref(git_ref: str) -> dict | None:
    """Load openapi.yaml as it was at the given git ref. Returns None if not found."""
    try:
        content = subprocess.check_output(
            ["git", "show", f"{git_ref}:{OPENAPI_PATH}"],
            stderr=subprocess.DEVNULL,
        )
        return yaml.safe_load(content)
    except subprocess.CalledProcessError:
        return None


def iter_operations(spec: dict):
    """Yield (path, method, operation_dict) for every operation in the spec."""
    for path, path_item in (spec.get("paths") or {}).items():
        if not isinstance(path_item, dict):
            continue
        for method, operation in path_item.items():
            if method.lower() not in HTTP_METHODS:
                continue
            if isinstance(operation, dict):
                yield path, method.lower(), operation


def response_fields(operation: dict) -> set[str]:
    """Collect all property names from 200/201 response schemas (shallow)."""
    fields: set[str] = set()
    for status, resp in (operation.get("responses") or {}).items():
        if str(status) not in ("200", "201"):
            continue
        content = (resp or {}).get("content") or {}
        for media in content.values():
            schema = (media or {}).get("schema") or {}
            fields.update((schema.get("properties") or {}).keys())
    return fields


def required_request_fields(operation: dict) -> set[str]:
    """Return required fields in the request body schema."""
    body = operation.get("requestBody") or {}
    content = body.get("content") or {}
    for media in content.values():
        schema = (media or {}).get("schema") or {}
        return set(schema.get("required") or [])
    return set()


def success_status_codes(operation: dict) -> set[str]:
    return {str(s) for s in (operation.get("responses") or {}) if str(s).startswith("2")}


def detect_breaking_change(old_op: dict, new_op: dict) -> str | None:
    """Return a human-readable reason if a breaking change is detected, else None."""
    old_fields = response_fields(old_op)
    new_fields = response_fields(new_op)
    removed = old_fields - new_fields
    if removed:
        return f"response fields removed: {sorted(removed)}"

    old_required = required_request_fields(old_op)
    new_required = required_request_fields(new_op)
    added_required = new_required - old_required
    if added_required:
        return f"request fields became required: {sorted(added_required)}"

    old_codes = success_status_codes(old_op)
    new_codes = success_status_codes(new_op)
    if old_codes and old_codes != new_codes:
        return f"success status codes changed: {old_codes} → {new_codes}"

    return None


def check_all(spec: dict) -> tuple[list[str], list[str]]:
    """Return (errors, warnings) for --check-all mode."""
    errors: list[str] = []
    warnings: list[str] = []
    for path, method, op in iter_operations(spec):
        if not op.get("x-since"):
            warnings.append(f"  WARN  {method.upper():7} {path} — missing x-since")
    return errors, warnings


def check_diff(base_spec: dict, new_spec: dict) -> tuple[list[str], list[str]]:
    """Return (errors, warnings) for --diff-base mode."""
    errors: list[str] = []
    warnings: list[str] = []

    old_ops: dict[tuple[str, str], dict] = {
        (p, m): op for p, m, op in iter_operations(base_spec)
    }
    new_ops: dict[tuple[str, str], dict] = {
        (p, m): op for p, m, op in iter_operations(new_spec)
    }

    # Removed operations
    for key in old_ops:
        if key not in new_ops:
            path, method = key
            errors.append(f"  ERROR {method.upper():7} {path} — operation REMOVED (breaking change)")

    for (path, method), op in new_ops.items():
        if (path, method) not in old_ops:
            # New operation — must have x-since
            since = op.get("x-since", "")
            if not since:
                errors.append(
                    f"  ERROR {method.upper():7} {path} — new operation is missing x-since"
                )
            else:
                print(f"  OK    {method.upper():7} {path} — new, x-since={since}")
        else:
            # Existing operation — check for breaking changes
            old_op = old_ops[(path, method)]
            reason = detect_breaking_change(old_op, op)
            if reason:
                breaking = op.get("x-breaking-change", "")
                if not breaking:
                    errors.append(
                        f"  ERROR {method.upper():7} {path} — breaking change detected "
                        f"({reason}) but x-breaking-change is not set"
                    )
                else:
                    print(
                        f"  OK    {method.upper():7} {path} — breaking change documented "
                        f"in x-breaking-change={breaking}"
                    )

    return errors, warnings


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument(
        "--diff-base",
        metavar="GIT_REF",
        help="Compare against this git ref (branch or commit). Errors on new operations without x-since.",
    )
    parser.add_argument(
        "--check-all",
        action="store_true",
        help="Warn about all operations missing x-since (non-blocking).",
    )
    parser.add_argument(
        "--spec",
        default=str(OPENAPI_PATH),
        help=f"Path to openapi.yaml (default: {OPENAPI_PATH})",
    )
    args = parser.parse_args()

    spec_path = Path(args.spec)
    if not spec_path.exists():
        print(f"ERROR: spec not found at {spec_path}", file=sys.stderr)
        return 1

    new_spec = load_spec(spec_path)
    errors: list[str] = []
    warnings: list[str] = []

    if args.diff_base:
        base_spec = load_spec_at_ref(args.diff_base)
        if base_spec is None:
            print(
                f"WARN: could not load spec at {args.diff_base} — assuming all operations are new",
                file=sys.stderr,
            )
            base_spec = {"paths": {}}
        e, w = check_diff(base_spec, new_spec)
        errors.extend(e)
        warnings.extend(w)
    elif args.check_all:
        e, w = check_all(new_spec)
        errors.extend(e)
        warnings.extend(w)
    else:
        parser.print_help()
        return 0

    if warnings:
        print("\nWarnings (non-blocking):")
        for w in warnings:
            print(w)

    if errors:
        print("\nErrors (blocking):")
        for e in errors:
            print(e)
        return 1

    print("\nAll compatibility checks passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
