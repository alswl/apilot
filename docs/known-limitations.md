# Known limitations

- Deliberately detached processes (`setsid` or another process group), and execution helpers that are also killed, are outside the cleanup guarantee. The App is not a sandbox.
- Real system sleep/resume has not been exercised. Resume events and future schedule reset are implemented; no custom daylight-saving behavior or special tests are included.
- Notification delivery/permission refusal, login-item registration, VoiceOver, input-method composition have not been fully verified in the packaged App. Their interfaces exist, but automated core tests cannot prove OS behavior.
- The unified Workbench has headless navigation tests and packaged macOS probes for menu-bar entry, window close and reopen, settings, and reuse of task and observer editors. The packaged App passed 20 quit and crash recovery repetitions. Full mouse and keyboard form submission, file opening, copying, and scroll restoration still need manual acceptance.
- Real Codex/herdr commands have not been run as acceptance fixtures. Generic local command/observer integration is tested.
- macOS 12 and Intel/universal binaries have not been validated. Published downloads target Apple Silicon; development builds are ad hoc. See [distribution](distribution.md) for signing metadata.
- Linux and Windows applications are not implemented or accepted. Cross-compilation checks cover only the shared domain, configuration, scheduling and presentation packages; process helpers, IPC, desktop integration and SQLite's C toolchain still need platform work.
- JSON output is the stable automation format. CLI event/observer lists use default pagination; additional pagination flags are pending.
- Observer results are limited to the latest bounded check. There is no observation history or external-tool-specific health parser.
- History is held in memory for the retention period and refreshed in UI snapshots. Large histories beyond the tested 1,000 records require further profiling.
- Output beyond each stream's configured limit is drained and discarded. stdout/stderr are independent streams; their relative order is not reconstructed.
- Environment definitions are stored locally in plaintext under current-user permissions. User commands and command arguments can contain or print secrets; automatic metadata hiding cannot redact arbitrary command output.

## Workbench mock mode

The [Workbench guide](workbench.md) describes the fixture mode. Source edits, sync results, assistant replies and scheduled runs are simulated. Task edits, saved views and conversations reset on relaunch. Production workbench mode reads local Obsidian Tasks, GitHub repositories, and an opt-in GitHub Issues fixture. Production workbench mode persists named views. Assistant inference and conversation persistence are pending. Obsidian sync covers Markdown list checkboxes and supported Tasks emoji fields. Ambiguous duplicates or anonymous task text edits can appear as removals and additions.

GitHub Issue writes are checked against the last observed issue before dispatch, but GitHub does not offer a conditional state update here. A concurrent change between the final GET and PATCH can therefore be overwritten. A create whose response is lost and whose remote identity is unknown remains pending review; Apilot does not automatically repeat it. GitHub repository access and packaged Keychain behavior still require the real-source acceptance described in the feature quickstart.

Obsidian writeback requires write permission and a uniquely identifiable destination. The default writeback method uses the Obsidian CLI. Enable the CLI in Obsidian and open the target vault. The optional REST method requires a local HTTPS configuration and the patched bridge. The patched bridge rejected the tested overlapping native editor save and preserved the newer text. See [Enable Obsidian writeback](workbench.md#启用-obsidian-回写).
