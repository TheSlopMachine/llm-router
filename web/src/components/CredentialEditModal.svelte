<script lang="ts">
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { t } from '../lib/i18n.svelte'
  import { squircle } from '../lib/squircle'
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
  <div class="form-group">
    <label for="cred-edit-label">{t('Name')}</label>
    <input id="cred-edit-label" type="text" bind:value={label} {onkeydown} placeholder={t('e.g. Work account')} use:squircle={12} />
  </div>
  <div class="actions">
    <button class="btn btn-primary" onclick={save} disabled={saving} use:squircle={12}>
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
  .actions {
    display: flex;
    justify-content: flex-end;
  }
</style>
