<script lang="ts">
  import Link from '../../FUI/controls/Link.svelte'
  import type { UINode } from '../../lib/types'
  import Banner from '../../FUI/composite/Banner.svelte'
  import CodeBlock from '../../FUI/composite/CodeBlock.svelte'
  import SectionCard from '../../FUI/composite/SectionCard.svelte'
  import Grid from '../../FUI/layout/Grid.svelte'
  import Box from '../../FUI/layout/Box.svelte'
  import TextEdit from '../../FUI/controls/TextEdit.svelte'
  import Select from '../../FUI/controls/Select.svelte'
  import Switch from '../../FUI/controls/Switch.svelte'
  import Text from '../../FUI/controls/Text.svelte'
  import VStack from '../../FUI/layout/VStack.svelte'
  import HStack from '../../FUI/layout/HStack.svelte'
  import Spacer from '../../FUI/layout/Spacer.svelte'
  import Divider from '../../FUI/controls/Divider.svelte'
  import type { Align, Justify, Step } from '../../FUI/tokens'
  import { isStepValue, isBannerVariant } from '../../FUI/core/ui-guards'
  import { isSecretInput } from '../../FUI/core/button-state'

  let {
    nodes,
    values = $bindable({}),
    busy = false,
  } = $props<{
    nodes: UINode[]
    values?: Record<string, unknown>
    busy?: boolean
  }>()

  function setValue(name: string, value: unknown): void {
    values = { ...values, [name]: value }
  }

  function selectValue(node: UINode): string {
    const current = values[node.name ?? '']
    if (typeof current === 'string' && current) return current
    if (typeof node.value === 'string' && node.value) return node.value
    return node.options?.[0] ?? ''
  }

  function fieldValue(node: UINode): string {
    const current = values[node.name ?? '']
    if (typeof current === 'string') return current
    if (typeof node.value === 'string') return node.value
    return ''
  }

  function optionLabel(node: UINode, option: string): string {
    return node.option_labels?.[option] ?? option
  }

  // Backend normalizes gap/size to Step int (legacy sm|md|lg accepted).
  const LEGACY_GAP: Record<string, Step> = { sm: 2, md: 4, lg: 6 }
  function toStep(v: unknown, fallback: Step): Step {
    if (isStepValue(v)) return v as Step
    if (typeof v === 'string' && v in LEGACY_GAP) return LEGACY_GAP[v]
    return fallback
  }

  function bannerVariant(node: UINode): 'info' | 'warning' | 'error' | 'success' {
    return node.variant && isBannerVariant(node.variant) ? node.variant : 'info'
  }
</script>

<script lang="ts" module>
  import type { UINode as UINodeType } from '../../lib/types'
  import { isButtonVariant } from '../../FUI/core/ui-guards'

  // Collects button nodes in tree order for footer rendering.
  // DynamicForm itself never renders buttons; hosts own the footer.
  export function collectButtons(list: UINodeType[]): UINodeType[] {
    const out: UINodeType[] = []
    const walk = (nodes: UINodeType[]): void => {
      for (const n of nodes) {
        if (n.type === 'button') out.push(n)
        if (n.content) walk(n.content)
      }
    }
    walk(list)
    return out
  }

  export function buttonVariant(node: UINodeType): 'primary' | 'secondary' | 'danger' {
    if (node.variant && isButtonVariant(node.variant)) {
      return node.variant
    }
    const action = node.form_action || 'submit'
    return action === 'cancel' || action === 'restart' ? 'secondary' : 'primary'
  }
</script>

{#snippet nodeList(list: UINode[])}
  {#each list as node}
    {#if node.type === 'text'}
      <Text tone="soft">{node.text}</Text>
    {:else if node.type === 'banner'}
      <Banner variant={bannerVariant(node)} text={node.text ?? ''} />
    {:else if node.type === 'input'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <TextEdit
          id="dyn-{node.name}"
          type={isSecretInput(node.input_type) ? 'secret' : 'text'}
          value={fieldValue(node)}
          hint={node.placeholder ?? ''}
          required={node.required ?? false}
          disabled={busy}
          onchange={(v) => setValue(node.name ?? '', v)}
        />
      </VStack>
    {:else if node.type === 'select'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <Select
          value={selectValue(node)}
          options={(node.options ?? []).map((opt) => ({ value: opt, label: optionLabel(node, opt) }))}
          disabled={busy}
          onchange={(v) => setValue(node.name ?? '', v)}
        />
      </VStack>
    {:else if node.type === 'checkbox'}
      <Switch
        checked={values[node.name ?? ''] === true}
        disabled={busy}
        label={node.label}
        onchange={(v) => setValue(node.name ?? '', v)}
      />
    {:else if node.type === 'link'}
      <Text size="base"><Link href={node.url ?? ''} external text={node.text || node.url} /></Text>
    {:else if node.type === 'secret'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <TextEdit
          id="dyn-{node.name}"
          type="secret"
          value={fieldValue(node)}
          hint={node.placeholder ?? ''}
          required={node.required ?? false}
          disabled={busy}
          onchange={(v) => setValue(node.name ?? '', v)}
        />
      </VStack>
    {:else if node.type === 'code'}
      <CodeBlock text={node.text ?? ''} label={node.label ?? ''} />
    {:else if node.type === 'flow'}
      {#if (node.direction ?? 'vertical') === 'horizontal'}
        <HStack
          gap={toStep(node.gap, 4)}
          align={(node.align ?? 'stretch') as Align}
          justify={(node.justify ?? 'start') as Justify}
          wrap={node.wrap ?? true}
        >
          {@render nodeList(node.content ?? [])}
        </HStack>
      {:else}
        <VStack
          gap={toStep(node.gap, 4)}
          align={(node.align ?? 'stretch') as Align}
          justify={(node.justify ?? 'start') as Justify}
          wrap={node.wrap ?? true}
        >
          {@render nodeList(node.content ?? [])}
        </VStack>
      {/if}
    {:else if node.type === 'grid'}
      <Grid cols={node.columns ?? 1} gap={toStep(node.gap, 4)} class="dyn-grid">
        {@render nodeList(node.content ?? [])}
      </Grid>
    {:else if node.type === 'section'}
      <SectionCard title={node.title ?? ''} description={node.subtitle}>
        {@render nodeList(node.content ?? [])}
      </SectionCard>
    {:else if node.type === 'spacer'}
      {#if node.grow ?? true}
        <Spacer />
      {:else}
        <Spacer size={toStep(node.size, 4)} axis="y" />
      {/if}
    {:else if node.type === 'divider'}
      <Divider />
    {:else if node.type === 'group'}
      <Box elev pad={3} radius="md">
        {@render nodeList(node.content ?? [])}
      </Box>
    {/if}
  {/each}
{/snippet}

<VStack gap={4}>
  {@render nodeList(nodes)}
</VStack>

<style>
  /* Grid has no responsive prop: narrow screens collapse plugin grids. */
  :global(.dyn-grid) {
    width: 100%;
  }
  @media (max-width: 560px) {
    :global(.dyn-grid) {
      grid-template-columns: 1fr !important;
    }
  }
</style>
