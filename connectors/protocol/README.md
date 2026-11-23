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

The first Core delivery negotiates only `describe`, which returns the
Connector ID, release, supported protocol versions, supported capabilities
and versioned endpoint and scope schemas. Each schema declares a JSON Schema
object and may include UI fields with control and dynamic option hints.
Other methods are listed below for later implementation. The example
returns a structured `unsupported` error when called with one of them.

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
