<script lang="ts">
  import { onMount } from 'svelte'
  import { Banner, Button, ConfirmAction, ContentUnavailable, FloatingView, HStack, Table, Text, VStack, modal, type TableColumn } from '@fuckyouryan/fui'
  import SectionHeading from '../../../components/SectionHeading.svelte'
  import PluginPermissionList from '../../../components/domain/PluginPermissionList.svelte'
  import DockerLogs from './DockerLogs.svelte'
  import { api } from '$lib/api'
  import { t, tx } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { containerStatus, containerTarget, describeImage, describeWait, dockerBusy, dockerRows, stageHint } from '$lib/docker'
  import type { DockerRow, DockerSectionState } from '$lib/docker'
  import type { DockerContainerStatus, DockerPluginView } from '$lib/types'

  let { providerId, onstate }: {
    providerId: string
    onstate?: (state: DockerSectionState) => void
  } = $props()

  const FAST_POLL_MS = 2000
  const SLOW_POLL_EVERY = 8

  const columns: TableColumn[] = [
    { key: 'name', title: t('common.labels.name'), width: '1fr', priority: 1 },
    { key: 'status', title: t('common.labels.status'), width: '1fr', priority: 1 },
    { key: 'actions', title: t('common.labels.actions'), width: 'auto', align: 'right', priority: 1 },
  ]

  let pluginId = $state('')
  let view = $state<DockerPluginView | null>(null)
  let usesDocker = $state(false)
  let error = $state('')
  let acting = $state(false)

  let permissionsOpen = $state(false)
  let permissionsAnchor = $state<HTMLElement>()
  let removeTarget = $state<DockerContainerStatus | null>(null)
  let removeAnchor = $state<HTMLElement>()

  let busy = $derived(dockerBusy(view))
  let configured = $derived(!!view?.connection.endpoint)
  let rows = $derived(view ? dockerRows(view) : [])

  onMount(() => {
    void load()
    let ticks = 0
    const timer = window.setInterval(() => {
      ticks++
      if (busy || ticks % SLOW_POLL_EVERY === 0) void load()
    }, FAST_POLL_MS)
    return () => window.clearInterval(timer)
  })

  async function load(): Promise<void> {
    try {
      const answer = await api.docker.provider(providerId)
      pluginId = answer.plugin_id
      usesDocker = answer.uses_docker
      view = answer.view ?? null
      error = ''
    } catch (e) {
      error = getErrorMessage(e)
    }
    onstate?.({ busy: dockerBusy(view), waitText: describeWait(view) })
  }

  async function act(container: DockerContainerStatus, actions: Array<'start' | 'stop' | 'remove'>): Promise<void> {
    acting = true
    error = ''
    try {
      for (const action of actions) await api.docker.action(pluginId, action, containerTarget(container))
      await load()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      acting = false
    }
  }

  async function confirmRemove(): Promise<void> {
    const container = removeTarget
    if (!container) return
    await act(container, ['remove'])
    removeTarget = null
  }

  function openRemove(container: DockerContainerStatus, anchor: HTMLElement): void {
    removeTarget = removeTarget === container ? null : container
    removeAnchor = anchor
  }

  function openLogs(container: DockerContainerStatus): void {
    const target = containerTarget(container)
    modal.open({ title: tx('providers.docker.logs.title', { target }), content: DockerLogs, size: 'lg', severity: 'low', props: { pluginId, target } })
  }

  function togglePermissions(anchor: HTMLElement): void {
    permissionsAnchor = anchor
    permissionsOpen = !permissionsOpen
  }
</script>

