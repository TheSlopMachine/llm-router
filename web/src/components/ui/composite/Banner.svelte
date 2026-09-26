<script lang="ts">
  import { squircle } from '../../../lib/squircle'
  import { theme } from '../../../lib/theme.svelte'
  import { tintSoft } from '../../../lib/tint'

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
  use:squircle={12}
>
  {text}
</div>

<style>
  .banner {
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-md);
    font-size: var(--text-sm);
  }
</style>
