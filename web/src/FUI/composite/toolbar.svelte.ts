import { getContext, setContext, type Snippet } from 'svelte'

export interface ToolbarItemReg {
  id: string
  pinned: boolean
  priority: number | undefined
  label: string | undefined
  active: boolean
  children: Snippet
}

/** Shared state of one Toolbar: degradation level and the items that moved to the menu. */
export class ToolbarCtx {
  /** 0 full, 1 icon-only buttons, 2 compact search, 3+ items moving to the menu / wrapping */
  level = $state(0)
  items = $state<ToolbarItemReg[]>([])
  menuIds = $state<string[]>([])

  register(item: ToolbarItemReg): void {
    this.items = [...this.items, item]
  }
  unregister(id: string): void {
    this.items = this.items.filter((i) => i.id !== id)
  }
  update(item: ToolbarItemReg): void {
    this.items = this.items.map((i) => (i.id === item.id ? item : i))
  }
}

const KEY = Symbol('fui-toolbar')
const PLACE = Symbol('fui-toolbar-placement')
const PIN = Symbol('fui-toolbar-pinned')

export function setToolbarCtx(c: ToolbarCtx): void {
  setContext(KEY, c)
}
export function getToolbarCtx(): ToolbarCtx | null {
  return (getContext(KEY) as ToolbarCtx | undefined) ?? null
}
export function setToolbarPlacement(p: 'panel' | 'menu'): void {
  setContext(PLACE, p)
}
export function getToolbarPlacement(): 'panel' | 'menu' {
  return (getContext(PLACE) as 'panel' | 'menu' | undefined) ?? 'panel'
}
export function setToolbarPinned(v: boolean): void {
  setContext(PIN, v)
}
export function getToolbarPinned(): boolean {
  return (getContext(PIN) as boolean | undefined) ?? false
}
