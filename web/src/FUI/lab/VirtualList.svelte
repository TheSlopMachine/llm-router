<script lang="ts">
// VirtualList: окно фиксированной высоты строки. Пропсы: items, itemHeight, overscan, row.
import type { Snippet } from 'svelte'
let { items = [], itemHeight = 32, overscan = 4, row }: { items?: unknown[]; itemHeight?: number; overscan?: number; row?: Snippet<[unknown, number]> } = $props()
let top = $state(0); let viewH = $state(320); let sc: HTMLElement | null = $state(null)
let start = $derived(Math.max(0, Math.floor(top / itemHeight) - overscan))
let count = $derived(Math.ceil(viewH / itemHeight) + overscan * 2)
let slice = $derived(items.slice(start, start + count))
function onscroll() { if (sc) top = sc.scrollTop }
</script>
<div class="vl" bind:this={sc} onscroll={onscroll}><div style:height={`${items.length * itemHeight}px`} class="spacer">{#each slice as it, k}<div class="cell" style:height={`${itemHeight}px`} style:transform={`translateY(${(start + k) * itemHeight}px)`}>{#if row}{@render row(it, start + k)}{:else}{String(it)}{/if}</div>{/each}</div></div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.vl { overflow-y: auto; max-height: var(--fui-ctl-large); min-height: 120px; background: var(--fui-elev); border-radius: var(--fui-radius-sm); font-size: var(--fui-text-sm); color: var(--fui-color-text); }
.spacer { position: relative; }
.cell { position: absolute; top: 0; left: 0; right: 0; padding: var(--fui-space-1) var(--fui-space-3); overflow: hidden; }
</style>
