// Shared observer registry.
//
// Every widget that needs "re-run when my box changes" or "re-run when my
// style attribute is rewritten" used to create its own ResizeObserver +
// MutationObserver pair. At 128 squircled elements that is 256 observers,
// each with its own callback queue, plus a window resize handler looping
// over all of them synchronously.
//
// One ResizeObserver and one MutationObserver for the whole app instead.
// Callbacks are keyed by node in a Map, and every flush is batched into a
// single rAF so a burst of mutations produces one layout pass, not N.

type NodeCallback = (node: HTMLElement) => void

// ── batching ────────────────────────────────────────────────────────────────
// Reads (offsetWidth) and writes (style.clipPath) interleave badly. Collect
// the nodes first, then run every callback inside one frame.

const pending = new Set<HTMLElement>()
let frame = 0

function flush(): void {
  frame = 0
  const batch = Array.from(pending)
  pending.clear()
  for (const node of batch) {
    if (!node.isConnected) continue
    const cbs = resizeCallbacks.get(node)
    if (cbs) for (const cb of cbs) cb(node)
  }
}

function schedule(node: HTMLElement): void {
  pending.add(node)
  if (frame === 0) frame = requestAnimationFrame(flush)
}

/** Force every registered node to re-run on the next frame. */
export function invalidateAll(): void {
  for (const node of resizeCallbacks.keys()) pending.add(node)
  if (frame === 0) frame = requestAnimationFrame(flush)
}

// ── resize ──────────────────────────────────────────────────────────────────

const resizeCallbacks = new Map<HTMLElement, Set<NodeCallback>>()

let resizeObserver: ResizeObserver | null = null

function ensureResizeObserver(): ResizeObserver {
  if (resizeObserver) return resizeObserver
  resizeObserver = new ResizeObserver((entries) => {
    for (const entry of entries) schedule(entry.target as HTMLElement)
  })
  return resizeObserver
}

export function observeResize(node: HTMLElement, cb: NodeCallback): () => void {
  let set = resizeCallbacks.get(node)
  if (!set) {
    set = new Set()
    resizeCallbacks.set(node, set)
    const ro = ensureResizeObserver()
    try {
      ro.observe(node, { box: 'border-box' })
    } catch {
      ro.observe(node)
    }
  }
  set.add(cb)

  return () => {
    const current = resizeCallbacks.get(node)
    if (!current) return
    current.delete(cb)
    if (current.size === 0) {
      resizeCallbacks.delete(node)
      resizeObserver?.unobserve(node)
      pending.delete(node)
    }
  }
}

// ── style-attribute rewrites ────────────────────────────────────────────────
// A component that sets style={...} replaces the whole attribute and wipes
// any inline property an action wrote. One MutationObserver watches every
// such node; the per-node callback re-asserts what it owns.

const attrCallbacks = new Map<HTMLElement, Set<NodeCallback>>()

let attrObserver: MutationObserver | null = null

function ensureAttrObserver(): MutationObserver {
  if (attrObserver) return attrObserver
  attrObserver = new MutationObserver((mutations) => {
    for (const m of mutations) {
      const node = m.target as HTMLElement
      const cbs = attrCallbacks.get(node)
      if (cbs) for (const cb of cbs) cb(node)
    }
  })
  return attrObserver
}

export function observeStyleAttribute(node: HTMLElement, cb: NodeCallback): () => void {
  let set = attrCallbacks.get(node)
  if (!set) {
    set = new Set()
    attrCallbacks.set(node, set)
    ensureAttrObserver().observe(node, { attributes: true, attributeFilter: ['style'] })
  }
  set.add(cb)

  return () => {
    const current = attrCallbacks.get(node)
    if (!current) return
    current.delete(cb)
    if (current.size === 0) attrCallbacks.delete(node)
    // MutationObserver has no per-node unobserve; the map lookup above is the
    // guard. Entries are dropped when the last callback leaves, so a detached
    // node costs one dead observation record and nothing else.
  }
}

// ── document-level element tracking ─────────────────────────────────────────
// Replaces the ad-hoc `new MutationObserver(document.body, {subtree:true})`
// blocks that each ran a document-wide querySelectorAll on every mutation.
// Here the added/removed nodes are inspected directly, and work is coalesced
// into one microtask per burst.

interface TrackerOptions {
  selector: string
  attach: (el: HTMLElement) => void
  detach?: (el: HTMLElement) => void
}

const trackers: TrackerOptions[] = []
const trackedNodes = new Set<Node>()
let trackerObserver: MutationObserver | null = null
let trackerQueued = false

function runTrackers(added: Node[], removed: Node[]): void {
  for (const tracker of trackers) {
    for (const node of added) {
      if (!(node instanceof HTMLElement)) continue
      if (node.matches(tracker.selector)) tracker.attach(node)
      const nested = node.querySelectorAll<HTMLElement>(tracker.selector)
      for (const el of nested) tracker.attach(el)
    }
    if (!tracker.detach) continue
    for (const node of removed) {
      if (!(node instanceof HTMLElement)) continue
      if (node.matches(tracker.selector)) tracker.detach(node)
      const nested = node.querySelectorAll<HTMLElement>(tracker.selector)
      for (const el of nested) tracker.detach(el)
    }
  }
}

const addedQueue: Node[] = []
const removedQueue: Node[] = []

function drainTrackerQueue(): void {
  trackerQueued = false
  const added = addedQueue.splice(0)
  const removed = removedQueue.splice(0)
  trackedNodes.clear()
  if (added.length === 0 && removed.length === 0) return
  runTrackers(added, removed)
}

function ensureTrackerObserver(): void {
  if (trackerObserver) return
  trackerObserver = new MutationObserver((mutations) => {
    for (const m of mutations) {
      for (const node of m.addedNodes) {
        if (trackedNodes.has(node)) continue
        trackedNodes.add(node)
        addedQueue.push(node)
      }
      for (const node of m.removedNodes) removedQueue.push(node)
    }
    if (!trackerQueued) {
      trackerQueued = true
      queueMicrotask(drainTrackerQueue)
    }
  })
  trackerObserver.observe(document.body, { childList: true, subtree: true })
}

/**
 * Run `attach` for every current and future element matching `selector`,
 * and `detach` when one leaves the DOM. Shares a single document observer
 * with every other tracker.
 */
export function trackElements(options: TrackerOptions): () => void {
  trackers.push(options)
  ensureTrackerObserver()

  const initial = document.body.querySelectorAll<HTMLElement>(options.selector)
  for (const el of initial) options.attach(el)

  return () => {
    const i = trackers.indexOf(options)
    if (i >= 0) trackers.splice(i, 1)
    if (trackers.length === 0) {
      trackerObserver?.disconnect()
      trackerObserver = null
    }
  }
}

// ── viewport ────────────────────────────────────────────────────────────────
// A radius-only CSS change (theme swap, live token tuning) does not move any
// border box, so ResizeObserver misses it. One listener, one batched flush.

let viewportHooked = false

export function hookViewport(): void {
  if (viewportHooked || typeof window === 'undefined') return
  viewportHooked = true
  window.addEventListener('resize', invalidateAll, { passive: true })
}
