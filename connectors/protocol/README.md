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
The fixture and GitHub Connector demonstrate the shared `Serve` entrypoint.


## Scope types

Declare `scope_config.types` in the description when the Connector supports
`project`, `space`, or both. The host may pass `scope_type` to
`config.options`. Treat the selected field, search string, account, endpoint
and scope type as part of the paging context. Option values and cursors are
opaque to the host. Without a scope type declaration, the host omits
`scope_type` from protocol requests.

Accept the selected type in `connection.validate` and return a stable scope
reference that distinguishes a project from a same-named space. Validate
membership using the stored scope type, configuration and reference for all
source reads and writes. Host calls carry the original saved context even
after a package upgrade.

## Signed local packages

An importable package is a ZIP file with three regular files at its root.
`manifest.json` contains the Connector manifest, `connector` is the native
executable named by its entrypoint, and `manifest.sig` contains the 64 raw
bytes of the Ed25519 signature over the exact manifest bytes. Hash the
finished executable before signing the manifest.

Apilot reads trusted publisher keys from `connector-publishers.json` in its
data directory. The JSON object maps approved Connector IDs to base64-encoded
Ed25519 public keys. The trust file is provisioned outside the archive.
Imported releases have private storage per Connector ID. Upgrades require
the original publisher and a higher semantic version.

Build independent synthetic packages with disposable publisher keys from the
repository root. The same output directory reuses the key for the upgrade.

```sh
fixture_dir=$(mktemp -d)
scripts/build-scoped-connector-fixture.sh "$fixture_dir" 0.1.0
scripts/build-scoped-connector-fixture.sh "$fixture_dir" 0.2.0
scripts/build-scoped-connector-fixture.sh "$fixture_dir" 0.1.0 synthetic-project project
data_dir=$(mktemp -d)
install -m 600 "$fixture_dir/connector-publishers.json" "$data_dir/connector-publishers.json"
APILOT_DATA_DIR="$data_dir" build/darwin-arm64/Apilot.app/Contents/MacOS/Apilot --workbench
```

In the App, import the `0.1.0` package through the file chooser. Its source
namespace remains empty until you click **Log In** from **Add Connection**.
The fixture writes that call to `connectors/synthetic-scopes/authorization.log`
under the disposable data directory. Project Alpha and Project Beta each
contain one task. Space Alpha contains both. The test publisher can also be
used from the CLI with `apilot workbench plugin import PACKAGE` while the
App or service is running with the same data directory.

Run the isolated package and range regression with
`go -C core test ./pkg/server -run '^TestSignedScopedConnector$' -count=1`.

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

## Remaining 011 Core checks

Build the four binaries from the selected commit as shown above. Then run
the Core/CLI scenarios against the disposable fixture:

```sh
python3 scripts/verify-uc01-core.py "$candidate" --remaining
go -C core test ./pkg/server ./pkg/dal ./cmd/apilot \
  -run 'TestWorkbenchRemainingMVP|TestWorkbenchConnectorSyncRejectsOlderSnapshot$|TestWorkbenchCLIOperationRoutes$|TestWorkbenchEventStreamCommitRestartAndShutdown$' -count=1
go -C connectors/github-issues test ./... -run 'TestGitHubMutation|TestGitHubLostCreationResponse'
```

The CLI exposes scope and projected-field filters on `task list`, paginated
segments on `task detail`, and available edit fields and transitions on
`task edit-metadata`. Use `connection fields` and `connection mapping
show|preview|save` with the displayed mapping revision. A mapping input can
include a sample `task_id` or request `restore_defaults`.

For an edit, submit a task's current `connection_version` and
`target_version` with `task operation submit -`. Include one typed `fields`
map or one `action` plus its `action_fields`. Read the receipt with `task
operation show`. If it reports `needs_review`, query the original request
with `task operation reconcile`. Fixture options `write_result: "conflict"`
and `write_result: "lost"` exercise conflict and lost acknowledgement.

Use `sync list --scope SCOPE_ID`, `sync inspect SYNC_RUN_ID` and `sync usage
--connection CONNECTION_ID` to match run evidence and usage. The fixture
flags `rate_limited_scope` and `partial_scope` inject a scope-local backoff
and a page error with incomplete counting, respectively.

