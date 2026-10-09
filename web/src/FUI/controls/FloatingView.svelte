<script lang="ts">
  import { scale } from 'svelte/transition'
  import { setContext, untrack, type Snippet } from 'svelte'
  import { portal } from '../core/portal'
  import { squircle } from '../core/squircle'
  import { bindDismiss } from '../core/popover'
  import { shouldDismissOnKey } from '../core/steps'

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
    width,
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
    width?: 'sm' | 'md' | 'lg'
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
    const width = el ? el.offsetWidth : 0
    const height = el ? el.offsetHeight : 0

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

  let floatingStyle = $derived(
    `top: ${menuTop}px; left: ${menuLeft}px; transform-origin: ${originX} ${originY};`
  )

  function close(): void {
    open = false
    onclose?.()
  }

  setContext('floatingview', {
    close,
  })

  function handleKeydown(e: KeyboardEvent): void {
    if (shouldDismissOnKey(e, { closeOnEsc, open })) {
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
    class:floating-sm={width === 'sm'}
    class:floating-md={width === 'md'}
    class:floating-lg={width === 'lg'}
    style={floatingStyle}
    {role}
    aria-label={label}
    tabindex="-1"
    use:squircle
    in:scale={{ start: 0.85, duration: 200, easing: easeOutBackSoft, opacity: 0 }}
    out:scale={{ start: 0.85, duration: 160, easing: easeInBackSoft, opacity: 0 }}
    onkeydown={handleKeydown}
  >
    {@render children?.({ close })}
  </div>
{/if}

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .floating-view {
    position: fixed;
    z-index: var(--fui-z-popover);
    width: max-content;
    background: var(--fui-color-surface-container-high);
    border: none;
    border-radius: var(--fui-radius-lg);
    overflow: hidden;
    padding: var(--fui-space-4);
    outline: none;
    box-sizing: border-box;
  }
  .floating-sm {
    width: min(var(--fui-popover-w-sm), calc(100vw - 2 * var(--fui-space-5)));
  }
  .floating-md {
    width: min(var(--fui-popover-w-md), calc(100vw - 2 * var(--fui-space-5)));
  }
  .floating-lg {
    width: min(var(--fui-popover-w-lg), calc(100vw - 2 * var(--fui-space-5)));
  }
</style>
