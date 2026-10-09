<script module lang="ts">
  import { squircle } from '../core/squircle'
  import { squircleAuto } from '../core/squircle-baked'
  import Icon from '../controls/Icon.svelte'
  // Column block: header title plus the cells beneath it.
  export interface TableColumn {
    key: string
    // Text-only header. Empty allowed.
    title?: string
    // '120px', '25%' or undefined (= content width).
    width?: string
    // Default left.
    align?: 'left' | 'center' | 'right'
    sortable?: boolean
    // Responsive hiding: 1 never hides, 2 hides when very narrow,
    // 3 hides first. Unset means 1.
    priority?: 1 | 2 | 3
  }

  export type TableSortDir = 'asc' | 'desc'
</script>

<script lang="ts" generics="T">
  import type { Snippet } from 'svelte'
  import { CARD_BELOW, priorityCeiling, alignClass, sortAria } from '../core/table-layout'
  let {
    columns,
    rows,
    rowKey,
    loading = false,
    skeletonRows = 5,
    sortKey = null,
    sortDir = null,
    onsort,
    draggable = false,
    onReorder,
    rowClass,
    cell,
    empty,
    card,
    onrowclick,
    hideHeaderWhenEmpty = false
  } = $props<{
    columns: TableColumn[]
    rows: T[]
    rowKey: (row: T) => string | number
    loading?: boolean
    skeletonRows?: number
    sortKey?: string | null
    sortDir?: TableSortDir | null
    onsort?: (key: string) => void
    draggable?: boolean
    onReorder?: (from: number, to: number) => void
    rowClass?: (row: T) => string
    cell: Snippet<[{ column: TableColumn; row: T; rowIndex: number }]>
    empty?: Snippet<[]>
    card?: Snippet<[{ row: T }]>
    onrowclick?: (row: T) => void
    hideHeaderWhenEmpty?: boolean
  }>()

  // Sortable and draggable never mix: drag wins, sort renders plain.
  const sortOn = $derived(!draggable)

  // Responsive columns: the table measures itself and drops low-priority
  // columns as it narrows (<1024 hides p3, <640 hides p2 too). Never hide
  // everything: fall back to the full set rather than an empty table.
  let rootEl = $state<HTMLElement | null>(null)
  let tableWidth = $state<number | null>(null)
  $effect(() => {
    const el = rootEl
    if (!el || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver((entries) => {
      tableWidth = entries[0]?.contentRect.width ?? null
    })
    ro.observe(el)
    return () => ro.disconnect()
  })
  const visibleColumns = $derived.by(() => {
    const w = tableWidth
    if (w == null) return columns
    const maxP = priorityCeiling(w)
    const vis = columns.filter((c: TableColumn) => (c.priority ?? 1) <= maxP)
    return vis.length > 0 ? vis : columns
  })

  const template = $derived(
    (draggable ? 'var(--fui-table-drag-col) ' : '') + visibleColumns.map((c: TableColumn) => c.width ?? 'auto').join(' ')
  )

  function alignCls(col: TableColumn): string {
    return alignClass(col.align)
  }

  function sortIcon(col: TableColumn): string {
    if (sortKey !== col.key || sortDir == null) return 'unfold_more'
    return sortDir === 'asc' ? 'arrow_upward' : 'arrow_downward'
  }

  let dragFrom: number | null = $state(null)

  function onDragStart(e: DragEvent, index: number): void {
    dragFrom = index
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      try {
        e.dataTransfer.setData('text/plain', String(index))
      } catch {
        // DataTransfer may be unavailable; index state suffices.
      }
    }
  }

  function onDrop(e: DragEvent, index: number): void {
    e.preventDefault()
    if (dragFrom != null && dragFrom !== index) onReorder?.(dragFrom, index)
    dragFrom = null
  }

  const isCard = $derived(card != null && tableWidth != null && tableWidth < CARD_BELOW)

  function handleRowKey(e: KeyboardEvent, row: T): void {
    if (!onrowclick) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      onrowclick(row)
    }
  }
</script>

