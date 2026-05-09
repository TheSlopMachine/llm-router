<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal'
  import ProviderCard from './ProviderCard.svelte'
  import ProviderDetailModal from './ProviderDetailModal.svelte'
  import type { Provider, ProviderStats } from '../lib/types'

  let providers: Provider[] = []
  let providerStats: Record<string, ProviderStats> = {}
  let loading: boolean = true
  let error: string = ''

  onMount(load)

  async function load(): Promise<void> {
    loading = true
    error = ''
    try {
      [providers, providerStats] = await Promise.all([
        api.providers.list(),
        api.providers.stats(),
      ])
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  async function openProviderDetail(provider: Provider): Promise<void> {
    try {
      const allCredentials = await api.credentials.list()
      const providerCreds = allCredentials.filter(c => c.provider_id === provider.id)

      modal.open({
        title: `${provider.name} Credentials`,
        content: ProviderDetailModal,
        severity: 'medium',
        size: 'large',
        props: {
          provider,
          credentials: providerCreds,
          onUpdate: async () => {
            // Reload data and update the modal's credentials
            const updatedCreds = await api.credentials.list()
            const updatedProviderCreds = updatedCreds.filter(c => c.provider_id === provider.id)
            
            // Update the modal props
            modal.updateProps({ credentials: updatedProviderCreds })
            
            // Reload provider stats
            await load()
          },
          onComplete: async () => {
            modal.close()
            await load()
          }
        }
      })
    } catch (e) {
      error = (e as Error).message
    }
  }
</script>

<div class="page-header">
  <h1>Providers</h1>
  <p>Registered upstream LLM backends.</p>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if loading}
  <div class="empty">Loading…</div>
{:else if providers.length === 0}
  <div class="empty">No providers registered yet.</div>
{:else}
  <div class="providers-grid">
    {#each providers as provider}
      <ProviderCard 
        {provider}
        stats={providerStats[provider.id] || null}
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

  .empty {
    padding: 32px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
  }
</style>