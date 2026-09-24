<script lang="ts">
  import { api } from '../../lib/api'
  import { modal } from '../../lib/modal.svelte'
  import { getErrorMessage } from '../../lib/errors'
  import { toast } from '../../lib/toast.svelte'
  import type { Credential, Provider, Proxy, TestResult } from '../../lib/types'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import ProviderCredentialWizard from './components/ProviderCredentialWizard.svelte'
  import ModelsSection from './components/ModelsSection.svelte'
  import { Button, Chip, HStack, Image, Picker, Spacer, Switch, Table, Text, TextEdit, VStack } from '../../components/ui'
  import type { TableColumn } from '../../components/ui'
  import { squircle } from '../../lib/squircle'
  import { t } from '../../lib/i18n.svelte'

  let { providerId } = $props<{ providerId: string }>()

  let provider = $state<Provider | null>(null)
  let loading = $state(true)
  let error = $state('')

  let credentials = $state<Credential[]>([])
  let credentialsLoading = $state(false)
  let credentialTestResults = $state<Record<string, TestResult | 'loading'>>({})
  const resultTimers = new Map<string, ReturnType<typeof setTimeout>>()

  let editingCred = $state<Credential | null>(null)
  let editingCredLabel = $state('')
  let savingCred = $state(false)
  let editCredError = $state('')

  let proxyMode = $state<'disabled' | 'auto' | 'manual'>('disabled')
  let proxyIds = $state<Record<string, boolean>>({})
  let poolManual = $state<Proxy[]>([])
  let savingProxy = $state(false)

  const providerIdValue = $derived(provider?.id ?? '')

  const credentialColumns: TableColumn[] = [
    { key: 'priority', title: '#', width: '72px', align: 'center' },
    { key: 'name', title: t('Name'), width: '1fr' },
    { key: 'actions', title: t('Actions'), width: 'auto', align: 'right' },
  ]

  $effect(() => {
    void providerId
    void loadPage()
  })

  $effect(() => {
    if (!provider) return
    void reloadCredentials()
    initProxyConfig()
    void loadProxyPool()
  })

  async function loadPage(): Promise<void> {
    loading = true
    error = ''
    try {
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? null
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function toggleProviderEnabled(enabled: boolean): Promise<void> {
    if (!provider) return
    error = ''
    try {
      await api.providers.updateInstance(provider.id, { name: provider.name, disabled: !enabled })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerId) ?? provider
      toast.success(enabled ? `${provider.name} enabled` : `${provider.name} disabled`)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function back(): void {
    window.location.hash = '#/providers'
  }

  function openEditProvider(): void {
    if (!provider) return
    modal.open({
      title: t('Edit Provider'),
      content: CustomProviderWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        editingProvider: provider,
        onComplete: async () => {
          modal.close()
          await loadPage()
        },
      },
    })
  }

  async function deleteProvider(): Promise<void> {
    if (!provider) return
    const confirmed = await modal.confirm({
      title: t('Delete Provider'),
      message: `${t('Are you sure you want to delete')} "${provider.name}"? ${t('Credentials for this provider will be removed as well.')}`,
      severity: 'high',
      confirmText: t('Delete'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    try {
      await api.providers.delete(provider.id)
      back()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function sortCredentials(list: Credential[]): Credential[] {
    return [...list].sort((a, b) => {
      const oa = a.order ?? 0
      const ob = b.order ?? 0
      if ((oa > 0) !== (ob > 0)) return oa > 0 ? -1 : 1
      if (oa > 0 && ob > 0 && oa !== ob) return oa - ob
      return 0
    })
  }

  async function reloadCredentials(): Promise<void> {
    if (!providerIdValue) return
    credentialsLoading = true
    try {
      const creds = await api.credentials.list()
      credentials = sortCredentials((creds as Credential[]).filter((c) => c.provider_id === providerIdValue))
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      credentialsLoading = false
    }
  }

  function openAddCredential(): void {
    if (!provider) return
    modal.open({
      title: `Add Credential · ${provider.name}`,
      content: ProviderCredentialWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        provider,
        onComplete: async () => {
          modal.close()
          await reloadCredentials()
        },
      },
    })
  }

  function openEditCredential(cred: Credential): void {
    editingCred = cred
    editingCredLabel = cred.label || ''
    editCredError = ''
  }

  async function saveCredentialLabel(): Promise<void> {
    if (!editingCred) return
    savingCred = true
    editCredError = ''
    try {
      await api.credentials.update(editingCred.id, { label: editingCredLabel.trim() })
      editingCred = null
      await reloadCredentials()
    } catch (e) {
      editCredError = getErrorMessage(e)
    } finally {
      savingCred = false
    }
  }

  async function deleteCredential(cred: Credential): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Delete key'),
      message: `${t('Are you sure you want to delete')} "${cred.label || t('Unnamed')}"? ${t('This action cannot be undone.')}`,
      severity: 'medium',
      confirmText: t('Delete'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    try {
      await api.credentials.delete(cred.id)
      credentials = credentials.filter((c) => c.id !== cred.id)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function toggleCredential(cred: Credential, enabled: boolean): Promise<void> {
    try {
      await api.credentials.update(cred.id, { disabled: !enabled })
      await reloadCredentials()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function flashTimer(key: string, clear: () => void): void {
    const old = resultTimers.get(key)
    if (old) clearTimeout(old)
    resultTimers.set(key, setTimeout(clear, 3000))
  }

  async function testCredential(cred: Credential): Promise<void> {
    credentialTestResults = { ...credentialTestResults, [cred.id]: 'loading' }
    let res: TestResult
    try {
      res = await api.credentials.test(cred.id)
    } catch (e) {
      res = { ok: false, latency_ms: 0, error: getErrorMessage(e) }
    }
    credentialTestResults = { ...credentialTestResults, [cred.id]: res }
    if (res.ok) toast.success(`Key "${cred.label || 'Unnamed'}" works · ${res.latency_ms}ms`)
    else toast.error(`Key "${cred.label || 'Unnamed'}" failed: ${res.error}`)
    flashTimer(`cred:${cred.id}`, () => {
      const next = { ...credentialTestResults }
      delete next[cred.id]
      credentialTestResults = next
    })
  }

  function testIcon(res: TestResult | 'loading' | undefined, idleTitle: string): { icon: string; title: string } {
    if (!res) return { icon: 'network_check', title: idleTitle }
    if (res === 'loading') return { icon: 'progress_activity', title: t('Testing…') }
    if (res.ok) return { icon: 'check_circle', title: `OK · ${res.latency_ms}ms` }
    return { icon: 'error', title: res.error ?? t('Failed') }
  }

  async function reorderCredentials(from: number, to: number): Promise<void> {
    if (!provider) return
    const next = [...credentials]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    credentials = next
    try {
      await api.credentials.reorder(provider.id, next.map((c) => c.id))
      await reloadCredentials()
    } catch (e) {
      error = getErrorMessage(e)
      await reloadCredentials()
    }
  }

  function initProxyConfig(): void {
    const raw = (provider?.config?.proxy ?? {}) as { mode?: string; ids?: string[] }
    proxyMode = raw.mode === 'auto' || raw.mode === 'manual' ? raw.mode : 'disabled'
    proxyIds = {}
    for (const id of raw.ids ?? []) proxyIds[id] = true
  }

  async function loadProxyPool(): Promise<void> {
    try {
      poolManual = (await api.proxies.list()).filter((p: Proxy) => p.source === 'manual')
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function saveProxyConfig(): Promise<void> {
    if (!provider) return
    savingProxy = true
    error = ''
    try {
      const ids = Object.keys(proxyIds).filter((id) => proxyIds[id])
      await api.providers.updateInstance(provider.id, {
        name: provider.name,
        config: {
          ...(provider.config ?? {}),
          proxy: { mode: proxyMode, ...(proxyMode === 'manual' ? { ids } : {}) },
        },
      })
      const providers = await api.providers.list()
      provider = (providers as Provider[]).find((p) => p.id === providerIdValue) ?? provider
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      savingProxy = false
    }
  }
</script>

{#if loading}
  <VStack align="center" gap={2} class="provider-state">
    <Text tone="soft">{t('Loading…')}</Text>
  </VStack>
{:else if !provider}
  <VStack align="center" gap={4} class="provider-state">
    <Text tone="soft">{t('Provider not found.')}</Text>
    <Button onclick={back}>{t('Back to providers')}</Button>
  </VStack>
{:else}
  <VStack gap={6} class="provider-detail">
    <HStack gap={4} align="center" class="detail-header">
      <Button onclick={back} ariaLabel={t('Back to providers')} title={t('Back to providers')} icon={{ name: 'arrow_back' }} />
      <Image src={provider.icon_url} alt={provider.name} width={40} height={40} radius={3} fit="contain" fallbackIcon="cloud" />
      <VStack gap={0} grow class={provider.disabled ? 'provider-off' : ''}>
        <Text tag="h1" size="lg" weight="bold">{provider.name}</Text>
        <Text tone="soft" size="sm">{provider.type_key}</Text>
      </VStack>
      <Spacer />
      {#if !provider.is_ui_readonly}
        <HStack gap={2}>
          <Button onclick={openEditProvider} icon={{ name: 'edit' }}>{t('Edit')}</Button>
          <Button tint="#dc2626" onclick={deleteProvider} icon={{ name: 'delete' }}>{t('Delete')}</Button>
        </HStack>
      {/if}
      <Switch
        checked={!provider.disabled}
        ariaLabel={t('Enable provider')}
        size="xl"
        onchange={(v) => toggleProviderEnabled(v)}
      />
    </HStack>

    {#if error}
      <VStack class="error-msg" gap={0}>
        <Text tone="danger" size="sm">{error}</Text>
      </VStack>
    {/if}

    <VStack tag="section" gap={4} class="provider-section">
      <HStack align="center" gap={2}>
        <Text tag="h2" size="md" weight="medium">{t('API Keys')}</Text>
        <Text size="xs" tone="soft">{credentials.length}</Text>
        <Spacer />
        <Button style="prominent" onclick={openAddCredential} icon={{ name: 'add' }}>{t('Add key')}</Button>
      </HStack>
      <Table
        columns={credentialColumns}
        rows={credentials}
        rowKey={(cred) => cred.id}
        loading={credentialsLoading}
        draggable
        onReorder={(from, to) => void reorderCredentials(from, to)}
        rowClass={(cred) => cred.disabled ? 'row-off' : ''}
      >
        {#snippet cell({ column, row })}
          {@const cred = row as Credential}
          {#if column.key === 'priority'}
            <Text size="sm">{credentials.indexOf(cred) + 1}</Text>
          {:else if column.key === 'name'}
            <HStack gap={2} align="center" wrap>
              <Text size="base" weight="medium">{cred.label || t('Unnamed')}</Text>
              {#if cred.is_expired}
                <Chip text={t('Expired')} color="chip-red" />
              {/if}
            </HStack>
          {:else}
            {@const ti = testIcon(credentialTestResults[cred.id], t('Test key'))}
            <HStack gap={3} justify="end">
              <Button
                size="small"
                icon={{ name: ti.icon }}
                style="text"
                disabled={credentialTestResults[cred.id] === 'loading'}
                ariaLabel={ti.title}
                title={ti.title}
                onclick={() => testCredential(cred)}
              />
              <Button size="small" style="text" icon={{ name: 'edit' }} ariaLabel={t('Edit key')} title={t('Edit key')} onclick={() => openEditCredential(cred)} />
              <Button size="small" tint="#dc2626" style="text" icon={{ name: 'delete' }} ariaLabel={t('Delete key')} title={t('Delete key')} onclick={() => deleteCredential(cred)} />
              <Switch
                checked={!cred.disabled}
                ariaLabel={t('Enable key')}
                onchange={(v) => toggleCredential(cred, v)}
              />
            </HStack>
          {/if}
        {/snippet}
        {#snippet empty()}
          <VStack align="center" gap={2} class="table-empty">
            <Text size="sm" tone="soft">{t('No keys yet. Add one to route traffic to this provider.')}</Text>
          </VStack>
        {/snippet}
      </Table>
    </VStack>

    <VStack tag="section" gap={4} align="start" class="provider-section">
      <HStack align="center" gap={3}>
        <Text tag="h2" size="md" weight="medium">{t('Proxy')}</Text>
        <Picker
          bind:value={proxyMode}
          options={[
            { value: 'disabled', label: t('Disabled') },
            { value: 'auto', label: t('Auto') },
            { value: 'manual', label: t('Manual') },
          ]}
          ariaLabel={t('Proxy mode')}
          onchange={() => void saveProxyConfig()}
        />
        {#if savingProxy}
          <Text size="xs" tone="soft">{t('Saving…')}</Text>
        {/if}
      </HStack>
      <Text size="sm" tone="soft">
        {#if proxyMode === 'disabled'}
          {t('Direct connection, no proxying.')}
        {:else if proxyMode === 'auto'}
          {t('Route through the fastest pooled proxy matching the plugin locations.')}
        {:else}
          {t('Route through the proxies you select below (first usable wins).')}
        {/if}
      </Text>
      {#if proxyMode === 'manual'}
        {#if poolManual.length === 0}
          <Text size="sm" tone="soft">{t('No manual proxies in the pool. Add them on the Proxies page.')}</Text>
        {:else}
          <HStack wrap align="start" gap={2}>
            {#each poolManual as p (p.id)}
              <Button
                size="small"
                style={proxyIds[p.id] ? 'prominent' : 'none'}
                title={p.url}
                ariaLabel={p.url}
                onclick={() => {
                  proxyIds = { ...proxyIds, [p.id]: !proxyIds[p.id] }
                  void saveProxyConfig()
                }}
              >{`${p.url}${p.location ? ` · ${p.location}` : ''}`}</Button>
            {/each}
          </HStack>
        {/if}
      {/if}
    </VStack>

    <ModelsSection bind:provider onrefresh={loadPage} />
  </VStack>
{/if}

{#if editingCred}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div class="modal-backdrop" onclick={() => { editingCred = null }} role="presentation">
    <div
      class="modal-card modal-small"
      role="dialog"
      aria-modal="true"
      onclick={(e) => e.stopPropagation()}
      use:squircle
    >
      <div class="modal-header">
        <div class="modal-title-col">
          <h2>{t('Edit Key')}</h2>
        </div>
        <button class="btn-icon modal-close" onclick={() => { editingCred = null }} aria-label={t('Close')}>
          <span class="icon">close</span>
        </button>
      </div>
      <div class="modal-body">
        <VStack gap={4}>
          {#if editCredError}
            <Text tone="danger" size="sm">{editCredError}</Text>
          {/if}
          <VStack gap={1}>
            <Text size="sm" weight="medium">{t('Name')}</Text>
            <TextEdit
              id="cred-edit-label"
              bind:value={editingCredLabel}
              hint={t('e.g. Work account')}
              onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') void saveCredentialLabel() }}
            />
          </VStack>
        </VStack>
      </div>
      <div class="modal-footer footer-bordered">
        <Button onclick={() => { editingCred = null }}>{t('Cancel')}</Button>
        <Button style="prominent" onclick={saveCredentialLabel} disabled={savingCred}>
          {savingCred ? t('Saving…') : t('Save')}
        </Button>
      </div>
    </div>
  </div>
{/if}

<style>
  :global(.provider-detail),
  :global(.provider-section) {
    width: 100%;
  }

  :global(.provider-state) {
    min-height: 240px;
    justify-content: center;
  }

  :global(.provider-off) {
    opacity: 0.55;
  }

  :global(.error-msg) {
    background: var(--color-notification-error-bg);
    padding: var(--space-4) var(--space-5);
    border-radius: var(--radius-md);
  }

  :global(.table-empty) {
    width: 100%;
    padding: var(--space-4);
  }

  @media (max-width: 768px) {
    :global(.detail-header) {
      flex-wrap: wrap;
    }
  }
</style>
