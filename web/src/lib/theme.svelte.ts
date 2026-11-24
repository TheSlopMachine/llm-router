import { setTheme, type ThemePreference } from '@fuckyouryan/fui'
// Colour scheme: picked in Settings, persisted per browser. FUI follows the system scheme for auto.

export type Theme = ThemePreference

const STORAGE_KEY = 'theme'

function getInitialTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'auto' || stored === 'light' || stored === 'dark') return stored
  } catch (e) {
    console.error('Failed to access localStorage for theme:', e)
  }
  return 'auto'
}

let current = $state<Theme>(getInitialTheme())

export const theme: {
  get value(): Theme
  set value(v: Theme)
} = {
  get value(): Theme {
    return current
  },
  set value(v: Theme) {
    current = v
    try {
      localStorage.setItem(STORAGE_KEY, v)
    } catch (e) {
      console.error('Failed to save theme to localStorage:', e)
    }
    setTheme(v)
  }
}

setTheme(current)
