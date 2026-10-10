<script lang="ts">
  import ModelsTable from '../../components/domain/ModelsTable.svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { createListResource } from '$lib/list-resource.svelte'
  import type { VirtualModel, AvailableModel } from '$lib/types'
  import { setPendingClone } from '$lib/virtual-clone'
  import { t } from '$lib/i18n.svelte'
  import EmptyState from '../../FUI/composite/EmptyState.svelte'
  import { Button, FloatingView, HStack, Switch, Text, VStack, Banner, ConfirmAction, Header, ToolbarItem } from '$ui'
  import { unionMembers as unionIds, minPositive } from '$lib/model-aggregates'
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
  // model over its member ids.
  let candidates: AvailableModel[] = $state([])

  $effect(() => {
    if ((resource.data ?? []).length > 0 && candidates.length === 0) {
      void loadCandidates()
    }
  })

  async function loadCandidates(): Promise<void> {
    try {
      candidates = await api.models.available()
    } catch {
      candidates = []
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

<VStack gap={6}>
  <Header
    title={t('virtual.models_plural')}
    info={t('virtual.models_desc')}
  >
    {#snippet actions()}
      <ToolbarItem primary><Button text={t('virtual.actions.new_model')} style="prominent" icon={{ name: 'add' }} onclick={openNew} /></ToolbarItem>
    {/snippet}
  </Header>

  {#if resource.error}
    <Banner variant="error" text={resource.error} />
  {/if}

  {#if resource.loading}
    <EmptyState title={t('common.state.loading')} />
  {:else}
    <ModelsTable
      models={(resource.data ?? []).filter((a) => a).map((vm) => ({
        kind: 'virtual' as const,
        id: vm.id,
        fullId: `virtual/${vm.id}`,
        description: vm.description || '—',
        contextWindow: minMembers(vm, (m) => m?.context_window),
        maxTokens: minMembers(vm, (m) => m?.max_tokens),
        inputModalities: unionMembers(vm, (m) => m?.input_modalities),
        outputModalities: unionMembers(vm, (m) => m?.output_modalities),
        capabilities: unionMembers(vm, (m) => m?.capabilities),
        source: vm,
      }))}
      sortable
    >
      {#snippet actions({ model })}
        {@const vm = model.source as VirtualModel}
         <HStack gap={2} class="vm-actions" align="center" justify="end">
           <Button style="text" icon={{ name: 'content_copy' }} ariaLabel={t('tokens.actions.clone')} title={t('tokens.actions.clone')} size="small" onclick={() => clone(vm)} />
           {#if isEditableVm(vm)}
             <Button style="text" icon={{ name: 'edit' }} ariaLabel={t('common.actions.edit')} title={t('common.actions.edit')} size="small" onclick={() => openEdit(vm)} />
             <Button style="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} ariaLabel={t('common.actions.delete')} title={t('common.actions.delete')} size="small" onclick={(e) => openDelete(vm, e.currentTarget as HTMLElement)} />
           {/if}
           <Switch
             checked={!vm.disabled}
             ariaLabel={vm.disabled ? t('virtual.enable') : t('virtual.disable')}
             onchange={(en) => toggle(vm, en)}
           />
         </HStack>
      {/snippet}
      {#snippet empty()}
       {#snippet createVmAction()}
           <Button style="prominent" icon={{ name: 'add' }} onclick={openNew}>{t('virtual.create.first')}</Button>
         {/snippet}
         <EmptyState
           icon="robot"
           title={t('virtual.list.empty')}
           action={createVmAction}
         />
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
         onCancel={close}
         onConfirm={confirmDelete} />
     {/snippet}
   </FloatingView>
</VStack>
