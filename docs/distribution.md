# Distribution

`make app` builds the App, bundles the standalone CLI, signs ad hoc, and creates
`build/darwin-arm64/apilot-0.1.0-arm64.dmg` using MyGo's native disk-image builder.
The image contains apilot and an Applications shortcut with a Finder drag layout.
`packaging/package-downloads.sh` verifies signatures and the image checksum, then
exports DMG, App ZIP, CLI archive, provenance and SHA256SUMS into `dist/`.

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
When enabled, a missing/invalid secret or failed notarization fails the tag build;
it never silently publishes an unsigned replacement.

Only a pushed `v*` tag runs signing/notarization, and its version must match
`packaging/mygo.json`. PRs, regular pushes and manual development runs never
receive signing secrets. No `pull_request_target`, no credentials in Go caches.
The existing standard macOS job does the signing, with a 15-minute step limit;
there is no second native build or per-PR notarization.

`packaging/sign-release.sh` creates a temporary keychain, imports the certificate,
stores notarytool credentials there, and invokes MyGo's existing signing and
notarization support. MyGo signs nested CLI code and the App with hardened runtime
and timestamps, signs the DMG, submits it once to Apple, and staples the DMG and
the original App. The standalone CLI archive uses that same signed CLI, which
Apple scanned inside the submitted DMG; standalone executables cannot have a
stapled ticket. App ZIP comes from the stapled original bundle. The DMG's embedded
App is covered by the notarized, stapled image. The script validates both tickets
and the App's Gatekeeper assessment before packaging. It restores the keychain
search list and packaging config and removes temporary credentials on exit.

Signing configuration, tag publishing and Apple's response have not yet been
verified with live credentials. This setup does not enroll a developer account,
generate/revoke certificates or spend on paid GitHub runners.

## Brand assets

The primary logo is the approved **Sunset Aviator (A)** portrait. Masters and
monochrome alternatives live in `resources/brand/`; see its README for provenance
and roles. `make icons` exports the bundle icon, in-app mark and alpha-only menu
icon from those masters with the pinned Go dependencies. Generated PNGs are
committed; CI needs no image generation service or extra graphics tools.
