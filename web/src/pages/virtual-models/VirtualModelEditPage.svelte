<script lang="ts">
  import ModalitiesFlow from '../../components/domain/ModalitiesFlow.svelte'
  import { Icon, Button, Chip, HStack, GroupBox, Text, TextEditor, TextField, VStack, Banner, Header, ContentUnavailable, Picker } from '@fuckyouryan/fui'
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import { CAPABILITY_META } from '$lib/capabilities'
  import type { VirtualModel, AvailableModel } from '$lib/types'
  import { takePendingClone } from '$lib/virtual-clone'
  import { isUnauthenticated } from '$lib/credential-state'
  import { intersectMembers, minPositive } from '$lib/model-aggregates'

  let { vmId = null }: { vmId: string | null } = $props()

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
      const message = getErrorMessage(e)
      if (isUnauthenticated(e, message)) {
        window.location.href = '/login'
        return
      }
      error = message
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
      const message = getErrorMessage(e)
      if (isUnauthenticated(e, message)) {
        window.location.href = '/login'
        return
      }
      formError = message
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
      const message = getErrorMessage(e)
      if (message.includes('modified by another process')) formError = t('virtual.errors.modified_elsewhere')
      else if (message.includes('already exists')) formError = t('virtual.errors.duplicate_name')
      else formError = message
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

  function modelInfoOf(id: string): AvailableModel | undefined {
    return byId.get(id)
  }

  // Aggregated preview: capabilities = intersection, limits = minima.
  // Modalities intersect the same way.
  let aggregates = $derived.by(() => {
    const found = models.map(modelInfoOf).filter((m): m is AvailableModel => !!m)
    if (found.length === 0) return { caps: [] as string[], inMods: [] as string[], outMods: [] as string[], context: 0, output: 0 }
    const count = new Map<string, number>()
    for (const m of found) for (const c of m.capabilities ?? []) count.set(c, (count.get(c) ?? 0) + 1)
    const caps = [...count.entries()].filter(([, n]) => n === found.length).map(([c]) => c).sort()
    return {
      caps,
      inMods: intersectMembers(models, (id) => modalityMap[id]?.input),
      outMods: intersectMembers(models, (id) => modalityMap[id]?.output),
      context: minPositive(found.map((m) => m.context_window)),
      output: minPositive(found.map((m) => m.max_tokens))
    }
  })

  let modelOptions = $derived(availableModels.map((m) => ({ value: m.full_model_id, label: m.display_name })))
  let canSave = $derived(name.trim() !== '' && models.length > 0 && models.every((id) => id !== ''))
  let isManaged = $derived(!!vm?.managed_by)
  let isEditable = $derived(!vm?.managed_by)
  let saveLabel = $derived(saving ? t('common.actions.saving') : vm ? t('common.actions.save') : t('virtual.create.title'))
