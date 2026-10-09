<script lang="ts">
  import type { Snippet } from 'svelte'
  import { space, type Step } from '../tokens'

  let {
    gap = 0,
    halign = 'start',
    valign = 'center',
    tag = 'div',
    class: cls = '',
    children,
    ...rest
  } = $props<{
    gap?: Step
    halign?: 'start' | 'center' | 'end'
    valign?: 'start' | 'center' | 'end' | 'baseline'
    tag?: 'div' | 'section' | 'nav' | 'header' | 'footer' | 'aside' | 'ul' | 'li' | 'form'
    class?: string
    children: Snippet
    [key: string]: unknown
  }>()

  const HALIGN: Record<string, string> = { start: 'flex-start', center: 'center', end: 'flex-end' }
  const VALIGN: Record<string, string> = { start: 'flex-start', center: 'center', end: 'flex-end', baseline: 'baseline' }
</script>

<svelte:element
  this={tag}
  class="wrp {cls}"
  style:--fui-wrp-gap={space(gap)}
  style:--fui-wrp-halign={HALIGN[halign]}
  style:--fui-wrp-valign={VALIGN[valign]}
  {...rest}
>
  {@render children()}
</svelte:element>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .wrp {
    display: flex;
    flex-wrap: wrap;
    row-gap: var(--fui-wrp-gap, 0px);
    column-gap: var(--fui-wrp-gap, 0px);
    justify-content: var(--fui-wrp-halign, flex-start);
    align-items: var(--fui-wrp-valign, center);
    min-width: 0;
  }
</style>
