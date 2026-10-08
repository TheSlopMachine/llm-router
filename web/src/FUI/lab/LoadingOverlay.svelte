<script lang="ts">
// LoadingOverlay: заливка поверх children. Пропсы: active, label snippet, children.
import type { Snippet } from 'svelte'
let { active = false, label, children }: { active?: boolean; label?: Snippet; children?: Snippet } = $props()
</script>
<div class="lo">{#if children}{@render children()}{/if}{#if active}<div class="veil" aria-busy="true"><span class="spin"></span>{#if label}{@render label()}{/if}</div>{/if}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.lo { position: relative; background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.veil { position: absolute; inset: 0; display: flex; gap: var(--fui-space-3); align-items: center; justify-content: center; background: var(--fui-overlay-scrim); border-radius: var(--fui-radius-sm); color: var(--fui-color-text-on-button-reverse); font-size: var(--fui-text-sm); }
.spin { width: var(--fui-space-5); height: var(--fui-space-5); border-radius: 50%; background: var(--fui-color-accent); animation: fui-pulse 1s infinite ease-in-out alternate; }
@keyframes fui-pulse { from { opacity: 0.4; } to { opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
</style>
