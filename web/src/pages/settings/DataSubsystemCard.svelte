<script lang="ts">
  // One data management subsystem: Import / Export / Clear in the header. With `items` the card
  // folds open to the subsystem's items, one row each with its own export (see data-items.ts).
  import { Button, Card, Chip, ContentUnavailable, Divider, HStack, Spacer, Text, VStack, ZStack, modal, toast, type HeaderAction } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { t } from '$lib/i18n.svelte'
  import { getErrorMessage } from '$lib/errors'
  import { exportSubsystem, importSubsystem, type DataItem, type ItemExport } from './data-items'

  let {
    subsystem,
    title,
    count,
    items,
    purgeItem,
    onchanged,
  }: {
    subsystem: string
    title: string
    count: number
    /** per-item export; makes the card collapsible */
    items?: ItemExport
    /** a destructive per-item action (providers: remove the provider with all its data) */
    purgeItem?: (item: DataItem) => Promise<boolean>
    /** after an import, clear or purge */
    onchanged: () => Promise<void>
  } = $props()

  let open = $state(false)
  let list = $state<DataItem[] | null>(null)
  let listLoading = $state(false)
  let busy = $state(false)

  // the first open loads the items
  function setOpen(value: boolean): void {
    open = value
    if (value && list === null && !listLoading) void loadItems()
  }

  async function loadItems(): Promise<void> {
    if (!items) return
    listLoading = true
    try {
      list = await items.list()
    } catch (e) {
      list = []
      toast.error(getErrorMessage(e))
    } finally {
      listLoading = false
    }
  }

  async function changed(): Promise<void> {
    await onchanged()
    if (list !== null) await loadItems()
  }

  async function importFile(): Promise<void> {
    busy = true
    try {
      if (!(await importSubsystem(subsystem))) return
      toast.success(t('data.import.success'))
      await changed()
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      busy = false
    }
  }

  async function exportAll(): Promise<void> {
    try {
      await exportSubsystem(subsystem)
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function exportItem(item: DataItem): Promise<void> {
    if (!items) return
    try {
      await items.exportItem(item)
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  async function clear(): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('data.clear_subsystem'),
      message: t('data.clear_confirm'),
      confirmText: t('data.clear'),
      confirmRole: 'destructive',
    })
    if (!confirmed) return
    busy = true
    try {
      await api.data.clearSubsystem(subsystem)
      toast.success(t('data.clear.success'))
      await changed()
    } catch (e) {
      toast.error(getErrorMessage(e))
    } finally {
      busy = false
    }
  }

  async function purge(item: DataItem): Promise<void> {
    if (!purgeItem) return
    try {
      if (await purgeItem(item)) await changed()
    } catch (e) {
      toast.error(getErrorMessage(e))
    }
  }

  const actions = $derived<HeaderAction[]>([
    { id: 'import', text: t('data.import'), icon: 'upload', iconOnly: true, buttonStyle: 'text', disabled: busy, onclick: () => void importFile() },
    { id: 'export', text: t('data.export'), icon: 'download', iconOnly: true, buttonStyle: 'text', disabled: busy, onclick: () => void exportAll() },
    { id: 'clear', text: t('data.clear'), icon: 'delete', iconOnly: true, buttonStyle: 'text', tint: 'var(--fui-color-danger)', disabled: busy, onclick: () => void clear() },
  ])
</script>

{#snippet countChip()}
  <Chip text={String(count)} />
{/snippet}

{#if items}
  <Card {title} {actions} accessory={countChip} collapsible bind:open={() => open, setOpen}>
    {#if listLoading && list === null}
      <ContentUnavailable title={t('common.state.loading')} />
    {:else if !list || list.length === 0}
      <ContentUnavailable title={t('common.empty.no_data')} />
    {:else}
      <!-- the Card body pads every side and spaces its children by the same step: rows follow it,
           the dividers sit on that step between them -->
      <VStack gap={5}>
        {#each list as item, index (item.id)}
          {#if index > 0}<Divider />{/if}
          <HStack valign="center" gap={3}>
            <Text truncate>{item.label}</Text>
            <Spacer />
            <Button size="sm" icon={{ name: 'download' }} onclick={() => void exportItem(item)}>{t('data.export')}</Button>
            {#if purgeItem}
              <Button size="sm" buttonStyle="text" tint="var(--fui-color-danger)" icon={{ name: 'delete' }} onclick={() => void purge(item)}>{t('data.purge_provider')}</Button>
            {/if}
          </HStack>
        {/each}
      </VStack>
    {/if}
  </Card>
{:else}
  <!-- an empty box where the collapsible rows have their chevron: the actions line up across rows -->
  {#snippet chevronGap()}<ZStack width="var(--fui-ctl-md)" height="var(--fui-ctl-md)" />{/snippet}
  <Card {title} {actions} accessory={countChip} trailing={chevronGap} />
{/if}
