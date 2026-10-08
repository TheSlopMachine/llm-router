# FUI

Self-contained Svelte 5 widget library. Copy this folder into any SvelteKit/Vite project.

Rule: nothing in `FUI/` imports from outside `FUI/` (except `svelte`).

## Setup
```ts
import './FUI/styles/tokens.css'   // required: all --fui-* tokens
import './FUI/styles/base.css'     // optional: zero-specificity body baseline
```
Alias (optional): `$ui` -> `src/FUI/index.ts`.

## The site must provide
- **Fonts**: `--fui-font-normal` (weights 400/500/600) and `--fui-font-mono` (400). The icon font ships inside FUI.
- **Theme**: `setTheme('light' | 'dark')`. Persistence / "auto" is the site's job.
- **Accent**: `setAccent('#rrggbb', { tintElev?: boolean })`; hover and on-accent text are derived unless passed.
- **Texts**: `setFuiTexts(() => ({ close, confirm, cancel, searchPlaceholder, noOptions, copy }))` (defaults are empty).
- **Hosts**: mount `<Modal />` and `<Toasts />` once at the root.

## Squircle
`use:squircle` picks a tier once: native `corner-shape` -> clip-path -> plain `border-radius`.
No borders/shadows on squircle elements (clip-path would cut them).

## Style priority
widget internals + props > library tokens > site settings.
