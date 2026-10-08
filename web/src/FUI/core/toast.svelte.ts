// Toast notifications: top-right transient cards.
// Usage: toast.success('Saved'), toast.error(message)
//
// Each toast has a TTL timer; hovering a toast pauses it (toast.pause) and
// leaving resumes with the remaining time (toast.resume).

export interface ToastItem {
  id: number
  kind: 'success' | 'error'
  text: string
}

let items = $state<ToastItem[]>([])
let seq = 0
const MAX_VISIBLE = 3
const TTL_MS = 4000

interface Timer {
  handle: ReturnType<typeof setTimeout> | null
  deadline: number
  remaining: number
}

const timers = new Map<number, Timer>()

function schedule(id: number, ms: number): void {
  const t = timers.get(id)
  if (!t) return
  t.remaining = ms
  t.deadline = Date.now() + ms
  t.handle = setTimeout(() => dismiss(id), ms)
}

function dismiss(id: number): void {
  const t = timers.get(id)
  if (t?.handle) clearTimeout(t.handle)
  timers.delete(id)
  items = items.filter((t) => t.id !== id)
}

function push(kind: ToastItem['kind'], text: string): void {
  const id = ++seq
  items = [...items, { id, kind, text }].slice(-MAX_VISIBLE)
  timers.set(id, { handle: null, deadline: 0, remaining: TTL_MS })
  schedule(id, TTL_MS)
}

export const toast = {
  success(text: string): void { push('success', text) },
  error(text: string): void { push('error', text) },
  dismiss,
  /** Freeze the TTL while hovered. */
  pause(id: number): void {
    const t = timers.get(id)
    if (!t?.handle) return
    clearTimeout(t.handle)
    t.handle = null
    t.remaining = Math.max(0, t.deadline - Date.now())
  },
  /** Resume after pause with the remaining time. */
  resume(id: number): void {
    const t = timers.get(id)
    if (!t || t.handle) return
    schedule(id, t.remaining)
  },
  get items(): ToastItem[] { return items },
}
