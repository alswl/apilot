# apilot CLI reference

Use the installed CLI's `--help` as the authority when versions differ. Examples
use placeholders `TASK`, `OBSERVER`, `RUN_ID`, `EVENT_ID` and `REVISION`; resolve
them from actual results. Task/observer references accept an ID or name; prefer
the resolved ID for updates. The desktop App owns persistence and execution.

## Global options and output

- `--json`: versioned JSON on stdout; place before `-- EXECUTABLE`.
- `-h`, `--help`: help, including on individual commands.
- Without `--json`, use human-readable output. Errors use stderr except JSON
  error envelopes. Do not interpret exit status 0 as a delivered agent message.
- `APILOT_DATA_DIR`: selects an isolated profile. The App and CLI must use the
  same profile. Leave it unchanged when operating an existing user's tasks.

JSON shape:

```json
{"protocol_version":1,"data":{},"error":null}
```

On error, `error` contains `code`, `message` and `details`; `doctor` can return
both diagnostic data and an error. Check both the process exit code and error
field. Lists return `data.items` plus pagination metadata. `task run` returns
a Run, with its ID in `data.id`.

| Exit | Meaning |
| --- | --- |
| 0 | Command succeeded; a newly admitted Run may still be queued/running |
| 1 | Other failure |
| 2 | `INVALID_ARGUMENT` |
| 3 | `APP_NOT_RUNNING` |
| 4 | `NOT_FOUND` |
| 5 | `CONFLICT` or `STALE_REVISION` |
| 6 | `IPC_TIMEOUT`, `IPC_UNAVAILABLE` or `PROTOCOL_MISMATCH` |

## Application

```sh
apilot version --json
apilot status --json
apilot doctor --json
```

`version` is local and reports app/protocol versions. `status` reports readiness,
active counts and attention count. `doctor` reports profile/path diagnostics,
including when IPC is unavailable. For an offline App, reopen the existing App
through its supported entry rather than editing its database or starting a daemon.

## Tasks

```sh
apilot task list --limit 100 --offset 0 --json
apilot task show TASK --json
apilot task add NAME [INPUT_FLAGS] -- EXECUTABLE [ARGS...]
apilot task update TASK --if-revision REVISION [INPUT_FLAGS] --json
apilot task update TASK --if-revision REVISION --json -- EXECUTABLE [ARGS...]
apilot task remove TASK --json
apilot task enable TASK --json
apilot task disable TASK --json
apilot task run TASK --json
```

`list` defaults to 100 items, offset 0; limit can be at most 1000. Advance
`--offset` to inspect later pages. `remove` deletes the definition; use it only
when deletion is intended. Enable/disable controls automatic triggering; manual
Run admission is separate and existing active Runs are not stopped by disabling.

### Task input flags

| Flag | Value / meaning |
| --- | --- |
| `--name` | Task name; `add` also requires the positional name |
| `--description` | Description |
| `--cwd` | Working directory; a new task defaults to the CLI's current directory |
| `--timeout` | Duration such as `30s` or `2m`; creation uses the App default timeout |
| `--disabled=true\|false` | Disable/enable the task's automatic triggering |
| `--cron` | Five-field cron expression; mutually exclusive with `--interval` |
| `--interval` | Fixed interval duration such as `1h` |
| `--timezone` | IANA timezone; defaults to `Local` for a newly supplied schedule |
| `--schedule-enabled=true\|false` | Enable/disable an existing schedule |
| `--clear-schedule` | Remove the schedule; do not combine with a new cron/interval |
| `--env-file` | File containing explicit `KEY=VALUE` updates |
| `--env-unset` | Key to remove; repeat for multiple keys |
| `--if-revision` | Require the inspected revision on update |

Updates read the current definition and apply explicitly changed flags.
For a delivery-only update, omit all unrelated flags. After a revision conflict,
reread and reconcile rather than dropping `--if-revision`.

