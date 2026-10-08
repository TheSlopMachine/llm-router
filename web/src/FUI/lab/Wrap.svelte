<script lang="ts">
// Wrap: flex-wrap контейнер. Пропсы: gap, rowGap, align, justify, children.
import type { Snippet } from 'svelte'
import { squircle } from '../core/squircle'
let { gap = 3, rowGap, align = 'center', justify = 'start', children }: { gap?: number; rowGap?: number; align?: string; justify?: string; children?: Snippet } = $props()
const sp = (n: number) => `var(--fui-space-${Math.max(0, Math.min(8, n))})`
let rg = $derived(rowGap === undefined ? sp(gap) : sp(rowGap))
</script>
<div class="wrap" style:gap={sp(gap)} style:row-gap={rg} style:align-items={align} style:justify-content={justify} use:squircle>
{#if children}{@render children()}{/if}
</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.wrap { display: flex; flex-wrap: wrap; background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.wrap:focus-visible { box-shadow: var(--fui-focus-ring); }
@media (prefers-reduced-motion: reduce) { .wrap { transition: none; } }
</style>
