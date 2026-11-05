<script lang="ts">
  import { Chip, HStack, Text, VStack } from '@fuckyouryan/fui'
  import { keyLabel } from '$lib/event-labels'
  import { locale } from '$lib/i18n.svelte'
  import type { FacetGroup } from '$lib/events'

  let { groups, onfilter }: {
    groups: FacetGroup[]
    onfilter: (key: string, value: string) => void
  } = $props()

  const counter = $derived(new Intl.NumberFormat(locale()))
</script>

<VStack gap={3}>
  {#each groups as group (group.key)}
    <HStack gap={3} valign="start">
      <HStack width={112} minWidth={0}>
        <Text size="sm" foregroundStyle="secondary" truncate title={group.key}>{keyLabel(group.key)}</Text>
      </HStack>
      <HStack grow gap={2} wrap>
        {#each group.values as facet (facet.value)}
          <Chip
            size="sm"
            tint="neutral"
            text={`${facet.value} · ${counter.format(facet.count)}`}
            onclick={() => onfilter(group.key, facet.value)}
          />
        {/each}
      </HStack>
    </HStack>
  {/each}
</VStack>
