<script lang="ts">
  import Switch from '../components/ui/controls/Switch.svelte'
  import Button from '../components/ui/controls/Button.svelte'
  import Picker from '../components/ui/controls/Picker.svelte'
  import SearchField from '../components/ui/controls/SearchField.svelte'
  import TextEdit from '../components/ui/controls/TextEdit.svelte'
  import TextArea from '../components/ui/controls/TextArea.svelte'
  import SectionCard from '../components/ui/composite/SectionCard.svelte'
  import CodeBlock from '../components/ui/composite/CodeBlock.svelte'
  import DynamicForm from '../components/domain/DynamicForm.svelte'
  import Select from '../components/ui/controls/Select.svelte'
  import FloatingList from '../components/ui/controls/FloatingList.svelte'
  import Chip from '../components/ui/controls/Chip.svelte'
  import { VStack, Text } from '$ui'
  import Table from '../components/ui/composite/Table.svelte'
  import type { TableColumn, TableSortDir } from '../components/ui/composite/Table.svelte'
  import ModelsTable from '../components/ui/composite/ModelsTable.svelte'
  import type { ModelsTableModel } from '../components/ui/composite/ModelsTable.svelte'
  import { modal } from '../lib/modal.svelte'
  import { toast } from '../lib/toast.svelte'
  import { theme } from '../lib/theme.svelte'
  import { squircle } from '../lib/squircle'
  import { tintSoft } from '../lib/tint'
  import type { UINode } from '../lib/types'

  // Buttons
  let btnStyle = $state('none')
  let btnSize = $state('medium')
  let btnText = $state('Demo button')
  let btnIcon = $state('add')
  let btnIconPlace = $state('left')
  let btnDisabled = $state(false)
  let btnTint = $state('')

  // TextEdit
  let teValue = $state('')
  let teType = $state('text')
  let teHint = $state('Type here…')
  let teLeading = $state('')
  let teDisabled = $state(false)
  let teRegex = $state('')
  let teValid = $state(true)
  let teExtraTrailing = $state(false)

  // TextArea
  let taValue = $state('')
  let taMin = $state('2')
  let taMax = $state('6')
  let taDisabled = $state(false)

  // SearchField
  let searchVal = $state('')
  let searchPlaceholder = $state('Search the polygon')
  let searchDisabled = $state(false)

  // Select
  let selVal = $state('two')
  let selSize = $state('medium')
  let selSearchable = $state(false)
  let selDisabled = $state(false)
  let selAutoWidth = $state(false)
  let selPlaceholder = $state('Pick one…')

  // Switch
  let swChecked = $state(true)
  let swLabel = $state('Demo switch')
  let swSize = $state('md')
  let swDisabled = $state(false)

  // Chips
  let chipText = $state('Demo chip')
  let chipIcon = $state('star')
  let chipColor = $state('chip-blue')
  let chipSize = $state('medium')

  let seg = $state('b')
  let segSize = $state('medium')
  let lastAction = $state('(none)')
  let demoMenuOpen = $state(false)
  let demoMenuAnchor = $state<HTMLElement>()
  let confirmResult = $state('(not asked)')
  let toastText = $state('Custom toast text — edit me and show')

  // CodeBlock
  let cbLabel = $state('Live values')
  let cbText = $state('{"model": "demo"}')

  interface DemoRow { name: string; ctx: string; n: number; off?: boolean }
  const demoRows: DemoRow[] = [
    { name: 'Gemini 2.5 Flash', ctx: '1049k ctx', n: 23 },
    { name: 'A very long model name that must wrap inside its cell', ctx: '8k ctx', n: 3, off: true },
    { name: '', ctx: '', n: 0 },
  ]
  const tblCols: TableColumn[] = [
    { key: 'name', title: 'Model', width: '40%', sortable: true },
    { key: 'ctx', title: 'Context', width: '120px', align: 'center' },
    { key: 'n', title: 'Count', align: 'right', sortable: true },
  ]
  let sortRows = $state<DemoRow[]>([...demoRows])
  let sortKey = $state<string | null>(null)
  let sortDir = $state<TableSortDir | null>(null)
  function demoSort(key: string): void {
    if (sortKey !== key) {
      sortKey = key
      sortDir = 'asc'
    } else if (sortDir === 'asc') {
      sortDir = 'desc'
    } else {
      sortKey = null
      sortDir = null
    }
    if (sortKey == null || sortDir == null) {
      sortRows = [...demoRows]
      return
    }
    const dir = sortDir === 'asc' ? 1 : -1
    sortRows = [...demoRows].sort((a, b) => {
      const av = a[sortKey as keyof DemoRow] ?? ''
      const bv = b[sortKey as keyof DemoRow] ?? ''
      if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * dir
      return String(av).localeCompare(String(bv)) * dir
    })
  }
  let dragRows = $state<DemoRow[]>([...demoRows])
  function demoReorder(from: number, to: number): void {
    const next = [...dragRows]
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    dragRows = next
  }

  let tblMode = $state('none')
  let tblLoading = $state(false)
  let tblEmpty = $state(false)
  const tblRows = $derived(tblEmpty ? [] : tblMode === 'draggable' ? dragRows : sortRows)

  // ModelsTable demo
  interface MtRow { id: string; name: string; ctx?: number; disabled: boolean }
  let mtRows = $state<MtRow[]>([
    { id: 'flash', name: 'Flash', ctx: 1049000, disabled: false },
    { id: 'pro', name: 'Pro', ctx: 2000000, disabled: true },
  ])
  let mtSortable = $state(false)
  let mtLoading = $state(false)
  const mtModels = $derived<ModelsTableModel[]>(mtRows.map((r) => ({
    kind: 'model',
    id: r.id,
    fullId: `demo/${r.id}`,
    name: r.name,
    contextWindow: r.ctx,
    inputModalities: ['text'],
    outputModalities: ['text'],
    capabilities: ['tools'],
    disabled: r.disabled,
    source: r,
  })))

  let formValues = $state<Record<string, unknown>>({})

  let themePick = $state(theme.value === 'light' ? 'light' : 'dark')

  // Tinted chips: soft fill + HSL-adjusted text (the chosen strategy, "A").
  let tintedChips = $derived.by(() => {
    void theme.value
    const dark = document.documentElement.classList.contains('dark')
    return ['#ff2d55', '#7c3aed', '#b8860b'].map((c) => ({ c, ...tintSoft(c, dark) }))
  })

  const formNodes: UINode[] = [
    { type: 'banner', variant: 'info', text: 'Info banner.' },
    { type: 'banner', variant: 'error', text: 'Error banner.' },
    { type: 'banner', variant: 'success', text: 'Success banner.' },
    { type: 'text', text: 'Plain text node.' },
    { type: 'input', name: 'api_host', label: 'Host', placeholder: 'example.com' },
    { type: 'secret', name: 'api_key', label: 'API key' },
    { type: 'select', name: 'region', label: 'Region', options: ['us', 'eu', 'asia'] },
    { type: 'checkbox', name: 'verbose', label: 'Verbose logging' },
    { type: 'code', text: '{"model": "demo"}' },
    { type: 'link', text: 'Docs', url: 'https://example.com' },
    { type: 'divider' },
    { type: 'section', title: 'Nested section', content: [
      { type: 'input', name: 'nested', label: 'Nested input' },
    ] },
    { type: 'group', content: [
      { type: 'input', name: 'g1', label: 'Grouped 1' },
      { type: 'input', name: 'g2', label: 'Grouped 2' },
    ] },
    { type: 'flow', direction: 'horizontal', content: [
      { type: 'input', name: 'f1', label: 'Flow 1' },
      { type: 'input', name: 'f2', label: 'Flow 2' },
    ] },
    { type: 'grid', columns: 2, content: [
      { type: 'input', name: 'c1', label: 'Cell 1' },
      { type: 'input', name: 'c2', label: 'Cell 2' },
    ] },
    { type: 'spacer' },
    { type: 'text', text: 'End of form.' },
  ]

  function askConfirm(): void {
    void modal.confirm({ title: 'Demo confirm', message: 'Confirm this demo action?', confirmRole: 'destructive' }).then((ok) => {
      confirmResult = ok ? 'confirmed' : 'cancelled'
    })
  }

  function askTitleOnly(): void {
    void modal.confirm({ title: 'Demo confirm', confirmRole: 'destructive' }).then((ok) => {
      confirmResult = ok ? 'confirmed' : 'cancelled'
    })
  }

  function openContentModal(): void {
    modal.open({
      title: 'Demo modal',
      subtitle: 'Content modal with buttons',
      size: 'medium',
      contentSnippet: () => {
        return (
          <VStack align="center" gap={4} style="padding: 20px;">
            <Text size="xl" icon={{ name: 'extension' }}>{t('No plugins installed')}</Text>
            <Text tone="soft" align="center">{t('Browse the catalog to install a provider plugin.')}</Text>
            <Button style="prominent" icon={{ name: 'download' }} onclick={() => toast.success('Browse catalog clicked')}>{t('Browse catalog')}</Button>
          </VStack>
        )
      },
      buttons: [{ label: 'Done', variant: 'primary', onClick: () => modal.close() }],
    })
  }
