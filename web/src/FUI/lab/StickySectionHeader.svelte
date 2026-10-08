<script lang="ts">
// StickySectionHeader: sticky-заголовки групп. Пропсы: sections [{title, items}].
let { sections = [] }: { sections?: { title: string; items: string[] }[] } = $props()
</script>
<div class="sticky">{#each sections as s}<div class="h">{s.title}</div>{#each s.items as it}<div class="r">{it}</div>{/each}{/each}</div>
<style>
:where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
.sticky { background: var(--fui-elev); border-radius: var(--fui-radius-sm); max-height: 280px; overflow-y: auto; }
.h { position: sticky; top: 0; background: var(--fui-color-accent-soft); color: var(--fui-color-text); font-size: var(--fui-text-sm); font-weight: var(--fui-weight-medium); padding: var(--fui-space-2) var(--fui-space-3); }
.r { padding: var(--fui-space-2) var(--fui-space-3); font-size: var(--fui-text-base); color: var(--fui-color-text); }
</style>
