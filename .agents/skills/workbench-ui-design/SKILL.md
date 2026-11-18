---
name: workbench-ui-design
description: Design or revise Apilot macOS workbench screens and shared components using the project standards. Apply when implementing workbench layouts, interaction flows, component states, or UI copy.
---

# Workbench UI design

- Read the [Apilot macOS UI standard](../../../docs/standards/apilot-macos-ui.md), including its pinned guides. Write the user decision, object, action and feedback before changing a page. Reuse shared controls and existing theme roles.
- Describe each transient surface as an entry and return pair. Include the visible control, keyboard command, restored focus, selection, scroll or draft. Check the nearest active layer handles Escape first. Leave text input in control of typed letters.
- Identify which properties and actions come from the source connector and which are local. Exercise GitHub and Obsidian with the same production views and different fixtures.
- Keep navigation, list controls, action names, and status where the user can find them. Inspect narrow and wide layouts with English and Chinese copy. Run `make check-workbench` before handoff. The [manual rubric](../workbench-ui-lint/references/rubric.md) contains the acceptance cases.
