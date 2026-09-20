<script lang="ts">
  import Chip from './Chip.svelte'
  import { CAPABILITY_META } from '../../lib/capabilities'
  import { t } from '../../lib/i18n.svelte'

  let { caps = [], hints = {} } = $props<{
    caps?: string[]
    hints?: Record<string, string>
  }>()
</script>

<div class="cap-chips">
  {#each caps as cap}
    {@const meta = CAPABILITY_META[cap]}
    {#if meta}
      <Chip icon={meta.icon} text={t(meta.label)} cls={meta.cls} title={hints[cap] ?? t(meta.hint)} />
    {:else}
      <Chip text={cap} title={hints[cap] ?? cap} />
    {/if}
  {/each}
</div>

<style>
  /* Chips flow inside table cells: wrap with gaps instead of one long line. */
  .cap-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
  }
</style>
