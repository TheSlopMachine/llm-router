<script lang="ts">
  import { Button, Field, HStack, Picker, SearchField, TextField, VStack } from '@fuckyouryan/fui'
  import { SEVERITY_LEVELS, TIME_RANGES } from '$lib/events'
  import { t } from '$lib/i18n.svelte'

  const CUSTOM = 'custom'

  let {
    live,
    liveAvailable,
    severity,
    rangeId,
    customStart = $bindable(''),
    customEnd = $bindable(''),
    search = $bindable(''),
    loading,
    onlive,
    onseverity,
    onrange,
    oncustomrange,
    onrefresh,
  }: {
    live: boolean
    /** false while the range has a fixed end: nothing new can arrive */
    liveAvailable: boolean
    severity: number
    rangeId: string
    customStart?: string
    customEnd?: string
    search?: string
    loading: boolean
    onlive: (next: boolean) => void
    onseverity: (next: number) => void
    onrange: (next: string) => void
    oncustomrange: () => void
    onrefresh: () => void
  } = $props()

  const rangeOptions = $derived([
    ...TIME_RANGES.map((range) => ({ value: range.id, label: t(range.label) })),
    { value: CUSTOM, label: t('metrics.logs.range_custom.label') },
  ])

  const severityOptions = $derived(
    SEVERITY_LEVELS.map((entry) => ({ value: String(entry.level), label: t(entry.label) })),
  )
</script>

<VStack gap={3}>
  <HStack gap={2} valign="center" wrap>
    <Button
      buttonStyle={live && liveAvailable ? 'prominent' : 'neutral'}
      icon={{ name: live && liveAvailable ? 'sensors' : 'pause' }}
      text={t('metrics.logs.live.action')}
      disabled={!liveAvailable}
      onclick={() => onlive(!live)}
    />
    <Picker
      value={rangeId}
      options={rangeOptions}
      onchange={onrange}
      ariaLabel={t('metrics.time_range')}
      fill="sm"
    />
    <Picker
      pickerStyle="segmented"
      value={String(severity)}
      options={severityOptions}
      onchange={(next) => onseverity(Number(next))}
      ariaLabel={t('metrics.logs.severity.label')}
    />
    <Button
      buttonStyle="neutral"
      icon={{ name: 'refresh' }}
      title={t('common.actions.refresh')}
      ariaLabel={t('common.actions.refresh')}
      disabled={loading}
      onclick={onrefresh}
    />
  </HStack>

  {#if rangeId === CUSTOM}
    <HStack gap={3} wrap>
      <Field label={t('metrics.logs.range_from.label')}>
        <TextField type="datetime-local" bind:value={customStart} onchange={() => oncustomrange()} />
      </Field>
      <Field label={t('metrics.logs.range_to.label')}>
        <TextField type="datetime-local" bind:value={customEnd} onchange={() => oncustomrange()} />
      </Field>
    </HStack>
  {/if}

  <SearchField bind:value={search} placeholder={t('metrics.logs.search.placeholder')} />

</VStack>
