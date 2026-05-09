<script lang="ts">
  import { createEventDispatcher } from 'svelte'
  import { api } from '../lib/api'

  const dispatch = createEventDispatcher<{ done: void }>()

  let username: string = ''
  let password: string = ''
  let password2: string = ''
  let error: string = ''
  let loading: boolean = false

  async function submit(): Promise<void> {
    error = ''
    if (password !== password2) { error = 'Passwords do not match.'; return }
    if (password.length < 8) { error = 'Password must be at least 8 characters.'; return }
    loading = true
    try {
      await api.bootstrap(username, password)
      dispatch('done')
    } catch (e) {
      error = (e as Error).message || 'Failed to create account.'
    } finally {
      loading = false
    }
  }
</script>

<div class="auth-wrap">
  <div class="auth-card">
    <div class="brand">llm-router</div>
    <h1>Create admin account</h1>
    <p class="sub">First run — set up your dashboard credentials.</p>

    {#if error}
      <div class="error-msg">{error}</div>
    {/if}

    <div class="form-group">
      <label for="u">Username</label>
      <input id="u" type="text" bind:value={username} autocomplete="username" />
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p">Password</label>
      <input id="p" type="password" bind:value={password} autocomplete="new-password" />
    </div>
    <div class="form-group" style="margin-top: 12px;">
      <label for="p2">Confirm password</label>
      <input id="p2" type="password" bind:value={password2} autocomplete="new-password" on:keydown={(e) => e.key === 'Enter' && submit()} />
    </div>
    <button class="btn btn-primary submit-btn" on:click={submit} disabled={loading}>
      {loading ? 'Creating…' : 'Create account'}
    </button>
  </div>
</div>

<style>
  .auth-wrap {
    min-height: 100vh;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-background);
  }
  .auth-card {
    background: var(--color-surface);
    border: 1px solid var(--color-outline-light);
    border-radius: 16px;
    padding: 40px;
    width: 100%;
    max-width: 400px;
    box-shadow: var(--shadow-md);
  }
  .brand {
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--color-text);
    margin-bottom: 24px;
  }
  h1 { 
    font-size: 24px; 
    font-weight: 600; 
    margin-bottom: 6px;
    color: var(--color-text);
  }
  .sub { 
    color: var(--color-text-soft); 
    font-size: 14px; 
    margin-bottom: 24px; 
  }
  .submit-btn { 
    width: 100%; 
    justify-content: center; 
    margin-top: 20px; 
    height: 40px;
  }
</style>
