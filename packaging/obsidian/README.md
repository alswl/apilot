# Obsidian Local REST API for Apilot writeback

Apilot requires atomic conditional writes for Obsidian task creation and status changes. This patch applies to Local REST API 5.5.2 at upstream commit `7c603fbc42918bd9ec6f34ab5b46300bac0d0eaa`. It checks the expected version inside Obsidian's `Vault.process()` and advertises that capability on reads. Existing Obsidian connections remain readable without the patched bridge.

## Build and install

Run from the Apilot repository root with Git, Node.js 22 or later, and npm. Set `OBSIDIAN_VAULT` to the absolute path of your target vault.

```sh
apilot_root="$PWD"
bridge_source="$(mktemp -d)"
git clone --branch 5.5.2 --depth 1 https://github.com/coddingtonbear/obsidian-local-rest-api.git "$bridge_source"
git -C "$bridge_source" apply "$apilot_root/packaging/obsidian/local-rest-api-5.5.2-vault-process.patch"
npm --prefix "$bridge_source" ci
(cd "$bridge_source" && node esbuild.config.mjs production)
```

Disable Local REST API in the target vault if it is running. Then copy the built bundle and manifest into that vault's plugin directory. Keep the existing settings file so the port, certificate and token remain available.

```sh
bridge_destination="${OBSIDIAN_VAULT:?Set OBSIDIAN_VAULT}/.obsidian/plugins/obsidian-local-rest-api"
mkdir -p "$bridge_destination"
cp "$bridge_source/main.js" "$bridge_source/manifest.json" "$bridge_destination/"
```

Enable Local REST API in Obsidian and configure the connection in Apilot. Upgrading through the community plugin manager replaces the patched bundle. Rebuild and test the patch against the target revision before using a later release.

[Local REST API 5.5.2](https://github.com/coddingtonbear/obsidian-local-rest-api/releases/tag/5.5.2) · [Obsidian Vault API](https://docs.obsidian.md/Plugins/Vault)
