<script lang="ts">
  import type { Snippet } from 'svelte'
  import { ALIGN, space, type Step, type Align } from '../tokens'

  let {
    cols = 1,
    gap = 0,
    align = 'stretch',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    /** a column count, or a raw grid-template-columns string for ragged layouts */
    cols?: number | string
    gap?: Step
    align?: Align
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  const template = $derived(typeof cols === 'number' ? `repeat(${cols}, minmax(0, 1fr))` : cols)
</script>

<div
  class="grd {cls}"
  style:--grd-cols={template}
  style:--grd-gap={space(gap)}
  style:--grd-align={ALIGN[align as Align]}
  {...rest}
>
  {@render children()}
</div>
