// Step state + radius helpers for small ui controls.
export type StepState = 'done' | 'current' | 'todo'

export function resolveStepState(index: number, cur: number): StepState {
  if (index < cur) return 'done'
  if (index === cur) return 'current'
  return 'todo'
}

export function stepToRadiusToken(radius: number): 'xs' | 'sm' | 'md' | 'lg' {
  if (radius <= 1) return 'xs'
  if (radius === 2) return 'sm'
  if (radius === 3) return 'md'
  return 'lg'
}

export function resolveFloatingStyle(minWidth: number | undefined, maxWidth: number | undefined): string {
  const min = minWidth ? `min-width: ${minWidth}px;` : ''
  const max = maxWidth ? `max-width: min(${maxWidth}px, calc(100vw - 16px));` : ''
  return `${min} ${max}`.trim()
}

export function resolveMenuWidth(
  width: number | undefined,
  minWidth: number | undefined
): string {
  if (width !== undefined) return `width: ${width}px;`
  if (minWidth !== undefined) return `min-width: ${minWidth}px;`
  return ''
}

export function shouldDismissOnKey(e: KeyboardEvent, opts: { closeOnEsc: boolean; open: boolean }): boolean {
  return !!opts.closeOnEsc && e.key === 'Escape' && opts.open
}
