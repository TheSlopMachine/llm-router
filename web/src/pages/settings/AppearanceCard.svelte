<script lang="ts">
  // Per-browser look: language, theme, accent, widget shape. Every choice applies and persists at once.
  import { Card, ColorPicker, Field, Picker } from '@fuckyouryan/fui'
  import { t } from '$lib/i18n.svelte'
  import { language, type Language } from '$lib/language.svelte'
  import { theme, type Theme } from '$lib/theme.svelte'
  import { accent, accentOf, accents } from '$lib/accent.svelte'
  import { shape, type Shape } from '$lib/shape.svelte'
  import { surfaceLevel, surfaceLevels, type SurfaceLevel } from '$lib/surface.svelte'

  const levelLabels = { [-1]: 'settings.surface.darker', 0: 'settings.surface.normal', 1: 'settings.surface.lighter' } as const
  const levelOptions = $derived(surfaceLevels.map((level) => ({ value: String(level), label: t(levelLabels[level]) })))

  const languageOptions = $derived([
    { value: 'auto', label: t('common.auto') },
    { value: 'en', label: t('settings.language.english') },
    { value: 'ru', label: t('settings.language.russian') },
  ])
  const themeOptions = $derived([
    { value: 'auto', label: t('common.auto') },
    { value: 'light', label: t('settings.theme.light') },
    { value: 'dark', label: t('settings.theme.dark') },
  ])
  const shapeOptions = $derived([
    { value: 'continuous', label: t('settings.shape.continuous') },
    { value: 'rounded', label: t('settings.shape.rounded') },
    { value: 'capsule', label: t('settings.shape.capsule') },
    { value: 'square', label: t('settings.shape.square') },
  ])
</script>

<Card title={t('settings.preferences')}>
  <Field label={t('settings.language.label')}>
    <Picker value={language.value} options={languageOptions} onchange={(value) => (language.value = value as Language)} />
  </Field>

  <Field label={t('settings.theme.label')}>
    <Picker pickerStyle="segmented" value={theme.value} options={themeOptions} onchange={(value) => (theme.value = value as Theme)} />
  </Field>

  <Field label={t('settings.accent')}>
    <ColorPicker
      label={t('settings.accent')}
      colors={accents.map((preset) => preset.bg)}
      value={accent.value.bg}
      onchange={(bg) => (accent.value = accentOf(bg))}
    />
  </Field>

  <Field label={t('settings.shape.label')}>
    <Picker pickerStyle="segmented" value={shape.value} options={shapeOptions} onchange={(value) => (shape.value = value as Shape)} />
  </Field>

  <Field label={t('settings.surface.label')} hint={t('settings.surface.hint')}>
    <Picker pickerStyle="segmented" value={String(surfaceLevel.value)} options={levelOptions} onchange={(value) => (surfaceLevel.value = Number(value) as SurfaceLevel)} />
  </Field>
</Card>
