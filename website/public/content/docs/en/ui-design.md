# UI Design

> This page is a concise English guide. The complete, authoritative document is available in Chinese — switch the language toggle at the top right to read the full version.

ModernChat's UI follows a single cross-platform design system shared by web, website, desktop, mobile and admin console.

## Design tokens

All colors, type sizes, radii, spacing and motion values are defined as CSS custom properties. Never hardcode hex values in components.

- Colors: brand blue (`#0052D9` light / `#7CB8FF` dark), surface, container, overlay and hairline layers, plus semantic colors (success / warning / danger).
- Type scale: 7 steps. Radii: 4 steps plus pill. Spacing: 4pt grid.
- Motion: use `--mc-dur-*` / `--mc-ease-*` tokens; animate only `transform` and `opacity`, and respect `prefers-reduced-motion`.

## Component feel

Buttons, inputs, dialogs and popovers are built as in-house `Ui*` controls. TDesign is reserved for admin tables, tabs and auth forms. Self-built buttons must set `border-none` to avoid white rings in dark mode.

## Known constraints

- UnoCSS `/alpha` does not work with CSS variables — use the `-soft` / `-line` token tiers instead.
- Message images must never use `loading="lazy"` (breaks virtual-list height measurement).
- Message bodies use `{{ }}` interpolation; `v-html` is banned project-wide.

## Acceptance

Check light and dark themes, narrow layouts, focus-visible states and reduced-motion before calling a UI change done. See the Chinese version for the full token table and implementation record.
