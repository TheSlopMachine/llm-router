<script lang="ts">
// ScrollArea: тонкие скроллбары заливкой. Пропсы: maxHeight, autoHide, children.
import type { Snippet } from 'svelte'
let { maxHeight = 240, autoHide = true, children }: { maxHeight?: number; autoHide?: boolean; children?: Snippet } = $props()
</script>
<div class="sa" class:autohide={autoHide} style:max-height={`${maxHeight}px`}>{#if children}{@render children()}{/if}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.sa { overflow: auto; background: var(--fui-elev); border-radius: var(--fui-radius-sm); padding: var(--fui-space-3); scrollbar-width: thin; scrollbar-color: var(--fui-color-switch-off) transparent; }
.sa::-webkit-scrollbar { width: var(--fui-space-3); height: var(--fui-space-3); }
.sa::-webkit-scrollbar-thumb { background: var(--fui-color-switch-off); border-radius: var(--fui-radius-xs); }
.autohide:not(:hover)::-webkit-scrollbar-thumb { background: transparent; }
.autohide:not(:hover) { scrollbar-color: transparent transparent; }
</style>
