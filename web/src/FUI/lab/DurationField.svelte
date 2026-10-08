<script lang="ts">
// DurationField: ввод "1h 30m 15s" -> секунды (bind). Пропсы: value, allowedUnits, placeholder, locale.
  let { value = $bindable(0), allowedUnits = ['h', 'm', 's'], placeholder = '', locale = 'en' } = $props<{ value?: number; allowedUnits?: string[]; placeholder?: string; locale?: string }>();
  let raw = $state(''); let bad = $state(false); let dirty = $state(false);
  const show = $derived(dirty ? raw : fmtDur(value));
  function fmtDur(s: number): string {
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60), sec = Math.round(s % 60); const p: string[] = [];
    if (h && allowedUnits.includes('h')) p.push(`${new Intl.NumberFormat(locale).format(h)}h`);
    if (m && allowedUnits.includes('m')) p.push(`${new Intl.NumberFormat(locale).format(m)}m`);
    if ((sec || !p.length) && allowedUnits.includes('s')) p.push(`${new Intl.NumberFormat(locale).format(sec)}s`);
    return p.join(' ');
  }
  function parse(t: string): number | null {
    const re = /(\d+(?:[.,]\d+)?)\s*([hms])/gi; let total = 0, hit = false, m;
    while ((m = re.exec(t))) { if (!allowedUnits.includes(m[2].toLowerCase())) return null; total += parseFloat(m[1].replace(',', '.')) * (m[2].toLowerCase() === 'h' ? 3600 : m[2].toLowerCase() === 'm' ? 60 : 1); hit = true; }
    return hit ? total : null;
  }
  function commit() { const v = parse(raw); if (v === null && raw.trim()) bad = true; else { bad = false; if (v !== null) value = v; dirty = false; } }
</script>
<input class="f" class:bad value={show} {placeholder} inputmode="numeric" aria-invalid={bad} oninput={(e) => { raw = e.currentTarget.value; dirty = true; bad = false; }} onblur={commit} onkeydown={(e) => e.key === 'Enter' && commit()} />
<style>
  :where(*, *::before, *::after) { box-sizing: border-box; margin: 0; padding: 0; }
  .f { height: var(--fui-ctl-medium); padding: var(--fui-field-pad-v) var(--fui-field-pad-h); font-family: var(--fui-font-mono); font-size: var(--fui-text-base); color: var(--fui-color-text); background: var(--fui-elev); border-radius: var(--fui-radius-md); }
  .f.bad { background: var(--fui-color-notification-error-bg); color: var(--fui-color-notification-error-text); }
  .f:focus-visible { box-shadow: var(--fui-focus-ring); }
</style>
