<script lang="ts">
  // Body of the Add custom model modal: a model the provider serves but does not list.
  import { Banner, Button, Field, Form, HStack, ModalChrome, TextField } from '@fuckyouryan/fui'
  import { api } from '$lib/api'
  import { getErrorMessage } from '$lib/errors'
  import { t } from '$lib/i18n.svelte'
  import { CAPABILITY_META } from '$lib/capabilities'

  let { providerId, onComplete, closeModal }: { providerId: string; onComplete: () => void; closeModal: () => void } = $props()

  const KNOWN_CAPABILITIES = ['tools', 'vision', 'audio', 'json_mode', 'structured_outputs', 'reasoning']

  let id = $state('')
  let name = $state('')
  let capabilities = $state<string[]>([])
  let adding = $state(false)
  let error = $state('')

  function toggleCapability(capability: string): void {
    capabilities = capabilities.includes(capability) ? capabilities.filter((c) => c !== capability) : [...capabilities, capability]
  }

  async function add(): Promise<void> {
    if (!id.trim() || adding) return
    adding = true
    error = ''
    try {
      await api.models.setOverride(providerId, id.trim(), { custom: true, display_name: name.trim(), capabilities })
      onComplete()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      adding = false
    }
  }
</script>

<ModalChrome subtitle={t('models.custom.not_listed')} buttons={[
  { label: t('common.actions.cancel'), buttonStyle: 'neutral', onclick: closeModal, disabled: adding },
  { label: t('models.actions.add'), buttonStyle: 'prominent', onclick: add, disabled: !id.trim(), loading: adding },
]} />

{#if error}
  <Banner tint="danger" text={error} />
{/if}
<Form>
  <Field label={t('models.custom.id_hint')}>
    <TextField bind:value={id} autocomplete="off" />
  </Field>
  <Field label={t('models.custom.display_name')}>
    <TextField bind:value={name} autocomplete="off" />
  </Field>
  <HStack spacing="tight" wrap valign="center" controlSize="sm">
    {#each KNOWN_CAPABILITIES as capability (capability)}
      {@const meta = CAPABILITY_META[capability]}
      <Button
        selected={capabilities.includes(capability)}
        tint={meta?.tint}
        icon={meta?.icon ? { name: meta.icon } : undefined}
        title={meta ? t(meta.hint) : capability}
        onclick={() => toggleCapability(capability)}
      >{meta ? t(meta.label) : capability}</Button>
    {/each}
  </HStack>
</Form>
