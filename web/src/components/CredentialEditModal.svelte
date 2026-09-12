<script lang="ts">
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { t } from '../lib/i18n.svelte'
  import type { Credential } from '../lib/types'

  let {
    credential,
    onComplete,
  } = $props<{
    credential: Credential
    onComplete: () => void
  }>()

  // The modal is opened per credential; the prop is the initial value only.
  // svelte-ignore state_referenced_locally
  let label = $state(credential.label)
  let saving = $state(false)
  let error = $state('')

  async function save(): Promise<void> {
    saving = true
    error = ''
    try {
      await api.credentials.update(credential.id, { label: label.trim() })
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      saving = false
    }
  }

  function onkeydown(e: KeyboardEvent): void {
    if (e.key === 'Enter') save()
  }
</script>

<div class="form">
  {#if error}
    <div class="error-msg">{error}</div>
  {/if}
  <label class="field">
    <span class="field-label">{t('Name')}</span>
    <input class="field-input" type="text" bind:value={label} {onkeydown} placeholder={t('e.g. Work account')} />
  </label>
  <div class="actions">
    <button class="btn btn-primary" onclick={save} disabled={saving}>
      {saving ? t('Saving…') : t('Save')}
    </button>
  </div>
</div>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .field-label {
    font-size: 13px;
    font-weight: 500;
    color: var(--color-text-soft);
  }
  .field-input {
    padding: 8px 12px;
    border: 1px solid var(--color-outline-light);
    border-radius: 8px;
    background: var(--color-surface);
    color: var(--color-text);
    font-family: inherit;
    font-size: 14px;
  }
  .field-input:focus {
    outline: 2px solid var(--color-accent);
    outline-offset: -1px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
</style>
