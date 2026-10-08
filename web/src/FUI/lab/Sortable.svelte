<script lang="ts">
// Sortable: Pointer Events reorder без либ. Пропсы: items (bind), row snippet.
import type { Snippet } from 'svelte'
let { items = $bindable([]), row }: { items?: string[]; row?: Snippet<[string, number]> } = $props()
let drag = $state(-1)
function down(i: number, e: PointerEvent) { drag = i; (e.target as HTMLElement).setPointerCapture?.(e.pointerId) }
function over(i: number) { if (drag < 0 || drag === i) return; const next = [...items]; const [m] = next.splice(drag, 1); next.splice(i, 0, m); items = next; drag = i }
</script>
<ul class="sort">{#each items as it, i (it)}<li class:grip={drag===i} onpointerdown={(e) => down(i, e)} onpointerover={() => over(i)}>{#if row}{@render row(it, i)}{:else}{it}{/if}</li>{/each}</ul>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.sort { list-style: none; display: flex; flex-direction: column; gap: var(--fui-space-2); background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.sort li { cursor: grab; background: var(--fui-color-surface-container-high); border-radius: var(--fui-radius-xs); padding: var(--fui-space-2) var(--fui-space-3); font-size: var(--fui-text-base); color: var(--fui-color-text); touch-action: none; }
.sort li.grip { background: var(--fui-color-accent-soft); }
.sort li:focus-visible { box-shadow: var(--fui-focus-ring); }
</style>
