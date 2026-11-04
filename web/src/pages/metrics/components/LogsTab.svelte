<script lang="ts">
  import { onMount, tick } from 'svelte'
  import {
    Banner, Button, ContentUnavailable, DisclosureGroup, GroupBox, HStack, Spacer, VStack, VirtualList, modal,
  } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import {
    CUSTOM_RANGE, FACET_KEYS, facetGroups, isoOrUndefined, matchesSearch,
    mergeEntries, observedValues, rangeBounds, selectableKeys, toDateTimeLocal, withFilter,
  } from '$lib/events'
  import { locale, t } from '$lib/i18n.svelte'
  import type { EventEntry, EventKeyFilter } from '$lib/types'
  import LogDetail from './LogDetail.svelte'
  import LogFacets from './LogFacets.svelte'
  import LogFilterBuilder from './LogFilterBuilder.svelte'
  import LogRow from './LogRow.svelte'
  import LogToolbar from './LogToolbar.svelte'

  const PAGE_SIZE = 1000
  const LIVE_PAGE_SIZE = 500
  const BUFFER_CAP = 20000
  const POLL_INTERVAL = 2000
  const ROW_HEIGHT = 32
  const VALUE_OPTIONS_LIMIT = 200
  // Events around a picked one: this many ms on each side.
  const AROUND_SPAN = 30_000

  let severity = $state(1)
  let rangeId = $state('1h')
  let customStart = $state('')
  let customEnd = $state('')
  let filters = $state.raw<EventKeyFilter[]>([])
  let search = $state('')
  let live = $state(true)
  let follow = $state(true)
  let entries = $state.raw<EventEntry[]>([])
  let loading = $state(true)
  let loadingOlder = $state(false)
  let olderAvailable = $state(false)
  let error = $state('')
  let clock = $state(Date.now())
  let facetsOpen = $state<string[]>([])
  let list: ReturnType<typeof VirtualList<EventEntry>> | undefined = $state()

  let latestRequest = 0
  let polling = false

  const bounds = $derived(rangeBounds(rangeId, customStart, customEnd, clock))
  const liveAvailable = $derived(bounds.end === null)
  const needle = $derived(search.trim())
  const visible = $derived(needle === '' ? entries : entries.filter((entry) => matchesSearch(entry, needle)))
  const keys = $derived(selectableKeys(entries))
  const facets = $derived(facetGroups(entries, FACET_KEYS, 8))

  // Rows carry a date too once the range can span midnight.
  const longRange = $derived(bounds.start === null || (bounds.end ?? clock) - bounds.start >= 86_400_000)

  const timeFormatter = $derived(
    new Intl.DateTimeFormat(locale(), {
      ...(longRange ? { month: 'short', day: 'numeric' } : {}),
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
      hour12: false,
    }),
  )
  function formatTime(timestamp: string): string {
    return timeFormatter.format(new Date(timestamp))
  }

  async function reload(): Promise<void> {
    const request = ++latestRequest
    loading = true
    error = ''
    clock = Date.now()
    const range = rangeBounds(rangeId, customStart, customEnd, clock)
    try {
      const result = await api.events.list({
        severity,
        start: isoOrUndefined(range.start),
        end: isoOrUndefined(range.end),
        limit: PAGE_SIZE,
        filters,
      })
      if (request !== latestRequest) return
      entries = mergeEntries([], result, BUFFER_CAP)
      olderAvailable = result.length >= PAGE_SIZE
      follow = true
    } catch (err) {
      if (request === latestRequest) error = getErrorMessage(err)
    } finally {
      if (request === latestRequest) loading = false
    }
  }

  // New events only: the next request starts at the newest loaded timestamp (inclusive).
  async function poll(): Promise<void> {
    if (!live || !liveAvailable || loading || polling || document.hidden) return
    polling = true
    const request = latestRequest
    try {
      const newest = entries.at(-1)
      const range = rangeBounds(rangeId, customStart, customEnd, Date.now())
      const result = await api.events.list({
        severity,
        start: newest === undefined ? isoOrUndefined(range.start) : newest.timestamp,
        limit: LIVE_PAGE_SIZE,
        filters,
      })
      if (request !== latestRequest) return
      clock = Date.now()
      error = ''
      if (result.length >= LIVE_PAGE_SIZE) {
        // A burst larger than one page: the middle is unreachable, so start over from the newest.
        polling = false
        await reload()
        return
      }
      const fresh = result.filter((entry) => newest === undefined || entry.id > newest.id)
      if (fresh.length > 0) entries = mergeEntries(entries, fresh, BUFFER_CAP)
    } catch (err) {
      if (request === latestRequest) error = getErrorMessage(err)
    } finally {
      polling = false
    }
  }

  async function loadOlder(): Promise<void> {
    const oldest = entries[0]
    if (oldest === undefined || loadingOlder) return
    loadingOlder = true
    const request = latestRequest
    try {
      const range = rangeBounds(rangeId, customStart, customEnd, Date.now())
      // `end` is inclusive, so the oldest loaded entry comes back too and is dropped below.
      const result = await api.events.list({
        severity,
        start: isoOrUndefined(range.start),
        end: oldest.timestamp,
        limit: PAGE_SIZE,
        filters,
      })
      if (request !== latestRequest) return
      const older = result.filter((entry) => entry.id < oldest.id)
      olderAvailable = result.length >= PAGE_SIZE
      follow = false
      entries = mergeEntries(entries, older, BUFFER_CAP)
      await tick()
      list?.scrollBy(older.length * ROW_HEIGHT)
    } catch (err) {
      error = getErrorMessage(err)
    } finally {
      loadingOlder = false
    }
  }

  function setLive(next: boolean): void {
    live = next
    if (!next) return
    list?.scrollToEnd()
    void poll()
  }

  function setSeverity(next: number): void {
    severity = next
    void reload()
  }

  function setRange(next: string): void {
    rangeId = next
    if (next === CUSTOM_RANGE && customStart === '') {
      customStart = toDateTimeLocal(Date.now() - 3_600_000)
      customEnd = ''
    }
    void reload()
  }

  function applyFilters(next: EventKeyFilter[]): void {
    filters = next
    void reload()
  }

  function addFilter(key: string, value: string, exclude: boolean): void {
    applyFilters(withFilter(filters, { key, value, exclude }))
  }

  function showAround(entry: EventEntry): void {
    const moment = Date.parse(entry.timestamp)
    customStart = toDateTimeLocal(moment - AROUND_SPAN)
    customEnd = toDateTimeLocal(moment + AROUND_SPAN)
    rangeId = CUSTOM_RANGE
    severity = 0
    filters = []
    void reload()
  }

  function openDetail(entry: EventEntry): void {
    modal.open({
      title: t('metrics.logs.detail.title'),
      content: LogDetail,
      size: 'md',
      severity: 'medium',
      props: {
        entry,
        onfilter: (key: string, value: string) => addFilter(key, value, false),
        onexclude: (key: string, value: string) => addFilter(key, value, true),
        onaround: showAround,
      },
    })
  }

  onMount(() => {
    void reload()
    const timer = window.setInterval(() => void poll(), POLL_INTERVAL)
    return () => window.clearInterval(timer)
  })
