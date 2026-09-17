// Tint math: derive readable color pairs from a user-picked tint.
//
// Filled widgets (Button prominent/none/icon): the tint is the background,
// text is white or ink chosen by HSL lightness — a bright tint gets ink
// (white text has nowhere brighter to go), a dark tint gets white; near the
// midpoint the fill is pulled darker so the pair never meets in the middle.
//
// Text-only tints (TextButton, chip text): the tint is re-lightened to read
// on surfaces — darkened on the light theme, lightened on the dark one.
// Soft tints (chips): translucent fill + adjusted text.

export function hexToHsl(hex: string): { h: number; s: number; l: number } {
  const m = hex.replace('#', '')
  const r = parseInt(m.slice(0, 2), 16) / 255
  const g = parseInt(m.slice(2, 4), 16) / 255
  const b = parseInt(m.slice(4, 6), 16) / 255
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const l = (max + min) / 2
  if (max === min) return { h: 0, s: 0, l }
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h = 0
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0)
  else if (max === g) h = (b - r) / d + 2
  else h = (r - g) / d + 4
  return { h: h * 60, s, l }
}

export function hslToHex(h: number, s: number, l: number): string {
  const a = s * Math.min(l, 1 - l)
  const f = (n: number): string => {
    const k = (n + h / 30) % 12
    const c = l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
    return Math.round(c * 255)
      .toString(16)
      .padStart(2, '0')
  }
  return `#${f(0)}${f(8)}${f(4)}`
}

export interface FillTint {
  bg: string
  hover: string
  text: string
}

// WCAG relative luminance of a #rrggbb color.
export function relativeLuminance(hex: string): number {
  const m = hex.replace('#', '')
  const ch = [0, 2, 4].map((i) => {
    const v = parseInt(m.slice(i, i + 2), 16) / 255
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * ch[0] + 0.7152 * ch[1] + 0.0722 * ch[2]
}

export function contrastRatio(a: string, b: string): number {
  const la = relativeLuminance(a)
  const lb = relativeLuminance(b)
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05)
}

// White or ink — whichever reads better on the given background.
export function contrastText(bgHex: string, ink = '#2b2d31'): string {
  return contrastRatio(bgHex, '#ffffff') >= contrastRatio(bgHex, ink) ? '#ffffff' : ink
}

export function tintFill(hex: string): FillTint {
  const { h, s, l } = hexToHsl(hex)
  let bg = hex
  let text = contrastText(bg)
  // Weak-contrast middle band: pull the fill away from the chosen text —
  // darker for white text, lighter for ink — until the pair separates.
  for (let i = 0; i < 6 && contrastRatio(bg, text) < 3; i++) {
    const step = text === '#ffffff' ? -0.06 : 0.06
    const next = Math.min(0.95, Math.max(0.05, hexToHsl(bg).l + step))
    bg = hslToHex(h, s, next)
  }
  return {
    bg,
    hover: hslToHex(h, s, Math.max(0, hexToHsl(bg).l * 0.86)),
    text,
  }
}

export function tintText(hex: string, dark: boolean): string {
  const { h, s, l } = hexToHsl(hex)
  if (dark) return hslToHex(h, s, Math.min(0.78, Math.max(0.6, l + 0.3)))
  return hslToHex(h, s, Math.min(l * 0.6, 0.45))
}

export interface SoftTint {
  bg: string
  text: string
}

export function tintSoft(hex: string, dark: boolean): SoftTint {
  const m = hex.replace('#', '')
  const r = parseInt(m.slice(0, 2), 16)
  const g = parseInt(m.slice(2, 4), 16)
  const b = parseInt(m.slice(4, 6), 16)
  return { bg: `rgba(${r}, ${g}, ${b}, 0.12)`, text: tintText(hex, dark) }
}
