// Anchored floating-layer positioning, shared by Select and Menu.
//
// Extracted from the old god-Dropdown, which mixed a listbox and an action
// menu behind an `isActionMode` flag. The positioning, flip, outside-click
// and reposition-on-scroll logic is identical for both, so it lives here and
// the two widgets keep only their own semantics.

export interface AnchorRect {
  top: number
  left: number
  width: number
  flipped: boolean
}

export interface AnchorOptions {
  /** estimated menu height, used to decide whether to flip above the anchor */
  height: number
  /** minimum menu width; the anchor's own width is used when it is wider */
  minWidth?: number
  gap?: number
  margin?: number
}

/**
 * Fixed coordinates for a floating layer pinned to `anchor`.
 * Fixed positioning is what lets the layer escape overflow:hidden ancestors
 * once it is portaled to <body>.
 */
export function anchorTo(anchor: HTMLElement, options: AnchorOptions): AnchorRect {
  const { height, minWidth = 200, gap = 4, margin = 8 } = options
  const rect = anchor.getBoundingClientRect()
  const width = Math.max(rect.width, minWidth)

  const spaceBelow = window.innerHeight - rect.bottom
  const spaceAbove = rect.top
  const flipped = spaceBelow < height && spaceAbove > spaceBelow

  const top = flipped ? Math.max(margin, rect.top - height - gap) : rect.bottom + gap

  const overflowsRight = rect.left + width > window.innerWidth - margin
  const left = overflowsRight
    ? Math.max(margin, rect.right - width)
    : Math.max(margin, rect.left)

  return { top, left, width: Math.max(1, Math.round(rect.width)), flipped }
}

/**
 * Close-on-outside-click plus reposition-on-scroll/resize.
 * Returns a teardown function; call it from $effect's cleanup.
 */
export function bindDismiss(params: {
  isOpen: () => boolean
  contains: (target: Node) => boolean
  onDismiss: () => void
  onReposition: () => void
}): () => void {
  function handleClick(e: MouseEvent): void {
    if (!params.isOpen()) return
    if (params.contains(e.target as Node)) return
    params.onDismiss()
  }
  function handleReposition(): void {
    if (params.isOpen()) params.onReposition()
  }

  document.addEventListener('click', handleClick)
  window.addEventListener('resize', handleReposition)
  window.addEventListener('scroll', handleReposition, true)

  return () => {
    document.removeEventListener('click', handleClick)
    window.removeEventListener('resize', handleReposition)
    window.removeEventListener('scroll', handleReposition, true)
  }
}

/** Row height used to estimate menu height before it is measured. */
export const MENU_ROW_HEIGHT = 40
export const MENU_MAX_HEIGHT = 180
