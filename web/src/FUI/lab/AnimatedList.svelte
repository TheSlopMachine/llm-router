<script lang="ts">
// AnimatedList: появление/удаление строк с анимацией высоты+opacity.
import type { Snippet } from 'svelte'
let { items = [], row }: { items?: unknown[]; row?: Snippet<[unknown, number]> } = $props()
</script>
<ul class="al">{#each items as it, i (i)}<li class="row">{#if row}{@render row(it, i)}{:else}{String(it)}{/if}</li>{/each}</ul>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.al { list-style: none; display: flex; flex-direction: column; gap: var(--fui-space-2); background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.row { animation: fui-row 0.18s ease; background: var(--fui-color-surface-container-high); border-radius: var(--fui-radius-xs); padding: var(--fui-space-2) var(--fui-space-3); font-size: var(--fui-text-base); color: var(--fui-color-text); }
@keyframes fui-row { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .row { animation: none; } }
</style>
