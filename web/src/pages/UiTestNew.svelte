<script lang="ts">
  import Switch from '../FUI/controls/Switch.svelte'
  import Button from '../FUI/controls/Button.svelte'
  import Picker from '../FUI/controls/Picker.svelte'
  import Select from '../FUI/controls/Select.svelte'
  import TextEdit from '../FUI/controls/TextEdit.svelte'
  import SectionCard from '../FUI/composite/SectionCard.svelte'
  import { Text } from '$ui'
  import { theme, setTheme } from '../FUI/core/theme.svelte'
  import { setAccent } from '../FUI/core/accent.svelte'
  import Field from '../FUI/composite/Field.svelte'
  import Accordion from '../FUI/composite/Accordion.svelte'
  import Pagination from '../FUI/composite/Pagination.svelte'
  import Tooltip from '../FUI/controls/Tooltip.svelte'
  import Badge from '../FUI/controls/Badge.svelte'
  import Kbd from '../FUI/controls/Kbd.svelte'
  import Slider from '../FUI/controls/Slider.svelte'
  import NumberField from '../FUI/controls/NumberField.svelte'
  import Progress from '../FUI/charts/Progress.svelte'
  import Skeleton from '../FUI/controls/Skeleton.svelte'
  import DescriptionList from '../FUI/composite/DescriptionList.svelte'
  import Timeline from '../FUI/composite/Timeline.svelte'
  import Sparkline from '../FUI/charts/Sparkline.svelte'
  import Gauge from '../FUI/charts/Gauge.svelte'
  import Donut from '../FUI/charts/Donut.svelte'
  import StackedBar from '../FUI/charts/StackedBar.svelte'
  import UptimeBar from '../FUI/charts/UptimeBar.svelte'
  import PercentileBar from '../FUI/charts/PercentileBar.svelte'
  import RateMeter from '../FUI/charts/RateMeter.svelte'
  import NumberTicker from '../FUI/controls/NumberTicker.svelte'
  import RelativeTime from '../FUI/controls/RelativeTime.svelte'
  import { size, SizeReader } from '../FUI/core/size.svelte'
  import { focusTrap } from '../FUI/core/focusTrap.svelte'
  import { clickOutside, hotkey } from '../FUI/core/clicks.svelte'

  let dark = $state(theme.value === 'dark')
  let accentHex = $state('#7c3aed')
  let accentBad = $state(false)
  function applyAccent() {
    if (!/^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/.test(accentHex.trim())) { accentBad = true; return }
    accentBad = false
    setAccent(accentHex.trim())
  }

  // Базовое
  let fieldLabel = $state('Name')
  let fieldHint = $state('Optional hint')
  let fieldError = $state('')
  let fieldRequired = $state(false)
  let fieldOrient = $state('vertical')
  let labelText = $state('Status label')
  let labelIcon = $state('star')
  let labelSize = $state('base')
  let labelTone = $state('default')
  let labelIconPos = $state('start')
  let groupTitle = $state('Group title')
  let tabsVal = $state('a')
  let tabsSize = $state('medium')
  let tabsFull = $state(false)
  let accMultiple = $state(true)
  let accOpen = $state<string[]>(['one'])
  let pgPage = $state(3)
  let pgCount = $state('12')
  let pgSiblings = $state('1')
  let pgEdges = $state(true)
  let tipText = $state('Helpful tooltip')
  let tipSide = $state('top')
  let badgeVal = $state('7')
  let badgeDot = $state(false)
  let badgeTone = $state('red')
  let badgePos = $state('tr')
  let avatarName = $state('Ada Lovelace')
  let avatarSize = $state('32')
  let kbdSize = $state('medium')
  let sliderVal = $state(40)
  let sliderShow = $state(true)
  let sliderDis = $state(false)
  let rangeVal = $state<[number, number]>([20, 80])
  let rangeGap = $state('0')
  let numVal = $state(3)
  let numStep = $state('1')
  let numUnit = $state('ms')
  let rateVal = $state(3.5)
  let rateMax = $state('5')
  let rateHalf = $state(true)
  let rateRO = $state(false)

  // Данные
  let progVal = $state('0.65')
  let progVariant = $state('bar')
  let progTone = $state('accent')
  let progIndet = $state(false)
  let skLines = $state('3')
  let skRedacted = $state(false)
  let skActive = $state(true)
  let statDelta = $state('12')
  let statInvert = $state(false)
  let dlCols = $state('2')
  let dlOrient = $state('horizontal')
  let tlAlign = $state('left')
  let sparkVariant = $state('line')
  let sparkTone = $state('accent')
  let sparkHi = $state(true)
  let gaugeVal = $state('0.7')
  let sbLegend = $state(true)
  let sbPct = $state(true)
  let pmP50 = $state('120')
  let pmP95 = $state('340')
  let pmP99 = $state('800')
  let rmUsed = $state('72')
  let rmLimit = $state('100')
  let tickerVal = $state(1234)
  let durVal = $state(5400)
  let digitsVal = $state('12:45')
  let digitsTone = $state('accent')

  // Лейауты и утилиты
  let wrapGap = $state('3')
  let arRatio = $state('1.777')
  let arFit = $state('cover')
  let masCols = $state('3')
  let swMode = $state('fade')
  let swAlt = $state(false)
  let alItems = $state<string[]>(['Alpha', 'Beta', 'Gamma'])
  let alDraft = $state('')
  let ivOnce = $state(true)
  let ivAnim = $state('fade-up')
  let vlH = $state('32')
  let vlOver = $state('4')
  let sortItems = $state<string[]>(['First', 'Second', 'Third'])
  let saMax = $state('160')
  let saHide = $state(true)
  let loActive = $state(true)
  let trapOn = $state(true)

  let lastClick = $state('(none)')
  let lastKey = $state('(none)')
  let sizeInfo = $state({ width: 0, height: 0 })
  const sr = new SizeReader()
  function attachSr(node: HTMLElement): void { sr.attach(node) }

  const big = Array.from({ length: 100000 }, (_, i) => `Row ${i + 1}`)
  let vlVisible = $derived(Math.ceil(320 / Math.max(1, Number(vlH) || 32)) + (Number(vlOver) || 0) * 2)
  const sparkVals = [4, 7, 5, 9, 6, 11, 8, 12]
  const crumbs = [{ label: 'Home' }, { label: 'Library' }, { label: 'Data' }]
  const donutItems = [
    { label: 'Blue', value: 40, tone: 'b' },
    { label: 'Green', value: 30, tone: 'g' },
    { label: 'Red', value: 20, tone: 'r' },
    { label: 'Yellow', value: 10, tone: 'y' },
  ]
  const barItems = [
    { label: 'A', value: 50, tone: 'b' },
    { label: 'B', value: 30, tone: 'g' },
    { label: 'C', value: 20, tone: 'y' },
  ]
  const upDays = Array.from({ length: 14 }, (_, i) => ({
    date: `2026-09-${String(i + 1).padStart(2, '0')}`,
    status: (i % 5 === 4 ? 'degraded' : i % 9 === 8 ? 'down' : 'ok') as 'ok' | 'degraded' | 'down',
  }))
  const tlItems = [
    { time: '09:00', title: 'Deploy started', body: 'Rollout begins', icon: 'star', tone: 'blue' as const },
    { time: '09:20', title: 'Health check', body: 'All green', icon: 'check_circle', tone: 'green' as const },
  ]
  const dlItems = [
    { key: 'Owner', value: 'Ada' },
    { key: 'Region', value: 'eu' },
    { key: 'Status', value: 'active' },
    { key: 'Plan', value: 'pro' },
  ]
  const stickySecs = [
    { title: 'A', items: ['Ada', 'Alan'] },
    { title: 'B', items: ['Bob', 'Bea'] },
  ]
