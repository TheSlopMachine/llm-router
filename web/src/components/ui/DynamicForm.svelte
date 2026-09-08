<script lang="ts">
  import type { UINode } from '../../lib/types'
  import SecretInput from './SecretInput.svelte'
  import CodeBlock from './CodeBlock.svelte'

  let {
    nodes,
    values = $bindable({}),
    onSubmit,
    submitLabel = 'Submit',
    busy = false,
  } = $props<{
    nodes: UINode[]
    values?: Record<string, unknown>
    onSubmit: (action: string, values: Record<string, unknown>) => void
    submitLabel?: string
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

  function buttonAction(node: UINode): string {
    return node.form_action || 'submit'
  }

  function hasSubmitButton(list: UINode[]): boolean {
    return list.some(
      (n) =>
        n.type === 'button' ||
        ((n.type === 'group' || n.type === 'flow' || n.type === 'grid' || n.type === 'section') &&
          n.content ? hasSubmitButton(n.content) : false)
    )
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

  function handleButton(action: string): void {
    onSubmit(action, { ...values })
  }
</script>

{#snippet nodeList(list: UINode[])}
  {#each list as node}
    {#if node.type === 'text'}
      <p class="form-text">{node.text}</p>
    {:else if node.type === 'banner'}
      <div class="banner banner-{node.variant || 'info'}">{node.text}</div>
    {:else if node.type === 'input'}
      <div class="form-group">
        {#if node.label}<label for="dyn-{node.name}">{node.label}{#if node.required} *{/if}</label>{/if}
        <input
          id="dyn-{node.name}"
          type={node.input_type || 'text'}
          value={(values[node.name ?? ''] as string) ?? ''}
          oninput={(e) => setValue(node.name ?? '', (e.target as HTMLInputElement).value)}
          placeholder={node.placeholder ?? ''}
          required={node.required ?? false}
          autocomplete={node.input_type === 'password' ? 'new-password' : 'off'}
        />
      </div>
    {:else if node.type === 'select'}
      <div class="form-group">
        {#if node.label}<label for="dyn-{node.name}">{node.label}{#if node.required} *{/if}</label>{/if}
        <select
          id="dyn-{node.name}"
          value={selectValue(node)}
          onchange={(e) => setValue(node.name ?? '', (e.target as HTMLSelectElement).value)}
        >
          {#each node.options ?? [] as option}
            <option value={option}>{option}</option>
          {/each}
        </select>
      </div>
    {:else if node.type === 'checkbox'}
      <div class="form-group form-check">
        <input
          id="dyn-{node.name}"
          type="checkbox"
          checked={values[node.name ?? ''] === true}
          onchange={(e) => setValue(node.name ?? '', (e.target as HTMLInputElement).checked)}
        />
        {#if node.label}<label for="dyn-{node.name}">{node.label}</label>{/if}
      </div>
    {:else if node.type === 'link'}
      <p class="form-link"><a href={node.url} target="_blank" rel="noopener noreferrer">{node.text || node.url}</a></p>
    {:else if node.type === 'button'}
      <div class="form-actions">
        <button
          type="button"
          class="btn {buttonAction(node) === 'cancel' || buttonAction(node) === 'restart' ? 'btn-secondary' : 'btn-primary'}"
          disabled={busy}
          onclick={() => handleButton(buttonAction(node))}
        >
          {node.text || submitLabel}
        </button>
      </div>
    {:else if node.type === 'secret'}
      <SecretInput
        id="dyn-{node.name}"
        label={node.label ?? ''}
        required={node.required ?? false}
        placeholder={node.placeholder ?? ''}
        value={(values[node.name ?? ''] as string) ?? ''}
        disabled={busy}
        oninput={(v) => setValue(node.name ?? '', v)}
      />
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
        <div class="spacer-grow" aria-hidden="true"></div>
      {:else}
        <div class="spacer-fixed" style="height: {gapSize[node.size ?? 'md'] ?? '12px'}" aria-hidden="true"></div>
      {/if}
    {:else if node.type === 'divider'}
      <hr class="divider" />
    {:else if node.type === 'group'}
      <div class="form-group-nested">
        {@render nodeList(node.content ?? [])}
      </div>
    {/if}
  {/each}
{/snippet}

<div class="dynamic-form">
  {@render nodeList(nodes)}
  {#if !hasSubmitButton(nodes)}
    <div class="form-actions">
      <button type="button" class="btn btn-primary" disabled={busy} onclick={() => handleButton('submit')}>
        {submitLabel}
      </button>
    </div>
  {/if}
</div>

<style>
  .dynamic-form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .form-text {
    font-size: 14px;
    color: var(--color-text-soft);
    margin: 0;
  }
  .banner {
    padding: 10px 12px;
    border-radius: 8px;
    font-size: 13px;
  }
  .banner-info {
    background: var(--color-info-bg, #eef4ff);
    color: var(--color-text);
  }
  .banner-error {
    background: var(--color-danger-bg, #fdecec);
    color: var(--color-danger, #b42318);
  }
  .banner-success {
    background: var(--color-success-bg, #e9f7ef);
    color: var(--color-success, #1e7e34);
  }
  .form-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .form-group label {
    font-size: 13px;
    font-weight: 500;
  }
  .form-check {
    flex-direction: row;
    align-items: center;
    gap: 8px;
  }
  .form-link {
    margin: 0;
    font-size: 14px;
  }
  .form-actions {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }
  .form-group-nested {
    border-left: 2px solid var(--color-outline-soft);
    padding-left: 12px;
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
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
  }
  .section-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .section-subtitle {
    margin: -8px 0 0 0;
    font-size: 13px;
    color: var(--color-text-soft);
  }
  .spacer-grow {
    flex: 1 1 auto;
  }
  .divider {
    border: none;
    border-top: 1px solid var(--color-outline-light);
    margin: 4px 0;
  }
</style>
