// size: action use:size + SizeReader. Отдаёт {width,height} через колбэк.
export interface SizeInfo { width: number; height: number }
export function size(node: HTMLElement, onsize?: (s: SizeInfo) => void) {
  if (!onsize) return {}
  const ro = new ResizeObserver(() => onsize({ width: node.clientWidth, height: node.clientHeight }))
  ro.observe(node)
  onsize({ width: node.clientWidth, height: node.clientHeight })
  return { destroy() { ro.disconnect() } }
}
export class SizeReader {
  width = $state(0); height = $state(0)
  attach(node: HTMLElement) {
    const ro = new ResizeObserver(() => { this.width = node.clientWidth; this.height = node.clientHeight })
    ro.observe(node)
    this.width = node.clientWidth; this.height = node.clientHeight
    return () => ro.disconnect()
  }
}
