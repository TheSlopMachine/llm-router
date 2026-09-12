<script lang="ts">
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import { createListResource } from '../lib/list-resource.svelte'
  import type { Agent } from '../lib/types'
  import { t } from '../lib/i18n.svelte'
  import EmptyState from './EmptyState.svelte'

  const resource = createListResource<Agent[]>(
    async () => {
      try {
        const response = (await api.agents.list()) as unknown
        return Array.isArray(response) ? (response as Agent[]) : []
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

  function openNewAgent() {
    window.location.hash = '#/agents/new'
  }

  function openEditAgent(agent: Agent) {
    window.location.hash = `#/agents/${agent.id}`
  }

  async function deleteAgent(agent: Agent) {
    const confirmed = await modal.confirm({
      title: t('Delete Agent'),
      message: `${t('Are you sure you want to delete')} "${agent.name}"? ${t('This action cannot be undone.')}`,
      severity: 'high',
      size: 'small',
      confirmText: t('Delete'),
      cancelText: t('Cancel'),
      danger: true
    })

    if (!confirmed) return

    try {
      await api.agents.delete(agent.id)
      await resource.reload()
    } catch (e) {
      resource.error = getErrorMessage(e)
    }
  }
</script>

<div class="page">
  <div class="page-header">
    <div>
      <h1>{t('Agents')}</h1>
      <p>{t('Virtual models that orchestrate requests across multiple providers with custom instructions.')}</p>
    </div>
    {#if resource.data && resource.data.length > 0}
      <button class="btn btn-primary" onclick={openNewAgent}>
        <span class="icon">add</span>
        {t('New Agent')}
      </button>
    {/if}
  </div>

  {#if resource.error}
    <div class="error-msg">{resource.error}</div>
  {/if}

  {#if resource.loading}
    <div class="loading">{t('Loading agents...')}</div>
  {:else if !resource.data || resource.data.length === 0}
    <EmptyState
      icon="robot"
      message={t('No agents yet')}
      hint={t('Create an agent to orchestrate requests across multiple models with custom instructions.')}
      buttonText={t('Create Your First Agent')}
      buttonIcon="add"
      onButtonClick={openNewAgent}
    />
  {:else}
    <div class="card">
      <div class="card-header"><h2>{t('Agents')}</h2></div>
      <table>
        <thead>
          <tr>
            <th>{t('Name')}</th>
            <th>{t('Description')}</th>
            <th>{t('Models')}</th>
            <th>{t('Decision Model')}</th>
            <th>{t('Actions')}</th>
          </tr>
        </thead>
        <tbody>
           {#each resource.data.filter(a => a) as agent}
            <tr>
              <td>
                <strong>{agent.name}</strong>
              </td>
              <td>{agent.description || '—'}</td>
              <td>
                {#if agent.is_draft}
                  <span class="badge badge-yellow">{t('Draft')}</span>
                {:else}
                    {agent.models?.length || 0}
                {/if}
              </td>
              <td>
                {#if agent.decision_model}
                  <span class="badge badge-blue">✓</span>
                {:else}
                  <span class="text-soft">—</span>
                {/if}
              </td>
              <td class="row-actions">
                <button class="btn btn-secondary btn-small" onclick={() => openEditAgent(agent)}>
                  <span class="icon">edit</span>
                  {t('Edit')}
                </button>
                <button class="btn btn-danger btn-small" onclick={() => deleteAgent(agent)}>
                  <span class="icon">delete</span>
                  {t('Delete')}
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
  .page {
    max-width: 1200px;
  }

  table {
    width: 100%;
    border-collapse: collapse;
  }

  th {
    text-align: left;
    font-size: 12px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-soft);
    padding: 12px 16px;
    border-bottom: 1px solid var(--color-outline-light);
  }

  td {
    padding: 12px 16px;
    border-bottom: 1px solid var(--color-outline-soft);
    color: var(--color-text-soft);
  }

  tr:hover {
    background: var(--color-hover-bg);
  }

  .row-actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }

  .text-soft {
    color: var(--color-text-soft);
  }
</style>
