<script lang="ts">
  import { VStack, HStack, Text, Chip } from '$ui'
  import { t } from '$lib/i18n.svelte'

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
    <Text size="xs" weight="medium" tone="soft">{t('Repository URL')}</Text>
    <Text size="xs" tone="soft" mono break>{url}</Text>
  </VStack>

  <HStack align="center" gap={3}>
    {#if builtin}<Chip text={t('Built-in')} color="chip-neutral" size="small" />{/if}
    <Text size="sm" tone="soft">{fileCount} {t('plugins')}</Text>
  </HStack>
</VStack>
