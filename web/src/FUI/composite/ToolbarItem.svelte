<script lang="ts">
  import type { Snippet } from 'svelte'
  import { onDestroy } from 'svelte'
  import type { FillSize } from '../tokens'
  import { getToolbarCtx, setToolbarPinned, type ToolbarItemReg } from './toolbar.svelte'

  // Wraps ONE Button / Switch / Checkbox. Unwrapped Toolbar children never go to the menu.
  // pinned: never leaves the panel. priority: higher number leaves first (default: DOM order).
  // label: text shown beside a Switch/Checkbox when it lives in the menu.
  let { pinned = false, priority, label, active = false, fill, children } = $props<{
    pinned?: boolean
    priority?: number
    label?: string
    /** shows a dot on the "more" button while this item sits in the menu (e.g. an enabled filter) */
    active?: boolean
    /** grow to the free width of the row (min basis from --fui-basis-*) */
    fill?: FillSize
    children: Snippet
  }>()

  const ctx = getToolbarCtx()
  const id = `tbi-${Math.random().toString(36).slice(2)}`
  setToolbarPinned(pinned)
  const reg = (): ToolbarItemReg => ({ id, pinned, priority, label, active, children })
  ctx?.register(reg())
  $effect(() => {
    void pinned; void priority; void label; void active
    ctx?.update(reg())
  })
  onDestroy(() => ctx?.unregister(id))
  const inMenu = $derived(ctx?.menuIds.includes(id) ?? false)
</script>

{#if !inMenu}
  <span class="tbi" class:fill={fill != null && fill !== false} style:flex-grow={fill ? 1 : undefined}
    style:flex-basis={fill ? `var(--fui-basis-${fill === true ? 'md' : fill})` : undefined}>{@render children()}</span>
{/if}

<style>
  .tbi { display: inline-flex; align-items: center; flex-shrink: 0; min-width: 0; }
  .tbi.fill { display: flex; }
  .tbi.fill > :global(*) { flex: 1 1 auto; min-width: 0; }
</style>
