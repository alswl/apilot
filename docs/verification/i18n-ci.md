# i18n and release verification — 2026-11-12

Branch: `feat/i18n-release`. Reference: sibling `chatta` CI/tag release workflows. Release candidate: `v0.0.2`, macOS arm64.

## Interface

137 messages in English and Simplified Chinese, including menus, tasks, observers, settings, CLI installation and notification titles. MyGo resolves the macOS preferred app language; unsupported languages fall back to English. Packaging declares both languages. Native MyGo controls load their Chinese translation too. User names, commands, logs, stored event summaries, service errors and CLI JSON remain unchanged; CLI help remains English.

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

Branch cold-cache dispatch [43120980145](https://github.com/alswl/apilot/actions/runs/43120980145) succeeded on source `837d178`. All formatting, vet, race, development App/DMG build and download verification passed. Cache miss; cache upload saved 408,155,403 bytes in a 13-second post step. Go acquisition plus setup took 15s, with no module/build restore. A superseded queued dispatch [43120862273](https://github.com/alswl/apilot/actions/runs/43120862273) was manually cancelled before any steps after the CLI version fix.

| Metric | Recent baseline PRs (n=2, warm) | New branch dispatch (n=1, cold) | Interpretation |
| --- | ---: | ---: | --- |
| End-to-end p50 / descriptive p95 | 114s / 114s | 191s / insufficient sample | Different cache state and source; not a causal speed comparison |
| Job wait | 5s / 11s | 4s | created→started, not billing |
| Preparation | 42s / 52s | 19s | Cold run skips module/build cache restore |
| Target work | 24s / 34s | 141s | Cold compilation included in actual target commands |
| Finishing | 10s / 15s | 22s | New cache save adds 13s |
| Unattributed | 26s / 6s | 3s | Left explicit |
| macOS runner minutes, total / per run | 3.48 / 1.74 | 3.08 / 3.08 | No Linux job actually executes on non-tag dispatch |
| Cache | Selected log: hit, 407,429,220 B; restore ~13s | Miss; saved 408,155,403 B | Branch/default/PR cache visibility differs |
| Success | 2/2 | 1/1 | Same macOS checks; UI gained bilingual coverage |
| Duplicate runs per trigger | 1 effective workflow per PR push | 1 effective workflow per dispatch | No branch push trigger added |
| Compute charge | Public standard hosted runner: free | Same | No monetary savings claimed |

The residual bottleneck on cold cache is compilation in vet/race/build. Splitting it into extra jobs would duplicate preparation and cannot remove platform-specific compilation. Keep the native setup-go cache rather than add another overlapping cache. A second identical branch dispatch was requested for warm-cache behavior; signed tag run is a separate workload.

Tag `v0.0.2` points at `837d178` and is pushed. CLI and App status now share `internal/version.Version`; packaging rejects a CLI/App version mismatch. Signed release and warm dispatch results are recorded below.

PR cancellation/doc filtering and main independent runs are statically checked; this task does not manufacture PR/main commits solely to exercise existing behavior.


### Release blocker

[v0.0.2 run 43121424733](https://github.com/alswl/apilot/actions/runs/43121424733) passed formatting, vet and race tests, then failed before signing: `security: SecKeychainItemImport: MAC verification failed during PKCS12 import (wrong password?)`. No release or downloads were published. The certificate payload/password Secrets must be repaired as a matching pair; no secret values were printed or read back. Existing `v0.0.2` tag is retained; rerun the failed jobs after repair.

A local hidden-input repair wizard is available at `tmp/i18n-ci/repair-signing.sh` (ignored). It verifies the exported .p12 and the exact Developer ID identity using an isolated temporary keychain, updates only `APPLE_CERTIFICATE_P12_BASE64` and `APPLE_CERTIFICATE_PASSWORD` in `alswl/apilot`, then retries the existing run without retagging. It does not change the API key or signing identity. `bash -n` passes; stage-specific shellcheck passes with SC1087/SC2329 excluded for the unchanged wizard template's Readline string and unused helper functions. The interactive procedure was not executed; it requires the user's certificate file and hidden password.

Warm dispatch [43121456108](https://github.com/alswl/apilot/actions/runs/43121456108) remained queued without a runner or executed steps for over five minutes and was cancelled to avoid leaving unnecessary work queued while credentials are unresolved. Remote warm-cache success is therefore unverified; the two local incremental builds passed. No p95 or speedup is inferred from one cold remote sample.

Remaining risks: signed/notarized distribution is blocked by credentials; Intel and macOS 12 runtime support were not exercised; dynamic language changes in already-open native window titles were not tested (documented workflow is reopen the app). PR cancellation, document-only filtering, failing gate behavior and main/tag independence retain their existing workflow logic and were statically reviewed, not exhaustively exercised.

Historical PR caches are scoped to their merge refs and cannot warm a new branch/tag. A successful trusted default-branch build can provide a cache for future branches/tags; no master push or merge was performed in this task. See [GitHub cache access restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#restrictions-for-accessing-a-cache). Cache usage after cold dispatch: two caches, 815,584,623 bytes, well below the default 10 GiB allowance.


## Release completed — 2026-11-12

The user repaired the matching certificate/password Secrets. Attempt 2 of run 43121424733 succeeded, including signing, Apple notarization, staple validation and publication: https://github.com/alswl/apilot/releases/tag/v0.0.2 . All five release assets exist. Downloaded archives and metadata passed SHA256SUMS; the downloaded App reports Developer ID Application: Jingchao Di (3494952NTZ) and its stapled ticket validates locally. Source is clean commit 837d178. This resolves the earlier credential blocker.

A metadata detection bug was found during downloaded-artifact verification: `codesign -dv` omits Authority on this system; `-dvv` exposes it. Packaging now uses `-dvv`. Published build-info signing was corrected to developer-id using the downloaded App's verified authority, SHA256SUMS regenerated, and release notes corrected; executable archives were unchanged.

Future tag releases reuse the GitHub Secrets; no repeat export is needed until credentials change. The current public signing certificate expires 2027-02-01 22:12:15 UTC (2027-02-02 in Asia/Shanghai). A private-key-only .p12 can be combined with the matching public certificate read from the local keychain; the repair wizard checks public-key equality before packaging, validates an isolated-keychain identity, and uploads the combined archive and its matching password. No .p7b export is needed.

## Patch candidate v0.0.3 — Settings language selection

Added Settings → Language with Follow System, English and 简体中文, using MyGo ChosenLocale/SetLocale for immediate application and persistence. Menu bar, application menu and auxiliary window titles refresh with the selected language. User data and CLI contracts remain unchanged. Picker interaction tests exercise all three choices in both interface languages. Full `go test -race ./...`, `go vet ./...`, `git diff --check`, local App/DMG build and packaged download checksums passed. Version is synchronized at 0.0.3 in the App and CLI. The user accepted local UI behavior and authorized squash merge and patch release; existing release tags remain unchanged.
