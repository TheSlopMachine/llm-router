<script lang="ts">
  import { onMount, onDestroy, createEventDispatcher } from 'svelte'
  import { api } from '../lib/api'
  import type { Stats, MetricsOverview, Credential, Provider, ProviderStats, Agent } from '../lib/types'

  type PanelId = 'overview' | 'metrics' | 'providers' | 'tokens' | 'credentials' | 'agents'

  const dispatch = createEventDispatcher<{ navigate: PanelId }>()

  interface Notification {
    type: 'setup' | 'warning'
    message: string
    action: string
    navigate: PanelId
  }

  // Data state
  let stats: Stats = { providers: 0, tokens: 0, credentials: 0 }
  let metrics: MetricsOverview | null = null
  let credentials: Credential[] = []
  let agents: Agent[] = []
  let providers: Provider[] = []
  let providerStats: Record<string, ProviderStats> = {}
  let notifications: Notification[] = []
  let isReady = true
  let errorRate = 0
  let topProvider: { name: string; requests: number } | null = null
  let refreshInterval: number

  onMount(async () => {
    await loadData()
    // Auto-refresh every 30 seconds
    refreshInterval = window.setInterval(loadData, 30000)
  })

  onDestroy(() => {
    if (refreshInterval) {
      clearInterval(refreshInterval)
    }
  })

  async function loadData() {
    try {
      // Fetch all data in parallel
      const [statusData, metricsData, credsData, agentsData, providersData, statsData] = await Promise.all([
        api.status(),
        api.metrics.overview({ time_range: 'hour' }),
        api.credentials.list(),
        api.agents.list(),
        api.providers.list(),
        api.providers.stats(),
      ])

      stats = statusData.stats || stats
      metrics = metricsData
      credentials = credsData || []
      agents = (agentsData as Agent[]) || []
      providers = providersData || []
      providerStats = statsData || {}

      // Calculate derived data
      calculateErrorRate()
      findTopProvider()
      generateNotifications()
      determineSystemStatus()
    } catch (e) {
      console.error('Failed to load overview data:', e)
    }
  }

  function calculateErrorRate() {
    if (metrics && metrics.total_requests > 0) {
      errorRate = (metrics.total_errors / metrics.total_requests) * 100
    } else {
      errorRate = 0
    }
  }

  function findTopProvider() {
    let maxRequests = 0
    let topProviderId = ''

    for (const [providerId, stats] of Object.entries(providerStats)) {
      if (stats.requests_today > maxRequests) {
        maxRequests = stats.requests_today
        topProviderId = providerId
      }
    }

    if (topProviderId && maxRequests > 0) {
      const provider = providers.find(p => p.id === topProviderId)
      topProvider = {
        name: provider?.name || topProviderId,
        requests: maxRequests
      }
    } else {
      topProvider = null
    }
  }

  function generateNotifications() {
    const newNotifications: Notification[] = []

    // Setup notifications (friendly, first-run)
    if (stats.providers === 0) {
      newNotifications.push({
        type: 'setup',
        message: 'Get started by configuring your first provider',
        action: 'Configure Providers',
        navigate: 'providers'
      })
    }

    if (stats.credentials === 0) {
      newNotifications.push({
        type: 'setup',
        message: 'Add credentials to authenticate with providers',
        action: 'Add Credentials',
        navigate: 'providers'
      })
    }

    if (stats.tokens === 0) {
      newNotifications.push({
        type: 'setup',
        message: 'Issue your first API token to start making requests',
        action: 'Issue Token',
        navigate: 'tokens'
      })
    }

    // Warning notifications (actual issues)
    const expiredCount = credentials.filter(c => c.is_expired).length
    if (expiredCount > 0) {
      newNotifications.push({
        type: 'warning',
        message: `${expiredCount} credential${expiredCount > 1 ? 's have' : ' has'} expired`,
        action: 'Renew Credentials',
        navigate: 'credentials'
      })
    }

    if (errorRate >= 15) {
      newNotifications.push({
        type: 'warning',
        message: `High error rate detected: ${errorRate.toFixed(1)}% (${metrics?.total_errors || 0}/${metrics?.total_requests || 0} requests failed in last hour)`,
        action: 'View Metrics',
        navigate: 'metrics'
      })
    }

    notifications = newNotifications
  }

  function determineSystemStatus() {
    isReady = notifications.length === 0
  }

  function formatNumber(num: number): string {
    return num.toLocaleString()
  }
</script>

<div class="page-header">
  <h1>Overview</h1>
  <p>System health and activity monitoring</p>
</div>

