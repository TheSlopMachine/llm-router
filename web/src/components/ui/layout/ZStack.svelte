<script lang="ts">
  // Children share one grid cell and paint in source order. Used for badges
  // over avatars, loading veils, and anything that would otherwise need a
  // position:absolute wrapper invented per call site.
  import type { Snippet } from 'svelte'
  import { ALIGN, JUSTIFY, type Align, type Justify } from '../tokens'

  let {
    align = 'stretch',
    justify = 'stretch',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    align?: Align | 'stretch'
    justify?: Justify | 'stretch'
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  const alignValue = $derived(align === 'stretch' ? 'stretch' : ALIGN[align as Align])
  const justifyValue = $derived(justify === 'stretch' ? 'stretch' : JUSTIFY[justify as Justify])
</script>

<div
  class="zstk {cls}"
  style:align-items={alignValue}
  style:justify-items={justifyValue}
  {...rest}
>
  {@render children()}
</div>
