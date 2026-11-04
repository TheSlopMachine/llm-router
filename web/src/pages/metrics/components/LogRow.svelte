<script lang="ts">
  import { Chip, HStack, Text } from '@fuckyouryan/fui'
  import { severityLabel, severityTint } from '$lib/events'
  import { t } from '$lib/i18n.svelte'
  import type { EventEntry } from '$lib/types'

  let { entry, formatTime }: {
    entry: EventEntry
    formatTime: (timestamp: string) => string
  } = $props()

  const summary = $derived(
    entry.keys.map((pair) => (pair.value === null ? pair.key : `${pair.key}=${pair.value}`)).join(' '),
  )
</script>

<HStack valign="center" gap={3} padX={3} height="full">
  <HStack width={88} valign="center">
    <Chip size="sm" tint={severityTint(entry.severity)} text={t(severityLabel(entry.severity))} />
  </HStack>
  <Text size="xs" mono foregroundStyle="secondary">{formatTime(entry.timestamp)}</Text>
  <HStack grow minWidth={0}>
    <Text size="sm" truncate>{entry.message}</Text>
  </HStack>
  <HStack maxWidth="45%" minWidth={0}>
    <Text size="xs" mono foregroundStyle="tertiary" truncate>{summary}</Text>
  </HStack>
</HStack>
