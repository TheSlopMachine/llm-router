<script lang="ts">
  import type { Plugin } from '../../lib/types'

  let {
    plugin,
    logs = [],
    crashes = [],
    loading = false
  } = $props<{
    plugin: Plugin
    logs?: Array<{ at: string; message: string }>
    crashes?: Array<{ at: string; type_key: string; cause: string }>
    loading?: boolean
  }>()
</script>

<div class="details">
  <h3>Allow hosts</h3>
  <div class="muted">{plugin.allow_hosts.join(', ')}</div>
  <h3>Recent crashes</h3>
  {#if loading}
    <div class="muted">Loading…</div>
  {:else if crashes.length === 0}
    <div class="muted">None recorded.</div>
  {:else}
    {#each crashes as crash}
      <div class="log-line"><span class="muted">{crash.at} [{crash.type_key}]</span> {crash.cause}</div>
    {/each}
  {/if}
  <h3>Recent log output</h3>
  {#if loading}
    <div class="muted">Loading…</div>
  {:else if logs.length === 0}
    <div class="muted">None recorded.</div>
  {:else}
    {#each logs as log}
      <div class="log-line"><span class="muted">{log.at}</span> {log.message}</div>
    {/each}
  {/if}
</div>

<style>
  .details {
    margin-top: 12px;
    border-top: 1px solid var(--color-border);
    padding-top: 12px;
  }
  .details h3 {
    font-size: 13px;
    margin: 12px 0 4px 0;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .log-line {
    font-size: 13px;
    padding: 4px 0;
  }
</style>
