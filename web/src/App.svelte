<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from './lib/api'
  import { getErrorMessage } from './lib/errors'
  import Login from './pages/Login.svelte'
  import Bootstrap from './pages/Bootstrap.svelte'
  import Dashboard from './pages/Dashboard.svelte'
  import Modal from './FUI/composite/Modal.svelte'
  import Toasts from './FUI/composite/Toasts.svelte'
  import Banner from './FUI/composite/Banner.svelte'
  import { startSecureHardening } from './lib/secure'

  type AppState = 'loading' | 'bootstrap' | 'login' | 'dashboard'

  let appState: AppState = $state('loading')
  let error: string | null = $state(null)

  onMount(async (): Promise<void> => {
    startSecureHardening()
    const path = window.location.pathname

    try {
      const status = await api.status()

      if (!status.bootstrapped) {
        if (path !== '/bootstrap') {
          window.location.replace('/bootstrap')
          return
        }
        appState = 'bootstrap'
      } else if (!status.authenticated) {
        if (path !== '/login') {
          window.location.replace('/login')
          return
        }
        appState = 'login'
      } else {
        if (path !== '/') {
          window.location.replace('/' + (window.location.hash || '#/metrics'))
          return
        }
        if (!window.location.hash) {
          window.location.hash = '#/metrics'
        }
        appState = 'dashboard'
      }
    } catch (e) {
      error = getErrorMessage(e)
      if (path !== '/login') {
        window.location.replace('/login')
        return
      }
      appState = 'login'
    }
  })

  function onLogin(): void {
    window.location.replace('/#/metrics')
  }

  function onBootstrap(): void {
    window.location.replace('/login')
  }

  function onLogout(): void {
    window.location.replace('/login')
  }
</script>

{#if appState === 'loading'}
  <div class="splash">
    {#if error}
      <div style="max-width: var(--fui-splash-max-w); text-align: center;">
        <div style="font-size: var(--fui-text-lg); font-weight: 600; margin-bottom: var(--fui-splash-gap);">llm-router</div>
        <Banner variant="error" text={error} />
      </div>
    {:else}
      llm-router
    {/if}
  </div>
{:else if appState === 'bootstrap'}
  <Bootstrap ondone={onBootstrap} />
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
    font-size: var(--fui-text-lg);
    font-weight: 600;
    color: var(--fui-color-text-soft);
    letter-spacing: var(--fui-tracking-wide);
  }
</style>
