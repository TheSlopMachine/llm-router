<script lang="ts">
  import Icon from '../../components/ui/controls/Icon.svelte'
  import Button from '../../components/ui/controls/Button.svelte'
  import Text from '../../components/ui/controls/Text.svelte'
  import { modal } from '../../lib/modal.svelte'
  import { api } from '../../lib/api'
  import { createListResource } from '../../lib/list-resource.svelte'
  import CustomProviderWizard from '../../components/wizards/CustomProviderWizard.svelte'
  import Switch from '../../components/ui/controls/Switch.svelte'
  import List from '../../components/ui/composite/List.svelte'
  import { squircle } from '../../lib/squircle'
  import type { Provider, ProviderStats } from '../../lib/types'
  import { t, n } from '../../lib/i18n.svelte'

  const resource = createListResource<{ providers: Provider[]; providerStats: Record<string, ProviderStats> }>(
    async () => {
      const [p, s] = await Promise.all([api.providers.list(), api.providers.stats()])
      return { providers: p, providerStats: s }
    },
    { providers: [], providerStats: {} }
  )

  let visibleProviders = $derived(resource.data.providers)

  function openProviderDetail(provider: Provider): void {
    window.location.hash = '#/providers/' + provider.id
  }

  function onRowKeydown(e: KeyboardEvent, provider: Provider): void {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      openProviderDetail(provider)
    }
  }

  async function toggleProvider(provider: Provider, enabled: boolean): Promise<void> {
    resource.error = ''
    try {
      await api.providers.updateInstance(provider.id, { name: provider.name, disabled: !enabled })
      await resource.reload()
    } catch (e) {
      resource.error = e instanceof Error ? e.message : String(e)
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
</script>

<div class="page-header">
  <div>
    <h1>{t('Providers')}</h1>
    <p>{t('Registered upstream LLM backends.')}</p>
  </div>
  <Button style="prominent" onclick={openCreate} icon={{ name: 'add' }}>{t('New Provider')}</Button>
</div>

{#if resource.error}
  <div class="error-msg">{resource.error}</div>
{/if}

{#if resource.loading}
  <div class="empty">{t('Loading…')}</div>
{:else if visibleProviders.length === 0}
  <div class="empty">{t('No providers yet. Add one to get started.')}</div>
{:else}
  <List>
    <div class="table-row table-head">
      <span class="col-icon"></span>
      <span class="col-name">{t('Provider')}</span>
      <span class="col-creds">{t('Credentials')}</span>
      <span class="col-models">{t('Models')}</span>
      <span class="col-toggle"></span>
    </div>
    {#each visibleProviders as provider (provider.id)}
      {@const stats = resource.data.providerStats[provider.id] || null}
      <div
        class="table-row row-clickable"
        class:row-disabled={provider.disabled}
        role="button"
        tabindex="0"
        onclick={() => openProviderDetail(provider)}
        onkeydown={(e) => onRowKeydown(e, provider)}
      >
        <span class="col-icon">
          {#if provider.icon_url}
            <img src={provider.icon_url} alt="" class="provider-icon" use:squircle={10} />
          {:else}
            <span class="provider-icon-fallback"><Icon name="cloud" size="lg" /></span>
          {/if}
        </span>
        <span class="col-name">
          <span class="name-col">
            <span class="display-name">{provider.name}</span>
            <span class="subtle">{provider.type}{provider.qualifier ? ':' + provider.qualifier : ''}</span>
            {#if provider.disabled && provider.disabled_by === 'system'}
              <Text size="sm" tone="danger">{t('Disabled automatically')}{provider.disabled_reason ? `: ${provider.disabled_reason}` : ''}</Text>
            {/if}
          </span>
        </span>
        <span class="col-creds">{n(stats?.credential_count ?? 0, 'active', 'active', 'активен', 'активны', 'активно')}</span>
        <span class="col-models">{stats?.model_count ?? 0}</span>
        <!-- The switch eats its own clicks/keys so row activation never fires
             from the toggle cell. -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <span
          class="col-toggle"
          onclick={(e) => e.stopPropagation()}
          onkeydown={(e) => e.stopPropagation()}
        >
          <Switch
            checked={!provider.disabled}
            ariaLabel={t('Enable provider')}
            onchange={(v) => toggleProvider(provider, v)}
          />
        </span>
      </div>
    {/each}
  </List>
{/if}

<style>
  /* Row grid + states, scoped: container, shape and dividers come from List. */
  .table-row {
    display: grid;
    align-items: center;
    padding: 10px 16px;
    gap: var(--space-4);
  }
  .table-head {
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--color-text-soft);
  }
  .table-row {
    grid-template-columns: 44px minmax(0, 2fr) minmax(0, 0.8fr) minmax(0, 0.7fr) 72px;
  }

  .row-clickable { cursor: pointer; }

  .row-clickable:hover {
    background: var(--color-hover-bg);
  }

  .row-clickable:focus-visible {
    box-shadow: inset 0 0 0 2px var(--color-accent);
    outline: none;
  }

  .row-disabled { opacity: 0.55; }


  .col-icon {
    display: flex;
    align-items: center;
  }

  .provider-icon {
    width: 32px;
    height: 32px;
    border-radius: 10px;
    object-fit: contain;
    flex-shrink: 0;
  }

  .provider-icon-fallback {
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-surface-container-highest);
    border-radius: 10px;
    font-size: var(--text-lg);
    color: var(--color-text-soft);
    flex-shrink: 0;
  }

  .col-name {
    min-width: 0;
    display: flex;
    align-items: center;
  }

  .name-col {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .display-name {
    font-size: var(--text-base);
    font-weight: 500;
    color: var(--color-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .subtle {
    color: var(--color-text-soft);
    font-size: var(--text-sm);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .col-creds,
  .col-models {
    font-size: var(--text-sm);
    color: var(--color-text-soft);
    white-space: nowrap;
  }

  .col-toggle {
    display: flex;
    justify-content: flex-end;
  }

  @media (max-width: 900px) {
    .table-row {
      grid-template-columns: 44px minmax(0, 2fr) minmax(0, 0.8fr) 72px;
    }
    .col-models {
      display: none;
    }
  }
</style>
