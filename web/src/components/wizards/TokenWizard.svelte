<script lang="ts">
  import { onMount } from 'svelte'
  import { untrack } from 'svelte'
  import { api } from '../../lib/api'
  import { getErrorMessage } from '../../lib/errors'
  import type { Token, Provider, ProviderModels, ModalButton } from '../../lib/types'

  let {
    providers,
    editingToken = null,
    cloningToken = null,
    onComplete,
    updateButtons,
    updateTitle,
    closeModal
  } = $props<{
    providers: Provider[]
    editingToken?: Token | null
    cloningToken?: Token | null
    onComplete: (result: { token?: string }) => void
    updateButtons: (buttons: ModalButton[]) => void
    updateTitle: (title: string) => void
    closeModal: () => void
  }>()

  let wizardStep: number = $state(1)
  let wizardLoading: boolean = $state(false)
  let error: string = $state('')

  let tokenName: string = $state('')
  let allowAllProviders: boolean = $state(false)
  let selectedProviders: Set<string> = $state(new Set())
  let providerModels: ProviderModels[] = $state([])
  let allowAllModels: boolean = $state(false)
  let selectedModels: Set<string> = $state(new Set())
  let allowAllCredentials: boolean = $state(false)
  let selectedCredentials: Set<string> = $state(new Set())
  let allCredentials: Array<{ id: string; provider_id: string; provider_name: string; label: string; is_expired: boolean }> = $state([])

  onMount(async () => {
    const source = editingToken ?? cloningToken
    if (source) {
      const r: any = source.rules || {}
      tokenName = cloningToken ? `${source.name} (copy)` : source.name
      allowAllProviders = !!r.allow_all_providers
      selectedProviders = new Set(r.allowed_providers || [])
      allowAllModels = !!r.allow_all_models
      selectedModels = new Set(r.allowed_models || [])
      allowAllCredentials = !!r.allow_all_credentials
      selectedCredentials = new Set(r.allowed_credentials || [])
    }

    try {
      const creds: any = await api.credentials.list()
      allCredentials = Array.isArray(creds) ? creds : (creds?.credentials ?? creds ?? [])
      if (!Array.isArray(allCredentials)) allCredentials = []
    } catch (_) {
      allCredentials = []
    }

    syncButtons()
  })

  function getEligibleProviderIds(): Set<string> {
    if (!allowAllProviders) return new Set(selectedProviders)
    if (!allowAllModels && selectedModels.size > 0) {
      const types = new Set<string>()
      for (const m of selectedModels) {
        const pre = m.split('/')[0]
        if (pre) types.add(pre)
      }
      const ids = providers.filter((p: Provider) => types.has(p.type)).map((p: Provider) => p.id)
      if (ids.length > 0) return new Set(ids)
    }
    return new Set(providers.map((p: Provider) => p.id))
  }

  let eligibleProviderIds = $derived(getEligibleProviderIds())
  let eligibleProviders = $derived(providers.filter((p: Provider) => eligibleProviderIds.has(p.id)))
  let eligibleCredentialsGrouped = $derived(eligibleProviders.map((p: Provider) => ({
    provider: p,
    creds: allCredentials.filter(c => c.provider_id === p.id)
  })))

  function syncButtons(): void {
    const title = editingToken ? 'Edit token' : cloningToken ? 'Clone token' : 'New token'
    // Parent callbacks mutate modal stack ($state) — untrack so this write is not tracked as a dependency of any $effect
    // Prevents effect_update_depth_exceeded (Svelte 5: $effect for side effects only, parent writes must be untracked)
    untrack(() => {
      if (wizardStep === 1) {
        updateTitle(`${title} · Step 1 of 3`)
        updateButtons([
          {
            label: 'Cancel',
            variant: 'secondary',
            onClick: closeModal
          },
          {
            label: 'Next',
            variant: 'primary',
            onClick: goToStep2,
            disabled: !tokenName.trim() || (!allowAllProviders && selectedProviders.size === 0),
            loading: wizardLoading
          }
        ])
      } else if (wizardStep === 2) {
        updateTitle(`${title} · Step 2 of 3`)
        updateButtons([
          {
            label: 'Back',
            variant: 'secondary',
            onClick: goBackToStep1
          },
          {
            label: 'Next',
            variant: 'primary',
            onClick: goToStep3,
            disabled: !allowAllModels && selectedModels.size === 0,
            loading: wizardLoading
          }
        ])
      } else {
        updateTitle(`${title} · Step 3 of 3`)
        updateButtons([
          {
            label: 'Back',
            variant: 'secondary',
            onClick: goBackToStep2
          },
          {
            label: editingToken ? 'Update token' : 'Create token',
            variant: 'primary',
            onClick: submit,
            disabled: !allowAllCredentials && selectedCredentials.size === 0,
            loading: wizardLoading
          }
        ])
      }
    })
  }

  function goBackToStep1(): void {
    wizardStep = 1
    error = ''
    syncButtons()
  }

  function goBackToStep2(): void {
    wizardStep = 2
    error = ''
    syncButtons()
  }

  function toggleProvider(id: string): void {
    const s = new Set(selectedProviders)
    s.has(id) ? s.delete(id) : s.add(id)
    selectedProviders = s
    syncButtons()
  }

  function toggleModel(fullId: string): void {
    const s = new Set(selectedModels)
    s.has(fullId) ? s.delete(fullId) : s.add(fullId)
    selectedModels = s
    syncButtons()
  }

  function toggleCredential(id: string): void {
    const s = new Set(selectedCredentials)
    s.has(id) ? s.delete(id) : s.add(id)
    selectedCredentials = s
    syncButtons()
  }

  async function goToStep2(): Promise<void> {
    error = ''

    if (!tokenName.trim()) {
      error = 'Token name is required.'
      return
    }

    if (!allowAllProviders && selectedProviders.size === 0) {
      error = 'Select at least one provider or allow all.'
      return
    }

    wizardLoading = true
    syncButtons()

    try {
      const ids = allowAllProviders ? providers.map((p: Provider) => p.id) : [...selectedProviders]
      if (ids.length === 0) {
        providerModels = []
      } else {
        const result: any = await api.models.list(ids)
        providerModels = result?.providers || result?.data || []
        if (!Array.isArray(providerModels)) providerModels = []
      }
      const availableTypes = new Set(providerModels.map((p: any) => p.provider_type))
      for (const m of [...selectedModels]) {
        const t = m.split('/')[0]
        if (!availableTypes.has(t)) selectedModels.delete(m)
      }
      selectedModels = new Set(selectedModels)
      wizardStep = 2
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      wizardLoading = false
      syncButtons()
    }
  }

  async function goToStep3(): Promise<void> {
    error = ''
    if (!allowAllModels && selectedModels.size === 0) {
      error = 'Select at least one model or allow all.'
      return
    }
    wizardLoading = true
    syncButtons()
    try {
      if (allCredentials.length === 0) {
        try {
          const creds: any = await api.credentials.list()
          allCredentials = Array.isArray(creds) ? creds : []
        } catch (_) {}
      }
      const eligible = getEligibleProviderIds()
      for (const cid of [...selectedCredentials]) {
        const cred = allCredentials.find(c => c.id === cid)
        if (!cred || !eligible.has(cred.provider_id)) selectedCredentials.delete(cid)
      }
      selectedCredentials = new Set(selectedCredentials)
      wizardStep = 3
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      wizardLoading = false
      syncButtons()
    }
  }

  async function submit(): Promise<void> {
    error = ''
    wizardLoading = true
    syncButtons()

    const payload = {
      name: tokenName,
      rules: {
        allowed_providers: allowAllProviders ? null : [...selectedProviders],
        allow_all_providers: allowAllProviders,
        allowed_models: allowAllModels ? null : [...selectedModels],
        allow_all_models: allowAllModels,
        allowed_credentials: allowAllCredentials ? null : [...selectedCredentials],
        allow_all_credentials: allowAllCredentials,
      },
    }

    try {
      if (editingToken) {
        await api.tokens.update(editingToken.id, payload as any)
        onComplete({})
      } else {
        const result: any = await api.tokens.create(payload as any)
        onComplete({ token: result?.token ?? result?.Token ?? result?.token_hash })
      }
    } catch (e) {
      error = getErrorMessage(e)
      wizardLoading = false
      syncButtons()
    }
  }

  // Svelte 5: $effect is for side effects only; use $derived for computed values.
  // The effect syncs footer buttons with form state. Parent writes (updateTitle/updateButtons mutate modal stack)
  // must be untracked to prevent effect_update_depth_exceeded.
  $effect(() => {
    void wizardStep
    void tokenName
    void allowAllProviders
    void selectedProviders.size
    void allowAllModels
    void selectedModels.size
    void allowAllCredentials
    void selectedCredentials.size
    void wizardLoading
    untrack(() => syncButtons())
  })
