<script lang="ts">
  import { Button, Page, Scaffold, Sidebar, Swap, Text, type IconName } from '@fuckyouryan/fui'
  import { surfaceLevel } from '$lib/surface.svelte'
  import { viewport } from '$lib/viewport.svelte'
  import { Persisted } from '$lib/persisted.svelte'
  import { api } from '../lib/api'
  import Metrics from './metrics/Metrics.svelte'
  import Plugins from './plugins/Plugins.svelte'
  import VirtualModelEditorPage from './virtual-models/VirtualModelEditPage.svelte'
  import ProviderDetailPage from './providers/ProviderDetailPage.svelte'
  import ProxyPage from './proxy/ProxyPage.svelte'
  import Models from './models/Models.svelte'
  import Providers from './providers/Providers.svelte'
  import Tokens from './tokens/Tokens.svelte'
  import SettingsPage from './settings/SettingsPage.svelte'
  import type { PluginsTab } from './plugins/Plugins.svelte'
  import type { ProxyTab } from './proxy/ProxyPage.svelte'
  import type { MetricsTab } from './metrics/Metrics.svelte'
  import { t } from '../lib/i18n.svelte'
  import { PLUGINS_ICON, PROVIDERS_ICON, PROXIES_ICON } from '../lib/subsystems'
  import AppFooter from '../components/AppFooter.svelte'

  let { onlogout }: { onlogout: () => void } = $props()

  // One source of truth: the tuple drives both the type and the runtime validation below.
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
    // Virtual models list with the others under Models; #/virtual keeps only the editor.
    if (segments[0] === 'virtual' && segments.length === 1) {
      panel = 'models'
      routeSegments = []
      window.location.hash = '#/models'
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

  applyRoute()

  function openSettings(): void {
    navigateTo('settings')
  }

  async function logout(): Promise<void> {
    await api.logout()
    onlogout?.()
  }

  const nav: NavItem[] = [
    { id: 'metrics',      label: 'metrics.title',      icon: 'analytics' },
    { id: 'providers',    label: 'providers.list.title',    icon: PROVIDERS_ICON },
    { id: 'models',       label: 'models.list.title',       icon: 'view_list' },
    { id: 'plugins',      label: 'plugins.title',      icon: PLUGINS_ICON },
    { id: 'tokens',       label: 'tokens.list.title',       icon: 'key' },
    { id: 'proxy',        label: 'proxy.title',      icon: PROXIES_ICON },
  ]

  // A new page animates in. The detail pages count as one page per provider or virtual model; tabs
  // animate inside their page.
  let pageKey = $derived(panel === 'providers' || panel === 'virtual' ? `${panel}/${routeSegments[0] ?? ''}` : panel)

  let pluginsTab = $derived<PluginsTab>(routeSegments[0] === 'catalog' ? 'catalog' : 'installed')

  function selectPluginsTab(next: PluginsTab): void {
    window.location.hash = '#/plugins/' + next
  }

  // #/proxy leaves the tab to the page. Older links (#/proxy/status, #/proxy/sources) land on the auto pool tab.
  let proxyTab = $derived<ProxyTab | undefined>(routeSegments[0] === 'pools' ? 'pools' : routeSegments[0] ? 'auto' : undefined)

  let metricsTab = $derived<MetricsTab>(routeSegments[0] === 'logs' ? 'logs' : 'overview')

  function selectMetricsTab(next: MetricsTab): void {
    window.location.hash = next === 'overview' ? '#/metrics' : '#/metrics/' + next
  }

  function selectProxyTab(next: ProxyTab): void {
    window.location.hash = '#/proxy/' + next
  }

  // Manual toggle wins (remembered); otherwise the sidebar folds into a rail on narrow widths.
  // Phone widths are the Scaffold's drawer.
  // null: never toggled, the width decides
  const collapsedOverride = new Persisted<boolean | null>('llmr_sidebar_collapsed', null, {
    parse: (stored) => (stored === '1' ? true : stored === '0' ? false : undefined),
    serialize: (collapsed) => (collapsed ? '1' : '0'),
  })
  function getCollapsed(): boolean {
    return collapsedOverride.value ?? viewport.narrow
  }
  function setCollapsed(next: boolean): void {
    collapsedOverride.value = next
  }
</script>

<svelte:window onhashchange={applyRoute} />

<div class="layout">
<!-- the shell two levels below FUI's default: sidebar -2, content column -1, cards 0 -->
<Scaffold level={surfaceLevel.value} bind:collapsed={getCollapsed, setCollapsed}>
  {#snippet sidebar()}
    <Sidebar>
      {#snippet header()}<Text class="brand" weight="bold" size="sm" text="llm-router" />{/snippet}
      {#each nav as item (item.id)}
        <Button icon={{ name: item.icon }} text={t(item.label)} selected={panel === item.id || (panel === 'virtual' && item.id === 'models')} onclick={() => navigateTo(item.id)} />
      {/each}
      {#snippet footer()}
        <Button icon={{ name: 'settings' }} text={t('settings.title')} selected={panel === 'settings'} onclick={openSettings} />
        <Button icon={{ name: 'logout' }} text={t('auth.sign_out.action')} onclick={logout} />
      {/snippet}
    </Sidebar>
  {/snippet}

  <Page>
      <Swap key={pageKey}>
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
        <VirtualModelEditorPage vmId={routeSegments[0] === 'new' ? null : routeSegments[0]} />
      {:else if panel === 'tokens'}
        <Tokens />
      {:else if panel === 'plugins'}
        <Plugins tab={pluginsTab} ontabchange={selectPluginsTab} />
      {:else if panel === 'proxy'}
        <ProxyPage tab={proxyTab} ontabchange={selectProxyTab} />
      {:else if panel === 'settings'}
        <SettingsPage />
      {/if}
      </Swap>
  </Page>
  <AppFooter />
</Scaffold>
</div>

<style>
  .layout {
    height: 100vh;
    height: 100dvh;
  }
  .layout :global(.brand) {
    letter-spacing: var(--fui-tracking-caps);
    text-transform: uppercase;
  }
</style>
