<script lang="ts">
  // Body of the event detail modal. Rendered by the shared Modal shell (title, close button).
  import { Button, Chip, CopyButton, Card, HStack, Spacer, Text, VStack } from '@fuckyouryan/fui'
  import { keyLabel } from '$lib/event-labels'
  import { severityLabel, severityTint } from '$lib/events'
  import { locale, t } from '$lib/i18n.svelte'
  import type { EventEntry } from '$lib/types'

  let { entry, onfilter, onexclude, onaround, closeModal }: {
    entry: EventEntry
    onfilter: (key: string, value: string) => void
    onexclude: (key: string, value: string) => void
    onaround: (entry: EventEntry) => void
    closeModal: () => void
  } = $props()

  const moment = $derived(
    new Intl.DateTimeFormat(locale(), { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(entry.timestamp)),
  )

  function apply(action: () => void): void {
    action()
    closeModal()
  }
</script>

<VStack gap={4}>
  <HStack valign="center" gap={3}>
    <Chip tint={severityTint(entry.severity)} text={t(severityLabel(entry.severity))} />
    <Text size="sm" mono foregroundStyle="secondary">{moment}</Text>
    <Spacer />
    <CopyButton text={JSON.stringify(entry, null, 2)} />
  </HStack>

  <Text break>{entry.message}</Text>

  {#if entry.keys.length > 0}
    <Card title={t('metrics.logs.detail_keys.title')}>
      <VStack gap={2}>
        {#each entry.keys as pair}
          <HStack valign="center" gap={3}>
            <HStack width={120} minWidth={0}>
              <Text size="sm" foregroundStyle="secondary" truncate title={pair.key}>{keyLabel(pair.key)}</Text>
            </HStack>
            <HStack grow minWidth={0}>
              <Text size="sm" mono break>{pair.value ?? '—'}</Text>
            </HStack>
            <Button
              buttonStyle="text"
              size="sm"
              icon={{ name: 'filter_list' }}
              title={t('metrics.logs.detail_filter.action')}
              ariaLabel={t('metrics.logs.detail_filter.action')}
              onclick={() => apply(() => onfilter(pair.key, pair.value ?? ''))}
            />
            <Button
              buttonStyle="text"
              size="sm"
              icon={{ name: 'block' }}
              title={t('metrics.logs.detail_exclude.action')}
              ariaLabel={t('metrics.logs.detail_exclude.action')}
              onclick={() => apply(() => onexclude(pair.key, pair.value ?? ''))}
            />
          </HStack>
        {/each}
      </VStack>
    </Card>
  {/if}

  <HStack>
    <Button
      buttonStyle="neutral"
      icon={{ name: 'history' }}
      text={t('metrics.logs.detail_around.action')}
      onclick={() => apply(() => onaround(entry))}
    />
  </HStack>
</VStack>
