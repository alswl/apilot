# apilot brand — Sunset Aviator

The approved primary logo is **A / Sunset Aviator**, using the simplified C2
female aviator character: three-quarter face, forehead goggles and a friendly
smile. Preserve the character and broad silhouettes.

- `apilot-sunset.png`: approved full-color primary master, without labels or presentation margins.
- `apilot-template.png`: transparent black master for the system-tinted menu-bar icon.
- `apilot-positive.png` / `apilot-negative.png`: approved monochrome alternatives.

`make icons` deterministically exports the primary master to `resources/icon.png`
(1024 px) and `internal/ui/assets/app.png` (128 px), and the alpha-only menu icon
at 32 px. It adds the macOS icon inset and rounded mask; it never regenerates
the illustration or requires an image-generation service during builds.

UI colors borrow coral/peach for primary actions, teal for successful states,
plum for text and dark surfaces, and amber for warnings. Action colors are
adjusted for legible text in each appearance. Failure remains labeled in red;
color never substitutes for state text.
