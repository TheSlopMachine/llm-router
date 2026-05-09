<script lang="ts">
  import { onMount, createEventDispatcher } from 'svelte'
  import { api } from '../lib/api'
  import Overview from '../components/Overview.svelte'
  import Metrics from './Metrics.svelte'
  import Providers from '../components/Providers.svelte'
  import Tokens from '../components/Tokens.svelte'
  import Credentials from '../components/Credentials.svelte'
  import Agents from '../components/Agents.svelte'
  import ThemeToggle from '../components/ThemeToggle.svelte'

  const dispatch = createEventDispatcher<{ logout: void }>()
  
  type PanelId = 'overview' | 'metrics' | 'providers' | 'tokens' | 'credentials' | 'agents'
  
  interface NavItem {
    id: PanelId
    label: string
    icon: string
  }
  
  let panel: PanelId = 'overview'
  
  function navigateTo(panelId: PanelId): void {
    panel = panelId
    window.location.hash = '#/' + panelId
  }
  
  onMount((): void => {
    const hash = window.location.hash.slice(2)
    const validPanels: PanelId[] = ['overview', 'metrics', 'providers', 'tokens', 'credentials', 'agents']
    
    if (validPanels.includes(hash as PanelId)) {
      panel = hash as PanelId
    } else {
      panel = 'overview'
      window.location.hash = '#/overview'
    }
    
    window.addEventListener('hashchange', (): void => {
      const route = window.location.hash.slice(2)
      if (validPanels.includes(route as PanelId)) {
        panel = route as PanelId
      } else {
        panel = 'overview'
        window.location.hash = '#/overview'
      }
    })
  })
  
  async function logout(): Promise<void> {
    await api.logout()
    dispatch('logout')
  }
  
  const nav: NavItem[] = [
    { id: 'overview',     label: 'Overview',     icon: 'dashboard' },
    { id: 'metrics',      label: 'Metrics',      icon: 'analytics' },
    { id: 'providers',    label: 'Providers',    icon: 'cloud' },
    { id: 'agents',       label: 'Agents',       icon: 'robot' },
    { id: 'tokens',       label: 'Tokens',       icon: 'key' },
    { id: 'credentials',  label: 'Credentials',  icon: 'lock' },
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

  <main class="main">
    <div class="main-content">
      {#if panel === 'overview'}
        <Overview on:navigate={(e) => navigateTo(e.detail)} />
      {:else if panel === 'metrics'}
        <Metrics />
      {:else if panel === 'providers'}
        <Providers />
      {:else if panel === 'agents'}
        <Agents />
      {:else if panel === 'tokens'}
        <Tokens />
      {:else if panel === 'credentials'}
        <Credentials />
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
  .main-content {
    width: 100%;
    max-width: 1200px;
    margin: 0 auto;
  }
</style>
