<script lang="ts">
  import { Button, Chip, HStack, Picker, TextField, VStack } from '@fuckyouryan/fui'
  import { keyLabel } from '$lib/event-labels'
  import { withFilter, withoutFilter } from '$lib/events'
  import { t } from '$lib/i18n.svelte'
  import type { EventKeyFilter } from '$lib/types'

  let { part = 'all', filters, keys, valuesFor, onchange }: {
    /** all: builder row and active chips; chips: the chips only (the phone page, the row lives in the filters modal) */
    part?: 'all' | 'chips'
    filters: EventKeyFilter[]
    keys: string[]
    /** values the key takes in the loaded events; those events already obey the applied filters */
    valuesFor: (key: string) => string[]
    onchange: (next: EventKeyFilter[]) => void
  } = $props()

  let key = $state('')
  let mode = $state('include')
  let value = $state('')

  const values = $derived(key === '' ? [] : valuesFor(key))

  function setKey(next: string): void {
    key = next
    value = ''
  }

  function add(): void {
    if (key === '') return
    onchange(withFilter(filters, { key, value: value.trim(), exclude: mode === 'exclude' }))
    value = ''
  }

  function reset(): void {
    key = ''
    value = ''
  }

  function describe(filter: EventKeyFilter): string {
    return filter.value === '' ? keyLabel(filter.key) : `${keyLabel(filter.key)}: ${filter.value}`
  }
</script>

<VStack gap={3}>
  {#if part !== 'chips'}
  <HStack gap={2} valign="center" wrap>
    <Picker
      value={key}
      onchange={setKey}
      options={keys.map((candidate) => ({ value: candidate, label: keyLabel(candidate) }))}
      placeholder={t('metrics.logs.filter_key.label')}
      ariaLabel={t('metrics.logs.filter_key.label')}
      searchable
      fill="sm"
    />
    <Picker
      pickerStyle="segmented"
      bind:value={mode}
      options={[
        { value: 'include', label: t('metrics.logs.filter_include.label') },
        { value: 'exclude', label: t('metrics.logs.filter_exclude.label') },
      ]}
      ariaLabel={t('metrics.logs.filter_include.label')}
    />
    {#if values.length > 0}
      <Picker
        bind:value
        options={[
          { value: '', label: t('metrics.logs.filter_any.label') },
          ...values.map((candidate) => ({ value: candidate, label: candidate })),
        ]}
        ariaLabel={t('metrics.logs.filter_value.label')}
        searchable
        fill="md"
      />
    {:else}
      <TextField
        bind:value
        placeholder={t('metrics.logs.filter_value.placeholder')}
        ariaLabel={t('metrics.logs.filter_value.placeholder')}
        fill="md"
      />
    {/if}
    <Button
      buttonStyle="neutral"
      icon={{ name: 'add' }}
      text={t('metrics.logs.filter_add.action')}
      disabled={key === ''}
      onclick={add}
    />
    {#if key !== '' || value !== ''}
      <Button
        buttonStyle="text"
        icon={{ name: 'close' }}
        title={t('metrics.logs.filter_reset.action')}
        ariaLabel={t('metrics.logs.filter_reset.action')}
        onclick={reset}
      />
    {/if}
  </HStack>
  {/if}

  {#if filters.length > 0}
    <HStack gap={2} valign="center" wrap>
      {#each filters as filter (`${filter.exclude ? '!' : ''}${filter.key}`)}
        <Chip
          text={describe(filter)}
          icon={filter.exclude ? 'block' : 'filter_list'}
          tint={filter.exclude ? 'danger' : 'accent'}
          removeLabel={t('common.actions.remove')}
          onremove={() => onchange(withoutFilter(filters, filter))}
        />
      {/each}
      <Button
        buttonStyle="text"
        size="sm"
        text={t('metrics.logs.filters_clear.action')}
        onclick={() => onchange([])}
      />
    </HStack>
  {/if}
</VStack>
