<script lang="ts">
// Avatar + AvatarGroup: initials by name hash. Props: src, name, size, shape; Group: avatars[], max, size.
  import { squircle } from '../core/squircle';
  let { src = '', name = '', size = 32, shape = 'circle', group, max = 4, groupSize = 32 } = $props<{ src?: string; name?: string; size?: number; shape?: 'circle' | 'squircle'; group?: GroupAvatar[]; max?: number; groupSize?: number }>();
  export interface GroupAvatar { src?: string; name: string; }
  const initials = $derived((name ?? '').split(/\s+/).map((w: string) => w[0]).slice(0, 2).join('').toUpperCase());
  let hash = $derived([...(name ?? '')].reduce((a: number, c: string) => (a * 31 + c.charCodeAt(0)) | 0, 0));
  const HUES = ['--fui-color-badge-blue-bg', '--fui-color-badge-green-bg', '--fui-color-badge-purple-bg', '--fui-color-badge-teal-bg', '--fui-color-badge-orange-bg'];
  const bg = $derived(`var(${HUES[Math.abs(hash) % HUES.length]})`);
  const isGroup = $derived(!!group);
  const vis = $derived((group ?? []).slice(0, max));
  const rest = $derived((group ?? []).length - vis.length);
</script>
{#if isGroup}
  <span class="grp" role="group">
    {#each vis as a, i (a.name + i)}
      <span class="av circle" style:width={groupSize + 'px'} style:height={groupSize + 'px'} style:background={a.src ? 'var(--fui-elev)' : bg} style:z-index={vis.length - i} style:font-size="var(--fui-text-xs)">
        {#if a.src}<img src={a.src} alt={a.name} />{:else}{a.name.split(/\s+/).map((w: string) => w[0]).slice(0, 2).join('').toUpperCase()}{/if}
      </span>
    {/each}
    {#if rest > 0}<span class="av circle more" style:width={groupSize + 'px'} style:height={groupSize + 'px'}>+{rest}</span>{/if}
  </span>
{:else}
  <span class="av" class:circle={shape === 'circle'} use:squircle={8} style:width={size + 'px'} style:height={size + 'px'} style:background={src ? 'var(--fui-elev)' : bg} role="img" aria-label={name}>
    {#if src}<img src={src} alt={name} />{:else}<span>{initials}</span>{/if}
  </span>
{/if}
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .av { display: inline-flex; align-items: center; justify-content: center; overflow: hidden; border-radius: var(--fui-radius-md); font-size: var(--fui-text-sm); font-weight: var(--fui-weight-medium); color: var(--fui-color-text); flex-shrink: 0; }
  .av.circle { border-radius: 9999px; }
  .av img { width: 100%; height: 100%; object-fit: cover; }
  .grp { display: inline-flex; align-items: center; }
  .grp .av + .av { margin-left: calc(var(--fui-space-3) * -1); }
  .more { background: var(--fui-color-surface-container-high); color: var(--fui-color-text-soft); font-size: var(--fui-text-xs); }
</style>
