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

// Set to false to disable squircle effect globally
let squircleEnabled = true

let superellipseExp = 0.8 // 2/n with n = 2.5

// Live tuning from the polygon: re-applies every mounted squircle.
export function setSquircleExponent(n: number): void {
  if (!Number.isFinite(n) || n < 2) return
  superellipseExp = 2 / n
  for (const apply of liveApplies) apply()
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

export function squirclePath(width: number, height: number, radius: number): string {
  const r = Math.max(0, Math.min(radius, width / 2, height / 2))
  const w = width
  const h = height
  return [
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
}

const supportsPath = typeof CSS !== 'undefined' && CSS.supports('clip-path', "path('M 0 0 L 1 1 Z')")

// Class-driven squircle for elements rendered in dozens of places where a
// per-element action would be noise (chips). Attaches the action to every
// current and future match; detaches when the node leaves the DOM.
const AUTO_SELECTOR = '.chip'

export function startAutoSquircle(root: ParentNode = document.body): () => void {
  const attached = new WeakMap<HTMLElement, { destroy(): void }>()

  function attach(el: HTMLElement): void {
    if (!attached.has(el)) attached.set(el, squircle(el))
  }
  function scan(node: Node): void {
    if (!(node instanceof HTMLElement)) return
    if (node.matches(AUTO_SELECTOR)) attach(node)
    node.querySelectorAll(AUTO_SELECTOR).forEach((el) => attach(el as HTMLElement))
  }
  function teardown(node: Node): void {
    if (!(node instanceof HTMLElement)) return
    const entry = attached.get(node)
    if (entry) {
      entry.destroy()
      attached.delete(node)
    }
    node.querySelectorAll(AUTO_SELECTOR).forEach((el) => {
      const e = attached.get(el as HTMLElement)
      if (e) {
        e.destroy()
        attached.delete(el as HTMLElement)
      }
    })
  }

  scan(root as unknown as Node)
  const mo = new MutationObserver((muts) => {
    for (const m of muts) {
      m.addedNodes.forEach(scan)
      m.removedNodes.forEach(teardown)
    }
  })
  mo.observe(root, { childList: true, subtree: true })
  return () => mo.disconnect()
}

// CSS border-radius is the source of truth; the action argument is a fallback.
// The inline override (see below) is temporarily cleared to read the CSS value.
// Percent radii (e.g. 50%) resolve against the smaller side.
function resolveRadius(node: HTMLElement, w: number, h: number, fallback: number): number {
  const inline = node.style.borderRadius
  if (inline) node.style.borderRadius = ''
  const raw = getComputedStyle(node).borderTopLeftRadius
  if (inline) node.style.borderRadius = inline
  if (!raw) return fallback
  if (raw.endsWith('%')) {
    const pct = parseFloat(raw)
    return Number.isFinite(pct) ? (pct / 100) * Math.min(w, h) : fallback
  }
  const px = parseFloat(raw)
  return Number.isFinite(px) && px > 0 ? px : fallback
}

// A radius-only CSS change does not move the border box, so ResizeObserver
// alone misses it; window resize (also dispatched by the metrics playground)
// forces every live squircle to re-read its computed radius.
const liveApplies = new Set<() => void>()
let windowHooked = false

function hookWindow(): void {
  if (windowHooked) return
  windowHooked = true
  window.addEventListener('resize', () => {
    for (const apply of liveApplies) apply()
  })
}

export function squircle(node: HTMLElement, radius = 10): { update(r: number): void; destroy(): void } {
  let fallback = radius

  // Early return if squircle is disabled
  if (!squircleEnabled) {
    return {
      update(next: number) {},
      destroy() {},
    }
  }

  // Keep the CSS border-radius as the no-clip-path fallback, but mask it where
  // the clip applies, otherwise the circular radius always wins the silhouette.
  if (supportsPath) node.style.borderRadius = '0'

  function apply(): void {
    const w = node.offsetWidth
    const h = node.offsetHeight
    if (!w || !h) return
    node.style.clipPath = `path('${squirclePath(w, h, resolveRadius(node, w, h, fallback))}')`
  }

  // A component setting style={...} (e.g. Button's tint vars) rewrites the
  // style attribute and wipes our inline clip-path/border-radius. Re-assert
  // them when that happens; our own writes leave clip-path in place and
  // setting an unchanged value does not mutate, so this terminates.
  const mo = new MutationObserver(() => {
    if (supportsPath && node.style.clipPath === '') apply()
    if (supportsPath && node.style.borderRadius !== '0') node.style.borderRadius = '0'
  })
  mo.observe(node, { attributes: true, attributeFilter: ['style'] })

  apply()
  // border-box: padding-only changes grow the widget while the content box
  // (the default observed box) stays put, and the clip must follow the widget.
  const ro = new ResizeObserver(apply)
  try {
    ro.observe(node, { box: 'border-box' })
  } catch {
    ro.observe(node)
  }
  hookWindow()
  liveApplies.add(apply)

  return {
    update(next: number) {
      fallback = next
      apply()
    },
    destroy() {
      mo.disconnect()
      ro.disconnect()
      liveApplies.delete(apply)
      node.style.borderRadius = ''
      node.style.clipPath = ''
    },
  }
}
