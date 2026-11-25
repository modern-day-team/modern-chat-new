# Plugin Development

> This page is a concise English guide. The complete, authoritative document is available in Chinese — switch the language toggle at the top right to read the full version.

ModernChat runs two kinds of plugins:

- **User plugins** — single-file JavaScript (or TypeScript transpiled by the server), installed per user, sandboxed in an iframe with `srcdoc` and a strict permission allowlist.
- **Server plugins** — admin-installed, either JSON-manifest webhook plugins or WASM modules executed in a restricted WASI sandbox via `wazero`.

## Manifest

Every plugin starts with a JSON comment block declaring `name`, `version`, `description`, `permissions` and `allowedHosts`. Unknown permission names are rejected at install time (HTTP 400). The full permission list lives in the Chinese document and in `server/internal/plugin/manifest.go`.

## SDK

The host object injected into user plugins is `api`. Bare `fetch` does not work inside the opaque-origin sandbox — use `api.request` instead. Responses are capped at 2 MB. SDK v5 adds conversation write operations, message history, file upload and declarative UI controls (`tabs`, `steps`, `empty`, `loading`, `upload`).

## Events

Plugins can subscribe to `message:new`, `message:recall` and `presence` (user plugins) or `message:new` webhooks (server plugins). Note the payload shape of `message:new` differs between the browser host and the server runtime.

## Examples

See `plugins/examples/` for user plugins (weather, search-kit, page-kit, ai-chat, power-kit) and `server-examples/` for WASM plugins. The website plugin market offers downloadable copies.