</script>

<div class="uit-stack">
<div class="page-header">
  <div>
    <h1>UI test polygon</h1>
    <p>Every reusable component in one place. Nothing here touches the backend.</p>
  </div>
  <Picker bind:value={themePick} ariaLabel="Theme" options={[
    { value: 'light', label: 'Light' },
    { value: 'dark', label: 'Dark' },
  ]} onchange={(v) => { theme.value = v === 'light' ? 'light' : 'dark' }} />
</div>

<SectionCard title="Buttons">
  <div class="row">
    <Picker bind:value={btnStyle} ariaLabel="Style" options={[
      { value: 'none', label: 'none' },
      { value: 'prominent', label: 'prominent' },
      { value: 'text', label: 'text' },
    ]} />
    <Picker bind:value={btnSize} ariaLabel="Size" options={[
      { value: 'small', label: 'small' },
      { value: 'medium', label: 'medium' },
      { value: 'large', label: 'large' },
    ]} />
    <Switch bind:checked={btnDisabled} label="Disabled" />
  </div>
  <div class="row">
    <TextEdit bind:value={btnText} hint="Button text" />
    <TextEdit bind:value={btnIcon} hint="Icon name (empty = none)" />
    <Picker bind:value={btnIconPlace} ariaLabel="Icon placement" options={[
      { value: 'left', label: 'icon left' },
      { value: 'right', label: 'icon right' },
    ]} />
    <TextEdit bind:value={btnTint} hint="Tint hex (empty = none)" />
  </div>
  <div class="row">
    <Button
      text={btnText}
      style={btnStyle as 'prominent' | 'none' | 'text'}
      size={btnSize as 'small' | 'medium' | 'large'}
      icon={btnIcon.trim() ? { name: btnIcon.trim(), placement: btnIconPlace as 'left' | 'right' } : undefined}
      tint={btnTint.trim() || undefined}
      disabled={btnDisabled}
      ariaLabel="Demo button"
      onclick={() => { lastAction = 'button' }}
    />
    <span class="hint">Action: <span class="mono">{lastAction}</span></span>
  </div>
