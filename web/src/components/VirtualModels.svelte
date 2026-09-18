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
    <div class="error-msg">{resource.error}</div>
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
      <div class="table-row table-head">
        <span class="col-id">ID</span>
        <span class="col-desc">{t('Description')}</span>
        <span class="col-caps">{t('Capabilities')}</span>
        <span class="col-actions">{t('Actions')}</span>
      </div>
      {#each resource.data.filter((a) => a) as vm (vm.id)}
        <div class="table-row">
          <span class="col-id mono">{vm.id}</span>
          <span class="col-desc">{vm.description || vm.name || '—'}</span>
          <span class="col-caps">
            <CapabilityChips caps={vm.capabilities ?? []} />
          </span>
          <span class="col-actions">
            <Button style="icon" icon={{ name: 'edit' }} ariaLabel={t('Edit')} onclick={() => openEdit(vm)} />
            <Button style="icon" danger icon={{ name: 'delete' }} ariaLabel={t('Delete')} onclick={() => remove(vm)} />
          </span>
        </div>
      {/each}
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
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-caps {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
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
