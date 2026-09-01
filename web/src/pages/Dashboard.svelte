<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte'
  import { api } from '../lib/api'
  import Chat from '../components/Chat.svelte'
  import Metrics from './Metrics.svelte'
  import AgentEditorPage from './AgentEditorPage.svelte'
  import Models from '../components/Models.svelte'
  import Providers from '../components/Providers.svelte'
  import Tokens from '../components/Tokens.svelte'
  import Agents from '../components/Agents.svelte'
  import ThemeToggle from '../components/ThemeToggle.svelte'

  const dispatch = createEventDispatcher<{ logout: void }>()
  
  type PanelId = 'chat' | 'metrics' | 'providers' | 'models' | 'agents' | 'tokens'
  
  interface NavItem {
    id: PanelId
    label: string
    icon: string
  }
  
  let panel: PanelId = 'metrics'
  let routeSegments: string[] = []

  function navigateTo(panelId: PanelId): void {
    panel = panelId
    window.location.hash = '#/' + panelId
  }

  function applyRoute(): void {
    const raw = window.location.hash.replace(/^#\/?/, '')
    const segments = raw.split('/').filter(Boolean)
    const validPanels: PanelId[] = ['chat', 'metrics', 'providers', 'models', 'agents', 'tokens']

    if (segments.length === 0) {
      panel = 'metrics'
      routeSegments = []
      window.location.hash = '#/metrics'
      return
    }

    const nextPanel = segments[0] as PanelId
    if (!validPanels.includes(nextPanel)) {
      panel = 'metrics'
      routeSegments = []
      window.location.hash = '#/metrics'
      return
    }

    panel = nextPanel
    routeSegments = segments.slice(1)
  }
  
  onMount((): void => {
    applyRoute()
    window.addEventListener('hashchange', applyRoute)
  })
  
  async function logout(): Promise<void> {
    await api.logout()
    dispatch('logout')
  }
  
  const nav: NavItem[] = [
    { id: 'chat',         label: 'Chat',         icon: 'chat' },
    { id: 'metrics',      label: 'Metrics',      icon: 'analytics' },
    { id: 'providers',    label: 'Providers',    icon: 'cloud' },
    { id: 'models',       label: 'Models',       icon: 'view_list' },
    { id: 'agents',       label: 'Agents',       icon: 'robot' },
    { id: 'tokens',       label: 'Tokens',       icon: 'key' },
  ]
</script>

<div class="layout">
  <aside class="sidebar">
    <div class="brand">llm-router</div>
    <nav>
      {#each nav as item}
        <button
          class="nav-item"
          class:active={panel === item.id}
          on:click={() => navigateTo(item.id)}
        >
          <span class="icon">{item.icon}</span>
          <span>{item.label}</span>
        </button>
      {/each}
    </nav>
    <div class="sidebar-footer">
      <ThemeToggle />
      <button class="logout-btn" on:click={logout}>
        <span class="icon">logout</span>
        <span>Sign out</span>
      </button>
    </div>
  </aside>

  <main class="main" class:chat={panel === 'chat'}>
    <div class="main-content" class:chat={panel === 'chat'}>
      {#if panel === 'chat'}
        <Chat />
      {:else if panel === 'metrics'}
        <Metrics />
      {:else if panel === 'providers'}
        <Providers />
      {:else if panel === 'models'}
        <Models />
      {:else if panel === 'agents'}
        {#if routeSegments[0] === 'new'}
          <AgentEditorPage agentId={null} />
        {:else if routeSegments[0]}
          <AgentEditorPage agentId={routeSegments[0]} />
        {:else}
          <Agents />
        {/if}
      {:else if panel === 'tokens'}
        <Tokens />
      {/if}
    </div>
  </main>
</div>

<style>
  .layout {
    display: flex;
    height: 100vh;
    overflow: hidden;
  }
  .sidebar {
    width: var(--sidebar-w);
    flex-shrink: 0;
    background: var(--color-sidebar-bg);
    border-right: 1px solid var(--color-outline-light);
    display: flex;
    flex-direction: column;
    padding: 20px 0;
  }
  .brand {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--color-text);
    padding: 0 20px;
    margin-bottom: 24px;
  }
  nav { 
    display: flex; 
    flex-direction: column; 
    gap: 4px; 
    padding: 0 12px; 
  }
  .nav-item {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: 8px;
    text-align: left;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 14px;
    font-weight: 500;
    color: var(--color-text-soft);
    background: none;
    border: none;
    cursor: pointer;
    width: 100%;
  }
  .nav-item:hover { 
    background: var(--color-nav-hover); 
    color: var(--color-text); 
  }
  .nav-item.active { 
    background: var(--color-nav-active); 
    color: var(--color-text); 
  }
  .nav-item .icon {
    font-size: 20px;
  }
  .sidebar-footer {
    margin-top: auto;
    padding: 0 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .logout-btn {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: 8px;
    width: 100%;
    text-align: left;
    padding: 8px 12px;
    border-radius: 8px;
    font-size: 14px;
    color: var(--color-text-soft);
    background: none;
    border: none;
    cursor: pointer;
    transition: none;
  }
  .logout-btn:hover { 
    color: var(--color-text); 
    background: var(--color-nav-hover); 
  }
  .logout-btn .icon {
    font-size: 20px;
  }
  .main {
    flex: 1;
    overflow-y: auto;
    padding: 32px;
    background: var(--color-surface);
  }
  .main.chat {
    padding: 0;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .main-content {
    width: 100%;
    max-width: 1200px;
    margin: 0 auto;
  }
  .main-content.chat {
    max-width: none;
    flex: 1;
    display: flex;
    margin: 0;
    min-height: 0;
  }
</style>
