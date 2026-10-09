import { BP_MOBILE } from './breakpoints'

let mobile = $state(typeof window !== 'undefined' ? window.matchMedia(`(max-width: ${BP_MOBILE}px)`).matches : false)

if (typeof window !== 'undefined') {
  const mql = window.matchMedia(`(max-width: ${BP_MOBILE}px)`)
  const onChange = (e: MediaQueryListEvent): void => {
    mobile = e.matches
  }
  mql.addEventListener('change', onChange)
}

export const viewport = {
  get mobile(): boolean {
    return mobile
  }
}
