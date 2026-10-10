// focusTrap: Tab по кругу внутри узла, возврат фокуса. use:focusTrap={active}.
export function focusTrap(node: HTMLElement, active = true) {
  let prev: Element | null = null
  function key(e: KeyboardEvent) {
    if (e.key !== 'Tab' || !active) return
    const els = [...node.querySelectorAll<HTMLElement>('button,[href],input,select,textarea,[tabindex]:not([tabindex="-1"])')].filter((el) => !el.hasAttribute('disabled'))
    if (!els.length) return
    const first = els[0]; const last = els[els.length - 1]
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
  }
  node.addEventListener('keydown', key)
  return { update(v: boolean) { if (v) { prev = document.activeElement } else if (prev instanceof HTMLElement) prev.focus(); active = v }, destroy() { node.removeEventListener('keydown', key) } }
}
