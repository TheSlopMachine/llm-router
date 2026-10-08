<script lang="ts">
// Skeleton placeholder + Redacted wrapper. Props: width, height, radius, shape, lines; Redacted: active, children.
  import type { Snippet } from 'svelte';
  let { width = '100%', height, radius, shape = 'rect', lines = 1, redacted = false, active = true, children } = $props<{
    width?: string; height?: string; radius?: string; shape?: 'rect'|'text'|'circle'; lines?: number;
    redacted?: boolean; active?: boolean; children?: Snippet;
  }>();
</script>
{#if redacted}
  <span class="red" class:on={active}>{#if children}{@render children()}{/if}</span>
{:else}
  <span class="sk" aria-hidden="true">
    {#each Array(shape === 'text' ? lines : 1) as _, i (i)}
      <span class="line line-{shape}" style:width={i === lines - 1 && shape === 'text' ? '60%' : width} style:height={height ?? (shape === 'circle' ? width : shape === 'text' ? 'var(--fui-text-base)' : 'var(--fui-space-5)')} style:border-radius={radius ?? (shape === 'circle' ? '9999px' : 'var(--fui-radius-xs)')}></span>
    {/each}
  </span>
{/if}
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .sk { display: inline-flex; flex-direction: column; gap: var(--fui-space-2); }
  .line { display: block; background: linear-gradient(90deg, var(--fui-elev) 25%, var(--fui-color-surface-container-high) 50%, var(--fui-elev) 75%); background-size: 200% 100%; animation: shimmer 1.4s linear infinite; }
  @keyframes shimmer { from { background-position: 200% 0; } to { background-position: -200% 0; } }
  .red.on { color: transparent !important; }
  .red.on > :global(*) { color: transparent !important; background: var(--fui-elev) !important; border-radius: var(--fui-radius-xs); user-select: none; }
  @media (prefers-reduced-motion: reduce) { .line { animation: none; } }
</style>
