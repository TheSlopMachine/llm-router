<script lang="ts">
  // A padded / filled container with no layout opinion. Use when you need
  // surface + padding but not a flex context.
  import type { Snippet } from 'svelte'
  import { space, type Step } from '../tokens'
  import { squircle } from '../../../lib/squircle'

  let {
    pad,
    surface = 'none',
    radius,
    squircled = false,
    tag = 'div',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    pad?: Step
    surface?: 'none' | 'elev' | 'container' | 'high' | 'highest'
    radius?: 'xs' | 'sm' | 'md' | 'lg'
    /** clip-path corners; leave false for anything that scrolls or overflows */
    squircled?: boolean
    tag?: 'div' | 'section' | 'article' | 'aside'
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  const SURFACE: Record<string, string> = {
    none: 'transparent',
    elev: 'var(--elev)',
    container: 'var(--color-surface-container)',
    high: 'var(--color-surface-container-high)',
    highest: 'var(--color-surface-container-highest)',
  }
  const bg = $derived(SURFACE[surface] ?? 'transparent')
  const rad = $derived(radius ? `var(--radius-${radius})` : undefined)
</script>

{#if squircled}
  <svelte:element
    this={tag}
    class="box {cls}"
    style:--box-pad={space(pad)}
    style:--box-bg={bg}
    style:--box-radius={rad}
    use:squircle
    {...rest}
  >
    {@render children()}
  </svelte:element>
{:else}
  <svelte:element
    this={tag}
    class="box {cls}"
    style:--box-pad={space(pad)}
    style:--box-bg={bg}
    style:--box-radius={rad}
    {...rest}
  >
    {@render children()}
  </svelte:element>
{/if}
