<script lang="ts">
  import type { Snippet } from 'svelte'
  import { t } from '../../../lib/i18n.svelte'
  import { squircle } from '../../../lib/squircle'

  let {
    title,
    description,
    badge,
    children
  } = $props<{
    title: string
    description?: string
    badge?: Snippet
    children: Snippet
  }>()
</script>

<section class="card section-card" use:squircle={18}>
  <div class="card-header">
    <div class="section-title-col">
      <h3>
        {t(title)}
        {#if badge}{@render badge()}{/if}
      </h3>
      {#if description}
        <p class="help-text">{t(description)}</p>
      {/if}
    </div>
  </div>
  <div class="section-body">
    {@render children()}
  </div>
</section>

<style>
  .section-card {
    overflow: visible;
  }

  .section-title-col {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .section-title-col h3 {
    font-size: var(--text-md);
    font-weight: 500;
    color: var(--color-text);
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: 0;
  }

  .help-text {
    font-size: var(--text-sm);
    color: var(--color-text-soft);
    margin: 0;
  }

  .section-body {
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }
</style>
