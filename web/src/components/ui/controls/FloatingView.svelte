<script lang="ts">
  import { scale } from 'svelte/transition'
  import { setContext, untrack, type Snippet } from 'svelte'
  import { portal } from '../../../lib/portal'
  import { squircle } from '../../../lib/squircle'
  import { bindDismiss } from '../../../lib/popover'

  function easeOutBackSoft(x: number): number {
    const c1 = 0.8
    const c3 = c1 + 1
    return 1 + c3 * Math.pow(x - 1, 3) + c1 * Math.pow(x - 1, 2)
  }

  function easeInBackSoft(x: number): number {
    const c1 = 0.8
    const c3 = c1 + 1
    return c3 * x * x * x - c1 * x * x
  }

  let {
    open = $bindable(false),
    anchor,
    placement = 'auto',
    align = 'end',
    gap = 4,
    margin = 8,
    minWidth,
    maxWidth = 480,
    role = 'dialog',
    label,
    closeOnOutside = true,
    closeOnEsc = true,
    onclose,
    onopen,
    children,
  } = $props<{
    open?: boolean
    anchor?: HTMLElement | undefined
    placement?: 'auto' | 'below' | 'above'
    align?: 'start' | 'end' | 'center'
    gap?: number
    margin?: number
    minWidth?: number
    maxWidth?: number
    role?: string
    label?: string
    closeOnOutside?: boolean
    closeOnEsc?: boolean
    onclose?: () => void
    onopen?: () => void
    children?: Snippet<[{ close: () => void }]>
  }>()

  let menuElement = $state<HTMLDivElement>()
  let flipped = $state(false)
  let menuTop = $state(0)
  let menuLeft = $state(0)
  let originX = $state('50%')
  let originY = $state('top')

  function position(node?: HTMLElement): void {
    if (!anchor) return
    const rect = anchor.getBoundingClientRect()
    const el = node || menuElement
    const width = el ? el.offsetWidth : (minWidth || 240)
    const height = el ? el.offsetHeight : 140

    const vv = typeof window !== 'undefined' ? window.visualViewport : null
    const vvTop = vv ? vv.offsetTop : 0
    const vvLeft = vv ? vv.offsetLeft : 0
    const vvHeight = vv ? vv.height : window.innerHeight
    const vvWidth = vv ? vv.width : window.innerWidth

    const anchorTop = rect.top + vvTop
    const anchorBottom = rect.bottom + vvTop
    const anchorLeft = rect.left + vvLeft
    const anchorRight = rect.right + vvLeft

    const spaceBelow = vvTop + vvHeight - anchorBottom
    const spaceAbove = anchorTop - vvTop

    if (placement === 'below') {
      flipped = false
    } else if (placement === 'above') {
      flipped = true
    } else if (spaceBelow >= height + gap) {
      flipped = false
    } else if (spaceAbove >= height + gap) {
      flipped = true
    } else if (spaceBelow >= spaceAbove) {
      flipped = false
    } else {
      flipped = true
    }

    if (flipped) {
      menuTop = Math.max(vvTop + margin, anchorTop - height - gap)
      originY = 'bottom'
    } else {
      menuTop = Math.min(vvTop + vvHeight - height - margin, anchorBottom + gap)
      originY = 'top'
    }

    let idealLeft = anchorLeft
    if (align === 'end') {
      idealLeft = anchorRight - width
    } else if (align === 'center') {
      idealLeft = anchorLeft + (rect.width - width) / 2
    } else {
      idealLeft = anchorLeft
    }

    menuLeft = Math.max(vvLeft + margin, Math.min(idealLeft, vvLeft + vvWidth - width - margin))
    const anchorCenterX = anchorLeft + rect.width / 2
    originX = `${Math.round(anchorCenterX - menuLeft)}px`
  }

  function close(): void {
    open = false
    onclose?.()
  }

  setContext('floatingview', {
    close,
  })

  function handleKeydown(e: KeyboardEvent): void {
    if (closeOnEsc && e.key === 'Escape' && open) {
      e.preventDefault()
      e.stopPropagation()
      close()
      anchor?.focus()
    }
  }

  function initLayer(node: HTMLDivElement) {
    position(node)
    const ro = new ResizeObserver(() => {
      position(node)
    })
    ro.observe(node)
    return {
      destroy() {
        ro.disconnect()
      },
    }
  }

  $effect(() => {
    void open
    untrack(() => {
      if (open) {
        onopen?.()
      }
    })
  })

  $effect(() => {
    if (open && anchor) {
      anchor.classList.add('active')
      anchor.setAttribute('aria-expanded', 'true')
      return () => {
        anchor.classList.remove('active')
        anchor.removeAttribute('aria-expanded')
      }
    }
  })

  $effect(() =>
    bindDismiss({
      isOpen: () => untrack(() => open),
      contains: (target) =>
        Boolean(menuElement?.contains(target)) || Boolean(anchor?.contains(target)),
      onDismiss: () => {
        if (closeOnOutside) close()
      },
      onReposition: () => position(menuElement),
    })
  )
</script>

{#if open}
  <div
    use:portal
    use:initLayer
    bind:this={menuElement}
    class="floating-view"
    style="top: {menuTop}px; left: {menuLeft}px; transform-origin: {originX} {originY}; {minWidth ? `min-width: ${minWidth}px;` : ''} {maxWidth ? `max-width: min(${maxWidth}px, calc(100vw - 16px));` : ''}"
    {role}
    aria-label={label}
    tabindex="-1"
    use:squircle={14}
    in:scale={{ start: 0.85, duration: 200, easing: easeOutBackSoft, opacity: 0 }}
    out:scale={{ start: 0.85, duration: 160, easing: easeInBackSoft, opacity: 0 }}
    onkeydown={handleKeydown}
  >
    {@render children?.({ close })}
  </div>
{/if}

<style>
  .floating-view {
    position: fixed;
    z-index: 2000;
    width: max-content;
    background: var(--color-surface-container-high);
    border: none;
    border-radius: var(--radius-lg);
    box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.25), 0 8px 10px -6px rgba(0, 0, 0, 0.2);
    overflow: hidden;
    padding: var(--space-4);
    outline: none;
    box-sizing: border-box;
  }
</style>
