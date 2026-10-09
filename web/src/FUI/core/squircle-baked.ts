// Baked squircle clip-paths for elements whose size never depends on content:
// icon-only chips, square icon buttons, checkboxes, switches.
//
// use:squircleAuto={{ bake: 'name' }} measures the FIRST element of that name once,
// writes one shared rule  [data-sq="name"] { clip-path: path(...) }  and tags every
// element of that name with the attribute. Those elements then carry no ResizeObserver,
// no style observer and no per-element work at all.
//
// Everything else keeps the dynamic path: use:squircleAuto without `bake` is exactly
// use:squircle. Native corner-shape engines (Chrome) already shape elements for free,
// so they always take the dynamic branch.
//
// Re-bake triggers: pointer coarse/fine switch (control sizes change), exponent change,
// fonts finishing (icon glyph sizes), and rebakeSquircles() / the "fui:rebake-squircles"
// window event (token edits in the polygon).

import { squircle, squirclePath, supportsNative, onSquircleChange } from './squircle'

export interface SquircleAutoOpts {
  /** Shape name. Same name MUST mean same size and radius. */
  bake?: string
  /** Wait for web fonts before the first measurement (icon glyph sized elements). */
  fonts?: boolean
  /** Dynamic fallback radius (only used when the CSS radius is 0). Prefer none. */
  radius?: number | string
  /** Disable shaping entirely (e.g. a wrapper that no longer paints anything). */
  off?: boolean
}

interface Shape { w: number; h: number; r: number }

const isDev = Boolean((import.meta as unknown as { env?: { DEV?: boolean } }).env?.DEV)
const shapes = new Map<string, Shape>()
const pending = new Map<string, Promise<void>>()
const live = new Map<HTMLElement, { name: string }>()
const warned = new Set<string>()
let styleEl: HTMLStyleElement | null = null

function rebuildStyle(): void {
  if (typeof document === 'undefined') return
  if (!styleEl) {
    styleEl = document.createElement('style')
    styleEl.setAttribute('data-fui', 'squircle-baked')
    document.head.appendChild(styleEl)
  }
  let css = ''
  for (const [name, s] of shapes) {
    css += `[data-sq="${name}"]{clip-path:path('${squirclePath(s.w, s.h, s.r)}') !important;border-radius:0 !important}`
  }
  styleEl.textContent = css
}

function readRadius(cs: CSSStyleDeclaration, w: number, h: number): number {
  const raw = cs.borderTopLeftRadius
  const v = parseFloat(raw)
  if (!Number.isFinite(v)) return 0
  const px = raw.includes('%') ? (v / 100) * Math.min(w, h) : v
  return Math.max(0, Math.min(px, w / 2, h / 2))
}

/** Node must be in the DOM and must NOT carry data-sq (its radius is zeroed by the rule). */
function measure(node: HTMLElement): Shape | null {
  const w = node.offsetWidth
  const h = node.offsetHeight
  if (!w || !h) return null
  return { w, h, r: readRadius(getComputedStyle(node), w, h) }
}

function devCheck(node: HTMLElement, name: string): void {
  if (!isDev) return
  const s = shapes.get(name)
  if (!s || warned.has(name)) return
  if (Math.abs(node.offsetWidth - s.w) > 1 || Math.abs(node.offsetHeight - s.h) > 1) {
    warned.add(name)
    console.warn(
      `[FUI] baked squircle "${name}" is ${s.w}x${s.h} but an element measures ` +
        `${node.offsetWidth}x${node.offsetHeight}. Same name must mean same size; use a dynamic squircle there.`,
    )
  }
}

async function bakeFor(node: HTMLElement, name: string, fonts: boolean): Promise<void> {
  if (!shapes.has(name)) {
    let p = pending.get(name)
    if (!p) {
      p = (async () => {
        if (fonts && document.fonts) {
          // The icon font is requested lazily; force it so icon-sized boxes are measured with real glyphs.
          try { await document.fonts.load('1em "Material Symbols Outlined"') } catch { /* keep fallback metrics; loadingdone re-bakes */ }
          if (document.fonts.status === 'loading') await document.fonts.ready
        }
        if (!node.isConnected || node.hasAttribute('data-sq')) return
        const s = measure(node)
        if (s) {
          shapes.set(name, s)
          rebuildStyle()
        }
      })()
      pending.set(name, p)
      void p.finally(() => pending.delete(name))
    }
    await p
  }
  if (live.get(node)?.name === name && shapes.has(name)) {
    node.setAttribute('data-sq', name)
    devCheck(node, name)
  }
}

let rebakeFrame = 0
export function rebakeSquircles(): void {
  if (rebakeFrame) return
  rebakeFrame = requestAnimationFrame(() => {
    rebakeFrame = 0
    const nodes = [...live.entries()]
    for (const [n] of nodes) n.removeAttribute('data-sq') // writes first ...
    shapes.clear()
    const first = new Map<string, HTMLElement>()
    for (const [n, v] of nodes) if (!first.has(v.name)) first.set(v.name, n)
    for (const [name, n] of first) {
      // ... then reads, one layout for all
      const s = measure(n)
      if (s) shapes.set(name, s)
    }
    warned.clear()
    rebuildStyle()
    for (const [n, v] of nodes) if (shapes.has(v.name)) n.setAttribute('data-sq', v.name)
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
  let name = ''

  function teardown(): void {
    if (mode === 'dyn') dyn?.destroy()
    if (mode === 'baked') {
      live.delete(node)
      node.removeAttribute('data-sq')
    }
    dyn = null
    mode = null
  }

  function apply(o: SquircleAutoOpts): void {
    const want: 'dyn' | 'baked' | 'off' = o.off ? 'off' : o.bake && !supportsNative ? 'baked' : 'dyn'
    if (want === mode && (want !== 'baked' || o.bake === name)) {
      if (want === 'dyn') dyn?.update(o.radius ?? 10)
      return
    }
    teardown()
    mode = want
    if (want === 'dyn') {
      dyn = squircle(node, o.radius ?? 10)
    } else if (want === 'baked') {
      wire()
      name = o.bake as string
      live.set(node, { name })
      void bakeFor(node, name, o.fonts ?? false)
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
