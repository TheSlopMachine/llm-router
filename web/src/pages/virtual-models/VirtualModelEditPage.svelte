<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import { CAPABILITY_META } from '$lib/capabilities'
  import type { VirtualModel, AvailableModel } from '$lib/types'
  import { takePendingClone } from '$lib/virtual-clone'
  import { Button, Chip, HStack, ModalitiesFlow, SectionCard, Select, Text, TextArea, TextEdit, VStack } from '$ui'

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
      if ((e as { status?: number })?.status === 401 || msg.includes('unauthenticated')) {
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
      if ((e as { status?: number })?.status === 401 || msg.includes('unauthenticated')) {
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
      if (msg.includes('modified by another process')) formError = t('This virtual model was modified elsewhere. Please refresh and try again.')
      else if (msg.includes('already exists')) formError = t('A virtual model with this name already exists. Please choose a different name.')
      else formError = msg
      saving = false
    }
  }

  function addModel() {
    // Never duplicate: the keyed list crashes on duplicate keys and the
    // backend rejects duplicates on save.
    const next = availableModels.map((m) => m.full_model_id).find((id) => id && !models.includes(id))
    if (next === undefined) {
      formError = t('All available models are already in the list.')
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
      formError = t('This model is already in the list.')
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
    const intersect = (pick: (id: string) => string[] | undefined): string[] => {
      const lists = models.map((id) => pick(id) ?? []).filter((l) => l.length > 0)
      if (lists.length === 0) return []
      const first = lists[0]
      return first.filter((m) => lists.every((l) => l.includes(m)))
    }
    const count = new Map<string, number>()
    for (const m of infos) for (const c of m.capabilities ?? []) count.set(c, (count.get(c) ?? 0) + 1)
    const caps = [...count.entries()].filter(([, n]) => n === infos.length).map(([c]) => c).sort()
    const min = (pick: (m: AvailableModel) => number | undefined) =>
      infos.reduce((acc, m) => {
        const v = pick(m)
        return v && v > 0 && (acc === 0 || v < acc) ? v : acc
      }, 0)
    return {
      caps,
      inMods: intersect((id) => modalityMap[id]?.input),
      outMods: intersect((id) => modalityMap[id]?.output),
      context: min((m) => m.context_window),
      output: min((m) => m.max_tokens)
    }
  })

  let modelOptions = $derived(availableModels.map((m) => ({ value: m.full_model_id, label: m.display_name })))
  let canSave = $derived(name.trim() !== '' && models.length > 0 && models.every((id) => id !== ''))
</script>

