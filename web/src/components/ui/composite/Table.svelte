<script module lang="ts">
  import { squircle } from '../../../lib/squircle'
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

<div class="uit-table" role="table" style="grid-template-columns: {template}"  use:squircle={18}>
  <div class="uit-head" role="row">
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
      <div class="uit-row" role="row" aria-hidden="true">
        {#if draggable}<span class="uit-cell align-c"></span>{/if}
        {#each columns as col, c}
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
    background: var(--elev);
    border-radius: 0;
  }
  /* One shared grid: header rows are transparent so every track sizes once.
     Draggable body rows own a box (display:contents elements cannot start a
     native drag), so they lay out as subgrids spanning all tracks: same
     column alignment, plus a real drag source. */
  .uit-head {
    display: contents;
  }
  .uit-row {
    display: contents;
  }
  .uit-row-drag {
    display: grid;
    grid-template-columns: subgrid;
    grid-column: 1 / -1;
  }
  .uit-head {
    cursor: default;
  }
  .uit-th {
    display: flex;
    align-items: center;
    min-width: 0;
    padding: var(--space-3) var(--space-2);
    /* Previously inherited from the legacy global .table-head: owned here
       now that the composite no longer shares those class names. */
    font-size: var(--text-sm);
    font-weight: 600;
    color: var(--color-text-soft);
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
    gap: var(--space-1);
    min-width: 0;
    padding: var(--space-3) var(--space-2);
  }
  .uit-th:first-child,
  .uit-cell:first-child {
    padding-left: var(--space-5);
  }
  .uit-th:last-child,
  .uit-cell:last-child {
    padding-right: var(--space-5);
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
    gap: var(--space-2);
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
    font-size: var(--text-base);
    opacity: 0.7;
  }
  /* Column direction now: align-items positions the stacked content
     horizontally, justify-content (set above) centers it vertically. */
  .uit-cell.align-l { align-items: flex-start; }
  .uit-cell.align-c { align-items: center; }
  .uit-cell.align-r { align-items: flex-end; }
  .uit-grip {
    color: var(--color-text-soft);
    font-size: var(--text-md);
    cursor: grab;
    margin-right: var(--space-3);
  }
  .uit-empty {
    grid-column: 1 / -1;
    padding: var(--space-6) var(--space-5);
    display: flex;
    justify-content: center;
    text-align: center;
  }
  /* Dimmed row (disabled entries). Owned here so rowClass can use it. */
  .row-off {
    opacity: 0.55;
  }
  /* Skeleton: one elevation above the cell background. */
  .skel {
    display: block;
    height: 12px;
    border-radius: 6px;
    background: linear-gradient(
      90deg,
      var(--color-surface-container-high) 25%,
      var(--color-surface-container-highest) 50%,
      var(--color-surface-container-high) 75%
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
