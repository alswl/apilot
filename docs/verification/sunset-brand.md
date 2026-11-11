# Sunset Aviator identity

2026-11-11 · macOS arm64 · Go 1.27.3.

The user approved full-color option A (Sunset Aviator), retaining the simplified
C2 human portrait, and authorized small matching UI color changes.

- Exported the approved illustration without the presentation label/margins;
  generated a transparent monochrome template from the approved C2 mark.
- `make icons` exports 1024 px bundle, 128 px UI and 32 px menu-bar PNGs from
  committed raster masters. The superseded hand-drawn SVG and Swift generator
  were removed. Export uses existing pinned Go dependencies.
- Menu template requires nonempty artwork and transparent pixels; export
  discards RGB so macOS applies its own menu-bar tint.
- Brand masters and alternate full-size logos are excluded from bundled resources.
- Light/dark themes use coral/peach accents, plum text/surfaces and teal success.
  Contrast calculations: primary-button text 5.38:1 / 8.93:1, ordinary text
  14.37:1 / 15.67:1, secondary text 5.33:1 / 8.79:1, success text
  4.90:1 / 10.28:1 (light / dark).
- Go test and vet passed; App and DMG built and signature verification passed.
- Packaged native probe used a disposable SQLite/IPC profile with three safe
  task definitions; screenshots inspected at 640×480, 480×360 and dark mode.
  Closing, reopening and Quit passed. No fixture commands were executed.
- This change does not exercise Apple notarization or complete pending UI
  navigation/accessibility acceptance. See earlier verification records.

Screenshots and logs are ignored under `tmp/sunset-verification/`.

- Installed the resulting bundle in `/Applications/apilot.app` after confirming
  zero active task/observer runs. Previous bundle preserved under ignored
  `tmp/app-backups/`. New installed signature verifies and CLI reports `ready`.
- Repeated `make icons` produced identical hashes for all three generated PNGs.