## Controlled GitHub assignee verification

From the checkout of the acceptance commit, use the existing Go toolchain,
Python 3 and OpenSSL. Build a separate Connector executable and run the
targeted JSON-RPC probe:

```sh
candidate_dir="$PWD/tmp/011-github-assignee"
mkdir -p "$candidate_dir"
go -C connectors/github-issues build -mod=readonly -trimpath \
  -tags apilot_controlled_github -o "$candidate_dir/apilot-github-issues-controlled" \
  ./cmd/apilot-github-issues
go version -m "$candidate_dir/apilot-github-issues-controlled"
python3 scripts/verify-011-github-assignee.py \
  "$candidate_dir/apilot-github-issues-controlled" > "$candidate_dir/result.json"
```

The probe creates a disposable profile and a TLS upstream on a loopback
port. It approves a fake device login and stores the fake credential in
the disposable profile. No GitHub token, API key or Keychain entry is
required. The controlled build requires `controlled-github.json` in its
absolute `APILOT_CONNECTOR_DATA_DIR`, with the HTTPS loopback `origin` and
fixture certificate `root_pem`. The probe supplies both values and removes
the profile afterward.

In `result.json`, expect `confirmed_assignees` to contain `777 / octocat`,
`wrong_id_status` to be `not_executed`, and `patch_count` to be `1`.
The event trace shows login resolution via `GET /users/octocat` and the
issue readback after the fixture PATCH. The report includes the executable
SHA-256. Go build metadata records the source commit and the controlled tag
when built from a clean Git checkout.

This probe exercises the GitHub Connector over stdio. macOS release
packaging builds the regular helper and rejects the controlled build tag
before installing the helper in the app bundle.

## Connector 011 review verification

A full-only Connector may omit the terminal full checkpoint. A successful
delta always supplies its next checkpoint. An action version conflict includes
the source state as `current.state`; it may also include applicable source
values, such as `state_reason` and current values for action inputs.

In `describe`, the optional `sync_rate_limit_buckets` maps each read mode to
the bucket IDs that mode consumes. Core combines the bucket match with scope
isolation. An undeclared mode uses conservative backoff. An unnamed bucket
applies across operations in its affected scope or connection. Bucket IDs
are case-sensitive and the host reserves the `scope:` prefix.

Workbench action conflict receipts expose `conflict.action` (the submitted
action ID), `conflict.proposed` (action inputs) and `conflict.current` (source
values). `latest_sync_run_id` on a usage group identifies the latest recorded
run within the current filters. Pass it to `workbench sync inspect` to reuse
the same run shown in the desktop usage drill-down.

From a clean macOS arm64 checkout of the candidate, use the existing Go
toolchain:

```sh
go -C core test ./pkg/dal ./pkg/server -count=1 \
  -run 'TestWorkbench(ConnectorBackoffMatchesBucketsAndScopes|ConnectorFullOnlyAndTemporaryFailureRetainsTasks|RemainingMVPWritesAndActions|RemainingMVPRunsAndUsage)$'
go -C connectors/github-issues test ./internal/connector -count=1 \
  -run 'Test(GitHubRefreshHTTP200ErrorPreservesCredential|GitHubEditMetadataListsRepositoryLabels|GitHubMutationConfirmsCreateCloseAndReopen|GitHubAuthoritativeAssigneeLookupAndConfirmation|UC01GitHubLoginMatchesAccountAndRotatesCredential)$'
go -C desktop test ./internal/ui/workbench -count=1 \
  -run 'TestConnector011Desktop(DetailOperationAndUsagePresentation|ReviewActionConflictAndRepositoryLabels)$'
make check-openapi check-workbench
make app
python3 scripts/verify-bundled-connectors.py build/darwin-arm64/Apilot.app --arch arm64
```

### State conflicts and label metadata failures

