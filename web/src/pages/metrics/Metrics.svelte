<script lang="ts" module>
  export type MetricsTab = 'overview' | 'logs'
</script>

<script lang="ts">
  import { Header, Picker, VStack } from '@fuckyouryan/fui'
  import { t } from '$lib/i18n.svelte'
  import LogsTab from './components/LogsTab.svelte'
  import OverviewTab from './components/OverviewTab.svelte'

  let { tab = 'overview', ontabchange }: {
    tab?: MetricsTab
    ontabchange?: (next: MetricsTab) => void
  } = $props()
</script>

<VStack gap={6}>
  <Header title={t('metrics.title')} subtitle={t('metrics.subtitle')} />

  <Picker
    pickerStyle="segmented"
    value={tab}
    onchange={(next) => ontabchange?.(next as MetricsTab)}
    options={[
      { value: 'overview', label: t('metrics.tab_overview.label') },
      { value: 'logs', label: t('metrics.tab_logs.label') },
    ]}
    ariaLabel={t('metrics.title')}
  />

  {#if tab === 'logs'}
    <LogsTab />
  {:else}
    <OverviewTab />
  {/if}
</VStack>
