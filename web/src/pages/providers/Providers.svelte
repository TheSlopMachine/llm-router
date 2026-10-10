<script lang="ts">
  import { Button, Text, Switch, Banner, Icon, Table, Header, HStack, VStack, ToolbarItem } from '$ui'
  import type { TableColumn } from '$ui'
  import EmptyState from '../../FUI/composite/EmptyState.svelte'
  import { openFormModal } from '$lib/modal-helpers'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { createListResource } from '$lib/list-resource.svelte'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import { squircle } from '../../FUI/core/squircle'
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

  let visibleProviders = $derived(resource.data.providers)

  const providerColumns: TableColumn[] = [
    { key: 'name', title: t('providers.detail.title'), width: '1fr', priority: 1 },
    { key: 'creds', title: t('credentials.title_plural'), width: 'var(--fui-table-col-xl)', priority: 2 },
    { key: 'models', title: t('models.list.title'), width: 'var(--fui-table-col-md)', align: 'right', priority: 2 },
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
      size: 'medium',
      props: { editingProvider: null },
      onReload: () => void resource.reload()
    })
  }
</script>

<VStack gap={6}>
  <Header title={t('providers.list.title')} info={t('providers.list.subtitle')}>
    {#snippet actions()}
      <ToolbarItem primary><Button style="prominent" onclick={openCreate} icon={{ name: 'add' }}>{t('providers.actions.new')}</Button></ToolbarItem>
    {/snippet}
  </Header>

  {#if resource.error}
    <Banner variant="error" text={resource.error} />
  {/if}

  {#if resource.loading}
    <EmptyState title={t('common.state.loading')} />
  {:else if visibleProviders.length === 0}
    <EmptyState title={t('providers.list.empty')} icon="cloud" />
  {:else}
    <Table
      columns={providerColumns}
      rows={visibleProviders}
      rowKey={(p) => (p as Provider).id}
      onrowclick={(p) => openProviderDetail(p as Provider)}
    >
      {#snippet cell({ column, row })}
        {@const provider = row as Provider}
        {@const stats = resource.data.providerStats[provider.id] || null}
        {#if column.key === 'name'}
          <HStack gap={3} align="center">
            {#if provider.icon_url}
              <img src={provider.icon_url} alt="" class="provider-icon" use:squircle />
            {:else}
              <span class="provider-icon-fallback"><Icon name="cloud" size="lg" /></span>
            {/if}
            <VStack gap={1}>
              <Text variant="value">{provider.name}</Text>
              <Text variant="caption">{provider.type}{provider.qualifier ? ':' + provider.qualifier : ''}</Text>
              {#if isSystemDisabled(provider)}
                <Text variant="caption" tone="danger">{t('providers.detail.disabled_auto')}{formatDisableReason(provider.disabled_reason)}</Text>
              {/if}
            </VStack>
          </HStack>
        {:else if column.key === 'creds'}
          <Text size="sm">{n(stats?.credential_count ?? 0, 'credentials.status.active.one', 'credentials.status.active.many')}</Text>
        {:else if column.key === 'models'}
          <Text size="sm">{String(stats?.model_count ?? 0)}</Text>
        {:else if column.key === 'toggle'}
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span
            class="col-toggle"
            onclick={(e) => e.stopPropagation()}
            onkeydown={(e) => e.stopPropagation()}
          >
            <Switch
              checked={!provider.disabled}
              ariaLabel={t('providers.detail.enable')}
              onchange={(v) => toggleProvider(provider, v)}
            />
          </span>
        {/if}
      {/snippet}
      {#snippet card({ row })}
        {@const provider = row as Provider}
        {@const stats = resource.data.providerStats[provider.id] || null}
        <HStack gap={3} align="center">
          {#if provider.icon_url}
            <img src={provider.icon_url} alt="" class="provider-icon" use:squircle />
          {:else}
            <span class="provider-icon-fallback"><Icon name="cloud" size="lg" /></span>
          {/if}
          <VStack gap={1} grow>
            <Text variant="value">{provider.name}</Text>
            <Text variant="caption">{provider.type}{provider.qualifier ? ':' + provider.qualifier : ''}</Text>
          </VStack>
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span
            onclick={(e) => e.stopPropagation()}
            onkeydown={(e) => e.stopPropagation()}
          >
            <Switch
              checked={!provider.disabled}
              ariaLabel={t('providers.detail.enable')}
              onchange={(v) => toggleProvider(provider, v)}
            />
          </span>
        </HStack>
        <Text size="sm">{n(stats?.credential_count ?? 0, 'credentials.status.active.one', 'credentials.status.active.many')} · {stats?.model_count ?? 0} {t('models.list.title')}</Text>
        {#if isSystemDisabled(provider)}
          <Text variant="caption" tone="danger">{t('providers.detail.disabled_auto')}{formatDisableReason(provider.disabled_reason)}</Text>
        {/if}
      {/snippet}
      {#snippet empty()}
        <EmptyState title={t('providers.list.empty')} icon="cloud" />
      {/snippet}
    </Table>
  {/if}
</VStack>

<style>
  .provider-icon {
    width: var(--fui-size-icon-lg);
    height: var(--fui-size-icon-lg);
    border-radius: var(--fui-radius-sm);
    object-fit: contain;
    flex-shrink: 0;
  }
  .provider-icon-fallback {
    width: var(--fui-size-icon-lg);
    height: var(--fui-size-icon-lg);
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--fui-color-surface-container-highest);
    border-radius: var(--fui-radius-sm);
    color: var(--fui-color-text-soft);
    flex-shrink: 0;
  }
  .col-toggle {
    display: flex;
    justify-content: flex-end;
  }
</style>