</SectionCard>

<SectionCard title="TextEdit">
  <div class="row">
    <Picker bind:value={teType} ariaLabel="Type" options={[
      { value: 'text', label: 'text' },
      { value: 'secret', label: 'secret' },
    ]} />
    <TextEdit bind:value={teHint} hint="Placeholder" />
    <TextEdit bind:value={teLeading} hint="Leading icon (empty = none)" />
    <Switch bind:checked={teDisabled} label="Disabled" />
    <Switch bind:checked={teExtraTrailing} label="Extra trailing" />
  </div>
  <div class="row">
    <TextEdit bind:value={teRegex} hint="Regex (empty = off)" />
  </div>
  <TextEdit
    bind:value={teValue}
    type={teType as 'text' | 'secret'}
    hint={teHint}
    leadingIcon={teLeading.trim() || undefined}
    trailing={teExtraTrailing ? [{ icon: 'star', label: 'Extra', onclick: () => { lastAction = 'extra' } }] : []}
    regex={teRegex.trim() || undefined}
    disabled={teDisabled}
    onvalid={(v) => { teValid = v }}
  />
  <p class="hint">Value: <span class="mono">{teValue || '(empty)'}</span> · Valid: <span class="mono">{teValid ? 'yes' : 'no'}</span> · Action: <span class="mono">{lastAction}</span></p>
</SectionCard>

<SectionCard title="TextArea">
  <div class="row">
    <Picker bind:value={taMin} ariaLabel="Min rows" options={[
      { value: '1', label: 'min 1' },
      { value: '2', label: 'min 2' },
      { value: '4', label: 'min 4' },
    ]} />
    <Picker bind:value={taMax} ariaLabel="Max rows" options={[
      { value: '3', label: 'max 3' },
      { value: '6', label: 'max 6' },
      { value: '12', label: 'max 12' },
    ]} />
    <Switch bind:checked={taDisabled} label="Disabled" />
  </div>
  <TextArea bind:value={taValue} hint="Multiline…" minRows={Number(taMin)} maxRows={Number(taMax)} disabled={taDisabled} />
  <p class="hint">Length: <span class="mono">{taValue.length}</span></p>
</SectionCard>

