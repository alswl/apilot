---
name: workbench-ui-design
description: Design or revise Apilot macOS workbench screens and shared components using the project standards. Apply when implementing workbench layouts, interaction flows, component states, UI copy, or moving an existing macOS window into the workbench.
---

# Workbench UI design

- Read the [Apilot macOS UI standard](../../../docs/standards/apilot-macos-ui.md), including its pinned guides. Write the user decision, object, action and feedback before changing a page. Reuse shared controls and existing theme roles.
- Describe each transient surface as an entry and return pair. Include the visible control, keyboard command, restored focus, selection, scroll or draft. Check the nearest active layer handles Escape first. Leave text input in control of typed letters.
- Identify which properties and actions come from the source connector and which are local. Exercise GitHub and Obsidian with the same production views and different fixtures.
- Give empty views concise guidance. State what is missing and when content will appear. Offer a working next action when one exists, or name the prerequisite when the capability is unavailable. Keep empty, filtered, loading, and unavailable states distinct.
- Keep navigation, list controls, action names, and status where the user can find them. Inspect narrow and wide layouts with English and Chinese copy. Run `make check-workbench` before handoff. The [manual rubric](../workbench-ui-lint/references/rubric.md) contains the acceptance cases.


## Replacing an existing window

- Inventory the old functions in their existing order and map each one to a Workbench page, preference pane, or system entry point. Include menu-bar and keyboard actions, modal editor ownership, notification targets, login behavior, and CLI installation where present. Preserve access to existing data and verify the mapped entry points before switching the default window.
- Distinguish source permissions from Apilot's own data. Keep external read-only connectors read-only. For a Local connection, exercise CLI import and the Workbench view against the same service and data directory. For a source-based demo, seed fixtures through the production connector and CLI path. Verify the launch command against the seeded service. Include the checkout directory when a source launch depends on it.
- If delivery is a replacement for an installed App, verify the packaged App and bundled CLI in addition to running the UI tests. Exercise opening from the menu bar, navigation through the migrated pages, editor return, and closing and reopening the window. Record build identity, signing, and any OS behavior still awaiting manual verification.
