<script lang="ts">
// AspectRatio: держит ratio, children заполняют. Пропсы: ratio, fit, children.
import type { Snippet } from 'svelte'
let { ratio = 16/9, fit = 'cover', children }: { ratio?: number; fit?: 'cover'|'contain'; children?: Snippet } = $props()
</script>
<div class="ar" style:aspect-ratio={`${ratio}`} class:cover={fit==='cover'}>{#if children}{@render children()}{/if}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.ar { width: 100%; overflow: hidden; background: var(--fui-elev); border-radius: var(--fui-radius-sm); }
.ar > :global(*) { width: 100%; height: 100%; }
.cover > :global(*) { object-fit: cover; }
</style>
