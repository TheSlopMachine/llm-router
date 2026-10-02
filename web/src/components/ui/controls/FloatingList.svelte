<script lang="ts">
  // Floating action list. Pairs with an ordinary Button as its trigger:
  // the button opens it, this component draws the anchored layer.
  // Select stays a separate widget; this covers the menu case.
  import { untrack } from 'svelte'
  import { bindDismiss, MENU_ROW_HEIGHT, MENU_MAX_HEIGHT } from '../../../lib/popover'
  import MenuPanel from '../internal/MenuPanel.svelte'
  import MenuItem from '../internal/MenuItem.svelte'
  import Icon from './Icon.svelte'

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

<MenuPanel
  {open}
  bind:el={menuElement}
  top={menuTop}
  left={menuLeft}
  minWidth={menuWidth}
  {flipped}
  role="menu"
  {label}
  onkeydown={handleKeydown}
>
  {#each actions as act (act.id)}
    <MenuItem role="menuitem" tint={act.tint} disabled={act.disabled} onclick={() => run(act)}>
      {#if act.icon}<span class="glyph"><Icon name={act.icon} size="md" /></span>{/if}
      {act.label}
    </MenuItem>
  {/each}
</MenuPanel>

<style>
  .glyph {
    display: inline-flex;
    margin-right: var(--space-3);
  }
</style>
