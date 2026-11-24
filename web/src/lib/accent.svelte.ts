import { setAccent } from '@fuckyouryan/fui'
import { Persisted } from './persisted.svelte'
// Accent color: a preset or any color picked in Settings, persisted per browser
// (preset name or #rrggbb), applied to both themes. Presets carry tuned hover
// and on-accent text colors; for a custom color FUI setAccent derives them
// (on-accent text is white or dark by luminance).

export interface Accent {
  /** preset name, or 'custom' */
  name: string
  bg: string
  hover?: string
  /** Text color on the accent fill */
  text?: string
}

export const accents: Accent[] = [
  { name: 'blue', bg: '#2483e2', hover: '#1b6dca', text: '#ffffff' },
  { name: 'green', bg: '#16a34a', hover: '#15803d', text: '#ffffff' },
  { name: 'orange', bg: '#ea580c', hover: '#c2410c', text: '#ffffff' },
  { name: 'purple', bg: '#7c3aed', hover: '#6d28d9', text: '#ffffff' },
  { name: 'pink', bg: '#e11d48', hover: '#be123c', text: '#ffffff' },
  { name: 'teal', bg: '#0d9488', hover: '#0f766e', text: '#ffffff' },
  { name: 'yellow', bg: '#fcbd00', hover: '#eab308', text: '#2b2d31' },
  { name: 'graphite', bg: '#4b4f55', hover: '#33373c', text: '#ffffff' },
]

const HEX_COLOR = /^#[0-9a-f]{6}$/i

/** The preset with this fill color, else a custom accent. */
export function accentOf(bg: string): Accent {
  return accents.find((a) => a.bg.toLowerCase() === bg.toLowerCase()) ?? { name: 'custom', bg: bg.toLowerCase() }
}

// Site decision: should elevation overlays pick up the accent?
const ELEV_TINT = false

// Stored as the preset name, or the #rrggbb of a custom color.
export const accent = new Persisted<Accent>('accent', accents[0], {
  parse: (stored) => accents.find((a) => a.name === stored) ?? (HEX_COLOR.test(stored) ? accentOf(stored) : undefined),
  serialize: (a) => (a.name === 'custom' ? a.bg : a.name),
  apply: (a) => setAccent(a.bg, { hover: a.hover, text: a.text, tintElev: ELEV_TINT }),
})
