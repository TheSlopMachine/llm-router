<script lang="ts">
// Masonry: CSS-columns кладка. Пропсы: columns, gap, items snippet.
import type { Snippet } from 'svelte'
let { columns = 3, gap = 3, items }: { columns?: number; gap?: number; items?: Snippet } = $props()
</script>
<div class="masonry" style:column-count={columns} style:column-gap={`var(--fui-space-${gap})`}>{#if items}{@render items()}{/if}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.masonry { background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.masonry > :global(*) { break-inside: avoid; margin-bottom: var(--fui-space-3); }
</style>
