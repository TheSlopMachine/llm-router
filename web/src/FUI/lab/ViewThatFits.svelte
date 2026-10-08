<script lang="ts">
// ViewThatFits: первый помещающийся variant из массива snippets по ширине.
import type { Snippet } from 'svelte'
let { variants = [], fallback }: { variants?: Snippet[]; fallback?: Snippet } = $props()
let box: HTMLElement | null = $state(null)
let fit = $state(0)
let w = $state(0)
function measure() { if (!box) return; w = box.clientWidth; fit = 0 }
$effect(() => { if (!box) return; const ro = new ResizeObserver(measure); ro.observe(box); return () => ro.disconnect() })
</script>
<div class="vtf"><div class="live" bind:this={box}>{#if variants[fit]}{@render variants[fit]()}{:else if fallback}{@render fallback()}{/if}</div><div class="hidden">{#each variants as v, i}<span data-i={i}>{@render v()}</span>{/each}</div></div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.vtf { background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); min-width: 0; }
.live { min-width: 0; overflow: hidden; }
.hidden { position: absolute; visibility: hidden; pointer-events: none; }
@media (prefers-reduced-motion: reduce) { .vtf { transition: none; } }
</style>
