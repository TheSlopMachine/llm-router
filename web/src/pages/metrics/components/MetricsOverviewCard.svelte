<script lang="ts">
  import { SectionCard, HStack, VStack, Text, Spacer, Icon } from '$ui'
  import { t } from '$lib/i18n.svelte'

  let { title, value, loading = false, icon = 'show_chart' } = $props<{
    title: string
    value: number | null
    loading?: boolean
    icon?: string
  }>()
</script>

<SectionCard {title}>
  {#snippet badge()}
    <Icon name={icon} />
  {/snippet}
  <VStack align="center" justify="center" style="min-height: 100px;">
    {#if loading}
      <Text tone="soft" size="sm">{t('common.state.loading')}</Text>
    {:else if value === null || value === 0}
      <Text tone="soft" size="sm">{t('common.empty.no_data')}</Text>
    {:else}
      <Text size="2xl" weight="bold">{value.toLocaleString()}</Text>
    {/if}
  </VStack>
</SectionCard>