</script>

{#if wizardStep === 1}
  <div class="form-group">
    <label for="token-name">Token name</label>
    <input id="token-name" type="text" bind:value={tokenName} placeholder="My Application" />
    {#if error && !tokenName.trim()}
      <span class="field-error">{error}</span>
    {/if}
  </div>

  <div class="form-group">
    <label class="checkbox-item">
      <input type="checkbox" bind:checked={allowAllProviders} onchange={syncButtons} />
      <span>Allow all providers</span>
    </label>
    {#if allowAllProviders}
      <div class="hint">All providers are allowed.</div>
    {/if}
  </div>

  {#if !allowAllProviders}
    <div class="form-group">
      <div class="form-label">Providers</div>
      <div class="checkbox-list">
        {#each providers as p}
          <label class="checkbox-item">
            <input type="checkbox" checked={selectedProviders.has(p.id)} onchange={() => toggleProvider(p.id)} />
            <span>{p.name} <span class="text-muted">({p.type}{p.qualifier ? ':' + p.qualifier : ''})</span></span>
          </label>
        {/each}
      </div>
      {#if error && selectedProviders.size === 0}
        <span class="field-error">{error}</span>
      {:else if !allowAllProviders && selectedProviders.size === 0}
        <span class="field-error">Select at least one provider or enable Allow all.</span>
      {/if}
    </div>
  {/if}

{:else if wizardStep === 2}
  {#if error}
    <div class="error-msg" style="color: var(--color-error-text);">{error}</div>
  {/if}

  <div class="form-group">
    <label class="checkbox-item">
      <input type="checkbox" bind:checked={allowAllModels} onchange={syncButtons} />
      <span>Allow all models</span>
    </label>
    {#if allowAllModels}
      <div class="hint">All models of the selected providers are allowed.</div>
    {/if}
  </div>

  {#if !allowAllModels}
    <div class="model-sections">
      {#each providerModels as pm}
        <div class="model-section">
          <div class="section-label">
            {pm.provider_name}
            {#if pm.error}
              <span class="badge badge-red">{pm.error}</span>
            {/if}
          </div>
          {#if pm.models?.length}
            <div class="checkbox-list">
              {#each pm.models as model}
                {@const fullId = `${pm.provider_type}/${model}`}
                <label class="checkbox-item">
                  <input type="checkbox" checked={selectedModels.has(fullId)} onchange={() => toggleModel(fullId)} />
                  <span class="mono">{model}</span>
                </label>
              {/each}
            </div>
          {:else if !pm.error}
            <div class="empty-state">No models available</div>
          {/if}
        </div>
      {/each}
      {#if providerModels.length === 0}
        <div class="empty-state">No providers selected — go back and select providers.</div>
      {:else if !allowAllModels && selectedModels.size === 0}
        <span class="field-error">Select at least one model or enable Allow all.</span>
      {/if}
    </div>
  {/if}

{:else if wizardStep === 3}
  {#if error}
    <div class="error-msg" style="color: var(--color-error-text);">{error}</div>
  {/if}

  <div class="form-group">
    <label class="checkbox-item">
      <input type="checkbox" bind:checked={allowAllCredentials} onchange={syncButtons} />
      <span>Allow all accounts</span>
    </label>
    {#if allowAllCredentials}
      <div class="hint">All accounts of the eligible providers are allowed.</div>
    {/if}
  </div>

  {#if !allowAllCredentials}
    {#if allCredentials.length === 0}
      <div class="empty-state">No accounts registered yet.</div>
    {:else if eligibleCredentialsGrouped.length === 0}
      <div class="empty-state">No accounts match the selected providers/models.</div>
    {:else}
      <div class="model-sections">
        {#each eligibleCredentialsGrouped as group}
          <div class="model-section">
            <div class="section-label">{group.provider.name} <span class="text-muted">({group.provider.id})</span></div>
            {#if group.creds.length === 0}
              <div class="empty-state">No accounts for this provider</div>
            {:else}
              <div class="checkbox-list">
                {#each group.creds as cred}
                  <label class="checkbox-item">
                    <input type="checkbox" checked={selectedCredentials.has(cred.id)} onchange={() => toggleCredential(cred.id)} />
                    <span>{cred.label} <span class="text-muted">({cred.id.slice(0, 8)}…)</span> {#if cred.is_expired}<span class="badge badge-red">expired</span>{/if}</span>
                  </label>
                {/each}
              </div>
            {/if}
          </div>
        {/each}
      </div>
      {#if !allowAllCredentials && selectedCredentials.size === 0}
        <span class="field-error">Select at least one account or enable Allow all.</span>
      {/if}
    {/if}
  {/if}
{/if}

<style>
  .form-group {
    margin-bottom: 16px;
  }

  .form-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
    margin-bottom: 6px;
  }

  .checkbox-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
    gap: 8px 16px;
    margin-top: 8px;
  }

  .checkbox-item {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    cursor: pointer;
    user-select: none;
  }

  .checkbox-item input[type="checkbox"] {
    width: auto;
    cursor: pointer;
  }

  .text-muted {
    color: var(--color-text-soft);
  }

  .field-error {
    display: block;
    margin-top: 6px;
    font-size: 12px;
    color: #dc2626;
  }

  .hint {
    margin-top: 6px;
    font-size: 12px;
    color: var(--color-text-soft);
  }

  .model-sections {
    display: flex;
    flex-direction: column;
    gap: 24px;
    margin-top: 16px;
  }

  .model-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .section-label {
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text);
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .empty-state {
    padding: 16px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
</style>
