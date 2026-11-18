#!/usr/bin/env python3
"""Score a manually produced Apilot workbench UI report."""
import argparse
import json
from pathlib import Path
import re
import sys


RUBRIC = (Path(__file__).resolve().parents[1] /
          ".agents/skills/workbench-ui-lint/references/checks.json")
POINTS = {"pass": 1, "partial": 0.5, "fail": 0, "not_verified": 0, "not_applicable": 0}


def template(rubric):
    return {
        "schema_version": rubric["schema_version"],
        "commit": "",
        "scope": "",
        "mode": "fixture",
        "checks": {
            key: {"status": "not_verified", "evidence": ""}
            for key in rubric["checks"]
        },
    }


def score(rubric, report):
    if not isinstance(report, dict) or report.get("schema_version") != rubric["schema_version"]:
        raise ValueError("unsupported report schema_version")
    commit = report.get("commit")
    if not isinstance(commit, str) or not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("commit must be the full Git SHA")
    scope = report.get("scope")
    if not isinstance(scope, str) or not scope.strip():
        raise ValueError("scope must describe the reviewed surface")
    if report.get("mode") not in ("fixture", "live", "mixed"):
        raise ValueError("mode must be fixture, live or mixed")
    specs = rubric["checks"]
    checks = report.get("checks")
    if not isinstance(checks, dict) or checks.keys() != specs.keys():
        raise ValueError("checks must contain exactly the rubric IDs")

    earned = verified = possible = 0
    counts = dict.fromkeys(POINTS, 0)
    blockers = []
    needs_attention = []
    results = {}
    for key, spec in specs.items():
        check = checks[key]
        if not isinstance(check, dict):
            raise ValueError(f"{key}: expected a check object")
        status, evidence = check.get("status"), check.get("evidence")
        if not isinstance(status, str) or status not in POINTS:
            raise ValueError(f"{key}: invalid status")
        if not isinstance(evidence, str) or not evidence.strip():
            raise ValueError(f"{key}: evidence or a reason is required")
        counts[status] += 1
        weight = spec["weight"]
        if status == "not_applicable":
            if not spec.get("optional"):
                raise ValueError(f"{key}: not_applicable is not allowed")
            weight = 0
        elif status != "pass":
            needs_attention.append(key)
            if spec["critical"]:
                blockers.append(key)
        points = weight * POINTS[status]
        possible += weight
        earned += points
        if status != "not_verified":
            verified += weight
        results[key] = {"status": status, "earned": points, "possible": weight, "evidence": evidence}

    verdict = "needs_work"
    if blockers:
        verdict = "blocked"
    elif earned * 100 >= rubric["pass_score"] * possible and verified == possible:
        verdict = "pass"
    return {
        "schema_version": rubric["schema_version"],
        "commit": commit,
        "scope": scope,
        "mode": report["mode"],
        "score": round(100 * earned / possible, 2),
        "coverage": round(100 * verified / possible, 2),
        "verdict": verdict,
        "counts": counts,
        "blockers": blockers,
        "needs_attention": needs_attention,
        "checks": results,
    }


def unique_keys(pairs):
    fields = {}
    for key, value in pairs:
        if key in fields:
            raise ValueError(f"duplicate JSON key {key!r}")
        fields[key] = value
    return fields


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    inputs = parser.add_mutually_exclusive_group(required=True)
    inputs.add_argument("report", nargs="?", type=Path)
    inputs.add_argument("--template", action="store_true")
    args = parser.parse_args()
    try:
        rubric = json.loads(RUBRIC.read_text(encoding="utf-8"), object_pairs_hook=unique_keys)
        if args.template:
            print(json.dumps(template(rubric), ensure_ascii=False, indent=2))
            return 0
        report = json.loads(args.report.read_text(encoding="utf-8"), object_pairs_hook=unique_keys)
        result = score(rubric, report)
    except (OSError, ValueError) as error:
        print(error, file=sys.stderr)
        return 2
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0 if result["verdict"] == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
