# Declarative MCP plugins

Orole implements schema-version-1 declarative plugins: persisted manifests contribute command-palette entries and dashboard widgets backed by fixed MCP actions. This is not an arbitrary-code plugin host. Plugins cannot import JavaScript, execute HTML, load modules, spawn a process directly, or obtain credentials. There is no production sample-data fallback or automatically installed example plugin.

## Ownership and contracts

React Settings imports and inspects manifests; stateless `internal/app/plugins.go` bindings forward to authenticated daemon IPC. `internal/daemon/svc_plugins.go` exposes `plugins.list`, `plugins.save`, `plugins.trust`, `plugins.remove`, and `plugins.run`. The daemon's `internal/plugins` manager owns validation, persistence, trust and cancellation. It invokes only the policy-checked MCP entry point; MCP transport, credentials and tool permissions stay in their existing Go services.

- Manifest: `schemaVersion`, namespaced lowercase `id`, `name`, `version`, `description`, `capabilities`, `actions`, `commands`, `widgets`.
- Capability: exact `serverId` / `tool` pair. These reference registered MCP servers and discovered tool names, not endpoint URLs.
- Action: local `id`, `title`, `description`, declared `serverId` / `tool`, fixed JSON-object `arguments`.
- Command/widget: local `id`, `title`, `description`, and a known `actionId`.
- Save: `{manifest, expectedDigest}`; empty digest means creation, otherwise it must match the current record.
- Trust: `{id, digest, enabled}`. Remove: `{id, digest}`. Run: `{id, digest, actionId}`. Callers cannot override action arguments, server, tool or actor.
- Records contain `manifest`, `digest`, `enabled`, `approvedDigest`, and `updatedAt` (Unix milliseconds). `plugins.changed` announces successful mutations; the frontend reloads the daemon snapshot.
- Run returns `{text, isError, truncated}`. Text/resource-text blocks are sanitized and bounded to 128 KiB. Binary content, resource links and structured objects are not forwarded as executable or privileged content.

Manifests are limited to 64 KiB, 1–16 capabilities, 1–32 actions, up to 32 commands and 16 widgets, with at least one contribution. Duplicate JSON keys, unknown manifest fields, excessive nesting, duplicate identifiers, undeclared capabilities, unknown action references, non-object arguments and unsafe metadata are rejected. Action arguments are persisted as plaintext: never embed secrets.

## Trust and cancellation

The daemon canonicalizes the validated manifest and hashes it with SHA-256. Trust requires `enabled && digest == approvedDigest`; it is tied to that exact canonical manifest, not merely the plugin ID or version. Saving changed content clears approval and disables the plugin; saving identical canonical content preserves current trust. Concurrent updates use digest checks and fail with a conflict rather than overwriting a newer review.

Enabling requires a separate destructive central-policy approval for `plugins.trust`, resource `plugins/<id>/<digest>`, actor `user`. The approval details identify the digest and capabilities. The manager rechecks the record and cancellation state after approval before committing. Revocation does not itself require another approval.

Each invocation uses actor `plugin:<id>` and still passes through `mcp.tool.call` authorization for `mcp/<serverId>/<tool>`. Plugin trust does not grant blanket MCP permission. Tool annotations are untrusted hints, not authority. Even an enabled plugin cannot bypass a denied tool operation.

Manifest changes, revocation, removal and daemon shutdown cancel tracked work. Trust is checked again before MCP dispatch, and canceled or stale results are discarded. Cancellation is cooperative: it cannot roll back effects already performed by a remote tool, and there is an unavoidable boundary between the last local guard and remote dispatch. Closing the command result panel is not cancellation; revoke the plugin to cancel its active calls. There is no standalone per-plugin-run cancel API.

## Frontend contribution mounting

`App.tsx` mounts `PluginContributions` once beside the shell services. It synchronizes `stores/plugins.ts`, invalidates records on `pluginsChanged`, reloads on reconnect/focus, and removes usable contributions when the snapshot is stale/offline. Active records register with the existing command and widget registries, rather than a separate plugin registry.

Commands use IDs `plugin:<pluginId>:command:<commandId>` and open a result panel. Widgets use the plugin-prefixed type from `features/plugins/manifest.ts` and the stable `PluginWidget` component. Neither registration, mounting, revalidation, opening a command panel nor adding a widget invokes MCP. Only **Run action** / **Run again** does so. Output is rendered as React text, never HTML.

