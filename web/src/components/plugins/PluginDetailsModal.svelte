<script lang="ts">
  import type { PluginFacts } from './plugin-facts'
  import { t } from '../../lib/i18n.svelte'

  let {
    facts,
    logs = [],
    crashes = [],
    loading = false
  } = $props<{
    facts: PluginFacts
    logs?: Array<{ at: string; message: string }>
    crashes?: Array<{ at: string; type_key: string; cause: string }>
    loading?: boolean
  }>()
</script>

{#if facts.description}<p class="description">{facts.description}</p>{/if}
<div class="mono muted">{facts.idLine}</div>

{#if facts.typeKeys.length > 0}
  <h3>{t('Provides')}</h3>
  <div class="muted">{facts.typeKeys.join(', ')}</div>
{/if}

<h3>{t('Permissions')}</h3>
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

{#if logs.length > 0 || crashes.length > 0 || loading}
  <h3>{t('Recent crashes')}</h3>
  {#if loading}
    <div class="muted">{t('Loading…')}</div>
  {:else if crashes.length === 0}
    <div class="muted">{t('None recorded.')}</div>
  {:else}
    {#each crashes as crash}
      <div class="log-line"><span class="muted">{crash.at} [{crash.type_key}]</span> {crash.cause}</div>
    {/each}
  {/if}
  <h3>{t('Recent log output')}</h3>
  {#if loading}
    <div class="muted">{t('Loading…')}</div>
  {:else if logs.length === 0}
    <div class="muted">{t('None recorded.')}</div>
  {:else}
    {#each logs as log}
      <div class="log-line"><span class="muted">{log.at}</span> {log.message}</div>
    {/each}
  {/if}
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
  .log-line {
    font-size: 13px;
    padding: 4px 0;
  }
</style>
