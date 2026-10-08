// Button variant resolution. Single owner for the nested ternaries in
// Button.svelte so style derivation stays testable outside the template.
export type ButtonEffStyle = 'prominent' | 'soft' | 'text' | 'none'
export type ButtonVariant = 'prominent' | 'text' | 'soft' | 'ghost' | 'neutral'

export function resolveSelectedStyle(
  selected: boolean | undefined,
  style: ButtonEffStyle
): ButtonEffStyle {
  if (selected === true) return 'prominent'
  if (selected === false) return 'soft'
  return style
}

export function resolveButtonVariant(effStyle: ButtonEffStyle, iconMode: boolean): ButtonVariant {
  if (effStyle === 'prominent') return 'prominent'
  if (effStyle === 'text') return 'text'
  if (effStyle === 'soft') return 'soft'
  if (iconMode) return 'ghost'
  return 'neutral'
}

export function resolveInputType(
  isSecret: boolean,
  revealed: boolean,
  type: string
): string {
  if (isSecret) return revealed ? 'text' : 'password'
  return type
}

export function isSecretInput(inputType: string | undefined): boolean {
  return inputType === 'password' || inputType === 'secret'
}

export function hasButtonIcon(icon: { name?: string; src?: string } | undefined): boolean {
  return icon?.name != null || icon?.src != null
}

export function isIconOnly(
  icon: { name?: string; src?: string } | undefined,
  text: string,
  hasChildren: boolean
): boolean {
  return hasButtonIcon(icon) && !text && !hasChildren
}

export function hasLeftIcon(icon: { placement?: string } | undefined): boolean {
  return !!icon && (icon.placement ?? 'left') === 'left'
}

export function hasRightIcon(icon: { placement?: string } | undefined): boolean {
  return !!icon && icon.placement === 'right'
}
