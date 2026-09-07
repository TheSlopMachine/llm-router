<script lang="ts">
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import { createListResource } from '../lib/list-resource.svelte'
  import ProviderCard from './ProviderCard.svelte'
  import ProviderDetailModal from './ProviderDetailModal.svelte'
  import CustomProviderWizard from './wizards/CustomProviderWizard.svelte'
  import type { Provider, ProviderStats } from '../lib/types'

  const resource = createListResource<{ providers: Provider[]; providerStats: Record<string, ProviderStats> }>(
    async () => {
      const [p, s] = await Promise.all([api.providers.list(), api.providers.stats()])
      return { providers: p, providerStats: s }
    },
    { providers: [], providerStats: {} }
  )

  let visibleProviders = $derived(resource.data.providers)

  async function openProviderDetail(provider: Provider): Promise<void> {
    try {
      const allCredentials = (await api.credentials.list()) as any[]
      const providerCreds = allCredentials.filter((c: any) => c.provider_id === provider.id)

      modal.open({
        title: `${provider.name} Credentials`,
        content: ProviderDetailModal,
        severity: 'medium',
        size: 'large',
        props: {
          provider,
          credentials: providerCreds,
          onUpdate: async () => {
            const updatedCreds = (await api.credentials.list()) as any[]
            const updatedProviderCreds = updatedCreds.filter((c: any) => c.provider_id === provider.id)
            modal.updateProps({ credentials: updatedProviderCreds })
            await resource.reload()
          },
          onComplete: async () => {
            modal.close()
            await resource.reload()
          },
          onEdit: () => { modal.close(); openEdit(provider) },
          onDelete: () => deleteProvider(provider)
        }
      })
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }

  function openCreate(): void {
    resource.error = ''

    modal.open({
      title: 'New Provider',
      content: CustomProviderWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        editingProvider: null,
        onComplete: async () => {
          modal.close()
          await resource.reload()
        }
      }
    })
  }

  function openEdit(provider: Provider): void {
    resource.error = ''

    modal.open({
      title: 'Edit Provider',
      content: CustomProviderWizard,
      severity: 'medium',
      size: 'medium',
      props: {
        editingProvider: provider,
        onComplete: async () => {
          modal.close()
          await resource.reload()
        }
      }
    })
  }

  async function deleteProvider(provider: Provider): Promise<void> {
    const confirmed = await modal.confirm({
      title: 'Delete Provider',
      message: `Are you sure you want to delete "${provider.name}"? Credentials for this provider will be removed as well.`,
      severity: 'high',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true
    })

    if (!confirmed) return

    try {
      await api.providers.delete(provider.id)
      modal.close()
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }
</script>

<div class="page-header">
  <div>
    <h1>Providers</h1>
    <p>Registered upstream LLM backends.</p>
  </div>
  <button class="btn btn-primary" onclick={openCreate}>
    <span class="icon">add</span>
    New Provider
  </button>
</div>

{#if resource.error}
  <div class="error-msg">{resource.error}</div>
{/if}

{#if resource.loading}
  <div class="empty">Loading…</div>
{:else if visibleProviders.length === 0}
  <div class="empty">No providers yet. Add one to get started.</div>
{:else}
  <div class="providers-grid">
    {#each visibleProviders as provider}
      <ProviderCard
        {provider}
        stats={resource.data.providerStats[provider.id] || null}
        onClick={() => openProviderDetail(provider)}
      />
    {/each}
  </div>
{/if}

<style>
  .providers-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
    align-items: start;
  }

  @media (max-width: 1200px) {
    .providers-grid {
      grid-template-columns: repeat(3, 1fr);
    }
  }

  @media (max-width: 768px) {
    .providers-grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  @media (max-width: 640px) {
    .providers-grid {
      grid-template-columns: 1fr;
    }
  }

</style>
