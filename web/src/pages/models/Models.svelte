<script lang="ts">
  import ModelsTable from '../../components/domain/ModelsTable.svelte'
  import type { ModelsTableModel } from '../../components/domain/ModelsTable.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import type { AvailableModel, VirtualModel } from '$lib/types'
  import { t, n } from '$lib/i18n.svelte'
  import { Button, ConfirmAction, ContentUnavailable, FloatingView, HStack, Picker, SearchField, Spacer, Toggle, VStack, Banner, Header } from '@fuckyouryan/fui'
  import { rankModels, matchesVirtualModel } from '$lib/model-search'
  import { unionMembers as unionIds, minPositive } from '$lib/model-aggregates'
  import { setPendingClone } from '$lib/virtual-clone'

  // Which models the table lists: real or virtual ones, all or only enabled or disabled.
  // Both choices are kept in this browser.
  type ModelKind = 'real' | 'virtual'
  type ModelStatus = 'all' | 'enabled' | 'disabled'
  const KIND_KEY = 'llm-router:models-kind'
  const STATUS_KEY = 'llm-router:models-status'

  function loadChoice<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
    try {
      const stored = localStorage.getItem(key)
      const found = allowed.find((value) => value === stored)
      if (found) return found
    } catch (e) {
      console.error(`Failed to access localStorage for ${key}:`, e)
    }
    return fallback
  }

  function saveChoice(key: string, value: string): void {
    try {
      localStorage.setItem(key, value)
    } catch (e) {
      console.error(`Failed to save ${key} to localStorage:`, e)
    }
  }

  function selectKind(next: ModelKind): void {
    kind = next
    saveChoice(KIND_KEY, next)
  }

  function selectStatus(next: ModelStatus): void {
    status = next
    saveChoice(STATUS_KEY, next)
  }

  // replaced as a whole on load, never mutated: raw state keeps the rows out of deep proxies
  let allModels = $state.raw<AvailableModel[]>([])
  let virtualModels = $state.raw<VirtualModel[]>([])
  let loading = $state(true)
  let error = $state('')
  let query = $state('')
  let kind = $state<ModelKind>(loadChoice(KIND_KEY, ['real', 'virtual'], 'real'))
  let status = $state<ModelStatus>(loadChoice(STATUS_KEY, ['all', 'enabled', 'disabled'], 'enabled'))

  let hasModels = $derived(!loading && allModels.length + virtualModels.length > 0)
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
      name: `${vm.name} Copy`,
      description: vm.description ?? '',
      instruction: vm.instruction ?? '',
      models: (vm.models ?? []).map((entry) => entry.model_id),
    })
    window.location.hash = '#/virtual/new'
  }

  // provider-managed virtual models follow their provider: no edit, no delete
  const isEditableVirtualModel = (vm: VirtualModel): boolean => !vm.managed_by

  let deleteTarget = $state<VirtualModel | null>(null)
  let deleteAnchor = $state<HTMLElement>()
  let deleting = $state(false)

  function openDelete(vm: VirtualModel, anchor: HTMLElement): void {
    if (deleteTarget?.id === vm.id) {
      deleteTarget = null
      return
    }
    deleteTarget = vm
    deleteAnchor = anchor
  }

  async function confirmDelete(): Promise<void> {
    const vm = deleteTarget
    if (!vm || deleting) return
    deleting = true
    try {
      await api.virtualModels.delete(vm.id)
      deleteTarget = null
      await load()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      deleting = false
    }
  }
</script>

<VStack gap={6} grow>
  <Header
    title={t('models.list.title')}
    subtitle={hasModels ? n(allModels.length + virtualModels.length, 'units.model.one', 'units.model.many') : undefined}
    primary={{ id: 'new', text: t('virtual.actions.new_model'), icon: 'add', onclick: openNewVirtualModel }}
  />

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  <VStack gap={3}>
    <HStack gap={3} wrap valign="center">
      <Picker
        pickerStyle="segmented"
        value={status}
        options={[
          { value: 'all', label: t('common.actions.all') },
          { value: 'enabled', label: t('common.labels.enabled') },
          { value: 'disabled', label: t('common.labels.disabled') },
        ]}
        ariaLabel={t('models.filter.visibility')}
        onchange={(next) => selectStatus(next as ModelStatus)}
      />
      <Spacer />
      <Picker
        pickerStyle="segmented"
        value={kind}
        options={[
          { value: 'real', label: t('models.kind.real.label') },
          { value: 'virtual', label: t('models.kind.virtual.label') },
        ]}
        ariaLabel={t('models.kind.title')}
        onchange={(next) => selectKind(next as ModelKind)}
      />
    </HStack>
    <SearchField bind:value={query} placeholder={t('models.search.placeholder')} />
  </VStack>

  <ModelsTable models={tableModels} sortable showProviderLink lazy {loading}>
    {#snippet actions({ model })}
      {#if model.kind === 'virtual'}
        {@const vm = model.source as VirtualModel}
        <HStack gap={2} valign="center" halign="end">
          <Button buttonStyle="text" icon={{ name: 'content_copy' }} ariaLabel={t('tokens.actions.clone')} title={t('tokens.actions.clone')} size="sm" onclick={() => cloneVirtualModel(vm)} />
          {#if isEditableVirtualModel(vm)}
            <Button buttonStyle="text" icon={{ name: 'edit' }} ariaLabel={t('common.actions.edit')} title={t('common.actions.edit')} size="sm" onclick={() => openEditVirtualModel(vm)} />
            <Button buttonStyle="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} ariaLabel={t('common.actions.delete')} title={t('common.actions.delete')} size="sm" onclick={(e) => openDelete(vm, e.currentTarget as HTMLElement)} />
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

  <FloatingView
    open={Boolean(deleteTarget)}
    anchor={deleteAnchor}
    width="sm"
    onclose={() => { deleteTarget = null }}
    label={t('virtual.delete_title')}
  >
    {#snippet children({ close })}
      <ConfirmAction
        title={t('virtual.delete_title')}
        body={`${t('misc.delete_confirm')} "${deleteTarget?.name}"? ${t('common.undo.cannot_undo')}`}
        busy={deleting}
        busyLabel={t('common.actions.deleting')}
        oncancel={close}
        onconfirm={confirmDelete}
      />
    {/snippet}
  </FloatingView>
</VStack>
