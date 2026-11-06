<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '$lib/api'
  import { t } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { Button, HStack, Card, Text, VStack, TextField, Banner, Icon, Chip, Header, ContentUnavailable, Field, toast, modal, CardGroup, type HeaderAction } from '@fuckyouryan/fui'
  import type { SubsystemStats, DoctorReport } from '$lib/types'
  import AppearanceCard from './AppearanceCard.svelte'
  import DataSubsystemCard from './DataSubsystemCard.svelte'
  import { clearAllData, itemExports, type DataItem } from './data-items'

  // Security
  let currentPassword = $state('')
  let newPassword = $state('')
  let confirmPassword = $state('')
  let passwordSaving = $state(false)
  let passwordError = $state('')
  let isPasswordFormValid = $derived(!!currentPassword && !!newPassword && !passwordSaving)

  // Data Management
  let subsystemStatistics = $state<SubsystemStats | null>(null)
  let subsystemStatisticsLoading = $state(false)

  // Database Doctor
  let doctorReport = $state<DoctorReport | null>(null)
  let doctorLoading = $state(false)
  let doctorFixing = $state(false)

  onMount(async () => {
    await Promise.all([loadDataStats(), runDoctorInspect()])
  })

  async function loadDataStats(): Promise<void> {
    subsystemStatisticsLoading = true
    try {
      subsystemStatistics = await api.data.stats()
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      subsystemStatisticsLoading = false
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

  async function refreshData(): Promise<void> {
    await Promise.all([loadDataStats(), runDoctorInspect()])
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

  async function fixIssues(): Promise<void> {
    if (doctorFixing) return
    doctorFixing = true
    try {
      const fixResult = await api.doctor.fix([])
      toast.success(`${fixResult.fixed} ${t('data.doctor.resolved')}`)
      await refreshData()
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      doctorFixing = false
    }
  }

  let clearingAll = $state(false)
  // bumped after a clear of every subsystem: the subsystem cards mount anew, so open item lists reload
  let subsystemCardsGeneration = $state(0)

  async function clearAll(): Promise<void> {
    if (clearingAll) return
    clearingAll = true
    try {
      if (!(await clearAllData())) return
      toast.success(t('data.clear.success'))
    } catch (e) {
      // a failed step leaves the earlier subsystems cleared: refresh anyway
      toast.error(getErrorMessage(e))
    } finally {
      clearingAll = false
    }
    subsystemCardsGeneration++
    await refreshData()
  }

  async function purgeProvider(provider: DataItem): Promise<boolean> {
    const confirmed = await modal.confirm({
      title: t('data.purge_provider'),
      message: t('data.purge_confirm'),
      confirmText: t('data.purge'),
      confirmRole: 'destructive'
    })
    if (!confirmed) return false
    await api.data.purgeProvider(provider.id)
    toast.success(t('providers.purge.success'))
    return true
  }

  const subsystems = $derived(subsystemStatistics ? [
    { key: 'providers', title: t('data.providers_credentials'), count: subsystemStatistics.providers },
    { key: 'virtual_models', title: t('data.virtual_models'), count: subsystemStatistics.virtual_models },
    { key: 'plugins', title: t('data.plugins_repos'), count: subsystemStatistics.plugins },
    { key: 'tokens', title: t('data.access_tokens'), count: subsystemStatistics.tokens },
    { key: 'proxy', title: t('data.proxy'), count: subsystemStatistics.proxy_pools + subsystemStatistics.proxies },
  ] : [])

  const dataActions = $derived<HeaderAction[]>([
    { id: 'clear-all', text: t('data.clear_all'), icon: 'delete', buttonStyle: 'prominent', tint: 'var(--fui-color-danger)', disabled: clearingAll, onclick: () => void clearAll() },
  ])
</script>

<VStack gap={6}>
  <Header title={t('settings.title')} subtitle={t('settings.subtitle')} />

  <AppearanceCard />

  <Card title={t('settings.security')}>
    {#if passwordError}
      <Banner tint="danger" text={passwordError} />
    {/if}

    <Field label={t('settings.password.current')}>
      <TextField id="current-password" name="current-password" bind:value={currentPassword} size="lg" secure autocomplete="current-password" />
    </Field>

    <Field label={t('settings.password.new')}>
      <TextField id="new-password" name="new-password" bind:value={newPassword} size="lg" secure autocomplete="new-password" />
    </Field>

    <Field label={t('settings.password.confirm')}>
      <TextField id="confirm-password" name="new-password-confirm" bind:value={confirmPassword} size="lg" secure autocomplete="new-password" />
    </Field>

    <HStack halign="end">
      <Button buttonStyle="prominent" onclick={updatePassword} disabled={!isPasswordFormValid}>
        {passwordSaving ? t('common.actions.saving') : t('settings.password.change')}
      </Button>
    </HStack>
  </Card>

  <Card title={t('data.doctor.title')}>
    {#if doctorLoading}
      <ContentUnavailable title={t('common.state.loading')} />
    {:else if doctorReport}
      {#if doctorReport.total_issues === 0}
        <Banner tint="success" text={t('data.doctor.no_issues')} />
      {:else}
        <VStack gap={3}>
          <Banner tint="warning" text={`${t('data.doctor.found')}: ${doctorReport.total_issues}`} />
          <VStack gap={2} class="issues-container">
            {#each doctorReport.issues as issue}
              <HStack valign="center" gap={3} class="issue-row">
                <Icon name="error" tint="danger" />
                <VStack gap={0} grow>
                  <Text weight="medium" size="sm">{issue.title}</Text>
                  <Text size="xs" foregroundStyle="secondary">{issue.description}</Text>
                </VStack>
                <Chip text={String(issue.count)} tint="danger" />
              </HStack>
            {/each}
          </VStack>
          <HStack halign="end">
            <Button buttonStyle="prominent" tint="var(--fui-color-danger)" onclick={fixIssues} disabled={doctorFixing}>
              {doctorFixing ? t('data.doctor.resolving') : t('data.doctor.clean')}
            </Button>
          </HStack>
        </VStack>
      {/if}
    {/if}
  </Card>

  {#if subsystemStatisticsLoading && !subsystemStatistics}
    <ContentUnavailable title={t('common.state.loading')} />
  {/if}
  {#if subsystemStatistics}
    {#key subsystemCardsGeneration}
      <CardGroup title={t('data.management.title')} actions={dataActions}>
        {#each subsystems as subsystem (subsystem.key)}
          <DataSubsystemCard
            subsystem={subsystem.key}
            title={subsystem.title}
            count={subsystem.count}
            items={itemExports[subsystem.key]}
            purgeItem={subsystem.key === 'providers' ? purgeProvider : undefined}
            onchanged={refreshData}
          />
        {/each}
      </CardGroup>
    {/key}
  {/if}
</VStack>

<style>
  :global(.issues-container) {
    width: 100%;
  }
  :global(.issue-row) {
    padding: var(--fui-space-3) var(--fui-space-4);
    background: var(--fui-color-surface-container);
    border-radius: var(--fui-radius-md);
  }
</style>
