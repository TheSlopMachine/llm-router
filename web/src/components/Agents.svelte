<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal'
  import type { Agent } from '../lib/types'
  import AgentEditor from './wizards/AgentEditor.svelte'
  import EmptyState from './EmptyState.svelte'

  let agents: Agent[] = []
  let loading = true
  let error = ''

  async function load() {
    loading = true
    error = ''
    try {
      const response = await api.agents.list()
      agents = response as Agent[]
    } catch (e: any) {
      // Handle authentication errors
      if (e.status === 401 || e.message?.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  async function openNewAgent() {
    // Check if models available first
    try {
      const models = await api.agents.availableModels()
      if (!models || (models as any[]).length === 0) {
        error = 'No models available. Please configure providers and credentials first.'
        return
      }
    } catch (e: any) {
      if (e.status === 401 || e.message?.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = 'Failed to load models. Please try again.'
      return
    }
    
    // Open modal only if models exist
    modal.open({
      title: 'New Agent',
      content: AgentEditor,
      severity: 'medium',
      size: 'extra-large',
      props: {
        agent: undefined,
        onComplete: async () => {
          modal.close()
          await load()
        }
      }
    })
  }

  onMount(() => {
    load()
  })

  function openEditAgent(agent: Agent) {
    modal.open({
      title: 'Edit Agent',
      content: AgentEditor,
      severity: 'medium',
      size: 'extra-large',
      props: {
        agent,
        onComplete: async () => {
          modal.close()
          await load()
        }
      }
    })
  }

  async function deleteAgent(agent: Agent) {
    const confirmed = await modal.confirm({
      title: 'Delete Agent',
      message: `Are you sure you want to delete "${agent.name}"? This action cannot be undone.`,
      severity: 'high',
      size: 'small',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      danger: true
    })

    if (!confirmed) return

    try {
      await api.agents.delete(agent.id)
      await load()
    } catch (e) {
      error = (e as Error).message
    }
  }
</script>

<div class="page">
  <div class="page-header">
    <div>
      <h1>Agents</h1>
      <p>Virtual models that orchestrate requests across multiple providers with custom instructions.</p>
    </div>
    {#if agents && agents.length > 0}
      <button class="btn btn-primary" on:click={openNewAgent}>
        <span class="icon">add</span>
        New Agent
      </button>
    {/if}
  </div>

  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  {#if loading}
    <div class="loading">Loading agents...</div>
  {:else if !agents || agents.length === 0}
    <EmptyState 
      icon="robot"
      message="No agents yet"
      hint="Create an agent to orchestrate requests across multiple models with custom instructions."
      buttonText="Create Your First Agent"
      buttonIcon="add"
      onButtonClick={openNewAgent}
    />
  {:else}
    <div class="card">
      <div class="card-header"><h2>Agents</h2></div>
      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Description</th>
            <th>Models</th>
            <th>Decision Model</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
           {#each agents.filter(a => a) as agent}
            <tr>
              <td>
                <strong>{agent.name}</strong>
              </td>
              <td>{agent.description || '—'}</td>
              <td>
                {#if agent.is_draft}
                  <span class="badge badge-yellow">Draft</span>
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
                <button class="btn btn-secondary btn-small" on:click={() => openEditAgent(agent)}>
                  <span class="icon">edit</span>
                  Edit
                </button>
                <button class="btn btn-danger btn-small" on:click={() => deleteAgent(agent)}>
                  <span class="icon">delete</span>
                  Delete
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

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 24px;
    gap: 24px;
  }

  .page-header h1 {
    font-size: 24px;
    font-weight: 400;
    margin: 0 0 4px 0;
  }

  .page-header p {
    color: var(--color-text-soft);
    font-size: 14px;
    margin: 0;
  }

  .loading {
    text-align: center;
    padding: 48px;
    color: var(--color-text-soft);
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
