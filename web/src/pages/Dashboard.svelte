<script lang="ts">
  import Button from '../components/ui/controls/Button.svelte'
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import Metrics from './metrics/Metrics.svelte'
  import Plugins from './plugins/Plugins.svelte'
  import VirtualModelEditorPage from './virtual-models/VirtualModelEditPage.svelte'
  import ProviderDetailPage from './providers/ProviderDetailPage.svelte'
  import ProxyPage from './proxy/ProxyPage.svelte'
  import ProxySourceDetailPage from './proxy/ProxySourceDetailPage.svelte'
  import Models from './models/Models.svelte'
  import Providers from './providers/Providers.svelte'
  import Tokens from './tokens/Tokens.svelte'
  import VirtualModels from './virtual-models/VirtualModels.svelte'
  import SettingsPage from './settings/SettingsPage.svelte'
  import type { PluginsTab } from './plugins/Plugins.svelte'
  import { t } from '../lib/i18n.svelte'

  let { onlogout } = $props<{ onlogout: () => void }>()

  // One source of truth: the tuple drives both the type and the runtime
  // validation below. Previously the same nine ids were written twice and
  // drifted apart the moment a section was added.
  const PANELS = ['metrics', 'providers', 'models', 'virtual', 'tokens', 'plugins', 'proxy', 'settings', 'ui-test'] as const
  type PanelId = (typeof PANELS)[number]

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
    if (segments.length === 0) {
      panel = 'metrics'
      routeSegments = []
      window.location.hash = '#/metrics'
      return
    }

    const nextPanel = segments[0] as PanelId
    if (!(PANELS as readonly string[]).includes(nextPanel)) {
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
    navigateTo('settings')
  }

  async function logout(): Promise<void> {
    await api.logout()
    onlogout?.()
  }

  const nav: NavItem[] = [
    { id: 'metrics',      label: 'Metrics',      icon: 'analytics' },
    { id: 'providers',    label: 'Providers',    icon: 'cloud' },
    { id: 'models',       label: 'Models',       icon: 'view_list' },
    { id: 'plugins',      label: 'Plugins',      icon: 'extension' },
    { id: 'virtual',      label: 'Virtual models', icon: 'robot' },
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
      <Button onclick={() => { drawerOpen = true }} ariaLabel={t('Open menu')} title={t('Menu')} icon={{ name: 'menu' }} />
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
      <button class="logout-btn" class:active={panel === 'settings'} onclick={openSettings} aria-label={t('Open settings')} title={collapsed ? t('Settings') : undefined}>
        <span class="icon">settings</span>
        <span class="label">{t('Settings')}</span>
      </button>
      <button class="logout-btn" onclick={logout} title={collapsed ? t('Sign out') : undefined}>
        <span class="icon">logout</span>
        <span class="label">{t('Sign out')}</span>
      </button>
    </div>
  </aside>

  <main class="main">
    <div class="main-content">
      {#if panel === 'metrics'}
        <Metrics />
      {:else if panel === 'providers'}
        {#if routeSegments[0]}
          <ProviderDetailPage providerId={routeSegments[0]} />
        {:else}
          <Providers />
        {/if}
      {:else if panel === 'models'}
        <Models />
      {:else if panel === 'virtual'}
        {#if routeSegments[0] === 'new'}
          <VirtualModelEditorPage vmId={null} />
        {:else if routeSegments[0]}
          <VirtualModelEditorPage vmId={routeSegments[0]} />
        {:else}
          <VirtualModels />
        {/if}
      {:else if panel === 'tokens'}
        <Tokens />
      {:else if panel === 'plugins'}
        <Plugins tab={pluginsTab} ontabchange={selectPluginsTab} />
      {:else if panel === 'proxy'}
        {#if routeSegments[0] === 'source' && routeSegments[1]}
          <ProxySourceDetailPage sourceKey={decodeURIComponent(routeSegments[1])} />
        {:else}
          <ProxyPage />
        {/if}
      {:else if panel === 'settings'}
        <SettingsPage />
      {:else if panel === 'ui-test'}
        <UiTest />
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
    margin-bottom: var(--space-6);
    min-height: 24px;
  }
  .brand {
    font-size: var(--text-sm);
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
    padding: var(--space-2);
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
    gap: var(--space-2);
    padding: 0 var(--space-4);
  }
  .nav-item {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--space-3);
    text-align: left;
    padding: var(--space-3) var(--space-4);
    border-radius: 8px;
    font-size: var(--text-base);
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
  .nav-item.active,
  .logout-btn.active {
    background: var(--color-nav-active);
    color: var(--color-text);
  }
  .nav-item .icon {
    font-size: var(--text-lg);
  }
  .sidebar-footer {
    margin-top: auto;
    padding: 0 var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .logout-btn {
    display: flex;
    align-items: center;
    justify-content: flex-start;
    gap: var(--space-3);
    width: 100%;
    text-align: left;
    padding: var(--space-3) var(--space-4);
    border-radius: 8px;
    font-size: var(--text-base);
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
    font-size: var(--text-lg);
  }
  .main {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-7);
    background: var(--color-surface);
  }
  .main-content {
    width: 100%;
    max-width: 1200px;
    margin: 0 auto;
  }

  /* ── Phone layout: app bar + drawer sidebar ── */
  .appbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    height: 52px;
    padding: 0 var(--space-5);
    flex-shrink: 0;
    background: var(--color-sidebar-bg);
    border-bottom: 1px solid var(--color-outline-light);
  }
  .appbar-brand {
    font-size: var(--text-sm);
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
    border-right: 1px solid var(--color-outline-soft);
  }
  .layout.mobile .sidebar.drawer-open {
    transform: translateX(0);
    visibility: visible;
    transition:
      transform 0.3s cubic-bezier(0.32, 0.72, 0, 1),
      visibility 0s;
  }
  .layout.mobile .main {
    padding: var(--space-5);
  }
</style>
