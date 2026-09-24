// Squircle (superellipse, Lamé curve n=2.5) clip-path for arbitrary elements.
//
// The action measures the element's border box and sets clip-path: path() in
// real pixels, so corners keep uniform curvature at any size (objectBoundingBox
// masks distort corners by aspect ratio). clip-path: path() works in Chrome 88+,
// Safari 15.4+, Firefox 97+; older engines keep the CSS border-radius
// fallback, so always keep a border-radius on the element.
//
// The corner radius comes from the element's computed border-radius — CSS is
// the single source of truth, so theme variables rescale the clip live. The
// numeric action argument is only a fallback for elements without a CSS radius.
//
// The rendered silhouette is the intersection of clip-path and border-radius,
// and a superellipse corner always contains the circular corner of the same
// radius — so a live border-radius would mask the clip entirely. The action
// therefore zeroes the radius inline (keeping the CSS rule as the fallback
// for engines without clip-path: path()) and reads the CSS value back by
// temporarily clearing that inline override.
//
// Squircle elements are borderless by design: CSS borders do not follow a
// clip-path. Use a fill that contrasts with the background instead of a
// border (the Apple grouped-list approach).
//
// -- Performance ------------------------------------------------------------
// Four things keep this cheap at hundreds of elements:
//   1. Observers are shared (lib/observers.ts): one ResizeObserver and one
//      MutationObserver for the whole app instead of a pair per element.
//   2. Paths are memoised on (width, height, radius, exponent). A table of
//      40 identical chips computes the geometry once.
//   3. Applies are batched into a rAF and a node whose box and radius did not
//      change writes nothing, so a resize storm costs one pass.
//   4. Within that pass, the read/write/read/write dance needed to peek at
//      the CSS radius (see resolveAndCommit below) is phase-separated across
//      every node in the burst rather than interleaved per node — see the
//      comment on resolveAndCommit for why that matters.

import {
  observeResize,
  observeStyleAttribute,
  trackElements,
  hookViewport,
  invalidateAll,
} from './observers'

let superellipseExp = 0.8 // 2/n with n = 2.5

// Live tuning from the polygon: re-applies every mounted squircle.
export function setSquircleExponent(n: number): void {
  if (!Number.isFinite(n) || n < 2) return
  superellipseExp = 2 / n
  pathCache.clear()
  invalidateAll()
}

const CORNER_SAMPLES = 24

function pt(x: number, y: number): string {
  return `L ${x.toFixed(2)} ${y.toFixed(2)}`
}

// One superellipse corner arc, sampled clockwise in path direction.
// quadrant: 0 = top-right, 1 = bottom-right, 2 = bottom-left, 3 = top-left.
function cornerArc(cx: number, cy: number, r: number, quadrant: number): string {
  const points: string[] = []
  for (let i = 0; i <= CORNER_SAMPLES; i++) {
    const t = (i / CORNER_SAMPLES) * (Math.PI / 2)
    const s = r * Math.pow(Math.sin(t), superellipseExp)
    const c = r * Math.pow(Math.cos(t), superellipseExp)
    switch (quadrant) {
      case 0: points.push(pt(cx + s, cy - c)); break // (cx, cy-r) -> (cx+r, cy)
      case 1: points.push(pt(cx + c, cy + s)); break // (cx+r, cy) -> (cx, cy+r)
      case 2: points.push(pt(cx - s, cy + c)); break // (cx, cy+r) -> (cx-r, cy)
      default: points.push(pt(cx - c, cy - s)); break // (cx-r, cy) -> (cx, cy-r)
    }
  }
  return points.join(' ')
}

// Identical geometry recurs constantly -- every chip in a table, every row
// button. Memoise on the rounded box so the 25-point-per-corner sampling runs
// once per distinct shape instead of once per element.
const pathCache = new Map<string, string>()
const PATH_CACHE_LIMIT = 512

export function squirclePath(width: number, height: number, radius: number): string {
  const r = Math.max(0, Math.min(radius, width / 2, height / 2))
  const key = `${width.toFixed(1)}x${height.toFixed(1)}r${r.toFixed(2)}`
  const hit = pathCache.get(key)
  if (hit !== undefined) return hit

  const w = width
  const h = height
  const path = [
    `M ${r.toFixed(2)} 0`,
    `L ${(w - r).toFixed(2)} 0`,
    cornerArc(w - r, r, r, 0),
    `L ${w} ${(h - r).toFixed(2)}`,
    cornerArc(w - r, h - r, r, 1),
    `L ${r.toFixed(2)} ${h}`,
    cornerArc(r, h - r, r, 2),
    `L 0 ${r.toFixed(2)}`,
    cornerArc(r, r, r, 3),
    'Z',
  ].join(' ')

  // Bounded cache: drop the oldest entry rather than grow without limit on a
  // long-lived dashboard whose tables resize continuously.
  if (pathCache.size >= PATH_CACHE_LIMIT) {
    const oldest = pathCache.keys().next().value
    if (oldest !== undefined) pathCache.delete(oldest)
  }
  pathCache.set(key, path)
  return path
}

const supportsPath = typeof CSS !== 'undefined' && CSS.supports('clip-path', "path('M 0 0 L 1 1 Z')")

// Class-driven squircle for elements rendered in dozens of places where a
// per-element action would be noise (chips). Shares the app-wide element
// tracker instead of opening its own document observer.
const AUTO_SELECTOR = '.chip'

