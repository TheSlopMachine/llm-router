<script lang="ts">
  import { GroupBox, HStack, VStack, Text, Spacer, Icon, type IconName } from '@fuckyouryan/fui'
  import { t } from '$lib/i18n.svelte'

  let { title, value, loading = false, icon = 'show_chart' }: {
    title: string
    value: number | null
    loading?: boolean
    icon?: IconName
  } = $props()
</script>

<GroupBox {title}>
  {#snippet badge()}
    <Icon name={icon} />
  {/snippet}
  <VStack halign="center" valign="center" style="min-height: var(--fui-metrics-card-min-h);">
    {#if loading}
      <Text foregroundStyle="secondary" size="sm">{t('common.state.loading')}</Text>
    {:else if value === null || value === 0}
      <Text foregroundStyle="secondary" size="sm">{t('common.empty.no_data')}</Text>
    {:else}
      <Text size={32} weight="bold">{value.toLocaleString()}</Text>
    {/if}
  </VStack>
</GroupBox>
