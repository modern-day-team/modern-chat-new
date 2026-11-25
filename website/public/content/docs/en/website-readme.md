# Website Runbook

> This page is a concise English guide. The complete, authoritative document is available in Chinese — switch the language toggle at the top right to read the full version.

The `website/` app is the public marketing site, docs reader and plugin market. It is a standalone Vue 3 + Vite app with no component library — all styles are handwritten against `src/styles/tokens.css`.

## Routes

- `/` — landing page
- `/docs/:file?` — docs reader
- `/plugins` — plugin market

## Commands

```bash
npm run dev          # :5174
npm run build        # type-check + build + precompression
npm run type-check
npm run lint         # oxlint + eslint, check-only
```

## Keeping docs in sync

Chinese docs are copied from the repository root into `public/content/docs/` by `website/scripts/sync-docs.ps1` (Windows) or `sync-docs.sh` (Unix). When you add or rename a doc, update **three** places: the source file under `docs/`, both sync-script maps, and the `DOCS` table in `src/views/DocsView.vue`. English mirrors under `public/content/docs/en/` need manual updates.

## Keeping the plugin market in sync

Register each example plugin in `src/views/PluginsView.vue` **and** copy its file to `public/content/plugins/` so the download link resolves.

## Gotchas

- Never place static assets in a directory with the same name as a route (`/docs`, `/plugins`) — the static server would shadow the SPA route.
- In vue-i18n strings, escape `@`, `|` and `{}` or the page will white-screen at render time.
- Icons are inline SVG (`UiIcon.vue`), never emoji.
