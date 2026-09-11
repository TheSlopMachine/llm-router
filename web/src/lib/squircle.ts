// Squircle (superellipse, Lamé curve n=4) clip-path for arbitrary elements.
//
// The action measures the element and sets clip-path: path() in real pixels,
// so corners keep uniform curvature at any size (objectBoundingBox masks
// distort corners by aspect ratio). clip-path: path() works in Chrome 88+,
// Safari 15.4+, Firefox 97+; older engines keep the CSS border-radius
// fallback, so always keep a border-radius on the element.
//
// Squircle elements are borderless by design: CSS borders do not follow a
// clip-path. Use a fill that contrasts with the background instead of a
// border (the Apple grouped-list approach).

const SUPERELLIPSE_EXP = 0.5 // 2/n with n = 4
const CORNER_SAMPLES = 10

function pt(x: number, y: number): string {
  return `L ${x.toFixed(2)} ${y.toFixed(2)}`
}

// One superellipse corner arc, sampled clockwise in path direction.
// quadrant: 0 = top-right, 1 = bottom-right, 2 = bottom-left, 3 = top-left.
function cornerArc(cx: number, cy: number, r: number, quadrant: number): string {
  const points: string[] = []
  for (let i = 0; i <= CORNER_SAMPLES; i++) {
    const t = (i / CORNER_SAMPLES) * (Math.PI / 2)
    const s = r * Math.pow(Math.sin(t), SUPERELLIPSE_EXP)
    const c = r * Math.pow(Math.cos(t), SUPERELLIPSE_EXP)
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

export function squircle(node: HTMLElement, radius = 10): { update(r: number): void; destroy(): void } {
  let r = radius

  function apply(): void {
    const w = node.offsetWidth
    const h = node.offsetHeight
    if (!w || !h) return
    node.style.clipPath = `path('${squirclePath(w, h, r)}')`
  }

  apply()
  const ro = new ResizeObserver(apply)
  ro.observe(node)

  return {
    update(next: number) {
      r = next
      apply()
    },
    destroy() {
      ro.disconnect()
    },
  }
}
