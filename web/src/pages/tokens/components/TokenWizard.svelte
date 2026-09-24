<script lang="ts">
  import { onMount } from 'svelte'
  import { untrack } from 'svelte'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { toggleSet } from '$lib/token-helpers'
  import { t, n } from '$lib/i18n.svelte'
  import type { Token, Provider, VirtualModel, AvailableModel, ProviderModel } from '$lib/types'
  import type { ModalButton, StepperConfig } from '$lib/modal.svelte'
  import { buildBackendTokenRules } from '$lib/token-rules'
  import { Switch, Checkbox, SearchField, Text, TextEdit, Spacer, ModelsTable, VStack, HStack, List } from '$ui'
  import type { ModelsTableModel } from '$ui'
  import TokenSuccessView from './TokenSuccessView.svelte'

  let {
    providers,
    editingToken = null,
    onComplete,
    updateButtons,
    updateTitle,
    updateSubtitle,
    updateStepper,
    closeModal
  } = $props<{
    providers: Provider[]
    editingToken?: Token | null
    onComplete: (result: { token?: string }) => void
    updateButtons: (buttons: ModalButton[]) => void
    updateTitle: (title: string) => void
    updateSubtitle: (subtitle: string) => void
    updateStepper: (stepper: StepperConfig | null) => void
    closeModal: () => void
  }>()

  let wizardStep: number = $state(1)
  let wizardLoading: boolean = $state(false)
  let error: string = $state('')

  // Step 1: name + full access shortcut.
  let tokenName: string = $state('')
  let fullAccess: boolean = $state(false)

  // Step 2: providers and their accounts.
  let allowAllProvidersAccounts: boolean = $state(false)
  let providerUseAll: Set<string> = $state(new Set())
  let providerCreds: Set<string> = $state(new Set())
  let virtualEnabled: boolean = $state(false)
  let allCredentials: Array<{ id: string; provider_id: string; provider_name: string; label: string; is_expired: boolean; updated_at?: string }> = $state([])

  // Step 3: models.
  let availableModels: AvailableModel[] = $state([])
  let virtualModels: VirtualModel[] = $state([])
  // Unfiltered candidates for per-virtual metadata union.
  let allCandidates: AvailableModel[] = $state([])
  let allowAllModels: boolean = $state(false)
  let selectedModels: Set<string> = $state(new Set())
  let searchModels: string = $state('')

  let createdToken: string | null = $state(null)

  let isEditMode = $derived(!!editingToken)
  // The virtual-models provider row is UI-hidden backend-side, so it never
  // arrives in `providers`: append it explicitly for selection.
  let displayProviders = $derived(
    providers.some((p: Provider) => p.type === 'virtual')
      ? providers
      : [
          ...providers,
          {
            id: 'virtual',
            name: t('Virtual models'),
            type: 'virtual',
            type_key: 'virtual',
            qualifier: '',
            config: {},
            auth_type: '',
            base_url: '',
            icon_url: '',
            supports_auth_flow: false,
            is_ui_readonly: true,
            is_ui_hidden: true,
            disabled: false
          } as Provider
        ]
  )
  let baseTitle = $derived(editingToken ? t('Edit token') : t('New token'))
  let subtitle = $derived(wizardStep === 4 ? t('Token created successfully') : tokenName.trim() ? tokenName.trim() : `${t('Step')} ${wizardStep} ${t('of 4')}`)
  let stepperConfig: StepperConfig | null = $derived({ current: Math.min(wizardStep, 4), total: 4, labels: [t('Name'), t('Providers'), t('Models'), t('Done')] })

  onMount(async () => {
    if (editingToken) {
      const r: any = editingToken.rules || {}
      tokenName = editingToken.name
      fullAccess = !!r.allow_all_providers && !!r.allow_all_models && !!r.allow_all_credentials
      allowAllProvidersAccounts = !!r.allow_all_providers && !!r.allow_all_credentials
      virtualEnabled = !!r.allow_all_providers || (r.allowed_providers || []).includes('virtual')
      allowAllModels = !!r.allow_all_models
      selectedModels = new Set(r.allowed_models || [])
      providerCreds = new Set(r.allowed_credentials || [])
    }
    try {
      const creds: any = await api.credentials.list()
      allCredentials = Array.isArray(creds) ? creds : (creds?.credentials ?? creds ?? [])
      if (!Array.isArray(allCredentials)) allCredentials = []
    } catch (_) {
      allCredentials = []
    }
    syncChrome()
  })

  function credsOf(providerId: string): typeof allCredentials {
    return allCredentials.filter((c) => c.provider_id === providerId)
  }

  function withoutIds(set: Set<string>, ids: string[]): Set<string> {
    const next = new Set(set)
    for (const id of ids) next.delete(id)
    return next
  }

  // Header checkbox: checked only when everything is picked. Clicking an
  // unchecked box picks all, clicking a checked box clears all.
  function providerAllChecked(providerId: string): boolean {
    if (providerId === 'virtual') return virtualEnabled
    if (providerUseAll.has(providerId)) return true
    const creds = credsOf(providerId)
    return creds.length > 0 && creds.every((c) => providerCreds.has(c.id))
  }

  function toggleProviderAll(providerId: string): void {
    if (providerId === 'virtual') {
      virtualEnabled = !virtualEnabled
      return
    }
    if (providerAllChecked(providerId)) {
      providerUseAll = withoutIds(providerUseAll, [providerId])
      providerCreds = withoutIds(providerCreds, credsOf(providerId).map((c) => c.id))
    } else {
      providerCreds = new Set([...providerCreds, ...credsOf(providerId).map((c) => c.id)])
    }
  }

  function toggleProviderUseAll(providerId: string, v: boolean): void {
    const next = new Set(providerUseAll)
    if (v) next.add(providerId)
    else next.delete(providerId)
    providerUseAll = next
  }

  function toggleCred(id: string): void { providerCreds = toggleSet(providerCreds, id) }
  function toggleModel(fullId: string): void { selectedModels = toggleSet(selectedModels, fullId) }

  function providerActive(providerId: string): boolean {
    if (allowAllProvidersAccounts) return true
    if (providerId === 'virtual') return virtualEnabled
    return providerUseAll.has(providerId) || credsOf(providerId).some((c) => providerCreds.has(c.id))
  }

  let activeProviderIds: string[] = $derived(displayProviders.filter((p: Provider) => providerActive(p.id)).map((p: Provider) => p.id))

  // ModelsTable rows: models of the active providers plus virtual models
  // when virtual is in scope. Selection identity is the token rule string
  // (`type/model`, virtual ones `virtual/<id>`).
  function modelFullId(model: ModelsTableModel): string {
    return model.kind === 'virtual' ? `virtual/${model.id}` : (model.fullId ?? model.id)
  }

  let includeVirtualModels = $derived(allowAllProvidersAccounts || virtualEnabled)

  // One dashboard request: candidates carry modalities, virtual models
  // fold their members over the same list.
  let tableModels = $derived.by((): ModelsTableModel[] => {
    const out: ModelsTableModel[] = []
    const scope = new Set(activeProviderIds)
    const seen = new Set<string>()
    const candidateById = new Map(allCandidates.map((m) => [m.full_model_id, m]))
    function unionMembers(ids: string[], pick: (m: AvailableModel | undefined) => string[] | undefined): string[] {
      const union: string[] = []
      for (const id of ids) {
        for (const x of pick(candidateById.get(id)) ?? []) {
          if (!union.includes(x)) union.push(x)
        }
      }
      return union
    }
    function minMembers(ids: string[], pick: (m: AvailableModel | undefined) => number | undefined): number | undefined {
      let min = 0
      for (const id of ids) {
        const v = pick(candidateById.get(id))
        if (v && v > 0 && (min === 0 || v < min)) min = v
      }
      return min || undefined
    }
    function push(model: ModelsTableModel): void {
      const key = modelFullId(model)
      if (seen.has(key)) return
      seen.add(key)
      out.push(model)
    }
    for (const m of availableModels) {
      if (m.provider_type === 'virtual' || !scope.has(m.provider_id)) continue
      push({
        kind: 'model',
        id: m.model_name,
        fullId: m.full_model_id,
        name: m.display_name || m.model_name,
        providerId: m.provider_id,
        providerName: m.provider_name,
        contextWindow: m.context_window,
        maxTokens: m.max_tokens,
        inputModalities: m.input_modalities,
        outputModalities: m.output_modalities,
        capabilities: m.capabilities
      })
    }
    if (includeVirtualModels) {
      for (const vm of virtualModels) {
        const ids = (vm.models ?? []).map((e) => e.model_id)
        push({
          kind: 'virtual',
          id: vm.id,
          fullId: `virtual/${vm.id}`,
          description: vm.description || '—',
          contextWindow: minMembers(ids, (m) => m?.context_window),
          maxTokens: minMembers(ids, (m) => m?.max_tokens),
          inputModalities: unionMembers(ids, (m) => m?.input_modalities),
          outputModalities: unionMembers(ids, (m) => m?.output_modalities),
          capabilities: unionMembers(ids, (m) => m?.capabilities)
        })
      }
    }
    return out
  })

  let filteredTableModels = $derived.by((): ModelsTableModel[] => {
    const q = searchModels.trim().toLowerCase()
    if (!q) return tableModels
    return tableModels.filter((m) =>
      [m.name, m.id, m.fullId, m.providerName].some((v) => v?.toLowerCase().includes(q))
    )
  })

  // Header checkbox over the visible rows: same all-or-nothing logic.
  let modelsAllChecked = $derived(
    filteredTableModels.length > 0 && filteredTableModels.every((m) => selectedModels.has(modelFullId(m)))
  )

  function toggleModelsAll(): void {
    const ids = filteredTableModels.map(modelFullId)
    if (modelsAllChecked) selectedModels = withoutIds(selectedModels, ids)
    else selectedModels = new Set([...selectedModels, ...ids])
  }

  let hasAnyModelAvailable = $derived(tableModels.length > 0)

  let coveredCredIds = $derived.by((): Set<string> => {
    const out = new Set<string>()
    if (allowAllProvidersAccounts) return out
    for (const pid of activeProviderIds) {
      if (pid === 'virtual') continue
      const ids = providerUseAll.has(pid) ? credsOf(pid).map((c) => c.id) : [...providerCreds].filter((id) => credsOf(pid).some((c) => c.id === id))
      for (const id of ids) out.add(id)
    }
    return out
  })

  let step1Valid = $derived(!!tokenName.trim())
  let step2Valid = $derived(allowAllProvidersAccounts || activeProviderIds.length > 0)
  let step3Valid = $derived.by(() => {
    if (allowAllModels) return true
    if (!hasAnyModelAvailable) return true
    return selectedModels.size > 0
  })
  let currentValid = $derived(wizardStep === 1 ? step1Valid : wizardStep === 2 ? step2Valid : wizardStep === 3 ? step3Valid : true)

  function syncChrome(): void {
    untrack(() => {
      updateTitle(baseTitle)
      updateSubtitle(subtitle)
      updateStepper(stepperConfig)
      if (wizardStep === 4) {
        updateButtons([
          { label: t('Create another'), variant: 'secondary', onClick: resetWizard },
          { label: t('Done'), variant: 'primary', onClick: handleDone }
        ])
        return
      }
      if (wizardStep === 1) {
        updateButtons([
          { label: t('Cancel'), variant: 'secondary', onClick: closeModal },
          { label: t('Next'), variant: 'primary', onClick: nextFromStep1, disabled: !currentValid, loading: wizardLoading }
        ])
      } else if (wizardStep === 2) {
        updateButtons([
          { label: t('Back'), variant: 'secondary', onClick: goBackToStep1 },
          { label: t('Next'), variant: 'primary', onClick: goToStep3, disabled: !currentValid, loading: wizardLoading }
        ])
      } else {
        updateButtons([
          { label: t('Back'), variant: 'secondary', onClick: goBackToStep2 },
          { label: isEditMode ? t('Update token') : t('Create token'), variant: 'primary', onClick: submit, disabled: !currentValid, loading: wizardLoading }
        ])
      }
    })
  }

  function goBackToStep1(): void { wizardStep = 1; error = ''; syncChrome() }
  function goBackToStep2(): void { wizardStep = 2; error = ''; syncChrome() }

  function nextFromStep1(): void {
    error = ''
    if (!tokenName.trim()) { error = t('Token name is required.'); return }
    if (fullAccess) {
      void submit()
      return
    }
    wizardStep = 2; error = ''; syncChrome()
  }

  async function goToStep3(): Promise<void> {
    error = ''
    wizardLoading = true; syncChrome()
    try {
      const scope = new Set(activeProviderIds)
      const [availResult, vmsResult] = await Promise.all([
        api.models.available().catch(() => []),
        includeVirtualModels ? api.virtualModels.list().catch(() => []) : Promise.resolve([])
      ])
      const avail: AvailableModel[] = Array.isArray(availResult) ? availResult : []
      allCandidates = avail
      availableModels = avail.filter((m) => scope.has(m.provider_id))
      const vms: any = vmsResult
      virtualModels = Array.isArray(vms) ? vms.filter((vm: VirtualModel) => !vm.disabled) : []
      const valid = new Set<string>()
      for (const m of availableModels) valid.add(m.full_model_id)
      for (const vm of virtualModels) valid.add(`virtual/${vm.id}`)
      selectedModels = new Set([...selectedModels].filter((id) => valid.has(id)))
      wizardStep = 3; error = ''
    } catch (e) { error = getErrorMessage(e) } finally { wizardLoading = false; syncChrome() }
  }

  function wizardState() {
    return {
      name: tokenName,
      fullAccess,
      allowAllProvidersAccounts,
      providers: activeProviderIds.map((id: string) => ({
        id,
        useAll: id === 'virtual' ? virtualEnabled : allowAllProvidersAccounts || providerUseAll.has(id),
        selected: [...providerCreds].filter((cid) => credsOf(id).some((c) => c.id === cid)),
        known: credsOf(id).map((c) => c.id)
      })),
      allowAllModels,
      models: [...selectedModels]
    }
  }

  async function submit(): Promise<void> {
    error = ''; wizardLoading = true; syncChrome()
    const state = wizardState()
    const payload = {
      name: state.name,
      rules: buildBackendTokenRules(state)
    }
    try {
      if (editingToken) {
        await api.tokens.update(editingToken.id, payload as any)
        onComplete({}); closeModal()
      } else {
        const result: any = await api.tokens.create(payload as any)
        const tokenStr: string | null = result?.token ?? result?.Token ?? result?.token_hash ?? null
        createdToken = tokenStr
        if (tokenStr) { wizardStep = 4; error = '' }
        else { onComplete({ token: tokenStr ?? undefined }); closeModal(); return }
        syncChrome()
      }
    } catch (e) { error = getErrorMessage(e) } finally {
      wizardLoading = false
      syncChrome()
    }
  }

  function handleDone(): void { onComplete({ token: createdToken ?? undefined }); closeModal() }
  function resetWizard(): void {
    tokenName = ''; fullAccess = false
    allowAllProvidersAccounts = false; providerUseAll = new Set(); providerCreds = new Set(); virtualEnabled = false
    availableModels = []; virtualModels = []; allCandidates = []; allowAllModels = false; selectedModels = new Set(); searchModels = ''
    error = ''; createdToken = null
    wizardStep = 1; syncChrome()
  }

  $effect(() => {
    void wizardStep; void tokenName; void fullAccess
    void allowAllProvidersAccounts; void providerUseAll.size; void providerCreds.size; void virtualEnabled
    void allowAllModels; void selectedModels.size; void searchModels
    void wizardLoading; void createdToken; void error; void subtitle; void stepperConfig
    untrack(() => syncChrome())
  })
