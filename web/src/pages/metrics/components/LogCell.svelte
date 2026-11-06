<script lang="ts" module>
  export type LogColumn = 'level' | 'time' | 'message' | 'attributes'
</script>

<script lang="ts">
  import { HStack, Text, ZStack, tintColor } from '@fuckyouryan/fui'
  import { severityName, severityTint } from '$lib/events'
  import type { EventEntry } from '$lib/types'

  let { column, entry, formatTime }: {
    column: LogColumn
    entry: EventEntry
    formatTime: (timestamp: string) => string
  } = $props()

  const summary = $derived(
    entry.keys.map((pair) => (pair.value === null ? pair.key : `${pair.key}=${pair.value}`)).join(' '),
  )
</script>

{#if column === 'level'}
  <ZStack
    width="var(--fui-ctl-tile-sm)"
    height="var(--fui-ctl-tile-sm)"
    radius="xs"
    role="img"
    aria-label={severityName(entry.severity)}
    title={severityName(entry.severity)}
    style="background: {tintColor(severityTint(entry.severity))}"
  />
{:else if column === 'time'}
  <HStack width="full" minWidth={0}>
    <Text size="xs" mono foregroundStyle="secondary" truncate>{formatTime(entry.timestamp)}</Text>
  </HStack>
{:else if column === 'message'}
  <HStack width="full" minWidth={0}>
    <Text size="sm" truncate>{entry.message}</Text>
  </HStack>
{:else}
  <HStack width="full" minWidth={0}>
    <Text size="xs" mono foregroundStyle="tertiary" truncate title={summary}>{summary}</Text>
  </HStack>
{/if}
