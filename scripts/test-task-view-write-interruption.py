#!/usr/bin/env python3
"""Run the task view test with deterministic faults before and after atomic rename."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

root = Path(__file__).resolve().parent.parent
source = root / "desktop/internal/ui/workbench/task_query_storage.go"
rename = "\tif err := os.Rename(file.Name(), path); err != nil {"
sync = "\tparent, err := os.Open(dir)"
original = source.read_text()
if original.count(rename) != 1 or original.count(sync) != 1:
    raise SystemExit("expected exactly one task-view rename and directory sync site")

with tempfile.TemporaryDirectory() as directory:
    scratch = Path(directory)
    replacement = scratch / source.name
    replacement.write_text(original.replace(
        rename,
        '\tif os.Getenv("APILOT_TEST_TASK_VIEW_INTERRUPT") != "" { os.Exit(93) }\n'
        + rename,
    ).replace(
        sync,
        '\tif os.Getenv("APILOT_TEST_TASK_VIEW_DIRECTORY_SYNC") == "1" {'
        ' return true, errors.New("injected directory sync failure") }\n' + sync,
    ))
    overlay = scratch / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {str(source): str(replacement)}}))
    env = dict(os.environ, APILOT_TEST_TASK_VIEW_OVERLAY="1")
    env.pop("APILOT_TEST_TASK_VIEW_INTERRUPT", None)
    env.pop("APILOT_TEST_TASK_VIEW_DIRECTORY_SYNC", None)
    raise SystemExit(subprocess.call([
        "go", "-C", "desktop", "test", "-overlay", str(overlay),
        "./internal/ui/workbench", "-run", "^TestTaskQueryConfig(WriteInterruption|DirectorySyncWarning)$",
        "-count=1", "-v",
    ], cwd=root, env=env))
