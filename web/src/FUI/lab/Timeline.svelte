<script lang="ts">
// Timeline: vertical event feed. Props: items[{time,title,body?,icon?,tone?}], align.
  import Icon from '../controls/Icon.svelte';
  interface Item { time: string; title: string; body?: string; icon?: string; tone?: 'blue'|'green'|'yellow'|'red'|'purple'|'teal'|'orange'; }
  let { items, align = 'left' } = $props<{ items: Item[]; align?: 'left' | 'alternate' }>();
</script>
<ol class="tl" class:alt={align === 'alternate'}>
  {#each items as it (it.time + it.title)}
    <li class="ev">
      <span class="rail" aria-hidden="true"><span class="dot" data-tone={it.tone ?? 'blue'}>{#if it.icon}<Icon name={it.icon} size="xs" />{/if}</span></span>
      <div class="card">
        <span class="time">{it.time}</span>
        <span class="title">{it.title}</span>
        {#if it.body}<span class="body">{it.body}</span>{/if}
      </div>
    </li>
  {/each}
</ol>
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .tl { list-style: none; display: flex; flex-direction: column; }
  .ev { display: flex; gap: var(--fui-space-3); }
  .rail { display: flex; flex-direction: column; align-items: center; }
  .dot { display: inline-flex; align-items: center; justify-content: center; width: var(--fui-text-lg); height: var(--fui-text-lg); border-radius: 9999px; }
  .dot[data-tone='blue'] { background: var(--fui-color-badge-blue-bg); color: var(--fui-color-badge-blue-text); }
  .dot[data-tone='green'] { background: var(--fui-color-badge-green-bg); color: var(--fui-color-badge-green-text); }
  .dot[data-tone='yellow'] { background: var(--fui-color-badge-yellow-bg); color: var(--fui-color-badge-yellow-text); }
  .dot[data-tone='red'] { background: var(--fui-color-badge-red-bg); color: var(--fui-color-badge-red-text); }
  .dot[data-tone='purple'] { background: var(--fui-color-badge-purple-bg); color: var(--fui-color-badge-purple-text); }
  .dot[data-tone='teal'] { background: var(--fui-color-badge-teal-bg); color: var(--fui-color-badge-teal-text); }
  .dot[data-tone='orange'] { background: var(--fui-color-badge-orange-bg); color: var(--fui-color-badge-orange-text); }
  .ev:not(:last-child) .rail::after { content: ''; width: 2px; flex: 1; background: var(--fui-elev); margin: var(--fui-space-1) 0; }
  .card { display: flex; flex-direction: column; gap: var(--fui-space-1); padding-bottom: var(--fui-space-5); min-width: 0; }
  .time { font-family: var(--fui-font-mono); font-size: var(--fui-text-xs); color: var(--fui-color-text-disabled); }
  .title { font-size: var(--fui-text-base); font-weight: var(--fui-weight-medium); color: var(--fui-color-text); }
  .body { font-size: var(--fui-text-sm); color: var(--fui-color-text-soft); }
  .alt .ev:nth-child(even) { flex-direction: row-reverse; text-align: right; }
  .alt .ev:nth-child(even) .card { align-items: flex-end; }
</style>
