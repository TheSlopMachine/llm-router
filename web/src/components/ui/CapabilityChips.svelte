<script lang="ts">
  import { CAPABILITY_META } from '../../lib/capabilities'
  import { t } from '../../lib/i18n.svelte'

  let { caps = [] } = $props<{
    caps?: string[]
  }>()
</script>

{#each caps as cap}
  {@const meta = CAPABILITY_META[cap]}
  <span class="chip chip-sm cap-icon {meta?.cls ?? 'chip-neutral'}" title={t(meta?.hint ?? cap)}>
    {#if meta?.icon}<span class="icon">{meta.icon}</span>{:else}{cap}{/if}
  </span>
{/each}

<style>
  .cap-icon {
    padding: 3px 6px;
  }
  .cap-icon .icon {
    font-size: 13px;
  }
</style>
