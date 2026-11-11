# apilot brand — Sunset Aviator

The approved primary logo is **A / Sunset Aviator**, using the simplified C2
female aviator character: three-quarter face, forehead goggles and a friendly
smile. Preserve the character and broad silhouettes.

- `apilot-sunset.png`: approved full-color primary master, without labels or presentation margins.
- `apilot-template.png`: menu-specific transparent black master: compact human profile, a simple visible eye and two goggle cutouts, without collar or hair strands.
- `apilot-positive.png` / `apilot-negative.png`: approved monochrome alternatives.

`make icons` deterministically exports the primary master to `resources/icon.png`
(1024 px) and `internal/ui/assets/app.png` (128 px), and the alpha-only menu icon
at 36 px (2× for MyGo’s actual 18pt tray display), fitting the visible alpha bounds
inside a one-pixel inset while preserving aspect ratio. It adds the macOS icon inset and rounded mask; it never regenerates
the illustration or requires an image-generation service during builds.

UI colors borrow coral/peach for primary actions, teal for successful states,
plum for text and dark surfaces, and amber for warnings. Action colors are
adjusted for legible text in each appearance. Failure remains labeled in red;
color never substitutes for state text.
