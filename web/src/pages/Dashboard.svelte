<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import Chat from '../components/Chat.svelte'
  import Metrics from './Metrics.svelte'
  import AgentEditorPage from './AgentEditorPage.svelte'
  import ProviderDetailPage from './ProviderDetailPage.svelte'
  import ProxyPage from './ProxyPage.svelte'
  import Models from '../components/Models.svelte'
  import Providers from '../components/Providers.svelte'
  import Tokens from '../components/Tokens.svelte'
  import Agents from '../components/Agents.svelte'
  import PluginsPage, { type PluginsTab } from './PluginsPage.svelte'
  import { modal } from '../lib/modal.svelte'
  import SettingsModal from '../components/SettingsModal.svelte'
  import { t } from '../lib/i18n.svelte'

  let { onlogout } = $props<{ onlogout: () => void }>()

  type PanelId = 'chat' | 'metrics' | 'providers' | 'models' | 'agents' | 'tokens' | 'plugins' | 'proxy'

  interface NavItem {
    id: PanelId
    label: string
    icon: string
  }

  let panel = $state<PanelId>('metrics')
  let routeSegments = $state<string[]>([])

  function navigateTo(panelId: PanelId): void {
    panel = panelId
    window.location.hash = '#/' + panelId
    drawerOpen = false
  }

  function applyRoute(): void {
    const raw = window.location.hash.replace(/^#\/?/, '')
    const segments = raw.split('/').filter(Boolean)
    if (segments[0] === 'store') {
      panel = 'plugins'
      routeSegments = ['catalog']
      window.location.hash = '#/plugins/catalog'
      return
    }
    const validPanels: PanelId[] = ['chat', 'metrics', 'providers', 'models', 'agents', 'tokens', 'plugins', 'proxy']

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

  $effect(() => {
    applyRoute()
    window.addEventListener('hashchange', applyRoute)
    return () => window.removeEventListener('hashchange', applyRoute)
  })

  function openSettings(): void {
    modal.open({ title: 'Settings', content: SettingsModal })
  }

  async function logout(): Promise<void> {
    await api.logout()
    onlogout?.()
  }

  const nav: NavItem[] = [
    { id: 'chat',         label: 'Chat',         icon: 'chat' },
    { id: 'metrics',      label: 'Metrics',      icon: 'analytics' },
    { id: 'providers',    label: 'Providers',    icon: 'cloud' },
    { id: 'models',       label: 'Models',       icon: 'view_list' },
    { id: 'plugins',      label: 'Plugins',      icon: 'extension' },
    { id: 'agents',       label: 'Agents',       icon: 'robot' },
    { id: 'tokens',       label: 'Tokens',       icon: 'key' },
    { id: 'proxy',        label: 'Proxies',      icon: 'vpn_lock' },
  ]

  let pluginsTab = $derived<PluginsTab>(routeSegments[0] === 'catalog' ? 'catalog' : 'installed')

  function selectPluginsTab(next: PluginsTab): void {
    window.location.hash = '#/plugins/' + next
  }

  const SIDEBAR_KEY = 'llmr_sidebar_collapsed'
  let collapsedOverride = $state<boolean | null>(
    localStorage.getItem(SIDEBAR_KEY) === null ? null : localStorage.getItem(SIDEBAR_KEY) === '1'
  )
  let narrow = $state(false)
  let mobile = $state(false)
  let drawerOpen = $state(false)

  // Manual toggle wins; otherwise the sidebar follows viewport width.
  // On phone widths the sidebar becomes a drawer instead of a rail.
  let collapsed = $derived(collapsedOverride ?? (narrow && !mobile))

  onMount(() => {
    const narrowMql = window.matchMedia('(max-width: 1024px)')
    const mobileMql = window.matchMedia('(max-width: 768px)')
    narrow = narrowMql.matches
    mobile = mobileMql.matches
    const onNarrow = (e: MediaQueryListEvent) => { narrow = e.matches }
    const onMobile = (e: MediaQueryListEvent) => {
      mobile = e.matches
      if (!e.matches) drawerOpen = false
    }
    narrowMql.addEventListener('change', onNarrow)
    mobileMql.addEventListener('change', onMobile)
    return () => {
      narrowMql.removeEventListener('change', onNarrow)
      mobileMql.removeEventListener('change', onMobile)
    }
  })

  function toggleSidebar(): void {
    collapsedOverride = !collapsed
    localStorage.setItem(SIDEBAR_KEY, collapsedOverride ? '1' : '0')
  }
</script>

<div class="layout" class:mobile>
  {#if mobile}
    <header class="appbar">
      <button class="btn-icon" onclick={() => { drawerOpen = true }} aria-label={t('Open menu')} title={t('Menu')}>
        <span class="icon">menu</span>
      </button>
      <div class="appbar-brand">llm-router</div>
    </header>
    {#if drawerOpen}
      <button class="scrim" aria-label={t('Close menu')} onclick={() => { drawerOpen = false }}></button>
    {/if}
  {/if}
  <aside class="sidebar" class:collapsed={collapsed && !mobile} class:drawer={mobile} class:drawer-open={drawerOpen}>
    <div class="brand-row">
      <div class="brand">llm-router</div>
      {#if mobile}
        <button
          class="collapse-btn"
          onclick={() => { drawerOpen = false }}
          aria-label={t('Close menu')}
          title={t('Close menu')}
        >
          <span class="icon">close</span>
        </button>
      {:else}
        <button
          class="collapse-btn"
          onclick={toggleSidebar}
          aria-label={collapsed ? t('Expand sidebar') : t('Collapse sidebar')}
          title={collapsed ? t('Expand sidebar') : t('Collapse sidebar')}
        >
          <span class="icon">{collapsed ? 'left_panel_open' : 'left_panel_close'}</span>
        </button>
      {/if}
    </div>
    <nav>
      {#each nav as item}
        <button
          class="nav-item"
          class:active={panel === item.id}
          onclick={() => navigateTo(item.id)}
          title={collapsed ? t(item.label) : undefined}
        >
          <span class="icon">{item.icon}</span>
          <span class="label">{t(item.label)}</span>
        </button>
      {/each}
    </nav>
    <div class="sidebar-footer">
      <button class="logout-btn" onclick={openSettings} aria-label={t('Open settings')} title={collapsed ? t('Settings') : undefined}>
        <span class="icon">settings</span>
        <span class="label">{t('Settings')}</span>
      </button>
      <button class="logout-btn" onclick={logout} title={collapsed ? t('Sign out') : undefined}>
        <span class="icon">logout</span>
        <span class="label">{t('Sign out')}</span>
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
        {#if routeSegments[0]}
          <ProviderDetailPage providerId={routeSegments[0]} />
        {:else}
          <Providers />
        {/if}
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
      {:else if panel === 'plugins'}
        <PluginsPage tab={pluginsTab} ontabchange={selectPluginsTab} />
      {:else if panel === 'proxy'}
        <ProxyPage />
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
    transition: width 0.28s cubic-bezier(0.3, 1.15, 0.5, 1);
  }
  .sidebar.collapsed {
    width: 68px;
  }
  /* Padding and justify stay constant in both states: when the brand
     collapses to zero width, the toggle button lands exactly centered. */
  .brand-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 20px;
    margin-bottom: 24px;
    min-height: 24px;
  }
  .brand {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--color-text);
    white-space: nowrap;
    overflow: hidden;
    max-width: 200px;
    opacity: 1;
    transform: translateX(0);
    transition:
      max-width 0.28s cubic-bezier(0.3, 1.15, 0.5, 1),
      opacity 0.15s ease,
      transform 0.22s ease;
  }
  .sidebar.collapsed .brand {
    max-width: 0;
    opacity: 0;
    transform: translateX(-10px);
  }
  .collapse-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 4px;
    border-radius: 6px;
    color: var(--color-text-soft);
    background: none;
    border: none;
    cursor: pointer;
    transition: background-color 0.15s ease, transform 0.12s ease;
  }
  .collapse-btn:hover {
    background: var(--color-nav-hover);
    color: var(--color-text);
  }
  .collapse-btn:active {
    transform: scale(0.88);
  }
  .nav-item .label,
  .logout-btn .label {
    white-space: nowrap;
    overflow: hidden;
    max-width: 160px;
    opacity: 1;
    transition:
      max-width 0.28s cubic-bezier(0.3, 1.15, 0.5, 1),
      opacity 0.15s ease;
  }
  .sidebar.collapsed .label {
    max-width: 0;
    opacity: 0;
  }
  /* justify and padding stay identical in both states: with 8px 12px
     padding the icon sits exactly centered in the collapsed column,
     so the icon never jumps — only gap and label width animate. */
  .sidebar.collapsed .nav-item,
  .sidebar.collapsed .logout-btn {
    gap: 0;
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
    transition:
      background-color 0.15s ease,
      color 0.15s ease,
      transform 0.12s ease,
      gap 0.28s cubic-bezier(0.3, 1.15, 0.5, 1);
  }
  .nav-item:active {
    transform: scale(0.96);
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
    transition:
      background-color 0.15s ease,
      color 0.15s ease,
      transform 0.12s ease,
      gap 0.28s cubic-bezier(0.3, 1.15, 0.5, 1);
  }
  .logout-btn:active {
    transform: scale(0.96);
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

  /* ── Phone layout: app bar + drawer sidebar ── */
  .appbar {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 52px;
    padding: 0 16px;
    flex-shrink: 0;
    background: var(--color-sidebar-bg);
    border-bottom: 1px solid var(--color-outline-light);
  }
  .appbar-brand {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--color-text);
  }
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 30;
    background: rgba(0, 0, 0, 0.45);
    border: none;
    padding: 0;
    cursor: default;
    animation: scrim-in 0.2s ease;
  }
  @keyframes scrim-in {
    from { opacity: 0; }
  }
  .layout.mobile {
    flex-direction: column;
  }
  .layout.mobile .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    width: 270px;
    z-index: 40;
    transform: translateX(-105%);
    visibility: hidden;
    transition:
      transform 0.3s cubic-bezier(0.32, 0.72, 0, 1),
      visibility 0s linear 0.3s;
    box-shadow: var(--shadow-xl);
  }
  .layout.mobile .sidebar.drawer-open {
    transform: translateX(0);
    visibility: visible;
    transition:
      transform 0.3s cubic-bezier(0.32, 0.72, 0, 1),
      visibility 0s;
  }
  .layout.mobile .main {
    padding: 16px;
  }
</style>
