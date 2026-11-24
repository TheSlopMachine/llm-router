import { setTheme, type ThemePreference } from '@fuckyouryan/fui'
import { Persisted, oneOf } from './persisted.svelte'
// Colour scheme: picked in Settings, kept in this browser. FUI follows the system scheme for auto.

export type Theme = ThemePreference

export const theme = new Persisted<Theme>('theme', 'auto', { parse: oneOf(['auto', 'light', 'dark'] as const), apply: setTheme })
