export const CAPABILITY_META: Record<string, { label: string; cls: string; icon: string; hint: string }> = {
  tools: { label: 'Tools', cls: 'chip-blue', icon: 'build', hint: 'Tool calling — the model can invoke functions' },
  vision: { label: 'Vision', cls: 'chip-purple', icon: 'image', hint: 'Vision — accepts image input' },
  audio: { label: 'Audio', cls: 'chip-teal', icon: 'graphic_eq', hint: 'Audio — accepts audio input' },
  json_mode: { label: 'JSON', cls: 'chip-yellow', icon: 'data_object', hint: 'JSON mode — response_format: json_object' },
  structured_outputs: { label: 'Structured', cls: 'chip-yellow', icon: 'schema', hint: 'Structured outputs — responses follow a JSON schema' },
  reasoning: { label: 'Reasoning', cls: 'chip-orange', icon: 'psychology', hint: 'Reasoning — thinks before answering (reasoning_content)' },
}
