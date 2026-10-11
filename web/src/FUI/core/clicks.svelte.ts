// clicks: actions clickOutside + hotkey. Никаких текстов, только обработчики.
export function clickOutside(node: HTMLElement, handler?: (e: PointerEvent) => void) {
  function ondown(e: PointerEvent) { if (!node.contains(e.target as Node)) handler?.(e) }
  document.addEventListener('pointerdown', ondown)
  return { update(h?: (e: PointerEvent) => void) { handler = h }, destroy() { document.removeEventListener('pointerdown', ondown) } }
}
export function hotkey(node: HTMLElement, options?: { combo?: string; handler?: (e: KeyboardEvent) => void }) {
  function onkey(e: KeyboardEvent) {
    const want = (options?.combo ?? '').toLowerCase()
    const mod = e.metaKey || e.ctrlKey
    const hit = want === 'mod+k' ? (mod && e.key.toLowerCase() === 'k') : node.contains(e.target as Node)
    if (hit) { e.preventDefault(); options?.handler?.(e) }
  }
  document.addEventListener('keydown', onkey)
  return { update(next?: typeof options) { options = next }, destroy() { document.removeEventListener('keydown', onkey) } }
}
