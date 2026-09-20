<script lang="ts">
  import VirtualModelEditor from '../components/VirtualModelEditor.svelte'
  import { squircle } from '../lib/squircle'
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import type { VirtualModel } from '../lib/types'
  import { t } from '../lib/i18n.svelte'

  let { vmId = null } = $props<{ vmId: string | null }>()

  let vm = $state<VirtualModel | undefined>(undefined)
  let loading = $state(false)
  let error = $state('')

  async function loadVirtualModel() {
    error = ''
    vm = undefined

    if (!vmId) {
      return
    }

    loading = true
    try {
      vm = await api.virtualModels.get(vmId) as VirtualModel
    } catch (e: unknown) {
      const msg = getErrorMessage(e)
      if ((e as { status?: number })?.status === 401 || msg.includes('unauthenticated')) {
        window.location.href = '/login'
        return
      }
      error = msg
    } finally {
      loading = false
    }
  }

  function backToList() {
    window.location.hash = '#/virtual'
  }

  $effect(() => {
    void vmId
    void loadVirtualModel()
  })
</script>

<div class="page-header">
  <div>
    <h1>{vmId ? t('Edit virtual model') : t('New virtual model')}</h1>
    <p>{vmId ? t('Update the fall-through list and instruction for this virtual model.') : t('Create a virtual model that routes requests across multiple models with fall-through.')}</p>
  </div>
</div>

{#if error}
  <div class="error-msg" use:squircle={12}>{error}</div>
{:else if loading}
  <div class="loading">{t('Loading virtual model...')}</div>
{:else}
  <VirtualModelEditor
    {vm}
    onComplete={backToList}
    onCancel={backToList}
  />
{/if}
