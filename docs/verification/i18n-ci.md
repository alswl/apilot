# i18n and release verification — 2026-11-12

Branch: `feat/i18n-release`. Reference: sibling `chatta` CI/tag release workflows. Release candidate: `v0.0.2`, macOS arm64.

## Interface

138 messages in English and Simplified Chinese, including menus, tasks, observers, settings, CLI installation and notification titles. MyGo resolves the macOS preferred app language; unsupported languages fall back to English. Packaging declares both languages. Native MyGo controls load their Chinese translation too. User names, commands, logs, stored event summaries, service errors and CLI JSON remain unchanged; CLI help remains English.

Catalog coverage, placeholder counts, static translation-call coverage and both languages' empty task/history views pass. Existing UI navigation assertions follow the active language. `go test -race ./...`, `go vet ./...`, YAML parsing and `git diff --check` pass. actionlint was unavailable.

Both languages passed the actual App lifecycle probe: accessory startup, open management window, task sheet, CLI window, dark/narrow views, close/reopen and quit. English/Chinese task-sheet screenshots were visually checked. Screenshots and probe logs: `tmp/i18n-ci/{en,zh-Hans}/` (local, ignored).

Identical consecutive `make app` builds, without cleaning: 7.29s / 6.57s. Compilation: 1.3s / 0.632s; DMG generation: 4.8s / 4.7s. DMG packaging deliberately repeats and dominates incremental time; do not hide inputs to manufacture a no-op.

## Actions baseline

Requested 50 runs; repository has only 6. All attempts' job pages were collected. Raw runs, jobs, selected logs and analysis are in `tmp/i18n-ci/`. Three successful PR runs, two cancelled runs, one failed tag run. No system workflows. Skipped release jobs do not count as executed work.

| Run | Event / result | Job wait s | Preparation s | Target work s | Finishing s | Unattributed s | Runner s |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| [43058323029](https://github.com/alswl/apilot/actions/runs/43058323029) attempt 1 | push / cancelled | 0 | 0 | 0 | 0 | 902.0 | 902.0 |
| [43058295982](https://github.com/alswl/apilot/actions/runs/43058295982) attempt 1 | push / cancelled | 3.0 | 17.0 | 0 | 1.0 | 5.0 | 23.0 |
| [43058295982](https://github.com/alswl/apilot/actions/runs/43058295982) attempt 1 | push / cancelled | 0 | 0 | 0 | 0 | 0 | 0 |
| [43058277877](https://github.com/alswl/apilot/actions/runs/43058277877) attempt 1 | pull_request / cancelled | 0 | 0 | 0 | 0 | 25.0 | 25.0 |
| [43058277877](https://github.com/alswl/apilot/actions/runs/43058277877) attempt 1 | pull_request / cancelled | 0 | 0 | 0 | 0 | 0 | 0 |
| [43052101503](https://github.com/alswl/apilot/actions/runs/43052101503) attempt 1 | pull_request / success | 11.0 | 42.0 | 24.0 | 10.0 | 26.0 | 102.0 |
| [42994016694](https://github.com/alswl/apilot/actions/runs/42994016694) attempt 1 | pull_request / success | 5.0 | 52.0 | 34.0 | 15.0 | 6.0 | 107.0 |
| [42988716089](https://github.com/alswl/apilot/actions/runs/42988716089) attempt 2 | pull_request / success | 4.0 | 26.0 | 4.0 | 4.0 | 5.0 | 39.0 |

Job wait is created→started, not assumed to be runner scheduling alone. Empty-step cancelled jobs provide no evidence of executed tests; their started→completed interval cannot reliably be attributed to runner execution or billing. Preparation includes runner/checkout/toolchain/cache/platform verification and explicit dependency listing. Target work is format/vet/race/build. Finishing includes packaging, upload, summary and post actions. Step timestamps are second-resolution; unexplained intervals stay unattributed. Critical path is the single build job, followed by publication only for tags.

The comparable recent successful PR runs took 114s end-to-end each; runner durations were 107s and 102s. The older attempt used different packaging and took 39s runner time, so it is not a like-for-like comparison. Across three successful runs, runner p50=102s, p95=107s (small sample, descriptive only). Full run creation→completion for attempt 2 includes earlier-attempt time and must not be treated as queue time.

Selected baseline cache log: Go acquisition ~20s, cache download/extraction ~13s; cache hit, 407,429,220 bytes. Setup total 36s in that run. Cache usage at inspection: one cache, 407,429,220 bytes. Successful baseline setup steps took 21s, 44s and 36s. No evidence supports deleting the useful compilation cache or sharding this short suite.

Rulesets returned empty; master branch protection returned “Branch not protected”. Account plan was unavailable from `gh api user`; no account-wide concurrency or budget claim is made. Repository is public with standard hosted runners: compute is free under [GitHub's current billing rules](https://docs.github.com/en/billing/concepts/product-billing/github-actions), checked 2026-11-12. Storage is separate. No dollar savings claimed.

## Optimization

Removed two explicit dependency-graph scans (1–2s in successful history); vet/test/build resolve their own dependencies and MyGo tool dependencies. Kept setup-go's native cache, pinned actions, macOS race tests/build, artifact compression=0 and seven-day retention. Existing PR-only cancellation, unique non-PR run groups, source path filtering, and master-only push versus PR triggers already eliminate the relevant waste. No Ubuntu gate or extra shards: additional jobs would add preparation for negligible work and would not replace necessary macOS tests. No trusted precompiled MyGo binary exists for the exact pinned commit, so the pinned Go tool is retained.

Tag publication verifies artifact checksums from the same run and retains Developer ID signing/notarization. Release notes now describe the two supported interface languages. Existing `v0.0.1` failed without any steps/logs in its build job; no cause inferred and tag left unchanged.

## Remote validation

Pending branch dispatch and signed `v0.0.2` release. PR cancellation/doc filtering and main independent runs are statically checked; this task does not manufacture PR/main commits solely to exercise existing behavior.
