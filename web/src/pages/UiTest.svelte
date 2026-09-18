<script lang="ts">
  import Switch from '../components/ui/Switch.svelte'
  import Button from '../components/ui/Button.svelte'
  import SegmentedControl from '../components/ui/SegmentedControl.svelte'
  import SearchField from '../components/ui/SearchField.svelte'
  import SecretInput from '../components/ui/SecretInput.svelte'
  import SectionCard from '../components/ui/SectionCard.svelte'
  import CodeBlock from '../components/ui/CodeBlock.svelte'
  import DynamicForm from '../components/ui/DynamicForm.svelte'
  import Dropdown from '../components/Dropdown.svelte'
  import ActionDropdown from '../components/ActionDropdown.svelte'
  import EmptyState from '../components/EmptyState.svelte'
  import ProviderCard from '../components/ProviderCard.svelte'
  import PluginCard from '../components/plugins/PluginCard.svelte'
  import ModelCard from '../components/wizards/agents/ModelCard.svelte'
  import { modal } from '../lib/modal.svelte'
  import { toast } from '../lib/toast.svelte'
  import { theme } from '../lib/theme.svelte'
  import { squircle, setSquircleExponent } from '../lib/squircle'
  import { onMount } from 'svelte'
  import { tintSoft } from '../lib/tint'
  import type { UINode, Provider, ProviderStats, AgentModel, AvailableModel } from '../lib/types'

  let textVal = $state('')
  let secretVal = $state('')
  let searchVal = $state('')
  let checked = $state(false)
  let checkedOn = $state(true)
  let switchOn = $state(true)
  let switchOff = $state(false)
  let seg = $state('b')
  let dropVal = $state('two')
  let lastAction = $state('(none)')
  let confirmResult = $state('(not asked)')
  let toastText = $state('Custom toast text — edit me and show')

  // Font preview: chosen family applies app-wide while the polygon is open.
  $effect(() => {
    document.body.classList.add('uit-chrysanthemum')
    return () => document.body.classList.remove('uit-chrysanthemum')
  })

  let squircleN = $state('4')

  interface FontFile { url: string; weight: string; style: string }
  interface FontFamily { family: string; files: FontFile[] }
  let fontFamilies = $state<FontFamily[]>([])
  let fontPick = $state('Chrysanthemum')
  let fontStatus = $state('')

  onMount(async () => {
    try {
      const res = await fetch('/fonts/preview/manifest.json')
      if (!res.ok) throw new Error(`font manifest: HTTP ${res.status}`)
      fontFamilies = await res.json()
    } catch (e) {
      fontStatus = 'Font manifest failed to load'
      console.error(e)
    }
  })

  async function applyFont(family: string): Promise<void> {
    fontPick = family
    fontStatus = ''
    const entry = fontFamilies.find((f) => f.family === family)
    if (!entry) {
      document.body.style.setProperty('--uit-lab-font', "'Inter'")
      return
    }
    try {
      await Promise.all(entry.files.map(async (f) => {
        const face = new FontFace(family, `url('${f.url}')`, { weight: f.weight, style: f.style })
        await face.load()
        document.fonts.add(face)
      }))
      document.body.style.setProperty('--uit-lab-font', `'${family}'`)
    } catch (e) {
      fontStatus = `Failed to load ${family}`
      console.error(e)
    }
  }

  let formValues = $state<Record<string, unknown>>({})

  let padBtnH = $state('14px')
  let padBtnV = $state('6px')
  let padFieldH = $state('12px')
  let padFieldV = $state('10px')
  let ctlRadius = $state('14px')

  function px(v: string): string {
    const t = v.trim()
    return /^\d+(\.\d+)?$/.test(t) ? t + 'px' : t
  }

  function applyMetrics(): void {
    const root = document.documentElement
    root.style.setProperty('--btn-pad-h', px(padBtnH))
    root.style.setProperty('--btn-pad-v', px(padBtnV))
    root.style.setProperty('--field-pad-h', px(padFieldH))
    root.style.setProperty('--field-pad-v', px(padFieldV))
    root.style.setProperty('--ctl-radius', px(ctlRadius))
    // Radius-only changes never move a border box, so ResizeObserver misses
    // them; squircle re-reads computed radii on window resize.
    window.dispatchEvent(new Event('resize'))
  }

  function resetMetrics(): void {
    padBtnH = '14px'
    padBtnV = '6px'
    padFieldH = '12px'
    padFieldV = '10px'
    ctlRadius = '14px'
    const root = document.documentElement
    root.style.removeProperty('--btn-pad-h')
    root.style.removeProperty('--btn-pad-v')
    root.style.removeProperty('--field-pad-h')
    root.style.removeProperty('--field-pad-v')
    root.style.removeProperty('--ctl-radius')
    window.dispatchEvent(new Event('resize'))
  }

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

  const sampleProvider: Provider = {
    id: 'demo', name: 'Demo provider', type: 'Demo', type_key: 'demo',
    qualifier: '', config: {}, auth_type: 'key', base_url: 'https://example.com',
    icon_url: '', supports_auth_flow: false, is_ui_readonly: false,
    is_ui_hidden: false, disabled: false,
  }
  const sampleStats: ProviderStats = { model_count: 3, credential_count: 2 }

  let agentModel = $state<AgentModel>({ model_id: 'demo/model-a', priority: 1, description: 'Demo', instructions: '' })
  const availableModels: AvailableModel[] = [
    { full_model_id: 'demo/model-a', provider_id: 'demo', provider_name: 'Demo', provider_type: 'demo', model_name: 'model-a', display_name: 'Model A' },
    { full_model_id: 'demo/model-b', provider_id: 'demo', provider_name: 'Demo', provider_type: 'demo', model_name: 'model-b', display_name: 'Model B' },
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

  function askNoTitle(): void {
    void modal.confirm({ title: '', message: 'Confirm this demo action?', confirmRole: 'destructive' }).then((ok) => {
      confirmResult = ok ? 'confirmed' : 'cancelled'
    })
  }

  function openContentModal(): void {
    modal.open({
      title: 'Demo modal',
      subtitle: 'Content modal with buttons',
      size: 'medium',
      content: EmptyState,
      props: {
        icon: 'extension',
        message: 'Modal body component',
        hint: 'Any component renders here.',
        buttonText: 'Close',
        buttonIcon: 'close',
        onButtonClick: () => modal.close(),
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
  <SegmentedControl bind:value={themePick} ariaLabel="Theme" options={[
    { value: 'light', label: 'Light' },
    { value: 'dark', label: 'Dark' },
  ]} onchange={(v) => { theme.value = v === 'light' ? 'light' : 'dark' }} />
</div>

<SectionCard title="Playground">
  <p class="hint">Accent color lives in Settings now. Control metrics (px values, live):</p>
  <div class="metric-row">
    <label class="metric-field">btn pad-h<input type="text" value={padBtnH} oninput={(e) => { padBtnH = (e.target as HTMLInputElement).value; applyMetrics() }} /></label>
    <label class="metric-field">btn pad-v<input type="text" value={padBtnV} oninput={(e) => { padBtnV = (e.target as HTMLInputElement).value; applyMetrics() }} /></label>
    <label class="metric-field">field pad-h<input type="text" value={padFieldH} oninput={(e) => { padFieldH = (e.target as HTMLInputElement).value; applyMetrics() }} /></label>
    <label class="metric-field">field pad-v<input type="text" value={padFieldV} oninput={(e) => { padFieldV = (e.target as HTMLInputElement).value; applyMetrics() }} /></label>
    <label class="metric-field">radius<input type="text" value={ctlRadius} oninput={(e) => { ctlRadius = (e.target as HTMLInputElement).value; applyMetrics() }} /></label>
    <label class="metric-field">squircle n<input type="text" value={squircleN} oninput={(e) => { squircleN = (e.target as HTMLInputElement).value; setSquircleExponent(parseFloat(squircleN)) }} /></label>
    <label class="metric-field">font
      <select value={fontPick} onchange={(e) => applyFont((e.target as HTMLSelectElement).value)}>
        <option value="Inter">Inter (default)</option>
        {#each fontFamilies as f}
          <option value={f.family}>{f.family}</option>
        {/each}
      </select>
    </label>
    <button class="btn btn-secondary btn-sm" onclick={resetMetrics} use:squircle={8}>Reset</button>
  </div>
  {#if fontStatus}<p class="hint">{fontStatus}</p>{/if}
</SectionCard>

<SectionCard title="Buttons (one Button widget)">
  <div class="row">
    <Button text="Prominent" style="prominent" onclick={() => { lastAction = 'prominent' }} />
    <Button text="Plain (none)" onclick={() => { lastAction = 'plain' }} />
    <Button text="Text" style="text" onclick={() => { lastAction = 'text' }} />
    <Button text="Disabled" disabled />
  </div>
  <div class="row">
    <Button text="Tinted" tint="#7c3aed" />
    <Button text="Tinted prominent" style="prominent" tint="#e11d48" />
    <Button text="Tinted text" style="text" tint="#0d9488" />
    <Button text="Danger via tint" tint="#dc2626" />
  </div>
  <div class="row">
    <Button text="Small" size="sm" />
    <Button text="Large" size="lg" />
    <Button text="Icon left" icon={{ name: 'add', placement: 'left' }} />
    <Button text="Icon right" icon={{ name: 'arrow_forward', placement: 'right' }} />
  </div>
  <div class="row">
    <span class="hint">Icon buttons:</span>
    <Button style="icon" icon={{ name: 'add' }} ariaLabel="Icon button" />
    <Button style="icon" size="sm" icon={{ name: 'add' }} ariaLabel="Small icon button" />
    <Button style="icon" tint="#dc2626" icon={{ name: 'delete' }} ariaLabel="Tinted icon button" />
  </div>
</SectionCard>

<SectionCard title="Inputs">
  <div class="form-group">
    <label for="uit-text">Text input</label>
    <input id="uit-text" type="text" placeholder="Type here" bind:value={textVal} use:squircle={12} />
  </div>
  <div class="form-group">
    <label for="uit-pass">Password</label>
    <input id="uit-pass" type="password" placeholder="Secret" use:squircle={12} />
  </div>
  <div class="form-group">
    <label for="uit-area">Textarea</label>
    <textarea id="uit-area" placeholder="Multiline" use:squircle={12}></textarea>
  </div>
  <div class="form-group">
    <label for="uit-sel">Select</label>
    <select id="uit-sel" use:squircle={12}>
      <option>Alpha</option>
      <option>Beta</option>
    </select>
  </div>
  <div class="form-group">
    <label for="uit-dis">Disabled</label>
    <input id="uit-dis" type="text" value="Locked" disabled use:squircle={12} />
  </div>
  <p class="hint">Hint text. Value so far: <span class="mono">{textVal || '(empty)'}</span></p>
  <SecretInput id="uit-secret" label="Secret input" placeholder="sk-..." value={secretVal} oninput={(v) => { secretVal = v }} />
  <SearchField bind:value={searchVal} placeholder="Search the polygon" />
</SectionCard>

<SectionCard title="Toggles">
  <div class="row">
    <Switch bind:checked={switchOn} label="On switch" />
    <Switch bind:checked={switchOff} label="Off switch" />
    <Switch checked disabled label="Disabled" />
  </div>
  <div class="row">
    <label class="check-row">
      <input type="checkbox" class="check" bind:checked use:squircle={6} />
      <span>Project checkbox (.check, {checked ? 'on' : 'off'})</span>
    </label>
    <label class="check-row">
      <input type="checkbox" class="check" bind:checked={checkedOn} use:squircle={6} />
      <span>Checked sample ({checkedOn ? 'on' : 'off'})</span>
    </label>
  </div>
  <SegmentedControl bind:value={seg} ariaLabel="Demo segments" options={[
    { value: 'a', label: 'Alpha' },
    { value: 'b', label: 'Beta' },
    { value: 'c', label: 'Gamma' },
  ]} />
  <p class="hint">Segment: <span class="mono">{seg}</span></p>
</SectionCard>

<SectionCard title="Chips (badges merged here)">
  <div class="row">
    <span class="chip chip-sm chip-blue">sm blue</span>
    <span class="chip chip-sm chip-green">sm green</span>
    <span class="chip chip-sm chip-red">sm red</span>
  </div>
  <div class="row">
    <span class="chip chip-blue">blue</span>
    <span class="chip chip-green">green</span>
    <span class="chip chip-yellow">yellow</span>
    <span class="chip chip-red">red</span>
    <span class="chip chip-purple">purple</span>
    <span class="chip chip-teal">teal</span>
    <span class="chip chip-orange">orange</span>
    <span class="chip chip-neutral">neutral</span>
  </div>
  <div class="row">
    <span class="chip chip-lg chip-blue">lg blue</span>
    <span class="chip chip-lg chip-green">lg green</span>
    <span class="chip chip-lg chip-red">lg red</span>
    <span class="mono">mono text</span>
  </div>
  <div class="row">
    {#each tintedChips as tc}
      <span class="chip" style="background: {tc.bg}; color: {tc.text}">tint {tc.c}</span>
    {/each}
  </div>
</SectionCard>

<SectionCard title="Overlays">
  <div class="row">
    <div class="confirm-col">
      <button class="btn btn-secondary" onclick={askConfirm} use:squircle={12}>Confirm modal</button>
      <button class="btn btn-secondary" onclick={askTitleOnly} use:squircle={12}>Title only</button>
      <button class="btn btn-secondary" onclick={askNoTitle} use:squircle={12}>No title</button>
    </div>
    <button class="btn btn-secondary" onclick={openContentModal} use:squircle={12}>Content modal</button>
    <button class="btn btn-secondary" onclick={() => toast.success('Demo success toast')} use:squircle={12}>Success toast</button>
    <button class="btn btn-secondary" onclick={() => toast.error('Demo error toast')} use:squircle={12}>Error toast</button>
  </div>
  <div class="form-group">
    <label for="uit-toast-text">Toast text</label>
    <textarea id="uit-toast-text" rows={3} bind:value={toastText} use:squircle={12}></textarea>
  </div>
  <div class="row">
    <button class="btn btn-secondary" onclick={() => toast.success(toastText || '(empty)')} use:squircle={12}>Show toast</button>
  </div>
  <p class="hint">Confirm result: <span class="mono">{confirmResult}</span></p>
</SectionCard>

<SectionCard title="Dropdowns">
  <div class="row">
    <Dropdown bind:value={dropVal} options={[
      { value: 'one', label: 'Option one' },
      { value: 'two', label: 'Option two' },
      { value: 'three', label: 'Option three' },
    ]} />
    <ActionDropdown label="Demo actions" actions={[
      { id: 'edit', label: 'Edit', icon: 'edit' },
      { id: 'delete', label: 'Delete', icon: 'delete', danger: true },
    ]} onaction={(id) => { lastAction = id }} />
  </div>
  <p class="hint">Dropdown: <span class="mono">{dropVal}</span> · Action: <span class="mono">{lastAction}</span></p>
</SectionCard>

<SectionCard title="Dynamic form">
  <DynamicForm nodes={formNodes} bind:values={formValues} />
  <CodeBlock label="Live values" text={JSON.stringify(formValues, null, 2)} />
</SectionCard>

<SectionCard title="Cards">
  <div class="cards">
    <ProviderCard provider={sampleProvider} stats={sampleStats} onClick={() => toast.success('Provider clicked')} onToggle={(v) => toast.success(v ? 'Enabled' : 'Disabled')} />
    <PluginCard
      title="Demo plugin"
      meta="tester · 1.0.0"
      badges={[{ text: 'safe', kind: 'chip-green' }]}
      description="Uninstalled sample card."
      mode="uninstalled"
      installLabel="Install"
      onInstall={() => toast.success('Install clicked')}
      onDetails={() => toast.success('Details clicked')}
    />
    <PluginCard
      title="Demo plugin"
      meta="tester · 1.0.0"
      description="Installed sample card."
      mode="installed"
      actions={[{ id: 'remove', label: 'Remove', icon: 'delete', danger: true }]}
      onaction={(id) => { lastAction = id }}
      onDetails={() => toast.success('Details clicked')}
    />
  </div>
  <ModelCard
    model={agentModel}
    index={0}
    total={1}
    {availableModels}
    onChange={(next) => { agentModel = next }}
    onMoveUp={() => {}}
    onMoveDown={() => {}}
    onDelete={() => toast.error('Delete clicked')}
  />
  <div class="card-pad">
    <EmptyState icon="search_off" message="Nothing here" hint="Demo empty state." buttonText="Do thing" buttonIcon="add" onButtonClick={() => toast.success('Empty action clicked')} />
  </div>
</SectionCard>

<SectionCard title="Table and messages">
  <div class="table" use:squircle={18}>
    <table>
      <thead><tr><th>Name</th><th>Status</th><th>Count</th></tr></thead>
      <tbody>
        <tr><td>Alpha</td><td><span class="chip chip-green">ok</span></td><td>12</td></tr>
        <tr><td>Beta</td><td><span class="chip chip-red">fail</span></td><td>3</td></tr>
      </tbody>
    </table>
  </div>
  <div class="error-msg">Demo error message.</div>
  <div class="success-msg">Demo success message.</div>
</SectionCard>
</div>

<style>
  /* Composer owns spacing: widgets render marginless, the stack gaps them. */
  :global(body.uit-chrysanthemum) {
    font-family: var(--uit-lab-font, 'Chrysanthemum'), 'Inter', system-ui, -apple-system, sans-serif;
  }
  .uit-stack {
    display: flex;
    flex-direction: column;
    gap: 24px;
  }
  .uit-stack > .page-header {
    margin-bottom: 8px;
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    align-items: center;
    margin-bottom: 16px;
  }
  .row:last-child {
    margin-bottom: 0;
  }
  .metric-row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    align-items: flex-end;
  }
  .metric-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12px;
    color: var(--color-text-soft);
  }
  .metric-field input {
    width: 90px;
  }
  .check-row {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 14px;
    cursor: pointer;
    user-select: none;
  }
  .form-group {
    margin-bottom: 12px;
  }
  .cards {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 16px;
  }
  .card-pad {
    border-top: 1px solid var(--color-outline-soft);
    margin-top: 4px;
  }
  .table {
    margin-bottom: 16px;
    border-radius: var(--radius-lg);
  }
  .error-msg {
    margin-top: 12px;
  }
  .success-msg {
    margin-top: 12px;
  }
  .confirm-col {
    display: flex;
    flex-direction: column;
    gap: 8px;
    align-items: flex-start;
  }
</style>
