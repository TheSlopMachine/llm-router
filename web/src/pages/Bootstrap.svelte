<script lang="ts">
  import { api } from '../lib/api'
  import { getErrorMessage } from '../lib/errors'
  import { Button, TextField, Banner, VStack, Text } from '@fuckyouryan/fui'
  import AuthCard from '../components/AuthCard.svelte'

  let { ondone }: { ondone: () => void } = $props()

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
      <Banner tint="danger" text={error} />
    {/if}

    <VStack gap={1}>
      <label for="u">Username</label>
      <TextField id="u" name="username" bind:value={username} autocomplete="username" />
    </VStack>
    <VStack gap={1}>
      <label for="p">Password</label>
      <TextField id="p" secure name="new-password" bind:value={password} autocomplete="new-password" />
      {#if showLengthHint}
        <Text size="sm" tint="warning">Recommendation: use at least 8 characters for a stronger password.</Text>
      {/if}
    </VStack>
    <VStack gap={1}>
      <label for="p2">Confirm password</label>
      <TextField id="p2" secure name="new-password-confirm" bind:value={password2} autocomplete="new-password" onkeydown={(e: KeyboardEvent) => e.key === 'Enter' && submit()} />
    </VStack>
    <Button buttonStyle="prominent" block onclick={submit} disabled={loading}>{loading ? 'Creating…' : 'Create account'}</Button>
  </VStack>
</AuthCard>
