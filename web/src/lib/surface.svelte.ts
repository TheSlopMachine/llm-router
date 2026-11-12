// App surface level: picked in Settings, persisted per browser, fed to the
// Scaffold `level` (the sidebar stands there, the content column and modals
// one above). Higher is lighter in the dark theme.

// Shown to people as "background brightness": darker / normal / lighter.
export const surfaceLevels = [-1, 0, 1] as const
export type SurfaceLevel = (typeof surfaceLevels)[number]

const STORAGE_KEY = 'surface-level'
const DEFAULT_LEVEL: SurfaceLevel = 0

function getInitialLevel(): SurfaceLevel {
  if (typeof window === 'undefined') return DEFAULT_LEVEL
  try {
    const stored = Number(localStorage.getItem(STORAGE_KEY))
    const found = surfaceLevels.find((level) => level === stored)
    if (found !== undefined && localStorage.getItem(STORAGE_KEY) !== null) return found
  } catch (e) {
    console.error('Failed to access localStorage for surface level:', e)
  }
  return DEFAULT_LEVEL
}

let current = $state<SurfaceLevel>(getInitialLevel())

export const surfaceLevel: {
  get value(): SurfaceLevel
  set value(next: SurfaceLevel)
} = {
  get value(): SurfaceLevel {
    return current
  },
  set value(next: SurfaceLevel) {
    current = next
    try {
      localStorage.setItem(STORAGE_KEY, String(next))
    } catch (e) {
      console.error('Failed to save surface level to localStorage:', e)
    }
  },
}
