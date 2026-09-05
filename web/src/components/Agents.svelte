<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import type { Agent } from '../lib/types'
  import EmptyState from './EmptyState.svelte'

  let agents = $state<Agent[]>([])
  let loading = $state(true)
  let error = $state('')

  async function load() {
    loading = true
    error = ''
    try {
      const response = await api.agents.list()
      agents = response as Agent[]
    } catch (e: any) {
      if (e.status === 401 || e.message?.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  function openNewAgent() {
    window.location.hash = '#/agents/new'
  }

  onMount(() => {
    load()
  })

  function openEditAgent(agent: Agent) {
    window.location.hash = `#/agents/${agent.id}`
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
      error = getErrorMessage(e)
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
      <button class="btn btn-primary" onclick={openNewAgent}>
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
                <button class="btn btn-secondary btn-small" onclick={() => openEditAgent(agent)}>
                  <span class="icon">edit</span>
                  Edit
                </button>
                <button class="btn btn-danger btn-small" onclick={() => deleteAgent(agent)}>
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
