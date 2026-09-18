<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import type { Proxy, ProxySourceInfo } from '../lib/types'
  import { squircle } from '../lib/squircle'
  import { t, n } from '../lib/i18n.svelte'

  let { sourceKey }: { sourceKey: string } = $props()

  const PAGE_SIZE = 100

  let info = $state<ProxySourceInfo | null>(null)
  let items = $state<Proxy[]>([])
  let total = $state(0)
  let loading = $state(true)
  let loadingMore = $state(false)
  let error = $state('')

  let hasMore = $derived(items.length < total)
  let active = $derived(info !== null && info.status !== 'idle')

  onMount(() => {
    load()
    // While the source worker is busy, poll so counts and new arrivals show up.
    const poll = setInterval(() => {
      if (active) {
        reloadInfo()
        reloadItems(false)
      }
    }, 2000)
    return () => clearInterval(poll)
  })

  async function load(): Promise<void> {
    loading = true
    error = ''
    try {
      await Promise.all([reloadInfo(), reloadItems(false)])
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function reloadInfo(): Promise<void> {
    const sources = await api.proxies.sources()
    info = sources.find((s) => s.key === sourceKey) ?? null
  }

  async function reloadItems(append: boolean): Promise<void> {
    const offset = append ? items.length : 0
    const page = await api.proxies.sourceProxies(sourceKey, offset, PAGE_SIZE)
    total = page.total
    items = append ? [...items, ...page.items] : page.items
  }

  async function showMore(): Promise<void> {
    loadingMore = true
    try {
      await reloadItems(true)
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loadingMore = false
    }
  }

  function back(): void {
    window.location.hash = '#/proxy'
  }
</script>

{#if loading}
  <div class="empty">{t('Loading…')}</div>
{:else if !info}
  <div class="empty">
    <p>{t('Proxy source not found.')}</p>
    <button class="btn btn-secondary" onclick={back} use:squircle={12}>{t('Back')}</button>
  </div>
{:else}
  <div class="detail-header">
    <button class="btn-icon" onclick={back} aria-label={t('Back')} title={t('Back')} use:squircle={10}>
      <span class="icon">arrow_back</span>
    </button>
    <div class="source-title">
      <h1>{info.key}</h1>
      <p>
        {#if info.status === 'fetching'}
          <span class="icon spin status-icon">progress_activity</span>{t('Fetching list…')}
        {:else if info.status === 'checking'}
          <span class="icon spin status-icon">progress_activity</span>{t('Checking proxies…')}
        {:else}
          {t('Idle')}
        {/if}
        · {n(info.total, 'fetched', 'fetched', 'загружено', 'загружено', 'загружено')}
        · {n(info.checked, 'checked', 'checked', 'проверен', 'проверено', 'проверено')}
        · {n(info.alive, 'alive', 'alive', 'живой', 'живых', 'живых')}
      </p>
    </div>
  </div>

  {#if info.last_error}
    <div class="error-msg">{info.last_error}</div>
  {/if}
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}

  {#if items.length === 0}
    <div class="empty-state" use:squircle={18}>
      {active ? t('Nothing verified yet. The first working proxies will appear here.') : t('No verified proxies from this source. Refresh it from the Proxy Lists tab.')}
    </div>
  {:else}
    <div class="table" use:squircle={18}>
      <div class="table-row table-head">
        <span class="pcol-url">{t('Proxy')}</span>
        <span class="pcol-proto">{t('Protocol')}</span>
        <span class="pcol-country">{t('Country')}</span>
        <span class="pcol-latency">{t('Latency')}</span>
      </div>
      {#each items as p (p.id)}
        <div class="table-row">
          <span class="pcol-url mono">{p.url}</span>
          <span class="pcol-proto">{p.protocol}</span>
          <span class="pcol-country">{p.country || '—'}</span>
          <span class="pcol-latency">{p.latency_ms ? `${p.latency_ms}ms` : '—'}</span>
        </div>
      {/each}
    </div>
    {#if hasMore}
      <div class="more-row">
        <button class="btn btn-secondary" onclick={showMore} disabled={loadingMore} use:squircle={12}>
          <span class="icon" class:spin={loadingMore}>{loadingMore ? 'progress_activity' : 'expand_more'}</span>
          {t('Show more')} ({items.length}/{total})
        </button>
      </div>
    {/if}
  {/if}
{/if}

<style>
  .detail-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 20px;
  }
  .source-title h1 {
    font-size: 20px;
    font-weight: 600;
    margin: 0;
  }
  .source-title p {
    margin: 4px 0 0;
    font-size: 13px;
    color: var(--color-text-soft);
    display: flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
  }
  .status-icon {
    font-size: 16px;
  }
  /* Column layout only — table widget chrome comes from the global rules. */
  .table-row {
    grid-template-columns: minmax(0, 1.8fr) 110px 90px 110px;
  }
  .mono {
    font-family: 'DM Mono', monospace;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pcol-latency {
    font-variant-numeric: tabular-nums;
  }
  .more-row {
    display: flex;
    justify-content: center;
    margin-top: 16px;
  }
  .empty-state {
    padding: 32px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
    background: var(--color-surface-container-high);
    border-radius: var(--radius-lg);
  }
  .empty {
    padding: 48px;
    text-align: center;
    color: var(--color-text-soft);
  }
  @media (max-width: 768px) {
    .table-row {
      grid-template-columns: minmax(0, 1fr) 90px;
    }
    .pcol-country,
    .pcol-latency {
      display: none;
    }
  }
</style>
