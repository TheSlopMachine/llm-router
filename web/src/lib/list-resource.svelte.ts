import { onMount } from 'svelte'
import { getErrorMessage } from './errors'

/**
 * Generic list-loader for CRUD pages (Providers / Tokens / Agents).
 * Collapse of the repeated pattern:
 *   let loading = $state(true); let error = $state('')
 *   async function load() { loading=true; error=''; try{ data=await fetcher() } catch(e){ error=getErrorMessage(e)} finally{ loading=false } }
 *   onMount(load)
 *
 * Properly generic now that `api` is typed (P1 done) — no `any`.
 * Uses `$state` + `onMount` (not `$effect`) per AGENTS.md §4.
 */
export function createListResource<T>(fetcher: () => Promise<T>, initialValue: T) {
  let data = $state<T>(initialValue)
  let loading = $state(true)
  let error = $state('')

  async function reload(): Promise<void> {
    loading = true
    error = ''
    try {
      data = await fetcher()
    } catch (e) {
      error = getErrorMessage(e)
    } finally {
      loading = false
    }
  }

  onMount(reload)

  return {
    get data(): T {
      return data
    },
    set data(v: T) {
      data = v
    },
    get loading(): boolean {
      return loading
    },
    get error(): string {
      return error
    },
    set error(v: string) {
      error = v
    },
    reload,
  }
}
