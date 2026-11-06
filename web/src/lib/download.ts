// File download/upload helpers. Single owner for the Blob anchor dance and the
// hidden file input.
export function downloadBlob(filename: string, blob: Blob): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function downloadJson(filename: string, data: unknown): void {
  downloadBlob(filename, new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }))
}

/** Opens the system file picker for one file; resolves with it, or null when nothing was picked. */
export function pickFile(accept: string): Promise<File | null> {
  return new Promise((resolve) => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = accept
    input.onchange = () => resolve(input.files?.[0] ?? null)
    input.oncancel = () => resolve(null)
    input.click()
  })
}

/** Opens the system file picker for one .json file; resolves with its parsed content, or null when nothing was picked. */
export async function pickJsonFile(): Promise<unknown | null> {
  const file = await pickFile('.json,application/json')
  return file ? readJsonFile(file) : null
}

export function readJsonFile(file: File): Promise<unknown> {
  return file.text().then((text) => JSON.parse(text) as unknown)
}
