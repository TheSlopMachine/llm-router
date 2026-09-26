<script lang="ts">
  // A padded container with no layout opinion. Layering is boolean:
  // a widget either carries one elev wash or none — nesting accumulates it.
  // Use when you need padding (+ optional elev) but not a flex context.
  import type { Snippet } from 'svelte'
  import { space, type Step } from '../tokens'
  import { squircle } from '../../../lib/squircle'

  let {
    pad,
    elev = false,
    radius,
    squircled = false,
    tag = 'div',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    pad?: Step
    /** one elev wash over the parent background; unset stays transparent */
    elev?: boolean
    radius?: 'xs' | 'sm' | 'md' | 'lg'
    /** clip-path corners; leave false for anything that scrolls or overflows */
    squircled?: boolean
    tag?: 'div' | 'section' | 'article' | 'aside'
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  const bg = $derived(elev ? 'var(--elev)' : 'transparent')
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

<style>
  /* Box: a padded/filled container with no layout opinion of its own. */
  .box {
    padding: var(--box-pad, 0px);
    background: var(--box-bg, transparent);
    border-radius: var(--box-radius, 0px);
    min-width: 0;
  }
</style>
