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

// --color-accent-soft: the accent at 10% alpha, used for tint fills.
function softOf(hex: string): string {
  const r = parseInt(hex.slice(1, 3), 16)
  const g = parseInt(hex.slice(3, 5), 16)
  const b = parseInt(hex.slice(5, 7), 16)
  return `rgba(${r}, ${g}, ${b}, 0.1)`
}

function applyAccent(a: Accent): void {
  if (typeof window === 'undefined') return
  const root = document.documentElement
  root.style.setProperty('--color-accent', a.bg)
  root.style.setProperty('--color-accent-hover', a.hover)
  root.style.setProperty('--color-accent-soft', softOf(a.bg))
  root.style.setProperty('--color-text-on-button-reverse', a.text)
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
