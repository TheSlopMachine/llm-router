// Toast notifications: top-right transient cards.
// Usage: toast.success('Saved'), toast.error(message)

export interface ToastItem {
  id: number
  kind: 'success' | 'error'
  text: string
}

let items = $state<ToastItem[]>([])
let seq = 0
const MAX_VISIBLE = 3
const TTL_MS = 4000

function dismiss(id: number): void {
  items = items.filter((t) => t.id !== id)
}

function push(kind: ToastItem['kind'], text: string): void {
  const id = ++seq
  items = [...items, { id, kind, text }].slice(-MAX_VISIBLE)
  setTimeout(() => dismiss(id), TTL_MS)
}

export const toast = {
  success(text: string): void { push('success', text) },
  error(text: string): void { push('error', text) },
  dismiss,
  get items(): ToastItem[] { return items },
}
