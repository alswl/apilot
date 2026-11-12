# Distribution

`make app` builds the App, bundles the standalone CLI, signs ad hoc, and creates
`build/darwin-arm64/apilot-VERSION-arm64.dmg` using MyGo's native disk-image builder.
The image contains apilot and an Applications shortcut with a Finder drag layout.
`packaging/darwin/package-downloads.sh` verifies signatures and the image checksum, then
exports DMG, App ZIP, CLI archive, provenance and SHA256SUMS into `dist/`.

`mygo.json` is the shared configuration. `make app` stages the CLI under
`resources/darwin-GOARCH/bin/`, then explicitly selects that MyGo build target;
resources from other targets are excluded. Current download packaging remains
macOS arm64. Linux/Windows application builds are not yet supported.

## Install and first launch

Open the DMG, drag apilot to Applications, eject the image, and open the installed
App. It resides in the menu bar; the management window opens on demand. The first
launch offers to link the bundled CLI to `~/.local/bin/apilot`. It requires no
administrator password and never overwrites an existing file or a different
symlink. Launching from a DMG or an App Translocation path disables installation;
move the App to `/Applications` or `~/Applications` first.

If needed, copy `export PATH="$HOME/.local/bin:$PATH"` into `~/.zshrc` and open a
new terminal. Verify with `apilot status --json`. The App must be running. Install
can be reopened from the menu bar. Remove the link to uninstall the CLI. Moving
or deleting the App also breaks the link; reinstall after relocating the bundle.
There is no daemon and no automatic shell configuration change.

## Developer ID signing and notarization

A Developer ID Application certificate can sign multiple apps belonging to the
same team; apilot keeps the independent bundle identifier `com.alswl.apilot`.
Do not revoke an existing certificate to add this app. Use a repository-scoped
secret copy of the certificate and keep access limited to trusted maintainers.
Apple notarization requires Developer ID signing, hardened runtime and timestamps:
[Apple's distribution requirements](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution).

Configure these GitHub repository Secrets through GitHub Settings → Secrets and
variables → Actions (never commit or paste private keys into an issue/chat):

| Secret | Value |
| --- | --- |
| `APPLE_CERTIFICATE_P12_BASE64` | Base64 of a Developer ID Application certificate exported with its private key as password-protected `.p12` |
| `APPLE_CERTIFICATE_PASSWORD` | Export password |
| `APPLE_SIGNING_IDENTITY` | Full `Developer ID Application: Name (TEAMID)` identity |
| `APPLE_API_KEY_P8_BASE64` | Base64 of an App Store Connect **team** API private key |
| `APPLE_API_KEY_ID` | API key ID |
| `APPLE_API_ISSUER_ID` | Team API issuer UUID |

After configuring all six, set repository Variable `APPLE_SIGNING_ENABLED=true`.
When false/unset, downloads remain ad hoc and metadata says `notarized: false`.
When enabled, a missing/invalid secret or failed notarization fails the signed build;
it never silently publishes an unsigned replacement.

Pushed `v*` tags use signing/notarization when enabled; the tag must match
`mygo.json`. To build a signed temporary package without a tag or Release, run
the workflow on the desired branch with `notarize=true`. Its verified files are
available as an Actions artifact for seven days. PRs, regular pushes and manual
runs without that option never receive signing secrets. No `pull_request_target`,
no credentials in Go caches.
The existing standard macOS job does the signing, with a 15-minute step limit;
there is no second native build or per-PR notarization.

`packaging/darwin/sign-release.sh` creates a temporary keychain, imports the certificate,
stores notarytool credentials there, and invokes MyGo's existing signing and
notarization support. MyGo signs nested CLI code and the App with hardened runtime
and timestamps, signs the DMG, submits it once to Apple, and staples the DMG and
the original App. The standalone CLI archive uses that same signed CLI, which
Apple scanned inside the submitted DMG; standalone executables cannot have a
stapled ticket. App ZIP comes from the stapled original bundle. The DMG's embedded
App is covered by the notarized, stapled image. The script validates both tickets
and the App's Gatekeeper assessment before packaging. It restores the keychain
search list and packaging config and removes temporary credentials on exit.

Published downloads are Developer ID signed and notarized when their
`build-info.json` reports `signing: developer-id` and `notarized: true`.
Unsigned development runs remain ad hoc. Prerelease labels remain visible in the
App and CLI; signed bundles use the numeric base version in Apple's version keys.
Check each download's metadata and
`SHA256SUMS`; do not infer signing from its filename.

## Brand assets

Committed masters and their provenance live in [design/brand](../design/brand/README.md).
`make icons` renders the App and menu marks with pinned Go dependencies;
normal builds need no image-generation service.
