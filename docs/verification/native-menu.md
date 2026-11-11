# Compact native menu and Apilot display name

2026-11-12 · macOS arm64 · MyGo b4882ccc2592.

## Decisions and sources

- Follow [alswl/guides macOS UI guide](https://github.com/alswl/guides/blob/main/macos-app-ui-guide.zh-CN.md): lightweight-tool entry, object-scoped actions, progressive disclosure, separate settings, stable common commands and recoverable hidden UI.
- [Apple Menus](https://developer.apple.com/design/human-interface-guidelines/menus): group related commands, limit submenus to one level, keep unavailable commands discoverable and use clear action labels.
- [Apple The menu bar](https://developer.apple.com/design/human-interface-guidelines/the-menu-bar): retain a native menu; its widest label controls width, including alternate items. Avoid relying on a visible status icon as the only route back to the app.
- Community guidance: KDE's [Simple by default](https://develop.kde.org/hig/simple_by_default/) and [Powerful when needed](https://develop.kde.org/hig/powerful_when_needed/) support a clear primary path with optional details reachable by explicit actions. These inform the product choice; they are not macOS API rules.

The measured width and content limits below are project choices, not Apple specifications.

## Resulting behavior

- Short status heading. Empty running/attention groups are omitted entirely.
- Running and attention groups preview at most three entries; overflow remains accessible through full history/reminder windows. All reminders have a compact scrollable window and source navigation.
- At most five task submenus on the first level, retaining run, automatic scheduling and edit. New task, all tasks, history, settings and Quit retain fixed entry points. History previews only completed runs.
- Dynamic labels use a conservative 28-unit East Asian text budget, normalize whitespace and remove control formatting; full names remain in tooltips and details. This is not font-exact width enforcement.
- Raw errors open through a short details action. They no longer become native menu titles. Empty sections are not stored as hidden long items.
- CLI setup lives in Settings → 命令行工具; first-launch offer remains available.
- Reopening the running app restores its management window when no window is visible.
- Display name is **Apilot** in the bundle, menus, window and DMG title. CLI, bundle identifier, data directory and download filenames remain `apilot` / `com.alswl.apilot` for compatibility.

## Validation

- Full Go tests and vet passed. Existing daily menu actions and schedule revision checks remain covered. Added regression checks for 40 tasks/runs/reminders, bounded previews, full-name retention, single submenu depth, active/completed history separation and empty-state command availability.
- Built App and DMG; packaging script verified DMG integrity, all checksums, ZIP signature and standalone CLI extraction.
- Native AX probe used an isolated profile: empty menu **151×235pt**; populated menu with nine long Chinese/English task names, four failures and one active sleep command **266×379pt**. Captured and visually inspected both menus. These are observed sizes, not fixed width settings.
- Actually opened all reminders (480×360), Settings and the management window through a targeted reopen Apple event. Invoked native Quit with a run active; process exited 0 and CLI returned APP_NOT_RUNNING. Read-only SQLite inspection confirmed the sleep run was canceled.
- Initial probe attempts had harness issues: JSON flag after executable separator, then a non-targeted reopen event with multiple same-bundle instances. Retained logs; corrected harness and reran successfully without changing production data.
- Native dark appearance, VoiceOver announcements, physical notch/third-party menu-bar-manager behavior and simultaneous display configurations were not tested. Local builds use ad-hoc signing; Apple notarization was not exercised.

Ignored evidence and probe: `tmp/menu-verification/`. Native screenshots were cropped to menu bounds.

Installed the final bundle at `/Applications/Apilot.app`, verified its signature
and CLI `ready`, and updated only the pre-existing CLI symlink owned by this App.
The previous installed bundle is retained in ignored `tmp/app-backups/`.
