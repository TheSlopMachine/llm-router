<script lang="ts">
  import { onMount, tick } from 'svelte'
  import Text from '../controls/Text.svelte'

  // Stepped progress: circles with step titles, 2px rails between them.
  // States come from the current binding: done (check), current (number),
  // todo (number). Narrow widths collapse to first / current / last with …
  // markers. Needs at least 2 steps; fewer logs an error and renders as-is.
  let {
    steps,
    current = $bindable(1)
  } = $props<{
    steps: string[]
    current?: number
  }>()

  const last = $derived(steps.length - 1)
  const cur = $derived(Math.min(Math.max(current, 1), Math.max(steps.length, 1)) - 1)

  type Node = { kind: 'step'; index: number } | { kind: 'dots'; key: string }

  let root = $state<HTMLElement>()
  let compact = $state(false)
  let lastWidth = $state(0)

  function measure(): void {
    if (!root) return
    compact = root.scrollWidth > root.clientWidth + 1
  }

  async function remeasure(): Promise<void> {
    compact = false
    await tick()
    measure()
  }

  onMount(() => {
    void remeasure()
    if (!root) return
    const ro = new ResizeObserver(() => {
      if (!root || root.clientWidth === lastWidth) return
      lastWidth = root.clientWidth
      void remeasure()
    })
    ro.observe(root)
    return () => ro.disconnect()
  })

  $effect(() => {
    if (steps.length < 2) {
      console.error(`StepsView requires at least 2 steps, received ${steps.length}.`)
    }
    void current
    void remeasure()
  })

  const nodes = $derived.by((): Node[] => {
    if (!compact || steps.length <= 3) {
      return steps.map((_: string, index: number): Node => ({ kind: 'step', index }))
    }
    const picked = [...new Set([0, cur, last])].sort((a, b) => a - b)
    const out: Node[] = []
    picked.forEach((index, k) => {
      if (k > 0 && index - picked[k - 1] > 1) {
        out.push({ kind: 'dots', key: `dots-${picked[k - 1]}-${index}` })
      }
      out.push({ kind: 'step', index })
    })
    return out
  })

  // A rail is done when everything up to its right end is at or behind the
  // current step.
  function rightIndex(nodes: Node[], k: number): number {
    const node = nodes[k]
    if (node.kind === 'step') return node.index
    const next = nodes[k + 1]
    return next && next.kind === 'step' ? next.index : last
  }
</script>

<div class="stepper" role="list" bind:this={root}>
  {#each nodes as node, k (node.kind === 'step' ? `step-${node.index}` : node.key)}
    {#if k > 0}
      <span class="step-rail" class:done={rightIndex(nodes, k) <= cur} aria-hidden="true"></span>
    {/if}
    {#if node.kind === 'step'}
      {@const state = node.index < cur ? 'done' : node.index === cur ? 'current' : 'todo'}
      <div class="step-cell" role="listitem" aria-current={state === 'current' ? 'step' : undefined}>
        <span class="step-circle" class:done={state === 'done'} class:current={state === 'current'}>
          {#if state === 'done'}
            <span class="icon step-check" aria-hidden="true">check</span>
          {:else}
            <Text size={state === 'current' ? 'md' : 'sm'} weight="medium">{node.index + 1}</Text>
          {/if}
        </span>
        <div class="step-title">
          <Text size="xs" tone={state === 'current' ? 'default' : 'soft'} weight={state === 'current' ? 'medium' : 'normal'}>
            {steps[node.index]}
          </Text>
        </div>
      </div>
    {:else}
      <span class="step-dots" aria-hidden="true"><Text tone="soft">…</Text></span>
    {/if}
  {/each}
</div>

<style>
  .stepper {
    display: flex;
    align-items: flex-start;
    width: 100%;
    /* At least half the screen, never wider than the parent. */
    min-width: min(50vw, 100%);
    overflow: hidden;
  }
  /* Cells hug the circle (28px) while rails take all free space, so each
     rail runs exactly from one circle edge to the next. Titles overflow
     their cell symmetrically below the rails, never overlapping them. */
  .step-cell {
    flex: none;
    width: 28px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-2);
  }
  .step-title {
    width: max-content;
    max-width: 120px;
    align-self: center;
    text-align: center;
  }
  /* Edge anchors: first title starts at its circle's left edge, last title
     ends at its circle's right edge. */
  .step-cell:first-child .step-title {
    align-self: flex-start;
    text-align: left;
  }
  .step-cell:last-child .step-title {
    align-self: flex-end;
    text-align: right;
  }
  .step-circle {
    width: 28px;
    height: 28px;
    flex: none;
    display: grid;
    place-content: center;
    border-radius: 50%;
    /* Lifted elev: upcoming steps read closer to the rails between them. */
    background: color-mix(in srgb, var(--color-text) 20%, var(--elev));
    color: var(--color-text);
  }
  .step-circle.done,
  .step-circle.current {
    background: var(--color-text);
    color: var(--color-surface);
  }
  /* Text paints its own tone color; on the filled circles the glyph and the
     number read in the surface color instead. */
  .step-circle.done > :global(.txt-default),
  .step-circle.current > :global(.txt-default) {
    color: var(--color-surface);
  }
  /* Text numbers ride a tall line box (1.5/1.3); collapse it so the glyph
     itself centers, then nudge up: Raleway digits sit below cap height. */
  .step-circle > :global(.txt-sm),
  .step-circle > :global(.txt-md) {
    line-height: 1;
    display: inline-block;
    transform: translateY(-0.12em);
  }
  .step-check {
    font-size: var(--text-md);
  }
  .step-title {
    min-width: 0;
    text-align: center;
  }
  /* Rails ride at circle-center height: (28 - 2) / 2. */
  .step-rail {
    flex: 1 1 0;
    min-width: 8px;
    height: 2px;
    margin-top: 13px;
    border-radius: 2px;
    background: var(--color-text-disabled);
  }
  .step-rail.done {
    background: var(--color-text);
  }
  .step-dots {
    flex: none;
    height: 28px;
    display: grid;
    place-content: center;
  }
</style>
