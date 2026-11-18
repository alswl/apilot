---
name: workbench-ui-lint
description: Manually review Apilot's macOS workbench against an evidence-based rubric and produce a scored JSON report. Invoke explicitly with $workbench-ui-lint or an explicit request to run the workbench UI lint.
---

# Workbench UI lint

Run this skill on explicit request. Use the [Apilot UI standard](../../../docs/standards/apilot-macos-ui.md) and the [rubric and report contract](references/rubric.md).

1. Record the commit, reviewed surface and fixture or live mode. Run `make check-workbench` and record its result as supporting evidence.
2. Use the real desktop UI for placement, clipping, discoverability and focus verification. Reproduce the relevant workflows in both languages at the documented viewport sizes. Cite screenshots, a recording, or reproducible steps with observed results. Go tests can support a finding; they cannot establish native visual behavior by themselves.
3. Generate a report skeleton with `python3 scripts/score-workbench-ui.py --template > /tmp/apilot-ui-review.json`. Fill every check. Leave an unavailable test as `not_verified` with the reason. Use `not_applicable` only where the rubric allows it, with a rationale.
4. Run `python3 scripts/score-workbench-ui.py /tmp/apilot-ui-review.json`. Return the input report path and the scored output with the score, evidence coverage, verdict and check IDs requiring work. Keep the report outside the repository unless the user requests a committed artifact.
