<script lang="ts">
// VisuallyHidden: только для скринридера. Пропсы: text, children.
import type { Snippet } from 'svelte'
let { text = '', children }: { text?: string; children?: Snippet } = $props()
</script>
<span class="vh">{#if children}{@render children()}{:else}{text}{/if}</span>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.vh { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
