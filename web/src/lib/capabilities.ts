import type { TranslationKey } from './i18n/types'

export const CAPABILITY_META: Record<string, { label: TranslationKey; color: string; tint: string; icon: string; hint: TranslationKey }> = {
  tools: { label: 'models.capabilities.tools', color: 'chip-blue', tint: '#3b82f6', icon: 'build', hint: 'models.capabilities.tools_desc' },
  vision: { label: 'models.capabilities.vision', color: 'chip-purple', tint: '#8b5cf6', icon: 'image', hint: 'models.capabilities.vision_desc' },
  audio: { label: 'models.capabilities.audio', color: 'chip-teal', tint: '#14b8b8', icon: 'graphic_eq', hint: 'models.capabilities.audio_desc' },
  json_mode: { label: 'models.capabilities.json_label', color: 'chip-yellow', tint: '#eab308', icon: 'data_object', hint: 'models.capabilities.json_mode' },
  structured_outputs: { label: 'models.capabilities.structured', color: 'chip-yellow', tint: '#f59e0b', icon: 'schema', hint: 'models.capabilities.structured_outputs' },
  reasoning: { label: 'models.capabilities.reasoning', color: 'chip-orange', tint: '#f97316', icon: 'psychology', hint: 'models.capabilities.reasoning_desc' },
}

export function hasModalities(m: {
  inputModalities?: string[]
  outputModalities?: string[]
  input_modalities?: string[]
  output_modalities?: string[]
}): boolean {
  return (
    (m.inputModalities?.length ?? m.input_modalities?.length ?? 0) > 0 ||
    (m.outputModalities?.length ?? m.output_modalities?.length ?? 0) > 0
  )
}

export function hasCapabilities(m: { capabilities?: string[]; custom?: boolean }): boolean {
  return (m.capabilities?.length ?? 0) > 0 || !!m.custom
}
