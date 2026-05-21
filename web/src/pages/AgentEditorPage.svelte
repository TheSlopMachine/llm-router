<script lang="ts">
  import AgentEditor from '../components/wizards/AgentEditor.svelte'
  import { api } from '../lib/api'
  import type { Agent } from '../lib/types'

  export let agentId: string | null = null

  let agent: Agent | undefined
  let loading = false
  let error = ''
  let loadedAgentId: string | null | undefined

  async function loadAgent() {
    error = ''
    agent = undefined

    if (!agentId) {
      loadedAgentId = agentId
      return
    }

    loading = true
    try {
      agent = await api.agents.get(agentId) as Agent
    } catch (e: any) {
      if (e.status === 401 || e.message?.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = (e as Error).message
    } finally {
      loading = false
      loadedAgentId = agentId
    }
  }

  function backToAgents() {
    window.location.hash = '#/agents'
  }

  $: if (agentId !== loadedAgentId) {
    void loadAgent()
  }
</script>

<div class="page-header">
  <div>
    <h1>{agentId ? 'Edit Agent' : 'New Agent'}</h1>
    <p>{agentId ? 'Update routing, models, and instructions for this agent.' : 'Create a virtual model that orchestrates requests across multiple providers.'}</p>
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{:else if loading}
  <div class="loading">Loading agent...</div>
{:else}
  <AgentEditor
    {agent}
    onComplete={backToAgents}
    onCancel={backToAgents}
  />
{/if}

<style>
  .loading {
    text-align: center;
    padding: 48px;
    color: var(--color-text-soft);
  }
</style>
