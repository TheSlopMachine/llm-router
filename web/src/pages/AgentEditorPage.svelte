<script lang="ts">
  import AgentEditor from '../components/wizards/AgentEditor.svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import type { Agent } from '../lib/types'
  import { t } from '../lib/i18n.svelte'

  let { agentId = null } = $props<{ agentId: string | null }>()

  let agent = $state<Agent | undefined>(undefined)
  let loading = $state(false)
  let error = $state('')

  async function loadAgent() {
    error = ''
    agent = undefined

    if (!agentId) {
      return
    }

    loading = true
    try {
      agent = await api.agents.get(agentId) as Agent
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if ((e as { status?: number })?.status === 401 || msg.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = msg
    } finally {
      loading = false
    }
  }

  function backToAgents() {
    window.location.hash = '#/agents'
  }

  $effect(() => {
    void agentId
    void loadAgent()
  })
</script>

<div class="page-header">
  <div>
    <h1>{agentId ? t('Edit Agent') : t('New Agent')}</h1>
    <p>{agentId ? t('Update routing, models, and instructions for this agent.') : t('Create a virtual model that orchestrates requests across multiple providers.')}</p>
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{:else if loading}
  <div class="loading">{t('Loading agent...')}</div>
{:else}
  <AgentEditor
    {agent}
    onComplete={backToAgents}
    onCancel={backToAgents}
  />
{/if}