`-- EXECUTABLE [ARGS...]` replaces the full literal executable/argv. Preserve
spaces and empty arguments using an argument array through the calling tool;
no shell expansion is implicit. Shell execution requires explicitly selecting
a shell and its arguments, and is not needed just to change a receiver.

`task show` redacts environment values, returning key names. `--env-file` merges
specified keys; omitted keys survive. Values can contain `=`; this is not a
shell script and is not sourced. Blank lines and lines starting with `#` are
ignored. Keep private env files private and remove only operation-owned files
after use. Never reconstruct a secret environment from the redacted snapshot.

Example:

```sh
apilot task add hourly-check --interval 1h --cwd /path/to/project -- /absolute/path/to/check 'one argument'
apilot task update TASK --if-revision REVISION --timeout 2m --json
```

## Runs and logs

```sh
apilot run list --task TASK --limit 20 --offset 0 --json
apilot run show RUN_ID --json
apilot run stop RUN_ID --json
apilot logs RUN_ID --stream stdout --offset 0
apilot logs RUN_ID --stream stderr --offset 0
apilot logs RUN_ID --follow
```

`run list` defaults to 20 items, offset 0, with a maximum limit of 1000;
`--task` is optional. `run stop` requests cancellation of that Run, not removal
of its Task. `logs` accepts `--stream stdout|stderr` (default stdout), byte
`--offset` (default 0), and `--follow` (default false). Normal output is log text;
JSON log chunks include base64-encoded `data_base64` bytes, `next_offset` and `eof`.
Avoid unbounded following when only a bounded verification is needed.

Run states: `queued`, `running`, `succeeded`, `failed`, `canceled`, `interrupted`,
`skipped`. Queued/running are active. Track the Run ID returned by your trigger,
not just a Task's latest-run field, which another trigger can replace. A successful
Run proves process execution; confirm agent receipt separately.

## Observers

```sh
apilot observe list --json
apilot observe show OBSERVER --json
apilot observe add NAME [OBSERVER_FLAGS] -- EXECUTABLE [ARGS...]
apilot observe update OBSERVER --if-revision REVISION [OBSERVER_FLAGS] --json
apilot observe update OBSERVER --if-revision REVISION --json -- EXECUTABLE [ARGS...]
apilot observe remove OBSERVER --json
apilot observe enable OBSERVER --json
apilot observe disable OBSERVER --json
```

Observer input flags: `--name`, `--cwd`, `--interval` (default `30s`), `--timeout`
(default `10s`), `--output text|json` (default text), `--disabled=true|false`,
`--env-file`, repeated `--env-unset`, and `--if-revision`. Updates preserve
unmodified fields and hidden environment values. Replacement argv is literal.

Observers retain their latest bounded result separately from Task Runs. A failed
observation is evidence about the check, not proof that an external agent failed.
`observe list` currently exposes no pagination flags; do not invent them.

## Attention events

```sh
apilot event list --unread --json
apilot event ack EVENT_ID --json
```

`--unread` is optional and defaults false. `event list` currently exposes no
pagination flags. Acknowledgement means seen, not recovered or fixed.

## Settings

```sh
apilot settings show --json
apilot settings update --max-concurrent 4 --json
```

| Update flag | Value / default |
| --- | --- |
| `--default-timeout` | Duration, `30m` |
| `--max-concurrent` | Integer, `4` |
| `--retention-days` | Integer, `30` |
| `--log-limit` | Bytes per stream, `52428800` |
| `--notify-failure=true\|false` | Task failure notification, true |
| `--notify-success=true\|false` | Task success notification, false |
| `--notify-observer=true\|false` | Observation change notification, true |

Only explicitly supplied settings change; update uses the current revision.
These application-wide flags are unrelated to session routing. Interface language
and login launch are managed in the desktop UI, not through invented CLI flags.

## Session delivery

apilot has no built-in `bind`, `attach`, `session` command or per-run message
payload override. Read [session delivery](session-delivery.md) to adapt the
runtime's supported receiving API and the existing task's delivery adapter.
Use the exact interface documented by that integration, not guessed flags.
