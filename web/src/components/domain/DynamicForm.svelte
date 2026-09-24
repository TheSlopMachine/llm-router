<script lang="ts">
  import type { UINode } from '../../lib/types'
  import { squircle } from '../../lib/squircle'
  import CodeBlock from '../ui/composite/CodeBlock.svelte'
  import TextEdit from '../ui/controls/TextEdit.svelte'
  import Select from '../ui/controls/Select.svelte'
  import Switch from '../ui/controls/Switch.svelte'
  import Text from '../ui/controls/Text.svelte'
  import VStack from '../ui/layout/VStack.svelte'
  import HStack from '../ui/layout/HStack.svelte'
  import Spacer from '../ui/layout/Spacer.svelte'
  import Divider from '../ui/controls/Divider.svelte'

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

  function optionLabel(node: UINode, option: string): string {
    return node.option_labels?.[option] ?? option
  }

  const flexDirection: Record<string, string> = { horizontal: 'row', vertical: 'column' }
  const flexAlign: Record<string, string> = { start: 'flex-start', center: 'center', end: 'flex-end', stretch: 'stretch' }
  const flexJustify: Record<string, string> = { start: 'flex-start', center: 'center', end: 'flex-end', between: 'space-between' }
  const gapSize: Record<string, string> = { sm: '8px', md: '12px', lg: '16px' }

  function flowStyle(node: UINode): string {
    const parts = [
      `flex-direction: ${flexDirection[node.direction ?? 'vertical'] ?? 'column'}`,
      `gap: ${gapSize[node.gap ?? 'md'] ?? '12px'}`,
      `align-items: ${flexAlign[node.align ?? 'stretch'] ?? 'stretch'}`,
      `justify-content: ${flexJustify[node.justify ?? 'start'] ?? 'flex-start'}`,
    ]
    if (node.wrap ?? true) parts.push('flex-wrap: wrap')
    return parts.join('; ')
  }
</script>

<script lang="ts" module>
  import type { UINode as UINodeType } from '../../lib/types'

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
    if (node.variant === 'primary' || node.variant === 'secondary' || node.variant === 'danger') {
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
      <div class="banner banner-{node.variant || 'info'}" use:squircle={12}>{node.text}</div>
    {:else if node.type === 'input'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <TextEdit
          id="dyn-{node.name}"
          type={node.input_type === 'password' || node.input_type === 'secret' ? 'secret' : 'text'}
          value={(values[node.name ?? ''] as string) ?? ''}
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
      <p class="form-link"><a href={node.url} target="_blank" rel="noopener noreferrer">{node.text || node.url}</a></p>
    {:else if node.type === 'secret'}
      <VStack gap={1}>
        {#if node.label}
          <Text size="sm" weight="medium">{node.label}{#if node.required} *{/if}</Text>
        {/if}
        <TextEdit
          id="dyn-{node.name}"
          type="secret"
          value={(values[node.name ?? ''] as string) ?? ''}
          hint={node.placeholder ?? ''}
          required={node.required ?? false}
          disabled={busy}
          onchange={(v) => setValue(node.name ?? '', v)}
        />
      </VStack>
    {:else if node.type === 'code'}
      <CodeBlock text={node.text ?? ''} label={node.label ?? ''} />
    {:else if node.type === 'flow'}
      <div class="flow" style={flowStyle(node)}>
        {@render nodeList(node.content ?? [])}
      </div>
    {:else if node.type === 'grid'}
      <div
        class="grid"
        style="grid-template-columns: repeat({node.columns || 1}, 1fr); gap: {gapSize[node.gap ?? 'md'] ?? '12px'}"
      >
        {@render nodeList(node.content ?? [])}
      </div>
    {:else if node.type === 'section'}
      <section class="section">
        <h3 class="section-title">{node.title}</h3>
        {#if node.subtitle}<p class="section-subtitle">{node.subtitle}</p>{/if}
        {@render nodeList(node.content ?? [])}
      </section>
    {:else if node.type === 'spacer'}
      {#if node.grow ?? true}
        <Spacer />
      {:else}
        <div class="spacer-fixed" style="height: {gapSize[node.size ?? 'md'] ?? '12px'}" aria-hidden="true"></div>
      {/if}
    {:else if node.type === 'divider'}
      <Divider />
    {:else if node.type === 'group'}
      <div class="form-group-nested">
        {@render nodeList(node.content ?? [])}
      </div>
    {/if}
  {/each}
{/snippet}

<div class="dynamic-form">
  {@render nodeList(nodes)}
</div>

<style>
  .dynamic-form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .form-text {
    font-size: var(--text-base);
    color: var(--color-text-soft);
    margin: 0;
  }
  .banner {
    padding: var(--space-4) var(--space-5);
    border-radius: 12px;
    font-size: var(--text-sm);
  }
  .banner-info {
    background: var(--color-notification-info-bg);
    color: var(--color-notification-info-text);
  }
  .banner-error {
    background: var(--color-notification-error-bg);
    color: var(--color-notification-error-text);
  }
  .banner-success {
    background: var(--color-notification-success-bg);
    color: var(--color-notification-success-text);
  }

  .form-check {
    flex-direction: row;
    align-items: center;
    gap: var(--space-3);
  }
  .form-link {
    margin: 0;
    font-size: var(--text-base);
  }
  .form-group-nested {
    border-left: 2px solid var(--color-outline-soft);
    padding-left: var(--space-4);
  }
  .flow {
    display: flex;
    width: 100%;
  }
  .grid {
    display: grid;
    width: 100%;
  }
  @media (max-width: 560px) {
    .grid {
      grid-template-columns: 1fr !important;
    }
  }
  .section {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-4);
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
  }

  .section-subtitle {
    margin: -8px 0 0 0;
    font-size: var(--text-sm);
    color: var(--color-text-soft);
  }
  .spacer-grow {
    flex: 1 1 auto;
  }

</style>
