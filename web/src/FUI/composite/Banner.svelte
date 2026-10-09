<script lang="ts">
  import { squircle } from '../core/squircle'
  import { theme } from '../core/theme.svelte'
  import { tintSoft } from '../core/tint'

  // Tinted notice strip. The tint paints the font; the background is the
  // same tint softened (translucent fill + theme-adjusted text) — the same
  // strategy as tinted chips. `variant` is shorthand for the built-in
  // notification hues (icon-token hexes from app.css); `tint` overrides it.
  let {
    text,
    tint,
    variant = 'info',
  } = $props<{
    text: string
    tint?: string
    variant?: 'info' | 'warning' | 'error' | 'success'
  }>()

  const VARIANTS: Record<string, string> = {
    info: '#3b82f6',
    warning: '#f59e0b',
    error: '#dc2626',
    success: '#16a34a',
  }

  const hex = $derived(tint ?? VARIANTS[variant] ?? VARIANTS.info)
  const soft = $derived.by(() => {
    void theme.value
    const dark = document.documentElement.classList.contains('dark')
    return tintSoft(hex, dark)
  })
</script>

<div
  class="banner"
  style:background={soft.bg}
  style:color={soft.text}
  use:squircle
>
  {text}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .banner {
    padding: var(--fui-space-4) var(--fui-space-5);
    border-radius: var(--fui-radius-md);
    font-size: var(--fui-text-sm);
  }
</style>
