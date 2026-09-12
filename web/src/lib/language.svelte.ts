export type Language = 'auto' | 'en' | 'ru'

const STORAGE_KEY = 'language'

function getInitialLanguage(): Language {
  if (typeof window === 'undefined') return 'auto'
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'auto' || stored === 'en' || stored === 'ru') return stored
  } catch (e) {
    console.error('Failed to access localStorage for language:', e)
  }
  return 'auto'
}

let current = $state<Language>(getInitialLanguage())

function persist(v: Language): void {
  if (typeof window === 'undefined') return
  try {
    localStorage.setItem(STORAGE_KEY, v)
  } catch (e) {
    console.error('Failed to save language to localStorage:', e)
  }
}

export const language: {
  get value(): Language
  set value(v: Language)
} = {
  get value(): Language {
    return current
  },
  set value(v: Language) {
    persist(v)
    current = v
  }
}