export function startAutoSquircle(): () => void {
  if (!supportsPath) return () => {}
  const attached = new WeakMap<HTMLElement, { destroy(): void }>()
  return trackElements({
    selector: AUTO_SELECTOR,
    attach(el) {
      if (!attached.has(el)) attached.set(el, squircle(el))
    },
    detach(el) {
      const entry = attached.get(el)
      if (entry) {
        entry.destroy()
        attached.delete(el)
      }
    },
  })
}

// CSS border-radius is the source of truth; the action argument is a fallback.
// Percent radii (e.g. 50%) resolve against the smaller side. Pure function:
// the inline-override clear/restore around this read lives in
// resolveAndCommit now, batched across every node in the burst.
function readCssRadius(node: HTMLElement, w: number, h: number, fallback: number): number {
  const raw = getComputedStyle(node).borderTopLeftRadius
  if (!raw) return fallback
  if (raw.endsWith('%')) {
    const pct = parseFloat(raw)
    return Number.isFinite(pct) ? (pct / 100) * Math.min(w, h) : fallback
  }
  const px = parseFloat(raw)
  return Number.isFinite(px) && px > 0 ? px : fallback
}

interface ApplyItem {
  node: HTMLElement
  fallbackBox: { value: number }
  keyBox: { value: string }
  destroyed: boolean
}

// Resolving the true CSS radius means clearing our inline override, reading
// getComputedStyle, then restoring the override -- and getComputedStyle
// flushes *every* pending style write in the document, not just this node's.
// Doing that dance once per node, interleaved with the next node's
// offsetWidth read (itself a layout-forcing read), forces one recalculation
// per node in the burst instead of one for the whole burst -- exactly the
// thrashing pattern the shared rAF batching in observers.ts was meant to
// avoid, just pushed down a level. Phase-separating it here (read every box,
// clear every radius, read every radius, restore every radius, then commit)
// means only the *first* radius read forces a flush; the rest land clean.
function resolveAndCommit(items: ApplyItem[]): void {
  const live = items.filter((it) => !it.destroyed && it.node.isConnected)
  const n = live.length
  if (n === 0) return

  const w = new Array<number>(n)
  const h = new Array<number>(n)
  for (let i = 0; i < n; i++) {
    w[i] = live[i].node.offsetWidth
    h[i] = live[i].node.offsetHeight
  }

  for (let i = 0; i < n; i++) {
    if (w[i] && h[i] && live[i].node.style.borderRadius) {
      live[i].node.style.borderRadius = ''
    }
  }

  const r = new Array<number>(n)
  for (let i = 0; i < n; i++) {
    r[i] = w[i] && h[i]
      ? readCssRadius(live[i].node, w[i], h[i], live[i].fallbackBox.value)
      : live[i].fallbackBox.value
  }

  for (let i = 0; i < n; i++) {
    if (w[i] && h[i]) live[i].node.style.borderRadius = '0'
  }

  for (let i = 0; i < n; i++) {
    if (!w[i] || !h[i]) continue
    const { node, keyBox } = live[i]
    const key = `${w[i]}x${h[i]}r${r[i].toFixed(2)}`
    if (key === keyBox.value && node.style.clipPath !== '') continue
    keyBox.value = key
    node.style.clipPath = `path('${squirclePath(w[i], h[i], r[i])}')`
  }
}

// Auto-trigger queue (ResizeObserver / style-attribute reapply). A microtask
// still runs before the next paint, so nothing visible is delayed -- it just
// lets every node touched within the same synchronous callback burst (e.g.
// observers.ts's own flush() looping over a resize batch) land in one
// resolveAndCommit call instead of N. Manual init/update() stay synchronous
// on purpose: single-element, user-driven, no burst to coalesce.
let applyQueue: ApplyItem[] = []
let applyScheduled = false

function queueApply(item: ApplyItem): void {
  applyQueue.push(item)
  if (!applyScheduled) {
    applyScheduled = true
    queueMicrotask(() => {
      applyScheduled = false
      const queue = applyQueue
      applyQueue = []
      resolveAndCommit(queue)
    })
  }
}

export function squircle(
  node: HTMLElement,
  radius = 10
): { update(r: number): void; destroy(): void } {
  const fallbackBox = { value: radius }

  if (!supportsPath) {
    // No clip-path support: the CSS border-radius fallback is the whole
    // behaviour, so skip every observer and measurement.
    return {
      update(next: number) {
        fallbackBox.value = next
      },
      destroy() {},
    }
  }

  // Keep the CSS border-radius as the no-clip-path fallback, but mask it where
  // the clip applies, otherwise the circular radius always wins the silhouette.
  node.style.borderRadius = '0'

  const keyBox = { value: '' }
  const item: ApplyItem = { node, fallbackBox, keyBox, destroyed: false }

  // A component setting style={...} (e.g. Button's tint vars) rewrites the
  // style attribute and wipes our inline clip-path/border-radius. Re-assert
  // them when that happens; our own writes leave clip-path in place and
  // setting an unchanged value does not mutate, so this terminates.
  const stopAttr = observeStyleAttribute(node, () => {
    if (node.style.borderRadius !== '0') node.style.borderRadius = '0'
    if (node.style.clipPath === '') {
      keyBox.value = ''
      queueApply(item)
    }
  })

  resolveAndCommit([item])
  // border-box: padding-only changes grow the widget while the content box
  // (the default observed box) stays put, and the clip must follow the widget.
  const stopResize = observeResize(node, () => queueApply(item))
  hookViewport()

  return {
    update(next: number) {
      fallbackBox.value = next
      keyBox.value = ''
      resolveAndCommit([item])
    },
    destroy() {
      item.destroyed = true
      stopAttr()
      stopResize()
      node.style.borderRadius = ''
      node.style.clipPath = ''
    },
  }
}