<!-- System Status Bar -->
<div class="status-bar" class:ready={isReady} class:setup-needed={!isReady}>
  <span class="status-text">
    {isReady ? 'All systems ready' : 'Setup required'}
  </span>
  <span class="status-divider">|</span>
  <span class="status-item">{stats.providers} provider{stats.providers !== 1 ? 's' : ''}</span>
  <span class="status-divider">|</span>
  <span class="status-item">{stats.tokens} token{stats.tokens !== 1 ? 's' : ''}</span>
  <span class="status-divider">|</span>
  <span class="status-item">{stats.credentials} credential{stats.credentials !== 1 ? 's' : ''}</span>
</div>

<!-- Status Section (only show if there are notifications) -->
{#if notifications.length > 0}
  <div class="section">
    <h2 class="section-title">
      <span class="icon">notifications</span>
      Status
    </h2>
    
    <div class="notifications-list">
      {#each notifications as notification}
        <div class="notification-item {notification.type}">
          <div class="notification-content">
            <span class="notification-icon icon">
              {notification.type === 'setup' ? 'info' : 'warning'}
            </span>
            <span class="notification-text">{notification.message}</span>
          </div>
          <button class="btn btn-sm btn-notification" on:click={() => dispatch('navigate', notification.navigate)}>
            {notification.action}
          </button>
        </div>
      {/each}
    </div>
  </div>
{/if}

<!-- Activity Section -->
<div class="section">
  <h2 class="section-title">Activity (Last Hour)</h2>
  
  <div class="activity-stats">
    <span class="activity-stat">
      {formatNumber(metrics?.total_requests || 0)} request{(metrics?.total_requests || 0) !== 1 ? 's' : ''}
    </span>
    <span class="activity-divider">|</span>
    <span class="activity-stat">
      {formatNumber(metrics?.total_errors || 0)} error{(metrics?.total_errors || 0) !== 1 ? 's' : ''} ({errorRate.toFixed(2)}%)
    </span>
    <span class="activity-divider">|</span>
    <span class="activity-stat">
      Peak: {formatNumber(metrics?.peak_rpm || 0)} RPM
    </span>
    <span class="activity-divider">|</span>
    <span class="activity-stat">
      Top: {topProvider ? `${topProvider.name} (${formatNumber(topProvider.requests)} req)` : '—'}
    </span>
  </div>
</div>

<!-- Resources Section -->
<div class="section">
  <h2 class="section-title">Resources</h2>
  
  <div class="resources-grid">
    <button 
      class="resource-card" 
      on:click={() => dispatch('navigate', 'providers')}
      title="Click to manage providers and credentials"
    >
      <div class="resource-label">Providers</div>
      <div class="resource-value">{stats.providers}</div>
    </button>
    
    <button 
      class="resource-card" 
      on:click={() => dispatch('navigate', 'agents')}
      title="Click to manage virtual agents"
    >
      <div class="resource-label">Agents</div>
      <div class="resource-value">{agents.length}</div>
    </button>
    
    <button 
      class="resource-card" 
      on:click={() => dispatch('navigate', 'tokens')}
      title="Click to manage API tokens"
    >
      <div class="resource-label">Tokens</div>
      <div class="resource-value">{stats.tokens}</div>
    </button>
    
    <button 
      class="resource-card" 
      on:click={() => dispatch('navigate', 'credentials')}
      title="Click to manage provider credentials"
    >
      <div class="resource-label">Credentials</div>
      <div class="resource-value">{stats.credentials}</div>
    </button>
  </div>
</div>

<style>
  .page-header {
    margin-bottom: 24px;
  }

  /* System Status Bar */
  .status-bar {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 16px 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 12px;
    font-size: 14px;
    margin-bottom: 24px;
  }

  .status-bar.ready {
    border-left: 3px solid #10b981;
  }

  .status-bar.setup-needed {
    border-left: 3px solid #3b82f6;
  }

  .status-text {
    font-weight: 600;
    color: var(--color-text);
  }

  .status-divider {
    color: var(--color-text-soft);
  }

  .status-item {
    color: var(--color-text);
  }

  /* Section */
  .section {
    margin-bottom: 24px;
  }

  .section-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 16px;
    font-weight: 600;
    color: var(--color-text);
    margin-bottom: 12px;
  }

  .section-title .icon {
    font-size: 20px;
    color: var(--color-text-soft);
  }

  /* Notifications */
  .notifications-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .notification-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 12px 16px;
    border-radius: 8px;
  }

  /* Setup notifications (blue) */
  .notification-item.setup {
    background: var(--color-notification-info-bg);
    border: 1px solid var(--color-notification-info-border);
  }

  .notification-item.setup .notification-icon {
    color: var(--color-notification-info-icon);
  }

  .notification-item.setup .notification-text {
    color: var(--color-notification-info-text);
  }

  .notification-item.setup .btn-notification {
    background: #ffffff;
    border: 1px solid var(--color-notification-info-border);
    color: var(--color-notification-info-text);
  }

  .notification-item.setup .btn-notification:hover {
    background: var(--color-notification-info-bg);
    border-color: var(--color-notification-info-icon);
  }

  .notification-item.setup .notification-icon {
    color: var(--color-notification-info-icon);
  }

  .notification-item.setup .notification-text {
    color: var(--color-notification-info-text);
  }

  .notification-item.setup .btn-notification {
    background: #ffffff;
    border: 1px solid var(--color-notification-info-border);
    color: var(--color-notification-info-text);
  }

  .notification-item.setup .btn-notification:hover {
    background: var(--color-notification-info-bg);
    border-color: var(--color-notification-info-icon);
  }

  .notification-item.warning {
    background: var(--color-notification-warning-bg);
    border: 1px solid var(--color-notification-warning-border);
  }

  .notification-item.warning .notification-icon {
    color: var(--color-notification-warning-icon);
  }

  .notification-item.warning .notification-text {
    color: var(--color-notification-warning-text);
  }

  .notification-item.warning .btn-notification {
    background: #ffffff;
    border: 1px solid var(--color-notification-warning-border);
    color: var(--color-notification-warning-text);
  }

  .notification-item.warning .btn-notification:hover {
    background: var(--color-notification-warning-bg);
    border-color: var(--color-notification-warning-icon);
  }

  .notification-item.warning .notification-icon {
    color: var(--color-notification-warning-icon);
  }

  .notification-item.warning .notification-text {
    color: var(--color-notification-warning-text);
  }

  .notification-item.warning .btn-notification {
    background: #ffffff;
    border: 1px solid var(--color-notification-warning-border);
    color: var(--color-notification-warning-text);
  }

  .notification-item.warning .btn-notification:hover {
    background: var(--color-notification-warning-bg);
    border-color: var(--color-notification-warning-icon);
  }

  .notification-item.setup .notification-icon {
    color: #3b82f6;
  }

  .notification-item.setup .notification-text {
    color: #1e40af;
  }

  .notification-item.setup .btn-notification {
    background: #ffffff;
    border: 1px solid #bfdbfe;
    color: #1e40af;
  }

  .notification-item.setup .btn-notification:hover {
    background: #dbeafe;
    border-color: #93c5fd;
  }

  /* Warning notifications (yellow) */
  .notification-item.warning {
    background: #fef3c7;
    border: 1px solid #fde68a;
  }

  .notification-item.warning .notification-icon {
    color: #f59e0b;
  }

  .notification-item.warning .notification-text {
    color: #92400e;
  }

  .notification-item.warning .btn-notification {
    background: #ffffff;
    border: 1px solid #fde68a;
    color: #92400e;
  }

  .notification-item.warning .btn-notification:hover {
    background: #fef3c7;
    border-color: #fcd34d;
  }

  .notification-content {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
  }

  .notification-icon {
    font-size: 18px;
    flex-shrink: 0;
  }

  .notification-text {
    font-size: 14px;
  }

  .btn-notification {
    padding: 6px 16px;
    border-radius: 6px;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
    white-space: nowrap;
  }

  /* Activity */
  .activity-stats {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 16px 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 12px;
    font-size: 14px;
  }

  .activity-stat {
    color: var(--color-text);
  }

  .activity-divider {
    color: var(--color-text-soft);
  }

  /* Resources */
  .resources-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
  }

  .resource-card {
    padding: 24px;
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 12px;
    text-align: center;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s;
  }

  .resource-card:hover {
    background: var(--color-nav-hover);
    border-color: var(--color-outline);
  }

  .resource-label {
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-soft);
    margin-bottom: 12px;
    font-weight: 500;
  }

  .resource-value {
    font-size: 36px;
    font-weight: 700;
    color: var(--color-text);
  }

  @media (max-width: 1024px) {
    .resources-grid {
      grid-template-columns: repeat(2, 1fr);
    }
  }

  @media (max-width: 640px) {
    .status-bar {
      flex-wrap: wrap;
    }

    .activity-stats {
      flex-direction: column;
      align-items: flex-start;
      gap: 8px;
    }

    .activity-divider {
      display: none;
    }

    .resources-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
