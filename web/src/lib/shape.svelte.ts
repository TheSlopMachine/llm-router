import { setShape, type Shape } from '@fuckyouryan/fui'
import { Persisted, oneOf } from './persisted.svelte'
// Widget shape: picked in Settings, kept in this browser, applied app-wide through FUI setShape
// (data-fui-shape on <html>).

export type { Shape }

export const shapes: Shape[] = ['continuous', 'rounded', 'capsule', 'square']

export const shape = new Persisted<Shape>('shape', 'continuous', { parse: oneOf(shapes), apply: setShape })
