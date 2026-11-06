<script lang="ts">
  import { Button, Scaffold, Sidebar, Text, type IconName } from '@fuckyouryan/fui'
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import Metrics from './metrics/Metrics.svelte'
  import Plugins from './plugins/Plugins.svelte'
  import VirtualModelEditorPage from './virtual-models/VirtualModelEditPage.svelte'
  import ProviderDetailPage from './providers/ProviderDetailPage.svelte'
  import ProxyPage from './proxy/ProxyPage.svelte'
  import Models from './models/Models.svelte'
  import Providers from './providers/Providers.svelte'
  import Tokens from './tokens/Tokens.svelte'
  import VirtualModels from './virtual-models/VirtualModels.svelte'
  import SettingsPage from './settings/SettingsPage.svelte'
  import type { PluginsTab } from './plugins/Plugins.svelte'
  import type { ProxyTab } from './proxy/ProxyPage.svelte'
  import type { MetricsTab } from './metrics/Metrics.svelte'
  import { t } from '../lib/i18n.svelte'
  import { BP_NARROW } from '../lib/breakpoints'
  import AppFooter from '../components/AppFooter.svelte'

  let { onlogout }: { onlogout: () => void } = $props()

  // One source of truth: the tuple drives both the type and the runtime
  // validation below. Previously the same nine ids were written twice and
  // drifted apart the moment a section was added.
  const PANELS = ['metrics', 'providers', 'models', 'virtual', 'tokens', 'plugins', 'proxy', 'settings'] as const
  type PanelId = (typeof PANELS)[number]

  interface NavItem {
    id: PanelId
    label: import('../lib/i18n.svelte').TranslationKey
    icon: IconName
  }

  let panel = $state<PanelId>('metrics')
  let routeSegments = $state<string[]>([])

  function navigateTo(panelId: PanelId): void {
    panel = panelId
    window.location.hash = '#/' + panelId
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
    { id: 'metrics',      label: 'metrics.title',      icon: 'analytics' },
    { id: 'providers',    label: 'providers.list.title',    icon: 'cloud' },
    { id: 'models',       label: 'models.list.title',       icon: 'view_list' },
    { id: 'plugins',      label: 'plugins.title',      icon: 'extension' },
    { id: 'virtual',      label: 'virtual.models_plural', icon: 'robot' },
    { id: 'tokens',       label: 'tokens.list.title',       icon: 'key' },
    { id: 'proxy',        label: 'proxy.title',      icon: 'vpn_lock' },
  ]

  let pluginsTab = $derived<PluginsTab>(routeSegments[0] === 'catalog' ? 'catalog' : 'installed')

  function selectPluginsTab(next: PluginsTab): void {
    window.location.hash = '#/plugins/' + next
  }

  let proxyTab = $derived<ProxyTab>(
    routeSegments[0] === 'sources' ? 'sources' : routeSegments[0] === 'pools' ? 'pools' : 'status'
  )

  let metricsTab = $derived<MetricsTab>(routeSegments[0] === 'logs' ? 'logs' : 'overview')

  function selectMetricsTab(next: MetricsTab): void {
    window.location.hash = next === 'overview' ? '#/metrics' : '#/metrics/' + next
  }

  function selectProxyTab(next: ProxyTab): void {
    window.location.hash = '#/proxy/' + next
  }

  // Manual toggle wins (remembered); otherwise the sidebar folds into a rail on narrow widths.
  // Phone widths are the Scaffold's drawer.
  const SIDEBAR_KEY = 'llmr_sidebar_collapsed'
  let collapsedOverride = $state<boolean | null>(
    localStorage.getItem(SIDEBAR_KEY) === null ? null : localStorage.getItem(SIDEBAR_KEY) === '1'
  )
  let narrow = $state(false)

  onMount(() => {
    const narrowMql = window.matchMedia(`(max-width: ${BP_NARROW}px)`)
    narrow = narrowMql.matches
    const onNarrow = (e: MediaQueryListEvent) => { narrow = e.matches }
    narrowMql.addEventListener('change', onNarrow)
    return () => narrowMql.removeEventListener('change', onNarrow)
  })

  function getCollapsed(): boolean {
    return collapsedOverride ?? narrow
  }
  function setCollapsed(next: boolean): void {
    collapsedOverride = next
    localStorage.setItem(SIDEBAR_KEY, next ? '1' : '0')
  }
</script>

<div class="layout">
<!-- the shell two levels below FUI's default: sidebar -2, content column -1, cards 0 -->
<Scaffold level={-2} bind:collapsed={getCollapsed, setCollapsed}>
  {#snippet sidebar()}
    <Sidebar>
      {#snippet header()}<Text class="brand" weight="bold" size="sm" text="llm-router" />{/snippet}
      {#each nav as item (item.id)}
        <Button icon={{ name: item.icon }} text={t(item.label)} selected={panel === item.id} onclick={() => navigateTo(item.id)} />
      {/each}
      {#snippet footer()}
        <Button icon={{ name: 'settings' }} text={t('settings.title')} selected={panel === 'settings'} onclick={openSettings} />
        <Button icon={{ name: 'logout' }} text={t('auth.sign_out.action')} onclick={logout} />
      {/snippet}
    </Sidebar>
  {/snippet}

  <div class="main-scroll">
    <div class="main-content">
      {#if panel === 'metrics'}
        <Metrics tab={metricsTab} ontabchange={selectMetricsTab} />
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
        <ProxyPage tab={proxyTab} ontabchange={selectProxyTab} />
      {:else if panel === 'settings'}
        <SettingsPage />
      {/if}
    </div>
  </div>
  <AppFooter />
</Scaffold>
</div>

<style>
  .layout {
    height: 100vh;
    height: 100dvh;
    --fui-page-pad: var(--fui-space-5);
  }
  .layout :global(.brand) {
    letter-spacing: var(--fui-tracking-caps);
    text-transform: uppercase;
  }
  .main-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--fui-page-pad);
    padding-left: calc(var(--fui-page-pad) + env(safe-area-inset-left));
    padding-right: calc(var(--fui-page-pad) + env(safe-area-inset-right));
  }
  /* A flex column filling the scroll area, so a page can `grow` a child
     (the logs table) down to the viewport bottom instead of scrolling. */
  .main-content {
    display: flex;
    flex-direction: column;
    min-height: 100%;
    width: 100%;
    max-width: var(--fui-content-max-w);
    min-width: 0;
    margin: 0 auto;
  }
</style>
