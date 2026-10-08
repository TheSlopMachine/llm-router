// Accent channel: the site passes one colour, the library derives the rest.
export interface AccentOptions {
  /** defaults to a darkened accent */
  hover?: string
  /** text on accent-filled buttons; defaults to white/dark by luminance */
  text?: string
  /** mix the accent into elevation overlays (see --fui-elev-accent-mix) */
  tintElev?: boolean
}

function parseHex(hex: string): [number, number, number] | null {
  let h = hex.trim().replace('#', '')
  if (h.length === 3) h = h.split('').map((c) => c + c).join('')
  if (!/^[0-9a-fA-F]{6}$/.test(h)) return null
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

function darken(rgb: [number, number, number], k = 0.14): string {
  const c = rgb.map((v) => Math.round(v * (1 - k)).toString(16).padStart(2, '0'))
  return '#' + c.join('')
}

function readableOn(rgb: [number, number, number]): string {
  const [r, g, b] = rgb.map((v) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.5 ? '#2b2d31' : '#ffffff'
}

export function setAccent(bg: string, opts: AccentOptions = {}): void {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  const rgb = parseHex(bg)
  root.style.setProperty('--fui-color-accent', bg)
  root.style.setProperty('--fui-color-accent-hover', opts.hover ?? (rgb ? darken(rgb) : bg))
  root.style.setProperty('--fui-color-accent-soft', rgb ? `rgba(${rgb.join(', ')}, 0.1)` : bg)
  root.style.setProperty('--fui-color-text-on-button-reverse', opts.text ?? (rgb ? readableOn(rgb) : '#ffffff'))
  root.toggleAttribute('data-fui-tint-elev', !!opts.tintElev)
}