Dashboard layouts are independently persisted workspace data. When a plugin is revoked or removed, `WidgetGrid` preserves its saved layout entry and displays **Unavailable widget** with **Remove widget**, rather than silently deleting layout data. Re-enabling the matching contribution restores it. A stale open command shows an unavailable/changed-manifest message.

## Runtime verification

On 2026-10-08, a throwaway acceptance helper exercised the packaged daemon through
authenticated named-pipe IPC with a fresh OS-temporary `OROLE_HOME` and an
official-SDK Streamable HTTP fixture bound only to `127.0.0.1`.

- A new manifest imported disabled; direct invocation was denied with zero effects.
- Exact-digest trust required an explicitly resolved `plugins.trust` approval.
- The enabled manifest, command, and widget declarations survived daemon restart.
- Denying the separate MCP approval produced no effect despite plugin trust.
- Approving the MCP call returned `wrote plugin-proof`, wrote the actual file
  bytes `plugin-proof`, and incremented the fixture invocation count to one.
- Revocation denied further invocation without another effect.
- The revoked record survived another daemon restart and remained non-invocable.
- The owned daemon, loopback endpoint, and temporary data were cleaned up.

The helper did not open SQLite directly or install permissive policy rules.
These helper checks establish persistence and real authorized execution.
The throwaway helper was removed after recording the evidence.

Additional NSIS-installed WebView2 acceptance used a separately owned loopback
MCP fixture and isolated home. The UI imported a disabled manifest, displayed its
canonical digest, requested exact-manifest trust, and registered its command and
Home customization entry. Running the command and then its dashboard widget each
required independent MCP approval and wrote observable proof-file effects 1 and 2;
both displayed the returned plaintext. Revocation replaced the saved dashboard
entry with **Unavailable widget**, without deleting its layout. The revoked record
survived GUI quit/relaunch; confirmed plugin removal and widget removal were exercised.

This run found an unnamed native trust checkbox. Its visible label now explicitly
targets the input. The rebuilt installed app exposed the full accessible name;
Space enabled the trust action and a second Space disabled it. No trust or MCP
permission is inferred from the checkbox alone. Widget drag/resize persistence
was not exercised in this native run.

## Native and browser smoke actions

Native verification requires a separately isolated desktop session, a registered and connected MCP server with a known harmless observable tool, and a manifest whose exact server ID/tool/fixed arguments match that server. The automatic helper shuts down its endpoint and deletes its home when done; its ephemeral server ID is not a reusable native fixture. Use a separately owned long-lived fixture or an explicitly approved real MCP tool for native checks.

1. In **Settings → Plugins**, paste the manifest or choose its JSON file. Choose **Review JSON**, inspect all arguments/contributions, then **Import disabled plugin**. Expect **Disabled / needs review**, a canonical digest, and no plugin command in search or plugin widget in **Home → Customize**.
2. Choose **Inspect capabilities & manifest**, expand **Complete manifest, action arguments and contributions**, check **I reviewed the complete manifest, fixed arguments and requested capabilities.**, then **Trust and enable**. Approve the separate daemon request. Expect **Trusted and enabled**.
3. Open global command search and select the manifest's command title under **Plugin · <name>**. Confirm opening the panel has no tool effect. Choose **Run action**, deny the MCP approval, and verify no effect. Run again, approve it, and verify **Completed**, plaintext output and the independent tool effect.
4. In **Home → Customize**, add the contributed widget by its title (plugin widgets start Medium and Tall). Confirm no automatic tool call. Choose **Run action** in the widget and handle MCP policy approval. Drag it to another place and change its size from its menu, then restart the isolated desktop using the same home. Confirm the trusted command, saved widget order and size return without automatic invocation.
5. Leave a command panel open, then **Settings → Plugins → Revoke and disable**. Confirm the command disappears from new searches, the open panel becomes unavailable, and the existing Home widget shows **Unavailable widget** with **Remove widget**. Restart the isolated session and confirm revocation and the unavailable layout entry persist. The IPC helper separately checks that direct invocation is denied after revocation.
6. Re-trust, then **Edit manifest JSON**, change its version or fixed arguments, **Review JSON**, and **Save manifest update**. Confirm a new digest, disabled state and required re-review. **Remove plugin → Confirm removal** deletes the manifest; existing widgets remain removable. Do not infer rollback of already completed remote effects.

In an ordinary browser without the native MyGo binding, **Settings → Plugins** must show its desktop-required state, not mock manifests, an enabled importer or functioning tool calls. No plugin commands/widgets should be registered from fabricated data. Browser rendering can establish this unavailable state only; it cannot prove authenticated IPC, native imports, approvals or persisted daemon execution.
