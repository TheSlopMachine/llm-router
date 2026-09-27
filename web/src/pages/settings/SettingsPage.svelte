<script lang="ts">
  import { onMount } from 'svelte'
  import { theme } from '$lib/theme.svelte'
  import type { Theme } from '$lib/theme.svelte'
  import { language } from '$lib/language.svelte'
  import type { Language } from '$lib/language.svelte'
  import { api } from '$lib/api'
  import { t } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { accent, accents } from '$lib/accent.svelte'
  import { Button, HStack, SectionCard, Select, Spacer, Switch, Text, VStack, TextEdit, Banner, Icon, Chip } from '$ui'
  import { squircle } from '$lib/squircle'
  import type { SubsystemStats, DoctorReport } from '$lib/types'
  import { toast } from '$lib/toast.svelte'

  let languageOptions = $derived([
    { value: 'auto', label: t('Auto') },
    { value: 'en', label: t('English') },
    { value: 'ru', label: t('Russian') }
  ])
  let themeOptions: Array<{ value: string; label: string }> = $derived([
    { value: 'auto', label: t('Auto') },
    { value: 'light', label: t('Light') },
    { value: 'dark', label: t('Dark') }
  ])

  // Preferences
  let lang = $state<Language>(language.value)
  let th = $state<Theme>(theme.value)

  // Config
  let isClusterNode = $state(false)
  let disableTelemetry = $state(false)
  let minDownloadSpeedKbps = $state(15000)
  let maxProxiesPerLocation = $state(10)
  let updateIntervalMinutes = $state(15)

  // Security
  let currentPassword = $state('')
  let newPassword = $state('')
  let confirmPassword = $state('')
  let passwordSaving = $state(false)
  let passwordError = $state('')

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
      minDownloadSpeedKbps = cfg.min_download_speed_kbps
      maxProxiesPerLocation = cfg.max_proxies_per_location
      updateIntervalMinutes = cfg.update_interval_minutes
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
        disable_telemetry: disableTelemetry,
        min_download_speed_kbps: minDownloadSpeedKbps,
        max_proxies_per_location: maxProxiesPerLocation,
        update_interval_minutes: updateIntervalMinutes
      })
      toast.success(t('Changes saved'))
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
      passwordError = t('Passwords do not match')
      return
    }
    passwordSaving = true
    try {
      await api.admin.changePassword(currentPassword, newPassword)
      toast.success(t('Password updated successfully'))
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
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `llm_router_${sub}_export.json`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
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
      toast.error(t('Please select a file first'))
      return
    }
    try {
      const text = await file.text()
      const json = JSON.parse(text)
      await api.data.importSubsystem(sub, json)
      toast.success(`${sub} imported successfully`)
      importFileMap = { ...importFileMap, [sub]: null }
      await Promise.all([loadDataStats(), runDoctorInspect()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function clearSubsystem(sub: string): Promise<void> {
    if (!confirm(t('Are you sure you want to clear all data in this subsystem? This action is irreversible.'))) {
      return
    }
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
      toast.success(`${res.fixed} ${t('issues resolved successfully')}`)
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
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `llm_router_provider_${providerId}_export.json`
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)
      toast.success(t('Provider exported successfully'))
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
      toast.error(t('Please select a file first'))
      return
    }
    try {
      const text = await providerImportFile.text()
      const json = JSON.parse(text)
      await api.data.importProvider(providerId, json)
      toast.success(t('Provider imported successfully'))
      providerImportFile = null
      await Promise.all([loadDataStats(), runDoctorInspect(), loadProviders()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function purgeIndividualProvider(providerId: string): Promise<void> {
    if (!confirm(t('Are you sure you want to completely purge this provider and all its data? This action is irreversible.'))) {
      return
    }
    try {
      await api.data.purgeProvider(providerId)
      toast.success(t('Provider purged successfully'))
      await Promise.all([loadDataStats(), runDoctorInspect(), loadProviders()])
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

</script>

<VStack gap={6}>
  <VStack gap={1}>
    <Text tag="h1" size="lg" weight="bold">{t('Settings')}</Text>
    <Text tone="soft" size="sm">{t('Preferences and instance configuration.')}</Text>
  </VStack>

  {#if error}
    <Banner variant="error" text={error} />
  {/if}

  <SectionCard title="Preferences" description="This browser only">
    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="settings-language">{t('Language')}</Text>
      <Select value={lang} options={languageOptions} onchange={(v) => (lang = v as Language)} />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="settings-theme">{t('Theme')}</Text>
      <Select value={th} options={themeOptions} onchange={(v) => (th = v as Theme)} />
    </VStack>

    <VStack gap={1}>
      <Text size="sm" weight="medium">{t('Accent')}</Text>
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
            use:squircle={12}
          >Aa</button>
        {/each}
      </div>
    </VStack>
  </SectionCard>

  <SectionCard title="Instance configuration" description="This deployment">
    <HStack align="center" gap={4}>
      <Text>{t('Make this instance a cluster node')}</Text>
      <Spacer />
      <Switch bind:checked={isClusterNode} />
    </HStack>

    <HStack align="center" gap={4}>
      <Text>{t('Disable anonymized telemetry')}</Text>
      <Spacer />
      <Switch bind:checked={disableTelemetry} />
    </HStack>

    <HStack justify="end">
      <Button style="prominent" onclick={save} disabled={saving}>
        {saving ? t('Saving...') : t('Save changes')}
      </Button>
    </HStack>
  </SectionCard>

  <!-- Security section -->
  <SectionCard title="Security" description="Update administrator password">
    {#if passwordError}
      <Banner variant="error" text={passwordError} />
    {/if}

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="current-password">{t('Current password')}</Text>
      <TextEdit id="current-password" bind:value={currentPassword} type="secret" />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="new-password">{t('New password')}</Text>
      <TextEdit id="new-password" bind:value={newPassword} type="secret" />
    </VStack>

    <VStack gap={1}>
      <Text tag="label" size="sm" weight="medium" for="confirm-password">{t('Confirm new password')}</Text>
      <TextEdit id="confirm-password" bind:value={confirmPassword} type="secret" />
    </VStack>

    <HStack justify="end">
      <Button style="prominent" onclick={updatePassword} disabled={passwordSaving || !currentPassword || !newPassword}>
        {passwordSaving ? t('Saving...') : t('Change password')}
      </Button>
    </HStack>
  </SectionCard>

  <!-- Database Doctor Section -->
  <SectionCard title="Database Doctor" description="Audit and clean orphaned data">
    {#if doctorLoading}
      <Text tone="soft" size="sm">{t('Scanning database for anomalies...')}</Text>
    {:else if doctorReport}
      {#if doctorReport.total_issues === 0}
        <Banner variant="success" text={t('No issues found. Your database is perfectly clean!')} />
      {:else}
        <VStack gap={3}>
          <Banner variant="warning" text={`${t('Found issues in database')}: ${doctorReport.total_issues}`} />
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
            <Button style="prominent" tint="var(--color-text-danger)" onclick={fixIssues} disabled={doctorFixing}>
              {doctorFixing ? t('Resolving...') : t('Clean database')}
            </Button>
          </HStack>
        </VStack>
      {/if}
    {/if}
  </SectionCard>

  <!-- Data Management section -->
  <SectionCard title="Data Management" description="Export, import and purge subsystem data">
    {#if statsLoading}
      <Text tone="soft" size="sm">{t('Loading stats...')}</Text>
    {:else if stats}
      <VStack gap={4} class="data-container">
        {#each [
          { key: 'providers', label: t('Providers & Keys'), count: stats.providers },
          { key: 'virtual_models', label: t('Virtual Models'), count: stats.virtual_models },
          { key: 'plugins', label: t('Plugins & Repos'), count: stats.plugins },
          { key: 'proxies', label: t('Proxies'), count: stats.proxies },
          { key: 'tokens', label: t('Access Tokens'), count: stats.tokens }
        ] as sub}
          <VStack gap={2} class="subsystem-box">
            <HStack align="center" gap={3}>
              <VStack gap={0} grow>
                <Text weight="medium" size="base">{sub.label}</Text>
                <Text size="xs" tone="soft">{sub.count} {t('items')}</Text>
              </VStack>
              <HStack gap={2}>
                <Button size="small" icon={{ name: 'download' }} onclick={() => exportSubsystem(sub.key)}>{t('Export')}</Button>
                <Button size="small" style="text" tint="var(--color-text-danger)" icon={{ name: 'delete' }} onclick={() => clearSubsystem(sub.key)}>{t('Clear')}</Button>
              </HStack>
            </HStack>
            <HStack gap={3} align="center" class="file-action-row">
              <input type="file" accept=".json" onchange={(e) => handleFileChange(sub.key, e)} class="file-input" id="file-{sub.key}" />
              <label for="file-{sub.key}" class="file-label">
                <Icon name="attach_file" />
                <span>{importFileMap[sub.key] ? importFileMap[sub.key]?.name : t('Select JSON')}</span>
              </label>
              {#if importFileMap[sub.key]}
                <Button size="small" style="prominent" onclick={() => importSubsystem(sub.key)}>{t('Import')}</Button>
              {/if}
            </HStack>
          </VStack>
        {/each}
      </VStack>
    {/if}
  </SectionCard>

  <!-- Individual Provider Management section -->
  <SectionCard title="Individual Provider Management" description="Export, import or purge a specific provider">
    {#if providersLoading}
      <Text tone="soft" size="sm">{t('Loading providers...')}</Text>
    {:else if allProviders.length === 0}
      <Text tone="soft" size="sm">{t('No providers available for individual management.')}</Text>
    {:else}
      <VStack gap={3}>
        <VStack gap={1}>
          <Text tag="label" size="sm" weight="medium" for="select-provider">{t('Select a provider')}</Text>
          <Select id="select-provider" bind:value={selectedProviderId} options={providerOptions} />
        </VStack>

        <HStack gap={2} align="center">
          <Button size="small" icon={{ name: 'download' }} onclick={() => exportIndividualProvider(selectedProviderId)} disabled={!selectedProviderId}>{t('Export')}</Button>
          <Button size="small" style="text" tint="var(--color-text-danger)" icon={{ name: 'delete' }} onclick={() => purgeIndividualProvider(selectedProviderId)} disabled={!selectedProviderId}>{t('Purge provider')}</Button>
        </HStack>

        <HStack gap={3} align="center" class="file-action-row">
          <input type="file" accept=".json" onchange={handleIndividualProviderFileChange} class="file-input" id="file-individual-provider" />
          <label for="file-individual-provider" class="file-label">
            <Icon name="attach_file" />
            <span>{providerImportFile ? providerImportFile?.name : t('Select JSON')}</span>
          </label>
          {#if providerImportFile}
            <Button size="small" style="prominent" onclick={() => importIndividualProvider(selectedProviderId)} disabled={!selectedProviderId}>{t('Import')}</Button>
          {/if}
        </HStack>
      </VStack>
    {/if}
  </SectionCard>
</VStack>

<style>
  .accent-grid {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  .accent-swatch {
    width: 44px;
    height: 44px;
    border-radius: var(--ctl-radius);
    border: none;
    font-size: var(--text-base);
    font-weight: 700;
    cursor: pointer;
    padding: 0;
  }
  .accent-swatch.selected {
    outline: 2px solid var(--color-text);
    outline-offset: 2px;
  }
  :global(.issues-container), :global(.data-container) {
    width: 100%;
  }
  :global(.issue-row) {
    padding: var(--space-3) var(--space-4);
    background: var(--color-surface-container);
    border-radius: var(--radius-md);
  }
  :global(.subsystem-box) {
    padding: var(--space-4);
    background: var(--elev);
    border-radius: var(--radius-md);
    border: 1px solid var(--color-outline-soft);
  }
  :global(.file-action-row) {
    margin-top: var(--space-2);
    padding-top: var(--space-2);
    border-top: 1px dashed var(--color-border);
  }
  .file-input {
    display: none;
  }
  .file-label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    cursor: pointer;
    background: var(--color-button-bg);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--ctl-radius);
    font-size: var(--text-sm);
    font-weight: 500;
    border: 1px solid var(--color-border);
  }
  .file-label:hover {
    background: var(--color-button-hover-bg);
  }
</style>
