// Pending virtual-model clone payload: set by the list clone action,
// consumed once by the editor on new-model mount.
export interface VirtualCloneData {
  name: string
  description: string
  instruction: string
  models: string[]
}

let pending: VirtualCloneData | null = null

export function setPendingClone(data: VirtualCloneData): void {
  pending = data
}

export function takePendingClone(): VirtualCloneData | null {
  const data = pending
  pending = null
  return data
}
