#!/usr/bin/env python3
"""Validate bundled workbench data and supported launch examples."""
import json
from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = Path("internal/ui/workbench/fixtures")
SINGLE_FILE_LAUNCH = re.compile(
    r"\bgo run\b[^\n`]*\bcmd/apilot-(?:app|mockup)/[^\s`]+\.go\b"
)


def unique_keys(pairs):
    values = {}
    for key, value in pairs:
        if key in values:
            raise ValueError(f"duplicate JSON key {key!r}")
        values[key] = value
    return values


def check(root):
    errors = []

    def read(path):
        try:
            return json.loads((root / path).read_text(encoding="utf-8"),
                              object_pairs_hook=unique_keys)
        except (OSError, ValueError) as error:
            raise ValueError(f"{path}: {error}") from error

    def index(rows, key, path):
        values = set()
        for number, row in enumerate(rows, start=1):
            value = row.get(key)
            if not isinstance(value, str) or not value.strip() or value in values:
                errors.append(f"{path}: row {number} has missing or duplicate {key}: {value!r}")
            else:
                values.add(value)
        return values

    def reference(value, valid, location):
        if not isinstance(value, str) or value not in valid:
            errors.append(f"{location}: unknown fixture reference {value!r}")

    base_path = FIXTURES / "workbench.json"
    tasks_path = FIXTURES / "tasks_extended.json"
    usage_path = FIXTURES / "usage.json"
    data = read(base_path)
    extended = read(tasks_path)
    usage = read(usage_path)
    for path in sorted((root / FIXTURES).glob("*.json")):
        if path.relative_to(root) not in (base_path, tasks_path, usage_path):
            read(path.relative_to(root))
    for path in sorted((root / "docs").glob("keybindings.*.json")):
        read(path.relative_to(root))

    objects = data["objects"] + extended
    task_ids = index(objects, "source_object_id", f"{base_path}, {tasks_path}")
    connection_ids = index(data["connections"], "connection_id", base_path)
    for path, rows in ((base_path, data["objects"]), (tasks_path, extended)):
        for row in rows:
            location = f"{path}: {row.get('source_object_id', '<missing task ID>')}"
            reference(row.get("connection_id"), connection_ids, location)
            for related in row.get("related", []):
                if "task_id" in related:
                    reference(related["task_id"], task_ids, f"{location}.related")
    for key in ("evidence_refs", "subject_ids"):
        for value in data["feedback"][key]:
            reference(value, task_ids, f"{base_path}: feedback.{key}")
    reference(data["mutation"]["source_object_id"], task_ids, f"{base_path}: mutation")
    for row in usage["syncs"]:
        reference(row.get("connection_id"), connection_ids, f"{usage_path}: syncs")

    paths = [Path("README.md"), Path("cmd/apilot-mockup/README.md")]
    paths.extend(path.relative_to(root) for path in sorted((root / "docs").glob("*.md")))
    for path in paths:
        for line, content in enumerate((root / path).read_text(encoding="utf-8").splitlines(), 1):
            if SINGLE_FILE_LAUNCH.search(content):
                errors.append(f"{path}:{line}: run the app package directory with go run")
    return errors


if __name__ == "__main__":
    try:
        problems = check(ROOT)
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as error:
        problems = [str(error)]
    if problems:
        print("\n".join(problems), file=sys.stderr)
        sys.exit(1)
    print("Workbench fixture, keybinding JSON and launch example checks passed.")
