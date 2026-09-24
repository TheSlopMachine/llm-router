<script lang="ts">
  // Floating action list. Pairs with an ordinary Button as its trigger:
  // the button opens it, this component draws the anchored layer.
  // Select stays a separate widget; this covers the menu case.
  import { fly, fade } from 'svelte/transition'
  import { cubicOut, cubicIn } from 'svelte/easing'
  import { untrack } from 'svelte'
  import { portal } from '../../../lib/portal'
  import { squircle } from '../../../lib/squircle'
  import { bindDismiss, MENU_ROW_HEIGHT, MENU_MAX_HEIGHT } from '../../../lib/popover'

  export type FloatingAction = {
    id: string
    label: string
    icon?: string
    disabled?: boolean
    tint?: string
  }

  let {
    actions = [],
    open = $bindable(false),
    anchor,
    label = 'Actions',
    minWidth = 200,
    onaction,
    onclose,
  } = $props<{
    actions: FloatingAction[]
    open?: boolean
    anchor?: HTMLElement | undefined
    label?: string
    minWidth?: number
    onaction?: (id: string) => void
    onclose?: () => void
  }>()

  let menuElement = $state<HTMLDivElement>()
  let flipped = $state(false)
  let menuTop = $state(0)
  let menuLeft = $state(0)
  let menuWidth = $state(0)

  const GAP = 4
  const MARGIN = 8

  // Position computes itself: below the anchor, right edges aligned,
  // expanding left. Above when the anchor sits close to the viewport
  // bottom. Left edges aligned, expanding right, when close to the
  // viewport left edge.
  function position(): void {
    if (!anchor) return
    const height = Math.min(actions.length * MENU_ROW_HEIGHT + 8, MENU_MAX_HEIGHT)
    const rect = anchor.getBoundingClientRect()
    const width = Math.max(rect.width, minWidth)

    const spaceBelow = window.innerHeight - rect.bottom
    const spaceAbove = rect.top
    if (spaceBelow >= height + GAP) {
      menuTop = rect.bottom + GAP
      flipped = false
    } else if (spaceAbove >= height + GAP) {
      menuTop = Math.max(MARGIN, rect.top - height - GAP)
      flipped = true
    } else if (spaceBelow >= spaceAbove) {
      menuTop = rect.bottom + GAP
      flipped = false
    } else {
      menuTop = Math.max(MARGIN, rect.top - height - GAP)
      flipped = true
    }

    const rightAligned = rect.right - width
    if (rightAligned >= MARGIN) {
      menuLeft = rightAligned
    } else {
      menuLeft = Math.max(MARGIN, Math.min(rect.left, window.innerWidth - width - MARGIN))
    }
    menuWidth = Math.max(1, Math.round(width))
  }

  function close(): void {
    open = false
    onclose?.()
  }

  function run(action: FloatingAction): void {
    if (action.disabled) return
    onaction?.(action.id)
    close()
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && open) {
      e.preventDefault()
      e.stopPropagation()
      close()
      anchor?.focus()
    }
  }

  $effect(() => {
    if (open) position()
  })

  $effect(() =>
    bindDismiss({
      isOpen: () => untrack(() => open),
      contains: (target) =>
        Boolean(menuElement?.contains(target)) || Boolean(anchor?.contains(target)),
      onDismiss: () => {
        open = false
        onclose?.()
      },
      onReposition: position,
    })
  )
</script>

{#if open}
  <div
    use:portal
    bind:this={menuElement}
    class="dropdown-menu"
    style="top: {menuTop}px; left: {menuLeft}px; min-width: {menuWidth}px;"
    role="menu"
    aria-label={label}
    tabindex="-1"
    use:squircle={12}
    in:fly={{ y: flipped ? 8 : -8, duration: 200, easing: cubicOut, opacity: 0 }}
    out:fade={{ duration: 150, easing: cubicIn }}
    onkeydown={handleKeydown}
  >
    <div class="dropdown-options">
      {#each actions as act (act.id)}
        <button
          class="dropdown-option"
          style={act.tint ? `color: ${act.tint}` : undefined}
          onclick={() => run(act)}
          disabled={act.disabled}
          role="menuitem"
        >
          {#if act.icon}<span class="icon menu-glyph">{act.icon}</span>{/if}
          {act.label}
        </button>
      {/each}
    </div>
  </div>
{/if}

<style>
  .menu-glyph {
    font-size: var(--text-md);
    margin-right: var(--space-3);
  }
</style>
