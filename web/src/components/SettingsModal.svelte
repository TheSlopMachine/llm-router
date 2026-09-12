<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { theme } from '../lib/theme.svelte'
  import type { Theme } from '../lib/theme.svelte'
  import { language } from '../lib/language.svelte'
  import type { Language } from '../lib/language.svelte'
  import { api } from '../lib/api'
  import { t } from '../lib/i18n.svelte'
  import { getErrorMessage } from '../lib/errors'
  import Dropdown from './Dropdown.svelte'
  import Switch from './ui/Switch.svelte'

  let {
    updateButtons,
    closeModal
  } = $props<{
    updateButtons: (buttons: import('../lib/modal.svelte').ModalButton[]) => void
    closeModal: () => void
  }>()

  let languageOptions = $derived([
    { value: 'auto', label: t('Auto') },
    { value: 'en', label: t('English') },
    { value: 'ru', label: t('Russian') }
  ])
  let themeOptions: Array<{ value: string; label: string }> = $derived([
    { value: 'auto', label: t('Auto') },
    { value: 'light', label: t('Light') },
    { value: 'dark', label: t('Dark') }
  ])
  let lang = $state<Language>(language.value)
  let th = $state<Theme>(theme.value)
  let isClusterNode = $state(false)
  let disableTelemetry = $state(false)
  let maxRetries = $state(7)
  let saving = $state(false)
  let error = $state('')

  onMount(async () => {
    try {
      const cfg = await api.config.get()
      isClusterNode = cfg.is_cluster_node
      disableTelemetry = cfg.disable_telemetry
      maxRetries = cfg.max_retries
    } catch (e) {
      error = getErrorMessage(e)
    }
  })

  function syncButtons(): void {
    untrack(() =>
      updateButtons([
        { label: t('Cancel'), variant: 'secondary', onClick: closeModal, disabled: saving },
        { label: saving ? t('Saving...') : t('Save changes'), variant: 'primary', onClick: save, loading: saving }
      ])
    )
  }

  onMount(() => {
    syncButtons()
  })

  $effect(() => {
    void saving
    untrack(() => syncButtons())
  })

  async function save(): Promise<void> {
    if (saving) return
    saving = true
    error = ''
    syncButtons()
    try {
      language.value = lang
      theme.value = th
      await api.config.update({
        is_cluster_node: isClusterNode,
        disable_telemetry: disableTelemetry,
        max_retries: Number(maxRetries)
      })
      closeModal()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
      syncButtons()
    }
  }
</script>

{#if error}
  <div class="error-msg">{error}</div>
{/if}

<div class="settings">
  <section class="settings-section">
    <h3>{t('Preferences')}</h3>
    <p class="section-hint">{t('This browser only')}</p>

    <div class="field">
      <label for="settings-language">{t('Language')}</label>
      <Dropdown value={lang} options={languageOptions} onchange={(v) => (lang = v as Language)} />
    </div>

    <div class="field">
      <label for="settings-theme">{t('Theme')}</label>
      <Dropdown value={th} options={themeOptions} onchange={(v) => (th = v as Theme)} />
    </div>
  </section>

  <hr class="divider" />

  <section class="settings-section">
    <h3>{t('Instance configuration')}</h3>
    <p class="section-hint">{t('This deployment')}</p>

    <div class="field-row">
      <span class="field-label">{t('Make this instance a cluster node')}</span>
      <Switch bind:checked={isClusterNode} />
    </div>

    <div class="field-row">
      <span class="field-label">{t('Disable anonymized telemetry')}</span>
      <Switch bind:checked={disableTelemetry} />
    </div>

    <div class="field">
      <label for="settings-max-retries">{t('Max retries')}</label>
      <input id="settings-max-retries" type="number" min="0" max="20" step="1" bind:value={maxRetries} />
    </div>
  </section>
</div>

<style>
  .settings {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  .settings-section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .settings-section h3 {
    font-size: 14px;
    font-weight: 600;
    color: var(--color-text);
    margin: 0;
  }

  .section-hint {
    font-size: 12px;
    color: var(--color-text-soft);
    margin: 0;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .field label,
  .field-label {
    font-size: 12px;
    font-weight: 500;
    color: var(--color-text-soft);
  }

  .field-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
</style>
