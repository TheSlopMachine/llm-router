<script lang="ts">
  import ModelsTable from '../../components/domain/ModelsTable.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { createListResource } from '$lib/list-resource.svelte'
  import type { VirtualModel, AvailableModel } from '$lib/types'
  import { setPendingClone } from '$lib/virtual-clone'
  import { t } from '$lib/i18n.svelte'
  import { ContentUnavailable, Button, FloatingView, HStack, Toggle, Text, VStack, Banner, ConfirmAction, Header, SearchField } from '@fuckyouryan/fui'
  import { unionMembers as unionIds, minPositive } from '$lib/model-aggregates'
  import { matchesVirtualModel } from '$lib/model-search'
  import type { ModelsTableModel } from '../../components/domain/ModelsTable.svelte'
  import { isUnauthenticated } from '$lib/credential-state'

  const resource = createListResource<VirtualModel[]>(
    async () => {
      try {
        const response = (await api.virtualModels.list()) as unknown
        return Array.isArray(response) ? (response as VirtualModel[]) : []
      } catch (e: unknown) {
        if (isUnauthenticated(e, getErrorMessage(e))) {
          window.location.href = '/login'
          return []
        }
        throw e
      }
    },
    []
  )

  // Candidate metadata comes from one dashboard call: union per virtual
  // model over its member ids. Loaded once on mount: the list does not
  // depend on the virtual models, so no effect re-triggers it.
  // replaced as a whole on load, never mutated: raw state keeps the rows out of deep proxies
  let candidates: AvailableModel[] = $state.raw([])
  let candidatesError = $state('')

  onMount(() => {
    void loadCandidates()
  })

  async function loadCandidates(): Promise<void> {
    try {
      candidates = await api.models.available()
      candidatesError = ''
    } catch (e: unknown) {
      candidatesError = getErrorMessage(e)
    }
  }

  const byId = $derived(new Map(candidates.map((m) => [m.full_model_id, m])))

  function unionMembers(vm: VirtualModel, pick: (m: AvailableModel | undefined) => string[] | undefined): string[] {
    return unionIds(
      (vm.models ?? []).map((e) => e.model_id),
      (id) => pick(byId.get(id))
    )
  }

  function minMembers(vm: VirtualModel, pick: (m: AvailableModel | undefined) => number | undefined): number {
    return minPositive((vm.models ?? []).map((e) => pick(byId.get(e.model_id))))
  }

  let query = $state('')

  const tableModels = $derived(
    (resource.data ?? [])
      .filter((vm) => vm && matchesVirtualModel(vm, query))
      .map((vm): ModelsTableModel => ({
        kind: 'virtual',
        id: vm.id,
        fullId: `virtual/${vm.id}`,
        description: vm.description || '—',
        contextWindow: minMembers(vm, (m) => m?.context_window),
        maxTokens: minMembers(vm, (m) => m?.max_tokens),
        inputModalities: unionMembers(vm, (m) => m?.input_modalities),
        outputModalities: unionMembers(vm, (m) => m?.output_modalities),
        capabilities: unionMembers(vm, (m) => m?.capabilities),
        source: vm,
      })),
  )
  const hasVirtualModels = $derived((resource.data ?? []).length > 0)

  let isEditableVm = (vm: VirtualModel): boolean => !vm.managed_by

  function openNew() {
    window.location.hash = '#/virtual/new'
  }

  function openEdit(vm: VirtualModel) {
    window.location.hash = `#/virtual/${vm.id}`
  }

  function clone(vm: VirtualModel) {
    setPendingClone({
      name: `${vm.name} Copy`,
      description: vm.description ?? '',
      instruction: vm.instruction ?? '',
      models: (vm.models ?? []).map((m) => m.model_id),
    })
    window.location.hash = '#/virtual/new'
  }

  async function toggle(vm: VirtualModel, enabled: boolean) {
    try {
      await api.virtualModels.update(vm.id, { ...vm, disabled: !enabled })
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }

  let deleteTarget = $state<VirtualModel | null>(null)
  let deleteAnchor = $state<HTMLElement>()
  let deleting = $state(false)

  function openDelete(vm: VirtualModel, anchorEl?: HTMLElement): void {
    if (deleteTarget?.id === vm.id) {
      deleteTarget = null
    } else {
      deleteTarget = vm
      deleteAnchor = anchorEl
    }
  }

  async function confirmDelete(): Promise<void> {
    const vm = deleteTarget
    if (!vm || deleting) return
    deleting = true
    try {
      await api.virtualModels.delete(vm.id)
      deleteTarget = null
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    } finally {
      deleting = false
    }
  }
</script>

<VStack gap={6} grow>
  <Header
    title={t('virtual.models_plural')}
    subtitle={t('virtual.models_desc')}
    primary={{ id: 'new', text: t('virtual.actions.new_model'), icon: 'add', onclick: openNew }}
  />

  {#if candidatesError}
    <Banner tint="warning" text={candidatesError} />
  {/if}
  {#if resource.error}
    <Banner tint="danger" text={resource.error} />
  {/if}

  <SearchField bind:value={query} placeholder={t('models.search.placeholder')} />

  {#if resource.loading}
    <ContentUnavailable title={t('common.state.loading')} />
  {:else}
    <ModelsTable models={tableModels} sortable lazy>
      {#snippet actions({ model })}
        {@const vm = model.source as VirtualModel}
         <HStack gap={2} class="vm-actions" valign="center" halign="end">
           <Button buttonStyle="text" icon={{ name: 'content_copy' }} ariaLabel={t('tokens.actions.clone')} title={t('tokens.actions.clone')} size="sm" onclick={() => clone(vm)} />
           {#if isEditableVm(vm)}
             <Button buttonStyle="text" icon={{ name: 'edit' }} ariaLabel={t('common.actions.edit')} title={t('common.actions.edit')} size="sm" onclick={() => openEdit(vm)} />
             <Button buttonStyle="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} ariaLabel={t('common.actions.delete')} title={t('common.actions.delete')} size="sm" onclick={(e) => openDelete(vm, e.currentTarget as HTMLElement)} />
           {/if}
           <Toggle
             checked={!vm.disabled}
             ariaLabel={t('common.labels.enabled')}
             onchange={(en) => toggle(vm, en)}
           />
         </HStack>
      {/snippet}
      {#snippet empty()}
       {#snippet createVmAction()}
           <Button buttonStyle="prominent" icon={{ name: 'add' }} onclick={openNew}>{t('virtual.create.first')}</Button>
         {/snippet}
         {#if hasVirtualModels}
           <ContentUnavailable title={t('models.list.no_match')} icon="search" />
         {:else}
           <ContentUnavailable
             icon="robot"
             title={t('virtual.list.empty')}
             action={createVmAction}
           />
         {/if}
       {/snippet}
     </ModelsTable>
   {/if}

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
         onconfirm={confirmDelete} />
     {/snippet}
   </FloatingView>
</VStack>
