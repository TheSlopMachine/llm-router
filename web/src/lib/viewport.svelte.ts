import { BP_MOBILE, BP_NARROW } from './breakpoints'

let mobile = $state(typeof window !== 'undefined' ? window.matchMedia(`(max-width: ${BP_MOBILE}px)`).matches : false)
let narrow = $state(typeof window !== 'undefined' ? window.matchMedia(`(max-width: ${BP_NARROW}px)`).matches : false)

if (typeof window !== 'undefined') {
  const mobileMql = window.matchMedia(`(max-width: ${BP_MOBILE}px)`)
  const narrowMql = window.matchMedia(`(max-width: ${BP_NARROW}px)`)
  const onMobileChange = (e: MediaQueryListEvent): void => {
    mobile = e.matches
  }
  const onNarrowChange = (e: MediaQueryListEvent): void => {
    narrow = e.matches
  }
  mobileMql.addEventListener('change', onMobileChange)
  narrowMql.addEventListener('change', onNarrowChange)
}

export const viewport = {
  get mobile(): boolean {
    return mobile
  },
  get narrow(): boolean {
    return narrow
  }
}
