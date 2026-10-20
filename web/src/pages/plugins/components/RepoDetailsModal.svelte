<script lang="ts">
  import { VStack, HStack, Text, Chip } from '@fuckyouryan/fui'
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
    <Text size="md">{description}</Text>
  {/if}

  <VStack gap={1}>
    <Text size="xs" weight="medium" foregroundStyle="secondary">{t('plugins.repo.url')}</Text>
    <Text size="xs" foregroundStyle="secondary" mono break>{url}</Text>
  </VStack>

  <HStack valign="center" gap={3}>
    {#if builtin}<Chip text={t('plugins.repo.builtin')} tint="neutral" size="sm" />{/if}
    <Text size="sm" foregroundStyle="secondary">{n(fileCount, 'units.plugin.one', 'units.plugin.many')}</Text>
  </HStack>
</VStack>