<VStack gap={4}>
  <VStack gap={1}>
    <Text tag="h1" size="lg" weight="bold">{vmId ? t('Edit virtual model') : t('New virtual model')}</Text>
    <Text tone="soft" size="sm">{vmId ? t('Update the fall-through list and instruction for this virtual model.') : t('Create a virtual model that routes requests across multiple models with fall-through.')}</Text>
  </VStack>

  {#if error}
    <Text tone="danger" size="sm">{error}</Text>
  {:else if loading}
    <Text tone="soft" size="sm">{t('Loading virtual model...')}</Text>
  {:else}
    {#if formError}
      <Text tone="danger" size="sm">{formError}</Text>
    {/if}

    {#if vm?.managed_by}
      <Text tone="soft" size="sm">{t('Managed by the provider. Members refresh automatically; only the enabled toggle on the lists can change it.')}</Text>
    {/if}

    {#if modelsLoadState === 'empty'}
      <div class="warning-banner">
        <span class="icon">warning</span>
        <VStack gap={2}>
          <Text weight="medium">{t('No models available')}</Text>
          <Text size="base" tone="soft">{t('Configure at least one provider with credentials to create virtual models.')}</Text>
          <Button text={t('Go to Providers')} style="prominent" icon={{ name: 'cloud' }} onclick={() => { window.location.hash = '#/providers' }} />
        </VStack>
      </div>
    {/if}

    <SectionCard title="Basic information">
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-name">ID *</Text>
        <TextEdit id="vm-name" bind:value={name} hint={t('How it appears in the model list — lowercase, hyphenated')} disabled={!!vm?.managed_by} />
      </VStack>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-description">{t('Description')}</Text>
        <TextArea id="vm-description" bind:value={description} hint={t('Pretty name, e.g. Gemini fallback')} minRows={2} disabled={!!vm?.managed_by} />
      </VStack>
      <VStack gap={1}>
        <Text tag="label" size="sm" weight="medium" for="vm-instruction">{t('System instruction')}</Text>
        <TextArea id="vm-instruction" bind:value={instruction} hint={t('Additional guidance for the LLM (behavioral, stylistic)')} minRows={4} disabled={!!vm?.managed_by} />
      </VStack>
    </SectionCard>

    <SectionCard title="Models">
      {#if modelsLoadState === 'loading'}
        <Text tone="soft" size="sm">{t('Loading available models...')}</Text>
      {:else if modelsLoadState === 'error'}
        <VStack gap={2} align="center">
          <Text tone="danger" size="sm">{t('Failed to load models. Please try again.')}</Text>
          <Button text={t('Reload')} onclick={loadAvailableModels} />
        </VStack>
      {:else if models.length === 0}
        {#if modelsLoadState === 'empty'}
          <Text tone="soft" size="sm">{t('No models are currently available. Configure providers first.')}</Text>
        {:else if !vm?.managed_by}
          <Button text={t('Add model')} icon={{ name: 'add' }} onclick={addModel} />
        {/if}
      {:else}
        <VStack gap={3}>
          <!-- keyed by the model id, not the index: this list is drag-reorderable,
               and an index key makes Svelte rewrite rows in place instead of
               moving them, so per-row state sticks to the position -->
          {#each models as id, i (id || `empty-${i}`)}
            <div
              class="model-row"
              draggable={vm?.managed_by ? 'false' : 'true'}
              ondragstart={(e) => onDragStart(e, i)}
              ondragover={onDragOver}
              ondrop={(e) => onDrop(e, i)}
              role="listitem"
            >
              <HStack gap={0} align="center" class="col-priority">
                <span class="icon drag-handle" title={t('Drag to reorder')}>drag_indicator</span>
                <Text size="sm" tone="soft" align="center" class="row-num">{i + 1}</Text>
              </HStack>
              <Select value={id} options={modelOptions} searchable={true} placeholder={t('Select a model...')} onchange={(v) => setModel(i, v)} disabled={!!vm?.managed_by} />
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
              <Button tint="#dc2626" style="text" icon={{ name: 'delete' }} ariaLabel={t('Remove model')} onclick={() => removeModel(i)} disabled={!!vm?.managed_by} />
            </div>
          {/each}
        </VStack>
        {#if !vm?.managed_by}
        <Button text={t('Add model')} icon={{ name: 'add' }} onclick={addModel} />
        {/if}
        <HStack gap={3} wrap align="center">
          <Text size="sm" weight="medium" tone="soft">{t('Virtual model capabilities')}</Text>
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
            <Text size="sm" tone="soft" title={t('Smallest context window across the models')}>{(aggregates.context / 1000).toFixed(0)}k ctx</Text>
          {/if}
          {#if aggregates.output > 0}
            <Text size="sm" tone="soft" title={t('Smallest max output across the models')}>{(aggregates.output / 1000).toFixed(0)}k out</Text>
          {/if}
        </HStack>
      {/if}
    </SectionCard>

    {#if !vm?.managed_by}
    <HStack justify="end" gap={2}>
      <Button text={t('Cancel')} style="text" onclick={backToList} disabled={saving} />
      <Button
        text={saving ? t('Saving…') : vm ? t('Save') : t('Create virtual model')}
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
    gap: var(--space-4);
  }

  .model-row[draggable='true'] {
    cursor: grab;
  }
  .model-row[draggable='true']:active {
    cursor: grabbing;
  }

  /* :global — classes ride HStack/VStack roots in another component. */
  :global(.col-priority) {
    color: var(--color-text-soft);
  }

  /* The number fills the cell past the grip so its center lands exactly
     between the grip icon and the picker. */
  :global(.row-num) {
    flex: 1 1 0;
  }

  :global(.row-caps) {
    justify-content: flex-end;
  }

  :global(.row-ctx) {
    text-align: right;
  }

  .warning-banner {
    display: flex;
    gap: var(--space-4);
    padding: var(--space-5);
    background: var(--color-notification-warning-bg);
    border: 1px solid var(--color-notification-warning-border);
    border-radius: 8px;
  }

  .warning-banner .icon {
    font-size: var(--text-xl);
    color: var(--color-notification-warning-icon);
    flex-shrink: 0;
  }
</style>
