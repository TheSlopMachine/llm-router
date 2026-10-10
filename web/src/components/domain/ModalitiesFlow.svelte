<script lang="ts">
  import Chip from '../../FUI/controls/Chip.svelte'
  import Icon from '../../FUI/controls/Icon.svelte'
  import HStack from '../../FUI/layout/HStack.svelte'
  import VStack from '../../FUI/layout/VStack.svelte'
  import { modalityColor, modalityIcon } from '../../lib/modalities'

  // In → out modality flow. The root always runs left to right;
  // chipsDirection only switches the chip groups between columns
  // (table style) and one flat row (editor style). Either both modality
  // lists carry at least one entry, or neither does.
  let {
    modalities,
    size = 'medium',
    chipsDirection = 'vertical'
  } = $props<{
    modalities: { input?: string[]; output?: string[] }
    size?: 'small' | 'medium' | 'large'
    chipsDirection?: 'horizontal' | 'vertical'
  }>()

  const input = $derived(modalities.input ?? [])
  const output = $derived(modalities.output ?? [])

  $effect(() => {
    const hasIn = input.length > 0
    const hasOut = output.length > 0
    if (hasIn !== hasOut) {
      console.error('ModalitiesFlow requires both input and output modalities, or neither.')
    }
  })
</script>

{#if input.length > 0 || output.length > 0}
  <HStack gap={2} align="center">
    {#if chipsDirection === 'vertical'}
      <div class="mods">
        {#each input as mod}
          <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
        {/each}
      </div>
      <span class="arrow"><Icon name="arrow_forward" size="base" tone="disabled" /></span>
      <div class="mods">
        {#each output as mod}
          <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
        {/each}
      </div>
    {:else}
      {#each input as mod}
        <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
      {/each}
      <span class="arrow"><Icon name="arrow_forward" size="base" tone="disabled" /></span>
      {#each output as mod}
        <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
      {/each}
    {/if}
  </HStack>
{/if}

<style>
  .arrow {
    display: inline-flex;
    flex: none;
  }
  /* at most two rows; more chips add columns */
  .mods {
    display: grid;
    grid-auto-flow: column;
    grid-template-rows: repeat(2, auto);
    gap: var(--fui-space-2);
    justify-items: center;
    align-items: center;
  }
</style>
