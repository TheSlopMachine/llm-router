<script lang="ts">
  // The provider's virtual models: one per endpoint, falling through the provider's models of that
  // endpoint. A provider-wide switch turns them all off.
  import { onMount } from 'svelte'
  import { ContentUnavailable, CopyButton, HStack, SectionHeader, Table, Text, Toggle, VStack } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t, type TranslationKey } from '$lib/i18n.svelte'
  import type { ProviderVMGroup, VirtualModel } from '$lib/types'

  let { providerId, onerror }: { providerId: string; onerror: (message: string) => void } = $props()

  const ENDPOINTS: Record<string, { path: string; hint: TranslationKey }> = {
    chat: { path: '/v1/chat/completions', hint: 'models.endpoints.text_chat' },
    transcription: { path: '/v1/audio/transcriptions', hint: 'models.endpoints.speech_to_text' },
    speech: { path: '/v1/audio/speech', hint: 'models.endpoints.text_to_speech' },
    images: { path: '/v1/images/generations', hint: 'models.endpoints.image_gen' },
    embeddings: { path: '/v1/embeddings', hint: 'models.endpoints.text_embeddings' },
  }

  let groups = $state.raw<ProviderVMGroup[]>([])
  let enabled = $state(true)
  let switching = $state(false)
  // Managed records are ensured once per provider view: the toggle needs a record, and sync is
  // idempotent and cache-only.
  let syncEnsured = false

  onMount(() => void reload())

  /** reloads the groups; their members follow the provider's model list */
  export async function reload(): Promise<void> {
    try {
      const [isEnabled, loaded] = await Promise.all([
        api.providers.virtualModelsEnabled(providerId),
        api.providers.virtualModels(providerId),
      ])
      enabled = isEnabled
      if (isEnabled && loaded.some((group) => !group.virtual) && !syncEnsured) {
        syncEnsured = true
        groups = await api.providers.syncVirtualModels(providerId)
      } else {
        groups = loaded
      }
    } catch (e) {
      groups = []
      onerror(getErrorMessage(e))
    }
  }

  async function switchAll(next: boolean): Promise<void> {
    switching = true
    try {
      groups = await api.providers.setVirtualModelsEnabled(providerId, next)
      enabled = next
    } catch (e) {
      onerror(getErrorMessage(e))
    } finally {
      switching = false
    }
  }

  async function toggle(virtualModel: VirtualModel, next: boolean): Promise<void> {
    try {
      await api.virtualModels.update(virtualModel.id, { ...virtualModel, disabled: !next })
      await reload()
    } catch (e) {
      onerror(getErrorMessage(e))
    }
  }

  const endpointOf = (group: ProviderVMGroup): { path: string; hint: TranslationKey } =>
    ENDPOINTS[group.endpoint] ?? { path: group.endpoint, hint: group.label as TranslationKey }
</script>

{#snippet virtualToggle(group: ProviderVMGroup)}
  {#if group.virtual}
    {@const virtualModel = group.virtual}
    <Toggle checked={!virtualModel.disabled} ariaLabel={t('common.labels.enabled')} onchange={(next) => toggle(virtualModel, next)} />
  {/if}
{/snippet}

<VStack tag="section" gap={4}>
  <SectionHeader title={t('virtual.models_plural')} subtitle={t('providers.models.fallthrough_description')}>
    {#snippet actions()}
      <Toggle checked={enabled} disabled={switching} ariaLabel={t('common.labels.enabled')} onchange={(next) => void switchAll(next)} />
    {/snippet}
  </SectionHeader>

  <!-- switched off, the provider has no virtual models to list -->
  {#if enabled}
    <Table
      columns={[
        { key: 'vm', title: t('virtual.model'), width: '1.4fr' },
        { key: 'endpoint', title: t('providers.models.endpoint'), width: '1.2fr' },
        { key: 'count', title: t('models.list.title'), width: '0.4fr' },
        { key: 'toggle', width: 'auto' },
      ]}
      rows={groups}
      rowKey={(group) => group.endpoint}
    >
      {#snippet cell({ column, row })}
        {@const group = row as ProviderVMGroup}
        {#if column.key === 'vm'}
          {#if group.virtual}
            <Text size="md" weight="medium">{group.virtual.name}</Text>
            <HStack gap={2} valign="center">
              <Text mono size="sm">virtual/{group.virtual.id}</Text>
              <CopyButton size="sm" text={`virtual/${group.virtual.id}`} title={t('models.actions.copy_id')} ariaLabel={t('models.actions.copy_id')} />
            </HStack>
          {:else}
            <Text foregroundStyle="tertiary">—</Text>
          {/if}
        {:else if column.key === 'endpoint'}
          {@const endpoint = endpointOf(group)}
          <Text size="sm" foregroundStyle="secondary" title={t(endpoint.hint)}>{endpoint.path}</Text>
        {:else if column.key === 'count'}
          <Text size="sm" foregroundStyle="secondary">{group.models.length}</Text>
        {:else}
          {@render virtualToggle(group)}
        {/if}
      {/snippet}
      {#snippet card({ row })}
        {@const group = row as ProviderVMGroup}
        <HStack gap={3} valign="center">
          <VStack gap={1} grow>
            {#if group.virtual}
              <Text variant="value">{group.virtual.name}</Text>
              <Text variant="caption" mono>virtual/{group.virtual.id}</Text>
            {:else}
              <Text foregroundStyle="tertiary">—</Text>
            {/if}
          </VStack>
          {@render virtualToggle(group)}
        </HStack>
        <Text variant="caption">{endpointOf(group).path} · {group.models.length} {t('models.list.title')}</Text>
      {/snippet}
      {#snippet empty()}
        <ContentUnavailable title={t('providers.models.no_endpoint_groups')} />
      {/snippet}
    </Table>
  {/if}
</VStack>