<div bind:this={rootEl} class="uit-table" class:uit-cards-mode={isCard} role="table" style:grid-template-columns={template} use:squircleAuto={{ off: isCard }}>
  {#if isCard}
    {#if loading}
      {#each Array(skeletonRows) as _}
        <div class="uit-card" aria-hidden="true"><span class="skel" style:width="72%"></span></div>
      {/each}
    {:else if rows.length === 0}
      <div class="uit-empty">
        {#if empty}{@render empty()}{/if}
      </div>
    {:else}
      {#each rows as row (rowKey(row))}
        {#if onrowclick}
          <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
          <div
            class="uit-card uit-clickable"
            role="button"
            tabindex="0"
            onclick={() => onrowclick(row)}
            onkeydown={(e) => handleRowKey(e, row)}
            use:squircle
          >{@render card?.({ row })}</div>
        {:else}
          <div class="uit-card" role="row" use:squircle>{@render card?.({ row })}</div>
        {/if}
      {/each}
    {/if}
  {:else}
  {#if !(hideHeaderWhenEmpty && rows.length === 0 && !loading)}
  <div class="uit-head" role="row">
    {#if draggable}<span class="uit-th uit-draghead" aria-hidden="true"></span>{/if}
    {#each visibleColumns as col (col.key)}
      <span
        class="uit-th {alignCls(col)}"
        role="columnheader"
        aria-sort={sortAria(sortOn, sortKey, col.key, sortDir)}
      >
        {#if sortOn && col.sortable}
          <button type="button" class="thead-sort {alignCls(col)}" onclick={() => onsort?.(col.key)}>
            <span class="thead-title">{col.title ?? ''}</span>
            <span class="thead-sort-icon"><Icon name={sortIcon(col)} size="base" /></span>
          </button>
        {:else}
          {col.title ?? ''}
        {/if}
      </span>
    {/each}
  </div>
  {/if}
  {#if loading}
    {#each Array(skeletonRows) as _, r}
      <div class="uit-row" role="row" aria-hidden="true">
        {#if draggable}<span class="uit-cell align-c"></span>{/if}
        {#each visibleColumns as col, c}
          <span class="uit-cell {alignCls(col)}"><span class="skel" style:width={`${[72, 48, 88, 60, 78][(r + c) % 5]}%`}></span></span>
        {/each}
      </div>
    {/each}
  {:else if rows.length === 0}
    <div class="uit-empty">
      {#if empty}{@render empty()}{/if}
    </div>
  {:else}
    {#each rows as row, i (rowKey(row))}
      {@const cls = rowClass?.(row) ?? ''}
      {#if onrowclick && !draggable}
        <!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
        <div
          class="uit-row uit-row-clickable {cls}"
          class:uit-row-drag={draggable}
          role="button"
          tabindex={0}
          onclick={() => onrowclick(row)}
          onkeydown={(e) => handleRowKey(e, row)}
          ondragstart={(e) => draggable && onDragStart(e, i)}
          ondragover={(e) => draggable && e.preventDefault()}
          ondrop={(e) => draggable && onDrop(e, i)}
        >
          {#each visibleColumns as col (col.key)}
            <span class="uit-cell {alignCls(col)}" role="cell">
              {@render cell({ column: col, row, rowIndex: i })}
            </span>
          {/each}
        </div>
      {:else}
      <div
        class="uit-row"
        class:uit-row-drag={draggable}
        role="row"
        tabindex={draggable ? 0 : undefined}
        draggable={draggable}
        ondragstart={(e) => draggable && onDragStart(e, i)}
        ondragover={(e) => draggable && e.preventDefault()}
        ondrop={(e) => draggable && onDrop(e, i)}
      >
        {#if draggable}
          <span class="uit-cell align-c {cls}" role="cell">
            <span class="uit-grip"><Icon name="drag_indicator" size="md" tone="soft" /></span>
          </span>
        {/if}
        {#each visibleColumns as col (col.key)}
          <span class="uit-cell {alignCls(col)} {cls}" role="cell">
            {@render cell({ column: col, row, rowIndex: i })}
          </span>
        {/each}
      </div>
      {/if}
    {/each}
  {/if}
  {/if}
</div>

<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  :where(button) { font: inherit; color: inherit; background: none; border: 0; cursor: pointer; }
  :where(button:disabled) { cursor: not-allowed; }
  .uit-table {
    display: grid;
    width: 100%;
    /* The root is the table's base layer (one elev wash) and its squircle
       silhouette; the header band stacks a second wash on top. */
    background: var(--fui-elev);
    border-radius: var(--fui-radius-lg);
  }
  /* One shared grid: head and rows lay their cells on the root's tracks, so
     every column sizes once. The head is a subgrid box (not display:contents)
     so it can own a background: one elev wash over the body's, i.e. the
     header sits one level above (L*N), painted once across all tracks, no
     per-cell fills and so no subpixel seams. */
  .uit-head {
    display: grid;
    grid-template-columns: subgrid;
    grid-column: 1 / -1;
    background: var(--fui-elev);
    cursor: default;
  }
  .uit-row {
    display: contents;
  }
  /* Draggable body rows own a box too (display:contents elements cannot
     start a native drag), same subgrid pattern: same column alignment, plus
     a real drag source. */
  .uit-row-drag {
    display: grid;
    grid-template-columns: subgrid;
    grid-column: 1 / -1;
  }
  .uit-th {
    display: flex;
    align-items: center;
    gap: var(--fui-space-4);
    min-width: 0;
    overflow-wrap: anywhere;
    padding: var(--fui-space-3) var(--fui-space-2);
    /* Previously inherited from the legacy global .table-head: owned here
       now that the composite no longer shares those class names. */
    font-size: var(--fui-text-sm);
    font-weight: 600;
    color: var(--fui-color-text-soft);
  }
  /* Body cells stack their content vertically by default (a VStack): most
     cells hold a title plus a subtitle/meta line. A cell that genuinely
     needs a row just wraps its own content in an HStack -- nothing here
     needs to change for that. align-l/c/r now controls the horizontal
     position of the stacked column, not a row's justify-content. */
  .uit-cell {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--fui-space-1);
    min-width: 0;
    padding: var(--fui-space-3) var(--fui-space-2);
  }
  .uit-th:first-child,
  .uit-cell:first-child {
    padding-left: var(--fui-space-5);
  }
  .uit-th:last-child,
  .uit-cell:last-child {
    padding-right: var(--fui-space-5);
  }
  .uit-th {
    background: transparent;
  }
  /* Row dividers live on the cells: display:contents rows render nothing. */
  .uit-cell {
    border-top: var(--fui-border-w) solid var(--fui-color-outline-soft);
  }
  .uit-th.align-l { justify-content: flex-start; }
  .uit-th.align-c { justify-content: center; }
  .uit-th.align-r { justify-content: flex-end; }
  /* Sort control: bare text button, no fill, full header-cell click target. */
  .thead-sort {
    display: inline-flex;
    align-items: center;
    gap: var(--fui-space-2);
    background: transparent;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }
  .thead-sort:focus-visible {
    outline: none;
    background: var(--fui-color-button-container-high);
    border-radius: var(--fui-table-focus-radius);
  }
  .thead-sort-icon {
    display: inline-flex;
    opacity: var(--fui-opacity-faint);
  }
  /* Column direction now: align-items positions the stacked content
     horizontally, justify-content (set above) centers it vertically. */
  .uit-cell.align-l { align-items: flex-start; }
  .uit-cell.align-c { align-items: center; }
  .uit-cell.align-r { align-items: flex-end; }
  .uit-grip {
    display: inline-flex;
    cursor: grab;
    margin-right: var(--fui-space-3);
  }
  .uit-empty {
    grid-column: 1 / -1;
    padding: var(--fui-space-6) var(--fui-space-5);
    display: flex;
    justify-content: center;
    text-align: center;
  }
  /* Dimmed row (disabled entries). Owned here so rowClass can use it. */
  .row-off {
    opacity: var(--fui-opacity-disabled);
  }
  /* Card mode: narrow container renders cards instead of the grid. */
  .uit-cards-mode {
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-3);
    background: transparent;
  }
  .uit-card {
    background: var(--fui-elev);
    border-radius: var(--fui-radius-lg);
    padding: var(--fui-space-4);
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-2);
    min-width: 0;
  }
  .uit-clickable {
    cursor: pointer;
  }
  .uit-row-clickable {
    cursor: pointer;
  }
  .uit-card:focus-visible,
  .uit-row-clickable:focus-visible {
    outline: none;
    box-shadow: var(--fui-focus-ring);
  }
  /* Skeleton: one elevation above the cell background. */
  .skel {
    display: block;
    height: var(--fui-table-skel-h);
    border-radius: var(--fui-table-focus-radius);
    background: linear-gradient(
      90deg,
      var(--fui-color-surface-container-high) 25%,
      var(--fui-color-surface-container-highest) 50%,
      var(--fui-color-surface-container-high) 75%
    );
    background-size: 200% 100%;
    animation: uit-skel var(--fui-dur-skeleton) ease-in-out infinite;
  }
  @keyframes uit-skel {
    from { background-position: 200% 0; }
    to { background-position: -200% 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    .skel {
      animation: none;
    }
  }
</style>