</script>

<VStack gap={4}>
  <LogToolbar
    {live}
    {liveAvailable}
    {severity}
    {rangeId}
    bind:customStart
    bind:customEnd
    bind:search
    {loading}
    onlive={setLive}
    onseverity={setSeverity}
    onrange={setRange}
    oncustomrange={() => void reload()}
    onrefresh={() => void reload()}
  />

  <LogFilterBuilder {filters} {keys} valuesFor={(key) => observedValues(entries, key, VALUE_OPTIONS_LIMIT)} onchange={applyFilters} />

  {#if error}
    <Banner tint="danger" text={error} />
  {/if}

  {#if facets.length > 0}
    <DisclosureGroup
      bind:value={facetsOpen}
      items={[{ id: 'facets', title: t('metrics.logs.facets.title'), body: facetsBody }]}
    />
  {/if}

  <GroupBox>
    <VStack gap={3}>
      {#if olderAvailable || !follow}
        <HStack valign="center" gap={3}>
          <Spacer />
          {#if olderAvailable}
            <Button
              buttonStyle="neutral"
              size="sm"
              icon={{ name: 'arrow_upward' }}
              text={t('metrics.logs.load_older.action')}
              loading={loadingOlder}
              onclick={() => void loadOlder()}
            />
          {/if}
          {#if !follow}
            <Button
              buttonStyle="prominent"
              size="sm"
              icon={{ name: 'keyboard_double_arrow_down' }}
              text={t('metrics.logs.jump_latest.action')}
              onclick={() => list?.scrollToEnd()}
            />
          {/if}
        </HStack>
      {/if}
      <VirtualList
        bind:this={list}
        bind:follow
        items={visible}
        rowHeight={ROW_HEIGHT}
        itemKey={(entry) => entry.id}
        onselect={openDetail}
        height="72dvh"
        minHeight={360}
      >
        {#snippet row(entry)}
          <LogRow {entry} {formatTime} />
        {/snippet}
        {#snippet empty()}
          <ContentUnavailable title={loading ? t('common.state.loading') : t('metrics.logs.empty.title')} />
        {/snippet}
      </VirtualList>
    </VStack>
  </GroupBox>
</VStack>

{#snippet facetsBody()}
  <LogFacets groups={facets} onfilter={(key, value) => addFilter(key, value, false)} />
{/snippet}
