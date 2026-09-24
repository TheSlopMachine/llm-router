<script lang="ts">
  import Chip from '../controls/Chip.svelte'
  import HStack from '../layout/HStack.svelte'
  import VStack from '../layout/VStack.svelte'
  import { modalityColor, modalityIcon } from '../../../lib/modalities'

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
  <HStack gap={2} align="center" class="mods-flow">
    {#if chipsDirection === 'vertical'}
      <VStack gap={2} align="center" class="mods-group">
        {#each input as mod}
          <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
        {/each}
      </VStack>
      <span class="icon mods-arrow" aria-hidden="true">arrow_forward</span>
      <VStack gap={2} align="center" class="mods-group">
        {#each output as mod}
          <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
        {/each}
      </VStack>
    {:else}
      {#each input as mod}
        <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
      {/each}
      <span class="icon mods-arrow" aria-hidden="true">arrow_forward</span>
      {#each output as mod}
        <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
      {/each}
    {/if}
  </HStack>
{/if}

<style>
  /* :global — classes ride HStack/VStack roots in another component. */
  :global(.mods-group) {
    flex-wrap: wrap;
  }

  .mods-arrow {
    color: var(--color-text-disabled);
    font-size: var(--text-base);
    flex: none;
  }
</style>
