<script lang="ts">
  import { Text, Toggle, Banner, Table, Header, ListRow, type TableColumn, ContentUnavailable } from '@fuckyouryan/fui'
  import { PROVIDERS_ICON } from '$lib/subsystems'
  import { openFormModal } from '$lib/modal-helpers'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { createListResource } from '$lib/list-resource.svelte'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import type { Provider, ProviderStats } from '$lib/types'
  import { isSystemDisabled } from '$lib/credential-state'
  import { formatDisableReason } from '$lib/format'
  import { t, n } from '$lib/i18n.svelte'

  const resource = createListResource<{ providers: Provider[]; providerStats: Record<string, ProviderStats> }>(
    async () => {
      const [p, s] = await Promise.all([api.providers.list(), api.providers.stats()])
      return { providers: p, providerStats: s }
    },
    { providers: [], providerStats: {} }
  )

  const providerColumns: TableColumn[] = [
    { key: 'name', title: t('providers.detail.title'), width: '1fr', priority: 1 },
    { key: 'creds', title: t('credentials.title_plural'), width: 'var(--fui-table-col-xl)', priority: 2 },
    { key: 'models', title: t('models.list.title'), width: 'var(--fui-table-col-md)', priority: 2 },
    { key: 'toggle', title: '', width: 'auto', align: 'right', priority: 1 }
  ]

  function openProviderDetail(provider: Provider): void {
    window.location.hash = '#/providers/' + provider.id
  }

  async function toggleProvider(provider: Provider, enabled: boolean): Promise<void> {
    resource.error = ''
    try {
      await api.providers.updateInstance(provider.id, { name: provider.name, disabled: !enabled })
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }

  function openCreate(): void {
    resource.error = ''
    openFormModal(CustomProviderWizard, {
      title: t('providers.actions.new'),
      size: 'md',
      props: { editingProvider: null },
      onReload: () => void resource.reload()
    })
  }
</script>

{#snippet toggle(provider: Provider)}
  <Toggle checked={!provider.disabled} ariaLabel={t('common.labels.enabled')} onchange={(v) => toggleProvider(provider, v)} />
{/snippet}

<!-- the table row and the phone card show the provider the same way; the toggle is the card's own control -->
{#snippet identity(provider: Provider, withToggle: boolean)}
  {#snippet providerToggle()}{@render toggle(provider)}{/snippet}
  <ListRow
    title={provider.name}
    subtitle={provider.type + (provider.qualifier ? ':' + provider.qualifier : '')}
    image={provider.icon_url}
    icon={PROVIDERS_ICON}
    trailing={withToggle ? providerToggle : undefined}
  >
    {#if isSystemDisabled(provider)}
      <Text variant="caption" tint="danger">{t('providers.detail.disabled_auto')}{formatDisableReason(provider.disabled_reason)}</Text>
    {/if}
  </ListRow>
{/snippet}

<Header title={t('providers.list.title')} subtitle={t('providers.list.subtitle')} primary={{ id: 'new', text: t('providers.actions.new'), icon: 'add', onclick: openCreate }} />

  {#if resource.error}
    <Banner tint="danger" text={resource.error} />
  {/if}

  <Table
    columns={providerColumns}
    rows={resource.data.providers}
    rowKey={(p) => (p as Provider).id}
    loading={resource.loading}
    onrowclick={(p) => openProviderDetail(p as Provider)}
  >
    {#snippet cell({ column, row })}
      {@const provider = row as Provider}
      {@const stats = resource.data.providerStats[provider.id] || null}
      {#if column.key === 'name'}
        {@render identity(provider, false)}
      {:else if column.key === 'creds'}
        <Text size="sm">{n(stats?.credential_count ?? 0, 'credentials.status.active.one', 'credentials.status.active.many')}</Text>
      {:else if column.key === 'models'}
        <Text size="sm">{String(stats?.enabled_model_count ?? 0)}</Text>
      {:else if column.key === 'toggle'}
        {@render toggle(provider)}
      {/if}
    {/snippet}
    {#snippet card({ row })}
      {@render identity(row as Provider, true)}
    {/snippet}
    {#snippet empty()}
      <ContentUnavailable title={t('providers.list.empty')} icon="cloud" />
    {/snippet}
  </Table>
