# Known limitations

- Deliberately detached processes (`setsid` or another process group), and execution helpers that are also killed, are outside the cleanup guarantee. The App is not a sandbox.
- Real system sleep/resume has not been exercised. Resume events and future schedule reset are implemented; no custom daylight-saving behavior or special tests are included.
- Notification delivery/permission refusal, login-item registration, VoiceOver, input-method composition have not been fully verified in the packaged App. Their interfaces exist, but automated core tests cannot prove OS behavior.
- GUI navigation has headless tests plus real native screenshots at 640×480 and 480×360. Full mouse/keyboard form submission, file opening, copy and scroll restoration still need manual acceptance.
- Real Codex/herdr commands have not been run as acceptance fixtures. Generic local command/observer integration is tested.
- macOS 12 and Intel/universal binaries have not been validated. Published downloads target Apple Silicon; development builds are ad hoc. See [distribution](distribution.md) for signing metadata.
- JSON output is the stable automation format. CLI event/observer lists use default pagination; additional pagination flags are pending.
- Observer results are limited to the latest bounded check. There is no observation history or external-tool-specific health parser.
- History is held in memory for the retention period and refreshed in UI snapshots. Large histories beyond the tested 1,000 records require further profiling.
- Output beyond each stream's configured limit is drained and discarded. stdout/stderr are independent streams; their relative order is not reconstructed.
- Environment definitions are stored locally in plaintext under current-user permissions. User commands and command arguments can contain or print secrets; automatic metadata hiding cannot redact arbitrary command output.
