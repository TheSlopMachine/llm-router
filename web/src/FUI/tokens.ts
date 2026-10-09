// Shared prop -> token maps for the layout primitives.
//
// Every primitive takes scale *steps*, never pixels: gap={3}, pad={5}. The
// step resolves to a CSS custom property, so a literal like 13px or 5px
// cannot enter the system through a component prop at all.

export type Step = 0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8
export type Align = 'start' | 'center' | 'end' | 'stretch' | 'baseline'
export type Justify = 'start' | 'center' | 'end' | 'between' | 'around' | 'evenly'
export type Size = 'small' | 'medium' | 'large'

export const ALIGN: Record<Align, string> = {
  start: 'flex-start',
  center: 'center',
  end: 'flex-end',
  stretch: 'stretch',
  baseline: 'baseline',
}

export const JUSTIFY: Record<Justify, string> = {
  start: 'flex-start',
  center: 'center',
  end: 'flex-end',
  between: 'space-between',
  around: 'space-around',
  evenly: 'space-evenly',
}

/** Scale step -> spacing custom property. `undefined` means "do not set". */
export function space(step: Step | undefined): string | undefined {
  return step === undefined ? undefined : `var(--fui-space-${step})`
}

/** Control size -> height custom property. `undefined` means "do not set". */
export function controlHeight(size: Size | undefined): string | undefined {
  return size === undefined ? undefined : `var(--fui-ctl-${size})`
}

export type FillSize = boolean | 'sm' | 'md' | 'lg'

/** Growing flex item: true -> 'md', otherwise the named basis. `undefined` means "do not set". */
export function fillStyle(fill: FillSize | undefined): string | undefined {
  if (fill === undefined || fill === false) return undefined
  const size = fill === true ? 'md' : fill
  return `1 1 var(--fui-basis-${size})`
}
