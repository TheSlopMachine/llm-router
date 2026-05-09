import { writable } from 'svelte/store'

export type Theme = 'auto' | 'light' | 'dark'

const STORAGE_KEY = 'theme'

function getInitialTheme(): Theme {
  if (typeof window === 'undefined') return 'auto'
  
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'auto' || stored === 'light' || stored === 'dark') {
      return stored
    }
  } catch (e) {
    console.error('Failed to access localStorage for theme:', e)
  }
  
  return 'auto'
}

function applyTheme(theme: Theme) {
  if (typeof window === 'undefined') return
  
  const root = document.documentElement
  
  if (theme === 'auto') {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    root.classList.toggle('dark', prefersDark)
  } else {
    root.classList.toggle('dark', theme === 'dark')
  }
}

function createThemeStore() {
  const { subscribe, set, update } = writable<Theme>(getInitialTheme())
  
  return {
    subscribe,
    set: (theme: Theme) => {
      if (typeof window !== 'undefined') {
        try {
          localStorage.setItem(STORAGE_KEY, theme)
        } catch (e) {
          console.error('Failed to save theme to localStorage:', e)
        }
        applyTheme(theme)
      }
      set(theme)
    },
    cycle: () => {
      update(current => {
        const themes: Theme[] = ['auto', 'light', 'dark']
        const currentIndex = themes.indexOf(current)
        const next = themes[(currentIndex + 1) % themes.length]
        
        if (typeof window !== 'undefined') {
          try {
            localStorage.setItem(STORAGE_KEY, next)
          } catch (e) {
            console.error('Failed to save theme to localStorage during cycle:', e)
          }
          applyTheme(next)
        }
        
        return next
      })
    }
  }
}

export const theme = createThemeStore()

// Initialize theme on import
if (typeof window !== 'undefined') {
  applyTheme(getInitialTheme())
  
  // Listen for system preference changes
  const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
  mediaQuery.addEventListener('change', () => {
    const current = getInitialTheme()
    if (current === 'auto') {
      applyTheme('auto')
    }
  })
}
