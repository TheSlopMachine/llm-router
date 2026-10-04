export const CAPABILITY_META: Record<string, { label: string; color: string; tint: string; icon: string; hint: string }> = {
  tools: { label: 'Tools', color: 'chip-blue', tint: '#3b82f6', icon: 'build', hint: 'Tool calling — the model can invoke functions' },
  vision: { label: 'Vision', color: 'chip-purple', tint: '#8b5cf6', icon: 'image', hint: 'Vision — accepts image input' },
  audio: { label: 'Audio', color: 'chip-teal', tint: '#14b8b8', icon: 'graphic_eq', hint: 'Audio — accepts audio input' },
  json_mode: { label: 'JSON', color: 'chip-yellow', tint: '#eab308', icon: 'data_object', hint: 'JSON mode — response_format: json_object' },
  structured_outputs: { label: 'Structured', color: 'chip-yellow', tint: '#f59e0b', icon: 'schema', hint: 'Structured outputs — responses follow a JSON schema' },
  reasoning: { label: 'Reasoning', color: 'chip-orange', tint: '#f97316', icon: 'psychology', hint: 'Reasoning — thinks before answering (reasoning_content)' },
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
