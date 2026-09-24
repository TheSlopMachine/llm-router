<script lang="ts">
  import type { Snippet } from 'svelte'
  import { squircle } from '../../../lib/squircle'

  // SwiftUI-like list: owns the rounded background and the dividers between
  // rows. Rows are arbitrary views owning their own inner padding. Direct
  // children are the row hosts: block-level, full width, hover-painted.
  let {
    radius = 18,
    dividers = true,
    class: cls = '',
    children,
    ...rest
  } = $props<{
    radius?: number
    dividers?: boolean
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()
</script>

<div class="lst {cls}" class:lst-dividers={dividers} use:squircle={radius} {...rest}>
  {@render children()}
</div>

<style>
  .lst {
    background: var(--elev);
  }
  .lst-dividers > :global(*) + :global(*) {
    border-top: 1px solid var(--color-outline-soft);
  }
  .lst > :global(*) {
    display: block;
  }

</style>
