// One tone vocabulary for every data widget (charts, meters, badges), so colour always means
// the same thing: success / warning / danger carry status, the rest are categorical hues.
export type Tone = 'accent' | 'success' | 'warning' | 'danger' | 'info' | 'purple' | 'teal' | 'orange' | 'neutral'

// Short keys used by older call sites keep working.
const ALIAS: Record<string, Tone> = {
  ok: 'success', g: 'success', err: 'danger', r: 'danger', warn: 'warning', y: 'warning',
  b: 'info', blue: 'info', p: 'purple', t: 'teal', o: 'orange', green: 'success', red: 'danger', yellow: 'warning',
}

function norm(t?: string): Tone {
  if (!t) return 'accent'
  return (ALIAS[t] ?? t) as Tone
}

const FILL: Record<Tone, string> = {
  accent: 'var(--fui-color-accent)',
  success: 'var(--fui-color-success-text)',
  warning: 'var(--fui-color-warning-text)',
  danger: 'var(--fui-color-error-text)',
  info: 'var(--fui-color-badge-blue-text)',
  purple: 'var(--fui-color-badge-purple-text)',
  teal: 'var(--fui-color-badge-teal-text)',
  orange: 'var(--fui-color-badge-orange-text)',
  neutral: 'var(--fui-color-text-soft)',
}

const SOFT: Record<Tone, string> = {
  accent: 'var(--fui-color-accent-soft)',
  success: 'var(--fui-color-badge-green-bg)',
  warning: 'var(--fui-color-badge-yellow-bg)',
  danger: 'var(--fui-color-badge-red-bg)',
  info: 'var(--fui-color-badge-blue-bg)',
  purple: 'var(--fui-color-badge-purple-bg)',
  teal: 'var(--fui-color-badge-teal-bg)',
  orange: 'var(--fui-color-badge-orange-bg)',
  neutral: 'var(--fui-elev)',
}

/** Solid colour for strokes, bars and dots. */
export function toneFill(t?: string): string {
  return FILL[norm(t)] ?? FILL.accent
}
/** Tinted background that sits under text of the same tone. */
export function toneSoft(t?: string): string {
  return SOFT[norm(t)] ?? SOFT.accent
}
