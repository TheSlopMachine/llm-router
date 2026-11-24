import { Persisted, oneOf } from './persisted.svelte'

export type Language = 'auto' | 'en' | 'ru'

export const language = new Persisted<Language>('language', 'auto', { parse: oneOf(['auto', 'en', 'ru'] as const) })
