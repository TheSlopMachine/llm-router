import { getContext, setContext, type Snippet } from 'svelte'

export interface AppBarContent {
  title: string
  subtitle?: string | Snippet
  info?: string | Snippet
  actions?: Snippet
}

/** Content slot of the app bar. A page-level <Header> claims it while it is mounted. */
export class AppBarState {
  content = $state<AppBarContent | null>(null)
  private owner: symbol | null = null

  claim(id: symbol, c: AppBarContent): void {
    this.owner = id
    this.content = c
  }
  release(id: symbol): void {
    if (this.owner !== id) return
    this.owner = null
    this.content = null
  }
}

const KEY = Symbol('fui-appbar')
export function setAppBarState(s: AppBarState): void {
  setContext(KEY, s)
}
export function getAppBarState(): AppBarState | null {
  return (getContext(KEY) as AppBarState | undefined) ?? null
}
