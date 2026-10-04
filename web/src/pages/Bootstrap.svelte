<script lang="ts">
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { Button, TextEdit, Banner, VStack, Text } from '$ui'
  import AuthCard from '../components/AuthCard.svelte'

  let { ondone } = $props<{ ondone: () => void }>()

  let username = $state('')
  let password = $state('')
  let password2 = $state('')
  let error = $state('')
  let loading = $state(false)
  let passwordsMatch = $derived(password === password2)
  let showLengthHint = $derived(password.length > 0 && password.length < 8)

  async function submit(): Promise<void> {
    error = ''
    if (!passwordsMatch) { error = 'Passwords do not match.'; return }
    loading = true
    try {
      await api.bootstrap(username, password)
      ondone?.()
    } catch (e) {
      error = getErrorMessage(e) || 'Failed to create account.'
    } finally {
      loading = false
    }
  }
</script>

<AuthCard title="Create admin account" subtitle="First run — set up your dashboard credentials.">
  <VStack gap={4}>
    {#if error}
      <Banner variant="error" text={error} />
    {/if}

    <VStack gap={1}>
      <label for="u">Username</label>
      <TextEdit id="u" bind:value={username} autocomplete="username" />
    </VStack>
    <VStack gap={1}>
      <label for="p">Password</label>
      <TextEdit id="p" type="secret" bind:value={password} autocomplete="new-password" />
      {#if showLengthHint}
        <Text size="sm" tone="warning">Recommendation: use at least 8 characters for a stronger password.</Text>
      {/if}
    </VStack>
    <VStack gap={1}>
      <label for="p2">Confirm password</label>
      <TextEdit id="p2" type="secret" bind:value={password2} autocomplete="new-password" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()} />
    </VStack>
    <Button style="prominent" block onclick={submit} disabled={loading}>{loading ? 'Creating…' : 'Create account'}</Button>
  </VStack>
</AuthCard>
