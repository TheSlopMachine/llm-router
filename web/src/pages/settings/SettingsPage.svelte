<script lang="ts">
  import { onMount } from 'svelte'
  import { theme } from '$lib/theme.svelte'
  import type { Theme } from '$lib/theme.svelte'
  import { language } from '$lib/language.svelte'
  import type { Language } from '$lib/language.svelte'
  import { api } from '$lib/api'
  import { t, n } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { accent, accents } from '$lib/accent.svelte'
  import { Button, HStack, SectionCard, Select, Spacer, Switch, Text, VStack, TextEdit, Banner, Icon, Chip, Header, EmptyState } from '$ui'
  import { squircle } from '../../FUI/core/squircle'
  import type { Provider, SubsystemStats, DoctorReport } from '$lib/types'
  import { toast } from '../../FUI/core/toast.svelte'
  import { downloadJson, readJsonFile } from '$lib/download'
  import { modal } from '../../FUI/core/modal.svelte'

  let languageOptions = $derived([
    { value: 'auto', label: t('common.auto') },
    { value: 'en', label: t('settings.language.english') },
    { value: 'ru', label: t('settings.language.russian') }
  ])
  let themeOptions: Array<{ value: string; label: string }> = $derived([
    { value: 'auto', label: t('common.auto') },
    { value: 'light', label: t('settings.theme.light') },
    { value: 'dark', label: t('settings.theme.dark') }
  ])

  // Preferences
  let lang = $state<Language>(language.value)
  let th = $state<Theme>(theme.value)

  // Config
  let isClusterNode = $state(false)
  let disableTelemetry = $state(false)

  // Security
  let currentPassword = $state('')
  let newPassword = $state('')
  let confirmPassword = $state('')
  let passwordSaving = $state(false)
  let passwordError = $state('')
  let isPasswordFormValid = $derived(!!currentPassword && !!newPassword && !passwordSaving)

  // Data Management
  let stats = $state<SubsystemStats | null>(null)
  let statsLoading = $state(false)
  let importFileMap = $state<Record<string, File | null>>({})

  let allProviders = $state<Provider[]>([])
  let providerOptions = $derived(allProviders.map(p => ({ value: p.id, label: p.name })))
  let selectedProviderId = $state('')
  let providerImportFile = $state<File | null>(null)
  let providersLoading = $state(false)

  // Database Doctor
  let doctorReport = $state<DoctorReport | null>(null)
  let doctorLoading = $state(false)
  let doctorFixing = $state(false)

  // General state
  let saving = $state(false)
  let error = $state('')

  onMount(async () => {
    await Promise.all([loadConfig(), loadDataStats(), runDoctorInspect(), loadProviders()])
  })

  async function loadProviders(): Promise<void> {
    providersLoading = true
    try {
      allProviders = await api.providers.list()
      if (allProviders.length > 0) {
        selectedProviderId = allProviders[0].id
      }
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      providersLoading = false
    }
  }

  async function loadConfig(): Promise<void> {
    try {
      const cfg = await api.config.get()
      isClusterNode = cfg.is_cluster_node
      disableTelemetry = cfg.disable_telemetry
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function loadDataStats(): Promise<void> {
    statsLoading = true
    try {
      stats = await api.data.stats()
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      statsLoading = false
    }
  }

  async function runDoctorInspect(): Promise<void> {
    doctorLoading = true
    try {
      doctorReport = await api.doctor.inspect()
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      doctorLoading = false
    }
  }

  async function save(): Promise<void> {
    if (saving) return
    saving = true
    error = ''
    try {
      language.value = lang
      theme.value = th
      await api.config.update({
        is_cluster_node: isClusterNode,
        disable_telemetry: disableTelemetry
      })
      toast.success(t('settings.saved'))
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }

  async function updatePassword(): Promise<void> {
    if (passwordSaving) return
    passwordError = ''
    if (newPassword !== confirmPassword) {
      passwordError = t('settings.password.mismatch')
      return
    }
    passwordSaving = true
    try {
      await api.admin.changePassword(currentPassword, newPassword)
      toast.success(t('settings.password.success'))
      currentPassword = ''
      newPassword = ''
      confirmPassword = ''
    } catch (e) {
      passwordError = getErrorMessage(e)
    } finally {
      passwordSaving = false
    }
  }

  // Data Management actions
  async function exportSubsystem(sub: string): Promise<void> {
    try {
      const data = await api.data.exportSubsystem(sub)
      downloadJson(`llm_router_${sub}_export.json`, data)
      toast.success(`${sub} exported successfully`)
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function handleFileChange(sub: string, event: Event): Promise<void> {
    const input = event.target as HTMLInputElement
    const file = input.files?.[0] ?? null
    importFileMap = { ...importFileMap, [sub]: file }
  }

  async function importSubsystem(sub: string): Promise<void> {
    const file = importFileMap[sub]
    if (!file) {
      toast.error(t('data.select_file_first'))
      return
    }
    try {
      const json = await readJsonFile(file)
      await api.data.importSubsystem(sub, json)
      toast.success(`${sub} imported successfully`)
      importFileMap = { ...importFileMap, [sub]: null }
      await Promise.all([loadDataStats(), runDoctorInspect()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function clearSubsystem(sub: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('data.clear_subsystem'),
      message: t('data.clear_confirm'),
      confirmText: t('data.clear'),
      confirmRole: 'destructive'
    })
    if (!confirmed) return
    try {
      await api.data.clearSubsystem(sub)
      toast.success(`${sub} cleared successfully`)
      await Promise.all([loadDataStats(), runDoctorInspect()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  // Database Doctor actions
  async function fixIssues(): Promise<void> {
    if (doctorFixing) return
    doctorFixing = true
    try {
      const res = await api.doctor.fix([])
      toast.success(`${res.fixed} ${t('data.doctor.resolved')}`)
      await Promise.all([loadDataStats(), runDoctorInspect()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      doctorFixing = false
    }
  }

  // Individual Provider Management actions
  async function exportIndividualProvider(providerId: string): Promise<void> {
    try {
      const data = await api.data.exportProvider(providerId)
      downloadJson(`llm_router_provider_${providerId}_export.json`, data)
      toast.success(t('providers.export.success'))
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function handleIndividualProviderFileChange(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement
    providerImportFile = input.files?.[0] ?? null
  }

  async function importIndividualProvider(providerId: string): Promise<void> {
    if (!providerImportFile) {
      toast.error(t('data.select_file_first'))
      return
    }
    try {
      const json = await readJsonFile(providerImportFile)
      await api.data.importProvider(providerId, json as never)
      toast.success(t('providers.import.success'))
      providerImportFile = null
      await Promise.all([loadDataStats(), runDoctorInspect(), loadProviders()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function purgeIndividualProvider(providerId: string): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('data.purge_provider'),
      message: t('data.purge_confirm'),
      confirmText: t('data.purge'),
      confirmRole: 'destructive'
    })
    if (!confirmed) return
    try {
      await api.data.purgeProvider(providerId)
      toast.success(t('providers.purge.success'))
      await Promise.all([loadDataStats(), runDoctorInspect(), loadProviders()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

</script>

<VStack gap={6}>
  <Header title={t('settings.title')} subtitle={t('settings.subtitle')} />

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  <SectionCard title={t('settings.preferences')} description={t('settings.preferences.desc')}>
    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="settings-language">{t('settings.language.label')}</Text>
      <Select value={lang} options={languageOptions} onchange={(v) => (lang = v as Language)} />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="settings-theme">{t('settings.theme.label')}</Text>
      <Select value={th} options={themeOptions} onchange={(v) => (th = v as Theme)} />
    </VStack>

    <VStack gap={1}>
      <Text size="sm" weight="medium">{t('settings.accent')}</Text>
      <div class="accent-grid">
        {#each accents as a}
          <button
            type="button"
            class="accent-swatch"
            class:selected={accent.value.name === a.name}
            style="background: {a.bg}; color: {a.text}"
            onclick={() => { accent.value = a }}
            title="{a.name} ({a.bg})"
            aria-label={a.name}
            use:squircle
          >Aa</button>
        {/each}
      </div>
    </VStack>
  </SectionCard>

  <SectionCard title={t('settings.instance.title')} description={t('settings.instance.desc')}>
    <HStack align="center" gap={4}>
      <Text>{t('settings.cluster_node')}</Text>
      <Spacer />
      <Switch bind:checked={isClusterNode} />
    </HStack>

    <HStack align="center" gap={4}>
      <Text>{t('settings.disable_telemetry')}</Text>
      <Spacer />
      <Switch bind:checked={disableTelemetry} />
    </HStack>

    <HStack justify="end">
      <Button style="prominent" onclick={save} disabled={saving}>
        {saving ? t('common.actions.saving') : t('common.actions.save_changes')}
      </Button>
    </HStack>
  </SectionCard>

  <!-- Security section -->
  <SectionCard title={t('settings.security')} description={t('settings.password.title')}>
    {#if passwordError}
      <Banner variant="error" text={passwordError} />
    {/if}

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="current-password">{t('settings.password.current')}</Text>
      <TextEdit id="current-password" bind:value={currentPassword} type="secret" />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="new-password">{t('settings.password.new')}</Text>
      <TextEdit id="new-password" bind:value={newPassword} type="secret" />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="confirm-password">{t('settings.password.confirm')}</Text>
      <TextEdit id="confirm-password" bind:value={confirmPassword} type="secret" />
    </VStack>

    <HStack justify="end">
      <Button style="prominent" onclick={updatePassword} disabled={!isPasswordFormValid}>
        {passwordSaving ? t('common.actions.saving') : t('settings.password.change')}
      </Button>
    </HStack>
  </SectionCard>

  <!-- Database Doctor Section -->
  <SectionCard title={t('data.doctor.title')} description={t('data.doctor.desc')}>
    {#if doctorLoading}
      <EmptyState title={t('common.state.loading')} />
    {:else if doctorReport}
      {#if doctorReport.total_issues === 0}
        <Banner variant="success" text={t('data.doctor.no_issues')} />
      {:else}
        <VStack gap={3}>
          <Banner variant="warning" text={`${t('data.doctor.found')}: ${doctorReport.total_issues}`} />
          <VStack gap={2} class="issues-container">
            {#each doctorReport.issues as issue}
              <HStack align="center" gap={3} class="issue-row">
                <Icon name="error" tone="danger" />
                <VStack gap={0} grow>
                  <Text weight="medium" size="sm">{issue.title}</Text>
                  <Text size="xs" tone="soft">{issue.description}</Text>
                </VStack>
                <Chip text={String(issue.count)} color="chip-red" />
              </HStack>
            {/each}
          </VStack>
          <HStack justify="end">
            <Button style="prominent" tint="var(--fui-color-danger)" onclick={fixIssues} disabled={doctorFixing}>
              {doctorFixing ? t('data.doctor.resolving') : t('data.doctor.clean')}
            </Button>
          </HStack>
        </VStack>
      {/if}
    {/if}
  </SectionCard>

  <!-- Data Management section -->
  <SectionCard title={t('data.title')} description={t('data.subtitle')}>
    {#if statsLoading}
      <EmptyState title={t('common.state.loading')} />
    {:else if stats}
      <VStack gap={4} class="data-container">
        {#each [
          { key: 'providers', label: t('data.providers_credentials'), count: stats.providers },
          { key: 'virtual_models', label: t('data.virtual_models'), count: stats.virtual_models },
          { key: 'plugins', label: t('data.plugins_repos'), count: stats.plugins },
          { key: 'tokens', label: t('data.access_tokens'), count: stats.tokens }
        ] as sub}
          <VStack gap={2} class="subsystem-box">
            <HStack align="center" gap={3}>
              <VStack gap={0} grow>
                <Text weight="medium" size="base">{sub.label}</Text>
                <Text size="xs" tone="soft">{n(sub.count, 'units.item.one', 'units.item.many')}</Text>
              </VStack>
              <HStack gap={2} wrap>
                <Button size="small" icon={{ name: 'download' }} onclick={() => exportSubsystem(sub.key)}>{t('data.export')}</Button>
                <Button size="small" style="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} onclick={() => clearSubsystem(sub.key)}>{t('data.clear')}</Button>
              </HStack>
            </HStack>
            <HStack gap={3} align="center" wrap class="file-action-row">
              <input type="file" accept=".json" onchange={(e) => handleFileChange(sub.key, e)} class="file-input" id="file-{sub.key}" />
              <label for="file-{sub.key}" class="file-label">
                <Icon name="attach_file" />
                <span>{importFileMap[sub.key] ? importFileMap[sub.key]?.name : t('data.select_json')}</span>
              </label>
              {#if importFileMap[sub.key]}
                <Button size="small" style="prominent" onclick={() => importSubsystem(sub.key)}>{t('data.import')}</Button>
              {/if}
            </HStack>
          </VStack>
        {/each}
      </VStack>
    {/if}
  </SectionCard>

  <!-- Individual Provider Management section -->
  <SectionCard title={t('data.provider.title')} description={t('data.provider.desc')}>
    {#if providersLoading}
      <EmptyState title={t('common.state.loading')} />
    {:else if allProviders.length === 0}
      <EmptyState title={t('providers.manage.none_available')} />
    {:else}
      <VStack gap={3}>
        <VStack gap={1}>
          <Text size="sm" weight="medium">{t('data.provider.select')}</Text>
          <Select ariaLabel={t('data.provider.select')} bind:value={selectedProviderId} options={providerOptions} />
        </VStack>

        <HStack gap={2} align="center" wrap>
          <Button size="small" icon={{ name: 'download' }} onclick={() => exportIndividualProvider(selectedProviderId)} disabled={!selectedProviderId}>{t('data.export')}</Button>
          <Button size="small" style="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} onclick={() => purgeIndividualProvider(selectedProviderId)} disabled={!selectedProviderId}>{t('data.purge_provider')}</Button>
        </HStack>

        <HStack gap={3} align="center" wrap class="file-action-row">
          <input type="file" accept=".json" onchange={handleIndividualProviderFileChange} class="file-input" id="file-individual-provider" />
          <label for="file-individual-provider" class="file-label">
            <Icon name="attach_file" />
            <span>{providerImportFile ? providerImportFile?.name : t('data.select_json')}</span>
          </label>
          {#if providerImportFile}
            <Button size="small" style="prominent" onclick={() => importIndividualProvider(selectedProviderId)} disabled={!selectedProviderId}>{t('data.import')}</Button>
          {/if}
        </HStack>
      </VStack>
    {/if}
  </SectionCard>
</VStack>

<style>
  .accent-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--fui-ctl-large), 1fr));
    gap: var(--fui-space-3);
  }
  .accent-swatch {
    width: 100%;
    height: var(--fui-ctl-large);
    border-radius: var(--fui-ctl-radius);
    border: none;
    font-size: var(--fui-text-base);
    font-weight: 700;
    cursor: pointer;
    padding: 0;
  }
  .accent-swatch.selected {
    outline: var(--fui-settings-accent-outline) solid var(--fui-color-text);
    outline-offset: var(--fui-settings-accent-offset);
  }
  :global(.issues-container), :global(.data-container) {
    width: 100%;
  }
  :global(.issue-row) {
    padding: var(--fui-space-3) var(--fui-space-4);
    background: var(--fui-color-surface-container);
    border-radius: var(--fui-radius-md);
  }
  :global(.subsystem-box) {
    padding: var(--fui-space-4);
    background: var(--fui-elev);
    border-radius: var(--fui-radius-md);
    border: var(--fui-border-w) solid var(--fui-color-outline-soft);
  }
  :global(.file-action-row) {
    margin-top: var(--fui-space-2);
    padding-top: var(--fui-space-2);
    border-top: var(--fui-border-w) dashed var(--fui-color-outline-soft);
  }
  .file-input {
    display: none;
  }
  .file-label {
    display: inline-flex;
    align-items: center;
    gap: var(--fui-space-2);
    cursor: pointer;
    background: var(--fui-color-button-container);
    padding: var(--fui-space-2) var(--fui-space-3);
    border-radius: var(--fui-ctl-radius);
    font-size: var(--fui-text-sm);
    font-weight: 500;
    border: var(--fui-border-w) solid var(--fui-color-outline-soft);
  }
  .file-label:hover {
    background: var(--fui-color-button-container-high);
  }
</style>
