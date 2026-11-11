# Simplified menu-bar glyph

2026-11-12 · macOS arm64 · MyGo b4882ccc2592.

The user requested fewer menu-bar details and authorized rebuilding and local
installation. The approved Sunset Aviator primary logo remains unchanged.

- Edited the approved monochrome portrait into a menu-specific human profile:
  two goggle cutouts, compact hair silhouette and a simple visible eye; removed
  collar and separate hair strands. The user explicitly requested retaining the eye
  after reviewing the first simplified silhouette.
- Confirmed MyGo's Darwin `tray.SetImage` fixes image height at 18pt. Export is
  now 36px for 2× display, fitting the visible alpha bounds inside a one-pixel
  inset with preserved aspect ratio. Output RGB is black; alpha carries the shape.
- Viewed old/new static comparisons at 18px and 36px on light and dark surfaces.
  Checked nonempty transparency and black-only exported pixels. Primary 1024px
  and UI 128px icon hashes are unchanged.
- `go test ./internal/ui`, `make icons`, `make app` and `git diff --check` passed.
  App and DMG built; staged and installed bundle signatures verified.
- Confirmed zero active task/observer runs, stopped the old App normally,
  replaced `/Applications/apilot.app`, reopened it and confirmed CLI `ready`.
  Previous bundle preserved under ignored `tmp/app-backups/`.
- Captured and inspected the installed normal-state menu bar. Actual dark/selected
  state and VoiceOver behavior were not tested in this change. This local build
  uses ad-hoc signing; Apple notarization was not exercised.

Ignored evidence: `tmp/brand/menu-refinement/` contains the original tray export,
main-icon hashes, master preview, old/new size comparison and installed menu-bar
capture. The production master is `resources/brand/apilot-template.png`.
