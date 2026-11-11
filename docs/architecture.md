# Architecture

The App is the only long-lived owner of the scheduler, SQLite write connection and local IPC endpoint. CLI requests and native UI actions call the same Application Service. Core packages do not import MyGo.

- `internal/model`: shared values and run state transitions, avoiding a storage/service import cycle.
- `internal/app`: validated mutations, revision checks, admission, scheduling integration, recovery, retention and observation.
- `internal/storage`: numbered transactional SQL migrations, indexed identity/status/time columns, JSON payloads for non-query attributes, and separately stored private environment values. This keeps SQL small without an ORM.
- `internal/schedule`: robfig's five-field parser and Next calculation. App-owned timers apply skip-on-resume and dynamic reload policy. No custom daylight-saving logic.
- `internal/runner`: per-Run helper and control/result pipes, process-group termination, independent timeout, separate bounded stdout/stderr files.
- `internal/ipc` / `internal/cli`: current-user Unix Socket, versioned JSON requests, strict input and stable errors. No network listener.
- `internal/ui`: MyGo native window, system menu/tray, on-demand compact tabs, sheets, settings and CLI-install windows, notifications, and brand colors following system appearance. The App starts as an accessory with no persistent Dock icon; routine actions live in its native status menu. Data snapshots are fetched outside the main thread.

Launch takes a data-directory flock before migration, restores unfinished records as interrupted, recomputes future schedules and then starts IPC. Window close preserves the App; normal Quit blocks new work, closes IPC, cancels tasks, waits for cleanup and closes storage. A killed App cannot run cleanup code; each finite helper observes its App-owned pipe closing and terminates the target group before exiting. Neither helper nor CLI can schedule additional work.

Run admission records queued before launching a command. Task-level and global admission share a mutex; SQLite also has a partial unique index for one active Run per task. Terminal results cannot be overwritten. Task edits retain running snapshots. Observation results use separate records and a separate concurrency slot.

MyGo windows, menus and desktop integration are native; the `ui` widgets are rendered by MyGo itself, not individual AppKit controls. The framework and build tool are pinned to the same commit through go.mod and go.sum.
