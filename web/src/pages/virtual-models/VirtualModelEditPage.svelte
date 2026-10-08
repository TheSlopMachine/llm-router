<script lang="ts">
  import ModalitiesFlow from '../../components/domain/ModalitiesFlow.svelte'
  import Icon from '../../FUI/controls/Icon.svelte'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import { CAPABILITY_META } from '$lib/capabilities'
  import type { VirtualModel, AvailableModel } from '$lib/types'
  import { takePendingClone } from '$lib/virtual-clone'
  import { isUnauthenticated } from '$lib/credential-state'
  import { intersectMembers, minPositive } from '$lib/model-aggregates'
  import { Button, Chip, HStack, SectionCard, Select, Text, TextArea, TextEdit, VStack, Banner } from '$ui'

  let { vmId = null } = $props<{ vmId: string | null }>()

  let vm = $state<VirtualModel | undefined>(undefined)
  let loading = $state(false)
  let error = $state('')

  let name = $state('')
  let description = $state('')
  let instruction = $state('')
  let models = $state<string[]>([])

  let availableModels = $state<AvailableModel[]>([])
  let modelsLoadState = $state<'loading' | 'loaded' | 'empty' | 'error'>('loading')
  let saving = $state(false)
  let formError = $state('')

  const byId = $derived(new Map(availableModels.map((m) => [m.full_model_id, m])))
  // Modalities ride the available payload: keyed by full model id.
  let modalityMap = $derived<Record<string, { input?: string[]; output?: string[] }>>(
    Object.fromEntries(
      availableModels.map((m) => [m.full_model_id, { input: m.input_modalities, output: m.output_modalities }])
    )
  )

  onMount(() => {
    void loadAvailableModels()
  })

  // Reload when navigating between list/new/edit without a remount. Reads
  // the vmId prop only, so the loader cannot feed back into the effect.
  $effect(() => {
    void vmId
    void loadVirtualModel()
  })

  async function loadVirtualModel() {
    error = ''
    vm = undefined
    if (!vmId) {
      resetForm()
      return
    }
    loading = true
    try {
      const fetched = (await api.virtualModels.get(vmId)) as VirtualModel
      vm = fetched
      hydrate(fetched)
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if (isUnauthenticated(e, msg)) {
        window.location.href = '/login'
        return
      }
      error = msg
    } finally {
      loading = false
    }
  }

  function resetForm(): void {
    const clone = vmId ? null : takePendingClone()
    if (clone) {
      name = clone.name
      description = clone.description
      instruction = clone.instruction
      models = [...clone.models]
      return
    }
    name = ''
    description = ''
    instruction = ''
    models = []
  }

  function hydrate(v: VirtualModel): void {
    name = v.name ?? ''
    description = v.description ?? ''
    instruction = v.instruction ?? ''
    // Dedupe defensively: the keyed list below crashes on duplicate keys,
    // and the backend rejects duplicates on save.
    models = [...new Set((v.models ?? []).map((m: { model_id: string }) => m.model_id))]
  }

  function backToList() {
    window.location.hash = '#/virtual'
  }

  async function loadAvailableModels() {
    try {
      availableModels = await api.models.available()
      modelsLoadState = availableModels.length === 0 ? 'empty' : 'loaded'
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if (isUnauthenticated(e, msg)) {
        window.location.href = '/login'
        return
      }
      formError = msg
      modelsLoadState = 'error'
      availableModels = []
    }
  }

  async function save() {
    if (!canSave || saving) return
    saving = true
    formError = ''
    try {
      const payload = {
        name: name.trim(),
        description: description.trim(),
        instruction,
        models: models.map((model_id) => ({ model_id })),
        version: vm?.version || 0,
      }
      if (vm) await api.virtualModels.update(vm.id, payload)
      else await api.virtualModels.create(payload)
      backToList()
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if (msg.includes('modified by another process')) formError = t('virtual.errors.modified_elsewhere')
      else if (msg.includes('already exists')) formError = t('virtual.errors.duplicate_name')
      else formError = msg
      saving = false
    }
  }

  function addModel() {
    // Never duplicate: the keyed list crashes on duplicate keys and the
    // backend rejects duplicates on save.
    const next = availableModels.map((m) => m.full_model_id).find((id) => id && !models.includes(id))
    if (next === undefined) {
      formError = t('virtual.all_models_in_list')
      return
    }
    formError = ''
    models = [...models, next]
  }
  function removeModel(index: number) {
    models = models.filter((_, i) => i !== index)
  }
  function setModel(index: number, id: string) {
    if (id && models.some((m, i) => i !== index && m === id)) {
      formError = t('virtual.model_already_in_list')
      return
    }
    formError = ''
    const next = [...models]
    next[index] = id
    models = next
  }

  // Drag to reorder — same pattern as the credential pool.
  let dragIndex = $state(-1)

  function onDragStart(e: DragEvent, index: number) {
    dragIndex = index
    e.dataTransfer?.setData('text/plain', String(index))
    if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
  }
  function onDragOver(e: DragEvent) {
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }
  function onDrop(e: DragEvent, target: number) {
    e.preventDefault()
    const from = dragIndex >= 0 ? dragIndex : Number(e.dataTransfer?.getData('text/plain') ?? -1)
    dragIndex = -1
    if (from < 0 || from === target) return
    const next = [...models]
    const [moved] = next.splice(from, 1)
    next.splice(target, 0, moved)
    models = next
  }

  function infoOf(id: string): AvailableModel | undefined {
    return byId.get(id)
  }

  // Aggregated preview: capabilities = intersection, limits = minima.
  // Modalities intersect the same way.
  let aggregates = $derived.by(() => {
    const infos = models.map(infoOf).filter((m): m is AvailableModel => !!m)
    if (infos.length === 0) return { caps: [] as string[], inMods: [] as string[], outMods: [] as string[], context: 0, output: 0 }
    const count = new Map<string, number>()
    for (const m of infos) for (const c of m.capabilities ?? []) count.set(c, (count.get(c) ?? 0) + 1)
    const caps = [...count.entries()].filter(([, n]) => n === infos.length).map(([c]) => c).sort()
    return {
      caps,
      inMods: intersectMembers(models, (id) => modalityMap[id]?.input),
      outMods: intersectMembers(models, (id) => modalityMap[id]?.output),
      context: minPositive(infos.map((m) => m.context_window)),
      output: minPositive(infos.map((m) => m.max_tokens))
    }
  })

  let modelOptions = $derived(availableModels.map((m) => ({ value: m.full_model_id, label: m.display_name })))
  let canSave = $derived(name.trim() !== '' && models.length > 0 && models.every((id) => id !== ''))
  let isManaged = $derived(!!vm?.managed_by)
  let isEditable = $derived(!vm?.managed_by)
  let saveLabel = $derived(saving ? t('common.actions.saving') : vm ? t('common.actions.save') : t('virtual.create.title'))
