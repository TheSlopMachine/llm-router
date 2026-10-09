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

export function resolveMenuWidth(enabled: boolean): string {
  if (!enabled) return ''
  return `min-width: var(--fui-toast-offset);`
}

export function shouldDismissOnKey(e: KeyboardEvent, opts: { closeOnEsc: boolean; open: boolean }): boolean {
  return !!opts.closeOnEsc && e.key === 'Escape' && opts.open
}
