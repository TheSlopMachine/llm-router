// Site-side predicates for backend-provided UI schemas and table state.
// They validate JSON coming from the Go backend, so they belong to the site,
// not to the widget library.

export function isStepValue(v: unknown): v is number {
  return typeof v === 'number' && Number.isInteger(v) && v >= 0 && v <= 8
}

export function isBannerVariant(v: unknown): v is 'info' | 'warning' | 'error' | 'success' {
  return v === 'info' || v === 'warning' || v === 'error' || v === 'success'
}

export function isButtonVariant(v: unknown): v is 'primary' | 'secondary' | 'danger' {
  return v === 'primary' || v === 'secondary' || v === 'danger'
}

export function hasUiNodes(schema: { nodes?: unknown[] | null; enabled?: boolean } | undefined | null): boolean {
  if (!schema) return false
  if (schema.enabled) return true
  return (schema.nodes?.length ?? 0) > 0
}

/** Backend `input_type` values that must render as a masked field. */
export function isSecretInput(inputType: string | undefined): boolean {
  return inputType === 'password' || inputType === 'secret'
}

export function isSortActive(
  sortable: boolean,
  key: string | null | undefined,
  dir: 'asc' | 'desc' | null | undefined
): boolean {
  return !!sortable && !!key && !!dir
}
