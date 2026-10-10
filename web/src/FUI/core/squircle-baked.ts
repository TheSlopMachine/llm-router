// Shared-rule squircles for elements whose size is fixed by design (icon-only chips and
// buttons, checkboxes, switches). Instead of one clip-path + observers PER element, each
// element is measured once and tagged  data-sq="<w>x<h>r<radius>" ; one CSS rule per distinct
// shape does the clipping, so a page with hundreds of switches costs a handful of rules.
//
// There is no name-to-size contract to get wrong: the key IS the measured size. One shared
// ResizeObserver re-tags an element (synchronously, before paint) if its size ever differs,
// so this is always as correct as the dynamic action, just much cheaper.
//
// Native corner-shape engines (Chrome) shape elements for free and always take the dynamic path.
// Re-measure triggers: pointer coarse/fine, exponent change, fonts finishing, and
// rebakeSquircles() / the "fui:rebake-squircles" window event (token edits in the polygon).

import { squircle, squirclePath, supportsNative, onSquircleChange } from './squircle'

export interface SquircleAutoOpts {
  /** Opt in to the shared-rule path (the string is only a debugging label). */
  bake?: string
  /** Wait for the icon font before measuring (icon-sized boxes). */
  fonts?: boolean
  /** Dynamic fallback radius (only used when the CSS radius is 0). Prefer none. */
  radius?: number | string
  /** Disable shaping entirely. */
  off?: boolean
}

interface Rec {
  fonts: boolean
  r: number
  key: string
}

const live = new Map<HTMLElement, Rec>()
const rules = new Set<string>()
let sheet: CSSStyleSheet | null = null
let ro: ResizeObserver | null = null
let iconFont: Promise<void> | null = null
const pendingMeasure = new Set<HTMLElement>()
let microtaskQueued = false

function getSheet(): CSSStyleSheet | null {
  if (sheet) return sheet
  if (typeof document === 'undefined') return null
  const el = document.createElement('style')
  el.setAttribute('data-fui', 'squircle-baked')
  document.head.appendChild(el)
  sheet = el.sheet
  return sheet
}

function ensureRule(key: string, w: number, h: number, r: number): void {
  if (rules.has(key)) return
  const s = getSheet()
  if (!s) return
  s.insertRule(
    `[data-sq="${key}"]{clip-path:path('${squirclePath(w, h, r)}') !important;border-radius:0 !important}`,
    s.cssRules.length,
  )
  rules.add(key)
}

function clearRules(): void {
  const s = getSheet()
  while (s && s.cssRules.length > 0) s.deleteRule(0)
  rules.clear()
}

function readRadius(cs: CSSStyleDeclaration, w: number, h: number): number {
  const raw = cs.borderTopLeftRadius
  const v = parseFloat(raw)
  if (!Number.isFinite(v)) return 0
  const px = raw.includes('%') ? (v / 100) * Math.min(w, h) : v
  return Math.max(0, Math.min(px, w / 2, h / 2))
}

function paint(node: HTMLElement, rec: Rec, w: number, h: number): void {
  const key = `${w}x${h}r${rec.r.toFixed(1)}`
  if (key === rec.key) return
  ensureRule(key, w, h, rec.r)
  node.setAttribute('data-sq', key)
  rec.key = key
}

/** Reads first, writes after: one layout for the whole batch. Nodes must not carry data-sq. */
function measureAll(nodes: Iterable<HTMLElement>): void {
  const reads: [HTMLElement, number, number, number][] = []
  for (const n of nodes) {
    if (!n.isConnected || !live.has(n)) continue
    const w = n.offsetWidth
    const h = n.offsetHeight
    if (!w || !h) continue // not laid out yet; the ResizeObserver will bring it back
    reads.push([n, w, h, readRadius(getComputedStyle(n), w, h)])
  }
  for (const [n, w, h, r] of reads) {
    const rec = live.get(n)
    if (!rec) continue
    rec.r = r
    rec.key = ''
    paint(n, rec, w, h)
  }
}

async function flushPending(): Promise<void> {
  microtaskQueued = false
  const batch = [...pendingMeasure]
  pendingMeasure.clear()
  if (batch.some((n) => live.get(n)?.fonts) && typeof document !== 'undefined' && document.fonts) {
    iconFont ??= document.fonts
      .load('1em "Material Symbols Outlined"')
      .then(() => undefined)
      .catch(() => undefined)
    await iconFont
  }
  measureAll(batch.filter((n) => !n.hasAttribute('data-sq')))
}

function queueMeasure(node: HTMLElement): void {
  pendingMeasure.add(node)
  if (microtaskQueued) return
  microtaskQueued = true
  queueMicrotask(() => void flushPending())
}

function ensureObserver(): ResizeObserver {
  ro ??= new ResizeObserver((entries) => {
    // Runs after layout and before paint: re-tagging here is visible in the same frame.
    const unmeasured: HTMLElement[] = []
    for (const e of entries) {
      const n = e.target as HTMLElement
      const rec = live.get(n)
      if (!rec) continue
      if (rec.key === '') {
        unmeasured.push(n)
        continue
      }
      const w = n.offsetWidth
      const h = n.offsetHeight
      if (w && h) paint(n, rec, w, h)
    }
    if (unmeasured.length) measureAll(unmeasured)
  })
  return ro
}

let rebakeFrame = 0
export function rebakeSquircles(): void {
  if (rebakeFrame || typeof requestAnimationFrame === 'undefined') return
  rebakeFrame = requestAnimationFrame(() => {
    rebakeFrame = 0
    clearRules()
    const nodes = [...live.keys()]
    for (const n of nodes) {
      n.removeAttribute('data-sq') // writes first ...
      const rec = live.get(n)
      if (rec) rec.key = ''
    }
    measureAll(nodes) // ... then one batched read
  })
}

let wired = false
function wire(): void {
  if (wired || typeof window === 'undefined') return
  wired = true
  window.matchMedia?.('(pointer: coarse)').addEventListener?.('change', rebakeSquircles)
  window.addEventListener('fui:rebake-squircles', rebakeSquircles)
  document.fonts?.addEventListener?.('loadingdone', rebakeSquircles)
  onSquircleChange(rebakeSquircles)
}

export function squircleAuto(node: HTMLElement, opts: SquircleAutoOpts = {}) {
  let mode: 'dyn' | 'baked' | 'off' | null = null
  let dyn: { update(r: number | string): void; destroy(): void } | null = null

  function teardown(): void {
    if (mode === 'dyn') dyn?.destroy()
    if (mode === 'baked') {
      ro?.unobserve(node)
      live.delete(node)
      pendingMeasure.delete(node)
      node.removeAttribute('data-sq')
    }
    dyn = null
    mode = null
  }

  function apply(o: SquircleAutoOpts): void {
    const want: 'dyn' | 'baked' | 'off' = o.off ? 'off' : o.bake && !supportsNative ? 'baked' : 'dyn'
    if (want === mode) {
      if (want === 'dyn') dyn?.update(o.radius ?? 10)
      return
    }
    teardown()
    mode = want
    if (want === 'dyn') {
      dyn = squircle(node, o.radius ?? 10)
    } else if (want === 'baked') {
      wire()
      live.set(node, { fonts: o.fonts ?? false, r: 0, key: '' })
      ensureObserver().observe(node)
      queueMeasure(node)
    }
  }

  apply(opts)
  return {
    update(o: SquircleAutoOpts) {
      apply(o)
    },
    destroy() {
      teardown()
    },
  }
}
