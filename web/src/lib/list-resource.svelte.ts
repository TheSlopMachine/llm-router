import { onMount } from 'svelte'
import { getErrorMessage } from './errors'

/**
 * Loads a list for a page: `data`, `loading`, `error` and `reload`.
 * Loads in `onMount`, not in an `$effect` (AGENTS.md §8).
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
