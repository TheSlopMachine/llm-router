<script lang="ts">
  import type { PluginFacts } from './plugin-facts'
  import { t } from '../../lib/i18n.svelte'

  let { facts } = $props<{ facts: PluginFacts }>()
</script>

{#if facts.description}<p class="description">{facts.description}</p>{/if}
{#if facts.versionLine}<div class="muted">{facts.versionLine}</div>{/if}

<h3>{t('Requested permissions')}</h3>
{#if facts.unsafe}
  <div class="badge badge-red">{t('Unrestricted network')}</div>
{/if}
{#if facts.allowHosts.length === 0}
  <div class="muted">{t('No network hosts.')}</div>
{:else}
  {#each facts.allowHosts as host}
    <div class="mono host">{host}</div>
  {/each}
{/if}

<style>
  .description {
    margin: 0 0 8px 0;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
  }
  h3 {
    font-size: 13px;
    margin: 16px 0 6px 0;
  }
  .host {
    padding: 2px 0;
  }
</style>
