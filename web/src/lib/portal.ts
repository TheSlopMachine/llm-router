// portal moves a node to document.body for the duration of its lifetime,
// so floating content escapes overflow:hidden ancestors. Returns the node
// to its original place is unnecessary: Svelte removes it on unmount.
export function portal(node: HTMLElement): { destroy: () => void } {
  document.body.appendChild(node)
  return {
    destroy(): void {
      node.remove()
    }
  }
}