<SectionCard title="SearchField">
  <div class="row">
    <TextEdit bind:value={searchPlaceholder} hint="Placeholder" />
    <Switch bind:checked={searchDisabled} label="Disabled" />
  </div>
  <SearchField bind:value={searchVal} placeholder={searchPlaceholder} disabled={searchDisabled} />
  <p class="hint">Value: <span class="mono">{searchVal || '(empty)'}</span></p>
</SectionCard>

<SectionCard title="Select">
  <div class="row">
    <Switch bind:checked={selSearchable} label="Searchable" />
    <Switch bind:checked={selDisabled} label="Disabled" />
    <Switch bind:checked={selAutoWidth} label="Auto width" />
    <Picker bind:value={selSize} ariaLabel="Size" options={[
      { value: 'small', label: 'small' },
      { value: 'medium', label: 'medium' },
      { value: 'large', label: 'large' },
    ]} />
    <TextEdit bind:value={selPlaceholder} hint="Placeholder" />
  </div>
  <div class="row">
    <Select
      bind:value={selVal}
      size={selSize as 'small' | 'medium' | 'large'}
      options={[
        { value: 'one', label: 'Option one' },
        { value: 'two', label: 'Option two' },
        { value: 'three', label: 'Option three' },
      ]}
      placeholder={selPlaceholder}
      searchable={selSearchable}
      disabled={selDisabled}
      autoWidth={selAutoWidth}
    />
  </div>
  <p class="hint">Value: <span class="mono">{selVal}</span></p>
</SectionCard>

<SectionCard title="Switch">
  <div class="row">
    <TextEdit bind:value={swLabel} hint="Label" />
    <Picker bind:value={swSize} ariaLabel="Size" options={[
      { value: 'md', label: 'md' },
      { value: 'xl', label: 'xl' },
    ]} />
    <Switch bind:checked={swDisabled} label="Disabled" />
  </div>
  <div class="row">
    <Switch bind:checked={swChecked} label={swLabel} size={swSize as 'md' | 'xl'} disabled={swDisabled} />
    <span class="hint">State: <span class="mono">{swChecked ? 'on' : 'off'}</span></span>
  </div>
  <div class="row">
    <Picker bind:value={seg} size={segSize as 'small' | 'medium' | 'large'} ariaLabel="Demo segments" options={[
      { value: 'a', label: 'Alpha' },
      { value: 'b', label: 'Beta' },
      { value: 'c', label: 'Gamma' },
    ]} />
    <Picker bind:value={segSize} ariaLabel="Picker size" options={[
      { value: 'small', label: 'small' },
      { value: 'medium', label: 'medium' },
      { value: 'large', label: 'large' },
    ]} />
    <span class="hint">Segment: <span class="mono">{seg}</span></span>
  </div>
</SectionCard>

