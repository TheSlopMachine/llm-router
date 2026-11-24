<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from './lib/api'
  import { onSessionExpired } from './lib/api-client'
  import { getErrorMessage } from './lib/errors'
  import Login from './pages/Login.svelte'
  import Dashboard from './pages/Dashboard.svelte'
  import { Modal, Toasts, Banner, Text, VStack } from '@fuckyouryan/fui'

  type AppState = 'loading' | 'login' | 'dashboard'

  let appState: AppState = $state('loading')
  let error: string | null = $state(null)

  onMount(async (): Promise<void> => {
    try {
      const status = await api.status()
      if (status.authenticated) {
        showDashboard(window.location.hash || '#/metrics')
      } else {
        showLogin()
      }
    } catch (e) {
      error = getErrorMessage(e)
    }
  })

  // The login form and the dashboard switch in place: the address changes
  // through the history API, so the document never loads a second time.
  function showLogin(): void {
    if (window.location.pathname !== '/login' || window.location.hash) {
      window.history.replaceState(null, '', '/login')
    }
    appState = 'login'
  }

  function showDashboard(hash: string): void {
    if (window.location.pathname !== '/' || window.location.hash !== hash) {
      window.history.replaceState(null, '', '/' + hash)
    }
    appState = 'dashboard'
  }

  // A session that expires while the dashboard is open brings back the login form.
  onSessionExpired(showLogin)

  function onLogin(): void {
    showDashboard('#/metrics')
  }

  function onLogout(): void {
    showLogin()
  }
</script>

{#if appState === 'loading'}
  <div class="splash">
    <VStack gap={5} width="full" maxWidth="var(--app-splash-max-w)">
      <Text size="xl" weight="bold" foregroundStyle="secondary" align="center">llm-router</Text>
      {#if error}
        <Banner tint="danger" text={error} />
      {/if}
    </VStack>
  </div>
{:else if appState === 'login'}
  <Login ondone={onLogin} />
{:else}
  <Dashboard onlogout={onLogout} />
{/if}

<Modal />
<Toasts />

<style>
  .splash {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100vh;
    height: 100dvh;
  }
</style>
