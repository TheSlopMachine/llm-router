// Shared UI predicates. Single owner for compound conditions repeated
// across templates so markup reads a named boolean, not a 3-operand chain.
export function isStepValue(v: unknown): v is number {
  return typeof v === 'number' && Number.isInteger(v) && v >= 0 && v <= 8
}

export function isBannerVariant(v: unknown): v is 'info' | 'warning' | 'error' | 'success' {
  return v === 'info' || v === 'warning' || v === 'error' || v === 'success'
}

export function isButtonVariant(v: unknown): v is 'primary' | 'secondary' | 'danger' {
  return v === 'primary' || v === 'secondary' || v === 'danger'
}

export function isTextButtonVariant(v: unknown): boolean {
  return v === 'text' || v === 'text danger' || v === 'text plain'
}

export function hasIcon(icon: { name?: string; src?: string } | undefined): boolean {
  return icon?.name != null || icon?.src != null
}

export function isIconOnly(
  icon: { name?: string; src?: string } | undefined,
  text: string,
  hasChildren: boolean
): boolean {
  return hasIcon(icon) && !text && !hasChildren
}

export function hasLeftIcon(icon: { placement?: string } | undefined): boolean {
  return !!icon && (icon.placement ?? 'left') === 'left'
}

export function hasRightIcon(icon: { placement?: string } | undefined): boolean {
  return !!icon && icon.placement === 'right'
}

export function hasFooterButtons(buttons: unknown): boolean {
  return Array.isArray(buttons) && buttons.length > 0
}

export function hasUiNodes(schema: { nodes?: unknown[] | null; enabled?: boolean } | undefined | null): boolean {
  if (!schema) return false
  if (schema.enabled) return true
  return (schema.nodes?.length ?? 0) > 0
}
