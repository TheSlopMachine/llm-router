<script lang="ts">
  import { api } from '$lib/api'
  import { modal } from '$lib/modal.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { createListResource } from '$lib/list-resource.svelte'
  import type { VirtualModel, AvailableModel } from '$lib/types'
  import { setPendingClone } from '$lib/virtual-clone'
  import { t } from '$lib/i18n.svelte'
  import EmptyState from '../../components/EmptyState.svelte'
  import { Button, ModelsTable, HStack, Switch, Text, VStack } from '$ui'

  const resource = createListResource<VirtualModel[]>(
    async () => {
      try {
        const response = (await api.virtualModels.list()) as unknown
        return Array.isArray(response) ? (response as VirtualModel[]) : []
      } catch (e: unknown) {
        const rec = e as { status?: number; message?: string }
        if (rec?.status === 401 || getErrorMessage(e).includes('unauthenticated')) {
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
    const out: string[] = []
    for (const e of vm.models ?? []) {
      for (const x of pick(byId.get(e.model_id)) ?? []) {
        if (!out.includes(x)) out.push(x)
      }
    }
    return out
  }

  function minMembers(vm: VirtualModel, pick: (m: AvailableModel | undefined) => number | undefined): number {
    let min = 0
    for (const e of vm.models ?? []) {
      const v = pick(byId.get(e.model_id))
      if (v && v > 0 && (min === 0 || v < min)) min = v
    }
    return min
  }

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

  async function remove(vm: VirtualModel) {
    const confirmed = await modal.confirm({
      title: t('Delete virtual model'),
      message: `${t('Are you sure you want to delete')} "${vm.name}"? ${t('This action cannot be undone.')}`,
      severity: 'high',
      size: 'small',
      confirmText: t('Delete'),
      confirmRole: 'destructive'
    })

    if (!confirmed) return

    try {
      await api.virtualModels.delete(vm.id)
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }
</script>

<VStack gap={4}>
  <HStack align="center" justify="between" gap={3}>
    <VStack gap={1}>
      <Text tag="h1" size="lg" weight="bold">{t('Virtual models')}</Text>
      <Text tone="soft" size="sm">{t('Virtual models route requests across multiple models with fall-through.')}</Text>
    </VStack>
    <Button text={t('Add')} style="prominent" icon={{ name: 'add' }} onclick={openNew} />
  </HStack>

  {#if resource.error}
    <Text tone="danger" size="sm" class="error-msg">{resource.error}</Text>
  {/if}

  {#if resource.loading}
    <Text tone="soft" size="sm">{t('Loading virtual models...')}</Text>
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
          <Button style="text" icon={{ name: 'content_copy' }} ariaLabel={t('Clone')} title={t('Clone')} size="small" onclick={() => clone(vm)} />
          {#if !vm.managed_by}
            <Button style="text" icon={{ name: 'edit' }} ariaLabel={t('Edit')} title={t('Edit')} size="small" onclick={() => openEdit(vm)} />
            <Button style="text" tint="#dc2626" icon={{ name: 'delete' }} ariaLabel={t('Delete')} title={t('Delete')} size="small" onclick={() => remove(vm)} />
          {/if}
          <Switch
            checked={!vm.disabled}
            ariaLabel={vm.disabled ? t('Enable virtual model') : t('Disable virtual model')}
            onchange={(en) => toggle(vm, en)}
          />
        </HStack>
      {/snippet}
      {#snippet empty()}
        <EmptyState
          icon="robot"
          message={t('No virtual models yet')}
          buttonText={t('Create your first virtual model')}
          buttonIcon="add"
          onButtonClick={openNew}
        />
      {/snippet}
    </ModelsTable>
  {/if}
</VStack>
