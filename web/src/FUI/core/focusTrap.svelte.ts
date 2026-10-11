// focusTrap: Tab по кругу внутри узла, возврат фокуса. use:focusTrap={active}.
export function focusTrap(node: HTMLElement, active = true) {
  let prev: Element | null = null
  function key(e: KeyboardEvent) {
    if (e.key !== 'Tab' || !active) return
    const elements = [...node.querySelectorAll<HTMLElement>('button,[href],input,select,textarea,[tabindex]:not([tabindex="-1"])')].filter((element) => !element.hasAttribute('disabled'))
    if (!elements.length) return
    const first = elements[0]; const last = elements[elements.length - 1]
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
  }
  node.addEventListener('keydown', key)
  return { update(v: boolean) { if (v) { prev = document.activeElement } else if (prev instanceof HTMLElement) prev.focus(); active = v }, destroy() { node.removeEventListener('keydown', key) } }
}
