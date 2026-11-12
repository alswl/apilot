<div align="center">

<img src="resources/icon.png" width="96" height="96" alt="Apilot icon" />

# Apilot — Agent Pilot

**A lightweight, agent-friendly automation sidecar for macOS.**

[![Build macOS](https://github.com/alswl/apilot/actions/workflows/build.yml/badge.svg)](https://github.com/alswl/apilot/actions/workflows/build.yml)

[Install](#install) · [Quick start](#quick-start) · [CLI](#cli) · [Build](#build-from-source) · [Documentation](#documentation)

</div>

Run local commands on a schedule, check their history, and read their logs from a small menu-bar app or the terminal. Apilot works on its own and alongside Codex, Claude Code, Pi or herdr. It needs no account, cloud service or specific agent runtime.

> [!IMPORTANT]
> **Open the App to run the scheduler. Quit the App to stop it.** Closing a window keeps Apilot in the menu bar. The CLI connects to the running App; it never starts a scheduler or background daemon.

## Features

- **Local automation:** five-field cron, fixed intervals and manual runs, with live task updates.
- **Menu-bar controls:** run tasks, stop active work and toggle automatic schedules without opening the main window.
- **History and logs:** execution status, timing, exit codes, separate stdout/stderr and live log following.
- **First-class CLI:** task management, versioned JSON, predictable exit codes and noninteractive operation.
- **Read-only observation:** poll external CLI tools, retain their latest result and report state changes.
- **Native integration:** system menus, compact management tabs, notifications and optional login launch.

Task definitions, settings and execution history stay in local SQLite. Apilot is a sidecar for local automation; agent reasoning, orchestration and messaging remain with your existing tools.

## Install

Published builds are available from [GitHub Releases](https://github.com/alswl/apilot/releases). Download `apilot-macos-arm64.dmg`, open it and drag **Apilot** into **Applications**.

> [!NOTE]
> Development builds are ad-hoc signed. Tagged releases use Developer ID signing and Apple notarization when configured; check each release’s build-info.json for its verified signing status.

Downloads currently target **Apple Silicon**. The configured minimum is macOS 12; that version and Intel Macs have not been validated.

1. Open `/Applications/Apilot.app`. It starts in the menu bar.
2. Accept the first-launch CLI installation offer, or choose **设置… → 命令行工具…** later.
3. Check the connection:

   ```sh
   apilot status
   ```

The installer creates `~/.local/bin/apilot` as a link to the App's bundled CLI. It needs no administrator access and does not overwrite an existing tool. If your shell cannot find it, add this to `~/.zshrc`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Development downloads are under [Actions](https://github.com/alswl/apilot/actions/workflows/build.yml): choose a successful run and download its artifact. These require GitHub sign-in and expire after seven days. Downloads also include an App ZIP, standalone CLI archive and `SHA256SUMS`; verify downloaded files with `shasum -a 256 --check SHA256SUMS`.

## Quick start

Create a harmless hourly task and run it immediately:

```sh
apilot task add hello --interval 1h -- /bin/echo "Hello from Apilot"
apilot task run hello --json
apilot run list --task hello --limit 5 --json
apilot logs RUN_ID
```

Replace `RUN_ID` with the ID returned by `task run`. Triggering returns immediately; use `apilot run show RUN_ID --json` to check completion.

In the menu bar, each shortcut task has **立即运行**, **自动运行** when scheduled, and **编辑…**. **新建任务…** opens the task editor. **全部任务…** opens the **任务 / 历史 / 观察** tabs; select a run to read its logs. Running work and pending reminders appear when relevant, and **执行历史** lists recent completed runs. The menu previews up to five tasks; the complete list remains in the management window.

Choose **设置…** or press `⌘,` for notifications, timeouts, concurrency and retention. Reopen the App to recover the management window if its menu-bar icon is hidden.

## CLI

Schedule a coding-agent command:

```sh
apilot task add daily-review \
  --cron '0 9 * * 1-5' \
  --cwd "$HOME/projects/demo" \
  -- codex exec "Review today's changes"

apilot task list --json
apilot task show daily-review --json
apilot task update daily-review --timeout 10m
apilot task run daily-review --json
apilot run show RUN_ID --json
apilot logs RUN_ID --stream stderr
apilot logs RUN_ID --follow
apilot run stop RUN_ID
apilot task disable daily-review
apilot task enable daily-review
apilot task remove daily-review
```

Commands use an executable and separate arguments. For shell features, explicitly supply a shell, such as `-- /bin/zsh -lc 'your script'`; the GUI also offers Shell mode. A disabled task can still run manually.

> [!TIP]
> Finder's environment, including `PATH`, can differ from your terminal's. Prefer absolute executable paths when needed. CLI-created tasks default to your current directory; GUI tasks default to your home directory.

### Automation contract

- Put `--json` **before** the executable separator `--`; everything after it belongs to the task command.
- Responses use `protocol_version`, `data` and `error`. Timestamps use UTC; unknown values are `null`.
- `task run` returns an admitted Run, not its eventual command exit code. Poll the Run's status; a recorded `skipped` trigger also returns successfully.
- With the App closed, `apilot status --json` returns error code `APP_NOT_RUNNING` and exits with code `3`.

| CLI exit code | Meaning |
| --- | --- |
| `0` | Request succeeded |
| `1` | Other failure |
| `2` | Invalid argument |
| `3` | App not running |
| `4` | Object not found |
| `5` | Conflict or stale revision |
| `6` | IPC unavailable, timeout or protocol mismatch |

Use `apilot --help`, `apilot task add --help` or `apilot doctor --json` for command help and local diagnostics.

### Observe an external tool

```sh
apilot observe add herdr --interval 30s --output json -- herdr status --json
apilot observe list --json
apilot observe disable herdr
apilot event list --unread --json
apilot event ack EVENT_ID
```

Choose read-only commands. An observer's exit code determines health; JSON mode also requires valid bounded JSON. Observers retain their latest check, use a separate concurrency slot and never automatically repair or control external tools.

## Execution and local data

Defaults are **four concurrent tasks**, a **30-minute timeout**, **30-day history retention** and **50 MiB per log stream**. The same task cannot overlap itself. Capacity conflicts are recorded as skipped, missed schedules are not replayed, and Apilot does not prevent sleep.

On Quit, Apilot stops scheduling and cancels managed work. A finite per-run helper handles process-group cleanup if the App crashes; unfinished records recover as `interrupted` on restart. Deliberately detached processes and helpers that are themselves killed fall outside this cleanup guarantee.

Data lives in `~/Library/Application Support/apilot/`: `apilot.db` holds definitions and history, and `logs/` holds output. Deleting a task preserves its history and never deletes its working directory. `APILOT_DATA_DIR` selects an isolated profile; App and CLI must use the same value.

Use `--env-file PATH` for explicit `KEY=VALUE` environment definitions. Values are omitted from normal task list/show output and are not automatically logged, but are stored unencrypted in the private database. Commands can print secrets themselves; keep logs private and avoid credentials in command arguments.

## Build from source

Requires macOS, Xcode Command Line Tools and Go. The project pins **Go 1.27.3** through `go.mod` and the Makefile; toolchain and dependency downloads need network access.

```sh
git clone https://github.com/alswl/apilot.git
cd apilot
make app
open build/darwin-arm64/Apilot.app
./bin/apilot status
```

On Apple Silicon, `make app` produces the App and `build/darwin-arm64/apilot-0.0.3-arm64.dmg`. Local builds use ad-hoc signing. `make icons` deterministically exports the committed brand masters; normal builds require no image-generation service.

```sh
make check              # Read-only source, dependency and documentation checks
make test-core          # Logic and real-resource tests without MyGo
make test               # Complete suite including MyGo presentation tests
make race               # Race-enabled suite
make verify-ui          # Packaged native UI probe
make verify-lifecycle   # Process cleanup and restart checks
```

Verification commands use disposable data. The [contributor guide](CONTRIBUTING.md) links the pinned Go/UI standards and project exceptions.

The Go core is separate from MyGo UI: `internal/app` owns application behavior, `internal/storage` owns SQLite, `internal/runner` executes commands, and `internal/ipc` serves the CLI over a private Unix Socket.

## Documentation

- [Architecture](docs/architecture.md) — ownership, scheduling, IPC and process lifecycle.
- [Distribution](docs/distribution.md) — CI artifacts, Developer ID signing and notarization setup.
- [Known limitations](docs/known-limitations.md) — including unverified OS integration, sleep/resume and accessibility behavior.

### Interface language

Apilot supports English and Simplified Chinese. It follows the preferred macOS app language, with English as the fallback. Choose Follow System, English, or 简体中文 in Apilot Settings → Language. Changes apply immediately and are remembered across launches. For a temporary local preview, launch the App executable with `MYGO_LOCALE=en` or `MYGO_LOCALE=zh-Hans`. Task names, commands, logs, stored event summaries and CLI JSON values remain unchanged.

Translations live in `internal/ui/locales`; `tr` resolves interface messages using MyGo’s selected language. CLI commands and help remain English.
