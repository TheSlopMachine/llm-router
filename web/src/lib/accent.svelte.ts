import { setAccent } from '../FUI/core/accent.svelte'
// Accent color: user-picked in Settings, persisted per browser, applied to
// both themes. Widgets read --color-accent (+hover/soft) and
// --color-text-on-button-reverse; inline properties beat theme rules, so one
// accent covers light and dark.

export interface Accent {
  name: string
  bg: string
  hover: string
  /** Text color on the accent fill */
  text: string
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

const STORAGE_KEY = 'accent'

function getInitialAccent(): Accent {
  if (typeof window === 'undefined') return accents[0]
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    const found = accents.find((a) => a.name === stored)
    if (found) return found
  } catch (e) {
    console.error('Failed to access localStorage for accent:', e)
  }
  return accents[0]
}

// Site decision: should elevation overlays pick up the accent?
const ELEV_TINT = false

function applyAccent(a: Accent): void {
  setAccent(a.bg, { hover: a.hover, text: a.text, tintElev: ELEV_TINT })
}

let current = $state<Accent>(getInitialAccent())

export const accent: {
  get value(): Accent
  set value(a: Accent)
} = {
  get value(): Accent {
    return current
  },
  set value(a: Accent) {
    current = a
    try {
      localStorage.setItem(STORAGE_KEY, a.name)
    } catch (e) {
      console.error('Failed to save accent to localStorage:', e)
    }
    applyAccent(a)
  },
}

if (typeof window !== 'undefined') {
  applyAccent(current)
}