<SectionCard title="Chips">
  <div class="row">
    <TextEdit bind:value={chipText} hint="Text (empty = icon only)" />
    <TextEdit bind:value={chipIcon} hint="Icon (empty = none)" />
    <Picker bind:value={chipColor} ariaLabel="Color" options={[
      { value: 'chip-blue', label: 'blue' },
      { value: 'chip-green', label: 'green' },
      { value: 'chip-yellow', label: 'yellow' },
      { value: 'chip-red', label: 'red' },
      { value: 'chip-purple', label: 'purple' },
      { value: 'chip-teal', label: 'teal' },
      { value: 'chip-orange', label: 'orange' },
      { value: 'chip-neutral', label: 'neutral' },
    ]} />
    <Picker bind:value={chipSize} ariaLabel="Size" options={[
      { value: 'small', label: 'small' },
      { value: 'medium', label: 'medium' },
      { value: 'large', label: 'large' },
    ]} />
  </div>
  <div class="row">
    <Chip
      icon={chipIcon.trim() || undefined}
      text={chipText}
      color={chipColor}
      size={chipSize as 'small' | 'medium' | 'large'}
    />
  </div>
  <div class="row">
    {#each tintedChips as tc}
      <span class="chip" style="background: {tc.bg}; color: {tc.text}">tint {tc.c}</span>
    {/each}
  </div>
</SectionCard>

<SectionCard title="FloatingList">
  <div class="row">
    <Button
      text="Demo actions"
      icon={{ name: 'expand_more', placement: 'right' }}
      onclick={(e) => {
        demoMenuAnchor = e.currentTarget as HTMLElement
        demoMenuOpen = !demoMenuOpen
      }}
    />
    <FloatingList
      bind:open={demoMenuOpen}
      anchor={demoMenuAnchor}
      label="Demo actions"
      actions={[
        { id: 'edit', label: 'Edit', icon: 'edit' },
        { id: 'delete', label: 'Delete', icon: 'delete', tint: '#dc2626' },
      ]}
      onaction={(id) => { lastAction = id }}
    />
    <span class="hint">Action: <span class="mono">{lastAction}</span></span>
  </div>
</SectionCard>

<SectionCard title="CodeBlock">
  <div class="row">
    <TextEdit bind:value={cbLabel} hint="Label" />
  </div>
  <div class="row">
    <TextEdit bind:value={cbText} hint="Code text" />
  </div>
  <CodeBlock label={cbLabel} text={cbText} />
</SectionCard>

<SectionCard title="Tables">
  <p class="hint">One table, every mode. Columns cover left / center / right alignment.</p>
  <div class="row">
    <Picker bind:value={tblMode} ariaLabel="Table mode" options={[
      { value: 'none', label: 'none' },
      { value: 'draggable', label: 'draggable' },
      { value: 'sortable', label: 'sortable' },
    ]} />
    <Switch bind:checked={tblLoading} label="Loading" />
    <Switch bind:checked={tblEmpty} label="Empty" />
  </div>
  <div class="demo-table" use:squircle={18}>
    <Table
      columns={tblCols}
      rows={tblRows}
      rowKey={(r) => r.name || '(empty)'}
      rowClass={(r) => (r.off ? 'row-off' : '')}
      sortKey={tblMode === 'sortable' ? sortKey : null}
      sortDir={tblMode === 'sortable' ? sortDir : null}
      onsort={demoSort}
      draggable={tblMode === 'draggable'}
      onReorder={demoReorder}
      loading={tblLoading}
    >
      {#snippet cell({ column, row })}
        {#if column.key === 'name'}{row.name || '—'}
        {:else if column.key === 'ctx'}{row.ctx || '—'}
        {:else}{row.n}{/if}
      {/snippet}
      {#snippet empty()}
        <div class="uit-empty-note">No rows — caller-provided empty content.</div>
      {/snippet}
    </Table>
  </div>
  <p class="hint">Sortable: click the header cell. Draggable: grip column spawns automatically, rows reorder.</p>
</SectionCard>

<SectionCard title="ModelsTable">
  <div class="row">
    <Switch bind:checked={mtSortable} label="Sortable" />
    <Switch bind:checked={mtLoading} label="Loading" />
  </div>
  <ModelsTable
    models={mtModels}
    sortable={mtSortable}
    loading={mtLoading}
  >
    {#snippet actions({ model })}
      {@const r = model.source as MtRow}
      <Switch
        checked={!r.disabled}
        ariaLabel="Enable model"
        onchange={(v) => {
          mtRows = mtRows.map((m) => (m.id === r.id ? { ...m, disabled: !v } : m))
        }}
      />
    {/snippet}
    {#snippet empty()}
      <div class="uit-empty-note">No models.</div>
    {/snippet}
  </ModelsTable>
</SectionCard>

<SectionCard title="Overlays">
  <div class="row">
    <div class="confirm-col">
      <Button text="Confirm modal" onclick={askConfirm} />
      <Button text="Title only" onclick={askTitleOnly} />
    </div>
    <Button text="Content modal" onclick={openContentModal} />
    <Button text="Success toast" onclick={() => toast.success('Demo success toast')} />
    <Button text="Error toast" onclick={() => toast.error('Demo error toast')} />
  </div>
  <TextArea bind:value={toastText} hint="Toast text" minRows={3} maxRows={6} />
  <div class="row">
    <Button text="Show toast" onclick={() => toast.success(toastText || '(empty)')} />
  </div>
  <p class="hint">Confirm result: <span class="mono">{confirmResult}</span></p>
</SectionCard>

<SectionCard title="Dynamic form">
  <DynamicForm nodes={formNodes} bind:values={formValues} />
  <CodeBlock label="Live values" text={JSON.stringify(formValues, null, 2)} />
</SectionCard>

</div>

<style>
  /* Composer owns spacing: widgets render marginless, the stack gaps them. */
  .uit-stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-6);
  }
  .uit-stack > .page-header {
    margin-bottom: var(--space-3);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
    align-items: center;
    margin-bottom: var(--space-5);
  }
  .uit-empty-note {
    color: var(--color-text-soft);
    font-size: var(--text-sm);
  }
  .row:last-child {
    margin-bottom: 0;
  }
  .demo-table {
    margin-bottom: var(--space-5);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .confirm-col {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    align-items: flex-start;
  }
</style>
