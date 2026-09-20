<script lang="ts">
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import { createListResource } from '../lib/list-resource.svelte'
  import type { VirtualModel } from '../lib/types'
  import { t } from '../lib/i18n.svelte'
  import { squircle } from '../lib/squircle'
  import EmptyState from './EmptyState.svelte'
  import Button from './ui/Button.svelte'
  import CapabilityChips from './ui/CapabilityChips.svelte'
  import Table from './ui/Table.svelte'

  const resource = createListResource<VirtualModel[]>(
    async () => {
      try {
        const response = (await api.virtualModels.list()) as unknown
        return Array.isArray(response) ? (response as VirtualModel[]) : []
      } catch (e: unknown) {
        const rec = e as { status?: number; message?: string }
        if (rec?.status === 401 || getErrorMessage(e).includes('unauthenticated')) {
          window.location.href = '/login'
          return []
        }
        throw e
      }
    },
    []
  )

  function openNew() {
    window.location.hash = '#/virtual/new'
  }

  function openEdit(vm: VirtualModel) {
    window.location.hash = `#/virtual/${vm.id}`
  }

  async function remove(vm: VirtualModel) {
    const confirmed = await modal.confirm({
      title: t('Delete virtual model'),
      message: `${t('Are you sure you want to delete')} "${vm.name}"? ${t('This action cannot be undone.')}`,
      severity: 'high',
      size: 'small',
      confirmText: t('Delete'),
      confirmRole: 'destructive'
    })

    if (!confirmed) return

    try {
      await api.virtualModels.delete(vm.id)
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }
</script>

<div class="page">
  <div class="page-header">
    <div>
      <h1>{t('Virtual models')}</h1>
      <p>{t('Virtual models route requests across multiple models with fall-through.')}</p>
    </div>
    <Button text={t('Add')} style="prominent" icon={{ name: 'add' }} onclick={openNew} />
  </div>

  {#if resource.error}
    <div class="error-msg" use:squircle={12}>{resource.error}</div>
  {/if}

  {#if resource.loading}
    <div class="loading">{t('Loading virtual models...')}</div>
  {:else if !resource.data || resource.data.length === 0}
    <EmptyState
      icon="robot"
      message={t('No virtual models yet')}
      buttonText={t('Create your first virtual model')}
      buttonIcon="add"
      onButtonClick={openNew}
    />
  {:else}
    <div class="table" use:squircle={18}>
    <Table
      columns={[
        { key: 'id', title: 'ID', width: '2fr' },
        { key: 'desc', title: t('Description'), width: '2fr' },
        { key: 'caps', title: t('Capabilities'), width: '25%' },
        { key: 'actions', title: t('Actions'), align: 'right' },
      ]}
      rows={resource.data.filter((a) => a)}
      rowKey={(vm) => vm.id}
    >
      {#snippet cell({ column, row })}
        {@const vm = row as VirtualModel}
        {#if column.key === 'id'}
          <span class="mono">{vm.id}</span>
        {:else if column.key === 'desc'}
          <span class="vm-desc">{vm.description || vm.name || '—'}</span>
        {:else if column.key === 'caps'}
          <CapabilityChips caps={vm.capabilities ?? []} />
        {:else}
          <span class="vm-actions">
            <Button style="icon" icon={{ name: 'edit' }} ariaLabel={t('Edit')} onclick={() => openEdit(vm)} />
            <Button style="icon" danger icon={{ name: 'delete' }} ariaLabel={t('Delete')} onclick={() => remove(vm)} />
          </span>
        {/if}
      {/snippet}
      {#snippet empty()}
        <EmptyState
          icon="robot"
          message={t('No virtual models yet')}
          buttonText={t('Create your first virtual model')}
          buttonIcon="add"
          onButtonClick={openNew}
        />
      {/snippet}
    </Table>
    </div>
  {/if}
</div>

<style>
  .page {
    max-width: 1200px;
  }

  .table-row {
    grid-template-columns: minmax(0, 2fr) minmax(0, 2fr) 25% auto;
  }

  .col-id {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-desc {
    overflow-wrap: break-word;
    white-space: normal;
  }

  .col-caps {
    display: grid;
    grid-template-columns: repeat(4, max-content);
    gap: 4px;
    justify-content: start;
    align-content: start;
  }

  .col-actions {
    display: flex;
    gap: 4px;
    justify-content: flex-end;
  }

  .loading {
    text-align: center;
    padding: 32px;
    color: var(--color-text-soft);
  }
</style>
