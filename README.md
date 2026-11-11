<img src="resources/icon.png" width="88" height="88" alt="apilot icon" />

# Apilot — Agent Pilot

A lightweight, agent-friendly automation sidecar for macOS.

Apilot schedules local commands while its App is open. Close the window to keep it in the menu bar; choose Quit to stop scheduling and terminate managed tasks. Tasks, history and settings stay on this Mac. No account, cloud service or separate daemon.

## Downloads

- Tagged builds: [GitHub Releases](https://github.com/alswl/apilot/releases), with a drag-to-Applications DMG, App ZIP, standalone CLI archive and SHA256SUMS.
- Development builds: [Build macOS Actions](https://github.com/alswl/apilot/actions/workflows/build.yml). Open a successful run and download its `apilot-macos-arm64-RUN_ID-ATTEMPT` artifact. GitHub sign-in is required; artifacts expire after 7 days.

Downloads currently target Apple Silicon only. Unzip the artifact, open `apilot-macos-arm64.dmg` and drag the App into Applications. The CLI archive preserves executable permissions. Verify checksums with `shasum -a 256 --check SHA256SUMS`.

Development builds are ad-hoc signed and not notarized; macOS may require explicit approval before opening the App. Release signing/notarization can be enabled with repository Secrets; see [distribution setup](docs/distribution.md). No certificate or private key is committed.

## Build and launch

Requires macOS 12+, Xcode Command Line Tools and Go 1.27.3. An installed Go 1.26.5 can bootstrap the required toolchain using go.mod/GOTOOLCHAIN; `.tool-versions` selects that installed bootstrap. Dependency downloads require network access. Local command execution does not require a network connection.

```sh
make build app
open build/darwin-arm64/Apilot.app
./bin/apilot status
```

The default build uses the current architecture. The arm64 App has been built and run locally; macOS 12 and Intel Macs have not been tested. The bundle uses ad-hoc signing for local use. Developer ID signing and notarization are available for tagged CI builds after credential setup; live notarization has not been verified.

To install, copy the App into Applications and accept the first-launch CLI installation offer, or open it later from Settings → 命令行工具. The CLI connects to the running App and never starts another scheduler. With the App closed, `apilot status --json` returns `APP_NOT_RUNNING` and exit code 3.

## Small native window

The main window has **任务 / 历史 / 观察** tabs. Open a task for its command, schedule and history; open a run to read its logs. New and edited tasks use a sheet. Settings open separately through the menu or `⌘,`. The main window starts at 640×480 and supports a narrower layout.

Task commands use an executable and separate arguments. Choose Shell explicitly to use `/bin/zsh -lc`. A disabled task can still be run manually. The automatic schedule and task can be enabled independently. Defaults: no overlap for the same task, four concurrent tasks, 30-minute timeout, 30-day history retention and 50 MiB per stdout/stderr file. Capacity conflicts are recorded as skipped; missed schedules are not replayed.

## CLI

```sh
apilot task add daily-review \
  --cron '0 9 * * 1-5' \
  --cwd "$HOME/projects/demo" \
  -- codex exec "Review today's changes"

apilot task list --json
apilot task run daily-review --json
apilot run list --task daily-review --limit 20 --json
apilot run show RUN_ID --json
apilot logs RUN_ID --stream stderr
apilot logs RUN_ID --follow
apilot run stop RUN_ID
apilot task update daily-review --timeout 10m
apilot task disable daily-review
apilot task remove daily-review
apilot doctor --json
```

Use `--json` **before** the `--` command separator; anything after the separator is a task argument. JSON fields are versioned, timestamps use UTC and unknown execution times/exit codes are null. `task run` returns the admitted Run immediately; poll `run show` for its final result. A skipped run is still a successfully recorded trigger.

```sh
apilot observe add herdr --interval 30s --output json -- herdr status --json
apilot observe list --json
apilot observe disable herdr
apilot settings show --json
apilot settings update --max-concurrent 2 --notify-success=false
apilot event list --unread --json
apilot event ack EVENT_ID
```

An observer records its latest check, not Task Run history. Its exit code determines health; JSON mode also requires valid bounded JSON. Observers have a separate execution slot and never repair or control external tools automatically. Users must choose read-only commands.

For credentials, use `--env-file PATH`, with one literal `KEY=VALUE` per line. Environment values are not returned by list/show or automatically logged. Explicit environment definitions are stored in the private local database; they are not encrypted. Task commands can print secrets themselves, so treat logs as private and avoid putting credentials in command arguments.

The App inherits the environment from its launch context. Finder's PATH may differ from your shell; prefer absolute executable paths. CLI-created tasks default to the CLI's current directory; GUI tasks visibly default to the user's home directory. Shell startup files are only used when explicitly choosing Shell mode.

## Data and execution

Data: `~/Library/Application Support/apilot/apilot.db`, with logs below `logs/`. The App owns the SQLite write connection and a private Unix Socket. `APILOT_DATA_DIR` selects a separate local profile, primarily useful for isolated testing; CLI and App must use the same value.

Every active Run has a finite execution helper inside the App binary. It has no scheduler, database or management server. If the App disappears, the helper terminates the command's process group and exits. On restart, unfinished Runs become interrupted, with unknown actual finish times left null. Processes deliberately detaching into another process group, or the execution helper itself being killed, are limitations.

Deleting a task keeps its history and never deletes its working directory. Stop an active run before deleting its task. Log output is capped per stream and continues to drain after truncation. History cleanup only affects App-owned terminal records and files.

## Validation

```sh
make test
make race
make verify-ui
make verify-lifecycle
```

These checks use disposable data. See [verification results](docs/verification/final.md), [architecture](docs/architecture.md) and [known limitations](docs/known-limitations.md). Runtime screenshots live in ignored `tmp/` and are not part of the source distribution.

License: [MIT](LICENSE).

## CI and release policy

One standard macOS arm64 runner runs vet, the full race-enabled test suite and the App build. Go's setup action caches module downloads and compilation. The App is built once; the CI does not also build the unused development App binary. Pure documentation changes skip this workflow. PRs keep only their newest run; each default-branch push gets its own run. Downloads are compressed before upload and retained for 7 days.

The workflow runs on PRs, relevant pushes to `master`, `v*` tags and manual dispatch. After merging it, push a tag matching the version in `packaging/mygo.json` to publish a GitHub Release. The Linux publication job reuses that run's verified files; it never rebuilds the App. Tag publication requires an intentional tag push and has not been exercised by the initial PR build.

### 菜单栏与安装

打开 DMG，将 Apilot 拖入 Applications；启动后常驻菜单栏。任务菜单可直接运行、停止、开关自动运行或编辑，新建任务也无需先打开主窗口。「全部任务」提供任务 / 历史 / 观察三个 tab。

首次启动提供 CLI 安装入口，将 App 内的工具链接到 `~/.local/bin/apilot`，不需要管理员权限，也不覆盖已有文件。若终端找不到命令，将 `export PATH="$HOME/.local/bin:$PATH"` 加入 `~/.zshrc`。可随时从设置的「命令行工具」重新打开安装入口。

DMG、Developer ID 签名和自动公证的配置见 [分发说明](docs/distribution.md)。开发构建保持临时签名；配置 Apple 凭据并启用后，仅正式标签构建执行签名与公证。
