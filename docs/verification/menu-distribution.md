# Menu bar and distribution update

2026-11-11 · macOS 27.2 arm64 · Go 1.27.3.

- Replaced the placeholder dot with an original arrow/orbit mark; generated
  a 1024 px bundle icon and a monochrome Retina menu-bar template from the
  committed Swift source. The SVG is editable. No new runtime dependency.
- Startup leaves the management window closed. Native probe reports
  `startup_main_window=false`, invokes the same management menu action, closes,
  reopens and quits successfully. This verifies handlers and lifecycle; it does
  not claim full mouse-driven acceptance of every native menu item.
- Checked actual MyGo-rendered screenshots in light/dark modes, 640×480 and
  480×360, plus the CLI installation window and task sheet. Screenshots are
  ignored under `tmp/`. The OS menu retains native appearance and contrast.
- Menu actions expose per-task run/edit/automatic scheduling, active-run stop,
  new task, management, CLI install, settings and Quit. Tests ensure a running
  task cannot be retriggered, and toggling a disabled schedule enables it while
  preserving environment keys and rejecting stale revisions.
- CLI link installation is idempotent for the same bundle and refuses to replace
  an existing file or a different link. Temporary-directory tests passed.
  Installation to the real user's PATH was intentionally not performed.
- `make app` built an ad-hoc signed App and DMG, including the standalone CLI.
  Packager verified App signatures, image checksum and all download checksums.
  Mounted the read-only DMG, verified the Applications shortcut, icon path from
  Info.plist, embedded CLI execution and bundle signature; detached afterward.
  One initial verification assertion used `icon.icns`; corrected the verifier to
  read CFBundleIconFile (`AppIcon.icns`). No packaging defect was found.
- `go vet ./...`, `go test -race -vet=off ./...`, actionlint and shell syntax
  validation passed. The earlier full core/CLI verification remains applicable;
  see final.md for the original acceptance boundaries.
- Developer ID import, notarytool submission, ticket stapling and Gatekeeper
  assessment are implemented only for opted-in tag builds. Real Apple
  credentials, signing/notarization and tag publication remain unverified.
- Repeated the full process lifecycle suite after these changes: 10 normal quits
  and 10 App SIGKILL/restart cycles all passed (`tmp/menu-lifecycle.log`).