`tasks.edit_metadata` can return an optional `observation` from the same
authorized source read as its available actions. It contains the source
version and native `state`, plus `state_reason` when supplied. When that
version has changed and a previously selected action is absent, Core persists
the conflict before dispatch. The Workbench receipt includes the prior state
as `conflict.previous_state`, the current source state and the requested
action and inputs. A GitHub label lookup failure omits label editing for that
metadata response while keeping the confirmed actions and other edit fields.

From the candidate checkout, run the local fixture and headless regressions:

```sh
go -C core test ./pkg/server -count=1 \
  -run 'TestWorkbench(UnavailableStaleActionReturnsDurableConflict|RemainingMVPWritesAndActions)$'
go -C connectors/github-issues test ./internal/connector -count=1 \
  -run '^TestGitHub(EditMetadata|MutationConfirms)'
go -C desktop test ./internal/ui/workbench -count=1 \
  -run '^TestConnector011Desktop(ReviewActionConflictAndRepositoryLabels|RetriesConflictInOriginalEditor)$'
make check-openapi check-workbench check-format
make app
```

The fixture starts with an available action, then replaces it with the actions
from a newer source state. It checks the initial conflict and repeated
submission under the same request ID. The Connector test supplies a local
HTTP 503 response for repository labels. The headless desktop cases reopen
the matching editor mode and select labels that match the menu caption or
contain commas, with existing labels named after control keys.

### Conflict after background sync and narrow split

With a writable Connector task, keep its open-state `target_version`.
Change the source to closed in the local fixture and complete another
sync. Submit the original close action with the old `target_version`.
The CLI returns a `conflict` receipt with the synced source state and
requested action. Submitting the same request ID again or inspecting
it returns that receipt. Core does not dispatch the stale write. The
prior state is omitted when only the old opaque version is available.

For the desktop case, use the default task split at 680 × 580 pt. In the
selected task's conflict feedback, both recovery buttons are visible
without expanding the detail. Edit again refreshes the scope, then
reopens the original editor with the available actions, including after
a successful sync that leaves the target version unchanged.

```sh
go -C core test ./pkg/server -count=1 -run 'TestWorkbench(StaleActionAfterSyncReturnsDurableConflict|UnavailableStaleActionReturnsDurableConflict|RemainingMVPWritesAndActions)$'
go -C core test ./cmd/apilot -count=1 -run '^TestWorkbenchCLIOperationRoutes$/^submitWorkbenchTaskOperation$'
go -C desktop test ./internal/ui/workbench -count=1 -run '^TestConnector011Desktop(ConflictButtonsVisibleInDefaultSplit|RetriesConflictInOriginalEditor)$'
make check-openapi check-workbench check-format
make app
```

### Retry Keep source value after a failed refresh

In the local GitHub fixture, change the source state while the Workbench
still has the old version and submit the old action to show a conflict.
Make the next read return HTTP 503 and click **Keep source value**. After
the sync reaches `failed / temporary`, the last confirmed task value,
conflict and both recovery buttons remain visible. Restore the source
and click **Keep source value** again. The successful refresh displays
the source value and dismisses the conflict. Neither click writes upstream.

The focused headless check exercises accepted, running, failed and
successful refresh snapshots in the default split:

```sh
go -C desktop test ./internal/ui/workbench -count=1 -run '^TestConnector011Desktop(AcceptSourceKeepsRecoveryUntilSyncSucceeds|RetriesConflictInOriginalEditor|ConflictButtonsVisibleInDefaultSplit)$'
make check-workbench check-format
make app
```

### Concurrent conflict recovery

In two writable scopes with conflicts, start recovery in the first scope,
switch to the second scope and start recovery there before the first
finishes. Both scopes show **Syncing** while their recovery buttons are
disabled. Complete one scope successfully and fail the other temporarily,
then repeat with the completion order reversed. The successful scope clears
its conflict. The failed scope retains both recovery buttons for a retry.

```sh
go -C desktop test ./internal/ui/workbench -count=1 -run '^TestConnector011Desktop(ConcurrentConflictRecovery|AcceptSourceKeepsRecoveryUntilSyncSucceeds|RetriesConflictInOriginalEditor|ConflictButtonsVisibleInDefaultSplit)$'
make check-workbench check-format
make app
```
