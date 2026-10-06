<script lang="ts">
  import { VStack, HStack, Text, Chip } from '$ui'
  import { t, n } from '$lib/i18n.svelte'

  let {
    title,
    description = '',
    url,
    builtin,
    fileCount
  } = $props<{
    title: string
    description?: string
    url: string
    builtin: boolean
    fileCount: number
  }>()
</script>

<VStack gap={4}>
  {#if description}
    <Text size="base">{description}</Text>
  {/if}

  <VStack gap={1}>
    <Text size="xs" weight="medium" tone="soft">{t('plugins.repo.url')}</Text>
    <Text size="xs" tone="soft" mono break>{url}</Text>
  </VStack>

  <HStack align="center" gap={3}>
    {#if builtin}<Chip text={t('plugins.repo.builtin')} color="chip-neutral" size="small" />{/if}
    <Text size="sm" tone="soft">{n(fileCount, 'units.plugin.one', 'units.plugin.many')}</Text>
  </HStack>
</VStack>
