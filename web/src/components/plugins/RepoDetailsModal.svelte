<script lang="ts">
  let {
    title,
    description = '',
    url,
    builtin,
    fileCount
  } = $props<{
    title: string
    description?: string
    url: string
    builtin: boolean
    fileCount: number
  }>()
</script>

{#if description}<p class="description">{description}</p>{/if}
<div class="muted">{url}</div>
<div class="facts">
  {#if builtin}<span class="badge">Built-in</span>{/if}
  <span class="muted">{fileCount} plugins</span>
</div>

<style>
  .description {
    margin: 0 0 8px 0;
  }
  .muted {
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .facts {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }
</style>
