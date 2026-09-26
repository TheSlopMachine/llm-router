<script lang="ts">
  import Chip from '../controls/Chip.svelte'
  import Icon from '../controls/Icon.svelte'
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
  <HStack gap={2} align="center">
    {#if chipsDirection === 'vertical'}
      <VStack gap={2} align="center" wrap>
        {#each input as mod}
          <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
        {/each}
      </VStack>
      <Icon name="arrow_forward" size="base" tone="disabled" class="mods-arrow" />
      <VStack gap={2} align="center" wrap>
        {#each output as mod}
          <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
        {/each}
      </VStack>
    {:else}
      {#each input as mod}
        <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
      {/each}
      <Icon name="arrow_forward" size="base" tone="disabled" class="mods-arrow" />
      {#each output as mod}
        <Chip icon={modalityIcon(mod)} text="" color={modalityColor(mod)} title={mod} {size} />
      {/each}
    {/if}
  </HStack>
{/if}

<style>
  /* Global: the class rides the Icon root in another component, Svelte
     drops component-passed classes from scoped CSS as unused. */
  :global(.mods-arrow) {
    flex: none;
  }
</style>
