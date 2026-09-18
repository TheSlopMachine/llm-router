<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import type { Proxy, ProxyStatus, ProxySourceInfo } from '../lib/types'
  import { squircle } from '../lib/squircle'
  import { t, n } from '../lib/i18n.svelte'

  let proxies = $state<Proxy[]>([])
  let sources = $state<ProxySourceInfo[]>([])
  let status = $state<ProxyStatus | null>(null)
  let loading = $state(true)
  let error = $state('')

  let newUrl = $state('')
  let newCountry = $state('')
  let adding = $state(false)
  let checkingId = $state('')
  let checkingAll = $state(false)

  let manualProxies = $derived(proxies.filter((p) => p.source === 'manual'))
  let sourcesActive = $derived(sources.some((s) => s.status !== 'idle'))

  onMount(() => {
    loadAll()
    // While a source worker is busy, poll so the table shows live progress.
    const poll = setInterval(() => {
      if (sourcesActive) {
        reloadSources()
      }
    }, 2000)
    return () => clearInterval(poll)
  })

  async function loadAll(): Promise<void> {
    loading = true
    error = ''
    try {
      const [p, s, st] = await Promise.all([api.proxies.list(), api.proxies.sources(), api.proxies.status()])
      proxies = p
      sources = s
      status = st
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  async function reloadPool(): Promise<void> {
    proxies = await api.proxies.list()
    status = await api.proxies.status()
  }

  async function reloadSources(): Promise<void> {
    try {
      sources = await api.proxies.sources()
      status = await api.proxies.status()
    } catch {
      // Polling is best-effort; the next tick retries.
    }
  }

  async function addProxy(): Promise<void> {
    const url = newUrl.trim()
    if (!url) return
    adding = true
    error = ''
    try {
      await api.proxies.add(url, newCountry.trim().toUpperCase())
      newUrl = ''
      newCountry = ''
      await reloadPool()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      adding = false
    }
  }

  async function deleteProxy(p: Proxy): Promise<void> {
    const confirmed = await modal.confirm({
      title: t('Delete proxy'),
      message: `${t('Remove')} ${p.url} ${t('from the pool?')}`,
      severity: 'medium',
      confirmText: t('Delete'),
      cancelText: t('Cancel'),
      danger: true,
    })
    if (!confirmed) return
    try {
      await api.proxies.delete(p.id)
      proxies = proxies.filter((x) => x.id !== p.id)
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  async function checkProxy(p: Proxy): Promise<void> {
    checkingId = p.id
    try {
      await api.proxies.check(p.id)
    } catch {
      // dead proxies are culled by the backend; the pool reload reflects it
    } finally {
      checkingId = ''
      await reloadPool()
    }
  }

  let checkAllAbort: AbortController | null = null

  // Second click cancels: the endpoint is synchronous and dies with the request.
  async function checkAll(): Promise<void> {
    if (checkingAll) {
      checkAllAbort?.abort()
      return
    }
    checkingAll = true
    checkAllAbort = new AbortController()
    try {
      await api.proxies.checkAll(checkAllAbort.signal)
      await reloadPool()
    } catch (e) {
      if (!checkAllAbort.signal.aborted) {
        error = getErrorMessage(e)
      }
    } finally {
      checkingAll = false
      checkAllAbort = null
    }
  }

  async function refreshSource(s: ProxySourceInfo): Promise<void> {
    error = ''
    try {
      await api.proxies.refreshSource(s.key)
      // Fetch starts in the background; mark the row busy at once.
      sources = sources.map((x) => (x.key === s.key ? { ...x, status: 'fetching' as const } : x))
      await reloadSources()
    } catch (e) {
      error = getErrorMessage(e)
    }
  }

  function openSource(s: ProxySourceInfo): void {
    window.location.hash = '#/proxy/source/' + encodeURIComponent(s.key)
  }

  function onSourceRowKeydown(e: KeyboardEvent, s: ProxySourceInfo): void {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      openSource(s)
    }
  }

  function protocolChip(protocol: string): string {
    switch (protocol) {
      case 'http': return 'chip-blue'
      case 'https': return 'chip-green'
      case 'socks5': return 'chip-purple'
      case 'socks4': return 'chip-teal'
      default: return 'chip-neutral'
    }
  }
</script>

<div class="page-header">
  <div>
    <h1>{t('Proxies')}</h1>
    <p>
      {#if status}
        {n(status.total, 'total', 'total', 'всего', 'всего', 'всего')} · {n(status.alive, 'working', 'working', 'рабочий', 'рабочих', 'рабочих')}
      {:else}
        {t('Outbound proxy pool.')}
      {/if}
    </p>
  </div>
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if loading}
  <div class="empty">{t('Loading…')}</div>
{:else}
  <section class="section">
    <div class="section-header">
      <h2>{t('My Proxies')}</h2>
    </div>
    <div class="add-form">
      <input
        class="url-input"
        type="text"
        placeholder={t('protocol://[user:pass@]host:port')}
        bind:value={newUrl}
        use:squircle={12}
      />
      <input
        class="country-input"
        type="text"
        placeholder={t('CC')}
        maxlength="2"
        bind:value={newCountry}
        use:squircle={12}
      />
      <button class="btn btn-primary" onclick={addProxy} disabled={!newUrl.trim() || adding} use:squircle={12}>
        <span class="icon">add</span>
        {t('Add proxy')}
      </button>
      <button class="btn btn-secondary" onclick={checkAll} disabled={!checkingAll && manualProxies.length === 0} use:squircle={12}>
        <span class="icon">{checkingAll ? 'stop' : 'network_check'}</span>
        {checkingAll ? t('Checking… click to cancel') : t('Check all')}
      </button>
    </div>
    {#if manualProxies.length === 0}
      <div class="empty-state" use:squircle={18}>{t('No manual proxies yet. Add one above, or pull free lists from the Proxy Lists tab.')}</div>
    {:else}
      <div class="table" use:squircle={18}>
        <div class="table-row table-head">
          <span class="pcol-url">{t('Proxy')}</span>
          <span class="pcol-proto">{t('Protocol')}</span>
          <span class="pcol-country">{t('Country')}</span>
          <span class="pcol-status">{t('Status')}</span>
          <span class="pcol-actions">{t('Actions')}</span>
        </div>
        {#each manualProxies as p (p.id)}
          <div class="table-row" class:row-dead={!p.alive}>
            <span class="pcol-url mono">{p.url}</span>
            <span class="pcol-proto"><span class="chip {protocolChip(p.protocol)}">{p.protocol}</span></span>
            <span class="pcol-country">{p.country || '—'}</span>
            <span class="pcol-status">
              {#if p.alive}
                <span class="chip chip-green" title={t('Last probe latency')}>{t('alive')}{p.latency_ms ? ` · ${p.latency_ms}ms` : ''}</span>
              {:else}
                <span class="chip chip-red" title={t('Failed the last probe; recheck to revive')}>{t('dead')}</span>
              {/if}
            </span>
            <span class="pcol-actions">
              <button class="btn-icon" onclick={() => checkProxy(p)} disabled={checkingId === p.id} aria-label={t('Check proxy')} title={t('Check proxy')} use:squircle={10}>
                <span class="icon" class:spin={checkingId === p.id}>{checkingId === p.id ? 'progress_activity' : 'network_check'}</span>
              </button>
              <button class="btn-icon icon-danger" onclick={() => deleteProxy(p)} aria-label={t('Delete proxy')} title={t('Delete proxy')} use:squircle={10}>
                <span class="icon">delete</span>
              </button>
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <section class="section">
    <div class="section-header">
      <h2>{t('Proxy Lists')}</h2>
    </div>
    {#if sources.length === 0}
      <div class="empty-state" use:squircle={18}>{t('No proxy list sources installed. Install a proxy-source plugin (e.g. proxifly).')}</div>
    {:else}
      <div class="table sources-table" use:squircle={18}>
        <div class="table-row table-head">
          <span class="scol-name">{t('Source')}</span>
          <span class="scol-status">{t('Status')}</span>
          <span class="scol-num">{t('Total')}</span>
          <span class="scol-num">{t('Checked')}</span>
          <span class="scol-num">{t('Alive')}</span>
          <span class="scol-actions"></span>
        </div>
        {#each sources as s (s.key)}
          <div
            class="table-row row-clickable"
            role="button"
            tabindex="0"
            onclick={() => openSource(s)}
            onkeydown={(e) => onSourceRowKeydown(e, s)}
          >
            <span class="scol-name source-name">{s.key}</span>
            <span class="scol-status">
              {#if s.status === 'fetching'}
                {t('Fetching list…')}
              {:else if s.status === 'checking'}
                {t('Checking proxies…')}
              {:else if s.last_error}
                <span class="status-error" title={s.last_error}>{t('Failed')}</span>
              {:else}
                {t('Idle')}
              {/if}
            </span>
            <span class="scol-num">{s.total > 0 ? s.total : '—'}</span>
            <span class="scol-num">{s.checked > 0 ? s.checked : '—'}</span>
            <span class="scol-num">{s.alive > 0 ? s.alive : '—'}</span>
            <!-- The button eats its own clicks so row navigation never fires
                 from the action cell. -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <span class="scol-actions" onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
              <button class="btn-text" onclick={() => refreshSource(s)} disabled={s.status !== 'idle'}>
                {t('Refresh')}
              </button>
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </section>
{/if}

<style>
  .section {
    margin-bottom: 32px;
  }
  .section-header {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }
  .section-header h2 {
    font-size: 16px;
    font-weight: 600;
    margin: 0;
  }
  .add-form {
    display: flex;
    gap: 12px;
    margin-bottom: 16px;
  }
  .url-input {
    flex: 1 1 auto;
  }
  .country-input {
    flex: 0 0 72px;
    text-transform: uppercase;
  }
  /* Column layout only — table widget chrome comes from the global rules. */
  .table-row {
    grid-template-columns: minmax(0, 1.6fr) 110px 90px minmax(0, 1fr) auto;
  }
  .sources-table .table-row {
    grid-template-columns: minmax(0, 1.4fr) minmax(0, 1.2fr) 90px 110px 90px 110px;
  }
  .row-clickable {
    cursor: pointer;
    transition: background-color 0.15s ease;
  }
  .row-clickable:hover {
    background: var(--color-hover-bg);
  }
  .row-clickable:focus-visible {
    box-shadow: inset 0 0 0 2px var(--color-accent);
    outline: none;
  }
  .row-dead {
    opacity: 0.5;
  }
  .mono {
    font-family: 'DM Mono', monospace;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pcol-actions {
    display: flex;
    gap: 6px;
    justify-content: flex-end;
  }
  .source-name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .scol-status {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--color-text-soft);
    font-size: 13px;
    overflow: hidden;
    white-space: nowrap;
  }
  .status-error {
    color: var(--color-danger);
  }
  .scol-num {
    font-variant-numeric: tabular-nums;
  }
  .scol-actions {
    display: flex;
    justify-content: flex-end;
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
  @media (max-width: 900px) {
    .sources-table .table-row {
      grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr) 70px 90px;
    }
    .scol-num:nth-of-type(4),
    .scol-num:nth-of-type(5) {
      display: none;
    }
  }
  @media (max-width: 768px) {
    .add-form {
      flex-wrap: wrap;
    }
    .table-row {
      grid-template-columns: minmax(0, 1fr) auto auto;
    }
    .pcol-country {
      display: none;
    }
  }
</style>
