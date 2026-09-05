<script lang="ts">
  import type { Snippet } from 'svelte'

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

<section class="card section-card">
  <div class="card-header">
    <div class="section-title-col">
      <h3>
        {title}
        {#if badge}{@render badge()}{/if}
      </h3>
      {#if description}
        <p class="help-text">{description}</p>
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
    gap: 4px;
  }

  .section-title-col h3 {
    font-size: 16px;
    font-weight: 500;
    color: var(--color-text);
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
  }

  .help-text {
    font-size: 12px;
    color: var(--color-text-soft);
    margin: 0;
  }

  .section-body {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
</style>