</script>

  <VStack gap={6}>
  <Header title={vmId ? t('virtual.edit.title') : t('virtual.create.title')} info={vmId ? t('virtual.update_desc') : t('virtual.create_desc')} />

  {#if error}
    <Banner tint="danger" text={error} />
  {:else if loading}
    <ContentUnavailable title={t('common.state.loading')} />
  {:else}
    {#if formError}
      <Banner tint="danger" text={formError} />
    {/if}

    {#if isManaged}
      <Text foregroundStyle="secondary" size="sm">{t('virtual.managed_by_provider')}</Text>
    {/if}

    {#if modelsLoadState === 'empty'}
      <Banner tint="warning" text={t('virtual.configure_first')} />
    {/if}

    <GroupBox title={t('virtual.basic_info')}>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-name">ID *</Text>
        <TextField id="vm-name" bind:value={name} hint={t('virtual.model')} disabled={isManaged} />
      </VStack>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-description">{t('virtual.description_for_decision')}</Text>
        <TextEditor id="vm-description" bind:value={description} hint={t('virtual.description_example')} minRows={2} disabled={isManaged} />
      </VStack>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-instruction">{t('virtual.instruction_label')}</Text>
        <TextEditor id="vm-instruction" bind:value={instruction} hint={t('virtual.general_instructions')} minRows={4} disabled={isManaged} />
      </VStack>
    </GroupBox>

    <GroupBox title={t('models.list.title')}>
      {#if modelsLoadState === 'loading'}
        <ContentUnavailable title={t('common.state.loading')} />
      {:else if modelsLoadState === 'error'}
        <VStack gap={2} halign="center">
          <Banner tint="danger" text={t('models.list.failed_load')} />
          <Button text={t('common.actions.reload')} onclick={loadAvailableModels} />
        </VStack>
      {:else if models.length === 0}
        {#if modelsLoadState === 'empty'}
          <ContentUnavailable title={t('virtual.no_models_configure')} />
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
              <div class="model-row-top">
                <HStack gap={0} valign="center" class="col-priority">
                  <span class="drag-handle" title={t('models.drag_reorder')}><Icon name="drag_indicator" /></span>
                  <Text size="sm" foregroundStyle="secondary" align="center" class="row-num">{i + 1}</Text>
                </HStack>
                <div class="model-row-select">
                  <Picker value={id} options={modelOptions} searchable={true} placeholder={t('models.select.placeholder')} onchange={(v) => setModel(i, v)} disabled={isManaged} />
                </div>
                <Button tint="var(--fui-color-danger)" buttonStyle="text" icon={{ name: 'delete' }} ariaLabel={t('virtual.remove_model')} onclick={() => removeModel(i)} disabled={isManaged} />
              </div>
              <div class="model-row-bottom">
                <HStack gap={2} wrap valign="center" class="row-caps">
                  <ModalitiesFlow modalities={{ input: modalityMap[id]?.input, output: modalityMap[id]?.output }} chipsDirection="horizontal" />
                  {#each modelInfoOf(id)?.capabilities ?? [] as cap}
                    {@const capability = CAPABILITY_META[cap]}
                    {#if capability}
                      <Chip icon={capability.icon} tint={capability.chipTint} size="md" title={t(capability.hint)} />
                    {:else}
                      <Chip text={cap} size="md" title={cap} />
                    {/if}
                  {/each}
                </HStack>
                <VStack gap={0} class="row-ctx">
                  {#if modelInfoOf(id)?.context_window}<Text size="sm" foregroundStyle="secondary">{(modelInfoOf(id)!.context_window! / 1000).toFixed(0)}k ctx</Text>{/if}
                  {#if modelInfoOf(id)?.max_tokens}<Text size="sm" foregroundStyle="secondary">{(modelInfoOf(id)!.max_tokens! / 1000).toFixed(0)}k out</Text>{/if}
                </VStack>
              </div>
            </div>
          {/each}
        </VStack>
        {#if isEditable}
          <Button text={t('models.actions.add')} icon={{ name: 'add' }} onclick={addModel} />
        {/if}
        <HStack gap={3} wrap valign="center">
          <Text size="sm" weight="medium" foregroundStyle="secondary">{t('virtual.capabilities')}</Text>
          <ModalitiesFlow modalities={{ input: aggregates.inMods, output: aggregates.outMods }} chipsDirection="horizontal" />
          {#if aggregates.caps.length > 0}
            {#each aggregates.caps as cap}
              {@const capability = CAPABILITY_META[cap]}
              {#if capability}
                <Chip icon={capability.icon} text={t(capability.label)} size="md" tint={capability.chipTint} title={t(capability.hint)} />
              {:else}
                <Chip text={cap} title={cap} />
              {/if}
            {/each}
          {:else}
            <Text foregroundStyle="tertiary" size="sm">—</Text>
          {/if}
          {#if aggregates.context > 0}
            <Text size="sm" foregroundStyle="secondary" title={t('virtual.smallest_context')}>{(aggregates.context / 1000).toFixed(0)}k ctx</Text>
          {/if}
          {#if aggregates.output > 0}
            <Text size="sm" foregroundStyle="secondary" title={t('virtual.smallest_output')}>{(aggregates.output / 1000).toFixed(0)}k out</Text>
          {/if}
        </HStack>
      {/if}
    </GroupBox>

    {#if isEditable}
    <HStack halign="end" gap={2}>
      <Button text={t('common.actions.cancel')} buttonStyle="text" onclick={backToList} disabled={saving} />
      <Button
        text={saveLabel}
        buttonStyle="prominent"
        disabled={!canSave || saving}
        onclick={save}
      />
    </HStack>
    {/if}
  {/if}
</VStack>

<style>
  .model-row {
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-2);
    min-width: 0;
  }
  .model-row-top {
    display: flex;
    align-items: center;
    gap: var(--fui-space-3);
    min-width: 0;
  }
  .model-row-select {
    flex: 1 1 var(--fui-vmedit-name-min);
    min-width: 0;
  }
  .model-row-bottom {
    display: flex;
    flex-wrap: wrap;
    gap: var(--fui-space-2);
    min-width: 0;
  }

  .model-row[draggable='true'] {
    cursor: grab;
  }
  .model-row[draggable='true']:active {
    cursor: grabbing;
  }
</style>
