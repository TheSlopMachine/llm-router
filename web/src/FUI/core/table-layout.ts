// Table layout helpers. Single owner for breakpoint and alignment chains.
export const CARD_BELOW = 640
export function priorityCeiling(width: number): 1 | 2 | 3 {
  if (width < 640) return 1
  if (width < 1024) return 2
  return 3
}

export function alignClass(align: string | undefined): string {
  if (align === 'center') return 'align-c'
  if (align === 'right') return 'align-r'
  return 'align-l'
}

export function sortAria(
  sortOn: boolean,
  sortKey: string | null | undefined,
  colKey: string,
  sortDir: 'asc' | 'desc' | null | undefined
): 'ascending' | 'descending' | undefined {
  if (!sortOn || sortKey !== colKey || sortDir == null) return undefined
  return sortDir === 'asc' ? 'ascending' : 'descending'
}

export function isSortActive(
  sortable: boolean,
  key: string | null | undefined,
  dir: 'asc' | 'desc' | null | undefined
): boolean {
  return !!sortable && !!key && !!dir
}
