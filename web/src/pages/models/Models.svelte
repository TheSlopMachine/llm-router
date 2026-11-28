<script lang="ts">
  import ModelsTable from '../../components/domain/ModelsTable.svelte'
  import type { ModelsTableModel } from '../../components/domain/ModelsTable.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { AvailableModel, VirtualModel } from '$lib/types'
  import { t, tx } from '$lib/i18n.svelte'
  import { Button, ContentUnavailable, DeleteButton, HStack, Picker, SearchField, Section, Toggle, Banner, Header, confirmAt } from '@fuckyouryan/fui'
  import { rankModels, matchesVirtualModel } from '$lib/model-search'
  import { unionMembers as unionIds, minPositive } from '$lib/model-aggregates'
  import { setPendingClone } from '$lib/virtual-clone'
  import { Persisted, oneOf } from '$lib/persisted.svelte'

  // Which models the table lists: real or virtual ones, all or only enabled or disabled.
  // Both choices are kept in this browser.
  type ModelKind = 'real' | 'virtual'
  type ModelStatus = 'all' | 'enabled' | 'disabled'
  const kindChoice = new Persisted<ModelKind>('llm-router:models-kind', 'real', { parse: oneOf(['real', 'virtual'] as const) })
  const statusChoice = new Persisted<ModelStatus>('llm-router:models-status', 'enabled', { parse: oneOf(['all', 'enabled', 'disabled'] as const) })

  function selectKind(next: ModelKind): void {
    kindChoice.value = next
  }

  function selectStatus(next: ModelStatus): void {
    statusChoice.value = next
  }

  // replaced as a whole on load, never mutated: raw state keeps the rows out of deep proxies
  let allModels = $state.raw<AvailableModel[]>([])
  let virtualModels = $state.raw<VirtualModel[]>([])
  let loading = $state(true)
  let error = $state('')
  let query = $state('')
  let kind = $derived(kindChoice.value)
  let status = $derived(statusChoice.value)

  let isEmptyCatalog = $derived(allModels.length + virtualModels.length === 0)

  let realRows = $derived.by((): ModelsTableModel[] =>
    rankModels(allModels, query)
      .sort((a, b) => {
        if (a.category !== b.category) return a.category - b.category
        if (a.category === 2 && a.score !== b.score) return b.score - a.score
        if (a.provider_name !== b.provider_name) return a.provider_name.localeCompare(b.provider_name)
        if (a.display_name !== b.display_name) return a.display_name.localeCompare(b.display_name)
        return a.full_model_id.localeCompare(b.full_model_id)
      })
      .map((model) => ({
        kind: 'model',
        id: model.model_name,
        fullId: model.full_model_id,
        name: model.display_name,
        providerId: model.provider_id,
        providerName: model.provider_name,
        contextWindow: model.context_window,
        maxTokens: model.max_tokens,
        inputModalities: model.input_modalities,
        outputModalities: model.output_modalities,
        capabilities: model.capabilities,
        disabled: model.disabled,
        source: model,
      })),
  )

  const candidateById = $derived(new Map(allModels.map((m) => [m.full_model_id, m])))

  // A virtual model shows the union of its members' modalities and capabilities and the
  // smallest of their limits.
  let virtualRows = $derived.by((): ModelsTableModel[] =>
    virtualModels
      .filter((vm) => matchesVirtualModel(vm, query))
      .map((vm) => {
        const memberIds = (vm.models ?? []).map((entry) => entry.model_id)
        const members = memberIds.map((id) => candidateById.get(id))
        const union = (pick: (m: AvailableModel | undefined) => string[] | undefined): string[] =>
          unionIds(memberIds, (id) => pick(candidateById.get(id)))
        return {
          kind: 'virtual',
          id: vm.id,
          fullId: `virtual/${vm.id}`,
          description: vm.description || '—',
          contextWindow: minPositive(members.map((m) => m?.context_window)) || undefined,
          maxTokens: minPositive(members.map((m) => m?.max_tokens)) || undefined,
          inputModalities: union((m) => m?.input_modalities),
          outputModalities: union((m) => m?.output_modalities),
          capabilities: union((m) => m?.capabilities),
          disabled: vm.disabled,
          source: vm,
        }
      }),
  )

  const matchesStatus = (row: ModelsTableModel, wanted: ModelStatus): boolean =>
    wanted === 'all' || (wanted === 'disabled') === Boolean(row.disabled)

  let tableModels = $derived((kind === 'real' ? realRows : virtualRows).filter((row) => matchesStatus(row, status)))

  // The pickers count what each choice lists, before the search: the status counts are the
  // selected kind's.
  const kindCounts = $derived({ real: allModels.length, virtual: virtualModels.length })
  const kindSource = $derived<{ disabled?: boolean }[]>(kind === 'real' ? allModels : virtualModels)
  const statusCounts = $derived({
    all: kindSource.length,
    enabled: kindSource.filter((model) => !model.disabled).length,
    disabled: kindSource.filter((model) => model.disabled).length,
  })

  onMount(load)

  // The two lists load apart: a failing one leaves the other listed and shows its error.
  async function load() {
    const [models, virtual] = await Promise.allSettled([
      api.models.available({ includeDisabled: true }),
      api.virtualModels.list(),
    ])
    const failures: string[] = []
    if (models.status === 'fulfilled') allModels = models.value
    else failures.push(getErrorMessage(models.reason))
    if (virtual.status === 'fulfilled') virtualModels = Array.isArray(virtual.value) ? (virtual.value as VirtualModel[]) : []
    else failures.push(getErrorMessage(virtual.reason))
    error = failures.join('\n')
    loading = false
  }

  async function toggleModel(model: AvailableModel, enabled: boolean): Promise<void> {
    try {
      await api.models.setOverride(model.provider_id, model.model_name, { disabled: !enabled })
      await load()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function toggleVirtualModel(vm: VirtualModel, enabled: boolean): Promise<void> {
    try {
      await api.virtualModels.update(vm.id, { ...vm, disabled: !enabled })
      await load()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function openNewVirtualModel(): void {
    window.location.hash = '#/virtual/new'
  }

  function openEditVirtualModel(vm: VirtualModel): void {
    window.location.hash = `#/virtual/${vm.id}`
  }

  function cloneVirtualModel(vm: VirtualModel): void {
    setPendingClone({
      name: tx('common.clone.name', { name: vm.name }),
      description: vm.description ?? '',
      instruction: vm.instruction ?? '',
      models: (vm.models ?? []).map((entry) => entry.model_id),
    })
    window.location.hash = '#/virtual/new'
  }

  // provider-managed virtual models follow their provider: no edit, no delete
  const isEditableVirtualModel = (vm: VirtualModel): boolean => !vm.managed_by

  async function deleteVirtualModel(vm: VirtualModel, anchor: HTMLElement): Promise<void> {
    error = ''
    try {
      const deleted = await confirmAt(anchor, {
        title: t('virtual.delete_title'),
        body: `${t('misc.delete_confirm')} "${vm.name}"? ${t('common.undo.cannot_undo')}`,
        busyLabel: t('common.actions.deleting'),
        run: () => api.virtualModels.delete(vm.id),
      })
      if (deleted) await load()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }
</script>

<Header
  title={t('models.list.title')}
  primary={{ id: 'new', text: t('virtual.actions.new_model'), icon: 'add', onclick: openNewVirtualModel }}
/>

{#if error}
  <Banner tint="danger" text={error} />
{/if}

<Section grow>
  <HStack wrap valign="center">
    <Picker
      pickerStyle="segmented"
      value={status}
      options={[
        { value: 'all', label: t('common.actions.all'), badge: statusCounts.all },
        { value: 'enabled', label: t('common.labels.enabled'), badge: statusCounts.enabled },
        { value: 'disabled', label: t('common.labels.disabled'), badge: statusCounts.disabled },
      ]}
      ariaLabel={t('models.filter.visibility')}
      onchange={(next) => selectStatus(next as ModelStatus)}
    />
    <Picker
      pickerStyle="segmented"
      value={kind}
      options={[
        { value: 'real', label: t('models.kind.real.label'), badge: kindCounts.real },
        { value: 'virtual', label: t('models.kind.virtual.label'), badge: kindCounts.virtual },
      ]}
      ariaLabel={t('models.kind.title')}
      onchange={(next) => selectKind(next as ModelKind)}
    />
  </HStack>
  <SearchField bind:value={query} placeholder={t('models.search.placeholder')} />

  <ModelsTable models={tableModels} sortable showProviderLink lazy {loading}>
    {#snippet actions({ model })}
      {#if model.kind === 'virtual'}
        {@const vm = model.source as VirtualModel}
        <HStack valign="center" halign="end">
          <Button buttonStyle="text" icon={{ name: 'content_copy' }} ariaLabel={t('tokens.actions.clone')} title={t('tokens.actions.clone')} onclick={() => cloneVirtualModel(vm)} />
          {#if isEditableVirtualModel(vm)}
            <Button buttonStyle="text" icon={{ name: 'edit' }} ariaLabel={t('common.actions.edit')} title={t('common.actions.edit')} onclick={() => openEditVirtualModel(vm)} />
            <DeleteButton onclick={(e) => void deleteVirtualModel(vm, e.currentTarget as HTMLElement)} />
          {/if}
          <Toggle checked={!vm.disabled} ariaLabel={t('common.labels.enabled')} onchange={(enabled) => void toggleVirtualModel(vm, enabled)} />
        </HStack>
      {:else}
        {@const realModel = model.source as AvailableModel}
        <Toggle checked={!realModel.disabled} ariaLabel={t('common.labels.enabled')} onchange={(enabled) => void toggleModel(realModel, enabled)} />
      {/if}
    {/snippet}
    {#snippet empty()}
      {#snippet createVirtualModelAction()}
        <Button buttonStyle="prominent" icon={{ name: 'add' }} onclick={openNewVirtualModel}>{t('virtual.create.first')}</Button>
      {/snippet}
      {#if isEmptyCatalog}
        <ContentUnavailable title={t('models.list.empty_configure_first')} icon="cloud" />
      {:else if kind === 'virtual' && virtualModels.length === 0}
        <ContentUnavailable title={t('virtual.list.empty')} icon="robot" action={createVirtualModelAction} />
      {:else}
        <ContentUnavailable title={t('models.list.no_match')} icon="search" />
      {/if}
    {/snippet}
  </ModelsTable>
</Section>
