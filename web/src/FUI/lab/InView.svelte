<script lang="ts">
// InView: показывает children при входе в viewport. Пропсы: once, threshold, animation.
import type { Snippet } from 'svelte'
let { once = true, threshold = 0.1, animation = 'fade-up', children }: { once?: boolean; threshold?: number; animation?: 'fade-up'|'none'; children?: Snippet } = $props()
let box: HTMLElement | null = $state(null)
let seen = $state(false)
$effect(() => { if (!box) return; const io = new IntersectionObserver((es) => { for (const e of es) if (e.isIntersecting) { seen = true; if (once) io.disconnect() } else if (!once) seen = false }, { threshold }); io.observe(box); return () => io.disconnect() })
</script>
<div class="iv" class:seen={seen} class:plain={animation==='none'} bind:this={box}>{#if children}{@render children()}{/if}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.iv { opacity: 0; transform: translateY(12px); transition: opacity 0.25s ease, transform 0.25s ease; background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); }
.iv.seen { opacity: 1; transform: none; }
.iv.plain { opacity: 1; transform: none; transition: none; }
@media (prefers-reduced-motion: reduce) { .iv { opacity: 1; transform: none; transition: none; } }
</style>
