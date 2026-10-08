<script lang="ts">
// AnimatedSwitcher: смена children по key. Пропсы: mode fade|slide|scale, child snippet.
import type { Snippet } from 'svelte'
let { mode = 'fade', child }: { mode?: 'fade'|'slide'|'scale'; child?: Snippet } = $props()
</script>
<div class="sw sw-{mode}">{#if child}{@render child()}{/if}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.sw { background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.sw > :global(*) { animation: fui-in 0.2s ease; }
.sw-slide > :global(*) { animation-name: fui-slide; }
.sw-scale > :global(*) { animation-name: fui-scale; }
@keyframes fui-in { from { opacity: 0; } to { opacity: 1; } }
@keyframes fui-slide { from { opacity: 0; transform: translateX(12px); } to { opacity: 1; } }
@keyframes fui-scale { from { opacity: 0; transform: scale(0.96); } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .sw > :global(*) { animation: none; } }
</style>
