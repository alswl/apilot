# Apilot Connector v1

This Go module provides the wire types and stdio server for Apilot task connectors.
It builds independently of Core and the desktop app.

Run the local no-key fixture from this directory:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":"example-1","method":"describe"}' |
  go run ./cmd/apilot-fixture serve --protocol 1
```

Launch the binary directly with `serve --protocol 1`. Write one UTF-8 JSON-RPC
request followed by a newline and close stdin. Use a nonempty string ID. Read
newline-terminated notifications and exactly one response with the same ID
from stdout. Put only redacted diagnostic text on stderr. Each JSON frame is
limited to 8 MiB excluding its newline. Login notifications are limited to 64
KiB. Hosts may cancel a call by terminating its process group.

A release manifest declares the Connector ID, release and protocol versions,
OS, architecture, relative entrypoint, and its SHA-256 digest. Hosts verify the
package and compare the manifest with the running binary's `describe` response.

Native field values carry a type and JSON value. Dates are ISO calendar dates,
datetimes are RFC 3339 timestamps, options and accounts carry stable IDs and
display labels, and rich text declares its format. JSON null clears an editable
field. A key absent from a field patch is left unchanged.

## Manifest and discovery

```json
{
  "manifest_version": 1,
  "connector_id": "example-issues",
  "release_version": "0.1.0",
  "protocol_versions": [1],
  "os": "darwin",
  "arch": "arm64",
  "entrypoint": "apilot-example-issues",
  "entrypoint_sha256": "<lowercase hexadecimal SHA-256 of the final executable>"
}
```

Bundled manifests live in `Contents/Resources/connectors`. Their entrypoints
resolve under `Contents/Helpers/connectors`. Build or sign the helper before
hashing it. Compare the executable's ID, release and protocol versions with
the manifest before registration. The host provides a private data namespace
and request deadline in `APILOT_CONNECTOR_DATA_DIR` and
`APILOT_CONNECTOR_DEADLINE` (RFC 3339).

The `describe` response returns the Connector ID, release, protocol versions,
supported capabilities, and versioned endpoint and scope schemas. Each
schema declares a JSON Schema object and may include UI fields with control
and dynamic option hints. The UC01 fixture and GitHub Issues implement login,
configuration options and validation, full and incremental sync, and missing
item resolution. Unsupported methods return a structured `unsupported` error.

| Method | Purpose |
| --- | --- |
| `auth.login`, `auth.status`, `auth.logout` | Manage authorization through opaque account and endpoint references |
| `config.options`, `connection.validate`, `fields.describe` | Discover options, validate configuration and describe native fields |
| `tasks.sync`, `tasks.resolve` | Read full or incremental pages and resolve missing source IDs |
| `tasks.detail`, `tasks.edit_metadata` | Read detail sections, editable fields and available status actions |
| `tasks.mutate`, `tasks.reconcile` | Submit a stable operation ID or check the result of an earlier operation |

The Connector keeps platform credentials in its own storage. An
`open_browser` notification contains the originating `request_id`, an HTTPS
`url`, and an optional `user_code`. Keep those values out of diagnostics.
A response contains either `result` (including JSON null when appropriate)
or `error`. An application error carries an integer `code`, safe `message`,
and `data.kind`. The v1 kinds are `auth_required`, `access_denied`,
`invalid_config`, `invalid_checkpoint`, `rate_limited`, `temporary` and
`unsupported`. Optional structured data can carry a field path, retry
time, affected resource level or usage counters.

For a full sync, independently resolve missing IDs before reconciling scope
membership. An incremental page only applies explicit changes. Publish the
cursor together with a complete successful sync. Writes may return a
confirmed value, a conflict, a pending confirmation or a definite
non-execution. Reconcile uncertain writes using the original operation ID.

In a Go module, import `github.com/alswl/apilot/connectors/protocol`.
The two commands in this repository demonstrate the shared `Serve` entrypoint.


## UC01 Core and CLI candidate

From the repository root, build the selected commit into a directory outside
an App bundle. The headless command uses the existing Core Unix socket and
opens no window.

```sh
candidate="$PWD/tmp/uc01-$(git rev-parse --short HEAD)"
mkdir -p "$candidate"
go -C core build -o "$candidate/apilot" ./cmd/apilot
go -C desktop build -o "$candidate/apilot-app" ./cmd/apilot-app
go -C connectors/protocol build -o "$candidate/apilot-fixture" ./cmd/apilot-fixture
go -C connectors/github-issues build -o "$candidate/apilot-github-issues" ./cmd/apilot-github-issues
python3 scripts/verify-uc01-core.py "$candidate"
```

The Core/CLI check uses Python, OpenSSL, a loopback HTTPS browser fixture and a
disposable database. It checks cancel, pagewise discovery, repeated
selection, independent first runs and retry. For independently probing the
HTTP layer and publish transaction, run:

```sh
go -C core test ./pkg/server ./pkg/dal ./cmd/apilot \
  -run 'TestWorkbenchConnector(UC01HTTP|SyncRejectsOlderSnapshot)$|TestWorkbenchCLIOperationRoutes$' -count=1
```

For manual use, start `apilot-app --serve` with a fresh `APILOT_DATA_DIR` and an
absolute `APILOT_DEV_CONNECTOR_MANIFEST` next to the fixture binary. Use the
same data directory for the CLI. The developer manifest is available to the
standalone executable. App bundles discover their signed Connector through
the bundle resources.

- `apilot --json workbench connector login fixture` starts an asynchronous
  session. Poll `connector auth-status ID` or stop it via `connector cancel-login ID`.
- `apilot --json workbench connector options fixture --authorization ID --field project`
  returns the first page. Pass the returned `next_page` with `--page`.
- Pass the selection below to `apilot --json workbench connector connection save -`.
  Use `connector connection list` and `connector connection show CONNECTION_ID`
  to identify the parent, scope IDs and first run results. Use
  `connector connection sync CONNECTION_ID` to retry.
- Set `fail_second_page` to `OPS` in
  `$APILOT_DATA_DIR/connectors/fixture/fixture.json` before saving to simulate a
  partial upstream read. Replace it with `{}` to recover.
  `denied_scope`, `empty_scope` and `unverified_scope` exercise access loss,
  an empty full enumeration, and an incomplete membership check.

```json
{
  "authorization_id": "<completed session ID>",
  "endpoint_schema_version": 1,
  "scope_schema_version": 1,
  "scopes": [
    {"config": {"project": "PAY"}},
    {"config": {"project": "OPS"}},
    {"config": {"project": "PAY"}}
  ]
}
```
