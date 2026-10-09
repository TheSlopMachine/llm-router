<script lang="ts">
  import type { Snippet } from 'svelte'
  import { ALIGN, space, type Step, type Align } from '../tokens'

  const GRID_MIN: Record<string, string> = {
    xs: 'var(--fui-grid-min-xs)',
    sm: 'var(--fui-grid-min-sm)',
    md: 'var(--fui-grid-min-md)',
    lg: 'var(--fui-grid-min-lg)'
  }

  let {
    cols = 1,
    min,
    gap = 0,
    align = 'stretch',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    /** a column count, or a raw grid-template-columns string for ragged layouts */
    cols?: number | string
    /** named min column width: repeat(auto-fit, minmax(min(100%, token), 1fr)) */
    min?: 'xs' | 'sm' | 'md' | 'lg'
    gap?: Step
    align?: Align
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  const template = $derived(
    min != null ? `repeat(auto-fit, minmax(min(100%, ${GRID_MIN[min]}), 1fr))` : typeof cols === 'number' ? `repeat(${cols}, minmax(0, 1fr))` : cols
  )
</script>

<div
  class="grd {cls}"
  style:--fui-grd-cols={template}
  style:--fui-grd-gap={space(gap)}
  style:--fui-grd-align={ALIGN[align as Align]}
  {...rest}
>
  {@render children()}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .grd {
    display: grid;
    gap: var(--fui-grd-gap, 0px);
    grid-template-columns: var(--fui-grd-cols, 1fr);
    align-items: var(--fui-grd-align, stretch);
    min-width: 0;
  }
</style>
