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

function applyTheme(theme: Theme): void {
  if (typeof window === 'undefined') return

  const root = document.documentElement

  if (theme === 'auto') {
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
    root.classList.toggle('dark', prefersDark)
  } else {
    root.classList.toggle('dark', theme === 'dark')
  }
}

let current = $state<Theme>(getInitialTheme())

export const theme = {
  get value(): Theme {
    return current
  },
  get current(): Theme {
    return current
  },
  set(v: Theme): void {
    if (typeof window !== 'undefined') {
      try {
        localStorage.setItem(STORAGE_KEY, v)
      } catch (e) {
        console.error('Failed to save theme to localStorage:', e)
      }
      applyTheme(v)
    }
    current = v
  },
  cycle(): void {
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

    current = next
  }
}

if (typeof window !== 'undefined') {
  applyTheme(getInitialTheme())

  const mq = window.matchMedia('(prefers-color-scheme: dark)')
  mq.addEventListener('change', () => {
    if (theme.value === 'auto') applyTheme('auto')
  })
}
