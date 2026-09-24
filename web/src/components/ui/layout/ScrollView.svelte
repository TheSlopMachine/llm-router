<script lang="ts">
  // Scrolling container.
  //
  // NOTE: never wrap this in use:squircle. clip-path clips descendants, so a
  // squircled ancestor would cut the scrolled content instead of just its own
  // corners. Scrollers keep plain border-radius.
  import type { Snippet } from 'svelte'
  import { space, type Step } from '../tokens'

  let {
    axis = 'y',
    pad,
    grow = true,
    class: cls = '',
    children,
    ...rest
  } = $props<{
    axis?: 'x' | 'y' | 'both'
    pad?: Step
    grow?: boolean
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()
</script>

<div
  class="scrollview {cls}"
  class:sv-y={axis === 'y' || axis === 'both'}
  class:sv-x={axis === 'x' || axis === 'both'}
  class:stk-grow={grow}
  style:padding={space(pad)}
  {...rest}
>
  {@render children()}
</div>

<style>
  .scrollview {
    min-width: 0;
    min-height: 0;
    overscroll-behavior: contain;
  }
  .sv-y { overflow-y: auto; }
  .sv-x { overflow-x: auto; }
</style>
