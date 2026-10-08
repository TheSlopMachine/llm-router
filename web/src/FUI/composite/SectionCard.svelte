<script lang="ts">
  import type { Snippet } from 'svelte'
  import { squircle } from '../core/squircle'

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
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  /* Card chrome, scoped: SectionCard is the sole consumer of the removed
     global .card/.card-header rules. */
  .card {
    background: var(--fui-elev);
    border: none;
    border-radius: var(--fui-radius-lg);
    overflow: hidden;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--fui-space-5);
    border-bottom: 1px solid var(--fui-color-outline-soft);
  }

  .section-card {
    overflow: visible;
  }

  .section-title-col {
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-2);
  }

  .section-title-col h3 {
    font-size: var(--fui-text-md);
    font-weight: 500;
    color: var(--fui-color-text);
    display: flex;
    align-items: center;
    gap: var(--fui-space-3);
    margin: 0;
  }

  .help-text {
    font-size: var(--fui-text-sm);
    color: var(--fui-color-text-soft);
    margin: 0;
  }

  .section-body {
    padding: var(--fui-space-5);
    display: flex;
    flex-direction: column;
    gap: var(--fui-space-5);
  }
</style>