</script>

  <VStack gap={4}>
  <VStack gap={1}>
    <Text tag="h1" size="lg" weight="bold">{vmId ? t('virtual.edit.title') : t('virtual.create.title')}</Text>
    <Text tone="soft" size="sm">{vmId ? t('virtual.update_desc') : t('virtual.create_desc')}</Text>
  </VStack>

  {#if error}
    <Banner variant="error" text={error} />
  {:else if loading}
    <Text tone="soft" size="sm">{t('virtual.loading_single')}</Text>
  {:else}
    {#if formError}
      <Banner variant="error" text={formError} />
    {/if}

    {#if isManaged}
      <Text tone="soft" size="sm">{t('virtual.managed_by_provider')}</Text>
    {/if}

    {#if modelsLoadState === 'empty'}
      <Banner variant="warning" text={t('virtual.configure_first')} />
    {/if}

    <SectionCard title={t('virtual.basic_info')}>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-name">ID *</Text>
        <TextEdit id="vm-name" bind:value={name} hint={t('virtual.model')} disabled={isManaged} />
      </VStack>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-description">{t('virtual.description_for_decision')}</Text>
        <TextArea id="vm-description" bind:value={description} hint={t('virtual.description_example')} minRows={2} disabled={isManaged} />
      </VStack>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-instruction">{t('virtual.instruction_label')}</Text>
        <TextArea id="vm-instruction" bind:value={instruction} hint={t('virtual.general_instructions')} minRows={4} disabled={isManaged} />
      </VStack>
    </SectionCard>

    <SectionCard title={t('models.list.title')}>
      {#if modelsLoadState === 'loading'}
        <Text tone="soft" size="sm">{t('models.list.loading')}</Text>
      {:else if modelsLoadState === 'error'}
        <VStack gap={2} align="center">
          <Text tone="danger" size="sm">{t('models.list.failed_load')}</Text>
          <Button text={t('common.actions.reload')} onclick={loadAvailableModels} />
        </VStack>
      {:else if models.length === 0}
        {#if modelsLoadState === 'empty'}
          <Text tone="soft" size="sm">{t('virtual.no_models_configure')}</Text>
        {:else if isEditable}
          <Button text={t('models.actions.add')} icon={{ name: 'add' }} onclick={addModel} />
        {/if}
      {:else}
        <VStack gap={3}>
          <!-- keyed by the model id, not the index: this list is drag-reorderable,
               and an index key makes Svelte rewrite rows in place instead of
               moving them, so per-row state sticks to the position -->
          {#each models as id, i (id || `empty-${i}`)}
            <div
              class="model-row"
              draggable={isManaged ? 'false' : 'true'}
              ondragstart={(e) => onDragStart(e, i)}
              ondragover={onDragOver}
              ondrop={(e) => onDrop(e, i)}
              role="listitem"
            >
              <HStack gap={0} align="center" class="col-priority">
                <span class="drag-handle" title={t('models.drag_reorder')}><Icon name="drag_indicator" /></span>
                <Text size="sm" tone="soft" align="center" class="row-num">{i + 1}</Text>
              </HStack>
              <Select value={id} options={modelOptions} searchable={true} placeholder={t('models.select.placeholder')} onchange={(v) => setModel(i, v)} disabled={isManaged} />
              <HStack gap={2} wrap align="center" class="row-caps">
                <ModalitiesFlow modalities={{ input: modalityMap[id]?.input, output: modalityMap[id]?.output }} chipsDirection="horizontal" />
                {#each infoOf(id)?.capabilities ?? [] as cap}
                  {@const meta = CAPABILITY_META[cap]}
                  {#if meta}
                    <Chip icon={meta.icon} color={meta.color} size="medium" title={t(meta.hint)} />
                  {:else}
                    <Chip text={cap} size="medium" title={cap} />
                  {/if}
                {/each}
              </HStack>
              <VStack gap={0} class="row-ctx">
                {#if infoOf(id)?.context_window}<Text size="sm" tone="soft">{(infoOf(id)!.context_window! / 1000).toFixed(0)}k ctx</Text>{/if}
                {#if infoOf(id)?.max_tokens}<Text size="sm" tone="soft">{(infoOf(id)!.max_tokens! / 1000).toFixed(0)}k out</Text>{/if}
              </VStack>
              <Button tint="var(--fui-color-danger)" style="text" icon={{ name: 'delete' }} ariaLabel={t('virtual.remove_model')} onclick={() => removeModel(i)} disabled={isManaged} />
            </div>
          {/each}
        </VStack>
        {#if isEditable}
          <Button text={t('models.actions.add')} icon={{ name: 'add' }} onclick={addModel} />
        {/if}
        <HStack gap={3} wrap align="center">
          <Text size="sm" weight="medium" tone="soft">{t('virtual.capabilities')}</Text>
          <ModalitiesFlow modalities={{ input: aggregates.inMods, output: aggregates.outMods }} chipsDirection="horizontal" />
          {#if aggregates.caps.length > 0}
            {#each aggregates.caps as cap}
              {@const meta = CAPABILITY_META[cap]}
              {#if meta}
                <Chip icon={meta.icon} text={t(meta.label)} size="medium" color={meta.color} title={t(meta.hint)} />
              {:else}
                <Chip text={cap} title={cap} />
              {/if}
            {/each}
          {:else}
            <Text tone="disabled" size="sm">—</Text>
          {/if}
          {#if aggregates.context > 0}
            <Text size="sm" tone="soft" title={t('virtual.smallest_context')}>{(aggregates.context / 1000).toFixed(0)}k ctx</Text>
          {/if}
          {#if aggregates.output > 0}
            <Text size="sm" tone="soft" title={t('virtual.smallest_output')}>{(aggregates.output / 1000).toFixed(0)}k out</Text>
          {/if}
        </HStack>
      {/if}
    </SectionCard>

    {#if isEditable}
    <HStack justify="end" gap={2}>
      <Button text={t('common.actions.cancel')} style="text" onclick={backToList} disabled={saving} />
      <Button
        text={saveLabel}
        style="prominent"
        disabled={!canSave || saving}
        onclick={save}
      />
    </HStack>
    {/if}
  {/if}
</VStack>

<style>
  .model-row {
    display: grid;
    grid-template-columns: 56px minmax(220px, 1fr) auto auto auto;
    align-items: center;
    gap: var(--fui-space-4);
  }

  .model-row[draggable='true'] {
    cursor: grab;
  }
  .model-row[draggable='true']:active {
    cursor: grabbing;
  }
</style>