</script>

<div class="stand">
  <div class="head">
    <div>
      <h1>UI lab stand</h1>
      <p>Hidden route #/ui-test-new. Live params under each widget.</p>
    </div>
    <div class="head-ctl">
      <Switch checked={dark} label="Dark theme" onchange={(v) => { dark = v; setTheme(v ? 'dark' : 'light') }} />
      <TextEdit bind:value={accentHex} hint="#hex accent" />
      <Button text="Apply" onclick={applyAccent} />
      {#if accentBad}<Text size="sm" tone="soft">Bad hex</Text>{/if}
    </div>
  </div>

  <details open class="grp">
    <summary>Базовое</summary>
    <div class="cards">
      <SectionCard title="Field">
        <Field label={fieldLabel} hint={fieldHint} error={fieldError} required={fieldRequired} orientation={fieldOrient as 'vertical' | 'horizontal'}>
          <TextEdit bind:value={fieldLabel} hint="Inner control" />
        </Field>
        <div class="row">
          <TextEdit bind:value={fieldHint} hint="Hint" />
          <TextEdit bind:value={fieldError} hint="Error" />
          <Switch bind:checked={fieldRequired} label="Required" />
          <Picker bind:value={fieldOrient} ariaLabel="Orientation" options={[{ value: 'vertical', label: 'vertical' }, { value: 'horizontal', label: 'horizontal' }]} />
        </div>
      </SectionCard>




      <SectionCard title="Accordion">
        <Accordion items={[{ id: 'one', title: 'First', body: accA }, { id: 'two', title: 'Second', body: accB }]} multiple={accMultiple} bind:value={accOpen} />
        <div class="row">
          <Switch bind:checked={accMultiple} label="Multiple" />
          <Text size="sm">Open: {accOpen.join(', ') || '(none)'}</Text>
        </div>
      </SectionCard>


      <SectionCard title="Pagination">
        <Pagination bind:page={pgPage} pageCount={Number(pgCount) || 1} siblings={Number(pgSiblings) || 1} showEdges={pgEdges} />
        <div class="row">
          <TextEdit bind:value={pgCount} hint="Pages" />
          <Picker bind:value={pgSiblings} ariaLabel="Siblings" options={[{ value: '0', label: '0' }, { value: '1', label: '1' }, { value: '2', label: '2' }]} />
          <Switch bind:checked={pgEdges} label="Edges" />
        </div>
      </SectionCard>

      <SectionCard title="Tooltip">
        <Tooltip text={tipText} side={tipSide as 'top' | 'bottom' | 'left' | 'right'}><Button text="Hover me" /></Tooltip>
        <div class="row">
          <TextEdit bind:value={tipText} hint="Tooltip text" />
          <Picker bind:value={tipSide} ariaLabel="Side" options={['top', 'bottom', 'left', 'right'].map((v) => ({ value: v, label: v }))} />
        </div>
      </SectionCard>

      <SectionCard title="Badge">
        <Badge value={badgeVal} dot={badgeDot} tone={badgeTone as 'red' | 'blue' | 'green' | 'yellow' | 'purple' | 'teal' | 'orange'} position={badgePos as 'tr' | 'tl' | 'br' | 'bl'}>
          <Button text="Inbox" />
        </Badge>
        <div class="row">
          <TextEdit bind:value={badgeVal} hint="Value" />
          <Switch bind:checked={badgeDot} label="Dot" />
          <Select bind:value={badgeTone} options={['red', 'blue', 'green', 'yellow', 'purple', 'teal', 'orange'].map((v) => ({ value: v, label: v }))} />
          <Select bind:value={badgePos} options={['tr', 'tl', 'br', 'bl'].map((v) => ({ value: v, label: v }))} />
        </div>
      </SectionCard>


      <SectionCard title="Kbd">
        <Kbd keys={['Ctrl', 'K']} size={kbdSize as 'small' | 'medium' | 'large'} />
        <div class="row">
          <Picker bind:value={kbdSize} ariaLabel="Size" options={[{ value: 'small', label: 'small' }, { value: 'medium', label: 'medium' }, { value: 'large', label: 'large' }]} />
        </div>
      </SectionCard>

      <SectionCard title="Slider">
        <Slider bind:value={sliderVal} showValue={sliderShow} disabled={sliderDis} />
        <div class="row">
          <Switch bind:checked={sliderShow} label="Show value" />
          <Switch bind:checked={sliderDis} label="Disabled" />
          <Text size="sm">Value: {sliderVal}</Text>
        </div>
      </SectionCard>


      <SectionCard title="NumberField">
        <NumberField bind:value={numVal} step={Number(numStep) || 1} unit={numUnit} />
        <div class="row">
          <TextEdit bind:value={numStep} hint="Step" regex="^[0-9.]*$" />
          <TextEdit bind:value={numUnit} hint="Unit" />
        </div>
      </SectionCard>

    </div>
  </details>

  <details open class="grp">
    <summary>Данные</summary>
    <div class="cards">
      <SectionCard title="Progress">
        <Progress value={progIndet ? null : (Number(progVal) || 0)} variant={progVariant as 'bar' | 'ring' | 'semicircle'} tone={progTone as 'accent' | 'success' | 'warning' | 'danger'}>
          {#snippet label()}<Text size="sm">custom</Text>{/snippet}
        </Progress>
        <div class="row">
          <TextEdit bind:value={progVal} hint="0..1" regex="^[0-9.]*$" />
          <Picker bind:value={progVariant} ariaLabel="Variant" options={['bar', 'ring', 'semicircle'].map((v) => ({ value: v, label: v }))} />
          <Select bind:value={progTone} options={['accent', 'success', 'warning', 'danger'].map((v) => ({ value: v, label: v }))} />
          <Switch bind:checked={progIndet} label="Indeterminate" />
        </div>
      </SectionCard>

      <SectionCard title="Skeleton">
        <Skeleton shape="text" lines={Number(skLines) || 1} />
        <Skeleton redacted active={skActive}><Text size="sm">Secret line</Text></Skeleton>
        <div class="row">
          <TextEdit bind:value={skLines} hint="Lines" regex="^[0-9.]*$" />
          <Switch bind:checked={skRedacted} label="Redacted demo below" />
          <Switch bind:checked={skActive} label="Redact active" />
        </div>
        {#if skRedacted}<Skeleton redacted active={skActive}><Text size="sm">Hidden value</Text></Skeleton>{/if}
      </SectionCard>


      <SectionCard title="DescriptionList">
        <DescriptionList items={dlItems} columns={Number(dlCols) || 1} orientation={dlOrient as 'horizontal' | 'vertical'} />
        <div class="row">
          <Picker bind:value={dlCols} ariaLabel="Columns" options={[{ value: '1', label: '1' }, { value: '2', label: '2' }]} />
          <Picker bind:value={dlOrient} ariaLabel="Orientation" options={[{ value: 'horizontal', label: 'horizontal' }, { value: 'vertical', label: 'vertical' }]} />
        </div>
      </SectionCard>

      <SectionCard title="Timeline">
        <Timeline items={tlItems} align={tlAlign as 'left' | 'alternate'} />
        <div class="row">
          <Picker bind:value={tlAlign} ariaLabel="Align" options={[{ value: 'left', label: 'left' }, { value: 'alternate', label: 'alternate' }]} />
        </div>
      </SectionCard>

      <SectionCard title="Sparkline">
        <Sparkline values={sparkVals} variant={sparkVariant as 'line' | 'area' | 'bars'} tone={sparkTone} highlightLast={sparkHi} />
        <div class="row">
          <Picker bind:value={sparkVariant} ariaLabel="Variant" options={['line', 'area', 'bars'].map((v) => ({ value: v, label: v }))} />
          <Select bind:value={sparkTone} options={['accent', 'ok', 'err', 'warn', 'b', 'g', 'r', 'y'].map((v) => ({ value: v, label: v }))} />
          <Switch bind:checked={sparkHi} label="Highlight last" />
        </div>
      </SectionCard>

      <SectionCard title="Gauge">
        <Gauge value={Number(gaugeVal) || 0} label={'Load ' + gaugeVal} thresholds={[{ at: 0.5, tone: 'warn' }, { at: 0.85, tone: 'err' }]} />
        <div class="row"><TextEdit bind:value={gaugeVal} hint="0..1" regex="^[0-9.]*$" /></div>
      </SectionCard>

      <SectionCard title="Donut">
        <Donut items={donutItems}>
          {#snippet centerLabel()}<span class="ctr">100</span>{/snippet}
        </Donut>
        <div class="row"><Text size="sm">Static 4 segments + centerLabel</Text></div>
      </SectionCard>

      <SectionCard title="StackedBar">
        <StackedBar items={barItems} showLegend={sbLegend} showPercent={sbPct} />
        <div class="row">
          <Switch bind:checked={sbLegend} label="Legend" />
          <Switch bind:checked={sbPct} label="Percent" />
        </div>
      </SectionCard>

      <SectionCard title="UptimeBar">
        <UptimeBar days={upDays} />
        <div class="row"><Text size="sm">14 cells, hover for date</Text></div>
      </SectionCard>

      <SectionCard title="PercentileBar">
        <PercentileBar p50={Number(pmP50) || 0} p95={Number(pmP95) || 0} p99={Number(pmP99) || 0} unit="ms" />
        <div class="row">
          <TextEdit bind:value={pmP50} hint="p50" regex="^[0-9.]*$" />
          <TextEdit bind:value={pmP95} hint="p95" regex="^[0-9.]*$" />
          <TextEdit bind:value={pmP99} hint="p99" regex="^[0-9.]*$" />
        </div>
      </SectionCard>

      <SectionCard title="RateMeter">
        <RateMeter used={Number(rmUsed) || 0} limit={Number(rmLimit) || 100} resetIn="resets soon" />
        <div class="row">
          <TextEdit bind:value={rmUsed} hint="Used" regex="^[0-9.]*$" />
          <TextEdit bind:value={rmLimit} hint="Limit" regex="^[0-9.]*$" />
        </div>
      </SectionCard>

      <SectionCard title="NumberTicker">
        <NumberTicker value={tickerVal} />
        <div class="row">
          <Button text="+100" onclick={() => { tickerVal += 100 }} />
          <Button text="Reset" onclick={() => { tickerVal = 0 }} />
        </div>
      </SectionCard>

      <SectionCard title="RelativeTime">
        <RelativeTime date={Date.now() - 5 * 60 * 1000} locale="en" />
        <div class="row"><Text size="sm">Fixed: 5 min ago, auto-updates</Text></div>
      </SectionCard>


    </div>
  </details>

  <details open class="grp">
    <summary>Лейауты и утилиты</summary>
    <div class="cards">













      <SectionCard title="size action">
        <div class="demo-box" use:size={(s) => { sizeInfo = s }}>
          Resize window — box reports its size
        </div>
        <div class="row"><Text size="sm">Last: {sizeInfo.width}×{sizeInfo.height}</Text></div>
      </SectionCard>

      <SectionCard title="SizeReader">
        <div class="demo-box" use:attachSr>Tracked by SizeReader class</div>
        <div class="row"><Text size="sm">Reader: {sr.width}×{sr.height}</Text></div>
      </SectionCard>

      <SectionCard title="focusTrap">
        <div class="demo-box" use:focusTrap={trapOn}>
          <Button text="First" /><Button text="Second" /><Button text="Third" />
        </div>
        <div class="row">
          <Switch bind:checked={trapOn} label="Active" />
          <Text size="sm">Tab cycles inside</Text>
        </div>
      </SectionCard>

      <SectionCard title="clickOutside + hotkey">
        <div class="demo-box" use:clickOutside={() => { lastClick = 'outside ' + new Date().toLocaleTimeString() }}>
          Click inside vs outside this box
        </div>
        <div class="row" use:hotkey={{ combo: 'mod+k', handler: () => { lastKey = 'mod+k ' + new Date().toLocaleTimeString() } }}>
          <Text size="sm">Outside: {lastClick}</Text>
          <Text size="sm">Hotkey: {lastKey}</Text>
        </div>
      </SectionCard>
    </div>
  </details>
</div>

{#snippet accA()}<Text size="sm">First body — single open unless multiple.</Text>{/snippet}
{#snippet accB()}<Text size="sm">Second body with static text.</Text>{/snippet}
{#snippet vtfWide()}<Text size="sm">Wide variant: full toolbar with extra actions visible</Text>{/snippet}
{#snippet vtfNarrow()}<Text size="sm">Narrow</Text>{/snippet}

<style>
  .stand { display: flex; flex-direction: column; gap: var(--fui-space-6); }
  .head { display: flex; flex-wrap: wrap; gap: var(--fui-space-4); align-items: flex-end; justify-content: space-between; }
  .head h1 { font-size: var(--fui-text-xl); color: var(--fui-color-text); margin: 0; }
  .head p { color: var(--fui-color-text-soft); font-size: var(--fui-text-sm); margin: 0; }
  .head-ctl { display: flex; flex-wrap: wrap; gap: var(--fui-space-4); align-items: center; }
  .grp { border: 1px solid var(--fui-color-outline-light); border-radius: var(--fui-radius-lg); padding: var(--fui-space-4); background: var(--fui-color-surface); }
  .grp summary { cursor: pointer; font-size: var(--fui-text-md); color: var(--fui-color-text); padding: var(--fui-space-2); }
  .cards { display: flex; flex-direction: column; gap: var(--fui-space-6); margin-top: var(--fui-space-4); }
  .row { display: flex; flex-wrap: wrap; gap: var(--fui-space-4); align-items: center; }
  .demo-box { padding: var(--fui-space-4); background: var(--fui-color-surface-container-high); border-radius: var(--fui-radius-sm); color: var(--fui-color-text); font-size: var(--fui-text-sm); display: flex; gap: var(--fui-space-3); flex-wrap: wrap; align-items: center; }
  .ar-fill { display: flex; align-items: center; justify-content: center; background: var(--fui-color-accent-soft); color: var(--fui-color-text); font-size: var(--fui-text-sm); }
  .m { padding: var(--fui-space-3); background: var(--fui-color-surface-container-high); border-radius: var(--fui-radius-xs); color: var(--fui-color-text); font-size: var(--fui-text-sm); }
  .m.tall { padding: var(--fui-space-6) var(--fui-space-3); }
  .sa-row { padding: var(--fui-space-1) 0; color: var(--fui-color-text); font-size: var(--fui-text-sm); }
  .ctr { font-size: var(--fui-text-sm); color: var(--fui-color-text); }
</style>
