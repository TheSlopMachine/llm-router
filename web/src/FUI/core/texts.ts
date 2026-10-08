// Texts the library needs but must not own. The site registers one provider
// (called lazily, so a reactive t() keeps working); defaults are empty strings.
export interface FuiTexts {
  close: string
  confirm: string
  cancel: string
  searchPlaceholder: string
  noOptions: string
  copy: string
}
let provider: () => Partial<FuiTexts> = () => ({})
export function setFuiTexts(p: () => Partial<FuiTexts>): void {
  provider = p
}
export function fuiText(key: keyof FuiTexts): string {
  return provider()[key] ?? ''
}
