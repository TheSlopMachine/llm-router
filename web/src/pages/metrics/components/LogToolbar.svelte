<script lang="ts">
  import { Button, Field, HStack, Picker, SearchField, TextField, VStack } from '@fuckyouryan/fui'
  import { SEVERITY_LEVELS, TIME_RANGES } from '$lib/events'
  import { t } from '$lib/i18n.svelte'

  const CUSTOM = 'custom'

  let {
    part = 'full',
    filterCount = 0,
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
    onfilters,
  }: {
    /** full: everything (wide screens); bar: live, search and the filters button (phones);
     *  settings: range, severity and refresh (the phone filters modal) */
    part?: 'full' | 'bar' | 'settings'
    /** shown on the filters button of the bar */
    filterCount?: number
    onfilters?: () => void
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

{#snippet liveButton(iconOnly: boolean)}
  <Button
    buttonStyle={live && liveAvailable ? 'prominent' : 'neutral'}
    icon={{ name: live && liveAvailable ? 'sensors' : 'pause' }}
    text={iconOnly ? undefined : t('metrics.logs.live.action')}
    title={iconOnly ? t('metrics.logs.live.action') : undefined}
    ariaLabel={iconOnly ? t('metrics.logs.live.action') : undefined}
    disabled={!liveAvailable}
    onclick={() => onlive(!live)}
  />
{/snippet}

{#if part === 'bar'}
  <HStack gap={2} valign="center">
    {@render liveButton(true)}
    <SearchField bind:value={search} placeholder={t('metrics.logs.search.placeholder')} fill="sm" />
    <Button
      buttonStyle={filterCount > 0 ? 'prominent' : 'neutral'}
      icon={{ name: 'filter_list' }}
      text={filterCount > 0 ? `${filterCount}` : undefined}
      title={t('metrics.logs.filters.action')}
      ariaLabel={t('metrics.logs.filters.action')}
      onclick={() => onfilters?.()}
    />
  </HStack>
{:else}
<VStack gap={3}>
  <HStack gap={2} valign="center" wrap>
    {#if part === 'full'}{@render liveButton(false)}{/if}
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

  {#if part === 'full'}
    <SearchField bind:value={search} placeholder={t('metrics.logs.search.placeholder')} />
  {/if}
</VStack>
{/if}
