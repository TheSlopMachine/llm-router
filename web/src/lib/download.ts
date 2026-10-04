// JSON file download/upload helpers. Single owner for the Blob anchor dance
// duplicated in SettingsPage export/import paths.
export function downloadJson(filename: string, data: unknown): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

export function readJsonFile(file: File): Promise<unknown> {
  return file.text().then((text) => JSON.parse(text) as unknown)
}
