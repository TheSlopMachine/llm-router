<script module lang="ts">
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
  }

  export type TableSortDir = 'asc' | 'desc'
</script>

<script lang="ts" generics="T">
  import type { Snippet } from 'svelte'

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
    empty
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
  }>()

  // Sortable and draggable never mix: drag wins, sort renders plain.
  const sortOn = $derived(!draggable)

  const template = $derived(
    (draggable ? '28px ' : '') + columns.map((c: TableColumn) => c.width ?? 'auto').join(' ')
  )

  function alignCls(col: TableColumn): string {
    return col.align === 'center' ? 'align-c' : col.align === 'right' ? 'align-r' : 'align-l'
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
</script>

<div class="table uit-table" role="table" style="grid-template-columns: {template}">
  <div class="table-row table-head uit-head" role="row">
    {#if draggable}<span class="uit-th uit-draghead" aria-hidden="true"></span>{/if}
    {#each columns as col (col.key)}
      <span
        class="uit-th {alignCls(col)}"
        role="columnheader"
        aria-sort={sortOn && sortKey === col.key && sortDir != null ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
      >
        {#if sortOn && col.sortable}
          <button type="button" class="thead-sort {alignCls(col)}" onclick={() => onsort?.(col.key)}>
            <span class="thead-title">{col.title ?? ''}</span>
            <span class="icon thead-sort-icon" aria-hidden="true">{sortIcon(col)}</span>
          </button>
        {:else}
          {col.title ?? ''}
        {/if}
      </span>
    {/each}
  </div>
  {#if loading}
    {#each Array(skeletonRows) as _, r}
      <div class="table-row uit-row" role="row" aria-hidden="true">
        {#if draggable}<span class="uit-cell align-c"></span>{/if}
        {#each columns as col, c}
          <span class="uit-cell align-l"><span class="skel" style:width={`${[72, 48, 88, 60, 78][(r + c) % 5]}%`}></span></span>
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
      <div
        class="table-row uit-row"
        role="row"
        tabindex={draggable ? 0 : undefined}
        draggable={draggable}
        ondragstart={(e) => draggable && onDragStart(e, i)}
        ondragover={(e) => draggable && e.preventDefault()}
        ondrop={(e) => draggable && onDrop(e, i)}
      >
        {#if draggable}
          <span class="uit-cell align-c {cls}" role="cell">
            <span class="icon uit-grip" aria-hidden="true">drag_indicator</span>
          </span>
        {/if}
        {#each columns as col (col.key)}
          <span class="uit-cell {alignCls(col)} {cls}" role="cell">
            {@render cell({ column: col, row, rowIndex: i })}
          </span>
        {/each}
      </div>
    {/each}
  {/if}
</div>

<style>
  .uit-table {
    display: grid;
    width: 100%;
    /* Transparent, square root: the page wrapper owns the single base
       layer and the squircle silhouette. A painted, rounded root would
       stack a second elev level and peek out at the corners. */
    background: transparent;
    border-radius: 0;
  }
  /* One shared grid: rows are transparent so every track sizes once —
     header titles and cell contents always start at the same x. */
  .uit-head,
  .uit-row {
    display: contents;
  }
  .uit-head {
    cursor: default;
  }
  .uit-th,
  .uit-cell {
    display: flex;
    align-items: center;
    min-width: 0;
    padding: 10px 6px;
  }
  .uit-th:first-child,
  .uit-cell:first-child {
    padding-left: 16px;
  }
  .uit-th:last-child,
  .uit-cell:last-child {
    padding-right: 16px;
  }
  /* Header band tiles per cell (no grid gap inside the widget). */
  .uit-th {
    background: var(--elev);
  }
  /* Row dividers live on the cells: display:contents rows render nothing. */
  .uit-cell {
    border-top: 1px solid var(--color-outline-soft);
  }
  .uit-th.align-l { justify-content: flex-start; }
  .uit-th.align-c { justify-content: center; }
  .uit-th.align-r { justify-content: flex-end; }
  /* Sort control: no hover fill, full header-cell click target. */
  .thead-sort {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    background: transparent;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }
  .thead-sort:hover {
    background: transparent;
  }
  .thead-sort:focus-visible {
    background: var(--color-button-container-high);
    border-radius: 6px;
  }
  .thead-sort-icon {
    font-size: 14px;
    opacity: 0.7;
  }
  .uit-cell.align-l { justify-content: flex-start; }
  .uit-cell.align-c { justify-content: center; }
  .uit-cell.align-r { justify-content: flex-end; }
  .uit-grip {
    color: var(--color-text-soft);
    font-size: 18px;
    cursor: grab;
  }
  .uit-empty {
    grid-column: 1 / -1;
    padding: 24px 16px;
    display: flex;
    justify-content: center;
    text-align: center;
  }
  /* Dimmed row (disabled entries). Owned here so rowClass can use it. */
  .row-off {
    opacity: 0.55;
  }
  /* Skeleton: shimmering outlines while loading. */
  .skel {
    display: block;
    height: 12px;
    border-radius: 6px;
    background: linear-gradient(
      90deg,
      var(--color-surface-container-highest) 25%,
      var(--color-outline-soft) 50%,
      var(--color-surface-container-highest) 75%
    );
    background-size: 200% 100%;
    animation: uit-skel 1.2s ease-in-out infinite;
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