{#snippet rowName(row: DockerRow)}
  <VStack gap={0}>
    <Text variant="value">{row.name}</Text>
    <Text variant="caption">{row.kind === 'image' ? t('providers.docker.image.label') : t('providers.docker.container.label')}</Text>
  </VStack>
{/snippet}

{#snippet rowStatus(row: DockerRow)}
  {#if row.kind === 'image'}
    {#if row.image.state === 'failed'}
      <VStack gap={0} halign="start">
        <Text size="sm" tint="danger">{describeImage(row.image)}</Text>
        {#if row.image.error}
          <Text size="xs" foregroundStyle="tertiary" truncate title={row.image.error}>{row.image.error}</Text>
        {/if}
      </VStack>
    {:else}
      <Text size="sm" foregroundStyle="secondary">{describeImage(row.image)}</Text>
    {/if}
  {:else}
    {@const status = containerStatus(row.container)}
    <VStack gap={0} halign="start">
      <Text size="sm" tint={status.tint} foregroundStyle={status.tint ? undefined : 'secondary'}>{status.label}</Text>
      {#if status.caption}
        <Text size="xs" foregroundStyle="tertiary">{status.caption}</Text>
      {/if}
    </VStack>
  {/if}
{/snippet}

{#snippet rowActions(row: DockerRow)}
  {#if row.kind === 'container'}
    {@const container = row.container}
    <HStack gap={3} halign="end">
      {#if container.state === 'running'}
        <Button size="sm" buttonStyle="text" icon={{ name: 'restart_alt' }} ariaLabel={t('providers.docker.restart.action')} title={t('providers.docker.restart.action')} disabled={acting} onclick={() => void act(container, ['stop', 'start'])} />
        <Button size="sm" buttonStyle="text" icon={{ name: 'stop' }} ariaLabel={t('providers.docker.stop.action')} title={t('providers.docker.stop.action')} disabled={acting} onclick={() => void act(container, ['stop'])} />
      {:else}
        <Button size="sm" buttonStyle="text" icon={{ name: 'play_arrow' }} ariaLabel={t('providers.docker.start.action')} title={t('providers.docker.start.action')} disabled={acting} onclick={() => void act(container, ['start'])} />
      {/if}
      <Button size="sm" buttonStyle="text" icon={{ name: 'article' }} ariaLabel={t('providers.docker.logs.action')} title={t('providers.docker.logs.action')} onclick={() => openLogs(container)} />
      <Button size="sm" buttonStyle="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} ariaLabel={t('providers.docker.delete.title')} title={t('providers.docker.delete.title')} disabled={acting} onclick={(e) => openRemove(container, e.currentTarget as HTMLElement)} />
    </HStack>
  {/if}
{/snippet}

{#if usesDocker && view}
  {@const policy = view.policy}
  <VStack tag="section" gap={4} class="provider-section">
    <SectionHeading title={t('providers.docker.title')}>
      {#snippet actions()}
        <Button buttonStyle="neutral" icon={{ name: 'info' }} ariaLabel={t('plugins.permissions.title')} title={t('plugins.permissions.title')} onclick={(e) => togglePermissions(e.currentTarget as HTMLElement)} />
      {/snippet}
    </SectionHeading>

    {#if error}
      <Banner tint="danger" text={error} />
    {/if}
    {#if !configured}
      <Banner tint="info" text={t('providers.docker.unconfigured.hint')} />
    {:else if view.error}
      <Banner tint="warning" text={view.error} />
      {#if view.error_stage}
        <Text size="sm">{stageHint(view.error_stage)}</Text>
      {/if}
    {/if}
    {#if configured && !view.error}
      <Table {columns} {rows} rowKey={(row) => row.id}>
        {#snippet cell({ column, row })}
          {#if column.key === 'name'}
            {@render rowName(row)}
          {:else if column.key === 'status'}
            {@render rowStatus(row)}
          {:else}
            {@render rowActions(row)}
          {/if}
        {/snippet}
        {#snippet card({ row })}
          <HStack gap={3} valign="center">
            <VStack gap={1} grow>{@render rowName(row)}</VStack>
            {@render rowActions(row)}
          </HStack>
          {@render rowStatus(row)}
        {/snippet}
        {#snippet empty()}
          <ContentUnavailable title={t('providers.docker.containers.empty')} />
        {/snippet}
      </Table>
    {/if}
  </VStack>

  <FloatingView open={permissionsOpen} anchor={permissionsAnchor} width="sm" onclose={() => { permissionsOpen = false }} label={t('plugins.permissions.title')}>
    {#snippet children()}
      <PluginPermissionList hosts={policy.allow_hosts} unsafe={policy.allow_hosts.includes('*')} docker={policy.directives} />
    {/snippet}
  </FloatingView>

  <FloatingView open={Boolean(removeTarget)} anchor={removeAnchor} width="sm" onclose={() => { removeTarget = null }} label={t('providers.docker.delete.title')}>
    {#snippet children({ close })}
      <ConfirmAction
        title={t('providers.docker.delete.title')}
        body={`${t('misc.delete_confirm')} "${removeTarget ? containerTarget(removeTarget) : ''}"? ${t('common.undo.cannot_undo')}`}
        busy={acting}
        busyLabel={t('common.actions.deleting')}
        oncancel={close}
        onconfirm={confirmRemove} />
    {/snippet}
  </FloatingView>
{:else if error}
  <Banner tint="danger" text={error} />
{/if}
