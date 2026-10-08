// Resolved colour scheme. The site decides *which* one (auto, persistence, ...)
// and hands the library 'light' | 'dark'.
export type Theme = 'light' | 'dark'

let current = $state<Theme>('light')

export const theme: { readonly value: Theme } = {
  get value(): Theme {
    return current
  },
}

export function setTheme(v: Theme): void {
  current = v
  if (typeof document !== 'undefined') document.documentElement.classList.toggle('dark', v === 'dark')
}
