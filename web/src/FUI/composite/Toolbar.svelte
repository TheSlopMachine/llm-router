<script lang="ts">
  import type { Snippet } from 'svelte'
  import { onMount, tick } from 'svelte'
  import Button from '../controls/Button.svelte'
  import FloatingView from '../controls/FloatingView.svelte'
  import { space, type Step } from '../tokens'
  import { ToolbarCtx, setToolbarCtx } from './toolbar.svelte'
  import ToolbarMenuRow from './ToolbarMenuRow.svelte'

  // Panel with a degradation ladder, applied while the content does not fit:
  //   1 icon-only buttons -> 2 compact search -> 3.. movable items go to the "more" menu
  //   (overflow="menu") -> last step: remaining children wrap onto new lines.
  // overflow="wrap" skips the menu: 1 -> 2 -> wrap.
  let {
    overflow = 'wrap',
    gap = 3,
    halign = 'start',
    valign = 'center',
    moreLabel = 'More',
    children,
  } = $props<{
    overflow?: 'wrap' | 'menu'
    gap?: Step
    halign?: 'start' | 'center' | 'end'
    valign?: 'start' | 'center' | 'end' | 'baseline'
    moreLabel?: string
    children: Snippet
  }>()

  const ctx = new ToolbarCtx()
  setToolbarCtx(ctx)

  let host = $state<HTMLElement>()
  let row = $state<HTMLElement>()
  let menuOpen = $state(false)
  let menuAnchor = $state<HTMLElement>()
  // Natural row width recorded at the moment each level overflowed: restore a level
  // only when the host is at least that wide (no flicker at the boundary).
  let thresholds: number[] = []

  // Items that can leave, in leaving order (highest priority number first).
  const movable = $derived(
    ctx.items
      .map((it, order) => ({ it, order }))
      .filter((x) => !x.it.pinned)
      .sort((a, b) => (b.it.priority ?? b.order) - (a.it.priority ?? a.order) || b.order - a.order)
      .map((x) => x.it),
  )
  const wrapLevel = $derived(overflow === 'menu' ? 3 + movable.length : 3)
  const wrapping = $derived(ctx.level >= wrapLevel)
  const menuItems = $derived(movable.filter((m) => ctx.menuIds.includes(m.id)))
  const dot = $derived(menuItems.some((m) => m.active))

  $effect(() => {
    const k = overflow === 'menu' ? Math.max(0, Math.min(ctx.level - 2, movable.length)) : 0
    ctx.menuIds = movable.slice(0, k).map((m) => m.id)
  })

  let raf = 0
  function schedule(): void {
    if (raf) return
    raf = requestAnimationFrame(async () => {
      raf = 0
      await tick()
      check()
    })
  }

  function check(): void {
    if (!host || !row) return
    const cw = host.clientWidth
    if (cw <= 0) return
    if (!wrapping && row.scrollWidth > cw + 1) {
      thresholds[ctx.level] = Math.max(thresholds[ctx.level] ?? 0, row.scrollWidth)
      ctx.level += 1
      schedule()
      return
    }
    if (ctx.level > 0 && cw >= (thresholds[ctx.level - 1] ?? Infinity)) {
      ctx.level -= 1
      schedule()
    }
  }

  // Item set changed (or first render): re-run from the top.
  let lastCount = -1
  $effect(() => {
    const n = ctx.items.length
    if (n === lastCount) return
    lastCount = n
    thresholds = []
    ctx.level = 0
    schedule()
  })

  onMount(() => {
    const ro = new ResizeObserver(() => schedule())
    if (host) ro.observe(host)
    void document.fonts?.ready.then(() => {
      thresholds = []
      ctx.level = 0
      schedule()
    })
    return () => {
      ro.disconnect()
      cancelAnimationFrame(raf)
    }
  })

  function onMenuClick(e: MouseEvent): void {
    const t = e.target as HTMLElement
    if (t.closest('button:not([role="switch"])')) menuOpen = false
  }
</script>

<div class="tbar" bind:this={host} style:--fui-tbar-gap={space(gap)}>
  <div
    class="tbar-row"
    class:wrapping
    style:justify-content={halign === 'start' ? 'flex-start' : halign === 'center' ? 'center' : 'flex-end'}
    style:align-items={valign === 'start' ? 'flex-start' : valign === 'end' ? 'flex-end' : valign === 'baseline' ? 'baseline' : 'center'}
    bind:this={row}
  >
    {@render children()}
    {#if menuItems.length > 0}
      <span class="tbar-more">
        <Button
          icon={{ name: 'more_vert' }}
          style="text"
          ariaLabel={moreLabel}
          title={moreLabel}
          ariaExpanded={menuOpen}
          onclick={(e) => {
            menuAnchor = e.currentTarget as HTMLElement
            menuOpen = !menuOpen
          }}
        />
        {#if dot}<span class="tbar-dot" aria-hidden="true"></span>{/if}
      </span>
    {/if}
  </div>
  <FloatingView open={menuOpen && menuItems.length > 0} anchor={menuAnchor} onclose={() => (menuOpen = false)} label={moreLabel}>
    {#snippet children()}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div class="tbar-menu" onclick={onMenuClick}>
        {#each menuItems as item (item.id)}
          <ToolbarMenuRow {item} />
        {/each}
      </div>
    {/snippet}
  </FloatingView>
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .tbar { min-width: 0; max-width: 100%; }
  .tbar-row {
    display: flex;
    flex-wrap: nowrap;
    gap: var(--fui-tbar-gap, 0);
    min-width: 0;
  }
  .tbar-row > :global(*) { flex-shrink: 0; }
  .tbar-row.wrapping { flex-wrap: wrap; }
  .tbar-row.wrapping > :global(*) { flex-shrink: 1; }
  .tbar-more { display: inline-flex; position: relative; flex-shrink: 0; margin-inline-start: auto; }
  .tbar-dot {
    position: absolute;
    top: var(--fui-space-1);
    right: var(--fui-space-1);
    width: var(--fui-space-2);
    height: var(--fui-space-2);
    border-radius: 50%;
    background: var(--fui-color-accent);
  }
  .tbar-menu { display: flex; flex-direction: column; gap: var(--fui-space-1); min-width: 0; }
</style>
