import { MediaQuery } from 'svelte/reactivity'
import { BP_MOBILE, BP_NARROW } from './breakpoints'

const mobile = new MediaQuery(`max-width: ${BP_MOBILE}px`)
const narrow = new MediaQuery(`max-width: ${BP_NARROW}px`)

export const viewport = {
  get mobile(): boolean {
    return mobile.current
  },
  get narrow(): boolean {
    return narrow.current
  }
}
