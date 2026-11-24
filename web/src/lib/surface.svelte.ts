import { Persisted, oneOf } from './persisted.svelte'
// App surface level: picked in Settings, kept in this browser, fed to the Scaffold `level` (the
// sidebar stands there, the content column and modals one above). Higher is lighter in the dark theme.

// Shown to people as "background brightness": darker / normal / lighter.
export const surfaceLevels = [-1, 0, 1] as const
export type SurfaceLevel = (typeof surfaceLevels)[number]

export const surfaceLevel = new Persisted<SurfaceLevel>('surface-level', 0, { parse: oneOf(surfaceLevels) })
