<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../lib/api'
  import { modal } from '../lib/modal.svelte'
  import { getErrorMessage } from '../lib/errors'
  import type { Proxy, ProxyStatus } from '../lib/types'
  import SegmentedControl from '../components/ui/SegmentedControl.svelte'
  import { squircle } from '../lib/squircle'
  import { t, n } from '../lib/i18n.svelte'

  type Tab = 'mine' | 'lists'

  let tab = $state<Tab>('mine')
  let proxies = $state<Proxy[]>([])
  let sources = $state<string[]>([])
  let status = $state<ProxyStatus | null>(null)
  let loading = $state(true)
  let error = $state('')

  let newUrl = $state('')
  let newCountry = $state('')
  let adding = $state(false)
  let checkingId = $state('')
  let checkingAll = $state(false)
  let refreshingSource = $state('')
  let refreshNote = $state('')

  let manualProxies = $derived(proxies.filter((p) => p.source === 'manual'))
  let listProxies = $derived(proxies.filter((p) => p.source !== 'manual'))

  onMount(loadAll)

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

  async function checkAll(): Promise<void> {
    checkingAll = true
    try {
      await api.proxies.checkAll()
      // The check runs in the background; give it a moment before reloading.
      setTimeout(reloadPool, 3000)
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      checkingAll = false
    }
  }

  async function refreshSource(key: string): Promise<void> {
    refreshingSource = key
    refreshNote = ''
    try {
      const res = await api.proxies.refreshSource(key)
      refreshNote = `${key}: ${res.total} fetched, ${res.added} new`
      await reloadPool()
    } catch (e) {
      refreshNote = `${key}: ${getErrorMessage(e)}`
    } finally {
      refreshingSource = ''
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
        {t('Server location')}: <b>{status.server_country || t('unknown')}</b> · {n(status.alive, 'alive', 'alive', 'жив', 'живо', 'живо')}/{n(status.total, 'total', 'total', 'всего', 'всего', 'всего')}
      {:else}
        {t('Outbound proxy pool.')}
      {/if}
    </p>
  </div>
  <SegmentedControl
    bind:value={tab}
    options={[
      { value: 'mine', label: t('My Proxies') },
      { value: 'lists', label: t('Proxy Lists') },
    ]}
    ariaLabel={t('Proxy tabs')}
  />
</div>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

{#if loading}
  <div class="empty">{t('Loading…')}</div>
{:else if tab === 'mine'}
  <section class="section">
    <div class="add-form">
      <input
        class="search-input url-input"
        type="text"
        placeholder={t('protocol://[user:pass@]host:port')}
        bind:value={newUrl}
        use:squircle={12}
      />
      <input
        class="search-input country-input"
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
      <button class="btn btn-secondary" onclick={checkAll} disabled={checkingAll || manualProxies.length === 0} use:squircle={12}>
        <span class="icon" class:spin={checkingAll}>{checkingAll ? 'progress_activity' : 'network_check'}</span>
        {t('Check all')}
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
                <span class="badge badge-green" title={t('Last probe latency')}>{t('alive')}{p.latency_ms ? ` · ${p.latency_ms}ms` : ''}</span>
              {:else}
                <span class="badge badge-red" title={t('Failed the last probe; recheck to revive')}>{t('dead')}</span>
              {/if}
            </span>
            <span class="pcol-actions">
              <button class="btn-icon" onclick={() => checkProxy(p)} disabled={checkingId === p.id} aria-label={t('Check proxy')} title={t('Check proxy')}>
                <span class="icon" class:spin={checkingId === p.id}>{checkingId === p.id ? 'progress_activity' : 'network_check'}</span>
              </button>
              <button class="btn-icon" onclick={() => deleteProxy(p)} aria-label={t('Delete proxy')} title={t('Delete proxy')}>
                <span class="icon">delete</span>
              </button>
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </section>
{:else}
  <section class="section">
    <div class="section-header">
      <h2>{t('Sources')}</h2>
      {#if refreshNote}
        <span class="refresh-note">{refreshNote}</span>
      {/if}
    </div>
    {#if sources.length === 0}
      <div class="empty-state" use:squircle={18}>{t('No proxy list sources installed. Install a proxy-source plugin (e.g. proxifly).')}</div>
    {:else}
      <div class="sources-row">
        {#each sources as key}
          <div class="source-card" use:squircle={18}>
            <span class="source-name">{key}</span>
            <button class="btn btn-secondary" onclick={() => refreshSource(key)} disabled={refreshingSource === key} use:squircle={12}>
              <span class="icon" class:spin={refreshingSource === key}>{refreshingSource === key ? 'progress_activity' : 'refresh'}</span>
              {t('Refresh')}
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </section>

  <section class="section">
    <div class="section-header">
      <h2>{t('Pulled from lists')} ({listProxies.length})</h2>
    </div>
    {#if listProxies.length === 0}
      <div class="empty-state" use:squircle={18}>{t('Nothing pooled yet. Refresh a source above.')}</div>
    {:else}
      <div class="table list-table" use:squircle={18}>
        <div class="table-row table-head">
          <span class="pcol-url">{t('Proxy')}</span>
          <span class="pcol-proto">{t('Protocol')}</span>
          <span class="pcol-country">{t('Country')}</span>
          <span class="pcol-source">{t('Source')}</span>
        </div>
        {#each listProxies.slice(0, 200) as p (p.id)}
          <div class="table-row" class:row-dead={!p.alive}>
            <span class="pcol-url mono">{p.url}</span>
            <span class="pcol-proto"><span class="chip {protocolChip(p.protocol)}">{p.protocol}</span></span>
            <span class="pcol-country">{p.country || '—'}</span>
            <span class="pcol-source">{p.source.replace('list:', '')}</span>
          </div>
        {/each}
      </div>
      {#if listProxies.length > 200}
        <p class="form-hint">Showing 200 of {listProxies.length}.</p>
      {/if}
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
  .refresh-note {
    font-size: 13px;
    color: var(--color-text-soft);
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
  .search-input {
    padding: 8px 12px;
    border: none;
    border-radius: var(--radius-md);
    background: var(--color-surface-container-highest);
    color: var(--color-text);
    font-family: inherit;
    font-size: 14px;
  }
  .search-input:focus {
    outline: none;
    box-shadow: inset 0 0 0 2px var(--color-accent);
  }
  .table {
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .table-row {
    display: grid;
    grid-template-columns: minmax(0, 1.6fr) 110px 90px minmax(0, 1fr) auto;
    align-items: center;
    padding: 10px 16px;
    gap: 12px;
    background: var(--color-surface-container-high);
  }
  .table-row + .table-row {
    border-top: 1px solid var(--color-outline-soft);
  }
  .table-head {
    background: var(--color-surface-container-highest);
    font-size: 12px;
    font-weight: 600;
    color: var(--color-text-soft);
  }
  .row-dead {
    opacity: 0.5;
  }
  .list-table .table-row {
    grid-template-columns: minmax(0, 1.8fr) 110px 90px minmax(0, 1fr);
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
  .sources-row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .source-card {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 14px 18px;
    background: var(--color-surface-container-high);
    border-radius: var(--radius-lg);
  }
  .source-name {
    font-weight: 600;
  }
  .empty-state {
    padding: 32px;
    text-align: center;
    color: var(--color-text-soft);
    font-size: 14px;
    background: var(--color-surface-container-high);
    border-radius: var(--radius-lg);
  }
  .form-hint {
    margin-top: 8px;
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .empty {
    padding: 48px;
    text-align: center;
    color: var(--color-text-soft);
  }
  @media (max-width: 768px) {
    .add-form {
      flex-wrap: wrap;
    }
    .table-row {
      grid-template-columns: minmax(0, 1fr) auto auto;
    }
    .pcol-country, .pcol-source {
      display: none;
    }
  }
</style>
