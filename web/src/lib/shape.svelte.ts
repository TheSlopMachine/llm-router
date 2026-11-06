import { setShape, type Shape } from '@fuckyouryan/fui'
// Widget shape: picked in Settings, persisted per browser, applied app-wide
// through FUI setShape (data-fui-shape on <html>).

export type { Shape }

export const shapes: Shape[] = ['continuous', 'rounded', 'capsule', 'square']

const STORAGE_KEY = 'shape'

function getInitialShape(): Shape {
  if (typeof window === 'undefined') return 'continuous'
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    const found = shapes.find((s) => s === stored)
    if (found) return found
  } catch (e) {
    console.error('Failed to access localStorage for shape:', e)
  }
  return 'continuous'
}

let current = $state<Shape>(getInitialShape())

export const shape: {
  get value(): Shape
  set value(next: Shape)
} = {
  get value(): Shape {
    return current
  },
  set value(next: Shape) {
    current = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch (e) {
      console.error('Failed to save shape to localStorage:', e)
    }
    setShape(next)
  },
}

if (typeof window !== 'undefined') {
  setShape(current)
}
