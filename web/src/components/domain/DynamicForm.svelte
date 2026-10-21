<script lang="ts">
  import { Link, Banner, CodeBlock, GroupBox, Grid, TextField, Toggle, Text, VStack, HStack, Spacer, Divider, type HAlign, type VAlign, type Step, Picker } from '@fuckyouryan/fui'
  import { isStepValue, isBannerVariant, isSecretInput } from '$lib/ui-helpers'
  import type { UINode } from '../../lib/types'

  let {
    nodes,
    values = $bindable({}),
    busy = false,
  }: {
    nodes: UINode[]
    values?: Record<string, unknown>
    busy?: boolean
  } = $props()

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

  // The plugin contract names the failure style `error`; the widget tint is `danger`.
  function bannerTint(node: UINode): 'info' | 'warning' | 'danger' | 'success' {
    if (node.variant && isBannerVariant(node.variant)) return node.variant === 'error' ? 'danger' : node.variant
    return 'info'
  }
</script>

<script lang="ts" module>
  import type { UINode as UINodeType } from '../../lib/types'
  import { isButtonVariant } from '$lib/ui-helpers'

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

  // Plugin variants (primary, secondary, danger) map to a button style and an optional role.
  export function buttonAppearance(node: UINodeType): { buttonStyle: 'prominent' | 'neutral'; role?: 'destructive' } {
    if (node.variant && isButtonVariant(node.variant)) {
      if (node.variant === 'danger') return { buttonStyle: 'prominent', role: 'destructive' }
      return { buttonStyle: node.variant === 'primary' ? 'prominent' : 'neutral' }
    }
    const action = node.form_action || 'submit'
    return { buttonStyle: action === 'cancel' || action === 'restart' ? 'neutral' : 'prominent' }
  }
</script>

{#snippet nodeList(list: UINode[])}
  {#each list as node}
    {#if node.type === 'text'}
      <Text foregroundStyle="secondary">{node.text}</Text>
    {:else if node.type === 'banner'}
      <Banner tint={bannerTint(node)} text={node.text ?? ''} />
    {:else if node.type === 'input'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <TextField
          id="dyn-{node.name}"
          name="dyn-{node.name}"
          type={isSecretInput(node.input_type) ? 'secret' : 'text'}
          autocomplete={isSecretInput(node.input_type) ? 'off' : undefined}
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
        <Picker
          value={selectValue(node)}
          options={(node.options ?? []).map((opt) => ({ value: opt, label: optionLabel(node, opt) }))}
          disabled={busy}
          onchange={(v) => setValue(node.name ?? '', v)}
        />
      </VStack>
    {:else if node.type === 'checkbox'}
      <Toggle
        checked={values[node.name ?? ''] === true}
        disabled={busy}
        label={node.label}
        onchange={(v) => setValue(node.name ?? '', v)}
      />
    {:else if node.type === 'link'}
      <Text size="md"><Link href={node.url ?? ''} external text={node.text || node.url} /></Text>
    {:else if node.type === 'secret'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <TextField
          id="dyn-{node.name}"
          name="dyn-{node.name}"
          secure
          autocomplete="off"
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
          valign={(node.align ?? 'stretch') as VAlign}
          halign={(node.justify ?? 'start') as HAlign}
          wrap={node.wrap ?? true}
        >
          {@render nodeList(node.content ?? [])}
        </HStack>
      {:else}
        <VStack
          gap={toStep(node.gap, 4)}
          halign={(node.align ?? 'stretch') as HAlign}
          valign={(node.justify ?? 'start') as VAlign}
          wrap={node.wrap ?? true}
        >
          {@render nodeList(node.content ?? [])}
        </VStack>
      {/if}
    {:else if node.type === 'grid'}
      <Grid min="md" gap={toStep(node.gap, 4)} class="dyn-grid">
        {@render nodeList(node.content ?? [])}
      </Grid>
    {:else if node.type === 'section'}
      <GroupBox title={node.title ?? ''} description={node.subtitle}>
        {@render nodeList(node.content ?? [])}
      </GroupBox>
    {:else if node.type === 'spacer'}
      {#if node.grow ?? true}
        <Spacer />
      {:else}
        <Spacer size={toStep(node.size, 4)} axis="y" />
      {/if}
    {:else if node.type === 'divider'}
      <Divider />
    {:else if node.type === 'group'}
      <VStack elev pad={3} radius="md">
        {@render nodeList(node.content ?? [])}
      </VStack>
    {/if}
  {/each}
{/snippet}

<VStack gap={4}>
  {@render nodeList(nodes)}
</VStack>

<style>
  :global(.dyn-grid) {
    width: 100%;
  }
</style>