</script>

{#if wizardStep === 4 && createdToken}
  <TokenSuccessView
    token={createdToken}
    tokenName={tokenName.trim()}
    scopeLabel={{
      providers: fullAccess || allowAllProvidersAccounts ? t('All providers') : n(activeProviderIds.length, 'provider', 'providers', 'провайдер', 'провайдера', 'провайдеров'),
      models: allowAllModels ? t('All models') : n(selectedModels.size, 'model', 'models', 'модель', 'модели', 'моделей'),
      accounts: fullAccess || allowAllProvidersAccounts ? t('All accounts') : n(coveredCredIds.size, 'account', 'accounts', 'аккаунт', 'аккаунта', 'аккаунтов')
    }}
    {error}
  />
{:else if wizardStep === 1}
  {#if error}<Text tone="danger" size="sm">{error}</Text>{/if}
  <VStack gap={4}>
    <TextEdit id="token-name" bind:value={tokenName} hint={`${t('Token name')} (${t('required')})`} />
    <Switch bind:checked={fullAccess} label={t('Full access to everything')} id="full-access" />
  </VStack>
{:else if wizardStep === 2}
  {#if error}<Text tone="danger" size="sm">{error}</Text>{/if}
  <VStack gap={6}>
    <Switch bind:checked={allowAllProvidersAccounts} label={t('Access to all providers and accounts')} id="allow-all-providers-accounts" />
    {#each displayProviders as p (p.id)}
      {@const creds = p.type === 'virtual' ? [] : credsOf(p.id)}
      <div class={allowAllProvidersAccounts ? 'is-disabled' : ''}>
        <VStack gap={2}>
          <HStack align="center" gap={3}>
            <VStack gap={1}>
              <Text tag="h2" size="md" weight="medium">{p.name}</Text>
              <Text size="xs" tone="soft" mono>{p.id}</Text>
            </VStack>
            <Spacer />
            {#if p.type !== 'virtual'}
              <Switch
                checked={providerUseAll.has(p.id)}
                onchange={(v) => toggleProviderUseAll(p.id, v)}
                label={t('Allow all')}
                id={`use-all-${p.id}`}
                disabled={allowAllProvidersAccounts}
              />
            {/if}
            <div class="switch-check-gap" aria-hidden="true"></div>
            <div class="head-check">
              <Checkbox
                checked={providerAllChecked(p.id)}
                onchange={() => toggleProviderAll(p.id)}
                disabled={allowAllProvidersAccounts}
                ariaLabel={p.name}
              />
            </div>
          </HStack>
          {#if p.type !== 'virtual'}
            {#if creds.length === 0}
              <Text size="sm" tone="soft">{t('No accounts for this provider')}</Text>
            {:else}
              <List>
                {#each creds as c (c.id)}
                  <label class="provider-row">
                    <HStack gap={3} align="center">
                      <Text size="base">{c.label || t('API Key')}</Text>
                      <Spacer />
                      <Checkbox
                        checked={providerCreds.has(c.id)}
                        onchange={() => toggleCred(c.id)}
                        disabled={allowAllProvidersAccounts}
                        ariaLabel={c.label || c.id}
                      />
                    </HStack>
                  </label>
                {/each}
              </List>
            {/if}
          {/if}
        </VStack>
      </div>
    {/each}
  </VStack>
{:else if wizardStep === 3}
  {#if error}<Text tone="danger" size="sm">{error}</Text>{/if}
  <VStack gap={4}>
    <Switch bind:checked={allowAllModels} label={t('Allow all models')} id="allow-all-models" />
    <HStack align="center" gap={3}>
      <Text tag="h2" size="md" weight="medium">{t('Models')}</Text>
      <div class="models-search">
        <SearchField bind:value={searchModels} placeholder={t('Search models...')} disabled={allowAllModels} />
      </div>
      <div class="head-check-even">
        <Checkbox
          checked={modelsAllChecked}
          onchange={toggleModelsAll}
          disabled={allowAllModels || filteredTableModels.length === 0}
          ariaLabel={t('Models')}
        />
      </div>
    </HStack>
    <div class={allowAllModels ? 'is-disabled' : ''}>
      <ModelsTable models={filteredTableModels} loading={wizardLoading}>
        {#snippet actions({ model })}
          {@const fid = modelFullId(model as ModelsTableModel)}
          <Checkbox
            checked={selectedModels.has(fid)}
            onchange={() => toggleModel(fid)}
            disabled={allowAllModels}
            ariaLabel={(model as ModelsTableModel).fullId ?? (model as ModelsTableModel).id}
          />
        {/snippet}
        {#snippet empty()}
          <VStack align="center" gap={2}>
            <Text size="sm" tone="soft">{t('No models available. Select providers on the previous step.')}</Text>
          </VStack>
        {/snippet}
      </ModelsTable>
    </div>
  </VStack>
{/if}

<style>
  .provider-row { display: block; padding: var(--space-4) var(--space-5); cursor: pointer; }
  .head-check { margin-right: var(--space-5); display: grid; place-content: center; }
  .head-check-even { margin: 0 var(--space-5); display: grid; place-content: center; }
  .switch-check-gap { flex: 0 0 30%; }
  .models-search { flex: 1 1 220px; min-width: 0; }
</style>
