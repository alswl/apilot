# Architecture

The App is the only long-lived owner of SQLite, scheduling, task execution and the current-user Unix Socket. CLI requests and UI actions use the same Application Service. There is no network listener or independent daemon.

| Package | Responsibility |
| --- | --- |
| `internal/host` | Service/IPC startup, rollback, retention maintenance and ordered shutdown. |
| `internal/app` | Business validation, revisions, atomic mutations, admission, recovery and retention. |
| `internal/presentation` | Pure drafts, field errors, menu/notification projections and operation/refresh tokens; no MyGo dependency. |
| `internal/ui` | MyGo rendering, windows, menus, language and platform callbacks. |
| `internal/model` | Shared domain values and run transitions; terminology is in [GLOSSARY](../GLOSSARY.md). |
| `internal/storage` | Numbered SQLite migrations and bounded private log storage. |
| `internal/runner` | Finite execution helpers and process-group cleanup. |
| `internal/schedule`, `internal/observer` | Future schedule calculation and read-only observation. |
| `internal/ipc`, `internal/cli` | Versioned local requests, stable JSON errors and CLI behavior. |

The entry point dispatches execution-helper mode before opening any App resources. Normal startup acquires the data lock, migrates and recovers records, then starts IPC. Failed startup releases acquired resources.

Closing a window preserves the App. Quit prevents new mutations and scheduling and cancels managed work, closes IPC connections, joins UI/maintenance work, then closes storage. In-flight mutations finish under the Service lock or encounter the stopping gate; queued UI completions cannot revive closed windows. The UI thread remains available while shutdown finishes in the background.

Each accepted trigger records a Run before execution. Task/global admission shares a lock, and SQLite enforces a single active Run per task. Terminal results cannot be overwritten. Task edits preserve active execution snapshots. Resume recalculates future schedules without replaying missed times.

A killed App cannot run cleanup. Each finite helper detects its App-owned control pipe closing and terminates the target process group. It owns no database, scheduler or management endpoint. See [limitations](known-limitations.md) for the cleanup boundary.

Observation results stay separate from Task Runs: a check's failure does not establish failure of an external agent's work, and frequent checks must not flood task history. Observers use their own capacity slot and retain their latest bounded result. Attention acknowledgement means seen, not recovered.

Presentation drafts convert text to the same business inputs used through IPC. Business ranges and persistence remain in the Service. Refreshes coalesce behind one in-flight query; window generations discard obsolete results without starving slow queries. Log responses also match run, stream and offset. Only the UI thread changes presentation state.

MyGo provides native windows, menus and desktop integration; its widgets are rendered by MyGo rather than individual AppKit controls. Core tests do not prove OS notification, accessibility or login-item behavior.
